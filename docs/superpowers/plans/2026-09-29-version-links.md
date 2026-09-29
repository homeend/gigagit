# Version links (`?version=` hint) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task, IN THIS SESSION (this repo never uses subagents — CLAUDE.md). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One `gg://` link per recorded two-branch version — its frozen preview `gg://<repo>@<base>..<ours>?version=<unix>-<op>` — copied from the versions popup, the drift notice, `gg versions` and the web row menu, and landing (TUI + web) with the Branch versions surface revealed on that row.

**Architecture:** The grammar gains one hint kind; domain gains one lookup (`FindVersion`, one `for-each-ref` over the whole store, id tie-broken by the pair) that every consumer and the link description share; producers build a `model.Link` and print `String()`. The TUI lands the pair as today and then parks a `versionsPopup` under the open compare; the web lands the pair and pushes its versions layer on top with the row flashed.

**Tech Stack:** Go 1.26, Bubble Tea TUI, embedded web SPA (vanilla JS modules, node-run gates), real `git` in `t.TempDir()`, e2e TOML scenarios.

**Spec:** `docs/superpowers/specs/2026-09-28-version-links-design.md`

## Global Constraints

- Work in the worktree `/mnt/t/others/gigagit/.claude/worktrees/version-links` (branch `feat/version-links`, off main `d54c5e99`). Every command runs with `cd` to that absolute path; `git -C <abs>` for git.
- Every user-visible TUI string is an `i18n.T` literal present in all four bundles `internal/i18n/lang/{ja,ko,zh,ru}.toml` (insert near related keys, never re-sort). Engine/CLI/web prose stays English.
- `internal/tui` and `internal/cli` never import `internal/git`; `web` and `mcp` are domain-only.
- Never string-concatenate a link: build `model.Link` and call `String()`. Validate both shas with `model.CommitEndpoint` first.
- A hint changes the LANDING, never the meaning; it degrades (a notice), never fails.
- Commit after every task with the trailers:
  `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01MApQgJKJiw4fa2qVU2wZUq`. `gg add <paths>` then `git -C <abs> commit -F <msgfile>` (`gg commit` has no `-F`). Never push.
- Per-package tests while working (`go test ./internal/<pkg>/ -run <Name>`); `./test.sh race` once at the end (Task 7). New tests call `t.Parallel()`.
- Link text in the TUI comes from `m.linkRepoFor("")`; in the CLI/web from `svc.LinkRepo(ctx)`.

## Review Focus

1. A record whose trailer holds a non-sha `Base`/`Ours` (crafted or written by an older gg): every producer must decline ("the recorded commit is not usable"), never print an unparsable link — pinned in Task 4 (`TestVersionLinkForRefusesAnUnusableRecord`).
2. Two branches holding the same `<unix>-<op>` id (a pull records both sides): the link's pair must pick the right one — pinned in Task 2 (`TestFindVersionTieBreaksAnIDByThePair`).
3. The compare view closed (or replaced) before the version lookup returns: the popup must not be parked under nothing — pinned in Task 5 (`TestVersionHintPushesThePopupLiveWhenTheCompareIsGone`).
4. Copying from the drift notice must NOT remove the notice — pinned in Task 4 (`TestDriftNoticeCopyLinkKeepsTheNotice`).
5. A checkout with no link form (`LinkRepo` error): `gg versions` prints the rows without a link line instead of failing — pinned in Task 3 (`TestCmdVersionsNoLinkFormPrintsRowsOnly`).

---

### Task 1: model — `BranchVersion.ID()` and the `version` hint kind

**Files:**
- Modify: `internal/model/model.go:102-115` (add the method after the struct)
- Modify: `internal/model/link.go:104-116` (comment + `linkHintKinds`), `:574` (error text)
- Test: `internal/model/link_version_test.go` (new)

**Interfaces:**
- Produces: `func (v BranchVersion) ID() string` → `"<Unix>-<Op>"`; `model.LinkHintKindOK("version") == true`; `ParseLink` accepts `?version=<id>` on any address.

- [ ] **Step 1: Write the failing tests**

```go
package model

import (
	"strings"
	"testing"
)

const (
	vA = "1111111111111111111111111111111111111111"
	vB = "2222222222222222222222222222222222222222"
)

// A version link is the pair link plus ?version=<unix>-<op>; it round-trips
// and the hint splits off without touching the address.
func TestVersionHintRoundTrip(t *testing.T) {
	t.Parallel()
	s := "gg://gigagit@" + vA + ".." + vB + "?version=1727000000-rebase"
	l, err := ParseLink(s)
	if err != nil {
		t.Fatalf("ParseLink(%q): %v", s, err)
	}
	if l.Hint.Kind != "version" || l.Hint.ID != "1727000000-rebase" {
		t.Fatalf("hint = %+v, want {version 1727000000-rebase}", l.Hint)
	}
	if l.Target.Pair == nil || l.Target.Pair.A != vA || l.Target.Pair.B != vB {
		t.Fatalf("pair = %+v, want %s..%s", l.Target.Pair, vA, vB)
	}
	if got := l.String(); got != s {
		t.Errorf("String() = %q, want %q", got, s)
	}
	// The local form too (a repo with no remote).
	local := "gg:///mnt/t/repo@" + vA + ".." + vB + "?version=1727000000-pull"
	if l, err := ParseLink(local); err != nil || l.String() != local {
		t.Errorf("local form: err=%v String()=%q", err, l.String())
	}
}

// Every op token the engine records spells a hint id (digits, a dash, the
// token) — LinkHintIDOK must admit them all or a producer would refuse.
func TestEveryVersionOpTokenIsAHintID(t *testing.T) {
	t.Parallel()
	for _, op := range []string{"merge", "rebase", "pull", "interactive-rebase", "restore", "amend", "reset", "undo-commit", "delete-branch"} {
		id := BranchVersion{Unix: 1727000000, Op: op}.ID()
		if !LinkHintIDOK(id) {
			t.Errorf("LinkHintIDOK(%q) = false", id)
		}
		if _, err := ParseLink("gg://r@" + vA + ".." + vB + "?version=" + id); err != nil {
			t.Errorf("ParseLink with id %q: %v", id, err)
		}
	}
}

// ID() is the ref's last path element — the token `gg versions` prints.
func TestBranchVersionIDIsTheRefTail(t *testing.T) {
	t.Parallel()
	v := BranchVersion{Ref: "refs/gg/versions/feat/x/1727000000-interactive-rebase", Unix: 1727000000, Op: "interactive-rebase"}
	if got := v.ID(); got != "1727000000-interactive-rebase" || !strings.HasSuffix(v.Ref, "/"+got) {
		t.Fatalf("ID() = %q, want the ref tail", got)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/version-links && go test ./internal/model/ -run 'TestVersionHintRoundTrip|TestEveryVersionOpTokenIsAHintID|TestBranchVersionIDIsTheRefTail'`
Expected: build failure `v.ID undefined`, then (after a stub) `unknown hint kind "version"`.

- [ ] **Step 3: Implement**

In `internal/model/model.go`, after the `BranchVersion` struct:

```go
// ID is the version's token as `gg versions` prints it and a ?version= hint
// carries it: "<unix>-<op>", the last element of Ref. The ONE spelling — the
// CLI's id lookup and every link producer use it.
func (v BranchVersion) ID() string { return fmt.Sprintf("%d-%s", v.Unix, v.Op) }
```

(add `"fmt"` to the imports if missing).

In `internal/model/link.go`:

```go
// "version" names a RECORDED BRANCH VERSION (refs/gg/versions/<branch>/<id>)
// whose frozen preview the link's pair IS: the id is the ref's last element
// (<unix>-<op>, BranchVersion.ID) and carries no branch, because hint ids
// reject '/'. A lookup key like "preview": the consumer finds the record by
// id, tie-broken by the pair, else by the pair alone. Machine-local by
// nature — version refs are never pushed.
var linkHintKinds = map[string]bool{"bookmark": true, "shelf": true, "stash": true, "preview": true, "view": true, "version": true}
```

and the error text in `parseLinkHint`:

```go
return LinkHint{}, fmt.Errorf("%w: unknown hint kind %q (want bookmark, shelf, stash, preview, version or view)", ErrLink, kind)
```

- [ ] **Step 4: Run to verify they pass, and the package**

Run: `go test ./internal/model/`
Expected: PASS (the existing `TestLinkHintRejects` still passes: `branch` is still unknown).

- [ ] **Step 5: Commit**

```bash
gg add internal/model/model.go internal/model/link.go internal/model/link_version_test.go
git -C /mnt/t/others/gigagit/.claude/worktrees/version-links commit -F /home/homeend/.claude/jobs/ece1681e/tmp/c1.txt
```
Message: `feat(model): ?version= hint kind and BranchVersion.ID()` + trailers.

---

### Task 2: domain — `FindVersion`, `DriftReport.Version`, describe + refuse

**Files:**
- Create: `internal/domain/version_find.go`, `internal/domain/version_find_test.go`
- Modify: `internal/domain/drift.go:15-19` (struct), `:41-48` (fill it)
- Modify: `internal/domain/linkdesc.go:121-128` (add the `version` arm after `preview`)
- Modify: `internal/domain/linkresolve.go:535-539` (add a `version` arm after `preview`)
- Test: `internal/domain/drift_test.go` (one assertion added), `internal/domain/linkdesc_version_test.go` (new)

**Interfaces:**
- Consumes: `model.BranchVersion.ID()` (Task 1); `s.repo.VersionRefs(ctx, prefix)`; `git.ParseVersionRef`.
- Produces:
  `func (s *Service) FindVersion(ctx context.Context, id, base, ours string) (branch string, v model.BranchVersion, ok bool, err error)`;
  `DriftReport.Version model.BranchVersion` (zero when nothing was recorded);
  `DescribeLink` → `version: <branch> · <op> · <YYYY-MM-DD HH:MM>` on a hit;
  `ResolveLink` refuses an address-less `?version=` with `a version hint needs the preview it names`.

- [ ] **Step 1: Write the failing `FindVersion` tests**

`internal/domain/version_find_test.go`:

```go
package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/git"
)

// recordVersion writes one version record the way the engine does (synthetic
// commit + ref) and returns the ref. tip is the snapshotted branch tip.
func recordVersion(t *testing.T, svc *Service, branch, op string, unix int64, tip, base, ours, other string) string {
	t.Helper()
	ctx := context.Background()
	meta := git.VersionMeta{Op: op, Ours: ours, Other: other, Base: base, Source: branch, Target: "onto"}
	syn, err := svc.Repo().WriteVersionSnapshot(ctx, tip, meta, unix)
	if err != nil {
		t.Fatalf("WriteVersionSnapshot: %v", err)
	}
	ref := git.VersionRef(branch, op, unix)
	if err := svc.Repo().UpdateRef(ctx, ref, syn); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}
	return ref
}

// findVersionFixture: three commits c1 < c2 < c3 on main; "feat" froze the
// pair (c1,c2) and "main" froze (c1,c3), BOTH under the id 1700001000-pull —
// what a pull writes for its two branches in one second.
func findVersionFixture(t *testing.T) (svc *Service, c1, c2, c3 string) {
	t.Helper()
	dir := cleanDir(t)
	svc = svcAt(dir)
	c1 = gitOutDir(t, dir, "rev-parse", "main")
	writeFile(t, dir, "g.txt", "2\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-qm", "two")
	c2 = gitOutDir(t, dir, "rev-parse", "main")
	writeFile(t, dir, "g.txt", "3\n")
	gitRunDir(t, dir, "", "add", "-A")
	gitRunDir(t, dir, "", "commit", "-qm", "three")
	c3 = gitOutDir(t, dir, "rev-parse", "main")
	recordVersion(t, svc, "feat", "pull", 1700001000, c2, c1, c2, c3)
	recordVersion(t, svc, "main", "pull", 1700001000, c3, c1, c3, c2)
	stampVersionsFormat(t, dir)
	return svc, c1, c2, c3
}

func TestFindVersionTieBreaksAnIDByThePair(t *testing.T) {
	t.Parallel()
	svc, c1, c2, c3 := findVersionFixture(t)
	ctx := context.Background()
	branch, v, ok, err := svc.FindVersion(ctx, "1700001000-pull", c1, c3)
	if err != nil || !ok || branch != "main" || v.Ours != c3 {
		t.Fatalf("FindVersion(main's pair) = %q %+v ok=%v err=%v, want main", branch, v, ok, err)
	}
	branch, v, ok, err = svc.FindVersion(ctx, "1700001000-pull", c1, c2)
	if err != nil || !ok || branch != "feat" || v.Ours != c2 {
		t.Fatalf("FindVersion(feat's pair) = %q %+v ok=%v err=%v, want feat", branch, v, ok, err)
	}
}

func TestFindVersionFallsBackToTheIDAloneThenToThePair(t *testing.T) {
	t.Parallel()
	svc, c1, c2, c3 := findVersionFixture(t)
	ctx := context.Background()
	// The id is known but the pair matches neither record: the id still answers.
	if _, v, ok, err := svc.FindVersion(ctx, "1700001000-pull", c2, c3); err != nil || !ok || v.ID() != "1700001000-pull" {
		t.Fatalf("id-only fallback: %+v ok=%v err=%v", v, ok, err)
	}
	// A foreign id (another machine) with a pair this store froze.
	if branch, _, ok, err := svc.FindVersion(ctx, "1600000000-rebase", c1, c3); err != nil || !ok || branch != "main" {
		t.Fatalf("pair-only fallback: branch=%q ok=%v err=%v, want main", branch, ok, err)
	}
	// Nothing matches: a miss, not an error.
	if _, _, ok, err := svc.FindVersion(ctx, "1600000000-rebase", c2, c3); err != nil || ok {
		t.Fatalf("miss: ok=%v err=%v, want false/nil", ok, err)
	}
}

// The store OFF (unstamped legacy format) is a miss: the diff landed and the
// hint has nothing to reveal — never a failure.
func TestFindVersionDisabledStoreIsAMiss(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	svc := svcAt(dir)
	c1 := gitOutDir(t, dir, "rev-parse", "main")
	gitRunDir(t, dir, "", "update-ref", git.VersionRef("main", "rebase", 1700002000), c1)
	// No stampVersionsFormat: the versions feature resolves OFF.
	if _, _, ok, err := svc.FindVersion(context.Background(), "1700002000-rebase", c1, c1); err != nil || ok {
		t.Fatalf("disabled store: ok=%v err=%v, want a plain miss", ok, err)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/domain/ -run 'TestFindVersion'`
Expected: `svc.FindVersion undefined`.

- [ ] **Step 3: Implement `FindVersion`**

`internal/domain/version_find.go`:

```go
package domain

import (
	"context"
	"errors"
	"strings"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// FindVersion answers a ?version=<id> hint. An id (<unix>-<op>) is unique
// only per branch — a pull records both of its branches in the same second
// under the same token — and the hint carries no branch, so the link's own
// pair (base, ours) breaks the tie. The ladder, in order:
//
//  1. a record with this id whose (Base, Ours) equal the pair
//  2. a record with this id (a colliding or foreign id)
//  3. the newest record freezing this pair (the id was minted elsewhere)
//
// ok=false is a miss — an unknown id, a deleted record, or the versions
// store disabled — never an error: the hint degrades, the link landed.
// One for-each-ref over the whole store, like AllVersionBranches.
func (s *Service) FindVersion(ctx context.Context, id, base, ours string) (branch string, v model.BranchVersion, ok bool, err error) {
	if err := s.FeatureDisabledError(ctx, FeatureVersions); err != nil {
		var disabled *ErrFeatureDisabled
		if errors.As(err, &disabled) {
			return "", model.BranchVersion{}, false, nil
		}
		return "", model.BranchVersion{}, false, err
	}
	all, err := s.repo.VersionRefs(ctx, strings.TrimSuffix(git.VersionRefPrefix, "/"))
	if err != nil {
		return "", model.BranchVersion{}, false, err
	}
	samePair := func(r model.BranchVersion) bool {
		return base != "" && ours != "" && r.Base == base && r.Ours == ours
	}
	var byID, byPair *model.BranchVersion
	for i := range all {
		r := &all[i]
		if _, _, _, ok := git.ParseVersionRef(r.Ref); !ok {
			continue
		}
		if r.ID() == id {
			if samePair(*r) {
				b, _, _, _ := git.ParseVersionRef(r.Ref)
				return b, *r, true, nil
			}
			if byID == nil {
				byID = r
			}
		} else if samePair(*r) && (byPair == nil || r.Unix > byPair.Unix) {
			byPair = r
		}
	}
	pick := byID
	if pick == nil {
		pick = byPair
	}
	if pick == nil {
		return "", model.BranchVersion{}, false, nil
	}
	b, _, _, _ := git.ParseVersionRef(pick.Ref)
	return b, *pick, true, nil
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/domain/ -run 'TestFindVersion'`
Expected: PASS.

- [ ] **Step 5: `DriftReport.Version` — failing assertion**

In `internal/domain/drift_test.go`, inside `TestDriftSinceReportsResurrectionAfterRebase` right after `got, err := svc.DriftSince(ctx, ref, newFeatTip)` and its error check, add:

```go
	if got.Version.Ref != ref || got.Version.Base != baseSha || got.Version.Ours != oursSha {
		t.Fatalf("Version = %+v, want the record compared against (%s)", got.Version, ref)
	}
```

Run: `go test ./internal/domain/ -run TestDriftSinceReportsResurrectionAfterRebase` → `got.Version undefined`.

- [ ] **Step 6: Implement**

`internal/domain/drift.go`:

```go
type DriftReport struct {
	Ref     string
	Report  changeset.Report
	Checked bool
	// Version is the record the branch was compared against — the one
	// Ref names — so a notice can offer its preview link without a second
	// load. Zero when nothing was recorded.
	Version model.BranchVersion
}
```

and in `DriftSince`, right after the loop that finds `rec` (before the `if rec == nil || …` return): `if rec != nil { out.Version = *rec }`. (Add the `model` import if drift.go lacks it.)

Run the test → PASS.

- [ ] **Step 7: describe + refuse — failing tests**

`internal/domain/linkdesc_version_test.go`:

```go
package domain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// A version link describes as the record when this store holds it, and falls
// through to the pair's own description when it does not.
func TestDescribeLinkVersionHitAndMiss(t *testing.T) {
	t.Parallel()
	svc, c1, c2, c3 := findVersionFixture(t)
	ctx := context.Background()
	hit, err := model.ParseLink("gg://r@" + c1 + ".." + c3 + "?version=1700001000-pull")
	if err != nil {
		t.Fatal(err)
	}
	got := svc.DescribeLink(ctx, hit)
	if !strings.HasPrefix(got, "version: main · pull · 2023-11-14") { // 1700001000 = 2023-11-14 22:16:40 UTC; local hour may differ
		t.Fatalf("hit: %q, want version: main · pull · <date>", got)
	}
	miss, err := model.ParseLink("gg://r@" + c2 + ".." + c3 + "?version=1600000000-rebase")
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.DescribeLink(ctx, miss); !strings.HasPrefix(got, "link: gg://r@"+c2) {
		t.Fatalf("miss: %q, want the pair fall-through", got)
	}
}

// An address-less ?version= names nothing: the record is not the link's
// content, the pair is.
func TestResolveLinkRefusesAnAddresslessVersionHint(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := findVersionFixture(t)
	l, err := model.ParseLink("gg://" + localLinkRoot(t, svc) + "?version=1700001000-pull")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ResolveLink(context.Background(), l, ResolveOpts{Cwd: svc})
	if !errors.Is(err, model.ErrLink) || !strings.Contains(err.Error(), "a version hint needs the preview it names") {
		t.Fatalf("err = %v, want the version-specific refusal", err)
	}
}
```

Note on the date: `linkDescFields` formats `time.Unix(v.Unix, 0).Format("2006-01-02 15:04")` in LOCAL time; the assertion checks the date prefix only. If the machine's zone makes 1700001000 fall on 2023-11-15, change the fixture unix to `1700050000` in BOTH files and the expected date to `2023-11-15` — do not loosen the assertion.

Run: `go test ./internal/domain/ -run 'TestDescribeLinkVersionHitAndMiss|TestResolveLinkRefusesAnAddresslessVersionHint'` → FAIL (hit describes as `link: …`; refusal says "has no way to check").

- [ ] **Step 8: Implement**

`internal/domain/linkdesc.go`, after the `case "preview":` block in `linkDescFields`:

```go
	case "version":
		// The recorded version, when this store holds it; a miss (deleted,
		// another machine) falls THROUGH to the pair arms below.
		if l.Target.Pair != nil {
			if branch, v, ok, err := s.FindVersion(ctx, l.Hint.ID, l.Target.Pair.A, l.Target.Pair.B); err == nil && ok {
				return "version", branch + " · " + v.Op + " · " + time.Unix(v.Unix, 0).Format("2006-01-02 15:04"), ""
			}
		}
```

(add `"time"` to the imports). Check `LinkDesc(kind, id, subject)` renders `kind + ": " + id` for an empty subject — it does for bookmark/shelf, so `version: main · pull · 2023-11-14 23:16` results.

`internal/domain/linkresolve.go`, after the `case "preview":` arm in the hint-only switch:

```go
		case "version":
			return Resolved{}, fmt.Errorf("%w: a version hint needs the preview it names (@<base>..<ours>); %q alone names nothing", model.ErrLink, l.Hint.ID)
```

Run → PASS. Then `go test ./internal/domain/` (whole package) → PASS.

- [ ] **Step 9: Commit**

`feat(domain): FindVersion lookup, DriftReport.Version, version link description` + trailers.

---

### Task 3: CLI — `gg versions` link line, `printDrift` line, e2e

**Files:**
- Modify: `internal/cli/versions.go:69-77` (list loop), `:123-135` (`versionRefForID` uses `ID()`)
- Modify: `internal/cli/cli.go:307-322` (`printDrift`)
- Create: `internal/cli/version_link.go` (the one CLI producer helper)
- Test: `internal/cli/versions_test.go`, `internal/cli/drift_test.go`
- Modify: `e2e/scenarios/s82_cli_versions.toml`; Create: `e2e/scenarios/s95_version_links.toml`

**Interfaces:**
- Consumes: `svc.LinkRepo(ctx)`, `model.CommitEndpoint`, `model.BranchVersion.ID()`, `DriftReport.Version`.
- Produces: `func versionLinkText(ctx context.Context, svc *domain.Service, v model.BranchVersion) (string, bool)` — `false` for a one-branch record, an unusable sha, or no link form.

- [ ] **Step 1: Failing tests**

Append to `internal/cli/versions_test.go`:

```go
// A two-branch version row is followed by its preview link on an indented
// continuation line; a one-branch row (no Base/Ours) is not.
func TestCmdVersionsPrintsThePreviewLinkUnderTwoBranchRows(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	base, ours, other, _ := buildResurrectionFixture(t, dir, "feat", "feat")
	fabricateVersion(t, dir, "feat", "rebase", 1753100000, base, ours, other)
	gitRun(t, dir, "update-ref", "refs/gg/versions/feat/1753100001-amend", ours) // one-branch: no preview
	stampVersionsFormat(t, dir)

	code, out, errb := runCLI(t, dir, "versions", "feat")
	if code != 0 {
		t.Fatalf("versions exit %d: %s", code, errb)
	}
	want := "\n  gg:///" // the sandbox has no remote: local form, indented
	if !strings.Contains(out, want) || !strings.Contains(out, "@"+base+".."+ours+"?version=1753100000-rebase") {
		t.Fatalf("missing the continuation link line:\n%s", out)
	}
	if strings.Contains(out, "?version=1753100001-amend") {
		t.Fatalf("a one-branch row must not get a link:\n%s", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "1753100001-amend ") || !strings.HasPrefix(lines[1], "1753100000-rebase ") || !strings.HasPrefix(lines[2], "  gg://") {
		t.Fatalf("rows must stay `<id> …` first, link indented under its row:\n%s", out)
	}
}

// A checkout whose path holds a character the grammar cannot carry has no
// link form: the rows print, the link line is simply absent.
func TestCmdVersionsNoLinkFormPrintsRowsOnly(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	dir := filepath.Join(parent, "odd?name")
	if err := os.Rename(newRepoDir(t), dir); err != nil {
		t.Skip("cannot create a '?' path here: " + err.Error())
	}
	base, ours, other, _ := buildResurrectionFixture(t, dir, "feat", "feat")
	fabricateVersion(t, dir, "feat", "rebase", 1753100000, base, ours, other)
	stampVersionsFormat(t, dir)
	code, out, errb := runCLI(t, dir, "versions", "feat")
	if code != 0 {
		t.Fatalf("versions exit %d: %s", code, errb)
	}
	if !strings.Contains(out, "1753100000-rebase") || strings.Contains(out, "gg://") {
		t.Fatalf("want the row without a link line:\n%s", out)
	}
}
```

(`buildResurrectionFixture`/`fabricateVersion` live in `drift_test.go`, same package. `newRepoDir` returns a `t.TempDir()`-rooted path; `os.Rename` within the same filesystem works on Linux; the `t.Skip` guards Windows.)

Append to `internal/cli/drift_test.go`, inside `TestCmdRebaseReportsChangeSetDrift` after the `absorbed upstream` check:

```go
	if !strings.Contains(out, "  recorded version: gg:///") || !strings.Contains(out, "?version=9999999999-rebase") {
		t.Fatalf("stdout missing the recorded version's preview link:\n%s", out)
	}
```

Run: `go test ./internal/cli/ -run 'TestCmdVersions|TestCmdRebaseReportsChangeSetDrift'` → FAIL (no link lines).

- [ ] **Step 2: Implement**

`internal/cli/version_link.go`:

```go
package cli

import (
	"context"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// versionLinkText is the ONE CLI producer of a version's preview link:
// gg://<repo>@<base>..<ours>?version=<id>. false for a one-branch record
// (records no preview), a record whose trailer does not hold two usable
// shas, or a checkout the grammar cannot spell (LinkRepo error) — the
// caller prints nothing rather than something ParseLink would refuse.
func versionLinkText(ctx context.Context, svc *domain.Service, v model.BranchVersion) (string, bool) {
	if v.Base == "" || v.Ours == "" {
		return "", false
	}
	if _, err := model.CommitEndpoint(v.Base); err != nil {
		return "", false
	}
	if _, err := model.CommitEndpoint(v.Ours); err != nil {
		return "", false
	}
	repo, err := svc.LinkRepo(ctx)
	if err != nil {
		return "", false
	}
	l := model.Link{
		Repo:   repo,
		Target: model.LinkTarget{State: model.StateCommitted, Pair: &model.LinkPair{A: v.Base, B: v.Ours}},
		Side:   model.NoteSideNew,
		Hint:   model.LinkHint{Kind: "version", ID: v.ID()},
	}
	s := l.String()
	if _, err := model.ParseLink(s); err != nil {
		return "", false
	}
	return s, true
}
```

`internal/cli/versions.go` list loop:

```go
	for _, v := range rows {
		short := v.Hash
		if len(short) > 8 {
			short = short[:8]
		}
		when := time.Unix(v.Unix, 0).Format("2006-01-02T15:04")
		fmt.Fprintf(stdout, "%s %s %s %s\n", v.ID(), short, when, v.Subject)
		if link, ok := versionLinkText(ctx, svc, v); ok {
			fmt.Fprintf(stdout, "  %s\n", link)
		}
	}
```

(`ctx`/`svc` are the ones the command already holds — check the enclosing function's variable names and use them.) In `versionRefForID`, replace `fmt.Sprintf("%d-%s", v.Unix, v.Op) == id` with `v.ID() == id`.

`internal/cli/cli.go` `printDrift`, after the Removed loop:

```go
	if link, ok := versionLinkText(ctx, svc, drift.Version); ok {
		fmt.Fprintf(stdout, "  recorded version: %s\n", link)
	}
```

Run → PASS. Then `go test ./internal/cli/` → PASS (`TestCmdVersionsListAndRestore` still holds: its fabricated record is a bare `update-ref` with no trailer, so no link line).

- [ ] **Step 3: e2e**

`e2e/scenarios/s82_cli_versions.toml`: change the first `versions` run to
`stdout_contains = ["-merge", "main work", "?version="]`.

Create `e2e/scenarios/s95_version_links.toml`:

```toml
name = "version links: gg versions prints the preview link, and it drives diff and resolve"

[input]
steps = [
  { write = "a.txt", content = "A\n" },
  { commit = "base" },
  { branch = "feat" },
  { switch = "feat" },
  { write = "b.txt", content = "B\n" },
  { commit = "feat work" },
  { switch = "main" },
  { write = "c.txt", content = "C\n" },
  { commit = "main work" },
]

[[run]]
cmd  = ["merge", "feat"]
exit = 0

# The producer: the pair is the frozen preview, the hint is the id column.
[[run]]
cmd             = ["versions", "main"]
exit            = 0
stdout_contains = ["-merge", "\n  gg:///", "?version="]

# Any pair link the list prints diffs — the harness cannot capture stdout
# into the next run, so this resolves the SAME pair by name: the merge froze
# base = merge-base(main, feat) and ours = main's pre-merge tip.
[[run]]
cmd             = ["link", "--pair", "HEAD~1^..HEAD~1"]
exit            = 0
stdout_contains = ["gg:///", ".."]

[[run]]
cmd             = ["diff", "gg://{{cwd}}@ref:main"]
exit            = 0
```

(Keep the scenario to what the harness can assert: `stdout_contains` on each run. The `?version=` link's own `gg diff` round trip is pinned by the unit test in Step 4 below, which can capture stdout.)

Append to `internal/cli/versions_test.go`:

```go
// The printed link drives gg diff and gg link resolve --json carries the hint.
func TestVersionLinkRoundTripsThroughDiffAndResolve(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	base, ours, other, _ := buildResurrectionFixture(t, dir, "feat", "feat")
	fabricateVersion(t, dir, "feat", "rebase", 1753100000, base, ours, other)
	stampVersionsFormat(t, dir)
	_, out, _ := runCLI(t, dir, "versions", "feat")
	var link string
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "  gg://") {
			link = strings.TrimSpace(ln)
		}
	}
	if link == "" {
		t.Fatalf("no link line in:\n%s", out)
	}
	if code, dout, errb := runCLI(t, dir, "diff", link); code != 0 || !strings.Contains(dout, "f3.txt") {
		t.Fatalf("diff <link> exit %d out=%q err=%s", code, dout, errb)
	}
	code, rout, errb := runCLI(t, dir, "link", "resolve", "--json", link)
	if code != 0 || !strings.Contains(rout, `"hint_kind":"version"`) || !strings.Contains(rout, `"hint_id":"1753100000-rebase"`) {
		t.Fatalf("resolve --json exit %d out=%q err=%s", code, rout, errb)
	}
}
```

Run: `go test ./internal/cli/ -run TestVersionLinkRoundTripsThroughDiffAndResolve` → PASS (it exercises code already written; it is the Review Focus pin for the CLI round trip). Run `./test.sh e2e` → PASS, including s82 and s95. If s95's `link --pair HEAD~1^..HEAD~1` run does not resolve in the sandbox, drop that run (it is illustrative) — the first two runs are the assertions.

- [ ] **Step 4: Commit**

`feat(cli): gg versions prints each version's preview link; drift report names it` + trailers.

---

### Task 4: TUI producers — `L` in the versions popup, the drift notice action

**Files:**
- Create: `internal/tui/version_link.go`, `internal/tui/version_link_test.go`
- Modify: `internal/tui/versions_popup.go:176-180` (key), `:438-445` (hint line)
- Modify: `internal/tui/notify.go:36-40` (`noticeAction.keep`), `:611-619` (actions); `internal/tui/notice_popup.go:130-146` (`applyNoticeAction`)
- Modify: `internal/tui/link_compare_desc.go:76-86` (`linkOriginText`)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Test: `internal/tui/drift_test.go`, `internal/tui/versions_popup_test.go`

**Interfaces:**
- Consumes: `m.linkRepoFor("")`, `m.copyToClipboardCmd(ok, text)`, `domain.DriftReport.Version`, `model.BranchVersion.ID()`.
- Produces: `func (m Model) versionLinkFor(v model.BranchVersion) (string, bool)`; `noticeAction{keep: true}` leaves the notice in place.

- [ ] **Step 1: Failing tests — the producer helper**

`internal/tui/version_link_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

const (
	tvBase = "1111111111111111111111111111111111111111"
	tvOurs = "2222222222222222222222222222222222222222"
)

func twoBranchVersion() model.BranchVersion {
	return model.BranchVersion{Ref: "refs/gg/versions/main/1753100000-rebase", Hash: tvOurs, Subject: "did a rebase", Op: "rebase", Unix: 1753100000, Base: tvBase, Ours: tvOurs}
}

func TestVersionLinkForIsThePreviewPairPlusTheHint(t *testing.T) {
	t.Parallel()
	m := Model{linkRepoName: "gigagit"}
	got, ok := m.versionLinkFor(twoBranchVersion())
	if !ok || got != "gg://gigagit@"+tvBase+".."+tvOurs+"?version=1753100000-rebase" {
		t.Fatalf("versionLinkFor = %q ok=%v", got, ok)
	}
}

func TestVersionLinkForRefusesAOneBranchRecord(t *testing.T) {
	t.Parallel()
	m := Model{linkRepoName: "gigagit"}
	v := twoBranchVersion()
	v.Base, v.Ours = "", ""
	if got, ok := m.versionLinkFor(v); ok || got != "" {
		t.Fatalf("one-branch record: got %q ok=%v, want none", got, ok)
	}
}

// The trailer is split on whitespace and never sha-checked: a crafted or
// old record can hold anything, and the producer must decline, not emit.
func TestVersionLinkForRefusesAnUnusableRecord(t *testing.T) {
	t.Parallel()
	m := Model{linkRepoName: "gigagit"}
	v := twoBranchVersion()
	v.Ours = "not-a-sha"
	if got, ok := m.versionLinkFor(v); ok || strings.Contains(got, "not-a-sha") {
		t.Fatalf("unusable record: got %q ok=%v", got, ok)
	}
}
```

Run: `go test ./internal/tui/ -run 'TestVersionLinkFor'` → build failure.

- [ ] **Step 2: Implement the helper**

`internal/tui/version_link.go`:

```go
package tui

import "github.com/homeend/gigagit/internal/model"

// versionLinkFor is the TUI's ONE producer of a version's preview link —
// gg://<repo>@<base>..<ours>?version=<id> — shared by the versions popup's L
// and the drift notice's copy action. false for a one-branch record (it
// records no preview), a record whose trailer does not hold two usable shas
// (the trailer is never sha-checked on write), or a checkout the grammar
// cannot spell. Never concatenated: built as a model.Link and rendered.
func (m Model) versionLinkFor(v model.BranchVersion) (string, bool) {
	if v.Base == "" || v.Ours == "" {
		return "", false
	}
	if _, err := model.CommitEndpoint(v.Base); err != nil {
		return "", false
	}
	if _, err := model.CommitEndpoint(v.Ours); err != nil {
		return "", false
	}
	repo, ok := m.linkRepoFor("")
	if !ok {
		return "", false
	}
	l := model.Link{
		Repo:   repo,
		Target: model.LinkTarget{State: model.StateCommitted, Pair: &model.LinkPair{A: v.Base, B: v.Ours}},
		Side:   model.NoteSideNew,
		Hint:   model.LinkHint{Kind: "version", ID: v.ID()},
	}
	s := l.String()
	if _, err := model.ParseLink(s); err != nil {
		return "", false
	}
	return s, true
}
```

Run → PASS.

- [ ] **Step 3: Failing tests — the popup's `L`**

Append to `internal/tui/versions_popup_test.go`:

```go
// L on a two-branch row copies the preview link (the bookmark popup's key
// for the same thing); the hint line advertises it.
func TestVersionsPopupLCopiesThePreviewLink(t *testing.T) {
	t.Parallel()
	var copied string
	m := Model{width: 120, height: 40, linkRepoName: "gigagit"}
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	p := &versionsPopup{mode: versionsModeVersions, branch: "main", rows: []model.BranchVersion{twoBranchVersion()}}
	m = m.pushLayer(p)
	if !strings.Contains(m.View(), "[L] copy link") {
		t.Fatal("the hint line must advertise L")
	}
	mm, cmd := m.Update(keyMsg("L"))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("L must return the copy command")
	}
	cmd()
	if want := "gg://gigagit@" + tvBase + ".." + tvOurs + "?version=1753100000-rebase"; copied != want {
		t.Fatalf("copied %q, want %q", copied, want)
	}
	if layerOf[*versionsPopup](m) == nil {
		t.Fatal("copying must leave the popup open")
	}
}

func TestVersionsPopupLOnAOneBranchRowSaysNoPreview(t *testing.T) {
	t.Parallel()
	m := Model{width: 120, height: 40, linkRepoName: "gigagit"}
	v := twoBranchVersion()
	v.Base, v.Ours = "", ""
	p := &versionsPopup{mode: versionsModeVersions, branch: "main", rows: []model.BranchVersion{v}}
	m = m.pushLayer(p)
	mm, cmd := m.Update(keyMsg("L"))
	m = mm.(Model)
	if cmd != nil || m.statusMsg != i18n.T("this version records no preview") {
		t.Fatalf("status = %q cmd=%v, want the no-preview message and no copy", m.statusMsg, cmd)
	}
}
```

(imports: `io`, `strings`, `i18n`, `model` as needed.) Run → FAIL.

- [ ] **Step 4: Implement `L` + hint line**

`versions_popup.go` `update`, in the `tea.KeyRunes` switch after `case "y":`:

```go
		case "L":
			if p.mode == versionsModeVersions {
				return p.onCopyLink(m)
			}
```

Add after `onCopy`:

```go
// onCopyLink copies the row's preview link — what enter opens, addressed:
// gg://<repo>@<base>..<ours>?version=<id>. A one-branch record has no
// preview to link; an unusable record declines the way onEnter does.
func (p *versionsPopup) onCopyLink(m Model) (Model, tea.Cmd) {
	if p.sel < 0 || p.sel >= len(p.rows) {
		return m, nil
	}
	v := p.rows[p.sel]
	if v.Base == "" || v.Ours == "" {
		m.statusMsg = i18n.T("this version records no preview")
		return m, nil
	}
	text, ok := m.versionLinkFor(v)
	if !ok {
		m.statusMsg = i18n.T("the recorded commit is not usable")
		return m, nil
	}
	return m, m.copyToClipboardCmd(i18n.T("Copied link: %s", text), text)
}
```

Hint line: `hint = i18n.T("[enter] preview  [r] restore  [d] delete  [y] copy sha  [L] copy link")`.

Bundles — replace the old hint key (line ~1806 in each) and add the new key next to it:

| key | ja | ko | zh | ru |
|---|---|---|---|---|
| `[enter] preview  [r] restore  [d] delete  [y] copy sha  [L] copy link` | `[enter] プレビュー  [r] 復元  [d] 削除  [y] SHA をコピー  [L] リンクをコピー` | `[enter] 미리보기  [r] 복원  [d] 삭제  [y] SHA 복사  [L] 링크 복사` | `[enter] 预览  [r] 恢复  [d] 删除  [y] 复制 SHA  [L] 复制链接` | `[enter] предпросмотр  [r] восстановить  [d] удалить  [y] копировать SHA  [L] копировать ссылку` |
| `this version records no preview` | `このバージョンにはプレビューが記録されていません` | `이 버전에는 미리보기가 기록되어 있지 않습니다` | `此版本未记录预览` | `эта версия не содержит предпросмотра` |

Run the two popup tests → PASS. Run `go test ./internal/tui/ -run 'I18n|Vocab|MenuLabels|EngineProse'` → PASS.

- [ ] **Step 5: Failing tests — the notice action and `keep`**

Append to `internal/tui/drift_test.go`:

```go
// The drifted notice offers "Copy preview link" beside Dismiss — and the
// copy leaves the notice standing: it IS the list the user is about to
// research (the dismiss-only-surface lesson).
func TestDriftNoticeCopyLinkKeepsTheNotice(t *testing.T) {
	t.Parallel()
	var copied string
	m := Model{linkRepoName: "gigagit"}
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	report := domain.DriftReport{
		Ref: "refs/gg/versions/feat/1753100000-rebase", Checked: true,
		Report:  changeset.Report{Added: []changeset.Entry{{Status: 'A', Path: "f3.txt"}}},
		Version: model.BranchVersion{Ref: "refs/gg/versions/feat/1753100000-rebase", Op: "rebase", Unix: 1753100000, Base: tvBase, Ours: tvOurs},
	}
	m, _ = m.applyDriftReport("feat", report, false)
	n := &m.notices[0]
	act := noticeActionByLabel(t, n, i18n.T("Copy preview link"))
	m, cmd := m.applyNoticeAction(*n, act)
	if cmd == nil {
		t.Fatal("the action must return the copy command")
	}
	cmd()
	if want := "gg://gigagit@" + tvBase + ".." + tvOurs + "?version=1753100000-rebase"; copied != want {
		t.Fatalf("copied %q, want %q", copied, want)
	}
	if len(m.notices) != 1 {
		t.Fatalf("notices = %d after copying, want the notice kept", len(m.notices))
	}
	if m.noticeSessionDismissed[n.id] {
		t.Fatal("copying must not mark the notice dismissed")
	}
}

// A paused-only finding names no record: Dismiss stays alone.
func TestDriftNoticePausedOnlyHasNoCopyAction(t *testing.T) {
	t.Parallel()
	m := Model{linkRepoName: "gigagit"}
	m, _ = m.applyDriftReport("feat", domain.DriftReport{}, true)
	for _, a := range m.notices[0].actions {
		if a.label == i18n.T("Copy preview link") {
			t.Fatal("a paused-only notice must not offer a link")
		}
	}
}
```

(`changeset.Entry`/`Report` field names: check `internal/changeset` — `Entry{Status byte; Path string}` per `printDrift`'s `%c`; adjust to the real names.) Run → FAIL (`no action "Copy preview link"`).

- [ ] **Step 6: Implement `keep` + the action**

`notify.go`:

```go
type noticeAction struct {
	label string
	run   func(Model) (Model, tea.Cmd)
	never bool
	// keep leaves the notice standing after run: a copy is not a dismissal,
	// and removing the notice would delete the list the copy is about.
	keep bool
}
```

`notice_popup.go` `applyNoticeAction`:

```go
func (m Model) applyNoticeAction(n notice, act noticeAction) (Model, tea.Cmd) {
	if !act.keep {
		m = m.removeNotice(n.id)
		m.noticeSessionDismissed[n.id] = true // a mid-session health re-read must not resurrect it
	}
	// … (never / run unchanged)
```

(`noticeSessionDismissed` may be nil on a bare test Model: keep the existing guard/initialisation if there is one; if the map write panics on the bare `Model{}` in the new test, initialise it lazily: `if m.noticeSessionDismissed == nil { m.noticeSessionDismissed = map[string]bool{} }` — check how `applyDriftReport` creates it.)

`driftNotice`, build actions:

```go
	actions := []noticeAction{{label: i18n.T("Dismiss")}}
	if report.Version.Base != "" && report.Version.Ours != "" {
		v := report.Version
		actions = append([]noticeAction{{
			label: i18n.T("Copy preview link"),
			keep:  true,
			run: func(m Model) (Model, tea.Cmd) {
				text, ok := m.versionLinkFor(v)
				if !ok {
					m.statusMsg = i18n.T("the recorded commit is not usable")
					return m, nil
				}
				return m, m.copyToClipboardCmd(i18n.T("Copied link: %s", text), text)
			},
		}}, actions...)
	}
	return &notice{ /* … */ actions: actions }
```

Bundles: `"Copy preview link"` → ja `プレビューのリンクをコピー`, ko `미리보기 링크 복사`, zh `复制预览链接`, ru `Копировать ссылку на предпросмотр`. Place next to `"Dismiss"`.

`link_compare_desc.go` `linkOriginText`: add `case "version": return i18n.T("copied from a recorded version")` — bundles next to `"copied from a saved preview"`: ja `記録されたバージョンからコピー`, ko `기록된 버전에서 복사됨`, zh `复制自已记录的版本`, ru `скопировано из записанной версии`.

Run: `go test ./internal/tui/ -run 'TestDriftNotice|TestVersionsPopup|TestVersionLinkFor|I18n|Vocab|MenuLabels|EngineProse|TestNotice'` → PASS. The existing notice tests that assert `len(m.notices) == 0` after an action still pass (Dismiss has no `keep`).

- [ ] **Step 7: Commit**

`feat(tui): copy a version's preview link — L in the versions popup, Copy preview link on the drift notice` + trailers.

---

### Task 5: TUI consumer — the `version` landing arm

**Files:**
- Create: `internal/tui/version_hint.go`, `internal/tui/version_hint_test.go`
- Modify: `internal/tui/steer_nav.go:244-270` (`navigateLanded` arm)
- Modify: `internal/tui/model.go` `Update` (route the new msg; find the `bookmarksLoadedMsg` case near line 1368 and add a sibling case)
- Modify: `internal/i18n/lang/*.toml`

**Interfaces:**
- Consumes: `svc.FindVersion`, `svc.BranchVersions`, `pendingHint{cmd, tag, at}`, `m.hintGen`, `versionsPopup`, `m.filesReturnLayers`.
- Produces: `versionHintLoadedMsg{gen int; branch string; rows []model.BranchVersion; idx int; found bool; err error}`; `func (m Model) loadVersionForHintCmd(c steer.Command, gen int) tea.Cmd`; `func (m Model) versionHintLoaded(msg versionHintLoadedMsg) (Model, tea.Cmd)`.

- [ ] **Step 1: Failing tests**

`internal/tui/version_hint_test.go`:

```go
package tui

import (
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

func versionCmd(hintID string) steer.Command {
	return steer.Command{Cmd: "navigate", Target: &steer.Target{State: "pair", A: tvBase, B: tvOurs}, HintKind: "version", HintID: hintID}
}

// landedVersionModel: a Model whose compare view is already open on the
// pair (what steerNavigatePair leaves behind before navigateLanded runs).
func landedVersionModel(t *testing.T) Model {
	t.Helper()
	m := Model{width: 120, height: 40, linkRepoName: "gigagit"}
	base, _ := model.CommitEndpoint(tvBase)
	ours, _ := model.CommitEndpoint(tvOurs)
	m, _ = m.openCompareFiles(base, ours)
	if m.filesView == nil {
		t.Fatal("fixture: compare view did not open")
	}
	return m
}

func rowsFixture() []model.BranchVersion {
	other := twoBranchVersion()
	other.Ref, other.Unix, other.Op = "refs/gg/versions/main/1753200000-merge", 1753200000, "merge"
	return []model.BranchVersion{other, twoBranchVersion()} // newest first; ours is row 1
}

// The arm parks a load, never a for-each-ref on the Update thread.
func TestVersionHintParksALoadUnderTheHintGeneration(t *testing.T) {
	t.Parallel()
	m := landedVersionModel(t)
	nm, cmd := m.navigateLanded(versionCmd("1753100000-rebase"), "opened")
	if nm.pendingHint == nil || nm.pendingHint.cmd.HintKind != "version" || nm.pendingHint.tag != nm.hintGen {
		t.Fatalf("pendingHint = %+v, want the version hint parked under hintGen %d", nm.pendingHint, nm.hintGen)
	}
	if cmd == nil {
		t.Fatal("the arm must return the load command")
	}
}

// A hit parks the versions popup UNDER the open compare, on the record's
// row: esc on the compare restores it, exactly "popup → enter" in reverse.
func TestVersionHintParksThePopupUnderTheCompareOnTheRow(t *testing.T) {
	t.Parallel()
	m := landedVersionModel(t)
	m, _ = m.navigateLanded(versionCmd("1753100000-rebase"), "opened")
	m, _ = m.versionHintLoaded(versionHintLoadedMsg{gen: m.hintGen, branch: "main", rows: rowsFixture(), idx: 1, found: true})
	if m.pendingHint != nil {
		t.Fatal("the pending hint must be consumed")
	}
	if len(m.filesReturnLayers) != 1 {
		t.Fatalf("filesReturnLayers = %d, want the popup parked", len(m.filesReturnLayers))
	}
	p, ok := m.filesReturnLayers[0].(*versionsPopup)
	if !ok || p.mode != versionsModeVersions || p.branch != "main" || p.sel != 1 || len(p.rows) != 2 {
		t.Fatalf("parked = %+v, want versions mode for main on row 1", p)
	}
	if layerOf[*versionsPopup](m) != nil {
		t.Fatal("the popup must be parked, not drawn over the compare")
	}
}

// The compare was closed before the lookup returned: the popup is pushed
// live instead of parked under nothing.
func TestVersionHintPushesThePopupLiveWhenTheCompareIsGone(t *testing.T) {
	t.Parallel()
	m := landedVersionModel(t)
	m, _ = m.navigateLanded(versionCmd("1753100000-rebase"), "opened")
	m = m.closeFilesView()
	m, _ = m.versionHintLoaded(versionHintLoadedMsg{gen: m.hintGen, branch: "main", rows: rowsFixture(), idx: 1, found: true})
	p := layerOf[*versionsPopup](m)
	if p == nil || p.sel != 1 {
		t.Fatalf("popup = %+v, want it pushed live on row 1", p)
	}
}

func TestVersionHintMissIsANoticeAndTouchesNothing(t *testing.T) {
	t.Parallel()
	m := landedVersionModel(t)
	m, _ = m.navigateLanded(versionCmd("1600000000-rebase"), "opened")
	m, _ = m.versionHintLoaded(versionHintLoadedMsg{gen: m.hintGen, found: false})
	if m.statusMsg != i18n.T("version %s is not recorded here; the link still landed", "1600000000-rebase") {
		t.Fatalf("status = %q", m.statusMsg)
	}
	if len(m.filesReturnLayers) != 0 || layerOf[*versionsPopup](m) != nil {
		t.Fatal("a miss must not park or push anything")
	}
}

func TestVersionHintStaleGenerationIsDropped(t *testing.T) {
	t.Parallel()
	m := landedVersionModel(t)
	m, _ = m.navigateLanded(versionCmd("1753100000-rebase"), "opened")
	m, _ = m.versionHintLoaded(versionHintLoadedMsg{gen: m.hintGen - 1, branch: "main", rows: rowsFixture(), idx: 1, found: true})
	if m.pendingHint == nil || len(m.filesReturnLayers) != 0 {
		t.Fatal("a stale load must neither consume the pending hint nor park anything")
	}
}

var _ tea.Msg = versionHintLoadedMsg{}
```

(`closeFilesView` exists per `layer_stack.go`'s comment; confirm its exact name/receiver with `rtk grep -n "func (m Model) closeFilesView" internal/tui/*.go` and adjust. If `openCompareFiles` needs `m.svc` for a bare Model, look at how `TestVersionsPopupEnterOpensCompare` gets a compare view open with `Model{width, height}` — it does, via `enter`; reuse that path: push a `versionsPopup` with `twoBranchVersion()` and send `enter`, then clear `m.filesReturnLayers = nil` so the test starts from "compare open, nothing parked".)

Run: `go test ./internal/tui/ -run 'TestVersionHint'` → build failure.

- [ ] **Step 2: Implement**

`internal/tui/version_hint.go`:

```go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// versionHintLoadedMsg carries a ?version= hint's lookup back to the UI
// thread: the record's branch and that branch's rows (newest first), with
// idx the record's row. found=false is a miss, err a failed read.
type versionHintLoadedMsg struct {
	gen    int
	branch string
	rows   []model.BranchVersion
	idx    int
	found  bool
	err    error
}

// loadVersionForHintCmd runs domain.FindVersion (one for-each-ref) plus the
// branch's row list off the Update thread, stamped with the hint generation.
func (m Model) loadVersionForHintCmd(c steer.Command, gen int) tea.Cmd {
	svc := m.svc
	id := c.HintID
	var a, b string
	if c.Target != nil {
		a, b = c.Target.A, c.Target.B
	}
	return func() tea.Msg {
		if svc == nil {
			return versionHintLoadedMsg{gen: gen}
		}
		ctx := context.Background()
		branch, v, ok, err := svc.FindVersion(ctx, id, a, b)
		if err != nil || !ok {
			return versionHintLoadedMsg{gen: gen, err: err}
		}
		rows, err := svc.BranchVersions(ctx, branch)
		if err != nil {
			return versionHintLoadedMsg{gen: gen, err: err}
		}
		idx := -1
		for i := range rows {
			if rows[i].Ref == v.Ref {
				idx = i
				break
			}
		}
		if idx < 0 {
			return versionHintLoadedMsg{gen: gen}
		}
		return versionHintLoadedMsg{gen: gen, branch: branch, rows: rows, idx: idx, found: true}
	}
}

// versionHintLoaded honours the hint once its lookup lands: the Branch
// versions popup on that row, PARKED under the compare the link opened (esc
// on the compare restores it — "popup → enter" in reverse), or pushed live
// when the compare has since closed. A miss is a notice; a stale generation
// is dropped. The hint degrades, it never fails.
func (m Model) versionHintLoaded(msg versionHintLoadedMsg) (Model, tea.Cmd) {
	ph := m.pendingHint
	if ph == nil || ph.cmd.HintKind != "version" || msg.gen != ph.tag {
		return m, nil
	}
	m.pendingHint = nil
	if !msg.found {
		m.statusMsg = i18n.T("version %s is not recorded here; the link still landed", ph.cmd.HintID)
		return m, nil
	}
	p := &versionsPopup{mode: versionsModeVersions, branch: msg.branch, rows: msg.rows, sel: msg.idx}
	if m.filesView != nil {
		m.filesReturnLayers = []layer{p} // the latest opener wins
		return m, nil
	}
	return m.pushLayer(p), nil
}
```

`steer_nav.go` `navigateLanded`, after the `case "preview":` arm:

```go
	case "version":
		m.hintGen++
		m.pendingHint = &pendingHint{cmd: c, tag: m.hintGen, at: time.Now()}
		return m, tea.Batch(reply, m.loadVersionForHintCmd(c, m.hintGen))
```

`model.go` `Update`: add `case versionHintLoadedMsg: return m.versionHintLoaded(msg)` next to the `bookmarksLoadedMsg` case.

Bundle key `version %s is not recorded here; the link still landed` next to the `preview %s is not saved here…` key: ja `バージョン %s はここに記録されていません。リンク先は開きました`, ko `버전 %s 은(는) 여기에 기록되어 있지 않습니다. 링크는 열렸습니다`, zh `版本 %s 未在此处记录；链接仍已打开`, ru `версия %s здесь не записана; ссылка всё же открыта`.

Run: `go test ./internal/tui/ -run 'TestVersionHint|TestPreviewHint|I18n'` → PASS. Then the whole `go test ./internal/tui/` (several minutes) → PASS.

- [ ] **Step 3: Live check on the binary**

`go build -o /home/homeend/.claude/jobs/ece1681e/tmp/gg ./cmd/gg`, then in a scratch repo under `$CLAUDE_JOB_DIR/tmp` (two branches, `gg merge`, `gg versions main` prints the link): run `./tui-capture.sh` with a keyscript that pastes the link into `#` and then presses esc — the snapshot after esc shows the Branch versions popup on the row (see the `driving-tui-headless` skill for the keyscript form). Keep the snapshot path in the commit message body.

- [ ] **Step 4: Commit**

`feat(tui): a ?version= link lands on the compare with the Branch versions popup parked beneath` + trailers.

---

### Task 6: web — find door, row link, reveal, gates, probe

**Files:**
- Modify: `internal/web/versions.go` (row fields; new handler), `internal/web/server.go:181` (route)
- Modify: `internal/web/static/versions.js` (menu row; `revealVersion`), `internal/web/static/live.js:426-428` (`revealHint`), `internal/web/static/links.js:62-64` (kind gate), `internal/web/static/style.css:448` (flash rule)
- Modify: `internal/web/steer.go:227-229` (comment only)
- Test: `internal/web/versions_test.go` (or `drift_test.go`), `internal/web/linksjs_test.go`, `internal/web/steerhintjs_test.go`

**Interfaces:**
- Consumes: `svc.FindVersion`, `svc.LinkRepo`, `svc.DescribeLink`, `isFullSha`, `copyLink(link, desc)` (links.js), `openVersions(branch)`, `pushLayer`.
- Produces: `GET /api/version-find?id=&a=&b=` → `{found: bool, branch: string, ref: string}`; `versionRow.Link`/`.Desc` (`json:"link,omitempty"`, `json:"desc,omitempty"`); `export async function revealVersion(s)` in versions.js.

- [ ] **Step 1: Failing handler tests**

Append to `internal/web/versions_test.go`:

```go
// A two-branch version row carries its preview link (server-built, like a
// saved pair's) and the description its copy records.
func TestVersionsRowCarriesThePreviewLink(t *testing.T) {
	t.Parallel()
	dir := driftRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	runOpOK(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)
	var body struct {
		Versions []struct {
			Ref  string `json:"ref"`
			Link string `json:"link"`
			Desc string `json:"desc"`
		} `json:"versions"`
	}
	if code := getJSON(t, ts, "/api/versions?branch=main", &body); code != http.StatusOK || len(body.Versions) != 1 {
		t.Fatalf("GET /api/versions = %d, %+v", code, body)
	}
	v := body.Versions[0]
	id := v.Ref[strings.LastIndex(v.Ref, "/")+1:]
	if !strings.HasPrefix(v.Link, "gg:///") || !strings.HasSuffix(v.Link, "?version="+id) {
		t.Fatalf("link = %q, want the local-form pair link with ?version=%s", v.Link, id)
	}
	if !strings.HasPrefix(v.Desc, "version: main · merge · ") {
		t.Fatalf("desc = %q", v.Desc)
	}
}

// /api/version-find answers a landed hint: by id + pair, or a miss.
func TestVersionFindHitAndMiss(t *testing.T) {
	t.Parallel()
	dir := driftRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	runOpOK(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)
	var list struct {
		Versions []struct {
			Ref string `json:"ref"`
			Link string `json:"link"`
		} `json:"versions"`
	}
	getJSON(t, ts, "/api/versions?branch=main", &list)
	l, err := model.ParseLink(list.Versions[0].Link)
	if err != nil {
		t.Fatal(err)
	}
	q := url.Values{"id": {l.Hint.ID}, "a": {l.Target.Pair.A}, "b": {l.Target.Pair.B}}
	var out struct {
		Found  bool   `json:"found"`
		Branch string `json:"branch"`
		Ref    string `json:"ref"`
	}
	if code := getJSON(t, ts, "/api/version-find?"+q.Encode(), &out); code != http.StatusOK || !out.Found || out.Branch != "main" || out.Ref != list.Versions[0].Ref {
		t.Fatalf("find hit = %d %+v", code, out)
	}
	q.Set("id", "1600000000-rebase")
	q.Set("b", strings.Repeat("f", 40))
	if code := getJSON(t, ts, "/api/version-find?"+q.Encode(), &out); code != http.StatusOK || out.Found {
		t.Fatalf("find miss = %d %+v, want 200 found:false", code, out)
	}
	if code := getJSON(t, ts, "/api/version-find?id=x&a=nope&b=nope", &out); code != http.StatusBadRequest {
		t.Fatalf("bad shas = %d, want 400", code)
	}
}
```

Run: `go test ./internal/web/ -run 'TestVersionsRowCarriesThePreviewLink|TestVersionFindHitAndMiss'` → FAIL.

- [ ] **Step 2: Implement the server side**

`versions.go` — `versionRow` gains:

```go
	// Link is the version's preview link (gg://<repo>@<base>..<ours>?version=<id>)
	// and Desc what copying it records; both empty for a one-branch record
	// or a checkout the grammar cannot spell (the menu row is then not offered).
	Link string `json:"link,omitempty"`
	Desc string `json:"desc,omitempty"`
```

In `handleVersions`, compute `repo, repoErr := s.service().LinkRepo(r.Context())` once before the loop, and per row:

```go
		row := versionRow{ /* existing fields */ }
		if repoErr == nil && v.Base != "" && v.Ours != "" {
			if _, e1 := model.CommitEndpoint(v.Base); e1 == nil {
				if _, e2 := model.CommitEndpoint(v.Ours); e2 == nil {
					l := model.Link{Repo: repo, Target: model.LinkTarget{State: model.StateCommitted, Pair: &model.LinkPair{A: v.Base, B: v.Ours}}, Side: model.NoteSideNew, Hint: model.LinkHint{Kind: "version", ID: v.ID()}}
					if text := l.String(); text != "" {
						if _, err := model.ParseLink(text); err == nil {
							row.Link, row.Desc = text, s.service().DescribeLink(r.Context(), l)
						}
					}
				}
			}
		}
		rows = append(rows, row)
```

New handler in `versions.go`:

```go
// handleVersionFind answers a landed ?version= hint for the page, which
// holds no branch to ask /api/versions with: domain.FindVersion's ladder
// (id tie-broken by the pair, else the pair). A miss is 200 found:false —
// the hint degrades on the page, it never fails the landing.
func (s *Server) handleVersionFind(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, a, b := q.Get("id"), q.Get("a"), q.Get("b")
	if !model.LinkHintIDOK(id) || !isFullSha(a) || !isFullSha(b) {
		writeErr(w, http.StatusBadRequest, errors.New("version-find needs id and two full shas"))
		return
	}
	branch, v, ok, err := s.service().FindVersion(r.Context(), id, a, b)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, map[string]any{"found": false})
		return
	}
	writeJSON(w, map[string]any{"found": true, "branch": branch, "ref": v.Ref})
}
```

`server.go`: `mux.HandleFunc("GET /api/version-find", s.handleVersionFind)` next to the versions route. `steer.go:227` comment: list the kinds as `"bookmark", "shelf", "stash", "preview", "version"`.

Run → PASS.

- [ ] **Step 3: Failing JS gates**

`internal/web/linksjs_test.go` — add rows to the substring table:

```go
		// Version links (2026-09-29): the kind gate must admit the new kind,
		// the versions row must copy the server-built link, and the landing
		// must route to the versions layer before the entry reveal's
		// "cannot reveal" path.
		{"links.js", `kind === "version"`, "linkHintKindOK must admit the version kind (model.linkHintKinds' twin)"},
		{"versions.js", "copy gg link", "the versions row menu must offer the preview link"},
		{"versions.js", "export async function revealVersion(s)", "the version reveal must exist"},
		{"live.js", `s.hint_kind === "version"`, "revealHint must route a version hint to the versions layer"},
```

`internal/web/steerhintjs_test.go` — add:

```go
// The version reveal flashes a row of #versions-list; the rule must exist or
// the reveal is invisible (the revealSavedSet gate's twin).
func TestRevealVersionTargetHasAFlashRule(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("static", "versions.js"))
	if err != nil {
		t.Fatal(err)
	}
	css, err := os.ReadFile(filepath.Join("static", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(src), "export async function revealVersion(s) {")
	if i < 0 {
		t.Fatal("versions.js: revealVersion is gone")
	}
	j := strings.Index(string(src)[i:], "\n}\n")
	body := string(src)[i : i+j]
	m := regexp.MustCompile(`\$\("([a-z]+-list)"\)`).FindStringSubmatch(body)
	if m == nil || !strings.Contains(body, `classList.add("flash")`) {
		t.Fatalf("versions.js: revealVersion no longer flashes a *-list row")
	}
	if !regexp.MustCompile(`#` + m[1] + `\s+li\.flash\b`).Match(css) {
		t.Errorf("style.css has no `#%s li.flash` rule — the reveal would flash invisibly", m[1])
	}
}
```

Run: `go test ./internal/web/ -run 'TestLinksJS|TestRevealVersionTargetHasAFlashRule|TestRevealHintEntry'` → FAIL.

- [ ] **Step 4: Implement the page**

`links.js`: `return kind === "bookmark" || kind === "shelf" || kind === "stash" || kind === "preview" || kind === "view" || kind === "version";`

`versions.js` — in `showVersionMenu`, after the `open recorded preview` item:

```js
  if (v.link) items.push({ label: "copy gg link", act: () => copyLink(v.link, v.desc) });
```

(import `copyLink` from `./links.js` — check its export name; `copyLinkRow(link, desc)` returns the same row shape and may be used instead: `if (v.link) items.push(copyLinkRow(v.link, v.desc));`.)

Add and export:

```js
// revealVersion honours a landed ?version= hint: the server finds the
// record (id tie-broken by the pair, else the pair), and the versions layer
// for its branch opens ON TOP of the landed compare with the row flashed —
// esc shows the diff. The web has no "under": openVersionPreview closes
// this layer before opening a compare, so the order is the TUI's reversed.
// A miss is one op line; the hint degrades, it never fails.
export async function revealVersion(s) {
  let body;
  try {
    const q = new URLSearchParams({ id: s.hint_id, a: s.a || "", b: s.b || "" });
    body = await getJSON("/api/version-find?" + q.toString());
  } catch (e) {
    opLine("gg link: could not look up version " + s.hint_id + "; the link still landed", true);
    return;
  }
  if (!body.found) {
    opLine("gg link: version " + s.hint_id + " is not recorded here; the link still landed", true);
    return;
  }
  await openVersions(body.branch);
  const rows = $("versions-list")._rows || [];
  const i = rows.findIndex((r) => r.ref === body.ref);
  const li = i >= 0 ? $("versions-list").querySelector('li[data-i="' + i + '"]') : null;
  if (!li) return;
  li.scrollIntoView({ block: "center" });
  li.classList.add("flash");
  setTimeout(() => li.classList.remove("flash"), 900);
}
```

Add `revealVersion` to the `export { … }` list.

`live.js`:

```js
function revealHint(s) {
  if (s.hint_kind === "version") return revealVersion(s);
  return s.hint_kind === "preview" ? revealSavedSet(s) : revealHintEntry(s.hint_kind, s.hint_id);
}
```

(import `revealVersion` from `./versions.js`; watch for an import cycle — versions.js imports from `./ops.js`/`./files.js`, not live.js, so it is safe.)

`style.css:448`: append `, #versions-list li.flash` to the selector list.

Run: `go test ./internal/web/` → PASS (node gates included).

- [ ] **Step 5: Playwright probe**

Write `/home/homeend/.claude/jobs/ece1681e/tmp/versionprobe.mjs` (playwright, `executablePath` = the cached chromium, as the symmetric-compare probe did): fixture repo with a merge (so `main` has a `pull`/`merge` record), start `gg web` from a build of this branch, then:
1. open the Branches sidebar row menu → `previous versions…` → row menu → `copy gg link`; read the clipboard via `page.evaluate(() => navigator.clipboard.readText())` and assert it matches `/\?version=\d+-merge$/`.
2. navigate: `gg open --web <link>` (or post the steer the TUI's `#` uses via `/api/link-command`) and assert `#versions` has computed `display !== "none"`, the flashed row's text contains `merge`, and `#versions-title` names `main`.
3. Escape → `#versions` hidden, the compare pane visible.
Run FIRST against a build with the `version` arm removed from `revealHint` (must FAIL on step 2), then the real build (PASS). Record both outcomes in the commit body.

- [ ] **Step 6: Commit**

`feat(web): version links — row copy, /api/version-find, the versions layer revealed on landing` + trailers.

---

### Task 7: docs, skill bump, comments, gates

**Files:**
- Modify: `internal/agentskill/using-gg.md` (grammar block ~line 127; hint paragraph ~143; `gg versions` bullet ~574), `internal/agentskill/agentskill.go:22` (`Version = 102`)
- Modify: `CHANGELOG.md` (new top section), `README.md` (one line in the links paragraph near line 79), `docs/CLAUDE-details.md` (`<hint>` line ~820 and a short "version links" paragraph after the preview-hint one)
- Modify: `internal/steer/steer.go:106-108`, `internal/mcp/links.go:56-58` (comments list the kinds)

- [ ] **Step 1: using-gg**

Grammar block, after the `?preview=` row:

```text
gg://<repo>@<base>..<ours>?version=<unix>-<op>   a RECORDED BRANCH VERSION's frozen preview (the id `gg versions` prints)
```

Hint row: `… hint: bookmark | shelf | stash | preview | version`.

After the `?preview=` paragraph:

```
`?version=<id>` marks a link copied off a recorded BRANCH VERSION (the
snapshot gg takes before a merge/rebase/pull): the address is that version's
frozen preview `@<base>..<ours>`, so `gg diff` on it shows what the branch
contributed BEFORE the operation, and a TUI/web landing reveals the Branch
versions row it came from. The id is `<unix>-<op>` with no branch (ids reject
`/`); the consumer finds the record by id, tie-broken by the pair. These
links are MACHINE-LOCAL: version refs are never pushed and `<ours>` is a
rewritten tip, so on another checkout the pair itself will not resolve.
```

`gg versions` bullet: after `newest first: <id> <short-sha> <time> <subject>.` add
`A two-branch row is followed by one indented line holding its preview link (gg://<repo>@<base>..<ours>?version=<id>) — paste it to gg diff, or into a chat, to hand someone the pre-operation change set. One-branch rows have none.`

Bump `Version = 102`. Run `go test ./internal/agentskill/` and then `~/go/bin/gg init --update` from the MAIN checkout (`cd /mnt/t/others/gigagit`) only AFTER the merge and `./build.sh install` — note it in the handover instead of running it now.

- [ ] **Step 2: CHANGELOG / README / CLAUDE-details / comments**

CHANGELOG top section:

```markdown
## Links to branch versions

### Added

- **Copy a link to a recorded branch version.** A version's preview — what
  `enter` opens in Branch versions — has a link: `gg://<repo>@<base>..<ours>?version=<unix>-<op>`.
  `L` in the versions popup and *Copy preview link* on the drift notice copy
  it (the notice stays open); `gg versions <branch>` prints it under each
  two-branch row, the CLI's post-operation drift report names it, and the web
  versions row menu offers *copy gg link*. Opening the link lands on the pair's
  diff and reveals the Branch versions popup (web: layer) on that row — by id,
  tie-broken by the pair, else by the pair; a deleted record degrades to the
  plain diff and a notice. Machine-local: version refs are never pushed.
```

README (links paragraph, one sentence): `A recorded branch version copies as its preview link (`?version=`), so a "looks fishy" rebase can be handed to an agent as the pre-operation change set.`

CLAUDE-details: `<hint> = <kind>=<id>, kind ∈ {bookmark, shelf, stash, preview, version, view}` and one paragraph: the ladder of spec §2.2 plus "TUI parks the popup under the compare; web pushes the layer on top".

`steer.go` and `mcp/links.go` comments: `("bookmark", "shelf", "stash", "preview" or "version" — model.LinkHint's closed set …)`.

- [ ] **Step 3: Gates**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/version-links && ./test.sh race
```
Expected: all packages PASS, e2e PASS. (If `internal/tui` reports FAIL with no `--- FAIL` line after the 300 s conflict test, rerun `go test ./internal/tui/` alone — memory `notice-popup-fit-width-fix` records this as environmental — and report both results.)

Build a verify binary: `go build -o /home/homeend/.claude/jobs/ece1681e/tmp/gg-version-links ./cmd/gg` and give the user the path.

- [ ] **Step 4: Commit and hand over**

`docs: version links — using-gg (v102), changelog, readme, details` + trailers. Then `git -C <abs> log --oneline main..feat/version-links`, and ask the user before merging (memory: merge discipline — `gg merge -F <msgfile> --into main feat/version-links` from the main checkout, then `./build.sh install`, `./build.sh web`, `gg init --update`, verify main is clean, remove the worktree).

---

## Self-review

- **Spec coverage:** §2.1 → Task 1; §2.2/§2.3 → Task 2; §2.4 TUI → Task 5, web → Task 6, validators need nothing (they call `LinkHintKindOK`), comments → Task 7; §2.5 → Task 2 (+ web `desc` in Task 6; the spec's "formatter twin / `TestLinkDescJSMatchesGo`" does not exist — `links.js linkDesc`'s default arm already renders `kind: id`, and the versions row copies the SERVER's `desc`, so no JS change is needed there); §2.6 popup `L` + notice → Task 4, `gg versions` + `printDrift` → Task 3, web row → Task 6; §2.7 → Task 7; §4 tests → distributed as listed; §6.10 `keep` → Task 4.
- **Placeholders:** none; every step carries its code. The one "adjust to the real name" note (`closeFilesView`, `changeset.Entry` fields) names the grep to run.
- **Type consistency:** `FindVersion(ctx, id, base, ours) (branch, v, ok, err)` is used identically in Tasks 2, 5, 6; `versionLinkFor` (TUI) vs `versionLinkText` (CLI) are deliberately two names in two packages that cannot share code (`cli` and `tui` both may import `model`, but neither exposes helpers to the other); `versionHintLoadedMsg` fields match between the producer and the test; `noticeAction.keep` is read in `applyNoticeAction` and set in `driftNotice`.
- **Review Focus:** all five lines have their named test in the owning task.
