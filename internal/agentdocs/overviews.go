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
	Missing bool // its file or note was not found when last checked
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

// ParseAnchorDest reads a link destination as an anchor: `note:t<n>`, or a
// repo-relative slash path with an optional `:N` / `:N-M` line suffix.
// Anything that could leave the repository or is not a plain path is not an
// anchor. Dest is left for the caller.
func ParseAnchorDest(dest string) (Anchor, bool) {
	d := strings.TrimSpace(dest)
	if id, ok := strings.CutPrefix(d, "note:"); ok {
		if len(id) > 1 && id[0] == 't' && allDigits(id[1:]) {
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
	s.overviews[e.ID] = e
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
	id, ok := s.tours[key]
	if !ok {
		return "", false
	}
	if _, open := s.overviews[id]; !open {
		delete(s.tours, key)
		return "", false
	}
	return id, true
}

// SetOverview replaces an overview's text, and its title unless title is
// blank. An anchor whose destination stays keeps what the last check found.
func (s *Store) SetOverview(id, title, text string) (Overview, error) {
	if err := overviewRefusal(title, text, false); err != nil {
		return Overview{}, err
	}
	_, anchors := ParseOverview(text)
	s.mu.Lock()
	e := s.overviews[id]
	if e == nil {
		s.mu.Unlock()
		return Overview{}, errors.New("no overview " + id)
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
func (s *Store) RemoveOverview(id string) bool {
	s.mu.Lock()
	_, ok := s.overviews[id]
	delete(s.overviews, id)
	s.mu.Unlock()
	if ok {
		s.b.signal()
	}
	return ok
}

// Overview is a copy of the overview with id.
func (s *Store) Overview(id string) (Overview, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.overviews[id]
	if e == nil {
		return Overview{}, false
	}
	return e.clone(), true
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
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

// CheckAnchors marks the anchors that do not resolve: a file anchor whose
// path is not a regular file under the overview's worktree, a note anchor
// whose note is gone or sits in another root. The stats run without the
// lock (call it off a UI thread). It reports, and signals, whether a flag
// changed.
func (s *Store) CheckAnchors(id string) bool {
	s.mu.Lock()
	e := s.overviews[id]
	if e == nil {
		s.mu.Unlock()
		return false
	}
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
	e = s.overviews[id]
	if e == nil || e.Text != text {
		s.mu.Unlock()
		return false // removed or replaced meanwhile: its own check follows
	}
	changed := false
	for i := range e.Anchors {
		a := &e.Anchors[i]
		if a.Note != "" {
			_, n := s.findLocked(a.Note)
			gone[i] = n == nil || n.Root != e.Root
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

// SetAnchorMissing records what an open found: anchor i of overview id is
// gone (or back). It reports, and signals, whether the flag changed.
func (s *Store) SetAnchorMissing(id string, i int, missing bool) bool {
	s.mu.Lock()
	e := s.overviews[id]
	if e == nil || i < 0 || i >= len(e.Anchors) || e.Anchors[i].Missing == missing {
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
// not touch. "" for no such overview.
func (s *Store) OverviewStamp(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.overviews[id]
	if e == nil {
		return ""
	}
	h := fnv.New64a()
	fmt.Fprintf(h, "%q %q", e.Title, e.Text)
	for _, a := range e.Anchors {
		fmt.Fprintf(h, " %t", a.Missing)
		if a.Note == "" {
			continue
		}
		if _, n := s.findLocked(a.Note); n != nil && n.Root == e.Root {
			fmt.Fprintf(h, " %q:%d-%d", n.Path, n.Start, n.End)
		} else {
			h.Write([]byte(" -"))
		}
	}
	return strconv.FormatUint(h.Sum64(), 36)
}
