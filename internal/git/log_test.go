package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
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
	// Full refnames as emitted by --decorate=full. feat/foo is a SLASH-named
	// local branch: it must classify as RefLocal, not RefRemote.
	deco := "HEAD -> refs/heads/main, refs/heads/feat/foo, tag: refs/tags/v1, refs/remotes/origin/main"
	line1 := strings.Join([]string{"h1", "p1", "Ada", "1700000000", "subj one", deco}, "\x1f")
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
	if byName["feat/foo"].Kind != model.RefLocal || byName["feat/foo"].Head {
		t.Fatalf("feat/foo should be a non-head LOCAL branch (slash in name): %+v", byName["feat/foo"])
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
	if _, err := r.LogScoped(context.Background(), 50, 0, LogScope{}, true); err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"--date-order", "--ignore-missing", "--decorate=full", "--source", "--branches", "HEAD"} {
		if !logArgvContains(t, f, w) {
			t.Fatalf("all-scope argv missing %q: %v", w, f.Calls)
		}
	}
	f.Calls = nil
	if _, err := r.LogScoped(context.Background(), 50, 20, LogScope{Branches: []string{"feat"}}, true); err != nil {
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
	gitIn(t, dir, "branch", "feat/slashy") // slash-named local branch (regression)
	gitIn(t, dir, "tag", "v1")
	cs, err := repo.LogScoped(context.Background(), 10, 0, LogScope{}, true)
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
	if byName["feat/slashy"].Kind != model.RefLocal {
		t.Fatalf("slash-named local branch must be RefLocal, got %+v", cs[0].Refs)
	}
}

func TestLogScopedRealSource(t *testing.T) {
	dir, runner := newTestRepo(t) // one commit on main
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "checkout", "-b", "feat")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "feat work")
	cs, err := repo.LogScoped(context.Background(), 10, 0, LogScope{Branches: []string{"feat"}}, true)
	if err != nil || len(cs) == 0 {
		t.Fatalf("LogScoped: %v len=%d", err, len(cs))
	}
	// %S (via --source) stamps each commit with the branch it was reached from.
	if cs[0].Source != "feat" {
		t.Fatalf("commit Source = %q, want feat (from --source/%%S); refs=%+v", cs[0].Source, cs[0].Refs)
	}
}

func TestParseLogSourceOptional(t *testing.T) {
	// A 7-field line carries the source; a legacy 6-field line leaves it empty.
	seven := "h1\x1f\x1fAda\x1f0\x1fsubj\x1fHEAD -> main\x1ffeat\n"
	cs, _ := ParseLog([]byte(seven))
	if len(cs) != 1 || cs[0].Source != "feat" {
		t.Fatalf("7-field Source = %+v", cs)
	}
	six := "h2\x1f\x1fAda\x1f0\x1fsubj\x1fmain\n"
	cs2, _ := ParseLog([]byte(six))
	if len(cs2) != 1 || cs2[0].Source != "" {
		t.Fatalf("6-field Source should be empty, got %+v", cs2)
	}
}

func TestRepoLogReturnsCommits(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "commit", "--allow-empty", "-m", "second")

	commits, err := repo.LogScoped(context.Background(), 10, 0, LogScope{}, true)
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

// ParseNameStatus consumes the NUL-separated (`-z`) form of `--name-status`:
// a status token, then one path token (M/A/D/T) or two path tokens (R/C). `-z`
// also disables git's path quoting, so non-ASCII paths arrive as raw UTF-8.
func TestParseNameStatus(t *testing.T) {
	raw := []byte("M\x00CHANGELOG.md\x00" +
		"A\x00internal/tui/files_view.go\x00" +
		"D\x00old.txt\x00" +
		"R100\x00a/old.go\x00b/new.go\x00" +
		"T\x00link\x00" +
		"M\x00timing — kopia.log\x00" + // raw em-dash, not quoted
		"R077\x00timing4.log\x00timing — kopia.log\x00") // non-ASCII rename target
	got := ParseNameStatus(raw)
	want := []model.CommitFile{
		{Status: "M", Path: "CHANGELOG.md"},
		{Status: "A", Path: "internal/tui/files_view.go"},
		{Status: "D", Path: "old.txt"},
		{Status: "R", Path: "b/new.go", OldPath: "a/old.go"},
		{Status: "T", Path: "link"},
		{Status: "M", Path: "timing — kopia.log"},
		{Status: "R", Path: "timing — kopia.log", OldPath: "timing4.log"},
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
	f.SetResponse("git log (commit files)", gitexec.Result{Stdout: "M\x00file.txt\x00"})
	repo := &Repo{Runner: f}
	got, err := repo.CommitFiles(context.Background(), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 1 {
		t.Fatalf("git calls = %d, want 1", len(f.Calls))
	}
	argv := strings.Join(f.Calls[0].Argv, " ")
	for _, part := range []string{"log", "-1", "-m", "--first-parent", "--root", "--name-status", "-M", "-z", "--format=", "abc123"} {
		if !strings.Contains(argv, part) {
			t.Fatalf("argv = %q, missing %q", argv, part)
		}
	}
	if len(got) != 1 || got[0] != (model.CommitFile{Status: "M", Path: "file.txt"}) {
		t.Fatalf("files = %+v", got)
	}
}

func TestLogScopedAppendsUpstreams(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: ""})
	r := &Repo{Runner: f}
	_, _ = r.LogScoped(context.Background(), 10, 0, LogScope{Upstreams: []string{"origin/main"}}, false)
	if !logArgvContains(t, f, "--branches") || !logArgvContains(t, f, "HEAD") {
		t.Fatalf("argv = %v, want default --branches HEAD", f.Calls)
	}
	if !logArgvContains(t, f, "origin/main") {
		t.Fatalf("argv = %v, want the upstream ref appended", f.Calls)
	}
}

// TestLogScopedRealMissingUpstream reproduces the delete-remote-branch race:
// the feed's applied scope still lists an upstream whose remote-tracking ref
// was just deleted (git push --delete drops refs/remotes/<r>/<b> immediately,
// before the TUI's remote-branches list refreshes and the scope re-walks).
// The walk must skip the gone ref, not fail with exit 128 "unknown revision".
func TestLogScopedRealMissingUpstream(t *testing.T) {
	_, runner := newTestRepo(t) // one commit on main
	repo := &Repo{Runner: runner}
	cs, err := repo.LogScoped(context.Background(), 10, 0,
		LogScope{Upstreams: []string{"origin/deleted-branch"}}, true)
	if err != nil {
		t.Fatalf("LogScoped with a missing upstream must not fail: %v", err)
	}
	if len(cs) == 0 {
		t.Fatal("expected the repo's commits despite the missing upstream")
	}
}

func TestLogScopedSkipArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: ""})
	repo := &Repo{Runner: f}

	if _, err := repo.LogScoped(context.Background(), 200, 50, LogScope{}, true); err != nil {
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
	if _, err := repo.LogScoped(context.Background(), 50, 0, LogScope{}, true); err != nil {
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

// TestCommitFilesNonASCIIPathRoundTrip reproduces the diff-view failure: a
// committed file whose name has a non-ASCII byte (em-dash U+2014) was listed
// with git's quoted, octal-escaped path ("timing \342\200\224 kopia.log"), so
// the follow-up `git show <rev>:<path>` failed with exit 128. CommitFiles must
// return the raw UTF-8 path so ShowFile round-trips.
func TestCommitFilesNonASCIIPathRoundTrip(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	const name = "timing — kopia.log" // em-dash U+2014 → \342\200\224 when quoted
	if err := os.WriteFile(filepath.Join(dir, name), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "add non-ascii file")
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	head := strings.TrimSpace(string(out))

	files, err := repo.CommitFiles(context.Background(), head)
	if err != nil {
		t.Fatal(err)
	}
	var got model.CommitFile
	for _, f := range files {
		if f.Status == "A" {
			got = f
		}
	}
	if got.Path != name {
		t.Fatalf("CommitFiles path = %q (all: %+v), want raw %q", got.Path, files, name)
	}
	// The reported bug: with a quoted path this ShowFile fails with exit 128.
	data, err := repo.ShowFile(context.Background(), head, got.Path)
	if err != nil {
		t.Fatalf("ShowFile(%q) failed (the reported bug): %v", got.Path, err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("content = %q, want \"hello\\n\"", data)
	}
}

func TestTreeFilesRealRepo(t *testing.T) {
	dir, runner := newTestRepo(t) // initial commit contains README.md
	repo := &Repo{Runner: runner}

	// Add a nested file in a second commit; the full tree must list BOTH files
	// (unlike CommitFiles, which would list only the change).
	if err := os.MkdirAll(filepath.Join(dir, "pkg", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg", "sub", "x.go"), []byte("package sub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "add nested file")

	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	head := strings.TrimSpace(string(out))

	files, err := repo.TreeFiles(context.Background(), head)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.Path] = f.Status
	}
	if len(files) != 2 || got["README.md"] != "" || got["pkg/sub/x.go"] != "" {
		t.Fatalf("TreeFiles = %+v, want both files with empty status", files)
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

// writeFile writes content to path (creating parent dirs as needed) for test setup.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commitAll stages all changes and commits them with the given message,
// using "Test" as the author name so LogScope{Author: "Test"} matches.
func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("add", "-A")
	run("commit", "-m", msg)
}

// subjects returns the Subject field of each commit for test error messages.
func subjects(cs []model.Commit) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Subject
	}
	return out
}

func TestLogScopedPathFilter(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	writeFile(t, dir, "a.txt", "1")
	commitAll(t, dir, "touch a")
	writeFile(t, dir, "sub/b.txt", "1")
	commitAll(t, dir, "touch sub/b")

	got, err := repo.LogScoped(context.Background(), 50, 0, LogScope{Paths: []string{"sub"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Subject != "touch sub/b" {
		t.Fatalf("path filter: want only [touch sub/b], got %v", subjects(got))
	}
}

func TestLogScopedAuthorAndGrep(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	writeFile(t, dir, "a.txt", "1")
	commitAll(t, dir, "fix the race")
	writeFile(t, dir, "a.txt", "2")
	commitAll(t, dir, "unrelated change")

	byGrep, err := repo.LogScoped(context.Background(), 50, 0, LogScope{Grep: "RACE"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(byGrep) != 1 || byGrep[0].Subject != "fix the race" {
		t.Fatalf("grep -i: want [fix the race], got %v", subjects(byGrep))
	}

	byAuthor, err := repo.LogScoped(context.Background(), 50, 0, LogScope{Author: "Test"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(byAuthor) != 2 {
		t.Fatalf("author filter: want 2 commits, got %d", len(byAuthor))
	}
}

func TestLogScopeFilteredPredicate(t *testing.T) {
	if (LogScope{}).filtered() {
		t.Fatal("empty scope must not be filtered")
	}
	if (LogScope{Branches: []string{"main"}}).filtered() {
		t.Fatal("branch-only scope must NOT count as filtered (graph stays on)")
	}
	for _, s := range []LogScope{{Paths: []string{"x"}}, {Author: "a"}, {Grep: "g"}, {Since: "1 day ago"}, {Until: "now"}} {
		if !s.filtered() {
			t.Fatalf("%+v must be filtered", s)
		}
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

// TestLogScopedExcludesVersionRefDecorations verifies that refs/gg/* version
// refs do not appear in commit-feed decorations. The commit log should show
// only user-facing refs (branches, tags, remotes).
func TestLogScopedExcludesVersionRefDecorations(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	head, err := repo.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	// Create a version ref that should be excluded from decorations.
	if err := repo.UpdateRef(ctx, "refs/gg/versions/main/1753100000-merge", head); err != nil {
		t.Fatal(err)
	}

	commits, err := repo.LogScoped(ctx, 10, 0, LogScope{}, true)
	if err != nil {
		t.Fatal(err)
	}

	if len(commits) == 0 {
		t.Fatal("expected at least one commit")
	}

	for _, c := range commits {
		for _, ref := range c.Refs {
			if strings.Contains(ref.Name, "gg/versions") {
				t.Fatalf("version ref leaked into decorations: %v", c.Refs)
			}
		}
	}
	_ = dir
}
