package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// agentEnv pins XDG_STATE_HOME (the CLI reads the registry from
// domain.SessionRegistryDir() = <state>/gg/sessions) and a running session.
func agentEnv(t *testing.T, dir string) {
	t.Helper()
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	reg := filepath.Join(state, "gg", "sessions")
	// Written by hand: the frontends may not import sessionreg (archtest).
	body, _ := json.Marshal(map[string]any{"pid": 1, "worktree": "", "sessions": []map[string]string{
		{"id": "p/s1", "dir": dir, "agent": "claude", "state": "running"},
		{"id": "p/s2", "dir": dir, "agent": "codex", "state": "running"}}})
	if err := os.MkdirAll(reg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reg, "p.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GG_SESSION_ID", "p/s1")
}

func TestWorktreeListJSONAndFree(t *testing.T) {
	dir := newCLIRepo(t)
	agentEnv(t, dir)
	wt := cliWorktree(t, dir, "a", "wt-a")
	code, out, errb := runCLI(t, dir, "worktree", "list", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	var infos []map[string]any
	if err := json.Unmarshal([]byte(out), &infos); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if len(infos) != 2 || infos[0]["main"] != true {
		t.Fatalf("infos = %v", infos)
	}
	for _, k := range []string{"path", "branch", "head", "dirty", "paused_op", "git_lock", "tui", "sessions", "reserved", "claim", "free", "blocked_by", "recycle"} {
		if _, ok := infos[1][k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
	code, out, _ = runCLI(t, dir, "worktree", "list", "--free")
	if code != 0 || !strings.Contains(out, filepath.Base(wt)) || strings.Contains(out, "main\t") {
		t.Fatalf("--free text = %q", out)
	}
}

func TestWorktreeClaimReleaseRoundTrip(t *testing.T) {
	dir := newCLIRepo(t)
	agentEnv(t, dir)
	wt := cliWorktree(t, dir, "a", "wt-a")
	if code, _, errb := runCLI(t, dir, "worktree", "claim", "--note", "https://x/1", wt); code != 0 {
		t.Fatalf("claim exit %d: %s", code, errb)
	}
	if code, _, errb := runCLI(t, dir, "worktree", "claim", wt); code != 0 {
		t.Fatalf("a re-claim by the holder = %d %q, want success (safe to retry)", code, errb)
	}
	t.Setenv("GG_SESSION_ID", "p/s2")
	code, _, errb := runCLI(t, dir, "worktree", "claim", wt)
	if code != 1 || !strings.Contains(errb, "claimed") {
		t.Fatalf("a second session's claim = %d %q", code, errb)
	}
	t.Setenv("GG_SESSION_ID", "p/s1")
	if code, out, _ := runCLI(t, dir, "worktree", "release", wt); code != 0 || !strings.Contains(out, "released") {
		t.Fatalf("release = %d %q", code, out)
	}
}

func TestWorktreeClaimWithoutSession(t *testing.T) {
	dir := newCLIRepo(t)
	wt := cliWorktree(t, dir, "a", "wt-a")
	t.Setenv("GG_SESSION_ID", "")
	code, _, errb := runCLI(t, dir, "worktree", "claim", wt)
	if code != 2 || !strings.Contains(errb, "only an agent running inside gg can claim") {
		t.Fatalf("claim = %d %q", code, errb)
	}
}

func TestWorktreeClaimDeadSession(t *testing.T) {
	dir := newCLIRepo(t)
	agentEnv(t, dir)
	wt := cliWorktree(t, dir, "a", "wt-a")
	t.Setenv("GG_SESSION_ID", "0-1/s9")
	code, _, errb := runCLI(t, dir, "worktree", "claim", wt)
	if code != 2 || !strings.Contains(errb, "is not running in any gg") {
		t.Fatalf("claim by a dead session = %d %q", code, errb)
	}
}

func TestWorktreeClaimRelativePath(t *testing.T) {
	dir := newCLIRepo(t)
	agentEnv(t, dir)
	wt := cliWorktree(t, dir, "a", "wt-a")
	rel, _ := filepath.Rel(dir, wt)
	if code, _, errb := runCLI(t, dir, "worktree", "claim", rel+string(filepath.Separator)); code != 0 {
		t.Fatalf("claim rel exit %d: %s", code, errb)
	}
	if code, _, _ := runCLI(t, dir, "worktree", "claim", filepath.Join(dir, "not-a-wt")); code != 1 {
		t.Fatalf("unknown path exit = %d, want 1", code)
	}
}

func TestWorktreeReleaseNoClaim(t *testing.T) {
	dir := newCLIRepo(t)
	agentEnv(t, dir)
	wt := cliWorktree(t, dir, "a", "wt-a")
	code, out, _ := runCLI(t, dir, "worktree", "release", wt)
	if code != 0 || !strings.Contains(out, "no claim") {
		t.Fatalf("release = %d %q", code, out)
	}
}

func TestWorktreeReleaseNonHolder(t *testing.T) {
	dir := newCLIRepo(t)
	agentEnv(t, dir)
	wt := cliWorktree(t, dir, "a", "wt-a")
	runCLI(t, dir, "worktree", "claim", wt)
	t.Setenv("GG_SESSION_ID", "p/other")
	if code, _, _ := runCLI(t, dir, "worktree", "release", wt); code != 1 {
		t.Fatalf("non-holder release exit = %d", code)
	}
	if code, _, _ := runCLI(t, dir, "worktree", "release", "--force", wt); code != 0 {
		t.Fatalf("force release exit = %d", code)
	}
}

func TestWorktreeReserveUnreserve(t *testing.T) {
	dir := newCLIRepo(t)
	agentEnv(t, dir)
	wt := cliWorktree(t, dir, "a", "wt-a")
	if code, _, errb := runCLI(t, dir, "worktree", "reserve", wt); code != 0 {
		t.Fatalf("reserve exit %d: %s", code, errb)
	}
	code, _, errb := runCLI(t, dir, "worktree", "claim", wt)
	if code != 1 || !strings.Contains(errb, "reserved") {
		t.Fatalf("claim reserved = %d %q", code, errb)
	}
	if code, _, _ := runCLI(t, dir, "worktree", "unreserve", wt); code != 0 {
		t.Fatalf("unreserve exit %d", code)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".gg.toml"))
	if strings.Contains(string(raw), "wt-a") {
		t.Fatalf(".gg.toml still lists it:\n%s", raw)
	}
}

func TestWorktreeListBadStaleAfter(t *testing.T) {
	dir := newCLIRepo(t)
	os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte("[agents]\nstale_after = \"soon\"\n"), 0o644)
	code, _, errb := runCLI(t, dir, "worktree", "list", "--json")
	if code != 0 || !strings.Contains(errb, "warning: [agents] stale_after") {
		t.Fatalf("bad stale_after = %d %q", code, errb)
	}
}

func TestWorktreeClaimNudgesItsOwnTUI(t *testing.T) {
	dir := newCLIRepo(t)
	agentEnv(t, dir)
	inbox := t.TempDir()
	t.Setenv("GG_INBOX", inbox)
	wt := cliWorktree(t, dir, "a", "wt-a")
	if code, _, errb := runCLI(t, dir, "worktree", "claim", wt); code != 0 {
		t.Fatalf("claim: %s", errb)
	}
	ents, _ := os.ReadDir(inbox)
	found := false
	for _, e := range ents {
		if b, err := os.ReadFile(filepath.Join(inbox, e.Name())); err == nil && strings.Contains(string(b), `"worktrees"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no reload worktrees command in GG_INBOX (%d files)", len(ents))
	}
}
