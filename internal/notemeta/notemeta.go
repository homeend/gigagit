// Package notemeta is a note's free meta and the author's declaration of how
// it is shown (spec 2026-10-12-note-meta). gg owns no meta vocabulary: keys
// and values are the author's. What gg owns is the display mechanism — the
// places, colours, glyphs, styles — and two semantics, level and audience,
// that consumers read instead of key names. Meta never triggers anything.
//
// A DAG leaf: standard library only. model, notebatch, agentdocs, domain and
// the frontends import it.
package notemeta

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// KV is one meta entry.
type KV struct {
	Key   string `toml:"key" json:"key"`
	Value string `toml:"value" json:"value"`
}

// Added is meta written onto a note after its author wrote it — the user's
// Mark, another agent's double-check. RemarkFP is set on a review note's
// entry for one of its remarks.
type Added struct {
	RemarkFP string    `toml:"remark_fp,omitempty" json:"remark_fp,omitempty"`
	Key      string    `toml:"key" json:"key"`
	Value    string    `toml:"value" json:"value"`
	By       string    `toml:"by,omitempty" json:"by,omitempty"`
	At       time.Time `toml:"at" json:"at"`
}

// Problem names one dropped part of a meta list or a declaration, as an
// English path + reason ("display.kind.values.issue.color: \"crimson\" is
// not a colour").
type Problem string

// Places, styles and the limits (spec §2.6, §3.2, §3.3).
const (
	PlaceBadge = "badge"
	PlaceFrame = "frame"
	PlaceCount = "count"
	PlaceLine  = "line"

	StyleFilled = "filled"
	StyleText   = "text"

	MaxMetaKeys      = 32
	MaxKeyBytes      = 64
	MaxValueBytes    = 256
	MaxDisplayKeys   = 8
	MaxDisplayValues = 16
)

// Colors, Levels and Audiences are the fixed vocabularies, in display order.
var (
	Colors    = []string{"red", "orange", "yellow", "green", "blue", "purple", "grey"}
	Levels    = []string{"high", "medium", "low", "info"}
	Audiences = []string{"author", "reviewer"}
	places    = []string{PlaceBadge, PlaceFrame, PlaceCount, PlaceLine}
	semantics = []string{"level", "audience"}
)

func oneOf(v string, set []string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// CheckMeta keeps the valid entries in order and names the rest: an empty,
// multi-line or over-long key or value, a repeated key (the first wins),
// anything past MaxMetaKeys.
func CheckMeta(kv []KV) ([]KV, []Problem) {
	var out []KV
	var probs []Problem
	seen := map[string]bool{}
	for _, e := range kv {
		switch {
		case e.Key == "":
			probs = append(probs, "meta: an empty key")
		case strings.ContainsAny(e.Key, "\r\n"):
			probs = append(probs, Problem(fmt.Sprintf("meta %q: a key is one line", e.Key)))
		case len(e.Key) > MaxKeyBytes:
			probs = append(probs, Problem(fmt.Sprintf("meta %q: a key is at most %d bytes", e.Key[:MaxKeyBytes], MaxKeyBytes)))
		case strings.ContainsAny(e.Value, "\r\n"):
			probs = append(probs, Problem(fmt.Sprintf("meta %s: a value is one line", e.Key)))
		case len(e.Value) > MaxValueBytes:
			probs = append(probs, Problem(fmt.Sprintf("meta %s: a value is at most %d bytes", e.Key, MaxValueBytes)))
		case seen[e.Key]:
			probs = append(probs, Problem(fmt.Sprintf("meta %s: given twice, the first is kept", e.Key)))
		case len(out) == MaxMetaKeys:
			probs = append(probs, Problem(fmt.Sprintf("meta %s: a note holds at most %d keys", e.Key, MaxMetaKeys)))
		default:
			seen[e.Key] = true
			out = append(out, e)
		}
	}
	return out, probs
}

var tagMeta = regexp.MustCompile(`^([A-Za-z0-9_.-]{1,64}): (.+)$`)

// SplitTags separates "key: value" tags (meta written as tags before notes
// format 3) from plain ones ("review", "resolved", "perf").
func SplitTags(tags []string) (plain []string, meta []KV) {
	for _, t := range tags {
		if m := tagMeta.FindStringSubmatch(t); m != nil {
			meta = append(meta, KV{m[1], m[2]})
			continue
		}
		plain = append(plain, t)
	}
	return plain, meta
}

// ConfidenceWord turns the pre-format-3 confidence number back into the word
// it was made from (0.3/0.6/0.9); any other number keeps its decimal text.
func ConfidenceWord(f float64) string {
	switch f {
	case 0.3:
		return "low"
	case 0.6:
		return "medium"
	case 0.9:
		return "high"
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// GlyphOK reports a glyph from the safe set: one printable ASCII character
// (not space) or one of the few symbols gg already draws in one cell.
func GlyphOK(g string) bool {
	if utf8.RuneCountInString(g) != 1 {
		return false
	}
	r, _ := utf8.DecodeRuneInString(g)
	if r >= '!' && r <= '~' {
		return true
	}
	return strings.ContainsRune("◆·→»•", r)
}
