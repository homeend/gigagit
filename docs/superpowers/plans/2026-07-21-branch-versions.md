# Branch Versions (Operations History) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Before every history-rewriting operation (and merges), record the branch's tip as a hidden ref `refs/gg/versions/<branch>/<unix-ts>-<op>`, and let the user list, compare, restore, and delete those versions from the TUI, CLI, and Settings.

**Architecture:** New git verbs (`UpdateRef`/`DeleteRef`/`ForEachRef`) + a best-effort engine helper `snapshotBranchTip` called at the top of each trigger op, gated by an `engine.VersionsPolicy` carried on `OpDeps` (zero value = disabled, so existing direct-OpDeps tests are unaffected; `domain.Execute` injects the Service's policy, default enabled/90 days). Read side: `domain.BranchVersions`/`AllVersionBranches` over one `for-each-ref`. Restore is a new `RestoreBranchVersion` op (reset-hard for the current branch, `update-ref` otherwise). Spec: `docs/superpowers/specs/2026-07-21-branch-versions-design.md`.

**Tech Stack:** Go 1.26, real-git tests in `t.TempDir()`, Bubble Tea TUI, TOML config/e2e.

## Global Constraints

- Work in the worktree `/mnt/t/others/gigagit.worktrees/feat-branch-versions` on branch `feat/branch-versions`. All paths below are relative to that root. Write/Edit tools MUST use the worktree absolute path.
- `internal/tui` and `internal/cli` never import `internal/git` — go through `internal/domain` (archtest-enforced).
- Every new user-visible TUI string goes through `i18n.T("<english literal>")` and the key MUST exist in ALL FOUR bundles `internal/i18n/lang/{ja,ko,zh,ru}.toml` (append under `[strings]`; bundles are NOT sorted; grep for the key first — a duplicate TOML key fails the parse). Engine summaries/progress/prompts MUST be built only via `WithSummary`/`AppendSummary`/`Progressf`/`PromptReq` with English-literal formats, and those literals must also be in all four bundles (engine-prose AST gates).
- Decision `Options` values are English protocol; every statically declared option literal needs an `optionDisplayName` case (internal/tui/i18n_display.go) + bundle entries. Reuse existing tokens where specified (`"proceed"`, `"cancel"`, `"Delete"`, `"Cancel"` already exist).
- The version-op tokens are protocol values, exactly: `merge`, `rebase`, `interactive-rebase`, `amend`, `undo-commit`, `reset`, `pull`, `delete-branch`, `restore`.
- Snapshot writes are BEST-EFFORT: a failure must never fail the parent op.
- Run `./test.sh unit` before each commit claim; the final task runs `./test.sh race`.
- Commit messages end with:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01GATyiXADZYZWEavQEMrzP4`

---

### Task 1: `[versions]` config section + writers + settingDocs

**Files:**
- Modify: `internal/config/config.go` (Config struct ~:126, Defaults ~:134, Load ~:158, overlay funcs ~:287)
- Modify: `internal/config/write.go` (writers near SetRefreshInterval ~:65)
- Modify: `internal/config/template.go` (settingDocs entries ~:54, section-order slice at ~:96)
- Test: `internal/config/versions_config_test.go` (new), existing `template_test.go` coverage gate

**Interfaces:**
- Produces: `config.VersionsConfig{Disabled bool; MaxAgeDays int}` as `Config.Versions`; `config.SetVersionsDisabled(path string, disabled bool) error`; `config.SetVersionsMaxAgeDays(path string, days int) error`. Default `MaxAgeDays` = 90; `-1` = keep forever; overlay is nonzero-is-set for MaxAgeDays and true-wins for Disabled.

- [ ] **Step 1: Write the failing test** — `internal/config/versions_config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionsDefaultsAndOverlay(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	repo := filepath.Join(dir, "repo.toml")

	cfg, err := Load(global, repo) // neither file exists
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Versions.Disabled || cfg.Versions.MaxAgeDays != 90 {
		t.Fatalf("defaults = %+v, want enabled/90", cfg.Versions)
	}

	os.WriteFile(repo, []byte("[versions]\ndisabled = true\nmax_age_days = -1\n"), 0o644)
	cfg, err = Load(global, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Versions.Disabled || cfg.Versions.MaxAgeDays != -1 {
		t.Fatalf("overlay = %+v, want disabled/-1 (forever)", cfg.Versions)
	}

	// zero-is-unset: an explicit 0 must NOT override the 90 default.
	os.WriteFile(repo, []byte("[versions]\nmax_age_days = 0\n"), 0o644)
	cfg, _ = Load(global, repo)
	if cfg.Versions.MaxAgeDays != 90 {
		t.Fatalf("max_age_days=0 should stay default 90, got %d", cfg.Versions.MaxAgeDays)
	}
}

func TestVersionsWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gg.toml")
	if err := SetVersionsMaxAgeDays(path, -1); err != nil {
		t.Fatal(err)
	}
	if err := SetVersionsDisabled(path, true); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	for _, want := range []string{"[versions]", "max_age_days = -1", "disabled = true"} {
		if !strings.Contains(s, want) {
			t.Fatalf("file missing %q:\n%s", want, s)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails** — `go test ./internal/config/ -run TestVersions -v` → FAIL (no `Versions` field / undefined writers). `TestSettingDocsCoverAllFields` will also fail once the struct exists — that is the coverage gate working.

- [ ] **Step 3: Implement** — in `config.go`:

```go
// VersionsConfig configures branch-version snapshots (operations history).
// TOML keys snake_case under [versions]. Disabled is inverted on purpose: the
// zero-is-unset overlay could never turn a default-on plain bool off from the
// repo layer (the DisableRemoteTagsAuto precedent). MaxAgeDays uses
// nonzero-is-set so -1 (keep forever) overlays; 0 means unset (→ default 90).
type VersionsConfig struct {
	Disabled   bool `toml:"disabled"`
	MaxAgeDays int  `toml:"max_age_days"`
}
```

Add `Versions VersionsConfig \`toml:"versions"\`` to `Config`; add `Versions: VersionsConfig{MaxAgeDays: 90},` to `Defaults()`; add `overlayVersions(&cfg.Versions, layer.Versions)` inside the `Load` layer loop; add:

```go
// overlayVersions copies each set field of src onto dst. Disabled: true wins.
// MaxAgeDays: any nonzero value (including -1 = forever) overlays.
func overlayVersions(dst *VersionsConfig, src VersionsConfig) {
	if src.Disabled {
		dst.Disabled = true
	}
	if src.MaxAgeDays != 0 {
		dst.MaxAgeDays = src.MaxAgeDays
	}
}
```

In `write.go` (next to `SetRefreshInterval`):

```go
// SetVersionsDisabled persists `[versions] disabled` to the given config file
// (the repo .gg.toml), backing the Settings "Operations history" toggle.
func SetVersionsDisabled(path string, disabled bool) error {
	return setScalarLine(path, "versions", "disabled", strconv.FormatBool(disabled))
}

// SetVersionsMaxAgeDays persists `[versions] max_age_days` (-1 = keep forever)
// to the given config file, backing the Settings "Operations history" editor.
func SetVersionsMaxAgeDays(path string, days int) error {
	return setScalarLine(path, "versions", "max_age_days", strconv.Itoa(days))
}
```

In `template.go`: append to `settingDocs`:

```go
{"versions", "disabled", false, "disable branch-version snapshots before merges/rebases (default: on)"},
{"versions", "max_age_days", 90, "prune branch versions older than this many days; -1 = keep forever"},
```

and add `"versions"` to the hardcoded section slice at `template.go:96` (`[]string{"worktree", "ui", "debug", "refresh", "versions", "tools"}`).

- [ ] **Step 4: Run** — `go test ./internal/config/ -v` → PASS (including `TestSettingDocsCoverAllFields`).
- [ ] **Step 5: Commit** — `git add internal/config && git commit -m "feat(config): [versions] section, overlay, writers, settingDocs"` (+ trailers).

---

### Task 2: git verbs `UpdateRef`/`DeleteRef`/`ForEachRef` + version-ref naming

**Files:**
- Create: `internal/git/refs.go`, `internal/git/versionref.go`
- Modify: `internal/model/model.go` (add `RefInfo`), `internal/engine/gitops.go` (3 interface lines)
- Test: `internal/git/refs_test.go`, `internal/git/versionref_test.go`

**Interfaces:**
- Produces: `(*git.Repo) UpdateRef(ctx, ref, sha string) error`; `DeleteRef(ctx, ref string) error`; `ForEachRef(ctx, prefix string) ([]model.RefInfo, error)` (prefix WITHOUT trailing glob — a slash-boundary prefix like `refs/gg/versions`); `model.RefInfo{Ref, Hash, Subject string}`; `git.VersionRefPrefix = "refs/gg/versions/"`; `git.VersionRef(branch, opToken string, unix int64) string`; `git.ParseVersionRef(ref string) (branch, opToken string, unix int64, ok bool)`.

- [ ] **Step 1: Write failing tests** — `internal/git/versionref_test.go` (pure):

```go
package git

import "testing"

func TestVersionRefRoundTrip(t *testing.T) {
	ref := VersionRef("feat/x/y", "delete-branch", 1753100000)
	if ref != "refs/gg/versions/feat/x/y/1753100000-delete-branch" {
		t.Fatalf("ref = %q", ref)
	}
	b, op, ts, ok := ParseVersionRef(ref)
	if !ok || b != "feat/x/y" || op != "delete-branch" || ts != 1753100000 {
		t.Fatalf("parse = %q %q %d %v", b, op, ts, ok)
	}
}

func TestParseVersionRefRejects(t *testing.T) {
	for _, bad := range []string{
		"refs/heads/main",
		"refs/gg/versions/",
		"refs/gg/versions/main",          // no <ts>-<op> segment
		"refs/gg/versions/main/x-rebase", // non-numeric ts
	} {
		if _, _, _, ok := ParseVersionRef(bad); ok {
			t.Fatalf("ParseVersionRef(%q) unexpectedly ok", bad)
		}
	}
}
```

`internal/git/refs_test.go` (real git; mirror the package's existing `newTestRepo`-style helper in `repo_test.go` — reuse it if exported within the package, else copy its init pattern):

```go
func TestRefVerbs(t *testing.T) {
	dir, r := newTestRepo(t) // the package's existing real-git helper
	head, err := r.RevParse(context.Background(), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ref := "refs/gg/versions/main/1753100000-merge"
	if err := r.UpdateRef(ctx, ref, head); err != nil {
		t.Fatal(err)
	}
	infos, err := r.ForEachRef(ctx, "refs/gg/versions")
	if err != nil || len(infos) != 1 {
		t.Fatalf("ForEachRef = %v, %v; want 1 row", infos, err)
	}
	if infos[0].Ref != ref || infos[0].Hash != head || infos[0].Subject == "" {
		t.Fatalf("row = %+v", infos[0])
	}
	if err := r.DeleteRef(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if infos, _ = r.ForEachRef(ctx, "refs/gg/versions"); len(infos) != 0 {
		t.Fatalf("after delete: %v", infos)
	}
	_ = dir
}
```

(Adapt the helper call to the actual name/signature in `internal/git/repo_test.go`; if it returns only `*Repo`, drop `dir`.)

- [ ] **Step 2: Run to verify fail** — `go test ./internal/git/ -run 'TestVersionRef|TestRefVerbs' -v` → FAIL (undefined).
- [ ] **Step 3: Implement** — `internal/git/refs.go`:

```go
package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
	"github.com/homeend/gigagit/internal/model"
)

// UpdateRef points ref at sha (git update-ref), creating it if missing.
func (r *Repo) UpdateRef(ctx context.Context, ref, sha string) error {
	_, err := r.Runner.Run(ctx, "git update-ref", gitcmd.New("update-ref").Arg(ref, sha).ToArgv())
	return err
}

// DeleteRef removes ref (git update-ref -d).
func (r *Repo) DeleteRef(ctx context.Context, ref string) error {
	_, err := r.Runner.Run(ctx, "git update-ref", gitcmd.New("update-ref").Arg("-d", ref).ToArgv())
	return err
}

// ForEachRef lists refs under a slash-boundary prefix (no glob), one
// invocation, with target sha and commit subject. NUL separators survive any
// subject content (the Branches verb precedent).
func (r *Repo) ForEachRef(ctx context.Context, prefix string) ([]model.RefInfo, error) {
	const format = "%(refname)%00%(objectname)%00%(subject)"
	argv := gitcmd.New("for-each-ref").Arg("--format="+format, prefix).ToArgv()
	res, err := r.Runner.Run(ctx, "git for-each-ref (gg)", argv)
	if err != nil {
		return nil, err
	}
	var out []model.RefInfo
	for _, ln := range strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n") {
		ref, rest, ok := strings.Cut(ln, "\x00")
		if !ok || ref == "" {
			continue
		}
		hash, subject, _ := strings.Cut(rest, "\x00")
		out = append(out, model.RefInfo{Ref: ref, Hash: hash, Subject: subject})
	}
	return out, nil
}
```

(Use `%00` NUL separators like `Branches` does — simplify: build the format string directly as `"%(refname)%00%(objectname)%00%(subject)"`, drop the const/ReplaceAll.)

`internal/git/versionref.go`:

```go
package git

import (
	"strconv"
	"strings"
)

// VersionRefPrefix is the namespace of branch-version snapshot refs:
// refs/gg/versions/<branch>/<unix-ts>-<op>. Outside refs/heads|tags|remotes,
// so never pushed/fetched; shared by all worktrees; pins objects against gc.
const VersionRefPrefix = "refs/gg/versions/"

// VersionRef builds the snapshot ref name for branch at unix time ts caused
// by opToken (a protocol value like "rebase"). Collisions are resolved by the
// caller bumping ts.
func VersionRef(branch, opToken string, unix int64) string {
	return VersionRefPrefix + branch + "/" + strconv.FormatInt(unix, 10) + "-" + opToken
}

// ParseVersionRef splits a snapshot ref back into branch, op token, and
// timestamp. Parsing is from the END: the last path segment is always
// <ts>-<op>; everything between the prefix and it is the branch name.
func ParseVersionRef(ref string) (branch, opToken string, unix int64, ok bool) {
	rest, found := strings.CutPrefix(ref, VersionRefPrefix)
	if !found {
		return "", "", 0, false
	}
	i := strings.LastIndex(rest, "/")
	if i <= 0 || i == len(rest)-1 {
		return "", "", 0, false
	}
	branch, seg := rest[:i], rest[i+1:]
	tsStr, op, found := strings.Cut(seg, "-")
	if !found || op == "" {
		return "", "", 0, false
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return "", "", 0, false
	}
	return branch, op, ts, true
}
```

`internal/model/model.go` (next to `Branch`):

```go
// RefInfo is one row from a generic `git for-each-ref` read.
type RefInfo struct {
	Ref     string // full ref name
	Hash    string // full object id
	Subject string // commit subject
}
```

`internal/engine/gitops.go` — add to the interface (near `CreateBranch`):

```go
UpdateRef(ctx context.Context, ref, sha string) error
DeleteRef(ctx context.Context, ref string) error
ForEachRef(ctx context.Context, prefix string) ([]model.RefInfo, error)
```

- [ ] **Step 4: Run** — `go test ./internal/git/ ./internal/engine/ -run 'TestVersionRef|TestRefVerbs' -v` → PASS; `go build ./...` → OK (GitOps assertion at gitops.go:133 compiles).
- [ ] **Step 5: Commit** — `feat(git): UpdateRef/DeleteRef/ForEachRef verbs + version-ref naming`.

---

### Task 3: engine `VersionsPolicy` + `snapshotBranchTip` helper (+ prune)

**Files:**
- Create: `internal/engine/snapshot_version.go`
- Modify: `internal/engine/operation.go` (OpDeps field)
- Test: `internal/engine/snapshot_version_test.go`

**Interfaces:**
- Consumes: Task 2 verbs + naming.
- Produces: `engine.VersionsPolicy{Enabled bool; MaxAgeDays int}` (zero value = disabled; `MaxAgeDays <= 0` with Enabled = never prune); `OpDeps.Versions VersionsPolicy`; `func snapshotBranchTip(ctx context.Context, deps OpDeps, branch, opToken string)` — best-effort, no return value.

- [ ] **Step 1: Failing test** — `internal/engine/snapshot_version_test.go` (uses the package's existing `newRepo(t)` from `ops_basic_test.go`):

```go
package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/git"
)

func versionRefs(t *testing.T, r *git.Repo) []string {
	t.Helper()
	infos, err := r.ForEachRef(context.Background(), "refs/gg/versions")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, i := range infos {
		out = append(out, i.Ref)
	}
	return out
}

func TestSnapshotBranchTipRecordsAndSkips(t *testing.T) {
	_, repo := newRepo(t)
	ctx := context.Background()
	deps := OpDeps{Repo: repo} // zero policy: disabled

	snapshotBranchTip(ctx, deps, "main", "rebase")
	if got := versionRefs(t, repo); len(got) != 0 {
		t.Fatalf("disabled policy wrote %v", got)
	}

	deps.Versions = VersionsPolicy{Enabled: true, MaxAgeDays: 90}
	snapshotBranchTip(ctx, deps, "", "rebase") // detached HEAD: no branch
	if got := versionRefs(t, repo); len(got) != 0 {
		t.Fatalf("empty branch wrote %v", got)
	}

	snapshotBranchTip(ctx, deps, "main", "rebase")
	got := versionRefs(t, repo)
	if len(got) != 1 || !strings.Contains(got[0], "/main/") || !strings.HasSuffix(got[0], "-rebase") {
		t.Fatalf("refs = %v", got)
	}
	head, _ := repo.RevParse(ctx, "HEAD")
	infos, _ := repo.ForEachRef(ctx, "refs/gg/versions")
	if infos[0].Hash != head {
		t.Fatalf("snapshot points at %s, want %s", infos[0].Hash, head)
	}

	// Second snapshot in the same second must not collide (ts bumps).
	snapshotBranchTip(ctx, deps, "main", "rebase")
	if got := versionRefs(t, repo); len(got) != 2 {
		t.Fatalf("collision handling: %v", got)
	}
}

func TestSnapshotBranchTipPrunes(t *testing.T) {
	_, repo := newRepo(t)
	ctx := context.Background()
	head, _ := repo.RevParse(ctx, "HEAD")
	old := time.Now().AddDate(0, 0, -120).Unix()
	oldRef := git.VersionRef("main", "merge", old)
	if err := repo.UpdateRef(ctx, oldRef, head); err != nil {
		t.Fatal(err)
	}

	// MaxAgeDays -1 (forever): the old ref survives a new snapshot.
	snapshotBranchTip(ctx, OpDeps{Repo: repo, Versions: VersionsPolicy{Enabled: true, MaxAgeDays: -1}}, "main", "rebase")
	if got := versionRefs(t, repo); len(got) != 2 {
		t.Fatalf("forever policy pruned: %v", got)
	}

	// 90 days: the 120-day-old ref is pruned on the next write.
	snapshotBranchTip(ctx, OpDeps{Repo: repo, Versions: VersionsPolicy{Enabled: true, MaxAgeDays: 90}}, "main", "rebase")
	for _, ref := range versionRefs(t, repo) {
		if ref == oldRef {
			t.Fatalf("expired ref survived: %v", versionRefs(t, repo))
		}
	}
}
```

- [ ] **Step 2: Run to verify fail** — `go test ./internal/engine/ -run TestSnapshotBranchTip -v` → FAIL.
- [ ] **Step 3: Implement** — `internal/engine/snapshot_version.go`:

```go
package engine

import (
	"context"
	"time"

	"github.com/homeend/gigagit/internal/git"
)

// VersionsPolicy governs pre-operation branch-version snapshots. The zero
// value DISABLES snapshots so operations run with bare OpDeps (tests, direct
// callers) stay byte-identical; domain.Execute injects the real policy
// (default: enabled, 90 days). MaxAgeDays <= 0 means never prune.
type VersionsPolicy struct {
	Enabled    bool
	MaxAgeDays int
}

// snapshotNow is a test seam for the snapshot timestamp.
var snapshotNow = time.Now

// snapshotBranchTip records branch's current tip as a hidden version ref
// (refs/gg/versions/<branch>/<ts>-<opToken>) and prunes expired versions of
// that branch. BEST-EFFORT by contract: any failure emits a progress note and
// returns — recording must never block or fail the real operation.
func snapshotBranchTip(ctx context.Context, deps OpDeps, branch, opToken string) {
	if !deps.Versions.Enabled || branch == "" {
		return
	}
	sha, err := deps.Repo.RevParse(ctx, "refs/heads/"+branch)
	if err != nil || sha == "" {
		return // unborn or unknown branch: nothing to record
	}
	ts := snapshotNow().Unix()
	ref := git.VersionRef(branch, opToken, ts)
	// Same-second, same-op collision: bump the timestamp until free.
	existing := map[string]bool{}
	infos, err := deps.Repo.ForEachRef(ctx, "refs/gg/versions/"+branch)
	if err == nil {
		for _, i := range infos {
			existing[i.Ref] = true
		}
	}
	for existing[ref] {
		ts++
		ref = git.VersionRef(branch, opToken, ts)
	}
	deps.emit(ctx, Progress{Step: "recording branch version", Detail: branch})
	if err := deps.Repo.UpdateRef(ctx, ref, sha); err != nil {
		deps.emit(ctx, Progressf("recording branch version", "skipped: %s", err.Error()))
		return
	}
	pruneBranchVersions(ctx, deps, branch, infos)
}

// pruneBranchVersions deletes this branch's version refs older than the
// policy age. infos is the pre-snapshot listing (the fresh ref is never
// expired). Best-effort: delete errors are ignored.
func pruneBranchVersions(ctx context.Context, deps OpDeps, branch string, infos []model.RefInfo) {
	if deps.Versions.MaxAgeDays <= 0 {
		return
	}
	cutoff := snapshotNow().AddDate(0, 0, -deps.Versions.MaxAgeDays).Unix()
	for _, info := range infos {
		b, _, ts, ok := git.ParseVersionRef(info.Ref)
		if !ok || b != branch || ts >= cutoff {
			continue
		}
		_ = deps.Repo.DeleteRef(ctx, info.Ref)
	}
}
```

(import `"github.com/homeend/gigagit/internal/model"`). In `operation.go`, add to `OpDeps`:

```go
Versions VersionsPolicy // branch-version snapshots; zero value = disabled
```

- [ ] **Step 4: Run** — `go test ./internal/engine/ -run TestSnapshotBranchTip -v` → PASS.
- [ ] **Step 5: Bundle keys** — the new engine prose needs all four bundles (`internal/i18n/lang/*.toml`, append under `[strings]`, grep first):

| key | ja | ko | zh | ru |
|---|---|---|---|---|
| `recording branch version` | `ブランチバージョンを記録中` | `브랜치 버전 기록 중` | `正在记录分支版本` | `запись версии ветки` |
| `skipped: %s` | `スキップ: %s` | `건너뜀: %s` | `已跳过：%s` | `пропущено: %s` |

Run `go test ./internal/tui/ -run 'TestEngineProse|Test.*i18n' -v` → PASS.
- [ ] **Step 6: Commit** — `feat(engine): VersionsPolicy + best-effort snapshotBranchTip with pruning`.

---

### Task 4: wire snapshots into the trigger ops

**Files:**
- Modify: `internal/engine/smart_merge.go` (~:53, after validation), `smart_rebase.go` (~:55), `interactive_rebase.go` (after the validation switch, ~:40), `ops_basic.go` (Commit.Run, amend only), `undo.go`, `reset.go` (before `deps.Repo.Reset`), `delete_branch.go` (after the confirm decision), `smart_pull.go` (`pullCurrent`'s rebase/merge/reset case arms)
- Test: `internal/engine/snapshot_triggers_test.go`

**Interfaces:**
- Consumes: `snapshotBranchTip(ctx, deps, branch, opToken)` from Task 3.
- Produces: nothing new — behavioral only. Op tokens per site: SmartMerge→`"merge"` (branch = `target`), SmartRebase→`"rebase"` (branch = `branch`), InteractiveRebase→`"interactive-rebase"` (branch = `op.Branch`), Commit amend→`"amend"` (branch = current), UndoLastCommit→`"undo-commit"` (current), Reset→`"reset"` (current), DeleteBranch→`"delete-branch"` (op.Name), SmartPull→`"pull"` (branch param of pullCurrent).

- [ ] **Step 1: Failing test** — `internal/engine/snapshot_triggers_test.go`. One representative full test plus the matrix; enabled deps helper:

```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func enabledDeps(repo GitOps) OpDeps {
	return OpDeps{Repo: repo, Versions: VersionsPolicy{Enabled: true, MaxAgeDays: 90}}
}

func TestAmendSnapshotsAndPlainCommitDoesNot(t *testing.T) {
	dir, repo := newRepo(t)
	ctx := context.Background()

	os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644)
	if _, err := (Commit{Message: "second", All: true}).Run(ctx, enabledDeps(repo)); err != nil {
		t.Fatal(err)
	}
	if refs := versionRefs(t, repo); len(refs) != 0 {
		t.Fatalf("plain commit snapshotted: %v", refs)
	}

	if _, err := (Commit{Message: "second (amended)", Amend: true}).Run(ctx, enabledDeps(repo)); err != nil {
		t.Fatal(err)
	}
	refs := versionRefs(t, repo)
	if len(refs) != 1 || !strings.HasSuffix(refs[0], "-amend") {
		t.Fatalf("amend refs = %v", refs)
	}
}
```

Add sibling tests in the same file (same shape — set up the pre-state with `newRepo` + direct git verb calls on `repo`, run the op with `enabledDeps` and a `staticTestDecider` answering its forks, then assert `versionRefs` contains exactly the expected `-<op>` suffix and that the snapshot's `Hash` equals the pre-op tip):

- `TestSmartMergeSnapshotsTarget`: create branch `feat` off main (`repo.CreateBranch`), commit on main via `Commit`, commit on feat (switch, write, commit, switch back — use `repo.Switch`/`Commit` op), then run `SmartMerge{Source: "feat"}` with a decider answering any fork with its first option; expect one `/main/…-merge` ref pointing at main's pre-merge tip.
- `TestSmartRebaseSnapshotsBranch`: diverge feat from main, run `SmartRebase{Branch: "feat", Onto: "main"}` (from a state where feat is checked out or resolvable per the op's ladder — check out feat first); expect `/feat/…-rebase` at feat's old tip.
- `TestUndoLastCommitSnapshots`: commit twice, run `UndoLastCommit{}`; expect `/main/…-undo-commit` at the pre-undo tip.
- `TestResetSnapshots`: commit twice, `Reset{Commit: "HEAD~1", Mode: "hard"}`; expect `/main/…-reset`.
- `TestDeleteBranchSnapshots`: create `feat` with a commit, run `DeleteBranch{Name: "feat"}` with a decider answering `"delete"` then `"force-delete"`; expect `/feat/…-delete-branch` pointing at feat's old tip (recoverable after deletion).
- `TestDeleteBranchCancelledDoesNotSnapshot`: decider answers `"abort"`; expect zero refs.

For the decider use the package's existing test decider if one exists (grep `Decide(` in `internal/engine/*_test.go`); otherwise define once in this file:

```go
type staticTestDecider struct{ answers map[string]string }

func (d staticTestDecider) Decide(_ context.Context, req DecisionRequest) (DecisionResponse, error) {
	if a, ok := d.answers[req.ID]; ok {
		return DecisionResponse{Option: a}, nil
	}
	return DecisionResponse{Option: req.Options[0]}, nil
}
```

- [ ] **Step 2: Run to verify fail** — `go test ./internal/engine/ -run 'Snapshot|Snapshots' -v` → FAIL (no refs recorded).
- [ ] **Step 3: Implement** — one call per site, always AFTER validation/guards and any confirm decision, immediately BEFORE the first mutation:

  - `smart_merge.go`: after the branch/commit validation block, before the worktree-ladder mutations: `snapshotBranchTip(ctx, deps, target, "merge")`.
  - `smart_rebase.go`: same position: `snapshotBranchTip(ctx, deps, branch, "rebase")`.
  - `interactive_rebase.go`: after the validation `switch`: `snapshotBranchTip(ctx, deps, op.Branch, "interactive-rebase")`.
  - `ops_basic.go` `Commit.Run`: at the top, before `deps.Repo.Commit`:
    ```go
    if op.Amend {
        if cur, cerr := deps.Repo.CurrentBranch(ctx); cerr == nil {
            snapshotBranchTip(ctx, deps, cur, "amend")
        }
    }
    ```
  - `undo.go`: after the reflog-subject guard, before `ResetSoft`: same `CurrentBranch` + `snapshotBranchTip(ctx, deps, cur, "undo-commit")` pattern.
  - `reset.go`: right before `deps.Repo.Reset(ctx, mode, op.Commit)` (covers both the interactive and preset-mode paths): `CurrentBranch` + token `"reset"`.
  - `delete_branch.go`: after the `confirm.Option != "delete"` early return, before the `Progress{Step: "deleting branch"}` emit: `snapshotBranchTip(ctx, deps, op.Name, "delete-branch")`.
  - `smart_pull.go` `pullCurrent`: at the top of each of the three mutating case arms (`"rebase"`, `"merge"`, `"reset"`), before their `Pull`/`Reset` call: `snapshotBranchTip(ctx, deps, branch, "pull")`. The ff-only fast path (already returned above the decide) and the abort arm do NOT snapshot. `checkoutPull` (background pull of another worktree's branch) is explicitly out of scope for v1 — leave a one-line comment there: `// no version snapshot: background checkout-pull is additive in the common case; revisit with workspace groups.`

- [ ] **Step 4: Run** — `go test ./internal/engine/ -v` → PASS (all existing op tests still green — they run with zero-value policy and must be byte-identical).
- [ ] **Step 5: Commit** — `feat(engine): snapshot branch tip before history-changing ops + merge`.

---

### Task 5: engine `RestoreBranchVersion` + `DeleteBranchVersion` ops

**Files:**
- Create: `internal/engine/restore_branch_version.go`, `internal/engine/delete_branch_version.go`
- Test: `internal/engine/restore_branch_version_test.go`

**Interfaces:**
- Consumes: Task 2 verbs/naming, Task 3 helper.
- Produces: `engine.RestoreBranchVersion{Branch, Ref string}` (default TreeWrite; dirty-tree fork id `"restore-dirty"`, options `["proceed","cancel"]` — existing vocab); `engine.DeleteBranchVersion{Ref string}` (`LockMode()` = `repogate.RefWrite`; refuses refs outside the prefix).

- [ ] **Step 1: Failing tests** — `restore_branch_version_test.go`:

```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
)

func TestRestoreCurrentBranchResetsAndSnapshotsFirst(t *testing.T) {
	dir, repo := newRepo(t)
	ctx := context.Background()
	oldTip, _ := repo.RevParse(ctx, "HEAD")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644)
	if _, err := (Commit{Message: "second", All: true}).Run(ctx, OpDeps{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	ref := git.VersionRef("main", "rebase", 1753100000)
	if err := repo.UpdateRef(ctx, ref, oldTip); err != nil {
		t.Fatal(err)
	}
	newTip, _ := repo.RevParse(ctx, "HEAD")

	res, err := RestoreBranchVersion{Branch: "main", Ref: ref}.Run(ctx, enabledDeps(repo))
	if err != nil || !res.Changed {
		t.Fatalf("restore: %v %+v", err, res)
	}
	if head, _ := repo.RevParse(ctx, "HEAD"); head != oldTip {
		t.Fatalf("HEAD = %s, want restored %s", head, oldTip)
	}
	// Restore is itself undoable: a fresh "-restore" snapshot points at newTip.
	var sawRestore bool
	infos, _ := repo.ForEachRef(ctx, "refs/gg/versions")
	for _, i := range infos {
		if strings.HasSuffix(i.Ref, "-restore") && i.Hash == newTip {
			sawRestore = true
		}
	}
	if !sawRestore {
		t.Fatalf("no restore snapshot of the pre-restore tip: %+v", infos)
	}
}

func TestRestoreDirtyTreeForksAndCancelKeepsState(t *testing.T) {
	dir, repo := newRepo(t)
	ctx := context.Background()
	oldTip, _ := repo.RevParse(ctx, "HEAD")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644)
	if _, err := (Commit{Message: "second", All: true}).Run(ctx, OpDeps{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644) // uncommitted
	ref := git.VersionRef("main", "rebase", 1753100000)
	repo.UpdateRef(ctx, ref, oldTip)

	deps := enabledDeps(repo)
	deps.Decider = staticTestDecider{answers: map[string]string{"restore-dirty": "cancel"}}
	res, err := RestoreBranchVersion{Branch: "main", Ref: ref}.Run(ctx, deps)
	if err != nil || res.Changed {
		t.Fatalf("cancelled restore: %v %+v (want Changed=false, nil err)", err, res)
	}
}

func TestRestoreOtherBranchMovesRefAndRecreatesDeleted(t *testing.T) {
	_, repo := newRepo(t)
	ctx := context.Background()
	tip, _ := repo.RevParse(ctx, "HEAD")
	// A version of a branch that does not exist (deleted-branch recovery).
	ref := git.VersionRef("feat/gone", "delete-branch", 1753100000)
	repo.UpdateRef(ctx, ref, tip)

	res, err := RestoreBranchVersion{Branch: "feat/gone", Ref: ref}.Run(ctx, enabledDeps(repo))
	if err != nil || !res.Changed {
		t.Fatalf("restore deleted: %v %+v", err, res)
	}
	if sha, _ := repo.RevParse(ctx, "refs/heads/feat/gone"); sha != tip {
		t.Fatalf("feat/gone = %s, want %s", sha, tip)
	}
}

func TestRestoreRefusesBranchCheckedOutElsewhere(t *testing.T) {
	// Set up a linked worktree on branch "wt" and expect a refusal error
	// naming the worktree path (mirror create_worktree_test.go's setup).
}

func TestDeleteBranchVersion(t *testing.T) {
	_, repo := newRepo(t)
	ctx := context.Background()
	tip, _ := repo.RevParse(ctx, "HEAD")
	ref := git.VersionRef("main", "merge", 1753100000)
	repo.UpdateRef(ctx, ref, tip)

	if _, err := (DeleteBranchVersion{Ref: "refs/heads/main"}).Run(ctx, OpDeps{Repo: repo}); err == nil {
		t.Fatal("deleting outside the versions namespace must be refused")
	}
	res, err := DeleteBranchVersion{Ref: ref}.Run(ctx, OpDeps{Repo: repo})
	if err != nil || !res.Changed {
		t.Fatalf("delete: %v %+v", err, res)
	}
	if refs := versionRefs(t, repo); len(refs) != 0 {
		t.Fatalf("ref survived: %v", refs)
	}
}
```

(Fill `TestRestoreRefusesBranchCheckedOutElsewhere` from the worktree-setup pattern used in `create_worktree_test.go` — add a real `git worktree add` via the raw runner, then assert the error contains the worktree path.)

- [ ] **Step 2: Run to verify fail** — `go test ./internal/engine/ -run 'TestRestore|TestDeleteBranchVersion' -v` → FAIL.
- [ ] **Step 3: Implement** — `restore_branch_version.go`:

```go
package engine

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/git"
)

// RestoreBranchVersion moves Branch back to a recorded version ref. Current
// branch: a hard reset (dirty tree forks a proceed/cancel decision). A branch
// checked out in another worktree is refused. Any other branch — including a
// DELETED one — is moved (or recreated) via update-ref. The pre-restore tip
// is snapshotted first, so restore is itself undoable. Default TreeWrite lock
// (the current-branch lane touches the working tree).
type RestoreBranchVersion struct {
	Branch string // required
	Ref    string // required: a refs/gg/versions/... ref of Branch
}

var _ Operation = RestoreBranchVersion{}

func (op RestoreBranchVersion) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Branch == "" || op.Ref == "" {
		return Result{}, fmt.Errorf("restore version: Branch and Ref are required")
	}
	refBranch, _, _, ok := git.ParseVersionRef(op.Ref)
	if !ok || refBranch != op.Branch {
		return Result{}, fmt.Errorf("restore version: %s is not a version of branch %s", op.Ref, op.Branch)
	}
	sha, err := deps.Repo.RevParse(ctx, op.Ref)
	if err != nil {
		return Result{}, fmt.Errorf("restore version: %w", err)
	}
	cur, err := deps.Repo.CurrentBranch(ctx)
	if err != nil {
		return Result{}, err
	}

	short := sha
	if len(short) > 8 {
		short = short[:8]
	}
	if op.Branch == cur {
		dirty, derr := deps.Repo.IsDirty(ctx)
		if derr != nil {
			return Result{}, derr
		}
		if dirty {
			resp, perr := deps.decide(ctx, PromptReq("restore-dirty",
				"the working tree has uncommitted changes; restoring %s discards them",
				[]string{"proceed", "cancel"}, op.Branch))
			if perr != nil {
				return Result{}, perr
			}
			if resp.Option != "proceed" {
				return Result{Changed: false}.WithSummary("cancelled"), nil
			}
		}
		snapshotBranchTip(ctx, deps, op.Branch, "restore")
		deps.emit(ctx, Progressf("restoring branch version", "%s → %s", op.Branch, short))
		if err := deps.Repo.Reset(ctx, "hard", sha); err != nil {
			return Result{}, fmt.Errorf("restore version: %w", err)
		}
	} else {
		wt, werr := deps.Repo.WorktreeForBranch(ctx, op.Branch)
		if werr != nil {
			return Result{}, werr
		}
		if wt != nil {
			return Result{}, fmt.Errorf("restore version: %s is checked out in worktree %s — restore it there", op.Branch, wt.Path)
		}
		snapshotBranchTip(ctx, deps, op.Branch, "restore")
		deps.emit(ctx, Progressf("restoring branch version", "%s → %s", op.Branch, short))
		if err := deps.Repo.UpdateRef(ctx, "refs/heads/"+op.Branch, sha); err != nil {
			return Result{}, fmt.Errorf("restore version: %w", err)
		}
	}

	res := Result{Changed: true}.WithSummary("restored %s to %s", op.Branch, short)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```

`delete_branch_version.go`:

```go
package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/repogate"
)

// DeleteBranchVersion removes one recorded branch-version ref. Refuses any
// ref outside the versions namespace so a frontend bug can never delete a
// real branch or tag through this op.
type DeleteBranchVersion struct {
	Ref string // required, must start with refs/gg/versions/
}

var _ Operation = DeleteBranchVersion{}

// LockMode: moves (removes) a ref only; never index/worktree/HEAD.
func (op DeleteBranchVersion) LockMode() repogate.Mode { return repogate.RefWrite }

func (op DeleteBranchVersion) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if !strings.HasPrefix(op.Ref, git.VersionRefPrefix) {
		return Result{}, fmt.Errorf("delete version: %s is not a branch-version ref", op.Ref)
	}
	deps.emit(ctx, Progress{Step: "deleting branch version", Detail: op.Ref})
	if err := deps.Repo.DeleteRef(ctx, op.Ref); err != nil {
		return Result{}, fmt.Errorf("delete version: %w", err)
	}
	res := Result{Changed: true}.WithSummary("deleted branch version %s", strings.TrimPrefix(op.Ref, git.VersionRefPrefix))
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```

- [ ] **Step 4: Run** — `go test ./internal/engine/ -run 'TestRestore|TestDeleteBranchVersion' -v` → PASS.
- [ ] **Step 5: Bundle keys** (all four bundles; grep first — `cancelled`, `proceed`, `cancel` already exist):

| key | ja | ko | zh | ru |
|---|---|---|---|---|
| `the working tree has uncommitted changes; restoring %s discards them` | `作業ツリーに未コミットの変更があります。%s を復元すると破棄されます` | `작업 트리에 커밋되지 않은 변경이 있습니다. %s 을(를) 복원하면 삭제됩니다` | `工作区有未提交的更改；恢复 %s 将丢弃它们` | `в рабочем дереве есть незафиксированные изменения; восстановление %s удалит их` |
| `restoring branch version` | `ブランチバージョンを復元中` | `브랜치 버전 복원 중` | `正在恢复分支版本` | `восстановление версии ветки` |
| `%s → %s` (grep first — likely exists) | `%s → %s` | `%s → %s` | `%s → %s` | `%s → %s` |
| `restored %s to %s` | `%s を %s に復元しました` | `%s 을(를) %s (으)로 복원했습니다` | `已将 %s 恢复到 %s` | `%s восстановлена на %s` |
| `deleting branch version` | `ブランチバージョンを削除中` | `브랜치 버전 삭제 중` | `正在删除分支版本` | `удаление версии ветки` |
| `deleted branch version %s` | `ブランチバージョン %s を削除しました` | `브랜치 버전 %s 을(를) 삭제했습니다` | `已删除分支版本 %s` | `версия ветки %s удалена` |

Run `go test ./internal/tui/ -run 'EngineProse' -v` → PASS.
- [ ] **Step 6: Commit** — `feat(engine): RestoreBranchVersion + DeleteBranchVersion ops`.

---

### Task 6: domain — model types, queries, policy plumbing

**Files:**
- Modify: `internal/model/model.go`, `internal/domain/service.go` (Service field + setter + Execute OpDeps literal), `internal/domain/query.go` (two queries)
- Test: `internal/domain/versions_test.go`

**Interfaces:**
- Consumes: Tasks 2–3.
- Produces: `model.BranchVersion{Ref, Hash, Subject, Op string, Unix int64}`; `model.VersionedBranch{Branch string, Deleted bool, Count int, LatestUnix int64}`; `(*domain.Service) SetVersionsPolicy(p engine.VersionsPolicy) *Service`; `BranchVersions(ctx, branch string) ([]model.BranchVersion, error)` (newest first); `AllVersionBranches(ctx) ([]model.VersionedBranch, error)` (sorted by LatestUnix desc, `Deleted` = branch absent from `repo.Branches`). `Execute` passes `Versions: s.currentVersionsPolicy()` in the OpDeps literal; default policy = `{Enabled: true, MaxAgeDays: 90}`.

- [ ] **Step 1: Failing test** — `internal/domain/versions_test.go` (mirror the package's existing real-git service test setup — grep `func newService` / how `export_test.go` builds a Service; use `domain.New(git.Repo)` equivalent via the package's helper):

```go
func TestBranchVersionsListsNewestFirstAndFiltersBranch(t *testing.T) {
	// setup: repo with branches "main" and "feat/x"; write three version refs
	// via raw git update-ref: main@t1, main@t2, feat/x@t3.
	// assert: BranchVersions(ctx, "main") = 2 rows, Unix desc, Op parsed,
	// Subject non-empty, no feat/x row.
	// assert: BranchVersions(ctx, "feat/x") = 1 row.
	// Nested-name safety: versions of "feat/x" must NOT appear for "feat".
}

func TestAllVersionBranchesMarksDeleted(t *testing.T) {
	// setup: version refs for "main" and "gone" (no refs/heads/gone).
	// assert: two rows; row "gone" has Deleted=true, Count=1;
	// row "main" Deleted=false; order LatestUnix desc.
}

func TestExecuteInjectsVersionsPolicy(t *testing.T) {
	// run a cheap mutating op (engine.Stage{All:true} after dirtying a file)
	// through svc.Execute with default policy; then run an amend Commit and
	// assert a version ref appears (policy reached the op). Then
	// svc.SetVersionsPolicy(engine.VersionsPolicy{Enabled:false}) and assert a
	// second amend adds NO new ref.
}
```

Write these as real tests (full bodies) using the package's existing test-service constructor; the assertions above are the complete behavior contract.

- [ ] **Step 2: Run to verify fail** — `go test ./internal/domain/ -run 'TestBranchVersions|TestAllVersionBranches|TestExecuteInjects' -v` → FAIL.
- [ ] **Step 3: Implement** — `model.go`:

```go
// BranchVersion is one recorded pre-operation snapshot of a branch
// (refs/gg/versions/<branch>/<unix>-<op>).
type BranchVersion struct {
	Ref     string // full version ref
	Hash    string // snapshot tip (full sha)
	Subject string // tip commit subject
	Op      string // protocol op token: merge, rebase, restore, …
	Unix    int64  // when the snapshot was recorded
}

// VersionedBranch summarizes one branch's recorded versions.
type VersionedBranch struct {
	Branch     string
	Deleted    bool // branch no longer exists in refs/heads
	Count      int
	LatestUnix int64
}
```

`service.go` — field, setter, resolver (near `showEOLOnly`):

```go
// versionsPolicy stores the engine.VersionsPolicy injected into every
// Execute. nil (never set) resolves to the default: enabled, 90 days.
versionsPolicy atomic.Value
```

```go
// SetVersionsPolicy overrides the branch-version snapshot policy injected
// into operations (from [versions] config). Unset = enabled, 90 days.
func (s *Service) SetVersionsPolicy(p engine.VersionsPolicy) *Service {
	s.versionsPolicy.Store(p)
	return s
}

func (s *Service) currentVersionsPolicy() engine.VersionsPolicy {
	if v := s.versionsPolicy.Load(); v != nil {
		return v.(engine.VersionsPolicy)
	}
	return engine.VersionsPolicy{Enabled: true, MaxAgeDays: 90}
}
```

In `Execute`'s `engine.OpDeps{...}` literal add `Versions: s.currentVersionsPolicy(),`.

`query.go`:

```go
// BranchVersions lists a branch's recorded pre-operation snapshots, newest
// first, under a Read reservation.
func (s *Service) BranchVersions(ctx context.Context, branch string) ([]model.BranchVersion, error) {
	return query(ctx, s, "branch-versions:"+branch, func(ctx context.Context) ([]model.BranchVersion, error) {
		infos, err := s.repo.ForEachRef(ctx, strings.TrimSuffix(git.VersionRefPrefix, "/")+"/"+branch)
		if err != nil {
			return nil, err
		}
		var out []model.BranchVersion
		for _, info := range infos {
			b, op, ts, ok := git.ParseVersionRef(info.Ref)
			if !ok || b != branch { // prefix match may over-catch nested names
				continue
			}
			out = append(out, model.BranchVersion{Ref: info.Ref, Hash: info.Hash, Subject: info.Subject, Op: op, Unix: ts})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Unix > out[j].Unix })
		return out, nil
	})
}

// AllVersionBranches groups every recorded version by branch, marking
// branches that no longer exist (deleted-branch recovery entry point).
func (s *Service) AllVersionBranches(ctx context.Context) ([]model.VersionedBranch, error) {
	return query(ctx, s, "version-branches", func(ctx context.Context) ([]model.VersionedBranch, error) {
		infos, err := s.repo.ForEachRef(ctx, strings.TrimSuffix(git.VersionRefPrefix, "/"))
		if err != nil {
			return nil, err
		}
		byBranch := map[string]*model.VersionedBranch{}
		for _, info := range infos {
			b, _, ts, ok := git.ParseVersionRef(info.Ref)
			if !ok {
				continue
			}
			row := byBranch[b]
			if row == nil {
				row = &model.VersionedBranch{Branch: b}
				byBranch[b] = row
			}
			row.Count++
			if ts > row.LatestUnix {
				row.LatestUnix = ts
			}
		}
		if len(byBranch) == 0 {
			return nil, nil
		}
		branches, err := s.repo.Branches(ctx)
		if err != nil {
			return nil, err
		}
		exists := map[string]bool{}
		for _, b := range branches {
			exists[b.Name] = true
		}
		out := make([]model.VersionedBranch, 0, len(byBranch))
		for _, row := range byBranch {
			row.Deleted = !exists[row.Branch]
			out = append(out, *row)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].LatestUnix > out[j].LatestUnix })
		return out, nil
	})
}
```

- [ ] **Step 4: Run** — `go test ./internal/domain/ -v` → PASS.
- [ ] **Step 5: Commit** — `feat(domain): BranchVersions/AllVersionBranches queries + versions policy injection`.

---

### Task 7: commit-feed decoration exclusion

**Files:**
- Modify: `internal/git/log.go:56`
- Test: `internal/git/log_test.go` (or the file holding existing `LogScoped` tests)

- [ ] **Step 1: Failing test** — in the git package's log test file:

```go
func TestLogScopedExcludesVersionRefDecorations(t *testing.T) {
	dir, r := newTestRepo(t) // package's existing helper
	ctx := context.Background()
	head, _ := r.RevParse(ctx, "HEAD")
	if err := r.UpdateRef(ctx, "refs/gg/versions/main/1753100000-merge", head); err != nil {
		t.Fatal(err)
	}
	commits, err := r.LogScoped(ctx, 10, 0, LogScope{}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range commits {
		for _, ref := range c.Refs {
			if strings.Contains(ref, "gg/versions") {
				t.Fatalf("version ref leaked into decorations: %v", c.Refs)
			}
		}
	}
	_ = dir
}
```

(Adapt `LogScope{}` construction to the real signature in `log.go`; the assertion is on `model.Commit.Refs`.)

- [ ] **Step 2: Run to verify fail** — decoration appears → FAIL.
- [ ] **Step 3: Implement** — in `LogScoped`'s builder chain (log.go:56) add:

```go
Arg("--decorate-refs-exclude=refs/gg/*").
```

Also `rg -n '%D' internal/git internal/domain` — if any OTHER `%D` consumer exists (reflog, show), add the same flag there; today only `log.go` matches.

- [ ] **Step 4: Run** — `go test ./internal/git/ -run TestLogScoped -v` → PASS.
- [ ] **Step 5: Commit** — `fix(git): exclude refs/gg/* from commit-feed decorations`.

---

### Task 8: CLI `gg versions` (list + restore) + policy wiring + e2e + using-gg

**Files:**
- Create: `internal/cli/versions.go`, `internal/cli/versions_test.go`, `e2e/scenarios/s80_cli_versions.toml` (check `ls e2e/scenarios | tail` — use the next free number)
- Modify: `internal/cli/cli.go` (runOne case + `commands` map + policy load in `Run`), `internal/agentskill/using-gg.md` + `internal/agentskill/agentskill.go` (Version 52→53)

**Interfaces:**
- Consumes: Task 6 queries + Task 5 ops + Task 1 config.
- Produces: `gg versions [<branch>]` — one line per version: `<ts>-<op> <short-sha> <ISO-time> <subject>`; exit 0 (prints `(no versions)` when empty), 1 on error, 2 on usage. `gg versions restore [--discard] <branch> <id|latest>` — `--discard` maps decider policy `{"restore-dirty": "proceed"}`; `latest` picks the newest version. Routed through `runOne` (so `gg batch` drives it).

- [ ] **Step 1: Failing test** — `internal/cli/versions_test.go`, mirroring the package's existing CLI test harness (grep how `shelf_test.go` builds a repo + runs `runOne`/`Run`; it already uses raw `git update-ref` in setup):

```go
func TestCmdVersionsListAndRestore(t *testing.T) {
	// setup: test repo, two commits; git update-ref
	// refs/gg/versions/main/1753100000-merge = first-commit sha.
	// run: gg versions            → exit 0, stdout contains "1753100000-merge"
	//      and the first commit's subject.
	// run: gg versions restore main latest → exit 0
	// assert: HEAD == first-commit sha.
	// run: gg versions nosuch     → exit 0, "(no versions)".
	// run: gg versions restore main bogus-id → exit 1 (unknown version).
	// run: gg versions a b c      → exit 2 usage.
}
```

Write full bodies with the harness found. Cover `--discard` with a dirtied file: without the flag and empty stdin the decision fails loud (exit 1); with `--discard` restore succeeds.

- [ ] **Step 2: Run to verify fail** — `go test ./internal/cli/ -run TestCmdVersions -v` → FAIL.
- [ ] **Step 3: Implement** — `internal/cli/versions.go`:

```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

const versionsUsage = "usage: gg versions [<branch>] | gg versions restore [--discard] <branch> <id|latest>"

func cmdVersions(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "restore" {
		return cmdVersionsRestore(svc, args[1:], stdin, stdout, stderr)
	}
	fs := flag.NewFlagSet("versions", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, versionsUsage)
		return 2
	}
	ctx := context.Background()
	branch := ""
	if fs.NArg() == 1 {
		branch = fs.Arg(0)
	} else {
		cur, err := svc.CurrentBranch(ctx)
		if err != nil || cur == "" {
			fmt.Fprintln(stderr, "error: no current branch (detached HEAD) — name a branch")
			return 1
		}
		branch = cur
	}
	rows, err := svc.BranchVersions(ctx, branch)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if len(rows) == 0 {
		fmt.Fprintln(stdout, "(no versions)")
		return 0
	}
	for _, v := range rows {
		short := v.Hash
		if len(short) > 8 {
			short = short[:8]
		}
		id := fmt.Sprintf("%d-%s", v.Unix, v.Op)
		when := time.Unix(v.Unix, 0).Format("2006-01-02T15:04")
		fmt.Fprintf(stdout, "%s %s %s %s\n", id, short, when, v.Subject)
	}
	return 0
}

func cmdVersionsRestore(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("versions restore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	discard := fs.Bool("discard", false, "discard uncommitted changes if the restore needs a hard reset")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, versionsUsage)
		return 2
	}
	branch, id := fs.Arg(0), fs.Arg(1)
	ctx := context.Background()
	rows, err := svc.BranchVersions(ctx, branch)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	var ref string
	if id == "latest" {
		if len(rows) > 0 {
			ref = rows[0].Ref
		}
	} else {
		for _, v := range rows {
			if fmt.Sprintf("%d-%s", v.Unix, v.Op) == id {
				ref = v.Ref
				break
			}
		}
	}
	if ref == "" {
		fmt.Fprintf(stderr, "error: no version %q of branch %s (try `gg versions %s`)\n", id, branch, branch)
		return 1
	}
	policy := map[string]string{}
	if *discard {
		policy["restore-dirty"] = "proceed"
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(ctx, svc, engine.RestoreBranchVersion{Branch: branch, Ref: ref}, dec, stderr)
	return finish(res, err, stdout, stderr)
}
```

`cli.go`: add `case "versions": return cmdVersions(svc, rest, stdin, stdout, stderr)` to `runOne` and `"versions"` to the `commands` map. In `Run`, right after the Service is opened, inject config policy best-effort:

```go
if cfg, err := loadConfigFor(svc); err == nil {
	svc.SetVersionsPolicy(engine.VersionsPolicy{Enabled: !cfg.Versions.Disabled, MaxAgeDays: cfg.Versions.MaxAgeDays})
}
```

(`loadConfigFor` lives in `review.go`; if the import cycle/placement complains, move the call into `runOne`'s mutating cases — but a single best-effort load in `Run` is the intended shape. A load error silently keeps the domain default: enabled/90.)

- [ ] **Step 4: Run** — `go test ./internal/cli/ -run TestCmdVersions -v` → PASS.
- [ ] **Step 5: e2e scenario** — `e2e/scenarios/s80_cli_versions.toml`:

```toml
name = "cli: versions records a pre-merge snapshot and restore rewinds it"

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
cmd             = ["merge", "feat"]
exit            = 0

[[run]]
cmd             = ["versions"]
exit            = 0
stdout_contains = ["-merge", "main work"]

[[run]]
cmd             = ["versions", "restore", "main", "latest"]
exit            = 0

[[run]]
cmd             = ["versions"]
exit            = 0
stdout_contains = ["-merge", "-restore"]

[expect]
branch = "main"
log    = [{ subject = "main work" }, { subject = "base" }]
```

(Adapt the `[expect] log` entry shape to `LogExpect` in `e2e/scenario.go` — subjects from HEAD after restore: the merge commit and "feat work" are gone. Adapt the merge run if `gg merge` needs a decider answer in non-interactive mode — check an existing merge scenario for the pattern and copy its flags/stdin.) Run `go test ./e2e/ -run 80 -v` → PASS.

- [ ] **Step 6: using-gg** — add to the Commands section of `internal/agentskill/using-gg.md` (style of the `gg log` bullets):

```markdown
- `gg versions [<branch>]` — list a branch's recorded pre-operation
  snapshots (taken automatically before merges, rebases, resets, amends,
  and branch deletion), newest first: `<id> <short-sha> <time> <subject>`.
- `gg versions restore [--discard] <branch> <id|latest>` — move the branch
  back to a recorded version (its own pre-restore state is snapshotted
  first). Restoring the current branch hard-resets; `--discard` answers the
  dirty-tree prompt. Also recreates a deleted branch. Exit 0 restored,
  1 failure/unknown id, 2 usage.
```

Bump `agentskill.Version` to 53. **Commit the regenerated SKILL.md if `gg init --update` output is tracked anywhere in-repo** (memory gotcha).
- [ ] **Step 7: Commit** — `feat(cli): gg versions list/restore + e2e + using-gg entry`.

---

### Task 9: TUI versions popup — list, compare, restore, delete

**Files:**
- Create: `internal/tui/versions_popup.go`
- Modify: `internal/tui/action_menu.go` (register row), `internal/tui/command_palette.go` (entry), `internal/tui/source.go` (`opAffectedSources` cases), `internal/tui/i18n_display.go` (`opDisplayName` cases), `internal/tui/model.go` (Model field `versionsGen int`; msg handling), the four `internal/i18n/lang/*.toml`
- Test: `internal/tui/versions_popup_test.go` (+ the existing AST/i18n gates)

**Interfaces:**
- Consumes: Task 6 queries, Task 5 ops, `m.branchTipHash` (branch_compare.go:65), `openCompareFiles` (files_view.go:297), `decisionState`/`resolveModal` (op.go/model.go), `repoPopup` layer pattern, `popupMax`.
- Produces: `versionsPopup` layer with two modes (`modeBranches` list of `model.VersionedBranch`, `modeVersions` list of `model.BranchVersion`); `Model.openBranchVersions(branch string)` and `Model.openVersionBranchList()`; msgs `versionsLoadedMsg{gen int; branch string; rows []model.BranchVersion; err error}` and `versionBranchesLoadedMsg{gen int; rows []model.VersionedBranch; err error}`.

- [ ] **Step 1: Failing test** — `internal/tui/versions_popup_test.go`, following the package's popup-test style (construct a `Model` via the package's test fixture — grep `newTestModel` / how `repo_popup` or `gitconfig_popup` tests build one):

```go
// TestVersionsPopupRendersRows: push a versionsPopup in modeVersions with two
// fabricated model.BranchVersion rows; render; assert the box contains the
// short sha, the translated op label, and the subject.
// TestVersionsPopupEnterOpensCompare: fixture with m.branches containing the
// branch tip; enter on a row must set filesMode compare with both endpoints
// resolved to HASHES (assert m.filesLeft/filesRight Endpoint hashes — never
// a branch name).
// TestVersionsPopupRestoreOpensModal: pressing 'r' sets m.modal with options
// ["Reset branch","New branch at version","Cancel"].
// TestVersionsPopupDeletedBranchDisablesCompare: a modeVersions popup whose
// branch is deleted (no tip): enter sets a statusMsg instead of a compare.
```

Write full bodies against the real fixture helper found in the package.

- [ ] **Step 2: Run to verify fail** — `go test ./internal/tui/ -run TestVersionsPopup -v` → FAIL.
- [ ] **Step 3: Implement `versions_popup.go`** (complete structure; render mirrors `repoPopup.box` with `popupResolveRowCap`/`popupResolveWidth`/`popupBox`):

```go
package tui

// versionsPopup lists a branch's recorded versions (operations history), or —
// in branch mode, opened from the command palette — all branches that have
// versions, including deleted ones. Layer-stack popup, popupMax-embedding.

const (
	versionsModeBranches = iota
	versionsModeVersions
)

type versionsPopup struct {
	popupMax
	mode       int
	fromList   bool // versions mode entered by drilling from branch mode: esc goes back
	branch     string
	deleted    bool
	branchRows []model.VersionedBranch
	rows       []model.BranchVersion
	sel        int
	loading    bool
	err        string
}

type versionsLoadedMsg struct {
	gen    int
	branch string
	rows   []model.BranchVersion
	err    error
}

type versionBranchesLoadedMsg struct {
	gen  int
	rows []model.VersionedBranch
	err  error
}
```

Loading commands (gen-guarded via a new `Model.versionsGen int`, bumped on every open and on `reRoot`):

```go
func (m Model) loadBranchVersionsCmd(gen int, branch string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		rows, err := svc.BranchVersions(context.Background(), branch)
		return versionsLoadedMsg{gen: gen, branch: branch, rows: rows, err: err}
	}
}

func (m Model) loadVersionBranchesCmd(gen int) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		rows, err := svc.AllVersionBranches(context.Background())
		return versionBranchesLoadedMsg{gen: gen, rows: rows, err: err}
	}
}
```

Open helpers:

```go
func (m Model) openBranchVersions(branch string, deleted, fromList bool) (Model, tea.Cmd) {
	m.versionsGen++
	p := &versionsPopup{mode: versionsModeVersions, branch: branch, deleted: deleted, fromList: fromList, loading: true}
	m = m.pushLayer(p)
	return m, m.loadBranchVersionsCmd(m.versionsGen, branch)
}

func (m Model) openVersionBranchList() (Model, tea.Cmd) {
	m.versionsGen++
	p := &versionsPopup{mode: versionsModeBranches, loading: true}
	m = m.pushLayer(p)
	return m, m.loadVersionBranchesCmd(m.versionsGen)
}
```

Msg handling in `Model.Update` (beside other popup msgs): on `versionsLoadedMsg`/`versionBranchesLoadedMsg`, drop when `msg.gen != m.versionsGen`; otherwise find the top `*versionsPopup` via `layerOf[*versionsPopup](m)` and fill `rows`/`branchRows`/`err`, `loading = false`.

`update` keys: `up`/`down` move `sel`; `esc` → in versions mode with `fromList`, switch back to branch mode (re-trigger `loadVersionBranchesCmd`), else `m.popLayer()`. `enter`:
- branches mode → drill: `return m.openBranchVersionsFromList(...)` — set mode/branch/deleted on the SAME popup (`p.mode = versionsModeVersions; p.fromList = true; p.loading = true`) and return `loadBranchVersionsCmd`.
- versions mode → compare: if `p.deleted` → `m.statusMsg = i18n.T("branch no longer exists — restore it to compare")`; else resolve tip: `tip := m.branchTipHash(p.branch)`, then `m = m.clearLayers()` and `return m.openCompareFiles(model.Endpoint{Kind: model.EndpointCommit, Hash: v.Hash}, model.Endpoint{Kind: model.EndpointCommit, Hash: tip})` (version = base/left, current tip = subject/right — both HASHES, the diff-LRU gotcha).

`r` (versions mode): frontend modal (the tagDeleteRow pattern):

```go
v := p.rows[p.sel]
short := v.Hash
if len(short) > 8 { short = short[:8] }
branch, ref, hash := p.branch, v.Ref, v.Hash
m.modal = &decisionState{
	req: engine.DecisionRequest{
		ID:      "restore-version-choice",
		Prompt:  i18n.T("Restore %s to %s?", branch, short),
		Options: []string{"Reset branch", "New branch at version", "Cancel"},
	},
	onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
		switch opt {
		case "Reset branch":
			m = m.clearLayers()
			return m.startOp(engine.RestoreBranchVersion{Branch: branch, Ref: ref})
		case "New branch at version":
			return m.openVersionBranchNamePopup(hash)
		}
		return m, nil
	},
}
return m, nil
```

`openVersionBranchNamePopup(startHash string)` — a one-line textfield popup (mirror the smallest existing textfield popup in the package, e.g. `commit_name_popup.go`'s shape): title `i18n.T("New branch at version")`, enter → `m = m.clearLayers()` then `m.startOp(engine.CreateBranch{Name: value, StartPoint: startHash})`, esc → `popLayer`.

`d` (versions mode): confirm modal `Options: []string{"Delete", "Cancel"}` (existing vocab), prompt `i18n.T("Delete this version of %s?", branch)`; on `"Delete"` run the synchronous stageCmd-style command (popup stays open, rows reload after the write — the `gitConfigWriteCmd` pattern):

```go
func (m Model) versionDeleteCmd(gen int, branch, ref string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		if _, err := svc.Execute(context.Background(), engine.DeleteBranchVersion{Ref: ref}, nil, nil); err != nil {
			return versionsLoadedMsg{gen: gen, branch: branch, err: err}
		}
		rows, err := svc.BranchVersions(context.Background(), branch)
		return versionsLoadedMsg{gen: gen, branch: branch, rows: rows, err: err}
	}
}
```

`y` (versions mode): copy the FULL sha via the same clipboard path an existing `.`-menu copy row uses (`rg -n 'clipboard.Copy' internal/tui` — reuse that helper/seam, statusMsg `i18n.T("copied %s", short)`).

Row rendering (versions mode): `2026-07-21 14:03 · rebase · a1b2c3d4 <subject>` — time via `time.Unix(v.Unix, 0).Format("2006-01-02 15:04")`, op via `opDisplayName(v.Op)`, sha short 8. Branch mode rows: `<branch>  <count> versions` with a dim ` (deleted)` suffix when `Deleted`. Footer hint line inside the box: versions mode `i18n.T("[enter] compare  [r] restore  [d] delete  [y] copy sha")`, branch mode `i18n.T("[enter] versions")`.

**Branches-panel `.` row** — in the file holding Branches-panel action rows (locate with `rg -n 'panelBranches' internal/tui/*actions*.go internal/tui/action_menu.go` — follow the neighboring rows' file):

```go
func (m Model) branchVersionsRow() (actionRow, bool) {
	if m.focus != panelBranches || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelBranches)
	if !ok || bi < 0 || bi >= len(m.branches) {
		return actionRow{}, false
	}
	name := m.branches[bi].Name
	return actionRow{
		id:    "branch-versions",
		label: i18n.T("Previous versions…"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openBranchVersions(name, false, false)
		},
	}, true
}
```

Register it in `action_menu.go`'s registry beside the other branch rows. (Self-gating on zero versions is deliberately NOT done here — it would cost a git read per menu open; the popup itself shows `i18n.T("no versions recorded")`.)

**Palette entry** — in `paletteCommands()` (alphabetical position):

```go
{label: i18n.T("Branch versions…"), run: func(m Model) (Model, tea.Cmd) { return m.openVersionBranchList() }},
```

**`opAffectedSources`** (source.go): `case engine.RestoreBranchVersion: return []sourceKey{srcStatus, srcBranches, srcFeed, srcWorktrees}` and `case engine.DeleteBranchVersion: return []sourceKey{}`.

**`opDisplayName`** (i18n_display.go): add literal cases for the tokens not already present: `"interactive-rebase"`, `"amend"`, `"undo-commit"`, `"reset"`, `"pull"`, `"delete-branch"`, `"restore"` (each `return i18n.T("<token>")`; `"merge"`/`"rebase"` exist).

**`reRoot`**: bump `m.versionsGen++` where other gens are bumped so a stale load after a repo switch is dropped.

- [ ] **Step 4: Bundle keys** — add ALL new keys to the four bundles (grep each first). New keys: `Previous versions…`, `Branch versions…`, `Restore %s to %s?`, `Reset branch`, `New branch at version`, `Delete this version of %s?`, `New branch at version` (dup — once), `no versions recorded`, `branch no longer exists — restore it to compare`, `copied %s`, `(deleted)`, `[enter] compare  [r] restore  [d] delete  [y] copy sha`, `[enter] versions`, `interactive-rebase`, `amend`, `undo-commit`, `reset`, `pull`, `delete-branch`, `restore`, plus the popup titles `Versions of %s` / `Branch versions`. Provide translations in each language (translate naturally; single-word op tokens: ja `対話的リベース`/`amend→修正`/`undo-commit→コミット取り消し`/`reset→リセット`/`pull→プル`/`delete-branch→ブランチ削除`/`restore→復元`, and equivalents for ko/zh/ru). Run the gates:

```
go test ./internal/tui/ -run 'TestActionMenuLabelsTranslated|OptionsVocab|Test.*I18n|Test.*Scan' -v
```

All PASS (the options gate needs `optionDisplayName` cases for `Reset branch` / `New branch at version` — add them; `Delete`/`Cancel` exist).

- [ ] **Step 5: Run full TUI tests** — `go test ./internal/tui/ -v` → PASS (including Step 1's tests).
- [ ] **Step 6: Commit** — `feat(tui): branch versions popup — list, compare, restore, delete`.

---

### Task 10: TUI Settings "Operations history" + policy rewiring

**Files:**
- Modify: `internal/tui/settings_popup.go` (menu const + slice + title + enter case + sub-view), `internal/tui/source.go:364` and `internal/tui/load.go:89` (policy injection beside `SetShowEOLOnlyChanges`), the four bundles
- Create: helper in `internal/tui/versions_settings.go`
- Test: `internal/tui/versions_settings_test.go`

**Interfaces:**
- Consumes: Task 1 writers, Task 6 setter.
- Produces: `versionsPolicyFromConfig(cfg config.Config) engine.VersionsPolicy`; `Model.saveVersionsRetention(days int) Model`; `Model.toggleVersionsRecording() Model`; settings sub-view flag `opsHistView` on `settingsPopup`.

- [ ] **Step 1: Failing test** — `versions_settings_test.go`:

```go
// TestVersionsPolicyFromConfig: table — {Disabled:false,MaxAgeDays:90} →
// {Enabled:true,90}; {Disabled:true} → Enabled:false; MaxAgeDays:-1 passes
// through.
// TestSaveVersionsRetentionWritesConfigAndPolicy: fixture Model with a temp
// repoConfigPath; m.saveVersionsRetention(-1); assert file contains
// "max_age_days = -1", m.cfg.Versions.MaxAgeDays == -1.
// TestToggleVersionsRecording: flips cfg.Versions.Disabled and writes
// "disabled = true".
```

(Full bodies against the package fixture.)

- [ ] **Step 2: Run to verify fail** → FAIL.
- [ ] **Step 3: Implement** — `versions_settings.go`:

```go
package tui

func versionsPolicyFromConfig(cfg config.Config) engine.VersionsPolicy {
	return engine.VersionsPolicy{Enabled: !cfg.Versions.Disabled, MaxAgeDays: cfg.Versions.MaxAgeDays}
}

// saveVersionsRetention persists [versions] max_age_days (-1 = forever) to
// the active repo config and updates the live policy.
func (m Model) saveVersionsRetention(days int) Model {
	if days == 0 || days < -1 {
		m.statusMsg = i18n.T("retention must be a positive day count or -1 (keep forever)")
		return m
	}
	m.cfg.Versions.MaxAgeDays = days
	m.svc.SetVersionsPolicy(versionsPolicyFromConfig(m.cfg))
	if m.repoConfigPath == "" {
		m.statusMsg = i18n.T("retention set (not saved: no repo config path)")
		return m
	}
	if err := config.SetVersionsMaxAgeDays(m.repoConfigPath, days); err != nil {
		m.statusMsg = i18n.T("retention set but not saved: %s", err.Error())
	}
	return m
}

func (m Model) toggleVersionsRecording() Model {
	m.cfg.Versions.Disabled = !m.cfg.Versions.Disabled
	m.svc.SetVersionsPolicy(versionsPolicyFromConfig(m.cfg))
	if m.repoConfigPath == "" {
		m.statusMsg = i18n.T("recording toggled (not saved: no repo config path)")
		return m
	}
	if err := config.SetVersionsDisabled(m.repoConfigPath, m.cfg.Versions.Disabled); err != nil {
		m.statusMsg = i18n.T("recording toggled but not saved: %s", err.Error())
	}
	return m
}
```

`settings_popup.go`: add `settingsMenuOpsHist = "Operations history"` const + slice entry + `settingsMenuTitle` case (`i18n.T("Operations history")`); enter case sets `p.opsHistView = true; p.opsHistSel = 0; p.opsHistEditing = false`. Sub-view state fields on `settingsPopup`: `opsHistView bool; opsHistSel int; opsHistEditing bool; opsHistField textfield`. Mirror the `ratesView` update block (settings_popup.go:481-508): two rows — row 0 **Retention** (enter → edit; accept digits AND a leading `-`; enter parses `strconv.Atoi` and calls `m.saveVersionsRetention(n)`), row 1 **Recording** (enter → `m.toggleVersionsRecording()`). Esc: editing → stop editing; else leave the sub-view (mirror the `ratesView` esc handling at :355-360). Render: mirror the `ratesView` branch in the popup's box/render func with two `padCell`-aligned lines:

```go
retention := i18n.T("keep forever")
if m.cfg.Versions.MaxAgeDays > 0 {
	retention = i18n.T("%d days", m.cfg.Versions.MaxAgeDays)
}
rows := []string{
	padCell(i18n.T("Retention"), labelW) + retention,   // or the live edit field
	padCell(i18n.T("Recording"), labelW) + onOff(!m.cfg.Versions.Disabled),
}
```

Policy injection at both config-arrival paths: beside `svc.SetShowEOLOnlyChanges(cfg.UI.ShowEOLOnlyChanges)` in `source.go:364` AND `load.go:89`, add `svc.SetVersionsPolicy(versionsPolicyFromConfig(cfg))`.

- [ ] **Step 4: Bundle keys** — `Operations history`, `Retention`, `Recording`, `keep forever`, `%d days`, `retention must be a positive day count or -1 (keep forever)`, `retention set (not saved: no repo config path)`, `retention set but not saved: %s`, `recording toggled (not saved: no repo config path)`, `recording toggled but not saved: %s` — ×4 languages (grep first; `%d days` may exist). Gates green.
- [ ] **Step 5: Run** — `go test ./internal/tui/ -v` → PASS.
- [ ] **Step 6: Commit** — `feat(tui): Settings "Operations history" retention + recording editor`.

---

### Task 11: full verification + docs

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`

- [ ] **Step 1: Full suite** — `./test.sh` then `./test.sh race` → both PASS (fix anything that surfaces before proceeding).
- [ ] **Step 2: Docs** —
  - `CHANGELOG.md`: entry "Branch versions (operations history): pre-operation snapshots as hidden refs; list/compare/restore/delete via Branches `.` menu, command palette, Settings, and `gg versions`."
  - `README.md`: short user-facing section (what gets recorded, where to find it, `[versions]` config keys).
  - `CLAUDE.md`: package-map updates — `engine` row (snapshotBranchTip + VersionsPolicy on OpDeps + the two new ops), `git` row (UpdateRef/DeleteRef/ForEachRef + versionref helpers + decorate-exclude), `domain` row (BranchVersions/AllVersionBranches + SetVersionsPolicy), `config` row (`[versions]` + two writers), `tui` row (versions popup + Settings entry), `cli` row (`gg versions`).
- [ ] **Step 3: Commit** — `docs: branch-versions feature docs (CHANGELOG/README/CLAUDE)`.

---

## Self-review notes (already applied)

- Spec coverage: storage format (T2/T3), config (T1), triggers incl. DeleteBranch and SmartPull lanes (T4), restore/delete ops (T5), domain queries + policy (T6), decoration gotcha (T7), CLI + e2e + using-gg (T8), TUI popup incl. deleted-branch recovery via palette (T9), Settings editor + both config-arrival wirings (T10), docs (T11). `checkoutPull` explicitly deferred (commented in code).
- The spec's "policy passed into OpDeps by Execute" is honored via the new `OpDeps.Versions` field; the spec's `Service`-side storage via `atomic.Value` (nil = default enabled/90).
- Type names consistent across tasks: `VersionsPolicy`, `model.BranchVersion`, `model.VersionedBranch`, `model.RefInfo`, `git.VersionRef`/`ParseVersionRef`/`VersionRefPrefix`, ops `RestoreBranchVersion`/`DeleteBranchVersion`.
