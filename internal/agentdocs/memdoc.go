package agentdocs

import (
	"time"

	"github.com/homeend/gigagit/internal/steer"
)

// memdocWire is a memory document in its protocol form; text on show only.
func memdocWire(o Overview, text bool) steer.Overview {
	w := steer.Overview{ID: o.ID, Title: o.Title, Anchors: len(o.Anchors), Author: o.Author, Plain: o.Plain, Memory: true}
	if !o.Created.IsZero() {
		w.Created = o.Created.UTC().Format(time.RFC3339)
	}
	if text {
		w.Text = o.Text
	}
	return w
}

// AnswerMemdoc runs one memdoc_* command against the store: root is the
// store's key for c.Worktree. Shared by every consumer, so a TUI and the page
// it hosts answer alike. It never touches a file or a screen.
func AnswerMemdoc(s *Store, root string, c steer.Command) steer.Reply {
	fail := func(why string) steer.Reply { return steer.Reply{ID: c.ID, Error: why} }
	if c.Worktree == "" {
		return fail("a memory document needs a worktree")
	}
	memory := func() (Overview, bool) {
		o, ok := s.OverviewIn(root, c.FileID)
		return o, ok && o.Memory
	}
	done := func(o Overview, text bool) steer.Reply {
		return steer.Reply{ID: c.ID, OK: true, Overviews: []steer.Overview{memdocWire(o, text)}}
	}
	switch c.Cmd {
	case "memdoc_add":
		plain := c.Plain != nil && *c.Plain
		o, err := s.AddMemoryOverview(root, c.Worktree, c.FileID, c.Title, c.Text, c.Author, plain)
		if err != nil {
			return fail(err.Error())
		}
		return done(o, false)
	case "memdoc_set":
		if _, ok := memory(); !ok {
			return fail("no overview " + c.FileID)
		}
		if c.Plain != nil {
			if err := s.SetOverviewPlain(root, c.FileID, *c.Plain); err != nil {
				return fail(err.Error())
			}
		}
		if c.Text == "" && c.Title == "" {
			o, _ := memory()
			return done(o, false)
		}
		text := c.Text
		if text == "" {
			o, _ := memory()
			text = o.Text
		}
		o, err := s.SetOverviewIn(root, c.FileID, c.Title, text)
		if err != nil {
			return fail(err.Error())
		}
		return done(o, false)
	case "memdoc_list":
		list := s.MemoryOverviews(root)
		wire := make([]steer.Overview, 0, len(list))
		for _, o := range list {
			wire = append(wire, memdocWire(o, false))
		}
		return steer.Reply{ID: c.ID, OK: true, Overviews: wire}
	case "memdoc_show":
		o, ok := memory()
		if !ok {
			return fail("no overview " + c.FileID)
		}
		return done(o, true)
	case "memdoc_rm":
		if _, ok := memory(); !ok || !s.RemoveOverviewIn(root, c.FileID) {
			return fail("no overview " + c.FileID)
		}
		return steer.Reply{ID: c.ID, OK: true, Detail: "removed " + c.FileID}
	}
	return fail("unknown verb " + c.Cmd)
}
