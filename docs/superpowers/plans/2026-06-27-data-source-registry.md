# Unified Data-Source Registry — Implementation Plan (Phase A)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the monolithic TUI reload (`loadCmd`/`dataLoadedMsg`) with a per-source registry so each action refreshes only the data it changed, each panel shows its own manual-refresh spinner, and the architecture is ready for Phase B (background timers) and C (adaptive intervals).

**Architecture:** A keyed source registry in `internal/tui`. Each `sourceKey` maps to one existing gated `domain` query and to the panels that consume it (`srcConsumers`). A read produces a single generic `dataAvailableMsg{source, gen, value, manual, err}`; its handler drops stale generations, stores the value, recomputes that source's derived caches, and (if manual) clears that source's spinner. "Reload all", startup, and op-completion all fan out reads through this one path. No engine changes; one tiny derived-data query is added to `domain`.

**Tech Stack:** Go 1.26, Bubble Tea (Elm value-receiver `Model`), the existing `internal/domain` gated query layer, `go test`.

## Global Constraints

- Module `github.com/homeend/gigagit`, Go 1.26. (verbatim from CLAUDE.md)
- `internal/tui` must NOT import `internal/git` — reach git only through `internal/domain` (archtest-guarded).
- A git verb is one invocation; never shell out directly. Reads go through domain queries.
- TUI `Model` is a value receiver with pointer fields; mutate via returned copies.
- `main` is the trunk. This work is on branch `feat/source-registry` in worktree `.claude/worktrees/source-registry`. Do NOT merge — the human merges.
- TDD: write the failing test first, watch it fail, implement minimally, watch it pass, commit.
- Test commands run from the worktree root: `cd /mnt/t/others/gigagit/.claude/worktrees/source-registry`.
- Build check: `go build ./cmd/gg`. Stage check before any commit: `gofmt -l internal/ ; go vet ./internal/tui/...`.

## File Structure

- **Create** `internal/tui/source.go` — registry: `sourceKey` + constants, `srcConsumers`, `dataAvailableMsg`, `readSourceCmd`, `reloadSourcesCmd`, `reloadAllCmd`, `markSourcesLoading`, `opAffectedSources`, `bootstrapCmd`/`configReadyMsg`.
- **Create** `internal/tui/source_test.go` — registry unit tests.
- **Create** `internal/tui/selection.go` — `panelSelKey`/`restorePanelSel` selection-by-identity helpers.
- **Create** `internal/tui/selection_test.go`.
- **Modify** `internal/domain/conflict.go` — add the public `Conflict` derived-data query.
- **Modify** `internal/tui/model.go` — Model fields (`srcGen`/`srcInflight`/`srcLoading`), map init, `dataAvailableMsg` handler, `configReadyMsg` handler, `Init`, the `r` handler, the `opFinishedMsg` handler.
- **Modify** `internal/tui/viewstate.go` — per-source spinner in panel titles.
- **Modify/Delete** `internal/tui/load.go`, `internal/tui/op.go` — retire `loadCmd`/`dataLoadedMsg`/`reloadRefsCmd`/`reloadIdentityCmd` (Task 8).

Tasks are ordered so the suite stays green at every boundary: the registry is built **alongside** the old path (Tasks 1–5), the wiring is flipped to it (Tasks 6–7), then the dead old path is removed (Task 8).

---

### Task 1: Registry types + Model state

**Files:**
- Create: `internal/tui/source.go`
- Create: `internal/tui/source_test.go`
- Modify: `internal/tui/model.go` (Model struct + the constructor that inits `sel`/`sortModes`/… maps)

**Interfaces:**
- Produces: `type sourceKey int`; constants `srcStatus, srcBranches, srcRemotes, srcTags, srcReflog, srcWorktrees, srcFeed, srcIdentity, srcCount`; `var srcConsumers map[sourceKey][]panel`; `type dataAvailableMsg struct { source sourceKey; gen int; value any; manual bool; err error }`; Model fields `srcGen map[sourceKey]int`, `srcInflight map[sourceKey]bool`, `srcLoading map[sourceKey]bool`.

- [ ] **Step 1: Write the failing test**

```go
// internal/tui/source_test.go
package tui

import "testing"

func TestSrcConsumersCoverAllSources(t *testing.T) {
	for s := sourceKey(0); s < srcCount; s++ {
		if s == srcIdentity {
			continue // identity feeds the Settings popup, not a left panel
		}
		if len(srcConsumers[s]) == 0 {
			t.Errorf("source %d has no consumer panels", s)
		}
	}
}

func TestRegistryMapsInitialized(t *testing.T) {
	m := newTestModel(t) // existing helper used across *_test.go
	if m.srcGen == nil || m.srcInflight == nil || m.srcLoading == nil {
		t.Fatal("registry maps must be initialized by the constructor")
	}
}
```

Note: confirm the existing constructor-based test-model helper name with `rg -n "func newTestModel" internal/tui` and use it; if it differs (e.g. `newModelForTest`), adjust the call.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/source-registry && go test ./internal/tui/ -run 'TestSrcConsumers|TestRegistryMaps' -v`
Expected: FAIL — `undefined: sourceKey` / `srcConsumers`.

- [ ] **Step 3: Create the registry types**

```go
// internal/tui/source.go
package tui

// sourceKey identifies one independently-refreshable data source. Each maps to a
// single gated domain query and feeds one or more panels (see srcConsumers). It
// is the unit of the reactive refresh layer: a read of a source emits a
// dataAvailableMsg, and every consuming panel re-renders from the stored value.
type sourceKey int

const (
	srcStatus sourceKey = iota
	srcBranches
	srcRemotes
	srcTags
	srcReflog
	srcWorktrees
	srcFeed
	srcIdentity
	srcCount
)

// srcConsumers maps a source to the panels that render from it. Used to target
// the manual-refresh spinner and, in Phase B, to decide which sources a timer
// polls. srcIdentity feeds the Settings popup, not a left panel, so it is absent.
var srcConsumers = map[sourceKey][]panel{
	srcStatus:    {panelFiles, panelStaged},
	srcBranches:  {panelBranches, panelCommits},
	srcRemotes:   {panelRemotes},
	srcTags:      {panelTags},
	srcReflog:    {panelReflog},
	srcWorktrees: {panelWorktrees, panelBranches},
	srcFeed:      {panelCommits},
}

// dataAvailableMsg is the single event every source read produces. value is
// typed per source and asserted in the handler; gen ties the result to the read
// that issued it (stale gens are dropped); manual=true means a user-initiated
// read whose spinner must be cleared on arrival (false = silent, Phase B).
type dataAvailableMsg struct {
	source sourceKey
	gen    int
	value  any
	manual bool
	err    error
}
```

- [ ] **Step 4: Add Model fields + init**

In `internal/tui/model.go`, add to the `Model` struct (near `loadGen`):

```go
	srcGen      map[sourceKey]int  // per-source generation; stale dataAvailableMsg dropped
	srcInflight map[sourceKey]bool // a read of this source is outstanding (coalescing)
	srcLoading  map[sourceKey]bool // a manual read is in flight → consuming panels show ⏳
```

In the constructor where `sel`, `sortModes`, `dispModes`, `hscroll` maps are made, add:

```go
		srcGen:      map[sourceKey]int{},
		srcInflight: map[sourceKey]bool{},
		srcLoading:  map[sourceKey]bool{},
```

Find the constructor with `rg -n "sel: *map\[panel\]int\{\}|sortModes:" internal/tui/*.go` and add the three lines in the same literal.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/source-registry && go test ./internal/tui/ -run 'TestSrcConsumers|TestRegistryMaps' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/source.go internal/tui/source_test.go internal/tui/model.go
git commit -m "feat(tui): data-source registry types + per-source state"
```

---

### Task 2: Domain `Conflict` derived-data query

**Files:**
- Modify: `internal/domain/conflict.go`
- Test: `internal/domain/conflict_test.go` (append)

**Interfaces:**
- Produces: `func (s *Service) Conflict(ctx context.Context, st model.WorkingTreeStatus) ConflictState` — public wrapper over the private `conflictState`, so the TUI `status` source can derive conflict from the status it just read (matching how `loadSnapshot` derives `snap.Conflict`).

Rationale: the spec's "no new domain queries" refers to read sources; conflict is *derived data of status*. Exposing it (rather than re-reading status) keeps the status read single and the EOL reconcile authoritative.

- [ ] **Step 1: Write the failing test**

```go
// internal/domain/conflict_test.go (append; reuse the package's repo helper)
func TestConflictCleanRepoIsZero(t *testing.T) {
	s := newTestService(t) // confirm helper name with: rg -n "func newTestService" internal/domain
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Conflict(context.Background(), st); got != (ConflictState{}) {
		t.Errorf("clean repo conflict = %+v, want zero", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/source-registry && go test ./internal/domain/ -run TestConflictCleanRepoIsZero -v`
Expected: FAIL — `s.Conflict undefined`.

- [ ] **Step 3: Implement**

In `internal/domain/conflict.go`, add above `conflictState`:

```go
// Conflict derives the conflict source (merge/rebase/cherry-pick parties) from a
// status the caller already read. It is the public face of conflictState, used
// by the TUI's status source so a per-panel status refresh carries the same
// conflict attribution the full Snapshot did. Cheap: short-circuits on a clean
// working tree.
func (s *Service) Conflict(ctx context.Context, st model.WorkingTreeStatus) ConflictState {
	return s.conflictState(ctx, st)
}
```

Ensure `internal/domain/conflict.go` imports `context` and `model` (it already uses both via `conflictState`).

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/source-registry && go test ./internal/domain/ -run TestConflictCleanRepoIsZero -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/conflict.go internal/domain/conflict_test.go
git commit -m "feat(domain): public Conflict(st) derived query for per-source status refresh"
```

---

### Task 3: Per-source read commands

**Files:**
- Modify: `internal/tui/source.go`
- Modify: `internal/tui/source_test.go`

**Interfaces:**
- Consumes: domain queries `svc.Status`, `svc.Branches`, `svc.RemoteBranches`, `svc.Tags`, `svc.Reflog(limit)`, `svc.Worktrees`, `svc.Identity`, `svc.CommitTimes`, `svc.Conflict`; `feed.LoadInitial`.
- Produces: `func (m Model) readSourceCmd(s sourceKey, manual bool) tea.Cmd`; value payload types `statusPayload{status model.WorkingTreeStatus; conflict domain.ConflictState}`, `worktreesPayload{worktrees []model.Worktree; headTimes map[string]int64}`, `feedPayload{commits []model.Commit; exhausted bool}`. Other sources carry their bare domain type as `value`.

The command bakes in `m.srcGen[s]` so a result that arrives after a newer read of the same source is dropped by the handler.

- [ ] **Step 1: Write the failing test**

```go
// internal/tui/source_test.go (append)
func TestReadSourceBranchesProducesMsg(t *testing.T) {
	m := newTestModelWithRepo(t) // a model wired to a real temp repo; confirm helper name
	msg := m.readSourceCmd(srcBranches, true)()
	dm, ok := msg.(dataAvailableMsg)
	if !ok {
		t.Fatalf("want dataAvailableMsg, got %T", msg)
	}
	if dm.source != srcBranches || dm.gen != m.srcGen[srcBranches] || !dm.manual {
		t.Fatalf("bad envelope: %+v", dm)
	}
	if _, ok := dm.value.([]model.Branch); !ok {
		t.Fatalf("branches value type = %T, want []model.Branch", dm.value)
	}
}

func TestReadSourceStatusCarriesConflict(t *testing.T) {
	m := newTestModelWithRepo(t)
	msg := m.readSourceCmd(srcStatus, false)().(dataAvailableMsg)
	if _, ok := msg.value.(statusPayload); !ok {
		t.Fatalf("status value type = %T, want statusPayload", msg.value)
	}
}
```

Confirm the real-repo model helper name with `rg -n "func newTestModelWithRepo|func newModel\(|svc.*domain.New" internal/tui/*_test.go`; reuse whatever the suite already uses to build a Model bound to a temp repo.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/source-registry && go test ./internal/tui/ -run 'TestReadSource' -v`
Expected: FAIL — `m.readSourceCmd undefined`.

- [ ] **Step 3: Implement the read commands**

```go
// internal/tui/source.go (append; add imports: context, sync, tea, domain, model)

type statusPayload struct {
	status   model.WorkingTreeStatus
	conflict domain.ConflictState
}

type worktreesPayload struct {
	worktrees []model.Worktree
	headTimes map[string]int64
}

type feedPayload struct {
	commits   []model.Commit
	exhausted bool
}

// readSourceCmd reads one source off the UI thread via the gated domain layer and
// returns a dataAvailableMsg. gen is captured now so a result superseded by a
// newer read of the same source is dropped on arrival. Derived data that the old
// Snapshot computed alongside a read (conflict for status, head-times for
// worktrees) is computed here so a per-source refresh stays self-contained.
func (m Model) readSourceCmd(s sourceKey, manual bool) tea.Cmd {
	svc := m.svc
	feed := m.feed
	reflogLimit := m.cfg.UI.ReflogLimit
	gen := m.srcGen[s]
	return func() tea.Msg {
		ctx := context.Background()
		out := dataAvailableMsg{source: s, gen: gen, manual: manual}
		switch s {
		case srcStatus:
			st, err := svc.Status(ctx)
			if err != nil {
				out.err = err
				return out
			}
			out.value = statusPayload{status: st, conflict: svc.Conflict(ctx, st)}
		case srcBranches:
			bs, err := svc.Branches(ctx)
			out.value, out.err = bs, err
		case srcRemotes:
			rbs, err := svc.RemoteBranches(ctx)
			out.value, out.err = rbs, err
		case srcTags:
			tags, err := svc.Tags(ctx)
			out.value, out.err = tags, err
		case srcReflog:
			rl, err := svc.Reflog(ctx, reflogLimit)
			out.value, out.err = rl, err
		case srcWorktrees:
			wts, err := svc.Worktrees(ctx)
			if err != nil {
				out.err = err
				return out
			}
			shas := make([]string, 0, len(wts))
			for _, w := range wts {
				if w.Head != "" {
					shas = append(shas, w.Head)
				}
			}
			times, _ := svc.CommitTimes(ctx, shas) // best-effort, as in loadSnapshot
			out.value = worktreesPayload{worktrees: wts, headTimes: times}
		case srcFeed:
			fs, err := feed.LoadInitial(ctx)
			out.value, out.err = feedPayload{commits: fs.Commits, exhausted: fs.Exhausted}, err
		case srcIdentity:
			id, err := svc.Identity(ctx)
			out.value, out.err = id, err
		}
		return out
	}
}
```

Note on `srcReflog`: confirm `svc.Reflog`'s zero-limit behavior with `rg -n "func (s \*Service) Reflog" internal/domain/query.go` and match the startup default (`defaultReflogLimit`). If `Reflog` requires a positive limit, pass `m.cfg.UI.ReflogLimit` falling back to the same constant `loadCmd` used.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/source-registry && go test ./internal/tui/ -run 'TestReadSource' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/source.go internal/tui/source_test.go
git commit -m "feat(tui): per-source read commands producing dataAvailableMsg"
```

---

### Task 4: `dataAvailableMsg` handler

**Files:**
- Modify: `internal/tui/model.go` (add a `case dataAvailableMsg:` in `Update`)
- Modify: `internal/tui/source_test.go`

**Interfaces:**
- Consumes: `dataAvailableMsg`, the payload types from Task 3, existing helpers `rebuildCommitGraph`, `sortRemoteBranchesLocalFirst`, `feedUpstreams`, `feedScopeSig`, `feedScopeApplied`, `startFeedReload`, `proc.refreshed`.
- Produces: the handler. Behavior: drop stale gen; store value; recompute derived; clear `srcLoading[s]` if manual; clear `srcInflight[s]`.

- [ ] **Step 1: Write the failing test**

```go
// internal/tui/source_test.go (append)
func TestDataAvailableStaleGenDropped(t *testing.T) {
	m := newTestModel(t)
	m.srcGen[srcBranches] = 5
	old := []model.Branch{{Name: "old"}}
	m.branches = old
	nm, _ := m.Update(dataAvailableMsg{source: srcBranches, gen: 4,
		value: []model.Branch{{Name: "new"}}, manual: true})
	if got := nm.(Model).branches; len(got) != 1 || got[0].Name != "old" {
		t.Fatalf("stale gen must be ignored, branches=%+v", got)
	}
}

func TestDataAvailableManualClearsSpinner(t *testing.T) {
	m := newTestModel(t)
	m.srcLoading[srcBranches] = true
	m.srcInflight[srcBranches] = true
	nm, _ := m.Update(dataAvailableMsg{source: srcBranches, gen: m.srcGen[srcBranches],
		value: []model.Branch{{Name: "x"}}, manual: true})
	mm := nm.(Model)
	if mm.srcLoading[srcBranches] || mm.srcInflight[srcBranches] {
		t.Fatal("manual read must clear srcLoading and srcInflight on arrival")
	}
	if len(mm.branches) != 1 || mm.branches[0].Name != "x" {
		t.Fatalf("value not stored: %+v", mm.branches)
	}
}

func TestDataAvailableAutoLeavesNoSpinner(t *testing.T) {
	m := newTestModel(t)
	nm, _ := m.Update(dataAvailableMsg{source: srcTags, gen: m.srcGen[srcTags],
		value: []model.Tag{{Name: "v1"}}, manual: false})
	if nm.(Model).srcLoading[srcTags] {
		t.Fatal("auto read must never set a spinner")
	}
}

// Blocker 2 regression: when branches lands while a srcFeed read is still in
// flight, the upstream re-walk must be deferred (no startFeedReload cmd) so it
// cannot write the feed concurrently with the in-flight LoadInitial. Build a
// model whose feed has tracked upstreams and a stale applied scope so the latch
// would otherwise fire. (Use a real-repo model with a tracked upstream; confirm
// the feed/upstream setup against an existing feed test, e.g. TestDataLoadedTriggersUpstreamReload.)
func TestBranchesDefersFeedRewalkWhileFeedInflight(t *testing.T) {
	m := newTestModelWithTrackedUpstream(t) // see note above; reuse existing feed test scaffolding
	m.srcInflight[srcFeed] = true           // a srcFeed read is outstanding
	if m.maybeFeedUpstreamRewalk() {
		t.Fatal("re-walk must be deferred while srcFeed is in flight")
	}
	m.srcInflight[srcFeed] = false // feed read has now landed
	if !m.maybeFeedUpstreamRewalk() {
		t.Fatal("re-walk must fire once the feed read has landed")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/source-registry && go test ./internal/tui/ -run 'TestDataAvailable' -v`
Expected: FAIL — `dataAvailableMsg` not handled (value unchanged / spinner not cleared).

- [ ] **Step 3: Implement the handler**

Add to `Update`'s type switch in `internal/tui/model.go` (place it right after the existing `case dataLoadedMsg:` block):

```go
	case dataAvailableMsg:
		if msg.gen != m.srcGen[msg.source] {
			return m, nil // superseded by a newer read of this source
		}
		m.srcInflight[msg.source] = false
		if msg.manual {
			m.srcLoading[msg.source] = false
		}
		// m.loading is the legacy "a blocking refresh is in flight" flag still read
		// by ~10 action guards (avail.go:14 and the !m.running && !m.loading sites in
		// model.go). Keep it alive as a derived value = any manual source still
		// loading, so those guards keep working unchanged. (Phase B: auto reads set
		// no srcLoading, so they correctly never block actions.)
		m.loading = m.anySourceLoading()
		if msg.err != nil {
			// Best-effort sources (remotes/tags/reflog) must not blank the UI on a
			// transient error; surface it on the status line and keep prior data.
			m.statusMsg = sourceErr(msg.source, msg.err)
			return m, nil
		}
		switch msg.source {
		case srcStatus:
			p := msg.value.(statusPayload)
			m.status = p.status
			m.conflict = p.conflict
			if m.proc != nil {
				return m.proc.refreshed(m) // process re-derives from fresh status
			}
		case srcBranches:
			m.branches = msg.value.([]model.Branch)
			m.remoteBranches = sortRemoteBranchesLocalFirst(m.remoteBranches, m.branches)
			m = m.rebuildCommitGraph()
			// Upstream re-walk latch. maybeFeedUpstreamRewalk is false while a srcFeed
			// read is still in flight, so the initial LoadInitial and the scoped
			// re-walk never write the feed concurrently — when branches lands first,
			// the re-walk is deferred to srcFeed's arrival (below) instead.
			if m.maybeFeedUpstreamRewalk() {
				var reload tea.Cmd
				m, reload = m.startFeedReload()
				return m, reload
			}
		case srcRemotes:
			m.remoteBranches = sortRemoteBranchesLocalFirst(msg.value.([]model.RemoteBranch), m.branches)
		case srcTags:
			m.tags = msg.value.([]model.Tag)
		case srcReflog:
			m.reflog = msg.value.([]model.ReflogEntry)
		case srcWorktrees:
			p := msg.value.(worktreesPayload)
			m.worktrees = p.worktrees
			m.headTimes = p.headTimes
		case srcFeed:
			p := msg.value.(feedPayload)
			m.commits = p.commits
			m.commitsExhausted = p.exhausted
			m.commitsLoading = false
			m = m.rebuildCommitGraph()
			// The initial feed read just landed; now it is safe to run the upstream
			// re-walk (if branches already arrived and set the latch). This is the
			// other half of the startup ordering: exactly one path fires the re-walk,
			// always after the initial LoadInitial completes — no feed write race.
			if m.maybeFeedUpstreamRewalk() {
				var reload tea.Cmd
				m, reload = m.startFeedReload()
				return m, reload
			}
		case srcIdentity:
			m.identity = msg.value.(model.Identity)
		}
		return m, nil
```

Add these helpers to `source.go`:

```go
// anySourceLoading reports whether any source is mid manual-refresh. It backs the
// derived m.loading flag (the legacy action-blocking gate) and the per-panel
// spinner targeting.
func (m Model) anySourceLoading() bool {
	for _, v := range m.srcLoading {
		if v {
			return true
		}
	}
	return false
}

// maybeFeedUpstreamRewalk reports whether the one-time "re-walk the feed now that
// tracked upstreams are known" should fire. It is true only when upstreams exist,
// the applied scope is stale, AND no srcFeed read is in flight — the in-flight
// guard is what serializes the initial LoadInitial against the scoped re-walk so
// they never write the feed at once (Blocker 2). Both the srcBranches and srcFeed
// arrival handlers call it; whichever lands last with the guard clear fires once.
func (m Model) maybeFeedUpstreamRewalk() bool {
	return len(m.feedUpstreams()) > 0 &&
		m.feedScopeApplied != m.feedScopeSig() &&
		!m.srcInflight[srcFeed]
}
```

Add the small error-labeler near the handler (or in `source.go`):

```go
func sourceErr(s sourceKey, err error) string {
	name := map[sourceKey]string{
		srcStatus: "status", srcBranches: "branches", srcRemotes: "remotes",
		srcTags: "tags", srcReflog: "reflog", srcWorktrees: "worktrees",
		srcFeed: "commits", srcIdentity: "identity",
	}[s]
	return name + ": " + err.Error()
}
```

Note: selection clamping is intentionally NOT here yet — Task 5 adds selection-by-identity, which supersedes the old global clamp.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/source-registry && go test ./internal/tui/ -run 'TestDataAvailable' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/source.go internal/tui/source_test.go
git commit -m "feat(tui): dataAvailableMsg handler — store + derive + clear spinner per source"
```

---

### Task 5: Selection-by-identity stability

**Files:**
- Create: `internal/tui/selection.go`
- Create: `internal/tui/selection_test.go`
- Modify: `internal/tui/model.go` (call `restorePanelSel` in the `dataAvailableMsg` handler for each source that changed a panel's list)

**Interfaces:**
- Consumes: `m.sel`, `panelLen`, the per-panel slices.
- Produces: `func (m Model) panelSelKey(p panel) string` (identity of the currently-selected row: branch name / file path / commit hash / tag / worktree path / reflog index), `func (m Model) restorePanelSel(p panel, key string) Model` (re-find `key` in the panel's current list and set `m.sel[p]`; clamp to last row if gone).

- [ ] **Step 1: Write the failing test**

```go
// internal/tui/selection_test.go
package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestRestoreSelByIdentitySurvivesReorder(t *testing.T) {
	m := newTestModel(t)
	m.branches = []model.Branch{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	m.sel[panelBranches] = 2 // "c"
	key := m.panelSelKey(panelBranches)
	// Refresh reorders the list; "c" moves to index 0.
	m.branches = []model.Branch{{Name: "c"}, {Name: "a"}, {Name: "b"}}
	m = m.restorePanelSel(panelBranches, key)
	if m.sel[panelBranches] != 0 {
		t.Fatalf("selection should follow 'c' to index 0, got %d", m.sel[panelBranches])
	}
}

func TestRestoreSelClampsWhenItemGone(t *testing.T) {
	m := newTestModel(t)
	m.branches = []model.Branch{{Name: "a"}, {Name: "b"}}
	m.sel[panelBranches] = 1 // "b"
	key := m.panelSelKey(panelBranches)
	m.branches = []model.Branch{{Name: "a"}} // "b" deleted
	m = m.restorePanelSel(panelBranches, key)
	if m.sel[panelBranches] != 0 {
		t.Fatalf("selection should clamp to 0 when item gone, got %d", m.sel[panelBranches])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/source-registry && go test ./internal/tui/ -run 'TestRestoreSel' -v`
Expected: FAIL — `m.panelSelKey undefined`.

- [ ] **Step 3: Implement the helpers**

```go
// internal/tui/selection.go
package tui

// panelSelKey returns a stable identity for the currently-selected row of p, so
// a refresh that reorders or removes rows can restore the selection by identity
// rather than by a now-meaningless index. Empty string = no stable identity.
func (m Model) panelSelKey(p panel) string {
	i := m.sel[p]
	if i < 0 || i >= m.panelLen(p) {
		return ""
	}
	switch p {
	case panelBranches:
		return m.branches[i].Name
	case panelRemotes:
		return m.remoteBranches[i].Name
	case panelWorktrees:
		return m.worktrees[i].Path
	case panelTags:
		return m.tags[i].Name
	case panelFiles:
		return m.status.Modified[i].Path // confirm Files row source; see note below
	case panelStaged:
		return m.status.Staged[i].Path // confirm Staged row source; see note below
	case panelCommits:
		return m.commits[i].Hash
	case panelReflog:
		return m.reflog[i].Hash // reflog rows; fall back to index string if no stable id
	}
	return ""
}

// restorePanelSel re-finds key in p's current list and points m.sel[p] at it. If
// key is empty or gone, it clamps to the last row (or 0 when empty).
func (m Model) restorePanelSel(p panel, key string) Model {
	n := m.panelLen(p)
	if n == 0 {
		m.sel[p] = 0
		return m
	}
	if key != "" {
		for i := 0; i < n; i++ {
			if m.rowKeyAt(p, i) == key {
				m.sel[p] = i
				return m
			}
		}
	}
	if m.sel[p] >= n {
		m.sel[p] = n - 1
	}
	if m.sel[p] < 0 {
		m.sel[p] = 0
	}
	return m
}

// rowKeyAt is panelSelKey for an explicit index (used by the restore scan).
func (m Model) rowKeyAt(p panel, i int) string {
	saved := m.sel[p]
	m.sel[p] = i
	k := m.panelSelKey(p)
	m.sel[p] = saved
	return k
}
```

IMPORTANT — verify the Files/Staged/Reflog row sources before writing the cases: run `rg -n "func (m Model) panelLen" internal/tui/model.go` and read `panelLen`'s body to see exactly which slice each panel indexes (e.g. Files may index a filtered/derived list, not `m.status.Modified` directly). Use the same slice `panelLen` counts, so identity and length agree. If a panel has no natural identity (reflog), return `fmt.Sprintf("%d", i)` — restore then degrades to index-clamp, which is acceptable.

- [ ] **Step 4: Wire restore into the handler**

In the `dataAvailableMsg` handler (Task 4), wrap each panel-mutating source so selection is captured before and restored after. Example for `srcBranches` (apply the same pattern to every source that owns a panel):

```go
		case srcBranches:
			key := m.panelSelKey(panelBranches)
			m.branches = msg.value.([]model.Branch)
			m = m.restorePanelSel(panelBranches, key)
			m.remoteBranches = sortRemoteBranchesLocalFirst(m.remoteBranches, m.branches)
			m = m.rebuildCommitGraph()
			if m.maybeFeedUpstreamRewalk() { // see Task 4 — serialized against srcFeed
				var reload tea.Cmd
				m, reload = m.startFeedReload()
				return m, reload
			}
```

For `srcWorktrees` restore both `panelWorktrees` and `panelBranches` (it feeds the Branches path column too). For `srcStatus` restore `panelFiles` and `panelStaged`. For `srcRemotes`→`panelRemotes`, `srcTags`→`panelTags`, `srcReflog`→`panelReflog`, `srcFeed`→`panelCommits`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/source-registry && go test ./internal/tui/ -run 'TestRestoreSel|TestDataAvailable' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/selection.go internal/tui/selection_test.go internal/tui/model.go
git commit -m "feat(tui): selection-by-identity survives partial refresh"
```

---

### Task 6: `reloadAll` + bootstrap; wire `Init` and `r`

**Files:**
- Modify: `internal/tui/source.go` (add `reloadSourcesCmd`, `reloadAllCmd`, `markSourcesLoading`, `bootstrapCmd`, `configReadyMsg`)
- Modify: `internal/tui/model.go` (`Init`, `configReadyMsg` handler, the `r` key handler)
- Modify: `internal/tui/source_test.go`

**Interfaces:**
- Produces: `func (m Model) reloadSourcesCmd(srcs []sourceKey, manual bool) (Model, tea.Cmd)` (bumps `srcGen` + sets `srcInflight`/`srcLoading` per source, returns a `tea.Batch` of `readSourceCmd`); `func (m Model) reloadAllCmd(manual bool) (Model, tea.Cmd)` (= reload every source 0..srcCount); `type configReadyMsg struct{ cfg config.Config }`; `func (m Model) bootstrapCmd() tea.Cmd` (loads config + applies page sizes/EOL/MRU like today's `loadCmd` preamble, then emits `configReadyMsg`).
- Consumes: `feed.SetPageSizes`, `svc.SetShowEOLOnlyChanges`, `svc.TopLevel`, `config.Load`, `repos.Touch`.

- [ ] **Step 1: Write the failing test**

```go
// internal/tui/source_test.go (append)
func TestReloadAllBumpsEveryGenAndBatches(t *testing.T) {
	m := newTestModel(t)
	before := map[sourceKey]int{}
	for s := sourceKey(0); s < srcCount; s++ {
		before[s] = m.srcGen[s]
	}
	m, cmd := m.reloadAllCmd(true)
	if cmd == nil {
		t.Fatal("reloadAllCmd must return a batch command")
	}
	for s := sourceKey(0); s < srcCount; s++ {
		if m.srcGen[s] != before[s]+1 {
			t.Errorf("source %d gen not bumped", s)
		}
		if !m.srcInflight[s] {
			t.Errorf("source %d not marked in-flight", s)
		}
	}
}

func TestReloadSourcesManualSetsConsumerSpinners(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.reloadSourcesCmd([]sourceKey{srcStatus}, true)
	if !m.srcLoading[srcStatus] {
		t.Fatal("manual reload must set srcLoading for the source")
	}
	m2 := newTestModel(t)
	m2, _ = m2.reloadSourcesCmd([]sourceKey{srcStatus}, false)
	if m2.srcLoading[srcStatus] {
		t.Fatal("auto reload must not set srcLoading")
	}
}

// Blocker 1 regression: m.loading is the legacy action-blocking gate; a manual
// reload must set it (so pull/push/etc. guards block during refresh) and the
// arrival of the last source must clear it.
func TestManualReloadDrivesLegacyLoadingFlag(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.reloadSourcesCmd([]sourceKey{srcTags}, true)
	if !m.loading {
		t.Fatal("manual reload must set m.loading (action guards depend on it)")
	}
	nm, _ := m.Update(dataAvailableMsg{source: srcTags, gen: m.srcGen[srcTags],
		value: []model.Tag{{Name: "v1"}}, manual: true})
	if nm.(Model).loading {
		t.Fatal("m.loading must clear when the last manual source lands")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/source-registry && go test ./internal/tui/ -run 'TestReloadAll|TestReloadSources' -v`
Expected: FAIL — `reloadAllCmd undefined`.

- [ ] **Step 3: Implement reload fan-out**

```go
// internal/tui/source.go (append; add imports: filepath, time, config, repos)

// reloadSourcesCmd bumps each source's generation, marks it in-flight (and, if
// manual, loading), and returns a batch that reads them all concurrently. The
// per-source gen bump means any older in-flight read of the same source is
// dropped when it lands.
func (m Model) reloadSourcesCmd(srcs []sourceKey, manual bool) (Model, tea.Cmd) {
	// Defensive lazy-init: a Model built as a literal in a test (rather than via the
	// constructor patched in Task 1) would panic on the nil-map writes below.
	if m.srcGen == nil {
		m.srcGen = map[sourceKey]int{}
	}
	if m.srcInflight == nil {
		m.srcInflight = map[sourceKey]bool{}
	}
	if m.srcLoading == nil {
		m.srcLoading = map[sourceKey]bool{}
	}
	cmds := make([]tea.Cmd, 0, len(srcs))
	for _, s := range srcs {
		m.srcGen[s]++
		m.srcInflight[s] = true
		if manual {
			m.srcLoading[s] = true
		}
		cmds = append(cmds, m.readSourceCmd(s, manual))
	}
	// Keep the legacy action-blocking flag in sync (see the handler note in Task 4).
	m.loading = m.anySourceLoading()
	return m, tea.Batch(cmds...)
}

// reloadAllCmd refreshes every source — the registry's "reload everything" (r,
// and the post-bootstrap startup fan-out).
func (m Model) reloadAllCmd(manual bool) (Model, tea.Cmd) {
	all := make([]sourceKey, 0, srcCount)
	for s := sourceKey(0); s < srcCount; s++ {
		all = append(all, s)
	}
	return m.reloadSourcesCmd(all, manual)
}

// configReadyMsg carries the loaded config from bootstrapCmd; its handler applies
// it and fans out the first all-source read.
type configReadyMsg struct{ cfg config.Config }

// bootstrapCmd loads config and applies the settings the first reads depend on
// (feed page sizes, EOL-only visibility) plus the MRU touch, then emits
// configReadyMsg. This preserves the ordering loadCmd had: config BEFORE the feed
// walk and status read.
func (m Model) bootstrapCmd() tea.Cmd {
	svc := m.svc
	feed := m.feed
	statePath := m.statePath
	return func() tea.Msg {
		ctx := context.Background()
		cfg := config.Defaults()
		if top, err := svc.TopLevel(ctx); err == nil && top != "" {
			if c, cerr := config.Load(config.DefaultGlobalPath(), filepath.Join(top, ".gg.toml")); cerr == nil {
				cfg = c
			}
			if statePath != "" {
				_ = repos.Touch(statePath, top, time.Now())
			}
		}
		feed.SetPageSizes(cfg.UI.CommitInitialCount, cfg.UI.CommitBatchSize)
		svc.SetShowEOLOnlyChanges(cfg.UI.ShowEOLOnlyChanges)
		return configReadyMsg{cfg: cfg}
	}
}
```

- [ ] **Step 4: Wire `Init`, `configReadyMsg`, and `r`**

In `internal/tui/model.go`:

```go
// Init
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.bootstrapCmd(), loadSearchHistCmd(m.svc), heartbeatCmd())
}
```

Add the `configReadyMsg` case to `Update` (fans out the first all-source read; startup is manual so panels show ⏳ as each source lands instead of one long blank):

```go
	case configReadyMsg:
		m.cfg = msg.cfg
		var cmd tea.Cmd
		m, cmd = m.reloadAllCmd(true)
		return m, cmd
```

Replace the `case "r":` body:

```go
		case "r":
			// Block r while any source read is already in flight, to avoid stacking
			// duplicate reads of the same source.
			if !m.running && !m.anySourceInflight() {
				var cmd tea.Cmd
				m, cmd = m.reloadAllCmd(true)
				return m, cmd
			}
```

Add the guard helper (in `source.go`):

```go
func (m Model) anySourceInflight() bool {
	for _, v := range m.srcInflight {
		if v {
			return true
		}
	}
	return false
}
```

Note: leave the old `loadCmd`/`dataLoadedMsg` in place for now — other call sites (op completion) still use them until Task 7. `Init` and `r` no longer reference them.

- [ ] **Step 5: Run tests + build**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/source-registry && go test ./internal/tui/ -run 'TestReload' -v && go build ./cmd/gg`
Expected: PASS and a clean build.

- [ ] **Step 6: Manual smoke (optional but recommended)**

```bash
go build -o ./gg ./cmd/gg && ./gg   # open a repo, confirm panels populate, press r to refresh
```

- [ ] **Step 7: Commit**

```bash
git add internal/tui/source.go internal/tui/model.go internal/tui/source_test.go
git commit -m "feat(tui): reloadAll + bootstrap; route startup and r through the registry"
```

---

### Task 7: Op→source mapping; route op completion through the registry

**Files:**
- Modify: `internal/tui/source.go` (add `opAffectedSources`)
- Modify: `internal/tui/model.go` (the `opFinishedMsg` handler tail)
- Modify: `internal/tui/source_test.go`

**Interfaces:**
- Produces: `func opAffectedSources(res engine.Result, hadStash bool) []sourceKey` — returns the sources an op dirtied, or `nil` meaning "all sources" (the safe default). The mapping keys off the op identity available on completion (see note on how the TUI knows which op ran).
- Consumes: existing `opFinishedMsg` tail logic (switch/chain/process/stash/`pendingRefsReload`/`pendingIdentityReload`).

How the TUI knows which op ran: today `pendingRefsReload`/`pendingIdentityReload` are set at the call site (`startOp`) for create-worktree and SetIdentity. Generalize that: replace those two bools with a single `pendingSources []sourceKey` field on the Model (nil = all). Each call site that today sets `pendingRefsReload = true` instead sets `pendingSources = []sourceKey{srcBranches, srcWorktrees}`; the SetIdentity site sets `pendingSources = []sourceKey{srcIdentity}`. Unset (nil) keeps the full-refresh default.

- [ ] **Step 1: Write the failing test**

```go
// internal/tui/source_test.go (append)
func TestOpFinishedRefreshesOnlyPendingSources(t *testing.T) {
	m := newTestModel(t)
	m.pendingSources = []sourceKey{srcBranches, srcWorktrees}
	genB, genS := m.srcGen[srcBranches], m.srcGen[srcStatus]
	nm, _ := m.Update(opFinishedMsg{res: engine.Result{Summary: "done"}})
	mm := nm.(Model)
	if mm.srcGen[srcBranches] != genB+1 {
		t.Error("branches should refresh")
	}
	if mm.srcGen[srcStatus] != genS {
		t.Error("status should NOT refresh for a refs-only op")
	}
	if mm.pendingSources != nil {
		t.Error("pendingSources must reset after consumption")
	}
}

func TestOpFinishedDefaultRefreshesAll(t *testing.T) {
	m := newTestModel(t)
	m.pendingSources = nil // unmapped op
	genS := m.srcGen[srcStatus]
	nm, _ := m.Update(opFinishedMsg{res: engine.Result{Summary: "done"}})
	if nm.(Model).srcGen[srcStatus] != genS+1 {
		t.Error("unmapped op must refresh all sources (incl. status)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/source-registry && go test ./internal/tui/ -run 'TestOpFinished' -v`
Expected: FAIL — `m.pendingSources undefined` (and the handler still calls `loadCmd`).

- [ ] **Step 3: Replace the pending bools with `pendingSources`**

In `internal/tui/model.go`:
- Remove fields `pendingRefsReload bool` and `pendingIdentityReload bool`; add `pendingSources []sourceKey // sources to refresh after this op; nil = all (set at the startOp call site)`.
- At the create-worktree call site (`internal/tui/worktree_popup.go:337`, `m.pendingRefsReload = !switchAfter`): replace with `if !switchAfter { m.pendingSources = []sourceKey{srcBranches, srcWorktrees} }`.
- At the SetIdentity call site (where `pendingIdentityReload` is set — find with `rg -n pendingIdentityReload internal/tui`): replace with `m.pendingSources = []sourceKey{srcIdentity}`.

- [ ] **Step 4: Rewrite the `opFinishedMsg` tail**

Replace the block in `internal/tui/model.go` that currently reads (lines ~1357–1380):

```go
		m.loadGen++ // invalidate any in-flight full load before issuing a refresh
		if m.stashView != nil {
			m.stashView.loading = true
			return m, tea.Batch(m.loadCmd(), m.loadStashListCmd(m.stashView.tag))
		}
		if m.proc != nil {
			return m.proc.finished(m, msg.res, msg.err)
		}
		if refsReload {
			return m, m.reloadRefsCmd(m.statusMsg)
		}
		if idReload {
			return m, m.reloadIdentityCmd(m.statusMsg)
		}
		return m, m.loadCmd()
```

with:

```go
		srcs := m.pendingSources // nil = all
		m.pendingSources = nil
		if m.stashView != nil {
			m.stashView.loading = true
			var cmd tea.Cmd
			m, cmd = m.reloadSourcesCmd(sourcesOrAll([]sourceKey{srcStatus}), true)
			return m, tea.Batch(cmd, m.loadStashListCmd(m.stashView.tag))
		}
		if m.proc != nil {
			return m.proc.finished(m, msg.res, msg.err)
		}
		var cmd tea.Cmd
		m, cmd = m.reloadSourcesCmd(sourcesOrAll(srcs), true)
		return m, cmd
```

Remove the now-dead `refsReload`/`idReload` locals and their assignments earlier in the handler (lines ~1328–1329, ~1342–1344, ~1349–1350). Add the helper to `source.go`:

```go
// sourcesOrAll returns srcs, or every source when srcs is nil (the safe default
// for any op not explicitly mapped — correctness never regresses, only speed).
func sourcesOrAll(srcs []sourceKey) []sourceKey {
	if srcs != nil {
		return srcs
	}
	all := make([]sourceKey, 0, srcCount)
	for s := sourceKey(0); s < srcCount; s++ {
		all = append(all, s)
	}
	return all
}
```

Note on the stash branch: it refreshes `status` (the working tree changed) plus the stash list. If a stash op also moves refs in your codebase, widen the slice — confirm by checking what the old `loadCmd` refreshed for stash ops; `status` matches the common apply/pop/drop case.

- [ ] **Step 5: Run tests + full TUI suite + build**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/source-registry && go test ./internal/tui/ -run 'TestOpFinished' -v && go build ./cmd/gg`
Expected: PASS, clean build.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/source.go internal/tui/model.go internal/tui/worktree_popup.go internal/tui/source_test.go
git commit -m "feat(tui): route op completion through per-source refresh (default all)"
```

---

### Task 8: Per-source spinner + keep-visible-on-`r`; remove dead refs-reload commands

**Files:**
- Modify: `internal/tui/viewstate.go` (per-source spinner in panel titles; replace the `softReload` per-panel glyph)
- Modify: `internal/tui/view.go` (the blank-screen gate and the "⏳ reloading…" status line)
- Modify: `internal/tui/model.go` — add a `ready` flag, set it on first data arrival, reset it in `reRoot`; remove the `softReload` field and its uses.
- Delete from `internal/tui/op.go`: `reloadRefsCmd`, `refsRefreshedMsg`, `reloadIdentityCmd`, `identityRefreshedMsg` and their `case` handlers in `model.go` (dead after Task 7).

**IMPORTANT — do NOT delete `loadCmd`/`dataLoadedMsg`/`loadGen`.** `reRoot` (the repo switcher, `model.go` ~2122) still uses `loadCmd` → `dataLoadedMsg`, and `reRoot`/the `dataLoadedMsg` handler still use `loadGen`. Porting `reRoot` to the registry is deliberately out of scope for Phase A (it is a repo-switch concern, not per-source refresh). So the monolith is reduced to a single remaining caller (`reRoot`); a later phase removes it. This task only removes the refs-reload helpers that Task 7 made dead, plus `softReload`.

**Why the `ready` flag:** Task 6 routed `r` through `reloadAllCmd` which sets `m.loading = true` but not `softReload`. The View's blank gate is `if m.loading && !m.softReload` (view.go) — so `r` currently blanks the whole screen, regressing the soft-reload keep-visible UX. The fix: blank only on the FIRST load (no data yet), keep panels visible on every later refresh. A one-time `m.ready bool` (false until first data arrives, reset in `reRoot`) replaces `softReload` as the blank discriminator.

**Interfaces:**
- Produces: `func (m Model) panelLoading(p panel) bool`; Model field `ready bool`.
- Consumes: `srcLoading`, `srcConsumers`, `commitsLoadingGlyph`, the panel-title and blank-gate rendering.

- [ ] **Step 1: Write the failing test (spinner targeting)**

```go
// internal/tui/source_test.go (append)
func TestManualRefreshShowsConsumerSpinner(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 120, 40
	m, _ = m.reloadSourcesCmd([]sourceKey{srcStatus}, true) // Files + Staged consume status
	out := m.View()
	// The Files panel title should carry the loading glyph while status is loading.
	if !panelTitleHasGlyph(out, "Files", commitsLoadingGlyph) {
		t.Fatal("Files title should show the loading glyph during a manual status refresh")
	}
}
```

Note: `panelTitleHasGlyph` is illustrative — implement the assertion against however the suite already inspects rendered titles (search existing tests with `rg -n "commitsLoadingGlyph|Commits ⏳|titleWithGlyph" internal/tui/*_test.go` and mirror that style). If the suite asserts on a title-builder function rather than full `View()`, test that function instead.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/source-registry && go test ./internal/tui/ -run 'TestManualRefreshShowsConsumerSpinner' -v`
Expected: FAIL — Files title has no glyph.

- [ ] **Step 3: Add per-source spinner to the title builder**

In `internal/tui/viewstate.go`, where a panel title is built (near `commitsLoadingGlyph`, line ~510), add a helper and call it for every panel title:

```go
// panelLoading reports whether any source feeding p is mid manual-refresh, so the
// panel's title shows the ⏳ glyph (the per-panel generalization of the old
// commitsLoading flag).
func (m Model) panelLoading(p panel) bool {
	for s, panels := range srcConsumers {
		if !m.srcLoading[s] {
			continue
		}
		for _, cp := range panels {
			if cp == p {
				return true
			}
		}
	}
	return false
}
```

In the title-building code (viewstate.go ~528), replace the `softReload` term with `m.panelLoading(p)`:
```go
	// was: if m.softReload || (p == panelCommits && m.commitsLoading) {
	if m.panelLoading(p) || (p == panelCommits && m.commitsLoading) {
		base += " " + commitsLoadingGlyph
	}
```

- [ ] **Step 4: Run the spinner test**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/source-registry && go test ./internal/tui/ -run 'TestManualRefreshShowsConsumerSpinner' -v`
Expected: PASS.

- [ ] **Step 5: Keep panels visible on `r` (the `ready` flag) — TDD**

First the failing test:
```go
// internal/tui/source_test.go (append)
func TestReloadAfterFirstDataKeepsPanelsVisible(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 120, 40
	// Simulate first data having arrived (panels populated, ready set).
	m.ready = true
	m.branches = []model.Branch{{Name: "main"}}
	// A manual reload-all (r) marks sources loading but must NOT blank the screen.
	m, _ = m.reloadAllCmd(true)
	out := m.View()
	if strings.Contains(out, "(loading…)") {
		t.Fatal("r after first data must keep panels visible, not blank the screen")
	}
}

func TestInitialLoadBlanksUntilReady(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 120, 40
	m.ready = false
	m, _ = m.reloadAllCmd(true) // startup fan-out, no data yet
	if !strings.Contains(m.View(), "(loading…)") {
		t.Fatal("initial load (no data yet) should show the loading screen")
	}
}
```
(Add `"strings"` to the test imports if not present.)

Then implement:
- Add Model field `ready bool // true once the first data has arrived; gates the initial blank screen (replaces softReload's role)`.
- In the `dataAvailableMsg` handler (model.go), set `m.ready = true` right after the stale-gen check (so the first source to land flips it). Also set `m.ready = true` in the `dataLoadedMsg` handler (the reRoot path) where it currently sets `m.softReload = false`.
- Change the View blank gate (view.go ~2130) from `if m.loading && !m.softReload` to `if m.loading && !m.ready`.
- In `reRoot` (model.go ~2109) replace `m.softReload = false` with `m.ready = false` (a repo switch blanks until the new repo's first data lands).
- In the "⏳ reloading…" status-line block (view.go ~396) replace `if m.softReload && !m.running` with `if m.anySourceLoading() && !m.running` (so a manual refresh still shows the reloading hint).

- [ ] **Step 6: Remove `softReload` and the dead refs-reload commands**

- `rg -n "softReload" internal/tui` — after Step 5 the only matches should be the field declaration and the handler line that cleared it; remove the field (`model.go` ~30) and the `m.softReload = false` clear in the `dataLoadedMsg` handler (it's superseded by `m.ready = true`). Update/remove any `*_test.go` asserting `softReload` (e.g. a soft-reload test) to assert `ready`/`panelLoading` instead.
- Delete `reloadRefsCmd`/`refsRefreshedMsg`/`reloadIdentityCmd`/`identityRefreshedMsg` from `op.go` and their `case` handlers from `model.go` (dead after Task 7). Confirm dead first: `rg -n "reloadRefsCmd|refsRefreshedMsg|reloadIdentityCmd|identityRefreshedMsg" internal/tui` should show only the definitions + their cases (and any tests).
- **Do NOT delete `loadCmd`/`dataLoadedMsg`/`loadGen`** — `reRoot` still uses them (see the IMPORTANT note above). `rg -n "loadCmd|dataLoadedMsg|loadGen" internal/tui` must still show `reRoot` (and its dataLoadedMsg handler) as live readers.
- **Keep `m.loading`** — a derived flag still read by ~10 action guards (`avail.go:14` and the `!m.running && !m.loading` sites). Do NOT delete it.

- [ ] **Step 7: Full suite + race + build + fmt/vet**

Run:
```bash
cd /mnt/t/others/gigagit/.claude/worktrees/source-registry
gofmt -l internal/ && go vet ./internal/tui/... && go build ./cmd/gg && ./test.sh
```
Expected: gofmt prints nothing; vet clean; build clean; `./test.sh` green (unit + e2e). Then `./test.sh race`.

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "feat(tui): per-source spinners + keep-visible reload; remove dead refs-reload commands"
```

---

### Task 9: Docs

**Files:**
- Modify: `CHANGELOG.md` (always)
- Modify: `CLAUDE.md` (package map — `domain` now drives a TUI source registry; note `Conflict` query)
- Modify: `README.md` only if a user-facing surface changed (the `r` behavior is the same; per-source spinners are new but minor — a one-line note is enough).

- [ ] **Step 1: Update CHANGELOG.md**

Add an entry under the current cycle describing: window-by-window (per-source) async refresh; actions refresh only affected panels; manual refresh shows per-panel ⏳; foundation for background auto-refresh (Phase B) and adaptive intervals (Phase C).

- [ ] **Step 2: Update CLAUDE.md package map**

In the `domain` row, note it backs the TUI's per-source refresh registry and exposes the derived `Conflict(st)` query. In `tui`, note the source registry (`source.go`) replaced the monolithic load.

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md CLAUDE.md README.md
git commit -m "docs: data-source registry (Phase A) — changelog + package map"
```

---

## Self-Review

**Spec coverage:**
- Source registry as the one mechanism → Tasks 1, 3, 4, 6. ✓
- Source set + queries + derived data table → Task 3 (loaders), Task 4 (derived: conflict/headTimes/graph/remote-sort). ✓
- Generic `dataAvailableMsg` → Task 1; handler → Task 4. ✓
- Manual spinner vs silent auto → Task 4 (clear on manual), Task 6 (`manual` flag), Task 8 (title rendering). ✓
- Per-source generation + coalescing → Task 1 (fields), Task 3 (gen baked in), Task 6 (bump + inflight), Task 4 (stale drop). ✓
- `reloadAll`/startup through the same path → Task 6. ✓
- Op→affected-source mapping, default all → Task 7. ✓
- Selection by identity → Task 5. ✓
- Observability free via existing spans → no task needed (loaders use gated queries that already emit spans); noted. ✓
- Feed wrapped as a source, its gen composed not replaced → Task 3 (`srcFeed` = `LoadInitial`), Task 4 (branches handler keeps the upstream re-walk latch; scope reloads untouched). ✓
- Retire monolith → Task 8. ✓
- Tests enumerated in spec → covered across Tasks 1,3,4,5,6,7,8. ✓

**Placeholder scan:** No "TBD/TODO". Two illustrative test helpers (`newTestModel*`, `panelTitleHasGlyph`) are flagged with explicit "confirm the real name with `rg`" instructions because the exact helper names live in the existing suite — this is verification guidance, not a content gap.

**Type consistency:** `sourceKey` constants, `dataAvailableMsg` fields, `statusPayload`/`worktreesPayload`/`feedPayload`, `srcGen`/`srcInflight`/`srcLoading`, `pendingSources`, `reloadSourcesCmd`/`reloadAllCmd`/`sourcesOrAll`/`opAffectedSources`/`anySourceInflight`/`panelLoading`/`panelSelKey`/`restorePanelSel`/`rowKeyAt` are used consistently across tasks. `engine.Result` (in Task 7 tests) matches the existing `opFinishedMsg.res` type.

**Behavioral-preservation blockers (found in advisor review, resolved in-plan):**
1. **Legacy `m.loading` gate** — read by `avail.go:14` and ~10 `!m.running && !m.loading` action sites, not just `r`. Kept alive as a derived flag: set in `reloadSourcesCmd` (Task 6), recomputed in the `dataAvailableMsg` handler (Task 4), explicitly NOT deleted (Task 8). Regression test `TestManualReloadDrivesLegacyLoadingFlag` (Task 6).
2. **Feed double-writer race on startup** — `srcFeed` (`LoadInitial`) and the `srcBranches`-triggered upstream re-walk fanned out concurrently and wrote `m.commits`/`feed` with incompatible generation schemes. Serialized via `maybeFeedUpstreamRewalk` (Task 4), whose `!m.srcInflight[srcFeed]` guard defers the re-walk to `srcFeed`'s arrival when branches lands first. Regression test `TestBranchesDefersFeedRewalkWhileFeedInflight` (Task 4). On `r` the scope is already latched so the re-walk never fires — no race there.
3. **Nil-map panic** — `reloadSourcesCmd` lazy-inits the three registry maps so a `Model{}` literal in a test can't panic (Task 6).

**Open verification points the executor must resolve against the live code (each flagged inline):**
1. Exact test-model helper names (`newTestModel`, `newTestModelWithRepo`, `newTestService`).
2. Which slice each panel indexes in `panelLen` (drives `panelSelKey` correctness for Files/Staged/Reflog).
3. `svc.Reflog` zero-limit behavior vs `loadCmd`'s `defaultReflogLimit`.
4. The exact title-building site/style in `viewstate.go` for the spinner append.
5. The SetIdentity `startOp` call site for the `pendingSources` swap.
