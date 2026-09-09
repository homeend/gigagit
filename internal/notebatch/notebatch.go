// Package notebatch parses the two JSON batch shapes an AI agent may hand gg
// for `gg note apply --stdin`, the `gg_notes_apply` MCP tool and the
// `gg review --notes` import:
//
//  1. hunk's agent-context v1 sidecar
//     {"version":1,"summary":"…","files":[{"path":"…","summary":"…",
//     "annotations":[{"newRange":[a,b],"summary":"…","rationale":"…"}]}]}
//  2. hunk's `comment apply` batch
//     {"comments":[{"filePath":"…","newLine":12,"summary":"…"}]}
//
// It is a DAG leaf: pure validation over bytes, standard library only, no git
// and no store. The whole batch is validated before the caller writes anything,
// so a single bad item stores nothing. Anchor RESOLUTION (a hunk number → a
// line range, a path → an address) belongs to the caller and to domain; this
// package only says what the JSON meant.
package notebatch

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Target is how one item anchors. Exactly one of Hunk / NewLine / OldLine is
// set on a root item; a reply carries none (it inherits its parent's anchor).
type Target struct {
	Hunk    int
	NewLine [2]int
	OldLine [2]int
}

// IsSet reports whether any anchor field carries a value.
func (t Target) IsSet() bool {
	return t.Hunk != 0 || t.NewLine != [2]int{0, 0} || t.OldLine != [2]int{0, 0}
}

// Item is one validated note to create. Path is the repo-relative path exactly
// as the agent wrote it (the caller normalises notation); ReplyTo, when set,
// makes this a reply and leaves Path and Target empty.
type Item struct {
	Path       string
	ReplyTo    string
	Target     Target
	Summary    string
	Rationale  string
	Author     string
	Tags       []string
	Confidence float64
}

// Batch is a parsed input: the notes to create plus the unanchored prose
// (the top-level and per-file summaries of agent-context v1), which gg has
// nowhere to hang and therefore echoes to stderr instead of storing.
type Batch struct {
	Items    []Item
	Contexts []string
}

// confidenceWords maps hunk's three-value confidence enum onto model.Note's
// float64 field. Anything else is dropped (0 = unset), as hunk does.
var confidenceWords = map[string]float64{"low": 0.3, "medium": 0.6, "high": 0.9}

type rawTop struct {
	Version  *int              `json:"version"`
	Summary  *string           `json:"summary"`
	Files    []json.RawMessage `json:"files"`
	Comments []json.RawMessage `json:"comments"`
}

type rawFile struct {
	Path        string            `json:"path"`
	Summary     *string           `json:"summary"`
	Annotations []json.RawMessage `json:"annotations"`
}

type rawAnnotation struct {
	Summary    *string `json:"summary"`
	Rationale  *string `json:"rationale"`
	Author     *string `json:"author"`
	Tags       []any   `json:"tags"`
	Confidence *string `json:"confidence"`
	OldRange   []any   `json:"oldRange"`
	NewRange   []any   `json:"newRange"`
}

type rawComment struct {
	FilePath   string  `json:"filePath"`
	ReplyTo    string  `json:"replyTo"`
	NewLine    *int    `json:"newLine"`
	OldLine    *int    `json:"oldLine"`
	Hunk       *int    `json:"hunk"`
	HunkNumber *int    `json:"hunkNumber"`
	Summary    *string `json:"summary"`
	Rationale  *string `json:"rationale"`
	Author     *string `json:"author"`
}

// Parse reads either supported shape, chosen by the top-level key, and returns
// the validated batch. Every error names the offending item's index.
func Parse(data []byte) (Batch, error) {
	var top rawTop
	if err := json.Unmarshal(data, &top); err != nil {
		return Batch{}, fmt.Errorf("invalid JSON batch: %v", err)
	}
	hasFiles, hasComments := top.Files != nil, top.Comments != nil
	if hasFiles == hasComments {
		return Batch{}, fmt.Errorf(`a batch needs exactly one of a top-level "files" array (agent-context v1) or "comments" array (comment apply)`)
	}
	if hasFiles {
		return parseAgentContext(top)
	}
	return parseComments(top)
}

func parseAgentContext(top rawTop) (Batch, error) {
	if top.Version != nil && *top.Version != 0 && *top.Version != 1 {
		return Batch{}, fmt.Errorf("unsupported agent-context version %d (gg reads version 1)", *top.Version)
	}
	var b Batch
	if s := trimPtr(top.Summary); s != "" {
		b.Contexts = append(b.Contexts, s)
	}
	for fi, rawF := range top.Files {
		var f rawFile
		if err := json.Unmarshal(rawF, &f); err != nil {
			return Batch{}, fmt.Errorf("files[%d]: %v", fi, err)
		}
		if strings.TrimSpace(f.Path) == "" {
			return Batch{}, fmt.Errorf("files[%d]: a file entry requires a non-empty path", fi)
		}
		if s := trimPtr(f.Summary); s != "" {
			b.Contexts = append(b.Contexts, f.Path+": "+s)
		}
		for ai, rawA := range f.Annotations {
			where := fmt.Sprintf("files[%d].annotations[%d]", fi, ai)
			var a rawAnnotation
			if err := json.Unmarshal(rawA, &a); err != nil {
				return Batch{}, fmt.Errorf("%s: %v", where, err)
			}
			summary := trimPtr(a.Summary)
			if summary == "" {
				return Batch{}, fmt.Errorf("%s: each annotation requires a summary", where)
			}
			oldR, err := parseRange(a.OldRange, where+".oldRange")
			if err != nil {
				return Batch{}, err
			}
			newR, err := parseRange(a.NewRange, where+".newRange")
			if err != nil {
				return Batch{}, err
			}
			if oldR == ([2]int{0, 0}) && newR == ([2]int{0, 0}) {
				return Batch{}, fmt.Errorf("%s: an annotation needs an oldRange or newRange", where)
			}
			if newR != ([2]int{0, 0}) {
				oldR = [2]int{0, 0} // newRange wins when both are present
			}
			it := Item{
				Path:      f.Path,
				Target:    Target{NewLine: newR, OldLine: oldR},
				Summary:   summary,
				Rationale: trimPtr(a.Rationale),
				Author:    trimPtr(a.Author),
			}
			for _, tag := range a.Tags {
				if s, ok := tag.(string); ok && strings.TrimSpace(s) != "" {
					it.Tags = append(it.Tags, s)
				}
			}
			if a.Confidence != nil {
				it.Confidence = confidenceWords[strings.ToLower(strings.TrimSpace(*a.Confidence))]
			}
			b.Items = append(b.Items, it)
		}
	}
	return b, nil
}

func parseComments(top rawTop) (Batch, error) {
	var b Batch
	for ci, rawC := range top.Comments {
		where := fmt.Sprintf("comments[%d]", ci)
		var c rawComment
		if err := json.Unmarshal(rawC, &c); err != nil {
			return Batch{}, fmt.Errorf("%s: %v", where, err)
		}
		summary := trimPtr(c.Summary)
		if summary == "" {
			return Batch{}, fmt.Errorf("%s: each comment requires a summary", where)
		}
		it := Item{
			Summary:   summary,
			Rationale: trimPtr(c.Rationale),
			Author:    trimPtr(c.Author),
			ReplyTo:   strings.TrimSpace(c.ReplyTo),
		}
		if it.ReplyTo != "" {
			// A reply inherits its parent's anchor: naming a target too is a
			// contradiction, not a refinement.
			if c.FilePath != "" || c.NewLine != nil || c.OldLine != nil || c.Hunk != nil || c.HunkNumber != nil {
				return Batch{}, fmt.Errorf("%s: replyTo takes no filePath or target", where)
			}
			b.Items = append(b.Items, it)
			continue
		}
		if strings.TrimSpace(c.FilePath) == "" {
			return Batch{}, fmt.Errorf("%s: a root comment requires filePath", where)
		}
		hunk := c.Hunk
		if hunk == nil {
			hunk = c.HunkNumber
		} else if c.HunkNumber != nil && *c.HunkNumber != *c.Hunk {
			return Batch{}, fmt.Errorf("%s: hunk and hunkNumber disagree", where)
		}
		set := 0
		var target Target
		if c.NewLine != nil {
			set++
			if *c.NewLine < 1 {
				return Batch{}, fmt.Errorf("%s: newLine must be a 1-based line number", where)
			}
			target.NewLine = [2]int{*c.NewLine, *c.NewLine}
		}
		if c.OldLine != nil {
			set++
			if *c.OldLine < 1 {
				return Batch{}, fmt.Errorf("%s: oldLine must be a 1-based line number", where)
			}
			target.OldLine = [2]int{*c.OldLine, *c.OldLine}
		}
		if hunk != nil {
			set++
			if *hunk < 1 {
				return Batch{}, fmt.Errorf("%s: hunk must be a 1-based hunk number", where)
			}
			target.Hunk = *hunk
		}
		if set != 1 {
			return Batch{}, fmt.Errorf("%s: pass exactly one of hunk, hunkNumber, newLine or oldLine", where)
		}
		it.Path, it.Target = c.FilePath, target
		b.Items = append(b.Items, it)
	}
	return b, nil
}

// parseRange validates a [start,end] tuple: two integers, both >= 1, ordered.
// A missing tuple is [0,0] (unset), never an error.
func parseRange(v []any, where string) ([2]int, error) {
	if v == nil {
		return [2]int{0, 0}, nil
	}
	if len(v) != 2 {
		return [2]int{0, 0}, fmt.Errorf("%s: a range is a [start,end] pair", where)
	}
	var out [2]int
	for i, raw := range v {
		f, ok := raw.(float64)
		if !ok || f != float64(int(f)) {
			return [2]int{0, 0}, fmt.Errorf("%s: ranges must be integer tuples", where)
		}
		out[i] = int(f)
	}
	if out[0] < 1 || out[1] < 1 {
		return [2]int{0, 0}, fmt.Errorf("%s: ranges must use positive 1-based line numbers", where)
	}
	if out[1] < out[0] {
		return [2]int{0, 0}, fmt.Errorf("%s: ranges must be ordered start..end tuples", where)
	}
	return out, nil
}

func trimPtr(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}
