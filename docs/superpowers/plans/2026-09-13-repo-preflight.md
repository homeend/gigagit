# Repo Preflight & Feature Requirements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let every gg feature declare what it needs (data-format range, minimum git version, criticality) and have the runtime decide at repo-open whether to initialise it, ask consent for a destructive migration, disable it, or refuse to start.

**Architecture:** A new DAG-leaf package `internal/preflight` holds the feature registry, the requirement kinds and a pure resolver that maps probe results to verdicts. `internal/domain` runs the probes (two `for-each-ref` invocations and one `git version`), caches the verdicts on the `Service`, and exposes `FeatureEnabled`. The destructive migration is an engine `Operation` run through `Execute` under `repogate.RefWrite`. Frontends only render verdicts.

**Tech Stack:** Go 1.26, stdlib only for the new package. Existing seams: `gitcmd` argv builder, `gitexec.FakeRunner`, `engine.Operation`, `domain.Execute`, `i18n` TOML bundles.

**Spec:** `docs/superpowers/specs/2026-09-13-repo-preflight-design.md`

## Global Constraints

- Work in the worktree `/mnt/t/others/gigagit.worktrees/feat-repo-preflight` on branch `feat/repo-preflight`. Prefix every shell command with `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight` (the shell cwd resets between calls) and use absolute paths for Write/Edit.
- `internal/preflight` is a **DAG leaf**: stdlib imports only. It must not import `engine`, `git`, `model`, `domain`, or anything else in `internal/`. `internal/archtest` enforces this.
- `internal/tui`, `internal/cli`, `internal/mcp`, `internal/web` must never import `internal/git`. They reach git through `internal/domain`.
- A git verb is **one invocation**, built with `gitcmd`, run through `r.Runner`. Never shell out directly.
- Every user-visible TUI string goes through `i18n.T` with a **literal** key present in all four bundles (`ja`, `ko`, `zh`, `ru`). Engine/CLI prose stays English.
- Feature IDs, verdict names and op tokens are English protocol values: `"core"`, `"versions"`, `"migrate"`.
- Marker format numbers: `versions` is at format **1** throughout this plan. Format 2 arrives in the follow-up spec; do not write it here.
- Tests use a real `git` in `t.TempDir()` (`newRepo`/`newTestRepo` helpers) or `gitexec.FakeRunner` for argv assertions. New tests call `t.Parallel()` unless they touch global state.
- Run `./test.sh` before each commit; `./test.sh race` before the final one.

---

### Task 1: `internal/preflight` — types and the resolver

**Files:**
- Create: `internal/preflight/preflight.go`
- Create: `internal/preflight/preflight_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `preflight.Criticality` (`Optional`, `Required`), `preflight.State` (`Satisfied`, `Repairable`, `Unsatisfiable`), `preflight.Fit` (`FitOK`, `FitTooOld`, `FitTooNew`), `preflight.Text{Format string; Args []any}`, `preflight.StoreProbe{Format int; HasData bool}`, `preflight.Probes{Stores map[string]StoreProbe; GitVersion [3]int}`, `preflight.Requirement` interface, `preflight.DataFormat{Store string; Min, Max int}`, `preflight.GitVersion{Min [3]int}`, `preflight.Migration{Store string; From, To int; Describe func() Text}`, `preflight.Feature{ID string; Criticality Criticality; Requires []Requirement; Migrate *Migration}`, `preflight.Verdict{Feature Feature; State State; Reason Text}`, `preflight.Resolve(fs []Feature, p Probes) []Verdict`.

- [ ] **Step 1: Write the failing test**

Create `internal/preflight/preflight_test.go`:

```go
package preflight

import "testing"

func probes(format int, hasData bool, git [3]int) Probes {
	return Probes{
		Stores:     map[string]StoreProbe{"versions": {Format: format, HasData: hasData}},
		GitVersion: git,
	}
}

func versionsFeature(migrate *Migration) Feature {
	return Feature{
		ID:          "versions",
		Criticality: Optional,
		Requires:    []Requirement{DataFormat{Store: "versions", Min: 2, Max: 2}},
		Migrate:     migrate,
	}
}

func TestResolveStates(t *testing.T) {
	t.Parallel()
	mig := &Migration{Store: "versions", From: 1, To: 2,
		Describe: func() Text { return Text{Format: "discards %d snapshots", Args: []any{3}} }}

	cases := []struct {
		name    string
		feature Feature
		probes  Probes
		want    State
	}{
		{"in range", versionsFeature(mig), probes(2, true, [3]int{2, 40, 0}), Satisfied},
		{"no data, no marker", versionsFeature(mig), probes(0, false, [3]int{2, 40, 0}), Satisfied},
		{"legacy: data without marker is format 1", versionsFeature(mig), probes(0, true, [3]int{2, 40, 0}), Repairable},
		{"below range with migration", versionsFeature(mig), probes(1, true, [3]int{2, 40, 0}), Repairable},
		{"below range without migration", versionsFeature(nil), probes(1, true, [3]int{2, 40, 0}), Unsatisfiable},
		{"above range is never repairable", versionsFeature(mig), probes(3, true, [3]int{2, 40, 0}), Unsatisfiable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Resolve([]Feature{tc.feature}, tc.probes)
			if len(got) != 1 {
				t.Fatalf("Resolve returned %d verdicts, want 1", len(got))
			}
			if got[0].State != tc.want {
				t.Errorf("state = %v, want %v", got[0].State, tc.want)
			}
		})
	}
}

func TestResolveGitVersionIsNeverRepairable(t *testing.T) {
	t.Parallel()
	f := Feature{
		ID:          "core",
		Criticality: Required,
		Requires:    []Requirement{GitVersion{Min: [3]int{2, 40, 0}}},
		Migrate:     &Migration{Store: "versions", From: 1, To: 2, Describe: func() Text { return Text{} }},
	}
	got := Resolve([]Feature{f}, probes(2, true, [3]int{2, 30, 0}))
	if got[0].State != Unsatisfiable {
		t.Errorf("state = %v, want Unsatisfiable (a migration for another store must not repair git)", got[0].State)
	}
}

func TestResolveTakesWorstRequirement(t *testing.T) {
	t.Parallel()
	f := Feature{
		ID:          "versions",
		Criticality: Optional,
		Requires: []Requirement{
			DataFormat{Store: "versions", Min: 1, Max: 9}, // satisfied
			GitVersion{Min: [3]int{9, 0, 0}},              // unsatisfiable
		},
	}
	got := Resolve([]Feature{f}, probes(1, true, [3]int{2, 40, 0}))
	if got[0].State != Unsatisfiable {
		t.Errorf("state = %v, want Unsatisfiable", got[0].State)
	}
	if got[0].Reason.Format == "" {
		t.Error("Reason must describe the failing requirement")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/preflight/ -run TestResolve -v`
Expected: FAIL — the package does not compile (`undefined: Resolve`, `undefined: Probes`, …).

- [ ] **Step 3: Write minimal implementation**

Create `internal/preflight/preflight.go`:

```go
// Package preflight resolves what a gg feature needs against what a repository
// actually offers. It is a DAG leaf: stdlib only. It never runs git — callers
// hand it probe results and it returns verdicts, so the whole decision table is
// testable with plain values.
package preflight

import "fmt"

// Criticality says what happens when a feature cannot be satisfied.
type Criticality int

const (
	Optional Criticality = iota // gg runs with the feature disabled
	Required                    // gg explains and exits
)

// State is a feature's verdict. The zero value is Satisfied, and the ordering
// is significant: Resolve keeps the WORST state across a feature's
// requirements, so Unsatisfiable must sort above Repairable.
type State int

const (
	Satisfied State = iota
	Repairable
	Unsatisfiable
)

func (s State) String() string {
	switch s {
	case Satisfied:
		return "satisfied"
	case Repairable:
		return "repairable"
	case Unsatisfiable:
		return "unsatisfiable"
	}
	return "unknown"
}

// Fit is how one requirement sits against the probes.
type Fit int

const (
	FitOK Fit = iota
	FitTooOld
	FitTooNew
)

// Text is consent/reason prose. Deliberately the same shape as engine.Msg;
// domain converts at the boundary. preflight cannot import engine without
// breaking the DAG.
type Text struct {
	Format string
	Args   []any
}

// StoreProbe is what the resolver knows about one gg-owned store. Format is
// the marker value; 0 means NO MARKER, which is resolved against HasData —
// data without a marker is the pre-marker era, i.e. format 1.
type StoreProbe struct {
	Format  int
	HasData bool
}

// Probes is everything the resolver is allowed to look at.
type Probes struct {
	Stores     map[string]StoreProbe
	GitVersion [3]int
}

// Requirement is one thing a feature needs.
type Requirement interface {
	Fit(p Probes) Fit
	Reason(p Probes) Text
	// RepairStore names the store a migration could repair, or "" when nothing
	// can (a git version is never migratable).
	RepairStore() string
}

// DataFormat requires a store to sit within an inclusive format range. The
// range — rather than one expected number — exists for the above-Max case: an
// older gg cannot downgrade data a newer gg wrote, so it must disable the
// feature rather than touch it.
type DataFormat struct {
	Store    string
	Min, Max int
}

func (d DataFormat) found(p Probes) (int, bool) {
	s := p.Stores[d.Store]
	if s.Format == 0 {
		if !s.HasData {
			return 0, false // nothing stored yet: nothing to check
		}
		return 1, true // legacy: data without a marker is format 1 by definition
	}
	return s.Format, true
}

func (d DataFormat) Fit(p Probes) Fit {
	n, ok := d.found(p)
	if !ok {
		return FitOK
	}
	switch {
	case n < d.Min:
		return FitTooOld
	case n > d.Max:
		return FitTooNew
	}
	return FitOK
}

func (d DataFormat) Reason(p Probes) Text {
	n, _ := d.found(p)
	return Text{
		Format: "the %s store is at format %d; this build needs %d-%d",
		Args:   []any{d.Store, n, d.Min, d.Max},
	}
}

func (d DataFormat) RepairStore() string { return d.Store }

// GitVersion requires a minimum git binary version. Never repairable.
type GitVersion struct {
	Min [3]int
}

func (g GitVersion) Fit(p Probes) Fit {
	if less(p.GitVersion, g.Min) {
		return FitTooOld
	}
	return FitOK
}

func (g GitVersion) Reason(p Probes) Text {
	return Text{
		Format: "git %s is installed; this build needs %s or newer",
		Args:   []any{verString(p.GitVersion), verString(g.Min)},
	}
}

func (g GitVersion) RepairStore() string { return "" }

func less(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func verString(v [3]int) string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

// Migration repairs one store from a lower format. Describe returns the
// consequence prose shown before consent.
type Migration struct {
	Store    string
	From, To int
	Describe func() Text
}

// Feature is one declared unit of gg behaviour.
type Feature struct {
	ID          string // English protocol value
	Criticality Criticality
	Requires    []Requirement
	Migrate     *Migration // nil when nothing is repairable
}

// Verdict is a feature's resolved state plus, when not Satisfied, why.
type Verdict struct {
	Feature Feature
	State   State
	Reason  Text
}

// Resolve evaluates every feature against the probes. A feature takes the
// worst verdict among its requirements.
func Resolve(fs []Feature, p Probes) []Verdict {
	out := make([]Verdict, 0, len(fs))
	for _, f := range fs {
		v := Verdict{Feature: f, State: Satisfied}
		for _, r := range f.Requires {
			st := Satisfied
			switch r.Fit(p) {
			case FitTooNew:
				st = Unsatisfiable
			case FitTooOld:
				if f.Migrate != nil && r.RepairStore() != "" && f.Migrate.Store == r.RepairStore() {
					st = Repairable
				} else {
					st = Unsatisfiable
				}
			}
			if st > v.State {
				v.State, v.Reason = st, r.Reason(p)
			}
		}
		out = append(out, v)
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/preflight/ -v`
Expected: PASS, all subtests.

- [ ] **Step 5: Add the archtest leaf guard**

Open `internal/archtest/import_guard_test.go`, find the existing table of leaf packages (the entries asserting a package imports nothing from `internal/`), and add a `preflight` row in the same shape as the neighbouring leaf entries (e.g. `gitconfdocs`, `commitgraph`). Match the existing style exactly rather than inventing a new assertion.

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/archtest/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight
git add internal/preflight/ internal/archtest/import_guard_test.go
git commit -m "feat(preflight): feature requirement registry and resolver"
```

---

### Task 2: git verbs — version probe and empty-tree object

**Files:**
- Create: `internal/git/version.go`
- Create: `internal/git/version_test.go`
- Modify: `internal/git/refs.go` (append `EmptyTree`)
- Create: `internal/git/emptytree_test.go`

**Interfaces:**
- Consumes: `gitcmd.New`, `r.Runner.Run`, the existing `newRepo` test helper in `internal/git`.
- Produces: `git.ParseGitVersion(s string) ([3]int, error)`, `(*git.Repo).GitVersion(ctx) ([3]int, error)`, `(*git.Repo).EmptyTree(ctx) (string, error)`.

- [ ] **Step 1: Write the failing parser test**

Create `internal/git/version_test.go`:

```go
package git

import "testing"

func TestParseGitVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want [3]int
	}{
		{"git version 2.43.0", [3]int{2, 43, 0}},
		{"git version 2.39.3 (Apple Git-145)", [3]int{2, 39, 3}},
		{"git version 2.43.0.windows.1", [3]int{2, 43, 0}},
		{"git version 2.45\n", [3]int{2, 45, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseGitVersion(tc.in)
			if err != nil {
				t.Fatalf("ParseGitVersion(%q) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseGitVersion(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseGitVersionRejectsGarbage(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "not a version", "git version x.y.z"} {
		if _, err := ParseGitVersion(in); err == nil {
			t.Errorf("ParseGitVersion(%q) = nil error, want an error", in)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/git/ -run TestParseGitVersion -v`
Expected: FAIL — `undefined: ParseGitVersion`.

- [ ] **Step 3: Write the parser and the verb**

Create `internal/git/version.go`:

```go
package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// ParseGitVersion extracts major.minor.patch from `git version` output. Git
// appends vendor suffixes ("2.39.3 (Apple Git-145)", "2.43.0.windows.1") and
// may omit the patch entirely, so only the first three dot-separated numeric
// fields are read and a missing patch is 0.
func ParseGitVersion(s string) ([3]int, error) {
	var out [3]int
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) < 3 || fields[0] != "git" || fields[1] != "version" {
		return out, fmt.Errorf("unrecognised git version line %q", strings.TrimSpace(s))
	}
	parts := strings.Split(fields[2], ".")
	for i := 0; i < 3 && i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			if i == 0 {
				return [3]int{}, fmt.Errorf("unrecognised git version %q", fields[2])
			}
			break // vendor suffix: stop at the first non-numeric field
		}
		out[i] = n
	}
	if out == [3]int{} {
		return out, fmt.Errorf("unrecognised git version %q", fields[2])
	}
	return out, nil
}

// GitVersion reports the installed git binary's version.
func (r *Repo) GitVersion(ctx context.Context) ([3]int, error) {
	res, err := r.Runner.Run(ctx, "git version", gitcmd.New("version").ToArgv())
	if err != nil {
		return [3]int{}, err
	}
	return ParseGitVersion(res.Stdout)
}
```

- [ ] **Step 4: Run the parser tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/git/ -run TestParseGitVersion -v`
Expected: PASS.

- [ ] **Step 4b: Add the FakeRunner argv assertion**

Append to `internal/git/version_test.go`:

```go
func TestGitVersionArgv(t *testing.T) {
	t.Parallel()
	fake := gitexec.NewFakeRunner()
	fake.SetAll(gitexec.Result{Stdout: "git version 2.43.0\n"})
	r := &Repo{Runner: fake}

	got, err := r.GitVersion(context.Background())
	if err != nil {
		t.Fatalf("GitVersion: %v", err)
	}
	if want := ([3]int{2, 43, 0}); got != want {
		t.Errorf("GitVersion = %v, want %v", got, want)
	}
	if len(fake.Calls) != 1 {
		t.Fatalf("got %d git invocations, want exactly 1", len(fake.Calls))
	}
	if argv := fake.Calls[0].Argv; len(argv) == 0 || argv[0] != "version" {
		t.Errorf("argv = %v, want it to start with \"version\"", argv)
	}
}
```

Match `FakeRunner`'s real constructor, seeding method and call-record field names to how the other `internal/git` argv tests use it — grep the package for an existing `FakeRunner` test and copy its shape.

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/git/ -run TestGitVersionArgv -v`
Expected: PASS.

- [ ] **Step 5: Write the failing empty-tree test**

Create `internal/git/emptytree_test.go`:

```go
package git

import (
	"context"
	"testing"
)

func TestEmptyTreeIsWritableAndStable(t *testing.T) {
	t.Parallel()
	r := newRepo(t)
	ctx := context.Background()

	sha, err := r.EmptyTree(ctx)
	if err != nil {
		t.Fatalf("EmptyTree: %v", err)
	}
	if sha == "" {
		t.Fatal("EmptyTree returned an empty sha")
	}

	// The object must exist in the odb: update-ref refuses a missing target.
	if err := r.UpdateRef(ctx, "refs/gg/meta/probe/1", sha); err != nil {
		t.Fatalf("UpdateRef to the empty tree: %v", err)
	}

	again, err := r.EmptyTree(ctx)
	if err != nil {
		t.Fatalf("EmptyTree (second call): %v", err)
	}
	if again != sha {
		t.Errorf("EmptyTree is not stable: %q then %q", sha, again)
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/git/ -run TestEmptyTree -v`
Expected: FAIL — `r.EmptyTree undefined`.

- [ ] **Step 7: Implement `EmptyTree`**

Append to `internal/git/refs.go`:

```go
// EmptyTree writes (idempotently) the empty tree object and returns its id. It
// exists so marker refs have a target that pins nothing meaningful: the format
// number lives in the REF NAME, and the object is only there because
// update-ref requires one.
//
// The id is a constant per hash algorithm, but it is not hardcoded — a
// sha256 repository has a different one. `hash-object` is given an empty
// temporary FILE rather than /dev/null or --stdin: the Runner has no stdin,
// and /dev/null is not portable to Windows.
func (r *Repo) EmptyTree(ctx context.Context) (string, error) {
	f, err := os.CreateTemp("", "gg-empty-tree")
	if err != nil {
		return "", err
	}
	name := f.Name()
	_ = f.Close()
	defer func() { _ = os.Remove(name) }()

	argv := gitcmd.New("hash-object").Arg("-t", "tree", "-w", name).ToArgv()
	res, err := r.Runner.Run(ctx, "git hash-object", argv)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
```

Add `"os"` to `internal/git/refs.go`'s import block if it is not already there.

- [ ] **Step 8: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/git/ -run 'TestEmptyTree|TestParseGitVersion' -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight
git add internal/git/version.go internal/git/version_test.go internal/git/refs.go internal/git/emptytree_test.go
git commit -m "feat(git): git-version probe and empty-tree verb"
```

---

### Task 3: marker refs — naming and stamping

**Files:**
- Create: `internal/git/metaref.go`
- Create: `internal/git/metaref_test.go`

**Interfaces:**
- Consumes: `(*git.Repo).ForEachRef`, `(*git.Repo).UpdateRef`, `(*git.Repo).DeleteRef`, `(*git.Repo).EmptyTree` (Task 2).
- Produces: `git.MetaRefPrefix` (`"refs/gg/meta/"`), `git.MetaRef(store string, format int) string`, `git.ParseMetaRef(ref string) (store string, format int, ok bool)`, `(*git.Repo).StoreFormats(ctx) (map[string]int, error)`, `(*git.Repo).StampStoreFormat(ctx, store string, format int) error`.

- [ ] **Step 1: Write the failing test**

Create `internal/git/metaref_test.go`:

```go
package git

import (
	"context"
	"testing"
)

func TestMetaRefRoundTrip(t *testing.T) {
	t.Parallel()
	ref := MetaRef("versions", 2)
	if want := "refs/gg/meta/versions/2"; ref != want {
		t.Fatalf("MetaRef = %q, want %q", ref, want)
	}
	store, format, ok := ParseMetaRef(ref)
	if !ok || store != "versions" || format != 2 {
		t.Errorf("ParseMetaRef(%q) = (%q, %d, %v), want (versions, 2, true)", ref, store, format, ok)
	}
}

func TestParseMetaRefRejectsOtherRefs(t *testing.T) {
	t.Parallel()
	for _, ref := range []string{
		"refs/heads/main",
		"refs/gg/versions/main/1700000000-rebase",
		"refs/gg/meta/versions",   // no format segment
		"refs/gg/meta/versions/x", // non-numeric format
	} {
		if _, _, ok := ParseMetaRef(ref); ok {
			t.Errorf("ParseMetaRef(%q) = ok, want not ok", ref)
		}
	}
}

func TestStampAndReadStoreFormats(t *testing.T) {
	t.Parallel()
	r := newRepo(t)
	ctx := context.Background()

	got, err := r.StoreFormats(ctx)
	if err != nil {
		t.Fatalf("StoreFormats on a fresh repo: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("StoreFormats = %v, want empty", got)
	}

	if err := r.StampStoreFormat(ctx, "versions", 1); err != nil {
		t.Fatalf("StampStoreFormat: %v", err)
	}
	got, err = r.StoreFormats(ctx)
	if err != nil {
		t.Fatalf("StoreFormats: %v", err)
	}
	if got["versions"] != 1 {
		t.Errorf("StoreFormats = %v, want versions=1", got)
	}
}

func TestStampStoreFormatReplacesTheOldMarker(t *testing.T) {
	t.Parallel()
	r := newRepo(t)
	ctx := context.Background()

	if err := r.StampStoreFormat(ctx, "versions", 1); err != nil {
		t.Fatalf("StampStoreFormat(1): %v", err)
	}
	if err := r.StampStoreFormat(ctx, "versions", 2); err != nil {
		t.Fatalf("StampStoreFormat(2): %v", err)
	}

	got, err := r.StoreFormats(ctx)
	if err != nil {
		t.Fatalf("StoreFormats: %v", err)
	}
	if got["versions"] != 2 {
		t.Errorf("StoreFormats = %v, want versions=2", got)
	}
	refs, err := r.ForEachRef(ctx, MetaRefPrefix)
	if err != nil {
		t.Fatalf("ForEachRef: %v", err)
	}
	if len(refs) != 1 {
		t.Errorf("got %d marker refs, want exactly 1 (the old one must be removed)", len(refs))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/git/ -run 'MetaRef|StoreFormat' -v`
Expected: FAIL — `undefined: MetaRef`.

- [ ] **Step 3: Write the implementation**

Create `internal/git/metaref.go`:

```go
package git

import (
	"context"
	"strconv"
	"strings"
)

// MetaRefPrefix is the namespace of gg store-format markers:
// refs/gg/meta/<store>/<format>. The format number lives in the REF NAME so a
// single for-each-ref reads every marker — no blob writes, no cat-file. Like
// refs/gg/versions/, it sits outside refs/heads|tags|remotes, so it is never
// pushed or fetched and is shared by all worktrees via the common dir.
const MetaRefPrefix = "refs/gg/meta/"

// MetaRef builds the marker ref for store at format.
func MetaRef(store string, format int) string {
	return MetaRefPrefix + store + "/" + strconv.Itoa(format)
}

// ParseMetaRef splits a marker ref back into store and format. Store names
// never contain "/", so the split is on the last separator.
func ParseMetaRef(ref string) (store string, format int, ok bool) {
	rest, found := strings.CutPrefix(ref, MetaRefPrefix)
	if !found {
		return "", 0, false
	}
	i := strings.LastIndex(rest, "/")
	if i <= 0 || i == len(rest)-1 {
		return "", 0, false
	}
	n, err := strconv.Atoi(rest[i+1:])
	if err != nil {
		return "", 0, false
	}
	return rest[:i], n, true
}

// StoreFormats reads every marker in one invocation. A store with no marker is
// simply absent from the map; the caller resolves that against whether the
// store holds data (see preflight.DataFormat).
func (r *Repo) StoreFormats(ctx context.Context) (map[string]int, error) {
	infos, err := r.ForEachRef(ctx, MetaRefPrefix)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(infos))
	for _, info := range infos {
		store, format, ok := ParseMetaRef(info.Ref)
		if !ok {
			continue
		}
		// Defensive: a repo carrying two markers for one store keeps the
		// higher, so a half-finished migration reads as the newer format
		// rather than silently as the older one.
		if format > out[store] {
			out[store] = format
		}
	}
	return out, nil
}

// StampStoreFormat records store at format, removing any other marker for that
// store. Called by a store's WRITER on first write — never at startup.
func (r *Repo) StampStoreFormat(ctx context.Context, store string, format int) error {
	sha, err := r.EmptyTree(ctx)
	if err != nil {
		return err
	}
	if err := r.UpdateRef(ctx, MetaRef(store, format), sha); err != nil {
		return err
	}
	infos, err := r.ForEachRef(ctx, MetaRefPrefix+store+"/")
	if err != nil {
		return nil // the marker is written; pruning stale ones is best-effort
	}
	for _, info := range infos {
		s, f, ok := ParseMetaRef(info.Ref)
		if !ok || s != store || f == format {
			continue
		}
		_ = r.DeleteRef(ctx, info.Ref)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/git/ -run 'MetaRef|StoreFormat' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight
git add internal/git/metaref.go internal/git/metaref_test.go
git commit -m "feat(git): store-format marker refs under refs/gg/meta"
```

---

### Task 4: the v1 feature registry

**Files:**
- Create: `internal/domain/features.go`
- Create: `internal/domain/features_test.go`

**Interfaces:**
- Consumes: `preflight.Feature`, `preflight.DataFormat`, `preflight.GitVersion`, `preflight.Optional`, `preflight.Required`.
- Produces: `domain.FeatureCore` (`"core"`), `domain.FeatureVersions` (`"versions"`), `domain.StoreVersions` (`"versions"`), `domain.VersionsFormat` (`1`), `domain.Features() []preflight.Feature`.

- [ ] **Step 1: Write the failing test**

Create `internal/domain/features_test.go`:

```go
package domain

import (
	"testing"

	"github.com/homeend/gigagit/internal/preflight"
)

func TestFeaturesDeclaresCoreAndVersions(t *testing.T) {
	t.Parallel()
	fs := Features()

	byID := map[string]preflight.Feature{}
	for _, f := range fs {
		byID[f.ID] = f
	}

	core, ok := byID[FeatureCore]
	if !ok {
		t.Fatalf("Features() has no %q entry", FeatureCore)
	}
	if core.Criticality != preflight.Required {
		t.Errorf("%q criticality = %v, want Required", FeatureCore, core.Criticality)
	}
	if core.Migrate != nil {
		t.Errorf("%q must not declare a migration", FeatureCore)
	}

	versions, ok := byID[FeatureVersions]
	if !ok {
		t.Fatalf("Features() has no %q entry", FeatureVersions)
	}
	if versions.Criticality != preflight.Optional {
		t.Errorf("%q criticality = %v, want Optional", FeatureVersions, versions.Criticality)
	}
	if versions.Migrate != nil {
		t.Errorf("%q must not declare a migration yet (format 2 is the follow-up spec)", FeatureVersions)
	}
}

func TestFeaturesAreSatisfiedOnACurrentRepo(t *testing.T) {
	t.Parallel()
	p := preflight.Probes{
		Stores:     map[string]preflight.StoreProbe{StoreVersions: {Format: VersionsFormat, HasData: true}},
		GitVersion: [3]int{2, 45, 0},
	}
	for _, v := range preflight.Resolve(Features(), p) {
		if v.State != preflight.Satisfied {
			t.Errorf("feature %q = %v on a current repo, want Satisfied (%s)", v.Feature.ID, v.State, v.Reason.Format)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/domain/ -run TestFeatures -v`
Expected: FAIL — `undefined: Features`.

- [ ] **Step 3: Write the implementation**

Create `internal/domain/features.go`:

```go
package domain

import "github.com/homeend/gigagit/internal/preflight"

// Feature and store IDs are English protocol values: they appear in CLI
// output, MCP errors and config, and are never translated.
const (
	FeatureCore     = "core"
	FeatureVersions = "versions"

	StoreVersions = "versions"
)

// VersionsFormat is the branch-version layout this build writes. Format 1 is
// "a ref pointing at the pre-operation tip" — the layout gg has always used.
const VersionsFormat = 1

// MinGitVersion is the oldest git gg supports. 2.30 ships the for-each-ref and
// worktree behaviour every frontend assumes.
var MinGitVersion = [3]int{2, 30, 0}

// Features is the v1 registry. Adding a feature here is how it gains a
// requirement contract; nothing else has to change.
func Features() []preflight.Feature {
	return []preflight.Feature{
		{
			ID:          FeatureCore,
			Criticality: preflight.Required,
			Requires:    []preflight.Requirement{preflight.GitVersion{Min: MinGitVersion}},
		},
		{
			ID:          FeatureVersions,
			Criticality: preflight.Optional,
			Requires: []preflight.Requirement{
				preflight.DataFormat{Store: StoreVersions, Min: VersionsFormat, Max: VersionsFormat},
			},
			// No Migrate: format 2 and its migration arrive in the follow-up
			// spec. Repairable has only test consumers until then.
		},
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/domain/ -run TestFeatures -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight
git add internal/domain/features.go internal/domain/features_test.go
git commit -m "feat(domain): v1 feature registry (core, versions)"
```

---

### Task 5: `Service.Preflight` — probes, resolution, caching

**Files:**
- Create: `internal/domain/preflight.go`
- Create: `internal/domain/preflight_test.go`
- Modify: `internal/domain/service.go` (add cache fields to `Service`)

**Interfaces:**
- Consumes: `(*git.Repo).StoreFormats`, `(*git.Repo).GitVersion`, `(*git.Repo).ForEachRef`, `git.VersionRefPrefix`, `domain.Features()`, `preflight.Resolve`.
- Produces: `(*Service).Preflight(ctx) ([]preflight.Verdict, error)`, `(*Service).FeatureEnabled(ctx, id string) bool`, `ErrFeatureDisabled{ID, Reason string}` with `Error() string` (unwrapped by callers with `errors.As`), `(*Service).FeatureDisabledError(ctx, id string) error`.

- [ ] **Step 1: Add the cache fields**

In `internal/domain/service.go`, inside the `Service` struct, after the `prefixGlobal`/`prefixRepo` fields, add:

```go
	// preflightMu guards the resolved verdicts. reRoot builds a FRESH Service,
	// so a cached resolution can never outlive the repo it describes.
	preflightMu   sync.Mutex
	preflightDone bool
	preflightOut  []preflight.Verdict
```

Add `"github.com/homeend/gigagit/internal/preflight"` to the import block.

- [ ] **Step 2: Write the failing test**

Create `internal/domain/preflight_test.go`:

```go
package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/preflight"
)

func TestPreflightSatisfiedOnAFreshRepo(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)

	vs, err := svc.Preflight(context.Background())
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if len(vs) == 0 {
		t.Fatal("Preflight returned no verdicts")
	}
	for _, v := range vs {
		if v.State != preflight.Satisfied {
			t.Errorf("feature %q = %v, want Satisfied (%s)", v.Feature.ID, v.State, v.Reason.Format)
		}
	}
}

func TestPreflightTreatsUnmarkedVersionRefsAsLegacyFormatOne(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()

	// A repo from before markers existed: a version ref, no marker.
	head, err := svc.Repo().RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}
	if err := svc.Repo().UpdateRef(ctx, "refs/gg/versions/main/1700000000-rebase", head); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	p, err := svc.preflightProbes(ctx)
	if err != nil {
		t.Fatalf("preflightProbes: %v", err)
	}
	got := p.Stores[StoreVersions]
	if got.Format != 0 || !got.HasData {
		t.Errorf("versions probe = %+v, want {Format:0 HasData:true}", got)
	}

	// With this build at format 1, legacy data resolves as Satisfied. The
	// follow-up spec bumps the range to 2, which turns this into Repairable.
	if fit := (preflight.DataFormat{Store: StoreVersions, Min: 2, Max: 2}).Fit(p); fit != preflight.FitTooOld {
		t.Errorf("fit against a format-2 build = %v, want FitTooOld", fit)
	}
}

func TestPreflightIsCached(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()

	a, err := svc.Preflight(ctx)
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	b, err := svc.Preflight(ctx)
	if err != nil {
		t.Fatalf("Preflight (second call): %v", err)
	}
	if len(a) != len(b) {
		t.Fatalf("cached verdicts differ: %d then %d", len(a), len(b))
	}
	if !svc.preflightDone {
		t.Error("preflightDone = false after Preflight; the result was not cached")
	}
}

func TestFeatureEnabledAndErrFeatureDisabled(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()

	if !svc.FeatureEnabled(ctx, FeatureVersions) {
		t.Error("FeatureEnabled(versions) = false on a fresh repo, want true")
	}
	if svc.FeatureEnabled(ctx, "no-such-feature") {
		t.Error("FeatureEnabled of an unknown id = true, want false")
	}

	err := error(&ErrFeatureDisabled{ID: FeatureVersions, Reason: "the versions store is at format 9"})
	var target *ErrFeatureDisabled
	if !errors.As(err, &target) || target.ID != FeatureVersions {
		t.Errorf("errors.As did not unwrap ErrFeatureDisabled: %v", err)
	}
	if got := err.Error(); got == "" {
		t.Error("ErrFeatureDisabled.Error() is empty")
	}
}
```

If `newTestService` does not already exist in `internal/domain`, reuse whatever the package's existing tests use to build a `*Service` over a real temp repo (grep the package's `_test.go` files for the helper and call that instead — do not invent a second helper).

- [ ] **Step 3: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/domain/ -run 'TestPreflight|TestFeatureEnabled' -v`
Expected: FAIL — `svc.Preflight undefined`.

- [ ] **Step 4: Write the implementation**

Create `internal/domain/preflight.go`:

```go
package domain

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/preflight"
)

// ErrFeatureDisabled is returned by a query or command whose feature preflight
// turned off. Frontends render it; they never re-derive the decision.
type ErrFeatureDisabled struct {
	ID     string // English feature id
	Reason string // rendered English prose
}

func (e *ErrFeatureDisabled) Error() string {
	return fmt.Sprintf("the %s feature is unavailable in this repository: %s", e.ID, e.Reason)
}

// preflightProbes gathers everything the resolver may look at: one
// for-each-ref for the markers, one existence probe per store, one git
// version. All reads — preflight never writes.
func (s *Service) preflightProbes(ctx context.Context) (preflight.Probes, error) {
	formats, err := s.repo.StoreFormats(ctx)
	if err != nil {
		return preflight.Probes{}, err
	}
	ver, err := s.repo.GitVersion(ctx)
	if err != nil {
		return preflight.Probes{}, err
	}

	versionRefs, err := s.repo.ForEachRef(ctx, git.VersionRefPrefix)
	if err != nil {
		return preflight.Probes{}, err
	}

	return preflight.Probes{
		Stores: map[string]preflight.StoreProbe{
			StoreVersions: {Format: formats[StoreVersions], HasData: len(versionRefs) > 0},
		},
		GitVersion: ver,
	}, nil
}

// Preflight resolves every declared feature against this repository. The
// result is cached for the Service's lifetime; reRoot builds a fresh Service,
// so switching repos re-resolves automatically.
func (s *Service) Preflight(ctx context.Context) ([]preflight.Verdict, error) {
	s.preflightMu.Lock()
	defer s.preflightMu.Unlock()
	if s.preflightDone {
		return s.preflightOut, nil
	}
	p, err := s.preflightProbes(ctx)
	if err != nil {
		return nil, err
	}
	s.preflightOut = preflight.Resolve(Features(), p)
	s.preflightDone = true
	return s.preflightOut, nil
}

// FeatureEnabled reports whether id may be used. An unknown id is never
// enabled. A probe failure is treated as "enabled" so a transient git error
// cannot silently switch features off.
func (s *Service) FeatureEnabled(ctx context.Context, id string) bool {
	vs, err := s.Preflight(ctx)
	if err != nil {
		return true
	}
	for _, v := range vs {
		if v.Feature.ID == id {
			return v.State == preflight.Satisfied
		}
	}
	return false
}

// FeatureDisabledError builds the typed error an owning query returns. Returns
// nil when the feature is available.
func (s *Service) FeatureDisabledError(ctx context.Context, id string) error {
	if s.FeatureEnabled(ctx, id) {
		return nil
	}
	reason := ""
	if vs, err := s.Preflight(ctx); err == nil {
		for _, v := range vs {
			if v.Feature.ID == id && v.Reason.Format != "" {
				reason = fmt.Sprintf(v.Reason.Format, v.Reason.Args...)
			}
		}
	}
	return &ErrFeatureDisabled{ID: id, Reason: reason}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/domain/ -run 'TestPreflight|TestFeatureEnabled' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight
git add internal/domain/preflight.go internal/domain/preflight_test.go internal/domain/service.go
git commit -m "feat(domain): Service.Preflight, FeatureEnabled and ErrFeatureDisabled"
```

---

### Task 6: gate the versions surface and stamp on write

**Files:**
- Modify: `internal/domain/query.go:640` (`BranchVersions`) and `:666` (`AllVersionBranches`)
- Modify: `internal/engine/snapshot_version.go` (stamp the marker)
- Create: `internal/domain/versions_gate_test.go`
- Modify: `internal/engine/snapshot_version_test.go` (marker assertion)

**Interfaces:**
- Consumes: `(*Service).FeatureDisabledError`, `(*git.Repo).StampStoreFormat`, `domain.StoreVersions`, `domain.VersionsFormat`.
- Produces: `engine.OpDeps.Repo` gains `StampStoreFormat(ctx, store string, format int) error` on the `GitOps` interface.

- [ ] **Step 1: Write the failing gate test**

Create `internal/domain/versions_gate_test.go`:

```go
package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/preflight"
)

func TestBranchVersionsRefusesWhenTheFeatureIsDisabled(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()

	// Force the disabled verdict rather than constructing a future-format repo:
	// the gate, not the resolver, is under test here.
	svc.preflightMu.Lock()
	svc.preflightDone = true
	svc.preflightOut = []preflight.Verdict{{
		Feature: preflight.Feature{ID: FeatureVersions, Criticality: preflight.Optional},
		State:   preflight.Unsatisfiable,
		Reason:  preflight.Text{Format: "the %s store is at format %d; this build needs %d-%d", Args: []any{StoreVersions, 9, 1, 1}},
	}}
	svc.preflightMu.Unlock()

	_, err := svc.BranchVersions(ctx, "main")
	var disabled *ErrFeatureDisabled
	if !errors.As(err, &disabled) {
		t.Fatalf("BranchVersions error = %v, want ErrFeatureDisabled", err)
	}
	if disabled.ID != FeatureVersions {
		t.Errorf("disabled.ID = %q, want %q", disabled.ID, FeatureVersions)
	}

	if _, err := svc.AllVersionBranches(ctx); !errors.As(err, &disabled) {
		t.Errorf("AllVersionBranches error = %v, want ErrFeatureDisabled", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/domain/ -run TestBranchVersionsRefuses -v`
Expected: FAIL — `BranchVersions` returns the version list, not `ErrFeatureDisabled`.

- [ ] **Step 3: Add the gate**

At the top of `BranchVersions` (`internal/domain/query.go:640`) and `AllVersionBranches` (`:666`), before any other work:

```go
	if err := s.FeatureDisabledError(ctx, FeatureVersions); err != nil {
		return nil, err
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/domain/ -run TestBranchVersionsRefuses -v`
Expected: PASS.

- [ ] **Step 5: Write the failing stamp test**

Append to `internal/engine/snapshot_version_test.go`:

```go
func TestSnapshotBranchTipStampsTheStoreFormat(t *testing.T) {
	t.Parallel()
	// Build deps exactly the way the neighbouring snapshot tests in this file
	// do (same helper, same VersionsPolicy{Enabled: true}), then:
	snapshotBranchTip(ctx, deps, "main", "rebase")

	formats, err := deps.Repo.StoreFormats(ctx)
	if err != nil {
		t.Fatalf("StoreFormats: %v", err)
	}
	if formats["versions"] != 1 {
		t.Errorf("StoreFormats = %v, want versions=1 — the writer must stamp the marker", formats)
	}
}
```

Fill the `deps`/`ctx` construction from the helper already used by `TestSnapshotBranchTip…` in the same file; do not introduce a new helper.

- [ ] **Step 6: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/engine/ -run TestSnapshotBranchTipStamps -v`
Expected: FAIL — the marker map is empty.

- [ ] **Step 7: Stamp from the writer**

In `internal/engine/gitops.go`, add to the `GitOps` interface, next to the other ref verbs:

```go
	StoreFormats(ctx context.Context) (map[string]int, error)
	StampStoreFormat(ctx context.Context, store string, format int) error
```

In `internal/engine/snapshot_version.go`, inside `snapshotBranchTip`, immediately after the successful `deps.Repo.UpdateRef(ctx, ref, sha)` and before `pruneBranchVersions`:

```go
	// The store's WRITER stamps the format marker — never startup. Keeps every
	// `gg` invocation free of a ref write and removes the compare-and-swap race
	// between concurrently starting processes. Best-effort like the snapshot
	// itself: a stamp failure must not fail the real operation.
	_ = deps.Repo.StampStoreFormat(ctx, "versions", 1)
```

If `internal/engine` has a fake `GitOps` used by tests, add both methods there too (grep for the other `ForEachRef` implementation in `internal/engine/*_test.go`).

- [ ] **Step 8: Run the engine and domain suites**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/engine/ ./internal/domain/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight
git add internal/domain/query.go internal/domain/versions_gate_test.go internal/engine/gitops.go internal/engine/snapshot_version.go internal/engine/snapshot_version_test.go
git commit -m "feat: gate the versions surface on preflight and stamp its format marker on write"
```

---

### Task 7: `ApplyMigration` engine operation

**Files:**
- Create: `internal/engine/apply_migration.go`
- Create: `internal/engine/apply_migration_test.go`

**Interfaces:**
- Consumes: `engine.OpDeps`, `engine.Result`, `engine.PromptReq`, `repogate.RefWrite`, `GitOps.StampStoreFormat`.
- Produces: `engine.ApplyMigration{Feature, Store string; To int; Refs []string}` with `Run` and `LockMode`.

- [ ] **Step 1: Write the failing test**

Create `internal/engine/apply_migration_test.go`:

```go
package engine

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/repogate"
)

func TestApplyMigrationDeletesRefsAndStamps(t *testing.T) {
	t.Parallel()
	// Build deps over a real temp repo the same way the other engine op tests
	// in this package do, then seed two legacy refs:
	//   refs/gg/versions/main/1700000000-rebase
	//   refs/gg/versions/main/1700000001-merge
	op := ApplyMigration{
		Feature: "versions",
		Store:   "versions",
		To:      2,
		Refs:    []string{"refs/gg/versions/main/1700000000-rebase", "refs/gg/versions/main/1700000001-merge"},
	}
	res, err := op.Run(context.Background(), deps)
	if err != nil {
		t.Fatalf("ApplyMigration: %v", err)
	}
	if !res.Changed {
		t.Error("Result.Changed = false, want true")
	}

	left, err := deps.Repo.ForEachRef(ctx, "refs/gg/versions/")
	if err != nil {
		t.Fatalf("ForEachRef: %v", err)
	}
	if len(left) != 0 {
		t.Errorf("%d version refs survived the migration, want 0", len(left))
	}

	formats, err := deps.Repo.StoreFormats(ctx)
	if err != nil {
		t.Fatalf("StoreFormats: %v", err)
	}
	if formats["versions"] != 2 {
		t.Errorf("StoreFormats = %v, want versions=2", formats)
	}
}

func TestApplyMigrationTakesRefWrite(t *testing.T) {
	t.Parallel()
	if got := (ApplyMigration{}).LockMode(); got != repogate.RefWrite {
		t.Errorf("LockMode = %v, want RefWrite", got)
	}
}

func TestApplyMigrationRequiresAStore(t *testing.T) {
	t.Parallel()
	if _, err := (ApplyMigration{To: 2}).Run(context.Background(), OpDeps{}); err == nil {
		t.Error("Run with no Store = nil error, want an error")
	}
}
```

Fill `deps`/`ctx` from the helper the neighbouring op tests use.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/engine/ -run TestApplyMigration -v`
Expected: FAIL — `undefined: ApplyMigration`.

- [ ] **Step 3: Write the implementation**

Create `internal/engine/apply_migration.go`:

```go
package engine

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/repogate"
)

// ApplyMigration discards a store's unreadable data and stamps the new format
// marker. It is the ONLY write preflight ever performs, and it happens only
// after explicit consent — the frontends ask; this op does not.
//
// Refs is computed by the caller (domain) so the op stays a dumb executor: it
// never decides WHAT is stale, only removes what it was handed.
type ApplyMigration struct {
	Feature string   // English feature id, for the summary
	Store   string   // store whose marker is stamped
	To      int      // format written after the migration
	Refs    []string // refs to delete; may be empty
}

func (op ApplyMigration) LockMode() repogate.Mode { return repogate.RefWrite }

func (op ApplyMigration) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Store == "" {
		return Result{}, fmt.Errorf("apply migration: Store is required")
	}
	if op.To <= 0 {
		return Result{}, fmt.Errorf("apply migration: To must be a positive format number")
	}

	deps.emit(ctx, Progress{Step: "migrating store", Detail: op.Store})

	for _, ref := range op.Refs {
		if err := deps.Repo.DeleteRef(ctx, ref); err != nil {
			return Result{}, fmt.Errorf("apply migration: deleting %s: %w", ref, err)
		}
	}
	if err := deps.Repo.StampStoreFormat(ctx, op.Store, op.To); err != nil {
		return Result{}, fmt.Errorf("apply migration: stamping %s format %d: %w", op.Store, op.To, err)
	}

	return Result{Changed: true}.WithSummary("migrated %s to format %d (%d entries discarded)", op.Store, op.To, len(op.Refs)), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/engine/ -run TestApplyMigration -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight
git add internal/engine/apply_migration.go internal/engine/apply_migration_test.go
git commit -m "feat(engine): ApplyMigration operation under RefWrite"
```

---

### Task 8: `domain.PendingMigrations` and `domain.RunMigration`

**Files:**
- Modify: `internal/domain/preflight.go`
- Modify: `internal/domain/preflight_test.go`

**Interfaces:**
- Consumes: `(*Service).Preflight`, `(*Service).Execute`, `engine.ApplyMigration`, `(*git.Repo).ForEachRef`, `git.VersionRefPrefix`.
- Produces: `domain.PendingMigration{Feature, Store string; From, To int; Consequence string; Refs []string}`, `(*Service).PendingMigrations(ctx) ([]PendingMigration, error)`, `(*Service).RunMigration(ctx, m PendingMigration) error`.

- [ ] **Step 1: Write the failing test**

Append to `internal/domain/preflight_test.go`:

```go
func TestPendingMigrationsIsEmptyWhenNothingIsRepairable(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)

	got, err := svc.PendingMigrations(context.Background())
	if err != nil {
		t.Fatalf("PendingMigrations: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("PendingMigrations = %v, want none on a current repo", got)
	}
}

func TestPendingMigrationsListsRepairableFeaturesWithTheirRefs(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()

	head, err := svc.Repo().RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}
	ref := "refs/gg/versions/main/1700000000-rebase"
	if err := svc.Repo().UpdateRef(ctx, ref, head); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	// Stand in for a future build: versions requires format 2 and can repair 1.
	svc.preflightMu.Lock()
	svc.preflightDone = true
	svc.preflightOut = []preflight.Verdict{{
		Feature: preflight.Feature{
			ID:          FeatureVersions,
			Criticality: preflight.Optional,
			Migrate: &preflight.Migration{Store: StoreVersions, From: 1, To: 2,
				Describe: func() preflight.Text {
					return preflight.Text{Format: "discards every recorded branch version in %s", Args: []any{"this repository"}}
				}},
		},
		State:  preflight.Repairable,
		Reason: preflight.Text{Format: "the %s store is at format %d", Args: []any{StoreVersions, 1}},
	}}
	svc.preflightMu.Unlock()

	got, err := svc.PendingMigrations(ctx)
	if err != nil {
		t.Fatalf("PendingMigrations: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("PendingMigrations returned %d entries, want 1", len(got))
	}
	m := got[0]
	if m.Feature != FeatureVersions || m.From != 1 || m.To != 2 {
		t.Errorf("migration = %+v, want versions 1→2", m)
	}
	if m.Consequence == "" {
		t.Error("Consequence is empty; consent has nothing to show")
	}
	if len(m.Refs) != 1 || m.Refs[0] != ref {
		t.Errorf("Refs = %v, want [%s]", m.Refs, ref)
	}

	if err := svc.RunMigration(ctx, m); err != nil {
		t.Fatalf("RunMigration: %v", err)
	}
	left, err := svc.Repo().ForEachRef(ctx, "refs/gg/versions/")
	if err != nil {
		t.Fatalf("ForEachRef: %v", err)
	}
	if len(left) != 0 {
		t.Errorf("%d version refs survived RunMigration, want 0", len(left))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/domain/ -run TestPendingMigrations -v`
Expected: FAIL — `svc.PendingMigrations undefined`.

- [ ] **Step 3: Write the implementation**

Append to `internal/domain/preflight.go`:

```go
// PendingMigration is one repairable feature, already resolved to the refs it
// will discard and the English consequence prose to show before consent.
type PendingMigration struct {
	Feature     string
	Store       string
	From, To    int
	Consequence string
	Refs        []string
}

// storeRefs lists the refs a store owns, so a migration can report exactly
// what it will remove.
func (s *Service) storeRefs(ctx context.Context, store string) ([]string, error) {
	if store != StoreVersions {
		return nil, nil
	}
	infos, err := s.repo.ForEachRef(ctx, git.VersionRefPrefix)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(infos))
	for _, info := range infos {
		out = append(out, info.Ref)
	}
	return out, nil
}

// PendingMigrations lists every Repairable feature. Nothing is applied here.
func (s *Service) PendingMigrations(ctx context.Context) ([]PendingMigration, error) {
	vs, err := s.Preflight(ctx)
	if err != nil {
		return nil, err
	}
	var out []PendingMigration
	for _, v := range vs {
		if v.State != preflight.Repairable || v.Feature.Migrate == nil {
			continue
		}
		mig := v.Feature.Migrate
		refs, err := s.storeRefs(ctx, mig.Store)
		if err != nil {
			return nil, err
		}
		consequence := ""
		if mig.Describe != nil {
			t := mig.Describe()
			consequence = fmt.Sprintf(t.Format, t.Args...)
		}
		out = append(out, PendingMigration{
			Feature:     v.Feature.ID,
			Store:       mig.Store,
			From:        mig.From,
			To:          mig.To,
			Consequence: consequence,
			Refs:        refs,
		})
	}
	return out, nil
}

// RunMigration applies one pending migration through Execute, then drops the
// cached verdicts so the next Preflight sees the new state.
func (s *Service) RunMigration(ctx context.Context, m PendingMigration) error {
	op := engine.ApplyMigration{Feature: m.Feature, Store: m.Store, To: m.To, Refs: m.Refs}
	if _, err := s.Execute(ctx, op, nil); err != nil {
		return err
	}
	s.preflightMu.Lock()
	s.preflightDone, s.preflightOut = false, nil
	s.preflightMu.Unlock()
	return nil
}
```

Add `"github.com/homeend/gigagit/internal/engine"` to the file's imports. Check `Execute`'s real signature in `internal/domain/execute.go` and match it — if it takes a `Decider` plus an event sink, pass the no-op values the other non-interactive callers in `domain` use rather than `nil`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/domain/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight
git add internal/domain/preflight.go internal/domain/preflight_test.go
git commit -m "feat(domain): PendingMigrations and RunMigration"
```

---

### Task 9: `gg migrate` CLI command

**Files:**
- Create: `internal/cli/migrate.go`
- Create: `internal/cli/migrate_test.go`
- Modify: `internal/cli/cli.go:158` (dispatch switch) and `:179` (`commands` map)
- Modify: `cmd/gg/main.go:102` (the unknown-command help line)

**Interfaces:**
- Consumes: `(*domain.Service).PendingMigrations`, `(*domain.Service).RunMigration`, `domain.PendingMigration`.
- Produces: `cli.runMigrate(...)` wired to the `migrate` subcommand.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/migrate_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestMigrateListsNothingOnACurrentRepo(t *testing.T) {
	t.Parallel()
	// Build a repo + run `gg migrate` exactly the way the neighbouring CLI
	// tests in this package do (grep for the helper that runs Run() against a
	// temp repo and captures stdout).
	out, code := runCLI(t, "migrate")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "nothing to migrate") {
		t.Errorf("output = %q, want it to say nothing is pending", out)
	}
}

func TestMigrateWithoutYesChangesNothing(t *testing.T) {
	t.Parallel()
	// Seed a legacy version ref, then run `gg migrate` with no flags and assert
	// the ref is still present afterwards.
	out, code := runCLI(t, "migrate")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\n%s", code, out)
	}
	// assert refs/gg/versions/ is non-empty
}
```

Replace `runCLI` with the package's actual helper name and shape; do not add a second one.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/cli/ -run TestMigrate -v`
Expected: FAIL — `gg: unknown command "migrate"`.

- [ ] **Step 3: Write the command**

Create `internal/cli/migrate.go`:

```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
)

// runMigrate lists or applies pending store migrations. With no flags it
// changes NOTHING — it prints what would be discarded and why. --yes applies.
// This is the single consent path for headless use; the TUI and web ask
// interactively instead.
func runMigrate(ctx context.Context, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	yes := fs.Bool("yes", false, "apply the migrations (destructive)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	pending, err := svc.PendingMigrations(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "gg migrate:", err)
		return 1
	}
	if len(pending) == 0 {
		fmt.Fprintln(stdout, "nothing to migrate")
		return 0
	}

	for _, m := range pending {
		fmt.Fprintf(stdout, "%s: format %d → %d\n", m.Feature, m.From, m.To)
		fmt.Fprintf(stdout, "  %s\n", m.Consequence)
		fmt.Fprintf(stdout, "  discards %d entries\n", len(m.Refs))
	}
	if !*yes {
		fmt.Fprintln(stdout, "\nnothing changed; re-run with --yes to apply")
		return 0
	}
	for _, m := range pending {
		if err := svc.RunMigration(ctx, m); err != nil {
			fmt.Fprintln(stderr, "gg migrate:", err)
			return 1
		}
		fmt.Fprintf(stdout, "migrated %s to format %d\n", m.Feature, m.To)
	}
	return 0
}
```

- [ ] **Step 4: Wire the command**

In `internal/cli/cli.go`, add to the dispatch switch alongside `case "versions":`:

```go
	case "migrate":
		return runMigrate(ctx, svc, rest, stdout, stderr)
```

Match the surrounding cases' exact argument names and shape. Add `"migrate": true,` to the `commands` map at `:179`.

In `cmd/gg/main.go:102`, add `migrate` to the printed command list (the comment there says it is kept in sync with `cli.commands`).

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/cli/ -run TestMigrate -v`
Expected: PASS.

- [ ] **Step 6: Make the disabled-feature message name `gg migrate`**

In `internal/cli/versions.go`, where the command handles an error from `BranchVersions`/`AllVersionBranches`, add an `errors.As` branch for `*domain.ErrFeatureDisabled` that prints:

```go
	var disabled *domain.ErrFeatureDisabled
	if errors.As(err, &disabled) {
		fmt.Fprintf(stderr, "gg versions: %s\n", disabled.Error())
		fmt.Fprintln(stderr, "run `gg migrate` to see what can be repaired")
		return 1
	}
```

- [ ] **Step 7: Run the CLI suite**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/cli/`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight
git add internal/cli/migrate.go internal/cli/migrate_test.go internal/cli/cli.go internal/cli/versions.go cmd/gg/main.go
git commit -m "feat(cli): gg migrate lists and applies pending store migrations"
```

---

### Task 10: e2e scenario

**Files:**
- Create: `e2e/scenarios/migrate.toml`

**Interfaces:**
- Consumes: the `gg migrate` CLI surface from Task 9.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Read the schema before writing**

Read `e2e/scenarios/` for two existing scenarios that run a CLI command and assert repo state (pick the two closest in shape to "run a command, assert refs"). The harness's operation contracts are documented in the `writing-e2e-scenarios` skill — follow it rather than inventing keys.

- [ ] **Step 2: Write the scenario**

Create `e2e/scenarios/migrate.toml` asserting, in order:

1. On a repo with no gg state, `gg migrate` exits 0 and prints `nothing to migrate`.
2. `gg migrate` never mutates: after a run with no flags, the repo's ref set is unchanged.
3. A CLI command whose feature is disabled exits non-zero and its stderr names `gg migrate`. Produce the disabled state the way the harness can: seed a marker ref above this build's range (`refs/gg/meta/versions/99`, pointing at any existing object) plus one `refs/gg/versions/…` ref, then run `gg versions`.

Use the same section names and assertion keys as the scenarios read in Step 1.

- [ ] **Step 3: Run the e2e stage**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && ./test.sh e2e`
Expected: PASS, including the new scenario.

- [ ] **Step 4: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight
git add e2e/scenarios/migrate.toml
git commit -m "test(e2e): gg migrate lists without mutating"
```

---

### Task 11: TUI consent screen and disabled notice

**Files:**
- Modify: `cmd/gg/main.go` (call the gate before `tui.Run`)
- Create: `internal/tui/preflight.go`
- Create: `internal/tui/preflight_test.go`
- Modify: `internal/i18n/bundles/ja.toml`, `ko.toml`, `zh.toml`, `ru.toml`

**Interfaces:**
- Consumes: `(*domain.Service).Preflight`, `(*domain.Service).PendingMigrations`, `(*domain.Service).RunMigration`, `preflight.Required`, `preflight.Unsatisfiable`, `preflight.Repairable`.
- Produces: `tui.Preflight(svc *domain.Service, stdin io.Reader, stdout io.Writer) (proceed bool, err error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/preflight_test.go` with three cases, using the package's existing model/test helpers:

1. A `Required` feature resolving `Unsatisfiable` → `Preflight` returns `proceed == false` and an error whose text names the reason.
2. Nothing pending → returns `proceed == true` with no output.
3. A `Repairable` feature with the answer `skip` → returns `proceed == true` and leaves the refs untouched; with `migrate` → returns `proceed == true` and the refs are gone.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/tui/ -run TestPreflight -v`
Expected: FAIL — `undefined: Preflight`.

- [ ] **Step 3: Implement the gate**

Create `internal/tui/preflight.go`. It runs **before** the Bubble Tea program starts, as a plain prompt — a full TUI layer would have to initialise the very features under question:

```go
package tui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/preflight"
)

// Preflight resolves the repository's feature requirements before the UI
// starts. It returns proceed=false only when a Required feature cannot be
// satisfied, or when the user quits. A pending migration is asked about EVERY
// launch: there is deliberately no suppression, because a feature silently
// left off is the outcome this gate exists to prevent.
//
// Quit is always one of the choices. A pre-UI gate is the one place where
// trapping the user would be unforgivable.
func Preflight(svc *domain.Service, stdin io.Reader, out io.Writer) (bool, error) {
	ctx := context.Background()

	verdicts, err := svc.Preflight(ctx)
	if err != nil {
		return true, nil // a probe failure must not lock the user out
	}
	for _, v := range verdicts {
		if v.Feature.Criticality == preflight.Required && v.State == preflight.Unsatisfiable {
			reason := fmt.Sprintf(v.Reason.Format, v.Reason.Args...)
			return false, fmt.Errorf(i18n.T("gg cannot start: %s"), reason)
		}
	}

	pending, err := svc.PendingMigrations(ctx)
	if err != nil || len(pending) == 0 {
		return true, nil
	}

	r := bufio.NewReader(stdin)
	for _, m := range pending {
		fmt.Fprintf(out, i18n.T("%s needs a one-time migration")+"\n", m.Feature)
		fmt.Fprintf(out, "  %s\n", m.Consequence)
		fmt.Fprintf(out, "  "+i18n.T("This discards %d entries and cannot be undone.")+"\n", len(m.Refs))
		fmt.Fprintf(out, "  [m] %s  [s] %s  [q] %s: ",
			i18n.T("Migrate"), i18n.T("Skip"), i18n.T("Quit"))

		line, rerr := r.ReadString('\n')
		if rerr != nil && line == "" {
			return false, nil // no input available: treat as quit, never as consent
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "m":
			if err := svc.RunMigration(ctx, m); err != nil {
				return false, err
			}
		case "q":
			return false, nil
		default: // skip: proceed with the feature disabled
		}
	}
	return true, nil
}
```

Note the two safety defaults: an unreadable probe proceeds (a transient git
error must not lock anyone out), and unreadable *input* quits (silence is never
consent to a destructive migration).

Every user-visible string goes through `i18n.T` with a literal key. Add these keys to all four bundles (`ja`, `ko`, `zh`, `ru`) with real translations, not English placeholders:

- `"Migrate"`, `"Skip"`, `"Quit"`
- `"%s needs a one-time migration"`
- `"This discards %d entries and cannot be undone."`
- `"%s is unavailable in this repository"`
- `"gg cannot start: %s"`

- [ ] **Step 4: Wire it into `cmd/gg/main.go`**

After the existing `svc.TopLevel` friendly-error check (around `:122`) and before `tui.Run`:

```go
	proceed, err := tui.Preflight(svc, os.Stdin, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !proceed {
		os.Exit(0)
	}
```

- [ ] **Step 5: Add the standing disabled notice**

In `internal/tui/notify.go`, add a notice for each feature whose verdict is not `Satisfied`, following the existing notice-construction pattern in that file. Its text is the verdict's reason; for a `Repairable` one it also names `gg migrate`.

- [ ] **Step 6: Run the TUI suite and the i18n gates**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/tui/`
Expected: PASS, including `i18n_scan_test`, `options_vocab_test`, `menu_labels_test` and `engine_prose_test`.

- [ ] **Step 7: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight
git add internal/tui/preflight.go internal/tui/preflight_test.go internal/tui/notify.go internal/i18n/bundles/ cmd/gg/main.go
git commit -m "feat(tui): pre-UI migration consent gate and disabled-feature notice"
```

---

### Task 12: MCP and web surfaces

**Files:**
- Modify: `internal/mcp/` (tool registration)
- Create: `internal/mcp/preflight_test.go`
- Modify: `internal/web/` (server handler + the SPA's feature group)
- Create: `internal/web/preflight_test.go`

**Interfaces:**
- Consumes: `(*domain.Service).FeatureEnabled`, `(*domain.Service).PendingMigrations`, `(*domain.Service).RunMigration`, `domain.FeatureVersions`.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Write the failing MCP test**

Create `internal/mcp/preflight_test.go` asserting that when `versions` is disabled, the server's advertised tool list contains no versions tool, and that the same list on a healthy repo does. MCP **never prompts**: a `Repairable` verdict is treated exactly like `Unsatisfiable`, so the tools are simply absent.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/mcp/ -run TestPreflight -v`
Expected: FAIL — the tool is advertised regardless.

- [ ] **Step 3: Gate MCP tool registration**

At the point where tools are registered, skip any tool belonging to a feature where `svc.FeatureEnabled(ctx, id)` is false. Keep the mapping of tool → feature id in one table next to the registration, not scattered across handlers.

- [ ] **Step 4: Write the failing web test**

Create `internal/web/preflight_test.go` asserting that:

1. `GET /api/preflight` returns the pending migrations and the disabled features as JSON.
2. `POST /api/preflight/migrate` applies one, and a second `GET` then reports nothing pending.
3. Both endpoints keep the existing loopback and Host/Origin guards (assert a rejected cross-origin request, matching how the other handler tests in this package do it).

- [ ] **Step 5: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/web/ -run TestPreflight -v`
Expected: FAIL — 404 on both routes.

- [ ] **Step 6: Implement the web endpoints and panel**

Add the two handlers, then in the SPA render a consent panel before the app when `GET /api/preflight` reports anything pending, with **Migrate / Skip / Quit** buttons mirroring the TUI. When a feature is disabled, hide its sidebar group.

Remember the per-id visibility rule: a new `class="hidden"` element with no `#id.hidden` CSS rule is always visible. Give the panel an id and a matching rule.

- [ ] **Step 7: Run both suites**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && go test ./internal/mcp/ ./internal/web/`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight
git add internal/mcp/ internal/web/
git commit -m "feat(mcp,web): omit disabled features; web migration consent panel"
```

---

### Task 13: docs and the full gate

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md`
- Modify: `CLAUDE.md` (package map)
- Modify: `internal/agentskill/using-gg.md` and `internal/agentskill/agentskill.go` (`Version`)

**Interfaces:**
- Consumes: everything above.
- Produces: nothing.

- [ ] **Step 1: CHANGELOG entry**

Add an entry at the top describing: features declare requirements; verdicts resolved per repo; `gg migrate`; consent before any destructive migration; the `refs/gg/meta/` marker namespace.

- [ ] **Step 2: README**

Document `gg migrate` alongside the other CLI verbs, and one paragraph on what a disabled feature means.

- [ ] **Step 3: `CLAUDE.md` package map**

Add exactly one row, in the table's existing style:

```
| `preflight`  | Pure feature-requirement registry + resolver: features declare data-format ranges, a minimum git version and a criticality; `Resolve` maps probe results to Satisfied/Repairable/Unsatisfiable. DAG leaf; `domain` runs the probes and owns the migration op. |
```

- [ ] **Step 4: Agent skill**

`gg migrate` is a new CLI verb, so add it to `internal/agentskill/using-gg.md` and bump `agentskill.Version`. Skill-text edits under an installed marker require the version bump or the update is skipped.

- [ ] **Step 5: Full gate**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight && ./test.sh && ./test.sh race`
Expected: both PASS.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight
git add CHANGELOG.md README.md CLAUDE.md internal/agentskill/
git commit -m "docs: repo preflight, gg migrate, preflight package map row"
```

- [ ] **Step 7: Build a verify binary**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-repo-preflight
go build -o /tmp/gg-preflight ./cmd/gg
```

Deliver the absolute path to the user for manual testing.
