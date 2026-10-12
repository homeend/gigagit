# Note meta — plan 1: core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo forbids implementer subagents — CLAUDE.md). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every gg note can carry free meta key/values, an author-declared display declaration and meta added later by another writer; one pure package resolves them into what to show; every write surface (agent JSON, CLI, MCP, `gg session note`) accepts them.

**Architecture:** A new stdlib-only leaf `internal/notemeta` owns the types, validation, inheritance, resolution and counts. `model.Note` gains `Meta`, `Display`, `Added`; the notes store writes `format = 3` into each part file, converts `key: value` tags and the old confidence number into meta at read time, and refuses to write a newer format. Domain fills remarks from their review document, exposes `SetNoteMeta` / `UnsetNoteMeta` / `SetNoteDisplay`, and puts the resolved display on the JSON wire. No frontend rendering changes beyond keeping today's text (plans 2–4).

**Tech Stack:** Go 1.26, go-toml v2, encoding/json, the existing notes store, MCP go-sdk.

**Spec:** `docs/superpowers/specs/2026-10-12-note-meta-design.md` (§0–§5, §10–§12; R1–R16).

## Global Constraints

- `internal/notemeta` imports the standard library ONLY (archtest pins it).
- `internal/notebatch` stays stdlib + `notemeta`; `internal/agentdocs` may add `notemeta` to its allowed list; `internal/steer` must NOT import `notemeta` (it carries raw JSON).
- Limits: ≤ 32 meta keys per note; key ≤ 64 bytes, value ≤ 256 bytes, both single-line; ≤ 8 declared keys; ≤ 16 values per key.
- Places: `badge`, `frame`, `count`, `line`. Colours: `red`, `orange`, `yellow`, `green`, `blue`, `purple`, `grey`. Styles: `filled` (default), `text`. Semantics: `level` ∈ `high|medium|low|info`, `audience` ∈ `author|reviewer`. `as` ∈ `level|audience`.
- Glyph safe set: one rune, printable ASCII `!`..`~`, or one of `◆ · → » •`.
- Built-in declaration only for key `status` (values `agreed` green, `disagreed` red, `deferred` grey; badge, style text, order 100), used when the effective declaration has no `status` entry.
- Notes file format = 3; a part with `format > 3` reads but never writes (`notes.ErrNewerFormat`).
- Meta never triggers anything (R12): no code path may send, tick, filter, block or resolve because of a meta value.
- Engine/CLI prose stays English; no TUI strings change in this plan except the existing review-title meta suffix, which keeps its shape.
- Tests: real git via existing helpers, `t.Parallel()` on new tests; run package tests per task; the controller runs `./test.sh race` before handing over (check the log ends "all green").
- Commits: `gg add <paths>` then `git commit -F <msgfile>` ending with the Co-Authored-By / Claude-Session lines; never `git add -A`.

## Review Focus

1. **An older-format notes file with tags like `severity: risk` and `confidence = 0.6`** — a reviewer expects it to load with that meta, print the same in `gg note show`, and NOT be rewritten by a read-only command. Pinned in Task 3 (`TestUpgradeIsReadOnly`).
2. **A part written by a newer gg (`format = 4`)** — expect every read to work and every write (add, reply, resolve, meta) to fail with the upgrade message, file untouched. Pinned in Task 3 (`TestNewerFormatRefusesWrites`); the CLI reports it through the generic error path (exit 1, the error text).
3. **`gg note meta review:<id>:<n> set status=disagreed` after the review is re-saved with remarks reordered** — expect the mark to stay on the same remark (fingerprint), not the same index. Pinned in Task 6 (`TestRemarkAddedMetaFollowsFingerprint`).
4. **A display JSON with a typo field (`"colour"`) or an unknown colour** — expect the note to be written, the typo named once on stderr, the bad colour shown as a problem every time. Pinned in Task 1 and Task 7.
5. **A reply without its own declaration under a remark with one** — expect the reply's meta to render with the root's declaration. Pinned in Task 5 (`TestReplyInheritsRootDisplay`).

---

## File Structure

Create:
- `internal/notemeta/notemeta.go` — types, constants, limits, `GlyphOK`, `CheckMeta`, `SplitTags`, `ConfidenceWord`.
- `internal/notemeta/display.go` — `ParseDisplay`, `validate`, `Effective`, built-in `status`.
- `internal/notemeta/resolve.go` — `Resolve`, `Resolved`, `Count`.
- `internal/notemeta/notemeta_test.go`, `display_test.go`, `resolve_test.go`.
- `internal/domain/note_meta.go` — `NoteMeta`, remark helper, `SetNoteMeta` / `UnsetNoteMeta` / `SetNoteDisplay`, `ErrRemarkDisplay`.
- `internal/domain/note_meta_test.go`.
- `internal/cli/note_meta.go` — `--meta` / `--display` flag types, `gg note meta`, text helpers.
- `internal/cli/note_meta_test.go`.

Modify:
- `internal/model/note.go` — fields `LegacyConfidence`, `Meta`, `Display`, `Added`.
- `internal/notes/part_file.go` — `format`, upgrade on read, `ErrNewerFormat`.
- `internal/notebatch/notebatch.go`, `review.go` — meta/display on all shapes; `MetaKV` alias.
- `internal/domain/review_doc.go`, `note_link.go`, `forge_send.go`, `pr_reviews.go`, `review_notes.go`, `notewire.go`, `forge_send_candidates.go`, `forge_send_body.go`, `notebatch_plan.go`, `notes.go`.
- `internal/cli/note.go`, `note_show.go`.
- `internal/mcp/notes.go`.
- `internal/agentdocs/notes.go`, `internal/steer/steer.go`, `internal/cli/session_note.go`, `internal/tui/steer_file_notes.go`, `internal/web/steer_notes.go`.
- `internal/tui/diff_notes.go` (one line: the remark title suffix reads meta).
- `internal/archtest/import_guard_test.go`.
- Docs: `CHANGELOG.md`, `CLAUDE.md`, `docs/CLAUDE-details.md`, `internal/agentskill/using-gg.md` (+ version bump).

---

### Task 1: `notemeta` types, limits, display parsing

**Files:**
- Create: `internal/notemeta/notemeta.go`, `internal/notemeta/display.go`
- Test: `internal/notemeta/notemeta_test.go`, `internal/notemeta/display_test.go`
- Modify: `internal/archtest/import_guard_test.go`

**Interfaces:**
- Produces:
  - `type KV struct{ Key, Value string }` (tags `toml:"key" json:"key"`, `toml:"value" json:"value"`)
  - `type Added struct{ RemarkFP, Key, Value, By string; At time.Time }`
  - `type Display map[string]Key`; `type Key struct{ Show []string; Label string; Order int; Style, As string; Values map[string]Value; Other *Value }`; `type Value struct{ Color, Glyph, Level, Audience string }`
  - `type Problem string`
  - `func CheckMeta(kv []KV) ([]KV, []Problem)`
  - `func SplitTags(tags []string) (plain []string, meta []KV)`
  - `func ConfidenceWord(f float64) string`
  - `func GlyphOK(g string) bool`
  - `func ParseDisplay(raw []byte) (Display, []Problem, error)`
  - `func validate(d Display) (Display, []Problem)` (package-private, used by Task 2)
  - `func Effective(levels ...Display) Display` (farthest first, nearest last)
  - constants `PlaceBadge`, `PlaceFrame`, `PlaceCount`, `PlaceLine`, `StyleFilled`, `StyleText`, `MaxMetaKeys = 32`, `MaxKeyBytes = 64`, `MaxValueBytes = 256`, `MaxDisplayKeys = 8`, `MaxDisplayValues = 16`; `var Colors`, `Levels`, `Audiences` (ordered slices).

- [ ] **Step 1: Write the failing tests**

`internal/notemeta/notemeta_test.go`:

```go
package notemeta

import (
	"reflect"
	"strings"
	"testing"
)

func TestCheckMetaKeepsValidDropsTheRest(t *testing.T) {
	t.Parallel()
	in := []KV{
		{"kind", "issue"},
		{"kind", "note"},                        // duplicate: first wins
		{"", "x"},                               // empty key
		{"two\nlines", "x"},                     // multi-line key
		{"intent", "a\nb"},                      // multi-line value
		{strings.Repeat("k", 65), "x"},          // key too long
		{"long", strings.Repeat("v", 257)},      // value too long
		{"severity", "high"},
	}
	got, probs := CheckMeta(in)
	want := []KV{{"kind", "issue"}, {"severity", "high"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kept = %v, want %v", got, want)
	}
	if len(probs) != 6 {
		t.Fatalf("problems = %v, want 6", probs)
	}
}

func TestCheckMetaCapsAt32Keys(t *testing.T) {
	t.Parallel()
	var in []KV
	for i := 0; i < 40; i++ {
		in = append(in, KV{Key: "k" + string(rune('A'+i)), Value: "v"})
	}
	got, probs := CheckMeta(in)
	if len(got) != MaxMetaKeys || len(probs) != 8 {
		t.Fatalf("kept %d, problems %d; want 32, 8", len(got), len(probs))
	}
}

func TestSplitTags(t *testing.T) {
	t.Parallel()
	plain, meta := SplitTags([]string{"review", "severity: risk", "perf", "ruling: confirmed (as nit)", "a b: c", "resolved"})
	if !reflect.DeepEqual(plain, []string{"review", "perf", "a b: c", "resolved"}) {
		t.Fatalf("plain = %v", plain)
	}
	if !reflect.DeepEqual(meta, []KV{{"severity", "risk"}, {"ruling", "confirmed (as nit)"}}) {
		t.Fatalf("meta = %v", meta)
	}
}

func TestConfidenceWord(t *testing.T) {
	t.Parallel()
	for f, w := range map[float64]string{0.3: "low", 0.6: "medium", 0.9: "high", 0.5: "0.5"} {
		if got := ConfidenceWord(f); got != w {
			t.Errorf("ConfidenceWord(%v) = %q, want %q", f, got, w)
		}
	}
}

func TestGlyphOK(t *testing.T) {
	t.Parallel()
	for _, g := range []string{"!", "~", "A", "◆", "·", "→", "»", "•"} {
		if !GlyphOK(g) {
			t.Errorf("GlyphOK(%q) = false", g)
		}
	}
	for _, g := range []string{"", " ", "✓", "✗", "ab", "🔥", "\t"} {
		if GlyphOK(g) {
			t.Errorf("GlyphOK(%q) = true", g)
		}
	}
}
```

`internal/notemeta/display_test.go`:

```go
package notemeta

import (
	"strings"
	"testing"
)

func TestParseDisplayEmptyIsNone(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "  ", "null"} {
		d, probs, err := ParseDisplay([]byte(raw))
		if err != nil || d != nil || probs != nil {
			t.Fatalf("ParseDisplay(%q) = %v, %v, %v", raw, d, probs, err)
		}
	}
}

func TestParseDisplayMalformedIsAnError(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`[]`, `{"kind": 3}`, `{"kind": {"show": "badge"}}`, `{`} {
		if _, _, err := ParseDisplay([]byte(raw)); err == nil {
			t.Errorf("ParseDisplay(%s) = nil error", raw)
		}
	}
}

func TestParseDisplayUnknownFieldIsAProblemNotAnError(t *testing.T) {
	t.Parallel()
	d, probs, err := ParseDisplay([]byte(`{"kind": {"show": ["badge"], "values": {"issue": {"colour": "red"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(probs) != 1 || !strings.Contains(string(probs[0]), "colour") {
		t.Fatalf("problems = %v", probs)
	}
	if _, ok := d["kind"]; !ok {
		t.Fatalf("display lost its key: %v", d)
	}
}

func TestValidateDropsBadEntries(t *testing.T) {
	t.Parallel()
	d := Display{
		"kind": {Show: []string{"badge", "sidebar"}, Style: "bold", As: "colour",
			Values: map[string]Value{
				"issue": {Color: "crimson", Glyph: "✓", Level: "urgent", Audience: "everyone"},
				"good":  {Color: "green", Glyph: "+", Level: "info", Audience: "reviewer"},
			}},
	}
	got, probs := validate(d)
	k := got["kind"]
	if len(k.Show) != 1 || k.Show[0] != "badge" || k.Style != "" || k.As != "" {
		t.Fatalf("key = %+v", k)
	}
	if v := k.Values["issue"]; v != (Value{}) {
		t.Fatalf("issue kept bad fields: %+v", v)
	}
	if v := k.Values["good"]; v != (Value{Color: "green", Glyph: "+", Level: "info", Audience: "reviewer"}) {
		t.Fatalf("good = %+v", v)
	}
	// sidebar, bold, colour(as), crimson, ✓, urgent, everyone
	if len(probs) != 7 {
		t.Fatalf("problems = %d %v", len(probs), probs)
	}
}

func TestValidateCapsKeysAndValues(t *testing.T) {
	t.Parallel()
	d := Display{}
	for i := 0; i < 10; i++ {
		d[string(rune('a'+i))] = Key{Order: i, Show: []string{"line"}}
	}
	vals := map[string]Value{}
	for i := 0; i < 20; i++ {
		vals[string(rune('A'+i))] = Value{Color: "red"}
	}
	d["a"] = Key{Order: 0, Show: []string{"badge"}, Values: vals}
	got, probs := validate(d)
	if len(got) != MaxDisplayKeys {
		t.Fatalf("kept %d keys", len(got))
	}
	if _, ok := got["i"]; ok {
		t.Fatalf("kept key i past the cap (order 8)")
	}
	if len(got["a"].Values) != MaxDisplayValues {
		t.Fatalf("kept %d values", len(got["a"].Values))
	}
	if len(probs) != 2+4 {
		t.Fatalf("problems = %d %v", len(probs), probs)
	}
}

func TestEffectiveNearestReplacesWholeKey(t *testing.T) {
	t.Parallel()
	doc := Display{"kind": {Show: []string{"badge"}, Label: "kind"}, "intent": {Show: []string{"line"}}}
	file := Display{"kind": {Show: []string{"line"}}}
	got := Effective(doc, file, nil)
	if k := got["kind"]; len(k.Show) != 1 || k.Show[0] != "line" || k.Label != "" {
		t.Fatalf("kind = %+v (the nearer entry must replace, not merge)", k)
	}
	if _, ok := got["intent"]; !ok {
		t.Fatal("intent lost")
	}
	if Effective(nil, nil) != nil {
		t.Fatal("no levels must stay nil")
	}
}
```

Add to `internal/archtest/import_guard_test.go` — in `TestAgentdocsIsALeaf`'s `ok` map add `"github.com/homeend/gigagit/internal/notemeta": true` and change its message to "only markdown, notemeta, steer and textdiff are allowed"; and add a new test after it:

```go
// TestNotemetaIsAStdlibLeaf pins internal/notemeta: notebatch (stdlib-only)
// and agentdocs (a leaf) both import it, so it may import no gg package.
func TestNotemetaIsAStdlibLeaf(t *testing.T) {
	t.Parallel()
	for _, imp := range directImports(t, "github.com/homeend/gigagit/internal/notemeta") {
		if strings.HasPrefix(imp, "github.com/homeend/gigagit/") {
			t.Errorf("internal/notemeta imports %s — it must stay stdlib-only", imp)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/notemeta/ ./internal/archtest/`
Expected: FAIL — package `notemeta` has no non-test Go files / undefined names.

- [ ] **Step 3: Write `internal/notemeta/notemeta.go`**

```go
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
```

- [ ] **Step 4: Write `internal/notemeta/display.go`**

```go
package notemeta

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Display is an author's declaration: per meta key, how it is shown.
type Display map[string]Key

// Key declares one meta key. Show lists its places; Values (and Other, for a
// value not listed) give each value's presentation and semantics; As maps
// the key's VALUE straight onto a semantic ("level" or "audience").
type Key struct {
	Show   []string         `toml:"show,omitempty" json:"show,omitempty"`
	Label  string           `toml:"label,omitempty" json:"label,omitempty"`
	Order  int              `toml:"order,omitempty" json:"order,omitempty"`
	Style  string           `toml:"style,omitempty" json:"style,omitempty"`
	As     string           `toml:"as,omitempty" json:"as,omitempty"`
	Values map[string]Value `toml:"values,omitempty" json:"values,omitempty"`
	Other  *Value           `toml:"other,omitempty" json:"other,omitempty"`
}

// Value is one value's presentation (Color, Glyph) and semantics (Level,
// Audience).
type Value struct {
	Color    string `toml:"color,omitempty" json:"color,omitempty"`
	Glyph    string `toml:"glyph,omitempty" json:"glyph,omitempty"`
	Level    string `toml:"level,omitempty" json:"level,omitempty"`
	Audience string `toml:"audience,omitempty" json:"audience,omitempty"`
}

// ParseDisplay reads a declaration. Absent, blank or null is none (nil). JSON
// that is not a declaration's shape is an error — the command that carried
// it fails. An unknown field is not: it is named as a problem and the rest is
// kept (only here — once decoded, the field is gone).
func ParseDisplay(raw []byte) (Display, []Problem, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil, nil
	}
	var d Display
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	err := dec.Decode(&d)
	if err == nil {
		return d, nil, nil
	}
	if !strings.Contains(err.Error(), "unknown field") {
		return nil, nil, fmt.Errorf("display: %w", err)
	}
	d = nil
	if lerr := json.Unmarshal(raw, &d); lerr != nil {
		return nil, nil, fmt.Errorf("display: %w", lerr)
	}
	return d, []Problem{Problem("display: " + strings.TrimPrefix(err.Error(), "json: "))}, nil
}

// sortedKeys is d's keys by Order, then name: the one display order.
func sortedKeys(d Display) []string {
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := d[keys[i]], d[keys[j]]
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return keys[i] < keys[j]
	})
	return keys
}

// validate drops what gg cannot show and names it: an unknown place, style,
// `as`, colour, level or audience; a glyph outside the safe set; keys past
// MaxDisplayKeys (in display order) and values past MaxDisplayValues (by
// name). The result shares nothing with d.
func validate(d Display) (Display, []Problem) {
	if d == nil {
		return nil, nil
	}
	out := Display{}
	var probs []Problem
	for i, k := range sortedKeys(d) {
		if i >= MaxDisplayKeys {
			probs = append(probs, Problem(fmt.Sprintf("display.%s: a declaration holds at most %d keys", k, MaxDisplayKeys)))
			continue
		}
		in := d[k]
		at := "display." + k
		key := Key{Label: in.Label, Order: in.Order}
		for _, p := range in.Show {
			if oneOf(p, places) {
				key.Show = append(key.Show, p)
				continue
			}
			probs = append(probs, Problem(fmt.Sprintf("%s.show: %q is not a place (badge, frame, count, line)", at, p)))
		}
		switch {
		case in.Style == "" || in.Style == StyleFilled || in.Style == StyleText:
			key.Style = in.Style
		default:
			probs = append(probs, Problem(fmt.Sprintf("%s.style: %q is not a style (filled, text)", at, in.Style)))
		}
		switch {
		case in.As == "" || oneOf(in.As, semantics):
			key.As = in.As
		default:
			probs = append(probs, Problem(fmt.Sprintf("%s.as: %q is not a semantic (level, audience)", at, in.As)))
		}
		names := make([]string, 0, len(in.Values))
		for v := range in.Values {
			names = append(names, v)
		}
		sort.Strings(names)
		for j, v := range names {
			if j >= MaxDisplayValues {
				probs = append(probs, Problem(fmt.Sprintf("%s.values.%s: a key declares at most %d values", at, v, MaxDisplayValues)))
				continue
			}
			if key.Values == nil {
				key.Values = map[string]Value{}
			}
			cv, ps := checkValue(in.Values[v], at+".values."+v)
			key.Values[v] = cv
			probs = append(probs, ps...)
		}
		if in.Other != nil {
			cv, ps := checkValue(*in.Other, at+".other")
			key.Other = &cv
			probs = append(probs, ps...)
		}
		out[k] = key
	}
	return out, probs
}

func checkValue(in Value, at string) (Value, []Problem) {
	var out Value
	var probs []Problem
	if in.Color != "" {
		if oneOf(in.Color, Colors) {
			out.Color = in.Color
		} else {
			probs = append(probs, Problem(fmt.Sprintf("%s.color: %q is not a colour (%s)", at, in.Color, strings.Join(Colors, ", "))))
		}
	}
	if in.Glyph != "" {
		if GlyphOK(in.Glyph) {
			out.Glyph = in.Glyph
		} else {
			probs = append(probs, Problem(fmt.Sprintf("%s.glyph: %q is not one safe character", at, in.Glyph)))
		}
	}
	if in.Level != "" {
		if oneOf(in.Level, Levels) {
			out.Level = in.Level
		} else {
			probs = append(probs, Problem(fmt.Sprintf("%s.level: %q is not a level (%s)", at, in.Level, strings.Join(Levels, ", "))))
		}
	}
	if in.Audience != "" {
		if oneOf(in.Audience, Audiences) {
			out.Audience = in.Audience
		} else {
			probs = append(probs, Problem(fmt.Sprintf("%s.audience: %q is not an audience (author, reviewer)", at, in.Audience)))
		}
	}
	return out, probs
}

// Effective builds a note's declaration from its levels, farthest first
// (document, file, remark — or root, reply): key by key, the nearest entry
// REPLACES the farther one wholesale. nil when no level declares anything.
func Effective(levels ...Display) Display {
	var out Display
	for _, l := range levels {
		for k, v := range l {
			if out == nil {
				out = Display{}
			}
			out[k] = v
		}
	}
	return out
}

// StatusKey is the one key gg declares itself: the Mark use case's tag
// (agreed / disagreed / deferred), used when the effective declaration has
// no entry for it.
const StatusKey = "status"

// StatusDisplay is that built-in declaration.
var StatusDisplay = Key{Show: []string{PlaceBadge}, Order: 100, Style: StyleText,
	Values: map[string]Value{"agreed": {Color: "green"}, "disagreed": {Color: "red"}, "deferred": {Color: "grey"}}}
```

- [ ] **Step 5: Run the tests**

Run: `gofmt -l internal/notemeta; go vet ./internal/notemeta/ && go test ./internal/notemeta/ ./internal/archtest/`
Expected: PASS, no gofmt output.

- [ ] **Step 6: Commit**

```bash
gg add internal/notemeta internal/archtest/import_guard_test.go
git commit -F <msgfile>   # "notemeta: meta types, limits and the display declaration"
```

---

### Task 2: `notemeta.Resolve` and `Count`

**Files:**
- Create: `internal/notemeta/resolve.go`
- Test: `internal/notemeta/resolve_test.go`

**Interfaces:**
- Consumes: Task 1 (`KV`, `Added`, `Display`, `Key`, `Value`, `validate`, `sortedKeys`, `StatusDisplay`).
- Produces:
  - `type Badge struct{ Key, Value, Color, Glyph, Style string }` (json `key,value,color,glyph,style`)
  - `type Line struct{ Label, Value string }`
  - `type Change struct{ Key, From, To, By string }`
  - `type Counted struct{ Key, Label, Value, Color string }`
  - `type Resolved struct{ Badges []Badge; Frame string; Lines []Line; Counted []Counted; Level, Audience string; Stored []KV; Added []Change; Problems []Problem }` (json snake case, omitempty)
  - `func Resolve(meta []KV, d Display, added []Added) Resolved`
  - `func (r Resolved) Empty() bool`
  - `type CountValue struct{ Value, Color string; N int }`, `type CountGroup struct{ Key, Label string; Values []CountValue }`, `type Counts struct{ Groups []CountGroup; Author, Reviewer int }`
  - `func Count(rs []Resolved) Counts`

- [ ] **Step 1: Write the failing tests** — `internal/notemeta/resolve_test.go`:

```go
package notemeta

import (
	"reflect"
	"testing"
	"time"
)

var reviewDisplay = Display{
	"kind": {Show: []string{"badge", "count"}, Order: 1, Values: map[string]Value{
		"issue": {Color: "red", Audience: "author"},
		"good":  {Color: "green", Audience: "reviewer"},
	}},
	"severity": {Show: []string{"badge", "frame"}, Order: 2, Style: "text", Values: map[string]Value{
		"high": {Color: "red", Level: "high"},
		"low":  {Color: "grey", Level: "low"},
	}},
	"audience": {As: "audience", Order: 3},
	"intent":   {Show: []string{"line"}, Order: 4},
}

func TestResolvePlacesAndSemantics(t *testing.T) {
	t.Parallel()
	r := Resolve([]KV{{"intent", "checked the lock"}, {"kind", "issue"}, {"severity", "high"}, {"raised_by", "opus"}}, reviewDisplay, nil)
	wantBadges := []Badge{
		{Key: "kind", Value: "issue", Color: "red", Style: "filled"},
		{Key: "severity", Value: "high", Color: "red", Style: "text"},
	}
	if !reflect.DeepEqual(r.Badges, wantBadges) {
		t.Fatalf("badges = %+v", r.Badges)
	}
	if r.Frame != "red" || r.Level != "high" || r.Audience != "author" {
		t.Fatalf("frame %q level %q audience %q", r.Frame, r.Level, r.Audience)
	}
	if !reflect.DeepEqual(r.Lines, []Line{{"intent", "checked the lock"}}) {
		t.Fatalf("lines = %+v", r.Lines)
	}
	if !reflect.DeepEqual(r.Counted, []Counted{{Key: "kind", Label: "kind", Value: "issue", Color: "red"}}) {
		t.Fatalf("counted = %+v", r.Counted)
	}
	if !reflect.DeepEqual(r.Stored, []KV{{"raised_by", "opus"}}) {
		t.Fatalf("stored = %+v", r.Stored)
	}
}

func TestResolveAsKeyWinsOverValueSemantics(t *testing.T) {
	t.Parallel()
	r := Resolve([]KV{{"kind", "good"}, {"audience", "author"}}, reviewDisplay, nil)
	if r.Audience != "author" {
		t.Fatalf("audience = %q, want the direct key's author over good's reviewer", r.Audience)
	}
	r = Resolve([]KV{{"kind", "good"}, {"audience", "everyone"}}, reviewDisplay, nil)
	if r.Audience != "reviewer" || len(r.Problems) != 1 {
		t.Fatalf("an invalid direct value must not win: %q %v", r.Audience, r.Problems)
	}
}

func TestResolveNoDeclarationShowsEveryKeyAsALine(t *testing.T) {
	t.Parallel()
	r := Resolve([]KV{{"severity", "risk"}, {"confidence", "high"}}, nil, nil)
	if !reflect.DeepEqual(r.Lines, []Line{{"severity", "risk"}, {"confidence", "high"}}) || len(r.Badges) != 0 {
		t.Fatalf("lines %+v badges %+v", r.Lines, r.Badges)
	}
}

func TestResolveAddedOverridesAndShowsBuiltInStatus(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	r := Resolve([]KV{{"kind", "issue"}, {"severity", "high"}}, reviewDisplay,
		[]Added{{Key: "severity", Value: "low", By: "MR", At: at}, {Key: "status", Value: "disagreed", By: "MR", At: at}})
	if r.Level != "low" || r.Frame != "grey" {
		t.Fatalf("the added severity must drive level/frame: %q %q", r.Level, r.Frame)
	}
	last := r.Badges[len(r.Badges)-1]
	if last != (Badge{Key: "status", Value: "disagreed", Color: "red", Style: "text"}) {
		t.Fatalf("status badge = %+v", last)
	}
	want := []Change{{Key: "severity", From: "high", To: "low", By: "MR"}, {Key: "status", To: "disagreed", By: "MR"}}
	if !reflect.DeepEqual(r.Added, want) {
		t.Fatalf("added = %+v", r.Added)
	}
}

func TestResolveFrameOwnedByFirstKeyInOrder(t *testing.T) {
	t.Parallel()
	d := Display{
		"a": {Show: []string{"frame"}, Order: 2, Values: map[string]Value{"x": {Color: "blue"}}},
		"b": {Show: []string{"frame"}, Order: 1, Values: map[string]Value{"y": {Color: "green"}}},
	}
	if r := Resolve([]KV{{"a", "x"}, {"b", "y"}}, d, nil); r.Frame != "green" {
		t.Fatalf("frame = %q, want b's green (order 1)", r.Frame)
	}
}

func TestResolveUnlistedValueUsesOtherThenGrey(t *testing.T) {
	t.Parallel()
	d := Display{"kind": {Show: []string{"badge"}, Other: &Value{Color: "purple"}}}
	if b := Resolve([]KV{{"kind", "thought"}}, d, nil).Badges[0]; b.Color != "purple" {
		t.Fatalf("other colour = %q", b.Color)
	}
	d = Display{"kind": {Show: []string{"badge"}}}
	if b := Resolve([]KV{{"kind", "thought"}}, d, nil).Badges[0]; b.Color != "grey" || b.Style != "filled" {
		t.Fatalf("default badge = %+v", b)
	}
}

func TestResolveKeepsValidationProblems(t *testing.T) {
	t.Parallel()
	d := Display{"kind": {Show: []string{"badge"}, Values: map[string]Value{"issue": {Color: "crimson"}}}}
	r := Resolve([]KV{{"kind", "issue"}}, d, nil)
	if len(r.Problems) != 1 || r.Badges[0].Color != "grey" {
		t.Fatalf("problems %v badge %+v", r.Problems, r.Badges)
	}
}

func TestCount(t *testing.T) {
	t.Parallel()
	rs := []Resolved{
		Resolve([]KV{{"kind", "issue"}}, reviewDisplay, nil),
		Resolve([]KV{{"kind", "good"}}, reviewDisplay, nil),
		Resolve([]KV{{"kind", "good"}}, reviewDisplay, nil),
		Resolve([]KV{{"intent", "x"}}, reviewDisplay, nil),
	}
	c := Count(rs)
	want := []CountGroup{{Key: "kind", Label: "kind", Values: []CountValue{{"issue", "red", 1}, {"good", "green", 2}}}}
	if !reflect.DeepEqual(c.Groups, want) || c.Author != 1 || c.Reviewer != 2 {
		t.Fatalf("counts = %+v", c)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/notemeta/`
Expected: FAIL — undefined `Resolve`, `Badge`, `Count`, …

- [ ] **Step 3: Write `internal/notemeta/resolve.go`**

```go
package notemeta

// Badge is one chip: a key's value in its colour, glyph and style.
type Badge struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Color string `json:"color,omitempty"`
	Glyph string `json:"glyph,omitempty"`
	Style string `json:"style,omitempty"`
}

// Line is one "label: value" row under a note's title.
type Line struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Change is an added value shown over the author's (From "" when the
// author wrote no value for the key).
type Change struct {
	Key  string `json:"key"`
	From string `json:"from,omitempty"`
	To   string `json:"to"`
	By   string `json:"by,omitempty"`
}

// Counted is one key/value a note contributes to its review's counts.
type Counted struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
	Color string `json:"color,omitempty"`
}

// Resolved is what to show for one note — the one input every renderer
// (TUI, web, CLI text, forge adapters) reads.
type Resolved struct {
	Badges   []Badge   `json:"badges,omitempty"`
	Frame    string    `json:"frame,omitempty"`
	Lines    []Line    `json:"lines,omitempty"`
	Counted  []Counted `json:"counted,omitempty"`
	Level    string    `json:"level,omitempty"`
	Audience string    `json:"audience,omitempty"`
	Stored   []KV      `json:"stored,omitempty"`
	Added    []Change  `json:"added,omitempty"`
	Problems []Problem `json:"problems,omitempty"`
}

// Empty reports a note with nothing to show and nothing to report.
func (r Resolved) Empty() bool {
	return len(r.Badges) == 0 && r.Frame == "" && len(r.Lines) == 0 && len(r.Stored) == 0 &&
		len(r.Added) == 0 && len(r.Problems) == 0 && r.Level == "" && r.Audience == ""
}

// Resolve evaluates a note: its author's meta, its effective declaration and
// the meta added later (spec §3, §2.4). The declaration is re-validated here,
// so a bad entry is a problem for as long as it stands.
func Resolve(meta []KV, d Display, added []Added) Resolved {
	var out Resolved
	vals, order, changes := overlay(meta, added)
	out.Added = changes
	decl, probs := validate(d)
	out.Problems = probs
	if len(decl) == 0 {
		// No declaration at all: every key is a line (what tags looked like).
		for _, k := range order {
			if k == StatusKey {
				continue
			}
			out.Lines = append(out.Lines, Line{k, vals[k]})
		}
		decl = Display{}
	}
	if _, ok := decl[StatusKey]; !ok {
		decl[StatusKey] = StatusDisplay
	}
	declared := map[string]bool{}
	directLevel, directAudience := false, false
	for _, k := range sortedKeys(decl) {
		declared[k] = true
		v, ok := vals[k]
		if !ok {
			continue
		}
		key := decl[k]
		spec := key.Values[v]
		if _, listed := key.Values[v]; !listed && key.Other != nil {
			spec = *key.Other
		}
		label := key.Label
		if label == "" {
			label = k
		}
		for _, p := range key.Show {
			switch p {
			case PlaceBadge:
				c := spec.Color
				if c == "" {
					c = "grey"
				}
				st := key.Style
				if st == "" {
					st = StyleFilled
				}
				out.Badges = append(out.Badges, Badge{Key: k, Value: v, Color: c, Glyph: spec.Glyph, Style: st})
			case PlaceFrame:
				if out.Frame == "" && spec.Color != "" {
					out.Frame = spec.Color
				}
			case PlaceCount:
				out.Counted = append(out.Counted, Counted{Key: k, Label: label, Value: v, Color: spec.Color})
			case PlaceLine:
				out.Lines = append(out.Lines, Line{label, v})
			}
		}
		switch key.As {
		case "level":
			if oneOf(v, Levels) {
				out.Level, directLevel = v, true
			} else {
				out.Problems = append(out.Problems, Problem("meta "+k+": "+quote(v)+" is not a level"))
			}
		case "audience":
			if oneOf(v, Audiences) {
				out.Audience, directAudience = v, true
			} else {
				out.Problems = append(out.Problems, Problem("meta "+k+": "+quote(v)+" is not an audience"))
			}
		}
		if spec.Level != "" && !directLevel && out.Level == "" {
			out.Level = spec.Level
		}
		if spec.Audience != "" && !directAudience && out.Audience == "" {
			out.Audience = spec.Audience
		}
	}
	if len(d) > 0 {
		for _, k := range order {
			if !declared[k] {
				out.Stored = append(out.Stored, KV{k, vals[k]})
			}
		}
	}
	return out
}

// overlay applies the added entries over the author's meta: the values, the
// key order (author's first, then new keys in added order) and the changes.
func overlay(meta []KV, added []Added) (map[string]string, []string, []Change) {
	vals := map[string]string{}
	var order []string
	for _, kv := range meta {
		if _, dup := vals[kv.Key]; dup {
			continue
		}
		vals[kv.Key] = kv.Value
		order = append(order, kv.Key)
	}
	var changes []Change
	for _, a := range added {
		from, had := vals[a.Key]
		if had && from == a.Value {
			continue
		}
		if !had {
			order = append(order, a.Key)
		}
		vals[a.Key] = a.Value
		changes = append(changes, Change{Key: a.Key, From: from, To: a.Value, By: a.By})
	}
	return vals, order, changes
}

func quote(s string) string { return "\"" + s + "\"" }

// CountValue is one value's tally within a key.
type CountValue struct {
	Value string `json:"value"`
	Color string `json:"color,omitempty"`
	N     int    `json:"n"`
}

// CountGroup is one counted key's values, in first-seen order.
type CountGroup struct {
	Key    string       `json:"key"`
	Label  string       `json:"label"`
	Values []CountValue `json:"values"`
}

// Counts is a set of notes' tallies: per counted key, plus how many notes
// are for the author and how many for the reviewer.
type Counts struct {
	Groups   []CountGroup `json:"groups,omitempty"`
	Author   int          `json:"author,omitempty"`
	Reviewer int          `json:"reviewer,omitempty"`
}

// Count tallies resolved notes (a review's remarks, a file's notes).
func Count(rs []Resolved) Counts {
	var c Counts
	at := map[string]int{}
	for _, r := range rs {
		switch r.Audience {
		case "author":
			c.Author++
		case "reviewer":
			c.Reviewer++
		}
		for _, x := range r.Counted {
			gi, ok := at[x.Key]
			if !ok {
				gi = len(c.Groups)
				at[x.Key] = gi
				c.Groups = append(c.Groups, CountGroup{Key: x.Key, Label: x.Label})
			}
			g := &c.Groups[gi]
			found := false
			for i := range g.Values {
				if g.Values[i].Value == x.Value {
					g.Values[i].N++
					found = true
					break
				}
			}
			if !found {
				g.Values = append(g.Values, CountValue{Value: x.Value, Color: x.Color, N: 1})
			}
		}
	}
	return c
}
```

- [ ] **Step 4: Run tests**

Run: `gofmt -l internal/notemeta; go vet ./internal/notemeta/ && go test ./internal/notemeta/`
Expected: PASS.

- [ ] **Step 5: Commit** — `gg add internal/notemeta` then commit "notemeta: resolve a note's meta into what to show, and count it".

---

### Task 3: The note record and notes format 3

**Files:**
- Modify: `internal/model/note.go` (Note struct, after `Tags`)
- Modify: `internal/notes/part_file.go` (`index`, `readIndex`, `write`, `mutateCap`, `mutateResolved`), `internal/notes/store.go` (new error)
- Modify (compile fixes for the rename): `internal/notes/part_file_test.go:51-70`, `internal/domain/forge_send_body.go:19-33`, `internal/domain/notebatch_plan.go:87`
- Test: `internal/notes/format_test.go` (new)

**Interfaces:**
- Consumes: `notemeta.KV`, `notemeta.Display`, `notemeta.Added`, `notemeta.SplitTags`, `notemeta.ConfidenceWord`.
- Produces: `model.Note.Meta []notemeta.KV`, `.Display notemeta.Display`, `.Added []notemeta.Added`, `.LegacyConfidence float64` (decode-only); `notes.Format = 3`; `notes.ErrNewerFormat`; `model.MetaValue(n Note, key string) string` (the effective value: added over author's).

- [ ] **Step 1: Write the failing tests** — `internal/notes/format_test.go`:

```go
package notes

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notemeta"
)

const oldPart = `[[notes]]
id = 'aaaaaaaa'
source = 'agent'
author = 'Claude Code'
side = 'new'
range = [3, 3]
context_hash = ''
summary = 's'
tags = ['review', 'severity: risk', 'perf']
confidence = 0.6
created = 2026-10-01T10:00:00Z
updated = 2026-10-01T10:00:00Z

[notes.address]
state = 'unstaged'
path = 'a.go'
`

func writeRaw(t *testing.T, dir, body string) *partFile {
	t.Helper()
	p := filepath.Join(dir, "notes.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return &partFile{path: p}
}

func TestUpgradeMovesTagsAndConfidenceIntoMeta(t *testing.T) {
	t.Parallel()
	fs := writeRaw(t, t.TempDir(), oldPart)
	ns, err := fs.Load()
	if err != nil {
		t.Fatal(err)
	}
	n := ns[0]
	if !reflect.DeepEqual(n.Tags, []string{"review", "perf"}) {
		t.Fatalf("tags = %v", n.Tags)
	}
	want := []notemeta.KV{{Key: "severity", Value: "risk"}, {Key: "confidence", Value: "medium"}}
	if !reflect.DeepEqual(n.Meta, want) || n.LegacyConfidence != 0 {
		t.Fatalf("meta = %v legacy = %v", n.Meta, n.LegacyConfidence)
	}
}

func TestUpgradeIsReadOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := writeRaw(t, dir, oldPart)
	if _, err := fs.Load(); err != nil {
		t.Fatal(err)
	}
	// A mutation that changes nothing must not rewrite the converted file.
	if _, err := fs.Sweep(func(model.Note) bool { return true }); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "notes.toml"))
	if string(data) != oldPart {
		t.Fatalf("a no-op touched the file:\n%s", data)
	}
}

func TestWriteStampsFormat3AndRoundTripsMeta(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := newTestFile(dir)
	n := noteAt("aaaaaaaa", 1)
	n.Meta = []notemeta.KV{{Key: "kind", Value: "issue"}}
	n.Display = notemeta.Display{"kind": {Show: []string{"badge"}, Values: map[string]notemeta.Value{"issue": {Color: "red"}}}}
	n.Added = []notemeta.Added{{Key: "status", Value: "agreed", By: "MR", At: n.Created}}
	if err := fs.Put(n); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "notes.toml"))
	if !strings.HasPrefix(string(data), "format = 3\n") {
		t.Fatalf("file does not start with the format:\n%s", data)
	}
	ns, err := fs.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ns[0].Meta, n.Meta) || !reflect.DeepEqual(ns[0].Display, n.Display) ||
		len(ns[0].Added) != 1 || !ns[0].Added[0].At.Equal(n.Added[0].At) || ns[0].Added[0].Value != "agreed" {
		t.Fatalf("round trip lost meta: %+v", ns[0])
	}
}

func TestNewerFormatRefusesWrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := writeRaw(t, dir, "format = 4\n"+oldPart)
	if ns, err := fs.Load(); err != nil || len(ns) != 1 {
		t.Fatalf("a newer file must still read: %v %v", ns, err)
	}
	if err := fs.Put(noteAt("bbbbbbbb", 2)); !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("Put = %v, want ErrNewerFormat", err)
	}
	if err := fs.Resolve(model.ThreadResolution{Root: "aaaaaaaa"}); !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("Resolve = %v, want ErrNewerFormat", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "notes.toml"))
	if string(data) != "format = 4\n"+oldPart {
		t.Fatal("a refused write touched the file")
	}
}
```

Also add to `internal/model/note_test.go` (create if absent; package `model`):

```go
func TestMetaValuePrefersAdded(t *testing.T) {
	t.Parallel()
	n := Note{Meta: []notemeta.KV{{Key: "severity", Value: "high"}},
		Added: []notemeta.Added{{Key: "severity", Value: "low"}}}
	if got := MetaValue(n, "severity"); got != "low" {
		t.Fatalf("MetaValue = %q", got)
	}
	if got := MetaValue(n, "kind"); got != "" {
		t.Fatalf("MetaValue(kind) = %q", got)
	}
}
```

(imports: `testing`, `github.com/homeend/gigagit/internal/notemeta`).

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/notes/ ./internal/model/`
Expected: FAIL — `n.Meta` undefined / `ErrNewerFormat` undefined.

- [ ] **Step 3: Implement**

`internal/model/note.go` — import `"github.com/homeend/gigagit/internal/notemeta"`; in `Note` replace the `Confidence` line with:

```go
	Tags        []string    `toml:"tags,omitempty"`
	// LegacyConfidence is the pre-format-3 confidence number, read only so
	// the store can turn it into meta (notes.upgrade); never written again.
	LegacyConfidence float64 `toml:"confidence,omitempty"`
	// Meta is the author's free key/values (spec 2026-10-12-note-meta §2):
	// gg owns no vocabulary. Display is the author's declaration of how they
	// are shown; Added is meta written onto the note later by someone else
	// (on a review note: per remark, by fingerprint).
	Meta    []notemeta.KV    `toml:"meta,omitempty"`
	Display notemeta.Display `toml:"display,omitempty"`
	Added   []notemeta.Added `toml:"added,omitempty"`
```

and add at the end of the file:

```go
// MetaValue is n's effective value for key: an added entry (for the note
// itself, not a remark) over the author's own; "" when neither has one.
func MetaValue(n Note, key string) string {
	for i := len(n.Added) - 1; i >= 0; i-- {
		if a := n.Added[i]; a.RemarkFP == "" && a.Key == key {
			return a.Value
		}
	}
	for _, kv := range n.Meta {
		if kv.Key == key {
			return kv.Value
		}
	}
	return ""
}
```

`internal/notes/store.go` — after `ErrRemoveRecord`:

```go
// ErrNewerFormat refuses a write to a notes file a newer gg wrote: this
// build would drop the fields it does not know.
var ErrNewerFormat = errors.New("this notes file was written by a newer gg; upgrade gg to change it")
```

`internal/notes/part_file.go`:

```go
// Format is the notes file layout this build writes (spec
// 2026-10-12-note-meta §2.5): 3 = meta, display and added meta on notes.
// A file without the key is older and is converted as it is read.
const Format = 3

type index struct {
	Format   int                      `toml:"format,omitempty"`
	Notes    []model.Note             `toml:"notes"`
	Resolved []model.ThreadResolution `toml:"resolved,omitempty"`
}
```

In `readIndex`, after the successful `toml.Unmarshal`, before `return idx, nil`:

```go
	for i := range idx.Notes {
		upgrade(&idx.Notes[i])
	}
```

and add:

```go
// upgrade converts a note read from a pre-format-3 file: "key: value" tags
// and the confidence number become meta (spec §2.5). Idempotent.
func upgrade(n *model.Note) {
	plain, kv := notemeta.SplitTags(n.Tags)
	if len(kv) > 0 {
		n.Tags = plain
		n.Meta = append(n.Meta, kv...)
	}
	if n.LegacyConfidence > 0 {
		n.Meta = append(n.Meta, notemeta.KV{Key: "confidence", Value: notemeta.ConfidenceWord(n.LegacyConfidence)})
		n.LegacyConfidence = 0
	}
}
```

In `write`, first line of the non-empty branch (before `toml.Marshal`): `idx.Format = Format`.

In `mutateCap`, right after `before, err := fs.readIndex()` / its `if err != nil` block:

```go
	if before.Format > Format {
		return 0, ErrNewerFormat
	}
```

and the same three lines (returning `ErrNewerFormat`) in `mutateResolved` after its `readIndex`.

Compile fixes:
- `internal/notes/part_file_test.go` `TestPutLoadRoundTrip`: replace `n.Confidence = 0.5` with `n.Meta = []notemeta.KV{{Key: "confidence", Value: "0.5"}}` and the `got[0].Confidence != 0.5` check with `!reflect.DeepEqual(got[0].Meta, n.Meta)` (add the `notemeta` import).
- `internal/domain/forge_send_body.go` `sendBody`: replace the tags/confidence conditions and loop with meta (same output for converted notes):

```go
	case agent && (rat != "" || len(n.Tags) > 0 || len(n.Meta) > 0):
		b.WriteString("\n\n<details><summary>Details</summary>\n\n")
		if rat != "" {
			b.WriteString(rat + "\n\n")
		}
		for _, t := range n.Tags {
			b.WriteString("- " + t + "\n")
		}
		for _, kv := range n.Meta {
			b.WriteString("- " + kv.Key + ": " + kv.Value + "\n")
		}
		b.WriteString("\n</details>")
```

  (drop the `strconv` import if now unused). Plan 4 replaces this whole body.
- `internal/domain/notebatch_plan.go:87`: drop `Confidence: it.Confidence,` here (Task 4 adds `Meta`/`Display`).
- `internal/notebatch/notebatch.go`: leave `Item.Confidence` for Task 4 to remove.

Update the forge-send-body golden/unit test that asserted the `confidence:` line, if any (`grep -rn "confidence:" internal/domain/*_test.go`): the expected body now lists `- confidence: medium` among the meta lines.

- [ ] **Step 4: Run tests**

Run: `go build ./... && go test ./internal/notes/ ./internal/model/ ./internal/domain/ -run 'Note|Send|Forge|Format|Upgrade|Meta'`
Expected: PASS.

- [ ] **Step 5: Commit** — "notes: format 3 — meta, display and added meta on notes; tags and confidence convert on read".

---

### Task 4: Agent JSON shapes carry meta and display

**Files:**
- Modify: `internal/notebatch/notebatch.go` (Item, Batch, rawAnnotation, rawComment, rawTop, parseAgentContext, parseComments)
- Modify: `internal/notebatch/review.go` (MetaKV alias, ReviewDoc/ReviewFile/ReviewNote, rawReview, ParseReview)
- Modify: `internal/domain/notebatch_plan.go:80-90` (planned notes get meta/display)
- Test: `internal/notebatch/meta_test.go` (new), update `internal/notebatch/notebatch_test.go` confidence assertions

**Interfaces:**
- Consumes: `notemeta.KV`, `notemeta.Display`, `notemeta.ParseDisplay`, `notemeta.CheckMeta`, `notemeta.Effective`.
- Produces:
  - `notebatch.Item.Meta []notemeta.KV`, `Item.Display notemeta.Display` (Item.Confidence removed; a confidence word folds into Meta as `confidence`)
  - `notebatch.Batch.Problems []notemeta.Problem`
  - `type MetaKV = notemeta.KV`
  - `ReviewDoc.Display`, `ReviewFile.Display`, `ReviewNote.Display notemeta.Display`; `ReviewDoc.Problems []notemeta.Problem`

- [ ] **Step 1: Write the failing tests** — `internal/notebatch/meta_test.go`:

```go
package notebatch

import (
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/notemeta"
)

func TestAgentContextMetaAndDisplay(t *testing.T) {
	t.Parallel()
	b, err := Parse([]byte(`{"version":1,
	  "display":{"kind":{"show":["badge"]}},
	  "files":[{"path":"a.go","annotations":[
	    {"newRange":[1,1],"summary":"s","confidence":"high","meta":{"kind":"issue","n":3},
	     "display":{"kind":{"show":["line"]}}},
	    {"newRange":[2,2],"summary":"t","meta":{"kind":"good"}}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	first, second := b.Items[0], b.Items[1]
	if !reflect.DeepEqual(first.Meta, []notemeta.KV{{Key: "confidence", Value: "high"}, {Key: "kind", Value: "issue"}, {Key: "n", Value: "3"}}) {
		t.Fatalf("meta = %v", first.Meta)
	}
	if first.Display["kind"].Show[0] != "line" {
		t.Fatalf("an annotation's display must replace the batch's: %+v", first.Display)
	}
	if second.Display["kind"].Show[0] != "badge" {
		t.Fatalf("the batch display must reach every note: %+v", second.Display)
	}
}

func TestCommentBatchMetaAndDisplay(t *testing.T) {
	t.Parallel()
	b, err := Parse([]byte(`{"comments":[{"filePath":"a.go","newLine":3,"summary":"s",
	  "meta":{"status":"disagreed"},"display":{"status":{"show":["badge"],"colour":"x"}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if b.Items[0].Meta[0] != (notemeta.KV{Key: "status", Value: "disagreed"}) {
		t.Fatalf("meta = %v", b.Items[0].Meta)
	}
	if len(b.Problems) != 1 {
		t.Fatalf("the unknown field must be a problem, got %v", b.Problems)
	}
}

func TestBatchMalformedDisplayFailsTheBatch(t *testing.T) {
	t.Parallel()
	if _, err := Parse([]byte(`{"comments":[{"filePath":"a.go","newLine":3,"summary":"s","display":{"status":"badge"}}]}`)); err == nil {
		t.Fatal("a malformed display must fail the batch")
	}
}

func TestBatchOverLimitMetaIsDroppedWithAProblem(t *testing.T) {
	t.Parallel()
	b, err := Parse([]byte(`{"comments":[{"filePath":"a.go","newLine":3,"summary":"s","meta":{"intent":"a\nb","kind":"issue"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Items[0].Meta) != 1 || len(b.Problems) != 1 {
		t.Fatalf("meta %v problems %v", b.Items[0].Meta, b.Problems)
	}
}

func TestReviewDocDisplayAtThreeLevels(t *testing.T) {
	t.Parallel()
	doc, err := ParseReview([]byte(`{"version":1,"summary":"x",
	  "display":{"kind":{"show":["badge"]}},
	  "files":[{"path":"a.go","display":{"kind":{"show":["count"]}},
	    "annotations":[{"newRange":[1,1],"summary":"s","meta":{"kind":"issue"},"display":{"intent":{"show":["line"]}}}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Display["kind"].Show[0] != "badge" || doc.Files[0].Display["kind"].Show[0] != "count" ||
		doc.Files[0].Notes[0].Display["intent"].Show[0] != "line" {
		t.Fatalf("displays = %+v / %+v / %+v", doc.Display, doc.Files[0].Display, doc.Files[0].Notes[0].Display)
	}
}

func TestReviewDocMalformedDisplayIsNotADoc(t *testing.T) {
	t.Parallel()
	if _, err := ParseReview([]byte(`{"version":1,"summary":"x","display":[1],"files":[]}`)); err == nil {
		t.Fatal("want ErrNotReviewDoc")
	}
}
```

In `notebatch_test.go`, replace each `.Confidence` assertion with the meta form: an annotation with `"confidence":"high"` now yields `Meta` containing `{confidence high}`; an unknown word (`"certain"`) yields `{confidence certain}`? — NO: keep hunk's rule, an unknown word is dropped (assert `Meta` has no `confidence` key).

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/notebatch/`
Expected: FAIL — unknown fields `Meta`, `Display`, `Problems`.

- [ ] **Step 3: Implement**

`review.go`: replace `type MetaKV struct{ Key, Value string }` with

```go
// MetaKV is one meta entry, the value rendered as text (a string as is, any
// other JSON value as its compact JSON). It is notemeta's KV.
type MetaKV = notemeta.KV
```

(import `"github.com/homeend/gigagit/internal/notemeta"`; existing `MetaKV{k, s}` composite literals stay valid). Add `Display notemeta.Display` to `ReviewNote`, `ReviewFile`, `ReviewDoc`, and `Problems []notemeta.Problem` to `ReviewDoc`. In `rawReview` add `Display json.RawMessage \`json:"display"\`` at the top level, in each file and in each annotation. In `ParseReview` after each `metaKVs` call parse the sibling display:

```go
	if doc.Display, err = docDisplay(raw.Display, "display", &doc.Problems); err != nil {
		return ReviewDoc{}, err
	}
```

(`rf.Display` with `fmt.Sprintf("files[%d].display", i)`, `n.Display` with `where+".display"`), and add:

```go
// docDisplay parses one level's declaration; malformed is not a document,
// an unknown field is a problem the document keeps.
func docDisplay(raw json.RawMessage, where string, probs *[]notemeta.Problem) (notemeta.Display, error) {
	d, ps, err := notemeta.ParseDisplay(raw)
	if err != nil {
		return nil, notDoc("%s: %v", where, err)
	}
	for _, p := range ps {
		*probs = append(*probs, notemeta.Problem(where+": "+string(p)))
	}
	return d, nil
}
```

`notebatch.go`:
- `Item`: remove `Confidence float64`; add `Meta []notemeta.KV` and `Display notemeta.Display` (doc comment: "the author's meta and declaration (spec 2026-10-12-note-meta); a confidence word folds into Meta").
- `Batch`: add `Problems []notemeta.Problem // dropped meta / display parts, for the caller to echo`.
- `rawTop`: add `Display json.RawMessage \`json:"display"\``.
- `rawAnnotation`, `rawComment`: add `Meta json.RawMessage \`json:"meta"\`` and `Display json.RawMessage \`json:"display"\``.
- Replace `confidenceWords` map with a set: `var confidenceWords = map[string]bool{"low": true, "medium": true, "high": true}`.
- In `parseAgentContext`, parse the top display once: `top, probs, err := notemeta.ParseDisplay(t.Display)` → on error `return Batch{}, fmt.Errorf("display: %v", err)`; append probs to `b.Problems`. For each annotation build the item's meta and display with one helper:

```go
// itemMeta builds one item's meta (the agent's object, a confidence word
// first) and its effective display (the batch's, replaced key by key by the
// item's), recording what was dropped.
func itemMeta(where string, conf *string, rawMeta, rawDisplay json.RawMessage, batch notemeta.Display, b *Batch) ([]notemeta.KV, notemeta.Display, error) {
	var meta []notemeta.KV
	if conf != nil && confidenceWords[strings.TrimSpace(*conf)] {
		meta = append(meta, notemeta.KV{Key: "confidence", Value: strings.TrimSpace(*conf)})
	}
	kv, err := metaKVs(rawMeta)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: meta must be an object", where)
	}
	meta = append(meta, kv...)
	meta, probs := notemeta.CheckMeta(meta)
	for _, p := range probs {
		b.Problems = append(b.Problems, notemeta.Problem(where+": "+string(p)))
	}
	d, dprobs, err := notemeta.ParseDisplay(rawDisplay)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %v", where, err)
	}
	for _, p := range dprobs {
		b.Problems = append(b.Problems, notemeta.Problem(where+": "+string(p)))
	}
	return meta, notemeta.Effective(batch, d), nil
}
```

  and use it where `Item.Confidence` was set (annotation: `conf = a.Confidence`, `where = fmt.Sprintf("files[%d].annotations[%d]", fi, ai)`; comment: `conf = nil`, `where = fmt.Sprintf("comments[%d]", i)`, `batch = nil`). `metaKVs` returns `notDoc`-wrapped errors; map them to the batch's own message as above.

`internal/domain/notebatch_plan.go` where the planned note is built (line ~87): add `Meta: it.Meta, Display: it.Display,` next to `Tags: it.Tags`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/notebatch/ ./internal/domain/ -run 'Batch|Review|Meta'`
Expected: PASS.

- [ ] **Step 5: Commit** — "notebatch: meta and display on all three agent shapes".

---

### Task 5: Domain — remarks, replies and the wire carry meta

**Files:**
- Create: `internal/domain/note_meta.go`
- Modify: `internal/domain/review_notes.go` (`Review.Added`, `reviewOf` both branches), `internal/domain/review_doc.go:200-210`, `internal/domain/note_link.go:232-238`, `internal/domain/forge_send.go:603-607`, `internal/domain/pr_reviews.go:108-113`, `internal/domain/notewire.go` (WireNote, ToWireNote), `internal/domain/forge_send_candidates.go:183,210-218`, `internal/tui/diff_notes.go:671-673`
- Test: `internal/domain/note_meta_test.go`

**Interfaces:**
- Consumes: Tasks 1–4.
- Produces:
  - `func NoteMeta(n model.Note, root *model.Note) notemeta.Resolved` — `n`'s display, else `root`'s when `n` is a reply with none.
  - `func (r Review) remarkMeta(n *model.Note, f notebatch.ReviewFile, dn notebatch.ReviewNote, fp string)` — sets `n.Meta`, `n.Display` (= `notemeta.Effective(r.Doc.Display, f.Display, dn.Display)`), `n.Added` (= the review note's entries whose `RemarkFP == fp`, with `RemarkFP` cleared).
  - `WireNote.Meta []notemeta.KV \`json:"meta,omitempty"\``, `WireNote.Display *notemeta.Resolved \`json:"display,omitempty"\`` (nil when `Empty()`).
  - `func ToWireNoteIn(r ResolvedNote, root *model.Note) WireNote`; `ToWireNote(r)` = `ToWireNoteIn(r, nil)`; replies are converted with `&r.Note` as root.
  - `severityOf(n model.Note) string` = `model.MetaValue(n, "severity")`.

- [ ] **Step 1: Write the failing tests** — `internal/domain/note_meta_test.go`. Use the existing review fixture helpers (`grep -n "func saveTestReview\|func storeReview\|reviewDocJSON" internal/domain/*_test.go` and reuse the one `review_doc_test.go` uses to store a review note with a document on a commit; below it is called `storeDocReview(t, svc, commit, docJSON) (reviewID string)` — use the real helper name you find, keeping these bodies):

```go
package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notemeta"
)

const metaDoc = `{"version":1,"summary":"x",
 "display":{"kind":{"show":["badge"],"values":{"issue":{"color":"red","audience":"author"}}}},
 "files":[{"path":"a.txt","annotations":[
   {"newRange":[1,1],"summary":"first","meta":{"kind":"issue","raised_by":"opus"}},
   {"newRange":[2,2],"summary":"second","meta":{"kind":"good"},"display":{"kind":{"show":["line"]}}}]}]}`

func TestRemarksCarryMetaAndEffectiveDisplay(t *testing.T) {
	t.Parallel()
	svc, commit := newMetaTestRepo(t) // a repo with a.txt (3 lines) committed; returns the sha
	rid := storeDocReview(t, svc, commit, metaDoc)
	ctx := context.Background()
	root, _, _, err := svc.NoteThread(ctx, "review:"+rid+":0")
	if err != nil {
		t.Fatal(err)
	}
	r := NoteMeta(root, nil)
	if len(r.Badges) != 1 || r.Badges[0].Color != "red" || r.Audience != "author" {
		t.Fatalf("remark 0 = %+v", r)
	}
	if len(r.Stored) != 1 || r.Stored[0].Key != "raised_by" {
		t.Fatalf("stored = %+v", r.Stored)
	}
	root1, _, _, _ := svc.NoteThread(ctx, "review:"+rid+":1")
	if r1 := NoteMeta(root1, nil); len(r1.Badges) != 0 || len(r1.Lines) != 1 {
		t.Fatalf("remark 1 must use its own declaration: %+v", r1)
	}
}

func TestReplyInheritsRootDisplay(t *testing.T) {
	t.Parallel()
	root := model.Note{Meta: []notemeta.KV{{Key: "kind", Value: "issue"}},
		Display: notemeta.Display{"kind": {Show: []string{"badge"}}}}
	reply := model.Note{ParentID: "x", Meta: []notemeta.KV{{Key: "kind", Value: "good"}}}
	if r := NoteMeta(reply, &root); len(r.Badges) != 1 || r.Badges[0].Value != "good" {
		t.Fatalf("reply = %+v", r)
	}
	reply.Display = notemeta.Display{"kind": {Show: []string{"line"}}}
	if r := NoteMeta(reply, &root); len(r.Badges) != 0 {
		t.Fatalf("a reply's own declaration wins: %+v", r)
	}
}

func TestWireNoteCarriesMetaAndDisplay(t *testing.T) {
	t.Parallel()
	n := model.Note{ID: "a", Source: model.NoteSourceAgent, Meta: []notemeta.KV{{Key: "severity", Value: "high"}}}
	w := ToWireNote(ResolvedNote{Note: n})
	if len(w.Meta) != 1 || w.Display == nil || len(w.Display.Lines) != 1 || w.Severity != "high" {
		t.Fatalf("wire = %+v", w)
	}
	if w := ToWireNote(ResolvedNote{Note: model.Note{ID: "b"}}); w.Display != nil {
		t.Fatalf("a note with no meta must carry no display: %+v", w.Display)
	}
}
```

Write `newMetaTestRepo` in the same test file on top of the package's existing repo helper (`newTestRepo`/`testRepo` — whichever `review_doc_test.go` uses), committing `a.txt` = "one\ntwo\nthree\n".

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/domain/ -run 'RemarksCarry|ReplyInherits|WireNoteCarries'`
Expected: FAIL — `NoteMeta` undefined.

- [ ] **Step 3: Implement** — `internal/domain/note_meta.go`:

```go
package domain

import (
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
	"github.com/homeend/gigagit/internal/notemeta"
)

// NoteMeta resolves what to show for note n (spec 2026-10-12-note-meta §4):
// its meta, its declaration — a reply with none uses its root's — and the
// meta added to it later.
func NoteMeta(n model.Note, root *model.Note) notemeta.Resolved {
	d := n.Display
	if len(d) == 0 && root != nil {
		d = root.Display
	}
	return notemeta.Resolve(n.Meta, d, ownAdded(n.Added))
}

// ownAdded is the added meta of the note itself (a review note's per-remark
// entries are its remarks', never its own).
func ownAdded(as []notemeta.Added) []notemeta.Added {
	var out []notemeta.Added
	for _, a := range as {
		if a.RemarkFP == "" {
			out = append(out, a)
		}
	}
	return out
}

// remarkMeta gives a remark's note its meta, its effective declaration
// (document → file → remark) and the meta added to it on its review note.
func (r Review) remarkMeta(n *model.Note, f notebatch.ReviewFile, dn notebatch.ReviewNote, fp string) {
	n.Meta = append([]notemeta.KV(nil), dn.Meta...)
	var doc notemeta.Display
	if r.Doc != nil {
		doc = r.Doc.Display
	}
	n.Display = notemeta.Effective(doc, f.Display, dn.Display)
	n.Added = nil
	for _, a := range r.Added {
		if a.RemarkFP == fp {
			a.RemarkFP = ""
			n.Added = append(n.Added, a)
		}
	}
}
```

`review_notes.go`: add to `Review` after `RemarkSends`:

```go
	// Added is the review note's added meta; an entry with a RemarkFP belongs
	// to that remark (remarkMeta).
	Added []notemeta.Added
```

and `Added: n.Added` to both `Review{…}` literals in `reviewOf`.

Replace the four `for _, kv := range dn.Meta { …Tags = append(… kv.Key+": "+kv.Value) }` loops:
- `review_doc.go` (`reviewDocNotes`): `r.remarkMeta(&n, f, dn, fp)`.
- `pr_reviews.go`: the loop is inside `for _, f := range …Files / for _, dn := range f.Notes` with `fp` computed above it (`remarkFP(path, side, dn.Range, dn.Summary)` — compute it there if it is not already named) → `r.remarkMeta(&n, f, dn, fp)`.
- `forge_send.go`: `r.remarkMeta(&note, f, dn, fp)` (`fp` is in scope — `key := RemarkKey(r.ID, fp)` follows).
- `note_link.go` (`remarkThread`): the remark `rm` comes from `s.reviewRemark`; find its file and note in `r.Doc` by `rm.N` (walk `r.Doc.Files`/`Notes` counting to `rm.N`, the order `reviewDocNotes` numbers them) and call `r.remarkMeta(&root, f, dn, remarkFP(f.Path, reviewSide(dn.Side), dn.Range, dn.Summary))`. Add a small helper in `note_meta.go`:

```go
// docRemark is review r's remark number i: its file and its note.
func (r Review) docRemarkAt(i int) (notebatch.ReviewFile, notebatch.ReviewNote, bool) {
	if r.Doc == nil {
		return notebatch.ReviewFile{}, notebatch.ReviewNote{}, false
	}
	for _, f := range r.Doc.Files {
		if i < len(f.Notes) {
			return f, f.Notes[i], true
		}
		i -= len(f.Notes)
	}
	return notebatch.ReviewFile{}, notebatch.ReviewNote{}, false
}
```

`notewire.go`: add the two fields to `WireNote` (after `Severity`):

```go
	// Meta is the note's own meta; Display what to show for it (spec
	// 2026-10-12-note-meta §4) — badges, frame, lines, counted values,
	// semantics, added values and problems. Nil when there is nothing.
	Meta    []notemeta.KV      `json:"meta,omitempty"`
	Display *notemeta.Resolved `json:"display,omitempty"`
```

Rename `ToWireNote`'s body into `ToWireNoteIn(r ResolvedNote, root *model.Note) WireNote`, keep `func ToWireNote(r ResolvedNote) WireNote { return ToWireNoteIn(r, nil) }`, and inside it:

```go
	w.Severity = severityOf(r.Note)
	w.Meta = r.Note.Meta
	if res := NoteMeta(r.Note, root); !res.Empty() {
		w.Display = &res
	}
	…
	for _, rep := range r.Replies {
		w.Replies = append(w.Replies, ToWireNoteIn(rep, &r.Note))
	}
```

`forge_send_candidates.go`: `severityOf(n model.Note) string { return model.MetaValue(n, "severity") }`; call sites pass the note (`severityOf(n)`, `severityOf(r.Note)`).

`internal/tui/diff_notes.go:671-673` (kept text, plan 2 replaces it):

```go
	if model.IsReviewNoteID(r.Note.ID) && len(r.Note.Meta) > 0 {
		var kv []string
		for _, m := range r.Note.Meta {
			kv = append(kv, m.Key+": "+m.Value)
		}
		t += " · " + sanitizeLine(strings.Join(kv, " · ")) // the review's meta
	}
```

- [ ] **Step 4: Run tests**

Run: `go build ./... && go test ./internal/domain/ ./internal/tui/ -run 'Meta|Review|Remark|Wire|Send|Note'`
Expected: PASS (fix any golden/assertion that compared `Tags` of a remark: it now reads `Meta`).

- [ ] **Step 5: Commit** — "domain: remarks and replies carry meta and an effective display; the wire carries the resolved display".

---

### Task 6: Domain operations — set, unset, display

**Files:**
- Modify: `internal/domain/note_meta.go`, `internal/domain/notes.go` (`NoteAdd`, `NoteReply` clamp meta)
- Test: `internal/domain/note_meta_test.go`

**Interfaces:**
- Consumes: Task 5; `notes.Store.Edit`, `Service.Review`, `Review.remarkFPs`, `model.ParseReviewNoteID`.
- Produces:
  - `func (s *Service) SetNoteMeta(ctx context.Context, id, by string, kv []notemeta.KV) ([]notemeta.Problem, error)`
  - `func (s *Service) UnsetNoteMeta(ctx context.Context, id, by string, keys []string) error`
  - `func (s *Service) SetNoteDisplay(ctx context.Context, id string, d notemeta.Display) error`
  - `var ErrRemarkDisplay = errors.New("a remark's display is its review's")`

- [ ] **Step 1: Write the failing tests** (append to `note_meta_test.go`):

```go
func TestSetNoteMetaByAuthorRewritesInPlace(t *testing.T) {
	t.Parallel()
	svc, _ := newMetaTestRepo(t)
	ctx := context.Background()
	n, err := svc.NoteAdd(ctx, workNote(t, svc, "Claude Code")) // an unstaged note on a.txt:1
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetNoteMeta(ctx, n.ID, "Claude Code", []notemeta.KV{{Key: "kind", Value: "issue"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetNoteMeta(ctx, n.ID, "MR", []notemeta.KV{{Key: "status", Value: "agreed"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.NoteGet(ctx, n.ID)
	if model.MetaValue(got, "kind") != "issue" || len(got.Meta) != 1 {
		t.Fatalf("author meta = %v", got.Meta)
	}
	if len(got.Added) != 1 || got.Added[0].By != "MR" || got.Added[0].Value != "agreed" {
		t.Fatalf("added = %+v", got.Added)
	}
	if err := svc.UnsetNoteMeta(ctx, n.ID, "MR", []string{"status"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.NoteGet(ctx, n.ID); len(got.Added) != 0 {
		t.Fatalf("unset left %+v", got.Added)
	}
}

func TestRemarkAddedMetaFollowsFingerprint(t *testing.T) {
	t.Parallel()
	svc, commit := newMetaTestRepo(t)
	ctx := context.Background()
	rid := storeDocReview(t, svc, commit, metaDoc)
	if _, err := svc.SetNoteMeta(ctx, "review:"+rid+":1", "MR", []notemeta.KV{{Key: "status", Value: "disagreed"}}); err != nil {
		t.Fatal(err)
	}
	// Re-save the review with its two remarks swapped.
	swapped := `{"version":1,"summary":"x","files":[{"path":"a.txt","annotations":[
	  {"newRange":[2,2],"summary":"second","meta":{"kind":"good"}},
	  {"newRange":[1,1],"summary":"first","meta":{"kind":"issue"}}]}]}`
	resaveDocReview(t, svc, rid, swapped) // rewrite the review note's rationale in place (st.Edit)
	r0, _, _, _ := svc.NoteThread(ctx, "review:"+rid+":0")
	if model.MetaValue(r0, "status") != "disagreed" {
		t.Fatalf("the mark must follow 'second' to index 0: %+v", r0.Added)
	}
}

func TestSetNoteDisplayRefusals(t *testing.T) {
	t.Parallel()
	svc, commit := newMetaTestRepo(t)
	ctx := context.Background()
	rid := storeDocReview(t, svc, commit, metaDoc)
	if err := svc.SetNoteDisplay(ctx, "review:"+rid+":0", notemeta.Display{}); !errors.Is(err, ErrRemarkDisplay) {
		t.Fatalf("remark display = %v", err)
	}
	if err := svc.SetNoteDisplay(ctx, "forge:1", notemeta.Display{}); !errors.Is(err, ErrReadOnlyNote) {
		t.Fatalf("forge display = %v", err)
	}
	if _, err := svc.SetNoteMeta(ctx, "nosuchid", "MR", []notemeta.KV{{Key: "a", Value: "b"}}); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("missing note = %v", err)
	}
}

func TestSetNoteMetaReportsDroppedEntries(t *testing.T) {
	t.Parallel()
	svc, _ := newMetaTestRepo(t)
	ctx := context.Background()
	n, _ := svc.NoteAdd(ctx, workNote(t, svc, "A"))
	probs, err := svc.SetNoteMeta(ctx, n.ID, "A", []notemeta.KV{{Key: "ok", Value: "1"}, {Key: "bad", Value: "a\nb"}})
	if err != nil || len(probs) != 1 {
		t.Fatalf("probs %v err %v", probs, err)
	}
}
```

Helpers in the test file: `workNote(t, svc, author) model.Note` (an unstaged note on `a.txt` line 1 via `svc.NoteTarget(ctx, "a.txt", false, "")`), `resaveDocReview(t, svc, rid, doc)` (uses `svc.notesStore(ctx).Edit(rid, func(n *model.Note) error { n.Rationale = doc; return nil })`). Add `"errors"` to the imports.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/domain/ -run 'SetNoteMeta|RemarkAddedMeta|SetNoteDisplay'`
Expected: FAIL — undefined methods.

- [ ] **Step 3: Implement** (append to `note_meta.go`; add imports `context`, `errors`, `fmt`, `notes`):

```go
// ErrRemarkDisplay refuses a declaration on one remark: a declaration
// belongs to whoever wrote the meta it describes — the review's agent.
var ErrRemarkDisplay = errors.New("a remark's display is its review's")

// SetNoteMeta writes kv onto note id (spec §2.4, §5.1). On a review remark
// ("review:<id>:<n>") every entry is added meta on its review note, keyed
// by the remark's fingerprint; on a stored note it rewrites the author's
// meta when by is the note's author, else it is added meta. Over-limit
// entries are dropped and reported.
func (s *Service) SetNoteMeta(ctx context.Context, id, by string, kv []notemeta.KV) ([]notemeta.Problem, error) {
	kv, probs := notemeta.CheckMeta(kv)
	err := s.editMeta(ctx, id, func(n *model.Note, fp string) {
		for _, e := range kv {
			if fp == "" && by == n.Author {
				n.Meta = upsertKV(n.Meta, e)
				continue
			}
			n.Added = upsertAdded(n.Added, notemeta.Added{RemarkFP: fp, Key: e.Key, Value: e.Value, By: by, At: notes.Now().UTC()})
		}
		if len(n.Meta) > notemeta.MaxMetaKeys {
			n.Meta = n.Meta[:notemeta.MaxMetaKeys]
		}
	})
	return probs, err
}

// UnsetNoteMeta removes keys: from the author's meta when by is the author
// of a stored note, else from the added meta.
func (s *Service) UnsetNoteMeta(ctx context.Context, id, by string, keys []string) error {
	drop := map[string]bool{}
	for _, k := range keys {
		drop[k] = true
	}
	return s.editMeta(ctx, id, func(n *model.Note, fp string) {
		if fp == "" && by == n.Author {
			kept := n.Meta[:0:0]
			for _, e := range n.Meta {
				if !drop[e.Key] {
					kept = append(kept, e)
				}
			}
			n.Meta = kept
			return
		}
		kept := n.Added[:0:0]
		for _, a := range n.Added {
			if a.RemarkFP != fp || !drop[a.Key] {
				kept = append(kept, a)
			}
		}
		n.Added = kept
	})
}

// SetNoteDisplay replaces a stored note's own declaration (empty clears).
func (s *Service) SetNoteDisplay(ctx context.Context, id string, d notemeta.Display) error {
	if model.IsReviewNoteID(id) {
		return ErrRemarkDisplay
	}
	return s.editMeta(ctx, id, func(n *model.Note, _ string) {
		if len(d) == 0 {
			n.Display = nil
			return
		}
		n.Display = d
	})
}

// editMeta runs fn on the stored note that holds id's meta under its part
// lock: the review note for a remark (fp = the remark's fingerprint), the
// note itself otherwise (fp = "").
func (s *Service) editMeta(ctx context.Context, id string, fn func(n *model.Note, fp string)) error {
	if model.IsForgeNoteID(id) {
		return ErrReadOnlyNote
	}
	st := s.notesStore(ctx)
	if st == nil {
		return ErrNotesDisabled
	}
	target, fp := id, ""
	if rid, idx, ok := model.ParseReviewNoteID(id); ok {
		r, err := s.Review(ctx, rid)
		if err != nil {
			return err
		}
		fps := r.remarkFPs()
		if idx >= len(fps) {
			return fmt.Errorf("%w: review %s has no remark %d", ErrNoSuchRemark, rid, idx)
		}
		target, fp = rid, fps[idx]
	}
	err := st.Edit(target, func(n *model.Note) error {
		fn(n, fp)
		return nil
	})
	if errors.Is(err, notes.ErrNotFound) {
		return ErrNoteNotFound
	}
	if err == nil {
		s.invalidateNoteCounts()
	}
	return err
}

func upsertKV(kv []notemeta.KV, e notemeta.KV) []notemeta.KV {
	for i := range kv {
		if kv[i].Key == e.Key {
			kv[i].Value = e.Value
			return kv
		}
	}
	return append(kv, e)
}

func upsertAdded(as []notemeta.Added, e notemeta.Added) []notemeta.Added {
	for i := range as {
		if as[i].RemarkFP == e.RemarkFP && as[i].Key == e.Key {
			as[i] = e
			return as
		}
	}
	return append(as, e)
}
```

In `notes.go` `NoteAdd` and `NoteReply`, before storing: `n.Meta, _ = notemeta.CheckMeta(n.Meta)` (the CLI/MCP already refused or reported; this is the last guard).

- [ ] **Step 4: Run tests**

Run: `go test ./internal/domain/ -run 'Meta|Remark|NoteAdd|NoteReply'`
Expected: PASS.

- [ ] **Step 5: Commit** — "domain: SetNoteMeta, UnsetNoteMeta, SetNoteDisplay".

---

### Task 7: CLI — `--meta`, `--display`, `gg note meta`, text output

**Files:**
- Create: `internal/cli/note_meta.go`
- Modify: `internal/cli/note.go` (dispatch `meta`, `noteMutations`, usage strings, `noteAdd`, `noteReply`, `renderNoteLine`), `internal/cli/note_show.go`, `internal/cli/noteapply.go` (echo `Batch.Problems`)
- Test: `internal/cli/note_meta_test.go`

**Interfaces:**
- Consumes: Tasks 1, 5, 6.
- Produces: `type metaFlag []notemeta.KV` (flag.Value), `func readDisplayArg(v string) (notemeta.Display, []notemeta.Problem, error)`, `func noteMetaCmd(svc *domain.Service, args []string, stdout, stderr io.Writer) int`, `func metaText(r notemeta.Resolved) []string`.

- [ ] **Step 1: Write the failing tests** — `internal/cli/note_meta_test.go`, using the package's CLI harness (`grep -n "func runCLI\|func run(t" internal/cli/*_test.go`; the note tests call it with a temp repo — reuse exactly what `note_test.go` uses; below `runGG(t, dir, args...) (stdout, stderr string, code int)`):

```go
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoteAddWithMetaAndDisplay(t *testing.T) {
	t.Parallel()
	dir := noteRepo(t) // a repo with a.txt modified (unstaged), as note_test.go builds
	out, errOut, code := runGG(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "s",
		"--meta", "kind=issue", "--meta", "severity=high",
		"--display", `{"kind":{"show":["badge"],"values":{"issue":{"colour":"red"}}}}`, "--json")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "colour") {
		t.Fatalf("the unknown field must be named on stderr: %q", errOut)
	}
	var w struct {
		Meta    []struct{ Key, Value string } `json:"meta"`
		Display struct {
			Badges []struct{ Key, Value string } `json:"badges"`
		} `json:"display"`
	}
	if err := json.Unmarshal([]byte(out), &w); err != nil {
		t.Fatal(err)
	}
	if len(w.Meta) != 2 || len(w.Display.Badges) != 1 {
		t.Fatalf("wire = %s", out)
	}
}

func TestNoteAddBadMetaIsAUsageError(t *testing.T) {
	t.Parallel()
	dir := noteRepo(t)
	for _, bad := range []string{"kind", "=x", "intent=" + strings.Repeat("v", 300)} {
		_, errOut, code := runGG(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "s", "--meta", bad)
		if code != 2 {
			t.Fatalf("--meta %q: code %d (%s)", bad, code, errOut)
		}
	}
	_, _, code := runGG(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "s", "--display", `{"kind":"x"}`)
	if code != 2 {
		t.Fatalf("malformed display: code %d", code)
	}
}

func TestNoteMetaSetUnsetDisplay(t *testing.T) {
	t.Parallel()
	dir := noteRepo(t)
	id, _, _ := runGG(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "s", "--author", "A")
	id = strings.TrimSpace(id)
	if _, e, c := runGG(t, dir, "note", "meta", id, "set", "status=disagreed", "--author", "MR"); c != 0 {
		t.Fatalf("set: %d %s", c, e)
	}
	show, _, _ := runGG(t, dir, "note", "show", id)
	if !strings.Contains(show, "status: disagreed (MR)") {
		t.Fatalf("show = %q", show)
	}
	df := filepath.Join(t.TempDir(), "d.json")
	os.WriteFile(df, []byte(`{"status":{"show":["line"]}}`), 0o600)
	if _, e, c := runGG(t, dir, "note", "meta", id, "display", "@"+df, "--author", "A"); c != 0 {
		t.Fatalf("display: %d %s", c, e)
	}
	if _, e, c := runGG(t, dir, "note", "meta", id, "unset", "status", "--author", "MR"); c != 0 {
		t.Fatalf("unset: %d %s", c, e)
	}
	if show, _, _ := runGG(t, dir, "note", "show", id); strings.Contains(show, "disagreed") {
		t.Fatalf("unset left %q", show)
	}
}

func TestNoteListPrintsAMetaLine(t *testing.T) {
	t.Parallel()
	dir := noteRepo(t)
	runGG(t, dir, "note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "s", "--meta", "kind=issue")
	out, _, _ := runGG(t, dir, "note", "list", "--file", "a.txt")
	if !strings.Contains(out, "\n    meta: kind: issue\n") {
		t.Fatalf("list = %q", out)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/cli/ -run 'NoteAddWithMeta|NoteAddBadMeta|NoteMetaSet|NoteListPrintsAMeta'`
Expected: FAIL — `flag provided but not defined: -meta`.

- [ ] **Step 3: Implement** — `internal/cli/note_meta.go`:

```go
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/notemeta"
)

// metaFlag collects repeated --meta k=v.
type metaFlag []notemeta.KV

func (m *metaFlag) String() string { return "" }

func (m *metaFlag) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok || strings.TrimSpace(k) == "" {
		return fmt.Errorf("want key=value, got %q", v)
	}
	*m = append(*m, notemeta.KV{Key: strings.TrimSpace(k), Value: val})
	return nil
}

// checkedMeta is the CLI's strict limit check: any dropped entry is a usage
// error (exit 2), named on stderr.
func checkedMeta(sub string, m metaFlag, stderr io.Writer) ([]notemeta.KV, bool) {
	kv, probs := notemeta.CheckMeta(m)
	for _, p := range probs {
		fmt.Fprintf(stderr, "%s: --meta: %s\n", sub, p)
	}
	return kv, len(probs) == 0
}

// readDisplayArg reads --display: inline JSON, or @<file>.
func readDisplayArg(v string) (notemeta.Display, []notemeta.Problem, error) {
	if v == "" {
		return nil, nil, nil
	}
	data := []byte(v)
	if path, ok := strings.CutPrefix(v, "@"); ok {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		data = b
	}
	return notemeta.ParseDisplay(data)
}

// displayArg reads --display for sub: malformed = usage error, an unknown
// field = a warning on stderr.
func displayArg(sub, v string, stderr io.Writer) (notemeta.Display, bool) {
	d, probs, err := readDisplayArg(v)
	if err != nil {
		fmt.Fprintf(stderr, "%s: --display: %v\n", sub, err)
		return nil, false
	}
	for _, p := range probs {
		fmt.Fprintf(stderr, "%s: --display: %s (ignored)\n", sub, p)
	}
	return d, true
}

// noteMetaCmd is `gg note meta <id> set k=v… | unset k… | display <json|@file>`.
func noteMetaCmd(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	const usage = "usage: gg note meta [<repo-link>] <note-id|review:<id>:<n>> set <k=v>... | unset <k>... | display <json|@file> [--author <label>]"
	if len(args) < 2 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	id, verb := latestRemarkID(svc, args[0]), args[1]
	fs := flag.NewFlagSet("note meta", flag.ContinueOnError)
	fs.SetOutput(stderr)
	author := fs.String("author", "", "who writes (default: $GG_AGENT, else agent)")
	pos, err := parseSteerFlags(fs, args[2:]) // flags may sit between the values
	if err != nil {
		return 2
	}
	by := noteAuthorDefault(*author)
	ctx := context.Background()
	switch verb {
	case "set":
		var m metaFlag
		for _, p := range pos {
			if serr := m.Set(p); serr != nil {
				fmt.Fprintln(stderr, "note meta set:", serr)
				return 2
			}
		}
		kv, ok := checkedMeta("note meta set", m, stderr)
		if !ok || len(kv) == 0 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		_, err = svc.SetNoteMeta(ctx, id, by, kv)
	case "unset":
		if len(pos) == 0 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		err = svc.UnsetNoteMeta(ctx, id, by, pos)
	case "display":
		if len(pos) != 1 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		d, ok := displayArg("note meta display", pos[0], stderr)
		if !ok {
			return 2
		}
		err = svc.SetNoteDisplay(ctx, id, d)
	default:
		fmt.Fprintln(stderr, usage)
		return 2
	}
	switch {
	case errors.Is(err, domain.ErrRemarkDisplay):
		fmt.Fprintf(stderr, "note meta: %s — set it in the review document\n", err)
		return 1
	case err != nil:
		return noteIDExit("note meta", id, svc, err, stderr)
	}
	fmt.Fprintf(stdout, "%s %s\n", verb, id)
	return 0
}

// metaText is the resolved display as plain text lines (gg note show/list):
// shown keys, stored keys, added values with their writer, problems.
func metaText(r notemeta.Resolved) []string {
	var shown []string
	for _, b := range r.Badges {
		shown = append(shown, b.Key+": "+b.Value)
	}
	for _, l := range r.Lines {
		shown = append(shown, l.Label+": "+l.Value)
	}
	for _, s := range r.Stored {
		shown = append(shown, s.Key+": "+s.Value)
	}
	var out []string
	if len(shown) > 0 {
		out = append(out, "meta: "+strings.Join(shown, " · "))
	}
	for _, c := range r.Added {
		line := "added: " + c.Key + ": " + c.To
		if c.By != "" {
			line += " (" + c.By + ")"
		}
		if c.From != "" {
			line += ", was " + c.From
		}
		out = append(out, line)
	}
	for _, p := range r.Problems {
		out = append(out, "problem: "+string(p))
	}
	return out
}
```

`TestNoteMetaSetUnsetDisplay` expects `status: disagreed (MR)`: an added `status` with no author value prints `meta: status: disagreed` (the built-in badge) and `added: status: disagreed (MR)` — the substring matches the `added:` line.

`note.go`:
- `cmdNote` usage line: add `meta` to the list; `noteMutations`: add `"meta": true`; dispatch: `case "meta": return noteMetaCmd(svc, rest, stdout, stderr)`. In the NOTE-link rewrite block, accept `sub == "meta"` like reply/resolve; in `noteLinkShape` treat `"meta"` like `"reply"`.
- `noteAdd` and `noteReply`: declare `var meta metaFlag; fs.Var(&meta, "meta", "meta key=value (repeatable)")` and `display := fs.String("display", "", "display declaration: JSON or @file")`; after the summary check:

```go
	kv, ok := checkedMeta("note add", meta, stderr)
	if !ok {
		return 2
	}
	disp, ok := displayArg("note add", *display, stderr)
	if !ok {
		return 2
	}
```

  and pass `Meta: kv, Display: disp` in the `model.Note{…}` literal (sub name `"note reply"` in `noteReply`).
- `renderNoteLine(w, r, indent bool, status)` becomes `renderNoteLine(w, r, root *model.Note, status)` — `root == nil` is a thread root, otherwise the reply's root (so a reply's meta uses its root's declaration). Update its four callers (`note.go:875,877`, `prsend.go:260,262`: pass `nil` for the root line and `&r.Note` for each reply). Inside, replace `if indent` with `if root != nil` and add at the top `defer printMetaLines(w, r, root)`:

```go
// printMetaLines prints a note's resolved meta under its line (4 spaces;
// 6 under a reply).
func printMetaLines(w io.Writer, r domain.ResolvedNote, root *model.Note) {
	pad := "    "
	if root != nil {
		pad = "      "
	}
	for _, l := range metaText(domain.NoteMeta(r.Note, root)) {
		fmt.Fprintln(w, pad+l)
	}
}
```

`note_show.go`: after the rationale/link block of the root, `for _, l := range metaText(domain.NoteMeta(root, nil)) { fmt.Fprintln(stdout, "    "+l) }`; inside the replies loop after the rationale, the same with `domain.NoteMeta(r, &root)` and 6 spaces. `--json`: `root` already serialises `meta`/`display`/`added` (model fields have no json tags → Go names `Meta`, `Display`, `Added`); add `"display": domain.NoteMeta(root, nil)` to the map.

`noteapply.go`: after a successful parse, `for _, p := range b.Problems { fmt.Fprintf(stderr, "note apply: %s (ignored)\n", p) }`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cli/ -run 'Note'`
Expected: PASS (update any list-output golden that now gains a `meta:` line — only notes with meta gain one).

- [ ] **Step 5: Commit** — "cli: --meta/--display on note add/reply, gg note meta, meta in show/list".

---

### Task 8: MCP — meta on add/reply, `gg_note_meta`

**Files:**
- Modify: `internal/mcp/notes.go` (`noteAddIn`, `noteReplyIn`, handlers, new tool), the apply handler (echo problems into `out.Warning`)
- Test: `internal/mcp/notes_meta_test.go` (new; mirror the existing MCP note test harness — `grep -n "gg_note_add" internal/mcp/*_test.go`)

**Interfaces:**
- Consumes: Tasks 1, 6.
- Produces: tool `gg_note_meta` with input `{id, action: "set"|"unset"|"display", meta: {k: v}, keys: [..], display: {...}, author}` and output `okOut` + `problems []string`.

- [ ] **Step 1: Write the failing test**

```go
func TestNoteAddAndMetaTools(t *testing.T) {
	t.Parallel()
	s, dir := newNoteServer(t) // the existing helper the note tool tests use
	_ = dir
	res := callTool(t, s, "gg_note_add", map[string]any{"file": "a.txt", "new_line": 1, "summary": "s",
		"meta": map[string]string{"kind": "issue"}, "display": map[string]any{"kind": map[string]any{"show": []string{"badge"}}}})
	id := res["note"].(map[string]any)["id"].(string)
	disp := res["note"].(map[string]any)["display"].(map[string]any)
	if len(disp["badges"].([]any)) != 1 {
		t.Fatalf("display = %v", disp)
	}
	out := callTool(t, s, "gg_note_meta", map[string]any{"id": id, "action": "set", "meta": map[string]string{"status": "agreed"}, "author": "MR"})
	if out["ok"] != true {
		t.Fatalf("meta = %v", out)
	}
}
```

(`callTool` = the existing helper that invokes a tool and decodes its structured output; use its real name.)

- [ ] **Step 2: Run** `go test ./internal/mcp/ -run NoteAddAndMeta` — FAIL (unknown argument / unknown tool).

- [ ] **Step 3: Implement**
- `noteAddIn` and `noteReplyIn`: add

```go
	Meta    map[string]string `json:"meta,omitempty" jsonschema:"free key/values (suggested: kind, severity, intent, audience); see the review skills"`
	// Display is typed any, as gg_notes_apply's batch is: the SDK's schema
	// inference has no case for json.RawMessage. Re-marshalled for ParseDisplay.
	Display any `json:"display,omitempty" jsonschema:"how the meta keys are shown: {key: {show: [badge|frame|count|line], values: {value: {color, glyph, level, audience}}}}"`
```

- a helper `func metaFromTool(m map[string]string, display any) ([]notemeta.KV, notemeta.Display, []notemeta.Problem, error)` — keys sorted for determinism, `CheckMeta`; `display` (when non-nil) `json.Marshal`ed and passed to `ParseDisplay`; a malformed display is the tool's error.
- pass `Meta`/`Display` into the `model.Note` literals; append problems to a `Warning string` field on `noteOut` (add `Warning string \`json:"warning,omitempty"\``).
- register:

```go
	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_note_meta",
		Description: "Set or unset meta on a gg note (a stored note id or a review remark id review:<id>:<n>), or replace a stored note's display declaration. " +
			"Meta is a glance aid for the reader: it never sends, resolves or blocks anything. MUTATES gg's note store.",
		Annotations: mutatingAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in noteMetaIn) (*sdk.CallToolResult, noteMetaOut, error) {
		out := noteMetaOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		by := domain.NoteAuthorDefault(in.Author)
		var err error
		switch in.Action {
		case "set":
			kv, _, probs, merr := metaFromTool(in.Meta, nil)
			if merr != nil {
				return nil, out, merr
			}
			for _, p := range probs {
				out.Problems = append(out.Problems, string(p))
			}
			_, err = s.svc.SetNoteMeta(ctx, in.ID, by, kv)
		case "unset":
			err = s.svc.UnsetNoteMeta(ctx, in.ID, by, in.Keys)
		case "display":
			_, d, probs, merr := metaFromTool(nil, in.Display)
			if merr != nil {
				return nil, out, merr
			}
			for _, p := range probs {
				out.Problems = append(out.Problems, string(p))
			}
			err = s.svc.SetNoteDisplay(ctx, in.ID, d)
		default:
			return nil, out, fmt.Errorf(`action must be "set", "unset" or "display"`)
		}
		if err != nil {
			return nil, out, err
		}
		out.OK = true
		s.notifyNotesChanged()
		return nil, out, nil
	})
```

  with

```go
type noteMetaIn struct {
	ID      string            `json:"id" jsonschema:"a stored note id or a review remark id review:<id>:<n>"`
	Action  string            `json:"action" jsonschema:"set, unset or display"`
	Meta    map[string]string `json:"meta,omitempty" jsonschema:"set: the key/values"`
	Keys    []string          `json:"keys,omitempty" jsonschema:"unset: the keys"`
	Display any               `json:"display,omitempty" jsonschema:"display: the declaration (empty clears)"`
	Author  string            `json:"author,omitempty" jsonschema:"who writes (default: $GG_AGENT, else agent)"`
}

type noteMetaOut struct {
	Repo     RepoInfo `json:"repo"`
	OK       bool     `json:"ok"`
	Problems []string `json:"problems,omitempty"`
}
```

- `gg_notes_apply`: append each `b.Problems` entry to `out.Warning` (joined with "; ").

- [ ] **Step 4: Run** `go test ./internal/mcp/` — PASS (update the tool-list golden if the package pins tool names).

- [ ] **Step 5: Commit** — "mcp: meta and display on note tools; gg_note_meta".

---

### Task 9: Temporary notes (`gg session note`) carry meta

**Files:**
- Modify: `internal/agentdocs/notes.go` (`Note` fields, `AddNote` → `AddNoteMeta`, `NoteWire`), `internal/steer/steer.go` (`Command.Meta`, `Command.Display`, `FileNote.Meta`, `FileNote.Display`), `internal/cli/session_note.go` (flags), `internal/tui/steer_file_notes.go`, `internal/web/steer_notes.go` (pass through)
- Test: `internal/agentdocs/notes_meta_test.go` (new), extend `internal/cli/session_note_test.go`

**Interfaces:**
- Consumes: `notemeta.KV`, `notemeta.Display`, `notemeta.ParseDisplay`, `notemeta.CheckMeta`, `notemeta.Resolve`.
- Produces: `agentdocs.Note.Meta []notemeta.KV`, `.Display notemeta.Display`; `func (s *Store) AddNoteMeta(root, path string, lines []string, start, end int, t NoteText) (Note, error)`; `type NoteText struct{ Summary, Rationale, Author string; Meta []notemeta.KV; Display notemeta.Display }`; `AddNote` keeps its signature and calls `AddNoteMeta`. `steer.Command.Meta map[string]string`, `.Display json.RawMessage`; `steer.FileNote.Meta map[string]string`, `.Display json.RawMessage` (the resolved display, marshalled).

- [ ] **Step 1: Failing tests**

`internal/agentdocs/notes_meta_test.go`:

```go
package agentdocs

import (
	"testing"

	"github.com/homeend/gigagit/internal/notemeta"
)

func TestAddNoteMetaKeepsMetaAndDisplay(t *testing.T) {
	t.Parallel()
	s := NewStore()
	n, err := s.AddNoteMeta("/r", "a.go", []string{"x", "y"}, 1, 1, NoteText{Summary: "s",
		Meta: []notemeta.KV{{Key: "kind", Value: "issue"}}, Display: notemeta.Display{"kind": {Show: []string{"badge"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(n.Meta) != 1 || n.Display["kind"].Show[0] != "badge" {
		t.Fatalf("note = %+v", n)
	}
	w := NoteWire(n, "f1", nil)
	if w.Meta["kind"] != "issue" || len(w.Display) == 0 {
		t.Fatalf("wire = %+v", w)
	}
}
```

(`NewStore` = the constructor the package's tests use; adjust to the real name.)

In `internal/cli/session_note_test.go`, extend the existing note-add test that inspects the steer command written to the inbox: pass `--meta kind=issue --display '{"kind":{"show":["badge"]}}'` and assert the written command JSON has `"meta":{"kind":"issue"}` and a `"display"` object.

- [ ] **Step 2: Run** `go test ./internal/agentdocs/ ./internal/cli/ -run 'NoteMeta|SessionNote'` — FAIL.

- [ ] **Step 3: Implement**
- `steer.go` `Command`: after `Author`:

```go
	// Meta and Display are note_add's free meta and its display
	// declaration (raw JSON: steer knows no gg type).
	Meta    map[string]string `json:"meta,omitempty"`
	Display json.RawMessage   `json:"display,omitempty"`
```

  `FileNote`: `Meta map[string]string \`json:"meta,omitempty"\`` and `Display json.RawMessage \`json:"display,omitempty"\`` (the resolved display).
- `agentdocs/notes.go`: `Note` gains `Meta []notemeta.KV` and `Display notemeta.Display`; add `NoteText`, `AddNoteMeta` (the body of today's `AddNote`, storing `t.Meta` after `notemeta.CheckMeta` and `t.Display`), and make `AddNote(root, path, lines, start, end, summary, rationale, author)` return `s.AddNoteMeta(root, path, lines, start, end, NoteText{Summary: summary, Rationale: rationale, Author: author})`. `NoteWire` fills `Meta` (map from the KV list) and `Display` (`json.Marshal(notemeta.Resolve(n.Meta, n.Display, nil))`, omitted when `Empty()`).
- `cli/session_note.go` `sessionNoteAdd`: `var meta metaFlag; fs.Var(&meta, "meta", …)`, `display := fs.String("display", "", …)`; validate with `checkedMeta("session note add", …)` and `displayArg(…)`; set `c.Meta` (map) and `c.Display` (re-marshalled validated declaration, or the raw bytes when non-empty).
- `tui/steer_file_notes.go` `steerNoteAdd` and `web/steer_notes.go` `steerNoteAdd`: where they call `AddNote(…)`, call `AddNoteMeta(…, agentdocs.NoteText{Summary: c.Summary, Rationale: c.Rationale, Author: c.Author, Meta: kvFrom(c.Meta), Display: d})` with `d, _, _ := notemeta.ParseDisplay(c.Display)` (malformed was refused by the CLI; a hand-written malformed one is ignored here) and `kvFrom` sorting keys. Put `kvFrom` in `agentdocs` as `func MetaFromMap(m map[string]string) []notemeta.KV` so both callers share it.

- [ ] **Step 4: Run** `go test ./internal/agentdocs/ ./internal/steer/ ./internal/cli/ ./internal/tui/ ./internal/web/ ./internal/archtest/ -run 'Note|Steer|Leaf'` — PASS.

- [ ] **Step 5: Commit** — "agentdocs/steer: temporary notes carry meta and a display declaration".

---

### Task 10: Docs, skill text, full gate

**Files:**
- Modify: `CHANGELOG.md`, `CLAUDE.md` (package map: new `notemeta` row; `model`, `notebatch`, `notes`-adjacent rows mention meta in ≤ 1 line each), `docs/CLAUDE-details.md` (a "Note meta" section: the declaration grammar, places, semantics, format 3 + the known limit, added meta on the note), `internal/agentskill/using-gg.md` (`--meta`, `--display`, `gg note meta`, `gg session note --meta`), `internal/agentskill` version constant (+1)

- [ ] **Step 1:** Write the CHANGELOG entry under the unreleased heading:

```markdown
- **Note meta (core).** Every note can carry free `meta` key/values and a
  `display` declaration saying how each key is shown (badge, frame, count,
  line); review documents declare it once for all remarks. `gg note add|reply
  --meta k=v --display <json|@file>`, `gg note meta <id> set|unset|display`,
  the MCP `gg_note_meta` tool, and `gg session note --meta`. Meta never
  triggers anything. Notes files are now format 3 (`key: value` tags and
  the confidence number convert on read); a newer format is read-only.
```

- [ ] **Step 2:** `CLAUDE.md` new row (one line):

```markdown
| `notemeta`   | Pure note meta: free key/values + the author's display declaration (places badge/frame/count/line, fixed colours, safe glyphs, styles; semantics `level`/`audience` that consumers read instead of key names), `ParseDisplay`, `Effective` (inheritance), `Resolve` → what to show, `Count`; meta never triggers anything. Stdlib-only DAG leaf. |
```

- [ ] **Step 3:** `using-gg.md`: add the flags to the `gg note add` / `reply` lines, a `gg note meta` line, `--meta` on `gg session note add`; one sentence: "meta is free; see the review skills for the suggested vocabulary and the display declaration". Bump `agentskill.Version` (`grep -n "Version" internal/agentskill/*.go`).

- [ ] **Step 4:** Run the full gate: `./test.sh race 2>&1 | tee /tmp/claude-1000/-work-gigagit/35aa0a8f-6206-45e1-8e57-18ef8f60ff68/scratchpad/race-plan1.log; tail -5 …/race-plan1.log` — Expected: the log ends with "all green".

- [ ] **Step 5: Commit** — "docs: note meta core — changelog, package map, details, using-gg".

---

## Self-review notes

- Spec §2.2/§2.3 → Tasks 4, 5; §2.4 → Tasks 3, 6; §2.5 → Task 3; §2.6 → Tasks 1, 4, 6, 7; §3 → Tasks 1, 2; §4 → Tasks 1, 2; §5.1 → Tasks 5, 6; §5.2 → Task 4; §5.3 → Tasks 7, 8, 9; §5.4 (Mark menu) → plan 2/3 (the domain call it uses is Task 6); §6–§9 → plans 2–4; §10 → Tasks 3, 6, 7; §11 core rows → each task; §12 → Task 10.
- Types used across tasks: `notemeta.KV/Display/Key/Value/Added/Problem/Resolved/Counts`, `domain.NoteMeta`, `ToWireNoteIn`, `SetNoteMeta(ctx, id, by, kv) ([]Problem, error)`, `UnsetNoteMeta(ctx, id, by, keys) error`, `SetNoteDisplay(ctx, id, d) error`, `agentdocs.NoteText`, `AddNoteMeta`, `MetaFromMap` — consistent.
