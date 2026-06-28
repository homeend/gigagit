# Final-Fix Report — adaptive-refresh branch

## Finding 1 (substantive) — fetch lane-clear lacks generation guard

### Changes

**internal/tui/model.go**
- Line 112: Added `bgFetchGen int` field to Model struct (beside `bgActiveItem`/`bgBusy`)
- Lines 1459–1464: Added stale-gen guard at the top of `case bgFetchDoneMsg:`:
  ```go
  if msg.gen != m.bgFetchGen {
      return m, nil // a newer fetch superseded this one
  }
  ```

**internal/tui/refresh.go**
- `bgFetchDoneMsg` struct: added `gen int` field
- `bgFetchCmd`: signature changed to `func (m Model) bgFetchCmd(ctx context.Context, gen int) tea.Cmd`; sets `gen: gen` on returned message
- `refreshTick` fetch branch: bumps `m.bgFetchGen++` before calling `m.bgFetchCmd(m.bgCtx, m.bgFetchGen)`

### Covering test

**internal/tui/refresh_adaptive_test.go** — `TestBgFetchDoneIgnoresStaleGen`:
- Sets `m.bgFetchGen=2`, sends `bgFetchDoneMsg{gen:1}` → asserts `bgBusy` still true (stale ignored)
- Then sends `bgFetchDoneMsg{gen:2}` → asserts `bgBusy` is false (live lane freed)

Existing `TestBgFetchEnqueuesRemotesOnSuccess` uses `bgFetchDoneMsg{dur: time.Second}` (gen=0) with `newTestModel` (bgFetchGen=0) — still passes unchanged.

---

## Finding 2 (cosmetic) — stale comments

**internal/tui/model.go**
- `dataAvailableMsg` handler: Reworded "adaptive scheduler" → "informational stats (shown in the Refresh rates editor)"
- `opFinishedMsg` handler: Removed stale `effectiveInterval` reference; replaced with "stays opt-in via `[refresh] fetch` (0 = off, the default)"

**internal/config/config.go**
- `RefreshConfig` doc comment: Removed "(Phase B)"
- `MinSeconds` field comment: Replaced "backoff_factor × avg is tiny" with "a source reads very cheaply"

**internal/tui/refresh_adaptive_test.go**
- `TestManualReadRecordsDuration` comment: Replaced "seed measurements for the adaptive scheduler" with description of stats display

---

## Finding 3 (cosmetic) — CHANGELOG forward-reference contradiction

**CHANGELOG.md** (unreleased block)
- Phase B entry end: "Phase C (adaptive intervals derived from measured source-read durations) is next." → "Phase C (background-refresh config editor) followed."
- Phase A entry end: "Phase C (adaptive intervals from measured read durations)" → "Phase C (per-source interval config and stats editor)"

---

## Verify commands and output

```
gofmt -l internal/tui/model.go internal/tui/refresh.go \
         internal/tui/refresh_adaptive_test.go internal/config/config.go
# (no output — all formatted)

go vet ./internal/tui/
# No issues found

go test ./internal/tui/ -run 'BgFetch|Fetch|Rates|RefreshInterval' -v
# 8 passed

go test ./internal/tui/
# 1282 passed

go build ./cmd/gg
# Success
```

---

## Commit hash(es)

See git log after commit.
