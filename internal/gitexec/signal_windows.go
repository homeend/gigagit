//go:build windows

package gitexec

import "os"

// terminate kills the subprocess. Windows has no SIGTERM: os.Process.Signal
// rejects every signal but Kill, and there is no portable way to raise a
// console CTRL event in a process gg did not create a console group for. So a
// cancelled git here CAN leave a lockfile behind (see the unix sibling for
// why that matters) — the stale-lock detection in internal/git/lockfile.go
// plus the TUI's stale_git_lock notice is the recovery path on this platform.
func terminate(p *os.Process) error { return p.Kill() }

// gracefulCancel reports whether cancellation gives git a chance to clean up
// its lockfiles.
const gracefulCancel = false
