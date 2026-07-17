# TUI Multilanguage Stage 4 — Terminal Chrome Sweep (Design)

Date: 2026-07-16 · Branch: `feat/i18n-stage4` · Base: main `574ffb5`

## Goal

Finish TUI translation entirely: after this stage, 100% of user-visible TUI
text routes through `i18n.T`. Also land three carried code fixes (notice
staleness after a language switch, the untranslated "working changes"
status-bar argument, pad-width overflow on translated labels) and one new
enforcement gate for action-menu labels.

Out of scope, unchanged by design: engine/CLI prose (English — the
agent-facing surface), decision-option VALUES and every other protocol
string (config keys/values, op/source names, notice ids), and the possible
future engine message-ID stage.

## Background

Stages 1–3 built the mechanism (English-text-as-key `i18n.T`, four embedded
bundles ja/ko/zh/ru, the `i18n_scan_test.go` gate: literal keys, all-four-
bundle coverage, orphan-freedom, verb match) and translated panels, modals,
popups, settings, and the ~110-site statusMsg tail. A fresh survey of main
(574ffb5) confirmed the remaining gaps and found two categories the stage-3
backlog missed: ~18 more textfield popups and ~85 unwrapped `.`-menu action
row labels. Stage 4 covers all of it.

## Translation waves

Mechanism identical to stage 3: wrap the literal in `i18n.T`, add the key to
all four bundles **in the same commit**, insert in place (bundles are NOT
sorted — never re-sort), delete orphans from all four when a key is replaced.
Literals that reach the UI through helper returns/params wrap at the
DEFINITION site. Strings built by concatenation are restructured into format
keys so translators can reorder via indexed verbs (`%[2]s`), which
`CheckVerbs` already range-checks.

### Wave A — pair-op picker (~5 keys)

- `internal/tui/mark.go:38,45,52,59` — the four `pairOp.label` closures are
  raw concatenation. Restructure to format keys:
  `i18n.T("Merge %s into %s", marked, selected)`,
  `i18n.T("Rebase %s onto %s", …)`,
  `i18n.T("Interactive rebase %s onto %s", …)`,
  `i18n.T("Compare %s ↔ %s", …)`.
- `internal/tui/pairop_popup.go:115` — footer
  `"[↑/↓] choose  [enter] run  [z] mode  [esc] cancel"`.

### Wave B — backlog chrome (~75–90 keys)

Titles, headers, empty states, and footer hint strips in:

- `file_finder.go` — title `"Find file  (loading…)"` (214), count title
  (216), `"   (press / to filter)"` (224), `"  (loading…)"` (229),
  `"  (no match)"` (231), 6-entry footer (256),
  `"(file too large to preview)"` (412).
- `files_view.go` — `"Files "` title prefix ×3 (66,81,100), `"shelf #"`
  (103), `"(no files)"` (172), `"(loading…)"` sites, footers (810,812),
  `"  (no match)"` (802).
- `bookmark_popup.go` — headers (110,112), `"(none)"` (123), 14-item footer
  (150), paste-popup title/from-line/footer (544,545,547).
- `shelf_popup.go` — headers (90,92), `"(none)"` (103), 14-item footer (131).
- `command_palette.go` — title `"Commands"` (104), footer (122).
- `prefix_picker.go` — `"Fill "` title (176), fill footer (178), header
  (182), empty state (191), footer (214).
- `prefix_settings.go` — `"Add branch prefix"` (209), scope labels
  (200,202), tokens legend (218), footers (178,220,253), title (225),
  `"(none yet — [n] to add)"` (231), `"(loading…)"` (227).
- `tool_approval.go` — shared approval-box body + footer (49–51).
- `shell_escape.go` — title (253), footer (255).
- `checkout_as_popup.go` — restructure `"Check out " + ref + " as"` →
  `i18n.T("Check out %s as", ref)` (54), verb words (48,50), footer
  `"[enter] " + verb + …` → format key (56).
- `commit_eager.go` (`eagerPrompt`) — report sentences (188,190), title
  `"Search deeper?"` (193), footer (206).
- `conflict_process.go` — title `"Resolve conflicts"` (593), `"(all
  resolved)"` (599,633), `conflictHints()` fragments incl. six action
  labels (628–658), `"Working…  [esc] cancel"` (481), `"Resolve failed:"` +
  `"[any key] back to the list"` (483), `"Tool inputs"` title+footer
  (488,492), `"Run this command?  (%s)"` (495), tool-changed prompt (498),
  `"Run external tool"` title (667), `"(this file)"` (676), footer (679).
- `review.go:420–427` — chooser header (dedupes with conflict_process/
  commit_generate `"Run this command?  (%s)"` into ONE key), title
  `"Choose a review tool"`, footer.
- `conflict_picker.go` — two titles (58,89), side labels
  `"current"/"incoming"/"index"/"working"` (59–60,90–91), badges
  `"line-by-line"`/`"· undecided"` (238,240), header words (279,281),
  11-entry footer (289–293), `"result:"` (337).
- `repo_popup.go` — title (193), `"(press / to filter)"` (200),
  `"(no match)"` (206), 6-entry footer (232), relative-age labels
  `"just now"`/`"%dm ago"`/`"%dh ago"`/`"%dd ago"` (252–258) — counts use
  the established two-key singular/plural convention only where a language
  needs it; these are unit-suffixed numerals, one key each is acceptable.

Footer hint strips remain the sanctioned fragment-concatenation exception
(list-like, not sentences; conditional tokens are their own keys, leading
spaces load-bearing).

### Wave C — newly-found textfield-popup family (~30–40 keys)

Same title+footer pattern, in: `annotate_tag_popup.go`, `apply_patch.go`,
`export_patch.go`, `reflog_checkout_popup.go`, `rename_branch_popup.go`,
`shelf_actions.go`, `tag_checkout_popup.go`, `temp_export.go`, plus titles
in `commit_filter_popup.go`, `commit_generate.go` (tool chooser),
`goto_commit_popup.go`, `irebase_view.go`, `related_prompt_popup.go`,
`repo_path_popup.go`, `reword_popup.go`, `stash_action.go`,
`stash_popup.go`, `tag_popup.go`.

### Wave D — action-menu row labels (~85 keys)

~85 unwrapped `label:` literals across ~20 files (largest:
`commit_scope.go` 22, `tags_actions.go` 9, `remote_actions.go` 8,
`file_finder.go` 8, `bookmark.go` 7, `file_preview.go` 5, `review.go` 4;
the rest across `shelf.go`, `reflog_view.go`, `ignore_actions.go`,
`export_patch.go`, `edit_actions.go`, `commit_message_view.go`,
`branch_actions.go`, `branch_push.go`, `branch_pull.go`,
`force_push_actions.go`, `open_external.go`, `rename_branch_popup.go`,
`reword_popup.go`, `shelf_popup.go`).

Wrap at construction: action menus are built fresh on every `.` press, so
build-time translation is correct — the registries-are-funcs rule applies
only to long-lived cached state, which menu rows are not. Dynamic labels
(`"Review branch " + name`) become format keys.

## Code fixes

### F1 — notice staleness on language switch (0 keys)

Notices bake resolved `i18n.T` strings into `notice.title`/`detail`/action
labels at build time (`notify.go:139–213`), stored in `m.notices` by
`applyRepoHealth`; the Language picker's Enter (`language_popup.go:55–69`)
never rebuilds them, so a language switch leaves open notices stale until
the next repo-health read. Fix: after `SetLanguage` succeeds in the picker's
Enter handler, rebuild `m.notices` by re-running `applyRepoHealth` against
the cached health snapshot (and cached clipboard availability). Dismissal
state is keyed by stable notice ids stored outside the notice structs, so a
rebuild preserves it. If no health snapshot is cached yet, do nothing (the
first health read will build notices in the new language anyway).

### F2 — untranslated "working changes" status-bar argument (0 keys)

`domain.ReviewTarget.DisplayLabel()` falls back to the raw literal
`"working changes"` (domain must not import i18n); `reviewScopeLabel`
(`internal/tui/review.go:436–438`) passes it verbatim into the translated
`"⟳ reviewing %s…"` segment. Fix: `reviewScopeLabel` adopts the same
explicit empty-label check `reviewTitle` (review.go:323–328) already uses,
substituting `i18n.T("working changes")` — the key already exists in all
four bundles.

### F3 — pad-width overflow on translated labels (0 keys)

`padCell`/`padRight` pad but never truncate; a translation longer than the
hard-coded width misaligns the row. Confirmed overflows: ru
"закоммиченный" (13 runes) vs `padCell(…, 11)` in `repoconfig_popup.go:
224–225` (unconditional); ru "Глобальные"/"Действующие" vs
`padCell(label, 9)` in `identity_popup.go:349`; `gitconfig_popup.go:508`
headers under the narrow-terminal floor (`gitConfigSideFloor` = 8). Fix
shape: at each site compute the column width as
`max(lipgloss.Width(t) for t in the fixed label set actually rendered)`
(floored at the current constant so English layout is unchanged), instead
of the hard constant. `identity_popup.go:421`'s 10-cell field labels get
the same treatment. `settings_popup.go:769`'s `%-11s` is a confirmed false
positive (intentionally-untranslated config keys) — leave it.

## New enforcement gate — `menu_labels_test.go`

An AST scan over `internal/tui` in the `options_vocab_test.go` style:
every composite literal of the action-row type whose `label:` field is a
static string expression must produce that label via `i18n.T(...)` (a
direct call, or a same-function helper that itself is enforced). Raw string
literals and raw concatenations fail with file:line. Genuinely dynamic
labels (built from data, no literal prose) are logged and pass. The gate
starts green after Wave D and keeps future menus honest — this stage's
biggest gap existed precisely because no gate watched menu labels.

## QA and docs

- Per-language delta pass (ja/ko/zh/ru) near the end: audit the ~200 new
  keys per language; specific new risk — format-key ARGUMENT ORDER in the
  restructured concatenations ("Merge %s into %s"): a language that
  reorders must use indexed verbs (`%[2]s`/`%[1]s`), which `CheckVerbs`
  range-checks at load. Also re-run the pad-width check against final
  translations (F3's computed widths make this a non-event, but verify).
- No new error-prefix keys are expected; if any wave adds a
  `"<topic>: %s"` error key, the ERROR-KEY TOPIC-HEAD RULE applies
  (translations keep a non-empty topic head before the first `%`).
- Docs: `CHANGELOG.md` (always); `CLAUDE.md` — the i18n paragraph drops its
  "fully translated except…" exceptions (only engine/CLI prose remains
  English by design); `README.md` only if its language-support blurb
  changes. No CLI surface change → no agentskill bump.

## Testing

- Existing gates do the heavy lifting: `i18n_scan_test.go` (literal keys,
  ×4 coverage, orphans, verbs) covers every new key automatically;
  `options_vocab_test.go` unaffected.
- New: `menu_labels_test.go` (above), red-proven against a known unwrapped
  label before Wave D lands, green after.
- F1: a unit test switching language with a built notice present asserts
  the rebuilt notice title matches the new catalog.
- F3: table test asserting the computed column widths fit every bundle's
  translation of the label sets (the stage-3 CJK regression-test pattern).
- Full `./test.sh race` before the merge decision.

## Delivery

Subagent-driven execution on `feat/i18n-stage4` in
`.claude/worktrees/i18n-stage4`; sync-merge main early and again before the
merge decision; the human decides the merge.
