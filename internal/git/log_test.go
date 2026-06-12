package git

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/gitexec"
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

	commits, err := repo.Log(context.Background(), 10)
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
