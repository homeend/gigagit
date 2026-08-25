package domain

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
)

const (
	tagsFPSpan   = "git for-each-ref (tags fp)"
	tagsFullSpan = "git for-each-ref (tags)"
)

func countSpan(f *gitexec.FakeRunner, name string) int {
	n := 0
	for _, c := range f.Calls {
		if c.Name == name {
			n++
		}
	}
	return n
}

// one annotated tag "v1" in the full-read wire format ParseTags expects.
const tagsV1Line = "v1\x00tag\x00abc123\x00def456\x00release one\x001700000000\n"

func TestTagsCacheServedFromProbeWhenUnchanged(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse(tagsFPSpan, gitexec.Result{Stdout: "refs/tags/v1\x00aaa\n"})
	f.SetResponse(tagsFullSpan, gitexec.Result{Stdout: tagsV1Line})
	svc := New(&git.Repo{Runner: f})
	ctx := context.Background()

	first, err := svc.Tags(ctx)
	if err != nil {
		t.Fatalf("Tags (1st): %v", err)
	}
	if len(first) != 1 || first[0].Name != "v1" || first[0].Target != "def456" {
		t.Fatalf("first = %+v", first)
	}

	second, err := svc.Tags(ctx)
	if err != nil {
		t.Fatalf("Tags (2nd): %v", err)
	}
	if len(second) != 1 || second[0].Name != "v1" {
		t.Fatalf("second = %+v", second)
	}

	if n := countSpan(f, tagsFullSpan); n != 1 {
		t.Fatalf("full reads = %d, want 1 (second call must be served from the cache)", n)
	}
	if n := countSpan(f, tagsFPSpan); n != 2 {
		t.Fatalf("probes = %d, want 2 (one per Tags call)", n)
	}
}

func TestTagsCacheInvalidatesOnFingerprintChange(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	var probes, fulls atomic.Int32
	f.SetHandler(tagsFPSpan, func(ctx context.Context, argv []string) (gitexec.Result, error) {
		if probes.Add(1) == 1 {
			return gitexec.Result{Stdout: "refs/tags/v1\x00aaa\n"}, nil
		}
		return gitexec.Result{Stdout: "refs/tags/v1\x00aaa\nrefs/tags/v2\x00bbb\n"}, nil
	})
	f.SetHandler(tagsFullSpan, func(ctx context.Context, argv []string) (gitexec.Result, error) {
		if fulls.Add(1) == 1 {
			return gitexec.Result{Stdout: tagsV1Line}, nil
		}
		return gitexec.Result{Stdout: "v2\x00commit\x00bbb111\x00\x00\x001700000100\n" + tagsV1Line}, nil
	})
	svc := New(&git.Repo{Runner: f})
	ctx := context.Background()

	first, err := svc.Tags(ctx)
	if err != nil {
		t.Fatalf("Tags (1st): %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first = %+v", first)
	}

	second, err := svc.Tags(ctx)
	if err != nil {
		t.Fatalf("Tags (2nd): %v", err)
	}
	if len(second) != 2 || second[0].Name != "v2" {
		t.Fatalf("second = %+v, want the re-read two-tag list", second)
	}
	if n := fulls.Load(); n != 2 {
		t.Fatalf("full reads = %d, want 2 (changed fingerprint must re-read)", n)
	}
}

func TestTagsProbeErrorFallsBackToFullRead(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetError(tagsFPSpan, errors.New("probe boom"))
	f.SetResponse(tagsFullSpan, gitexec.Result{Stdout: tagsV1Line})
	svc := New(&git.Repo{Runner: f})
	ctx := context.Background()

	for i := 1; i <= 2; i++ {
		tags, err := svc.Tags(ctx)
		if err != nil {
			t.Fatalf("Tags (call %d): a probe failure must not fail the read: %v", i, err)
		}
		if len(tags) != 1 || tags[0].Name != "v1" {
			t.Fatalf("tags (call %d) = %+v", i, tags)
		}
	}
	// Without a fingerprint nothing can be validated — every call is a full read.
	if n := countSpan(f, tagsFullSpan); n != 2 {
		t.Fatalf("full reads = %d, want 2", n)
	}
}

// TestTagsCacheZeroTags pins that an empty repo caches too: ParseTags returns a
// nil slice, and the cache must treat "read, and there were none" as a hit.
func TestTagsCacheZeroTags(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse(tagsFPSpan, gitexec.Result{Stdout: ""})
	f.SetResponse(tagsFullSpan, gitexec.Result{Stdout: ""})
	svc := New(&git.Repo{Runner: f})
	ctx := context.Background()

	for i := 1; i <= 2; i++ {
		tags, err := svc.Tags(ctx)
		if err != nil {
			t.Fatalf("Tags (call %d): %v", i, err)
		}
		if len(tags) != 0 {
			t.Fatalf("tags (call %d) = %+v, want none", i, tags)
		}
	}
	if n := countSpan(f, tagsFullSpan); n != 1 {
		t.Fatalf("full reads = %d, want 1 (empty result must still cache)", n)
	}
}

// TestTagsCacheRealGit exercises the whole path against a real repository:
// adds and deletes must invalidate, unchanged state must not go stale.
func TestTagsCacheRealGit(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	svc := svcAt(dir)
	ctx := context.Background()

	gitRunDir(t, dir, "", "tag", "v1")
	tags, err := svc.Tags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0].Name != "v1" {
		t.Fatalf("tags = %+v", tags)
	}

	gitRunDir(t, dir, "", "tag", "v2")
	tags, err = svc.Tags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 {
		t.Fatalf("after add: tags = %+v, want v1+v2", tags)
	}

	gitRunDir(t, dir, "", "tag", "-d", "v2")
	tags, err = svc.Tags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0].Name != "v1" {
		t.Fatalf("after delete: tags = %+v, want just v1", tags)
	}
}
