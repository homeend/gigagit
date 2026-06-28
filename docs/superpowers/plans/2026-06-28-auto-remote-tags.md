# Auto remote-tag refresh — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Automatically run a silent background remote-tag lookup whenever the Tags panel's contents change (app load + after add/remove/push/delete-remote), on by default, with a Settings toggle + config key to disable.

**Architecture:** Reuse the `▲` feature's single-lane machinery. One new enqueue site in the `srcTags` arm of the `dataAvailableMsg` handler enqueues the existing synthetic `remoteTagsItem`; a new inverted-polarity config bool gates it; a Settings toggle + global writer expose it.

**Tech Stack:** Go 1.26, Bubble Tea TUI, `internal/{config,tui}`.

## Global Constraints

- `internal/tui` and `internal/cli` MUST NOT import `internal/git` (archtest-guarded).
- Default-ON bool stored INVERTED (`disable_remote_tags_auto`, default false = on), matching `[refresh] enabled` / `[ui] disable_slow_op_confirm`. Overlay uses inverted polarity exactly like `Enabled` in `overlayRefresh`.
- The auto-trigger is INDEPENDENT of `[refresh] enabled` (the master switch gates only the timed `dueItems` path; a directly-enqueued lane item drains regardless).
- The Settings toggle persists to the GLOBAL config (`config.DefaultGlobalPath()`), mirroring `toggleAutoRefresh` / `SetGlobalRefreshEnabled`.
- Adding the config field needs a `settingDoc` in `template.go` (guarded by `TestSettingDocsCoverAllFields`).
- TDD: failing test → see fail → implement → see pass → commit. Real git in TempDir or table tests; mirror existing patterns.

---

### Task 1: config field + overlay + settingDoc + global writer

**Files:**
- Modify: `internal/config/config.go` (`RefreshConfig.DisableRemoteTagsAuto`; `overlayRefresh`)
- Modify: `internal/config/template.go` (settingDoc)
- Modify: `internal/config/write.go` (`SetGlobalDisableRemoteTagsAuto`)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.RefreshConfig.DisableRemoteTagsAuto bool` (`toml:"disable_remote_tags_auto"`); default false (= auto on).
- Produces: `func SetGlobalDisableRemoteTagsAuto(path string, on bool) error`.

- [ ] **Step 1: Write failing tests**

In `internal/config/config_test.go` add (mirror the existing `Enabled` overlay test and a write test if one exists for `SetGlobalRefreshEnabled`):
```go
func TestOverlayRefreshDisableRemoteTagsAutoInverted(t *testing.T) {
	// default: false (auto on)
	if config.Defaults().Refresh.DisableRemoteTagsAuto {
		t.Fatal("default must be false (auto-refresh on)")
	}
	// a higher layer with true overlays onto a false base
	dst := config.RefreshConfig{}
	// use the same overlay entry point the package test uses for Enabled; if
	// overlayRefresh is unexported, test through config.Load with a temp repo
	// file containing [refresh] disable_remote_tags_auto = true, as the existing
	// Enabled overlay test does. Follow that test's exact mechanism.
	_ = dst
}
```
NOTE: `overlayRefresh` is unexported. Look at how the existing `Enabled` overlay/inversion is tested in `config_test.go` and follow the SAME mechanism (likely via `config.Load(globalPath, repoPath)` with temp TOML files). Write the test to:
1. assert `Defaults().Refresh.DisableRemoteTagsAuto == false`;
2. a global file with `[refresh]\ndisable_remote_tags_auto = true` → merged config has it true;
3. (inversion limitation) a repo file with the key absent does NOT reset a true global back to false.

Also add a writer round-trip test mirroring any existing `SetGlobalRefreshEnabled` test (grep `SetGlobalRefreshEnabled` in `*_test.go`): write true to a temp file, reload, assert the merged/parsed value is true; assert the file still parses and unrelated keys survive.

- [ ] **Step 2: Run, verify fail**

Run: `go test ./internal/config/ -run "DisableRemoteTagsAuto"`
Expected: FAIL — unknown field / undefined writer.

- [ ] **Step 3: Implement**

`config.go` — in `RefreshConfig`, after `RemoteTags`:
```go
	// DisableRemoteTagsAuto turns OFF the automatic remote-tag refresh that runs
	// when the tag list changes (app load + after tag add/remove/push). Inverted
	// polarity: default false = auto-refresh ON. Independent of Enabled.
	DisableRemoteTagsAuto bool `toml:"disable_remote_tags_auto"`
```
`overlayRefresh` — add alongside the `Enabled` inversion block:
```go
	if src.DisableRemoteTagsAuto {
		dst.DisableRemoteTagsAuto = true
	}
```
`template.go` — add to the `[refresh]` settingDocs (after the `remote_tags` row):
```go
	{"refresh", "disable_remote_tags_auto", false, "disable auto remote-tag refresh on tag-list changes (default: on)"},
```
`write.go` — add (mirror `SetGlobalRefreshEnabled`):
```go
// SetGlobalDisableRemoteTagsAuto persists `[refresh] disable_remote_tags_auto`
// to the global config file (preserving comments), backing the Settings
// "Auto remote-tag refresh" toggle.
func SetGlobalDisableRemoteTagsAuto(path string, disabled bool) error {
	return setScalarLine(path, "refresh", "disable_remote_tags_auto", strconv.FormatBool(disabled))
}
```

- [ ] **Step 4: Run, verify pass; whole package**

Run: `go test ./internal/config/`
Expected: PASS (incl. `TestSettingDocsCoverAllFields`).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/template.go internal/config/write.go internal/config/config_test.go
git commit -m "feat(config): [refresh] disable_remote_tags_auto (default off = auto on) + global writer"
```

---

### Task 2: TUI auto-trigger on tag-window change

**Files:**
- Modify: `internal/tui/model.go` (the `srcTags` arm of the `dataAvailableMsg` handler ~line 627; add `autoRemoteTagsEnabled` helper — put it in `internal/tui/remote_tags.go`)
- Modify: `internal/tui/remote_tags.go` (helper)
- Test: `internal/tui/remote_tags_auto_test.go`

**Interfaces:**
- Consumes: existing `remoteTagsItem` (refresh.go), `enqueueDue` (refresh.go), `m.cfg.Refresh.DisableRemoteTagsAuto` (Task 1).
- Produces: `func (m Model) autoRemoteTagsEnabled() bool` = `!m.cfg.Refresh.DisableRemoteTagsAuto`.

- [ ] **Step 1: Write failing tests**

`internal/tui/remote_tags_auto_test.go`. The trigger lives in the `srcTags`
arrival handler, so drive it by sending a `dataAvailableMsg{source: srcTags}`.
Inspect how other tests construct a Model + send a `dataAvailableMsg` (grep
`dataAvailableMsg{` in `internal/tui/*_test.go` for the exact field set — note
`gen` must match `m.srcGen[srcTags]` or the handler early-returns as stale, and
the value must be `[]model.Tag`). Tests:
```go
// Enabled (default) + tags present → remoteTagsItem enqueued.
func TestSrcTagsEnqueuesRemoteTagsWhenAutoOn(t *testing.T) {
	// build a Model with maps initialized (use the package's test-model helper),
	// cfg default (DisableRemoteTagsAuto=false), send dataAvailableMsg{source:srcTags,
	// gen: m.srcGen[srcTags], value: []model.Tag{{Name:"v1"}}}, assert remoteTagsItem
	// is in the resulting m.bgQueue.
}
// Disabled → not enqueued.
func TestSrcTagsNoEnqueueWhenAutoOff(t *testing.T) {
	// same but set m.cfg.Refresh.DisableRemoteTagsAuto = true; assert remoteTagsItem
	// NOT in m.bgQueue.
}
// No tags → not enqueued (nothing to annotate).
func TestSrcTagsNoEnqueueWhenNoTags(t *testing.T) {
	// enabled, value: []model.Tag{}; assert not enqueued.
}
```
Add a small helper in the test to check membership of `remoteTagsItem` in a
`[]refreshItem`.

- [ ] **Step 2: Run, verify fail**

Run: `go test ./internal/tui/ -run TestSrcTags`
Expected: FAIL (not enqueued — trigger absent / helper undefined).

- [ ] **Step 3: Implement**

In `internal/tui/remote_tags.go`:
```go
// autoRemoteTagsEnabled reports whether a tag-window change should auto-trigger
// a background remote-tag lookup (default on; inverted config flag).
func (m Model) autoRemoteTagsEnabled() bool {
	return !m.cfg.Refresh.DisableRemoteTagsAuto
}
```
In `internal/tui/model.go`, the `srcTags` arm (after `m = m.restorePanelSel(panelTags, key)`):
```go
		// Auto remote-tag refresh: a tag-window update enqueues a silent background
		// ls-remote so ▲ markers track local changes (create/delete/push) without a
		// manual refresh. Routed through the single lane (deduped); independent of
		// the [refresh] master switch. Skipped when disabled or there are no tags.
		if m.autoRemoteTagsEnabled() && len(m.tags) > 0 {
			m.bgQueue = enqueueDue(m.bgQueue, m.bgActiveItem, m.bgBusy, []refreshItem{remoteTagsItem})
		}
```
(Do NOT return early — the existing handler flow continues. Enqueuing mutates
`m.bgQueue`; `refreshTick` drains it on the next heartbeat.)

- [ ] **Step 4: Run, verify pass; whole package**

Run: `go test ./internal/tui/ -run TestSrcTags && go test ./internal/tui/`
Expected: PASS (~20-30s for the full package).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/remote_tags.go internal/tui/remote_tags_auto_test.go
git commit -m "feat(tui): auto-enqueue remote-tag lookup when the tag list changes"
```

---

### Task 3: Settings toggle

**Files:**
- Modify: `internal/tui/settings_popup.go` (menu const + order + label + enter case + `toggleAutoRemoteTags`)
- Test: `internal/tui/settings_popup_test.go`

**Interfaces:**
- Consumes: `config.SetGlobalDisableRemoteTagsAuto` (Task 1), `m.cfg.Refresh.DisableRemoteTagsAuto`.

- [ ] **Step 1: Write failing test**

In `internal/tui/settings_popup_test.go` (mirror `TestToggleAutoRefreshFlipsInMemory`):
```go
func TestToggleAutoRemoteTagsFlipsInMemory(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // don't write the real user config
	m := newTestModel(t)
	// default: enabled (DisableRemoteTagsAuto false)
	if m.cfg.Refresh.DisableRemoteTagsAuto {
		t.Fatal("precondition: auto-refresh remote tags on by default")
	}
	m = m.toggleAutoRemoteTags()
	if !m.cfg.Refresh.DisableRemoteTagsAuto {
		t.Fatal("toggle should disable in-memory")
	}
	m = m.toggleAutoRemoteTags()
	if m.cfg.Refresh.DisableRemoteTagsAuto {
		t.Fatal("toggle should re-enable in-memory")
	}
}
```
Also extend the menu count expectations if any test asserts `len(settingsMenu)`
(grep; `TestSettingsMenuWrapsAround` uses `len(settingsMenu)` dynamically so it
is unaffected).

- [ ] **Step 2: Run, verify fail**

Run: `go test ./internal/tui/ -run TestToggleAutoRemoteTags`
Expected: FAIL — `m.toggleAutoRemoteTags` undefined.

- [ ] **Step 3: Implement**

In `internal/tui/settings_popup.go`:
- Add a menu const beside the others:
```go
	settingsMenuRemoteTags  = "Auto remote-tag refresh"
```
- Insert it into the `settingsMenu` slice immediately AFTER `settingsMenuAutoRefresh`:
```go
var settingsMenu = []string{settingsMenuAgents, settingsMenuIdentity, settingsMenuPrefixes, settingsMenuOpLog, settingsMenuErrors, settingsMenuAutoRefresh, settingsMenuRemoteTags, settingsMenuRates}
```
- In `settingsMenuLabel`, add a dynamic label (presented positively):
```go
	if settingsMenu[i] == settingsMenuRemoteTags {
		if m.cfg.Refresh.DisableRemoteTagsAuto {
			return settingsMenuRemoteTags + ": off"
		}
		return settingsMenuRemoteTags + ": on"
	}
```
- In the enter-key `switch settingsMenu[p.menuSel]`, add:
```go
		case settingsMenuRemoteTags:
			return m.toggleAutoRemoteTags(), nil // stays open so the flip is visible
```
- Add the toggle method (mirror `toggleAutoRefresh`):
```go
// toggleAutoRemoteTags flips the auto remote-tag refresh switch (inverted flag),
// persisting to the global config so it survives restarts (mirrors toggleAutoRefresh).
func (m Model) toggleAutoRemoteTags() Model {
	wantDisabled := !m.cfg.Refresh.DisableRemoteTagsAuto
	m.cfg.Refresh.DisableRemoteTagsAuto = wantDisabled
	if err := config.SetGlobalDisableRemoteTagsAuto(config.DefaultGlobalPath(), wantDisabled); err != nil {
		m.statusMsg = "auto remote-tag refresh toggled but not saved: " + err.Error()
		return m
	}
	if wantDisabled {
		m.statusMsg = "auto remote-tag refresh off"
	} else {
		m.statusMsg = "auto remote-tag refresh on"
	}
	return m
}
```

- [ ] **Step 4: Run, verify pass; whole package**

Run: `go test ./internal/tui/ -run "TestToggleAutoRemoteTags|TestSettingsMenu" && go test ./internal/tui/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/settings_popup.go internal/tui/settings_popup_test.go
git commit -m "feat(tui): Settings toggle for auto remote-tag refresh"
```

---

### Task 4: docs + memory

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`
- Create/append memory under `/home/homeend/.claude/projects/-mnt-t-others-gigagit/memory/`

- [ ] **Step 1: CHANGELOG** — entry: auto remote-tag refresh on tag-window changes (app load + after add/remove/push), on by default, Settings toggle + `[refresh] disable_remote_tags_auto`.

- [ ] **Step 2: README** — in the Tags / `[refresh]` docs, note that `▲` now auto-refreshes on tag-list changes by default, and how to disable (Settings → "Auto remote-tag refresh", or `[refresh] disable_remote_tags_auto = true`). Add the key to any `[refresh]` table.

- [ ] **Step 3: CLAUDE.md** — update the `config` row's `[refresh]` key list to include `disable_remote_tags_auto`; in the `tui` row, note the `srcTags`-arrival auto-enqueue of `remoteTagsItem` and the new Settings toggle.

- [ ] **Step 4: memory** — update the existing `tag-remote-indicator-feature.md` (or add a short `auto-remote-tags-feature.md`) noting: `srcTags` arrival auto-enqueues `remoteTagsItem` (default on; inverted `disable_remote_tags_auto`; independent of `[refresh] enabled`); Settings toggle writes global. Add/adjust the `MEMORY.md` index line. (Memory files are outside the repo — not committed.)

- [ ] **Step 5: Build + test**

Run: `go build ./cmd/gg && ./test.sh unit`
Expected: build ok, unit tests pass.

- [ ] **Step 6: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: auto remote-tag refresh on tag-window changes"
```

---

## Self-review notes

- **Coverage:** config + writer (T1), trigger (T2), Settings toggle (T3), docs (T4).
- **Independent of master switch:** the enqueue is direct (not via `dueItems`), so it drains even when `[refresh] enabled` is false. Verify `refreshTick` drains the queue without gating on `Enabled` (it does — only `dueItems` checks `Enabled`).
- **Inverted bool:** default false = on; overlay mirrors `Enabled`. Repo can disable over a default; cannot re-enable over a disabling layer (accepted, matches convention).
- **No new lane/message:** reuses `remoteTagsItem`, `enqueueDue`, `remoteTagsCmd`, `remoteTagsMsg` from the `▲` feature.
