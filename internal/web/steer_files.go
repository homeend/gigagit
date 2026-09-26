package web

import "github.com/homeend/gigagit/internal/steer"

// The open-files steer verbs on the web (plan 5c): gg session files, files
// focus and open --background, answered by the server from its own list —
// the TUI's steer_files.go, word for word in every reply.

func steerOK(c steer.Command, detail string) steer.Reply {
	return steer.Reply{ID: c.ID, OK: true, Detail: detail}
}

func steerFail(c steer.Command, msg string) steer.Reply {
	return steer.Reply{ID: c.ID, Error: msg}
}

// steerFiles answers `gg session files`: the served worktree's open files,
// most recently shown first.
func (s *Server) steerFiles(c steer.Command) steer.Reply {
	r := steerOK(c, "")
	r.Files = s.ofs.list(s.service().Root())
	if len(r.Files) == 0 {
		r.Detail = "no open files"
	}
	return r
}
