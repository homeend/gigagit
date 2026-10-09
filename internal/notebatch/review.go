package notebatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// The structured review document: agent-context v1 with a markdown top-level
// summary (the review's own text), an optional stored overview (markdown
// whose links are anchors into the reviewed change) and a free-form "meta"
// object at every level in place of fixed fields, so what a review says
// about itself (a verdict, a severity, a confidence) can grow without a
// format change:
//
//	{"version":1,"summary":"<markdown>","overview":"<markdown, optional>","meta":{…},
//	 "files":[{"path":"…","summary":"…","meta":{…},
//	   "annotations":[{"newRange":[a,b]|"oldRange":[a,b],
//	                   "summary":"…","rationale":"…","meta":{…}}]}]}
//
// The older per-annotation "tags" and "confidence" fields still parse; they
// fold into meta.

// ErrNotReviewDoc wraps every reason a reply is not a review document. The
// caller keeps such a reply as text: a reply that is not the document is still
// the agent's review, just not a structured one.
var ErrNotReviewDoc = errors.New("notebatch: not a gg review document")

// MetaKV is one meta entry, the value rendered as text (a string as is, any
// other JSON value as its compact JSON).
type MetaKV struct{ Key, Value string }

// ReviewNote is one annotation. Side is "new" or "old"; Range is 1-based and
// inclusive on that side.
type ReviewNote struct {
	Side               string
	Range              [2]int
	Summary, Rationale string
	Meta               []MetaKV
}

// ReviewFile is the review of one path: its one-line summary and its notes.
type ReviewFile struct {
	Path, Summary string
	Meta          []MetaKV
	Notes         []ReviewNote
}

// MaxOverviewBytes caps a review's stored overview (spec §2.1). It equals
// agentdocs.MaxOverviewBytes — pinned by a domain test, since this package
// must stay stdlib-only.
const MaxOverviewBytes = 64 << 10

// ReviewDoc is a parsed review document. Summary is the review's own
// markdown text (what a forge gets as the review body); Overview is the
// optional stored walk — markdown whose links are anchors into the reviewed
// change (spec §2), local only.
type ReviewDoc struct {
	Summary  string
	Overview string
	Meta     []MetaKV
	Files    []ReviewFile
}

type rawReview struct {
	Version  *int            `json:"version"`
	Summary  string          `json:"summary"`
	Overview string          `json:"overview"`
	Meta     json.RawMessage `json:"meta"`
	Files    []struct {
		Path        string          `json:"path"`
		Summary     string          `json:"summary"`
		Meta        json.RawMessage `json:"meta"`
		Annotations []struct {
			NewRange   []int           `json:"newRange"`
			OldRange   []int           `json:"oldRange"`
			Summary    string          `json:"summary"`
			Rationale  string          `json:"rationale"`
			Meta       json.RawMessage `json:"meta"`
			Tags       []string        `json:"tags"`
			Confidence string          `json:"confidence"`
		} `json:"annotations"`
	} `json:"files"`
}

func notDoc(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrNotReviewDoc, fmt.Sprintf(format, args...))
}

// ParseReview reads an agent's reply as a review document. The document may
// arrive fenced in a ```json block, or as the "result" string of Claude's
// `--output-format json` envelope; both are unwrapped. Any other reply, or a
// document that breaks the shape, fails with an error wrapping ErrNotReviewDoc.
func ParseReview(data []byte) (ReviewDoc, error) {
	var raw rawReview
	dec := json.NewDecoder(bytes.NewReader(unwrap(data)))
	if err := dec.Decode(&raw); err != nil {
		return ReviewDoc{}, notDoc("%v", err)
	}
	if raw.Version == nil || *raw.Version != 1 {
		return ReviewDoc{}, notDoc("version must be 1")
	}
	if strings.TrimSpace(raw.Summary) == "" {
		return ReviewDoc{}, notDoc("summary is empty")
	}
	if len(raw.Overview) > MaxOverviewBytes {
		return ReviewDoc{}, notDoc("overview exceeds 64 KiB (%d bytes)", len(raw.Overview))
	}
	if strings.TrimSpace(raw.Overview) == "" {
		raw.Overview = "" // absent or blank = none (spec §2.1)
	}
	doc := ReviewDoc{Summary: raw.Summary, Overview: raw.Overview}
	var err error
	if doc.Meta, err = metaKVs(raw.Meta); err != nil {
		return ReviewDoc{}, err
	}
	for i, f := range raw.Files {
		if strings.TrimSpace(f.Path) == "" {
			return ReviewDoc{}, notDoc("files[%d]: path is empty", i)
		}
		rf := ReviewFile{Path: f.Path, Summary: f.Summary}
		if rf.Meta, err = metaKVs(f.Meta); err != nil {
			return ReviewDoc{}, err
		}
		for j, a := range f.Annotations {
			where := fmt.Sprintf("files[%d].annotations[%d]", i, j)
			n := ReviewNote{Summary: a.Summary, Rationale: a.Rationale}
			switch {
			case a.NewRange != nil && a.OldRange != nil:
				return ReviewDoc{}, notDoc("%s: both newRange and oldRange", where)
			case a.NewRange != nil:
				n.Side = "new"
				if n.Range, err = docRange(a.NewRange, where); err != nil {
					return ReviewDoc{}, err
				}
			case a.OldRange != nil:
				n.Side = "old"
				if n.Range, err = docRange(a.OldRange, where); err != nil {
					return ReviewDoc{}, err
				}
			default:
				return ReviewDoc{}, notDoc("%s: no newRange or oldRange", where)
			}
			if strings.TrimSpace(a.Summary) == "" {
				return ReviewDoc{}, notDoc("%s: summary is empty", where)
			}
			if n.Meta, err = metaKVs(a.Meta); err != nil {
				return ReviewDoc{}, err
			}
			if len(a.Tags) > 0 {
				n.Meta = foldMeta(n.Meta, "tags", strings.Join(a.Tags, ", "))
			}
			if a.Confidence != "" {
				n.Meta = foldMeta(n.Meta, "confidence", a.Confidence)
			}
			rf.Notes = append(rf.Notes, n)
		}
		doc.Files = append(doc.Files, rf)
	}
	return doc, nil
}

func docRange(r []int, where string) ([2]int, error) {
	if len(r) != 2 || r[0] < 1 || r[1] < r[0] {
		return [2]int{}, notDoc("%s: range %v is not [first,last] with 1 <= first <= last", where, r)
	}
	return [2]int{r[0], r[1]}, nil
}

// foldMeta adds key=value unless meta already names key, keeping key order.
func foldMeta(meta []MetaKV, key, value string) []MetaKV {
	for _, kv := range meta {
		if kv.Key == key {
			return meta
		}
	}
	meta = append(meta, MetaKV{key, value})
	sort.Slice(meta, func(i, j int) bool { return meta[i].Key < meta[j].Key })
	return meta
}

// metaKVs renders a meta object as key-sorted text pairs; absent or null is
// nil, anything but an object is not a document.
func metaKVs(raw json.RawMessage) ([]MetaKV, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, notDoc("meta must be an object")
	}
	var out []MetaKV
	for k, v := range m {
		var s string
		if json.Unmarshal(v, &s) != nil {
			var buf bytes.Buffer
			if json.Compact(&buf, v) == nil {
				s = buf.String()
			} else {
				s = string(v)
			}
		}
		out = append(out, MetaKV{k, s})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// unwrap finds the document inside a reply: Claude's JSON envelope's "result"
// string, else the first ``` fence whose body is an object, else the reply.
func unwrap(data []byte) []byte {
	data = bytes.TrimSpace(data)
	if bytes.HasPrefix(data, []byte("{")) {
		var env struct {
			Version json.RawMessage `json:"version"`
			Result  *string         `json:"result"`
		}
		if json.Unmarshal(data, &env) == nil && env.Result != nil && env.Version == nil {
			return unwrap([]byte(*env.Result))
		}
		return data
	}
	rest := data
	for {
		open := bytes.Index(rest, []byte("```"))
		if open < 0 {
			return data
		}
		rest = rest[open+3:]
		nl := bytes.IndexByte(rest, '\n')
		if nl < 0 {
			return data
		}
		body := rest[nl+1:]
		end := bytes.Index(body, []byte("```"))
		if end < 0 {
			return data
		}
		if b := bytes.TrimSpace(body[:end]); bytes.HasPrefix(b, []byte("{")) {
			return b
		}
		rest = body[end+3:]
	}
}

type canonMeta map[string]string

type canonNote struct {
	NewRange  *[2]int   `json:"newRange,omitempty"`
	OldRange  *[2]int   `json:"oldRange,omitempty"`
	Summary   string    `json:"summary"`
	Rationale string    `json:"rationale,omitempty"`
	Meta      canonMeta `json:"meta,omitempty"`
}

type canonFile struct {
	Path        string      `json:"path"`
	Summary     string      `json:"summary,omitempty"`
	Meta        canonMeta   `json:"meta,omitempty"`
	Annotations []canonNote `json:"annotations,omitempty"`
}

type canonDoc struct {
	Version  int         `json:"version"`
	Summary  string      `json:"summary"`
	Overview string      `json:"overview,omitempty"`
	Meta     canonMeta   `json:"meta,omitempty"`
	Files    []canonFile `json:"files,omitempty"`
}

func toCanonMeta(kvs []MetaKV) canonMeta {
	if len(kvs) == 0 {
		return nil
	}
	m := canonMeta{}
	for _, kv := range kvs {
		m[kv.Key] = kv.Value
	}
	return m
}

// Canonical is the document in its documented shape, indented: what gg stores,
// so a stored review reads the same whatever wrapping the agent used.
func (d ReviewDoc) Canonical() []byte {
	c := canonDoc{Version: 1, Summary: d.Summary, Overview: d.Overview, Meta: toCanonMeta(d.Meta)}
	for _, f := range d.Files {
		cf := canonFile{Path: f.Path, Summary: f.Summary, Meta: toCanonMeta(f.Meta)}
		for _, n := range f.Notes {
			r := n.Range
			cn := canonNote{Summary: n.Summary, Rationale: n.Rationale, Meta: toCanonMeta(n.Meta)}
			if n.Side == "old" {
				cn.OldRange = &r
			} else {
				cn.NewRange = &r
			}
			cf.Annotations = append(cf.Annotations, cn)
		}
		c.Files = append(c.Files, cf)
	}
	out, _ := json.MarshalIndent(c, "", "  ")
	return out
}

// NoteCount is how many notes the review holds and on how many files.
func (d ReviewDoc) NoteCount() (notes, files int) {
	for _, f := range d.Files {
		if len(f.Notes) > 0 {
			files++
			notes += len(f.Notes)
		}
	}
	return notes, files
}
