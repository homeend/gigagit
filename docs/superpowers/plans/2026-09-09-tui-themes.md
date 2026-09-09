# TUI Themes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A `[ui] theme` setting (`terminal` | `dark` | `light`) that makes gg's TUI render identically in every truecolor terminal, with `terminal` staying byte-identical to today.

**Architecture:** A new pure leaf package `internal/theme` holds named colour roles. The TUI's ~46 package-level lipgloss style vars move into one `styles` struct built from a `theme.Theme`, held behind an `atomic.Pointer` and read through `st()`. A pure `paintFrame` post-processor paints bg/fg under every cell of the final frame. Config, the Settings popup and i18n wire the setting.

**Tech Stack:** Go 1.26, lipgloss v1.1.0, bubbletea v1.3.10, `charmbracelet/x/ansi` v0.10.1 (already in go.mod), termenv v0.16.0 (tests only).

**Spec:** `docs/superpowers/specs/2026-09-09-tui-themes-design.md`

## Global Constraints

- Work in the worktree `/mnt/t/others/gigagit.worktrees/feat-tui-themes` on branch `feat/tui-themes`. Prefix every shell command with `cd /mnt/t/others/gigagit.worktrees/feat-tui-themes &&`; the shell cwd resets between calls. Use absolute worktree paths for Write/Edit.
- `internal/theme` imports NOTHING from the module and no lipgloss/termenv (DAG leaf; `internal/archtest` runs on `go test ./...`).
- `internal/tui` never imports `internal/git`; unchanged.
- `terminal` theme = zero `theme.Theme`; `buildStyles(theme.Terminal)` must reproduce today's literals exactly (Task 2 test pins them).
- Every user-visible TUI string goes through `i18n.T("literal")` with the key present in `internal/i18n/lang/{ja,ko,zh,ru}.toml`; the AST-gate tests fail otherwise.
- New tests call `t.Parallel()` EXCEPT tests that swap the styles pointer or `lipgloss.SetColorProfile` (process-global) — those are serial and say so in a comment.
- Colour literals in tables are copied verbatim from the spec §3.1.
- Commit after every task with the trailer:
  ```
  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01U1sb6UKEkBejtJ3hQEmhsX
  ```
- Run `cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && go build ./cmd/gg && go vet ./internal/theme ./internal/tui ./internal/config` before each commit; `./test.sh unit` at the end of Tasks 3, 5 and 6.

---

### Task 1: `internal/theme` package

**Files:**
- Create: `internal/theme/theme.go`
- Test: `internal/theme/theme_test.go`

**Interfaces:**
- Produces: `type Theme struct{…}` (fields below), `var Terminal, Dark, Light Theme`, `func Lookup(name string) (Theme, bool)`, `func Names() []string`, constants `NameTerminal = "terminal"`, `NameDark = "dark"`, `NameLight = "light"`.

- [ ] **Step 1: Write the failing tests**

`internal/theme/theme_test.go`:

```go
package theme

import (
	"regexp"
	"strconv"
	"testing"
)

var hexOrIndex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func validColour(s string) bool {
	if hexOrIndex.MatchString(s) {
		return true
	}
	n, err := strconv.Atoi(s)
	return err == nil && n >= 0 && n <= 255
}

func TestLookup(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", "terminal"} {
		th, ok := Lookup(name)
		if !ok || th.Name != NameTerminal || th.Bg != "" || th.Fg != "" {
			t.Fatalf("Lookup(%q) = %+v, %v; want Terminal", name, th, ok)
		}
	}
	if th, ok := Lookup("dark"); !ok || th.Name != NameDark || th.Bg != "#0C0C0C" {
		t.Fatalf("Lookup(dark) = %+v, %v", th, ok)
	}
	if th, ok := Lookup("light"); !ok || th.Name != NameLight || th.Bg != "#F3EAD3" {
		t.Fatalf("Lookup(light) = %+v, %v", th, ok)
	}
	if _, ok := Lookup("solarized"); ok {
		t.Fatal("unknown theme must not resolve")
	}
	if _, ok := Lookup("Dark"); ok {
		t.Fatal("lookup is case-sensitive (config values are English protocol strings)")
	}
}

func TestNamesOrder(t *testing.T) {
	t.Parallel()
	got := Names()
	want := []string{"terminal", "dark", "light"}
	if len(got) != len(want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// Every non-empty role must be a #rrggbb hex or a 0–255 index, and the two
// built-in colour themes must set every role except the two deliberately
// uncoloured syntax classes (Plain=0, Name=4).
func TestBuiltinsComplete(t *testing.T) {
	t.Parallel()
	for _, th := range []Theme{Dark, Light} {
		for i, c := range th.roles() {
			if c == "" {
				t.Errorf("%s: role %d is empty", th.Name, i)
			} else if !validColour(c) {
				t.Errorf("%s: role %d = %q is not a hex colour or 0–255 index", th.Name, i, c)
			}
		}
		for i, c := range th.Lanes {
			if !validColour(c) {
				t.Errorf("%s: Lanes[%d] = %q invalid", th.Name, i, c)
			}
		}
		for i, c := range th.Syntax {
			switch i {
			case 0, 4:
				if c != "" {
					t.Errorf("%s: Syntax[%d] must stay uncoloured, got %q", th.Name, i, c)
				}
			default:
				if !validColour(c) {
					t.Errorf("%s: Syntax[%d] = %q invalid", th.Name, i, c)
				}
			}
		}
	}
	var zero Theme
	for i, c := range zero.roles() {
		if c != "" {
			t.Fatalf("Terminal role %d must be empty (inherit), got %q", i, c)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && go test ./internal/theme/`
Expected: FAIL — package does not exist / undefined symbols.

- [ ] **Step 3: Write the package**

`internal/theme/theme.go`:

```go
// Package theme is gg's pure colour-role catalogue. A Theme names one colour
// per UI role; the TUI builds lipgloss styles from it (internal/tui/styles.go)
// and paints the frame's background from Bg/Fg. The zero Theme (Terminal)
// means "inherit": every role "" keeps the TUI's historical literal and no
// background is painted, so the terminal's own scheme shows through.
//
// Values are "#rrggbb" hex strings or 256-colour indexes ("240"); both are
// passed to lipgloss.Color unchanged. DAG leaf: no gg or lipgloss imports.
package theme

// Theme names. The config value is the English protocol string; only the
// Settings-row rendering is localized.
const (
	NameTerminal = "terminal"
	NameDark     = "dark"
	NameLight    = "light"
)

// Theme is one named colour scheme. Field groups mirror the spec's role table.
type Theme struct {
	Name string

	// Frame: painted under every cell when both are set.
	Bg, Fg string

	// Text tiers.
	Dim, Muted, Bright string

	// Chrome.
	FocusBorder, ModalBorder, TooltipFg, TooltipBg string
	ErrFg, ErrBg, TagDeco                          string

	// Diff / editor surfaces.
	DiffAddBg, DiffDelBg, DiffAddCursorBg, DiffDelCursorBg string
	CursorRowBg, FieldBg, FieldCursorFg, FieldCursorBg     string
	MessageBlockBg, SaveBannerFg, SaveBannerBg             string

	// Signals.
	NoticeHot, NoticeDim, ReviewHot, ReviewDim string
	NoteUser, NoteAgent, NoteStale, PickerLabel string

	// Palettes: index = graph lane % 7, and index = syntax.Class
	// (Plain, Keyword, Type, Func, Name, String, Number, Comment, Operator,
	// Punct, Attr). Syntax[Plain] and Syntax[Name] stay "" by design.
	Lanes  [7]string
	Syntax [11]string
}

// roles returns every scalar role in declaration order (tests + builders).
func (t Theme) roles() []string {
	return []string{
		t.Bg, t.Fg,
		t.Dim, t.Muted, t.Bright,
		t.FocusBorder, t.ModalBorder, t.TooltipFg, t.TooltipBg,
		t.ErrFg, t.ErrBg, t.TagDeco,
		t.DiffAddBg, t.DiffDelBg, t.DiffAddCursorBg, t.DiffDelCursorBg,
		t.CursorRowBg, t.FieldBg, t.FieldCursorFg, t.FieldCursorBg,
		t.MessageBlockBg, t.SaveBannerFg, t.SaveBannerBg,
		t.NoticeHot, t.NoticeDim, t.ReviewHot, t.ReviewDim,
		t.NoteUser, t.NoteAgent, t.NoteStale, t.PickerLabel,
	}
}

// Terminal is the inherit-everything theme (today's look).
var Terminal = Theme{Name: NameTerminal}

// Dark pins Windows Terminal's "Campbell" scheme for the basic ANSI slots gg
// uses and keeps the historical 256-cube accents (already identical
// everywhere). Source: learn.microsoft.com Windows Terminal color schemes.
var Dark = Theme{
	Name: NameDark,
	Bg:   "#0C0C0C", Fg: "#CCCCCC",
	Dim: "#585858", Muted: "#BCBCBC", Bright: "#FFFFFF",
	FocusBorder: "#3B78FF", ModalBorder: "#F9F1A5", TooltipFg: "#0C0C0C", TooltipBg: "#F9F1A5",
	ErrFg: "#E74856", ErrBg: "#C50F1F", TagDeco: "220",
	DiffAddBg: "22", DiffDelBg: "52", DiffAddCursorBg: "28", DiffDelCursorBg: "88",
	CursorRowBg: "237", FieldBg: "236", FieldCursorFg: "236", FieldCursorBg: "250",
	MessageBlockBg: "236", SaveBannerFg: "#F2F2F2", SaveBannerBg: "22",
	NoticeHot: "196", NoticeDim: "124", ReviewHot: "39", ReviewDim: "31",
	NoteUser: "75", NoteAgent: "141", NoteStale: "240", PickerLabel: "245",
	Lanes:  [7]string{"33", "208", "40", "201", "51", "220", "129"},
	Syntax: [11]string{"", "141", "79", "222", "", "150", "215", "245", "252", "250", "180"},
}

// Light is Everforest "light soft" (bg0 #F3EAD3), the warm not-white tier.
// Source: github.com/sainnhe/everforest palette.md. Diff/err backgrounds are
// ~15% tints over bg0.
var Light = Theme{
	Name: NameLight,
	Bg:   "#F3EAD3", Fg: "#5C6A72",
	Dim: "#A6B0A0", Muted: "#829181", Bright: "#3A4A52",
	FocusBorder: "#3A94C5", ModalBorder: "#DFA000", TooltipFg: "#3A4A52", TooltipBg: "#F1E4C5",
	ErrFg: "#F85552", ErrBg: "#F1D1CF", TagDeco: "#DFA000",
	DiffAddBg: "#E1E4BD", DiffDelBg: "#F4D9D4", DiffAddCursorBg: "#CFDAA8", DiffDelCursorBg: "#EBC3BD",
	CursorRowBg: "#E5DFC5", FieldBg: "#E5DFC5", FieldCursorFg: "#F3EAD3", FieldCursorBg: "#5C6A72",
	MessageBlockBg: "#EAE4CA", SaveBannerFg: "#F3EAD3", SaveBannerBg: "#8DA101",
	NoticeHot: "#F85552", NoticeDim: "#B85450", ReviewHot: "#3A94C5", ReviewDim: "#35A77C",
	NoteUser: "#3A94C5", NoteAgent: "#DF69BA", NoteStale: "#A6B0A0", PickerLabel: "#829181",
	Lanes:  [7]string{"#3A94C5", "#F57D26", "#8DA101", "#DF69BA", "#35A77C", "#DFA000", "#F85552"},
	Syntax: [11]string{"", "#DF69BA", "#35A77C", "#DFA000", "", "#8DA101", "#F57D26", "#939F91", "#5C6A72", "#829181", "#B8860B"},
}

var builtins = []Theme{Terminal, Dark, Light}

// Names lists the built-in theme names in Settings-cycle order.
func Names() []string {
	out := make([]string, len(builtins))
	for i, t := range builtins {
		out[i] = t.Name
	}
	return out
}

// Lookup resolves a config value. "" and "terminal" are Terminal. Unknown
// names return ok=false; callers fall back to Terminal and tell the user.
func Lookup(name string) (Theme, bool) {
	if name == "" {
		return Terminal, true
	}
	for _, t := range builtins {
		if t.Name == name {
			return t, true
		}
	}
	return Terminal, false
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && go test ./internal/theme/ && go test ./internal/archtest/`
Expected: PASS (both).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && git add internal/theme && git commit -m "feat(theme): pure colour-role catalogue — terminal (inherit), dark (Campbell), light (Everforest soft)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01U1sb6UKEkBejtJ3hQEmhsX"
```

---

### Task 2: `styles` struct behind an atomic pointer

**Files:**
- Create: `internal/tui/styles.go`
- Test: `internal/tui/styles_test.go`

**Interfaces:**
- Consumes: `theme.Theme`, `theme.Terminal`, `theme.Lookup` (Task 1).
- Produces: `type styles struct{…}` (fields below), `func buildStyles(th theme.Theme) *styles`, `func st() *styles`, `func setTheme(th theme.Theme)`, `func activeTheme() theme.Theme`. Later tasks read `st().<field>`, `st().lane(i)`, `st().syntaxStyle(base, c)`, and `st().frame()` (bg, fg for the paint pass).

- [ ] **Step 1: Write the failing test**

`internal/tui/styles_test.go`:

```go
package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/theme"
)

// buildStyles(theme.Terminal) must reproduce every literal the TUI used before
// themes existed — the guarantee that `terminal` is byte-identical to today.
func TestBuildStylesTerminalPinsLegacyLiterals(t *testing.T) {
	t.Parallel()
	s := buildStyles(theme.Terminal)
	fg := func(st lipgloss.Style) string { return string(st.GetForeground().(lipgloss.Color)) }
	bg := func(st lipgloss.Style) string { return string(st.GetBackground().(lipgloss.Color)) }
	border := func(st lipgloss.Style) string { return string(st.GetBorderTopForeground().(lipgloss.Color)) }
	checks := []struct {
		name string
		got  string
		want string
	}{
		{"focusedPanel border", border(s.focusedPanel), "12"},
		{"bluredPanel border", border(s.bluredPanel), "240"},
		{"modalStyle border", border(s.modalStyle), "11"},
		{"statusErr fg", fg(s.statusErr), "15"},
		{"statusErr bg", bg(s.statusErr), "1"},
		{"error fg", fg(s.errorText), "9"},
		{"dim fg", fg(s.dim), "240"},
		{"tagDeco fg", fg(s.tagDeco), "220"},
		{"pickerLabel fg", fg(s.pickerLabel), "245"},
		{"messageBlock bg", bg(s.messageBlock), "236"},
		{"diffDel bg", bg(s.diffDelCell), "52"},
		{"diffAdd bg", bg(s.diffAddCell), "22"},
		{"diffEmph fg", fg(s.diffEmph), "231"},
		{"diffCursorRow bg", bg(s.diffCursorRow), "237"},
		{"diffAddCursor bg", bg(s.diffAddCursor), "28"},
		{"diffDelCursor bg", bg(s.diffDelCursor), "88"},
		{"diffGapCursor bg", bg(s.diffGapCursor), "237"},
		{"noteFrameUser fg", fg(s.noteFrameUser), "75"},
		{"noteFrameAgent fg", fg(s.noteFrameAgent), "141"},
		{"noteBody fg", fg(s.noteBody), "250"},
		{"field bg", bg(s.field), "236"},
		{"fieldCursor fg", fg(s.fieldCursor), "236"},
		{"fieldCursor bg", bg(s.fieldCursor), "250"},
		{"noticeHot fg", fg(s.noticeHot), "196"},
		{"noticeDim fg", fg(s.noticeDim), "124"},
		{"saveBanner fg", fg(s.saveBanner), "15"},
		{"saveBanner bg", bg(s.saveBanner), "22"},
		{"tooltip fg", fg(s.tooltip), "0"},
		{"tooltip bg", bg(s.tooltip), "11"},
		{"reviewHot fg", fg(s.reviewHot), "39"},
		{"reviewDim fg", fg(s.reviewDim), "31"},
		{"lane 0", string(s.lane(0)), "33"},
		{"lane 8 wraps", string(s.lane(8)), "208"},
		{"lane -1 clamps", string(s.lane(-1)), "33"},
		{"syntax keyword", s.syntaxColor(syntax.Keyword), "141"},
		{"syntax attr", s.syntaxColor(syntax.Attr), "180"},
		{"syntax plain", s.syntaxColor(syntax.Plain), ""},
		{"syntax name", s.syntaxColor(syntax.Name), ""},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	if !s.selectedRow.GetReverse() {
		t.Error("selectedRow must stay Reverse(true)")
	}
	if bgc, fgc := s.frame(); bgc != "" || fgc != "" {
		t.Errorf("terminal frame = (%q, %q), want empty (no paint)", bgc, fgc)
	}
}

func TestBuildStylesDarkAppliesRoles(t *testing.T) {
	t.Parallel()
	s := buildStyles(theme.Dark)
	if got := string(s.focusedPanel.GetBorderTopForeground().(lipgloss.Color)); got != "#3B78FF" {
		t.Fatalf("dark focus border = %q", got)
	}
	if got := string(s.dim.GetForeground().(lipgloss.Color)); got != "#585858" {
		t.Fatalf("dark dim = %q", got)
	}
	// Unchanged 256-cube roles are kept verbatim.
	if got := string(s.diffAddCell.GetBackground().(lipgloss.Color)); got != "22" {
		t.Fatalf("dark diffAdd bg = %q, want 22", got)
	}
	if bgc, fgc := s.frame(); bgc != "#0C0C0C" || fgc != "#CCCCCC" {
		t.Fatalf("dark frame = (%q, %q)", bgc, fgc)
	}
}

// A role left "" in a theme inherits the terminal literal.
func TestBuildStylesEmptyRoleInherits(t *testing.T) {
	t.Parallel()
	th := theme.Dark
	th.Dim = ""
	s := buildStyles(th)
	if got := string(s.dim.GetForeground().(lipgloss.Color)); got != "240" {
		t.Fatalf("empty Dim must inherit 240, got %q", got)
	}
}

// NOTE: serial (no t.Parallel) — setTheme swaps the process-global styles pointer.
func TestSetThemeSwapsAndRestores(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	setTheme(theme.Light)
	if activeTheme().Name != theme.NameLight {
		t.Fatalf("activeTheme = %q after setTheme(Light)", activeTheme().Name)
	}
	if got := string(st().dim.GetForeground().(lipgloss.Color)); got != "#A6B0A0" {
		t.Fatalf("st().dim after Light = %q", got)
	}
	setTheme(theme.Terminal)
	if got := string(st().dim.GetForeground().(lipgloss.Color)); got != "240" {
		t.Fatalf("st().dim after Terminal = %q", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && go test ./internal/tui/ -run 'TestBuildStyles|TestSetTheme' 2>&1 | head`
Expected: FAIL — `undefined: buildStyles` etc.

- [ ] **Step 3: Write `styles.go`**

`internal/tui/styles.go`:

```go
package tui

import (
	"sync/atomic"

	"github.com/charmbracelet/lipgloss"
	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/theme"
)

// styles is every themed lipgloss style the TUI renders with. It is built
// once per theme by buildStyles and read through st(); the active pointer is
// swapped atomically by setTheme so a Settings switch races with nothing
// (TUI tests render in parallel — reassigning package vars would be a data
// race under ./test.sh race).
//
// Field names keep the pre-theme package-var names so call sites read as
// st().focusedPanel where they used to read focusedPanel.
type styles struct {
	th theme.Theme

	// view.go
	titleStyle   lipgloss.Style
	focusedPanel lipgloss.Style
	bluredPanel  lipgloss.Style
	selectedRow  lipgloss.Style
	modalStyle   lipgloss.Style
	statusErr    lipgloss.Style // was statusErrStyle
	errorText    lipgloss.Style // was errorStyle
	reviewHot    lipgloss.Style // was reviewHotStyle
	reviewDim    lipgloss.Style // was reviewDimStyle

	// shared dim/muted/bright tiers (were many "240"/"250"/"231" literals)
	dim    lipgloss.Style // dimIdentStyle, dimRowStyle, pickerDim, conflictSrcStyle, diffGapCell, diffGutter, diffFold, noteFrameStale, noteDim, unsetStyle, langHintStyle
	muted  lipgloss.Style // noteBody
	bright lipgloss.Style // diffEmph (bold), diffCursorNo (bold), noteSummary (bold) — see the bold variants below

	// commit_ident.go
	tagDeco lipgloss.Style

	// conflict_picker.go
	pickerLabel lipgloss.Style

	// content_popup.go
	messageBlock lipgloss.Style
	errModal     lipgloss.Style // modalStyle.BorderForeground(ErrFg)

	// diff_render.go
	diffDelCell    lipgloss.Style
	diffAddCell    lipgloss.Style
	diffGapCell    lipgloss.Style
	diffGutter     lipgloss.Style
	diffFold       lipgloss.Style
	diffEmph       lipgloss.Style
	diffCursorRow  lipgloss.Style
	diffCursorNo   lipgloss.Style
	diffAddCursor  lipgloss.Style
	diffDelCursor  lipgloss.Style
	diffGapCursor  lipgloss.Style
	noteFrameUser  lipgloss.Style
	noteFrameAgent lipgloss.Style
	noteFrameStale lipgloss.Style
	noteSummary    lipgloss.Style
	noteBody       lipgloss.Style
	noteDim        lipgloss.Style

	// field_style.go
	field       lipgloss.Style // was fieldStyle
	fieldCursor lipgloss.Style // was fieldCursorStyle

	// notify.go
	noticeHot lipgloss.Style
	noticeDim lipgloss.Style

	// save_content.go
	saveBanner lipgloss.Style

	// tooltip.go
	tooltip lipgloss.Style

	lanes  [7]lipgloss.Color
	syntax [11]string
}

// legacy is the pre-theme literal for each role: what `terminal` renders.
var legacy = theme.Theme{
	Name: theme.NameTerminal,
	Dim: "240", Muted: "250", Bright: "231",
	FocusBorder: "12", ModalBorder: "11", TooltipFg: "0", TooltipBg: "11",
	ErrFg: "9", ErrBg: "1", TagDeco: "220",
	DiffAddBg: "22", DiffDelBg: "52", DiffAddCursorBg: "28", DiffDelCursorBg: "88",
	CursorRowBg: "237", FieldBg: "236", FieldCursorFg: "236", FieldCursorBg: "250",
	MessageBlockBg: "236", SaveBannerFg: "15", SaveBannerBg: "22",
	NoticeHot: "196", NoticeDim: "124", ReviewHot: "39", ReviewDim: "31",
	NoteUser: "75", NoteAgent: "141", NoteStale: "240", PickerLabel: "245",
	Lanes:  [7]string{"33", "208", "40", "201", "51", "220", "129"},
	Syntax: [11]string{"", "141", "79", "222", "", "150", "215", "245", "252", "250", "180"},
}

// pick returns v, or the legacy literal when the theme leaves the role "".
func pick(v, fallback string) lipgloss.Color {
	if v == "" {
		return lipgloss.Color(fallback)
	}
	return lipgloss.Color(v)
}

// buildStyles materialises a theme. Bg/Fg are NOT applied to styles: they are
// painted under the whole frame by paintFrame (see frame()).
func buildStyles(th theme.Theme) *styles {
	s := &styles{th: th}
	dim := pick(th.Dim, legacy.Dim)
	muted := pick(th.Muted, legacy.Muted)
	bright := pick(th.Bright, legacy.Bright)
	ns := lipgloss.NewStyle

	s.titleStyle = ns().Bold(true)
	s.focusedPanel = ns().Border(lipgloss.RoundedBorder()).BorderForeground(pick(th.FocusBorder, legacy.FocusBorder)).Padding(0, 1)
	s.bluredPanel = ns().Border(lipgloss.RoundedBorder()).BorderForeground(dim).Padding(0, 1)
	s.selectedRow = ns().Reverse(true)
	s.modalStyle = ns().Border(lipgloss.DoubleBorder()).BorderForeground(pick(th.ModalBorder, legacy.ModalBorder)).Padding(1, 2)
	s.statusErr = ns().Bold(true).Foreground(pick(th.SaveBannerFg, legacy.SaveBannerFg)).Background(pick(th.ErrBg, legacy.ErrBg))
	s.errorText = ns().Foreground(pick(th.ErrFg, legacy.ErrFg))
	s.reviewHot = ns().Foreground(pick(th.ReviewHot, legacy.ReviewHot)).Bold(true)
	s.reviewDim = ns().Foreground(pick(th.ReviewDim, legacy.ReviewDim))

	s.dim = ns().Foreground(dim)
	s.muted = ns().Foreground(muted)
	s.bright = ns().Foreground(bright)

	s.tagDeco = ns().Foreground(pick(th.TagDeco, legacy.TagDeco))
	s.pickerLabel = ns().Bold(true).Foreground(pick(th.PickerLabel, legacy.PickerLabel))
	s.messageBlock = ns().Background(pick(th.MessageBlockBg, legacy.MessageBlockBg))
	s.errModal = s.modalStyle.BorderForeground(pick(th.ErrFg, legacy.ErrFg))

	s.diffDelCell = ns().Background(pick(th.DiffDelBg, legacy.DiffDelBg))
	s.diffAddCell = ns().Background(pick(th.DiffAddBg, legacy.DiffAddBg))
	s.diffGapCell = ns().Foreground(dim)
	s.diffGutter = ns().Foreground(dim)
	s.diffFold = ns().Foreground(dim)
	s.diffEmph = ns().Bold(true).Foreground(bright)
	s.diffCursorRow = ns().Background(pick(th.CursorRowBg, legacy.CursorRowBg))
	s.diffCursorNo = ns().Bold(true).Foreground(bright)
	s.diffAddCursor = ns().Background(pick(th.DiffAddCursorBg, legacy.DiffAddCursorBg))
	s.diffDelCursor = ns().Background(pick(th.DiffDelCursorBg, legacy.DiffDelCursorBg))
	s.diffGapCursor = s.diffGapCell.Background(pick(th.CursorRowBg, legacy.CursorRowBg))
	s.noteFrameUser = ns().Foreground(pick(th.NoteUser, legacy.NoteUser))
	s.noteFrameAgent = ns().Foreground(pick(th.NoteAgent, legacy.NoteAgent))
	s.noteFrameStale = ns().Foreground(pick(th.NoteStale, legacy.NoteStale))
	s.noteSummary = ns().Bold(true).Foreground(bright)
	s.noteBody = ns().Foreground(muted)
	s.noteDim = ns().Foreground(dim)

	s.field = ns().Background(pick(th.FieldBg, legacy.FieldBg))
	s.fieldCursor = ns().Background(pick(th.FieldCursorBg, legacy.FieldCursorBg)).Foreground(pick(th.FieldCursorFg, legacy.FieldCursorFg))

	s.noticeHot = ns().Foreground(pick(th.NoticeHot, legacy.NoticeHot)).Bold(true)
	s.noticeDim = ns().Foreground(pick(th.NoticeDim, legacy.NoticeDim))
	s.saveBanner = ns().Bold(true).Foreground(pick(th.SaveBannerFg, legacy.SaveBannerFg)).Background(pick(th.SaveBannerBg, legacy.SaveBannerBg))
	s.tooltip = ns().Foreground(pick(th.TooltipFg, legacy.TooltipFg)).Background(pick(th.TooltipBg, legacy.TooltipBg))

	for i := range s.lanes {
		s.lanes[i] = pick(th.Lanes[i], legacy.Lanes[i])
	}
	for i := range s.syntax {
		if th.Syntax[i] != "" {
			s.syntax[i] = th.Syntax[i]
		} else {
			s.syntax[i] = legacy.Syntax[i]
		}
	}
	return s
}

// lane is the graph lane colour (index by lane % 7, negative clamps to 0).
func (s *styles) lane(lane int) lipgloss.Color {
	if lane < 0 {
		lane = 0
	}
	return s.lanes[lane%len(s.lanes)]
}

// syntaxColor is the palette entry for c ("" for Plain / Name / unknown).
func (s *styles) syntaxColor(c syntax.Class) string {
	if int(c) < len(s.syntax) {
		return s.syntax[c]
	}
	return ""
}

// syntaxStyle returns base with c's foreground applied ("" leaves base alone,
// so an uncoloured class renders byte-identically to the pre-syntax path).
func (s *styles) syntaxStyle(base lipgloss.Style, c syntax.Class) lipgloss.Style {
	if col := s.syntaxColor(c); col != "" {
		return base.Foreground(lipgloss.Color(col))
	}
	return base
}

// frame returns the colours paintFrame lays under the whole screen; both ""
// for terminal (no painting).
func (s *styles) frame() (bg, fg lipgloss.Color) {
	return lipgloss.Color(s.th.Bg), lipgloss.Color(s.th.Fg)
}

var activeStyles atomic.Pointer[styles]

func init() { activeStyles.Store(buildStyles(theme.Terminal)) }

// st is the active style set. Never cache the pointer across a Settings
// switch; read it per render.
func st() *styles { return activeStyles.Load() }

// setTheme swaps the active style set. Callers that own a screen must follow
// it with tea.ClearScreen so unchanged rows are repainted.
func setTheme(th theme.Theme) { activeStyles.Store(buildStyles(th)) }

// activeTheme reports the theme st() was built from.
func activeTheme() theme.Theme { return st().th }
```

Note `statusErr` fg: today's literal is `15` (bright white) which is the same slot as `SaveBannerFg`; the builder reuses that role rather than adding a redundant one.

- [ ] **Step 4: Run to verify it passes**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && go test ./internal/tui/ -run 'TestBuildStyles|TestSetTheme'`
Expected: PASS. (If `GetBorderTopForeground` is not on lipgloss v1.1.0, use `GetBorderLeftForeground`; both exist on `Style`.)

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && git add internal/tui/styles.go internal/tui/styles_test.go && git commit -m "feat(tui): themed styles struct behind an atomic pointer; terminal pins the legacy literals

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01U1sb6UKEkBejtJ3hQEmhsX"
```

---

### Task 3: Migrate every call site to `st()`

**Files:**
- Modify: `internal/tui/view.go:77-89`, `internal/tui/view.go:1201-1202`, `internal/tui/view.go:1550-1580`
- Modify: `internal/tui/diff_render.go:14-40`
- Modify: `internal/tui/commit_ident.go:91,95,107,267-268`
- Modify: `internal/tui/commit_color.go` (whole file)
- Modify: `internal/tui/diff_syntax.go:18-40`
- Modify: `internal/tui/field_style.go:15-17`
- Modify: `internal/tui/notify.go:70-71`
- Modify: `internal/tui/tooltip.go:12`
- Modify: `internal/tui/content_popup.go:75,79,444`
- Modify: `internal/tui/conflict_picker.go:15,17`
- Modify: `internal/tui/conflict_process.go:649`
- Modify: `internal/tui/save_content.go:18-19`
- Modify: `internal/tui/gitconfig_popup.go:656`
- Modify: `internal/tui/language_popup.go:14`
- Test: existing `internal/tui` suite (no new tests; the identity test from Task 2 plus the untouched render tests prove byte-identity).

**Interfaces:**
- Consumes: `st()` and the `styles` fields from Task 2.
- Produces: no package-level `lipgloss.Color("…")` literal remains in non-test `internal/tui` files (verified by grep in Step 3).

- [ ] **Step 1: Record the pre-migration render baseline**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && go test ./internal/tui/ 2>&1 | tail -3`
Expected: `ok` — this is the baseline that must still pass after the migration.

- [ ] **Step 2: Delete the package-level style vars and rewrite each call site**

Per file, remove the `var (...)` blocks listed above and replace uses:

| Old identifier | New expression |
|---|---|
| `titleStyle` | `st().titleStyle` |
| `focusedPanel` / `bluredPanel` / `selectedRow` / `modalStyle` | `st().focusedPanel` / `st().bluredPanel` / `st().selectedRow` / `st().modalStyle` |
| `statusErrStyle` / `errorStyle` | `st().statusErr` / `st().errorText` |
| `reviewHotStyle` / `reviewDimStyle` | `st().reviewHot` / `st().reviewDim` |
| `dimIdentStyle`, `dimRowStyle`, `pickerDim`, `conflictSrcStyle`, `unsetStyle`, `langHintStyle` | `st().dim` |
| `tagDecoStyle` | `st().tagDeco` |
| `pickerLabel` | `st().pickerLabel` |
| `messageBlockStyle` | `st().messageBlock`; delete const `messageBlockColor` |
| `modalStyle.BorderForeground(lipgloss.Color("9"))` (content_popup.go:444) | `st().errModal` |
| `diffDelCell` … `noteDim` (diff_render.go 16-40) | `st().<same name>` |
| `fieldBg` / `fieldStyle` / `fieldCursorStyle` | `st().field.GetBackground()` where the colour itself is needed (grep `fieldBg` uses first) / `st().field` / `st().fieldCursor` |
| `noticeHotStyle` / `noticeDimStyle` | `st().noticeHot` / `st().noticeDim` |
| `saveBannerStyle` (save_content.go:17-19, check the actual var name) | `st().saveBanner` |
| `tooltipStyle` | `st().tooltip` |
| `laneColor(i)` | `st().lane(i)`; delete `commit_color.go`'s `lanePalette` and `laneColor` (keep the file if other helpers live there; otherwise delete it) |
| `syntaxPalette`, `syntaxColor(c)`, `syntaxStyle(base, c)` | delete; call `st().syntaxColor(c)` / `st().syntaxStyle(base, c)` |

For hot render loops (`diff_render.go` row painters, `view.go` commit rows), hoist `s := st()` once at the top of the function and use `s.` inside the loop — one atomic load per frame per function, not per cell.

Also update tests that referenced the old names (`grep -rn 'lanePalette\|laneColor\|syntaxPalette\|syntaxColor\|syntaxStyle\|diffAddCell\|fieldStyle\|tooltipStyle\|modalStyle\|selectedRow' internal/tui/*_test.go`) to the `st()` form.

- [ ] **Step 3: Verify no literal survives**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && grep -n 'lipgloss.Color("' internal/tui/*.go | grep -v _test | grep -v styles.go`
Expected: no output.

- [ ] **Step 4: Build, vet, run the full TUI suite**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && go build ./cmd/gg && go vet ./internal/tui && go test ./internal/tui/ 2>&1 | tail -3`
Expected: `ok` with the same test count as Step 1.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && git add internal/tui && git commit -m "refactor(tui): every colour literal reads from st(); no lipgloss.Color literals outside styles.go

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01U1sb6UKEkBejtJ3hQEmhsX"
```

---

### Task 4: `paintFrame` and its hook in `View()`

**Files:**
- Create: `internal/tui/paint.go`
- Modify: `internal/tui/model.go:3534-3545` (`View`)
- Test: `internal/tui/paint_test.go`

**Interfaces:**
- Consumes: `st().frame()` (Task 2).
- Produces: `func paintFrame(frame string, w, h int, bg, fg lipgloss.Color) string`.

- [ ] **Step 1: Write the failing tests**

`internal/tui/paint_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// NOTE: serial (no t.Parallel) — lipgloss.SetColorProfile is process-global.
func withTrueColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

const (
	bgSeq = "\x1b[48;2;12;12;12m"    // #0C0C0C
	fgSeq = "\x1b[38;2;204;204;204m" // #CCCCCC
	reset = "\x1b[0m"
)

func TestPaintFrameNoThemeIsIdentity(t *testing.T) {
	t.Parallel()
	in := "ab\n" + reset + "c\n"
	if got := paintFrame(in, 10, 5, "", ""); got != in {
		t.Fatalf("empty colours must return the frame unchanged:\n%q\n%q", in, got)
	}
}

func TestPaintFramePadsWidthAndHeight(t *testing.T) {
	withTrueColor(t)
	got := paintFrame("ab\ncd", 4, 3, lipgloss.Color("#0C0C0C"), lipgloss.Color("#CCCCCC"))
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines (padded to h), got %d: %q", len(lines), got)
	}
	want0 := bgSeq + fgSeq + "ab  " + reset
	if lines[0] != want0 {
		t.Fatalf("line0 = %q, want %q", lines[0], want0)
	}
	want2 := bgSeq + fgSeq + "    " + reset
	if lines[2] != want2 {
		t.Fatalf("padded line = %q, want %q", lines[2], want2)
	}
}

func TestPaintFrameReassertsAfterReset(t *testing.T) {
	withTrueColor(t)
	in := "\x1b[1mbold" + reset + "tail"
	got := paintFrame(in, 8, 1, lipgloss.Color("#0C0C0C"), lipgloss.Color("#CCCCCC"))
	want := bgSeq + fgSeq + "\x1b[1mbold" + reset + bgSeq + fgSeq + "tail" + reset
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestPaintFramePadsByDisplayWidth(t *testing.T) {
	withTrueColor(t)
	// 日本 is 4 cells wide; padding to 6 adds two spaces, not four.
	got := paintFrame("日本", 6, 1, lipgloss.Color("#0C0C0C"), lipgloss.Color("#CCCCCC"))
	want := bgSeq + fgSeq + "日本  " + reset
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestPaintFrameNeverTruncates(t *testing.T) {
	withTrueColor(t)
	got := paintFrame("abcdef", 3, 1, lipgloss.Color("#0C0C0C"), lipgloss.Color("#CCCCCC"))
	if !strings.Contains(got, "abcdef") {
		t.Fatalf("over-long line must be left intact: %q", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && go test ./internal/tui/ -run TestPaintFrame 2>&1 | head -3`
Expected: FAIL — `undefined: paintFrame`.

- [ ] **Step 3: Write `paint.go`**

```go
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// paintFrame lays bg/fg under every cell of frame, a w×h screen: each line is
// prefixed with the colour SGR, the SGR is re-asserted after every reset
// lipgloss emits (\x1b[0m), the line is padded with spaces to w display cells
// (ANSI-aware, wide glyphs count 2), and the frame is padded to h lines. Lines
// already ≥ w are never truncated. Empty bg and fg → frame unchanged, so the
// `terminal` theme is byte-identical to the pre-theme renderer.
//
// The SGR comes from a lipgloss style so the colour-profile downgrade
// (truecolor → 256 → 16) applies exactly as it does to every other style.
func paintFrame(frame string, w, h int, bg, fg lipgloss.Color) string {
	if bg == "" && fg == "" {
		return frame
	}
	style := lipgloss.NewStyle()
	if bg != "" {
		style = style.Background(bg)
	}
	if fg != "" {
		style = style.Foreground(fg)
	}
	// Render a single space to harvest the SGR prefix lipgloss emits for
	// this bg/fg; everything up to the space is the escape we re-assert.
	probe := style.Render(" ")
	sgr := probe[:strings.IndexByte(probe, ' ')]
	const reset = "\x1b[0m"

	lines := strings.Split(frame, "\n")
	var b strings.Builder
	b.Grow(len(frame) + (len(lines)+h)*(len(sgr)+len(reset)+8))
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(sgr)
		b.WriteString(strings.ReplaceAll(line, reset, reset+sgr))
		if pad := w - ansi.StringWidth(line); pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
		b.WriteString(reset)
	}
	blank := sgr + strings.Repeat(" ", max(w, 0)) + reset
	for i := len(lines); i < h; i++ {
		b.WriteByte('\n')
		b.WriteString(blank)
	}
	return b.String()
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && go test ./internal/tui/ -run TestPaintFrame -v 2>&1 | tail -8`
Expected: all five PASS. If the SGR ordering lipgloss emits differs from `bgSeq+fgSeq` (e.g. fg first), fix the test constants to match lipgloss's real output — the contract is "whatever lipgloss emits for this style", not a fixed byte order.

- [ ] **Step 5: Hook into `View()`**

In `internal/tui/model.go` replace the body of `View`:

```go
func (m Model) View() string {
	s := st()
	bg, fg := s.frame()
	w, h := m.termSize()
	if m.modal != nil {
		return paintFrame(m.render(), w, h, bg, fg)
	}
	if m.loading && !m.ready {
		return paintFrame("gigagit (loading…)\n", w, h, bg, fg) // startup + repo-switch keep the blank screen
	}
	if m.err != nil {
		return paintFrame(i18n.T("error: %s", m.err.Error())+"\n", w, h, bg, fg)
	}
	return paintFrame(m.render(), w, h, bg, fg)
}
```

`termSize` is the existing helper at `internal/tui/view.go:326-334` (`w, h := m.width, m.height` with the 80×24 floor) — confirm its exact name with `grep -n 'func (m Model) .*() (int, int)' internal/tui/view.go` and use that name.

- [ ] **Step 6: Run the TUI suite**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && go test ./internal/tui/ 2>&1 | tail -3`
Expected: `ok` — the default theme is `terminal`, so every existing render test is unaffected.

- [ ] **Step 7: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && git add internal/tui/paint.go internal/tui/paint_test.go internal/tui/model.go && git commit -m "feat(tui): paintFrame lays the theme bg/fg under every cell; View() paints the final frame

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01U1sb6UKEkBejtJ3hQEmhsX"
```

---

### Task 5: Config entry `[ui] theme`

**Files:**
- Modify: `internal/config/config.go:83-86` (UI struct, after `Language`), `:329-331` (overlay)
- Modify: `internal/config/template.go:49` (settingDocs, after the language row)
- Modify: `internal/config/write.go:58-60` (after `SetGlobalUILanguage`)
- Test: `internal/config/config_test.go`, `internal/config/write_test.go`, `internal/config/populate_test.go`

**Interfaces:**
- Consumes: `theme.Names()` (Task 1) — `config` may import `theme`.
- Produces: `UIConfig.Theme string`, `func SetGlobalUITheme(path, name string) error`, settingDocs row `{"ui", "theme", "terminal", …}`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestOverlayTheme(t *testing.T) {
	t.Parallel()
	if def := Defaults(); def.UI.Theme != "terminal" {
		t.Fatalf("default theme = %q, want terminal", def.UI.Theme)
	}
	dst := UIConfig{Theme: "terminal"}
	overlayUI(&dst, UIConfig{Theme: "dark"})
	if dst.Theme != "dark" {
		t.Fatalf("overlayUI did not propagate Theme, got %q", dst.Theme)
	}
	overlayUI(&dst, UIConfig{})
	if dst.Theme != "dark" {
		t.Fatalf("empty Theme must not clear an existing value, got %q", dst.Theme)
	}
}
```

Append to `internal/config/write_test.go` (copy the file-setup shape of the neighbouring `SetGlobalUILanguage` test — `grep -n 'SetGlobalUILanguage' internal/config/write_test.go`):

```go
func TestSetGlobalUITheme(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\n# theme = \"terminal\"\nlanguage = \"en\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetGlobalUITheme(path, "light"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "theme = \"light\"") {
		t.Fatalf("theme not written:\n%s", raw)
	}
	if !strings.Contains(string(raw), "language = \"en\"") {
		t.Fatalf("sibling key clobbered:\n%s", raw)
	}
	if err := SetGlobalUITheme(path, "dark"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	if strings.Count(string(raw), "theme = ") != 1 || !strings.Contains(string(raw), "theme = \"dark\"") {
		t.Fatalf("second write must replace, not append:\n%s", raw)
	}
}
```

Append to `internal/config/populate_test.go`:

```go
func TestPopulateEmitsTheme(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Populate(path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "theme = \"terminal\"") {
		t.Fatalf("populate must emit [ui] theme with its default:\n%s", raw)
	}
}
```

(Check `Populate`'s real signature with `grep -n '^func Populate' internal/config/populate.go` and match it.)

- [ ] **Step 2: Run to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && go test ./internal/config/ -run 'TestOverlayTheme|TestSetGlobalUITheme|TestPopulateEmitsTheme' 2>&1 | head -5`
Expected: FAIL — undefined `Theme` field / `SetGlobalUITheme`.

- [ ] **Step 3: Implement**

`internal/config/config.go`, after the `Language` field:

```go
	// Theme pins the TUI's colours so gg looks the same in every truecolor
	// terminal: "terminal" (default — inherit the terminal's own scheme,
	// nothing painted), "dark" (Windows Terminal Campbell look), "light"
	// (Everforest light soft). Empty = unset (zero-is-unset overlay rule);
	// resolved to "terminal". Values are theme.Names(); the Settings row cycles
	// them. Under a 256-colour profile hex roles snap to the nearest cube entry;
	// under 16 colours the theme is effectively off.
	Theme string `toml:"theme"`
```

In `Defaults()` add `Theme: "terminal"` to the `UI:` literal (line ~190). In `overlayUI` after the `Language` block:

```go
	if src.Theme != "" {
		dst.Theme = src.Theme
	}
```

`internal/config/template.go`, after the language row:

```go
	{"ui", "theme", "terminal", "TUI colours: terminal (default; inherit the terminal's own scheme), dark (Windows Terminal Campbell look, pinned everywhere), light (Everforest light soft); cycle from the , Settings menu"},
```

`internal/config/write.go`, after `SetGlobalUILanguage`:

```go
// SetGlobalUITheme persists `[ui] theme = "<name>"` to the GLOBAL config
// (callers pass DefaultGlobalPath() — a theme is per-human, like language),
// preserving comments. The normal [ui] overlay still lets a repo .gg.toml
// override it.
func SetGlobalUITheme(path, name string) error {
	return setScalarLine(path, "ui", "theme", strconv.Quote(name))
}
```

- [ ] **Step 4: Run to verify they pass, plus the whole config package**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && go test ./internal/config/ 2>&1 | tail -3`
Expected: `ok`. If `TestPopulateEmptyAddsAllKeys` or a template golden test counts keys, update its expectation for the new row.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && git add internal/config && git commit -m "feat(config): [ui] theme = terminal|dark|light with overlay, populate row and global writer

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01U1sb6UKEkBejtJ3hQEmhsX"
```

---

### Task 6: TUI wiring — startup, config arrival, Settings row, i18n

**Files:**
- Modify: `internal/tui/run.go:37-42` (after the `LogOperations` block)
- Modify: `internal/tui/model.go:936-943` (`configReadyMsg`) and `:1003-1008` (legacy `dataLoadedMsg` path)
- Modify: `internal/tui/settings_popup.go:61-67` (menu ids + order), `:104-105` (label), `:161-162` (row value), `:456-457` (enter handler), `:192-207` (add `cycleTheme` next to `toggleShowGraph`)
- Modify: `internal/i18n/lang/ja.toml`, `ko.toml`, `zh.toml`, `ru.toml`
- Test: `internal/tui/theme_setting_test.go`

**Interfaces:**
- Consumes: `theme.Lookup`, `theme.Names` (Task 1); `setTheme`, `activeTheme` (Task 2); `config.SetGlobalUITheme`, `cfg.UI.Theme` (Task 5).
- Produces: `func (m Model) applyTheme() Model`, `func (m Model) cycleTheme() (Model, tea.Cmd)`, `settingsMenuTheme = "Theme"`.

- [ ] **Step 1: Write the failing tests**

`internal/tui/theme_setting_test.go`:

```go
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/theme"
)

// These tests swap the process-global styles pointer, so they are SERIAL
// (no t.Parallel) and restore the previous theme on exit.

func themeCfg(v string) config.Config {
	c := config.Defaults()
	c.UI.Theme = v
	return c
}

func TestThemeAppliesOnConfigReady(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m := newTestModelForReload(t)
	m.refreshLastRun = map[refreshItem]time.Time{}
	nm, _ := m.Update(configReadyMsg{cfg: themeCfg("dark")})
	if activeTheme().Name != theme.NameDark {
		t.Fatalf("configReadyMsg with theme=dark must activate dark, got %q", activeTheme().Name)
	}
	nm2, _ := nm.(Model).Update(configReadyMsg{cfg: themeCfg("")})
	_ = nm2
	if activeTheme().Name != theme.NameTerminal {
		t.Fatalf("empty theme must fall back to terminal, got %q", activeTheme().Name)
	}
}

func TestThemeUnknownFallsBackWithNotice(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m := newTestModelForReload(t)
	m.refreshLastRun = map[refreshItem]time.Time{}
	nm, _ := m.Update(configReadyMsg{cfg: themeCfg("solarized")})
	if activeTheme().Name != theme.NameTerminal {
		t.Fatalf("unknown theme must fall back to terminal, got %q", activeTheme().Name)
	}
	if !strings.Contains(nm.(Model).statusMsg, "solarized") {
		t.Fatalf("status must name the unknown theme, got %q", nm.(Model).statusMsg)
	}
}

func TestCycleThemePersistsAndClears(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	m, dir := settingsModel(t)
	m.globalConfigPath = filepath.Join(dir, "config.toml")
	m.cfg.UI.Theme = "terminal"

	m, cmd := m.cycleTheme()
	if m.cfg.UI.Theme != "dark" || activeTheme().Name != theme.NameDark {
		t.Fatalf("terminal → dark expected, cfg=%q active=%q", m.cfg.UI.Theme, activeTheme().Name)
	}
	if cmd == nil {
		t.Fatal("cycleTheme must return tea.ClearScreen so stale rows repaint")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("ClearScreen cmd produced nil msg")
	}
	raw, err := os.ReadFile(m.globalConfigPath)
	if err != nil {
		t.Fatalf("cycle must write the global config: %v", err)
	}
	if !strings.Contains(string(raw), `theme = "dark"`) {
		t.Fatalf("config missing theme = \"dark\":\n%s", raw)
	}

	m, _ = m.cycleTheme()
	if m.cfg.UI.Theme != "light" {
		t.Fatalf("dark → light expected, got %q", m.cfg.UI.Theme)
	}
	m, _ = m.cycleTheme()
	if m.cfg.UI.Theme != "terminal" || activeTheme().Name != theme.NameTerminal {
		t.Fatalf("light → terminal expected, got %q", m.cfg.UI.Theme)
	}
}

func TestSettingsThemeRowShowsValue(t *testing.T) {
	m, _ := settingsModel(t)
	m.cfg.UI.Theme = "light"
	row := m.settingsRowLabel(settingsMenuTheme)
	if !strings.Contains(row, "light") {
		t.Fatalf("row = %q, want it to show the active value", row)
	}
}

var _ tea.Cmd = tea.ClearScreen
```

Check the real names before running: `settingsModel` exists (`show_graph_setting_test.go:62`); the row-label function is the one at `settings_popup.go:150-175` — `grep -n '^func (m Model) settings.*(id string) string' internal/tui/settings_popup.go` — use its actual name for `settingsRowLabel`. The global-config path field: `grep -n 'DefaultGlobalPath()' internal/tui/*.go` shows how `openLanguagePicker` writes; if the model has no such field, add `globalConfigPath string` to `Model` (defaulting to `config.DefaultGlobalPath()` in `New`) so tests can redirect it — mirror `repoConfigPath`.

- [ ] **Step 2: Run to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && go test ./internal/tui/ -run 'TestTheme|TestCycleTheme|TestSettingsThemeRow' 2>&1 | head -5`
Expected: FAIL — undefined `cycleTheme`, `settingsMenuTheme`.

- [ ] **Step 3: Implement `applyTheme` and `cycleTheme`**

In `internal/tui/settings_popup.go` after `toggleShowGraph`:

```go
// applyTheme activates [ui] theme from m.cfg. Unknown names fall back to
// terminal and say so in the status bar; the config value is left as written
// so the user can see and fix it.
func (m Model) applyTheme() Model {
	th, ok := theme.Lookup(m.cfg.UI.Theme)
	if !ok {
		m.statusMsg = i18n.T("theme %q unknown — using terminal (terminal, dark, light)", m.cfg.UI.Theme)
	}
	setTheme(th)
	return m
}

// cycleTheme steps terminal → dark → light → terminal, persists the choice to
// the GLOBAL config (a theme is per-human, like language), swaps the styles
// and returns tea.ClearScreen so every row repaints under the new colours.
func (m Model) cycleTheme() (Model, tea.Cmd) {
	names := theme.Names()
	cur := m.cfg.UI.Theme
	if cur == "" {
		cur = theme.NameTerminal
	}
	next := names[0]
	for i, n := range names {
		if n == cur {
			next = names[(i+1)%len(names)]
			break
		}
	}
	m.cfg.UI.Theme = next
	m = m.applyTheme()
	if err := config.SetGlobalUITheme(m.globalConfigPath, next); err != nil {
		m.statusMsg = i18n.T("theme → %s (not saved: %s)", next, err.Error())
	} else {
		m.statusMsg = i18n.T("theme: %s", next)
	}
	return m, tea.ClearScreen
}
```

Add `settingsMenuTheme = "Theme"` to the const block at line 61 and insert it in `settingsMenu` right after `settingsMenuLanguage`. Label case (line ~104): `case settingsMenuTheme: return i18n.T("Theme")`. Row value case (line ~161):

```go
	case settingsMenuTheme:
		name := m.cfg.UI.Theme
		if name == "" {
			name = theme.NameTerminal
		}
		return title + ": " + themeDisplayName(name)
```

with, in `internal/tui/i18n_display.go`:

```go
// themeDisplayName localizes a theme name for the Settings row; the config
// value itself stays the English protocol string.
func themeDisplayName(name string) string {
	switch name {
	case theme.NameDark:
		return i18n.T("dark")
	case theme.NameLight:
		return i18n.T("light")
	default:
		return i18n.T("terminal")
	}
}
```

Enter handler (line ~456): `case settingsMenuTheme: return m.cycleTheme() // stays open so the flip is visible`.

Config arrival, `model.go` `configReadyMsg` handler after `m = m.applyLanguage()`: add `m = m.applyTheme()`. Legacy `dataLoadedMsg` path (line ~1003, after `m.repoConfigPath = msg.repoTOML`): add `m = m.applyTheme()` too (a repo `.gg.toml` may override the theme, exactly like language).

Startup, `run.go` after the `LogOperations` block:

```go
	// Paint the very first frame in the configured theme; configReadyMsg
	// re-applies it (and any repo override) once the registry loads.
	if th, ok := theme.Lookup(cfg.UI.Theme); ok {
		setTheme(th)
	}
```

- [ ] **Step 4: Add the i18n keys**

Append to each of `internal/i18n/lang/{ja,ko,zh,ru}.toml`, next to the `"Show graph"` entry (line ~132):

```toml
"Theme" = "テーマ"
"terminal" = "ターミナル"
"dark" = "ダーク"
"light" = "ライト"
"theme: %s" = "テーマ: %s"
"theme → %s (not saved: %s)" = "テーマ → %s（未保存: %s）"
"theme %q unknown — using terminal (terminal, dark, light)" = "テーマ %q は不明です — terminal を使用します（terminal, dark, light）"
```

ko: `"Theme" = "테마"`, `"terminal" = "터미널"`, `"dark" = "다크"`, `"light" = "라이트"`, `"theme: %s" = "테마: %s"`, `"theme → %s (not saved: %s)" = "테마 → %s (저장 안 됨: %s)"`, `"theme %q unknown — using terminal (terminal, dark, light)" = "테마 %q 을(를) 알 수 없음 — terminal 사용 (terminal, dark, light)"`.
zh: `"Theme" = "主题"`, `"terminal" = "终端"`, `"dark" = "深色"`, `"light" = "浅色"`, `"theme: %s" = "主题：%s"`, `"theme → %s (not saved: %s)" = "主题 → %s（未保存：%s）"`, `"theme %q unknown — using terminal (terminal, dark, light)" = "未知主题 %q — 使用 terminal（terminal、dark、light）"`.
ru: `"Theme" = "Тема"`, `"terminal" = "терминал"`, `"dark" = "тёмная"`, `"light" = "светлая"`, `"theme: %s" = "тема: %s"`, `"theme → %s (not saved: %s)" = "тема → %s (не сохранено: %s)"`, `"theme %q unknown — using terminal (terminal, dark, light)" = "тема %q неизвестна — используется terminal (terminal, dark, light)"`.

If a key such as `"dark"` already exists in a bundle, keep the existing translation and do not duplicate the line (TOML rejects duplicate keys).

- [ ] **Step 5: Run the TUI + i18n suites**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && go build ./cmd/gg && go test ./internal/tui/ ./internal/i18n/ 2>&1 | tail -4`
Expected: `ok` for both, including `i18n_scan_test.go`, `options_vocab_test.go`, `menu_labels_test.go` (the Settings menu-labels test may list menu ids — add `settingsMenuTheme` there if it fails).

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && git add internal/tui internal/i18n && git commit -m "feat(tui): Theme row in , Settings cycles terminal/dark/light, persists globally, applies at startup and on config arrival

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01U1sb6UKEkBejtJ3hQEmhsX"
```

---

### Task 7: Headless visual check, docs tax, verify binary

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md` (package map, one row), `docs/CLAUDE-details.md`
- Create: `docs/superpowers/plans/2026-09-09-tui-themes-captures/` (three capture snapshots, optional) — or attach to the merge message.

- [ ] **Step 1: Capture all three themes headlessly**

Run (see the `driving-tui-headless` skill for the keyscript syntax):

```bash
cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && go build -o /tmp/claude-1000/gg-themes ./cmd/gg
for th in terminal dark light; do
  printf '[ui]\ntheme = "%s"\n' "$th" > /tmp/claude-1000/gg-theme-$th.toml
  XDG_CONFIG_HOME=/tmp/claude-1000/xdg-$th mkdir -p /tmp/claude-1000/xdg-$th/gg && cp /tmp/claude-1000/gg-theme-$th.toml /tmp/claude-1000/xdg-$th/gg/config.toml
  XDG_CONFIG_HOME=/tmp/claude-1000/xdg-$th GG_BIN=/tmp/claude-1000/gg-themes ./tui-capture.sh "wait:2 key:enter wait:1 key:, wait:1" 2>&1 | tail -2
done
```

(Adapt env-var and flag names to what `tui-capture.sh --help` documents; the skill file is authoritative.) Read the plain-text snapshots: the `terminal` one must equal a capture from `main`'s binary byte-for-byte (`diff`), the `dark`/`light` ones must show padded full-width rows (every line the same width) and the Settings popup's `Theme:` row.

- [ ] **Step 2: Docs**

`CHANGELOG.md` — new entry under Unreleased:

```markdown
### TUI themes
- `[ui] theme = "terminal" | "dark" | "light"` (default `terminal`, unchanged look). `dark` pins the Windows Terminal Campbell look everywhere; `light` is Everforest light soft. The `,` Settings menu's new **Theme** row cycles them live and saves to the global config. Under a 256-colour profile hex colours snap to the nearest cube entry; with only 16 colours the theme is effectively off.
```

`README.md` — in the configuration section, add the `theme` key with the same three-value sentence and the profile caveat.

`CLAUDE.md` package map — one row after `syntax`:

```
| `theme`      | Pure colour-role catalogue (`Terminal` zero-value = inherit, `Dark` Campbell, `Light` Everforest soft) behind `[ui] theme`; the TUI builds lipgloss styles from it (`styles.go`) and paints the frame (`paint.go`). DAG leaf. |
```

`docs/CLAUDE-details.md` — a `theme` section: roles table pointer to the spec, the `st()` atomic-pointer rule (never cache across a switch, hoist once per render function), `paintFrame` contract, the serial-test rule for theme swaps, and the profile caveat.

- [ ] **Step 3: Full unit stage + race on the touched packages**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && ./test.sh unit 2>&1 | tail -5 && go test -race ./internal/theme ./internal/tui ./internal/config 2>&1 | tail -3`
Expected: unit stage green; race clean (the race run on a quiet machine — see memory `race-gate-needs-a-quiet-machine`).

- [ ] **Step 4: Commit and deliver the verify binary**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && git add CHANGELOG.md README.md CLAUDE.md docs/CLAUDE-details.md && git commit -m "docs: TUI themes — CHANGELOG, README, package map, details

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01U1sb6UKEkBejtJ3hQEmhsX"
cd /mnt/t/others/gigagit.worktrees/feat-tui-themes && ./build.sh linux 2>&1 | tail -2
```

Then SendUserFile the built `gg` (absolute path) with the one-line try-out: `gg` → `,` → **Theme** → enter cycles; or `theme = "dark"` under `[ui]` in `~/.config/gg/config.toml`.

---

## Self-review

- **Spec coverage:** §3 package → Task 1; §3.1 roles → Task 1 literals + Task 2 `legacy`; §4.1 styles/atomic → Tasks 2–3; §4.2 paint pass → Task 4; §4.3 startup + live switch + ClearScreen → Task 6; §4.4 profile limits → docs (Task 7) + TrueColor pin in Task 4 tests; §5 config → Task 5 (global writer per the language precedent); §6 i18n → Task 6; §7 tests → each task; the spec's "golden fixture frame" identity test is realised as the literal-pinning `TestBuildStylesTerminalPinsLegacyLiterals` plus `TestPaintFrameNoThemeIsIdentity` plus the untouched existing render suite — cheaper and equally binding; §8 docs → Task 7; §9 out of scope respected.
- **Placeholders:** none; every step has code or an exact command. Two "confirm the real name" notes (`termSize`, the Settings row-label function, `Populate` signature) point at a grep, not at guesswork.
- **Type consistency:** `styles` field names in Task 2's test, builder and Task 3's table agree (`statusErr`, `errorText`, `field`, `fieldCursor`, `saveBanner`, `tooltip`, `errModal`); `setTheme`/`activeTheme`/`st` used identically in Tasks 4 and 6; `theme.NameTerminal/NameDark/NameLight` and `theme.Names()` used identically in Tasks 1, 5, 6.
