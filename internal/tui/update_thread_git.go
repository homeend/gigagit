package tui

import (
	"context"
	"errors"
	"time"
)

// A handful of actions must resolve a rev SYNCHRONOUSLY on the Bubble Tea
// Update thread, because the resolved value has to be in hand before the
// action's own return value is built (the diff-cache tag, a clipboard
// payload). Update is the single thread that draws the UI and reads keys, so
// anything that blocks there blocks EVERYTHING, ctrl+c included — ctrl+c is
// itself just another KeyMsg.
//
// The wait is real, not theoretical. Domain reads take a repogate Read
// reservation, and repogate is strict FIFO: a queued TreeWrite (any tree-
// touching op, including the background fast-forward lane) makes every later
// Read queue behind it. A TreeWrite op that is parked on a Decider — waiting
// for a modal the frozen Update thread can no longer draw — turns that wait
// into a deadlock with no way out but killing the process. m.opsIdle() does
// not cover it: it is only `!m.running && !m.loading`, and the background
// lane sets neither.
//
// So every synchronous domain read on the Update thread gets this deadline.
// Its job is to make an UNBOUNDED wait bounded — not to be imperceptible.
//
// It was 300ms, chosen as "shorter than a human notices", and that was wrong:
// it is also shorter than a single git invocation on Windows, where every
// process is a CreateProcess and an on-access virus scanner sits in the path.
// The full suite on Windows failed FIVE tui tests on it, all the same cause
// and all on a completely IDLE repo — resolveHeadEndpoint declining HEAD,
// copyShaRow falling back to the abbreviated hash, the compare tag refusing to
// build, ff-diff opening no diff layer. In a real session that is every TUI
// compare gesture and every ref/pair navigate answering "repo busy, try again"
// on a repo nobody is touching: the feature simply does not work there.
//
// A deadline cannot be both shorter than a human notices and longer than a
// slow git spawn, so it is the perceptibility goal that gives way. Two seconds
// is generous enough that the deadline fires only under REAL contention, and
// it costs nothing when there is none: an idle read returns in single-digit
// milliseconds whatever this says. What it buys is unchanged — Update unfreezes
// and can draw the modal a parked TreeWrite is waiting on, instead of the
// process needing to be killed.
//
// Do not tighten this back for tidiness. updateThreadGitFloor pins the reason.
const updateThreadGitTimeout = 2 * time.Second

// updateThreadGitFloor is the smallest deadline this file may carry. It exists
// so a future tidy-up cannot quietly restore a value that works on Linux and
// breaks the TUI on Windows — the failure is a spurious "repo busy" on an idle
// repo, and it is invisible to anyone testing only on Linux.
const updateThreadGitFloor = 1500 * time.Millisecond

// errRepoBusy is what a caller gets when the deadline above fires: the repo
// is legitimately in use, and the action should be retried rather than
// reported as a repo defect (which is what the underlying "unknown revision"
// convention would otherwise look like — domain.ResolveRev reports a git
// failure as ok=false, err=nil, and reserves a non-nil error for exactly this
// ctx case).
var errRepoBusy = errors.New("repo busy, try again")

// updateThreadCtx bounds one synchronous domain read on the Update thread.
// The caller MUST defer the cancel.
func updateThreadCtx(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// busyOr maps a context deadline to errRepoBusy and passes every other error
// through unchanged.
//
// KNOWN LIMIT: domain coalesces reads through singleflight BEFORE it takes
// the reservation, so if an identical read (same query key) is already in
// flight for a DIFFERENT caller's context, this call joins that flight and
// waits for it regardless of the deadline here. The deadline covers the case
// this fix is about — being first in the queue behind a writer — and
// tightening singleflight is a domain-level change, not a TUI one.
func busyOr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errRepoBusy
	}
	return err
}
