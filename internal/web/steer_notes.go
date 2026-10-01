package web

import (
	"context"
	"regexp"
	"strconv"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// The temporary-note verbs on the web (gg session note add|list|show|rm):
// answered by the server from its store when no TUI is live — the TUI's
// steer_file_notes.go, word for word, except the other-worktree refusal,
// which keeps steer_files.go's words.

var noteIDRE = regexp.MustCompile(`^t[0-9]{1,18}$`)

func isNoteVerb(cmd string) bool {
	switch cmd {
	case "note_add", "note_list", "note_show", "note_rm":
		return true
	}
	return false
}

func (s *Server) steerNote(ctx context.Context, c steer.Command) steer.Reply {
	switch c.Cmd {
	case "note_add":
		return s.steerNoteAdd(ctx, c)
	case "note_list":
		return s.steerNoteList(ctx, c)
	case "note_show":
		return s.steerNoteShow(ctx, c)
	}
	return s.steerNoteRm(ctx, c)
}

// noteFile resolves the open file a verb names by id or path; name is what
// a refusal calls it.
func (s *Server) noteFile(c steer.Command) (f steer.OpenFile, name string, ok bool) {
	name = c.FileID
	if name == "" {
		name = c.File
	}
	f, ok = s.ofs.resolve(s.service().Root(), c.FileID, c.File)
	return f, name, ok
}

// steerNoteAdd puts a note on a working-tree file as it is on disk now,
// listing the file in the background first when the page lacks it.
func (s *Server) steerNoteAdd(ctx context.Context, c steer.Command) steer.Reply {
	s.steerMu.Lock()
	shown := s.steerWorktree
	s.steerMu.Unlock()
	if c.Worktree != "" && shown != "" && !domain.SameCheckout(c.Worktree, shown) {
		return steerFail(c, "gg web is showing worktree "+shown+", not "+c.Worktree)
	}
	svc := s.service()
	wt := svc.Root()
	path := c.File
	if c.FileID != "" {
		f, ok := s.ofs.resolve(wt, c.FileID, "")
		if !ok || f.ID != c.FileID {
			return steerFail(c, "no open file "+c.FileID)
		}
		if f.Source != "worktree" {
			return steerFail(c, "temporary notes go on working-tree files only")
		}
		path = f.Path
	} else {
		present, err := svc.WorktreeFilesPresent(ctx, []string{path})
		if err != nil {
			return steerFail(c, "checking "+path+": "+err.Error())
		}
		if !present[path] {
			return steerFail(c, path+" is not in the working tree")
		}
	}
	data, err := readVersion(ctx, svc, "worktree", "", path)
	if err != nil {
		return steerFail(c, "reading "+path+": "+err.Error())
	}
	var lines []string // a file too large or binary has none to note
	if len(data) <= domain.MaxDiffBytes && !domain.IsBinary(data) {
		lines = agentdocs.Lines(data)
	}
	n, err := s.docs.AddNote(s.docsRoot(ctx), path, lines, c.Start, c.End, c.Summary, c.Rationale, c.Author)
	if err != nil {
		return steerFail(c, err.Error()) // before the list: a refused note lists nothing
	}
	k := ofKey{Src: "worktree", Path: path}
	f, ev, added := s.ofs.ensureOpen(wt, k, true) // noted now; the follow pass tells the tabs
	if added {
		s.baseline(wt, f.ID, k)
	}
	lead := ""
	if ev != "" {
		lead = "; closed " + ev + " (" + strconv.Itoa(maxOpenFiles) + " files open)"
	}
	r := steerOK(c, agentdocs.NotedDetail(n, lead))
	r.Notes = []steer.FileNote{agentdocs.NoteWire(n, f.ID, nil)}
	return r
}

// steerNoteList answers with every note of the served worktree, or of the
// one file named.
func (s *Server) steerNoteList(ctx context.Context, c steer.Command) steer.Reply {
	root, wt := s.docsRoot(ctx), s.service().Root()
	paths := s.docs.NotedPaths(root)
	if c.FileID != "" || c.File != "" {
		f, name, ok := s.noteFile(c)
		if !ok {
			return steerFail(c, "no open file "+name)
		}
		paths = []string{f.Path}
	}
	r := steerOK(c, "")
	for _, p := range paths {
		f, _ := s.ofs.lookup(wt, ofKey{Src: "worktree", Path: p})
		ns, _ := s.docs.Notes(root, p)
		for _, n := range ns {
			r.Notes = append(r.Notes, agentdocs.NoteWire(n, f.ID, nil))
		}
	}
	if len(r.Notes) == 0 {
		r.Detail = "no notes"
	}
	return r
}

// steerNoteShow answers with one note and the lines it sits on now.
func (s *Server) steerNoteShow(ctx context.Context, c steer.Command) steer.Reply {
	n, ok := s.docs.FindNote(c.NoteID)
	if !ok || n.Root != s.docsRoot(ctx) {
		return steerFail(c, "no note "+c.NoteID)
	}
	f, _ := s.ofs.lookup(s.service().Root(), ofKey{Src: "worktree", Path: n.Path})
	r := steerOK(c, "")
	r.Notes = []steer.FileNote{agentdocs.NoteWire(n, f.ID, s.docs.NoteText(n.ID))}
	return r
}

// steerNoteRm removes one note by id, or every note of a file (note clear).
func (s *Server) steerNoteRm(ctx context.Context, c steer.Command) steer.Reply {
	root := s.docsRoot(ctx)
	if c.NoteID != "" {
		if n, ok := s.docs.FindNote(c.NoteID); !ok || n.Root != root || !s.docs.RemoveNote(c.NoteID) {
			return steerFail(c, "no note "+c.NoteID)
		}
		return steerOK(c, "removed "+c.NoteID)
	}
	f, name, ok := s.noteFile(c)
	if !ok {
		return steerFail(c, "no open file "+name)
	}
	return steerOK(c, "removed "+strconv.Itoa(s.docs.ClearPath(root, f.Path))+" notes from "+f.Path)
}
