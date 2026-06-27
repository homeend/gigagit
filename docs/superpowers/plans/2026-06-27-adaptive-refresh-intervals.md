# Adaptive Refresh Intervals (Phase C) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make gigagit's background auto-refresh cost-aware and single-lane — each source's read is timed, its interval backs off from the measured average, too-slow sources drop to manual-only, and at most one background read runs at a time (FIFO, deduped by type), with a `⟳` status hint and Settings surfaces.

**Architecture:** Builds on Phase B's `internal/tui/refresh.go` scheduler. Pure decision functions (`meanDuration`, `effectiveInterval`, `dueItems`, `enqueueDue`) carry the logic and unit-test trivially with a fake `now`. Measurement is timed in `readSourceCmd`/`bgFetchCmd` and carried on `dataAvailableMsg`/`bgFetchDoneMsg`; a single-lane queue (`bgQueue`/`bgBusy`/`bgActiveItem`) drains one read per tick. Three new `[refresh]` config keys + one runtime writer + two Settings surfaces.

**Tech Stack:** Go 1.26, Bubble Tea (Elm-style value-receiver `Model`), `internal/config` TOML overlay, existing per-source registry (`internal/tui/source.go`).

## Global Constraints

- **Off by default.** The whole feature stays gated behind `[refresh] enabled`
  (Phase B master, default false). Adaptation is gated behind
  `[refresh] disable_adaptive` (default false ⇒ adaptive ON when refresh is on).
- **`bgBusy` is the SOLE source of truth for lane occupancy.** `bgActiveItem` is
  meaningful ONLY when `bgBusy` is true. Never treat a zero `refreshItem{}` as
  "none" — `refreshItem{}` equals `{source: srcStatus}`, a real item.
- **Manual `r` stays fully parallel and silent-free.** Only the background lane
  is single-file. Manual reads set `srcLoading` (⏳); background reads never do.
- **A silent (auto) read must never write `statusMsg`** (Phase B rule) — the
  error branch stays gated on `msg.manual`.
- **Defaults applied at read time:** `max_read_seconds` and `backoff_factor`
  default to **10** when the config value is `0` (zero-is-unset), mirroring the
  other "0 = unset → default" fields.
- **Back-off is computed in `time.Duration` then rounded up** (`math.Ceil` on
  seconds), never integer-second truncation.
- TUI never imports `internal/git` (archtest-guarded); reach git via `domain`.
- Tests use a real `git` in `t.TempDir()` or table-driven pure tests; follow TDD.

---

## File structure

| File | Change |
|------|--------|
| `internal/config/config.go` | 3 new `RefreshConfig` fields + overlay |
| `internal/config/write.go` | `SetGlobalRefreshDisableAdaptive` writer |
| `internal/config/template.go` | 3 new `settingDoc` rows |
| `internal/tui/refresh.go` | pure logic (`meanDuration`/`effectiveInterval`/`enqueueDue`), `dueItems` rewrite, single-lane `refreshTick`, `bgFetchDoneMsg` rewrite, measurement in `bgFetchCmd` |
| `internal/tui/source.go` | `dur` field on `dataAvailableMsg`; time the read in `readSourceCmd` |
| `internal/tui/model.go` | new `Model` fields + `New()` init; lane-clear + ring-append in `dataAvailableMsg` handler |
| `internal/tui/op.go` | `startOp` clears lane + queue |
| `internal/tui/view.go` | `⟳ <source>…` status-line hint |
| `internal/tui/settings_popup.go` | "Adaptive intervals" toggle + "Refresh rates" viewer |
| docs + memory | CHANGELOG / README / CLAUDE.md / memory |

---

## Task 1: Config — three new `[refresh]` keys, overlay, writer, template

**Files:**
- Modify: `internal/config/config.go` (`RefreshConfig` ~64-74, `overlayRefresh` ~219-247)
- Modify: `internal/config/write.go` (after `SetGlobalRefreshEnabled` ~29)
- Modify: `internal/config/template.go` (`settingDocs` after line 58)
- Test: `internal/config/config_test.go`, `internal/config/write_test.go`

**Interfaces:**
- Produces: `RefreshConfig.DisableAdaptive bool`, `RefreshConfig.MaxReadSeconds int`, `RefreshConfig.BackoffFactor int`; `config.SetGlobalRefreshDisableAdaptive(path string, disable bool) error`.

- [ ] **Step 1: Write the failing overlay test**

In `internal/config/config_test.go` add:

```go
func TestOverlayRefreshAdaptiveFields(t *testing.T) {
	dst := RefreshConfig{}
	// Inverted polarity: a true in a higher layer overlays; false leaves dst.
	overlayRefresh(&dst, RefreshConfig{DisableAdaptive: true, MaxReadSeconds: 15, BackoffFactor: 8})
	if !dst.DisableAdaptive {
		t.Fatal("DisableAdaptive true should overlay")
	}
	if dst.MaxReadSeconds != 15 || dst.BackoffFactor != 8 {
		t.Fatalf("ints should overlay: got %d/%d", dst.MaxReadSeconds, dst.BackoffFactor)
	}
	// Zero-is-unset: a zero int in a higher layer must NOT reset a set value.
	overlayRefresh(&dst, RefreshConfig{MaxReadSeconds: 0, BackoffFactor: 0})
	if dst.MaxReadSeconds != 15 || dst.BackoffFactor != 8 {
		t.Fatalf("zero ints must not reset: got %d/%d", dst.MaxReadSeconds, dst.BackoffFactor)
	}
}
```

- [ ] **Step 2: Run it — expect FAIL** (`go test ./internal/config/ -run TestOverlayRefreshAdaptiveFields`) with "unknown field DisableAdaptive".

- [ ] **Step 3: Add the fields** to `RefreshConfig` in `config.go` (after `Fetch int`):

```go
	Fetch     int  `toml:"fetch"` // seconds between background `git fetch`; 0 = off

	// DisableAdaptive turns OFF the adaptive interval system (Phase C). Inverted
	// polarity: default false ⇒ adaptation ON when refresh is enabled; a true in
	// a higher layer overlays (matching DisableSlowOpConfirm). When true, each
	// source auto-refreshes at its fixed configured interval.
	DisableAdaptive bool `toml:"disable_adaptive"`
	// MaxReadSeconds is the cutoff: a source whose average read exceeds it drops
	// out of auto-refresh (manual r only). 0 = unset → default 10.
	MaxReadSeconds int `toml:"max_read_seconds"`
	// BackoffFactor multiplies a source's average read duration to derive its
	// effective interval (floored by the configured value). 0 = unset → default 10.
	BackoffFactor int `toml:"backoff_factor"`
```

- [ ] **Step 4: Extend `overlayRefresh`** in `config.go` (append inside the function, after the `Fetch` block):

```go
	if src.DisableAdaptive {
		dst.DisableAdaptive = true
	}
	if src.MaxReadSeconds > 0 {
		dst.MaxReadSeconds = src.MaxReadSeconds
	}
	if src.BackoffFactor > 0 {
		dst.BackoffFactor = src.BackoffFactor
	}
```

- [ ] **Step 5: Run it — expect PASS** (`go test ./internal/config/ -run TestOverlayRefreshAdaptiveFields`).

- [ ] **Step 6: Write the failing writer test** in `internal/config/write_test.go`:

```go
func TestSetGlobalRefreshDisableAdaptiveRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[refresh]\nenabled = true\nstatus = 30\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetGlobalRefreshDisableAdaptive(path, true); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Refresh.DisableAdaptive {
		t.Fatal("disable_adaptive should be true after write")
	}
	// Other keys survive the line edit.
	if !cfg.Refresh.Enabled || cfg.Refresh.Status != 30 {
		t.Fatalf("unrelated keys clobbered: enabled=%v status=%d", cfg.Refresh.Enabled, cfg.Refresh.Status)
	}
	// Flip back to false (a default-on toggle must write both values).
	if err := SetGlobalRefreshDisableAdaptive(path, false); err != nil {
		t.Fatal(err)
	}
	cfg2, _ := Load(path, "")
	if cfg2.Refresh.DisableAdaptive {
		t.Fatal("disable_adaptive should be false after second write")
	}
}
```

(Ensure `os`, `path/filepath`, `testing` are imported in `write_test.go`.)

- [ ] **Step 7: Run it — expect FAIL** (undefined `SetGlobalRefreshDisableAdaptive`).

- [ ] **Step 8: Add the writer** in `write.go` after `SetGlobalRefreshEnabled`:

```go
// SetGlobalRefreshDisableAdaptive persists `[refresh] disable_adaptive` to the
// global config file (preserving comments), backing the Settings "Adaptive
// intervals" toggle — the third runtime config writer (see
// SetGlobalDebugLogOperations / SetGlobalRefreshEnabled). Unlike the other two
// (default-off positives), this backs a default-ON toggle, so it writes the
// explicit true/false either way.
func SetGlobalRefreshDisableAdaptive(path string, disable bool) error {
	return setScalarLine(path, "refresh", "disable_adaptive", strconv.FormatBool(disable))
}
```

- [ ] **Step 9: Run it — expect PASS** (`go test ./internal/config/ -run TestSetGlobalRefreshDisableAdaptive`).

- [ ] **Step 10: Add the three `settingDoc` rows** in `template.go` after the `fetch` row (line 58):

```go
	{"refresh", "disable_adaptive", false, "turn OFF adaptive intervals (Phase C); default false = adaptive on, each source's interval auto-tunes from its measured read time"},
	{"refresh", "max_read_seconds", 10, "a source whose average read exceeds this many seconds drops out of auto-refresh (manual r only)"},
	{"refresh", "backoff_factor", 10, "effective interval = max(configured, this × average read seconds)"},
```

- [ ] **Step 11: Run the config suite — expect PASS**, including `TestSettingDocsCoverAllFields` (the guard that fails until every field has a doc):

Run: `go test ./internal/config/...`
Expected: `ok`

- [ ] **Step 12: Commit**

```bash
git add internal/config/
git commit -m "feat(config): [refresh] disable_adaptive/max_read_seconds/backoff_factor + writer"
```

---

## Task 2: Pure decision logic in `refresh.go`

These are standalone, side-effect-free functions. They do NOT touch `dueItems`,
`refreshTick`, or `Model` yet (those change in Task 4), so the package keeps
compiling.

**Files:**
- Modify: `internal/tui/refresh.go` (add functions + the `math` import)
- Test: `internal/tui/refresh_adaptive_test.go` (new)

**Interfaces:**
- Consumes: `config.RefreshConfig` (Task 1 fields), existing `refreshItem`, `refreshIntervalFor`.
- Produces:
  - `meanDuration(samples []time.Duration) time.Duration`
  - `type intervalState int` with `stateOff`, `stateFixed`, `stateAdaptive`, `stateAdaptiveFloor`, `stateDisabled`
  - `effectiveInterval(cfg config.RefreshConfig, it refreshItem, avg time.Duration, haveSample bool) (int, intervalState)`
  - `enqueueDue(queue []refreshItem, active refreshItem, busy bool, due []refreshItem) []refreshItem`
  - consts `defaultMaxReadSeconds = 10`, `defaultBackoffFactor = 10`, `maxDurationSamples = 10`

- [ ] **Step 1: Write the failing tests** in a new file `internal/tui/refresh_adaptive_test.go`:

```go
package tui

import (
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
)

func TestMeanDuration(t *testing.T) {
	if got := meanDuration(nil); got != 0 {
		t.Fatalf("empty → 0, got %v", got)
	}
	got := meanDuration([]time.Duration{2 * time.Second, 4 * time.Second})
	if got != 3*time.Second {
		t.Fatalf("mean(2s,4s) = 3s, got %v", got)
	}
}

func TestEffectiveInterval(t *testing.T) {
	st := refreshItem{source: srcStatus}
	base := config.RefreshConfig{Enabled: true, Status: 10} // adaptive on (DisableAdaptive false)

	// cfg interval 0 → off.
	if secs, state := effectiveInterval(config.RefreshConfig{Enabled: true}, st, 0, false); state != stateOff || secs != 0 {
		t.Fatalf("interval 0 → off, got %d/%v", secs, state)
	}
	// adaptive off → fixed at configured.
	fixed := base
	fixed.DisableAdaptive = true
	if secs, state := effectiveInterval(fixed, st, 4*time.Second, true); state != stateFixed || secs != 10 {
		t.Fatalf("adaptive off → fixed 10, got %d/%v", secs, state)
	}
	// adaptive on, no sample → floor (run at configured pending measurement).
	if secs, state := effectiveInterval(base, st, 0, false); state != stateAdaptiveFloor || secs != 10 {
		t.Fatalf("no sample → floor 10, got %d/%v", secs, state)
	}
	// adaptive on, cheap read (0.3s) → floor wins (max(10, ceil(3s))=10).
	if secs, state := effectiveInterval(base, st, 300*time.Millisecond, true); state != stateAdaptiveFloor || secs != 10 {
		t.Fatalf("cheap → floor 10, got %d/%v", secs, state)
	}
	// adaptive on, 4.1s read → backoff 41 (max(10, ceil(41.0))).
	if secs, state := effectiveInterval(base, st, 4100*time.Millisecond, true); state != stateAdaptive || secs != 41 {
		t.Fatalf("4.1s → adaptive 41, got %d/%v", secs, state)
	}
	// adaptive on, 12s read > cutoff(10) → disabled.
	if secs, state := effectiveInterval(base, st, 12*time.Second, true); state != stateDisabled || secs != 0 {
		t.Fatalf("12s → disabled, got %d/%v", secs, state)
	}
	// custom cutoff/factor honored.
	custom := config.RefreshConfig{Enabled: true, Status: 5, MaxReadSeconds: 20, BackoffFactor: 3}
	if secs, state := effectiveInterval(custom, st, 12*time.Second, true); state != stateAdaptive || secs != 36 {
		t.Fatalf("custom factor 3×12s=36, got %d/%v", secs, state)
	}
}

func TestEnqueueDueDedup(t *testing.T) {
	a := refreshItem{source: srcStatus}
	b := refreshItem{source: srcBranches}

	// Empty queue, not busy → both appended in FIFO order.
	q := enqueueDue(nil, refreshItem{}, false, []refreshItem{a, b})
	if len(q) != 2 || q[0] != a || q[1] != b {
		t.Fatalf("want [a b], got %v", q)
	}
	// Re-enqueue same types → no duplicates.
	q = enqueueDue(q, refreshItem{}, false, []refreshItem{a, b})
	if len(q) != 2 {
		t.Fatalf("dedup failed, got %v", q)
	}
	// Active (busy) type is not re-enqueued even though absent from the queue.
	q2 := enqueueDue(nil, a, true, []refreshItem{a, b})
	if len(q2) != 1 || q2[0] != b {
		t.Fatalf("active type must be skipped, got %v", q2)
	}
}
```

- [ ] **Step 2: Run them — expect FAIL** (`go test ./internal/tui/ -run 'TestMeanDuration|TestEffectiveInterval|TestEnqueueDueDedup'`) with undefined identifiers.

- [ ] **Step 3: Implement** in `refresh.go`. Add `"math"` to the import block, then:

```go
const (
	defaultMaxReadSeconds = 10
	defaultBackoffFactor  = 10
	maxDurationSamples    = 10 // ring length per item for the rolling average
)

// intervalState classifies how an item's effective interval was derived (for the
// "Refresh rates" viewer and the scheduler's skip decision).
type intervalState int

const (
	stateOff           intervalState = iota // configured interval 0 → never auto-refresh
	stateFixed                              // adaptive off → configured interval verbatim
	stateAdaptive                           // backoff_factor × avg won (interval lengthened)
	stateAdaptiveFloor                      // configured floor won (cheap read, or not yet measured)
	stateDisabled                           // avg > cutoff → auto-refresh disabled (manual only)
)

// meanDuration is the arithmetic mean of samples (0 for an empty slice).
func meanDuration(samples []time.Duration) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	var sum time.Duration
	for _, s := range samples {
		sum += s
	}
	return sum / time.Duration(len(samples))
}

// effectiveInterval returns an item's effective interval in seconds and the
// state that produced it. secs is meaningful only for the fixed/adaptive states;
// stateOff and stateDisabled return 0 (the item does not auto-refresh).
func effectiveInterval(cfg config.RefreshConfig, it refreshItem, avg time.Duration, haveSample bool) (int, intervalState) {
	base := refreshIntervalFor(cfg, it)
	if base <= 0 {
		return 0, stateOff
	}
	if cfg.DisableAdaptive {
		return base, stateFixed
	}
	if !haveSample {
		return base, stateAdaptiveFloor // adaptive on, not yet measured → run at floor
	}
	cutoff := cfg.MaxReadSeconds
	if cutoff <= 0 {
		cutoff = defaultMaxReadSeconds
	}
	if avg > time.Duration(cutoff)*time.Second {
		return 0, stateDisabled
	}
	factor := cfg.BackoffFactor
	if factor <= 0 {
		factor = defaultBackoffFactor
	}
	backoff := int(math.Ceil((time.Duration(factor) * avg).Seconds()))
	if backoff <= base {
		return base, stateAdaptiveFloor
	}
	return backoff, stateAdaptive
}

// enqueueDue appends each due item that is neither already queued nor the
// currently-running item (when busy) — the dedup-by-type gate. FIFO order.
func enqueueDue(queue []refreshItem, active refreshItem, busy bool, due []refreshItem) []refreshItem {
	inQueue := func(it refreshItem) bool {
		for _, q := range queue {
			if q == it {
				return true
			}
		}
		return false
	}
	for _, d := range due {
		if busy && d == active {
			continue
		}
		if inQueue(d) {
			continue
		}
		queue = append(queue, d)
	}
	return queue
}
```

- [ ] **Step 4: Run them — expect PASS** (`go test ./internal/tui/ -run 'TestMeanDuration|TestEffectiveInterval|TestEnqueueDueDedup'`).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/refresh.go internal/tui/refresh_adaptive_test.go
git commit -m "feat(tui): pure adaptive-interval logic (mean/effectiveInterval/enqueueDue)"
```

---

## Task 3: Measurement plumbing + new `Model` fields

Adds the per-item duration ring and the single-lane fields, times every read,
and records durations. NO scheduling change yet — `dueItems`/`refreshTick` are
untouched (rewritten in Task 4), so behavior is unchanged except that durations
are now recorded.

**Files:**
- Modify: `internal/tui/model.go` (`Model` struct ~100-106; `New()` ~175-189; `dataAvailableMsg` handler ~535-559; `bgFetchDoneMsg` handler ~1433-1459)
- Modify: `internal/tui/source.go` (`dataAvailableMsg` ~52-58; `readSourceCmd` ~126-181)
- Modify: `internal/tui/refresh.go` (`bgFetchCmd` ~113-128, `bgFetchDoneMsg` ~107)
- Test: `internal/tui/refresh_adaptive_test.go`

**Interfaces:**
- Produces: `Model.refreshDur map[refreshItem][]time.Duration`, `Model.bgQueue []refreshItem`, `Model.bgBusy bool`, `Model.bgActiveItem refreshItem`; `dataAvailableMsg.dur time.Duration`; `bgFetchDoneMsg.dur time.Duration`; `Model.recordDuration(it refreshItem, d time.Duration)` helper.

- [ ] **Step 1: Write the failing test** in `refresh_adaptive_test.go`:

```go
func TestRecordDurationCapsAtTen(t *testing.T) {
	m := Model{refreshDur: map[refreshItem][]time.Duration{}}
	it := refreshItem{source: srcStatus}
	for i := 1; i <= 13; i++ {
		m = m.recordDuration(it, time.Duration(i)*time.Second)
	}
	got := m.refreshDur[it]
	if len(got) != maxDurationSamples {
		t.Fatalf("ring should cap at %d, got %d", maxDurationSamples, len(got))
	}
	// Oldest dropped: the ring holds samples 4s..13s, mean = 8.5s.
	if mean := meanDuration(got); mean != 8500*time.Millisecond {
		t.Fatalf("want mean 8.5s of last 10, got %v", mean)
	}
}

func TestDataAvailableRecordsDuration(t *testing.T) {
	m := newTestModel(t)
	m.bgActiveItem = refreshItem{} // lane idle
	it := refreshItem{source: srcTags}
	gen := m.srcGen[srcTags]
	msg := dataAvailableMsg{source: srcTags, gen: gen, value: []model.Tag(nil), dur: 2 * time.Second}
	nm, _ := m.Update(msg)
	got := nm.(Model).refreshDur[it]
	if len(got) != 1 || got[0] != 2*time.Second {
		t.Fatalf("status handler should record dur, got %v", got)
	}
}
```

(Add `"github.com/homeend/gigagit/internal/model"` to the test imports.)

- [ ] **Step 2: Run them — expect FAIL** (undefined `recordDuration`, `refreshDur`, `bgActiveItem`, `dur`).

- [ ] **Step 3: Add the `Model` fields** in `model.go` after `refreshLastRun` (line 106):

```go
	refreshLastRun      map[refreshItem]time.Time // last time each scheduled item fired (background scheduler)
	refreshDur          map[refreshItem][]time.Duration // rolling ring (≤10) of measured read durations per item (Phase C)
	bgQueue             []refreshItem             // FIFO of pending background reads; one drains per tick
	bgBusy              bool                      // a background read is in flight (sole lane-occupancy truth)
	bgActiveItem        refreshItem               // the running background item — meaningful ONLY when bgBusy
```

- [ ] **Step 4: Init the map in `New()`** (after `refreshLastRun: ...`, line 186):

```go
		refreshLastRun: map[refreshItem]time.Time{},
		refreshDur:     map[refreshItem][]time.Duration{},
```

- [ ] **Step 5: Add the `recordDuration` helper** in `refresh.go`:

```go
// recordDuration appends d to it's rolling ring, dropping the oldest beyond
// maxDurationSamples. Lazy-inits the map so a literal-built test Model is safe.
func (m Model) recordDuration(it refreshItem, d time.Duration) Model {
	if m.refreshDur == nil {
		m.refreshDur = map[refreshItem][]time.Duration{}
	}
	ring := append(m.refreshDur[it], d)
	if len(ring) > maxDurationSamples {
		ring = ring[len(ring)-maxDurationSamples:]
	}
	m.refreshDur[it] = ring
	return m
}
```

- [ ] **Step 6: Add `dur` to `dataAvailableMsg`** in `source.go` (struct ~52):

```go
type dataAvailableMsg struct {
	source sourceKey
	gen    int
	value  any
	manual bool
	dur    time.Duration // wall-clock of the domain read (Phase C measurement)
	err    error
}
```

- [ ] **Step 7: Time the read in `readSourceCmd`** in `source.go`. Wrap the closure body so `out.dur` is set on every path:

Change the closure (line 131) from `return func() tea.Msg {` body to capture start and stamp dur via a deferred-free explicit set. Replace `out := dataAvailableMsg{source: s, gen: gen, manual: manual}` and the final `return out` so:

```go
	return func() tea.Msg {
		start := time.Now()
		out := dataAvailableMsg{source: s, gen: gen, manual: manual}
		switch s {
		// ... unchanged cases ...
		}
		out.dur = time.Since(start)
		return out
	}
```

Note: the `srcStatus`/`srcWorktrees` cases have early `return out` on error — change those two `return out` to `out.dur = time.Since(start); return out`. (Grep the function for `return out` and ensure every one stamps `out.dur` first.)

- [ ] **Step 8: Record duration in the `dataAvailableMsg` handler** in `model.go`. After the existing `m.loading = m.anySourceLoading()` line (~549) and the error early-return block, record the duration on success. Insert immediately BEFORE the `switch msg.source {` (line 560):

```go
		// Record the measured read cost for the adaptive scheduler (success only;
		// a failed/partial read is not a representative duration).
		m = m.recordDuration(refreshItem{source: msg.source}, msg.dur)
```

(Leave the stale-gen early-return at the top as-is for now; Task 4 adds the lane-clear above it.)

- [ ] **Step 9: Add `dur` to `bgFetchDoneMsg` + time `bgFetchCmd`** in `refresh.go`:

```go
type bgFetchDoneMsg struct {
	dur time.Duration
	err error
}
```

In `bgFetchCmd`, wrap the execute with timing:

```go
	return func() tea.Msg {
		start := time.Now()
		events := make(chan engine.Event, 8)
		go func() {
			for range events {
			}
		}()
		_, err := svc.Execute(ctx, engine.Fetch{}, events, engine.MapDecider{})
		close(events)
		return bgFetchDoneMsg{dur: time.Since(start), err: err}
	}
```

- [ ] **Step 10: Record fetch duration in the `bgFetchDoneMsg` handler** in `model.go`. At the very top of the `case bgFetchDoneMsg:` block (before the existing guards), add:

```go
		if msg.err == nil {
			m = m.recordDuration(fetchItem, msg.dur)
		}
```

(The rest of the handler stays as-is for now; Task 4 rewrites it.)

- [ ] **Step 11: Run the tests — expect PASS** (`go test ./internal/tui/ -run 'TestRecordDuration|TestDataAvailableRecordsDuration'`).

- [ ] **Step 12: Run the full tui package — expect PASS** (`go test ./internal/tui/`). Phase B tests still pass (scheduling unchanged).

- [ ] **Step 13: Commit**

```bash
git add internal/tui/
git commit -m "feat(tui): measure read durations + single-lane Model fields (no scheduling change)"
```

---

## Task 4: Adaptive single-lane scheduler (the whole lane lifecycle)

This is the cohesive risk task: `dueItems` rewrite, single-lane `refreshTick`,
the lane-clear in BOTH `dataAvailableMsg` and `bgFetchDoneMsg`, the `startOp`
reset, and the `bgFetchDoneMsg` "mark remotes due" rewrite — plus migrating the
invalidated Phase B tests. Lane SET and lane CLEAR must land together so the
stranding bug can't slip a per-task gate.

**Files:**
- Modify: `internal/tui/refresh.go` (`dueItems` ~133-149, `refreshTick` ~73-102)
- Modify: `internal/tui/model.go` (`dataAvailableMsg` handler top ~535; `bgFetchDoneMsg` handler ~1433-1459)
- Modify: `internal/tui/op.go` (`startOp` ~184-187)
- Migrate: `internal/tui/refresh_test.go` (Phase B tests with changed signatures)
- Test: `internal/tui/refresh_adaptive_test.go`

**Interfaces:**
- Consumes: Task 2 (`dueItems` helpers, `enqueueDue`, `effectiveInterval`), Task 3 (`refreshDur`, `bgBusy`, `bgActiveItem`, `bgQueue`).
- Produces: new `dueItems(now, lastRun, durs, cfg, suppressed)` signature; single-lane `refreshTick`.

- [ ] **Step 1: Rewrite `dueItems` signature + body** in `refresh.go`:

```go
// dueItems returns the items whose effective interval has elapsed this tick.
// off/disabled items are excluded. Pure: durs is the per-item duration ring.
func dueItems(now time.Time, lastRun map[refreshItem]time.Time, durs map[refreshItem][]time.Duration, cfg config.RefreshConfig, suppressed bool) []refreshItem {
	if !cfg.Enabled || suppressed {
		return nil
	}
	var due []refreshItem
	for _, it := range scheduledItems {
		avg := meanDuration(durs[it])
		secs, state := effectiveInterval(cfg, it, avg, len(durs[it]) > 0)
		if state == stateOff || state == stateDisabled {
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

- [ ] **Step 2: Rewrite `refreshTick`** in `refresh.go` as the single-lane drainer:

```go
// refreshTick is called from the heartbeat. It enqueues newly-due items (deduped
// by type) and, when the single background lane is free, drains exactly one read
// under a shared cancellable bg context that a starting user op cancels.
func (m Model) refreshTick(now time.Time) (Model, tea.Cmd) {
	if m.refreshSuppressed() {
		return m, nil
	}
	due := dueItems(now, m.refreshLastRun, m.refreshDur, m.cfg.Refresh, false)
	m.bgQueue = enqueueDue(m.bgQueue, m.bgActiveItem, m.bgBusy, due)
	if m.bgBusy || len(m.bgQueue) == 0 {
		return m, nil
	}
	it := m.bgQueue[0]
	m.bgQueue = m.bgQueue[1:]
	// A source whose read is already in flight (e.g. a manual r) must not get a
	// second, superseding background read — that would strand the manual ⏳.
	// Drop it this tick; it re-enqueues next tick if still due (lastRun unchanged).
	if !it.isFetch && m.srcInflight[it.source] {
		return m, nil
	}
	if m.bgCancel == nil {
		m.bgCtx, m.bgCancel = context.WithCancel(context.Background())
	}
	m.bgBusy = true
	m.bgActiveItem = it
	m.refreshLastRun[it] = now
	if it.isFetch {
		return m, m.bgFetchCmd(m.bgCtx)
	}
	m.srcGen[it.source]++
	m.srcInflight[it.source] = true
	return m, m.readSourceCmd(m.bgCtx, it.source, false) // manual=false → silent
}
```

- [ ] **Step 3: Add the lane-clear at the TOP of the `dataAvailableMsg` handler** in `model.go`, BEFORE the stale-gen early-return (line 535-538). The clear must precede the stale-gen check because manual `r` bumps the gen, which would otherwise make the bg read's message early-return and strand the lane:

```go
	case dataAvailableMsg:
		// Free the background lane the moment its active read's message arrives —
		// BEFORE the stale-gen check below, because a manual r bumps srcGen and
		// would otherwise make this (now-stale) bg message early-return without
		// clearing bgBusy, deadlocking the lane until restart. Gated on bgBusy
		// (the sole occupancy truth) + a non-fetch, non-manual match.
		if m.bgBusy && !m.bgActiveItem.isFetch && !msg.manual && m.bgActiveItem.source == msg.source {
			m.bgBusy = false
		}
		if msg.gen != m.srcGen[msg.source] {
			return m, nil // superseded by a newer read of this source
		}
```

- [ ] **Step 4: Rewrite the `bgFetchDoneMsg` handler** in `model.go`. Replace the whole body (lines ~1433-1459) with the lane-clear + ring-record + "mark remotes due via the queue" form:

```go
	case bgFetchDoneMsg:
		// Free the lane if fetch was the active background item (fetch completes
		// via this message, not dataAvailableMsg).
		if m.bgBusy && m.bgActiveItem.isFetch {
			m.bgBusy = false
		}
		if msg.err != nil {
			return m, nil // silent: the domain failure seam already logged it
		}
		m = m.recordDuration(fetchItem, msg.dur)
		// A successful fetch updates remote-tracking refs, so refresh the Remotes
		// panel regardless of its configured interval — enqueued through the single
		// lane (deduped), drained on the next tick. Replaces Phase B's direct fire.
		m.bgQueue = enqueueDue(m.bgQueue, m.bgActiveItem, m.bgBusy, []refreshItem{{source: srcRemotes}})
		return m, nil
```

(The Step-10 ring-record from Task 3 is now folded into this rewrite — remove the duplicate added there; `recordDuration` appears once, here.)

- [ ] **Step 5: Update `startOp`** in `op.go` (lines ~184-187) to clear the whole lane on preempt:

```go
func (m Model) startOp(op engine.Operation) (Model, tea.Cmd) {
	if m.bgCancel != nil {
		m.bgCancel() // preempt in-flight background reads so the user's op gets the slot
		m.bgCancel = nil
	}
	// A user op preempts the entire background lane; still-due items re-enqueue on
	// the next post-op tick. bgActiveItem is left as-is (meaningless when !bgBusy).
	m.bgBusy = false
	m.bgQueue = nil
	// ... rest of startOp unchanged ...
```

- [ ] **Step 6: Migrate the invalidated Phase B tests** in `refresh_test.go`. Update the three `dueItems` call sites to the new 5-arg signature (add a `nil` durations map) and adjust `refreshTick` expectations:

In `TestDueItemsRespectsIntervalAndMaster` and `TestDueItemsFirstRunWhenUnseen`, change each `dueItems(now, last, cfg, suppressed)` to `dueItems(now, last, nil, cfg, suppressed)`. The assertions hold (no samples → floor → configured interval governs, exactly as before).

In `TestRefreshTickFiresSilentReadAndIsSuppressed`, the single-fire expectation now goes through the queue: after `refreshTick`, the status read is fired (lane busy). Keep the gen-bump + no-srcLoading assertions; they still hold. Add `m2.bgBusy` true and `m2.bgActiveItem.source == srcStatus`.

- [ ] **Step 7: Rewrite the two `bgFetchDone` tests** in `refresh_test.go` for the new "enqueue remotes" behavior:

Replace `TestBgFetchRefreshesRemotesOnSuccessSilently` so it asserts that after `bgFetchDoneMsg{}` (success), `srcRemotes` is enqueued (`bgQueue` contains `{source: srcRemotes}`) and `bgBusy` is false — rather than a remotes read firing immediately:

```go
func TestBgFetchEnqueuesRemotesOnSuccess(t *testing.T) {
	m := newTestModel(t)
	m.bgBusy = true
	m.bgActiveItem = fetchItem
	nm, _ := m.Update(bgFetchDoneMsg{dur: time.Second})
	mm := nm.(Model)
	if mm.bgBusy {
		t.Fatal("fetch done must free the lane")
	}
	found := false
	for _, it := range mm.bgQueue {
		if it == (refreshItem{source: srcRemotes}) {
			found = true
		}
	}
	if !found {
		t.Fatal("successful fetch must enqueue a remotes refresh")
	}
}
```

Replace `TestBgFetchDoneSkipsWhenRemotesInflight` with a guard that the drain (not the fetch-done handler) is what protects an in-flight manual remotes read — the refreshTick srcInflight guard:

```go
func TestRefreshTickSkipsSourceAlreadyInflight(t *testing.T) {
	m := newTestModel(t)
	m.cfg.Refresh = config.RefreshConfig{Enabled: true, Remotes: 1}
	m.loading = false
	m.srcInflight[srcRemotes] = true // a manual remotes read is already out
	m.bgQueue = []refreshItem{{source: srcRemotes}}
	_, cmd := m.refreshTick(time.Unix(3_000_000, 0))
	if cmd != nil {
		t.Fatal("must not fire a bg read for a source already in flight")
	}
}
```

- [ ] **Step 8: Write the lane lifecycle tests** in `refresh_adaptive_test.go`:

```go
func TestRefreshTickSingleLane(t *testing.T) {
	m := newTestModel(t)
	m.cfg.Refresh = config.RefreshConfig{Enabled: true, Status: 1, Branches: 1}
	m.loading = false
	t0 := time.Unix(4_000_000, 0)
	// First tick: both due, but only ONE fires (lane single-file); the other queues.
	m2, cmd := m.refreshTick(t0)
	if cmd == nil || !m2.bgBusy {
		t.Fatal("first tick should fire one read and mark the lane busy")
	}
	if len(m2.bgQueue) != 1 {
		t.Fatalf("the second due item should be queued, got %v", m2.bgQueue)
	}
	// Second tick while busy: nothing new fires.
	_, cmd2 := m2.refreshTick(t0)
	if cmd2 != nil {
		t.Fatal("must not fire a second read while the lane is busy")
	}
}

func TestManualRFreesLane(t *testing.T) {
	// Reproduces the stranding bug: a bg read of branches is in flight; manual r
	// bumps the gen; the bg message arrives stale and MUST still free the lane.
	m := newTestModel(t)
	m.bgBusy = true
	m.bgActiveItem = refreshItem{source: srcBranches}
	staleGen := m.srcGen[srcBranches]
	m.srcGen[srcBranches] = staleGen + 1 // simulate manual r having superseded it
	msg := dataAvailableMsg{source: srcBranches, gen: staleGen, manual: false, value: []model.Branch(nil)}
	nm, _ := m.Update(msg)
	if nm.(Model).bgBusy {
		t.Fatal("a stale bg read message must still free the lane (stranding bug)")
	}
}

func TestStartOpClearsLaneAndQueue(t *testing.T) {
	m := newTestModel(t)
	ctx, cancel := context.WithCancel(context.Background())
	m.bgCtx, m.bgCancel = ctx, cancel
	m.bgBusy = true
	m.bgActiveItem = refreshItem{source: srcTags}
	m.bgQueue = []refreshItem{{source: srcStatus}}
	m2, _ := m.startOp(engine.Fetch{})
	if m2.bgBusy || len(m2.bgQueue) != 0 || m2.bgCancel != nil {
		t.Fatalf("startOp must clear the lane + queue, got busy=%v queue=%v", m2.bgBusy, m2.bgQueue)
	}
}
```

(Ensure `context` and `engine` are imported in the test file.)

- [ ] **Step 9: Run the adaptive + migrated tests — expect PASS**

Run: `go test ./internal/tui/ -run 'Refresh|BgFetch|DueItems|ManualRFrees|StartOpClears|SingleLane'`
Expected: `ok`

- [ ] **Step 10: Run the full tui package + vet — expect PASS**

Run: `go test ./internal/tui/ && go vet ./internal/tui/`
Expected: `ok`

- [ ] **Step 11: Commit**

```bash
git add internal/tui/
git commit -m "feat(tui): single-lane adaptive scheduler (FIFO dedup queue, lane lifecycle)"
```

---

## Task 5: Status-line `⟳ <source>…` indicator

**Files:**
- Modify: `internal/tui/view.go` (`renderInterface` status assembly ~386-389)
- Modify: `internal/tui/refresh.go` (add a label helper)
- Test: `internal/tui/refresh_adaptive_test.go`

**Interfaces:**
- Consumes: `bgBusy`, `bgActiveItem`, `sourceNames` (source.go), `fetchItem`.
- Produces: `Model.bgRefreshHint() string`.

- [ ] **Step 1: Write the failing test**:

```go
func TestBgRefreshHint(t *testing.T) {
	m := newTestModel(t)
	if m.bgRefreshHint() != "" {
		t.Fatal("idle lane → no hint")
	}
	m.bgBusy = true
	m.bgActiveItem = refreshItem{source: srcBranches}
	if got := m.bgRefreshHint(); got != "⟳ branches…" {
		t.Fatalf("want '⟳ branches…', got %q", got)
	}
	m.bgActiveItem = fetchItem
	if got := m.bgRefreshHint(); got != "⟳ fetch…" {
		t.Fatalf("want '⟳ fetch…', got %q", got)
	}
}
```

- [ ] **Step 2: Run it — expect FAIL** (undefined `bgRefreshHint`).

- [ ] **Step 3: Implement the helper** in `refresh.go`:

```go
// bgRefreshHint is the unobtrusive status-line marker shown while the single
// background read runs (active-only, no countdown). Empty when the lane is idle.
func (m Model) bgRefreshHint() string {
	if !m.bgBusy {
		return ""
	}
	name := "fetch"
	if !m.bgActiveItem.isFetch {
		name = sourceNames[m.bgActiveItem.source]
	}
	return "⟳ " + name + "…"
}
```

- [ ] **Step 4: Wire it into the status line** in `view.go`. In the non-error branch of `renderInterface` (after `add(m.commitBranchHint())`, line 384), add the hint so it trails. Insert before `statusLine := strings.Join(parts, " · ")`:

```go
		add(m.bgRefreshHint())
```

- [ ] **Step 5: Run the test — expect PASS** (`go test ./internal/tui/ -run TestBgRefreshHint`).

- [ ] **Step 6: Run the full tui package — expect PASS** (`go test ./internal/tui/`).

- [ ] **Step 7: Commit**

```bash
git add internal/tui/refresh.go internal/tui/view.go internal/tui/refresh_adaptive_test.go
git commit -m "feat(tui): ⟳ active-only background-refresh status-line hint"
```

---

## Task 6: Settings — "Adaptive intervals" toggle

**Files:**
- Modify: `internal/tui/settings_popup.go` (consts ~30-40; `settingsMenuLabel` ~45-78; `update` enter switch ~203-219; add `toggleAdaptive`)
- Test: `internal/tui/settings_popup_test.go`

**Interfaces:**
- Consumes: `config.SetGlobalRefreshDisableAdaptive` (Task 1), `m.cfg.Refresh.DisableAdaptive`.
- Produces: `Model.toggleAdaptive() Model`; `settingsMenuAdaptive` const.

- [ ] **Step 1: Write the failing test** in `settings_popup_test.go`:

```go
func TestToggleAdaptiveFlipsInMemory(t *testing.T) {
	m := newTestModel(t)
	// Default: adaptive ON (DisableAdaptive false). Toggle → off (disable true).
	m2 := m.toggleAdaptive()
	if !m2.cfg.Refresh.DisableAdaptive {
		t.Fatal("first toggle should disable adaptation in memory")
	}
	m3 := m2.toggleAdaptive()
	if m3.cfg.Refresh.DisableAdaptive {
		t.Fatal("second toggle should re-enable adaptation")
	}
}
```

(The persistence call writes to the real global config path; the in-memory flip is what we assert, mirroring how Phase B's toggle is tested. If the existing tests stub the path, follow that pattern; otherwise the write is best-effort and the in-memory assertion stands.)

- [ ] **Step 2: Run it — expect FAIL** (undefined `toggleAdaptive`).

- [ ] **Step 3: Add the const + menu entry** in `settings_popup.go`:

```go
	settingsMenuAutoRefresh = "Auto-refresh"
	settingsMenuAdaptive    = "Adaptive intervals"
```

And add to the `settingsMenu` slice (after `settingsMenuAutoRefresh`):

```go
var settingsMenu = []string{settingsMenuAgents, settingsMenuIdentity, settingsMenuPrefixes, settingsMenuOpLog, settingsMenuErrors, settingsMenuAutoRefresh, settingsMenuAdaptive}
```

- [ ] **Step 4: Add the dynamic label** in `settingsMenuLabel` (before the final `return settingsMenu[i]`):

```go
	if settingsMenu[i] == settingsMenuAdaptive {
		if m.cfg.Refresh.DisableAdaptive {
			return settingsMenuAdaptive + ": off (fixed intervals)"
		}
		return settingsMenuAdaptive + ": on"
	}
```

- [ ] **Step 5: Add `toggleAdaptive`** after `toggleAutoRefresh` (~132):

```go
// toggleAdaptive flips the adaptive-interval system, persisting to the global
// config (mirrors toggleAutoRefresh). DisableAdaptive is the inverted key:
// false ⇒ adaptation on.
func (m Model) toggleAdaptive() Model {
	wantDisable := !m.cfg.Refresh.DisableAdaptive
	m.cfg.Refresh.DisableAdaptive = wantDisable // next tick honors it
	if err := config.SetGlobalRefreshDisableAdaptive(config.DefaultGlobalPath(), wantDisable); err != nil {
		m.statusMsg = "adaptive intervals toggled but not saved: " + err.Error()
		return m
	}
	if wantDisable {
		m.statusMsg = "adaptive intervals off — using fixed [refresh] intervals"
	} else {
		m.statusMsg = "adaptive intervals on — tuning from measured read times"
	}
	return m
}
```

- [ ] **Step 6: Wire the enter case** in `update` (in the `switch settingsMenu[p.menuSel]` block, after the `settingsMenuAutoRefresh` case):

```go
			case settingsMenuAdaptive:
				return m.toggleAdaptive(), nil // stays open so the state flip is visible
```

- [ ] **Step 7: Run the test — expect PASS** (`go test ./internal/tui/ -run TestToggleAdaptive`).

- [ ] **Step 8: Run the full tui package — expect PASS** (`go test ./internal/tui/`).

- [ ] **Step 9: Commit**

```bash
git add internal/tui/settings_popup.go internal/tui/settings_popup_test.go
git commit -m "feat(tui): Settings 'Adaptive intervals' toggle"
```

---

## Task 7: Settings — "Refresh rates" viewer

A read-only screen modeled on the existing `errorsView` screen: a `ratesView`
bool flag, a menu entry, navigation, and a row per scheduled item.

**Files:**
- Modify: `internal/tui/settings_popup.go` (`settingsPopup` struct ~19-28; consts/menu; `update` esc + enter + navigation; `box` render)
- Modify: `internal/tui/refresh.go` (add `refreshRateRow` formatting helper)
- Test: `internal/tui/refresh_adaptive_test.go`

**Interfaces:**
- Consumes: `effectiveInterval`, `meanDuration`, `scheduledItems`, `sourceNames`, `m.refreshDur`, `m.cfg.Refresh`.
- Produces: `Model.refreshRateRows() []string`; `settingsPopup.ratesView bool`; `settingsMenuRates` const.

- [ ] **Step 1: Write the failing test** for the pure row builder:

```go
func TestRefreshRateRows(t *testing.T) {
	m := newTestModel(t)
	m.cfg.Refresh = config.RefreshConfig{Enabled: true, Status: 10, Remotes: 30}
	// status has samples averaging 4s → adaptive 40s; remotes none → floor.
	it := refreshItem{source: srcStatus}
	m.refreshDur[it] = []time.Duration{4 * time.Second, 4 * time.Second}
	rows := m.refreshRateRows()
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "status") || !strings.Contains(joined, "40s") {
		t.Fatalf("status row should show adaptive 40s, got:\n%s", joined)
	}
	if !strings.Contains(joined, "remotes") {
		t.Fatalf("remotes row missing, got:\n%s", joined)
	}
}
```

(Add `"strings"` to the test imports if absent.)

- [ ] **Step 2: Run it — expect FAIL** (undefined `refreshRateRows`).

- [ ] **Step 3: Implement the row builder** in `refresh.go`:

```go
// stateLabel renders an intervalState for the Refresh rates viewer.
func stateLabel(s intervalState) string {
	switch s {
	case stateOff:
		return "off"
	case stateFixed:
		return "fixed"
	case stateAdaptive:
		return "adaptive"
	case stateAdaptiveFloor:
		return "adaptive (floor)"
	case stateDisabled:
		return "disabled (too slow)"
	}
	return ""
}

// refreshRateRows formats one line per scheduled item for the Settings viewer:
// name · configured · avg (n) · effective · state.
func (m Model) refreshRateRows() []string {
	rows := make([]string, 0, len(scheduledItems))
	for _, it := range scheduledItems {
		name := "fetch"
		if !it.isFetch {
			name = sourceNames[it.source]
		}
		samples := m.refreshDur[it]
		avg := meanDuration(samples)
		secs, state := effectiveInterval(m.cfg.Refresh, it, avg, len(samples) > 0)
		cfgSecs := refreshIntervalFor(m.cfg.Refresh, it)
		cfgStr := "off"
		if cfgSecs > 0 {
			cfgStr = fmt.Sprintf("%ds", cfgSecs)
		}
		avgStr := "—"
		if len(samples) > 0 {
			avgStr = fmt.Sprintf("%.1fs (%d)", avg.Seconds(), len(samples))
		}
		effStr := "—"
		if state == stateFixed || state == stateAdaptive || state == stateAdaptiveFloor {
			effStr = fmt.Sprintf("%ds", secs)
		}
		rows = append(rows, fmt.Sprintf("%-10s  cfg %-5s  avg %-12s  eff %-5s  %s", name, cfgStr, avgStr, effStr, stateLabel(state)))
	}
	return rows
}
```

(Add `"fmt"` to `refresh.go`'s imports if not present.)

- [ ] **Step 4: Run it — expect PASS** (`go test ./internal/tui/ -run TestRefreshRateRows`).

- [ ] **Step 5: Add the screen flag + const + menu entry** in `settings_popup.go`:

Struct field (after `errorsView bool`):

```go
	ratesView  bool // true = refresh-rates viewer screen
```

Const + menu (after `settingsMenuAdaptive`):

```go
	settingsMenuRates = "Refresh rates"
```

```go
var settingsMenu = []string{settingsMenuAgents, settingsMenuIdentity, settingsMenuPrefixes, settingsMenuOpLog, settingsMenuErrors, settingsMenuAutoRefresh, settingsMenuAdaptive, settingsMenuRates}
```

- [ ] **Step 6: Handle esc, enter, and navigation** in `update`:

In the esc handler (after the `p.errorsView` esc block ~161-164), add:

```go
		if p.ratesView {
			p.ratesView = false
			return m, nil
		}
```

In the menu enter switch (after the `settingsMenuAdaptive` case), add:

```go
			case settingsMenuRates:
				p.ratesView = true
				p.sel = 0
				p.hscroll = 0
				return m, nil
```

Guard the top-level menu navigation so the rates screen isn't treated as the menu: change the condition `if !p.picker && !p.errorsView {` (~190) to:

```go
	if !p.picker && !p.errorsView && !p.ratesView {
```

(The rates viewer is read-only — no row navigation needed beyond esc; the static-rows render shows them all. If rows exceed the popup, the existing `z`/scroll keys handled at the top of `update` apply.)

- [ ] **Step 7: Render the screen** in `box`. Add a branch BEFORE the `else if !p.picker` branch (i.e. as a sibling of the `if p.errorsView` block ~290), and widen like the errors view. At the top of `box`, extend the wide-width condition:

```go
	if p.errorsView || p.ratesView {
		inner = popupWideInnerWidth(w)
	}
```

Then add the render branch (after the errorsView block closes, before `} else if !p.picker {`):

```go
	} else if p.ratesView {
		b.WriteString("Refresh rates\n\n")
		mode := "adaptive"
		if m.cfg.Refresh.DisableAdaptive {
			mode = "fixed (adaptive off)"
		}
		if !m.cfg.Refresh.Enabled {
			mode = "auto-refresh off"
		}
		b.WriteString("  mode: " + mode + "\n\n")
		for _, row := range m.refreshRateRows() {
			b.WriteString("  " + row + "\n")
		}
		b.WriteString("\n[esc] back")
```

- [ ] **Step 8: Run the full tui package — expect PASS** (`go test ./internal/tui/`).

- [ ] **Step 9: Commit**

```bash
git add internal/tui/settings_popup.go internal/tui/refresh.go internal/tui/refresh_adaptive_test.go
git commit -m "feat(tui): Settings 'Refresh rates' viewer (configured/avg/effective/state)"
```

---

## Task 8: Documentation + memory

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`
- Modify: memory files (after merge, in the controller's main checkout)

- [ ] **Step 1: CHANGELOG.md** — add a Phase C entry under the appropriate heading:

```markdown
- **Adaptive refresh intervals (Phase C).** Background auto-refresh is now
  cost-aware: each source's read is timed (mean of the last 10), its interval
  backs off to `max(configured, backoff_factor × avg)`, and a source whose
  average exceeds `max_read_seconds` drops to manual-only. Background reads run
  one at a time (FIFO, deduped by type); manual `r` stays parallel. New
  `[refresh]` keys `disable_adaptive`/`max_read_seconds`/`backoff_factor`, a
  `⟳` status hint, and Settings "Adaptive intervals" toggle + "Refresh rates"
  viewer. Off by default.
```

- [ ] **Step 2: README.md** — extend the `[refresh]` config documentation with the three new keys (defaults: `disable_adaptive = false`, `max_read_seconds = 10`, `backoff_factor = 10`) and mention the Settings "Adaptive intervals" / "Refresh rates" surfaces.

- [ ] **Step 3: CLAUDE.md** — update two package-map rows:
  - `tui`: note `refresh.go` now runs a single-lane adaptive scheduler (FIFO dedup queue, per-item duration ring, `effectiveInterval` back-off, `⟳` hint, "Refresh rates" viewer).
  - `config`: note the `[refresh]` adaptive keys and `SetGlobalRefreshDisableAdaptive` as the **third** runtime config writer.

- [ ] **Step 4: Commit the docs**

```bash
git add CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: Phase C adaptive refresh intervals (changelog, README, CLAUDE)"
```

- [ ] **Step 5: Memory (after merge)** — create `adaptive-refresh-intervals-feature.md`, update `data-source-registry-feature.md` / `background-auto-refresh-feature.md` cross-links and the `MEMORY.md` index. (Done in the main checkout by the controller, not a task subagent.)

---

## Self-review notes (author)

- **Spec coverage:** §2 rule → Tasks 2+4; §3 single-lane FIFO dedup → Task 4 (+ Task 2 `enqueueDue`); §4 measurement → Task 3; §5 indicator → Task 5; §6 config → Task 1; two switches → Tasks 1+6; Settings viewer → Task 7; testing → folded per task; docs → Task 8. No gap.
- **Advisor landmines:** #1 lane-strand → Task 4 Step 3 + `TestManualRFreesLane`; #2 no `refreshItem{}` sentinel → Global Constraints + `bgBusy`-gated checks; #3 fetch measurement separate path → Task 3 Steps 9-10 + Task 4 Step 4; #4 invalidated Phase B tests → Task 4 Steps 6-7.
- **Type consistency:** `effectiveInterval`/`dueItems`/`enqueueDue`/`recordDuration`/`refreshRateRows` signatures are used identically across tasks; `intervalState` constants shared.
- **Rounding:** back-off via `math.Ceil` on `Duration.Seconds()` (Task 2).
