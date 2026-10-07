package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/repos"
)

// fakeAgent puts an executable named bin (e.g. "claude") first on PATH that
// writes its argv, joined by "|", into $GG_MESSAGE_FILE as a review
// document's summary.
func fakeAgent(t *testing.T, bin string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based fake agent")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nargs=$(printf '%s|' \"$@\")\nprintf '{\"version\":1,\"summary\":\"ARGS %s\",\"files\":[]}' \"$args\" > \"$GG_MESSAGE_FILE\"\n"
	if err := os.WriteFile(filepath.Join(dir, bin), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based review tool")
	}
}

// reviewDoc parses a --no-save --json review document.
func reviewDoc(t *testing.T, out string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not JSON: %q (%v)", out, err)
	}
	return doc
}

func TestReviewModelAppendsTheAgentFlag(t *testing.T) {
	isolateReviewEnv(t)
	fakeAgent(t, "claude")
	dir := newRepoDir(t)
	runGit(t, dir, "commit", "--allow-empty", "-m", "second")
	writeReviewTool(t, dir, "Claude", "claude -p x")
	code, out, errb := runCLI(t, dir, "review", "--tool", "Claude", "--model", "son net", "--no-save", "--json", "HEAD")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if s, _ := reviewDoc(t, out)["summary"].(string); !strings.Contains(s, "--model|son net|") {
		t.Fatalf("the model must reach the agent as one argument: %q", s)
	}
}

func TestReviewModelRefusedForACustomTool(t *testing.T) {
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	runGit(t, dir, "commit", "--allow-empty", "-m", "second")
	writeReviewTool(t, dir, "Echo", `printf x`)
	code, _, errb := runCLI(t, dir, "review", "--tool", "Echo", "--model", "m", "HEAD")
	if code != 2 || !strings.Contains(errb, "cannot take a model") {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
}

func TestReviewNoSaveStoresNothing(t *testing.T) {
	skipOnWindows(t)
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	runGit(t, dir, "commit", "--allow-empty", "-m", "second")
	writeReviewTool(t, dir, "Echo", `printf '{"version":1,"summary":"S","files":[]}' > "$GG_MESSAGE_FILE"`)
	code, out, errb := runCLI(t, dir, "review", "--tool", "Echo", "--no-save", "--json", "HEAD")
	if code != 0 || reviewDoc(t, out)["summary"] != "S" || strings.Contains(errb, "note:") {
		t.Fatalf("exit=%d out=%q stderr=%q", code, out, errb)
	}
	if code, _, _ := runCLI(t, dir, "review", "show", "latest"); code == 0 {
		t.Fatal("--no-save stored a review")
	}
	// Without --json it prints the review as usual, still storing nothing.
	code, out, errb = runCLI(t, dir, "review", "--tool", "Echo", "--no-save", "HEAD")
	if code != 0 || !strings.Contains(out, "S") || strings.Contains(errb, "note:") {
		t.Fatalf("exit=%d out=%q stderr=%q", code, out, errb)
	}
}

func TestReviewNoSaveJSONRefusesProse(t *testing.T) {
	skipOnWindows(t)
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	runGit(t, dir, "commit", "--allow-empty", "-m", "second")
	writeReviewTool(t, dir, "Echo", `printf 'prose\n'`)
	code, out, errb := runCLI(t, dir, "review", "--tool", "Echo", "--no-save", "--json", "HEAD")
	if code != 1 || out != "" || !strings.Contains(errb, "not a gg review document") || !strings.Contains(errb, "prose") {
		t.Fatalf("exit=%d out=%q stderr=%q", code, out, errb)
	}
}

func TestReviewFlagConflicts(t *testing.T) {
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	writeReviewTool(t, dir, "Echo", `printf x`)
	for _, args := range [][]string{
		{"--json", "HEAD"},
		{"--link", "gg://x", "--working"},
		{"--link", "gg://x", "HEAD"},
		{"--link", "gg://x", "--preview", "main...main"},
		{"--no-save", "--notes", "HEAD"},
	} {
		if code, _, errb := runCLI(t, dir, append([]string{"review", "--tool", "Echo"}, args...)...); code != 2 {
			t.Errorf("%v: exit=%d stderr=%s", args, code, errb)
		}
	}
}

// --link reviews exactly what `gg review save <link>` would.
func TestReviewLinkMatchesReviewSave(t *testing.T) {
	skipOnWindows(t)
	isolateReviewEnv(t)
	dir, link := reviewSaveRepo(t)
	code, out, errb := runCLI(t, dir, "review", "save", link, "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("dry-run = %d %q", code, errb)
	}
	var dry map[string]string
	if err := json.Unmarshal([]byte(out), &dry); err != nil {
		t.Fatal(err)
	}
	writeReviewTool(t, dir, "Echo", `printf '{"version":1,"summary":"R <range>","files":[]}' > "$GG_MESSAGE_FILE"`)
	code, out, errb = runCLI(t, dir, "review", "--tool", "Echo", "--link", link, "--no-save", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if s := reviewDoc(t, out)["summary"]; s != "R "+dry["range"] {
		t.Fatalf("summary %q, want the dry-run's range %q", s, dry["range"])
	}
}

// A link to another worktree's working changes runs the tool in THAT checkout.
func TestReviewLinkRunsInTheLinksCheckout(t *testing.T) {
	skipOnWindows(t)
	isolateReviewEnv(t)
	dir, _ := reviewSaveRepo(t)
	other := filepath.Join(t.TempDir(), "other")
	gitRun(t, dir, "worktree", "add", "-q", "-b", "side", other, "main")
	if err := os.WriteFile(filepath.Join(other, "f.txt"), []byte("a\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The resolver finds a link's checkout through gg's repository history.
	statePath := filepath.Join(t.TempDir(), "repos.toml")
	prevState := RepoStatePath
	RepoStatePath = statePath
	t.Cleanup(func() { RepoStatePath = prevState })
	for _, p := range []string{dir, other} {
		if err := repos.Touch(statePath, p, "", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	code, link, errb := runCLI(t, other, "link", "f.txt")
	if code != 0 {
		t.Fatalf("link = %d %q", code, errb)
	}
	writeReviewTool(t, dir, "Echo", `printf '{"version":1,"summary":"PWD %s","files":[]}' "$PWD" > "$GG_MESSAGE_FILE"`)
	code, out, errb := runCLI(t, dir, "review", "--tool", "Echo", "--link", strings.TrimSpace(link), "--no-save", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	want, _ := filepath.EvalSymlinks(other)
	if s, _ := reviewDoc(t, out)["summary"].(string); !strings.Contains(s, want) && !strings.Contains(s, other) {
		t.Fatalf("summary %q, want it run in %s", s, other)
	}
}

func TestReviewToolsJSON(t *testing.T) {
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	writeReviewTool(t, dir, "Claude", "claude -p x")
	writeReviewTool(t, dir, "Echo", `printf x`)
	block := "\n[[tools.command]]\ncategory = \"review\"\nname = \"Claude (interactive)\"\nmode = \"interactive\"\ncommand = \"claude\"\n"
	f, err := os.OpenFile(filepath.Join(dir, ".gg.toml"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(block)
	f.Close()
	code, out, errb := runCLI(t, dir, "review", "--tools", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	var tools []struct {
		Name, Agent, Mode string
		Model             bool
	}
	if err := json.Unmarshal([]byte(out), &tools); err != nil {
		t.Fatalf("%q: %v", out, err)
	}
	got := map[string]string{}
	for _, tl := range tools {
		got[tl.Name] = tl.Agent + "/" + tl.Mode + "/" + map[bool]string{true: "model", false: "-"}[tl.Model]
	}
	if got["Claude"] != "claude/capture/model" || got["Echo"] != "/capture/-" || got["Claude (interactive)"] != "claude/interactive/model" {
		t.Fatalf("tools = %v", got)
	}
}

// Two reviewers on one change run side by side, each with its own model.
func TestReviewNoSaveRunsConcurrently(t *testing.T) {
	skipOnWindows(t)
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	runGit(t, dir, "commit", "--allow-empty", "-m", "second")
	writeReviewTool(t, dir, "Slow", `sleep 1; printf '{"version":1,"summary":"M <model>","files":[]}' > "$GG_MESSAGE_FILE"`)
	start := time.Now()
	var wg sync.WaitGroup
	outs := map[string]string{}
	var mu sync.Mutex
	for _, m := range []string{"a", "b"} {
		wg.Add(1)
		go func(m string) {
			defer wg.Done()
			code, out, errb := runCLI(t, dir, "review", "--tool", "Slow", "--model", m, "--no-save", "--json", "HEAD")
			mu.Lock()
			defer mu.Unlock()
			if code != 0 {
				outs[m] = "exit " + errb
				return
			}
			outs[m] = out
		}(m)
	}
	wg.Wait()
	for _, m := range []string{"a", "b"} {
		if !strings.Contains(outs[m], `"M `+m+`"`) {
			t.Fatalf("reviewer %s: %q", m, outs[m])
		}
	}
	if d := time.Since(start); d > 1900*time.Millisecond {
		t.Fatalf("the two reviews took %v — they waited on each other", d)
	}
}
