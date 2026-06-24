# Local vs remote branch-tip markers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In the Commits panel, mark each commit that is the tip of a local branch (`■`) and/or the tip of its tracked remote (`▲`), so divergence between a local branch and its upstream reads at a glance — no ahead/behind numbers.

**Architecture:** Stage A is pure `internal/tui`: the commit identity gains marker state derived by joining `model.Commit.Refs` against the set of local-branch upstreams (`m.branches`), rendered as a fixed-width prefix on the identity column. Stage B walks those upstreams into the commit feed (`LogScope.Upstreams`) so the remote tip appears as its own row when the local branch is behind/diverged.

**Tech Stack:** Go 1.26, Bubble Tea / lipgloss TUI, `git` via `gitcmd`/`gitexec`, table-driven tests with a real `git` in `t.TempDir()` and `FakeRunner` for argv assertions.

## Global Constraints

- Module `github.com/gigagit/gg`, Go 1.26.
- A git verb is one invocation; build argv with `gitcmd`, run via `r.Runner`.
- `internal/tui` must NOT import `internal/git` (archtest-guarded) — reach git through `internal/domain`.
- TDD: write the failing test first, watch it fail, implement minimally, watch it pass, commit.
- Run `./test.sh unit` for fast loops; `./test.sh race` before the human merges.
- `main` is the trunk; this work lives on branch `feat/tip-markers` (worktree `.claude/worktrees/tip-markers`).
- Marker glyphs: local tip `■` (U+25A0), remote tip `▲` (U+25B2). Square/triangle distinction is the contract; exact glyphs may be tuned live.
- No ahead/behind numbers anywhere. No CLI surface. `internal/commitgraph` lane engine and the lane `●`/`◇` glyphs are untouched.

---

## Stage A — tip markers (pure TUI)

### Task A1: Marker state in `commitIdent` + upstream-aware `commitIdentOf`

Add remote-tip detection and marker state to the identity model. Signature of `commitIdentOf` changes to take a tracked-upstream map; all call sites pass `nil` for now (behavior unchanged) so the build stays green. Markers are not yet rendered — that is Task A2.

**Files:**
- Modify: `internal/tui/commit_ident.go` (struct `commitIdent` ~lines 46-51; func `commitIdentOf` ~lines 53-79)
- Modify call sites to add a `nil` argument: `internal/tui/view.go` (`commitIdentWidth` ~959, `commitIdentRowAt` ~994, `commitTextRevealAt` ~943, `commitDecoratorsRange` ~1058)
- Test: `internal/tui/commit_ident_test.go` (existing 3 calls to `commitIdentOf(c)` → `commitIdentOf(c, nil)`; add new cases)

**Interfaces:**
- Produces: `func commitIdentOf(c model.Commit, tracked map[string]string) commitIdent` — `tracked` maps an upstream short ref (e.g. `"origin/main"`) to the local branch name that tracks it (e.g. `"main"`); `nil` is valid (no remote-tip detection).
- Produces: `commitIdent` gains field `remoteTip bool` (this commit is the tip of a tracked remote). Existing `tip bool` = local-branch tip.

- [ ] **Step 1: Update existing tests to the new signature, then add failing cases**

In `internal/tui/commit_ident_test.go` change the three existing `commitIdentOf(c)` calls to `commitIdentOf(c, nil)`, then append:

```go
func TestCommitIdentOfInSyncTipMarksBoth(t *testing.T) {
	tracked := map[string]string{"origin/main": "main"}
	c := model.Commit{Refs: []model.Ref{
		{Name: "main", Kind: model.RefLocal, Head: true},
		{Name: "origin/main", Kind: model.RefRemote},
	}}
	id := commitIdentOf(c, tracked)
	if !id.tip || !id.remoteTip || id.name != "main" || !id.head {
		t.Fatalf("ident = %+v, want local+remote tip main (head)", id)
	}
}

func TestCommitIdentOfRemoteOnlyTipUsesBranchName(t *testing.T) {
	tracked := map[string]string{"origin/main": "main"}
	// A commit decorated only by the tracked remote ref (local branch is behind).
	c := model.Commit{Refs: []model.Ref{{Name: "origin/main", Kind: model.RefRemote}}, Source: "main"}
	id := commitIdentOf(c, tracked)
	if id.tip || !id.remoteTip || id.name != "main" {
		t.Fatalf("ident = %+v, want remote-only tip named main", id)
	}
}

func TestCommitIdentOfUntrackedRemoteIsNotMarked(t *testing.T) {
	// origin/feature is not any local branch's upstream → no remote-tip marker.
	c := model.Commit{Refs: []model.Ref{{Name: "origin/feature", Kind: model.RefRemote}}, Source: "main"}
	id := commitIdentOf(c, map[string]string{"origin/main": "main"})
	if id.remoteTip {
		t.Fatalf("ident = %+v, want no remoteTip for an untracked remote", id)
	}
	if id.name != "main" { // falls back to lineage source
		t.Fatalf("name = %q, want lineage main", id.name)
	}
}
```

- [ ] **Step 2: Run the new tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestCommitIdentOf' -v`
Expected: compile error (the existing `commitIdentOf(c, nil)` arity won't match the old 1-arg func) — that is the failing state. Fix it in Step 3.

- [ ] **Step 3: Add `remoteTip` to the struct and the new `commitIdentOf`**

In `internal/tui/commit_ident.go`, add the field to `commitIdent`:

```go
type commitIdent struct {
	name      string   // branch name (no * marker); "" when the commit has none
	tip       bool     // this commit is a local branch's tip
	remoteTip bool     // this commit is the tip of a tracked remote (upstream of a local branch)
	head      bool     // the chosen branch is the current (HEAD) branch
	extra     []string // additional local-branch tips at this commit (multi-tip)
}
```

Replace `commitIdentOf` with:

```go
// commitIdentOf derives the identity from a commit's local refs (a tip) or, when
// it decorates none, from its Source branch (lineage; from `git log --source`).
// tracked maps an upstream short ref ("origin/main") to the local branch that
// tracks it; a RefRemote in that set marks the row as a tracked remote tip. A
// nil map disables remote-tip detection.
func commitIdentOf(c model.Commit, tracked map[string]string) commitIdent {
	var locals []model.Ref
	var remoteTipName string // local branch name behind a tracked remote tip here
	for _, r := range c.Refs {
		switch r.Kind {
		case model.RefLocal:
			locals = append(locals, r)
		case model.RefRemote:
			if name, ok := tracked[r.Name]; ok {
				remoteTipName = name
			}
		}
	}
	if len(locals) == 0 {
		if remoteTipName != "" {
			return commitIdent{name: remoteTipName, remoteTip: true}
		}
		return commitIdent{name: c.Source, tip: false}
	}
	pick := 0
	for i, r := range locals {
		if r.Head {
			pick = i
			break
		}
	}
	id := commitIdent{name: locals[pick].Name, tip: true, head: locals[pick].Head, remoteTip: remoteTipName != ""}
	for i, r := range locals {
		if i != pick {
			id.extra = append(id.extra, r.Name)
		}
	}
	return id
}
```

- [ ] **Step 4: Add the `nil` argument at the four call sites**

In `internal/tui/view.go`, change each `commitIdentOf(c)` / `commitIdentOf(m.commits[...])` to pass `nil` as the second argument:
- `commitIdentWidth`: `commitIdentOf(c, nil)`
- `commitIdentRowAt`: `commitIdentOf(c, nil)`
- `commitTextRevealAt`: `commitIdentOf(c, nil)`
- `commitDecoratorsRange`: `commitIdentOf(m.commits[ci-m.wipCount()], nil)`

- [ ] **Step 5: Run tests to verify pass**

Run: `go test ./internal/tui/ -run 'TestCommitIdent' -v && go build ./...`
Expected: PASS; build succeeds.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/commit_ident.go internal/tui/view.go internal/tui/commit_ident_test.go
git commit -m "feat(tui): detect tracked remote-branch tips in commit identity

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Jph17tYzRNEckP4yJk2cHb"
```

---

### Task A2: Render the marker prefix and wire the upstream map

Render `■`/`▲` markers in the identity token, size and dim the column correctly, and feed the real upstream map from `m.branches` to every call site. This is the visible feature for the in-sync and local-ahead cases.

**Files:**
- Modify: `internal/tui/commit_ident.go` (`token`, `fullToken`, add `markers()` + `commitMarkerW`)
- Modify: `internal/tui/view.go` (`commitIdentWidth`, `commitIdentRowAt`, `commitDecoratorsRange` identStart + dim; add `trackedUpstreams()` — place near `commitIdentWidth`)
- Test: `internal/tui/commit_ident_test.go` (token/marker width), `internal/tui/commit_render_test.go` (NEW — row rendering)

**Interfaces:**
- Consumes: `commitIdentOf(c, tracked)` and `commitIdent{tip, remoteTip}` from Task A1.
- Produces: `func (m Model) trackedUpstreams() map[string]string` — `{b.Upstream: b.Name}` for every local branch with a non-empty `Upstream`.
- Produces: `const commitMarkerW = 3` — width of the marker prefix (2 glyph cells + 1 separator space) prepended to every commit identity token.
- Produces: `func (id commitIdent) markers() string` — a 2-cell, left-packed marker field (`"■▲"`, `"■ "`, `"▲ "`, or `"  "`).

- [ ] **Step 1: Write failing marker/width tests**

Append to `internal/tui/commit_ident_test.go`:

```go
func TestCommitIdentMarkers(t *testing.T) {
	cases := []struct {
		name string
		id   commitIdent
		want string
	}{
		{"in sync", commitIdent{tip: true, remoteTip: true}, "■▲"},
		{"local only", commitIdent{tip: true}, "■ "},
		{"remote only", commitIdent{remoteTip: true}, "▲ "},
		{"neither", commitIdent{}, "  "},
	}
	for _, tc := range cases {
		if got := tc.id.markers(); got != tc.want {
			t.Errorf("%s: markers() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCommitIdentTokenIncludesMarkerPrefix(t *testing.T) {
	id := commitIdent{name: "main", tip: true, remoteTip: true}
	tok, trimmed := id.token(commitIdentW)
	if trimmed {
		t.Fatal("a short name must not be trimmed")
	}
	if !strings.HasPrefix(tok, "■▲ ") {
		t.Fatalf("token = %q, want it to start with the marker prefix", tok)
	}
	if want := commitMarkerW + commitIdentW; lipgloss.Width(tok) != want {
		t.Fatalf("token width = %d, want %d", lipgloss.Width(tok), want)
	}
}

// Single-marker rows must be the SAME width as two-marker rows, else rows with
// one marker misalign against rows with two. Pins the left-pack field at 2 cells.
func TestCommitIdentTokenSingleMarkerWidth(t *testing.T) {
	for _, id := range []commitIdent{
		{name: "main", tip: true},       // ■  (local only)
		{name: "main", remoteTip: true}, // ▲  (remote only)
	} {
		tok, _ := id.token(commitIdentW)
		if want := commitMarkerW + commitIdentW; lipgloss.Width(tok) != want {
			t.Fatalf("token %q width = %d, want %d", tok, lipgloss.Width(tok), want)
		}
	}
}
```

Also update the existing `TestCommitIdentTokenTrimsLongName` in this file: its width assertion `lipgloss.Width(tok) != commitIdentW` must become `!= commitMarkerW+commitIdentW` (the marker prefix now adds `commitMarkerW`). This test constructs `commitIdent` directly, so the A2 Step 9 grep won't surface it — fix it here.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestCommitIdentMarkers|TestCommitIdentTokenIncludesMarkerPrefix' -v`
Expected: FAIL — `markers` undefined, `commitMarkerW` undefined.

- [ ] **Step 3: Add `commitMarkerW`, `markers()`, and weave the prefix into `token`/`fullToken`**

In `internal/tui/commit_ident.go` add near `commitIdentW`:

```go
// commitMarkerW is the display width of the tip-marker prefix on a commit
// identity token: two glyph cells (local ■, remote ▲) plus one separator space.
const commitMarkerW = 3

const (
	markerLocal  = "■" // tip of a local branch
	markerRemote = "▲" // tip of a tracked remote (a local branch's upstream)
)

// markers is the 2-cell, left-packed marker field for this identity: present
// markers fill from the left, missing slots are spaces, so the field is always
// exactly two display cells wide.
func (id commitIdent) markers() string {
	switch {
	case id.tip && id.remoteTip:
		return markerLocal + markerRemote
	case id.tip:
		return markerLocal + " "
	case id.remoteTip:
		return markerRemote + " "
	default:
		return "  "
	}
}
```

Replace `token` and `fullToken` so the name is sized to `w` and the fixed marker prefix is prepended (trimming applies to the name only):

```go
// token is the display token at width commitMarkerW+w: the marker prefix, a
// separator space, then the name trimmed with … when too long, else right-padded
// so subjects stay aligned. trimmed reports whether the NAME was truncated.
func (id commitIdent) token(w int) (text string, trimmed bool) {
	name := id.label()
	var body string
	if lipgloss.Width(name) > w {
		body, trimmed = truncate(name, w), true
	} else {
		body = padRight(name, w)
	}
	return id.markers() + " " + body, trimmed
}

// fullToken is the UNtrimmed label with the marker prefix, padded to commitMarkerW+w.
func (id commitIdent) fullToken(w int) string {
	return id.markers() + " " + padRight(id.label(), w)
}
```

- [ ] **Step 4: Run the marker/width tests to verify pass**

Run: `go test ./internal/tui/ -run 'TestCommitIdent' -v`
Expected: PASS.

- [ ] **Step 5: Write a failing row-render test for the real upstream map**

Create `internal/tui/commit_render_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// renderModelWithCommits builds a minimal Model whose Commits panel can render
// rows in list mode (no graph), with the given branches and commits.
func renderModelWithCommits(branches []model.Branch, commits []model.Commit) Model {
	m := Model{branches: branches, commits: commits, commitListMode: true}
	m.focus = panelCommits
	return m
}

func TestCommitRowShowsBothMarkersWhenInSync(t *testing.T) {
	branches := []model.Branch{{Name: "main", IsHead: true, Upstream: "origin/main"}}
	commits := []model.Commit{{
		Hash:    "aaaa111",
		Subject: "in sync commit",
		Refs:    []model.Ref{{Name: "main", Kind: model.RefLocal, Head: true}, {Name: "origin/main", Kind: model.RefRemote}},
	}}
	m := renderModelWithCommits(branches, commits)
	row := m.commitIdentRowAt(0, m.commitIdentWidth(), false)
	if !strings.Contains(row, "■▲") {
		t.Fatalf("row = %q, want both ■▲ markers", row)
	}
	if !strings.Contains(row, "*main") {
		t.Fatalf("row = %q, want *main label", row)
	}
}

func TestCommitRowRemoteOnlyTipNamesBranch(t *testing.T) {
	branches := []model.Branch{{Name: "main", Upstream: "origin/main"}}
	commits := []model.Commit{{
		Hash:    "bbbb222",
		Subject: "remote tip ahead",
		Refs:    []model.Ref{{Name: "origin/main", Kind: model.RefRemote}},
		Source:  "main",
	}}
	m := renderModelWithCommits(branches, commits)
	row := m.commitIdentRowAt(0, m.commitIdentWidth(), false)
	if !strings.Contains(row, "▲") || strings.Contains(row, "■") {
		t.Fatalf("row = %q, want only the remote ▲ marker", row)
	}
	if !strings.Contains(row, "main") {
		t.Fatalf("row = %q, want the local branch name main", row)
	}
}
```

- [ ] **Step 6: Run it to verify it fails**

Run: `go test ./internal/tui/ -run 'TestCommitRow' -v`
Expected: FAIL — markers absent (call sites still pass `nil`).

- [ ] **Step 7: Add `trackedUpstreams()` and wire it through the call sites**

In `internal/tui/view.go`, add near `commitIdentWidth`:

```go
// trackedUpstreams maps each local branch's upstream short ref ("origin/main")
// to the branch name ("main"), for marking tracked remote-branch tips in the
// commit graph. A branch with no upstream is omitted.
func (m Model) trackedUpstreams() map[string]string {
	out := make(map[string]string, len(m.branches))
	for _, b := range m.branches {
		if b.Upstream != "" {
			out[b.Upstream] = b.Name
		}
	}
	return out
}
```

Replace the four `commitIdentOf(..., nil)` calls so they pass `m.trackedUpstreams()`. In the two loops (`commitIdentWidth`, `commitDecoratorsRange`) hoist it to a single local `tracked := m.trackedUpstreams()` before the loop and pass `tracked`:

```go
// commitIdentWidth
tracked := m.trackedUpstreams()
w := 0
for _, c := range m.commits {
	if lw := lipgloss.Width(commitIdentOf(c, tracked).label()); lw > w {
		if w = lw; w >= commitIdentW {
			return commitIdentW
		}
	}
}
return w
```

```go
// commitIdentRowAt and commitTextRevealAt (single commit each)
id := commitIdentOf(c, m.trackedUpstreams())
```

```go
// commitDecoratorsRange: hoist before the for loop, then inside the loop
tracked := m.trackedUpstreams()
...
id := commitIdentOf(m.commits[ci-m.wipCount()], tracked)
```

- [ ] **Step 8: Fix the dim region — exclude remote tips and skip the marker prefix**

Still in `commitDecoratorsRange` (`internal/tui/view.go`): a tracked remote-only tip must read bright (it is a meaningful tip, not lineage), and the dim region must target the NAME, not the marker prefix. Change the dim condition and bump `identStart` by the marker width:

```go
id := commitIdentOf(m.commits[ci-m.wipCount()], tracked)
dim := !id.tip && !id.remoteTip && id.name != "" // gray a lineage row's branch name
```

Find the `identStart := 2` block and add the marker width so the dimmed range starts at the name (the marker prefix sits between the graph prefix and the name):

```go
identStart := 2
if m.commitListMode {
	identStart += 2 // "● "
} else if graphPrefix {
	identStart += m.graphCols()*2 + 1
}
identStart += commitMarkerW // skip the ■▲ marker prefix; dim only the name column
```

- [ ] **Step 9: Run the full TUI suite**

Run: `go test ./internal/tui/ -run 'TestCommit' -v && go build ./...`
Expected: PASS; build succeeds. (If any existing snapshot/row test asserts exact identity-column spacing, update its expectation to include the 3-col marker prefix — search with `grep -n "commitIdentRowAt\|commitRows\|identStart" internal/tui/*_test.go` and adjust.)

- [ ] **Step 10: Commit**

```bash
git add internal/tui/commit_ident.go internal/tui/view.go internal/tui/commit_ident_test.go internal/tui/commit_render_test.go
git commit -m "feat(tui): show ■ local-tip / ▲ tracked-remote-tip markers in Commits

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Jph17tYzRNEckP4yJk2cHb"
```

---

## Stage B — walk tracked upstreams into the feed

### Task B1: `LogScope.Upstreams` in the git verb and the domain scope key

Let the commit walk include extra remote-tracking refs so a behind/diverged remote tip appears as its own row.

**Files:**
- Modify: `internal/git/log.go` (`LogScope` ~19-21; `LogScoped` ~27-44)
- Modify: `internal/domain/query.go` (`scopeKey` ~189-193)
- Test: `internal/git/log_test.go` (argv assertion via FakeRunner), `internal/domain/query_test.go` (NEW or existing — `scopeKey`)

**Interfaces:**
- Produces: `LogScope` gains `Upstreams []string` — extra refs appended to the walk (in addition to `--branches HEAD` or named branches). Callers that leave it empty are unaffected.

- [ ] **Step 1: Write the failing git argv test**

In `internal/git/log_test.go` add (the file already uses `FakeRunner`; mirror its existing helper for constructing a `*Repo` with a fake — search the file for `FakeRunner` / `newFakeRepo` and reuse it):

```go
func TestLogScopedAppendsUpstreams(t *testing.T) {
	fake := &gitexec.FakeRunner{}
	repo := &Repo{Runner: fake} // adjust to the file's existing repo constructor if different
	_, _ = repo.LogScoped(context.Background(), 10, 0, LogScope{Upstreams: []string{"origin/main"}}, false)
	argv := fake.LastArgv() // adjust to the fake's recorded-argv accessor used elsewhere in this file
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--branches HEAD") {
		t.Fatalf("argv = %v, want default --branches HEAD", argv)
	}
	if !strings.Contains(joined, "origin/main") {
		t.Fatalf("argv = %v, want the upstream ref appended", argv)
	}
}
```

Note: match the exact `FakeRunner` construction and argv-recording accessor already used by the other tests in `log_test.go` (e.g. a `Calls`/`Commands` slice). Do not invent a new fake API.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/git/ -run TestLogScopedAppendsUpstreams -v`
Expected: FAIL — `Upstreams` field does not exist.

- [ ] **Step 3: Add the field and append it in `LogScoped`**

In `internal/git/log.go`:

```go
// LogScope selects which refs the walk covers. Empty Branches → all local
// branches (plus HEAD); otherwise exactly the listed branch names. Upstreams are
// extra remote-tracking refs (e.g. "origin/main") appended to the walk so a
// branch's remote tip shows even when the local branch is behind. Callers must
// only pass upstreams that resolve (git log errors on a missing ref).
type LogScope struct {
	Branches  []string
	Upstreams []string
}
```

In `LogScoped`, after the `if len(scope.Branches) == 0 { ... } else { ... }` block and before `r.Runner.Run`:

```go
if len(scope.Upstreams) > 0 {
	b = b.Arg(scope.Upstreams...)
}
```

- [ ] **Step 4: Run the git test to verify pass**

Run: `go test ./internal/git/ -run TestLogScoped -v`
Expected: PASS.

- [ ] **Step 5: Write the failing `scopeKey` test**

In `internal/domain` (add to an existing `*_test.go` for query, or create `internal/domain/scopekey_test.go`):

```go
package domain

import "testing"

func TestScopeKeyFoldsUpstreams(t *testing.T) {
	a := scopeKey(LogScope{Branches: []string{"main"}})
	b := scopeKey(LogScope{Branches: []string{"main"}, Upstreams: []string{"origin/main"}})
	if a == b {
		t.Fatalf("scopeKey must differ when Upstreams differ: %q == %q", a, b)
	}
}
```

- [ ] **Step 6: Run it to verify it fails**

Run: `go test ./internal/domain/ -run TestScopeKeyFoldsUpstreams -v`
Expected: FAIL — keys are equal (Upstreams ignored).

- [ ] **Step 7: Fold Upstreams into `scopeKey`**

In `internal/domain/query.go`:

```go
// scopeKey is the stable cache/singleflight discriminator for a scope.
func scopeKey(scope LogScope) string {
	base := "all"
	if len(scope.Branches) > 0 {
		base = strings.Join(scope.Branches, ",")
	}
	if len(scope.Upstreams) > 0 {
		base += "|up:" + strings.Join(scope.Upstreams, ",")
	}
	return base
}
```

- [ ] **Step 8: Run both suites to verify pass**

Run: `go test ./internal/git/ ./internal/domain/ -run 'LogScoped|ScopeKey' -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/git/log.go internal/git/log_test.go internal/domain/query.go internal/domain/scopekey_test.go
git commit -m "feat(domain): LogScope.Upstreams walks extra remote refs into the feed

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Jph17tYzRNEckP4yJk2cHb"
```

---

### Task B2: TUI populates the feed scope with resolved upstreams

Always include the in-scope branches' upstreams (filtered to refs that actually exist) in the feed walk, and reload once after the initial snapshot so the remote tips appear on first view.

**Files:**
- Modify: `internal/tui/commit_scope.go` (`reloadFeedCmd` ~33-41; add `feedScope()` + `feedUpstreams()`)
- Modify: `internal/tui/model.go` (the `dataLoadedMsg` handler that sets `m.branches`/`m.remoteBranches`, ~429-431) to trigger a one-shot feed reload when tracked upstreams exist
- Test: `internal/tui/commit_scope_test.go` (NEW or existing) for `feedUpstreams`

**Interfaces:**
- Consumes: `LogScope.Upstreams` (Task B1), `m.branches` (each `model.Branch.Upstream`), `m.remoteBranches` (`model.RemoteBranch.Name` = `"origin/main"`), `m.commitScopeBranches`.
- Produces: `func (m Model) feedUpstreams() []string` — deduped upstream refs of the in-scope local branches (all locals when scope is empty), restricted to refs present in `m.remoteBranches`.
- Produces: `func (m Model) feedScope() domain.LogScope` — `{Branches: copy(m.commitScopeBranches), Upstreams: m.feedUpstreams()}`.

- [ ] **Step 1: Write the failing `feedUpstreams` test**

Create `internal/tui/commit_scope_test.go` (or append if it exists):

```go
package tui

import (
	"slices"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestFeedUpstreamsFiltersToExistingRemoteRefs(t *testing.T) {
	m := Model{
		branches: []model.Branch{
			{Name: "main", Upstream: "origin/main"},
			{Name: "feat", Upstream: "origin/feat"}, // upstream configured but ref gone
			{Name: "local-only"},                    // no upstream
		},
		remoteBranches: []model.RemoteBranch{{Name: "origin/main"}}, // only origin/main exists
	}
	got := m.feedUpstreams()
	if !slices.Equal(got, []string{"origin/main"}) {
		t.Fatalf("feedUpstreams() = %v, want [origin/main]", got)
	}
}

func TestFeedUpstreamsRespectsSoloScope(t *testing.T) {
	m := Model{
		commitScopeBranches: []string{"feat"}, // soloed
		branches: []model.Branch{
			{Name: "main", Upstream: "origin/main"},
			{Name: "feat", Upstream: "origin/feat"},
		},
		remoteBranches: []model.RemoteBranch{{Name: "origin/main"}, {Name: "origin/feat"}},
	}
	got := m.feedUpstreams()
	if !slices.Equal(got, []string{"origin/feat"}) {
		t.Fatalf("feedUpstreams() = %v, want only the soloed branch's upstream [origin/feat]", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run TestFeedUpstreams -v`
Expected: FAIL — `feedUpstreams` undefined.

- [ ] **Step 3: Implement `feedUpstreams` and `feedScope`**

In `internal/tui/commit_scope.go` add:

```go
// feedUpstreams returns the deduped upstream refs of the local branches in the
// current feed scope (all local branches when the scope is empty), restricted to
// refs that actually exist as remote-tracking branches — git log errors on a
// missing ref, so a configured-but-unfetched upstream must be dropped.
func (m Model) feedUpstreams() []string {
	exists := make(map[string]bool, len(m.remoteBranches))
	for _, rb := range m.remoteBranches {
		exists[rb.Name] = true
	}
	inScope := func(name string) bool {
		return len(m.commitScopeBranches) == 0 || slices.Contains(m.commitScopeBranches, name)
	}
	var out []string
	seen := map[string]bool{}
	for _, b := range m.branches {
		if b.Upstream == "" || !inScope(b.Name) || !exists[b.Upstream] || seen[b.Upstream] {
			continue
		}
		seen[b.Upstream] = true
		out = append(out, b.Upstream)
	}
	return out
}

// feedScope is the LogScope the commit feed should walk: the scoped branches plus
// their tracked, resolvable upstreams.
func (m Model) feedScope() domain.LogScope {
	return domain.LogScope{
		Branches:  append([]string(nil), m.commitScopeBranches...),
		Upstreams: m.feedUpstreams(),
	}
}
```

Add `"slices"` to the import block if not already present.

- [ ] **Step 4: Use `feedScope()` in `reloadFeedCmd`**

In `internal/tui/commit_scope.go` replace the scope construction in `reloadFeedCmd`:

```go
func (m Model) reloadFeedCmd() tea.Cmd {
	feed := m.feed
	scope := m.feedScope()
	return func() tea.Msg {
		feed.SetScope(scope)
		st, _ := feed.LoadInitial(context.Background())
		return commitsReloadedMsg{gen: st.Gen, state: st}
	}
}
```

- [ ] **Step 5: Run the scope tests to verify pass**

Run: `go test ./internal/tui/ -run 'TestFeedUpstreams|TestStartFeedReload|TestReloadFeed' -v`
Expected: PASS.

- [ ] **Step 6: Write a failing one-shot startup-reload test**

The initial `loadCmd` walks the feed in parallel with the snapshot, so the first feed has no upstreams. When the snapshot's branches arrive and there ARE tracked upstreams, a feed reload must be issued exactly once. Append to `internal/tui/commit_scope_test.go`:

Assert the reload-specific signal, NOT merely `cmd != nil` — the `dataLoadedMsg`
handler may return a non-nil cmd for unrelated reasons, so `cmd != nil` would be a
false positive. `startFeedReload` sets `m.commitsLoading = true`, so check that.
Include the negative case (no tracked upstreams ⇒ the single fast initial walk is
preserved, `commitsLoading` stays false) — that is exactly what the
`len(feedUpstreams())>0` guard protects.

```go
func TestDataLoadedTriggersUpstreamReload(t *testing.T) {
	m := newTestModelForReload(t) // see note below
	msg := dataLoadedMsg{
		gen:            m.loadGen,
		branches:       []model.Branch{{Name: "main", IsHead: true, Upstream: "origin/main"}},
		remoteBranches: []model.RemoteBranch{{Name: "origin/main"}},
	}
	nm, _ := m.Update(msg)
	if !nm.(Model).commitsLoading {
		t.Fatal("expected a feed reload (commitsLoading=true) when tracked upstreams exist")
	}
}

func TestDataLoadedNoReloadWithoutTrackedUpstreams(t *testing.T) {
	m := newTestModelForReload(t)
	msg := dataLoadedMsg{
		gen:      m.loadGen,
		branches: []model.Branch{{Name: "main", IsHead: true}}, // no upstream
	}
	nm, _ := m.Update(msg)
	if nm.(Model).commitsLoading {
		t.Fatal("no tracked upstreams must NOT trigger a reload (preserve the fast initial walk)")
	}
}
```

Note: reuse the existing test-model constructor used by other `commit_scope`/reload tests (search for how `startFeedReload`/reload tests build a `Model` with a fake `svc`/`feed` — memory: `startFeedReload` builds its closure safely at test time). Name the helper to match what already exists; do not introduce a new fake if one is present. If no constructor exists, set the minimal fields the `dataLoadedMsg` branch reads (`loadGen`, `svc`, `feed`) using the same fakes the other tests use.

- [ ] **Step 7: Run it to verify it fails**

Run: `go test ./internal/tui/ -run TestDataLoadedTriggersUpstreamReload -v`
Expected: FAIL — no reload cmd returned today.

- [ ] **Step 8: Trigger the one-shot reload in the `dataLoadedMsg` handler**

In `internal/tui/model.go`, in the `dataLoadedMsg` case right after `m.branches = msg.branches` and `m.remoteBranches = msg.remoteBranches` are set (~429-431), add a guarded reload. Capture the existing return for this branch and prefer batching if it already returns a cmd; otherwise return the reload cmd:

```go
m.branches = msg.branches
m.remoteBranches = msg.remoteBranches
// The initial feed walk (loadCmd) ran in parallel with the snapshot, so it had
// no upstreams. Now that tracked branches are known, reload once to walk their
// remote tips in (so a behind/diverged remote tip shows). Guard on non-empty so
// repos with no tracked upstreams keep the single fast initial walk.
if len(m.feedUpstreams()) > 0 {
	var reload tea.Cmd
	m, reload = m.startFeedReload()
	cmds = append(cmds, reload)
}
```

Match the surrounding handler's command-accumulation style: if the case already builds a `cmds []tea.Cmd` / `tea.Batch`, append to it; if it returns a single cmd, combine via `tea.Batch(existing, reload)`. Read the full `dataLoadedMsg` case before editing to wire the return correctly. This fires only on the gen-gated initial load (and any path that delivers `dataLoadedMsg`), and `commitsReloadedMsg` does not re-emit `dataLoadedMsg`, so there is no reload loop.

- [ ] **Step 9: Run the TUI suite to verify pass**

Run: `go test ./internal/tui/ -v 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 10: Real-repo behind-case verification (domain)**

Add a real-`git` test proving a behind branch's remote tip is walked in. Put it in `internal/domain` (it may use the real-repo helper `newTestRepo`/`newRepo` already used there — search `internal/domain/*_test.go` for the helper):

```go
func TestCommitFeedWalksUpstreamWhenBehind(t *testing.T) {
	// Build: commit A on main; create origin/main at B (a child of A) so local
	// main is BEHIND its remote-tracking ref by one commit.
	// (Use the package's existing real-repo helper + git plumbing already used in
	// other domain tests to: commit A, set refs/remotes/origin/main to a commit B
	// that descends from A, and configure main's upstream = origin/main.)
	// Then: walk with LogScope{Branches:["main"], Upstreams:["origin/main"]} and
	// assert commit B appears in the returned feed, while a walk WITHOUT Upstreams
	// does not contain B.
	t.Skip("flesh out with the domain package's real-repo helper; asserts B ∈ feed only when Upstreams set")
}
```

Replace the `t.Skip` body using the concrete helper the domain tests already provide (mirror an existing `LogScoped`/`CommitFeed` real-repo test). The assertion is: with `Upstreams` set, the upstream-only commit `B` is present; without it, `B` is absent.

- [ ] **Step 11: Run race + e2e gate**

Run: `./test.sh race 2>&1 | tail -25`
Expected: PASS (vet/gofmt, unit, e2e).

- [ ] **Step 12: Commit**

```bash
git add internal/tui/commit_scope.go internal/tui/commit_scope_test.go internal/tui/model.go internal/domain/*_test.go
git commit -m "feat(tui): walk tracked upstreams into the feed so behind remote tips show

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Jph17tYzRNEckP4yJk2cHb"
```

---

## Final: docs + build for live test

### Task C1: Update docs and build the branch binary

**Files:**
- Modify: `CHANGELOG.md` (add an entry under the current unreleased/next section)
- Modify: `internal/tui/help.go` (Commits section — note the `■` local / `▲` remote tip markers)
- Build: `go build -o ./gg ./cmd/gg`

- [ ] **Step 1: Add a CHANGELOG entry**

Add under the top/unreleased section of `CHANGELOG.md`:

```markdown
- **Commits panel — local/remote tip markers.** Each commit that is the tip of a
  local branch shows `■`, and a commit that is the tip of that branch's tracked
  remote shows `▲`; when local and remote point at the same commit both markers
  appear together. Tracked-remote tips are walked into the feed so the marker
  shows even when the local branch is behind its upstream. No ahead/behind
  numbers; the divergence reads from the graph.
```

- [ ] **Step 2: Note the markers in the Commits help text**

In `internal/tui/help.go`, find the Commits panel help row(s) (search for `"commits:"` / the Commits section) and add a short line, e.g.:

```go
r("■ ▲", "in the Commits graph, ■ marks a local branch's tip and ▲ the tip of its tracked remote (both together = local and remote in sync)"),
```

Match the existing `r(...)` row helper signature and placement in that section.

- [ ] **Step 3: Build the branch binary**

Run: `go build -o ./gg ./cmd/gg && echo OK`
Expected: `OK`.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md internal/tui/help.go
git commit -m "docs: changelog + help for local/remote tip markers

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Jph17tYzRNEckP4yJk2cHb"
```

- [ ] **Step 5: Hand off for live test**

Tell the user the branch binary is at `<worktree>/gg` (absolute path) so they can eyeball the markers live before merge. Do NOT merge — the human owns trunk integration.

---

## Self-review notes

- **Spec coverage:** row format + marker semantics → A1/A2; "tracked = has a local copy" join → A1 (`tracked` map) + B2 (`feedUpstreams` filter); in-sync/ahead with no feed change → A2; behind/diverged via walking upstreams → B1+B2; always-on → B2 (`feedScope` + startup reload); no numbers / no CLI / commitgraph untouched → not implemented anywhere (by omission); `*` head + dim-lineage preserved → A1 keeps `label()`, A2 keeps dim for `!tip && !remoteTip`.
- **Glyph width risk:** `■`/`▲` are East-Asian-ambiguous; `TestCommitIdentTokenIncludesMarkerPrefix` asserts the token width is exactly `commitMarkerW+commitIdentW`. If `lipgloss.Width` reports 2 for a glyph in the build environment, swap to a width-1 alternative (e.g. `▪`/`▴`) and keep the test as the guard.
- **WIP pseudo-rows** (`Working tree`/`Staged`) are intentionally NOT given a marker prefix — they are not branch tips and already render without an identity token.
- **Soloed-view edge (known, ship as-is):** `trackedUpstreams()` (Stage A marker detection) uses ALL `m.branches`, while `feedUpstreams()` (Stage B scope) is scope-aware. In a soloed view a `▲` can appear for a tracked remote whose branch is out of scope, if its ref happens to decorate a commit already in the walk. The marker is still accurate ("a tracked remote tip is here"); it is only mildly inconsistent with the scope. Acceptable.
- **README:** glance at whether `README.md` documents the Commits panel; if it lists panel keys/legend, add the `■`/`▲` marker note there too (CLAUDE.md: update README when the user-facing surface changes). The agentskill bump is correctly skipped — no CLI surface changed.
- **Type consistency:** `commitIdentOf(model.Commit, map[string]string)`, `commitIdent{tip, remoteTip, head, name, extra}`, `commitMarkerW`, `markers()`, `trackedUpstreams()`, `feedUpstreams()`, `feedScope()`, `LogScope{Branches, Upstreams}`, `scopeKey(LogScope)` are used consistently across tasks.
