# Background Auto-Refresh — Implementation Plan (Phase B)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Periodically refresh data sources on their own, silently, on per-source configurable + individually-toggleable timers (off by default, plus a master on/off toggle), reusing Phase A's registry — without ever disrupting the user or letting background git block a user op.

**Architecture:** A thin scheduler over Phase A. The existing ~1s heartbeat tick consults a per-source **due-table** and fires Phase A's silent reads (`reloadSourcesCmd(srcs, manual=false)`) for sources whose interval elapsed, that are enabled, and that aren't suppressed. Background reads run under a cancellable context a starting user op cancels (requires the bug-#4 fix so a read blocked on the git semaphore actually observes cancellation). A quiet `fetch` task runs `git fetch` off the foreground op-slot and refreshes remotes on success.

**Tech Stack:** Go 1.26, Bubble Tea (`tea.Tick` heartbeat), the Phase A registry (`internal/tui/source.go`), `internal/config`, `internal/gitexec`, `go test`.

## Global Constraints

- Module `github.com/homeend/gigagit`, Go 1.26.
- `internal/tui` and `internal/cli` must NOT import `internal/git` — reach git only via `internal/domain` (archtest-guarded).
- TUI `Model` is a value receiver with pointer fields; mutate via returned copies. Maps (`srcGen`/`srcInflight`/`srcLoading`/`sel`) are reference-shared — only the UI thread writes them.
- A git verb is one invocation; reads go through domain queries.
- Config is read-at-startup; the ONLY runtime config write is the master `enabled` toggle, via a non-destructive line edit (mirror `SetGlobalDebugLogOperations`).
- Auto-refresh is SILENT: `manual=false` → no `srcLoading`, no spinner, no focus/cursor movement.
- Off by default: `[refresh] enabled` defaults false AND every interval defaults `0`.
- TDD; commit per task. `main` is trunk; do NOT merge (human merges). Work in worktree `.claude/worktrees/refresh-scheduler` on branch `feat/refresh-scheduler`.
- Test from worktree root: `cd /mnt/t/others/gigagit/.claude/worktrees/refresh-scheduler`. Pre-commit: `gofmt -l internal/ ; go vet ./internal/...`.

## File Structure

- **Modify** `internal/gitexec/limit.go` — bug #4: `select` on `ctx.Done()` vs the semaphore send (Task 1).
- **Modify** `internal/config/config.go` — `RefreshConfig` struct, `Config.Refresh`, `Defaults`, `overlayRefresh` (Task 2).
- **Modify** `internal/config/write.go` — `SetGlobalRefreshEnabled` (Task 3).
- **Modify** the config settings registry (the `gg config init` settingDocs) — one doc per new key (Task 2, via the `adding-config-entries` skill).
- **Create** `internal/tui/refresh.go` — the due-table + `dueSources` pure decision + `refreshTick` wiring + background ctx (Tasks 4–5).
- **Create** `internal/tui/refresh_test.go`.
- **Modify** `internal/tui/model.go` — model fields (`bgCtx`/`bgCancel`/`refreshLastRun`), the `heartbeatMsg` handler (call `refreshTick`), `readSourceCmd` ctx param (Phase A function), `Init` if needed (Tasks 4–5).
- **Modify** `internal/tui/op.go` — `startOp` cancels `bgCancel` (Task 5).
- **Modify** `internal/tui/settings_popup.go` (+ the settings menu rows) — master runtime toggle (Task 6).
- **Modify** `internal/tui/refresh.go` — quiet `fetch` task (Task 7).
- **Modify** `CHANGELOG.md`, `CLAUDE.md`, `README.md` (Task 8).

Tasks are ordered so the suite stays green throughout: #4 fix → config → scheduler (pure → wired) → toggle → fetch → docs.

---

### Task 1: Fix bug #4 — cancellable git-semaphore acquire

**Files:**
- Modify: `internal/gitexec/limit.go`
- Test: `internal/gitexec/limit_test.go` (create)

**Interfaces:**
- Produces: `LimitRunner.Run`/`RunEnv`/`Stream` now return `ctx.Err()` promptly when the context is cancelled while blocked acquiring the semaphore, instead of blocking until a slot frees.

Why first: Phase B's "a starting user op cancels in-flight background reads" is impossible while a blocked git call ignores `ctx` (it can't observe the cancel until a slot frees).

- [ ] **Step 1: Write the failing test**

```go
// internal/gitexec/limit_test.go
package gitexec

import (
	"context"
	"testing"
	"time"
)

// fillSem occupies all gitConcurrency slots so the next acquire must block.
func fillSem(t *testing.T) (release func()) {
	t.Helper()
	for i := 0; i < gitConcurrency; i++ {
		gitSem <- struct{}{}
	}
	return func() {
		for i := 0; i < gitConcurrency; i++ {
			<-gitSem
		}
	}
}

func TestLimitRunnerRunCancelsWhileBlocked(t *testing.T) {
	release := fillSem(t)
	defer release()
	lr := &LimitRunner{inner: stubRunner{}} // stubRunner: returns zero Result,nil; see note
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := lr.Run(ctx, "git", []string{"status"}); done <- err }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run must return ctx error when cancelled while blocked on the semaphore")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not observe cancellation while blocked (bug #4 not fixed)")
	}
}
```

Note: `stubRunner` must satisfy the `Runner` interface and return `(Result{}, nil)` for Run/RunEnv/Stream. If a test stub already exists in this package (search `rg -n "Runner interface|FakeRunner|stubRunner" internal/gitexec`), reuse it; otherwise define a minimal one in the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/refresh-scheduler && go test ./internal/gitexec/ -run TestLimitRunnerRunCancelsWhileBlocked -v`
Expected: FAIL — times out (current code blocks on `gitSem <- struct{}{}`).

- [ ] **Step 3: Implement the cancellable acquire**

Replace the bodies in `internal/gitexec/limit.go`:

```go
func (l *LimitRunner) RunEnv(ctx context.Context, name string, argv, env []string) (Result, error) {
	select {
	case gitSem <- struct{}{}:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	defer func() { <-gitSem }()
	return l.inner.RunEnv(ctx, name, argv, env)
}

func (l *LimitRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (Result, error) {
	select {
	case gitSem <- struct{}{}:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	defer func() { <-gitSem }()
	return l.inner.Stream(ctx, name, argv, onLine)
}
```

`Run` already delegates to `RunEnv`, so it inherits the fix. Confirm the `Result` zero value and error are what callers expect on cancel (match how other cancelled paths in this package return).

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/refresh-scheduler && go test ./internal/gitexec/ -v`
Expected: PASS (new test + existing).

- [ ] **Step 5: Commit**

```bash
git add internal/gitexec/limit.go internal/gitexec/limit_test.go
git commit -m "fix(gitexec): cancellable git-semaphore acquire (bug #4)"
```

---

### Task 2: `[refresh]` config section

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go` (append)
- Modify: the settingDoc registry for `gg config init` (see the `adding-config-entries` skill)

**Interfaces:**
- Produces: `type RefreshConfig struct { Enabled bool; Status,Branches,Remotes,Worktrees,Tags,Reflog,Feed,Fetch int }` with snake_case toml tags; `Config.Refresh RefreshConfig`; defaults (Enabled=false, intervals 0); `overlayRefresh`.

**FIRST — read the config skill:** before editing, read and follow `.claude/skills/adding-config-entries/SKILL.md` (it lists the full checklist: struct + Defaults + overlay + the settingDoc registry + the hand-synced literals). This task implements that checklist for the `[refresh]` keys. The code below is the struct/overlay; the settingDoc entries follow the skill.

- [ ] **Step 1: Write the failing test**

```go
// internal/config/config_test.go (append)
func TestRefreshConfigDefaultsOff(t *testing.T) {
	c := Defaults()
	if c.Refresh.Enabled {
		t.Error("refresh must default disabled")
	}
	if c.Refresh.Status != 0 || c.Refresh.Fetch != 0 {
		t.Error("refresh intervals must default 0 (off)")
	}
}

func TestRefreshConfigOverlayAndParse(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, ".gg.toml")
	if err := os.WriteFile(repo, []byte("[refresh]\nenabled = true\nstatus = 30\nfetch = 300\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load("", repo)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Refresh.Enabled || c.Refresh.Status != 30 || c.Refresh.Fetch != 300 {
		t.Fatalf("parsed/overlaid refresh wrong: %+v", c.Refresh)
	}
}
```

(Confirm `filepath`/`os` are imported in the test file.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/refresh-scheduler && go test ./internal/config/ -run 'TestRefreshConfig' -v`
Expected: FAIL — `c.Refresh` undefined.

- [ ] **Step 3: Add the struct, field, defaults, overlay**

In `internal/config/config.go`:

```go
// RefreshConfig configures background auto-refresh (Phase B). All off by
// default. Enabled is the master gate; each interval is seconds (0 = that
// source never auto-refreshes). TOML keys snake_case under [refresh].
type RefreshConfig struct {
	Enabled   bool `toml:"enabled"`   // master switch; default false (whole feature off)
	Status    int  `toml:"status"`    // seconds between background status reads; 0 = off
	Branches  int  `toml:"branches"`
	Remotes   int  `toml:"remotes"`
	Worktrees int  `toml:"worktrees"`
	Tags      int  `toml:"tags"`
	Reflog    int  `toml:"reflog"`
	Feed      int  `toml:"feed"`
	Fetch     int  `toml:"fetch"` // seconds between background `git fetch`; 0 = off
}
```

Add to `Config`:
```go
	Refresh  RefreshConfig  `toml:"refresh"`
```

`Defaults()` needs no `Refresh` entry (zero value = Enabled false + all intervals 0 = correct default). Add `overlayRefresh` and call it in `Load` alongside the other overlays (find where `overlayUI`/`overlayDebug` are invoked per layer and add `overlayRefresh(&cfg.Refresh, layer.Refresh)`):

```go
// overlayRefresh copies each set field of src onto dst. Intervals use the
// zero-is-unset rule (0 = unset). Enabled uses inverted polarity: default false
// is "off", so only a true in a higher layer overlays (a higher layer cannot
// reset a lower layer's true back to false — matching LogOperations).
func overlayRefresh(dst *RefreshConfig, src RefreshConfig) {
	if src.Enabled {
		dst.Enabled = true
	}
	if src.Status > 0 {
		dst.Status = src.Status
	}
	if src.Branches > 0 {
		dst.Branches = src.Branches
	}
	if src.Remotes > 0 {
		dst.Remotes = src.Remotes
	}
	if src.Worktrees > 0 {
		dst.Worktrees = src.Worktrees
	}
	if src.Tags > 0 {
		dst.Tags = src.Tags
	}
	if src.Reflog > 0 {
		dst.Reflog = src.Reflog
	}
	if src.Feed > 0 {
		dst.Feed = src.Feed
	}
	if src.Fetch > 0 {
		dst.Fetch = src.Fetch
	}
}
```

- [ ] **Step 4: Add settingDocs for `gg config init`**

Per the `adding-config-entries` skill, add a `settingDoc` for each new key (`refresh.enabled` + the 8 intervals) so `gg config init` emits commented, documented lines, and hand-sync whatever literals the skill names. Verify with the skill's test/command (e.g. `gg config init` output, or the registry test).

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/refresh-scheduler && go test ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/config/
git commit -m "feat(config): [refresh] section (master enabled + per-source intervals)"
```

---

### Task 3: `SetGlobalRefreshEnabled` (runtime master-toggle writer)

**Files:**
- Modify: `internal/config/write.go`
- Test: `internal/config/write_test.go` (append; find the existing `SetGlobalDebugLogOperations` test and mirror it)

**Interfaces:**
- Produces: `func SetGlobalRefreshEnabled(path string, on bool) error` — persists `[refresh] enabled = true|false` via the existing `setScalarLine`, preserving the rest of the file.

- [ ] **Step 1: Write the failing test**

```go
// internal/config/write_test.go (append)
func TestSetGlobalRefreshEnabledRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := SetGlobalRefreshEnabled(path, true); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, "")
	if err != nil || !c.Refresh.Enabled {
		t.Fatalf("enabled not persisted: %+v err=%v", c.Refresh, err)
	}
	if err := SetGlobalRefreshEnabled(path, false); err != nil {
		t.Fatal(err)
	}
	c, _ = Load(path, "")
	if c.Refresh.Enabled {
		t.Fatal("disabled not persisted")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/refresh-scheduler && go test ./internal/config/ -run TestSetGlobalRefreshEnabled -v`
Expected: FAIL — `SetGlobalRefreshEnabled` undefined.

- [ ] **Step 3: Implement**

In `internal/config/write.go`:

```go
// SetGlobalRefreshEnabled persists `[refresh] enabled` to the global config
// file (preserving comments), backing the , Settings master auto-refresh
// toggle — the second runtime config writer (see SetGlobalDebugLogOperations).
func SetGlobalRefreshEnabled(path string, on bool) error {
	return setScalarLine(path, "refresh", "enabled", strconv.FormatBool(on))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/refresh-scheduler && go test ./internal/config/ -run TestSetGlobalRefreshEnabled -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/write.go internal/config/write_test.go
git commit -m "feat(config): SetGlobalRefreshEnabled runtime master-toggle writer"
```

---

### Task 4: Due-table — the pure scheduling decision

**Files:**
- Create: `internal/tui/refresh.go`
- Create: `internal/tui/refresh_test.go`

**Interfaces:**
- Produces:
  - `type refreshItem struct { source sourceKey; isFetch bool }` — schedulable items; the 8 sources plus a synthetic fetch item.
  - `func refreshIntervalFor(cfg config.RefreshConfig, it refreshItem) int` — seconds for an item (`cfg.Status` … `cfg.Fetch`); 0 = off.
  - `func dueItems(now time.Time, lastRun map[refreshItem]time.Time, cfg config.RefreshConfig, suppressed bool) []refreshItem` — the items whose interval > 0 and elapsed, returns empty when `suppressed` or `!cfg.Enabled`.

This task is PURE (no timers, no model mutation) so it is fully unit-testable.

- [ ] **Step 1: Write the failing test**

```go
// internal/tui/refresh_test.go
package tui

import (
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
)

func TestDueItemsRespectsIntervalAndMaster(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	cfg := config.RefreshConfig{Enabled: true, Status: 30, Branches: 0}
	last := map[refreshItem]time.Time{{source: srcStatus}: t0}

	// 29s later: not due.
	if d := dueItems(t0.Add(29*time.Second), last, cfg, false); len(d) != 0 {
		t.Fatalf("status should not be due at 29s, got %v", d)
	}
	// 31s later: due.
	d := dueItems(t0.Add(31*time.Second), last, cfg, false)
	if len(d) != 1 || d[0].source != srcStatus {
		t.Fatalf("status should be due at 31s, got %v", d)
	}
	// Branches interval 0 → never due even at 31s.
	for _, it := range d {
		if it.source == srcBranches {
			t.Fatal("branches (interval 0) must never be due")
		}
	}
	// Master off → nothing due.
	if d := dueItems(t0.Add(31*time.Second), last, cfg, false); len(d) == 1 {
		cfgOff := cfg
		cfgOff.Enabled = false
		if d2 := dueItems(t0.Add(31*time.Second), last, cfgOff, false); len(d2) != 0 {
			t.Fatalf("master off must yield nothing, got %v", d2)
		}
	}
	// Suppressed → nothing due.
	if d := dueItems(t0.Add(31*time.Second), last, cfg, true); len(d) != 0 {
		t.Fatalf("suppressed must yield nothing, got %v", d)
	}
}

func TestDueItemsFirstRunWhenUnseen(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	cfg := config.RefreshConfig{Enabled: true, Status: 30}
	// No lastRun entry → treat as due immediately (first poll after enable).
	d := dueItems(t0, map[refreshItem]time.Time{}, cfg, false)
	if len(d) != 1 || d[0].source != srcStatus {
		t.Fatalf("unseen item with interval>0 should be due, got %v", d)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/refresh-scheduler && go test ./internal/tui/ -run 'TestDueItems' -v`
Expected: FAIL — `dueItems`/`refreshItem` undefined.

- [ ] **Step 3: Implement the pure decision**

```go
// internal/tui/refresh.go
package tui

import (
	"time"

	"github.com/homeend/gigagit/internal/config"
)

// refreshItem is one schedulable background-refresh unit: a source read, or the
// synthetic fetch task (isFetch).
type refreshItem struct {
	source  sourceKey
	isFetch bool
}

var fetchItem = refreshItem{isFetch: true}

// scheduledItems is the fixed set the scheduler considers each tick: the panel
// sources plus fetch. (srcIdentity is intentionally excluded — identity changes
// only via the SetIdentity op, never on its own.)
var scheduledItems = []refreshItem{
	{source: srcStatus}, {source: srcBranches}, {source: srcRemotes},
	{source: srcWorktrees}, {source: srcTags}, {source: srcReflog},
	{source: srcFeed}, fetchItem,
}

// refreshIntervalFor returns the configured seconds for it (0 = off).
func refreshIntervalFor(cfg config.RefreshConfig, it refreshItem) int {
	if it.isFetch {
		return cfg.Fetch
	}
	switch it.source {
	case srcStatus:
		return cfg.Status
	case srcBranches:
		return cfg.Branches
	case srcRemotes:
		return cfg.Remotes
	case srcWorktrees:
		return cfg.Worktrees
	case srcTags:
		return cfg.Tags
	case srcReflog:
		return cfg.Reflog
	case srcFeed:
		return cfg.Feed
	}
	return 0
}

// dueItems returns the items that should fire now: master enabled, not
// suppressed, interval > 0, and (now - lastRun) >= interval. An item with no
// lastRun entry is due immediately (first poll after enabling).
func dueItems(now time.Time, lastRun map[refreshItem]time.Time, cfg config.RefreshConfig, suppressed bool) []refreshItem {
	if !cfg.Enabled || suppressed {
		return nil
	}
	var due []refreshItem
	for _, it := range scheduledItems {
		secs := refreshIntervalFor(cfg, it)
		if secs <= 0 {
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

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/refresh-scheduler && go test ./internal/tui/ -run 'TestDueItems' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/refresh.go internal/tui/refresh_test.go
git commit -m "feat(tui): pure due-table decision for background refresh"
```

---

### Task 5: Wire the scheduler — heartbeat tick, background ctx, op preemption

**Files:**
- Modify: `internal/tui/model.go` (model fields; `heartbeatMsg` handler; `readSourceCmd` ctx param)
- Modify: `internal/tui/refresh.go` (the `refreshTick` model method + suppression predicate + `bgCtx` helper)
- Modify: `internal/tui/op.go` (`startOp` cancels `bgCancel`)
- Modify: `internal/tui/refresh_test.go`

**Interfaces:**
- Consumes: `dueItems` (Task 4), Phase A `reloadSourcesCmd`/`readSourceCmd`, `m.srcInflight`.
- Produces: `func (m Model) refreshSuppressed() bool`; `func (m Model) refreshTick(now time.Time) (Model, tea.Cmd)`; model fields `bgCtx context.Context`, `bgCancel context.CancelFunc`, `refreshLastRun map[refreshItem]time.Time`.

- [ ] **Step 1: Write the failing test**

```go
// internal/tui/refresh_test.go (append)
func TestRefreshTickFiresSilentReadAndIsSuppressed(t *testing.T) {
	m := newTestModel(t)
	m.cfg.Refresh = config.RefreshConfig{Enabled: true, Status: 30}
	t0 := time.Unix(2_000_000, 0)

	// Not suppressed, status unseen → due → fires a silent read (gen bumps, NO srcLoading).
	genBefore := m.srcGen[srcStatus]
	m2, cmd := m.refreshTick(t0)
	if cmd == nil {
		t.Fatal("expected a background read command")
	}
	if m2.srcGen[srcStatus] != genBefore+1 {
		t.Fatal("status gen should bump for a fired background read")
	}
	if m2.srcLoading[srcStatus] {
		t.Fatal("background (auto) read must NOT set srcLoading (silent)")
	}

	// Suppressed (op running) → no fire.
	mb := newTestModel(t)
	mb.cfg.Refresh = config.RefreshConfig{Enabled: true, Status: 30}
	mb.running = true
	_, cmd2 := mb.refreshTick(t0)
	if cmd2 != nil {
		t.Fatal("must not fire while an op is running")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/refresh-scheduler && go test ./internal/tui/ -run 'TestRefreshTick' -v`
Expected: FAIL — `m.refreshTick` undefined.

- [ ] **Step 3: Add model fields + ctx refactor**

In `internal/tui/model.go` `Model` struct:
```go
	bgCtx          context.Context        // context for in-flight background (auto) reads; cancelled when a user op starts
	bgCancel       context.CancelFunc     // cancels bgCtx; nil when no background batch is active
	refreshLastRun map[refreshItem]time.Time // last time each scheduled item fired (background scheduler)
```
Initialize `refreshLastRun: map[refreshItem]time.Time{}` in the constructor (alongside `srcGen` etc.).

Refactor Phase A's `readSourceCmd` to take a context (so background reads are cancellable). Change its signature from `func (m Model) readSourceCmd(s sourceKey, manual bool) tea.Cmd` to `func (m Model) readSourceCmd(ctx context.Context, s sourceKey, manual bool) tea.Cmd`, use that `ctx` instead of the internal `context.Background()`, and update its only caller `reloadSourcesCmd` to pass `context.Background()` (manual reads stay uncancellable-by-op, which is correct — a user-initiated refresh shouldn't be cancelled by the next op). Run `rg -n "readSourceCmd\(" internal/tui` to find every caller.

- [ ] **Step 4: Implement `refreshSuppressed`, `refreshTick`, bg ctx**

In `internal/tui/refresh.go`:
```go
import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/homeend/gigagit/internal/config"
)

// refreshSuppressed reports whether background auto-refresh must hold off right
// now: a running op, an open overlay/modal/decider, or active filter/search
// typing. (Per-source in-flight is handled per item in refreshTick.)
func (m Model) refreshSuppressed() bool {
	if m.running || m.loading {
		return true
	}
	if m.modal != nil || m.layerActive() { // layerActive: any popup/view/diff layer open — confirm helper name
		return true
	}
	if m.filterTyping || m.highlightTyping {
		return true
	}
	return false
}

// refreshTick is called from the heartbeat. It fires silent reads for every due
// item that is not already in-flight, under a shared cancellable bg context that
// a starting user op cancels.
func (m Model) refreshTick(now time.Time) (Model, tea.Cmd) {
	due := dueItems(now, m.refreshLastRun, m.cfg.Refresh, m.refreshSuppressed())
	if len(due) == 0 {
		return m, nil
	}
	if m.bgCancel == nil {
		m.bgCtx, m.bgCancel = context.WithCancel(context.Background())
	}
	var cmds []tea.Cmd
	for _, it := range due {
		if it.isFetch {
			m.refreshLastRun[it] = now
			cmds = append(cmds, m.bgFetchCmd(m.bgCtx)) // Task 7 (stub returns nil until then)
			continue
		}
		if m.srcInflight[it.source] {
			continue // don't stack a second read of a source already loading
		}
		m.srcGen[it.source]++
		m.srcInflight[it.source] = true
		m.refreshLastRun[it] = now
		cmds = append(cmds, m.readSourceCmd(m.bgCtx, it.source, false)) // manual=false → silent
	}
	if len(cmds) == 0 {
		return m, nil
	}
	return m, tea.Batch(cmds...)
}
```

Notes:
- Confirm the overlay-open predicate: `rg -n "func (m Model) layer|layers \[\]|len(m.layer" internal/tui` — use the real "any layer open" check (Phase A's blank-gate / view code already has one). If none exists, inline the correct check against the layer stack field.
- Until Task 7, add a temporary `func (m Model) bgFetchCmd(ctx context.Context) tea.Cmd { return nil }` so this compiles; Task 7 replaces it.

In `internal/tui/model.go` `heartbeatMsg` handler, call the tick (keep the existing re-arm):
```go
	case heartbeatMsg:
		var cmd tea.Cmd
		m, cmd = m.refreshTick(time.Now())
		return m, tea.Batch(cmd, heartbeatCmd())
```

In `internal/tui/op.go` `startOp`, cancel in-flight background reads before launching the op (user wins):
```go
func (m Model) startOp(op engine.Operation) (Model, tea.Cmd) {
	if m.bgCancel != nil {
		m.bgCancel() // preempt in-flight background reads so the user's op gets the git slots
		m.bgCancel = nil
	}
	m.pendingSources = opAffectedSources(op)
	// … rest unchanged …
```

- [ ] **Step 5: Run tests + build**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/refresh-scheduler && go test ./internal/tui/ -run 'TestRefreshTick|TestDueItems' -v && go build ./cmd/gg`
Expected: PASS, clean build. Also run the full `go test ./internal/tui/` to confirm the `readSourceCmd` signature change didn't break Phase A tests; fix call sites minimally if so.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/refresh.go internal/tui/refresh_test.go internal/tui/model.go internal/tui/op.go
git commit -m "feat(tui): background refresh scheduler — heartbeat tick + op preemption"
```

---

### Task 6: Master runtime toggle in Settings

**Files:**
- Modify: `internal/tui/settings_popup.go` (+ wherever the settings menu rows/labels are defined)
- Test: `internal/tui/settings_popup_test.go` (append; find existing settings/opLog toggle tests and mirror)

**Interfaces:**
- Consumes: `config.SetGlobalRefreshEnabled` (Task 3), `m.cfg.Refresh.Enabled`.
- Produces: `func (m Model) toggleAutoRefresh() Model`; a settings menu row that shows the state and invokes it.

- [ ] **Step 1: Write the failing test**

```go
// internal/tui/settings_popup_test.go (append)
func TestToggleAutoRefreshFlipsInMemory(t *testing.T) {
	m := newTestModel(t)
	m.statePath = "" // avoid touching the real global config in tests; see note
	if m.cfg.Refresh.Enabled {
		t.Fatal("precondition: starts disabled")
	}
	m = m.toggleAutoRefresh()
	if !m.cfg.Refresh.Enabled {
		t.Fatal("toggle should enable in-memory")
	}
	m = m.toggleAutoRefresh()
	if m.cfg.Refresh.Enabled {
		t.Fatal("toggle should disable in-memory")
	}
}
```

Note: `toggleAutoRefresh` persists to `config.DefaultGlobalPath()` like `toggleOpLog`. To keep the test from writing the real user config, check how the existing `toggleOpLog` test isolates this (e.g. an env override of the config path, or asserting only the in-memory flip and tolerating a save error). Mirror that approach; if the persistence isn't isolatable, assert the in-memory flip and that any save error lands in `m.statusMsg` rather than panicking.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/refresh-scheduler && go test ./internal/tui/ -run TestToggleAutoRefresh -v`
Expected: FAIL — `toggleAutoRefresh` undefined.

- [ ] **Step 3: Implement the toggle (mirror `toggleOpLog`)**

In `internal/tui/settings_popup.go`:
```go
// toggleAutoRefresh flips the master background-refresh switch, persisting to
// the global config so it survives restarts (mirrors toggleOpLog).
func (m Model) toggleAutoRefresh() Model {
	want := !m.cfg.Refresh.Enabled
	m.cfg.Refresh.Enabled = want // in-memory flip takes effect on the next heartbeat tick
	if err := config.SetGlobalRefreshEnabled(config.DefaultGlobalPath(), want); err != nil {
		m.statusMsg = "auto-refresh toggled but not saved: " + err.Error()
		return m
	}
	if want {
		m.statusMsg = "auto-refresh on (per-source intervals from [refresh])"
	} else {
		m.statusMsg = "auto-refresh off"
	}
	return m
}
```

- [ ] **Step 4: Add the settings menu row**

Find how settings rows are declared (e.g. the operation-log row + its dynamic label via `settingsMenuLabel`) and add an "Auto-refresh: on/off" row that calls `toggleAutoRefresh`. Mirror the operation-log row exactly (label reflects `m.cfg.Refresh.Enabled`). Confirm the row appears and dispatches by running the settings popup test style already in the suite.

- [ ] **Step 5: Run tests + build**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/refresh-scheduler && go test ./internal/tui/ -run 'TestToggleAutoRefresh|Settings' -v && go build ./cmd/gg`
Expected: PASS, clean build.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/settings_popup.go internal/tui/settings_popup_test.go
git commit -m "feat(tui): Settings master auto-refresh toggle"
```

---

### Task 7: Quiet `fetch` task

**Files:**
- Modify: `internal/tui/refresh.go` (replace the `bgFetchCmd` stub)
- Test: `internal/tui/refresh_test.go` (append) — use the test model's FakeRunner/real-repo style the suite uses

**Interfaces:**
- Consumes: `m.svc` (`domain.Service`), `engine.Fetch`, Phase A `readSourceCmd` (for the remotes refresh on success), `observ.NoteFailure` (silent error record).
- Produces: `func (m Model) bgFetchCmd(ctx context.Context) tea.Cmd`; `type bgFetchDoneMsg struct{ err error }`; its handler refreshes the `remotes` source silently on success.

Design: run `engine.Fetch` via `svc.Execute` under the background ctx, NOT through `startOp` — no `m.running`, no op slot, no modal, no busy line. Discard events; answer any decision with a no-op decider (fetch should not fork). On success, fire a silent `remotes` read; on error, swallow + record to the session error log (already wired process-globally in Phase A's error seam).

- [ ] **Step 1: Write the failing test**

```go
// internal/tui/refresh_test.go (append)
func TestBgFetchRefreshesRemotesOnSuccessSilently(t *testing.T) {
	m := newTestModelWithRemote(t) // a model whose repo has a reachable remote; reuse the suite's remote helper
	msg := m.bgFetchCmd(context.Background())()
	done, ok := msg.(bgFetchDoneMsg)
	if !ok {
		t.Fatalf("want bgFetchDoneMsg, got %T", msg)
	}
	if done.err != nil {
		t.Fatalf("fetch should succeed against the test remote: %v", done.err)
	}
	// Delivering it must trigger a SILENT remotes refresh (gen bump, no srcLoading),
	// and never set m.running or a modal.
	nm, _ := m.Update(done)
	mm := nm.(Model)
	if mm.running || mm.modal != nil {
		t.Fatal("background fetch must not enter the foreground op path")
	}
	if mm.srcLoading[srcRemotes] {
		t.Fatal("remotes refresh after bg fetch must be silent")
	}
}
```

Note: find the suite's helper that builds a model with a working remote (`rg -n "func newTestModelWith|Remote|httptest|bare" internal/tui/*_test.go`). If none exists, build a minimal local bare-remote temp repo the way `internal/domain` or `e2e` tests do, or assert the failure path instead (see Step 4) — but a success-path test is preferred.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/refresh-scheduler && go test ./internal/tui/ -run 'TestBgFetch' -v`
Expected: FAIL — `bgFetchDoneMsg` undefined / stub returns nil.

- [ ] **Step 3: Implement the quiet fetch**

```go
// internal/tui/refresh.go (replace the stub)
import (
	// … existing …
	"github.com/homeend/gigagit/internal/engine"
)

// bgFetchDoneMsg lands when a background fetch finishes. On success the handler
// fires a silent remotes refresh; on error it is swallowed (already recorded to
// the session error log by the domain failure seam).
type bgFetchDoneMsg struct{ err error }

// bgFetchCmd runs `git fetch` quietly — outside the foreground op slot, no
// m.running, no modal — under the background context.
func (m Model) bgFetchCmd(ctx context.Context) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		// Discard events; fetch does not fork, but pass a decider that never blocks.
		events := make(chan engine.Event, 8)
		go func() { for range events { } }()
		_, err := svc.Execute(ctx, engine.Fetch{}, events, bgDecider{})
		close(events)
		return bgFetchDoneMsg{err: err}
	}
}
```

Add a non-blocking decider (confirm the `Decider`/`uiDecider` interface in `op.go` and the engine package; `bgDecider` must satisfy the same interface `startOp` passes):
```go
// bgDecider answers any mid-op fork by declining/aborting — a background task
// must never block on a human. (Fetch should not fork; this is belt-and-braces.)
type bgDecider struct{}
// Implement the Decider interface to pick the abort/first option. Match the real
// signature found in internal/engine (e.g. Decide(ctx, DecisionRequest) (DecisionResponse, error)).
```

In `internal/tui/model.go` `Update`, handle the result:
```go
	case bgFetchDoneMsg:
		if msg.err != nil {
			return m, nil // silent; the domain failure seam already logged it
		}
		m.srcGen[srcRemotes]++
		m.srcInflight[srcRemotes] = true
		return m, m.readSourceCmd(m.bgCtx, srcRemotes, false) // silent remotes refresh
```
(If `m.bgCtx` may be nil here, fall back to `context.Background()`.)

- [ ] **Step 4: Run tests + build**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/refresh-scheduler && go test ./internal/tui/ -run 'TestBgFetch|TestRefreshTick' -v && go build ./cmd/gg`
Expected: PASS, clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/refresh.go internal/tui/refresh_test.go internal/tui/model.go
git commit -m "feat(tui): quiet background fetch task → silent remotes refresh"
```

---

### Task 8: Full gate + docs

**Files:**
- Modify: `CHANGELOG.md` (always), `CLAUDE.md`, `README.md`

- [ ] **Step 1: Full suite + race + build + fmt/vet**

Run, from the worktree root:
```bash
cd /mnt/t/others/gigagit/.claude/worktrees/refresh-scheduler
gofmt -l internal/ && go vet ./internal/... && go build ./cmd/gg && ./test.sh && ./test.sh race
```
Expected: gofmt prints nothing; vet clean; build clean; both test runs green. Fix anything red before docs.

- [ ] **Step 2: CHANGELOG.md**

Add an Unreleased entry: background auto-refresh (Phase B) — per-source configurable + individually-toggleable timers via `[refresh]` (off by default), a master `enabled` switch with a Settings runtime toggle, silent auto reads, a quiet background `git fetch`, user-op-preempts-background (bug #4 fixed). Note Phase C (adaptive intervals) is next.

- [ ] **Step 3: CLAUDE.md**

- `config` row: note the new `[refresh]` section and that the master `enabled` is the second runtime config writer (`SetGlobalRefreshEnabled`).
- `tui` row: note `refresh.go` — the background scheduler (heartbeat-driven due-table over the Phase A registry; silent reads; quiet fetch; op-preempts-background).
- `gitexec` row: note `LimitRunner` now honors `ctx` while acquiring the semaphore (bug #4 fixed).

- [ ] **Step 4: README.md**

Document the `[refresh]` config (with the example keys + that it's off by default), the Settings master toggle, and that background refresh is silent. Keep it to the existing config/settings section's style.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md CLAUDE.md README.md
git commit -m "docs: background auto-refresh (Phase B) — changelog, package map, readme"
```

---

## Self-Review

**Spec coverage:**
- Polling scheduler over Phase A → Tasks 4–5. ✓
- Off by default + per-source interval + per-source toggle (interval 0) → Task 2 (`RefreshConfig`), Task 4 (`refreshIntervalFor`/`dueItems`). ✓
- Master `enabled` toggle (config + runtime Settings toggle, non-destructive write) → Task 2 (field), Task 3 (writer), Task 6 (toggle). ✓
- Silent auto reads (manual=false, no spinner) → Task 5 (`readSourceCmd(ctx, s, false)`), tested. ✓
- Suppression (op running, overlay/modal, filter/search typing, source in-flight) → Task 5 (`refreshSuppressed` + per-item in-flight). ✓
- User op preempts background (needs #4) → Task 1 (#4), Task 5 (`startOp` cancels `bgCancel`, reads under `bgCtx`). ✓
- Quiet fetch → remotes refresh; silent on failure → Task 7. ✓
- Fetch carries network/auth risk, off the op slot → Task 7 (no `startOp`, `bgDecider`, swallow errors). ✓
- Cursor/selection survive silent refresh → inherited from Phase A (selection-by-identity); covered by Phase A tests + Task 5's no-`srcLoading` assertion. ✓
- Phase C deferred (due-table seam) → not implemented; noted. ✓

**Placeholder scan:** The `bgDecider` interface body and a few test helpers (`newTestModelWithRemote`, the overlay-open predicate `layerActive`) are flagged with explicit "confirm the real name/signature with rg" instructions — these are verification points against live code, not content gaps. No TBD/TODO.

**Type consistency:** `refreshItem`, `dueItems`, `refreshIntervalFor`, `scheduledItems`, `refreshTick`, `refreshSuppressed`, `bgCtx`/`bgCancel`/`refreshLastRun`, `bgFetchCmd`/`bgFetchDoneMsg`, `RefreshConfig`/`SetGlobalRefreshEnabled`, `toggleAutoRefresh` are used consistently across tasks. `readSourceCmd`'s new `ctx` first param is threaded through Tasks 5 and 7 and its Phase A caller `reloadSourcesCmd`.

**Open verification points (flagged inline) the executor resolves against live code:**
1. The "any overlay/layer open" predicate name (Phase A's view code has one).
2. The `Decider` interface signature for `bgDecider` (in `internal/engine`/`op.go`).
3. The settings-menu row declaration + dynamic-label mechanism (mirror the op-log row).
4. The suite's remote-capable test-model helper for Task 7 (else build a bare-remote temp repo).
5. How `toggleOpLog`'s test isolates the global-config write (mirror for Task 6).
6. The `adding-config-entries` skill's exact settingDoc + hand-synced literals (Task 2).
