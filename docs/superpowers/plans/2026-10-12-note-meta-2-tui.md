# Note meta — plan 2: TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. In THIS repo: executing-plans only, by the session that wrote the plan (CLAUDE.md: no implementer subagents).

**Goal:** The TUI shows what each note is at a glance: badges and an audience marker in the note box title, the declared frame colour, dim `label: value` line rows, a details row with stored meta, added changes and problems; counts in the review view; one meta row in the send panel's expanded row; Mark agreed / disagreed / deferred / clear; a notice when a notes file is newer than this build.

**Architecture:** Everything renders `notemeta.Resolved` (plan 1). One painting mechanism: 14 meta pseudo-classes in the `syntax.Class` space (7 colours × filled/text), resolved in `styles.syntaxStyle` the way the markdown classes are, so a `cls` mask colours badges on a `winRow` (send panel, file rows, Other notes rows) and on a `noteLine` (note box). Domain gains three pure read additions (review counts, Other-note meta, send-candidate meta) and a machine-local preflight probe for notes format > 3. Mark is a use case over `SetNoteMeta` / `UnsetNoteMeta`.

**Tech Stack:** Go 1.26, Bubble Tea, lipgloss, gg's `notemeta`, `domain`, `preflight`, `theme`, `i18n`, e2e golden screens.

**Spec:** `docs/superpowers/specs/2026-10-12-note-meta-design.md` (§3, §5.4, §6, §10, R6, R9, R12–R14). Plan 1 (core, merged `42960c61`): `docs/superpowers/plans/2026-10-12-note-meta-1-core.md`.

## Global Constraints

- A note with no meta renders byte-identically to today (every existing golden is the test). Exception allowed by R13: a note whose meta has NO declaration now shows its meta as `line` rows (e.g. a remark's `severity: low` that used to be a ` · severity: low` title suffix).
- Meta and marks are glance aids: nothing is sent, ticked, filtered, folded, blocked or resolved because of a meta value (R12). Mark only writes the tag and reloads.
- Send panel (R9): nothing ticked on open, no filter, no dimming, no new key; meta ONLY in the expanded (→) row.
- Paths are never end-cut: a too-wide title elides the path in the middle first (`elidePath`), then cuts today's parts, the badges last.
- Glyphs: only notemeta's safe set reaches the screen (Resolve already drops others); measure every width with `lipgloss.Width`; keep a space after `◆ · → » •` (East-Asian-ambiguous).
- Every gg-owned word goes through `i18n.T` with a literal key present in ja/ko/zh/ru (`→ author`, `for you`, `agreed`, `disagreed`, `deferred`, `clear`, `Mark…`, count phrasing, details prefixes, the notice). Agent keys and values are data: verbatim, sanitized (`sanitizeLine`), never translated — except the three built-in `status` values, which are gg's words.
- Theme roles `meta_red`, `meta_orange`, `meta_yellow`, `meta_green`, `meta_blue`, `meta_purple`, `meta_grey` for Dark, Light and Terminal; overridable under `[themes.<name>]`.
- Style `filled` = background in the colour, foreground = the theme's `bg` (Terminal: ANSI 0); style `text` = foreground in the colour.
- Stale box: dim wins (no meta frame colour, badges painted in the stale frame style).
- `go vet`: always keyed `notemeta.KV{Key: …, Value: …}` outside package notemeta.
- Tests: TDD, real git, `t.Parallel()` where the helpers allow; `./test.sh race` must end "all green".

## Review Focus

1. **Degenerate widths** — a pane narrower than the badges (`innerW == 0`, `paneW-6 < badge width`): the title fit must never panic, never produce a row wider than the pane, and must cut the badges only after today's parts are gone. Test in Task 4 (`TestFitNoteTitleNarrow`).
2. **A mark on a review remark must show after reload** — the review diff's notes come from `ReviewNotesFor` (re-reads the review, `Added` lands through `remarkMeta`); a reload path that served a cached `ResolvedNote` would show a stale box. Test in Task 8 (`TestMarkRemarkShowsAfterReload`).
3. **A note with meta but no declaration** (every converted tag, an agent that writes meta only) shows `line` rows and no badges, no frame colour — and the old title suffix is gone. Test in Task 4 (`TestNoteBoxUndeclaredMetaIsLines`).
4. **The cursor row reverses video**, so a `cls` mask is dropped there (renderWindow rule): badges go plain on the selected send-panel / file row — the text must still read correctly (no stray mask on shifted runes). Test in Task 7 (`TestSendPanelExpandedMetaRowSelected`).
5. **A reply under a declared root** uses the root's declaration (domain.NoteMeta(rep, &root)); a reply's own `kind` shows as a coloured badge row under the reply, not in the root's title. Test in Task 4 (`TestNoteBoxReplyUsesRootDisplay`).

---

## File Structure

- `internal/theme/theme.go`, `internal/theme/override.go` — 7 roles (fields, `roles()`, toml tags, `roleFields`, Dark/Light values).
- `internal/tui/styles.go` — `legacy` Terminal values; `metaFg [7]lipgloss.Color`, `metaFillFg lipgloss.Color`.
- `internal/tui/note_meta.go` (new) — meta pseudo-classes, `metaStyle`, badge runs, audience marker, line/details row texts, count texts, `fitNoteTitle`.
- `internal/tui/md_render.go` — `syntaxStyle` dispatch reaches `metaStyle` (one line).
- `internal/domain/note_meta.go` — `ReviewMetaCounts`.
- `internal/domain/review_doc.go` — `ReviewOtherNote.Meta`.
- `internal/domain/forge_send_candidates.go` — `SendCandidate.Meta`.
- `internal/agentdocs/notes.go` — `Note.Added` (memory only).
- `internal/tui/diff_notes.go`, `internal/tui/diff_render.go` — note box rows and painting.
- `internal/tui/open_file_notes.go`, `internal/tui/review_remarks.go` — temporary notes and remark boxes in previews.
- `internal/tui/review_view.go`, `internal/tui/files_view.go` — header counts, file-row counts, Other notes rows.
- `internal/tui/send_panel.go` — expanded meta row.
- `internal/tui/note_mark.go` (new) — Mark use case.
- `internal/preflight/preflight.go`, `internal/domain/features.go`, `internal/domain/preflight.go` — format > 3 notice.
- `internal/i18n/lang/{ja,ko,zh,ru}.toml` — every new key.
- `e2e/scenarios/tui_note_meta.toml`, `e2e/scenarios/tui_note_meta_send.toml` (+ `.screens/`).
- Docs: `CHANGELOG.md`, `README.md` (theme roles line), `docs/CLAUDE-details.md`.

---

### Task 1: Theme roles and the meta paint classes

**Files:**
- Modify: `internal/theme/theme.go` (Theme fields after `NoteGroup6`, `roles()`, Dark, Light)
- Modify: `internal/theme/override.go` (Override fields + `roleFields`)
- Modify: `internal/tui/styles.go` (`legacy`, `styles` fields, `buildStyles`)
- Create: `internal/tui/note_meta.go`
- Modify: `internal/tui/md_render.go` (or wherever `syntaxStyle` lives: `styles.go`) — dispatch
- Test: `internal/theme/override_test.go`, `internal/tui/note_meta_test.go`

**Interfaces:**
- Produces: `theme.Theme.MetaRed … MetaGrey string`; toml keys `meta_red … meta_grey`.
- Produces (tui): `metaClass(color, style string) syntax.Class` (0 = none for an unknown colour), `(*styles).metaStyle(base lipgloss.Style, c syntax.Class) lipgloss.Style`, consts `metaClassStart`, `metaClassEnd`.

- [ ] **Step 1: Failing tests**

`internal/theme/override_test.go`:
```go
func TestOverrideMetaRoles(t *testing.T) {
	t.Parallel()
	o := Override{MetaRed: "#FF0000", MetaGrey: "245"}
	out, bad := o.Overlay(Dark)
	if len(bad) > 0 {
		t.Fatalf("bad = %v", bad)
	}
	if out.MetaRed != "#FF0000" || out.MetaGrey != "245" || out.MetaBlue != Dark.MetaBlue {
		t.Fatalf("overlay = %+v", out)
	}
	for _, k := range []string{"meta_red", "meta_orange", "meta_yellow", "meta_green", "meta_blue", "meta_purple", "meta_grey"} {
		found := false
		for _, d := range RoleDocs() {
			if d.Key == k {
				found = true
			}
		}
		if !found {
			t.Errorf("RoleDocs lacks %s", k)
		}
	}
	for _, th := range []Theme{Dark, Light} {
		for _, c := range []string{th.MetaRed, th.MetaOrange, th.MetaYellow, th.MetaGreen, th.MetaBlue, th.MetaPurple, th.MetaGrey} {
			if c == "" || !ValidColour(c) {
				t.Errorf("%s: meta colour %q", th.Name, c)
			}
		}
	}
}
```
(Read `Overlay`'s and `RoleDocs`' real signatures first — adapt the calls, keep the assertions.)

`internal/tui/note_meta_test.go`:
```go
func TestMetaClassPaints(t *testing.T) {
	t.Parallel()
	s := buildStyles(theme.Dark)
	base := lipgloss.NewStyle()
	if metaClass("crimson", notemeta.StyleText) != 0 {
		t.Fatal("unknown colour must be class 0")
	}
	txt := s.metaStyle(base, metaClass("red", notemeta.StyleText))
	if txt.GetForeground() != lipgloss.Color(theme.Dark.MetaRed) {
		t.Fatalf("text fg = %v", txt.GetForeground())
	}
	fill := s.metaStyle(base, metaClass("green", notemeta.StyleFilled))
	if fill.GetBackground() != lipgloss.Color(theme.Dark.MetaGreen) || fill.GetForeground() != lipgloss.Color(theme.Dark.Bg) {
		t.Fatalf("filled = bg %v fg %v", fill.GetBackground(), fill.GetForeground())
	}
	// syntaxStyle routes the meta classes (one mask paints everywhere).
	if s.syntaxStyle(base, metaClass("red", notemeta.StyleText)).GetForeground() != lipgloss.Color(theme.Dark.MetaRed) {
		t.Fatal("syntaxStyle does not reach metaStyle")
	}
	term := buildStyles(theme.Terminal)
	if term.metaStyle(base, metaClass("orange", notemeta.StyleText)).GetForeground() == term.metaStyle(base, metaClass("yellow", notemeta.StyleText)).GetForeground() {
		t.Fatal("Terminal orange and yellow must differ")
	}
}
```

- [ ] **Step 2: Run, watch them fail**

Run: `go test ./internal/theme/ ./internal/tui/ -run 'TestOverrideMetaRoles|TestMetaClassPaints' -count=1`
Expected: compile errors (no `MetaRed`, no `metaClass`).

- [ ] **Step 3: Implement**

`theme.go`, after the NoteGroup block:
```go
	// Meta colours (spec 2026-10-12-note-meta §6.4): the seven colour names a
	// note's display declaration may use — badge chips, the box frame, the
	// counts' numbers.
	MetaRed, MetaOrange, MetaYellow, MetaGreen, MetaBlue, MetaPurple, MetaGrey string
```
`roles()` gains `t.MetaRed, t.MetaOrange, t.MetaYellow, t.MetaGreen, t.MetaBlue, t.MetaPurple, t.MetaGrey,` after the NoteGroup row.
Dark: `MetaRed: "#E06C75", MetaOrange: "#E5964B", MetaYellow: "#E5C07B", MetaGreen: "#98C379", MetaBlue: "#61AFEF", MetaPurple: "#C678DD", MetaGrey: "#7F848E",`
Light: `MetaRed: "#B23A48", MetaOrange: "#B35C1E", MetaYellow: "#A07400", MetaGreen: "#3E7D2E", MetaBlue: "#2F6FB8", MetaPurple: "#7D3C98", MetaGrey: "#6B6F73",`

`override.go`: fields `MetaRed string `toml:"meta_red"`` … and seven `roleFields` rows after `note_group_6`:
```go
	{"meta_red", "note meta colour red (badges, frame, counts)", func(t *Theme) *string { return &t.MetaRed }, func(o *Override) *string { return &o.MetaRed }},
	{"meta_orange", "note meta colour orange", …MetaOrange…},
	{"meta_yellow", "note meta colour yellow", …},
	{"meta_green", "note meta colour green", …},
	{"meta_blue", "note meta colour blue", …},
	{"meta_purple", "note meta colour purple", …},
	{"meta_grey", "note meta colour grey", …},
```
(each row written out in full in the code.)

`styles.go` `legacy` (Terminal): `MetaRed: "9", MetaOrange: "208", MetaYellow: "11", MetaGreen: "10", MetaBlue: "12", MetaPurple: "13", MetaGrey: "245",` (ANSI 16 where one exists; orange and grey have none, so the 256-cube entries the legacy table already uses elsewhere). `styles` gains:
```go
	// note_meta.go: the seven meta colours (index = metaColours order) and
	// the text a filled badge wears over them.
	metaFg     [7]lipgloss.Color
	metaFillFg lipgloss.Color
```
`buildStyles`:
```go
	for i, c := range [7][2]string{
		{th.MetaRed, legacy.MetaRed}, {th.MetaOrange, legacy.MetaOrange}, {th.MetaYellow, legacy.MetaYellow},
		{th.MetaGreen, legacy.MetaGreen}, {th.MetaBlue, legacy.MetaBlue}, {th.MetaPurple, legacy.MetaPurple},
		{th.MetaGrey, legacy.MetaGrey},
	} {
		s.metaFg[i] = pick(c[0], c[1])
	}
	s.metaFillFg = pick(th.Bg, "0")
```

`note_meta.go`:
```go
package tui

// The TUI's rendering of note meta (spec 2026-10-12-note-meta §6): every
// surface paints a notemeta.Resolved through ONE mechanism — the meta
// pseudo-classes below ride a syntax.Class mask (like the markdown classes),
// so a badge is coloured the same on a note box, a send-panel row or a file
// row. Agent keys and values are data: shown verbatim, never translated.

// metaColours is the declaration's colour vocabulary in class order.
var metaColours = [7]string{"red", "orange", "yellow", "green", "blue", "purple", "grey"}

// The meta pseudo-classes: text style first (7), then filled (7).
const (
	metaClassStart syntax.Class = 200
	metaClassEnd   syntax.Class = metaClassStart + 14
)

// metaClass is colour in style as a mask class; 0 (plain) for a colour
// outside the vocabulary.
func metaClass(color, style string) syntax.Class {
	for i, c := range metaColours {
		if c == color {
			if style == notemeta.StyleFilled {
				return metaClassStart + 7 + syntax.Class(i)
			}
			return metaClassStart + syntax.Class(i)
		}
	}
	return 0
}

// metaStyle paints c over base: a text class colours the foreground, a
// filled one the background with the theme's ground as text.
func (s *styles) metaStyle(base lipgloss.Style, c syntax.Class) lipgloss.Style {
	i := int(c - metaClassStart)
	if i >= 7 {
		return base.Background(s.metaFg[i-7]).Foreground(s.metaFillFg)
	}
	return base.Foreground(s.metaFg[i])
}
```
`syntaxStyle` gains, before the markdown branch:
```go
	if c >= metaClassStart && c < metaClassEnd {
		return s.metaStyle(base, c)
	}
```
Check `notemeta.StyleFilled` / `StyleText` constant names in `internal/notemeta/display.go` and use them.

- [ ] **Step 4: Run** — same command, PASS; then `go test ./internal/theme/ ./internal/tui/ -count=1` (role-count tests elsewhere may need the new roles listed — update them, never delete assertions).

- [ ] **Step 5: Commit**
```bash
git add internal/theme internal/tui/styles.go internal/tui/note_meta.go internal/tui/note_meta_test.go internal/tui/md_render.go
git commit -m "feat(tui): meta colour theme roles and one meta paint class set"
```

---

### Task 2: Meta texts — badge runs, audience marker, line/details rows, counts

**Files:**
- Modify: `internal/tui/note_meta.go`
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Test: `internal/tui/note_meta_test.go`

**Interfaces:**
- Consumes: `metaClass` (Task 1), `notemeta.Resolved/Badge/Line/Change/Counts`.
- Produces:
  - `badgeRun(res notemeta.Resolved) (string, []syntax.Class)` — badges then the audience marker; "" when neither.
  - `audienceMarker(a string) string` — `→ author` / `for you` / "".
  - `metaLineTexts(res notemeta.Resolved) []string` — `label: value`, one per `Lines` entry.
  - `metaDetailTexts(res notemeta.Resolved) []string` — `meta: k: v · k: v` (Stored), `added: k: from → to (by) · …` (Added), `display problem: …` per problem.
  - `metaCountsText(c notemeta.Counts) string` — `3 issue · 2 good · 5 for author · 9 for you`.
  - `metaCountsRun(c notemeta.Counts) (string, []syntax.Class)` — compact `issue 1 good 1`, the number in the value's colour (text style).

- [ ] **Step 1: Failing tests**
```go
func TestBadgeRun(t *testing.T) {
	t.Parallel()
	res := notemeta.Resolved{
		Badges: []notemeta.Badge{
			{Key: "kind", Value: "issue", Color: "red", Style: notemeta.StyleFilled},
			{Key: "severity", Value: "high", Color: "orange", Style: notemeta.StyleText, Glyph: "!"},
		},
		Audience: "author",
	}
	text, cls := badgeRun(res)
	if text != " issue  ! high  → author" {
		t.Fatalf("text = %q", text)
	}
	r := []rune(text)
	if len(cls) != len(r) {
		t.Fatalf("mask %d runes, text %d", len(cls), len(r))
	}
	// The filled chip's padding wears the fill; the text badge only its runes.
	if cls[0] != metaClass("red", notemeta.StyleFilled) || cls[6] != metaClass("red", notemeta.StyleFilled) {
		t.Fatalf("filled chip mask = %v", cls[:7])
	}
	if cls[7] != 0 || cls[8] != metaClass("orange", notemeta.StyleText) {
		t.Fatalf("gap/text mask = %v", cls[7:14])
	}
	if text, _ := badgeRun(notemeta.Resolved{}); text != "" {
		t.Fatalf("empty = %q", text)
	}
	// The built-in status values are gg's words; any other value is data.
	st, _ := badgeRun(notemeta.Resolved{Badges: []notemeta.Badge{{Key: notemeta.StatusKey, Value: "agreed", Color: "green", Style: notemeta.StyleText}}})
	if st != i18n.T("agreed") {
		t.Fatalf("status = %q", st)
	}
}

func TestAudienceMarker(t *testing.T) {
	t.Parallel()
	if audienceMarker("author") != i18n.T("→ author") || audienceMarker("reviewer") != i18n.T("for you") || audienceMarker("") != "" {
		t.Fatal("markers")
	}
}

func TestMetaLineAndDetailTexts(t *testing.T) {
	t.Parallel()
	res := notemeta.Resolved{
		Lines:    []notemeta.Line{{Label: "intent", Value: "checked\nthe lock"}},
		Stored:   []notemeta.KV{{Key: "model", Value: "opus"}, {Key: "run", Value: "3"}},
		Added:    []notemeta.Change{{Key: "severity", From: "high", To: "low", By: "MR"}, {Key: "status", To: "agreed", By: "MR"}},
		Problems: []notemeta.Problem{`display.kind.values.issue.color: "crimson" is not a colour`},
	}
	if got := metaLineTexts(res); len(got) != 1 || got[0] != "intent: checked the lock" {
		t.Fatalf("lines = %q", got)
	}
	got := metaDetailTexts(res)
	want := []string{
		i18n.T("meta: %s", "model: opus · run: 3"),
		i18n.T("added: %s", "severity: high → low (MR) · status: agreed (MR)"),
		i18n.T("display problem: %s", `display.kind.values.issue.color: "crimson" is not a colour`),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("details = %q", got)
	}
}

func TestMetaCounts(t *testing.T) {
	t.Parallel()
	c := notemeta.Counts{
		Groups: []notemeta.CountGroup{{Key: "kind", Label: "kind", Values: []notemeta.CountValue{{Value: "issue", Color: "red", N: 3}, {Value: "good", Color: "green", N: 8}}}},
		Author: 5, Reviewer: 9,
	}
	if got := metaCountsText(c); got != "3 issue · 8 good · "+i18n.T("%d for author", 5)+" · "+i18n.T("%d for you", 9) {
		t.Fatalf("text = %q", got)
	}
	run, cls := metaCountsRun(c)
	if run != "issue 3 good 8" {
		t.Fatalf("run = %q", run)
	}
	if cls[6] != metaClass("red", notemeta.StyleText) || cls[0] != 0 {
		t.Fatalf("mask = %v", cls)
	}
	if metaCountsText(notemeta.Counts{}) != "" {
		t.Fatal("empty counts must say nothing")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/tui/ -run 'TestBadgeRun|TestAudienceMarker|TestMetaLineAndDetailTexts|TestMetaCounts' -count=1` — FAIL (undefined).

- [ ] **Step 3: Implement** in `note_meta.go`:
```go
// badgeValue is what a badge says: the value verbatim (data), except the
// built-in status values, which are gg's own words.
func badgeValue(b notemeta.Badge) string {
	if b.Key == notemeta.StatusKey {
		switch b.Value {
		case "agreed":
			return i18n.T("agreed")
		case "disagreed":
			return i18n.T("disagreed")
		case "deferred":
			return i18n.T("deferred")
		}
	}
	return sanitizeLine(b.Value)
}

// badgeRun is a note's badges then its audience marker, one space apart
// (a filled chip carries one column of fill each side), with the mask that
// colours them. "" when the note shows neither.
func badgeRun(res notemeta.Resolved) (string, []syntax.Class) {
	var b strings.Builder
	var cls []syntax.Class
	add := func(s string, c syntax.Class) {
		b.WriteString(s)
		for range []rune(s) {
			cls = append(cls, c)
		}
	}
	for i, bd := range res.Badges {
		if i > 0 {
			add(" ", 0)
		}
		v := badgeValue(bd)
		if bd.Glyph != "" {
			v = bd.Glyph + " " + v
		}
		c := metaClass(bd.Color, bd.Style)
		if bd.Style == notemeta.StyleFilled {
			v = " " + v + " "
		}
		add(v, c)
	}
	if m := audienceMarker(res.Audience); m != "" {
		if b.Len() > 0 {
			add("  ", 0)
		}
		add(m, 0)
	}
	return b.String(), cls
}
```
(Adjust spacing so the test's exact string holds: filled chip `" issue "`, one space, text badge `"! high"`, two spaces, marker.)
```go
func audienceMarker(a string) string {
	switch a {
	case "author":
		return i18n.T("→ author")
	case "reviewer":
		return i18n.T("for you")
	}
	return ""
}

func metaLineTexts(res notemeta.Resolved) []string {
	out := make([]string, 0, len(res.Lines))
	for _, l := range res.Lines {
		out = append(out, sanitizeLine(l.Label+": "+l.Value))
	}
	return out
}

func metaDetailTexts(res notemeta.Resolved) []string {
	var out []string
	if len(res.Stored) > 0 {
		parts := make([]string, len(res.Stored))
		for i, kv := range res.Stored {
			parts[i] = sanitizeLine(kv.Key + ": " + kv.Value)
		}
		out = append(out, i18n.T("meta: %s", strings.Join(parts, " · ")))
	}
	if len(res.Added) > 0 {
		parts := make([]string, len(res.Added))
		for i, c := range res.Added {
			v := c.To
			if c.From != "" {
				v = c.From + " → " + c.To
			}
			if c.By != "" {
				v += " (" + c.By + ")"
			}
			parts[i] = sanitizeLine(c.Key + ": " + v)
		}
		out = append(out, i18n.T("added: %s", strings.Join(parts, " · ")))
	}
	for _, p := range res.Problems {
		out = append(out, i18n.T("display problem: %s", sanitizeLine(string(p))))
	}
	return out
}

func metaCountsText(c notemeta.Counts) string {
	var parts []string
	for _, g := range c.Groups {
		for _, v := range g.Values {
			parts = append(parts, strconv.Itoa(v.N)+" "+sanitizeLine(v.Value))
		}
	}
	if c.Author > 0 {
		parts = append(parts, i18n.T("%d for author", c.Author))
	}
	if c.Reviewer > 0 {
		parts = append(parts, i18n.T("%d for you", c.Reviewer))
	}
	return strings.Join(parts, " · ")
}

func metaCountsRun(c notemeta.Counts) (string, []syntax.Class) {
	var b strings.Builder
	var cls []syntax.Class
	add := func(s string, k syntax.Class) {
		b.WriteString(s)
		for range []rune(s) {
			cls = append(cls, k)
		}
	}
	for _, g := range c.Groups {
		for _, v := range g.Values {
			if b.Len() > 0 {
				add(" ", 0)
			}
			add(sanitizeLine(v.Value)+" ", 0)
			add(strconv.Itoa(v.N), metaClass(v.Color, notemeta.StyleText))
		}
	}
	return b.String(), cls
}
```
Bundles (use the adding-translations skill): `"→ author"`, `"for you"`, `"agreed"`, `"disagreed"`, `"deferred"`, `"meta: %s"`, `"added: %s"`, `"display problem: %s"`, `"%d for author"`, `"%d for you"` in all four files.

- [ ] **Step 4: Run** the Step 2 command, then `go test ./internal/tui/ -run 'I18n|Vocab|Menu' -count=1` (the i18n gates). PASS.

- [ ] **Step 5: Commit** `git commit -m "feat(tui): meta badge runs, line/details texts, counts"` (add the files explicitly).

---

### Task 3: Domain — review counts, Other-note meta, send-candidate meta, remark boxes keep added meta

**Files:**
- Modify: `internal/domain/note_meta.go` (`ReviewMetaCounts`)
- Modify: `internal/domain/review_doc.go` (`ReviewOtherNote.Meta`, filled in `reviewSplit`)
- Modify: `internal/domain/forge_send_candidates.go` (`SendCandidate.Meta`)
- Modify: `internal/agentdocs/notes.go` (`Note.Added`)
- Test: `internal/domain/note_meta_test.go`, `internal/domain/forge_send_candidates_test.go`, `internal/domain/review_doc_test.go`

**Interfaces:**
- Produces:
  - `func ReviewMetaCounts(r Review) (all notemeta.Counts, byFile map[string]notemeta.Counts)` — every remark GitHub does not own (not `remarkMoved`), resolved with `remarkMeta`; byFile keyed by `reviewPath(f.Path)`.
  - `ReviewOtherNote.Meta notemeta.Resolved`, `SendCandidate.Meta notemeta.Resolved`.
  - `agentdocs.Note.Added []notemeta.Added`.

- [ ] **Step 1: Failing tests** (use the plan-1 helpers `reviewRepo`, `commitFile`, `SaveReview`, `remarkID`; read `note_meta_test.go` for their exact shapes):
```go
func TestReviewMetaCounts(t *testing.T) {
	t.Parallel()
	s, ctx, rev := reviewRepo(t) // adapt to the helper's real return values
	doc := `{"version":1,"summary":"s","display":{"kind":{"show":["badge","count"],"values":{"issue":{"color":"red","audience":"author"},"good":{"color":"green","audience":"reviewer"}}}},
	 "files":[{"path":"a.txt","annotations":[
	   {"newRange":[1,1],"summary":"one","meta":{"kind":"issue"}},
	   {"newRange":[2,2],"summary":"two","meta":{"kind":"good"}}]},
	  {"path":"b.txt","annotations":[{"newRange":[1,1],"summary":"three","meta":{"kind":"issue"}}]}]}`
	id := saveReviewDoc(t, s, ctx, rev, doc)
	r, err := s.Review(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	all, byFile := ReviewMetaCounts(r)
	if all.Author != 2 || all.Reviewer != 1 || len(all.Groups) != 1 || all.Groups[0].Values[0].N != 2 {
		t.Fatalf("all = %+v", all)
	}
	if byFile["a.txt"].Author != 1 || byFile["b.txt"].Groups[0].Values[0].Value != "issue" {
		t.Fatalf("byFile = %+v", byFile)
	}
	// Added meta counts too: a mark changes what the counts say.
	if _, err := s.SetNoteMeta(ctx, remarkID(id, 1), "MR", []notemeta.KV{{Key: "kind", Value: "issue"}}); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Review(ctx, id)
	all, _ = ReviewMetaCounts(r)
	if all.Groups[0].Values[0].N != 3 {
		t.Fatalf("after mark = %+v", all)
	}
}

func TestSendCandidateCarriesMeta(t *testing.T) {
	// Copy the setup of the nearest existing PRSendCandidates test (fake gh,
	// refs/gg/pr/<n>), save a review whose remark has meta kind=issue and the
	// kind declaration above, then:
	//   c := cands.Groups[0].Rows[0]
	//   c.Meta.Badges[0].Value == "issue" && c.Meta.Audience == "author"
	//   c.Severity unchanged (still MetaValue "severity")
}

func TestReviewOtherNoteCarriesMeta(t *testing.T) {
	// A remark on a path the review's commit does not change lands in
	// ReviewOtherNotes; its Meta.Badges[0].Value == "issue".
}
```
Write the two sketched tests in full from the nearest existing tests in those files (`grep -n 'func Test.*PRSendCandidates\|func Test.*ReviewOtherNotes' internal/domain/*_test.go`), keeping the assertions above.

- [ ] **Step 2: Run** `go test ./internal/domain/ -run 'TestReviewMetaCounts|TestSendCandidateCarriesMeta|TestReviewOtherNoteCarriesMeta' -count=1` — FAIL (undefined).

- [ ] **Step 3: Implement**

`note_meta.go`:
```go
// ReviewMetaCounts tallies review r's remarks (spec §6.2): over the whole
// review and per file (keyed like ReviewFileCounts). A remark GitHub owns
// now is not the review's to count. Added meta counts: a mark is meta.
func ReviewMetaCounts(r Review) (notemeta.Counts, map[string]notemeta.Counts) {
	if r.Doc == nil {
		return notemeta.Counts{}, nil
	}
	var all []notemeta.Resolved
	per := map[string][]notemeta.Resolved{}
	for _, f := range r.Doc.Files {
		p := reviewPath(f.Path)
		for _, dn := range f.Notes {
			fp := remarkFP(f.Path, reviewSide(dn.Side), dn.Range, dn.Summary)
			if r.remarkMoved(fp) {
				continue
			}
			var n model.Note
			r.remarkMeta(&n, f, dn, fp)
			res := NoteMeta(n, nil)
			all = append(all, res)
			per[p] = append(per[p], res)
		}
	}
	byFile := make(map[string]notemeta.Counts, len(per))
	for p, rs := range per {
		byFile[p] = notemeta.Count(rs)
	}
	return notemeta.Count(all), byFile
}
```
`review_doc.go`: `ReviewOtherNote` gains
```go
	// Meta is the remark's resolved meta (spec 2026-10-12-note-meta §6.2:
	// a remark row wears its badges); zero for an outdated thread.
	Meta notemeta.Resolved
```
In `reviewSplit`, at both `other = append(other, ReviewOtherNote{…})` sites for a live remark, compute once per remark before the branches:
```go
			fp := remarkFP(f.Path, side, dn.Range, dn.Summary)
			if r.remarkMoved(fp) {
				continue
			}
			var mn model.Note
			r.remarkMeta(&mn, f, dn, fp)
			meta := NoteMeta(mn, nil)
```
and set `Meta: meta` in both literals (the existing `remarkMoved(remarkFP(…))` call becomes `remarkMoved(fp)`).

`forge_send_candidates.go`: `SendCandidate` gains `Meta notemeta.Resolved // what the expanded row shows (spec §6.3)`; in `candidateRow`: `row.Meta = NoteMeta(n, nil)` (a remark's note carries its effective declaration and added meta already — `remarkMeta` ran in PRNotes; a draft reply has no root here and shows its own).

`agentdocs/notes.go`: `Note` gains
```go
	// Added is meta written onto a review remark after its agent wrote it
	// (a mark): set only on the remark boxes a preview draws.
	Added []notemeta.Added
```

- [ ] **Step 4: Run** Step 2's command, then `go test ./internal/domain/ ./internal/agentdocs/ -count=1`. PASS.

- [ ] **Step 5: Commit** `git commit -m "feat(domain): review meta counts; meta on other notes and send candidates"`.

---

### Task 4: The note box — badges, frame, line rows, details, replies, collapsed rows

**Files:**
- Modify: `internal/tui/diff_notes.go` (`noteLine`, `noteRowMeta`, `noteBoxLines`, `noteBoxTitle` — remove the transitional suffix, `collapsedNoteLine`, a new `noteMetaRows`)
- Modify: `internal/tui/diff_render.go` (`noteBoxCell`)
- Modify: `internal/tui/note_meta.go` (`fitNoteTitle`)
- Test: `internal/tui/note_meta_box_test.go` (new)

**Interfaces:**
- Consumes: `badgeRun`, `metaLineTexts`, `metaDetailTexts`, `metaClass` (Tasks 1–2); `domain.NoteMeta`.
- Produces:
  - `noteLine` fields `badges string`, `badgeCls []syntax.Class`, `path string`, `metaFrame string`; kind `noteRowMeta` (dim, appended at the END of the const list so existing values hold).
  - `fitNoteTitle(badges string, cls []syntax.Class, text, path string, room int) (string, []syntax.Class, string)`.
  - `noteMetaRows(res notemeta.Resolved, lines, details, badges bool, innerW int, indent string, mk func(noteRowKind, string) noteLine) []noteLine` — reused by Task 5.

- [ ] **Step 1: Failing tests** (`note_meta_box_test.go`; build `domain.ResolvedNote` values by hand and a `diffView` the way the existing `diff_notes_test.go` box tests do — read one first):
```go
func declaredIssue() (model.Note, notemeta.Display) {
	d := notemeta.Display{
		"kind":     {Show: []string{"badge"}, Order: 1, Values: map[string]notemeta.Value{"issue": {Color: "red", Audience: "author"}}},
		"severity": {Show: []string{"badge", "frame"}, Order: 2, Style: notemeta.StyleText, Values: map[string]notemeta.Value{"high": {Color: "red"}}},
		"intent":   {Show: []string{"line"}, Order: 4},
	}
	n := model.Note{ID: "n1", Source: model.NoteSourceAgent, Author: "Claude Code", Side: model.NoteSideNew, Summary: "cached forever",
		Meta: []notemeta.KV{{Key: "kind", Value: "issue"}, {Key: "severity", Value: "high"}, {Key: "intent", Value: "checked the cache key"}, {Key: "model", Value: "opus"}},
		Display: d}
	return n, d
}

func TestNoteBoxMetaRows(t *testing.T) {
	t.Parallel()
	n, _ := declaredIssue()
	v := &diffView{noteAddr: model.FileAddress{Path: "src/config/configuration.ts"}}
	rows := v.noteBoxLines(domain.ResolvedNote{Note: n, Range: [2]int{12, 14}}, 70, v)
	top := rows[0]
	if !strings.HasPrefix(top.badges, " issue  high  "+i18n.T("→ author")) && top.badges != " issue  high  "+i18n.T("→ author") {
		t.Fatalf("badges = %q", top.badges)
	}
	if strings.Contains(top.text, "kind:") || strings.Contains(top.text, "issue") {
		t.Fatalf("meta leaked into the title text: %q", top.text)
	}
	if top.path != "src/config/configuration.ts" || top.metaFrame != "red" {
		t.Fatalf("path %q frame %q", top.path, top.metaFrame)
	}
	var meta []string
	for _, r := range rows {
		if r.kind == noteRowMeta {
			meta = append(meta, r.text)
		}
		if r.metaFrame != "red" {
			t.Fatalf("row %v lacks the frame colour", r.kind)
		}
	}
	want := []string{"intent: checked the cache key", i18n.T("meta: %s", "model: opus")}
	if !reflect.DeepEqual(meta, want) {
		t.Fatalf("meta rows = %q", meta)
	}
	// Order: top, blank, the line row, the summary … the details row, blank, bottom.
	if rows[2].kind != noteRowMeta || rows[3].kind != noteRowSummary || rows[len(rows)-3].kind != noteRowMeta {
		t.Fatalf("order = %v", kinds(rows))
	}
}

func TestNoteBoxUndeclaredMetaIsLines(t *testing.T) {
	t.Parallel()
	n := model.Note{ID: "review:r1:0", Source: model.NoteSourceAgent, Author: "Claude Code", Summary: "name it",
		Meta: []notemeta.KV{{Key: "severity", Value: "low"}}}
	v := &diffView{noteAddr: model.FileAddress{Path: "big.txt"}}
	rows := v.noteBoxLines(domain.ResolvedNote{Note: n, Range: [2]int{5, 5}}, 70, v)
	if rows[0].badges != "" || rows[0].metaFrame != "" || strings.Contains(rows[0].text, "severity") {
		t.Fatalf("top = %+v", rows[0])
	}
	if rows[2].kind != noteRowMeta || rows[2].text != "severity: low" {
		t.Fatalf("line row = %+v", rows[2])
	}
}

func TestNoteBoxNoMetaUnchanged(t *testing.T) {
	t.Parallel()
	n := model.Note{ID: "n2", Source: model.NoteSourceUser, Author: "MR", Summary: "why?"}
	v := &diffView{noteAddr: model.FileAddress{Path: "a.txt"}}
	rows := v.noteBoxLines(domain.ResolvedNote{Note: n, Range: [2]int{1, 1}}, 70, v)
	for _, r := range rows {
		if r.kind == noteRowMeta || r.badges != "" || r.metaFrame != "" {
			t.Fatalf("a note without meta grew meta rows: %+v", r)
		}
	}
	// The painted box is exactly today's: frame in the user colour, one Render.
	cell := noteBoxCell(rows[0], 40)
	if cell != st().noteFrameUser.Render("╭")+st().noteFrameUser.Render("─ note · MR · a.txt R1 "+strings.Repeat("─", 40-4-len("note · MR · a.txt R1")-1)+"╮") {
		t.Fatalf("top cell = %q", cell)
	}
}

func TestNoteBoxReplyUsesRootDisplay(t *testing.T) {
	t.Parallel()
	root, _ := declaredIssue()
	rep := model.Note{ID: "n1r", ParentID: "n1", Source: model.NoteSourceAgent, Author: "Codex", Summary: "agreed", Meta: []notemeta.KV{{Key: "kind", Value: "issue"}}}
	v := &diffView{noteAddr: model.FileAddress{Path: "a.txt"}}
	rows := v.noteBoxLines(domain.ResolvedNote{Note: root, Range: [2]int{1, 1}, Replies: []domain.ResolvedNote{{Note: rep}}}, 70, v)
	var repBadge *noteLine
	for i := range rows {
		if rows[i].id == "n1r" && rows[i].kind == noteRowMeta && rows[i].cls != nil {
			repBadge = &rows[i]
		}
	}
	if repBadge == nil || !strings.Contains(repBadge.text, "issue") || repBadge.cls[strings.Index(repBadge.text, "issue")] != metaClass("red", notemeta.StyleFilled) {
		t.Fatalf("reply badge row = %+v", repBadge)
	}
}

func TestFitNoteTitle(t *testing.T) {
	t.Parallel()
	b, cls := " issue ", []syntax.Class{1, 1, 1, 1, 1, 1, 1}
	text, path := "review · Claude Code · packages/babel-core/src/config/files/configuration.ts R12-14", "packages/babel-core/src/config/files/configuration.ts"
	// Fits: untouched.
	if gb, _, gt := fitNoteTitle(b, cls, text, path, 200); gb != b || gt != text {
		t.Fatal("a fitting title changed")
	}
	// Too wide: the path loses its middle first, the file name stays.
	gb, _, gt := fitNoteTitle(b, cls, text, path, 60)
	if gb != b || !strings.Contains(gt, "configuration.ts R12-14") || !strings.HasPrefix(gt, "review · Claude Code · ") || lipgloss.Width(gb)+1+lipgloss.Width(gt) > 60 {
		t.Fatalf("elided = %q %q", gb, gt)
	}
	// Narrower: today's parts are cut, the badges kept.
	gb, _, gt = fitNoteTitle(b, cls, text, path, 20)
	if gb != b || lipgloss.Width(gb)+1+lipgloss.Width(gt) > 20 {
		t.Fatalf("cut = %q %q", gb, gt)
	}
}

func TestFitNoteTitleNarrow(t *testing.T) {
	t.Parallel()
	b, cls := " issue  high ", make([]syntax.Class, 13)
	for _, room := range []int{-3, 0, 1, 4, 7, 13, 14} {
		gb, gc, gt := fitNoteTitle(b, cls, "review · a.txt R1", "a.txt", room)
		w := lipgloss.Width(gb) + lipgloss.Width(gt)
		if gb != "" && gt != "" {
			w++
		}
		if w > max(room, 0) || len(gc) != len([]rune(gb)) {
			t.Fatalf("room %d: %q %q (mask %d)", room, gb, gt, len(gc))
		}
		if room > 0 && room < 13 && gt != "" {
			t.Fatalf("room %d: today's parts must go before the badges: %q", room, gt)
		}
	}
	// A degenerate pane paints without panicking.
	_ = noteBoxCell(noteLine{kind: noteRowTop, text: "review · a.txt R1", badges: b, badgeCls: cls, path: "a.txt"}, 4)
}

func TestNoteBoxCellMetaFrameAndStale(t *testing.T) {
	t.Parallel()
	nl := noteLine{kind: noteRowBlank, metaFrame: "red", agent: true}
	if !strings.Contains(noteBoxCell(nl, 20), st().metaStyle(lipgloss.NewStyle(), metaClass("red", notemeta.StyleText)).Render("│")) {
		t.Fatal("the declared frame colour is not painted")
	}
	nl.stale = true
	if noteBoxCell(nl, 20) != noteBoxCell(noteLine{kind: noteRowBlank, stale: true, agent: true}, 20) {
		t.Fatal("a stale box must stay dim (dim wins)")
	}
}

func TestCollapsedRowCarriesBadges(t *testing.T) {
	t.Parallel()
	n, _ := declaredIssue()
	v := &diffView{collapsed: map[string]bool{"n1": true}}
	nl := v.collapsedNoteLine(domain.ResolvedNote{Note: n})
	if !strings.HasPrefix(nl.badges, " issue ") || strings.Contains(nl.text, "issue") {
		t.Fatalf("collapsed = %+v", nl)
	}
	if !strings.Contains(noteBoxCell(nl, 60), "issue") {
		t.Fatal("the collapsed row does not paint its badges")
	}
}
```
(`kinds(rows)` is a tiny test helper returning `[]noteRowKind`. The `TestNoteBoxNoMetaUnchanged` expected cell must be derived from the CURRENT `noteBoxCell` code before editing it — run it once against main's code to pin the exact string, then keep it.)

- [ ] **Step 2: Run** `go test ./internal/tui/ -run 'TestNoteBox|TestFitNoteTitle|TestCollapsedRowCarriesBadges' -count=1` — FAIL.

- [ ] **Step 3: Implement**

`diff_notes.go` — `noteLine` gains (after `errRow`):
```go
	// badges / badgeCls are a top or collapsed row's lead-in: the note's
	// badges and audience marker with the mask that colours them (spec
	// 2026-10-12-note-meta §6.1). "" = none — the row is today's.
	badges   string
	badgeCls []syntax.Class
	// path is the file path inside a top row's text: the first thing a too
	// wide title gives up (elided in the middle, never its name).
	path string
	// metaFrame is the frame colour the note's meta declares ("" = the
	// user/agent colour); a stale box ignores it.
	metaFrame string
```
`noteRowMeta` appended after `noteRowCollapsed`: `noteRowMeta // │ intent: … │ (dim): a line row, a reply's badges, the details`.

`noteMetaRows`:
```go
// noteMetaRows are one note's meta rows inside its box: its badges (a reply,
// whose badges have no title to sit in), its line rows, its details rows —
// each dim, wrapped, never cut.
func noteMetaRows(res notemeta.Resolved, badges, lines, details bool, innerW int, indent string, mk func(noteRowKind, string) noteLine) []noteLine {
	var rows []noteLine
	w := innerW - len([]rune(indent))
	if badges {
		if run, cls := badgeRun(res); run != "" {
			nl := mk(noteRowMeta, indent+run)
			nl.cls = offsetCls(cls, len([]rune(indent)))
			rows = append(rows, nl)
		}
	}
	add := func(texts []string) {
		for _, t := range texts {
			for _, ln := range noteWrap(t, w) {
				rows = append(rows, mk(noteRowMeta, indent+ln))
			}
		}
	}
	if lines {
		add(metaLineTexts(res))
	}
	if details {
		add(metaDetailTexts(res))
	}
	return rows
}
```
(a badge run wider than the box is cut by the painter's truncate guard; masks longer than the cut text are dropped by the existing `len(nl.cls) <= len(runes)` check — acceptable for a reply's badge row.)

`noteBoxLines` (diff): after `stale`/`allAgent`:
```go
	res := domain.NoteMeta(r.Note, nil)
	frame := func(kind noteRowKind, text string) noteLine {
		return noteLine{id: r.Note.ID, rootID: r.Note.ID, kind: kind, side: r.Note.Side, text: text, stale: stale, agent: allAgent, metaFrame: res.Frame}
	}
	…
	top := frame(noteRowTop, title)
	top.badges, top.badgeCls = badgeRun(res)
	top.path = sanitizeLine(owner.noteAddr.Path)
	rows := []noteLine{top, frame(noteRowBlank, "")}
	rootMk := func(kind noteRowKind, text string) noteLine {
		return noteLine{id: r.Note.ID, rootID: r.Note.ID, kind: kind, side: r.Note.Side, text: text, stale: stale,
			agent: r.Note.Source == model.NoteSourceAgent, metaFrame: res.Frame}
	}
	rows = append(rows, noteMetaRows(res, false, true, false, innerW, "", rootMk)...)
	rows = append(rows, noteBodyLines(r, r.Note.ID, 0, innerW, stale)...)
	rows = append(rows, noteMetaRows(res, false, false, true, innerW, "", rootMk)...)
	for _, rep := range r.Replies {
		rows = append(rows, noteBodyLines(rep, r.Note.ID, 1, innerW, stale)...)
		rr := domain.NoteMeta(rep.Note, &r.Note)
		repMk := func(kind noteRowKind, text string) noteLine {
			return noteLine{id: rep.Note.ID, rootID: r.Note.ID, depth: 1, kind: kind, side: r.Note.Side, text: text, stale: stale,
				agent: rep.Note.Source == model.NoteSourceAgent, metaFrame: res.Frame}
		}
		rows = append(rows, noteMetaRows(rr, true, true, true, innerW, "  ", repMk)...)
	}
	…
	// every row of the box wears the root's frame colour
	for i := range rows {
		rows[i].metaFrame = res.Frame
	}
```
(`noteBodyLines` rows get `metaFrame` from that final loop; keep the existing group-slot loop.)

`noteBoxTitle`: delete the `if model.IsReviewNoteID(r.Note.ID) && len(r.Note.Meta) > 0 { … }` block (and `strings` import if unused).

`collapsedNoteLine`: before `return nl`:
```go
	res := domain.NoteMeta(r.Note, nil)
	nl.badges, nl.badgeCls = badgeRun(res)
	nl.metaFrame = res.Frame
```

`note_meta.go` — `fitNoteTitle`:
```go
// minPathCols is the narrowest a title's path is elided to before today's
// parts start to go: "…/x.go" still names the file.
const minPathCols = 8

// fitNoteTitle fits a box title into room columns (spec §6.1): whole when it
// fits; else the path loses its middle; else today's parts are cut; the
// badges go last. badges and text are separated by one space when both stay.
func fitNoteTitle(badges string, cls []syntax.Class, text, path string, room int) (string, []syntax.Class, string) {
	if room <= 0 {
		return "", nil, ""
	}
	bw := lipgloss.Width(badges)
	gap := 0
	if badges != "" && text != "" {
		gap = 1
	}
	if bw+gap+lipgloss.Width(text) <= room {
		return badges, cls, text
	}
	textRoom := room - bw - gap
	if path != "" && textRoom > 0 {
		if before, after, ok := strings.Cut(text, path); ok {
			fixed := lipgloss.Width(before) + lipgloss.Width(after)
			if pw := textRoom - fixed; pw >= minPathCols {
				return badges, cls, before + elidePath(path, pw) + after
			}
			text = before + elidePath(path, minPathCols) + after
		}
	}
	if textRoom >= 1 {
		return badges, cls, truncate(text, textRoom)
	}
	// Today's parts are gone; the badges take the room, cut at their end.
	r := []rune(badges)
	w, cut := 0, 0
	for i, c := range r {
		cw := lipgloss.Width(string(c))
		if w+cw > room {
			break
		}
		w += cw
		cut = i + 1
	}
	return string(r[:cut]), cls[:min(cut, len(cls))], ""
}
```
(If `room` is 1..bw the badges are cut without an ellipsis — the frame rule follows; acceptable, tested only for width.)

`diff_render.go` `noteBoxCell`:
```go
	frame := s.noteFrameUser
	if nl.agent {
		frame = s.noteFrameAgent
	}
	if nl.metaFrame != "" {
		if c := metaClass(nl.metaFrame, notemeta.StyleText); c != 0 {
			frame = s.metaStyle(lipgloss.NewStyle(), c)
		}
	}
	text := s.noteBody
	switch nl.kind {
	case noteRowSummary:
		text = s.noteSummary
	case noteRowMeta:
		text = s.noteDim
	}
	if nl.stale { frame, text = s.noteFrameStale, s.noteDim } // unchanged: dim wins
	…
	case noteRowTop:
		if nl.badges == "" && nl.path == "" {
			// today's path, byte for byte
			title := truncate(sanitizeLine(nl.text), paneW-6)
			…unchanged…
			break
		}
		b, bc, t := fitNoteTitle(nl.badges, nl.badgeCls, sanitizeLine(nl.text), nl.path, paneW-6)
		used := lipgloss.Width(b) + lipgloss.Width(t)
		head := ""
		if b != "" {
			if nl.stale {
				head = frame.Render(b)
			} else {
				rb := []rune(b)
				head = styledRuns(rb, make([]emphLevel, len(rb)), bc, frame)
			}
			if t != "" {
				head += frame.Render(" ")
				used++
			}
		}
		rule := max(paneW-4-used, 1)
		cell = left.Render("╭") + frame.Render("─ ") + head + frame.Render(t+" "+strings.Repeat("─", rule-1)+"╮")
	case noteRowCollapsed:
		room := paneW - 2
		lead := ""
		if nl.badges != "" && lipgloss.Width(nl.badges)+2 <= room {
			rb := []rune(nl.badges)
			if nl.stale {
				lead = text.Render(nl.badges + " ")
			} else {
				lead = styledRuns(rb, make([]emphLevel, len(rb)), nl.badgeCls, text) + text.Render(" ")
			}
			room -= lipgloss.Width(nl.badges) + 1
		}
		body := padRight(truncate(sanitizeLine(nl.text), room), room)
		cell = left.Render("▸") + " " + lead + text.Render(body)
```
Note: with no badges a top row whose `path` is set takes the new path (path elision instead of an end cut) — a title that fitted is unchanged in text; only a too-wide one now keeps its file name (the user's path rule). Keep the old branch for `badges == "" && path == ""` (open-file notes, forge titles without a path).

- [ ] **Step 4: Run** Step 2's command; then the whole TUI package: `go test ./internal/tui/ -count=1`. Existing box tests that pinned a title with the old ` · k: v` suffix move to the line-row form (R13) — update them, never delete their intent.

- [ ] **Step 5: Commit** `git commit -m "feat(tui): note boxes show badges, frame colour, line rows and details"`.

---

### Task 5: Temporary notes and remark boxes in previews

**Files:**
- Modify: `internal/tui/open_file_notes.go` (`noteBoxLines(agentdocs.Note, …)`)
- Modify: `internal/tui/review_remarks.go` (`remarkBoxes`)
- Test: `internal/tui/note_meta_box_test.go`

**Interfaces:**
- Consumes: `noteMetaRows`, `badgeRun` (Task 4/2), `agentdocs.Note.Added` (Task 3).

- [ ] **Step 1: Failing tests**
```go
func TestOpenFileNoteBoxMeta(t *testing.T) {
	t.Parallel()
	_, d := declaredIssue()
	n := agentdocs.Note{ID: "t1", Author: "Claude Code", Start: 3, End: 3, Summary: "cached forever",
		Meta: []notemeta.KV{{Key: "kind", Value: "issue"}, {Key: "intent", Value: "checked the cache key"}}, Display: d,
		Added: []notemeta.Added{{Key: notemeta.StatusKey, Value: "disagreed", By: "MR"}}}
	rows := noteBoxLines(n, 60, 0)
	if !strings.Contains(rows[0].badges, "issue") || !strings.Contains(rows[0].badges, i18n.T("disagreed")) {
		t.Fatalf("badges = %q", rows[0].badges)
	}
	var texts []string
	for _, r := range rows {
		if r.kind == noteRowMeta {
			texts = append(texts, r.text)
		}
	}
	if len(texts) != 2 || texts[0] != "intent: checked the cache key" || !strings.HasPrefix(texts[1], i18n.T("added: %s", "status: disagreed (MR)")[:3]) {
		t.Fatalf("meta rows = %q", texts)
	}
}

func TestRemarkBoxesKeepMeta(t *testing.T) {
	t.Parallel()
	n, d := declaredIssue()
	n.Added = []notemeta.Added{{Key: "status", Value: "agreed", By: "MR"}}
	got := remarkBoxes([]domain.ResolvedNote{{Note: n, Range: [2]int{1, 1}}})
	if len(got[0].Meta) != len(n.Meta) || len(got[0].Display) != len(d) || len(got[0].Added) != 1 {
		t.Fatalf("remark box = %+v", got[0])
	}
}
```
- [ ] **Step 2: Run** `go test ./internal/tui/ -run 'TestOpenFileNoteBoxMeta|TestRemarkBoxesKeepMeta' -count=1` — FAIL.
- [ ] **Step 3: Implement**

`open_file_notes.go` `noteBoxLines`:
```go
	res := notemeta.Resolve(n.Meta, n.Display, n.Added)
	frame := func(kind noteRowKind, text string) noteLine {
		return noteLine{id: n.ID, rootID: n.ID, kind: kind, side: model.NoteSideNew, text: text, stale: n.Outdated, agent: true, metaFrame: res.Frame}
	}
	…
	body := noteMetaRows(res, false, true, false, innerW, "", frame)
	body = append(body, noteBodyLines(r, n.ID, 0, innerW, n.Outdated)...)
	body = append(body, noteMetaRows(res, false, false, true, innerW, "", frame)...)
	for i := range body {
		body[i].metaFrame = res.Frame
	}
	… (the existing maxRows cap now covers the meta rows too) …
	top := frame(noteRowTop, noteTitle(n))
	top.badges, top.badgeCls = badgeRun(res)
	rows := []noteLine{top, frame(noteRowBlank, "")}
```
`review_remarks.go` `remarkBoxes`: add `Meta: n.Note.Meta, Display: n.Note.Display, Added: n.Note.Added` to the literal.

- [ ] **Step 4: Run** Step 2 + `go test ./internal/tui/ -count=1`. PASS.
- [ ] **Step 5: Commit** `git commit -m "feat(tui): temporary notes and preview remark boxes show their meta"`.

---

### Task 6: Review view — header counts, file-row counts, Other notes rows

**Files:**
- Modify: `internal/tui/review_view.go` (`reviewViewState`, `reviewViewMsg`, loader, `reviewMetaLine`, `reviewOtherNoteLines`, `reviewSummaryLines`)
- Modify: `internal/tui/files_view.go` (`renderFilesView` row loop)
- Test: `internal/tui/review_meta_test.go` (new)

**Interfaces:**
- Consumes: `domain.ReviewMetaCounts`, `ReviewOtherNote.Meta` (Task 3); `metaCountsText`, `metaCountsRun`, `badgeRun` (Task 2).
- Produces: `reviewViewState.meta notemeta.Counts`, `reviewViewState.metaByFile map[string]notemeta.Counts`; `reviewFileCountsTail(st, path string, room int) (string, []syntax.Class)`.

- [ ] **Step 1: Failing tests**
```go
func TestReviewMetaLineCounts(t *testing.T) {
	t.Parallel()
	st := &reviewViewState{review: domain.Review{Agent: "Claude Code"},
		meta: notemeta.Counts{Groups: []notemeta.CountGroup{{Key: "kind", Values: []notemeta.CountValue{{Value: "issue", N: 3}, {Value: "good", N: 8}}}}, Author: 5, Reviewer: 9}}
	got := reviewMetaLine(st)
	if !strings.HasSuffix(got, "3 issue · 8 good · "+i18n.T("%d for author", 5)+" · "+i18n.T("%d for you", 9)) {
		t.Fatalf("header = %q", got)
	}
	st.meta = notemeta.Counts{}
	if strings.HasSuffix(reviewMetaLine(st), " · ") {
		t.Fatal("no counts must add nothing")
	}
}

func TestReviewFileRowCountsFitOrGo(t *testing.T) {
	t.Parallel()
	st := &reviewViewState{metaByFile: map[string]notemeta.Counts{"a.txt": {Groups: []notemeta.CountGroup{{Key: "kind", Values: []notemeta.CountValue{{Value: "issue", Color: "red", N: 1}}}}}}}
	tail, cls := reviewFileCountsTail(st, "a.txt", 40)
	if tail != "  issue 1" || cls[len(cls)-1] != metaClass("red", notemeta.StyleText) {
		t.Fatalf("tail = %q %v", tail, cls)
	}
	if tail, _ := reviewFileCountsTail(st, "a.txt", 5); tail != "" {
		t.Fatalf("a tail that does not fit must go before the path is cut: %q", tail)
	}
	if tail, _ := reviewFileCountsTail(st, "b.txt", 40); tail != "" {
		t.Fatal("a file without counts has no tail")
	}
}

func TestReviewOtherNoteRowBadges(t *testing.T) {
	t.Parallel()
	st := &reviewViewState{other: []domain.ReviewOtherNote{{Path: "x.go", Range: [2]int{4, 4}, Summary: "gone",
		Meta: notemeta.Resolved{Badges: []notemeta.Badge{{Key: "kind", Value: "issue", Color: "red", Style: notemeta.StyleFilled}}}}}}
	ls := reviewOtherNoteLines(st)
	if !strings.HasPrefix(ls[0].text, " issue  x.go:4 — gone") || ls[0].cls[0] != metaClass("red", notemeta.StyleFilled) {
		t.Fatalf("row = %q %v", ls[0].text, ls[0].cls)
	}
}
```
Plus one rendering test that opens a review view through the existing review-view test helper (grep `reviewViewMsg{` in `*_test.go`) with `metaByFile` set and asserts the rendered files view contains `issue 1` on the file's row, and that at a narrow width the row keeps its file name and drops the tail.

- [ ] **Step 2: Run** `go test ./internal/tui/ -run 'TestReviewMetaLineCounts|TestReviewFileRowCountsFitOrGo|TestReviewOtherNoteRowBadges' -count=1` — FAIL.
- [ ] **Step 3: Implement**
  - `reviewViewState` + `reviewViewMsg` gain `meta notemeta.Counts; metaByFile map[string]notemeta.Counts`; in `openReviewLanding`'s closure after the review read: `out.meta, out.metaByFile = domain.ReviewMetaCounts(out.review)`; `handleReviewViewMsg` copies both into `st`.
  - `reviewMetaLine`: `if c := metaCountsText(st.meta); c != "" { parts = append(parts, c) }` before the join.
  - `reviewFileCountsTail`:
```go
// reviewFileCountsTail is a review file row's compact meta counts ("  issue 1
// good 1", spec §6.2), or "" when the file has none or they would not fit in
// room — the counts go before the path is cut.
func reviewFileCountsTail(st *reviewViewState, path string, room int) (string, []syntax.Class) {
	if st == nil {
		return "", nil
	}
	run, cls := metaCountsRun(st.metaByFile[path])
	if run == "" || lipgloss.Width(run)+2 > room {
		return "", nil
	}
	return "  " + run, append([]syntax.Class{0, 0}, cls...)
}
```
  - `renderFilesView`: after the badge branches, before building `wr[i]`:
```go
		var cls []syntax.Class
		if m.filesReview != nil && l.path != "" && !l.heading {
			room := innerW - lipgloss.Width(prefix+text)
			if p.mode != modeCutoff {
				room = 1 << 20 // wrap/scroll show the whole row
			}
			if tail, tc := reviewFileCountsTail(m.filesReview, l.path, room); tail != "" {
				cls = append(make([]syntax.Class, len([]rune(prefix+text))), tc...)
				text += tail
			}
		}
		wr[i] = winRow{text: prefix + text, style: st, elide: elide, elideHead: len([]rune(prefix)), cls: cls}
```
  - `reviewOtherNoteLines`: for a live remark with badges:
```go
		if run, cls := badgeRun(o.Meta); run != "" {
			text = run + "  " + text
			l.cls = append(cls, make([]syntax.Class, len([]rune(text))-len(cls))...)
		}
```
  (build `contentLine{text: text, path: o.Path, cls: …}`); in `reviewSummaryLines` the `"  "+l.text` prefix also shifts the mask: `l.cls = offsetCls(l.cls, 2)`.
- [ ] **Step 4: Run** Step 2 + `go test ./internal/tui/ -count=1`. PASS.
- [ ] **Step 5: Commit** `git commit -m "feat(tui): review view counts meta in its header and file rows"`.

---

### Task 7: Send panel — the expanded row's meta row

**Files:**
- Modify: `internal/tui/send_panel.go` (`box`)
- Test: `internal/tui/send_panel_meta_test.go` (new)

**Interfaces:**
- Consumes: `SendCandidate.Meta` (Task 3), `badgeRun`, `metaLineTexts` (Task 2).
- Produces: `sendMetaRun(res notemeta.Resolved) (string, []syntax.Class)` — badges + marker + ` · `-joined line entries.

- [ ] **Step 1: Failing tests** (build a `sendPanel` by hand the way existing send-panel tests do — grep `&sendPanel{` in `*_test.go`):
```go
func TestSendPanelExpandedMetaRow(t *testing.T) {
	t.Parallel()
	c := domain.SendCandidate{ID: "review:r:0", Kind: "remark", Severity: "high", Path: "a.txt", Range: [2]int{3, 3}, Summary: "cached forever",
		Meta: notemeta.Resolved{Badges: []notemeta.Badge{{Key: "kind", Value: "issue", Color: "red", Style: notemeta.StyleFilled}}, Audience: "author",
			Lines: []notemeta.Line{{Label: "intent", Value: "checked the key"}}}}
	p := newTestSendPanel(c) // helper: one group, one row, nothing ticked
	out := p.box(testModel(t))
	if strings.Contains(out, "issue") || strings.Contains(out, "checked the key") {
		t.Fatal("a collapsed row shows no meta (R9)")
	}
	if p.tickedCount() != 0 {
		t.Fatal("nothing is ticked on open (R9)")
	}
	p.sel = p.firstCandidate()
	p.fold(true)
	out = p.box(testModel(t))
	want := " issue  " + i18n.T("→ author") + " · intent: checked the key"
	if !strings.Contains(stripANSI(out), want) {
		t.Fatalf("expanded row lacks %q:\n%s", want, stripANSI(out))
	}
}

func TestSendPanelExpandedMetaRowSelected(t *testing.T) {
	t.Parallel()
	// The meta row is not the cursor row (the candidate row is), so its mask
	// paints; the candidate row itself reverses video and carries no mask.
	run, cls := sendMetaRun(notemeta.Resolved{Badges: []notemeta.Badge{{Key: "kind", Value: "issue", Color: "red", Style: notemeta.StyleFilled}}})
	if len(cls) != len([]rune(run)) {
		t.Fatalf("mask %d vs %d runes", len(cls), len([]rune(run)))
	}
}
```
(Use the existing helpers for model/ANSI stripping — `grep -n 'func stripANSI\|func ansiStrip\|func plain(' internal/tui/*_test.go`.)
- [ ] **Step 2: Run** `go test ./internal/tui/ -run 'TestSendPanelExpandedMetaRow' -count=1` — FAIL.
- [ ] **Step 3: Implement**
```go
// sendMetaRun is an opened row's meta row (spec §6.3): badges, the audience
// marker, then each line entry. "" when the note has none.
func sendMetaRun(res notemeta.Resolved) (string, []syntax.Class) {
	run, cls := badgeRun(res)
	for _, l := range metaLineTexts(res) {
		sep := " · "
		if run == "" {
			sep = ""
		}
		run += sep + l
		cls = append(cls, make([]syntax.Class, len([]rune(sep+l)))...)
	}
	return run, cls
}
```
In `box()`, right after `rows = append(rows, row)` for a candidate:
```go
		indent := strings.Repeat(" ", panelHang)
		if open {
			if run, cls := sendMetaRun(c.Meta); run != "" {
				rows = append(rows, winRow{text: indent + run, cls: offsetCls(cls, panelHang), hang: panelHang})
			}
		}
```
(move the existing `indent :=` up so it is declared once.)
- [ ] **Step 4: Run** Step 2 + `go test ./internal/tui/ -run SendPanel -count=1`. PASS.
- [ ] **Step 5: Commit** `git commit -m "feat(tui): the send panel's expanded row shows the note's meta"`.

---

### Task 8: Mark — agreed / disagreed / deferred / clear

**Files:**
- Create: `internal/tui/note_mark.go`
- Modify: `internal/tui/note_keys.go` (`noteMenuRows` adds the row), `internal/tui/model.go` (route `noteMarkedMsg`)
- Modify: `internal/tui/diff_menu_parity_test.go` (the row in single and stacked diffs)
- Modify: bundles (`Mark…`, `Mark this note:`, options `agreed`, `disagreed`, `deferred`, `clear`)
- Test: `internal/tui/note_mark_test.go`

**Interfaces:**
- Consumes: `domain.(*Service).SetNoteMeta`, `UnsetNoteMeta`, `notemeta.StatusKey`; `notesAtCursor`, `replyableNoteTargets`, `withNoteTargetIn`, `noteQuote`, `noteMutatedMsg`.
- Produces: `func (m Model) noteMarkRow() (actionRow, bool)`, `type noteMarkedMsg struct{ err error }`.

- [ ] **Step 1: Failing tests** (model setup: copy the nearest `noteMenuRows` / resolve test in `note_keys_test.go` — a real repo with a stored note at the cursor):
```go
func TestNoteMenuOffersMark(t *testing.T) {
	m := modelWithNoteAtCursor(t) // existing-style helper: a diff with one stored agent note in reach
	rows := m.noteMenuRows()
	found := false
	for _, r := range rows {
		if r.id == "note-mark" && r.label == i18n.T("Mark…") {
			found = true
		}
	}
	if !found {
		t.Fatalf("rows = %v", rowIDs(rows))
	}
}

func TestMarkWritesStatusAndReloads(t *testing.T) {
	m := modelWithNoteAtCursor(t)
	m = runMenuRow(t, m, "note-mark")        // opens the modal
	if m.modal == nil || !slices.Contains(m.modal.req.Options, "disagreed") {
		t.Fatalf("modal = %+v", m.modal)
	}
	m = resolveModal(t, m, "disagreed")      // drains the write cmd and the reload
	n := storedNote(t, m, noteID)            // svc.Note(ctx, id)
	if len(n.Added) != 1 || n.Added[0].Key != notemeta.StatusKey || n.Added[0].Value != "disagreed" || n.Added[0].By != m.identity.EffectiveName {
		t.Fatalf("added = %+v", n.Added)
	}
	// R12: nothing else changed — not resolved, not folded.
	if n.Resolved() || m.diffLayer().collapsed[noteID] {
		t.Fatal("a mark must have no consequence")
	}
	if !strings.Contains(viewOf(m), i18n.T("disagreed")) {
		t.Fatal("the box does not show the mark after the reload")
	}
	m = runMenuRow(t, m, "note-mark")
	m = resolveModal(t, m, "clear")
	if n := storedNote(t, m, noteID); len(n.Added) != 0 {
		t.Fatalf("clear left %+v", n.Added)
	}
}

func TestMarkRemarkShowsAfterReload(t *testing.T) {
	// A review view diff with a remark in reach: mark it "agreed"; after the
	// noteMutatedMsg reload the remark's box title badges hold
	// i18n.T("agreed") (Added came back through ReviewNotesFor → remarkMeta).
}

func TestMarkNotOfferedOnForgeThread(t *testing.T) {
	// A PR diff whose only thread in reach is a forge thread: no "note-mark" row.
}
```
Write the sketched tests in full from the closest existing tests (`grep -n 'func Test.*Resolve.*Thread\|note-resolve' internal/tui/*_test.go`); the helper names above are placeholders for whatever those tests already use — reuse, don't invent.
- [ ] **Step 2: Run** `go test ./internal/tui/ -run 'TestNoteMenuOffersMark|TestMark' -count=1` — FAIL.
- [ ] **Step 3: Implement** `note_mark.go`:
```go
package tui

// Mark (spec 2026-10-12-note-meta §5.4, R6, R12): the reader tags a note
// agreed / disagreed / deferred — the same `status` key a double-checking
// agent writes. It is a tag and nothing else: it resolves, sends, ticks,
// folds and filters nothing.

// markOptions are the modal's choices: the status values, clear, Cancel.
var markOptions = []string{"agreed", "disagreed", "deferred", "clear", "Cancel"}

// noteMarkedMsg is a mark's write result.
type noteMarkedMsg struct{ err error }

// markableNoteTargets are the threads in reach a mark can be written on:
// stored notes and review remarks — never a GitHub thread (read-only).
func markableNoteTargets(ts []noteTarget) []noteTarget {
	var out []noteTarget
	for _, t := range ts {
		if !t.forge && !model.IsForgeNoteID(t.note.ID) {
			out = append(out, t)
		}
	}
	return out
}

// noteMarkRow is the . menu's "Mark…": a choice of the three marks or clear.
func (m Model) noteMarkRow() (actionRow, bool) {
	if _, ok := m.topLayer().(*diffView); !ok || m.svc == nil {
		return actionRow{}, false
	}
	ts := markableNoteTargets(replyableNoteTargets(m.notesAtCursor(), m.prOfDiff() > 0))
	if len(ts) == 0 {
		return actionRow{}, false
	}
	return actionRow{id: "note-mark", label: i18n.T("Mark…"), run: func(m Model) (tea.Model, tea.Cmd) {
		return m.withNoteTargetIn(ts, func(m Model, t noteTarget) (tea.Model, tea.Cmd) {
			return m.askNoteMark(t)
		})
	}}, true
}

// askNoteMark raises the mark choice for one note (esc = Cancel).
func (m Model) askNoteMark(t noteTarget) (tea.Model, tea.Cmd) {
	id := t.note.ID
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "note-mark",
			Prompt:  i18n.T("Mark this note:") + "\n" + noteQuote(t.note),
			Options: []string{"agreed", "disagreed", "deferred", "clear", "Cancel"},
		},
		sel: 0,
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt == "Cancel" || m.svc == nil {
				return m, nil
			}
			svc, by := m.svc, m.identity.EffectiveName
			return m, func() tea.Msg {
				ctx := context.Background()
				if opt == "clear" {
					return noteMarkedMsg{err: svc.UnsetNoteMeta(ctx, id, by, []string{notemeta.StatusKey})}
				}
				_, err := svc.SetNoteMeta(ctx, id, by, []notemeta.KV{{Key: notemeta.StatusKey, Value: opt}})
				return noteMarkedMsg{err: err}
			}
		},
	}
	return m, nil
}

// onNoteMarked reloads the notes (the one reload path) or says why not.
func (m Model) onNoteMarked(msg noteMarkedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = i18n.T("note: %s", msg.err.Error())
		m.diffNotice = "▸ " + m.statusMsg
		return m, nil
	}
	return m.Update(noteMutatedMsg{})
}
```
(The literal `Options` slice is what `options_vocab_test` scans; keep `markOptions` only if a test uses it, else drop it.) `noteMenuRows`: after the resolve row, `if r, ok := m.noteMarkRow(); ok { rows = append(rows, r) }`. `model.go` Update: `case noteMarkedMsg: return m.onNoteMarked(msg)` beside `threadResolvedMsg`. If the review view's counts can change through a mark (a declaration counting `status`), refresh them on `noteMarkedMsg` success when `m.filesReview != nil` with an off-thread `svc.Review` + `domain.ReviewMetaCounts` (a `reviewMetaCountsMsg{id, meta, byFile}` guarded by `st.id`), tested by asserting the header after a mark on a review declaring `"status":{"show":["count"]}`.
Add the `note-mark` row to the single/stacked parity table in `diff_menu_parity_test.go` (memory: stacked-blind menu rows are a recurring class).
- [ ] **Step 4: Run** Step 2 + `go test ./internal/tui/ -count=1`. PASS.
- [ ] **Step 5: Commit** `git commit -m "feat(tui): Mark a note agreed, disagreed or deferred"`.

---

### Task 9: A notes file newer than this build — the notice

**Files:**
- Modify: `internal/preflight/preflight.go` (`Probes.Newer`, `NewerLocal`)
- Modify: `internal/domain/features.go` (`FeatureNoteEdits`)
- Modify: `internal/domain/preflight.go` (`probesFrom`, `newerNotesPresent`)
- Modify: bundles (the Reason and Remedy literals)
- Test: `internal/preflight/preflight_test.go`, `internal/domain/preflight_test.go`, `internal/tui/notify_test.go`

**Interfaces:**
- Produces: `preflight.NewerLocal{Store string}`, `Probes.Newer map[string]bool`, `domain.FeatureNoteEdits = "note-edits"`.

- [ ] **Step 1: Failing tests**
```go
// preflight
func TestNewerLocalUnsatisfiable(t *testing.T) {
	t.Parallel()
	f := Feature{ID: "note-edits", Requires: []Requirement{NewerLocal{Store: "notes"}}}
	v := Resolve([]Feature{f}, Probes{Newer: map[string]bool{"notes": true}})
	if v[0].State != Unsatisfiable || v[0].Remedy.Format == "" {
		t.Fatalf("verdict = %+v", v[0])
	}
	if Resolve([]Feature{f}, Probes{})[0].State != Satisfied {
		t.Fatal("no newer file must be satisfied")
	}
}

// domain: write a part file with format = 4 into the repo's notes dir (find
// how the plan-1 ErrNewerFormat test wrote one: grep -n 'ErrNewerFormat' internal/domain/*_test.go internal/notes/*_test.go)
func TestPreflightNewerNotesFormat(t *testing.T) {
	// … Preflight(ctx) holds a verdict for FeatureNoteEdits with State ==
	// preflight.Unsatisfiable; with no newer file it is Satisfied; the
	// FeatureNotes verdict stays Satisfied either way (notes stay readable).
}

// tui
func TestNoticeForNewerNotes(t *testing.T) {
	t.Parallel()
	v := preflight.Resolve([]preflight.Feature{{ID: domain.FeatureNoteEdits, Requires: []preflight.Requirement{preflight.NewerLocal{Store: domain.StoreNotes}}}},
		preflight.Probes{Newer: map[string]bool{domain.StoreNotes: true}})
	ns := noticesForVerdicts(v, "repo")
	if len(ns) != 1 || ns[0].id != noticeFeatureDisabledPrefix+domain.FeatureNoteEdits {
		t.Fatalf("notices = %+v", ns)
	}
}
```
- [ ] **Step 2: Run** `go test ./internal/preflight/ ./internal/domain/ ./internal/tui/ -run 'NewerLocal|NewerNotes' -count=1` — FAIL.
- [ ] **Step 3: Implement**

preflight:
```go
// Probes gains:
	// Newer names the machine-local stores a newer gg wrote files of (a
	// notes part at a format above this build's): readable, never written.
	Newer map[string]bool

// NewerLocal requires that no file of a machine-local store was written by
// a newer gg. Unlike DataFormat it reads no git-ref marker: the files carry
// their own format (spec 2026-10-12-note-meta §2.5), and the answer is this
// machine's. Never repairable from inside gg.
type NewerLocal struct{ Store string }

func (n NewerLocal) Fit(p Probes) Fit {
	if p.Newer[n.Store] {
		return FitTooNew
	}
	return FitOK
}

func (n NewerLocal) Reason(p Probes) Text {
	return Text{Format: "the %s store was written by a newer gg: it stays readable, but this build will not change it", Args: []any{n.Store}}
}

func (n NewerLocal) Remedy(p Probes) Text { return Text{Format: "Upgrade gg to change it again."} }

func (n NewerLocal) RepairStore() string    { return "" }
func (n NewerLocal) NeedsStoreProbes() bool { return false }
```
domain/features.go:
```go
	// FeatureNoteEdits is writing notes: a notes file a newer gg wrote
	// (format above notes.Format) stays readable but refuses every write —
	// the notice says so (spec 2026-10-12-note-meta §10).
	FeatureNoteEdits = "note-edits"
…
		{
			ID:          FeatureNoteEdits,
			Criticality: preflight.Optional,
			Requires:    []preflight.Requirement{preflight.NewerLocal{Store: StoreNotes}},
		},
```
domain/preflight.go `probesFrom`: `Newer: map[string]bool{StoreNotes: s.newerNotesPresent(ctx)},` and
```go
// newerNotesPresent reports a notes part a newer gg wrote (one index read
// per part, on a full resolve only). An injected non-file store has none.
func (s *Service) newerNotesPresent(ctx context.Context) bool {
	fs, ok := s.notesStore(ctx).(*notes.FileStore)
	return ok && len(fs.NewerParts()) > 0
}
```
Bundles: both literals in all four files (the `preflightProseKeys` gate in `engine_prose_test.go` checks them). Check `features` tests that count `Features()` entries or list ids and add `note-edits`.
- [ ] **Step 4: Run** Step 2 + `go test ./internal/preflight/ ./internal/domain/ ./internal/tui/ -count=1`. PASS.
- [ ] **Step 5: Commit** `git commit -m "feat: notice when a notes file is newer than this gg"`.

---

### Task 10: e2e golden screens, docs, full gate

**Files:**
- Create: `e2e/scenarios/tui_note_meta.toml` (+ `tui_note_meta.screens/` via `-update`)
- Create: `e2e/scenarios/tui_note_meta_send.toml` (+ screens)
- Modify: existing goldens that legitimately change (R13: undeclared meta → line rows), reviewed one by one
- Modify: `CHANGELOG.md`, `README.md` (theme roles sentence near `note_group_1 … note_group_6`), `docs/CLAUDE-details.md` (TUI rendering of meta, theme roles, `NewerLocal`/`note-edits`)

- [ ] **Step 1: Scenario `tui_note_meta.toml`** (writing-e2e-scenarios skill). Remarks shaped after Appendix B (classification table rows 31, 35, 0, 34, 38), the §9 default display block:
```toml
name = "tui: a review's remarks show what they are — badges, frame, line rows, counts — and Mark tags one"

[input]
steps = [
  { write = "configuration.ts", content = "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n" },
  { write = "caching.ts", content = "a\nb\nc\nd\n" },
  { commit = "base" },
  { write = "configuration.ts", content = "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\ncache(12)\n13\n14\n15\n" },
  { write = "caching.ts", content = "a\nb\ncatch()\nd\n" },
  { commit = "cache config" },
]

[[run]]
cmd   = ["review", "save", "gg://{{cwd}}@{{rev:HEAD}}", "--agent", "Claude Code", "--stdin"]
stdin = '''{"version":1,"summary":"## Summary\nconfig caching","display":{
 "kind":{"show":["badge","count"],"order":1,"values":{"issue":{"color":"red","audience":"author"},"suggestion":{"color":"blue","audience":"author"},"nitpick":{"color":"grey","audience":"author"},"note":{"color":"yellow","audience":"reviewer"},"good":{"color":"green","audience":"reviewer"}}},
 "severity":{"show":["badge","frame"],"order":2,"style":"text","values":{"high":{"color":"red","level":"high"},"medium":{"color":"orange","level":"medium"},"low":{"color":"grey","level":"low"}}},
 "audience":{"as":"audience","order":3},
 "intent":{"show":["line"],"order":4}},
 "files":[
  {"path":"configuration.ts","annotations":[
   {"newRange":[12,12],"summary":"Config modules are cached forever","meta":{"kind":"issue","severity":"high","intent":"checked the cache key"}},
   {"newRange":[13,13],"summary":"Unknown callers now get ESM","meta":{"kind":"note","severity":"high","audience":"author"}}]},
  {"path":"caching.ts","annotations":[
   {"newRange":[3,3],"summary":"The no-op catch only marks the promise as handled","meta":{"kind":"good","intent":"verified the rejection path"}}]}]}'''
exit  = 0

[tui]
size = "160x40"
```
Steps (adapt the keys to how `tui_review_mixed.toml` reaches a commit's Reviews row; verify each screen by reading the golden, not by guessing):
  1. `review-view`: Commits → Files → the Reviews row → enter. `screen_contains = ["2 issue" or the real tally …]` — concretely `["1 issue · 1 note · 1 good", "for author", "for you"]` in the header and `["configuration.ts", "issue 1"]` on the file row.
  2. `remark-box`: open `configuration.ts`'s diff. `screen_contains = ["issue  high  → author", "intent: checked the cache key", "note  high  → author"]`.
  3. `mark-menu`: `.` then type to filter `Mark`, enter → `screen_contains = ["Mark this note:", "agreed", "disagreed", "deferred", "clear"]`.
  4. `marked`: choose `disagreed` → `screen_contains = ["disagreed", "added: status: disagreed"]`.
  5. `undeclared`: a stored note added with `gg note add … --meta confidence=high` (a `[[run]]` before `[tui]`) shows `confidence: high` as a line row, no badge.
- [ ] **Step 2: Scenario `tui_note_meta_send.toml`** — copy `tui_pr_send.toml`'s fakegh input and PR setup verbatim; the review's remark carries `"meta":{"kind":"issue","severity":"high","intent":"checked the key"}` with the kind/severity/intent declaration. Steps: open the panel (`i`, `s`), assert `0 ticked` and NO `issue` on screen; move to the remark row, `right` → `screen_contains = ["issue  high  → author · intent: checked the key"]`, still `0 ticked`.
- [ ] **Step 3: Generate and review goldens**
Run: `go test ./e2e/ -run 'TestScenarios/tui_note_meta' -update -count=1`, then read every `.screens/*.txt`; then `go test ./e2e/ -count=1` and read the diff of each pre-existing golden that changed (`git diff --stat e2e/`): only R13 changes (a ` · k: v` suffix becoming a `k: v` row; a too-wide title now elided in its path) are acceptable — anything else is a bug to fix, not a golden to update.
- [ ] **Step 4: Docs** — CHANGELOG entry (TUI shows note meta: badges, frame, line rows, details, review counts, send-panel expanded row, Mark, the newer-notes notice, theme roles); README: extend the theme-roles sentence with `meta_red` … `meta_grey`; `docs/CLAUDE-details.md`: a "Note meta in the TUI" paragraph (one paint mechanism = meta pseudo-classes; title fit order; Mark = status tag; `NewerLocal` probe).
- [ ] **Step 5: Gate**
Run: `./test.sh race 2>&1 | tee /tmp/claude-1000/-work-gigagit/35aa0a8f-6206-45e1-8e57-18ef8f60ff68/scratchpad/race.log | tail -5` — it must end with "all green" (grep the log for it; `tail` exits 0 on FAIL).
- [ ] **Step 6: Commit** `git commit -m "test(e2e): note meta golden screens; docs"`; build the verify binary (`./build.sh` in the worktree → `<worktree>/bin/gg`, never committed); final read-only review subagent on the whole branch; fix findings test-first; ask the user before merging.

---

## Self-review notes

- Spec coverage: §6.1 title/frame/lines/details → T4 (+T5 for temporary notes, preview remark boxes); §6.2 header/file rows/remark rows → T6 (remark rows = Other notes rows; remark boxes carry badges via T4); §6.3 → T7; §6.4 roles + i18n → T1/T2 (+ keys in each task); §5.4 Mark → T8 (TUI note-box menu only — review remark boxes in a review diff are note boxes; no Mark on Files-view note rows, user ruling "Open + Delete only"); §10 newer-format notice → T9; §11 TUI goldens → T10.
- Forge threads (`forgeNoteTitle`): painted generically — a forge `ResolvedNote` with meta would show badges, but in v1 forge notes carry none (§8.3); not asserted.
- Type names used across tasks: `badgeRun`, `metaLineTexts`, `metaDetailTexts`, `metaCountsText`, `metaCountsRun`, `metaClass`, `metaStyle`, `fitNoteTitle`, `noteMetaRows`, `noteRowMeta`, `reviewFileCountsTail`, `sendMetaRun`, `noteMarkRow`, `noteMarkedMsg`, `ReviewMetaCounts`, `NewerLocal`, `FeatureNoteEdits` — consistent.
