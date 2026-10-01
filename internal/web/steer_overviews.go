package web

import (
	"context"
	"strconv"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// The overview verbs on the web (gg session overview add|set|list|show|rm):
// answered by the server from its store when no TUI is live — the TUI's
// steer_overview.go, word for word, except the other-worktree refusal, which
// keeps steer_files.go's words and the busy reason, which is the web's.

func isOverviewVerb(cmd string) bool {
	switch cmd {
	case "overview_add", "overview_set", "overview_list", "overview_show", "overview_rm":
		return true
	}
	return false
}

func (s *Server) steerOverview(ctx context.Context, c steer.Command) steer.Reply {
	if c.Cmd != "overview_list" { // an overview id is this worktree's
		s.steerMu.Lock()
		shown := s.steerWorktree
		s.steerMu.Unlock()
		if c.Worktree != "" && shown != "" && !domain.SameCheckout(c.Worktree, shown) {
			return steerFail(c, "gg web is showing worktree "+shown+", not "+c.Worktree)
		}
	}
	switch c.Cmd {
	case "overview_add":
		return s.steerOverviewAdd(ctx, c)
	case "overview_set":
		return s.steerOverviewSet(ctx, c)
	case "overview_list":
		r := steerOK(c, "")
		for _, o := range s.docs.Overviews(s.docsRoot(ctx)) {
			r.Overviews = append(r.Overviews, s.overviewWire(o, false))
		}
		r.Detail = strconv.Itoa(len(r.Overviews)) + " overviews"
		return r
	case "overview_show":
		o, ok := s.servedOverview(ctx, c.FileID)
		if !ok {
			return steerFail(c, "no overview "+c.FileID)
		}
		r := steerOK(c, o.ID)
		r.Overviews = []steer.Overview{s.overviewWire(o, true)}
		return r
	}
	// overview_rm
	if _, ok := s.servedOverview(ctx, c.FileID); !ok || !s.docs.RemoveOverview(c.FileID) {
		return steerFail(c, "no overview "+c.FileID)
	}
	s.followDocs()
	return steerOK(c, "closed "+c.FileID)
}

// servedOverview is the overview id of the served worktree.
func (s *Server) servedOverview(ctx context.Context, id string) (agentdocs.Overview, bool) {
	o, ok := s.docs.Overview(id)
	if !ok || o.Root != s.docsRoot(ctx) {
		return agentdocs.Overview{}, false
	}
	return o, true
}

// overviewWire is o on the wire: shown while a tab shows it.
func (s *Server) overviewWire(o agentdocs.Overview, text bool) steer.Overview {
	state := "background"
	if s.ofs.shown(o.ID) {
		state = "shown"
	}
	return agentdocs.OverviewWire(o, state, text)
}

// steerOverviewAdd files the overview, checks its anchors, lists it, and —
// unless asked for the background or an op is in flight — has every tab show
// it, as a file_focus would.
func (s *Server) steerOverviewAdd(ctx context.Context, c steer.Command) steer.Reply {
	root, top := s.docsDirs(ctx)
	wt := s.service().Root()
	front := !c.Background && !s.opInFlight()
	var (
		o   agentdocs.Overview
		ev  string
		err error
	)
	s.listDocs(wt, func() string {
		if o, err = s.docs.AddOverview(root, top, c.Title, c.Text); err != nil {
			return ""
		}
		s.docs.CheckAnchors(o.ID)
		_, ev, _ = s.ofs.ensureOpenID(wt, overviewKey(o), o.ID, o.Title)
		if front {
			s.ofs.focus(wt, o.ID, "")
		}
		return ev
	})
	if err != nil {
		return steerFail(c, err.Error())
	}
	var detail string
	switch {
	case c.Background:
		detail = "added " + o.ID + " in the background"
	case !front:
		detail = "added " + o.ID + " in the background (operation in flight)"
	default:
		detail = "showing " + o.ID
		if s.ofs.liveTabs() == 0 {
			detail += "; no gg web tab is open to show it"
		}
	}
	if ev != "" {
		detail += "; closed " + ev + " (" + strconv.Itoa(maxOpenFiles) + " files open)"
	}
	if front {
		if h := s.liveHubRef(); h != nil {
			h.emitSteer(liveMsg{Changed: []string{}, Reason: "steer", Steer: &steerWire{Cmd: "file_focus", FileID: o.ID}})
		}
	}
	o, _ = s.docs.Overview(o.ID)
	r := steerOK(c, detail)
	w := s.overviewWire(o, false)
	if front && s.ofs.liveTabs() > 0 {
		w.State = "shown" // the tabs are bringing it up now
	}
	r.Overviews = []steer.Overview{w}
	return r
}

func (s *Server) steerOverviewSet(ctx context.Context, c steer.Command) steer.Reply {
	if _, ok := s.servedOverview(ctx, c.FileID); !ok {
		return steerFail(c, "no overview "+c.FileID)
	}
	o, err := s.docs.SetOverview(c.FileID, c.Title, c.Text)
	if err != nil {
		return steerFail(c, err.Error())
	}
	s.docs.CheckAnchors(o.ID)
	s.followDocs()
	o, _ = s.docs.Overview(o.ID)
	r := steerOK(c, "set "+o.ID)
	r.Overviews = []steer.Overview{s.overviewWire(o, false)}
	return r
}
