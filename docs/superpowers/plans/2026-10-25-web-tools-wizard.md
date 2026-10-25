# gg web tools wizard + repo settings location — Implementation Plan

> Executed inline (superpowers:executing-plans), TDD per task.

**Goal:** port the TUI's External tools wizard and Repo settings location popup to gg web.
**Spec:** docs/superpowers/specs/2026-10-25-web-tools-wizard-design.md

## Global constraints
- internal/web is domain-only (archtest); new wire values are allowlisted; the wire never names a path or a command for a write.
- Every TUI string ported verbatim (English).
- Tests: `t.Parallel()` unless touching process-global state; the web TestMain pins XDG_CONFIG_HOME.
- `go test ./internal/web` after every static edit.

## Review focus
1. A row key unknown at POST time (tool uninstalled, template renamed) → 400, nothing written.
2. A configured row in the POST → skipped, never a duplicate block.
3. Overwrite of a non-empty destination never happens without `overwrite: true`.
4. A move whose remove fails reports "copied but source not removed".
5. Hosted page: the terminal's write target follows a page relocation.

## Task 1: domain rules
- `internal/domain/repoconfig.go`: `type RepoConfigAction string` (`copy-to-private`, `move-to-private`, `copy-to-committed`, `move-to-committed`), `RepoConfigActions(committedExists, privateExists, haveCommitted, havePrivate bool) []RepoConfigAction`, `RepoConfigEndpoints(act, committed, private) (src, dst string, isMove bool)`, `(*Service).RepoConfigPaths(ctx) (committed, private string, err error)`, `RepoConfigActionLabel(act) string` (English, the web's).
- `internal/domain/tooltemplates.go`: `DefaultToolChecked(t exttool.CommandTemplate, existing bool) bool`.
- TUI `repoconfig_popup.go` / `settings_tools.go` call through (repoCfgAction = domain.RepoConfigAction; labels stay i18n).
- Tests: `internal/domain/repoconfig_test.go` (actions per existence, endpoints), DefaultToolChecked cases; TUI tests stay green.

## Task 2: web wizard endpoints
- `exttools.go`: GET adds `wizard` rows; `POST /api/exttools/add` (writeGuard). After a write: `forgetToolStatuses()`, `s.notifyConfigChanged(false)`.
- `server.go`: `configChanged func(relocated bool)` + `SetConfigChanged`; `host.go` `Host.SetConfigChanged`.
- Tests (`exttools_wizard_test.go`): defaults (yolo + conflict_complete unchecked), configured flagged, add writes N blocks to the global file, second add writes 0, unknown row 400, configChanged called with false.

## Task 3: web location endpoints
- `settings.go`: GET adds `repo_location`; `POST /api/settings/repo-location` (writeGuard); 400 bad/inapplicable action, 409 confirm, msg texts; after: resetFeed, applyUIPolicies, forgetToolStatuses, notifyConfigChanged(true).
- Tests (`repolocation_test.go`): copy to private, 409 then overwrite, move removes the source, inapplicable 400, configChanged(true).

## Task 4: TUI hosted follow
- `webhost.go`: `SetConfigChanged` in the WebHost interface; `webHostState.configs` chan; `configChangedFor(w)`, `waitWebConfigCmd`, `onWebConfigChanged` (tools → reloadToolConfig; relocated → rebind repoConfigPath + full reload / workspace reloadOwnConfig); armed beside the other waits.
- Tests: `webconfig_test.go` — relocated msg rebinds repoConfigPath to the private file after a move; tools msg reloads cfg.Tools.

## Task 5: static
- `exttools.js`: pure `wizardRowText(r)`, `wizardHint()`, `addRequest(rows, checked)`; the wizard section, keys (↑/↓ space u enter esc), the write button, op line + close.
- `repoloc.js` + `#repoloc` in index.html + css: rows, actions, hint, confirm (y/enter, n/esc), op line, settings re-read + refreshAfterOp(true).
- `settings.js`: "external tools" row (opens the wizard), "repo settings location" row (opens repoloc), footer/notes without "from the TUI"; review.js/help text.
- Tests: node pure functions + wiring pins (`exttools_wizard_js_test.go`).

## Task 6: probe + docs
- Probe (fx91/serve91/run91/probe91, both browsers): wizard defaults, toggle, write → config gains N blocks + op line, reopen configured, second write nothing; location copy → private present, overwrite confirm cancel/ok, move → committed absent.
- README, CHANGELOG, parity doc rows, CLAUDE-details.
