package git

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

// CommitMeta is the single-commit metadata lookup behind the files-view header
// (and any other surface handed a bare sha): author, author time, subject and
// ref decorations, with no walk.
func TestCommitMetaReturnsAuthorTimeAndSubject(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	// A commit whose AUTHOR time differs from its committer time — the header
	// renders the author date (model.Commit.UnixTime is %at), so a verb that
	// reported %ct would pass a same-time fixture and lie in the real world.
	const authored = "2001-02-03T04:05:06+00:00"
	cmd := exec.Command("git", "commit", "--allow-empty", "-m", "second subject")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=alice", "GIT_AUTHOR_EMAIL=a@a", "GIT_AUTHOR_DATE="+authored,
		"GIT_COMMITTER_NAME=bob", "GIT_COMMITTER_EMAIL=b@b")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}

	got, err := repo.CommitMeta(context.Background(), "HEAD")
	if err != nil {
		t.Fatalf("CommitMeta: %v", err)
	}
	if got.Subject != "second subject" {
		t.Errorf("Subject = %q, want %q", got.Subject, "second subject")
	}
	if got.Author != "alice" {
		t.Errorf("Author = %q, want alice (the AUTHOR, not the committer)", got.Author)
	}
	if got.UnixTime != 981173106 { // 2001-02-03T04:05:06Z
		t.Errorf("UnixTime = %d, want 981173106 (the author time, not the committer time)", got.UnixTime)
	}
	if len(got.Hash) != 40 {
		t.Errorf("Hash = %q, want a full sha", got.Hash)
	}
	if len(got.Parents) != 1 {
		t.Errorf("Parents = %v, want the one parent", got.Parents)
	}
}

// A tag name resolves like any other rev, and its decorations come back so the
// caller can show them.
func TestCommitMetaResolvesATagAndCarriesRefs(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	cmd := exec.Command("git", "tag", "v1")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tag: %v\n%s", err, out)
	}

	got, err := repo.CommitMeta(context.Background(), "v1")
	if err != nil {
		t.Fatalf("CommitMeta: %v", err)
	}
	if got.Subject != "initial" {
		t.Errorf("Subject = %q, want initial", got.Subject)
	}
	if got.UnixTime == 0 {
		t.Error("UnixTime = 0, want the commit's author time")
	}
	names := make([]string, 0, len(got.Refs))
	for _, r := range got.Refs {
		names = append(names, r.Name)
	}
	if !slices.Contains(names, "v1") {
		t.Errorf("Refs = %v, want the v1 tag among them", names)
	}
}

// A rev that resolves to nothing is an error, not a zero Commit that would
// render as an empty header.
func TestCommitMetaUnknownRevIsAnError(t *testing.T) {
	t.Parallel()
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	if _, err := repo.CommitMeta(context.Background(), "0123456789012345678901234567890123456789"); err == nil {
		t.Fatal("CommitMeta on an unknown rev = nil error, want an error")
	}
}

// One verb is one invocation: the header lookup must not fan out into several
// git calls on the files-view open path.
func TestCommitMetaIsOneInvocation(t *testing.T) {
	t.Parallel()
	fake := gitexec.NewFakeRunner()
	fake.SetResponse("git log", gitexec.Result{
		Stdout: "abc\x1f\x1falice\x1f981173106\x1fsubject\x1f\x1f\n",
	})
	repo := &Repo{Runner: fake}
	if _, err := repo.CommitMeta(context.Background(), "HEAD"); err != nil {
		t.Fatalf("CommitMeta: %v", err)
	}
	if n := len(fake.Calls); n != 1 {
		t.Fatalf("CommitMeta ran %d git invocations, want 1: %v", n, fake.Calls)
	}
	argv := strings.Join(fake.Calls[0].Argv, " ")
	for _, want := range []string{"log", "-1", "--format=", "HEAD"} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv %q missing %q", argv, want)
		}
	}
}
