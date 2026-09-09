# TUI themes — design

Date: 2026-09-09 · Branch: `feat/tui-themes` · Status: approved in chat, spec for review

## 1. Problem

gg looks different in every terminal. Two causes, both visible in the user's
WSL (Windows Terminal, "Campbell" scheme) vs Ubuntu (GNOME Terminal default)
screenshots:

- **gg never paints a background.** ~120 `Background()` clears and only a
  handful of coloured backgrounds (diff add/del cells, field boxes). Every
  unpainted cell shows the terminal's own background: black on WSL, purple on
  Ubuntu.
- **A dozen colours use basic ANSI slots** (`0`, `1`, `9`, `11`, `12`, `15`)
  that every terminal scheme remaps. The rest are fixed 256-cube values
  (`240`, `220`, `75`, …) that already look the same everywhere.

Goal: a `theme` setting with three values — `terminal` (today's behaviour,
byte-identical), `dark` (the WSL/Campbell look pinned everywhere), `light`
(a warm, not-white light scheme) — so gg renders identically in every
truecolor terminal.

## 2. Approach

**Paint pass + pinned palette.** After `View()` renders the frame, a pure
post-processor paints the theme's bg/fg under every cell and re-asserts them
after every SGR reset lipgloss emits; every colour literal in the TUI becomes a
role looked up from the active theme.

Rejected: OSC 10/11 sequences that recolour the terminal itself. A crash
leaves the user's terminal recoloured, tmux and several emulators ignore the
sequences, and it cannot be unit-tested.

## 3. Package `internal/theme` (new, pure DAG leaf)

No lipgloss / termenv / tui imports.

```go
type Theme struct {
    Name string
    // Frame
    Bg, Fg string
    // Text tiers
    Dim, Muted, Bright string
    // Chrome
    FocusBorder, ModalBorder, TooltipFg, TooltipBg string
    ErrFg, ErrBg, TagDeco string
    // Diff / editor surfaces
    DiffAddBg, DiffDelBg, DiffAddCursorBg, DiffDelCursorBg string
    CursorRowBg, FieldBg, FieldCursorFg, FieldCursorBg string
    MessageBlockBg, SaveBannerFg, SaveBannerBg string
    // Signals
    NoticeHot, NoticeDim, ReviewHot, ReviewDim string
    NoteUser, NoteAgent, NoteStale, PickerLabel string
    // Palettes (fixed length; index = lane / syntax.Class)
    Lanes  [7]string
    Syntax [11]string
}

var Terminal Theme            // zero value: every role "" = inherit today's literal
var Dark, Light Theme
func Lookup(name string) (Theme, bool)   // "", "terminal" → Terminal
func Names() []string                    // terminal, dark, light (Settings cycle order)
```

Colour values are hex strings (`#rrggbb`) or 256-cube indexes (`"240"`); the
TUI passes them to `lipgloss.Color` unchanged. A role that is `""` means
"keep the `terminal` literal for this role". `Terminal` therefore stays valid
even if a future theme leaves a role unset.

### 3.1 Role table

"unchanged" = keep today's 256-cube literal (already identical everywhere).

| Role | Today | dark (Campbell) | light (neutral grey) |
|---|---|---|---|
| Bg / Fg | none | `#0C0C0C` / `#CCCCCC` | `#E9E9E5` / `#33393F` |
| Dim | `240` | `#585858` | `#8A8F8A` |
| Muted | `250` | `#BCBCBC` | `#5F6570` |
| Bright | `231` | `#FFFFFF` | `#1F2428` |
| FocusBorder | `12` | `#3B78FF` | `#2F6FB8` |
| ModalBorder | `11` | `#F9F1A5` | `#B08000` |
| TooltipFg / TooltipBg | `0` / `11` | `#0C0C0C` / `#F9F1A5` | `#33393F` / `#F1E7B5` |
| ErrFg / ErrBg | `9` / `1` | `#E74856` / `#C50F1F` | `#C0392B` / `#F1D5D2` |
| StatusErrFg | `15` | `#F2F2F2` | `#33393F` |
| TagDeco | `220` | unchanged | `#B08000` |
| DiffAddBg / DiffDelBg | `22` / `52` | unchanged | `#D8EAD0` / `#F1D5D2` |
| DiffAddCursorBg / DiffDelCursorBg | `28` / `88` | unchanged | `#C5DDB9` / `#E6C2BE` |
| CursorRowBg | `237` | unchanged | `#DADAD5` |
| FieldBg | `236` | unchanged | `#DADAD5` |
| FieldCursorFg / FieldCursorBg | `236` / `250` | unchanged | `#E9E9E5` / `#33393F` |
| MessageBlockBg | `236` | unchanged | `#DFDFDA` |
| SaveBannerFg / SaveBannerBg | `15` / `22` | `#F2F2F2` / unchanged | `#E9E9E5` / `#3E8E41` |
| NoticeHot / NoticeDim | `196` / `124` | unchanged | `#C0392B` / `#A0564C` |
| ReviewHot / ReviewDim | `39` / `31` | unchanged | `#2F6FB8` / `#2A8C8C` |
| NoteUser / NoteAgent / NoteStale | `75` / `141` / `240` | unchanged | `#2F6FB8` / `#6B4FBB` / `#8A8F8A` |
| PickerLabel | `245` | unchanged | `#5F6570` |
| Lanes[7] | `33 208 40 201 51 220 129` | unchanged | `#2F6FB8 #C7641B #3E8E41 #6B4FBB #2A8C8C #B08000 #C0392B` |
| Syntax[11] (Plain,Keyword,Type,Func,Name,String,Number,Comment,Operator,Punct,Attr) | `"" 141 79 222 "" 150 215 245 252 250 180` | unchanged | `"" #6B4FBB #2A8C8C #B08000 "" #3E8E41 #C7641B #8A8F8A #33393F #5F6570 #A0682A` |
| Selection | `Reverse(true)` | Reverse | Reverse |

Sources: Windows Terminal Campbell scheme (learn.microsoft.com) for `dark`.
`light` is a neutral light-grey scheme modelled on a charcoal-on-off-white
nvim look: an off-white grey ground (`#E9E9E5`) with charcoal text
(`#33393F`) and muted, desaturated accents. It replaced a first cut built on
Everforest "light soft" (`#F3EAD3`), which the user rejected after seeing it
— the cream ground read as too colourful and the `#5C6A72` text as too
light. Light diff/err backgrounds are low-saturation tints over bg0 with the
cursor tints one step darker; the implementer may nudge them by eye under
tui-capture but must keep add ≠ del hue and cursor ≠ plain luminance.

`StatusErrFg` was split off from `SaveBannerFg` after a final-review finding
that reusing it for the error status bar gave a 1.19:1 contrast ratio under
the then-current Light palette (its frame bg on its `ErrBg`), so the two
roles now vary independently.

## 4. TUI wiring

### 4.1 Styles behind one accessor

Today's ~46 package-level `lipgloss.Style` vars (`view.go`, `diff_render.go`,
`commit_ident.go`, `field_style.go`, `notify.go`, `tooltip.go`,
`content_popup.go`, `conflict_picker.go`, `conflict_process.go`,
`save_content.go`, `gitconfig_popup.go`, `language_popup.go`,
`commit_color.go` lanes, `diff_syntax.go` palette) move into one
`styles` struct in `internal/tui/styles.go`, built by
`buildStyles(theme.Theme) *styles`, stored in an `atomic.Pointer[styles]`
and read through `st()` at every call site (`st().dim`, `st().lane(i)`,
`st().syntax(c)`).

Why a pointer swap and not reassigning globals: TUI tests run with
`t.Parallel()`; reassigning package vars during a theme switch is a data race
under `./test.sh race`. With the pointer, only the tests that *switch* themes
stay serial (same precedent as the `SetColorProfile` tests), everyone else
reads a stable snapshot.

`buildStyles(theme.Terminal)` must produce exactly today's literals (see §7
identity test). For any role that is `""`, the builder falls back to the
`terminal` literal.

### 4.2 Paint pass

`internal/tui/paint.go`:

```go
// paintFrame paints bg/fg under every cell of frame, a w×h screen.
// Empty bg and fg → returns frame unchanged.
func paintFrame(frame string, w, h int, bg, fg lipgloss.Color) string
```

Per line: prefix the SGR for bg+fg (rendered via lipgloss so the colour
profile downgrade applies), replace every `\x1b[0m` with `\x1b[0m` +
that SGR, pad with spaces to `w` measured by `ansi.StringWidth` (wide glyphs
count 2), then pad the line count to `h` with fully painted blank lines.
Lines already ≥ `w` are left alone (never truncate). `View()` calls it last.

Grep of `internal/tui` non-test files found no hand-built escapes (`\x1b`,
`termenv.`), only `ansi.StringWidth/Truncate` helpers — so lipgloss's `\x1b[0m`
is the only reset to handle.

### 4.3 Startup and live switch

- `run.go` reads `cfg.UI.Theme`, `theme.Lookup`s it (unknown → `terminal`,
  plus a startup notice via the existing config-error path) and swaps the
  styles pointer before `tea.NewProgram`.
- The Settings popup gains a row `Theme: terminal ▸ dark ▸ light` that cycles
  through `theme.Names()`, writes `[ui] theme` with the scoped line-edit
  writer (same path as `language`), swaps the styles pointer, and returns
  `tea.ClearScreen` so the diff renderer's changed-line optimisation cannot
  leave stale-coloured cells.

### 4.4 Colour-profile limits (documented, not fixed)

Under a 256-colour profile (tmux without `Tc`, no `COLORTERM`) hex roles snap
to the nearest cube entry — still consistent, slightly off-hue. Under a
16-colour profile they snap back to the terminal palette and the theme is
effectively off. README states this; tests pin `termenv.TrueColor`.

## 5. Config

Per the `adding-config-entries` skill:

- `UI.Theme string \`toml:"theme"\`` on the `[ui]` table; default
  `"terminal"`; field-level overlay (defaults → global → repo) like
  `Language`; validation accepts only `theme.Names()`. `config` imports
  `theme` (a leaf with no imports, so DAG-safe) rather than duplicating the
  name list.
- `settingDoc` entry for the settings registry.
- `gg config populate` emits the key with its comment.
- Writer: scoped line edit of `[ui] theme` (global by default, repo when
  the Settings popup is in repo scope — same as language).

## 6. i18n

Settings row label and the three option values go through `i18n.T` with a
case in `i18n_display.go` and entries in all four bundles (ja/ko/zh/ru); the
option VALUES written to config stay English.

## 7. Tests (TDD)

- `internal/theme`: `Lookup` for `""`/`terminal`/`dark`/`light`/unknown;
  `Names()` order; every non-empty role parses as `#rrggbb` or a 0–255 index;
  `Dark` and `Light` have no empty role except `Syntax[Plain]`/`Syntax[Name]`.
- `paintFrame`: empty colours → identical string; ASCII line padded to `w`;
  line containing a wide glyph padded by display width; a styled segment
  followed by text keeps bg after the reset; short frame padded to `h`;
  over-long line untouched.
- `buildStyles(theme.Terminal)` identity: a fixture frame (Branches +
  Commits + diff view + a modal) rendered before/after the refactor is
  byte-identical (fixture captured on `main` before the styles move, committed
  as golden). Guards existing snapshot tests and tui-capture output.
- Config: overlay precedence, populate output, unknown value rejected.
- Settings popup: row cycles, writes, returns `ClearScreen`; serial test.
- i18n gates already fail on a missing key.
- Manual: `./tui-capture.sh` under all three themes; screenshots attached to
  the PR/merge message.

## 8. Docs tax

CHANGELOG (always), README (new `[ui] theme` + profile caveat), settings
registry memory, `docs/CLAUDE-details.md` row for `theme`; CLAUDE.md package
map gains one line for `theme`. No CLI surface change → no `using-gg` bump.

## 9. Out of scope

- Web UI (own CSS, dark only) — separate item.
- `--theme` CLI flag / env override.
- User-defined themes in config (roles are exported; a later `[theme]` table
  could override them without redesign).
