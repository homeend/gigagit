package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// postAny posts JSON and decodes the response body REGARDLESS of status —
// the repairable handshake rides on a 409 (postJSON only decodes 2xx).
func postAny(t *testing.T, ts *httptest.Server, path, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestRerootCrossEnvRepair exercises the real cross-environment lane on a
// WSL machine: a repo under /mnt/<x>/… whose worktree link records are
// rewritten to the Windows notation of the SAME location (what a worktree
// created from Windows gg looks like from WSL, and vice versa). The reroot
// must refuse with the repairable handshake, then — confirmed — run a real
// `git worktree repair` and switch. Skips where no writable /mnt/<x> disk
// exists (non-WSL CI): the pure translation logic is covered by
// internal/worktree tests either way.
func TestRerootCrossEnvRepair(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	base, err := os.MkdirTemp("/mnt/t", "gg-crossenv-*")
	if err != nil {
		t.Skipf("no writable /mnt/t (not a WSL shared disk): %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })

	main := filepath.Join(base, "repo")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, main, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(main, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, main, "add", "-A")
	gitRun(t, main, "commit", "-m", "c1")
	wt := filepath.Join(base, "wt")
	gitRun(t, main, "worktree", "add", "-b", "side", wt)

	// Rewrite BOTH link records to the Windows notation of the same
	// location — the state a Windows-created worktree presents to WSL.
	toWin := func(p string) string { return "T:" + strings.ReplaceAll(strings.TrimPrefix(p, "/mnt/t"), "/", `\`) }
	admin := filepath.Join(main, ".git", "worktrees", "wt")
	if err := os.WriteFile(filepath.Join(admin, "gitdir"), []byte(toWin(wt)+`\.git`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+toWin(admin)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := New(domain.Open(main))
	ts := serve(t, srv)

	// The client posts what the sidebar shows, which is what git derives
	// from the broken backslash admin record: the worktree path WITH a
	// trailing \.git it could not strip.
	reported := toWin(wt) + `\.git`

	// Leg 1: the switch refuses with the structured repairable handshake —
	// never the raw chdir error the user hit.
	code, got := postAny(t, ts, "/api/reroot", `{"path":"`+strings.ReplaceAll(reported, `\`, `\\`)+`"}`)
	if code != http.StatusConflict {
		t.Fatalf("leg1 code = %d, want 409 (body %v)", code, got)
	}
	if got["repairable"] != true || got["translated"] != wt {
		t.Fatalf("leg1 body = %v", got)
	}

	// Leg 2: confirmed — the repair rebinds the records and the swap happens.
	code, info := postAny(t, ts, "/api/reroot", `{"path":"`+strings.ReplaceAll(reported, `\`, `\\`)+`","repair":true}`)
	if code != http.StatusOK {
		t.Fatalf("leg2 code = %d (body %v)", code, info)
	}
	// the records now carry this environment's notation
	b, _ := os.ReadFile(filepath.Join(wt, ".git"))
	if !strings.Contains(string(b), admin) {
		t.Errorf("worktree .git not repaired: %q", b)
	}
	// and the server serves the worktree's branch
	var repo struct {
		Branch string `json:"branch"`
	}
	if code := getJSON(t, ts, "/api/repo", &repo); code != http.StatusOK || repo.Branch != "side" {
		t.Errorf("post-swap repo = %d %+v, want branch side", code, repo)
	}
}

// TestRerootUnreachableIsFriendly: a target reachable under NEITHER notation
// refuses cleanly (409 from preflight), the old root keeps serving.
func TestRerootUnreachableIsFriendly(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	code, _ := postAny(t, ts, "/api/reroot", `{"path":"/definitely/not/here"}`)
	if code != http.StatusConflict {
		t.Fatalf("code = %d, want 409", code)
	}
	var repo struct {
		Branch string `json:"branch"`
	}
	if code := getJSON(t, ts, "/api/repo", &repo); code != http.StatusOK || repo.Branch != "main" {
		t.Errorf("old root must keep serving: %d %+v", code, repo)
	}
}

// The same Windows-created worktree with FORWARD-slash records (what
// Windows gg writes): Linux git lists it glued onto its admin dir —
// <repo>/.git/worktrees/wt/T:/…/wt — and that listed path must reach the
// repairable handshake, not preflight's "not a git repository".
func TestRerootCrossEnvRepairGluedPath(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	base, err := os.MkdirTemp("/mnt/t", "gg-crossenv-*")
	if err != nil {
		t.Skipf("no writable /mnt/t (not a WSL shared disk): %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	main := filepath.Join(base, "repo")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, main, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(main, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, main, "add", "-A")
	gitRun(t, main, "commit", "-m", "c1")
	wt := filepath.Join(base, "wt")
	gitRun(t, main, "worktree", "add", "-b", "side", wt)
	toWin := func(p string) string { return "T:" + strings.TrimPrefix(p, "/mnt/t") }
	admin := filepath.Join(main, ".git", "worktrees", "wt")
	if err := os.WriteFile(filepath.Join(admin, "gitdir"), []byte(toWin(wt)+"/.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+toWin(admin)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := New(domain.Open(main))
	ts := serve(t, srv)
	var list struct {
		Worktrees []struct {
			Path        string `json:"path"`
			Unreachable bool   `json:"unreachable"`
		} `json:"worktrees"`
	}
	getJSON(t, ts, "/api/worktrees", &list)
	reported := ""
	for _, w := range list.Worktrees {
		if strings.Contains(w.Path, "T:") {
			reported = w.Path
			if !w.Unreachable {
				t.Errorf("the glued row is not reachable as listed: %+v", w)
			}
		}
	}
	if !strings.HasPrefix(reported, admin+"/T:") {
		t.Fatalf("git did not glue the record (listed %q) — the scenario is gone", reported)
	}
	code, got := postAny(t, ts, "/api/reroot", `{"path":"`+reported+`"}`)
	if code != http.StatusConflict || got["repairable"] != true || got["translated"] != wt {
		t.Fatalf("code %d body %v, want the repairable handshake to %s", code, got, wt)
	}
}

// A listed worktree whose directory is gone: the TUI's refusal words, not
// preflight's "not a git repository … Run gg web from inside a repository".
func TestRerootUnreachableWorktreeSaysSo(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := newRepoDir(t, 1)
	wt := filepath.Join(t.TempDir(), "gone")
	gitRun(t, dir, "worktree", "add", "-b", "side", wt)
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))
	var list struct {
		Worktrees []struct {
			Path        string `json:"path"`
			Unreachable bool   `json:"unreachable"`
		} `json:"worktrees"`
	}
	getJSON(t, ts, "/api/worktrees", &list)
	seen := false
	for _, w := range list.Worktrees {
		if w.Path == wt {
			seen = true
			if !w.Unreachable {
				t.Errorf("a gone worktree's row says unreachable: %+v", w)
			}
		} else if w.Unreachable {
			t.Errorf("a reachable row is flagged: %+v", w)
		}
	}
	if !seen {
		t.Fatalf("git no longer lists %s: %+v", wt, list)
	}
	code, got := postAny(t, ts, "/api/reroot", `{"path":"`+wt+`"}`)
	if code != http.StatusConflict || got["error"] != "cannot switch: "+wt+" is not reachable from here" {
		t.Errorf("code %d body %v", code, got)
	}
}
