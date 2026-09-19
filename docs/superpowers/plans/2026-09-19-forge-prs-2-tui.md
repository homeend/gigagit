# Forge PRs — plan 2 of 3: TUI PRs tab + PR hub popup + independent poll

> **For agentic workers:** executed INLINE by the session that wrote it
> (project rule: never subagents), with superpowers:executing-plans. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** show the repository's pull requests in a fifth left-top tab, open a
PR's diff on the existing preview surface, show its description and
conversation in a hub popup, and keep the list fresh on its own timer.

**Architecture:** the PR list is a *synthetic* refresh item (`prsItem`, like
`fetchItem`) — not a `sourceKey` — because it is a network read: it must never
hold `m.loading`, block `r`, or ride an "all sources" fan-out. One
`prsLoadedMsg` carries `domain.ForgeStatus` + the list; the first available
status reveals the tab for the rest of the session (per repo). `enter` runs
`FetchPRHead` through `startOp`, and `opFinishedMsg` chains the existing
one-off preview open with a title override. The hub is a wrapped
`contentPopup` filled asynchronously (the commit-message viewer's pattern).

**Tech stack:** Go 1.26, Bubble Tea, `internal/domain` forge queries from
plan 1 (`ForgeStatus`, `PullRequests`, `PRComments`, `PRFetchOp`,
`PRForgetOp`, `PRPair`), fake `gh` (`internal/forge/forgetest`).

**Spec:** `docs/superpowers/specs/2026-09-19-forge-prs-design.md` (§5 PRs tab,
§5 PR hub popup, §7, §8). Plan 1: `2026-09-19-forge-prs-1-core.md`.

## Global constraints

- READ-ONLY: nothing in this plan posts to the forge.
- No usable forge → no tab, no footer key, no help section, no notice, no
  network call after the one startup probe.
- Once shown, the tab never vanishes in that repo session; a later failure is
  an error row. A repo switch (`reRoot`) resets it (new Service, new probe).
- The PR poll ignores `[refresh] enabled` (ruling 13): `[refresh] prs = 300`,
  `0` = off, floored by `min_seconds`.
- Closed / merged / unavailable rows stay, dimmed, state word instead of the
  review badge.
- Every new TUI string: `i18n.T` literal key + all four bundles
  (`internal/i18n/lang/{ja,ko,zh,ru}.toml`). Forge text is never translated.
- `internal/tui` never imports `internal/forge` in non-test code.
- TUI tests call `t.Parallel()`; a test that needs `GG_GH_BIN` is serial
  (uses `t.Setenv`) and lives in `pr_read_serial_test.go`.
- No wide glyphs in rows (wide-glyph overflow memory): `→`, `✓`, `✗`, `·` only.

## Amendments to the spec (recorded, not re-asked)

1. Config key is `prs` (ruling 13), not `prs_seconds`; it is a `*int` so an
   explicit `0` (off) is distinguishable from unset (300) in the overlay.
2. `srcPRs` does not exist: the list is a synthetic refresh item (above).
3. No `💬N` badge — `gh pr list` has no comment-count field.
4. Hub search = `contentPopup`'s `/` type-to-filter, not the `/ @ ] [`
   in-view search. Hunk snippets are plain `+`/`-` lines (no diff colouring).
5. Manual refresh = the global `r` (which now also re-reads PRs when the
   forge is on); no tab-private key. Forget = `d`. Copy URL = `y`.
6. The `gg://` pair-link copy and the hub opened *from the PR diff* move to
   plan 3 (they need the open-PR state plan 3 introduces for comments).

## File map

| File | Change |
|---|---|
| `internal/config/config.go`, `write.go` tests | `RefreshConfig.PRs *int`, `PRsSeconds()`, overlay |
| `internal/tui/settings_docs*.go` (settingDoc registry) | `refresh.prs` doc |
| `internal/forge/gh.go` | `GH_NO_UPDATE_NOTIFIER=1`, `GH_PROMPT_DISABLED=1` on every call |
| `internal/tui/pr_panel.go` (new) | `prsItem`, `prsLoadedMsg`, `readPRsCmd`, `prList`, `prRows`, `selectedPR`, handlers |
| `internal/tui/pr_hub.go` (new) | `prHubPopup`, `prHubMsg`, content builder |
| `internal/tui/pr_actions.go` (new) | enter/forget/copy-URL commands + predicates |
| `internal/tui/refresh.go` | `prsItem` in `scheduledItems`, interval/key/set arms, independent gate, tick arm |
| `internal/tui/model.go` | `panelPRs`, `m.leftTabs()`, fields, msg arms, key arms, `opFinishedMsg` chain, `reRoot` reset |
| `internal/tui/view.go`, `viewstate.go`, `selection.go`, `session_snapshot.go`, `link.go` | tab header, list, sel key, proto name |
| `internal/tui/preview_open.go` | `title` override on `previewOpenMsg`/`previewOpenState` |
| `internal/tui/source.go` | `opAffectedSources` arms |
| `internal/tui/footer.go`, `action_menu.go`, `help.go`, `settings_popup.go` | bindings, labels, help, rates row name |
| `internal/i18n/lang/*.toml` | new keys |
| docs | `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`, spec amendments, memory |

---

### Task 1: config `[refresh] prs`

**Files:** `internal/config/config.go`, `internal/config/config_test.go`
(or the existing refresh overlay test file), settingDoc registry (find with
`grep -rn '"refresh.remote_tags"' internal/`), follow
`.claude/skills/adding-config-entries/SKILL.md`.

**Produces:** `RefreshConfig.PRs *int` (`toml:"prs"`),
`func (r RefreshConfig) PRsSeconds() int` (nil → 300, negative → 0).

- [ ] **Step 1: failing tests**

```go
func TestPRsSecondsDefaultsTo300(t *testing.T) {
	t.Parallel()
	if got := (RefreshConfig{}).PRsSeconds(); got != 300 {
		t.Fatalf("unset prs = %d, want 300", got)
	}
	zero := 0
	if got := (RefreshConfig{PRs: &zero}).PRsSeconds(); got != 0 {
		t.Fatalf("prs = 0 must mean off, got %d", got)
	}
}

func TestOverlayRefreshPRsZeroWins(t *testing.T) {
	t.Parallel()
	ten, zero := 10, 0
	dst := RefreshConfig{PRs: &ten}
	overlayRefresh(&dst, RefreshConfig{PRs: &zero})
	if dst.PRsSeconds() != 0 {
		t.Fatalf("an explicit prs = 0 in a higher layer must turn the poll off")
	}
	overlayRefresh(&dst, RefreshConfig{}) // unset leaves it alone
	if dst.PRsSeconds() != 0 {
		t.Fatalf("an unset layer must not reset prs")
	}
}
```

Plus a TOML round-trip: `[refresh]\nprs = 0` loads as off; absent loads as 300.

- [ ] **Step 2:** run `go test ./internal/config/ -run 'PRs'` → FAIL (undefined).
- [ ] **Step 3: implement**

```go
	// PRs is the pull-request list poll in seconds. Unlike every other
	// interval it is INDEPENDENT of Enabled (the list is remote data no file
	// watcher or git read can refresh) and defaults ON: nil = unset → 300,
	// an explicit 0 = off. A pointer because the overlay's zero-is-unset rule
	// could not otherwise express "off" over a non-zero default.
	PRs *int `toml:"prs"`
```

```go
const defaultPRsSeconds = 300

func (r RefreshConfig) PRsSeconds() int {
	if r.PRs == nil {
		return defaultPRsSeconds
	}
	if *r.PRs < 0 {
		return 0
	}
	return *r.PRs
}
```

`overlayRefresh`: `if src.PRs != nil { v := *src.PRs; dst.PRs = &v }`.
Add the settingDoc (`refresh.prs`, default `300`, "seconds between
pull-request list reads; 0 = off; not gated by refresh.enabled") and update
the config settings registry memory at the end (Task 10).

- [ ] **Step 4:** `go test ./internal/config/ ./internal/tui/ -run 'PRs|SettingDoc|Settings'` → PASS.
- [ ] **Step 5:** commit `feat(config): [refresh] prs — the independent pull-request poll`.

### Task 2: quiet `gh`

**Files:** `internal/forge/gh.go`, `internal/forge/gh_test.go`.

- [ ] **Step 1:** failing test with the `FakeRunner`: every recorded call's
  env contains `GH_NO_UPDATE_NOTIFIER=1` and `GH_PROMPT_DISABLED=1` (check how
  `gitexec.Runner` carries env — `RunEnv`/`WithEnv`; if `FakeRunner` does not
  record env, assert through the fake gh binary instead: make `fakegh` fail
  with exit 3 when `GH_PROMPT_DISABLED` is unset and `GG_FAKEGH_STRICT=1`).
- [ ] **Step 2–4:** implement in the one `run` helper every `GH` method goes
  through; run `go test ./internal/forge/...`.
- [ ] **Step 5:** confirm `domain` passes the Service recorder to
  `forge.Default` (plan-1 advisor note); fix + test if it passes nil.
- [ ] **Step 6:** commit `fix(forge): gh never prompts or prints update notices`.

### Task 3: the `prsItem` read + independent schedule

**Files:** `internal/tui/pr_panel.go` (new), `refresh.go`, `model.go`,
`settings_popup.go`, tests `pr_panel_test.go`, `refresh_test.go`.

**Produces:**

```go
var prsItem = refreshItem{isPRs: true}

type prsLoadedMsg struct {
	status domain.ForgeStatus
	prs    []model.PullRequest
	err    error         // list error (status.Available() but the read failed)
	dur    time.Duration
	bg     bool          // true = came from the background lane (frees bgBusy)
	manual bool          // true = user asked (r): report errors on the status line
}

func (m Model) readPRsCmd(ctx context.Context, bg, manual bool) tea.Cmd
```

Model fields: `forgeShown bool`, `forgeProvider string`,
`prs []model.PullRequest`, `prsErr string`, `prsLoaded bool`,
`prsInflight bool`.

`readPRsCmd` (nil when `m.svc == nil`): `st := svc.ForgeStatus(ctx)`; if
`!st.Available()` return the msg with only status; else
`prs, err := svc.PullRequests(ctx)`.

- [ ] **Step 1: failing tests**

```go
func TestDueItemsPRsIgnoresMasterSwitch(t *testing.T) {
	t.Parallel()
	cfg := config.RefreshConfig{Enabled: false, Status: 10}
	due := dueItems(time.Now(), map[refreshItem]time.Time{}, cfg, false, false)
	if len(due) != 1 || due[0] != prsItem {
		t.Fatalf("due = %v, want only the PR item while the master switch is off", due)
	}
}

func TestDueItemsPRsOffAtZero(t *testing.T) { /* PRs:&0 → nothing due */ }

func TestRefreshTickSkipsPRsWithoutAForge(t *testing.T) {
	// forgeShown=false → the tick never returns a PR read and never sets bgBusy.
}

func TestPRsLoadedRevealsTheTabOnce(t *testing.T) {
	// available status → forgeShown true, prs stored; a later msg with
	// status unavailable/err keeps forgeShown and the OLD list, sets prsErr.
}

func TestPRsLoadedUnavailableStaysHidden(t *testing.T) {
	// status unavailable → forgeShown false, no statusMsg, leftTabs() has 4.
}
```

- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: implement**
  - `refreshItem` gains `isPRs bool`; `scheduledItems` gains `prsItem`;
    `refreshIntervalFor` → `cfg.PRsSeconds()`; `refreshTomlKey` → `"prs"`;
    `setRefreshIntervalField` → `v := secs; cfg.PRs = &v`; settings rates row
    name `"prs"` (both `settings_popup.go` name switches); `bgRefreshHint`
    name `i18n.T("pull requests")`.
  - `dueItems`: drop the early `!cfg.Enabled` return; inside the loop
    `if !cfg.Enabled && !it.isPRs { continue }`.
  - `refreshTick`: after computing `due`, drop `prsItem` when
    `!m.forgeShown`; the inflight guard treats `isPRs` like the other
    synthetic items but skips when `m.prsInflight`; the svc-nil guard covers
    it; dispatch `m.readPRsCmd(m.bgCtx, true, false)` with
    `m.prsInflight = true`.
  - Startup: in the bootstrap-done arm (model.go ~1345) add
    `m.prsInflight = true` and batch `m.readPRsCmd(context.Background(), false, false)`
    — the ONE probe; off the UI thread; never touches `srcLoading`.
  - `prsLoadedMsg` arm: clear `prsInflight`; if `msg.bg` free the lane the
    way `bgFetchDoneMsg` does (`bgBusy=false`, `recordDuration(prsItem, …)`);
    available → `forgeShown=true`, `forgeProvider`, and on `err == nil`
    replace `prs`, clear `prsErr`, restore selection by key; on `err != nil`
    keep `prs`, set `prsErr = firstLine(err)`, and only when `msg.manual`
    also `statusMsg`. Unavailable and not yet shown → nothing.
  - Global `r`: when `m.forgeShown && !m.prsInflight` batch
    `readPRsCmd(ctx, false, true)`.
  - `reRoot`: reset `forgeShown`, `prs`, `prsErr`, `prsLoaded`,
    `prsInflight`; if `activeLeftTab == panelPRs` → `panelBranches` (and
    focus with it); re-kick the probe where reRoot's load completes.
- [ ] **Step 4:** `go test ./internal/tui/ -run 'PRs|DueItems|RefreshTick|Settings'` → PASS; whole `./internal/tui/` green.
- [ ] **Step 5:** commit `feat(tui): pull-request list read on its own timer`.

### Task 4: the PRs tab

**Files:** `model.go`, `view.go`, `viewstate.go`, `selection.go`,
`session_snapshot.go`, `link.go`, `steer_nav.go` (no change expected — verify),
`pr_panel.go`, tests `pr_tab_test.go`.

**Produces:** `panelPRs`; `func (m Model) leftTabs() []panel`;
`topTabSegs(active panel, prs bool)`; `prList`; `func (m Model) prRows() []string`;
`func (m Model) selectedPR() (model.PullRequest, bool)`.

- [ ] **Step 1: failing tests**
  - `leftTabs()` has 4 entries hidden / 5 shown, `panelPRs` last.
  - ctrl+→ from Previews reaches PRs only when shown.
  - header contains `PR` short mark / `[Pull requests]` when active.
  - `prRows()`:
    `#7  Add streaming parser   octocat  feat/x → main  draft  ✓` —
    columns: number padded to the widest, title, author, `source → target`
    (fork heads as `owner:branch` via `SourceRepo`), `draft` word, review
    mark (`✓` approved, `✗` changes requested, `…` review required, empty
    otherwise); a non-open row ends with the translated state word
    (`merged` / `closed` / `unavailable`) instead.
  - an empty list renders the placeholder `(no open pull requests)`; `prsErr`
    renders a first row `gh: <line>  [r] retry` that `selectedPR()` skips.
  - non-open rows get `dimRowDecorator()`.
  - `panelSelKey(panelPRs)` = `strconv.Itoa(pr.Number)`; selection survives a
    reordered reload.
  - session snapshot names the panel `"prs"`.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: implement** — mirror every `panelPreviews` arm listed in the
  memory research (`model.go` enum/`leftTabs`/`activateTab` 3693/left-target
  3869, `view.go:681`, `viewstate.go:444` + `:749`, `selection.go:66`,
  `session_snapshot.go:137/148`). `prList` implements `panelList` +
  `Haystack` (row text is fine); `Name` = title, `Date` = `Updated.Unix()`.
  The error row lives OUTSIDE the list (a header line above the rows in the
  panel body, the way an empty-state placeholder is painted) so list indices
  stay PR indices. Find the panel decorator hook with
  `grep -n 'rowDecorator' view.go` and add the `panelPRs` arm.
- [ ] **Step 4:** tests PASS; i18n keys added: `Pull requests`, `draft`,
  `closed`, `unavailable`, `(no open pull requests)`, `gh: %s  [r] retry`
  (`merged` exists).
- [ ] **Step 5:** commit `feat(tui): Pull requests tab in the left-top slot`.

### Task 5: `enter` — fetch the head, open the diff

**Files:** `pr_actions.go` (new), `preview_open.go`, `model.go`, `source.go`,
tests `pr_open_test.go`.

**Produces:**

```go
func prTitle(p model.PullRequest) string // i18n.T("PR #%d · %s", n, title)
func (m Model) canOpenPR() bool          // focus==panelPRs && row && opsIdle
func (m Model) openPRCmd() (Model, tea.Cmd)   // resolves PRFetchOp off-thread
type prFetchReadyMsg struct{ pr model.PullRequest; op engine.FetchPRHead; err error }
func (m Model) openPRPreviewCmd(p model.PullRequest) tea.Cmd
```

Model field `pendingPROpen *model.PullRequest`.
`previewOpenMsg.title`, `previewOpenState.title` (`""` = `previewTitle`).

Flow: `enter` → `openPRCmd` (`svc.PRFetchOp` does a `BaseRepo` network call,
so it runs in a `tea.Cmd`; status line `fetching PR #7…`) →
`prFetchReadyMsg` → `m.pendingPROpen = &pr`; `m.startOp(msg.op)` →
`opFinishedMsg` success → `openPRPreviewCmd` (`svc.PRPair` then
`svc.PreviewOpen(ctx, pair.Head, pair.Base)`, stamped with `m.previewGen`,
`title: prTitle(p)`). `pendingPROpen` is cleared unconditionally beside
`pendingSwitch`. An op error leaves the friendly op error and opens nothing.

- [ ] **Step 1: failing tests**
  - `opAffectedSources(engine.FetchPRHead{})` and `(engine.ForgetPR{})` are
    non-nil and empty (no "all sources" fan-out, no ls-remote probe).
  - `handlePreviewOpenMsg` with `title: "PR #7 · x"` sets `filesTitle` and
    `previewOpen.title`; the re-arm path (`afterPreviewsRefresh`) keeps it.
  - `opFinishedMsg` success with `pendingPROpen` set returns a non-nil cmd and
    clears the field; with an error clears it and returns no preview cmd.
  - a `PreviewMerged` state for a PR (ahead == 0) reports
    `PR #7 has nothing to show against its base` rather than the branch prose.
- [ ] **Step 2–4:** implement, run `go test ./internal/tui/ -run 'PR|Preview|OpAffected'`.
- [ ] **Step 5:** commit `feat(tui): enter on a pull request opens its diff`.

### Task 6: PR hub popup (`i`)

**Files:** `pr_hub.go` (new), `model.go` (key + msg arms), tests `pr_hub_test.go`.

**Produces:**

```go
type prHubPopup struct {
	*contentPopup
	pr model.PullRequest
}
type prHubMsg struct {
	number int
	c      domain.PRComments
	err    error
}
func prHubTitle(n int) string                       // i18n.T("Pull request #%d", n) — the async tag
func prHubHeader(p model.PullRequest, now time.Time) []contentLine
func prHubBody(p model.PullRequest, c domain.PRComments, now time.Time) []contentLine
func (m Model) openPRHub(p model.PullRequest) (Model, tea.Cmd)
```

Layout (all `contentLine`s; section titles are `heading: true`):

```
#7 Add streaming parser
open · draft · octocat · feat/x → main · approved · updated 3h ago
https://github.com/o/r/pull/7

Description
  <body, CR stripped, tabs expanded, control chars sanitized; "(no description)" when empty>

Conversation (3)
  hubot · 2d
    looks good overall
  octocat · 1d · changes requested
    please split this

Outdated (1)
  parser.go:42 · hubot · 5d · resolved
    @@ -40,3 +40,4 @@
    +	x := 1
    please rename
```

`update`: `y` → copy `pr.URL` (the panel's existing clipboard cmd; status
`copied PR URL`), `r` → reload (`(loading…)` line back, re-dispatch), every
other key → `p.contentPopup.update`. `render` delegates. It embeds
`*contentPopup`, so `popupMax` (ctrl+t), scroll, `/` filter, `s` save and
esc-returns-to-opener come with it. `mode = modeWrap`. Truncated →
a last line `(comments truncated at the forge's page size)`. The `prHubMsg`
arm fills only a top layer that is a `*prHubPopup` with the same number
(stale-load gate); an error renders `(load failed: %s)  [r] retry`.

- [ ] **Step 1: failing tests** — header line composition (draft omitted when
  false, review state omitted when empty, state word for merged); body with
  no comments = Description only + `(no conversation yet)`; verdict words
  `approved` / `changes requested` / `commented`; outdated section only when
  non-empty, hunk limited to the last 6 lines; stale `prHubMsg` for another
  number is dropped; `y` returns a cmd; ctrl+t toggles `maxed()`; esc pops to
  the panel with focus still on `panelPRs`; the sanitizer strips `\r` and ESC.
- [ ] **Step 2–4:** implement; reuse `fileContentLines` for bodies (indent by
  two spaces) and the existing relative-age helper (`grep -n 'func relAge\|func humanAge\|ago' *.go`).
- [ ] **Step 5:** commit `feat(tui): pull-request hub popup`.

### Task 7: Forget (`d`), copy URL (`y`), footer, `.` menu, help

**Files:** `pr_actions.go`, `model.go`, `footer.go`, `action_menu.go`,
`help.go`, `source.go`, tests `pr_actions_test.go` (+ the existing
`TestHelpFooterCoverage` / menu-label gates).

**Produces:** `canForgetPR()` (row is not open, ops idle),
`forgetPR()` → `m.startOp(m.svc.PRForgetOp(n))` with `pendingPRsReload = true`
consumed in `opFinishedMsg` (batch `readPRsCmd(ctx,false,false)`);
footer bindings

```go
{"pr-open",   "enter", i18n.T("[enter] open"),  func(m Model) bool { return m.canOpenPR() }, scopeRow},
{"pr-hub",    "i",     i18n.T("[i]nfo"),        func(m Model) bool { return m.canOpenPR() }, scopeRow},
{"pr-copy",   "y",     i18n.T("[y] copy URL"),  func(m Model) bool { return m.canOpenPR() }, scopeRow},
{"pr-forget", "d",     i18n.T("[d] forget"),    func(m Model) bool { return m.canForgetPR() }, scopeRow},
```

menu labels `Open pull request diff`, `Pull request details…`,
`Copy pull request URL`, `Forget pull request`; help section
`Pull requests panel` (enter / i / y / d / r, and one line on the poll +
`[refresh] prs`). The help section is included only when `m.forgeShown`
(check how `helpWithHidden` filters; otherwise always listed with
"(shown when a forge CLI can read this repository)").

- [ ] Steps: failing tests (predicates; `d` on an open row does nothing and
  says `only a closed or merged pull request can be forgotten`; forget chains
  a list reload; menu rows present on the tab) → implement → gates green →
  commit `feat(tui): forget, copy URL and the PR keys in footer, menu and help`.

### Task 8: serial read test against the fake gh

**Files:** `internal/tui/pr_read_serial_test.go`.

- [ ] One non-parallel test: `t.Setenv("GG_GH_BIN", forgetest.BuildFakeGH(t))`,
  real repo + `forgetest.Seed`, `domain.OpenTUI`, run `readPRsCmd(...)()` and
  assert an available status with the seeded PRs; a second with
  `GG_GH_BIN` pointing at a missing file asserts unavailable + no error text
  on the model after the arm. Confirm `internal/archtest` stays green (test
  imports are not in `.Imports`).
- [ ] commit `test(tui): PR list read against the fake gh`.

### Task 9: headless verification

Use the `driving-tui-headless` skill. tmux hands sessions its START env, so
`GG_GH_BIN=… ./tui-capture.sh` does not reach `gg`: write a wrapper in the
scratchpad that `export`s `GG_GH_BIN` (built with
`go build -o <scratch>/fakegh ./internal/forge/testdata/fakegh`) and
`XDG_STATE_HOME`, then `exec`s the worktree-built `gg`; point
`tui-capture.sh` at it. Fixture repo in the scratchpad with
`.git/fakegh/{pr-list,pr-view-7,threads-7,repo-view}.json` copied from
`internal/forge/testdata/` plus a bare "base" remote carrying
`refs/pull/7/head` (the plan-1 e2e scenario shows the shape).

- [ ] Capture and READ each screen: tab header shows the 5th tab; rows
  (open + a merged one, dimmed word); `i` hub (wrapped body, conversation,
  outdated); ctrl+t maximized; `/` filter; esc back; `enter` → files view
  titled `PR #7 · …`; `.` menu rows; `?` help section; Settings → refresh
  rates shows `prs 300`; without `GG_GH_BIN` usable (wrapper pointing at
  `/nonexistent`) the tab is absent and the status line is quiet.
- [ ] Fix what the captures show; re-capture.

### Task 10: docs, gates, handoff

- [ ] `CHANGELOG.md`, `README.md` (the "Pull requests (read-only)" section
  gains the tab/hub/poll), `docs/CLAUDE-details.md` (forge section: synthetic
  item rationale, sticky tab, title override, amendments), spec
  "Amendments made while implementing plan 2", memory
  (`forge-prs-feature.md`, `config-settings-registry.md`, MEMORY.md line).
  `CLAUDE.md` unchanged unless a convention changed.
- [ ] `./test.sh race` from a clean tree → exit 0.
- [ ] Build the verify binary in the worktree
  (`go build -o <scratch>/gg-forge-prs-tui ./cmd/gg`, stripped), send it with
  the absolute path.
- [ ] Ask before merging. After the merge: `./build.sh install`, remove the
  worktree and branch. Never ask to push.

## Self-review

- Spec §5 PRs tab: Tasks 3, 4, 5, 7. Hub: Task 6. §7 config: Task 1. §8 error
  rows: Tasks 3/4 (list), 6 (hub), 5 (fetch failure). i18n: every task.
  Inline comments, collapse, comment re-poll, hub-from-diff, pair-link copy:
  plan 3 (amendment 6).
- Names used across tasks: `prsItem`, `prsLoadedMsg`, `readPRsCmd`,
  `forgeShown`, `prs`, `prsErr`, `prsInflight`, `selectedPR`, `canOpenPR`,
  `canForgetPR`, `pendingPROpen`, `pendingPRsReload`, `prTitle`,
  `openPRPreviewCmd`, `prHubPopup`, `prHubMsg`, `PRsSeconds`.
