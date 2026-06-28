# Refresh Config Editor (Phase C rework) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the adaptive interval engine; schedule background refresh on fixed user-configured intervals (floored at `min_seconds`, default 10) over the existing single-lane queue; keep read-duration measurements as stats only; turn "Refresh rates" into an inline editor that writes per-source intervals to the repo `.gg.toml`.

**Architecture:** A trivial `scheduledInterval(cfg, it)` replaces the adaptive `effectiveInterval`; everything below `dueItems` (the single-lane FIFO queue + lane lifecycle) is unchanged. The duration ring stays but only feeds the stats column. The Settings "Refresh rates" screen becomes an inline numeric editor (gg's `textfield`) that persists via a new `config.SetRefreshInterval` line-writer to `<repo-top>/.gg.toml`.

**Tech Stack:** Go 1.26, Bubble Tea (value-receiver `Model`), `internal/config` TOML overlay + line-edit writers, `internal/tui` settings popup + `textfield`.

## Global Constraints

- **Fixed scheduling only.** Effective interval per source = `max(min_seconds, configured)`; `configured == 0` ⇒ off. No adaptation, no cutoff, no auto-disable. Measurements never affect scheduling.
- **`min_seconds`** is config-file-only (not in the editor), default **10** applied at read time when 0/unset.
- **Editor writes the repo `.gg.toml`** (`<repo-top>/.gg.toml`), not the global config. The master `[refresh] enabled` toggle stays global (unchanged).
- **Single-lane queue + lane lifecycle are unchanged** (`bgBusy`/`bgActiveItem`/`bgQueue`, lane-clear before stale-gen, `startOp` clear, srcInflight drop guard, `bgFetchDoneMsg` rewrite). Manual `r` stays parallel.
- **Measurement plumbing kept** (startup-excluded `recordDuration`, foreground-fetch `opIsFetch`) — feeds only the stats column. `bgRefreshHint` keeps suppressing sub-1s averages.
- The package must compile and tests pass after **each** task (the removal order below guarantees this: TUI stops referencing the adaptive config fields before config deletes them).
- TUI never imports `internal/git`. TDD; real `git` in `t.TempDir()` or table tests.

---

## File structure

| File | Change |
|------|--------|
| `internal/tui/refresh.go` | `scheduledInterval` replaces `effectiveInterval`/`effectiveIntervalRaw`; `dueItems` loses the `durs` param; remove `intervalState`+states+`stateLabel`; `refreshRateRows` reworked; add `refreshTomlKey`/`setRefreshIntervalField`; drop `defaultMaxReadSeconds`/`defaultBackoffFactor` |
| `internal/tui/settings_popup.go` | remove "Adaptive intervals" entry + `toggleAdaptive`; convert the `ratesView` read-only screen into an inline editor |
| `internal/tui/model.go` | `Model.repoConfigPath`; set it in the `configReadyMsg` handler; `configReadyMsg` carries the repo `.gg.toml` path |
| `internal/tui/source.go` | `bootstrapCmd` computes + carries the repo `.gg.toml` path on `configReadyMsg` |
| `internal/config/config.go` | remove `DisableAdaptive`/`MaxReadSeconds`/`BackoffFactor` fields + overlays; keep `MinSeconds` |
| `internal/config/write.go` | remove `SetGlobalRefreshDisableAdaptive`; add `SetRefreshInterval` |
| `internal/config/template.go` | remove the 3 adaptive `settingDoc` rows; keep `min_seconds` |
| docs | CHANGELOG / README / CLAUDE / memory |

---

## Task 1: Simplify the scheduler — fixed intervals, drop adaptive logic

**Files:**
- Modify: `internal/tui/refresh.go`
- Modify: `internal/tui/refresh_test.go`, `internal/tui/refresh_adaptive_test.go`

**Interfaces:**
- Consumes: existing `refreshItem`, `refreshIntervalFor(cfg, it) int`, `meanDuration`, `config.RefreshConfig.MinSeconds`, `scheduledItems`, `sourceNames`.
- Produces:
  - `scheduledInterval(cfg config.RefreshConfig, it refreshItem) (secs int, on bool)`
  - `dueItems(now time.Time, lastRun map[refreshItem]time.Time, cfg config.RefreshConfig, suppressed bool) []refreshItem` (no `durs` param)
  - `refreshTomlKey(it refreshItem) string`
  - `setRefreshIntervalField(cfg *config.RefreshConfig, it refreshItem, secs int)`
  - `refreshRateRows() []string` (kept, reworked to use `scheduledInterval`)

- [ ] **Step 1: Write the failing tests** (replace the adaptive tests). In `internal/tui/refresh_adaptive_test.go`, DELETE `TestEffectiveInterval` and `TestFetchIsConfigGated`, and add:

```go
func TestScheduledInterval(t *testing.T) {
	st := refreshItem{source: srcStatus}
	// configured 0 → off.
	if secs, on := scheduledInterval(config.RefreshConfig{Enabled: true}, st); on || secs != 0 {
		t.Fatalf("interval 0 → off, got %d/%v", secs, on)
	}
	// configured below the floor → clamped to min (default 10).
	if secs, on := scheduledInterval(config.RefreshConfig{Enabled: true, Status: 3}, st); !on || secs != 10 {
		t.Fatalf("3 → floored to 10, got %d/%v", secs, on)
	}
	// configured at/above the floor → passthrough.
	if secs, on := scheduledInterval(config.RefreshConfig{Enabled: true, Status: 30}, st); !on || secs != 30 {
		t.Fatalf("30 → 30, got %d/%v", secs, on)
	}
	// custom min_seconds honored.
	if secs, _ := scheduledInterval(config.RefreshConfig{Enabled: true, Status: 3, MinSeconds: 20}, st); secs != 20 {
		t.Fatalf("min 20 → 20, got %d", secs)
	}
}

func TestRefreshTomlKeyAndSetField(t *testing.T) {
	// feed's display name is "commits" but its toml key is "feed".
	if k := refreshTomlKey(refreshItem{source: srcFeed}); k != "feed" {
		t.Fatalf("feed key = feed, got %q", k)
	}
	if k := refreshTomlKey(fetchItem); k != "fetch" {
		t.Fatalf("fetch key = fetch, got %q", k)
	}
	var c config.RefreshConfig
	setRefreshIntervalField(&c, refreshItem{source: srcRemotes}, 45)
	setRefreshIntervalField(&c, fetchItem, 90)
	if c.Remotes != 45 || c.Fetch != 90 {
		t.Fatalf("set fields: got remotes=%d fetch=%d", c.Remotes, c.Fetch)
	}
}
```

- [ ] **Step 2: Run them — expect FAIL** (`go test ./internal/tui/ -run 'TestScheduledInterval|TestRefreshTomlKeyAndSetField'`) with undefined identifiers.

- [ ] **Step 3: Rewrite `refresh.go`.** Remove the `intervalState` type, its constants, and `stateLabel`. Remove `effectiveInterval` and `effectiveIntervalRaw`. Remove the `defaultMaxReadSeconds` and `defaultBackoffFactor` consts (keep `defaultMinSeconds` and `maxDurationSamples`). Add:

```go
// scheduledInterval returns an item's fixed poll interval in seconds and whether
// it is scheduled. The configured value is floored at min_seconds (default 10);
// a configured 0 means off. Measurements never affect this.
func scheduledInterval(cfg config.RefreshConfig, it refreshItem) (int, bool) {
	base := refreshIntervalFor(cfg, it)
	if base <= 0 {
		return 0, false
	}
	min := cfg.MinSeconds
	if min <= 0 {
		min = defaultMinSeconds
	}
	if base < min {
		base = min
	}
	return base, true
}

// refreshTomlKey is the [refresh] TOML key for an item. Note srcFeed's display
// name is "commits" but its config key is "feed".
func refreshTomlKey(it refreshItem) string {
	if it.isFetch {
		return "fetch"
	}
	switch it.source {
	case srcStatus:
		return "status"
	case srcBranches:
		return "branches"
	case srcRemotes:
		return "remotes"
	case srcWorktrees:
		return "worktrees"
	case srcTags:
		return "tags"
	case srcReflog:
		return "reflog"
	case srcFeed:
		return "feed"
	}
	return ""
}

// setRefreshIntervalField writes secs into the RefreshConfig field for an item.
func setRefreshIntervalField(cfg *config.RefreshConfig, it refreshItem, secs int) {
	if it.isFetch {
		cfg.Fetch = secs
		return
	}
	switch it.source {
	case srcStatus:
		cfg.Status = secs
	case srcBranches:
		cfg.Branches = secs
	case srcRemotes:
		cfg.Remotes = secs
	case srcWorktrees:
		cfg.Worktrees = secs
	case srcTags:
		cfg.Tags = secs
	case srcReflog:
		cfg.Reflog = secs
	case srcFeed:
		cfg.Feed = secs
	}
}
```

- [ ] **Step 4: Update `dueItems`** in `refresh.go` to the new signature (drop `durs`, use `scheduledInterval`):

```go
// dueItems returns the items whose fixed interval has elapsed this tick. off
// items are excluded. Pure.
func dueItems(now time.Time, lastRun map[refreshItem]time.Time, cfg config.RefreshConfig, suppressed bool) []refreshItem {
	if !cfg.Enabled || suppressed {
		return nil
	}
	var due []refreshItem
	for _, it := range scheduledItems {
		secs, on := scheduledInterval(cfg, it)
		if !on {
			continue
		}
		last, seen := lastRun[it]
		if !seen || now.Sub(last) >= time.Duration(secs)*time.Second {
			due = append(due, it)
		}
	}
	return due
}
```

- [ ] **Step 5: Update `refreshTick`'s call to `dueItems`** in `refresh.go` — drop the `m.refreshDur` argument:

```go
	due := dueItems(now, m.refreshLastRun, m.cfg.Refresh, false)
```

- [ ] **Step 6: Rework `refreshRateRows`** in `refresh.go` to use `scheduledInterval` (interval + min marker + avg stat; no state column):

```go
// refreshRateRows formats one line per scheduled item for the Refresh rates
// editor: name · interval · avg stat. Uses scheduledInterval for the interval
// (showing the floored value with a (min) marker when the configured value was
// below min_seconds). avg is informational only.
func (m Model) refreshRateRows() []string {
	rows := make([]string, 0, len(scheduledItems))
	for _, it := range scheduledItems {
		name := "fetch"
		if !it.isFetch {
			name = sourceNames[it.source]
		}
		cfgSecs := refreshIntervalFor(m.cfg.Refresh, it)
		secs, on := scheduledInterval(m.cfg.Refresh, it)
		intervalStr := "off"
		if on {
			intervalStr = fmt.Sprintf("every %ds", secs)
			if cfgSecs < secs {
				intervalStr += " (min)"
			}
		}
		samples := m.refreshDur[it]
		avgStr := "—"
		if len(samples) > 0 {
			avg := meanDuration(samples)
			if avg < time.Second {
				avgStr = fmt.Sprintf("%dms (%d)", avg.Milliseconds(), len(samples))
			} else {
				avgStr = fmt.Sprintf("%.1fs (%d)", avg.Seconds(), len(samples))
			}
		}
		rows = append(rows, fmt.Sprintf("%-10s  %-16s  avg %s", name, intervalStr, avgStr))
	}
	return rows
}
```

- [ ] **Step 7: Fix the migrated scheduler tests** in `internal/tui/refresh_test.go`. Update every `dueItems(...)` call to the new 4-arg signature (remove the `nil`/`durs` argument): `dueItems(now, last, cfg, suppressed)`. The existing assertions hold (status 30 → floored stays 30 since 30 ≥ 10; branches 0 → off). In `refresh_adaptive_test.go`, update `TestRefreshRateRows` to the new output:

```go
func TestRefreshRateRows(t *testing.T) {
	m := newTestModel(t)
	m.cfg.Refresh = config.RefreshConfig{Enabled: true, Status: 30, Remotes: 3}
	it := refreshItem{source: srcStatus}
	m.refreshDur[it] = []time.Duration{120 * time.Millisecond, 120 * time.Millisecond}
	joined := strings.Join(m.refreshRateRows(), "\n")
	if !strings.Contains(joined, "status") || !strings.Contains(joined, "every 30s") || !strings.Contains(joined, "120ms (2)") {
		t.Fatalf("status row wrong:\n%s", joined)
	}
	// remotes configured 3 < min 10 → floored, shown with (min).
	if !strings.Contains(joined, "every 10s (min)") {
		t.Fatalf("remotes should show floored 10s (min):\n%s", joined)
	}
}
```

- [ ] **Step 8: Run the tui tests — expect PASS** (`go test ./internal/tui/`). The package still references `cfg.MinSeconds` only (not the to-be-removed adaptive fields); `toggleAdaptive` (still present) keeps `DisableAdaptive` referenced — that's removed in Task 2.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/refresh.go internal/tui/refresh_test.go internal/tui/refresh_adaptive_test.go
git commit -m "refactor(tui): fixed-interval scheduler; drop adaptive effectiveInterval"
```

---

## Task 2: Remove the "Adaptive intervals" Settings toggle

**Files:**
- Modify: `internal/tui/settings_popup.go`
- Modify: `internal/tui/settings_popup_test.go` (remove the toggle test)

**Interfaces:**
- After this task, no TUI code references `config.RefreshConfig.DisableAdaptive`.

- [ ] **Step 1: Remove the toggle test.** In `internal/tui/settings_popup_test.go`, delete `TestToggleAdaptiveFlipsInMemory`.

- [ ] **Step 2: Remove the menu wiring** in `settings_popup.go`:
  - Delete the `settingsMenuAdaptive = "Adaptive intervals"` const.
  - Remove `settingsMenuAdaptive` from the `settingsMenu` slice (so it reads `…, settingsMenuAutoRefresh, settingsMenuRates}` — keep `settingsMenuRates`).
  - Delete the `if settingsMenu[i] == settingsMenuAdaptive { … }` branch in `settingsMenuLabel`.
  - Delete the `case settingsMenuAdaptive:` arm in the menu `enter` switch in `update`.
  - Delete the `toggleAdaptive` method.

- [ ] **Step 3: Run it — expect PASS** (`go test ./internal/tui/`). No remaining reference to `DisableAdaptive` in the TUI (verify: `grep -rn DisableAdaptive internal/tui` returns nothing).

- [ ] **Step 4: Commit**

```bash
git add internal/tui/settings_popup.go internal/tui/settings_popup_test.go
git commit -m "refactor(tui): remove Adaptive intervals Settings toggle"
```

---

## Task 3: Remove adaptive config keys; add `SetRefreshInterval`

**Files:**
- Modify: `internal/config/config.go`, `internal/config/write.go`, `internal/config/template.go`
- Modify: `internal/config/config_test.go`, `internal/config/write_test.go`

**Interfaces:**
- Produces: `config.SetRefreshInterval(path, source string, secs int) error`.
- Removes: `RefreshConfig.{DisableAdaptive,MaxReadSeconds,BackoffFactor}`, `SetGlobalRefreshDisableAdaptive`.
- Keeps: `RefreshConfig.MinSeconds`.

- [ ] **Step 1: Write the failing writer test.** In `internal/config/write_test.go`, REPLACE `TestSetGlobalRefreshDisableAdaptiveRoundTrips` with:

```go
func TestSetRefreshIntervalRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gg.toml")
	if err := os.WriteFile(path, []byte("[refresh]\nenabled = true\nstatus = 30\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetRefreshInterval(path, "branches", 45); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load("", path) // repo layer
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Refresh.Branches != 45 {
		t.Fatalf("branches should be 45, got %d", cfg.Refresh.Branches)
	}
	// Unrelated keys survive.
	if !cfg.Refresh.Enabled || cfg.Refresh.Status != 30 {
		t.Fatalf("unrelated keys clobbered: enabled=%v status=%d", cfg.Refresh.Enabled, cfg.Refresh.Status)
	}
	// Update an existing key in place.
	if err := SetRefreshInterval(path, "status", 0); err != nil {
		t.Fatal(err)
	}
	cfg2, _ := Load("", path)
	if cfg2.Refresh.Status != 0 {
		t.Fatalf("status should be 0, got %d", cfg2.Refresh.Status)
	}
}
```

- [ ] **Step 2: Run it — expect FAIL** (undefined `SetRefreshInterval`).

- [ ] **Step 3: Edit `write.go`** — delete `SetGlobalRefreshDisableAdaptive`, add:

```go
// SetRefreshInterval persists `[refresh] <source> = secs` to the given config
// file (the repo .gg.toml), preserving the rest of the file. Backs the Settings
// "Refresh rates" inline editor.
func SetRefreshInterval(path, source string, secs int) error {
	return setScalarLine(path, "refresh", source, strconv.Itoa(secs))
}
```

- [ ] **Step 4: Edit `config.go`** — remove the `DisableAdaptive`, `MaxReadSeconds`, `BackoffFactor` fields from `RefreshConfig` (keep `MinSeconds`), and remove their three blocks from `overlayRefresh` (keep the `MinSeconds` block).

- [ ] **Step 5: Edit `template.go`** — remove the `disable_adaptive`, `max_read_seconds`, `backoff_factor` `settingDoc` rows (keep `min_seconds`).

- [ ] **Step 6: Fix `config_test.go`** — in `TestOverlayRefreshAdaptiveFields`, drop the removed fields; rename/trim to cover only `MinSeconds`:

```go
func TestOverlayRefreshMinSeconds(t *testing.T) {
	dst := RefreshConfig{}
	overlayRefresh(&dst, RefreshConfig{MinSeconds: 20})
	if dst.MinSeconds != 20 {
		t.Fatalf("MinSeconds should overlay, got %d", dst.MinSeconds)
	}
	overlayRefresh(&dst, RefreshConfig{MinSeconds: 0}) // zero-is-unset
	if dst.MinSeconds != 20 {
		t.Fatalf("zero must not reset, got %d", dst.MinSeconds)
	}
}
```

- [ ] **Step 7: Run the config suite — expect PASS** (`go test ./internal/config/...`), including `TestSettingDocsCoverAllFields` (now that the struct has no `DisableAdaptive`/`MaxReadSeconds`/`BackoffFactor`, and the docs for them are gone, the guard stays consistent).

- [ ] **Step 8: Build the whole module — expect success** (`go build ./...`): confirms no stray references to the removed fields/writer anywhere.

- [ ] **Step 9: Commit**

```bash
git add internal/config/
git commit -m "refactor(config): remove adaptive [refresh] keys; add SetRefreshInterval"
```

---

## Task 4: "Refresh rates" inline editor

**Files:**
- Modify: `internal/tui/source.go` (`bootstrapCmd` + `configReadyMsg` carry the repo `.gg.toml` path)
- Modify: `internal/tui/model.go` (`Model.repoConfigPath`; set it in the `configReadyMsg` handler)
- Modify: `internal/tui/settings_popup.go` (editor state + keys + render)
- Modify: `internal/tui/refresh_adaptive_test.go` (editor tests)

**Interfaces:**
- Consumes: `scheduledItems`, `refreshTomlKey`, `setRefreshIntervalField`, `config.SetRefreshInterval`, `textfield`, `m.refreshLastRun`, `m.cfg.Refresh`.
- Produces: `Model.repoConfigPath string`; `Model.saveRefreshInterval(it refreshItem, secs int) Model`; `settingsPopup` fields `ratesSel int`, `ratesEditing bool`, `ratesField textfield`.

- [ ] **Step 1: Write the failing editor test** in `internal/tui/refresh_adaptive_test.go`:

```go
func TestSaveRefreshIntervalUpdatesAndReseeds(t *testing.T) {
	m := newTestModel(t)
	m.repoConfigPath = filepath.Join(t.TempDir(), ".gg.toml")
	m.refreshLastRun = map[refreshItem]time.Time{}
	it := refreshItem{source: srcBranches}
	m = m.saveRefreshInterval(it, 45)
	if m.cfg.Refresh.Branches != 45 {
		t.Fatalf("in-memory cfg not updated, got %d", m.cfg.Refresh.Branches)
	}
	if _, seeded := m.refreshLastRun[it]; !seeded {
		t.Fatal("lastRun must be reseeded so the edit doesn't burst")
	}
	cfg, err := config.Load("", m.repoConfigPath)
	if err != nil || cfg.Refresh.Branches != 45 {
		t.Fatalf("config file not written: err=%v branches=%d", err, cfg.Refresh.Branches)
	}
}
```

(Ensure `path/filepath` and `config` are imported in the test file.)

- [ ] **Step 2: Run it — expect FAIL** (undefined `repoConfigPath`/`saveRefreshInterval`).

- [ ] **Step 3: Carry the repo path from bootstrap.** In `internal/tui/source.go`, change `configReadyMsg` to carry the repo path, and set it in `bootstrapCmd`:

```go
type configReadyMsg struct {
	cfg      config.Config
	repoTOML string // <repo-top>/.gg.toml, "" if not in a repo
}
```

In `bootstrapCmd`, where it computes `top` and loads config, capture the path:

```go
		cfg := config.Defaults()
		repoTOML := ""
		if top, err := svc.TopLevel(ctx); err == nil && top != "" {
			repoTOML = filepath.Join(top, ".gg.toml")
			if c, cerr := config.Load(config.DefaultGlobalPath(), repoTOML); cerr == nil {
				cfg = c
			}
			if statePath != "" {
				_ = repos.Touch(statePath, top, time.Now())
			}
		}
		feed.SetPageSizes(cfg.UI.CommitInitialCount, cfg.UI.CommitBatchSize)
		svc.SetShowEOLOnlyChanges(cfg.UI.ShowEOLOnlyChanges)
		return configReadyMsg{cfg: cfg, repoTOML: repoTOML}
```

- [ ] **Step 4: Add the model field + store it.** In `internal/tui/model.go`, add to the struct (near `refreshLastRun`):

```go
	repoConfigPath      string                    // <repo-top>/.gg.toml; the refresh-rates editor writes here
```

In the `configReadyMsg` handler, before the reload, add `m.repoConfigPath = msg.repoTOML`.

- [ ] **Step 5: Add `saveRefreshInterval`** in `internal/tui/refresh.go`:

```go
// saveRefreshInterval applies an edited interval: updates the in-memory config
// (next tick honors it), reseeds the item's lastRun (no enable-burst), and
// persists [refresh] <key> = secs to the repo .gg.toml. A write error is
// surfaced on the status line but the in-memory value still applies.
func (m Model) saveRefreshInterval(it refreshItem, secs int) Model {
	if secs < 0 {
		secs = 0
	}
	setRefreshIntervalField(&m.cfg.Refresh, it, secs)
	if m.refreshLastRun == nil {
		m.refreshLastRun = map[refreshItem]time.Time{}
	}
	m.refreshLastRun[it] = time.Now()
	if m.repoConfigPath == "" {
		m.statusMsg = "refresh interval set (not saved: no repo config path)"
		return m
	}
	if err := config.SetRefreshInterval(m.repoConfigPath, refreshTomlKey(it), secs); err != nil {
		m.statusMsg = "refresh interval set but not saved: " + err.Error()
	}
	return m
}
```

- [ ] **Step 6: Run the save test — expect PASS** (`go test ./internal/tui/ -run TestSaveRefreshIntervalUpdatesAndReseeds`).

- [ ] **Step 7: Convert the `ratesView` screen to an editor.** In `settings_popup.go`:

Add fields to `settingsPopup`:

```go
	ratesSel     int       // selected row in the Refresh rates editor
	ratesEditing bool      // an interval field is open
	ratesField   textfield // the inline numeric editor
```

In `update`, replace the `if p.ratesView { … }` navigation block (the read-only one that only handled esc) with editor handling. Place it BEFORE the agent-picker switch, mirroring the existing screen guards:

```go
	if p.ratesView {
		if p.ratesEditing {
			switch msg.Type {
			case tea.KeyEnter:
				secs := 0
				if v := strings.TrimSpace(p.ratesField.Value()); v != "" {
					if n, err := strconv.Atoi(v); err == nil {
						secs = n
					}
				}
				m = m.saveRefreshInterval(scheduledItems[p.ratesSel], secs)
				p.ratesEditing = false
				return m, nil
			case tea.KeyEsc:
				p.ratesEditing = false
				return m, nil
			case tea.KeyRunes:
				digits := true
				for _, r := range msg.Runes {
					if r < '0' || r > '9' {
						digits = false
					}
				}
				if digits {
					p.ratesField.insert(msg.Runes)
				}
				return m, nil
			default:
				(&p.ratesField).HandleEditKey(msg)
				return m, nil
			}
		}
		switch msg.Type {
		case tea.KeyUp:
			if p.ratesSel > 0 {
				p.ratesSel--
			}
		case tea.KeyDown:
			if p.ratesSel < len(scheduledItems)-1 {
				p.ratesSel++
			}
		case tea.KeyEnter:
			cur := refreshIntervalFor(m.cfg.Refresh, scheduledItems[p.ratesSel])
			start := ""
			if cur > 0 {
				start = strconv.Itoa(cur)
			}
			p.ratesField = newTextField(start)
			p.ratesEditing = true
		}
		return m, nil
	}
```

The esc-to-leave for `ratesView` is the existing `case tea.KeyEsc:` block at the top of `update` (`if p.ratesView { p.ratesView = false; return m, nil }`) — keep it, but note when `p.ratesEditing` the inner handler consumes esc first (it returns before reaching the outer esc). Ensure the outer esc block checks `!p.ratesEditing` OR (simpler) the inner block above runs before the outer screen-level esc — verify ordering so esc cancels the edit, then a second esc leaves the screen.

- [ ] **Step 8: Render the editor** in `box`. Replace the `else if p.ratesView { … }` render branch with an interactive one:

```go
	} else if p.ratesView {
		b.WriteString("Refresh rates\n\n")
		if !m.cfg.Refresh.Enabled {
			b.WriteString("  auto-refresh is OFF — enable it in Settings → Auto-refresh\n\n")
		}
		for i, it := range scheduledItems {
			name := "fetch"
			if !it.isFetch {
				name = sourceNames[it.source]
			}
			prefix := "  "
			if i == p.ratesSel {
				prefix = "> "
			}
			var valCell string
			if p.ratesEditing && i == p.ratesSel {
				valCell = p.ratesField.View(true) + "s"
			} else {
				secs, on := scheduledInterval(m.cfg.Refresh, it)
				if on {
					valCell = fmt.Sprintf("every %ds", secs)
				} else {
					valCell = "off"
				}
			}
			// avg stat
			avgStr := "—"
			if s := m.refreshDur[it]; len(s) > 0 {
				avg := meanDuration(s)
				if avg < time.Second {
					avgStr = fmt.Sprintf("%dms (%d)", avg.Milliseconds(), len(s))
				} else {
					avgStr = fmt.Sprintf("%.1fs (%d)", avg.Seconds(), len(s))
				}
			}
			b.WriteString(fmt.Sprintf("%s%-10s  %-16s  avg %s\n", prefix, name, valCell, avgStr))
		}
		if p.ratesEditing {
			b.WriteString("\n[0-9] edit  [enter] save  [esc] cancel   (0 = off)")
		} else {
			b.WriteString("\n[↑/↓] select  [enter] edit  [esc] back")
		}
```

Add `"strconv"` to `settings_popup.go` imports if absent. (`fmt`, `strings`, `time` are already there.) The wide-width branch already includes `p.ratesView` (keep it). With this branch rendering inline, `refreshRateRows` is no longer called by the popup; remove `refreshRateRows` from `refresh.go` AND its test `TestRefreshRateRows` (the editor render replaces it), OR keep `refreshRateRows` only if still referenced — verify with `grep -rn refreshRateRows internal/tui` and delete if unused.

- [ ] **Step 9: Reset editor state when opening the screen.** In the menu `enter` switch `case settingsMenuRates:` set `p.ratesSel = 0; p.ratesEditing = false` (alongside the existing `p.ratesView = true`).

- [ ] **Step 10: Write an editor key-flow test** in `refresh_adaptive_test.go` (drives the popup through `Update`):

```go
func TestRatesEditorEnterEditSave(t *testing.T) {
	m := newTestModel(t)
	m.repoConfigPath = filepath.Join(t.TempDir(), ".gg.toml")
	m = m.openSettings()
	p := layerOf[*settingsPopup](m)
	p.ratesView = true
	p.ratesSel = 0 // status
	// enter → open edit field
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if !p.ratesEditing {
		t.Fatal("enter should open the edit field")
	}
	// type "25"
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	// enter → save
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})
	if p.ratesEditing {
		t.Fatal("enter should close the edit field")
	}
	if got := refreshIntervalFor(m.cfg.Refresh, scheduledItems[0]); got != 25 {
		t.Fatalf("status interval should be 25, got %d", got)
	}
}

// updateModel is a tiny helper to thread Update returns as Model.
func updateModel(m Model, msg tea.Msg) (Model, tea.Cmd) {
	nm, cmd := m.Update(msg)
	return nm.(Model), cmd
}
```

(`p` is a pointer into the layer stack, so it observes mutations across `Update` calls. Add `tea` import if absent — it is, from Task 4 earlier additions. If `updateModel` already exists elsewhere, reuse it instead of redefining.)

- [ ] **Step 11: Run the tui tests — expect PASS** (`go test ./internal/tui/`).

- [ ] **Step 12: Run `go vet` + gofmt** (`go vet ./internal/tui/ ./internal/config/ && gofmt -l internal/`) — expect clean.

- [ ] **Step 13: Commit**

```bash
git add internal/tui/ 
git commit -m "feat(tui): Refresh rates inline editor writes per-source intervals to repo .gg.toml"
```

---

## Task 5: Documentation

**Files:** `CHANGELOG.md`, `README.md`, `CLAUDE.md` (memory post-merge).

- [ ] **Step 1: CHANGELOG.md** — add a rework entry:

```markdown
- **Refresh rates editor (Phase C rework).** Background auto-refresh now runs on
  fixed, user-set per-source intervals (floored at `[refresh] min_seconds`,
  default 10) over the single-lane queue — the adaptive engine
  (`disable_adaptive`/`max_read_seconds`/`backoff_factor`) is removed. Settings →
  "Refresh rates" is now an inline editor: select a source, press enter, type the
  seconds (0 = off); it writes `[refresh] <source>` to the repo `.gg.toml`. Read
  durations are still measured and shown there as stats.
```

- [ ] **Step 2: README.md** — in the `[refresh]` section: remove `disable_adaptive`/`max_read_seconds`/`backoff_factor`; keep `min_seconds`; replace the adaptive prose with: fixed intervals floored at `min_seconds`; the Settings "Refresh rates" inline editor (writes the repo `.gg.toml`); per-source `0 = off`; stats column is informational.

- [ ] **Step 3: CLAUDE.md** — update the `tui` row (fixed-interval `scheduledInterval` over the single-lane queue; "Refresh rates" inline editor writing the repo `.gg.toml` via `SetRefreshInterval`; durations kept as stats) and the `config` row (drop the adaptive keys; `min_seconds` floor; writers are `SetGlobalDebugLogOperations`/`SetGlobalRefreshEnabled`/`SetRefreshInterval`).

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: Phase C rework — fixed intervals + Refresh rates editor"
```

---

## Self-review notes (author)

- **Spec coverage:** scheduling §→Task 1; remove adaptive §→Tasks 1-3; stats kept→Task 1 (rows) + retained measurement; editor §→Task 4; config writes §→Tasks 3-4; min_seconds kept→Tasks 1,3; docs→Task 5. No gap.
- **Compile order:** Task 1 stops `refresh.go` using `MaxReadSeconds`/`BackoffFactor`; Task 2 stops `settings_popup.go` using `DisableAdaptive`; Task 3 then removes the fields — each task builds.
- **Type consistency:** `scheduledInterval`/`dueItems`(4-arg)/`refreshTomlKey`/`setRefreshIntervalField`/`saveRefreshInterval`/`SetRefreshInterval`/`repoConfigPath`/`configReadyMsg.repoTOML` used identically across tasks.
- **Single-lane untouched:** no task edits `refreshTick`'s lane logic beyond the `dueItems` call arg, `bgFetchDoneMsg`, `startOp`, or the `dataAvailableMsg` lane-clear.
- **feed key gotcha:** `srcFeed` display name is "commits" but toml key "feed" — handled in `refreshTomlKey`, tested in Task 1.
