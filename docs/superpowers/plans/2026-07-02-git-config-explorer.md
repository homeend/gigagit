# Git-Config Explorer Implementation Plan (Stage 3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Settings gains a "Git config explorer" — a searchable, full-height popup listing every config key git knows (columns: key | local | global | default, explicit `(unset)`), where ~64 curated keys show a real default + description and support one-key set-local / set-global / unset through the existing `engine.SetGitConfig` op.

**Architecture:** Two new read verbs (`git help -c` catalog; `git config --list --show-scope -z` values) merge in a `domain.GitConfigRows` query; a new pure package `internal/gitconfdocs` carries the curated table (staleness-tested against the live catalog); the explorer popup follows the `repoPopup` list/filter pattern; writes extend `SetGitConfig` with `Unset bool` (new `git.ConfigUnset` verb) and run through the `stageCmd`-style synchronous-Execute pattern (config writes are fast and decision-free).

**Tech Stack:** Go 1.26, Bubble Tea, real-git `t.TempDir()` tests + `gitexec.FakeRunner` argv assertions.

**Spec:** `docs/superpowers/specs/2026-07-02-related-prompts-notifications-git-config-design.md` (Stage 3 section).

## Global Constraints

- Work on branch `feat/git-config-explorer` in the worktree `.claude/worktrees/git-config-explorer` — never the shared checkout. Worktree-relative paths.
- TDD: failing test → implement → pass → gofmt → commit.
- `internal/tui`/`internal/cli` never import `internal/git` (archtest). `internal/gitconfdocs` is pure data (imports nothing above stdlib) — the TUI MAY import it directly (like `commitgraph`/`textdiff`); register it as a leaf in the archtest layering DAG.
- A git verb is one invocation; argv via `gitcmd`.
- Pinned output formats (verified live, git 2.x):
  - `git help -c` → one key per line, camelCase (e.g. `add.ignoreErrors`), ~870 lines, includes placeholder keys like `branch.<name>.remote`.
  - `git config --list --show-scope -z` → NUL-separated tokens in PAIRS: `scope` NUL `key\nvalue` NUL … Scope is `system`/`global`/`local`/`worktree`/`command`. Keys are LOWERCASED by git (section+key, subsections preserved) — merging against the camelCase catalog must match case-insensitively.
- Writes go through `engine.SetGitConfig` via `domain.Execute` — never ad-hoc `git config` calls from the TUI. One write op: extend `SetGitConfig` with `Unset bool` (spec's explicit decision: no second op). Existing constructors (`SetGitConfig{Key, Value}`, `{Key, Value, Global: true}`) must keep working unchanged — the notification center uses them.
- `git config --unset` on a missing key exits 5 — the verb treats exit 5 as a no-op success (the `ConfigGet` exit-1 pattern).
- Explorer: `/` filter is navigation-first move-while-typing (the `repoPopup` pattern — `filterMotion`); `z` cycles display modes; esc closes (never trap). Non-curated rows are read-only; their default column shows `—`.
- Unset scopes render an explicit dim `(unset)` — never blank.
- Commit trailer on every commit:
  ```
  Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n
  ```
- `gofmt -w` every touched file. `./test.sh race` green before the final task ends.

---

### Task 1: git verbs — `ConfigKeys`, `ConfigListScoped`, `ConfigUnset`

**Files:**
- Create: `internal/git/config_explore.go`
- Modify: `internal/git/config.go` (add `ConfigUnset` beside `ConfigSet`)
- Modify: `internal/engine/gitops.go` (add `ConfigUnset` to the `GitOps` interface, beside `ConfigSet`)
- Test: `internal/git/config_explore_test.go`

**Interfaces:**
- Consumes: `gitcmd.New`, `r.Runner.Run`; test helpers `newTestRepo(t)`/`gitIn`/`writeFile`/`commitAll` (see `internal/git/archive_test.go` / `format_patch_test.go` — match what exists).
- Produces (Tasks 2–3 rely on these exact shapes):
  - `func (r *Repo) ConfigKeys(ctx context.Context) ([]string, error)` — the `git help -c` catalog, one key per line, order preserved.
  - `type ConfigSetting struct { Scope ConfigScope; Key, Value string }`
  - `func (r *Repo) ConfigListScoped(ctx context.Context) ([]ConfigSetting, error)` — `git config --list --show-scope -z`; ONLY `local`/`global` records kept (system/worktree/command dropped); an empty config yields `(nil, nil)`.
  - `func (r *Repo) ConfigUnset(ctx context.Context, scope ConfigScope, key string) error` — `git config --local|--global --unset <key>`; exit 5 (key absent) returns nil.

- [ ] **Step 1: Write the failing test**

Create `internal/git/config_explore_test.go`:

```go
package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestConfigKeysParsesHelpOutput(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git help -c", gitexec.Result{Stdout: "add.ignoreErrors\nuser.name\n\nuser.email\n"})
	r := &Repo{Runner: f}
	keys, err := r.ConfigKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 || keys[0] != "add.ignoreErrors" || keys[2] != "user.email" {
		t.Fatalf("keys = %v, want the 3 non-blank lines in order", keys)
	}
	if got := strings.Join(f.Calls[0].Argv, " "); got != "help -c" {
		t.Fatalf("argv = %q, want 'help -c'", got)
	}
}

func TestConfigKeysRealGit(t *testing.T) {
	_, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	keys, err := r.ConfigKeys(context.Background())
	if err != nil {
		t.Fatalf("ConfigKeys: %v", err)
	}
	if len(keys) < 100 {
		t.Fatalf("expected a big catalog, got %d keys", len(keys))
	}
	found := false
	for _, k := range keys {
		if k == "user.name" {
			found = true
		}
	}
	if !found {
		t.Fatal("catalog must contain user.name")
	}
}

func TestConfigListScopedParsesZFormat(t *testing.T) {
	f := gitexec.NewFakeRunner()
	// Pinned -z format: scope NUL key\nvalue NUL, repeated. Include a system
	// record (dropped) and a multiline value (survives -z).
	raw := "system\x00core.something\ntrue\x00" +
		"global\x00user.name\nAda L\x00" +
		"local\x00core.filemode\nfalse\x00" +
		"local\x00alias.lg\nlog --graph\nall\x00"
	f.SetResponse("git config list", gitexec.Result{Stdout: raw})
	r := &Repo{Runner: f}
	set, err := r.ConfigListScoped(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(set) != 3 {
		t.Fatalf("settings = %+v, want 3 (system dropped)", set)
	}
	if set[0].Scope != ConfigGlobal || set[0].Key != "user.name" || set[0].Value != "Ada L" {
		t.Fatalf("first = %+v", set[0])
	}
	if set[2].Value != "log --graph\nall" {
		t.Fatalf("multiline value mangled: %q", set[2].Value)
	}
	if got := strings.Join(f.Calls[0].Argv, " "); got != "config --list --show-scope -z" {
		t.Fatalf("argv = %q", got)
	}
}

func TestConfigListScopedRealGit(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()
	if err := r.ConfigSet(ctx, ConfigGlobal, "user.name", "Global Person"); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigSet(ctx, ConfigLocal, "core.somekey", "v1"); err != nil {
		t.Fatal(err)
	}
	set, err := r.ConfigListScoped(ctx)
	if err != nil {
		t.Fatalf("ConfigListScoped: %v", err)
	}
	var sawGlobal, sawLocal bool
	for _, s := range set {
		if s.Scope == ConfigGlobal && s.Key == "user.name" && s.Value == "Global Person" {
			sawGlobal = true
		}
		if s.Scope == ConfigLocal && s.Key == "core.somekey" && s.Value == "v1" {
			sawLocal = true
		}
	}
	if !sawGlobal || !sawLocal {
		t.Fatalf("missing scoped records: global=%v local=%v in %+v", sawGlobal, sawLocal, set)
	}
	_ = dir
}

func TestConfigUnsetRemovesKeyAndToleratesMissing(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()
	if err := r.ConfigSet(ctx, ConfigLocal, "core.somekey", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigUnset(ctx, ConfigLocal, "core.somekey"); err != nil {
		t.Fatalf("unset existing: %v", err)
	}
	if _, set, _ := r.ConfigGet(ctx, ConfigLocal, "core.somekey"); set {
		t.Fatal("key still set after unset")
	}
	// Unsetting a missing key exits 5 — must be a no-op success.
	if err := r.ConfigUnset(ctx, ConfigLocal, "core.somekey"); err != nil {
		t.Fatalf("unset missing key must be a no-op, got %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/git/ -run 'TestConfigKeys|TestConfigListScoped|TestConfigUnset'`
Expected: FAIL to build — verbs undefined.

- [ ] **Step 3: Implement**

Create `internal/git/config_explore.go`:

```go
package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// ConfigKeys returns git's own config-key catalog (`git help -c`): one key
// per line, camelCase, catalog order preserved (it is already sorted).
// Placeholder keys like branch.<name>.remote come through verbatim.
func (r *Repo) ConfigKeys(ctx context.Context) ([]string, error) {
	res, err := r.Runner.Run(ctx, "git help -c", gitcmd.New("help").Arg("-c").ToArgv())
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, line := range strings.Split(res.Stdout, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			keys = append(keys, line)
		}
	}
	return keys, nil
}

// ConfigSetting is one set config entry with the scope it was set at.
type ConfigSetting struct {
	Scope ConfigScope
	Key   string // lowercased by git (section+key; subsections keep case)
	Value string
}

// ConfigListScoped lists every set config value with its scope
// (`git config --list --show-scope -z`). The -z record shape is
// `scope NUL key \n value NUL` — NUL-separated tokens arrive in PAIRS, and
// only -z survives multiline values. Only local and global records are kept:
// the explorer's columns are local | global | default, so system/worktree/
// command scopes are out of scope by design.
func (r *Repo) ConfigListScoped(ctx context.Context) ([]ConfigSetting, error) {
	argv := gitcmd.New("config").Arg("--list", "--show-scope", "-z").ToArgv()
	res, err := r.Runner.Run(ctx, "git config list", argv)
	if err != nil {
		return nil, err
	}
	tokens := strings.Split(res.Stdout, "\x00")
	var out []ConfigSetting
	for i := 0; i+1 < len(tokens); i += 2 {
		scope := tokens[i]
		kv := tokens[i+1]
		var sc ConfigScope
		switch scope {
		case "local":
			sc = ConfigLocal
		case "global":
			sc = ConfigGlobal
		default:
			continue // system/worktree/command: not an explorer column
		}
		key, value, _ := strings.Cut(kv, "\n")
		out = append(out, ConfigSetting{Scope: sc, Key: key, Value: value})
	}
	return out, nil
}
```

In `internal/git/config.go`, add after `ConfigSet`:

```go
// ConfigUnset removes one config key at the given scope (Local or Global
// only; an Effective/unknown scope falls back to Local). A key that is not
// set exits 5 — treated as a no-op success (the ConfigGet exit-1 pattern),
// so unset is idempotent for callers.
func (r *Repo) ConfigUnset(ctx context.Context, scope ConfigScope, key string) error {
	f, ok := scope.flag()
	if !ok {
		f = "--local"
	}
	b := gitcmd.New("config").Arg(f, "--unset", key)
	res, err := r.Runner.Run(ctx, "git config", b.ToArgv())
	if err != nil && res.ExitCode == 5 {
		return nil // key was not set: already in the desired state
	}
	return err
}
```

In `internal/engine/gitops.go`, add to the `GitOps` interface beside `ConfigSet`:

```go
	ConfigUnset(ctx context.Context, scope git.ConfigScope, key string) error
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/git/ -run 'TestConfigKeys|TestConfigListScoped|TestConfigUnset' && go build ./...`
Expected: PASS (5 tests), build clean. Note: if `FakeRunner.Run` errors on unmatched span names, add the missing `SetResponse` lines the same way `TestCommitGraphWriteArgv` did — check `internal/gitexec/fake.go` behavior first.

- [ ] **Step 5: gofmt and commit**

```bash
gofmt -w internal/git/config_explore.go internal/git/config_explore_test.go internal/git/config.go internal/engine/gitops.go
git add internal/git/config_explore.go internal/git/config_explore_test.go internal/git/config.go internal/engine/gitops.go
git commit -m "feat(git): ConfigKeys + ConfigListScoped + ConfigUnset verbs

git help -c (the key catalog), git config --list --show-scope -z (set values
with scope; -z survives multiline values; only local/global kept), and
git config --unset (exit 5 = already unset = no-op). Back the config
explorer's read/write paths.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
```

---

### Task 2: extend `engine.SetGitConfig` with `Unset bool`

**Files:**
- Modify: `internal/engine/set_git_config.go`
- Test: `internal/engine/set_git_config_test.go` (append)

**Interfaces:**
- Consumes: `GitOps.ConfigUnset` (Task 1).
- Produces: `engine.SetGitConfig{Key, Value string; Global, Unset bool}` — `Unset: true` removes the key at the chosen scope (Value ignored); existing zero-value behavior unchanged (the notification center's constructors keep working).

- [ ] **Step 1: Write the failing test** (append to `internal/engine/set_git_config_test.go`):

```go
func TestSetGitConfigUnsetRemovesKey(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, repo := newRepo(t)
	ctx := context.Background()
	if err := repo.ConfigSet(ctx, git.ConfigLocal, "fetch.writeCommitGraph", "true"); err != nil {
		t.Fatal(err)
	}

	res, err := SetGitConfig{Key: "fetch.writeCommitGraph", Unset: true}.Run(ctx, OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if _, set, _ := repo.ConfigGet(ctx, git.ConfigLocal, "fetch.writeCommitGraph"); set {
		t.Fatal("key still set after Unset op")
	}
}

func TestSetGitConfigUnsetMissingKeyIsNoOp(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, repo := newRepo(t)
	if _, err := (SetGitConfig{Key: "fetch.writeCommitGraph", Unset: true}).Run(context.Background(), OpDeps{Repo: repo}); err != nil {
		t.Fatalf("unsetting a missing key must succeed (verb maps exit 5 to nil), got %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/engine/ -run TestSetGitConfigUnset`
Expected: FAIL to build — `Unset` field unknown.

- [ ] **Step 3: Implement** — replace the body of `SetGitConfig.Run` in `internal/engine/set_git_config.go` (keep the struct doc; add `Unset bool // remove the key at the chosen scope; Value is ignored` to the struct):

```go
func (op SetGitConfig) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Key == "" {
		return Result{}, fmt.Errorf("set git config: key is required")
	}
	scope := git.ConfigLocal
	where := "in this repo"
	if op.Global {
		scope = git.ConfigGlobal
		where = "globally"
	}
	if op.Unset {
		deps.emit(ctx, Progress{Step: "unsetting git config", Detail: op.Key + " " + where})
		if err := deps.Repo.ConfigUnset(ctx, scope, op.Key); err != nil {
			return Result{}, fmt.Errorf("unset git config: %s: %w", op.Key, err)
		}
		res := Result{Summary: fmt.Sprintf("%s unset %s", op.Key, where), Changed: true}
		deps.emit(ctx, Done{Result: res})
		return res, nil
	}
	deps.emit(ctx, Progress{Step: "setting git config", Detail: op.Key + " " + where})
	if err := deps.Repo.ConfigSet(ctx, scope, op.Key, op.Value); err != nil {
		return Result{}, fmt.Errorf("set git config: %s: %w", op.Key, err)
	}
	res := Result{Summary: fmt.Sprintf("%s = %s set %s", op.Key, op.Value, where), Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/engine/ -run TestSetGitConfig` (all — the 3 existing set tests must stay green) then `go test ./internal/engine/ ./internal/tui/ -count=1` (the notification center constructs this op — full both packages).
Expected: PASS.

- [ ] **Step 5: gofmt and commit**

```bash
gofmt -w internal/engine/set_git_config.go internal/engine/set_git_config_test.go
git add internal/engine/set_git_config.go internal/engine/set_git_config_test.go
git commit -m "feat(engine): SetGitConfig gains Unset — one write op for set AND unset

Unset removes the key at the chosen scope (idempotent: the verb maps
git's exit-5 already-unset to success). Existing constructors unchanged.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
```

---

### Task 3: `model.GitConfigRow` + `domain.GitConfigRows` merge query

**Files:**
- Create: `internal/model/gitconfig.go`
- Create: `internal/domain/gitconfig.go`
- Test: `internal/domain/gitconfig_test.go`

**Interfaces:**
- Consumes: `git.ConfigKeys`/`ConfigListScoped` (Task 1); the domain `query(...)` wrapper + `newRealRepo(t)` (see `internal/domain/identity.go`/`repohealth.go`).
- Produces (Tasks 5–6 rely on):
  - `model.GitConfigRow{ Key string; LocalValue string; LocalSet bool; GlobalValue string; GlobalSet bool }` — `Key` is the display form: the catalog's camelCase when the key is in the catalog, else the as-set form.
  - `func (s *Service) GitConfigRows(ctx context.Context) ([]model.GitConfigRow, error)` — one row per catalog key (catalog order) PLUS any set key not in the catalog (e.g. `alias.*`), appended in first-seen order; values matched case-insensitively (git lowercases set keys; the catalog is camelCase); a key set twice at one scope keeps the LAST value (git's precedence order lists overrides last).

- [ ] **Step 1: Write the failing test**

Create `internal/domain/gitconfig_test.go`:

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
)

func TestGitConfigRowsMergesCatalogAndValues(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, svc := newRealRepo(t)
	ctx := context.Background()
	// fetch.writeCommitGraph is camelCase in the catalog; git stores the set
	// key lowercased — the merge must join them case-insensitively.
	if err := svc.Repo().ConfigSet(ctx, git.ConfigLocal, "fetch.writeCommitGraph", "true"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Repo().ConfigSet(ctx, git.ConfigGlobal, "user.name", "Global Person"); err != nil {
		t.Fatal(err)
	}

	rows, err := svc.GitConfigRows(ctx)
	if err != nil {
		t.Fatalf("GitConfigRows: %v", err)
	}
	if len(rows) < 100 {
		t.Fatalf("expected the whole catalog, got %d rows", len(rows))
	}
	byKey := map[string]int{}
	for i, r := range rows {
		byKey[r.Key] = i
	}
	i, ok := byKey["fetch.writeCommitGraph"] // display form = catalog camelCase
	if !ok {
		t.Fatal("catalog key fetch.writeCommitGraph missing (case-insensitive merge broken?)")
	}
	if r := rows[i]; !r.LocalSet || r.LocalValue != "true" || r.GlobalSet {
		t.Fatalf("fetch.writeCommitGraph row = %+v, want local true / global unset", r)
	}
	if r := rows[byKey["user.name"]]; !r.GlobalSet || r.GlobalValue != "Global Person" || r.LocalSet {
		t.Fatalf("user.name row = %+v, want global set / local unset", r)
	}
	// A catalog key never set anywhere: both scopes unset.
	if r := rows[byKey["add.ignoreErrors"]]; r.LocalSet || r.GlobalSet {
		t.Fatalf("add.ignoreErrors row = %+v, want both unset", r)
	}
}

func TestGitConfigRowsAppendsNonCatalogKeys(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, svc := newRealRepo(t)
	ctx := context.Background()
	if err := svc.Repo().ConfigSet(ctx, git.ConfigLocal, "alias.lg", "log --graph"); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.GitConfigRows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Key == "alias.lg" {
			found = true
			if !r.LocalSet || r.LocalValue != "log --graph" {
				t.Fatalf("alias.lg row = %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("a set key outside the catalog must still get a row")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/domain/ -run TestGitConfigRows`
Expected: FAIL to build.

- [ ] **Step 3: Implement**

Create `internal/model/gitconfig.go`:

```go
package model

// GitConfigRow is one key of the git-config explorer: the catalog key plus
// its explicitly-set local and global values. Unset scopes render an
// explicit "(unset)" in the UI — the zero value here IS "unset", which is
// why the Set flags exist (a key set to the empty string is still set).
type GitConfigRow struct {
	Key         string // display form: catalog camelCase when known, else as-set
	LocalValue  string
	LocalSet    bool
	GlobalValue string
	GlobalSet   bool
}
```

Create `internal/domain/gitconfig.go`:

```go
package domain

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// GitConfigRows merges git's config-key catalog (git help -c) with every
// explicitly-set local/global value (git config --list --show-scope), under
// one Read reservation. Set keys arrive lowercased while the catalog is
// camelCase, so the join is case-insensitive and the catalog form wins as
// the display key. Set keys outside the catalog (alias.*, tool sections)
// are appended after the catalog in first-seen order. A key set twice at
// one scope keeps the last value (git lists overrides last).
func (s *Service) GitConfigRows(ctx context.Context) ([]model.GitConfigRow, error) {
	return query(ctx, s, "gitconfigrows", func(ctx context.Context) ([]model.GitConfigRow, error) {
		keys, err := s.repo.ConfigKeys(ctx)
		if err != nil {
			return nil, err
		}
		settings, err := s.repo.ConfigListScoped(ctx)
		if err != nil {
			return nil, err
		}
		rows := make([]model.GitConfigRow, 0, len(keys)+8)
		index := make(map[string]int, len(keys)) // lowercase key → row index
		for _, k := range keys {
			index[strings.ToLower(k)] = len(rows)
			rows = append(rows, model.GitConfigRow{Key: k})
		}
		for _, st := range settings {
			lk := strings.ToLower(st.Key)
			i, ok := index[lk]
			if !ok {
				index[lk] = len(rows)
				i = len(rows)
				rows = append(rows, model.GitConfigRow{Key: st.Key})
			}
			switch st.Scope {
			case git.ConfigLocal:
				rows[i].LocalValue, rows[i].LocalSet = st.Value, true
			case git.ConfigGlobal:
				rows[i].GlobalValue, rows[i].GlobalSet = st.Value, true
			}
		}
		return rows, nil
	})
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/domain/ -run TestGitConfigRows`
Expected: PASS (2 tests). Then `go test ./internal/domain/ ./internal/model/`.

- [ ] **Step 5: gofmt and commit**

```bash
gofmt -w internal/model/gitconfig.go internal/domain/gitconfig.go internal/domain/gitconfig_test.go
git add internal/model/gitconfig.go internal/domain/gitconfig.go internal/domain/gitconfig_test.go
git commit -m "feat(domain): GitConfigRows — catalog ⋈ set values, case-insensitive

One row per git-help-c key plus set keys outside the catalog; local/global
kept distinct with explicit set flags (empty string is still set).

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
```

---

### Task 4: `internal/gitconfdocs` — the curated table + staleness gate

**Files:**
- Create: `internal/gitconfdocs/docs.go`
- Create: `internal/gitconfdocs/docs_test.go`
- Modify: `internal/archtest/import_guard_test.go` (DAG leaf)

**Interfaces:**
- Consumes: nothing (pure data; stdlib only).
- Produces (Tasks 5–6 rely on):
  - `type Kind int` with `KindBool`, `KindEnum`, `KindString`, `KindInt`
  - `type Doc struct { Key string; Kind Kind; Default string; Desc string; Options []string }` (`Options` only for `KindEnum`; `KindBool` implies options `true`/`false`)
  - `func Lookup(key string) *Doc` — case-insensitive; nil for non-curated
  - `func All() []Doc` — table order

- [ ] **Step 1: Write the failing test**

Create `internal/gitconfdocs/docs_test.go`:

```go
package gitconfdocs

import (
	"os/exec"
	"strings"
	"testing"
)

func TestLookupIsCaseInsensitive(t *testing.T) {
	if Lookup("fetch.writeCommitGraph") == nil {
		t.Fatal("curated key missing by exact case")
	}
	if Lookup("fetch.writecommitgraph") == nil {
		t.Fatal("lookup must be case-insensitive (git lowercases set keys)")
	}
	if Lookup("no.such.key") != nil {
		t.Fatal("non-curated key must return nil")
	}
}

func TestTableShape(t *testing.T) {
	docs := All()
	if len(docs) < 55 {
		t.Fatalf("curated table has %d entries, want ~60", len(docs))
	}
	seen := map[string]bool{}
	for _, d := range docs {
		lk := strings.ToLower(d.Key)
		if seen[lk] {
			t.Fatalf("duplicate curated key %q", d.Key)
		}
		seen[lk] = true
		if d.Desc == "" {
			t.Fatalf("%s: description required", d.Key)
		}
		if d.Kind == KindEnum && len(d.Options) < 2 {
			t.Fatalf("%s: enum kind needs options", d.Key)
		}
		if d.Kind != KindEnum && len(d.Options) != 0 {
			t.Fatalf("%s: options only belong on enums", d.Key)
		}
	}
}

// TestCuratedKeysExistInGitCatalog is the staleness gate: every curated key
// must still be a real key in THIS machine's `git help -c` output, so a git
// rename/removal breaks the build here instead of shipping stale docs.
func TestCuratedKeysExistInGitCatalog(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	out, err := exec.Command("git", "help", "-c").Output()
	if err != nil {
		t.Skipf("git help -c unavailable: %v", err)
	}
	catalog := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			catalog[strings.ToLower(line)] = true
		}
	}
	for _, d := range All() {
		if !catalog[strings.ToLower(d.Key)] {
			t.Errorf("curated key %q not in git help -c — stale table entry", d.Key)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/gitconfdocs/`
Expected: FAIL to build — package doesn't exist.

- [ ] **Step 3: Implement**

Create `internal/gitconfdocs/docs.go`. The table below is the deliverable — every key was verified against `git help -c` when this plan was written; the staleness test re-verifies at build time. `boolDoc`/`enumDoc` helpers keep the table readable:

```go
// Package gitconfdocs is the curated slice of git's config catalog behind
// the TUI's git-config explorer: for ~60 common keys it knows the real
// default, a one-line description, and the value kind (so bools/enums get
// an option picker instead of a free-text field). Pure data — no git, no
// TUI imports; a staleness test asserts every curated key still exists in
// `git help -c`.
package gitconfdocs

import "strings"

// Kind tells the explorer which editor a key gets.
type Kind int

const (
	KindBool   Kind = iota // option picker: true/false
	KindEnum               // option picker: Options
	KindString             // free-text field
	KindInt                // free-text field (digits)
)

// Doc is one curated key: real default, one-line description, value kind.
type Doc struct {
	Key     string
	Kind    Kind
	Default string
	Desc    string
	Options []string // KindEnum only
}

func boolDoc(key, def, desc string) Doc {
	return Doc{Key: key, Kind: KindBool, Default: def, Desc: desc}
}

func enumDoc(key, def, desc string, options ...string) Doc {
	return Doc{Key: key, Kind: KindEnum, Default: def, Desc: desc, Options: options}
}

// docs is the curated table, grouped by section. Defaults are git's own
// (not gg's opinions); "(none)" = git has no default for the key.
var docs = []Doc{
	boolDoc("add.ignoreErrors", "false", "continue adding files when some fail to add"),
	boolDoc("advice.detachedHead", "true", "explain what a detached HEAD is when you enter one"),
	{Key: "blame.date", Kind: KindString, Default: "iso", Desc: "date format used by git blame"},
	enumDoc("branch.autoSetupRebase", "never", "auto-configure new branches to pull with rebase", "never", "local", "remote", "always"),
	{Key: "checkout.defaultRemote", Kind: KindString, Default: "(none)", Desc: "remote to prefer when checkout <branch> is ambiguous (e.g. origin)"},
	enumDoc("color.ui", "auto", "when git colors its output", "false", "true", "always", "auto"),
	boolDoc("commit.gpgSign", "false", "GPG-sign every commit"),
	{Key: "commit.template", Kind: KindString, Default: "(none)", Desc: "file used as the starting commit message"},
	boolDoc("commit.verbose", "false", "show the diff in the commit message editor"),
	enumDoc("core.autocrlf", "false", "convert CRLF↔LF on checkout/checkin (Windows interop)", "true", "false", "input"),
	boolDoc("core.commitGraph", "true", "use the commit-graph file to speed up commit walks"),
	{Key: "core.compression", Kind: KindInt, Default: "-1", Desc: "zlib level for object compression (-1 = zlib default, 0-9)"},
	{Key: "core.editor", Kind: KindString, Default: "(none)", Desc: "editor for commit messages etc. (falls back to $EDITOR, then vi)"},
	enumDoc("core.eol", "native", "line endings for text files in the working tree", "lf", "crlf", "native"),
	{Key: "core.excludesFile", Kind: KindString, Default: "~/.config/git/ignore", Desc: "extra gitignore file applied to every repo"},
	boolDoc("core.fileMode", "true", "track the executable bit of files"),
	boolDoc("core.fsmonitor", "false", "use a filesystem monitor daemon to speed up git status"),
	{Key: "core.hooksPath", Kind: KindString, Default: ".git/hooks", Desc: "directory git runs hooks from"},
	boolDoc("core.ignoreCase", "false", "treat the filesystem as case-insensitive (set by git init)"),
	{Key: "core.pager", Kind: KindString, Default: "less", Desc: "pager for long output"},
	boolDoc("core.preloadIndex", "true", "preload index contents in parallel for faster status"),
	boolDoc("core.quotePath", "true", "escape non-ASCII path bytes in output (gg disables per-command)"),
	boolDoc("core.sparseCheckout", "false", "enable sparse-checkout (populate only part of the tree)"),
	boolDoc("core.symlinks", "true", "create symlinks in the working tree (false = plain files)"),
	enumDoc("core.untrackedCache", "keep", "cache untracked-file scans to speed up git status", "true", "false", "keep"),
	{Key: "core.whitespace", Kind: KindString, Default: "(none)", Desc: "whitespace problems git diff/apply should flag"},
	{Key: "credential.helper", Kind: KindString, Default: "(none)", Desc: "program that stores/supplies HTTPS credentials"},
	enumDoc("diff.algorithm", "myers", "diff algorithm", "myers", "minimal", "patience", "histogram"),
	enumDoc("diff.colorMoved", "no", "highlight moved lines in diffs", "no", "default", "plain", "blocks", "zebra", "dimmed-zebra"),
	enumDoc("diff.renames", "true", "rename detection in diffs", "false", "true", "copies"),
	{Key: "fetch.parallel", Kind: KindInt, Default: "1", Desc: "number of parallel children for fetching submodules/multiple remotes (0 = reasonable default)"},
	boolDoc("fetch.prune", "false", "prune deleted remote branches on every fetch"),
	boolDoc("fetch.pruneTags", "false", "also prune deleted remote tags on fetch (with fetch.prune)"),
	boolDoc("fetch.writeCommitGraph", "false", "refresh the commit-graph file after every fetch (big-repo speedup; gg's notification center sets this)"),
	{Key: "gc.auto", Kind: KindInt, Default: "6700", Desc: "loose-object threshold that triggers auto gc (0 = disable)"},
	boolDoc("gc.writeCommitGraph", "true", "rewrite the commit-graph file when gc runs"),
	enumDoc("gpg.format", "openpgp", "signature format for signing", "openpgp", "x509", "ssh"),
	boolDoc("grep.lineNumber", "false", "show line numbers in git grep by default"),
	{Key: "help.autoCorrect", Kind: KindString, Default: "0", Desc: "typo handling for subcommands (0=suggest, N=run after N/10s, immediate, prompt, never)"},
	{Key: "http.postBuffer", Kind: KindInt, Default: "1048576", Desc: "buffer size before HTTPS pushes switch to chunked transfer (bytes)"},
	{Key: "init.defaultBranch", Kind: KindString, Default: "master", Desc: "branch name git init creates"},
	boolDoc("log.abbrevCommit", "false", "show abbreviated commit hashes in git log by default"),
	enumDoc("log.date", "default", "date format in git log", "default", "relative", "local", "iso", "iso-strict", "rfc", "short", "raw", "human"),
	boolDoc("maintenance.auto", "true", "run automatic background maintenance after some commands"),
	enumDoc("merge.conflictStyle", "merge", "conflict marker style (zdiff3 adds the base, minimized)", "merge", "diff3", "zdiff3"),
	enumDoc("merge.ff", "true", "allow fast-forward merges (only = refuse real merges)", "true", "false", "only"),
	{Key: "merge.tool", Kind: KindString, Default: "(none)", Desc: "tool git mergetool launches"},
	{Key: "pack.threads", Kind: KindInt, Default: "0", Desc: "threads for pack compression (0 = one per CPU)"},
	enumDoc("pull.ff", "true", "fast-forward behavior for pull (only = refuse merge pulls)", "true", "false", "only"),
	enumDoc("pull.rebase", "false", "rebase instead of merge when pulling", "false", "true", "merges", "interactive"),
	boolDoc("push.autoSetupRemote", "false", "auto set upstream on first push of a new branch"),
	enumDoc("push.default", "simple", "what git push pushes with no refspec", "nothing", "current", "upstream", "tracking", "simple", "matching"),
	boolDoc("push.followTags", "false", "also push annotated tags reachable from pushed commits"),
	boolDoc("rebase.autoStash", "false", "stash/unstash automatically around rebase (gg's SmartPull does its own)"),
	boolDoc("rebase.autoSquash", "false", "auto-reorder fixup!/squash! commits in interactive rebase"),
	boolDoc("rebase.updateRefs", "false", "move stacked branch refs along when rebasing"),
	boolDoc("rerere.enabled", "false", "remember conflict resolutions and replay them"),
	{Key: "safe.directory", Kind: KindString, Default: "(none)", Desc: "repo paths exempt from the dubious-ownership check (* = all)"},
	enumDoc("status.showUntrackedFiles", "normal", "how much untracked content git status lists", "no", "normal", "all"),
	boolDoc("submodule.recurse", "false", "recurse into submodules for checkout/pull/etc."),
	{Key: "tag.sort", Kind: KindString, Default: "refname", Desc: "default sort order for git tag (e.g. -version:refname)"},
	{Key: "user.email", Kind: KindString, Default: "(none)", Desc: "author/committer email (gg: Settings → Identity & profiles)"},
	{Key: "user.name", Kind: KindString, Default: "(none)", Desc: "author/committer name (gg: Settings → Identity & profiles)"},
	{Key: "user.signingKey", Kind: KindString, Default: "(none)", Desc: "key id used for signed commits/tags"},
}

// byLower indexes the table for case-insensitive lookup (git lowercases set
// keys; the catalog and this table are camelCase).
var byLower = func() map[string]*Doc {
	m := make(map[string]*Doc, len(docs))
	for i := range docs {
		m[strings.ToLower(docs[i].Key)] = &docs[i]
	}
	return m
}()

// Lookup returns the curated doc for key (any case), or nil when the key is
// not curated (the explorer renders it read-only with default "—").
func Lookup(key string) *Doc { return byLower[strings.ToLower(key)] }

// All returns the curated table in its grouped order.
func All() []Doc { return docs }
```

- [ ] **Step 4: Register the DAG leaf and run the tests**

In `internal/archtest/import_guard_test.go`'s `TestLayeringDAG` cases map, add beside `promptstate`:

```go
		"gitconfdocs": {"git", "engine", "domain", "tui", "cli", "app"},
```

Run: `go test ./internal/gitconfdocs/ ./internal/archtest/`
Expected: PASS — including the staleness gate (all 64 keys exist in this machine's `git help -c`; if any fails, REMOVE or fix that table entry, don't skip the test).

- [ ] **Step 5: gofmt and commit**

```bash
gofmt -w internal/gitconfdocs/ internal/archtest/import_guard_test.go
git add internal/gitconfdocs/ internal/archtest/import_guard_test.go
git commit -m "feat(gitconfdocs): curated git-config docs — 64 keys, defaults, kinds

Pure data behind the explorer's curated rows (description + real default +
bool/enum/string/int kind for the editor). A staleness test asserts every
curated key still exists in git help -c, so a git rename breaks the build
instead of shipping stale docs.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
```

---

### Task 5: the explorer popup — read-only (menu, load, list, filter)

**Files:**
- Create: `internal/tui/gitconfig_popup.go`
- Modify: `internal/tui/settings_popup.go` (menu constant + slice + enter case)
- Modify: `internal/tui/model.go` (one Update case + one Model field)
- Test: `internal/tui/gitconfig_popup_test.go`

**Interfaces:**
- Consumes: `svc.GitConfigRows` (Task 3), `gitconfdocs.Lookup` (Task 4 — the TUI imports it directly; it is a pure-data leaf like `commitgraph`), the `repoPopup` list/filter pattern (`internal/tui/repo_popup.go` — read it first: `filterMotion`, navigation-first `/`, `renderWindow`, `popupBox`), `layerOf`, `settingsModel(t)`/`keyMsg` test helpers.
- Produces (Task 6 relies on):
  - `type gitConfigPopup struct { rows []model.GitConfigRow; loading bool; query string; filtering bool; sel int; mode dispMode; hscroll int }` implementing `layer`. (The `edit *configEdit` editor field is added by Task 6 — this task's struct does NOT have it.)
  - Test helper `openExplorer(t, m) Model` in the test file — Task 6's tests reuse it (same package).
  - `func (m Model) openGitConfigExplorer() (Model, tea.Cmd)` — pushes the popup loading + dispatches the rows read.
  - `gitConfigRowsMsg{gen int; rows []model.GitConfigRow; err error}` + Model field `gitConfigGen int` (bumped per dispatch; stale results dropped).
  - `func (p *gitConfigPopup) visible() []model.GitConfigRow` — case-insensitive substring filter over key + both values.
  - `settingsMenuGitConfig = "Git config explorer"` appended to `settingsMenu`.

**Row rendering:** four columns — `key` (36 cells) | `local` (18) | `global` (18) | `default` (rest). Unset scope cells render `(unset)`; the default cell shows `gitconfdocs.Lookup(key).Default` for curated keys, `—` otherwise. Under the list, one line shows the SELECTED row's curated description (blank line for non-curated). Title shows `Git config (⏳ loading…)` until rows land, then `Git config (870 keys)`. Tall popup: rows capped at `termH - 12` (the Session-errors precedent), min 3. Wide: `popupWideInnerWidth`.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/gitconfig_popup_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

func explorerRows() []model.GitConfigRow {
	return []model.GitConfigRow{
		{Key: "add.ignoreErrors"},
		{Key: "fetch.writeCommitGraph", LocalValue: "true", LocalSet: true},
		{Key: "user.name", GlobalValue: "Ada L", GlobalSet: true},
		{Key: "alias.lg", LocalValue: "log --graph", LocalSet: true}, // non-curated
	}
}

// openExplorer drives Settings → "Git config explorer" → enter, then delivers
// the rows as if the background read landed.
func openExplorer(t *testing.T, m Model) Model {
	t.Helper()
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	p := layerOf[*settingsPopup](m)
	if p == nil {
		t.Fatal("settings popup did not open")
	}
	for i, entry := range settingsMenu {
		if entry == settingsMenuGitConfig {
			p.menuSel = i
		}
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(Model)
	if layerOf[*gitConfigPopup](m) == nil {
		t.Fatal("enter must open the explorer")
	}
	u, _ = m.Update(gitConfigRowsMsg{gen: m.gitConfigGen, rows: explorerRows()})
	return u.(Model)
}

func TestExplorerOpensLoadsAndRenders(t *testing.T) {
	m, _ := settingsModel(t)
	m = openExplorer(t, m)
	out := m.View()
	for _, want := range []string{"fetch.writeCommitGraph", "(unset)", "Ada L", "—"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q:\n%s", want, out)
		}
	}
}

func TestExplorerStaleRowsDropped(t *testing.T) {
	m, _ := settingsModel(t)
	m = openExplorer(t, m)
	u, _ := m.Update(gitConfigRowsMsg{gen: m.gitConfigGen - 1, rows: []model.GitConfigRow{{Key: "stale.key"}}})
	if strings.Contains(u.(Model).View(), "stale.key") {
		t.Fatal("a stale-generation rows msg must be dropped")
	}
}

func TestExplorerFilterMovesWhileTyping(t *testing.T) {
	m, _ := settingsModel(t)
	m = openExplorer(t, m)
	p := layerOf[*gitConfigPopup](m)
	u, _ := m.Update(keyMsg("/"))
	m = u.(Model)
	for _, r := range "user" {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = u.(Model)
	}
	vis := p.visible()
	if len(vis) != 1 || vis[0].Key != "user.name" {
		t.Fatalf("filtered view = %+v, want just user.name", vis)
	}
	// esc clears the filter and stays open; second esc closes.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	if layerOf[*gitConfigPopup](m) == nil {
		t.Fatal("esc while filtering must only exit the filter")
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if layerOf[*gitConfigPopup](u.(Model)) != nil {
		t.Fatal("esc in navigation mode must close the explorer")
	}
}

func TestExplorerShowsCuratedDescription(t *testing.T) {
	m, _ := settingsModel(t)
	m = openExplorer(t, m)
	p := layerOf[*gitConfigPopup](m)
	for i, r := range p.visible() {
		if r.Key == "fetch.writeCommitGraph" {
			p.sel = i
		}
	}
	if out := m.View(); !strings.Contains(out, "notification center sets this") {
		t.Fatalf("selected curated row must show its description:\n%s", out)
	}
}

func TestExplorerSwallowsGlobalKeys(t *testing.T) {
	m, _ := settingsModel(t)
	m = openExplorer(t, m)
	before := len(m.layers.entries)
	for _, k := range []string{"p", "!", "G", ",", "R"} {
		u, _ := m.Update(keyMsg(k))
		m = u.(Model)
	}
	if len(m.layers.entries) != before || layerOf[*gitConfigPopup](m) == nil {
		t.Fatal("explorer must swallow global keys")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run TestExplorer`
Expected: FAIL to build.

- [ ] **Step 3: Implement `internal/tui/gitconfig_popup.go`**

Follow `repoPopup` closely (filter/nav split, `filterMotion`, `renderWindow`). The essential shape (write the full file — this is the skeleton with every behavior named):

```go
package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/gitconfdocs"
	"github.com/homeend/gigagit/internal/model"
)

// gitConfigPopup is the Settings → "Git config explorer": every key git
// knows (git help -c), the explicitly-set local/global values, and — for
// curated keys (internal/gitconfdocs) — the real default and a description.
// Navigation-first like the repo switcher: / filters (move-while-typing),
// z cycles display modes, esc closes.
type gitConfigPopup struct {
	rows      []model.GitConfigRow
	loading   bool
	query     string
	filtering bool
	sel       int
	mode      dispMode
	hscroll   int
}

// gitConfigRowsMsg carries the merged rows; gen guards repo switches and
// reopen races.
type gitConfigRowsMsg struct {
	gen  int
	rows []model.GitConfigRow
	err  error
}

// openGitConfigExplorer pushes the loading popup and reads the rows off the
// UI thread.
func (m Model) openGitConfigExplorer() (Model, tea.Cmd) {
	m.gitConfigGen++
	m = m.pushLayer(&gitConfigPopup{loading: true})
	svc := m.svc
	gen := m.gitConfigGen
	return m, func() tea.Msg {
		rows, err := svc.GitConfigRows(context.Background())
		return gitConfigRowsMsg{gen: gen, rows: rows, err: err}
	}
}

// moveSel moves the cursor by d, clamped to the filtered view.
func (p *gitConfigPopup) moveSel(d int) {
	n := p.sel + d
	if hi := len(p.visible()) - 1; n > hi {
		n = hi
	}
	if n < 0 {
		n = 0
	}
	p.sel = n
}

// visible returns the filtered rows (case-insensitive substring over the key
// and both values).
func (p *gitConfigPopup) visible() []model.GitConfigRow {
	if p.query == "" {
		return p.rows
	}
	q := strings.ToLower(p.query)
	out := make([]model.GitConfigRow, 0, len(p.rows))
	for _, r := range p.rows {
		if strings.Contains(strings.ToLower(r.Key), q) ||
			strings.Contains(strings.ToLower(r.LocalValue), q) ||
			strings.Contains(strings.ToLower(r.GlobalValue), q) {
			out = append(out, r)
		}
	}
	return out
}

func (p *gitConfigPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	// ctrl+c quit; filtering sub-mode EXACTLY like repoPopup.update (filterMotion,
	// esc clears filter, enter commits filter, backspace/space/runes edit query,
	// sel resets to 0 on query change); navigation mode: z/shift+arrows display
	// modes, esc closes (popLayer), up/down/j/k/pgup/pgdn move, "/" enters
	// filtering. Swallow everything else. (Task 6 adds l/g/u here.)
}

func (p *gitConfigPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *gitConfigPopup) box(m Model) string {
	w, termH := m.overlayDims()
	inner := popupWideInnerWidth(w)
	textW := popupTextWidth(inner)
	// Title with count / loading spinner; filter suffix like repoPopup.
	// Column layout: key 36 | local 18 | global 18 | default rest. Cells:
	// value when set, dim "(unset)" otherwise; default = curated Default or "—".
	// Rows via renderWindow(wr, winOpts{w: textW, h: cap, mode, anchor: p.sel, hscroll});
	// cap = termH - 12 floored at 3. Below the list: the selected row's curated
	// Desc (wrapped) or "" for non-curated. Hint line:
	// "[/] filter  [z] mode  [esc] close" (Task 6 extends it).
}

// configCell renders one scope cell: the value, or a dim "(unset)".
func configCell(v string, set bool, width int) string { /* padRight + unsetStyle */ }

var unsetStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
```

Care points the implementer must honor:
- `padRight`/`lipgloss.Width` for display-aware padding (multi-byte values); truncate long values to their column (`truncate` helper exists).
- The description line uses `gitconfdocs.Lookup(row.Key)` — nil for non-curated.
- `visible()` must not allocate per frame beyond the filter slice (this popup holds ~900 rows; the existing `renderWindow` handles windowing).

In `internal/tui/settings_popup.go`: add `settingsMenuGitConfig = "Git config explorer"` to the constants, append to `settingsMenu`, and add the enter case:

```go
			case settingsMenuGitConfig:
				return m.openGitConfigExplorer()
```

In `internal/tui/model.go`: add the Model field `gitConfigGen int // stale-drop guard for explorer row loads` (beside `noticeGen`), and the Update case:

```go
	case gitConfigRowsMsg:
		if msg.gen != m.gitConfigGen {
			return m, nil // stale: reopened or repo-switched since dispatch
		}
		if p := layerOf[*gitConfigPopup](m); p != nil {
			p.loading = false
			if msg.err != nil {
				m.statusMsg = "git config explorer: " + friendlyOpError(msg.err)
				return m.popLayer(), nil
			}
			p.rows = msg.rows
			if n := len(p.visible()); p.sel >= n && n > 0 {
				p.sel = n - 1
			}
		}
		return m, nil
```

(Verify `friendlyOpError` exists and fits — if it's op-specific, use `msg.err.Error()`.)

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/tui/ -run TestExplorer` then the FULL `go test ./internal/tui/` (settings menu grew — stage-1/2 settings tests must stay green).
Expected: PASS.

- [ ] **Step 5: gofmt and commit**

```bash
gofmt -w internal/tui/gitconfig_popup.go internal/tui/gitconfig_popup_test.go internal/tui/settings_popup.go internal/tui/model.go
git add internal/tui/gitconfig_popup.go internal/tui/gitconfig_popup_test.go internal/tui/settings_popup.go internal/tui/model.go
git commit -m "feat(tui): git-config explorer — searchable catalog with local/global/default columns

Settings → 'Git config explorer': every git help -c key plus set values
(explicit dim '(unset)'), curated keys show the real default + description;
/ filters move-while-typing, z cycles modes, esc closes. Read-only so far.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
```

---

### Task 6: set / unset on curated rows (`l` / `g` / `u`)

**Files:**
- Modify: `internal/tui/gitconfig_popup.go` (editor sub-state + keys + write cmd)
- Test: `internal/tui/gitconfig_write_test.go`

**Interfaces:**
- Consumes: `engine.SetGitConfig{Key, Value, Global, Unset}` (Task 2), `gitconfdocs.Doc.Kind/Options` (Task 4), `svc.Execute` via the `stageCmd` precedent (`internal/tui/op.go:100` — a fast decision-free op runs synchronously inside one `tea.Cmd`, no `startOp` busy machinery), `textfield` (`newTextField`, `HandleEditKey`, `Value`, `viewField` — see `commit_name_popup.go`), `m.repoHealthCmd(m.noticeGen)` (stage 2 — a config write can change repo health).
- Produces:
  - `type configEdit struct { key string; doc *gitconfdocs.Doc; global bool; unset bool; field textfield; optSel int }` — field `edit *configEdit` added to `gitConfigPopup` (nil = browsing).
  - `func (m Model) gitConfigWriteCmd(op engine.SetGitConfig) tea.Cmd` — Execute + re-read rows + `gitConfigRowsMsg{gen}` carrying the fresh rows; batched with `m.repoHealthCmd(m.noticeGen)`.

**Behavior contract:**
- `l` / `g` on a curated row open the editor targeting local/global scope: `KindBool`/`KindEnum` → an option list (Options; bools are `true`/`false`) with the CURRENT scope value pre-selected when set, else the default; `KindString`/`KindInt` → a `textfield` pre-filled with the current scope value (empty when unset). Enter writes (`gitConfigWriteCmd`), esc cancels back to browsing. `KindInt` accepts digits (plus a leading `-`) only.
- `u` on a curated row opens a small chooser offering ONLY the scopes that are set (`Unset local` / `Unset global` / `Cancel`; nothing set → statusMsg "nothing to unset"). Enter runs the Unset op; esc cancels.
- `l`/`g`/`u` on a NON-curated row: `m.statusMsg = "read-only: not a curated key (edit via git config)"` — no editor.
- While `edit != nil`, ALL keys route to the editor (the filter `/` etc. are inert).
- After a successful write the popup shows the refreshed rows (same `gitConfigRowsMsg` path — the write cmd bumps nothing; it reuses the CURRENT `m.gitConfigGen` so the refresh lands unless the popup was reopened) and `m.statusMsg` carries the op summary.
- Hint line extends to: `[l] set local  [g] set global  [u] unset  [/] filter  [z] mode  [esc] close`.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/gitconfig_write_test.go`:

```go
package tui

import (
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// selectExplorerRow moves the explorer selection to key (must be visible).
func selectExplorerRow(t *testing.T, m Model, key string) Model {
	t.Helper()
	p := layerOf[*gitConfigPopup](m)
	if p == nil {
		t.Fatal("explorer not open")
	}
	for i, r := range p.visible() {
		if r.Key == key {
			p.sel = i
			return m
		}
	}
	t.Fatalf("row %q not visible", key)
	return m
}

// drainCmd executes cmds until msgs stop, feeding each back into Update —
// enough to drive the synchronous write cmd + the rows refresh it returns.
func drainCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for i := 0; i < 20 && cmd != nil; i++ {
		msg := cmd()
		if msg == nil {
			break
		}
		u, next := m.Update(msg)
		m = u.(Model)
		cmd = next
	}
	return m
}

func TestExplorerSetLocalBoolWritesConfig(t *testing.T) {
	m, dir := settingsModel(t)
	m = openExplorer(t, m)
	m = selectExplorerRow(t, m, "fetch.writeCommitGraph")
	u, _ := m.Update(keyMsg("l"))
	m = u.(Model)
	p := layerOf[*gitConfigPopup](m)
	if p.edit == nil {
		t.Fatal("l on a curated bool must open the option editor")
	}
	// Option list: pick "true" (bool options are [true false]; optSel starts on
	// the current/default — move to be explicit).
	p.edit.optSel = 0
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drainCmd(t, u.(Model), cmd)

	out, err := exec.Command("git", "-C", dir, "config", "--local", "fetch.writeCommitGraph").Output()
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		t.Fatalf("local value = %q, %v; want true", out, err)
	}
	if p := layerOf[*gitConfigPopup](m); p == nil || p.edit != nil {
		t.Fatal("after writing, the explorer stays open in browsing mode")
	}
}

func TestExplorerSetGlobalStringWritesGlobalOnly(t *testing.T) {
	m, dir := settingsModel(t)
	m = openExplorer(t, m)
	m = selectExplorerRow(t, m, "user.name")
	u, _ := m.Update(keyMsg("g"))
	m = u.(Model)
	p := layerOf[*gitConfigPopup](m)
	if p.edit == nil || p.edit.doc.Key != "user.name" || !p.edit.global {
		t.Fatalf("g must open a global editor, got %+v", p.edit)
	}
	for _, r := range "Test Person" {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = u.(Model)
	}
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drainCmd(t, u.(Model), cmd)

	// The test harness isolates GIT_CONFIG_GLOBAL (settingsModel's repo env
	// setup) — verify the write really landed at global scope, not local.
	out, _ := exec.Command("git", "-C", dir, "config", "--global", "user.name").Output()
	if strings.TrimSpace(string(out)) != "Test Person" {
		t.Fatalf("global user.name = %q, want 'Test Person'", out)
	}
	if out, err := exec.Command("git", "-C", dir, "config", "--local", "user.name").Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		t.Fatal("local scope must stay untouched")
	}
}

func TestExplorerUnsetOffersOnlySetScopes(t *testing.T) {
	m, dir := settingsModel(t)
	if err := exec.Command("git", "-C", dir, "config", "--local", "fetch.writeCommitGraph", "true").Run(); err != nil {
		t.Fatal(err)
	}
	m = openExplorer(t, m)
	// Reload rows so the local value is visible to the popup state.
	p := layerOf[*gitConfigPopup](m)
	for i := range p.rows {
		if p.rows[i].Key == "fetch.writeCommitGraph" {
			p.rows[i].LocalSet, p.rows[i].LocalValue = true, "true"
		}
	}
	m = selectExplorerRow(t, m, "fetch.writeCommitGraph")
	u, _ := m.Update(keyMsg("u"))
	m = u.(Model)
	if p.edit == nil || !p.edit.unset {
		t.Fatal("u on a set curated row must open the unset chooser")
	}
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // first option = Unset local
	m = drainCmd(t, u.(Model), cmd)
	if err := exec.Command("git", "-C", dir, "config", "--local", "fetch.writeCommitGraph").Run(); err == nil {
		t.Fatal("local key must be unset")
	}
}

func TestExplorerWriteOnNonCuratedIsReadOnly(t *testing.T) {
	m, _ := settingsModel(t)
	m = openExplorer(t, m)
	m = selectExplorerRow(t, m, "alias.lg")
	u, _ := m.Update(keyMsg("l"))
	m = u.(Model)
	if p := layerOf[*gitConfigPopup](m); p.edit != nil {
		t.Fatal("non-curated rows are read-only")
	}
	if !strings.Contains(m.statusMsg, "read-only") {
		t.Fatalf("statusMsg = %q, want a read-only explanation", m.statusMsg)
	}
}

func TestExplorerEscCancelsEditor(t *testing.T) {
	m, _ := settingsModel(t)
	m = openExplorer(t, m)
	m = selectExplorerRow(t, m, "user.name")
	u, _ := m.Update(keyMsg("l"))
	m = u.(Model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	p := layerOf[*gitConfigPopup](m)
	if p == nil || p.edit != nil {
		t.Fatal("esc must cancel the editor back to browsing, not close the popup")
	}
}
```

Note: `settingsModel(t)` must give an isolated `GIT_CONFIG_GLOBAL` — check; if it does not, add `t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))` + `t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)` at the top of every test here (before `settingsModel`) so the developer's real global config is never read or written.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run 'TestExplorerSet|TestExplorerUnset|TestExplorerWrite|TestExplorerEscCancels'`
Expected: FAIL to build — `edit`/`configEdit` undefined.

- [ ] **Step 3: Implement** — extend `internal/tui/gitconfig_popup.go`:

```go
// configEdit is the in-popup editor state for one curated key: an option
// list for bool/enum kinds, a text field for string/int, or the unset-scope
// chooser. nil edit = browsing.
type configEdit struct {
	key     string
	doc     *gitconfdocs.Doc
	global  bool
	unset   bool      // the unset chooser (options built from set scopes)
	options []string  // option-list editors (incl. unset chooser labels)
	optSel  int
	field   textfield // string/int kinds
	useField bool
}
```

Key handling added to `update` (navigation mode), BEFORE the existing switch — when `p.edit != nil` route everything to an `updateEdit` helper first:
- `l`/`g`: `row := p.visible()[p.sel]`; `doc := gitconfdocs.Lookup(row.Key)`; nil → statusMsg read-only. Build `configEdit{key: doc.Key, doc: doc, global: msg == "g"}`; for `KindBool` options `[]string{"true", "false"}`, `KindEnum` `doc.Options` (pre-select current scope value when set else `doc.Default`); for `KindString`/`KindInt` `useField: true`, `field: newTextField(currentScopeValue)`.
- `u`: build the chooser from set scopes only (`Unset local` when `row.LocalSet`, `Unset global` when `row.GlobalSet`, always `Cancel`); none set → statusMsg `"nothing to unset"`.
- `updateEdit`: esc → `p.edit = nil`; up/down move `optSel` (option editors); runes → `field.HandleEditKey` (field editors; `KindInt` filters to digits/`-`); enter → build the op and dispatch:

```go
// gitConfigWriteCmd runs one config write synchronously (the stageCmd
// pattern — fast + decision-free, no busy-line machinery) and re-reads the
// rows so the popup refreshes in the same message.
func (m Model) gitConfigWriteCmd(op engine.SetGitConfig) tea.Cmd {
	svc := m.svc
	gen := m.gitConfigGen
	return func() tea.Msg {
		if _, err := svc.Execute(context.Background(), op, nil, nil); err != nil {
			return gitConfigRowsMsg{gen: gen, err: err}
		}
		rows, err := svc.GitConfigRows(context.Background())
		return gitConfigRowsMsg{gen: gen, rows: rows, err: err}
	}
}
```

Enter dispatch returns `m, tea.Batch(m.gitConfigWriteCmd(op), m.repoHealthCmd(m.noticeGen))` (a config write can change repo health — e.g. unsetting `fetch.writeCommitGraph` re-arms the commit-graph notice check on next load) and sets `p.edit = nil`. On `gitConfigRowsMsg` with `err != nil` while the popup is open, do NOT close: keep the stale rows and set `m.statusMsg` (adjust the Task 5 handler: only `popLayer` when `p.loading` was true — the initial load failing has nothing to show; a failed write still has the old rows).

Render: when `p.edit != nil`, the box shows the editor INSTEAD of the list — key + scope title, the option list (selectedRow highlight) or `viewField("value: ", p.edit.field, true, popupContentWidth(w))`, the curated description, and `[enter] save  [esc] cancel`. Update the browsing hint line to include `[l] set local  [g] set global  [u] unset`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/tui/ -run TestExplorer` (all explorer tests) then FULL `go test ./internal/tui/`.
Expected: PASS.

- [ ] **Step 5: gofmt and commit**

```bash
gofmt -w internal/tui/gitconfig_popup.go internal/tui/gitconfig_write_test.go
git add internal/tui/gitconfig_popup.go internal/tui/gitconfig_write_test.go
git commit -m "feat(tui): explorer set/unset on curated keys — l/g/u through SetGitConfig

Curated rows edit in place: bool/enum keys get an option picker, string/int
a text field, u a set-scopes-only unset chooser. Writes run synchronously
through domain.Execute (the stageCmd pattern) and refresh the rows + repo
health in one message; non-curated rows stay read-only.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
```

---

### Task 7: update the `updating-git-config-options` skill

**Files:**
- Modify: `.claude/skills/updating-git-config-options/SKILL.md`

**Interfaces:** consumes the shipped Task 1–6 shapes — verify every identifier against the real code before committing.

- [ ] **Step 1: Edit the skill.** Three changes:

1. In "The stack, bottom to top" item 1, extend the verbs sentence to cover the new reads + unset:

```markdown
   Also `ConfigUnset(ctx, scope, key) error` — `git config --unset`; exit 5
   (key absent) is a no-op success, so unset is idempotent. Read-side:
   `ConfigKeys(ctx)` (the `git help -c` catalog) and `ConfigListScoped(ctx)`
   (`git config --list --show-scope -z`; local/global only; -z survives
   multiline values; git lowercases set keys — join against the camelCase
   catalog case-insensitively).
```

2. In item 2, note the op's unset mode: `engine.SetGitConfig{Key, Value, Global, Unset}` — `Unset: true` removes the key (Value ignored); one op for set AND unset, per the spec's explicit decision.

3. Replace the stage-3 bullet in "The surfaces that share this path" with the real explorer + add a curated-table maintenance section:

```markdown
- The git-config explorer (Settings → "Git config explorer",
  `internal/tui/gitconfig_popup.go`): browse every catalog key with
  local/global/default columns; curated rows (`internal/gitconfdocs`) edit
  via `l`/`g`/`u` → `gitConfigWriteCmd` → `domain.Execute(SetGitConfig)`
  (the stageCmd synchronous pattern — config writes are fast and
  decision-free), then re-read rows + repo health in one message.

## Maintaining the curated table (internal/gitconfdocs)

- One `Doc` per key: `Key` (camelCase, exactly as `git help -c` prints it),
  `Kind` (bool/enum/string/int — picks the editor), `Default` (git's real
  default, `"(none)"` when git has none — never gg's opinion), `Desc` (one
  line), `Options` (enums only).
- `TestCuratedKeysExistInGitCatalog` is the staleness gate: every curated
  key must exist in the local `git help -c` (skipped when git is absent).
  If it fails after a git upgrade, fix or remove the entry — do not skip.
- Lookup is case-insensitive (`byLower`) because git lowercases set keys.
- Curated ⇒ writable in the explorer; think before adding keys whose
  values are dangerous to flip blindly (e.g. `core.fileMode` on the wrong
  filesystem) — the Desc should carry the caveat.
```

- [ ] **Step 2: Verify against reality**

Run: `grep -rn "ConfigUnset\|ConfigListScoped\|ConfigKeys\|gitConfigWriteCmd\|Unset" internal/git/config_explore.go internal/git/config.go internal/engine/set_git_config.go internal/tui/gitconfig_popup.go | head -15`
Expected: every identifier the skill names appears. Fix the skill on mismatch.

- [ ] **Step 3: Commit**

```bash
git add .claude/skills/updating-git-config-options/SKILL.md
git commit -m "docs(skills): updating-git-config-options — explorer surface + curated table

Adds the read verbs, the Unset op mode, the explorer's write path, and the
gitconfdocs maintenance rules (real defaults, staleness gate, case rules).

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
```

---

### Task 8: docs + full race gate + test binary

**Files:**
- Modify: `CHANGELOG.md` (top of `### Added` under `## [Unreleased]`)
- Modify: `README.md` (after the Notifications section)
- Modify: `CLAUDE.md` (package map: new `gitconfdocs` row; append to `git`, `engine`, `domain`, `tui` rows)

**Interfaces:** none — documents Tasks 1–7; verify wording against real code.

- [ ] **Step 1: CHANGELOG** (top of `### Added`):

```markdown
- **Git config explorer.** Settings (`,`) → "Git config explorer" opens a
  searchable, full-height view of every config key git knows (`git help -c`,
  ~870 keys) with columns key | local | global | default — unset scopes say
  `(unset)` explicitly. ~64 curated keys (`internal/gitconfdocs`) show git's
  real default plus a one-line description and edit in place: `l` sets local,
  `g` sets global, `u` unsets (choosing among set scopes); bools/enums get an
  option picker, strings/ints a text field. Writes run through the same
  `engine.SetGitConfig` op as the notification center (now with `Unset`);
  non-curated keys are read-only. `/` filters as you type, `z` cycles display
  modes.
```

- [ ] **Step 2: README** — after the `### Notifications` section:

```markdown
### Git config explorer

Settings (`,`) → **"Git config explorer"** lists every config key git knows
with what's set where: **key | local | global | default**, unset scopes shown
as an explicit `(unset)`. Around 64 common keys are curated — they show git's
real default and a one-line description, and can be edited right there:
**`l`** sets the repo-local value, **`g`** the global one, **`u`** unsets
(you pick which set scope); boolean and enum keys offer a picker, the rest a
text field. Everything else is read-only browsing (use `git config` for
exotic keys). `/` filters as you type; `esc` closes.
```

- [ ] **Step 3: CLAUDE.md** — add the package-map row (after `gitwatch`, alphabetical-ish placement near other pure packages is fine; match the table's 2-column format exactly):

```markdown
| `gitconfdocs` | Pure curated slice of git's config catalog (~64 keys: real default, one-line description, bool/enum/string/int kind) behind the git-config explorer's editable rows; `Lookup` is case-insensitive (git lowercases set keys). A staleness test asserts every curated key still exists in `git help -c` (skipped without git). Archtest DAG leaf; the TUI imports it directly like `commitgraph`/`textdiff`. |
```

And append inside these rows' cells (before each closing `|`), converting escaped backticks to real ones:
- `git` row: ` \`ConfigKeys\` (the \`git help -c\` catalog), \`ConfigListScoped\` (\`git config --list --show-scope -z\`; local/global only; -z survives multiline values), \`ConfigUnset\` (\`--unset\`; exit 5 = already unset = no-op).`
- `engine` row: ` \`SetGitConfig\` now also carries \`Unset bool\` — one op for set AND unset (Value ignored on unset).`
- `domain` row: ` \`GitConfigRows(ctx)\` — the explorer read-model: one row per catalog key ⋈ set local/global values (case-insensitive join — git lowercases set keys), non-catalog set keys appended.`
- `tui` row: ` **Git config explorer** (\`gitconfig_popup.go\`, Settings \`,\`): full-height searchable catalog (repoPopup-pattern \`/\` filter + \`z\` modes), columns key|local|global|default with explicit dim \`(unset)\`; curated rows (\`gitconfdocs\`) edit via \`l\`/\`g\`/\`u\` — option picker for bool/enum, textfield for string/int, unset chooser over set scopes — through \`gitConfigWriteCmd\` (stageCmd-style synchronous \`domain.Execute(SetGitConfig)\`) which re-reads rows + repo health in one message; non-curated rows read-only; gen-guarded loads (\`gitConfigGen\`).`

- [ ] **Step 4: Full verification**

```bash
./test.sh race
```

Expected: fully green (several minutes; run in the FOREGROUND with a 600000ms timeout; do not kill early).

- [ ] **Step 5: Commit and build**

```bash
git add CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: record the git-config explorer (stage 3)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
go build -o ./gg ./cmd/gg
```

Report the ABSOLUTE binary path `/mnt/t/others/gigagit/.claude/worktrees/git-config-explorer/gg` for manual verification (Settings → Git config explorer in any repo; try `/fetch.write` filter, `l` on `fetch.writeCommitGraph`, `u` on it, `l` on a non-curated `alias.*` row → read-only message). The user merges — do not merge.
