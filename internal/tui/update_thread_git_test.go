package tui

import (
	"errors"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/gittest"
)

// The two synchronous domain reads that run on the Bubble Tea Update thread
// must be DEADLINED. Undeadlined, a repogate Read queued behind a tree-write
// blocks Update — which is the one thread that draws the screen and reads
// keys, ctrl+c included — so a parked operation waiting for a modal that can
// no longer be drawn is an unrecoverable freeze, not a slow frame.
//
// Both tests drive the expiry with a deadline that has already passed, which
// is exactly what repogate.Acquire sees when it loses a long queue: the
// select falls straight to <-ctx.Done() and returns ctx.Err(). That makes the
// test deterministic — no reservation to hold, no timing window — while still
// exercising the production error path end to end.

func TestResolveHeadEndpointDeclinesWhenTheRepoIsBusy(t *testing.T) {
	t.Parallel()
	m := loadedModelAt(t, gittest.BasicRepo(t, "hi\n"))

	if _, err := m.resolveHeadEndpointWithin(time.Nanosecond); !errors.Is(err, errRepoBusy) {
		t.Fatalf("an expired deadline must report errRepoBusy, got %v", err)
	}

	// And the ordinary path is untouched: a repo nobody is holding resolves.
	if _, err := m.resolveHeadEndpoint(); err != nil {
		t.Fatalf("an idle repo must still resolve HEAD: %v", err)
	}
}

func TestCopyShaResolveFallsBackWhenTheRepoIsBusy(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hi\n")
	m := loadedModelAt(t, dir)

	const short = "abc1234"
	if got := m.resolveShaWithin("HEAD", short, time.Nanosecond); got != short {
		t.Fatalf("an expired deadline must fall back to the short hash, got %q", got)
	}

	full := m.resolveShaWithin("HEAD", short, updateThreadGitTimeout)
	if full == short || len(full) < 40 {
		t.Fatalf("an idle repo must still resolve the full sha, got %q", full)
	}
}

// A nil service is the menu-built-without-a-repo case: no ctx, no git, just
// the fallback. Pinned because resolveShaWithin moved that guard out of the
// action-row closure.
func TestCopyShaResolveWithoutAService(t *testing.T) {
	t.Parallel()
	var m Model
	if got := m.resolveShaWithin("HEAD", "abc1234", updateThreadGitTimeout); got != "abc1234" {
		t.Fatalf("a nil service must fall back, got %q", got)
	}
}
