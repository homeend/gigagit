# i18n stage 2 — sweep design (2026-07-15)

Stage 2 of TUI multilanguage support. Stage 1 (merged `9b18b1c`) shipped the
architecture: `internal/i18n` English-text-as-key bundles (ja/ko/zh/ru +
custom overlay files), `[ui] language` + Settings picker, and the
`internal/tui/i18n_scan_test.go` enforcement gate. This stage closes the
deferred list from stage 1's final whole-branch review. Architecture is
unchanged; this spec records only the new decisions.

**Ground rules carried over (unchanged):** decision option VALUES and CLI/
engine prose stay English (protocol); every new `i18n.T` key lands in ALL
FOUR bundles in the same change (scan test enforces); `T` keys are string
literals only; registries that hold translated labels must be funcs, not
package vars; display math on translated strings uses `lipgloss.Width`.

## Scope

The nine deferred items, grouped by kind of work:

| # | Item | Kind |
|---|------|------|
| 1 | `remote_actions.go` `copyShaRow` label/notice | extraction |
| 2 | `view.go` conflict/paused/marked status segments → full-sentence keys | restructuring |
| 3 | `CheckVerbs` hardening (`*` width, explicit `%[n]` index) | i18n package |
| 4 | Reword "page the selection (25% of the viewport)" source string | source + bundles |
| 5 | English dynamic args: `pushTagsNoun`, pick_commit "the current branch", refresh source names, `m.conflict.Op` | restructuring |
| 6 | `footerOverride()` mode footers; settings sub-screens (op-log toggle msgs, commit-graph enter msgs, errors/rates/tools wizard, agent-picker) | extraction |
| 7 | Split the dual-context `"language: %s"` key | key split |
| 8 | Language picker repo-override hint; EACCES on custom bundle reads | polish/hardening |
| 9 | Bundle-quality review pass, all four languages | translation QA |

## Design decisions

### D1. Plurals: two-key convention, no plural engine

For strings that embed a count, code selects between two full-sentence
English keys — a singular form and a `%d` plural form (e.g. `"⚠ 1 conflict"`
vs `"⚠ %d conflicts"`). Translators handle each key naturally; a language
whose plural rules don't fit two forms (Russian has three) may use a
count-neutral phrasing for the `%d` key (e.g. `"⚠ конфликтов: %d"`). A CLDR
plural engine for a handful of count strings is rejected as YAGNI.

### D2. Full-sentence keys with variants for optional parts (item 2)

The `view.go` status segments currently concatenate fragments
(`"⚠ %d conflict"` + `"s"` + `" " + src` + `T(" — press [x] to resolve")`),
freezing English word order and breaking pluralization. Restructure into
complete-sentence keys, with separate key variants when an optional part
(the conflict source) is present or absent, so every translation controls
its own word order:

- `"⚠ 1 conflict — press [x] to resolve"` / `"⚠ %d conflicts — …"`
- with-source variants carrying the source as an argument
- paused: `"⏸ %s paused — press [x] to continue or abort"` (+ with-source
  variant), the op name passed through `opDisplayName` (D3)
- the adjacent untranslated `"◆ marked: "` hint in the same render site
  becomes `T("◆ marked: %s", …)`

Exact key texts are fixed in the plan; the principle is: no fragment
concatenation, one key per sentence shape.

### D3. English dynamic args: translate-at-render display funcs (item 5)

Values that live in state or config keep their English identity (same rule
as decision options); only display translates, via literal-key switch funcs
following the stage-1 `settingsMenuTitle()` pattern (scan-test compatible):

- `opDisplayName(op)` for `m.conflict.Op` values (merge / rebase /
  cherry-pick / revert)
- `sourceDisplayName(key)` for refresh source names (status, branches, …)
  wherever they render (Refresh rates editor, `⟳` hint, notices)
- pick_commit's `"the current branch"` fallback becomes a plain `T()`
  literal

`pushTagsNoun` ("tag x" / "tags x, y") dissolves into the two-key convention
(D1) at each call site: e.g. `"Branch tip has tag %s not on the remote.
Push too?"` / `"Branch tip has tags %s not on the remote. Push too?"`, the
joined names as one argument. All `pushTagsNoun` callers get the same
treatment; the helper keeps returning only the joined name list (or is
inlined away).

### D4. Key splits (item 7)

`language_popup.go` uses `"language: %s"` for both the switch-failure path
(arg = error text) and the success path (arg = language name) — one key,
two meanings, untranslatable independently. Split the failure path into its
own key (e.g. `"language failed: %s"`); the success and not-saved keys stay
as-is. Exact wording fixed in the plan.

### D5. `CheckVerbs` hardening (item 3)

Two holes in `verbs()`:

- `*` (dynamic width/precision) is stripped, so `"%*d"` compares equal to
  `"%d"` despite consuming two args vs one. Fix: `*` counts as an
  arg-consuming element — each `*` becomes its own entry in the multiset,
  so a translation cannot add or drop one.
- An explicit argument index (`%[9]s`) is stripped, so a translation can
  reference an argument the key never supplies (runtime `%!s(MISSING)`).
  Fix: validate that every explicit index in the translation is within the
  key's argument count; reject otherwise.

Both stay per-key fail-soft at load (bad key skipped, English used), same as
stage 1.

### D6. Source-string reword (item 4)

`"page the selection (25% of the viewport)"` false-positives as a `%o` verb
(faithful to real Sprintf), which forced all four bundles to keep the
English parenthetical verbatim. Reword the source string to avoid `%`
(e.g. "page the selection (a quarter of the viewport)"), retranslate the
parenthetical in all four bundles, and drop the workaround.

### D7. Custom-bundle read errors and picker hint (item 8)

- `SetLanguage`'s custom-overlay read (`os.ReadFile`) currently ignores
  every error, so an unreadable file (EACCES) silently behaves as "not
  found". Fix: only `os.IsNotExist` is silently skipped; any other read
  error is returned from `SetLanguage` and surfaces through the existing
  fail-soft statusMsg path. `Available()` (listing) stays lenient — a
  listing should not fail because one file is unreadable.
- When the active repo config sets `[ui] language`, the Language picker
  shows a hint that the repo config overrides the global choice (the picker
  writes the GLOBAL config, so a repo-level key makes the selection appear
  to "not stick"). Mechanism (how the popup learns the repo-layer value) is
  a plan detail.

### D8. Bundle-quality review (item 9)

After all code tasks land, one reviewer subagent per language reviews its
full bundle for naturalness and consistency — known candidates: zh
full-width/half-width colon consistency, zh fetch/pull 拉取 disambiguation,
ru case agreement, ja/ko particle fit with inserted `%s` args. Reviewers
apply fixes directly to the bundle files; the scan test keeps changes
structurally safe (key set and verbs cannot drift). Output additionally
includes a short findings note per language in the ledger.

## Out of scope

- Engine/CLI prose, decision option values, e2e scenario text (English by
  design, unchanged from stage 1).
- A message-ID indirection layer for engine prose (possible later stage).
- Plural engine (D1).
- New languages.

## Error handling

Unchanged from stage 1: fail-soft everywhere — a bad custom bundle, a
verb-mismatched key, or a failed language switch degrades to English (or
the previous language) with a statusMsg notice; never a startup error.

## Testing

- The scan test remains the completeness gate for every new key.
- New display funcs (`opDisplayName`, `sourceDisplayName`) get unit tests
  (English passthrough + a translated case).
- Singular/plural key selection gets a table test per site.
- `CheckVerbs` hardening gets failing-first tests (`%*d` vs `%d`, `%[9]s`
  out of range, and in-range `%[2]s` reorder still accepted).
- `SetLanguage` EACCES vs not-found gets a unit test (permission-denied
  fixture skipped on platforms where chmod 000 is ineffective, e.g. running
  as root / Windows).
- `./test.sh race` before merge.
