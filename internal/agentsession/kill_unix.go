//go:build !windows

package agentsession

import (
	"syscall"
	"time"
)

func attachJob(*Session) {}

// killGrace is how long a SIGTERM'd process group gets before SIGKILL.
const killGrace = 3 * time.Second

// kill sends SIGTERM to the whole process group (the child is a session
// leader, so -pid is its group), then SIGKILL if it outlives the grace.
func (s *Session) kill() {
	if s.cmd.Process == nil || !s.running() {
		return
	}
	pid := s.cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	go func() {
		select {
		case <-s.done:
			// The leader is reaped, but a grandchild that ignored SIGTERM may
			// linger in the group; the SIGKILL below still reaches it.
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		case <-time.After(killGrace):
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}()
}
