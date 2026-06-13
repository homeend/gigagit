package git

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

func TestParseLog(t *testing.T) {
	// Fields separated by \x1f (unit separator), one commit per line.
	line1 := "aaa111" + "\x1f" + "" + "\x1f" + "Alice" + "\x1f" + "1700000000" + "\x1f" + "initial"
	line2 := "bbb222" + "\x1f" + "aaa111" + "\x1f" + "Bob" + "\x1f" + "1700000100" + "\x1f" + "second commit"
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

func TestRepoLogReturnsCommits(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "commit", "--allow-empty", "-m", "second")

	commits, err := repo.Log(context.Background(), 10, 0)
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
	f.SetResponse("git diff-tree", gitexec.Result{Stdout: "M\tfile.txt\n"})
	repo := &Repo{Runner: f}
	got, err := repo.CommitFiles(context.Background(), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 1 {
		t.Fatalf("git calls = %d, want 1", len(f.Calls))
	}
	argv := strings.Join(f.Calls[0].Argv, " ")
	for _, part := range []string{"diff-tree", "-r", "--root", "--no-commit-id", "--name-status", "-M", "--first-parent", "-m", "abc123"} {
		if !strings.Contains(argv, part) {
			t.Fatalf("argv = %q, missing %q", argv, part)
		}
	}
	if len(got) != 1 || got[0] != (model.CommitFile{Status: "M", Path: "file.txt"}) {
		t.Fatalf("files = %+v", got)
	}
}

func TestLogSkipArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: ""})
	repo := &Repo{Runner: f}

	if _, err := repo.Log(context.Background(), 200, 50); err != nil {
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
	if _, err := repo.Log(context.Background(), 50, 0); err != nil {
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
