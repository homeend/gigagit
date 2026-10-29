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

// claimFixture: a repo with a linked worktree wt2 claimed by "bob" from a
// foreign host (never judged dead, so no live gg is needed) and reserved in
// the repo config.
func claimFixture(t *testing.T) (main, second string) {
	t.Helper()
	main = newRepoDir(t, 1)
	second = filepath.Join(t.TempDir(), "wt2")
	gitRun(t, main, "worktree", "add", "-b", "w2", second)
	claim := "session = \"9-1\"\nagent = \"bob\"\nsince = 2026-10-24T13:05:00Z\nnote = \"fixing the parser\"\nhost = \"elsewhere/plan9\"\n"
	if err := os.WriteFile(filepath.Join(main, ".git", "worktrees", "wt2", "gg-claim"), []byte(claim), 0o644); err != nil {
		t.Fatal(err)
	}
	return main, second
}

func TestWorktreesCarryMarks(t *testing.T) {
	t.Parallel()
	main, second := claimFixture(t)
	if err := os.WriteFile(filepath.Join(main, ".gg.toml"), []byte("[agents]\nreserved = ["+tomlQuote(second)+"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(main)))
	var body struct {
		Worktrees []struct {
			Path     string `json:"path"`
			Reserved bool   `json:"reserved"`
			Claim    *struct {
				Agent string `json:"agent"`
				Since string `json:"since"`
				Note  string `json:"note"`
			} `json:"claim"`
		} `json:"worktrees"`
	}
	if code := getJSON(t, ts, "/api/worktrees", &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(body.Worktrees) != 2 {
		t.Fatalf("rows = %+v", body.Worktrees)
	}
	for _, w := range body.Worktrees {
		if domain.SameCheckout(w.Path, main) {
			if w.Reserved || w.Claim != nil {
				t.Errorf("main row marked: %+v", w)
			}
			continue
		}
		if !w.Reserved {
			t.Errorf("wt2 not reserved: %+v", w)
		}
		if w.Claim == nil || w.Claim.Agent != "bob" || w.Claim.Since != "2026-10-24T13:05:00Z" || w.Claim.Note != "fixing the parser" {
			t.Errorf("wt2 claim = %+v", w.Claim)
		}
	}
}

func tomlQuote(s string) string { return "'" + s + "'" }

// The Worktrees row draws the TUI's mark column before the label and the
// TUI bar's text in its tooltip, under the path.
func TestWorktreeRowDrawsMarks(t *testing.T) {
	t.Parallel()
	src := readStaticSrc(t, "sidebar.js")
	for _, want := range []string{
		`import { claimHint, markPrefix } from "./wtmarks.js";`,
		`const marks = markPrefix(w);`,
		"${mark(!!cur)}${marks ? `<span class=\"wmark\">${esc(marks)}</span>` : \"\"}${esc(label)}",
		`const tip = w.path + (claimHint(w) ? "\n" + claimHint(w) : "");`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("sidebar.js lacks %q", want)
		}
	}
}

func wt2Row(t *testing.T, ts *httptest.Server, main string) (reserved, claimed bool) {
	t.Helper()
	var body struct {
		Worktrees []struct {
			Path     string    `json:"path"`
			Reserved bool      `json:"reserved"`
			Claim    *struct{} `json:"claim"`
		} `json:"worktrees"`
	}
	if code := getJSON(t, ts, "/api/worktrees", &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	for _, w := range body.Worktrees {
		if !domain.SameCheckout(w.Path, main) {
			return w.Reserved, w.Claim != nil
		}
	}
	t.Fatal("no wt2 row")
	return false, false
}

// Reserve / Unreserve write [agents] reserved through the domain (the TUI's
// setReserved); a path that is not one of the repo's worktrees is refused.
func TestWorktreeReserveEndpoint(t *testing.T) {
	t.Parallel()
	main, second := claimFixture(t)
	ts := serve(t, New(domain.Open(main)))
	if r, _ := wt2Row(t, ts, main); r {
		t.Fatal("reserved before the request")
	}
	if code, out := postJSONRaw(t, ts, "/api/worktree/reserve", `{"path":`+jsonQ(second)+`,"on":true}`); code != http.StatusOK {
		t.Fatalf("reserve: %d %v", code, out)
	}
	if r, _ := wt2Row(t, ts, main); !r {
		t.Fatal("not reserved after Reserve")
	}
	if code, out := postJSONRaw(t, ts, "/api/worktree/reserve", `{"path":`+jsonQ(second)+`,"on":false}`); code != http.StatusOK {
		t.Fatalf("unreserve: %d %v", code, out)
	}
	if r, _ := wt2Row(t, ts, main); r {
		t.Fatal("still reserved after Unreserve")
	}
	if code, _ := postJSONRaw(t, ts, "/api/worktree/reserve", `{"path":"/nowhere/else","on":true}`); code != http.StatusBadRequest {
		t.Fatalf("unknown path: %d, want 400", code)
	}
	if _, err := os.Stat(filepath.Join(main, ".gg.toml")); err == nil {
		if b, _ := os.ReadFile(filepath.Join(main, ".gg.toml")); strings.Contains(string(b), "nowhere") {
			t.Fatalf("unknown path written: %s", b)
		}
	}
}

// Release claim takes the worktree back from the agent (force: the user's
// release, as the TUI's confirmReleaseClaim).
func TestWorktreeReleaseClaimEndpoint(t *testing.T) {
	t.Parallel()
	main, second := claimFixture(t)
	ts := serve(t, New(domain.Open(main)))
	if _, c := wt2Row(t, ts, main); !c {
		t.Fatal("not claimed before the request")
	}
	if code, out := postJSONRaw(t, ts, "/api/worktree/release-claim", `{"path":`+jsonQ(second)+`}`); code != http.StatusOK {
		t.Fatalf("release: %d %v", code, out)
	}
	if _, c := wt2Row(t, ts, main); c {
		t.Fatal("still claimed after Release")
	}
	if _, err := os.Stat(filepath.Join(main, ".git", "worktrees", "wt2", "gg-claim")); !os.IsNotExist(err) {
		t.Fatalf("claim file still there: %v", err)
	}
	if code, _ := postJSONRaw(t, ts, "/api/worktree/release-claim", `{"path":"/nowhere/else"}`); code != http.StatusBadRequest {
		t.Fatalf("unknown path: %d, want 400", code)
	}
}

// Both writes tell every tab to redraw its Worktrees rows.
func TestWorktreeMarkWritesFanOut(t *testing.T) {
	t.Parallel()
	src := readStaticSrcGo(t, "wtmarks.go")
	if n := strings.Count(src, `s.worktreesChanged()`); n < 2 {
		t.Fatalf("wtmarks.go: %d worktreesChanged calls, want one per write", n)
	}
}

func jsonQ(s string) string { b, _ := json.Marshal(s); return string(b) }

func readStaticSrcGo(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The worktree menu carries the TUI's . menu rows in its order: Start agent,
// Open terminal, Release claim, Reserve / Unreserve — one registration, so
// no other module's rows land between them. Release asks first (esc answers
// Cancel), and nothing runs while an op does (the TUI's opsIdle gate).
func TestWorktreeMarkRowsWired(t *testing.T) {
	t.Parallel()
	src := readStaticSrc(t, "sessions.js")
	for _, want := range []string{
		`import { markRows } from "./wtmarks.js";`,
		`registerRows("worktree", (w) => (w && w.path ? (w.bare ? [] : startActs(w.path)).concat(markActs(w)) : []));`,
		`showLocalConfirm(r.agent + " is working in " + wtLeaf(w.path) + ". Release its claim?", ["Release", "Cancel"]`,
		`if (opt !== "Release") return;`,
		`postJSON("/api/worktree/release-claim", { path: w.path })`,
		`opLine("released the claim on " + wtLeaf(w.path))`,
		`opLine("release claim: " + (e.message || e), true)`,
		`postJSON("/api/worktree/reserve", { path: w.path, on })`,
		`opLine("reserve: " + (e.message || e), true)`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("sessions.js lacks %q", want)
		}
	}
	if strings.Count(src, `if (opBusy())`) < 2 {
		t.Error("sessions.js: the mark rows must refuse while an op runs")
	}
	ops := readStaticSrc(t, "ops.js")
	if !strings.Contains(ops, `const ESCAPE_WORDS = /^(abort|cancel|no|stop|keep|skip|stay)$/i;`) {
		t.Error("ops.js: esc no longer answers Cancel in the release confirm")
	}
}
