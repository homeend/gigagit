package web

import (
	"context"
	"path/filepath"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// pausedNoticeWait bounds one repository's read: a slow member costs its
// notices this round, never the centre.
const pausedNoticeWait = 2 * time.Second

// pausedWorktreeNotices is the TUI's notice of the same name: one per worktree
// with an op stopped, in this repository and the open workspace's other members,
// without the worktree the page shows (its banner says so). It goes by itself
// when the op ends, so there is no "never" to offer.
func pausedWorktreeNotices(ctx context.Context, s *Server, svc *domain.Service, h model.RepoHealth, _ map[string]bool) []noticeWire {
	services := []*domain.Service{svc}
	if info := s.workspaceInfo(); info != nil {
		for _, m := range info.Members {
			if !m.Missing && m.Svc != nil {
				services = append(services, m.Svc)
			}
		}
	}
	here, err := svc.TopLevel(ctx)
	if err != nil {
		return nil
	}
	var out []noticeWire
	live := map[string]bool{} // a member's Service need not be the page's: dedupe here
	complete := true
	for _, one := range services {
		rctx, cancel := context.WithTimeout(ctx, pausedNoticeWait)
		wts, err := one.Worktrees(rctx)
		cancel()
		if err != nil {
			complete = false
			continue
		}
		for _, w := range wts {
			if w.Paused == "" || model.SamePath(w.Path, here) || live[w.PausedNoticeID()] {
				continue
			}
			live[w.PausedNoticeID()] = true
			name := filepath.Base(w.Path)
			out = append(out, noticeWire{
				ID:     w.PausedNoticeID(),
				Title:  pausedLabel(w) + " in " + name,
				Detail: []string{w.Path, "Another worktree is stopped here; finish or abort it there."},
				Actions: []noticeAction{
					// Keep: a refused switch leaves the notice, a landed one drops it.
					{Label: "switch to " + name, Reroot: w.Path, Keep: true},
					{Label: labelNotNow, Session: true},
				},
			})
		}
	}
	if complete { // a member that could not answer may still be paused
		s.notices.forgetEndedPaused(h.GitCommonDir, live)
	}
	return out
}

// pausedLabel is static/wtmarks.js pausedMark for a title.
func pausedLabel(w model.Worktree) string {
	switch {
	case w.Conflicted:
		return "✗ " + w.Paused + " conflict"
	case w.Paused == "merge":
		return "⑂ merge in progress"
	}
	return "↻ " + w.Paused + " in progress"
}
