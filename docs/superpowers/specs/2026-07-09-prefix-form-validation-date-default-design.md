# Prefix form: inline validation, bare `<date>` default, date-format help

**Date:** 2026-07-09
**Status:** Approved
**Branch:** `feat/prefix-form-date-help`

## Problem

Three gaps in the branch-prefix editing experience (Settings → Branch prefixes):

1. **A validation error throws the user's input away.** The add-prefix form
   (`internal/tui/prefix_settings.go`) flips back to browse mode on Enter
   *before* the async `AddPrefix` runs. When `domain.ValidatePrefixValue`
   rejects the value (unknown token, malformed argument), the error lands in
   the bottom status bar while the form — and the typed value — is already
   gone. The user must reopen the form and retype everything.
2. **`<date>` without a format is an error.** `template.resolveToken` refuses
   a bare `<date>` ("requires a format"), and `<date:>` (colon, empty format)
   silently resolves to an empty string. A sensible default exists:
   `yyyy-MM-dd`.
3. **No discoverable reference for date formats.** The form shows one
   `Tokens:` hint line; nothing explains which date format tokens exist
   (`yyyy`, `MM`, `dd`, `HH`, `mm`, `ss`) or shows examples.

## Design

### 1. Synchronous inline validation keeps the form open

On Enter in the add-prefix form (`prefixSettingsView.updateForm`):

- Call `domain.ValidatePrefixValue(p.Value)` **synchronously** before
  dispatching anything. The function is pure (no git, no I/O), and the TUI
  already imports `internal/domain`, so this respects the archtest layering.
- On error: stay in `pfForm`, keep the typed value and scope untouched, and
  store the error message in a new `formErr string` field on
  `prefixSettingsView`. The form's `box` render shows it as a red line (the
  existing error style used elsewhere in popups) under the fields, e.g.:

  ```
  invalid prefix: unknown token <datee>
  ```

- On success: clear `formErr`, flip to browse, and dispatch `addPrefixCmd` as
  today. `AddPrefix` re-validates internally (defense in depth — the CLI path
  depends on it); a store I/O failure still surfaces via `prefixDataMsg.err`
  in the bottom bar, as today. That path is rare and not worth reopening the
  form for.
- `formErr` is cleared when the form is (re)opened (`n`/`a` reset it along
  with the other fields) and on a successful Enter. It persists while the
  user edits — the next Enter recomputes it.
- The empty-value case ("prefix value is required"), currently a bottom-bar
  `statusMsg`, moves into the same inline `formErr` line for consistency.

**Rejected alternative:** keep the dispatch async and reopen the form when
the error message returns. More plumbing (distinguishing add-errors from
list-load errors in `prefixDataMsg`), and the form visibly flickers
closed-then-open.

### 2. Bare `<date>` defaults to `yyyy-MM-dd`

In `template.resolveToken` (`internal/template/template.go`), the `"date"`
case:

- No colon (`<date>`) **or** an empty format (`<date:>`) → resolve with the
  Go layout `"2006-01-02"`, i.e. gg's human format `yyyy-MM-dd`.
- A non-empty format behaves exactly as today (`goLayout(rest)`).
- The `Ctx.Now == nil` guard stays and now also covers the bare form.

Because every template consumer funnels through `Resolve`, the default
applies uniformly: branch prefixes, prefix validation
(`domain.ValidatePrefixValue`), the worktree `path_template`, and the
create-branch/create-worktree prefix pickers.

Note: `yyyy-mm-dd` (lowercase `mm`) would be minutes in gg's mapping; the
canonical spelling in all docs/hints is `yyyy-MM-dd`. The underlying Go
layout is the reference time `2006-01-02`.

### 3. `ctrl+d` date-format cheat sheet over the form

- While the add-prefix form is open, **`ctrl+d`** pushes a help sheet using
  the existing `contentPopup` cheat-sheet mechanism (`popup_help.go`
  pattern): scrollable, `/`-searchable, `esc`/`q` returns to the form with
  all typed state intact.
- `ctrl+d` is safe while typing: the form's `textfield` does not consume it
  (it handles runes, space, backspace/ctrl+h, delete, arrows,
  ctrl+left/right, home/end, ctrl+w only). Precedent: `ctrl+d` already
  exists as a popup-level key in the repo switcher.
- Sheet title: `Prefix tokens & date formats`. Content:
  - one `cheatRow`-style line per token: `<user:LABEL>`, `<seq:NAME>`,
    `<seq:NAME:N>`, `<date>`, `<date:FMT>`, `<parent-branch>`, `<repo>`,
    `<random-alpha:N>`, `<random-num:N>` — each with a one-line description;
  - a date-format table: `yyyy` year, `MM` month (01–12), `dd` day (01–31),
    `HH` hour (00–23), `mm` minute, `ss` second; any other characters are
    literal separators;
  - examples with concrete output: `<date>` → `2026-07-09`,
    `<date:yyyy-MM-dd>` → `2026-07-09`, `<date:yyyyMMdd-HHmm>` →
    `20260709-1724`.
- Advertised: the form's footer hint line gains `[ctrl+d] formats`, and the
  Settings section of `help.go` mentions the sheet (memory convention:
  advertise in help AND footer).

## Surfaces touched

| File | Change |
|---|---|
| `internal/template/template.go` | bare/`<date:>` default layout `2006-01-02`; doc comment |
| `internal/tui/prefix_settings.go` | sync validation + `formErr` inline error; `ctrl+d` opens help sheet; footer hint |
| `internal/tui/popup_help.go` | new prefix-tokens/date-formats cheat sheet content |
| `internal/tui/help.go` | mention the sheet in the Settings/prefix rows |
| `README.md` | correct `<date:YYYY-MM-DD>` → `<date:yyyy-MM-dd>`, mention bare `<date>` default |
| `internal/config/template.go` | `path_template` settingDoc comment mentions bare `<date>` |
| `internal/agentskill/using-gg.md` + `agentskill.Version` | bare `<date>` accepted by `gg prefix add` / templates |
| `CHANGELOG.md` | feature entry |

## Testing

- `internal/template`: bare `<date>` resolves to `Now` formatted
  `2006-01-02`; `<date:>` likewise; explicit formats unchanged; `Now == nil`
  still errors for the bare form.
- `internal/domain`: `ValidatePrefixValue("<date>")` now passes;
  known-invalid values still fail.
- `internal/tui` (prefix_settings tests): Enter with an invalid value keeps
  `mode == pfForm`, preserves the typed value, and sets the inline error
  line in the rendered box; Enter with a valid value closes the form and
  dispatches; empty value shows the inline "required" error instead of a
  statusMsg; `ctrl+d` in form mode pushes the help sheet and `esc` returns
  to the form with the value intact; the sheet renders the expected rows.

## Out of scope

- Reopening the form for async store I/O failures (rare; bottom bar stays).
- A help sheet trigger from browse mode or other template-editing surfaces
  (worktree popup, hook editor) — this sheet is scoped to the add-prefix
  form per the request.
- Any change to `<date:…>` parsing beyond the empty-format default.
