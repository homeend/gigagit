# i18n stage 3 — popups, decision options, statusMsg tail (2026-07-15)

Stage 3 of TUI multilanguage support. Stage 1 (`9b18b1c`) shipped the
architecture; stage 2 (`23d01fb`) closed its deferred sweep and added the
`internal/tui/i18n_display.go` translate-at-render seam, the two-key plural
convention, and `padCell`. Stage 3 finishes the remaining user-visible
English in the TUI: the six untranslated popups, decision-modal option
labels, and the ~149-site statusMsg tail. After this stage the TUI is fully
translated except engine-generated prose (op summaries/decision prompts
built inside `internal/engine`), which stays English by design.

**Ground rules carried over (unchanged from stages 1–2):** English text IS
the key; literal `T()` keys only; every new key in ALL FOUR bundles in the
same change, orphans deleted from all four; protocol strings (decision
option VALUES, config values, op/source identity) never translated —
display-only lookups at render; registries holding translated labels are
funcs; display math on translated strings uses `lipgloss.Width`
(`padCell`), never `len()`/`%-Ns`; full-sentence keys, no fragment
concatenation (footer hint strips remain the one sanctioned exception);
fail-soft everywhere.

## Scope

| # | Item | Kind |
|---|------|------|
| 1 | Decision-modal option labels + modal footer (`renderModal`) | render seam + new scan test |
| 2 | Six popups: identity, git-config explorer, notice, repoconfig, review view, hook editor | extraction (+`padCell` in identity) |
| 3 | statusMsg tail (~149 sites, `model.go` = 56) | extraction, 3 waves |
| 4 | Carried polish: `FileUILanguage`→`decodeFile`, picker-hint + language-failed tests, `toolConfiguredSuffixDecorator` rune/column fix | cleanup/tests |
| 5 | Per-language QA delta pass over the new keys | translation QA |

## Design decisions

### D1. Option labels translate at the single render site

All decision modals — engine ops' `DecisionNeeded` forks and TUI-authored
`decisionState` confirms alike — render their options through one loop in
`renderModal` (`internal/tui/view.go`, ~line 1551). A new
`optionDisplayName(value string) string` in `i18n_display.go` (the
`opDisplayName` pattern: literal-key switch, passthrough fallback) is
applied there and ONLY there: `wrapWords(optionDisplayName(opt), optW)`.
Option VALUES everywhere else — `Options:` lists, `onResolve`/decider
comparisons, the esc→`"abort"` mapping, CLI flag policies — stay English
protocol, untouched. The modal's hardcoded footer
`"[↑/↓] choose  [enter] confirm  [esc] abort"` becomes a normal key.

The vocabulary is ~45 distinct values (both styles: Title-case TUI confirms
like `"Push branch + tags"`, lower-case engine options like
`"force-with-lease"`). Values that already exist as bundle keys ("merge",
"rebase", …) share their entries. Hyphenated command-ish values
(`"force-with-lease"`, `"checkout-and-resolve"`, `"keep-conflicts"`)
translate as readable phrases — the displayed label no longer needs to be
typeable, since selection is by cursor.

### D2. Option-vocabulary scan test (the new gate)

A go/ast scan test (sibling of `i18n_scan_test.go`) collects every element
of each `Options: []string{…}` composite literal across `internal/engine`
and `internal/tui` — string literals directly, and identifiers resolved to
package-level/same-file string constants (e.g. `applyOptWorkingTree`) —
and asserts each collected value exists as a key in all four bundles.
Combined with the existing orphan check (every bundle key must be a live
`T()` literal), a newly added option value fails tests until it has a
display entry and four translations. Values the scan cannot resolve
statically (none known today) would be a test failure, not a silent skip.

### D3. Six popups — standard extraction

`identity_popup.go`, `gitconfig_popup.go`, `notice_popup.go`,
`repoconfig_popup.go`, `review_view.go`, `hook_editor.go` (~1,900 lines
total) get the stage-2 treatment: titles, field labels, hints, footers,
statusMsgs, placeholders → `T()` keys with translations ×4. Specifics:

- `identity_popup.go` uses `%-9s`/`%-10s` byte-width label padding — it
  switches to `padCell` (move `padCell` out of `settings_popup.go` into
  `i18n_display.go` now that it has a second caller).
- Identity/profile-scope words rendered from state (e.g. `local`/`global`
  from `model.ProfileScope`) follow the display-func rule if encountered —
  values stay protocol.
- `review_view.go`'s report CONTENT is agent output — never translated;
  only the viewer chrome (title prefix, search/footer hints).
- The notice popup's notice TEXTS are TUI-authored (notify.go) — in scope
  where they are plain literals; notice IDs stay protocol.

### D4. statusMsg tail — three waves

All remaining raw `m.statusMsg = "…"` literals (~149 sites; the grep
construct is `statusMsg = "` plus `statusMsg += "`), split:

- Wave 1: `model.go` (56 sites) — its own task.
- Waves 2–3: the remaining ~93 sites file-grouped into two tasks of
  roughly equal size.

Rules per site: error text stays English, passed as an arg
(`i18n.T("bookmark paste: %s", err.Error())`); concatenated dynamic middles
restructure into full-sentence keys with args (never key concatenation);
counts follow the two-key plural convention where English inflects;
protocol values interpolate as args. Sites already translated, or writing
protocol/log-only strings, are excluded. Each wave's implementer greps the
construct, not a helper (the stage-1 lesson).

### D5. Carried polish

- `config.FileUILanguage` delegates to the existing `decodeFile` (3-line
  cleanup; behavior identical — any error still yields "").
- New TUI tests: the language picker renders the repo-override hint when
  `repoConfigPath` sets `[ui] language`, and the `"language failed: %s"`
  path fires on a `SetLanguage` error.
- `toolConfiguredSuffixDecorator` (`settings_tools.go`): fix the
  rune-index vs display-column mix against `winOpts.hscroll` so the dimmed
  "(configured)" suffix range stays correct on horizontally scrolled rows
  with wide-glyph translations; regression test with a CJK label +
  nonzero hscroll.

### D6. QA delta pass

One reviewer subagent per language, scoped to the keys ADDED on this
branch (diff-derived list) rather than the whole bundle — stage 2 already
QA'd the existing 517. Same hard rules (values only; verbs/tokens/glyphs
frozen); same consistency checklists per language.

## Out of scope

- Engine prose (op summaries, decision prompt text built in
  `internal/engine`) and CLI output — English by design; a message-ID
  stage remains a separate future decision.
- e2e scenario text, errors.log content (English on purpose).
- New languages; plural engine (two-key convention stands).

## Error handling

Unchanged: fail-soft, English fallback, statusMsg notices. The new scan
test fails loudly at CI time, never at runtime.

## Testing

- New option-vocabulary AST scan test (D2) alongside the existing scan
  gate; existing orphan/coverage/verb checks keep enforcing bundles.
- `optionDisplayName` unit test (English passthrough for the full
  collected vocabulary).
- `padCell` relocation keeps its tests; identity popup gets a CJK
  alignment test.
- D5's three test additions.
- Per-task `go test ./internal/i18n/ ./internal/tui/ -count=1`;
  `./test.sh race` before merge.
