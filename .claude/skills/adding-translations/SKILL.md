---
name: adding-translations
description: Use when adding or changing user-visible text in gigagit's TUI — labels, titles, status messages, popup/menu rows, decision options, engine op summaries/progress/prompts — or when editing or adding a language bundle. Any string a user sees must render in all supported languages (ja/ko/zh/ru).
---

# Adding translated text

gg's TUI is fully translated. `internal/i18n` is an English-text-as-key
lookup: `i18n.T("English text", args...)` returns the active bundle's
translation, or the key itself (= English) on a miss. The four embedded
bundles are `internal/i18n/lang/{ja,ko,zh,ru}.toml`; users can overlay or
add languages via `$XDG_CONFIG_HOME/gg/lang/<code>.toml`.

**The iron rule:** any new user-visible string = an English literal key at
the call site + a translation in ALL FOUR bundles, in the same change.
AST-scan gate tests fail otherwise — by design.

**Never translated (protocol / agent surface):** engine & CLI English prose
(`Result.Summary`, prompts, errors — what agents and scripts parse),
decision option VALUES (`"run"`, `"abort"`, …), config keys as identity
(e.g. rates-editor row names ARE `[refresh]` keys), `settingsMenu` consts
(display goes through `settingsMenuTitle()`).

## Which lane is your text in?

| Text | Lane |
|---|---|
| TUI chrome: labels, titles, status messages, footer hints, popup text | A — wrap in `i18n.T` |
| Engine op summary / progress detail / decision prompt | B — msg.go helpers |
| Decision-modal option label | C — optionDisplayName |
| Action-menu row label (`actionRow{label: …}` or a `label string` param) | D — i18n.T at the label site |

## Lane A — TUI chrome (`i18n.T`)

1. Wrap the English literal: `i18n.T("Create branch")`,
   `i18n.T("pulled %s", name)`. The key MUST be a string literal
   (`TestI18nKeysAreLiterals`); dynamic data goes in args, never
   concatenated into the key.
2. Add the key to all four bundles under `[strings]`:
   `"pulled %s" = "…"`. Bundles are NOT key-sorted — insert in place near
   related keys; never re-sort the file.
3. Verbs: the translation's format verbs must match the key's
   (`CheckVerbs`; a mismatch makes that one key silently fall back to
   English). Translations reorder args ONLY via explicit indexes
   `%[1]s`/`%[2]s` — swapping two bare `%s` is the one silent bug
   CheckVerbs cannot catch; sweep multi-`%s` keys for it in review.
4. **Prose lives in the format, never in an arg.** Passing a phrase like
   `"in this repo"` as a `%s` arg renders English inside every
   translation — split into per-variant literal formats instead (six
   literal keys beat three keys + a prose arg).
5. Plurals: two keys (singular/plural) picked by count at the call site,
   never one string with the count glued in. A translation may collapse
   them to a count-neutral form (`ru` does).
6. A package-level `var` registry of labels freezes `T` at init — label
   registries must be funcs (see `contextBindings()` in the footer) or
   translate at render time.
7. Width math on translated strings: `lipgloss.Width`, never `len()`.
   Pad columns with `padCell`, size label columns with `maxLabelWidth`
   (both in `internal/tui/i18n_display.go` — a fixed English-length pad
   clips CJK).
8. Footer hint strips are the ONE sanctioned fragment-concatenation
   exception (list-like tokens; `"  [i] msg"` is its own key, leading
   spaces load-bearing). Never generalize it to sentences — a sentence is
   always one key.

## Lane B — engine event prose (summaries / progress / prompts)

Engine English strings are the CLI/agent surface and must stay
byte-identical. Localization rides a second channel (`engine.Msg`) built
ONLY through the `internal/engine/msg.go` helpers — never hand-build a
`Summary:`/`Prompt:` field or assign `.Summary`
(`TestEngineProseHelperOnly`):

- `Result{…}.WithSummary("created branch %s", name)`
- `.AppendSummary("; pushed to %s", remote)` — glue (`"; "`, `" ("`) lives
  inside the suffix format
- `Progressf(step, "…", args)` when the Detail mixes English words into
  data; pure-data Details stay plain `Progress{}`. `Progress.Step` values
  come from the stable step vocabulary — each step is its own key
- `PromptReq(id, "…", options, args)` for decisions

Formats must be English literals at the helper call site
(`TestEngineProseNoDynamic`), and every format/step literal must exist in
all four bundles (`TestEngineProseKeysInBundles`). The TUI renders the
channel in `internal/tui/i18n_engine.go` — the ONE allowlisted
non-literal-key `i18n.T` site. Engine ERROR prose stays English (it renders
inside the already-translated `friendlyOpError` frame).

## Lane C — decision options

`Options: []string{…}` values are protocol — English forever; the modal
translates labels at render via `optionDisplayName`
(`internal/tui/i18n_display.go`). A new statically declared option value
needs BOTH an `optionDisplayName` case and entries in all four bundles
(`TestDecisionOptionValuesTranslated`). A new helper that forwards an
options list must be taught to that gate — the `PromptReq` lesson: every
gate that scans `Options:` needs to learn the new construction site.

## Lane D — action-menu rows and label params

Every `actionRow{label: …}` value and every argument reaching a
same-package `label string` parameter must route through `i18n.T`
(`TestActionMenuLabelsTranslated`) — including prose reaching a row
positionally through a helper.

## The gates — run before claiming done

```bash
go test ./internal/i18n/ ./internal/tui/ -run \
  'I18n|EngineProse|DecisionOptionValues|ActionMenuLabels|CheckVerbs|FooterRendersTranslated'
```

| Gate | Enforces |
|---|---|
| `tui/i18n_scan_test.go` | literal `i18n.T` keys; every used key (TUI + engine formats) present in all 4 bundles; no orphan bundle keys; verbs match |
| `tui/engine_prose_test.go` | engine formats literal, helper-built only, in all 4 bundles, count ≥ floor |
| `tui/options_vocab_test.go` | every static option value has an `optionDisplayName` case + bundle entries |
| `tui/menu_labels_test.go` | menu labels route through `i18n.T` |
| `i18n/verbs_test.go` | `CheckVerbs` semantics (dynamic `*` widths, `%[n]` range) |

## Deleting / renaming a key

Delete or rename it in ALL FOUR bundles too — the orphan check fails on any
bundle key no call site uses. Restructuring work must plan orphan removal,
not just additions.

## Adding a language

- **Embedded:** drop `<code>.toml` (`[meta] name = "…"` + `[strings]`) into
  `internal/i18n/lang/` (go:embed picks it up) AND extend the hardcoded
  `{"ja","ko","zh","ru"}` lists in the gate tests above.
- **User-side (no rebuild):** a file at
  `$XDG_CONFIG_HOME/gg/lang/<code>.toml` adds a new language or overlays
  individual keys of an embedded one (`config.LangDir()`).
