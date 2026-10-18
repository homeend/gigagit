package agentdocs

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/notemeta"
	"github.com/homeend/gigagit/internal/steer"
)

// Session documents on disk (spec 2026-10-18-gg-session-docs §3): an
// overview is <id>.md — a +++ TOML front matter block, then markdown whose
// links are anchors — and a note is <id>.toml. The file name is the id. The
// encoders write the canonical spelling the examples fixture proves.

const (
	KindOverview = "overview"
	KindNote     = "note"

	// MaxDocBytes caps one document file; a larger one is ignored unread
	// (the same figure as a steer command file, for the same reason).
	MaxDocBytes = steer.MaxCommandBytes
	// DocFormat is the format this code writes and knows.
	DocFormat = 1

	frontMatterFence = "+++"
	tmpSuffix        = ".tmp"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// ValidID reports whether id may name a document.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// MintID is a fresh id: the UTC second plus four random hex digits. rnd nil
// reads crypto/rand.
func MintID(now time.Time, rnd io.Reader) string {
	if rnd == nil {
		rnd = rand.Reader
	}
	var b [2]byte
	if _, err := io.ReadFull(rnd, b[:]); err != nil {
		// crypto/rand failing is a broken platform; a time-only suffix keeps
		// the id unique per second per writer, which is the documented floor.
		b[0], b[1] = byte(now.Nanosecond()>>16), byte(now.Nanosecond()>>8)
	}
	return now.UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b[:])
}

// DocName reads a directory entry's name: the id and kind of a document, or
// ok=false for anything a reader ignores.
func DocName(name string) (id, kind string, ok bool) {
	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, tmpSuffix) {
		return "", "", false
	}
	switch {
	case strings.HasSuffix(name, ".md"):
		id, kind = strings.TrimSuffix(name, ".md"), KindOverview
	case strings.HasSuffix(name, ".toml"):
		id, kind = strings.TrimSuffix(name, ".toml"), KindNote
	default:
		return "", "", false
	}
	if !ValidID(id) {
		return "", "", false
	}
	return id, kind, true
}

// FileName is the entry name of a document.
func FileName(id, kind string) string {
	if kind == KindNote {
		return id + ".toml"
	}
	return id + ".md"
}

// OverviewFile is a parsed <id>.md.
type OverviewFile struct {
	Format  int
	Title   string
	Author  string
	Created time.Time
	Text    string
}

// overviewHead is the front matter's TOML shape. Created decodes a bare
// TOML date-time (a pointer: absent stays nil); go-toml would ENCODE a
// pointer as a quoted string, so the encoders use the *Dated variants below,
// whose value field writes a bare date-time, and omit the key when zero.
type overviewHead struct {
	Format  int        `toml:"format,omitempty"`
	Title   string     `toml:"title"`
	Author  string     `toml:"author,omitempty"`
	Created *time.Time `toml:"created,omitempty"`
}

type overviewHeadDated struct {
	Format  int       `toml:"format,omitempty"`
	Title   string    `toml:"title"`
	Author  string    `toml:"author,omitempty"`
	Created time.Time `toml:"created"`
}

// NoteFile is a parsed <id>.toml.
type NoteFile struct {
	Format    int
	Path      string
	Start     int
	End       int
	Summary   string
	Rationale string
	Author    string
	Created   time.Time
	Lines     []string
	Meta      []notemeta.KV
	Display   notemeta.Display
}

// noteWire is the file's TOML shape; the key order is the spec's example.
type noteWire struct {
	Format    int              `toml:"format,omitempty"`
	Path      string           `toml:"path"`
	Start     int              `toml:"start"`
	End       int              `toml:"end"`
	Summary   string           `toml:"summary"`
	Rationale string           `toml:"rationale,omitempty"`
	Author    string           `toml:"author,omitempty"`
	Created   *time.Time       `toml:"created,omitempty"`
	Lines     []string         `toml:"lines"`
	Meta      []notemeta.KV    `toml:"meta,omitempty"`
	Display   notemeta.Display `toml:"display,omitempty"`
}

type noteWireDated struct {
	Format    int              `toml:"format,omitempty"`
	Path      string           `toml:"path"`
	Start     int              `toml:"start"`
	End       int              `toml:"end"`
	Summary   string           `toml:"summary"`
	Rationale string           `toml:"rationale,omitempty"`
	Author    string           `toml:"author,omitempty"`
	Created   time.Time        `toml:"created"`
	Lines     []string         `toml:"lines"`
	Meta      []notemeta.KV    `toml:"meta,omitempty"`
	Display   notemeta.Display `toml:"display,omitempty"`
}

var errNoFrontMatter = errors.New("an overview file starts with a +++ front matter block")

// splitFrontMatter returns the TOML between the fences and the body after
// the closing fence's line break.
func splitFrontMatter(data []byte) (head, body []byte, err error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	first, rest, ok := strings.Cut(text, "\n")
	if !ok || first != frontMatterFence {
		return nil, nil, errNoFrontMatter
	}
	lines := strings.Split(rest, "\n")
	for i, l := range lines {
		if l == frontMatterFence {
			return []byte(strings.Join(lines[:i], "\n")), []byte(strings.Join(lines[i+1:], "\n")), nil
		}
	}
	return nil, nil, errNoFrontMatter
}

// tomlErr is a decode failure as protocol prose: a date that is not one is
// named as such, anything else carries go-toml's message.
func tomlErr(err error) error {
	var de *toml.DecodeError
	if errors.As(err, &de) {
		if strings.Contains(strings.ToLower(de.Error()), "time") {
			return errors.New("created must be an RFC 3339 date-time")
		}
		return errors.New("toml: " + de.Error())
	}
	return errors.New("toml: " + err.Error())
}

// ParseOverviewFile reads and validates an <id>.md (spec §3.2).
func ParseOverviewFile(data []byte) (OverviewFile, error) {
	head, body, err := splitFrontMatter(data)
	if err != nil {
		return OverviewFile{}, err
	}
	var h overviewHead
	if err := toml.Unmarshal(head, &h); err != nil {
		return OverviewFile{}, tomlErr(err)
	}
	if err := overviewRefusal(h.Title, string(body), true); err != nil {
		return OverviewFile{}, err
	}
	o := OverviewFile{Format: h.Format, Title: strings.TrimSpace(h.Title), Author: h.Author, Text: string(body)}
	if o.Format == 0 {
		o.Format = DocFormat
	}
	if o.Author == "" {
		o.Author = "agent"
	}
	if h.Created != nil {
		o.Created = h.Created.UTC()
	}
	return o, nil
}

// EncodeOverviewFile is the canonical <id>.md text.
func EncodeOverviewFile(o OverviewFile) ([]byte, error) {
	if err := overviewRefusal(o.Title, o.Text, true); err != nil {
		return nil, err
	}
	author := o.Author
	if author == "" {
		author = "agent"
	}
	var head []byte
	var err error
	if o.Created.IsZero() {
		head, err = toml.Marshal(overviewHead{Format: DocFormat, Title: strings.TrimSpace(o.Title), Author: author})
	} else {
		head, err = toml.Marshal(overviewHeadDated{Format: DocFormat, Title: strings.TrimSpace(o.Title), Author: author, Created: o.Created.UTC()})
	}
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString(frontMatterFence + "\n")
	b.Write(head)
	if !bytes.HasSuffix(head, []byte("\n")) {
		b.WriteByte('\n')
	}
	b.WriteString(frontMatterFence + "\n")
	b.WriteString(o.Text)
	if !strings.HasSuffix(o.Text, "\n") {
		b.WriteByte('\n')
	}
	return b.Bytes(), nil
}

// validPath is a slash-form path inside the worktree: relative, no empty,
// "." or ".." segment, no backslash, no drive colon.
func validPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\:") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// noteRefusal validates a note's fields with the store's prose.
func noteRefusal(n NoteFile) error {
	switch {
	case n.Path == "":
		return errors.New("a note needs a path")
	case !validPath(n.Path):
		return errors.New("path must be a relative slash path inside the worktree")
	case n.Start < 1:
		return errors.New("a line number is 1-based")
	case n.End < n.Start:
		return errors.New("the range ends before it starts")
	case strings.TrimSpace(n.Summary) == "":
		return errors.New("a note needs a summary")
	case utf8.RuneCountInString(n.Summary) > MaxNoteSummary:
		return fmt.Errorf("the summary is longer than %d characters", MaxNoteSummary)
	case utf8.RuneCountInString(n.Rationale) > MaxNoteRationale:
		return fmt.Errorf("the rationale is longer than %d characters", MaxNoteRationale)
	case len(n.Lines) != n.End-n.Start+1:
		return fmt.Errorf("lines must hold end-start+1 entries (got %d, want %d)", len(n.Lines), n.End-n.Start+1)
	}
	return nil
}

// stripBreaks drops a line terminator a writer left on an anchor line.
func stripBreaks(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(l, "\r\n")
	}
	return out
}

// ParseNoteFile reads and validates an <id>.toml (spec §3.3).
func ParseNoteFile(data []byte) (NoteFile, error) {
	var w noteWire
	if err := toml.Unmarshal(data, &w); err != nil {
		return NoteFile{}, tomlErr(err)
	}
	n := NoteFile{Format: w.Format, Path: w.Path, Start: w.Start, End: w.End, Summary: strings.TrimSpace(w.Summary),
		Rationale: w.Rationale, Author: w.Author, Lines: stripBreaks(w.Lines), Display: w.Display}
	if err := noteRefusal(n); err != nil {
		return NoteFile{}, err
	}
	if n.Format == 0 {
		n.Format = DocFormat
	}
	if n.Author == "" {
		n.Author = "agent"
	}
	if w.Created != nil {
		n.Created = w.Created.UTC()
	}
	n.Meta, _ = notemeta.CheckMeta(w.Meta)
	return n, nil
}

// EncodeNoteFile is the canonical <id>.toml text.
func EncodeNoteFile(n NoteFile) ([]byte, error) {
	n.Lines = stripBreaks(n.Lines)
	n.Summary = strings.TrimSpace(n.Summary)
	if err := noteRefusal(n); err != nil {
		return nil, err
	}
	author := n.Author
	if author == "" {
		author = "agent"
	}
	if n.Created.IsZero() {
		return toml.Marshal(noteWire{Format: DocFormat, Path: n.Path, Start: n.Start, End: n.End, Summary: n.Summary,
			Rationale: n.Rationale, Author: author, Lines: n.Lines, Meta: n.Meta, Display: n.Display})
	}
	return toml.Marshal(noteWireDated{Format: DocFormat, Path: n.Path, Start: n.Start, End: n.End, Summary: n.Summary,
		Rationale: n.Rationale, Author: author, Created: n.Created.UTC(), Lines: n.Lines, Meta: n.Meta, Display: n.Display})
}
