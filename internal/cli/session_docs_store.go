package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	if top == "" {
		fmt.Fprintln(stderr, "session "+verb+": not in a git worktree")
		return nil, false
	}
	if dir.inbox == "" {
		fmt.Fprintln(stderr, "session "+verb+": no state directory for the session docs")
		return nil, false
	}
	d := &docsSession{store: agentdocs.New(), root: domain.CheckoutKey(top), top: top, dir: steer.DocsDir(dir.inbox)}
	if err := d.store.Attach(d.root, top, d.dir); err != nil {
		fmt.Fprintln(stderr, "session "+verb+": "+err.Error())
		return nil, false
	}
	return d, true
}

func (d *docsSession) Close() { d.store.Close() }

// readLines is a working-tree file as the store splits it; nil when it
// cannot be read or names a place outside the worktree (the store then
// refuses with "<path> has no lines to note").
func (d *docsSession) readLines(path string) []string {
	if path == "" || strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\\:") {
		return nil
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return nil
		}
	}
	data, err := os.ReadFile(filepath.Join(d.top, filepath.FromSlash(path)))
	if err != nil {
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
