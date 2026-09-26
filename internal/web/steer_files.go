package web

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

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

// versionLines counts k's lines as the viewer would show them. known is
// false when there are none to land on (missing, empty, too large, a read
// that failed) — the reply then names no line, as the TUI's does.
func (s *Server) versionLines(ctx context.Context, k ofKey) (int, bool) {
	data, err := readVersion(ctx, s.service(), k.Src, k.Rev, k.Path)
	if err != nil || len(data) == 0 || len(data) > domain.MaxDiffBytes {
		return 0, false
	}
	return strings.Count(strings.TrimSuffix(string(data), "\n"), "\n") + 1, true
}

// steerLanded is the TUI's landedDetail over a line count.
func steerLanded(lead string, line, n int, known bool, evicted string) string {
	detail := lead
	if line > 0 && known {
		if line > n {
			detail = fmt.Sprintf("%s at line %d (line %d is past the end, %d lines)", detail, n, line, n)
		} else {
			detail = fmt.Sprintf("%s at line %d", detail, line)
		}
	}
	if evicted != "" {
		detail += fmt.Sprintf("; closed %s (%d files open)", evicted, maxOpenFiles)
	}
	return detail
}

// steerBackground loads a content link's file into the list without showing
// it (gg open --background): no tab moves. A file a tab shows is left exactly
// as the user has it.
func (s *Server) steerBackground(ctx context.Context, c steer.Command) steer.Reply {
	s.steerMu.Lock()
	shown := s.steerWorktree
	s.steerMu.Unlock()
	if c.Worktree != "" && shown != "" && !domain.SameCheckout(c.Worktree, shown) {
		return steerFail(c, "gg web is showing worktree "+shown+", not "+c.Worktree)
	}
	svc := s.service()
	present, err := svc.WorktreeFilesPresent(ctx, []string{c.File})
	if err != nil {
		return steerFail(c, "checking "+c.File+": "+err.Error())
	}
	if !present[c.File] {
		return steerFail(c, c.File+" is not in the working tree")
	}
	wt := svc.Root()
	k := ofKey{Src: "worktree", Path: c.File}
	if f, ok := s.ofs.lookup(wt, k); ok && f.State == "shown" {
		return steerOK(c, c.File+" is already open on screen")
	}
	line := 0
	if c.Line != nil {
		line = c.Line.No
	}
	f, ev := s.ofs.open(wt, k, "", line)
	s.baseline(wt, f.ID, k)
	s.broadcastOpened(wt, ev, f.Path)
	n, known := s.versionLines(ctx, k)
	return steerOK(c, steerLanded("opened "+c.File+" in the background", line, n, known, ev))
}
