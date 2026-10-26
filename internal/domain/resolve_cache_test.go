package domain

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
)

// Inside one WithResolveMemo context (a request) a full sha (with ^ / ~
// steps) is resolved once: opening a review used to spawn 18–33 rev-parses
// for its few revisions, ~0.3s each on a slow mount. A ref name moves, so it
// is asked every time; outside a memo nothing is remembered (a gc can delete
// a commit between two requests).
func TestResolveRevCachesImmutableRevs(t *testing.T) {
	t.Parallel()
	const sha = "8fb6f5e93bd6c493fc1128501f92c9da3396fb48"
	f := gitexec.NewFakeRunner()
	var calls atomic.Int32
	f.SetHandler("git rev-parse verify commit (find)", func(ctx context.Context, argv []string) (gitexec.Result, error) {
		calls.Add(1)
		rev := strings.TrimSuffix(argv[len(argv)-1], "^{commit}")
		if strings.HasPrefix(rev, "0000") {
			return gitexec.Result{ExitCode: 1}, errors.New("exit status 1")
		}
		return gitexec.Result{Stdout: sha + "\n"}, nil
	})
	svc := New(&git.Repo{Runner: f})
	bare := context.Background()
	svc.ResolveRev(bare, sha)
	svc.ResolveRev(bare, sha)
	if n := calls.Load(); n != 2 {
		t.Fatalf("git ran %d times without a memo, want 2", n)
	}
	calls.Store(0)
	ctx := WithResolveMemo(bare)
	if WithResolveMemo(ctx) != ctx {
		t.Fatal("a nested memo must share the outer one")
	}
	for i := 0; i < 3; i++ {
		for _, rev := range []string{sha, sha + "^", sha + "~2"} {
			if got, ok, err := svc.ResolveRev(ctx, rev); err != nil || !ok || strings.TrimSpace(got) != sha {
				t.Fatalf("ResolveRev(%s) = %q %v %v", rev, got, ok, err)
			}
		}
	}
	if n := calls.Load(); n != 3 {
		t.Fatalf("git ran %d times for 3 immutable revs, want 3", n)
	}
	for i := 0; i < 2; i++ {
		svc.ResolveRev(ctx, "main")
		svc.ResolveRev(ctx, "HEAD^")
		svc.ResolveRev(ctx, strings.Repeat("0", 40))
	}
	if n := calls.Load(); n != 8 {
		t.Fatalf("git ran %d times, want 8: refs every time, the missing sha once", n)
	}
}
