//go:build !windows

package gitexec

import (
	"os"
	"syscall"
)

// terminate asks a git subprocess to stop the way git itself expects.
//
// git registers a sigchain handler that removes every lockfile it holds
// (.git/index.lock, refs/…lock, FETCH_HEAD.lock, …) and then re-raises, so a
// SIGTERM'd git leaves the repository clean. SIGKILL — Go's default cancel
// action for exec.CommandContext — cannot be trapped, so it leaves those
// lockfiles behind and the next operation fails with "Another git process
// seems to be running in this repository". gg cancels git constantly (every
// user action preempts the background refresh lane), so the difference is the
// difference between a usable repo and one the user has to unlock by hand.
func terminate(p *os.Process) error { return p.Signal(syscall.SIGTERM) }

// gracefulCancel reports whether cancellation gives git a chance to clean up
// its lockfiles. False on Windows, where only a hard kill is available — the
// stale-lock notice is the recovery path there.
const gracefulCancel = true
