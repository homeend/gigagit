// Package filelock is a small, stdlib-only cross-process file lock: an
// O_EXCL lock file with an owner token and a stale-lock breaker. It exists
// because more than one process may write the same per-repo state file —
// the TUI, the CLI and gg web can all be open on one repo at once.
//
// internal/notes and internal/preview each carried a byte-identical copy of
// this logic; it is extracted here so a fix (like the Windows ErrPermission
// retry below) lands in one place instead of two or three.
package filelock

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	// Wait is the total retry budget for the cross-process lock.
	Wait = 2 * time.Second
	// Poll is the retry interval while waiting for a held lock.
	Poll = 20 * time.Millisecond
	// Stale is how old an unreleased lock file must be before Acquire
	// treats it as a crashed writer's and breaks it.
	Stale = 30 * time.Second
)

// Acquire takes the cross-process lock at path — an O_EXCL lock file,
// creating path's parent directory if it does not exist yet. It breaks a
// lock older than Stale (a crashed writer), retries for up to Wait, and
// returns a release func on success.
func Acquire(path string) (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// The retry budget uses the REAL clock, never a Now seam: a test that
	// freezes Now for expiry must not spin here forever on a held lock.
	deadline := time.Now().Add(Wait)
	// last is the most recent reason the create failed, so the deadline
	// error says WHICH wall we hit — a held lock reads differently from a
	// name Windows is still tearing down.
	var last error
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			// Stamp the lock with an owner token and check it before removing:
			// after a stale takeover (below) a SECOND process can be holding a
			// freshly created lock under the same name, and an unconditional
			// release would delete someone else's.
			token := lockToken()
			_, werr := f.WriteString(token)
			cerr := f.Close()
			if werr != nil || cerr != nil {
				// A token-less lock would be released by nobody but the
				// staleness breaker, so drop it and report the failure.
				os.Remove(path)
				return nil, errors.Join(werr, cerr)
			}
			return func() { releaseLock(path, token) }, nil
		}
		// ErrExist is the ordinary "someone holds it" answer. ErrPermission
		// must ALSO retry, and only Windows shows why: os.Remove there marks a
		// file whose handle someone still holds (an on-access virus scanner,
		// the search indexer) as PENDING DELETE rather than unlinking it, and
		// an O_EXCL create against that name then fails with
		// ERROR_ACCESS_DENIED — not ErrExist. Aborting on it failed a write
		// outright for a condition that clears in milliseconds; the Windows
		// suite caught it as "Access is denied" from a write whose lock had
		// already been released. Anything else is a real defect and still
		// aborts at once.
		if !errors.Is(err, os.ErrExist) && !errors.Is(err, os.ErrPermission) {
			return nil, err
		}
		// A pending-delete name also refuses os.Stat, so the staleness check
		// below simply does not fire for one — which is correct: a lock nobody
		// holds any more is not a stale lock to break, it is a name to retry.
		last = err
		if fi, statErr := os.Stat(path); statErr == nil && time.Since(fi.ModTime()) > Stale {
			// Stale: the writer died holding it. Retry at once only if the
			// removal actually worked — otherwise (permissions, a Windows
			// share, a racing breaker) fall through to the deadline check and
			// the backoff, so an unremovable stale lock can never spin.
			if rmErr := os.Remove(path); rmErr == nil {
				continue
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("filelock: %s is held; try again (last: %v)", path, last)
		}
		time.Sleep(Poll)
	}
}

// lockToken is one lock holder's identity: this process plus a random nonce,
// so two runs of the same pid (or two holders in one process) never collide.
func lockToken() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Randomness is a nicety here; the pid alone still beats no token.
		return strconv.Itoa(os.Getpid())
	}
	return strconv.Itoa(os.Getpid()) + "-" + hex.EncodeToString(b[:])
}

// releaseLock removes the lock only while WE still hold it. A lock file whose
// content is someone else's token was taken over after our own was declared
// stale, and removing it would strand that writer without a lock. A lock we
// cannot read at all is removed anyway: we wrote it a moment ago, so a read
// error is far likelier a transient (drvfs/9p) than a takeover, and leaving
// it would stall every writer for the Stale window; the Remove is a no-op if
// it is already gone. The takeover race that leaves is a microsecond gap
// behind a lock that was ALREADY stale.
func releaseLock(path, token string) {
	if b, err := os.ReadFile(path); err == nil && string(b) != token {
		return
	}
	os.Remove(path)
}
