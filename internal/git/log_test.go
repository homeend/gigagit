package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

func TestParseLog(t *testing.T) {
	// Fields separated by \x1f (unit separator), one commit per line. Trailing
	// field is the %D decoration (empty here).
	line1 := "aaa111" + "\x1f" + "" + "\x1f" + "Alice" + "\x1f" + "1700000000" + "\x1f" + "initial" + "\x1f" + ""
	line2 := "bbb222" + "\x1f" + "aaa111" + "\x1f" + "Bob" + "\x1f" + "1700000100" + "\x1f" + "second commit" + "\x1f" + ""
	raw := []byte(line1 + "\n" + line2 + "\n")

	got, err := ParseLog(raw)
	if err != nil {
		t.Fatalf("parse log: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("commits = %d, want 2", len(got))
	}
	if got[0].Hash != "aaa111" || got[0].Author != "Alice" || got[0].Subject != "initial" {
		t.Fatalf("commit0 = %+v", got[0])
	}
	if got[0].UnixTime != 1700000000 {
		t.Fatalf("commit0 time = %d, want 1700000000", got[0].UnixTime)
	}
	if len(got[1].Parents) != 1 || got[1].Parents[0] != "aaa111" {
		t.Fatalf("commit1 parents = %v, want [aaa111]", got[1].Parents)
	}
}

func TestParseLogDecorations(t *testing.T) {
	line1 := strings.Join([]string{"h1", "p1", "Ada", "1700000000", "subj one", "HEAD -> main, feature, tag: v1, origin/main"}, "\x1f")
	line2 := strings.Join([]string{"h2", "", "Bo", "1700000001", "subj two", ""}, "\x1f")
	cs, err := ParseLog([]byte(line1 + "\n" + line2 + "\n"))
	if err != nil || len(cs) != 2 {
		t.Fatalf("parse: %v len=%d", err, len(cs))
	}
	if cs[1].Refs != nil {
		t.Fatalf("undecorated commit should have nil Refs, got %+v", cs[1].Refs)
	}
	byName := map[string]model.Ref{}
	for _, r := range cs[0].Refs {
		byName[r.Name] = r
	}
	if byName["main"].Kind != model.RefLocal || !byName["main"].Head {
		t.Fatalf("main should be the head local branch: %+v", byName["main"])
	}
	if byName["feature"].Kind != model.RefLocal || byName["feature"].Head {
		t.Fatalf("feature should be a non-head local branch: %+v", byName["feature"])
	}
	if byName["v1"].Kind != model.RefTag || byName["origin/main"].Kind != model.RefRemote {
		t.Fatalf("tag/remote kinds wrong: %+v", cs[0].Refs)
	}
}

func logArgvContains(t *testing.T, f *gitexec.FakeRunner, want string) bool {
	t.Helper()
	for _, c := range f.Calls {
		if c.Name == "git log" && strings.Contains(" "+strings.Join(c.Argv, " ")+" ", " "+want+" ") {
			return true
		}
	}
	return false
}

func TestLogScopedArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: ""})
	r := &Repo{Runner: f}
	if _, err := r.LogScoped(context.Background(), 50, 0, LogScope{}); err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"--date-order", "--decorate", "--branches", "HEAD"} {
		if !logArgvContains(t, f, w) {
			t.Fatalf("all-scope argv missing %q: %v", w, f.Calls)
		}
	}
	f.Calls = nil
	if _, err := r.LogScoped(context.Background(), 50, 20, LogScope{Branches: []string{"feat"}}); err != nil {
		t.Fatal(err)
	}
	if !logArgvContains(t, f, "feat") || !logArgvContains(t, f, "--skip=20") || logArgvContains(t, f, "--branches") {
		t.Fatalf("solo-scope argv wrong: %v", f.Calls)
	}
}

func TestLogScopedRealDecorations(t *testing.T) {
	dir, runner := newTestRepo(t) // one commit on main
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "branch", "feature")
	gitIn(t, dir, "tag", "v1")
	cs, err := repo.LogScoped(context.Background(), 10, 0, LogScope{})
	if err != nil || len(cs) == 0 {
		t.Fatalf("LogScoped: %v len=%d", err, len(cs))
	}
	byName := map[string]model.Ref{}
	for _, r := range cs[0].Refs {
		byName[r.Name] = r
	}
	if !byName["main"].Head || byName["main"].Kind != model.RefLocal {
		t.Fatalf("expected main as head local branch, got refs %+v", cs[0].Refs)
	}
	if byName["feature"].Kind != model.RefLocal || byName["v1"].Kind != model.RefTag {
		t.Fatalf("expected feature(local)+v1(tag), got %+v", cs[0].Refs)
	}
}

func TestRepoLogReturnsCommits(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "commit", "--allow-empty", "-m", "second")

	commits, err := repo.LogScoped(context.Background(), 10, 0, LogScope{})
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("commits = %d, want 2", len(commits))
	}
	if commits[0].Subject != "second" {
		t.Fatalf("commit0 subject = %q, want second", commits[0].Subject)
	}
}

func TestCommitTimesBatchesOneInvocation(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log (commit times)", gitexec.Result{Stdout: "aaa\x001000\nbbb\x002000\n"})
	repo := &Repo{Runner: f}
	got, err := repo.CommitTimes(context.Background(), []string{"aaa", "bbb"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 1 {
		t.Fatalf("git calls = %d, want exactly 1 (batched)", len(f.Calls))
	}
	argv := strings.Join(f.Calls[0].Argv, " ")
	if !strings.Contains(argv, "--no-walk") || !strings.Contains(argv, "aaa") || !strings.Contains(argv, "bbb") {
		t.Fatalf("argv = %q", argv)
	}
	if got["aaa"] != 1000 || got["bbb"] != 2000 {
		t.Fatalf("times = %v", got)
	}
}

func TestCommitTimesEmptyInputMakesNoGitCall(t *testing.T) {
	f := gitexec.NewFakeRunner()
	repo := &Repo{Runner: f}
	got, err := repo.CommitTimes(context.Background(), nil)
	if err != nil || len(got) != 0 || len(f.Calls) != 0 {
		t.Fatalf("got=%v err=%v calls=%d", got, err, len(f.Calls))
	}
}

func TestCommitTimesRealRepo(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.TrimSpace(string(out))
	got, err := repo.CommitTimes(context.Background(), []string{sha})
	if err != nil {
		t.Fatal(err)
	}
	if got[sha] == 0 {
		t.Fatalf("no time for %s: %v", sha, got)
	}
}

func TestParseNameStatus(t *testing.T) {
	raw := []byte("M\tCHANGELOG.md\n" +
		"A\tinternal/tui/files_view.go\n" +
		"D\told.txt\n" +
		"R100\ta/old.go\tb/new.go\n" +
		"T\tlink\n" +
		"\n" +
		"bogus-line-without-tab\n")
	got := ParseNameStatus(raw)
	want := []model.CommitFile{
		{Status: "M", Path: "CHANGELOG.md"},
		{Status: "A", Path: "internal/tui/files_view.go"},
		{Status: "D", Path: "old.txt"},
		{Status: "R", Path: "b/new.go", OldPath: "a/old.go"},
		{Status: "T", Path: "link"},
	}
	if len(got) != len(want) {
		t.Fatalf("files = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("file[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestCommitFilesArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log (commit files)", gitexec.Result{Stdout: "M\tfile.txt\n"})
	repo := &Repo{Runner: f}
	got, err := repo.CommitFiles(context.Background(), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 1 {
		t.Fatalf("git calls = %d, want 1", len(f.Calls))
	}
	argv := strings.Join(f.Calls[0].Argv, " ")
	for _, part := range []string{"log", "-1", "-m", "--first-parent", "--root", "--name-status", "-M", "--format=", "abc123"} {
		if !strings.Contains(argv, part) {
			t.Fatalf("argv = %q, missing %q", argv, part)
		}
	}
	if len(got) != 1 || got[0] != (model.CommitFile{Status: "M", Path: "file.txt"}) {
		t.Fatalf("files = %+v", got)
	}
}

func TestLogScopedSkipArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: ""})
	repo := &Repo{Runner: f}

	if _, err := repo.LogScoped(context.Background(), 200, 50, LogScope{}); err != nil {
		t.Fatalf("log: %v", err)
	}
	var argv []string
	for _, c := range f.Calls {
		if c.Name == "git log" {
			argv = c.Argv
		}
	}
	if !strings.Contains(strings.Join(argv, " "), "--skip=50") {
		t.Fatalf("skip>0 should add --skip=50, got: %v", argv)
	}

	f.Calls = nil
	if _, err := repo.LogScoped(context.Background(), 50, 0, LogScope{}); err != nil {
		t.Fatalf("log: %v", err)
	}
	for _, c := range f.Calls {
		if c.Name == "git log" && strings.Contains(strings.Join(c.Argv, " "), "--skip") {
			t.Fatalf("skip==0 must omit --skip, got: %v", c.Argv)
		}
	}
}

func TestCommitFilesRealRepo(t *testing.T) {
	dir, runner := newTestRepo(t) // initial commit contains README.md
	repo := &Repo{Runner: runner}

	// Root commit lists its files (--root).
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	root := strings.TrimSpace(string(out))
	files, err := repo.CommitFiles(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Status != "A" || files[0].Path != "README.md" {
		t.Fatalf("root commit files = %+v, want [A README.md]", files)
	}

	// A rename commit reports R with both paths.
	gitIn(t, dir, "mv", "README.md", "DOCS.md")
	gitIn(t, dir, "commit", "-m", "rename")
	out, err = exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	head := strings.TrimSpace(string(out))
	files, err = repo.CommitFiles(context.Background(), head)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Status != "R" || files[0].OldPath != "README.md" || files[0].Path != "DOCS.md" {
		t.Fatalf("rename commit files = %+v, want [R README.md -> DOCS.md]", files)
	}
}

// TestCommitFilesStashNoDuplicate guards the stash file tree: a stash commit is
// a merge (HEAD + index parents), and a file changed against both parents must
// be listed once, not once per parent.
func TestCommitFilesStashNoDuplicate(t *testing.T) {
	dir, runner := newTestRepo(t) // initial commit contains README.md
	repo := &Repo{Runner: runner}

	// Modify the tracked file and stash it — the stash commit's tree differs
	// from both its HEAD and index parents.
	gitIn(t, dir, "config", "user.email", "t@t")
	gitIn(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "stash", "push", "-m", "wip")

	out, err := exec.Command("git", "-C", dir, "rev-parse", "stash@{0}").Output()
	if err != nil {
		t.Fatal(err)
	}
	stashSHA := strings.TrimSpace(string(out))

	files, err := repo.CommitFiles(context.Background(), stashSHA)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "README.md" {
		t.Fatalf("stash files = %+v, want a single [M README.md]", files)
	}
}

// TestCommitFilesMergeFirstParent pins first-parent semantics for a true 3-way
// merge: each side adds a different file, and the merge lists only what it
// brought to the mainline (the first-parent diff), not the union of both parents.
func TestCommitFilesMergeFirstParent(t *testing.T) {
	dir, runner := newTestRepo(t) // initial commit on main (README.md)
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "config", "user.email", "t@t")
	gitIn(t, dir, "config", "user.name", "t")

	gitIn(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-m", "feature adds b")

	gitIn(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-m", "main adds a")
	gitIn(t, dir, "merge", "--no-ff", "-m", "merge", "feature") // a + b in tree, no conflict

	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	merge := strings.TrimSpace(string(out))
	files, err := repo.CommitFiles(context.Background(), merge)
	if err != nil {
		t.Fatal(err)
	}
	// First parent is main; the merge brought feature's b.txt to it. a.txt was
	// already on the first-parent side, so it is not part of this merge's diff.
	if len(files) != 1 || files[0].Status != "A" || files[0].Path != "b.txt" {
		t.Fatalf("merge files = %+v, want a single [A b.txt] (first-parent only)", files)
	}
}
