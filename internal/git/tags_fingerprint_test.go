package git

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestTagsFingerprintArgv(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git for-each-ref (tags fp)", gitexec.Result{Stdout: "refs/tags/v1\x00abc\n"})
	repo := &Repo{Runner: f}

	fp, err := repo.TagsFingerprint(context.Background())
	if err != nil {
		t.Fatalf("TagsFingerprint: %v", err)
	}
	if fp != "refs/tags/v1\x00abc\n" {
		t.Fatalf("fingerprint = %q, want raw stdout", fp)
	}
	if len(f.Calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(f.Calls))
	}
	want := []string{"for-each-ref", "--format=%(refname)%00%(objectname)", "refs/tags"}
	argv := f.Calls[0].Argv
	if len(argv) != len(want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv = %v, want %v", argv, want)
		}
	}
}

// TestTagsFingerprintTracksTagSet pins the cache-key property: the fingerprint
// changes exactly when the tag ref set changes, and is stable otherwise.
func TestTagsFingerprintTracksTagSet(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	fp0, err := repo.TagsFingerprint(ctx)
	if err != nil {
		t.Fatalf("TagsFingerprint (no tags): %v", err)
	}

	gitExec(t, dir, "tag", "v1")
	fp1, err := repo.TagsFingerprint(ctx)
	if err != nil {
		t.Fatalf("TagsFingerprint (v1): %v", err)
	}
	if fp1 == fp0 {
		t.Fatalf("fingerprint unchanged after adding a tag: %q", fp1)
	}

	fp1again, err := repo.TagsFingerprint(ctx)
	if err != nil {
		t.Fatalf("TagsFingerprint (repeat): %v", err)
	}
	if fp1again != fp1 {
		t.Fatalf("fingerprint unstable with no ref change: %q vs %q", fp1again, fp1)
	}

	gitExec(t, dir, "tag", "-d", "v1")
	fp2, err := repo.TagsFingerprint(ctx)
	if err != nil {
		t.Fatalf("TagsFingerprint (deleted): %v", err)
	}
	if fp2 != fp0 {
		t.Fatalf("fingerprint after delete = %q, want the no-tags value %q", fp2, fp0)
	}
}
