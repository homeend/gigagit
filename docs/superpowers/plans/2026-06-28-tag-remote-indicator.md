# Tag pushed-state indicator — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a `▲` marker in the Tags panel for tags that exist on the remote, populated by an opt-in manual `.`-menu action and an opt-in background `[refresh] remote_tags` interval.

**Architecture:** A new `git ls-remote --tags` verb feeds a domain `RemoteTags` query (using a non-recording `queryQuiet` so offline background polls don't flood `errors.log`). The TUI stores the result as a name set, renders `▲`, refreshes it via a Tags `.`-menu action and a synthetic background `refreshItem{isRemoteTags}` (mirroring the existing `fetch` item — kept synthetic so the all-source `reloadAll`/`r` sweep never triggers a network call), and keeps the marker honest with optimistic add/remove on push/delete-remote completion.

**Tech Stack:** Go 1.26, Bubble Tea TUI, `internal/{git,domain,tui,config}`.

## Global Constraints

- A git verb is ONE git invocation, built with `gitcmd`, run via `r.Runner.Run`. (`internal/git`)
- `internal/tui` and `internal/cli` MUST NOT import `internal/git` — reach git via `internal/domain` (archtest-guarded).
- Background remote-tag failures MUST be silent — they must NOT reach `observ.NoteFailure`. Manual failures surface on the status line only.
- Remote resolution (origin-or-first) lives in `internal/domain`, never in the TUI.
- Comparison is by tag NAME (v1). No hash-mismatch detection.
- The marker is `▲` (U+25B2), appended after the existing row. Local-only and not-yet-checked both render blank.
- Adding the config field requires only a `settingDoc` in `template.go` (guarded by `TestSettingDocsCoverAllFields`); no per-command edits.
- TDD: write the failing test, see it fail, implement, see it pass, commit. Use a real `git` in a `t.TempDir()` (see `newRepo`/`newTestRepo`) or `FakeRunner` for argv assertions.
- No CLI surface changes → NO `agentskill.Version` bump.

---

### Task 1: git verb `RemoteTags` + `ParseRemoteTags`

**Files:**
- Modify: `internal/git/repo.go` (add `RemoteTags` verb near `Tags`, ~line 81)
- Create: `internal/git/remote_tags_parse.go` (parser)
- Test: `internal/git/remote_tags_test.go`

**Interfaces:**
- Produces: `func (r *Repo) RemoteTags(ctx context.Context, remote string) (map[string]bool, error)` — set of bare tag names present on `remote`.
- Produces: `func ParseRemoteTags(out []byte) map[string]bool` — pure parser.

`git ls-remote --tags <remote>` prints lines like:
```
<sha>\trefs/tags/v1.2.0
<sha>\trefs/tags/v1.2.0^{}
```
The `^{}` row is the peeled commit of an annotated tag — drop it. Extract the bare
name after `refs/tags/`.

- [ ] **Step 1: Write the failing parser test**

In `internal/git/remote_tags_test.go`:
```go
package git

import (
	"reflect"
	"testing"
)

func TestParseRemoteTags(t *testing.T) {
	out := []byte(
		"aaaaaaaaaaaa\trefs/tags/v1.0.0\n" +
			"bbbbbbbbbbbb\trefs/tags/v1.1.0\n" +
			"cccccccccccc\trefs/tags/v1.1.0^{}\n" + // peeled — must be dropped (still name v1.1.0)
			"dddddddddddd\trefs/tags/release/2024\n")
	got := ParseRemoteTags(out)
	want := map[string]bool{"v1.0.0": true, "v1.1.0": true, "release/2024": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseRemoteTags = %v, want %v", got, want)
	}
}

func TestParseRemoteTagsEmpty(t *testing.T) {
	if got := ParseRemoteTags(nil); len(got) != 0 {
		t.Fatalf("empty input should yield empty set, got %v", got)
	}
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./internal/git/ -run TestParseRemoteTags`
Expected: FAIL — `undefined: ParseRemoteTags`.

- [ ] **Step 3: Implement the parser**

In `internal/git/remote_tags_parse.go`:
```go
package git

import "strings"

// ParseRemoteTags extracts the set of bare tag names from `git ls-remote --tags`
// output. Each line is "<sha>\trefs/tags/<name>"; the "^{}" peeled-dereference
// rows of annotated tags are dropped (they carry the same name, already added).
func ParseRemoteTags(out []byte) map[string]bool {
	const prefix = "refs/tags/"
	names := map[string]bool{}
	for _, ln := range strings.Split(string(out), "\n") {
		tab := strings.IndexByte(ln, '\t')
		if tab < 0 {
			continue
		}
		ref := strings.TrimSpace(ln[tab+1:])
		if !strings.HasPrefix(ref, prefix) {
			continue
		}
		name := strings.TrimSuffix(ref[len(prefix):], "^{}")
		if name != "" {
			names[name] = true
		}
	}
	return names
}
```

- [ ] **Step 4: Run it, verify it passes**

Run: `go test ./internal/git/ -run TestParseRemoteTags`
Expected: PASS.

- [ ] **Step 5: Write the failing verb test (real git, two repos)**

Append to `internal/git/remote_tags_test.go`. Use the existing `newRepo`/helpers
in this package (check `repo_test.go` / `sync_test.go` for the exact helper names
— mirror how `TestPruneRemotes` / push tests set up a bare remote and push). The
test: create a repo, make a commit, create tag `v1`, push it to a local bare
remote, create a local-only tag `v2`, then assert `RemoteTags(ctx, "origin")`
returns `{"v1": true}` (and NOT `v2`).

```go
func TestRemoteTagsListsPushedOnly(t *testing.T) {
	// Mirror the bare-remote setup used by sync_test.go push tests.
	// 1. dir := newRepo(t); commit something.
	// 2. create a bare remote, `git remote add origin <bare>`.
	// 3. CreateTag v1 (lightweight is fine); push v1 to origin.
	// 4. CreateTag v2 (do NOT push).
	// 5. got, err := r.RemoteTags(ctx, "origin"); require no err.
	// 6. assert got["v1"] && !got["v2"].
}
```
(Write the concrete body using this package's real helpers; keep it a real-git
test, not FakeRunner, so the ls-remote parse is exercised end-to-end.)

- [ ] **Step 6: Run it, verify it fails**

Run: `go test ./internal/git/ -run TestRemoteTags`
Expected: FAIL — `r.RemoteTags undefined`.

- [ ] **Step 7: Implement the verb**

In `internal/git/repo.go`, after `Tags` (~line 81):
```go
// RemoteTags returns the set of bare tag names that exist on the named remote,
// via one `git ls-remote --tags <remote>`. This is a NETWORK call. The "^{}"
// peeled rows of annotated tags are folded into their base name by the parser.
func (r *Repo) RemoteTags(ctx context.Context, remote string) (map[string]bool, error) {
	argv := gitcmd.New("ls-remote").Arg("--tags", remote).ToArgv()
	res, err := r.Runner.Run(ctx, "git ls-remote (tags)", argv)
	if err != nil {
		return nil, err
	}
	return ParseRemoteTags([]byte(res.Stdout)), nil
}
```

- [ ] **Step 8: Run it, verify it passes; run the package**

Run: `go test ./internal/git/ -run TestRemoteTags && go test ./internal/git/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/git/remote_tags_parse.go internal/git/remote_tags_test.go internal/git/repo.go
git commit -m "feat(git): RemoteTags verb + ParseRemoteTags (ls-remote --tags)"
```

---

### Task 2: domain `queryQuiet` + `RemoteTags` query

**Files:**
- Modify: `internal/domain/query.go` (add `queryQuiet`; add `RemoteTags` method)
- Test: `internal/domain/remote_tags_test.go`

**Interfaces:**
- Consumes: `git.Repo.RemoteTags`, the existing `s.repo.RemoteNames(ctx)` verb.
- Produces: `func (s *Service) RemoteTags(ctx context.Context) (map[string]bool, error)` — resolves the remote internally (origin-or-first; empty set + nil error when no remote), runs under `queryQuiet` (NO failure recording).
- Produces: `func queryQuiet[T any](ctx, s, key, fn) (T, error)` — same as `query` minus `observ.NoteFailure`.

Note: `RemoteNames` is on `*git.Repo`. Confirm `s.repo` exposes it (the
`GitOps`/repo type behind the Service). If the Service's repo interface doesn't
list `RemoteNames`/`RemoteTags`, add them to that interface — search for where
`Tags`/`RemoteBranches` are declared on it and add alongside.

- [ ] **Step 1: Write the failing test**

`internal/domain/remote_tags_test.go` — use the domain package's existing test
helper that builds a `*Service` over a real repo (search `query_test.go` /
`*_test.go` for `newService`/`newTestService` style helpers). Three cases:
```go
// 1. No remote configured → RemoteTags returns empty map, nil error.
// 2. With origin + a pushed tag v1 and local-only v2 → returns {"v1":true}.
// 3. queryQuiet does NOT record a failure: point the service at a bogus remote
//    name so ls-remote errors, drain observ.SessionFailures() before and after,
//    assert the count did not increase (the error is returned, not recorded).
```
For case 3, consult `internal/observ` for the session-failure accessor
(`SessionFailures()` per CLAUDE.md) and any reset/seam-control used by existing
failure-seam tests (grep `NoteFailure`/`SessionFailures` in `*_test.go`).

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./internal/domain/ -run TestRemoteTags`
Expected: FAIL — `s.RemoteTags undefined`.

- [ ] **Step 3: Implement `queryQuiet` and `RemoteTags`**

In `internal/domain/query.go`, add next to `query`:
```go
// queryQuiet is query without the failure seam: it runs fn under a Read
// reservation + singleflight but does NOT record errors to observ. Use it for
// opt-in NETWORK reads (e.g. RemoteTags) where a recurring background failure
// (offline) would otherwise flood errors.log every interval. The error is still
// returned so a manual caller can surface it.
func queryQuiet[T any](ctx context.Context, s *Service, key string, fn func(context.Context) (T, error)) (T, error) {
	v, err := s.flight.Do(key, func() (any, error) {
		res, e := s.gateFor(ctx).Acquire(ctx, repogate.Read, "read "+key)
		if e != nil {
			return nil, e
		}
		defer res.Release()
		return fn(ctx)
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return v.(T), nil
}

// RemoteTags returns the set of tag names present on the default remote (origin
// if configured, else the first remote). NETWORK read; routed through queryQuiet
// so background polls never spam the failure seam. No remote → empty set, nil.
func (s *Service) RemoteTags(ctx context.Context) (map[string]bool, error) {
	return queryQuiet(ctx, s, "remote-tags", func(ctx context.Context) (map[string]bool, error) {
		names, err := s.repo.RemoteNames(ctx)
		if err != nil {
			return nil, err
		}
		remote := pickDefaultRemote(names)
		if remote == "" {
			return map[string]bool{}, nil
		}
		return s.repo.RemoteTags(ctx, remote)
	})
}

// pickDefaultRemote returns "origin" if present, else the first remote, else "".
func pickDefaultRemote(names []string) string {
	for _, n := range names {
		if n == "origin" {
			return "origin"
		}
	}
	if len(names) > 0 {
		return names[0]
	}
	return ""
}
```
If `s.repo` is an interface, add `RemoteNames(context.Context) ([]string, error)`
and `RemoteTags(context.Context, string) (map[string]bool, error)` to it.

- [ ] **Step 4: Run it, verify it passes; run the package**

Run: `go test ./internal/domain/ -run TestRemoteTags && go test ./internal/domain/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/query.go internal/domain/remote_tags_test.go
git commit -m "feat(domain): RemoteTags query via non-recording queryQuiet (origin-or-first)"
```

---

### Task 3: TUI — store + `▲` rendering

**Files:**
- Modify: `internal/tui/model.go` (add field `remoteTagNames map[string]bool` to the Model struct)
- Modify: `internal/tui/tags_view.go` (`tagRows` appends `▲`)
- Test: `internal/tui/tags_remote_marker_test.go`

**Interfaces:**
- Produces: `m.remoteTagNames` (nil until first lookup) read by `tagRows`.

- [ ] **Step 1: Write the failing render test**

`internal/tui/tags_remote_marker_test.go`:
```go
package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestTagRowsRemoteMarker(t *testing.T) {
	m := Model{
		tags: []model.Tag{
			{Name: "v1.0.0", Target: "aaaaaaa", Annotated: true},
			{Name: "v2.0.0", Target: "bbbbbbb", Annotated: true},
		},
		remoteTagNames: map[string]bool{"v1.0.0": true},
	}
	rows := m.tagRows()
	if !strings.Contains(rows[0], "▲") {
		t.Errorf("pushed tag row must carry ▲: %q", rows[0])
	}
	if strings.Contains(rows[1], "▲") {
		t.Errorf("local-only tag row must not carry ▲: %q", rows[1])
	}
}

func TestTagRowsNoMarkerWhenUnchecked(t *testing.T) {
	m := Model{tags: []model.Tag{{Name: "v1.0.0", Target: "aaaaaaa"}}} // remoteTagNames nil
	if strings.Contains(m.tagRows()[0], "▲") {
		t.Errorf("unchecked tag must not carry ▲: %q", m.tagRows()[0])
	}
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./internal/tui/ -run TestTagRows`
Expected: FAIL — unknown field `remoteTagNames`.

- [ ] **Step 3: Add the field and render the marker**

In `internal/tui/model.go`, add to the Model struct (near other tag/source
state):
```go
	remoteTagNames map[string]bool // tag names known on the default remote (▲); nil until a lookup runs
```
In `internal/tui/tags_view.go`, update `tagRows`:
```go
func (m Model) tagRows() []string {
	rows := make([]string, len(m.tags))
	for i, t := range m.tags {
		row := tagKindMark(t) + " " + t.Name + "  " + shortHash(t.Target)
		if t.Subject != "" {
			row += "  " + t.Subject
		}
		if m.remoteTagNames[t.Name] {
			row += "  ▲"
		}
		rows[i] = row
	}
	return rows
}
```
(`m.remoteTagNames[t.Name]` on a nil map is a valid false — no guard needed.)

- [ ] **Step 4: Run it, verify it passes**

Run: `go test ./internal/tui/ -run TestTagRows`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/tags_view.go internal/tui/tags_remote_marker_test.go
git commit -m "feat(tui): render ▲ on tags present on the remote"
```

---

### Task 4: TUI — manual refresh command, `.`-menu action, optimistic updates

**Files:**
- Create: `internal/tui/remote_tags.go` (`remoteTagsCmd`, `remoteTagsMsg`, handler helper)
- Modify: `internal/tui/model.go` (handle `remoteTagsMsg`; apply optimistic add/remove in `opFinishedMsg`; add pending fields)
- Modify: `internal/tui/tags_actions.go` (new `tagRefreshRemoteRow` action; set pending optimistic name in push/delete-remote run funcs)
- Modify: the tag action-menu assembly site (where `tagPushRow` etc. are collected — grep for `tagPushRow(` to find it)
- Test: `internal/tui/remote_tags_test.go`

**Interfaces:**
- Consumes: `m.svc.RemoteTags(ctx)` (Task 2).
- Produces: `remoteTagsCmd(ctx, manual bool) tea.Cmd`, `remoteTagsMsg{names map[string]bool; err error; manual bool}`.
- Produces: pending fields `m.pendingRemoteTagSet string` / `m.pendingRemoteTagUnset string` applied on op success.

- [ ] **Step 1: Write the failing tests**

`internal/tui/remote_tags_test.go`:
```go
package tui

import "testing"

// Manual remoteTagsMsg stores the set.
func TestRemoteTagsMsgStoresSet(t *testing.T) {
	m := Model{}
	u, _ := m.Update(remoteTagsMsg{names: map[string]bool{"v1": true}, manual: true})
	m = u.(Model)
	if !m.remoteTagNames["v1"] {
		t.Fatal("manual remoteTagsMsg should store the name set")
	}
}

// Manual error surfaces on the status line and leaves the set unchanged.
func TestRemoteTagsMsgManualErrorStatus(t *testing.T) {
	m := Model{remoteTagNames: map[string]bool{"old": true}}
	u, _ := m.Update(remoteTagsMsg{err: errTestRemote, manual: true})
	m = u.(Model)
	if m.statusMsg == "" {
		t.Fatal("manual error should set a status message")
	}
	if !m.remoteTagNames["old"] {
		t.Fatal("error must not clear the existing set")
	}
}

// Optimistic add on PushTag success; remove on DeleteRemoteTag success.
func TestOptimisticRemoteTagAdd(t *testing.T) {
	m := Model{pendingRemoteTagSet: "v9"}
	m = m.applyPendingRemoteTag() // success-path helper
	if !m.remoteTagNames["v9"] {
		t.Fatal("push success should add the tag to the remote set")
	}
}
func TestOptimisticRemoteTagRemove(t *testing.T) {
	m := Model{remoteTagNames: map[string]bool{"v9": true}, pendingRemoteTagUnset: "v9"}
	m = m.applyPendingRemoteTag()
	if m.remoteTagNames["v9"] {
		t.Fatal("delete-remote success should drop the tag from the remote set")
	}
}

var errTestRemote = errString("boom")

type errString string

func (e errString) Error() string { return string(e) }
```

- [ ] **Step 2: Run it, verify it fails**

Run: `go test ./internal/tui/ -run "TestRemoteTags|TestOptimistic"`
Expected: FAIL — undefined `remoteTagsMsg`, `applyPendingRemoteTag`, fields.

- [ ] **Step 3: Implement command, message, handler, pending fields**

Create `internal/tui/remote_tags.go`:
```go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

// remoteTagsMsg carries the result of a remote-tag lookup. manual=true means a
// user-initiated refresh (errors go to the status line); false means a silent
// background poll (errors discarded — see queryQuiet's no-record contract).
type remoteTagsMsg struct {
	names  map[string]bool
	err    error
	manual bool
}

// remoteTagsCmd runs the (network) remote-tag lookup off the UI thread. Shared by
// the manual .-menu action and the background scheduler lane.
func (m Model) remoteTagsCmd(ctx context.Context, manual bool) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		names, err := svc.RemoteTags(ctx)
		return remoteTagsMsg{names: names, err: err, manual: manual}
	}
}

// applyPendingRemoteTag folds a pending optimistic add/remove into the set on op
// success, then clears the pending fields. Lazy-inits the map for an add.
func (m Model) applyPendingRemoteTag() Model {
	if m.pendingRemoteTagSet != "" {
		if m.remoteTagNames == nil {
			m.remoteTagNames = map[string]bool{}
		}
		m.remoteTagNames[m.pendingRemoteTagSet] = true
		m.pendingRemoteTagSet = ""
	}
	if m.pendingRemoteTagUnset != "" {
		delete(m.remoteTagNames, m.pendingRemoteTagUnset)
		m.pendingRemoteTagUnset = ""
	}
	return m
}
```
In `internal/tui/model.go` Model struct, add:
```go
	pendingRemoteTagSet   string // tag to add to remoteTagNames on next op success (optimistic push)
	pendingRemoteTagUnset string // tag to drop from remoteTagNames on next op success (optimistic delete-remote)
```
Add a `case remoteTagsMsg:` to the message switch (near `bgFetchDoneMsg`):
```go
	case remoteTagsMsg:
		// Background completion frees the single lane (mirrors bgFetchDoneMsg;
		// remote-tags completes via this message, not dataAvailableMsg).
		if !msg.manual && m.bgBusy && m.bgActiveItem.isRemoteTags {
			m.bgBusy = false
		}
		if msg.err != nil {
			if msg.manual {
				m.statusMsg = "remote tags: " + msg.err.Error()
			}
			return m, nil // background: silent (queryQuiet did not record it)
		}
		if !msg.manual {
			m = m.recordDuration(remoteTagsItem, msg.dur) // dur added in Task 5; omit if not yet present
		}
		m.remoteTagNames = msg.names
		return m, nil
```
NOTE: `remoteTagsItem` and `msg.dur` are introduced in Task 5. For THIS task,
omit the `recordDuration` line and the `dur` field; add them in Task 5 when the
scheduler item exists. Keep `remoteTagsMsg` without `dur` here.

In the `opFinishedMsg` success branch (after `msg.res.Summary` handling, before
the pending-source reset around model.go:1522), apply the optimistic update:
```go
		m = m.applyPendingRemoteTag()
```
And on the ERROR branch, clear the pending fields so a failed push/delete does
not later mutate the set:
```go
		m.pendingRemoteTagSet = ""
		m.pendingRemoteTagUnset = ""
```
(Place the clear so it runs on the error path; the apply runs only on success.)

Create `internal/tui/tags_actions.go` action `tagRefreshRemoteRow`:
```go
// tagRefreshRemoteRow offers "Refresh remote status" on the Tags panel: run a
// one-shot ls-remote and annotate every tag with ▲. Available whenever the panel
// is focused and non-empty; operates on the whole list, not the selected row.
func (m Model) tagRefreshRemoteRow() (actionRow, bool) {
	if m.focus != panelTags || !m.opsIdle() || len(m.tags) == 0 {
		return actionRow{}, false
	}
	return actionRow{
		id:    "tag-refresh-remote",
		label: "Refresh remote status",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.remoteTagsCmd(context.Background(), true)
		},
	}, true
}
```
(Add the `context` import to `tags_actions.go` if absent.)

Set the optimistic pending name in the existing run funcs:
```go
// tagPushRow run:
run: func(m Model) (tea.Model, tea.Cmd) {
	m.pendingRemoteTagSet = name
	return m.startOp(engine.PushTag{Name: name})
},
// tagDeleteRemoteRow run:
run: func(m Model) (tea.Model, tea.Cmd) {
	m.pendingRemoteTagUnset = name
	return m.startOp(engine.DeleteRemoteTag{Tag: name})
},
```
Register `tagRefreshRemoteRow` in the tag action-menu builder (grep
`tagPushRow(` to find where the tag rows are appended; add it alongside).

- [ ] **Step 4: Run the tests, verify they pass; run the package**

Run: `go test ./internal/tui/ -run "TestRemoteTags|TestOptimistic" && go test ./internal/tui/`
Expected: PASS. (`go test ./internal/tui/` may take ~20s; allow it.)

- [ ] **Step 5: Commit**

```bash
git add internal/tui/remote_tags.go internal/tui/remote_tags_test.go internal/tui/model.go internal/tui/tags_actions.go
git commit -m "feat(tui): manual remote-tag refresh action + optimistic push/delete updates"
```

---

### Task 5: config field + background scheduler item

**Files:**
- Modify: `internal/config/config.go` (`RefreshConfig.RemoteTags`; `overlayRefresh`)
- Modify: `internal/config/template.go` (`settingDoc`)
- Modify: `internal/tui/refresh.go` (`isRemoteTags`, `remoteTagsItem`, `scheduledItems`, the four switch helpers, `refreshTick` drain, `bgRefreshHint`)
- Modify: `internal/tui/remote_tags.go` (add `dur time.Duration` to `remoteTagsMsg`; measure in `remoteTagsCmd`) and `internal/tui/model.go` (record duration in the `remoteTagsMsg` handler — the line deferred from Task 4)
- Test: `internal/config/config_test.go` (overlay), `internal/tui/refresh_remote_tags_test.go`

**Interfaces:**
- Produces: `config.RefreshConfig.RemoteTags int` (`toml:"remote_tags"`).
- Produces: `remoteTagsItem = refreshItem{isRemoteTags: true}`.

- [ ] **Step 1: Write the failing config + scheduler tests**

In `internal/config/config_test.go` add (mirror an existing overlay test for
`Fetch`):
```go
func TestOverlayRefreshRemoteTags(t *testing.T) {
	// a repo layer setting remote_tags = 30 overlays onto a zero base.
	// (Follow the existing overlayRefresh test pattern in this file.)
}
```
In `internal/tui/refresh_remote_tags_test.go`:
```go
package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/config"
)

func TestRemoteTagsScheduledItem(t *testing.T) {
	if refreshTomlKey(remoteTagsItem) != "remote_tags" {
		t.Fatalf("toml key = %q, want remote_tags", refreshTomlKey(remoteTagsItem))
	}
	cfg := config.RefreshConfig{Enabled: true, RemoteTags: 30, MinSeconds: 10}
	secs, on := scheduledInterval(cfg, remoteTagsItem)
	if !on || secs != 30 {
		t.Fatalf("scheduledInterval = (%d,%v), want (30,true)", secs, on)
	}
	// default 0 → off
	if _, on := scheduledInterval(config.RefreshConfig{Enabled: true}, remoteTagsItem); on {
		t.Fatal("remote_tags default 0 must be off")
	}
	// present in the scheduled set
	found := false
	for _, it := range scheduledItems {
		if it.isRemoteTags {
			found = true
		}
	}
	if !found {
		t.Fatal("remoteTagsItem must be in scheduledItems")
	}
}
```

- [ ] **Step 2: Run them, verify they fail**

Run: `go test ./internal/config/ -run TestOverlayRefreshRemoteTags ./internal/tui/ -run TestRemoteTagsScheduledItem`
Expected: FAIL — unknown field `RemoteTags` / undefined `remoteTagsItem`.

- [ ] **Step 3: Add the config field**

In `internal/config/config.go` `RefreshConfig`, after `Fetch`:
```go
	RemoteTags int `toml:"remote_tags"` // seconds between background remote-tag (ls-remote) lookups; 0 = off
```
In `overlayRefresh`, add the field copy mirroring the others (only-if-set
semantics consistent with the existing interval fields):
```go
	if src.RemoteTags != 0 {
		dst.RemoteTags = src.RemoteTags
	}
```
(Match the exact style already used in `overlayRefresh` — read it and copy the
pattern; intervals there use the set-field overlay rule.)

In `internal/config/template.go`, add to the `[refresh]` settingDocs (after the
`fetch` row at ~line 58):
```go
	{"refresh", "remote_tags", 0, "seconds between background remote-tag (ls-remote) lookups; 0 = off"},
```

- [ ] **Step 4: Wire the scheduler item in `internal/tui/refresh.go`**

```go
// refreshItem struct: add field
	isRemoteTags bool

// new var beside fetchItem
var remoteTagsItem = refreshItem{isRemoteTags: true}

// scheduledItems: append remoteTagsItem
var scheduledItems = []refreshItem{
	{source: srcStatus}, {source: srcBranches}, {source: srcRemotes},
	{source: srcWorktrees}, {source: srcTags}, {source: srcReflog},
	{source: srcFeed}, fetchItem, remoteTagsItem,
}

// refreshIntervalFor: before the source switch
	if it.isRemoteTags {
		return cfg.RemoteTags
	}

// refreshTomlKey: before the source switch
	if it.isRemoteTags {
		return "remote_tags"
	}

// setRefreshIntervalField: before the source switch
	if it.isRemoteTags {
		cfg.RemoteTags = secs
		return
	}
```
In `refreshTick`, after the `isFetch && m.svc == nil` guard and before the
`bgCancel` init, add the same nil-svc guard for remote tags, then in the
launch block (where `if it.isFetch { ... }` runs) add an `isRemoteTags` arm:
```go
	if it.isRemoteTags && m.svc == nil {
		return m, nil
	}
	...
	if it.isRemoteTags {
		return m, m.remoteTagsCmd(m.bgCtx, false)
	}
```
(The lane is freed by the `remoteTagsMsg` handler from Task 4; no srcInflight
bookkeeping since it is not a sourceKey.)

In `bgRefreshHint`, name the item:
```go
	name := "fetch"
	switch {
	case m.bgActiveItem.isRemoteTags:
		name = "remote tags"
	case !m.bgActiveItem.isFetch:
		name = sourceNames[m.bgActiveItem.source]
	}
```

- [ ] **Step 5: Add duration measurement (the deferred Task 4 line)**

In `internal/tui/remote_tags.go`, add `dur time.Duration` to `remoteTagsMsg` and
measure it in `remoteTagsCmd` (mirror `bgFetchCmd`: `start := time.Now()` …
`dur: time.Since(start)`). In the `remoteTagsMsg` handler in `model.go`, add the
deferred line for background reads:
```go
		if !msg.manual {
			m = m.recordDuration(remoteTagsItem, msg.dur)
		}
```

- [ ] **Step 6: Run the tests, verify they pass; build + vet**

Run: `go test ./internal/config/ ./internal/tui/ && go vet ./internal/config/ ./internal/tui/`
Expected: PASS. (`go test ./internal/tui/` ~20s.)

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/config/template.go internal/tui/refresh.go internal/tui/refresh_remote_tags_test.go internal/tui/remote_tags.go internal/tui/model.go
git commit -m "feat(config,tui): opt-in [refresh] remote_tags background scheduler item"
```

---

### Task 6: docs, help/footer, memory

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`
- Modify: `internal/tui/help.go` and the footer (advertise the `.`-menu action — per the "advertise features in help AND footer" convention; the action is in the `.` menu, so confirm whether help lists tag `.` actions and add a line if so)
- Create: memory file under `/home/homeend/.claude/projects/-mnt-t-others-gigagit/memory/` + index line in `MEMORY.md`

- [ ] **Step 1: CHANGELOG** — add an entry under the current unreleased/top section describing the `▲` pushed-tag indicator, the Tags `.` "Refresh remote status" action, and the opt-in `[refresh] remote_tags` interval (network, default off).

- [ ] **Step 2: README** — in the Tags section (or config `[refresh]` table), document the `▲` marker semantics (on-remote vs local-only/unchecked), the `.`-menu refresh, and `[refresh] remote_tags` (opt-in, network). Note name-based v1 comparison and origin-or-first remote.

- [ ] **Step 3: CLAUDE.md** — update the `config` package row's `[refresh]` key list to include `remote_tags`, and the `tui` `refresh.go` description to mention the synthetic `remoteTagsItem` lane alongside `fetch`. Update the `git` row only if you want to note the `RemoteTags` verb (optional).

- [ ] **Step 4: help/footer** — grep `help.go` for the tag-panel action list; if tag `.` actions are enumerated there, add "Refresh remote status". Per convention, keep help and footer in sync (footer truncates — short label).

- [ ] **Step 5: memory** — create `tag-remote-indicator-feature.md` (type: project) summarizing: `▲` = on remote; synthetic `remoteTagsItem` (NOT a sourceKey, mirrors fetch); `queryQuiet` (no failure-seam recording) for the network read; origin-or-first remote; optimistic add/remove on push/delete; opt-in `[refresh] remote_tags`. Link `[[background-auto-refresh-feature]]`, `[[refresh-config-editor-feature]]`, `[[local-remote-tip-markers-feature]]`. Add one index line to `MEMORY.md`.

- [ ] **Step 6: Build + full test**

Run: `go build ./cmd/gg && ./test.sh unit`
Expected: build ok, unit tests pass.

- [ ] **Step 7: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md internal/tui/help.go
git commit -m "docs: tag pushed-state indicator (▲) — changelog, readme, claude.md, help"
```
(Memory files live outside the repo; they are not committed.)

---

## Self-review notes

- **Spec coverage:** marker rendering (T3), manual refresh (T4), scheduler (T5),
  failure-seam silence via `queryQuiet` (T2), optimistic push+delete (T4),
  origin-or-first resolution (T2), config + settingDoc (T5), docs (T6). All
  covered.
- **No network on startup/`r`:** guaranteed by modeling remote-tags as a
  synthetic `refreshItem`, never a `sourceKey` (T5) — so the `0..srcCount`
  sweep in `reloadAllCmd` cannot reach it.
- **Type consistency:** `remoteTagsMsg` gains `dur` in T5 (the T4 handler is
  written without it and the `recordDuration` line is explicitly deferred to T5
  to avoid a forward reference). `engine.DeleteRemoteTag` uses field `Tag`;
  `engine.PushTag` uses field `Name` (verified in tags_actions.go).
- **Repo interface:** T2 flags that `RemoteNames`/`RemoteTags` may need adding to
  the Service's repo interface if it is not the concrete `*git.Repo`.
