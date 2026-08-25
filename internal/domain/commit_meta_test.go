package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
)

// CommitMeta is the domain door to the single-commit metadata lookup: frontends
// never touch internal/git, so the files-view header reaches it through here.
func TestCommitMetaReturnsTheCommit(t *testing.T) {
	t.Parallel()
	f := fakeReads()
	f.SetResponse("git log", gitexec.Result{
		Stdout: "aaaabbbb\x1fparent1\x1falice\x1f981173106\x1fthe subject\x1f\x1f\n",
	})
	got, err := New(&git.Repo{Runner: f}).CommitMeta(context.Background(), "aaaabbbb")
	if err != nil {
		t.Fatalf("CommitMeta: %v", err)
	}
	if got.Subject != "the subject" {
		t.Errorf("Subject = %q, want %q", got.Subject, "the subject")
	}
	if got.UnixTime != 981173106 {
		t.Errorf("UnixTime = %d, want 981173106", got.UnixTime)
	}
	if got.Author != "alice" {
		t.Errorf("Author = %q, want alice", got.Author)
	}
}

// An unresolvable rev surfaces the error rather than a zero Commit, so a
// caller can leave its header alone instead of painting an empty date.
func TestCommitMetaUnknownRevErrors(t *testing.T) {
	t.Parallel()
	f := fakeReads()
	f.SetResponse("git log", gitexec.Result{Stdout: ""})
	if _, err := New(&git.Repo{Runner: f}).CommitMeta(context.Background(), "deadbeef"); err == nil {
		t.Fatal("CommitMeta for an unresolvable rev = nil error, want an error")
	}
}
