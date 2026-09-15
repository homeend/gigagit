# Frozen Preview Versions + Drift Detection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every branch-version snapshot record the three endpoints needed to re-render the PR-style preview it froze, and report automatically when a rebase/merge/pull changed which files the branch contributes.

**Architecture:** A snapshot ref stops pointing at a bare tip and points at a synthetic `commit-tree` object whose single `Gg-Meta` trailer carries `(op, ours, other, base, source, target)`; parents exist only to pin objects. The frozen view is a lens over the existing compare pipeline (`PreviewEndpoints{Left: base, Right: ours}`). Drift is a set difference between two `--no-renames` name-status listings, computed after the repo gate is released. The format bump to 2 rides spec 1's existing preflight migration machinery.

**Tech Stack:** Go 1.26, stdlib only for new leaf packages. Existing seams: `gitcmd` argv builder, `gitexec.Runner`/`FakeRunner`, `engine.OpDeps`, `domain.Execute`, `preflight` registry, `i18n` TOML bundles.

**Spec:** `docs/superpowers/specs/2026-09-15-frozen-preview-versions-design.md`

## Global Constraints

- Work in `/mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions` on branch `feat/frozen-preview-versions`. Prefix every shell command with `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions` (the shell cwd resets between calls) and use absolute paths for Write/Edit.
- `internal/changeset` is a **DAG leaf**: stdlib imports only. `internal/archtest` enforces it.
- `internal/tui`, `internal/cli`, `internal/mcp`, `internal/web` never import `internal/git`.
- A git verb is **one invocation**, built with `gitcmd`, run through `r.Runner`.
- **Exactly one `%(trailers:key=…)` atom per `for-each-ref` format string.** Verified on git 2.43: two or more cross-contaminate, returning every value in every atom. This silently corrupts every version record.
- Metadata line: `Gg-Meta: <op> <ours> <other> <base> <source> <target>`. First four fields positional; the two names are last.
- `commit-tree` runs through `RunEnv` with fixed `GIT_AUTHOR_NAME/EMAIL`, `GIT_COMMITTER_NAME/EMAIL` and both `_DATE`s = the snapshot `ts`. Message via `-F <tempfile>`, never `-m` (the Runner has no stdin; repeated `-m` breaks the trailer block).
- Synthetic commit's tree is `<p1>^{tree}`, so `git log --all -p` shows no diff.
- Versions store format is **2** in this plan. `Min: 2, Max: 2`.
- Drift diffs use `--no-renames` on BOTH sides, and run in `Read` mode **after** `Execute` releases the gate — never under `TreeWrite`.
- Every user-visible TUI string is `i18n.T` with a literal key in all four bundles (`internal/i18n/lang/{ja,ko,ru,zh}.toml`), real translations. Engine and CLI prose stay English. New **engine** prose needs bundle keys too (`internal/tui/engine_prose_test.go` gate).
- Tests use a real `git` in `t.TempDir()` via existing helpers (`newTestRepo` in `internal/git`, `svcAt(cleanDir(t))` in `internal/domain`) — no new helpers. New tests call `t.Parallel()` unless they touch global state.
- Run `./test.sh` before each commit; `./test.sh race` before the final one. Run test commands in the FOREGROUND.

---

### Task 1: `internal/changeset` — the drift comparison

**Files:**
- Create: `internal/changeset/changeset.go`
- Create: `internal/changeset/changeset_test.go`
- Modify: `internal/archtest/import_guard_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `changeset.Entry{Status byte; Path string}`, `changeset.Report{Added, Removed []Entry}`, `changeset.Compare(before, after []Entry) Report`, `(Report).Drifted() bool`.

- [ ] **Step 1: Write the failing test**

Create `internal/changeset/changeset_test.go`:

```go
package changeset

import "testing"

func e(st byte, p string) Entry { return Entry{Status: st, Path: p} }

func TestCompare(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		before, after []Entry
		added         []Entry
		removed       []Entry
		drifted       bool
	}{
		{
			name:   "identical sets do not drift",
			before: []Entry{e('M', "a.txt"), e('A', "b.txt")},
			after:  []Entry{e('A', "b.txt"), e('M', "a.txt")},
		},
		{
			// The motivating case: upstream deleted f3, the branch had modified
			// it, and a modify/delete conflict was resolved the wrong way, so the
			// branch now reads as ADDING a file it never added.
			name:    "status change M->A is resurrection and alarms",
			before:  []Entry{e('M', "f3.txt"), e('A', "f4.txt")},
			after:   []Entry{e('A', "f3.txt"), e('A', "f4.txt")},
			added:   []Entry{e('A', "f3.txt")},
			removed: []Entry{e('M', "f3.txt")},
			drifted: true,
		},
		{
			name:    "a path only in after alarms",
			before:  []Entry{e('M', "a.txt")},
			after:   []Entry{e('M', "a.txt"), e('A', "new.txt")},
			added:   []Entry{e('A', "new.txt")},
			drifted: true,
		},
		{
			// Upstream absorbed the change (identical fix, cherry-pick). Reported,
			// but this direction is the quiet one.
			name:    "a path only in before is reported, not alarmed",
			before:  []Entry{e('M', "a.txt"), e('M', "gone.txt")},
			after:   []Entry{e('M', "a.txt")},
			removed: []Entry{e('M', "gone.txt")},
			drifted: false,
		},
		{
			name:   "both empty",
			before: nil, after: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Compare(tc.before, tc.after)
			if !equal(got.Added, tc.added) {
				t.Errorf("Added = %v, want %v", got.Added, tc.added)
			}
			if !equal(got.Removed, tc.removed) {
				t.Errorf("Removed = %v, want %v", got.Removed, tc.removed)
			}
			if got.Drifted() != tc.drifted {
				t.Errorf("Drifted() = %v, want %v", got.Drifted(), tc.drifted)
			}
		})
	}
}

func equal(a, b []Entry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCompareIsDeterministicallyOrdered(t *testing.T) {
	t.Parallel()
	before := []Entry{e('M', "z.txt"), e('M', "a.txt")}
	after := []Entry{e('A', "z.txt"), e('A', "a.txt")}
	got := Compare(before, after)
	if len(got.Added) != 2 || got.Added[0].Path != "a.txt" || got.Added[1].Path != "z.txt" {
		t.Errorf("Added = %v, want sorted by path", got.Added)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/changeset/ -v`
Expected: FAIL — the package does not compile (`undefined: Compare`).

- [ ] **Step 3: Write the implementation**

Create `internal/changeset/changeset.go`:

```go
// Package changeset compares two base-relative change sets — the set of paths
// an operation's before and after states report as touched — and says what the
// operation itself moved. It is a DAG leaf: stdlib only, no git.
package changeset

import "sort"

// Entry is one path and the status git reported for it (A, M, D, T).
type Entry struct {
	Status byte
	Path   string
}

// Report is the difference between two change sets.
//
// Added is `after - before`: changes the OPERATION introduced to the branch's
// change set. This is the alarming direction — a path that appears, or whose
// status letter changed (M -> A is a resurrected file).
//
// Removed is `before - after`, which is usually benign: upstream absorbed the
// change (an identical fix, a cherry-pick), so it correctly cancels. Reported,
// never alarmed.
type Report struct {
	Added   []Entry
	Removed []Entry
}

// Drifted reports whether the operation introduced anything. Only the Added
// direction counts; see Report.
func (r Report) Drifted() bool { return len(r.Added) > 0 }

// Compare diffs the two sets by (status, path). Order of the inputs is
// irrelevant; both outputs are sorted by path then status so callers and tests
// see a stable order.
func Compare(before, after []Entry) Report {
	in := func(es []Entry) map[Entry]bool {
		m := make(map[Entry]bool, len(es))
		for _, e := range es {
			m[e] = true
		}
		return m
	}
	b, a := in(before), in(after)

	var rep Report
	for _, e := range after {
		if !b[e] {
			rep.Added = append(rep.Added, e)
		}
	}
	for _, e := range before {
		if !a[e] {
			rep.Removed = append(rep.Removed, e)
		}
	}
	sortEntries(rep.Added)
	sortEntries(rep.Removed)
	return rep
}

func sortEntries(es []Entry) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].Path != es[j].Path {
			return es[i].Path < es[j].Path
		}
		return es[i].Status < es[j].Status
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/changeset/ -v`
Expected: PASS.

- [ ] **Step 5: Add the archtest leaf guard**

Open `internal/archtest/import_guard_test.go` and find `TestPreflightIsAStdlibLeaf` (or `TestSteerIsAStdlibLeaf`). Add a `TestChangesetIsAStdlibLeaf` in exactly that shape, pointed at `../changeset`. Do not invent a new assertion style.

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/archtest/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions
git add internal/changeset/ internal/archtest/import_guard_test.go
git commit -m "feat(changeset): compare two base-relative change sets"
```

---

### Task 2: `DiffNameStatus` git verb

**Files:**
- Create: `internal/git/namestatus.go`
- Create: `internal/git/namestatus_test.go`

**Interfaces:**
- Consumes: `gitcmd.New`, `r.Runner.Run`, `newTestRepo(t)`.
- Produces: `(*git.Repo).DiffNameStatus(ctx context.Context, a, b string) ([]changeset.Entry, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/git/namestatus_test.go`:

```go
package git

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/changeset"
)

func TestDiffNameStatusReportsStatusesAndIgnoresRenames(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()

	writeFile(t, dir, "keep.txt", "one\n")
	writeFile(t, dir, "moved.txt", "content that is long enough to be detected as a rename\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-m", "base")
	base := revParse(t, dir, "HEAD")

	writeFile(t, dir, "keep.txt", "two\n")
	gitRunDir(t, dir, "", "rm", "-q", "moved.txt")
	writeFile(t, dir, "renamed.txt", "content that is long enough to be detected as a rename\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-m", "change")
	head := revParse(t, dir, "HEAD")

	got, err := r.DiffNameStatus(ctx, base, head)
	if err != nil {
		t.Fatalf("DiffNameStatus: %v", err)
	}

	// --no-renames is mandatory: with detection on, the D+A pair below would
	// collapse into a single R entry and manufacture phantom set differences
	// against the other side of the comparison.
	want := map[changeset.Entry]bool{
		{Status: 'M', Path: "keep.txt"}:    true,
		{Status: 'D', Path: "moved.txt"}:   true,
		{Status: 'A', Path: "renamed.txt"}: true,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries %v, want %d", len(got), got, len(want))
	}
	for _, e := range got {
		if !want[e] {
			t.Errorf("unexpected entry %v (a rename was detected, or a status is wrong)", e)
		}
	}
}

func TestDiffNameStatusEmptyForIdenticalTrees(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	head := revParse(t, dir, "HEAD")

	got, err := r.DiffNameStatus(context.Background(), head, head)
	if err != nil {
		t.Fatalf("DiffNameStatus: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want no entries", got)
	}
}
```

Use whatever the package's real helpers are named for writing a file, running git in the temp repo, and resolving a revision — grep `internal/git/*_test.go` for the existing ones and substitute them for `writeFile`/`gitRunDir`/`revParse` if the names differ. Do not add new helpers.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/git/ -run TestDiffNameStatus -v`
Expected: FAIL — `r.DiffNameStatus undefined`.

- [ ] **Step 3: Write the implementation**

Create `internal/git/namestatus.go`:

```go
package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/changeset"
	"github.com/homeend/gigagit/internal/gitcmd"
)

// DiffNameStatus lists the paths that differ between two commit-ish values
// with their status letter, one git invocation.
//
// --no-renames is not a preference: the drift comparison diffs two of these
// listings as sets, and with rename detection on a D+A pair on one side can be
// reported as a single R on the other, manufacturing differences that are not
// there. -z keeps paths with spaces or newlines intact.
func (r *Repo) DiffNameStatus(ctx context.Context, a, b string) ([]changeset.Entry, error) {
	argv := gitcmd.New("diff").Arg("--name-status", "--no-renames", "-z", a, b).ToArgv()
	res, err := r.Runner.Run(ctx, "git diff --name-status", argv)
	if err != nil {
		return nil, err
	}
	// -z output is NUL-separated and alternates status, path, status, path…
	fields := strings.Split(strings.TrimRight(res.Stdout, "\x00"), "\x00")
	var out []changeset.Entry
	for i := 0; i+1 < len(fields); i += 2 {
		st := strings.TrimSpace(fields[i])
		if st == "" || fields[i+1] == "" {
			continue
		}
		out = append(out, changeset.Entry{Status: st[0], Path: fields[i+1]})
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/git/ -run TestDiffNameStatus -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions
git add internal/git/namestatus.go internal/git/namestatus_test.go
git commit -m "feat(git): DiffNameStatus verb with rename detection off"
```

---

### Task 3: the version record — write and parse

**Files:**
- Create: `internal/git/versionrecord.go`
- Create: `internal/git/versionrecord_test.go`

**Interfaces:**
- Consumes: `(*Repo).Runner.RunEnv`, `gitcmd.New`, `newTestRepo(t)`.
- Produces: `git.VersionMeta{Op, Ours, Other, Base, Source, Target string}`, `git.FormatVersionMeta(VersionMeta) string`, `git.ParseVersionMeta(s string) (VersionMeta, bool)`, `(*Repo).WriteVersionSnapshot(ctx context.Context, tip string, m VersionMeta, unix int64) (string, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/git/versionrecord_test.go`:

```go
package git

import (
	"context"
	"strings"
	"testing"
)

func TestFormatParseVersionMetaRoundTrip(t *testing.T) {
	t.Parallel()
	m := VersionMeta{
		Op: "merge", Ours: "aaa", Other: "bbb", Base: "ccc",
		Source: "feat/x", Target: "main",
	}
	got, ok := ParseVersionMeta(FormatVersionMeta(m))
	if !ok {
		t.Fatalf("ParseVersionMeta(%q) not ok", FormatVersionMeta(m))
	}
	if got != m {
		t.Errorf("round trip = %+v, want %+v", got, m)
	}
}

func TestParseVersionMetaRejectsJunk(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "merge aaa", "merge aaa bbb ccc"} {
		if _, ok := ParseVersionMeta(s); ok {
			t.Errorf("ParseVersionMeta(%q) = ok, want not ok", s)
		}
	}
}

func TestWriteVersionSnapshotShapeAndTrailer(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()

	tip := revParse(t, dir, "HEAD")
	m := VersionMeta{Op: "rebase", Ours: tip, Other: tip, Base: tip, Source: "feat/x", Target: "main"}

	sha, err := r.WriteVersionSnapshot(ctx, tip, m, 1700000000)
	if err != nil {
		t.Fatalf("WriteVersionSnapshot: %v", err)
	}
	if sha == "" || sha == tip {
		t.Fatalf("got %q, want a new synthetic commit distinct from the tip", sha)
	}

	// p1 is the snapshotted tip: every reader unwraps it.
	if p1 := strings.TrimSpace(gitOut(t, dir, "rev-parse", sha+"^1")); p1 != tip {
		t.Errorf("first parent = %s, want the tip %s", p1, tip)
	}
	// The tree is the tip's own, so `git log --all -p` shows no diff for it.
	if a, b := gitOut(t, dir, "rev-parse", sha+"^{tree}"), gitOut(t, dir, "rev-parse", tip+"^{tree}"); a != b {
		t.Errorf("tree = %s, want the tip's tree %s", a, b)
	}
	if diff := gitOut(t, dir, "show", "--format=", "--name-only", sha); strings.TrimSpace(diff) != "" {
		t.Errorf("synthetic commit shows a diff: %q", diff)
	}
	// The metadata must come back through ONE for-each-ref trailer atom.
	gitRunDir(t, dir, "", "update-ref", "refs/gg/versions/main/1700000000-rebase", sha)
	out := gitOut(t, dir, "for-each-ref",
		"--format=%(trailers:key=Gg-Meta,valueonly,separator=%x20)", "refs/gg/versions/")
	got, ok := ParseVersionMeta(strings.TrimSpace(out))
	if !ok || got != m {
		t.Errorf("trailer round trip = %+v (ok=%v), want %+v", got, ok, m)
	}
}

func TestWriteVersionSnapshotIsDeterministic(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()
	tip := revParse(t, dir, "HEAD")
	m := VersionMeta{Op: "rebase", Ours: tip, Other: tip, Base: tip, Source: "a", Target: "b"}

	// Fixed identity + fixed dates: the same inputs must yield the same object,
	// and it must not depend on the user's git config.
	a, err := r.WriteVersionSnapshot(ctx, tip, m, 1700000000)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b, err := r.WriteVersionSnapshot(ctx, tip, m, 1700000000)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if a != b {
		t.Errorf("not deterministic: %s then %s", a, b)
	}
}
```

Substitute the package's real test helpers for `revParse`/`gitRunDir`/`gitOut` if named differently.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/git/ -run 'VersionMeta|WriteVersionSnapshot' -v`
Expected: FAIL — `undefined: VersionMeta`.

- [ ] **Step 3: Write the implementation**

Create `internal/git/versionrecord.go`:

```go
package git

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// metaTrailerKey is the ONE trailer a version snapshot carries. It is one key
// holding every field rather than a key per field on purpose: git 2.43 returns
// every value in every atom when a for-each-ref format string contains more
// than one %(trailers:key=…), which would silently corrupt every record.
const metaTrailerKey = "Gg-Meta"

// VersionMeta is what a snapshot records beyond its tip. Ours is the
// contribution being frozen, Other the tip it is landing on or against, and
// Base their merge base — which cannot be recomputed later, because after a
// merge merge-base(target, source) returns the source's tip, not the fork
// point. One-branch ops (amend, reset, undo-commit, delete-branch, restore)
// leave every field but Op empty.
type VersionMeta struct {
	Op             string
	Ours           string
	Other          string
	Base           string
	Source, Target string
}

// HasPreview reports whether this record can render a frozen preview.
func (m VersionMeta) HasPreview() bool { return m.Ours != "" && m.Base != "" }

// FormatVersionMeta renders the trailer value. The two branch names go LAST
// because a refname may contain most characters; the first four fields are
// positional.
func FormatVersionMeta(m VersionMeta) string {
	return strings.Join([]string{m.Op, m.Ours, m.Other, m.Base, m.Source, m.Target}, " ")
}

// ParseVersionMeta reads a trailer value back. Missing optional fields are
// tolerated as empty; fewer than the fixed fields is a parse failure.
func ParseVersionMeta(s string) (VersionMeta, bool) {
	f := strings.Fields(strings.TrimSpace(s))
	if len(f) < 4 {
		return VersionMeta{}, false
	}
	m := VersionMeta{Op: f[0], Ours: f[1], Other: f[2], Base: f[3]}
	if len(f) > 4 {
		m.Source = f[4]
	}
	if len(f) > 5 {
		m.Target = f[5]
	}
	return m, true
}

// WriteVersionSnapshot creates the synthetic commit a version ref points at:
// tree = tip's own tree (so the commit is empty and `git log --all -p` shows
// no diff), first parent = tip (every reader unwraps it), plus Ours as a second
// parent when it differs — the merge case, where Ours is a live branch someone
// may delete. The message is the truth; parents only pin objects against gc.
func (r *Repo) WriteVersionSnapshot(ctx context.Context, tip string, m VersionMeta, unix int64) (string, error) {
	f, err := os.CreateTemp("", "gg-version-msg")
	if err != nil {
		return "", err
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	body := fmt.Sprintf("gg version snapshot (%s)\n\n%s: %s\n", m.Op, metaTrailerKey, FormatVersionMeta(m))
	if _, err := f.WriteString(body); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	b := gitcmd.New("commit-tree").Arg(tip + "^{tree}").Arg("-p", tip)
	if m.Ours != "" && m.Ours != tip {
		b = b.Arg("-p", m.Ours)
	}
	argv := b.Arg("-F", name).ToArgv()

	// Fixed identity and dates: commit-tree needs an identity a fresh repo may
	// not have, and pinning both makes the object deterministic.
	ts := strconv.FormatInt(unix, 10) + " +0000"
	env := []string{
		"GIT_AUTHOR_NAME=gg", "GIT_AUTHOR_EMAIL=gg@localhost", "GIT_AUTHOR_DATE=" + ts,
		"GIT_COMMITTER_NAME=gg", "GIT_COMMITTER_EMAIL=gg@localhost", "GIT_COMMITTER_DATE=" + ts,
	}
	res, err := r.Runner.RunEnv(ctx, "git commit-tree", argv, env)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/git/ -run 'VersionMeta|WriteVersionSnapshot' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions
git add internal/git/versionrecord.go internal/git/versionrecord_test.go
git commit -m "feat(git): synthetic version-snapshot commits carrying a Gg-Meta trailer"
```

---

### Task 4: read the record back

**Files:**
- Modify: `internal/git/refs.go` (a version-specific listing verb)
- Modify: `internal/model/model.go` (`BranchVersion` gains the endpoints)
- Modify: `internal/domain/query.go:632` (`BranchVersions`), `:664` (`AllVersionBranches`)
- Modify: `internal/domain/versions_test.go`

**Interfaces:**
- Consumes: `git.ParseVersionMeta`, `git.VersionMeta`, `git.VersionRefPrefix`, `git.ParseVersionRef`.
- Produces: `(*git.Repo).VersionRefs(ctx context.Context, prefix string) ([]model.BranchVersion, error)`; `model.BranchVersion` gains `Ours, Other, Base, Source, Target string`.

- [ ] **Step 1: Write the failing test**

Append to `internal/domain/versions_test.go`:

```go
func TestBranchVersionsUnwrapsTheSnapshotCommit(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	svc := svcAt(dir)
	ctx := context.Background()

	tip, err := svc.Repo().RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}
	meta := git.VersionMeta{Op: "rebase", Ours: tip, Other: tip, Base: tip, Source: "feat/x", Target: "main"}
	syn, err := svc.Repo().WriteVersionSnapshot(ctx, tip, meta, 1700000000)
	if err != nil {
		t.Fatalf("WriteVersionSnapshot: %v", err)
	}
	if err := svc.Repo().UpdateRef(ctx, git.VersionRef("main", "rebase", 1700000000), syn); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	vs, err := svc.BranchVersions(ctx, "main")
	if err != nil {
		t.Fatalf("BranchVersions: %v", err)
	}
	if len(vs) != 1 {
		t.Fatalf("got %d versions, want 1", len(vs))
	}
	v := vs[0]
	// Hash must be the snapshotted tip, NOT the synthetic commit — restore and
	// every UI that shows the recorded commit depend on this.
	if v.Hash != tip {
		t.Errorf("Hash = %s, want the snapshotted tip %s (synthetic was %s)", v.Hash, tip, syn)
	}
	if v.Ours != tip || v.Base != tip || v.Source != "feat/x" || v.Target != "main" {
		t.Errorf("endpoints = %+v, want the recorded meta", v)
	}
	if v.Op != "rebase" {
		t.Errorf("Op = %q, want rebase", v.Op)
	}
}
```

Add the `git` import to the test file if absent.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/domain/ -run TestBranchVersionsUnwraps -v`
Expected: FAIL — `v.Ours undefined`, and `Hash` is the synthetic commit.

- [ ] **Step 3: Extend the model**

In `internal/model/model.go`, add to `BranchVersion` after `Unix`:

```go
	// Endpoints recorded by the snapshot. Ours is the contribution frozen,
	// Other the tip it was landing on/against, Base their merge base. Empty for
	// a one-branch op (amend/reset/undo-commit/delete-branch/restore), which
	// records no preview.
	Ours, Other, Base string
	Source, Target    string
```

- [ ] **Step 4: Add the listing verb**

Append to `internal/git/refs.go`:

```go
// VersionRefs lists version snapshots under prefix in ONE invocation, unwrapping
// each synthetic commit: Hash is the snapshotted tip (the first parent), and the
// endpoints come from the single Gg-Meta trailer.
//
// The format string must contain exactly ONE %(trailers:key=…) atom — see
// metaTrailerKey in versionrecord.go for why.
func (r *Repo) VersionRefs(ctx context.Context, prefix string) ([]model.BranchVersion, error) {
	const format = "%(refname)%00%(parent)%00%(subject)%00" +
		"%(trailers:key=" + metaTrailerKey + ",valueonly,separator=%x20)"
	argv := gitcmd.New("for-each-ref").Arg("--format="+format, prefix).ToArgv()
	res, err := r.Runner.Run(ctx, "git for-each-ref (gg)", argv)
	if err != nil {
		return nil, err
	}
	var out []model.BranchVersion
	for _, ln := range strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n") {
		parts := strings.Split(ln, "\x00")
		if len(parts) < 4 || parts[0] == "" {
			continue
		}
		_, op, unix, ok := ParseVersionRef(parts[0])
		if !ok {
			continue
		}
		bv := model.BranchVersion{Ref: parts[0], Subject: parts[2], Op: op, Unix: unix}
		// %(parent) lists every parent space-separated; the first is the tip.
		if ps := strings.Fields(parts[1]); len(ps) > 0 {
			bv.Hash = ps[0]
		}
		if m, ok := ParseVersionMeta(parts[3]); ok {
			bv.Ours, bv.Other, bv.Base = m.Ours, m.Other, m.Base
			bv.Source, bv.Target = m.Source, m.Target
			if m.Op != "" {
				bv.Op = m.Op
			}
		}
		out = append(out, bv)
	}
	return out, nil
}
```

- [ ] **Step 4b: Resolve each tip's subject**

`%(subject)` is now the SYNTHETIC commit's subject ("gg version snapshot (rebase)"),
so every row would render identically. The tip's subject must be resolved from
`p1` — but not with a call per ref. After building the list, batch-resolve every
distinct `Hash` in ONE invocation and fill `Subject` from it:

```go
// git log --no-walk --format=%H%x00%s <sha>… prints one line per input commit.
func (r *Repo) subjectsOf(ctx context.Context, shas []string) (map[string]string, error) {
	if len(shas) == 0 {
		return nil, nil
	}
	b := gitcmd.New("log").Arg("--no-walk", "--format=%H%x00%s")
	for _, sha := range shas {
		b = b.Arg(sha)
	}
	res, err := r.Runner.Run(ctx, "git log (subjects)", b.ToArgv())
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, ln := range strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n") {
		h, subj, ok := strings.Cut(ln, "\x00")
		if ok {
			out[h] = subj
		}
	}
	return out, nil
}
```

Call it once from `VersionRefs` with the distinct `Hash` values and overwrite
`Subject`. A sha that fails to resolve keeps whatever `%(subject)` gave.

Add to the Task 4 test: assert `Subject` is the SNAPSHOTTED tip's subject, not
`"gg version snapshot (rebase)"`. Without that assertion this regression ships
silently — every version row in the TUI, CLI and web would read the same.

- [ ] **Step 5: Point the domain queries at it**

In `internal/domain/query.go`, `BranchVersions` and `AllVersionBranches` currently build their results from `ForEachRef` + `ParseVersionRef`. Replace those calls with `s.repo.VersionRefs(ctx, …)` and keep every existing behaviour — the feature gate added by spec 1 (first statement), the newest-first ordering, the same-unix tie-break by ref descending, and the deleted-branch marking in `AllVersionBranches`. The existing tests in `internal/domain/versions_test.go` cover all of those; they must still pass unchanged.

- [ ] **Step 6: Run the suites**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/git/ ./internal/domain/ ./internal/model/`
Expected: PASS, including the pre-existing ordering and tie-break tests.

- [ ] **Step 7: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions
git add internal/git/refs.go internal/model/model.go internal/domain/query.go internal/domain/versions_test.go
git commit -m "feat: read version snapshots back through one for-each-ref, unwrapping the tip"
```

---

### Task 5: write the record from the ops

**Files:**
- Modify: `internal/engine/snapshot_version.go`
- Modify: `internal/engine/gitops.go`
- Modify: the 12 call sites listed below
- Modify: `internal/engine/snapshot_version_test.go`

**Interfaces:**
- Consumes: `GitOps.WriteVersionSnapshot`, `git.VersionMeta`, `GitOps.MergeBase`, `GitOps.RevParse`.
- Produces: `snapshotBranchTip(ctx, deps, branch, opToken string, ours, other string)` — the two new trailing parameters are `""` for one-branch ops.

- [ ] **Step 1: Write the failing test**

Append to `internal/engine/snapshot_version_test.go` two tests, built with the same `deps`/`ctx` helper the neighbouring snapshot tests already use:

1. `TestSnapshotBranchTipRecordsEndpoints` — call `snapshotBranchTip(ctx, deps, "main", "rebase", oursSha, otherSha)`, then read the ref back with `deps.Repo.VersionRefs(ctx, git.VersionRefPrefix)` and assert `Hash` is the pre-op tip, `Ours`/`Other` are what was passed, and `Base` equals `merge-base(ours, other)` computed independently in the test.
2. `TestSnapshotBranchTipOneBranchOpRecordsNoEndpoints` — call it with `"", ""` for a `reset` token and assert `Ours`/`Other`/`Base` come back empty while `Hash` and `Op` are still right.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/engine/ -run TestSnapshotBranchTip -v`
Expected: FAIL — too many arguments to `snapshotBranchTip`.

- [ ] **Step 3: Extend `GitOps`**

In `internal/engine/gitops.go`, add next to the other ref verbs:

```go
	WriteVersionSnapshot(ctx context.Context, tip string, m git.VersionMeta, unix int64) (string, error)
	VersionRefs(ctx context.Context, prefix string) ([]model.BranchVersion, error)
```

`MergeBase` and `RevParse` are already on the interface — check before adding.

- [ ] **Step 4: Rewrite the writer**

In `internal/engine/snapshot_version.go`, change the signature to
`func snapshotBranchTip(ctx context.Context, deps OpDeps, branch, opToken, ours, other string)` and, after resolving `sha` (the pre-op tip) and before `UpdateRef`:

```go
	meta := git.VersionMeta{Op: opToken, Source: branch}
	if ours != "" && other != "" {
		base, berr := deps.Repo.MergeBase(ctx, ours, other)
		if berr == nil && base != "" {
			// Base cannot be recomputed after the op: once the branches have
			// merged, merge-base(target, source) returns the source tip rather
			// than the fork point. Record it or lose it.
			meta.Ours, meta.Other, meta.Base = ours, other, base
		}
	}
	target, err := deps.Repo.WriteVersionSnapshot(ctx, sha, meta, ts)
	if err != nil {
		deps.emit(ctx, Progressf("recording branch version", "skipped: %s", err.Error()))
		return
	}
```

then `UpdateRef(ctx, ref, target)` instead of `sha`. Keep everything else — the `Versions.Enabled` gate, the same-second collision bump, the best-effort contract (a failure emits a note and returns; it never fails the operation), the marker stamp, and the prune.

Set `meta.Target` from `other`'s branch name where the caller knows it; pass it through if you prefer an explicit parameter — but if you change the signature further, update this plan's Interfaces block in your report so later tasks match.

- [ ] **Step 5: Update all 12 call sites**

| File:line | Call becomes |
|---|---|
| `smart_rebase.go:61` | `snapshotBranchTip(ctx, deps, branch, "rebase", tipOf(branch), op.Onto)` |
| `interactive_rebase.go:73` | `…, op.Branch, "interactive-rebase", tipOf(op.Branch), op.Onto` (use the op's onto/upstream field; if it has none, pass `"", ""`) |
| `smart_merge.go:60` | `…, target, "merge", sourceTip, targetTip` — note Ours is the **source** here and p1 is the target's tip |
| `smart_pull.go:124,130` | `…, branch, "pull", tipOf(branch), remote+"/"+branch` — these run after the ff-only pull, so the remote ref is current |
| `smart_pull.go:140` | same as above (the reset path) |
| `reset.go:72`, `undo.go:26`, `ops_basic.go:22`, `delete_branch.go:48`, `restore_branch_version.go:60,73` | append `"", ""` |

Resolve `tipOf(x)` with the existing `deps.Repo.RevParse(ctx, "refs/heads/"+x)`; do not add a helper to `GitOps`.

- [ ] **Step 5b: Fix `RestoreBranchVersion` — it currently restores the WRONG commit**

`internal/engine/restore_branch_version.go:31` does
`sha, err := deps.Repo.RevParse(ctx, op.Ref)` and then `reset --hard sha`
(`:62`). Under the new shape that resolves to the SYNTHETIC commit, so restoring
a version would move the branch onto the snapshot object instead of the tip it
recorded — silently wrong history, and the single most damaging bug this change
can introduce.

Change it to resolve the first parent:

```go
	// The ref points at the synthetic snapshot commit; the tip it recorded is
	// its first parent. Every reader of a version ref unwraps p1.
	sha, err := deps.Repo.RevParse(ctx, op.Ref+"^1")
```

Add a test to `internal/engine/restore_branch_version_test.go`: snapshot a
branch, move it forward, restore, and assert the branch tip equals the ORIGINAL
tip — and that it is not the synthetic commit's own sha.

- [ ] **Step 6: Fix every fake**

Run `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go build ./... && go vet ./...` and fix every implementor of `GitOps` that no longer satisfies it. Note that the fakes in `internal/engine/*_test.go` nil-embed `GitOps` and override selectively, so most need nothing — `go vet ./...` is what proves it.

- [ ] **Step 7: Run the suites**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/engine/ ./internal/domain/`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions
git add internal/engine/ 
git commit -m "feat(engine): snapshots record (ours, other, base)"
```

---

### Task 6: format 2 and the migration declaration

**Files:**
- Modify: `internal/domain/features.go`
- Modify: `internal/engine/snapshot_version.go` (the stamped format)
- Modify: `internal/domain/features_test.go`
- Modify: `internal/i18n/lang/{ja,ko,ru,zh}.toml`

**Interfaces:**
- Consumes: `preflight.Migration`, `preflight.Text`, `VersionsFormat`.
- Produces: `VersionsFormat = 2`; `Features()`'s `versions` entry gains `Migrate`.

- [ ] **Step 1: Write the failing test**

Append to `internal/domain/features_test.go`:

```go
func TestVersionsDeclaresTheFormatTwoMigration(t *testing.T) {
	t.Parallel()
	if VersionsFormat != 2 {
		t.Fatalf("VersionsFormat = %d, want 2", VersionsFormat)
	}
	var versions preflight.Feature
	for _, f := range Features() {
		if f.ID == FeatureVersions {
			versions = f
		}
	}
	if versions.Migrate == nil {
		t.Fatal("versions must declare a migration now that format 2 exists")
	}
	m := versions.Migrate
	if m.Store != StoreVersions || m.From != 1 || m.To != 2 {
		t.Errorf("migration = %+v, want versions 1->2", m)
	}
	if m.Describe == nil {
		t.Fatal("Describe is required: the consent screen has nothing to show without it")
	}
	txt := m.Describe()
	if txt.Format == "" {
		t.Error("Describe returned an empty format")
	}
}

func TestLegacyVersionsResolveAsRepairable(t *testing.T) {
	t.Parallel()
	p := preflight.Probes{
		// Data present, no marker: the pre-marker era, i.e. format 1.
		Stores:     map[string]preflight.StoreProbe{StoreVersions: {Format: 0, HasData: true}},
		GitVersion: [3]int{2, 45, 0},
	}
	for _, v := range preflight.Resolve(Features(), p) {
		if v.Feature.ID != FeatureVersions {
			continue
		}
		if v.State != preflight.Repairable {
			t.Errorf("versions on a legacy repo = %v, want Repairable", v.State)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/domain/ -run 'TestVersionsDeclares|TestLegacyVersions' -v`
Expected: FAIL — `VersionsFormat = 1`, `Migrate` is nil.

- [ ] **Step 3: Declare it**

In `internal/domain/features.go`: set `VersionsFormat = 2`, change the requirement to `preflight.DataFormat{Store: StoreVersions, Min: VersionsFormat, Max: VersionsFormat}` (already written that way), and add:

```go
			Migrate: &preflight.Migration{
				Store: StoreVersions, From: 1, To: 2,
				Describe: func() preflight.Text {
					return preflight.Text{
						Format: "Discards every branch version recorded before this build. They cannot be converted: the old format records no merge base, so they can never open as a preview. Until you migrate, NO new versions are recorded — rebases and merges run without a safety net.",
					}
				},
			},
```

Add that literal as a key to all four bundles with real translations. It is `preflight.Text` written inside `internal/domain`, which the `preflightProseKeys` AST gate scans — a missing bundle key fails the build.

- [ ] **Step 4: Stamp format 2 from the writer**

In `internal/engine/snapshot_version.go`, the stamp call uses the format carried on `VersionsPolicy` (spec 1 moved it there). Confirm `domain` injects `VersionsFormat`, so this now stamps 2 with no change in `engine`. If any literal `1` remains at the stamp site, replace it with `deps.Versions.Format`.

- [ ] **Step 5: Run the suites**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/domain/ ./internal/engine/ && go test ./internal/tui/ -run 'Prose|I18n|i18n|Bundle'`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions
git add internal/domain/features.go internal/domain/features_test.go internal/engine/snapshot_version.go internal/i18n/lang/
git commit -m "feat(domain): versions moves to format 2 with a discard migration"
```

---

### Task 7: `VersionPreview` — the frozen view

**Files:**
- Create: `internal/domain/version_preview.go`
- Create: `internal/domain/version_preview_test.go`

**Interfaces:**
- Consumes: `(*Service).BranchVersions`, `PreviewEndpoints`, `model.Endpoint`, `LogRangeMessages`.
- Produces: `(*Service).VersionPreview(ctx context.Context, ref string) (PreviewEndpoints, error)`, and a sentinel `ErrNoPreview` for a fieldless record.

- [ ] **Step 1: Write the failing test**

Create `internal/domain/version_preview_test.go` with three cases, built with `svcAt(cleanDir(t))` and real refs written via `WriteVersionSnapshot`:

1. A record with endpoints → `VersionPreview` returns `Left.Hash == Base`, `Right.Hash == Ours`, both `Kind: model.EndpointCommit`.
2. A fieldless record (one-branch op) → returns `ErrNoPreview`, so callers render the commit view instead.
3. An unknown ref → a not-found error, not a panic.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/domain/ -run TestVersionPreview -v`
Expected: FAIL — `svc.VersionPreview undefined`.

- [ ] **Step 3: Write the implementation**

Create `internal/domain/version_preview.go`:

```go
package domain

import (
	"context"
	"errors"
	"fmt"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// ErrNoPreview means the record is from a one-branch op (amend, reset,
// undo-commit, delete-branch, restore) and records no endpoints, so there is
// nothing to render as a preview. Callers show the commit view instead.
var ErrNoPreview = errors.New("this version records no preview")

// VersionPreview returns the frozen PR-style endpoints for one version ref:
// left = the merge base recorded at snapshot time, right = the contribution.
// It feeds the existing compare pipeline, which takes commit hashes — so the
// frozen view is a lens over that pipeline, not a second renderer.
func (s *Service) VersionPreview(ctx context.Context, ref string) (PreviewEndpoints, error) {
	if err := s.FeatureDisabledError(ctx, FeatureVersions); err != nil {
		return PreviewEndpoints{}, err
	}
	branch, _, _, ok := git.ParseVersionRef(ref)
	if !ok {
		return PreviewEndpoints{}, fmt.Errorf("version preview: not a version ref: %s", ref)
	}
	vs, err := s.BranchVersions(ctx, branch)
	if err != nil {
		return PreviewEndpoints{}, err
	}
	for _, v := range vs {
		if v.Ref != ref {
			continue
		}
		if v.Base == "" || v.Ours == "" {
			return PreviewEndpoints{}, ErrNoPreview
		}
		return PreviewEndpoints{
			Left:  model.Endpoint{Kind: model.EndpointCommit, Hash: v.Base},
			Right: model.Endpoint{Kind: model.EndpointCommit, Hash: v.Ours},
		}, nil
	}
	return PreviewEndpoints{}, fmt.Errorf("version preview: no such version: %s", ref)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/domain/ -run TestVersionPreview -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions
git add internal/domain/version_preview.go internal/domain/version_preview_test.go
git commit -m "feat(domain): VersionPreview renders a frozen version as compare endpoints"
```

---

### Task 8: drift detection

**Files:**
- Create: `internal/domain/drift.go`
- Create: `internal/domain/drift_test.go`

**Interfaces:**
- Consumes: `(*git.Repo).DiffNameStatus`, `changeset.Compare`, `(*Service).BranchVersions`.
- Produces: `domain.DriftReport{Ref string; Report changeset.Report; Checked bool}`, `(*Service).DriftSince(ctx context.Context, ref, newTip string) (DriftReport, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/domain/drift_test.go`. Build a real repo reproducing the motivating case:

- `main` has `f3.txt`; branch `feat` modifies it and adds `f4.txt`; `main` then deletes `f3.txt`.
- Snapshot `feat` with `Ours = feat tip`, `Other = main tip`.
- Rebase `feat` onto `main` resolving the modify/delete conflict by KEEPING the file.
- `DriftSince(ctx, ref, newFeatTip)` must report exactly one `Added` entry `{Status:'A', Path:"f3.txt"}` and `Drifted() == true`.

Add a second test that is the regression guard for the wrong formula:

- A pull-merge fixture: `Ours = B_old`, `Other = U` (the new upstream), `newTip = M` (the merge commit). Upstream changed files the branch never touched. Assert `Drifted() == false` — the naive `oldTip..newTip` range would report every upstream file and alarm.

And a third: a record whose before-set is empty (fast-forward) returns `Checked == false` and no findings.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/domain/ -run TestDrift -v`
Expected: FAIL — `svc.DriftSince undefined`.

- [ ] **Step 3: Write the implementation**

Create `internal/domain/drift.go`:

```go
package domain

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/changeset"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// DriftReport is the comparison between a frozen change set and the current
// one. Checked is false when there was nothing to compare.
type DriftReport struct {
	Ref     string
	Report  changeset.Report
	Checked bool
}

// DriftSince compares the change set a version froze against the branch's
// change set now.
//
//	before = Base..Ours   (what the branch contributed before the op)
//	after  = Other..newTip (what it contributes now, against the same other side)
//
// One rule for every op path. The tempting `oldTip..newTip` is WRONG for a
// pull-merge: the merge commit's first parent is the branch's own old tip, so
// that range is UPSTREAM's changes and every file upstream touched would be
// reported as drift.
func (s *Service) DriftSince(ctx context.Context, ref, newTip string) (DriftReport, error) {
	out := DriftReport{Ref: ref}
	branch, _, _, ok := git.ParseVersionRef(ref)
	if !ok {
		return out, fmt.Errorf("drift: not a version ref: %s", ref)
	}
	vs, err := s.BranchVersions(ctx, branch)
	if err != nil {
		return out, err
	}
	var rec *model.BranchVersion
	for i := range vs {
		if vs[i].Ref == ref {
			rec = &vs[i]
			break
		}
	}
	if rec == nil || rec.Base == "" || rec.Ours == "" || rec.Other == "" || newTip == "" {
		return out, nil // nothing recorded to compare against
	}

	before, err := s.repo.DiffNameStatus(ctx, rec.Base, rec.Ours)
	if err != nil {
		return out, err
	}
	// Skip a branch that contributed nothing — a fast-forward pull, or a branch
	// with nothing ahead. Covers those without op-path special-casing.
	if len(before) == 0 {
		return out, nil
	}
	after, err := s.repo.DiffNameStatus(ctx, rec.Other, newTip)
	if err != nil {
		return out, err
	}
	out.Report, out.Checked = changeset.Compare(before, after), true
	return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/domain/ -run TestDrift -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions
git add internal/domain/drift.go internal/domain/drift_test.go
git commit -m "feat(domain): DriftSince compares a frozen change set against the current one"
```

---

### Task 9: run detection after the op

**Files:**
- Modify: `internal/domain/execute.go` or `internal/domain/service.go` (wherever `Execute` returns)
- Create: `internal/domain/drift_after_op_test.go`

**Interfaces:**
- Consumes: `(*Service).DriftSince`, `(*Service).BranchVersions`.
- Produces: `(*Service).DriftAfter(ctx context.Context, branch string) (DriftReport, error)` — resolves the newest version ref for `branch` and the branch's current tip, then calls `DriftSince`.

- [ ] **Step 1: Write the failing test**

Create `internal/domain/drift_after_op_test.go` asserting:

1. `DriftAfter` on a branch with a drifting newest version reports it.
2. `DriftAfter` on a branch with no versions returns `Checked == false` and no error.
3. **Lock ordering:** the two `name-status` diffs do NOT run while a repo-gate reservation is held. Assert it the way spec 1's `TestExecuteResolvesPreflightBeforeAcquiringTheGate` does — inspect the gate's queue from inside a probe, or assert the call ordering — and make sure reverting the placement fails the test.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/domain/ -run TestDriftAfter -v`
Expected: FAIL — `svc.DriftAfter undefined`.

- [ ] **Step 3: Implement and wire**

Append to `internal/domain/drift.go`:

```go
// DriftAfter compares the branch's newest recorded version against its tip now.
// Frontends call this AFTER Execute has returned — never from inside it. The two
// name-status diffs must not run under a repo-gate reservation: spec 1 shipped
// exactly that bug (preflight probes under an exclusive TreeWrite hold) and
// fixed it.
func (s *Service) DriftAfter(ctx context.Context, branch string) (DriftReport, error) {
	vs, err := s.BranchVersions(ctx, branch)
	if err != nil {
		var disabled *ErrFeatureDisabled
		if errors.As(err, &disabled) {
			return DriftReport{}, nil // feature off: nothing recorded, nothing to say
		}
		return DriftReport{}, err
	}
	if len(vs) == 0 {
		return DriftReport{}, nil
	}
	tip, err := s.repo.RevParse(ctx, "refs/heads/"+branch)
	if err != nil || tip == "" {
		return DriftReport{}, nil
	}
	// BranchVersions is newest-first.
	return s.DriftSince(ctx, vs[0].Ref, tip)
}
```

Add `"errors"` to the file's imports. Call it from the frontends (Tasks 10–12), never from inside `Execute`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/domain/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions
git add internal/domain/
git commit -m "feat(domain): DriftAfter resolves the newest version and compares"
```

---

### Task 10: CLI surface

**Files:**
- Modify: `internal/cli/versions.go`
- Modify: the rebase/merge/pull command files in `internal/cli`
- Create: `internal/cli/drift_test.go`

**Interfaces:**
- Consumes: `(*domain.Service).DriftAfter`, `DriftReport`, `(*domain.Service).VersionPreview`.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/drift_test.go` using the package's existing CLI-runner helper. Build the resurrection fixture (as in Task 8), run `gg rebase`, and assert stdout names the resurrected path and says the change set changed. Add a second case where nothing drifted and assert the summary is absent (unless the op paused — see step 3).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/cli/ -run TestDrift -v`
Expected: FAIL — no drift output.

- [ ] **Step 3: Print the summary**

After a successful `rebase`/`merge`/`pull`, call `DriftAfter` and print, in English (CLI prose is never translated):

- when `Report.Drifted()` — a line per `Added` entry, headed by a statement that the operation changed the branch's change set;
- `Removed` entries quietly beneath, labelled as absorbed upstream;
- the whole summary also when the op **paused for conflicts** (i.e. completed via resume), even if nothing drifted. Detect the paused case from the op result/`conflictState`, not from a persisted flag.

Also extend `gg versions` so a version with endpoints can print its frozen change set via `VersionPreview`, and a fieldless one says it records no preview.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/cli/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions
git add internal/cli/
git commit -m "feat(cli): report change-set drift after rebase, merge and pull"
```

---

### Task 11: TUI surface

**Files:**
- Modify: `internal/tui/versions_popup.go`
- Modify: `internal/tui/notify.go`
- Modify: `internal/i18n/lang/{ja,ko,ru,zh}.toml`
- Create: `internal/tui/drift_test.go`

**Interfaces:**
- Consumes: `(*domain.Service).VersionPreview`, `ErrNoPreview`, `(*domain.Service).DriftAfter`.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/drift_test.go` asserting: a version with endpoints opens the compare/preview layer rather than the commit view; a fieldless one still opens the commit view (`ErrNoPreview` is not an error the user sees); and a drifting op produces a notice naming the resurrected path.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/tui/ -run TestDrift -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

In the versions popup, enter on a row calls `VersionPreview`; on success push the existing compare layer with those endpoints, on `ErrNoPreview` keep today's commit view. After a rebase/merge/pull completes, call `DriftAfter` and raise a notice when it drifted or the op paused.

Every new string goes through `i18n.T` with a literal key present in all four bundles, with real translations. Remember `renderVerdictReason`/`renderMigrationConsequence` in `internal/tui/i18n_preflight.go` as the precedent for rendering structured text — do not `fmt.Sprintf` user-visible text.

- [ ] **Step 4: Run the suite and the gates**

Run in the FOREGROUND: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/tui/ -run 'Prose|I18n|i18n|Bundle|Vocab|Menu|Drift|Version' -v`, then the full `go test ./internal/tui/` once.
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions
git add internal/tui/ internal/i18n/lang/
git commit -m "feat(tui): versions open as the frozen preview; drift raises a notice"
```

---

### Task 12: web surface

**Files:**
- Modify: `internal/web/versions.go`
- Modify: `internal/web/static/` (the versions group + op result panel)
- Create: `internal/web/drift_test.go`

**Interfaces:**
- Consumes: `(*domain.Service).VersionPreview`, `(*domain.Service).DriftAfter`.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Write the failing test**

Create `internal/web/drift_test.go` asserting the version-preview endpoint returns the two hashes as JSON, that a fieldless record reports "no preview" rather than erroring, that a drift endpoint reports the added entries, and that all of them keep the loopback/Host/Origin guards (assert a rejected cross-origin request the way the neighbouring handler tests do).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/web/ -run TestDrift -v`
Expected: FAIL — 404.

- [ ] **Step 3: Implement**

Add the read endpoints (GET; no writes here, so no `writeGuard` needed — but any endpoint that mutates must have it). In the SPA, a version row opens the existing compare view with the returned hashes, and the op result panel shows the drift summary.

Per-id hiding rule: a new element with `class="hidden"` and no matching `#id.hidden` CSS rule is ALWAYS visible. Give any new panel an id and a matching rule, and assert the rule exists in a test.

- [ ] **Step 4: Run the suite**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && go test ./internal/web/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions
git add internal/web/
git commit -m "feat(web): frozen version previews and a drift summary"
```

---

### Task 13: e2e, docs and the full gate

**Files:**
- Create: `e2e/scenarios/version-drift.toml`
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`
- Modify: `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go`

- [ ] **Step 1: e2e scenario**

Read `.claude/skills/writing-e2e-scenarios/SKILL.md` and two existing scenarios first. Write a scenario that builds the resurrection fixture, rebases, and asserts the CLI output names the resurrected file. If the harness cannot express a conflicted rebase with a chosen resolution, say so in your report and cover what it can — do not invent harness features.

- [ ] **Step 2: Docs**

- `CHANGELOG.md`: the frozen preview, drift detection, and that upgrading **discards** pre-existing version records (and that skipping the migration means no new versions are recorded).
- `README.md`: what a version now shows, and what the drift summary means.
- `CLAUDE.md`: one row for `changeset` in the package-map table, in the existing style, and update the `git` row only if its one-line summary is now wrong.
- `internal/agentskill/using-gg.md`: the drift summary now appears in `gg rebase`/`merge`/`pull` output. Bump `agentskill.Version` (it was 69 after spec 1; set 70). A skill-text edit without the bump is silently skipped by installed markers. Do NOT run `gg init --update`. If `TestDogfoodSkillCopyInSync` fails, regenerate `.claude/skills/using-gg/SKILL.md` from `agentskill.SkillFile()`.

- [ ] **Step 3: Full gate**

Run, in the FOREGROUND: `cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions && ./test.sh && ./test.sh race`
Expected: both PASS.

- [ ] **Step 4: Commit and build a verify binary**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-frozen-preview-versions
git add CHANGELOG.md README.md CLAUDE.md internal/agentskill/ .claude/skills/ e2e/scenarios/version-drift.toml
git commit -m "docs: frozen preview versions, drift detection, format-2 migration"
go build -o /tmp/gg-frozen-versions ./cmd/gg
```

Report the binary path to the user.
