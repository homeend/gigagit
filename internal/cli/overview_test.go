package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func overviewCLIRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "config", "user.name", "t")
	gitRun(t, dir, "config", "user.email", "t@t")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("1\n2\n3\n4\n5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "a")
	return dir
}

func jsonField(t *testing.T, out, key string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("json %q: %v", out, err)
	}
	s, _ := m[key].(string)
	return s
}

const ovCLIDoc = `{"version":1,"title":"How a works","overview":"Start at [three](a.go:3).","files":[{"path":"a.go","annotations":[{"newRange":[3,3],"summary":"the gate"}]}]}`

func TestOverviewSaveListShowRm(t *testing.T) {
	t.Parallel()
	dir := overviewCLIRepo(t)
	code, out, errb := runCLIStdin(t, dir, ovCLIDoc, "overview", "save", "--agent", "Claude Code", "--stdin")
	if code != 0 || !strings.Contains(out, "?overview=") {
		t.Fatalf("save = %d out=%q err=%q", code, out, errb)
	}
	link := strings.TrimSpace(strings.Split(strings.TrimSpace(out), "\n")[len(strings.Split(strings.TrimSpace(out), "\n"))-1])
	code, out, _ = runCLI(t, dir, "overview", "list")
	if code != 0 || !strings.Contains(out, "How a works") || !strings.Contains(out, "on-branch") || !strings.Contains(out, "Claude Code") {
		t.Fatalf("list = %d %q", code, out)
	}
	id := strings.Fields(out)[0]
	code, out, _ = runCLI(t, dir, "overview", "show", link)
	if code != 0 || !strings.Contains(out, "How a works") || !strings.Contains(out, "Start at [three](a.go:3).") || !strings.Contains(out, "a.go:3  the gate") {
		t.Fatalf("show = %d %q", code, out)
	}
	code, out, _ = runCLI(t, dir, "overview", "show", id, "--json")
	if code != 0 || jsonField(t, out, "title") != "How a works" || jsonField(t, out, "kind") != "on-branch" {
		t.Fatalf("show --json = %d %q", code, out)
	}
	if code, _, errb = runCLI(t, dir, "overview", "rm", id); code != 0 {
		t.Fatalf("rm = %d %q", code, errb)
	}
	if code, out, _ = runCLI(t, dir, "overview", "list"); code != 0 || strings.Contains(out, id) {
		t.Fatalf("list after rm = %d %q", code, out)
	}
}

func TestOverviewSaveRefusal(t *testing.T) {
	t.Parallel()
	dir := overviewCLIRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("1\n2\nX\n4\n5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errb := runCLIStdin(t, dir, ovCLIDoc, "overview", "save", "--agent", "c", "--stdin")
	if code != 1 || out != "" || !strings.Contains(errb, "a.go: uncommitted changes") {
		t.Fatalf("save on a dirty file = %d out=%q err=%q", code, out, errb)
	}
	if _, out, _ = runCLI(t, dir, "overview", "list", "--all"); strings.TrimSpace(out) != "" {
		t.Fatalf("a refused save stored: %q", out)
	}
}

func TestOverviewSaveDryRunAndTitleFlag(t *testing.T) {
	t.Parallel()
	dir := overviewCLIRepo(t)
	doc := `{"version":1,"overview":"[a](a.go:1)"}`
	code, out, errb := runCLIStdin(t, dir, doc, "overview", "save", "--agent", "c", "--stdin", "--title", "From flag", "--dry-run")
	if code != 0 || !strings.Contains(out, "From flag") {
		t.Fatalf("dry run = %d %q %q", code, out, errb)
	}
	if _, out, _ = runCLI(t, dir, "overview", "list", "--all"); strings.TrimSpace(out) != "" {
		t.Fatalf("dry run stored: %q", out)
	}
	code, out, errb = runCLIStdin(t, dir, doc, "overview", "save", "--agent", "c", "--stdin", "--title", "From flag", "--json")
	if code != 0 || jsonField(t, out, "title") != "From flag" {
		t.Fatalf("save --title = %d %q %q", code, out, errb)
	}
}

func TestOverviewRmViaNoteRm(t *testing.T) {
	t.Parallel()
	dir := overviewCLIRepo(t)
	_, out, _ := runCLIStdin(t, dir, ovCLIDoc, "overview", "save", "--agent", "c", "--stdin", "--json")
	id := jsonField(t, out, "id")
	if code, _, errb := runCLI(t, dir, "note", "rm", id); code != 0 {
		t.Fatalf("note rm = %d %q", code, errb)
	}
	if _, out, _ = runCLI(t, dir, "overview", "list"); strings.Contains(out, id) {
		t.Fatalf("still listed after note rm: %q", out)
	}
}

func TestOverviewSaveRefusesAWorkingLink(t *testing.T) {
	t.Parallel()
	dir := overviewCLIRepo(t)
	code, _, errb := runCLIStdin(t, dir, ovCLIDoc, "overview", "save", "gg://"+dir+"/a.go", "--agent", "c", "--stdin")
	if code != 1 || !strings.Contains(errb, "a permanent overview needs a commit or a pull request") {
		t.Fatalf("working link = %d %q", code, errb)
	}
}

func TestOverviewListRowNamesItsPR(t *testing.T) {
	t.Parallel()
	row := overviewListRow(domain.Overview{ID: "o1", Commit: "abc", Scope: "main...refs/gg/pr/7"})
	if row["pr"] != 7 || row["scope"] != "main...refs/gg/pr/7" {
		t.Fatalf("row = %v", row)
	}
	row = overviewListRow(domain.Overview{ID: "o2", Commit: "abc"})
	if row["pr"] != 0 || row["scope"] != "" {
		t.Fatalf("commit row = %v", row)
	}
}

func TestOverviewShowJSONOfABrokenDocumentExits1(t *testing.T) {
	t.Parallel()
	var out, errb strings.Builder
	code := overviewShowJSON(&out, &errb, domain.Overview{ID: "o1", Commit: "abc", Title: "T"}, "gg://x")
	if code != 1 || jsonField(t, out.String(), "warn") == "" || !strings.Contains(errb.String(), "no longer parses") {
		t.Fatalf("code = %d out=%q err=%q", code, out.String(), errb.String())
	}
}

// `gg overview store` prints the store gg resolved for this repository:
// the oracle of spec 2026-10-15-gg-notes-store-format §9.3 — the directory,
// its key and BOTH files overview records live in.
func TestOverviewStorePrintsDirKeyAndFiles(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	svc := openCLIService(t, dir)
	want := svc.NotesStorePath(context.Background())
	if want.Dir == "" || !isHex16(want.Key) {
		t.Fatalf("NotesStorePath = %+v", want)
	}
	commits, previews := filepath.Join(want.Dir, "commits.toml"), filepath.Join(want.Dir, "previews.toml")
	code, out, errs := runCLI(t, dir, "overview", "store")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if out != "dir "+want.Dir+"\nkey "+want.Key+"\nfile "+commits+"\nfile "+previews+"\n" {
		t.Fatalf("text form:\n%s", out)
	}
	code, out, _ = runCLI(t, dir, "overview", "store", "--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var row struct {
		Dir, Key string
		Files    []string
	}
	if err := json.Unmarshal([]byte(out), &row); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if row.Dir != want.Dir || row.Key != want.Key || len(row.Files) != 2 || row.Files[0] != commits || row.Files[1] != previews {
		t.Fatalf("json = %+v, want dir %s key %s files [%s %s]", row, want.Dir, want.Key, commits, previews)
	}
}

func isHex16(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

// Review focus 5: the oracle works in a repository with no commit, where
// `overview list` cannot (HEAD has no commit) — and prints a real key.
func TestOverviewStoreOnEmptyRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "-b", "main", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if code, _, _ := runCLI(t, dir, "overview", "list"); code != 1 {
		t.Fatalf("overview list on an empty repo: exit %d, want 1", code)
	}
	code, out, errs := runCLI(t, dir, "overview", "store", "--json")
	if code != 0 {
		t.Fatalf("exit %d out %s err %s", code, out, errs)
	}
	var row struct {
		Key   string
		Files []string
	}
	if err := json.Unmarshal([]byte(out), &row); err != nil || !isHex16(row.Key) || len(row.Files) != 2 {
		t.Fatalf("json = %s (%v): want a 16-hex key and two files", out, err)
	}
}

// A link in backticks refuses the save with the fix; an overview with no
// anchor at all is stored, with a warning (it opens no code).
func TestOverviewSaveBacktickedAndAnchorless(t *testing.T) {
	t.Parallel()
	dir := overviewCLIRepo(t)
	doc := `{"version":1,"title":"T","overview":"Start at ` + "`[three](a.go:3)`" + `.","files":[]}`
	code, out, errb := runCLIStdin(t, dir, doc, "overview", "save", "--agent", "c", "--stdin")
	if code != 1 || out != "" || !strings.Contains(errb, "a link inside backticks is plain text, not an anchor — write [three](a.go:3)") {
		t.Fatalf("backticked save = %d out=%q err=%q", code, out, errb)
	}
	doc = `{"version":1,"title":"T","overview":"Only words.","files":[]}`
	code, out, errb = runCLIStdin(t, dir, doc, "overview", "save", "--agent", "c", "--stdin")
	if code != 0 || !strings.Contains(out, "?overview=") || !strings.Contains(errb, "warning: the overview has no anchor") {
		t.Fatalf("anchorless save = %d out=%q err=%q", code, out, errb)
	}
	if code, _, errb = runCLIStdin(t, dir, ovCLIDoc, "overview", "save", "--agent", "c", "--stdin"); code != 0 || strings.Contains(errb, "no anchor") {
		t.Fatalf("anchored save = %d err=%q", code, errb)
	}
}
