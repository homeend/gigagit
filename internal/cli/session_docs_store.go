package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// The session docs verbs (gg session note|overview|docs) are FILE
// operations on the caller's worktree directory (spec
// 2026-10-18-gg-session-docs §6.2): no window needs to be live. A verb
// attaches a throw-away agentdocs store to the directory — the same
// validation, minted ids and temp+rename every reader trusts — and closes
// it when done.

type docsSession struct {
	store *agentdocs.Store
	svc   *domain.Service
	root  string // domain.CheckoutKey(top)
	top   string // the worktree on disk
	dir   string // steer.DocsDir(inbox)
}

// openDocs attaches a store to the caller's session-docs directory; false
// (reason printed) without a worktree or a state home.
func openDocs(verb string, dir sessDir, svc *domain.Service, stderr io.Writer) (*docsSession, bool) {
	top := ""
	if svc != nil {
		top = callerWorktree(svc)
	}
	return openDocsAt(verb, top, dir.inbox, svc, stderr)
}

// openDocsAt attaches a store to worktree top's session-docs directory under
// inbox — any checkout's, not only the caller's (gg overview show <link>).
func openDocsAt(verb, top, inbox string, svc *domain.Service, stderr io.Writer) (*docsSession, bool) {
	if top == "" {
		fmt.Fprintln(stderr, "session "+verb+": not in a git worktree")
		return nil, false
	}
	if inbox == "" {
		fmt.Fprintln(stderr, "session "+verb+": no state directory for the session docs")
		return nil, false
	}
	d := &docsSession{store: agentdocs.New(), svc: svc, root: domain.CheckoutKey(top), top: top, dir: steer.DocsDir(inbox)}
	if err := d.store.Attach(d.root, top, d.dir); err != nil {
		fmt.Fprintln(stderr, "session "+verb+": "+err.Error())
		return nil, false
	}
	return d, true
}

func (d *docsSession) Close() { d.store.Close() }

// readLines is a working-tree file as the store splits it; nil when it
// cannot be read, names a place outside the worktree, is binary or is over
// the diff size cap (the store then refuses with "<path> has no lines to
// note", spec §6.2). Read through the service's worktree I/O, which keeps
// the repo's escape rule.
func (d *docsSession) readLines(path string) []string {
	if path == "" || strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\\:") {
		return nil
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return nil
		}
	}
	data, err := d.svc.WorktreeFile(context.Background(), path)
	if err != nil || len(data) > domain.MaxDiffBytes || domain.IsBinary(data) {
		return nil
	}
	return agentdocs.Lines(data)
}

// alignedNotes is path's notes placed on the file as it is now.
func (d *docsSession) alignedNotes(path string) []agentdocs.Note {
	if lines := d.readLines(path); len(lines) > 0 {
		return d.store.AlignedNotes(d.root, path, lines)
	}
	ns, _ := d.store.Notes(d.root, path)
	return ns
}

// noteWire is a note as the CLI prints it (the steer shape plus created).
func (d *docsSession) noteWire(n agentdocs.Note, text []string) steer.FileNote {
	w := agentdocs.NoteWire(n, "", text)
	if !n.Created.IsZero() {
		w.Created = n.Created.UTC().Format(time.RFC3339)
	}
	return w
}
