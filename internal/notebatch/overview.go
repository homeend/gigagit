package notebatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// A permanent overview's document (spec 2026-10-12 §4): the review
// document's shape with only the walk — a title, the overview markdown and
// the remarks on lines of the files at the commit. No summary, no meta, no
// old side: it explains code, it does not review a change.
//
//	{"version":1,"title":"…","overview":"<markdown>",
//	 "files":[{"path":"…","annotations":[{"newRange":[a,b],"summary":"…","rationale":"…"}]}]}

// ErrNotOverviewDoc wraps every reason a reply is not an overview document.
var ErrNotOverviewDoc = errors.New("notebatch: not a gg overview document")

// MaxOverviewTitle caps the title. It equals agentdocs.MaxOverviewTitle —
// pinned by a domain test, since this package must stay stdlib-only.
const MaxOverviewTitle = 200

// OverviewDoc is a parsed overview document. Files reuse ReviewFile; every
// note's Side is "new".
type OverviewDoc struct {
	Title, Overview string
	Files           []ReviewFile
}

func notOverview(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrNotOverviewDoc, fmt.Sprintf(format, args...))
}

// present reports a JSON value that is there and not null.
func present(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && string(t) != "null"
}

type rawOverview struct {
	Version  *int            `json:"version"`
	Title    string          `json:"title"`
	Overview string          `json:"overview"`
	Summary  *string         `json:"summary"`
	Meta     json.RawMessage `json:"meta"`
	Files    []struct {
		Path        string          `json:"path"`
		Summary     *string         `json:"summary"`
		Meta        json.RawMessage `json:"meta"`
		Annotations []struct {
			NewRange  []int           `json:"newRange"`
			OldRange  []int           `json:"oldRange"`
			Summary   string          `json:"summary"`
			Rationale string          `json:"rationale"`
			Meta      json.RawMessage `json:"meta"`
		} `json:"annotations"`
	} `json:"files"`
}

// ParseOverview reads an overview document (fenced or in Claude's envelope,
// like ParseReview). A document that breaks the shape fails with an error
// wrapping ErrNotOverviewDoc.
func ParseOverview(data []byte) (OverviewDoc, error) { return ParseOverviewTitled(data, "") }

// ParseOverviewTitled is ParseOverview with a fallback for a missing title
// (the CLI's --title); the document's own title wins.
func ParseOverviewTitled(data []byte, fallbackTitle string) (OverviewDoc, error) {
	var raw rawOverview
	if err := json.NewDecoder(bytes.NewReader(unwrap(data))).Decode(&raw); err != nil {
		return OverviewDoc{}, notOverview("%v", err)
	}
	switch {
	case raw.Version == nil || *raw.Version != 1:
		return OverviewDoc{}, notOverview("version must be 1")
	case raw.Summary != nil:
		return OverviewDoc{}, notOverview("an overview has no summary — that is a review")
	case present(raw.Meta):
		return OverviewDoc{}, notOverview("an overview has no meta — that is a review")
	}
	title := strings.TrimSpace(raw.Title)
	if title == "" {
		title = strings.TrimSpace(fallbackTitle)
	}
	switch {
	case title == "":
		return OverviewDoc{}, notOverview("title is empty")
	case len(title) > MaxOverviewTitle:
		return OverviewDoc{}, notOverview("title exceeds %d bytes", MaxOverviewTitle)
	case strings.TrimSpace(raw.Overview) == "":
		return OverviewDoc{}, notOverview("overview is empty")
	case len(raw.Overview) > MaxOverviewBytes:
		return OverviewDoc{}, notOverview("overview exceeds 64 KiB (%d bytes)", len(raw.Overview))
	}
	doc := OverviewDoc{Title: title, Overview: raw.Overview}
	for i, f := range raw.Files {
		if strings.TrimSpace(f.Path) == "" {
			return OverviewDoc{}, notOverview("files[%d]: path is empty", i)
		}
		if f.Summary != nil || present(f.Meta) {
			return OverviewDoc{}, notOverview("files[%d]: a file has no summary or meta in an overview", i)
		}
		rf := ReviewFile{Path: f.Path}
		for j, a := range f.Annotations {
			where := fmt.Sprintf("files[%d].annotations[%d]", i, j)
			if a.OldRange != nil {
				return OverviewDoc{}, notOverview("%s: oldRange — an overview remarks on lines at the commit (newRange)", where)
			}
			if a.NewRange == nil {
				return OverviewDoc{}, notOverview("%s: no newRange", where)
			}
			if len(a.NewRange) != 2 || a.NewRange[0] < 1 || a.NewRange[1] < a.NewRange[0] {
				return OverviewDoc{}, notOverview("%s: range %v is not [first,last] with 1 <= first <= last", where, a.NewRange)
			}
			if strings.TrimSpace(a.Summary) == "" {
				return OverviewDoc{}, notOverview("%s: summary is empty", where)
			}
			if present(a.Meta) {
				return OverviewDoc{}, notOverview("%s: a remark has no meta in an overview", where)
			}
			rf.Notes = append(rf.Notes, ReviewNote{Side: "new", Range: [2]int{a.NewRange[0], a.NewRange[1]},
				Summary: a.Summary, Rationale: a.Rationale})
		}
		doc.Files = append(doc.Files, rf)
	}
	return doc, nil
}

type canonOverviewNote struct {
	NewRange  [2]int `json:"newRange"`
	Summary   string `json:"summary"`
	Rationale string `json:"rationale,omitempty"`
}

type canonOverviewFile struct {
	Path        string              `json:"path"`
	Annotations []canonOverviewNote `json:"annotations,omitempty"`
}

type canonOverview struct {
	Version  int                 `json:"version"`
	Title    string              `json:"title"`
	Overview string              `json:"overview"`
	Files    []canonOverviewFile `json:"files,omitempty"`
}

// Canonical is the document in its documented shape, indented: what gg
// stores, so a stored overview reads the same whatever wrapping the agent
// used.
func (d OverviewDoc) Canonical() []byte {
	c := canonOverview{Version: 1, Title: d.Title, Overview: d.Overview}
	for _, f := range d.Files {
		cf := canonOverviewFile{Path: f.Path}
		for _, n := range f.Notes {
			cf.Annotations = append(cf.Annotations, canonOverviewNote{NewRange: n.Range, Summary: n.Summary, Rationale: n.Rationale})
		}
		c.Files = append(c.Files, cf)
	}
	out, _ := json.MarshalIndent(c, "", "  ")
	return out
}
