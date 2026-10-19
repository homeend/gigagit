package agentdocs

import (
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/homeend/gigagit/internal/markdown"
)

// Overview documents (spec 2026-09-30-agent-overview-documents): an agent's
// markdown whose links are ANCHORS — to a working-tree file, a line or range
// in it, or one of its notes. The store keeps the text and the anchors; each
// frontend lays the text out and keeps its own selection.

// The overview limits.
const (
	MaxOverviewBytes    = 64 << 10
	MaxOverviewsPerRoot = 20
	MaxOverviewTitle    = 200
	MaxAnchors          = 100 // links past it stay plain text
)

// Anchor is one anchor, in document order: a file (Start 0: no line), a line
// or range in it (1-based), or a note (Note "t<n>").
type Anchor struct {
	Dest    string // the destination as written
	Path    string
	Start   int
	End     int
	Note    string
	Label   string // the link text, markup dropped (ParseOverview)
	Missing bool   // its file or note was not found when last checked
}

// Overview is one overview, as a copy. ID is an open-file id ("f<n>") from
// NextFileSeq, so it is the same in every list drawing from this store.
type Overview struct {
	ID      string
	Seq     int64
	Root    string
	Title   string
	Text    string
	Anchors []Anchor
	// Author and Created come from a document file (attach.go); a memory
	// overview has neither.
	Author  string
	Created time.Time
}

// ovEntry is a stored overview plus the worktree on disk its file anchors
// are stat'ed under (Root is a key, case-folded on some systems).
type ovEntry struct {
	Overview
	dir string
}

func (o Overview) clone() Overview {
	o.Anchors = append([]Anchor(nil), o.Anchors...)
	return o
}

// ParseAnchorDest reads a link destination as an anchor: `note:<id>` (a
// document id — a session note's file name, or a memory t<n>), or a
// repo-relative slash path with an optional `:N` / `:N-M` line suffix.
// Anything that could leave the repository or is not a plain path is not an
// anchor. Dest is left for the caller.
func ParseAnchorDest(dest string) (Anchor, bool) {
	d := strings.TrimSpace(dest)
	if id, ok := strings.CutPrefix(d, "note:"); ok {
		if ValidID(id) {
			return Anchor{Note: id}, true
		}
		return Anchor{}, false
	}
	d = strings.TrimPrefix(d, "./")
	if d == "" || strings.HasPrefix(d, "/") || strings.Contains(d, "\\") || strings.Contains(d, "://") {
		return Anchor{}, false
	}
	if len(d) >= 2 && d[1] == ':' && (len(d) == 2 || d[2] == '/') && (d[0]|0x20) >= 'a' && (d[0]|0x20) <= 'z' {
		return Anchor{}, false // a Windows drive
	}
	for _, r := range d {
		if r <= ' ' || r == 0x7f {
			return Anchor{}, false
		}
	}
	a := Anchor{Path: d}
	if i := strings.LastIndexByte(d, ':'); i >= 0 {
		lo, hi, ranged := d[i+1:], "", false
		if j := strings.IndexByte(lo, '-'); j >= 0 {
			lo, hi, ranged = lo[:j], lo[j+1:], true
		}
		if allDigits(lo) && (!ranged || allDigits(hi)) {
			s, _ := strconv.Atoi(lo)
			e := s
			if ranged {
				e, _ = strconv.Atoi(hi)
			}
			if s < 1 || e < s {
				return Anchor{}, false
			}
			a = Anchor{Path: d[:i], Start: s, End: e}
		}
	}
	if a.Path == "" {
		return Anchor{}, false
	}
	for _, seg := range strings.Split(a.Path, "/") {
		if seg == ".." {
			return Anchor{}, false
		}
	}
	return a, true
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// IsAnchorDest is ParseAnchorDest's verdict alone (markdown.Options.Anchor).
func IsAnchorDest(dest string) bool { _, ok := ParseAnchorDest(dest); return ok }

// ParseOverview parses text with anchors: every anchor inline's Text is its
// index in the returned anchors (document order); links past MaxAnchors
// become plain text.
func ParseOverview(text string) (markdown.Doc, []Anchor) {
	doc := markdown.ParseWith(text, markdown.Options{Anchor: IsAnchorDest})
	var anchors []Anchor
	var number func([]markdown.Inline)
	number = func(in []markdown.Inline) {
		for i := range in {
			n := &in[i]
			if n.Kind == markdown.InAnchor {
				if len(anchors) >= MaxAnchors {
					*n = markdown.Inline{Kind: markdown.InText, Text: flat(n.In)}
					continue
				}
				a, _ := ParseAnchorDest(n.URL)
				a.Dest = n.URL
				a.Label = flat(n.In)
				n.Text = strconv.Itoa(len(anchors))
				anchors = append(anchors, a)
				continue
			}
			number(n.In)
		}
	}
	var blocks func([]markdown.Block)
	blocks = func(bs []markdown.Block) {
		for i := range bs {
			b := &bs[i]
			number(b.Inline)
			blocks(b.Blocks)
			for j := range b.Items {
				blocks(b.Items[j].Blocks)
			}
			for _, c := range b.Head {
				number(c)
			}
			for _, r := range b.Rows {
				for _, c := range r {
					number(c)
				}
			}
		}
	}
	blocks(doc.Blocks)
	return doc, anchors
}

// flat is the text of inlines, markup dropped.
func flat(in []markdown.Inline) string {
	var sb strings.Builder
	for _, n := range in {
		if n.Kind == markdown.InBreak { // a label over a line break reads as two words
			sb.WriteByte(' ')
			continue
		}
		sb.WriteString(n.Text)
		sb.WriteString(flat(n.In))
	}
	return sb.String()
}

// overviewRefusal checks an add's or a set's text and title.
func overviewRefusal(title, text string, needTitle bool) error {
	switch {
	case strings.TrimSpace(text) == "":
		return errors.New("an overview needs text")
	case len(text) > MaxOverviewBytes:
		return errors.New("the text is over 64 KiB")
	case needTitle && strings.TrimSpace(title) == "":
		return errors.New("an overview needs a title")
	case strings.ContainsAny(title, "\r\n") || utf8.RuneCountInString(title) > MaxOverviewTitle:
		return errors.New("the title must be one line of at most 200 characters")
	}
	return nil
}

// AddOverview files a new overview under root (the caller's key); dir is the
// worktree on disk its file anchors are checked under. The errors are English
// protocol prose for the agent that asked.
func (s *Store) AddOverview(root, dir, title, text string) (Overview, error) {
	if err := overviewRefusal(title, text, true); err != nil {
		return Overview{}, err
	}
	if r := s.rootOf(root); r != nil {
		return s.addOverviewFile(r, root, title, text)
	}
	_, anchors := ParseOverview(text)
	s.mu.Lock()
	n := 0
	for _, e := range s.overviews {
		if e.Root == root {
			n++
		}
	}
	if n >= MaxOverviewsPerRoot {
		s.mu.Unlock()
		return Overview{}, errors.New(strconv.Itoa(MaxOverviewsPerRoot) + " overviews are open; remove one first")
	}
	s.fileSeq++
	e := &ovEntry{Overview: Overview{ID: "f" + strconv.FormatInt(s.fileSeq, 10), Seq: s.fileSeq, Root: root,
		Title: strings.TrimSpace(title), Text: text, Anchors: anchors}, dir: dir}
	s.overviews[ovKey{root, e.ID}] = e
	out := e.clone()
	s.mu.Unlock()
	s.b.signal()
	return out, nil
}

// FileTour files or replaces the overview a caller keeps under key (an
// agent tour: "brief:<session>", "report:<session>"): replaced in place while
// it is open, filed anew when it never was or the user closed it. added
// says which.
func (s *Store) FileTour(key, root, dir, title, text string) (Overview, bool, error) {
	// Two filings of one key at once (the TUI's wake and a web click) must
	// not both add: the second would orphan an overview that counts against
	// the cap. s.mu cannot cover the steps (each takes it), tourMu does.
	s.tourMu.Lock()
	defer s.tourMu.Unlock()
	if r := s.rootOf(root); r != nil {
		return s.fileTourFile(r, root, key, title, text)
	}
	if id, ok := s.TourID(key); ok {
		if o, err := s.SetOverview(id, title, text); err == nil {
			return o, false, nil
		}
		// closed between the two calls: file it anew
	}
	o, err := s.AddOverview(root, dir, title, text)
	if err != nil {
		return Overview{}, false, err
	}
	s.mu.Lock()
	s.tours[key] = o.ID
	s.mu.Unlock()
	return o, true, nil
}

// TourID is the open overview filed under key.
func (s *Store) TourID(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fid := tourID(key); s.ovLocked(fid) != nil {
		return fid, true // a tour filed on an attached root: its fixed name
	}
	id, ok := s.tours[key]
	if !ok {
		return "", false
	}
	if s.ovLocked(id) == nil {
		delete(s.tours, key)
		return "", false
	}
	return id, true
}

// SetOverview replaces an overview's text, and its title unless title is
// blank. An anchor whose destination stays keeps what the last check found.
func (s *Store) SetOverview(id, title, text string) (Overview, error) {
	return s.setOverview("", id, title, text)
}

// SetOverviewIn is SetOverview for the overview id of one root (an id is
// unique per worktree, not per store).
func (s *Store) SetOverviewIn(root, id, title, text string) (Overview, error) {
	return s.setOverview(root, id, title, text)
}

func (s *Store) setOverview(root, id, title, text string) (Overview, error) {
	if err := overviewRefusal(title, text, false); err != nil {
		return Overview{}, err
	}
	_, anchors := ParseOverview(text)
	s.mu.Lock()
	e, err := s.ovResolveLocked(root, id)
	if err != nil {
		s.mu.Unlock()
		return Overview{}, err
	}
	if r := s.roots[e.Root]; r != nil {
		root := e.Root
		s.mu.Unlock()
		return s.setOverviewFile(r, root, id, title, text)
	}
	was := map[string]bool{}
	for _, a := range e.Anchors {
		was[a.Dest] = a.Missing
	}
	for i := range anchors {
		anchors[i].Missing = was[anchors[i].Dest]
	}
	e.Text, e.Anchors = text, anchors
	if t := strings.TrimSpace(title); t != "" {
		e.Title = t
	}
	out := e.clone()
	s.mu.Unlock()
	s.b.signal()
	return out, nil
}

// RemoveOverview drops an overview; false when there was none.
func (s *Store) RemoveOverview(id string) bool { return s.removeOverview("", id) }

// RemoveOverviewIn is RemoveOverview for the overview id of one root.
func (s *Store) RemoveOverviewIn(root, id string) bool { return s.removeOverview(root, id) }

func (s *Store) removeOverview(root, id string) bool {
	s.mu.Lock()
	e, err := s.ovResolveLocked(root, id)
	if err != nil {
		s.mu.Unlock()
		return false // none, or the same id in several roots: nothing is touched
	}
	if r := s.roots[e.Root]; r != nil {
		root := e.Root
		s.mu.Unlock()
		return s.removeDocFile(r, root, id, KindOverview)
	}
	delete(s.overviews, ovKey{e.Root, id})
	s.mu.Unlock()
	s.b.signal()
	return true
}

// Overview is a copy of the overview with id — when exactly one root holds
// that id. The same id in two attached worktrees resolves only by root
// (OverviewIn).
func (s *Store) Overview(id string) (Overview, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.ovLocked(id)
	if e == nil {
		return Overview{}, false
	}
	return e.clone(), true
}

// OverviewIn is a copy of root's overview id.
func (s *Store) OverviewIn(root, id string) (Overview, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.overviews[ovKey{root, id}]
	if e == nil {
		return Overview{}, false
	}
	return e.clone(), true
}

// ovLocked is the one overview with id across roots; nil when there is none
// or when several roots hold the id (s.mu held).
func (s *Store) ovLocked(id string) *ovEntry {
	var found *ovEntry
	for k, e := range s.overviews {
		if k.id != id {
			continue
		}
		if found != nil {
			return nil
		}
		found = e
	}
	return found
}

var errAmbiguousID = errors.New("the id names a document in several worktrees")

// ovResolveLocked is root's overview id, or — root "" — the one overview
// with that id; the errors are protocol prose (s.mu held).
func (s *Store) ovResolveLocked(root, id string) (*ovEntry, error) {
	if root != "" {
		if e := s.overviews[ovKey{root, id}]; e != nil {
			return e, nil
		}
		return nil, errors.New("no overview " + id)
	}
	n := 0
	var found *ovEntry
	for k, e := range s.overviews {
		if k.id == id {
			n++
			found = e
		}
	}
	switch n {
	case 0:
		return nil, errors.New("no overview " + id)
	case 1:
		return found, nil
	}
	return nil, fmt.Errorf("overview %s: %w", id, errAmbiguousID)
}

// Overviews are copies of root's overviews, oldest first.
func (s *Store) Overviews(root string) []Overview {
	s.mu.Lock()
	var out []Overview
	for _, e := range s.overviews {
		if e.Root == root {
			out = append(out, e.clone())
		}
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Seq != out[j].Seq {
			return out[i].Seq < out[j].Seq
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// CheckAnchors is CheckAnchorsIn for the one root holding id: an id two
// attached worktrees share checks nothing (a frontend showing several roots
// uses CheckAnchorsIn).
func (s *Store) CheckAnchors(id string) bool { return s.CheckAnchorsIn("", id) }

// CheckAnchorsIn marks the anchors of root's overview id that do not
// resolve: a file anchor whose path is not a regular file under the
// overview's worktree, a note anchor whose note is gone or sits in another
// root. The stats run without the lock (call it off a UI thread). It
// reports, and signals, whether a flag changed.
func (s *Store) CheckAnchorsIn(root, id string) bool {
	s.mu.Lock()
	e, err := s.ovResolveLocked(root, id)
	if err != nil {
		s.mu.Unlock()
		return false
	}
	key := ovKey{e.Root, id}
	text, dir, anchors := e.Text, e.dir, append([]Anchor(nil), e.Anchors...)
	s.mu.Unlock()
	gone := make([]bool, len(anchors))
	for i, a := range anchors {
		if a.Note == "" {
			st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(a.Path)))
			gone[i] = err != nil || !st.Mode().IsRegular()
		}
	}
	s.mu.Lock()
	e = s.overviews[key]
	if e == nil || e.Text != text {
		s.mu.Unlock()
		return false // removed or replaced meanwhile: its own check follows
	}
	changed := false
	for i := range e.Anchors {
		a := &e.Anchors[i]
		if a.Note != "" {
			_, n := s.findInLocked(e.Root, a.Note)
			gone[i] = n == nil
		}
		if a.Missing != gone[i] {
			a.Missing, changed = gone[i], true
		}
	}
	s.mu.Unlock()
	if changed {
		s.b.signal()
	}
	return changed
}

// SetAnchorMissing is SetAnchorMissingIn for the one root holding id (an
// ambiguous id sets nothing).
func (s *Store) SetAnchorMissing(id string, i int, missing bool) bool {
	return s.SetAnchorMissingIn("", id, i, missing)
}

// SetAnchorMissingIn records what an open found: anchor i of root's overview
// id is gone (or back). It reports, and signals, whether the flag changed.
func (s *Store) SetAnchorMissingIn(root, id string, i int, missing bool) bool {
	s.mu.Lock()
	e, err := s.ovResolveLocked(root, id)
	if err != nil || i < 0 || i >= len(e.Anchors) || e.Anchors[i].Missing == missing {
		s.mu.Unlock()
		return false
	}
	e.Anchors[i].Missing = missing
	s.mu.Unlock()
	s.b.signal()
	return true
}

// OverviewStamp fingerprints what a viewer draws of overview id — its title,
// text, the anchors' missing flags and where each anchored note sits — so a
// tab can skip re-fetching (and re-checking) an overview a store change did
// not touch. "" for no such overview. OverviewStampIn("", id): the one root
// holding id — an id in several roots stamps nothing.
func (s *Store) OverviewStamp(id string) string { return s.OverviewStampIn("", id) }

// OverviewStampIn is OverviewStamp for overview id of root.
func (s *Store) OverviewStampIn(root, id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.ovResolveLocked(root, id)
	if err != nil {
		return ""
	}
	h := fnv.New64a()
	fmt.Fprintf(h, "%q %q", e.Title, e.Text)
	for _, a := range e.Anchors {
		fmt.Fprintf(h, " %t", a.Missing)
		if a.Note == "" {
			continue
		}
		if _, n := s.findInLocked(e.Root, a.Note); n != nil {
			fmt.Fprintf(h, " %q:%d-%d", n.Path, n.Start, n.End)
		} else {
			h.Write([]byte(" -"))
		}
	}
	return strconv.FormatUint(h.Sum64(), 36)
}
