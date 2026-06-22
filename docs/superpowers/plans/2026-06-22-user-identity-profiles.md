# User Identity & App Profiles Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user view/edit the git identity (`user.name`/`user.email`) per-repo and globally, and manage named app-profiles (presets) they can apply onto either scope — all from the Settings popup.

**Architecture:** A new `internal/profile` side-store (two-scoped: global + per-repo, mirroring `internal/bookmark`) holds named presets. New thin `git config` read/write verbs in `internal/git`. A decision-free engine op `SetIdentity{Name,Email,Global}` is the single write path for both "edit identity" and "apply profile". A domain `Identity` query reads the current state. The TUI Settings popup gains an identity sub-surface. CLI is deferred.

**Tech Stack:** Go 1.26, Bubble Tea TUI, `pelletier/go-toml/v2`, shelling out to system `git` via `internal/gitcmd`/`internal/gitexec`.

## Global Constraints

- Module path: `github.com/gigagit/gg`. Go 1.26.
- A git verb is **one** git invocation, built with `gitcmd`, run via `r.Runner.Run`. Never shell out directly.
- Frontends (`internal/tui`, `internal/cli`) never import `internal/git`, `internal/profile`, `internal/bookmark`, or `internal/shelf` — they go through `internal/domain` (archtest-guarded).
- Operations are run via `domain.Execute`, never by assembling `OpDeps` by hand.
- TDD: write the failing test first, watch it fail, implement minimally, watch it pass, commit.
- `main` is the trunk. Work happens on branch `feat/user-identity-profiles` (already created).
- Tests use a real `git` in a `t.TempDir()` (`newRepo`/`newTestRepo` helpers) or `FakeRunner`.
- **Any test that writes _global_ git config MUST first isolate it** via `t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))` and `t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)`, or it will rewrite the developer's real `~/.gitconfig`. `newRepo`/`newTestRepo` do NOT isolate this.
- Run `./test.sh` (and `./test.sh race` before merge). Use `go test ./internal/<pkg>/...` for a single package during TDD.

---

### Task 1: `model` types + `internal/profile` two-scope store

**Files:**
- Create: `internal/model/profile.go`
- Create: `internal/profile/store.go`
- Create: `internal/profile/file_store.go`
- Test: `internal/profile/file_store_test.go`

**Interfaces:**
- Produces:
  - `model.ProfileScope` (`int`): `ProfileScopeGlobal`, `ProfileScopeRepo`; method `String() string` ("global"/"repo").
  - `model.Profile{ ID, Name, GitName, GitEmail string; Scope ProfileScope; Created time.Time }`.
  - `model.Identity{ GlobalName, GlobalEmail string; GlobalSet bool; LocalName, LocalEmail string; LocalSet bool; EffectiveName, EffectiveEmail string }`.
  - `profile.Store` interface: `Add(p model.Profile) (model.Profile, error)`, `Get(id string) (model.Profile, error)`, `List() ([]model.Profile, error)`, `Remove(id string) error`.
  - `profile.NewFileStore(root string, scope model.ProfileScope) *FileStore`.
  - `profile.ErrNotFound` (`error`).
  - `profile.ProfileID(name string) string` — `slug(name)`.

- [ ] **Step 1: Write `internal/model/profile.go`**

```go
package model

import "time"

// ProfileScope distinguishes a global app-profile (available in every repo)
// from a repo-specific one (only the repo it was created in).
type ProfileScope int

const (
	ProfileScopeGlobal ProfileScope = iota
	ProfileScopeRepo
)

func (s ProfileScope) String() string {
	if s == ProfileScopeRepo {
		return "repo"
	}
	return "global"
}

// Profile is a named git-identity preset. ID is derived from Name (slug);
// renaming yields a new ID (remove-then-add).
type Profile struct {
	ID       string       `toml:"id"`
	Name     string       `toml:"name"`
	GitName  string       `toml:"git_name"`
	GitEmail string       `toml:"git_email"`
	Scope    ProfileScope `toml:"-"` // implied by which store holds it; set on List
	Created  time.Time    `toml:"created"`
}

// Identity is the current git user identity, with global and repo-local
// values kept distinct (each with a "set?" flag) plus the effective merged
// value git would actually record in a commit.
type Identity struct {
	GlobalName, GlobalEmail string
	GlobalSet               bool
	LocalName, LocalEmail   string
	LocalSet                bool
	EffectiveName, EffectiveEmail string
}
```

- [ ] **Step 2: Write `internal/profile/store.go`**

```go
// Package profile is gigagit's writable registry of named git-identity
// presets ("app profiles"). It has two scopes — global (every repo) and
// repo-specific — each a separate file-backed store. The Store interface is
// the fixed API; the file-backed implementation is swappable.
package profile

import (
	"errors"

	"github.com/gigagit/gg/internal/model"
)

// ErrNotFound is returned by Get/Remove for an unknown id.
var ErrNotFound = errors.New("profile: not found")

// Store persists profile records for one scope. Safe for sequential use by
// one process; cross-process writes are last-writer-wins (atomic rewrite).
type Store interface {
	Add(p model.Profile) (model.Profile, error)
	Get(id string) (model.Profile, error)
	List() ([]model.Profile, error)
	Remove(id string) error
}
```

- [ ] **Step 3: Write the failing test `internal/profile/file_store_test.go`**

```go
package profile

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestFileStoreAddListRemove(t *testing.T) {
	fs := NewFileStore(t.TempDir(), model.ProfileScopeRepo)

	got, err := fs.Add(model.Profile{Name: "Work", GitName: "Ada", GitEmail: "ada@work.example"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if got.ID != "work" {
		t.Fatalf("id = %q, want work", got.ID)
	}
	if got.Scope != model.ProfileScopeRepo {
		t.Fatalf("scope = %v, want repo", got.Scope)
	}
	if got.Created.IsZero() {
		t.Fatal("created not stamped")
	}

	list, err := fs.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Work" || list[0].Scope != model.ProfileScopeRepo {
		t.Fatalf("list = %#v", list)
	}

	if err := fs.Remove("work"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if list, _ := fs.List(); len(list) != 0 {
		t.Fatalf("after remove list = %#v", list)
	}
	if err := fs.Remove("work"); err != ErrNotFound {
		t.Fatalf("remove missing = %v, want ErrNotFound", err)
	}
}

func TestFileStoreAddIsIdempotentBySlug(t *testing.T) {
	fs := NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	_, _ = fs.Add(model.Profile{Name: "Work", GitEmail: "a@x"})
	_, _ = fs.Add(model.Profile{Name: "work", GitEmail: "b@x"}) // same slug
	list, _ := fs.List()
	if len(list) != 1 || list[0].GitEmail != "b@x" {
		t.Fatalf("expected idempotent replace, got %#v", list)
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/profile/...`
Expected: FAIL — `NewFileStore` undefined.

- [ ] **Step 5: Write `internal/profile/file_store.go`** (mirrors `internal/bookmark/file_store.go`)

```go
package profile

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/gigagit/gg/internal/model"
)

// FileStore keeps an atomic-rewrite TOML registry under root/profiles.toml,
// all rows belonging to one scope.
type FileStore struct {
	root  string
	scope model.ProfileScope
}

// NewFileStore roots a store at a scope's directory (caller-supplied).
func NewFileStore(root string, scope model.ProfileScope) *FileStore {
	return &FileStore{root: root, scope: scope}
}

type index struct {
	Profiles []model.Profile `toml:"profiles"`
}

func (fs *FileStore) path() string { return filepath.Join(fs.root, "profiles.toml") }

func (fs *FileStore) read() index {
	var idx index
	data, err := os.ReadFile(fs.path())
	if err != nil {
		return idx
	}
	if err := toml.Unmarshal(data, &idx); err != nil {
		return index{}
	}
	for i := range idx.Profiles {
		idx.Profiles[i].Scope = fs.scope // Scope is toml:"-"; set from the store
	}
	return idx
}

// write persists idx via temp-file + rename (the seq-state pattern).
func (fs *FileStore) write(idx index) error {
	if err := os.MkdirAll(fs.root, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(idx)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(fs.root, "profiles-*.toml")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, fs.path()); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

var slugRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// ProfileID derives a stable id from a profile name (slug). Same name → same
// id, so Add is idempotent and rename is remove-then-add.
func ProfileID(name string) string {
	return strings.Trim(strings.ToLower(slugRe.ReplaceAllString(name, "-")), "-")
}

func (fs *FileStore) Add(p model.Profile) (model.Profile, error) {
	p.ID = ProfileID(p.Name)
	p.Scope = fs.scope
	if p.Created.IsZero() {
		p.Created = time.Now()
	}
	idx := fs.read()
	for i := range idx.Profiles {
		if idx.Profiles[i].ID == p.ID { // same slug → idempotent replace
			idx.Profiles[i] = p
			return p, fs.write(idx)
		}
	}
	idx.Profiles = append(idx.Profiles, p)
	return p, fs.write(idx)
}

func (fs *FileStore) Get(id string) (model.Profile, error) {
	for _, p := range fs.read().Profiles {
		if p.ID == id {
			return p, nil
		}
	}
	return model.Profile{}, ErrNotFound
}

func (fs *FileStore) List() ([]model.Profile, error) {
	ps := fs.read().Profiles
	sort.SliceStable(ps, func(a, b int) bool { return ps[a].Created.After(ps[b].Created) })
	return ps, nil
}

func (fs *FileStore) Remove(id string) error {
	idx := fs.read()
	kept := idx.Profiles[:0]
	found := false
	for _, p := range idx.Profiles {
		if p.ID == id {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	if !found {
		return ErrNotFound
	}
	idx.Profiles = kept
	return fs.write(idx)
}

var _ Store = (*FileStore)(nil)
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/profile/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/model/profile.go internal/profile/
git commit -m "feat(profile): two-scope app-profile side-store + model types

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: `git config` read/write verbs

**Files:**
- Create: `internal/git/config.go`
- Test: `internal/git/config_test.go`

**Interfaces:**
- Consumes: `gitcmd.New`, `r.Runner.Run` (from `*git.Repo`), `gitexec` error shape.
- Produces:
  - `git.ConfigScope` (`int`): `ConfigEffective`, `ConfigLocal`, `ConfigGlobal`.
  - `func (r *Repo) ConfigGet(ctx context.Context, scope ConfigScope, key string) (value string, set bool, err error)` — exit code 1 (key unset) → `("", false, nil)`.
  - `func (r *Repo) ConfigSet(ctx context.Context, scope ConfigScope, key, value string) error` — `scope` must be Local or Global.

- [ ] **Step 1: Write the failing test `internal/git/config_test.go`**

```go
package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigGetLocalDistinctFromGlobal(t *testing.T) {
	// Isolate global so the write below cannot touch the real ~/.gitconfig.
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	dir, runner := newTestRepo(t)
	_ = dir
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	// Nothing set locally yet.
	if _, set, err := repo.ConfigGet(ctx, ConfigLocal, "user.name"); err != nil || set {
		t.Fatalf("local unset: set=%v err=%v", set, err)
	}

	if err := repo.ConfigSet(ctx, ConfigGlobal, "user.name", "Global Person"); err != nil {
		t.Fatalf("set global: %v", err)
	}
	if err := repo.ConfigSet(ctx, ConfigLocal, "user.name", "Local Person"); err != nil {
		t.Fatalf("set local: %v", err)
	}

	g, gset, _ := repo.ConfigGet(ctx, ConfigGlobal, "user.name")
	l, lset, _ := repo.ConfigGet(ctx, ConfigLocal, "user.name")
	e, _, _ := repo.ConfigGet(ctx, ConfigEffective, "user.name")
	if !gset || g != "Global Person" {
		t.Fatalf("global = %q set=%v", g, gset)
	}
	if !lset || l != "Local Person" {
		t.Fatalf("local = %q set=%v", l, lset)
	}
	if e != "Local Person" { // effective prefers local
		t.Fatalf("effective = %q, want Local Person", e)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestConfigGet`
Expected: FAIL — `ConfigGet`/`ConfigSet`/`ConfigLocal` undefined.

- [ ] **Step 3: Write `internal/git/config.go`**

```go
package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// ConfigScope selects which git config layer a read/write targets.
type ConfigScope int

const (
	ConfigEffective ConfigScope = iota // merged (no --local/--global flag)
	ConfigLocal                        // repo .git/config
	ConfigGlobal                       // ~/.gitconfig
)

func (s ConfigScope) flag() (string, bool) {
	switch s {
	case ConfigLocal:
		return "--local", true
	case ConfigGlobal:
		return "--global", true
	default:
		return "", false
	}
}

// ConfigGet reads one config key at the given scope. A key that is unset
// returns ("", false, nil): `git config --get` exits 1 for a missing key,
// which the runner surfaces as a non-nil err with res.ExitCode == 1 (the
// CommitExists pattern), not a real error. Use an explicit Local/Global scope
// to distinguish a repo value from an inherited global one; ConfigEffective
// returns the merged value.
func (r *Repo) ConfigGet(ctx context.Context, scope ConfigScope, key string) (string, bool, error) {
	b := gitcmd.New("config")
	if f, ok := scope.flag(); ok {
		b = b.Arg(f)
	}
	b = b.Arg("--get", key)
	res, err := r.Runner.Run(ctx, "git config", b.ToArgv())
	if err == nil {
		return strings.TrimSpace(res.Stdout), true, nil
	}
	if res.ExitCode == 1 {
		return "", false, nil // key unset
	}
	return "", false, err
}

// ConfigSet writes one config key at the given scope (Local or Global only;
// an Effective/unknown scope falls back to Local).
func (r *Repo) ConfigSet(ctx context.Context, scope ConfigScope, key, value string) error {
	f, ok := scope.flag()
	if !ok {
		f = "--local"
	}
	b := gitcmd.New("config").Arg(f, key, value)
	_, err := r.Runner.Run(ctx, "git config", b.ToArgv())
	return err
}
```

(Verified against the codebase: the runner returns a non-nil err on non-zero
exit but carries the code in `res.ExitCode` — `internal/git/commit_exists.go`
uses the exact `if err == nil { ... } if res.ExitCode == 1 { ... }` shape.
`strings.TrimSpace(res.Stdout)` is the convention for single-value reads.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git/ -run TestConfigGet`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/git/config.go internal/git/config_test.go
git commit -m "feat(git): ConfigGet/ConfigSet verbs (local/global/effective)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: `SetIdentity` engine op

**Files:**
- Create: `internal/engine/set_identity.go`
- Modify: `internal/engine/gitops.go` (add `ConfigSet` to the `GitOps` interface, in the verb group near `Reset`)
- Test: `internal/engine/set_identity_test.go`

**Interfaces:**
- Consumes: `git.ConfigScope`/`ConfigLocal`/`ConfigGlobal`, `OpDeps`, `repogate.Read`.
- Produces: `engine.SetIdentity{ Name, Email string; Global bool }` implementing `Operation`; `LockMode() repogate.Mode` returning `repogate.Read`.

- [ ] **Step 1: Add `ConfigSet` to the `GitOps` interface**

In `internal/engine/gitops.go`, add to the interface (next to `Reset`):

```go
	ConfigSet(ctx context.Context, scope git.ConfigScope, key, value string) error
```

(`git` is already imported in `gitops.go`.)

- [ ] **Step 2: Write the failing test `internal/engine/set_identity_test.go`**

```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/git"
)

func TestSetIdentityWritesLocalScope(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, repo := newRepo(t)
	ctx := context.Background()

	res, err := SetIdentity{Name: "Ada L", Email: "ada@local"}.Run(ctx, OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	n, set, _ := repo.ConfigGet(ctx, git.ConfigLocal, "user.name")
	e, _, _ := repo.ConfigGet(ctx, git.ConfigLocal, "user.email")
	if !set || n != "Ada L" || e != "ada@local" {
		t.Fatalf("local identity = %q <%q> set=%v", n, e, set)
	}
	// Global must remain untouched.
	if _, gset, _ := repo.ConfigGet(ctx, git.ConfigGlobal, "user.name"); gset {
		t.Fatal("global was written; expected local-only")
	}
}

func TestSetIdentityWritesGlobalScope(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	_, repo := newRepo(t)
	ctx := context.Background()

	if _, err := (SetIdentity{Name: "G", Email: "g@x", Global: true}).Run(ctx, OpDeps{Repo: repo}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, gset, _ := repo.ConfigGet(ctx, git.ConfigGlobal, "user.name"); !gset {
		t.Fatal("global user.name not set")
	}
	if _, lset, _ := repo.ConfigGet(ctx, git.ConfigLocal, "user.name"); lset {
		t.Fatal("local was written; expected global-only")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestSetIdentity`
Expected: FAIL — `SetIdentity` undefined.

- [ ] **Step 4: Write `internal/engine/set_identity.go`**

```go
package engine

import (
	"context"
	"fmt"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/repogate"
)

// SetIdentity writes user.name and user.email to one git config scope. It is
// the single write path behind both "edit identity" (typed values) and "apply
// a profile" (saved values). Decision-free: the scope is fixed before any work
// (Global true = ~/.gitconfig, false = this repo's .git/config).
type SetIdentity struct {
	Name   string
	Email  string
	Global bool
}

// LockMode: a config write touches neither refs nor the work tree (and a
// global write is not even repo-scoped), so the lightest reservation suffices.
func (op SetIdentity) LockMode() repogate.Mode { return repogate.Read }

func (op SetIdentity) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Name == "" || op.Email == "" {
		return Result{}, fmt.Errorf("set identity: name and email are required")
	}
	scope := git.ConfigLocal
	where := "this repo"
	if op.Global {
		scope = git.ConfigGlobal
		where = "globally"
	}
	deps.emit(ctx, Progress{Step: "setting identity", Detail: where})
	if err := deps.Repo.ConfigSet(ctx, scope, "user.name", op.Name); err != nil {
		return Result{}, fmt.Errorf("set identity: user.name: %w", err)
	}
	if err := deps.Repo.ConfigSet(ctx, scope, "user.email", op.Email); err != nil {
		return Result{}, fmt.Errorf("set identity: user.email: %w", err)
	}
	res := Result{Summary: fmt.Sprintf("identity %s <%s> set %s", op.Name, op.Email, where), Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = SetIdentity{}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/engine/ -run TestSetIdentity`
Expected: PASS (both subtests). Also `go build ./...` to confirm the `GitOps` interface still compiles (the `var _ GitOps = (*git.Repo)(nil)` proof).

- [ ] **Step 6: Commit**

```bash
git add internal/engine/set_identity.go internal/engine/gitops.go internal/engine/set_identity_test.go
git commit -m "feat(engine): SetIdentity op (single write path, Read lock)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: domain `Identity` query + profile store wiring + CRUD

**Files:**
- Create: `internal/domain/identity.go` (the `Identity` query)
- Create: `internal/domain/profilestore.go` (two-scope store resolution + CRUD)
- Modify: `internal/domain/service.go` (add `profileGlobal`, `profileRepo profile.Store` fields to `Service`)
- Test: `internal/domain/identity_test.go`
- Test: `internal/domain/profilestore_test.go`

**Interfaces:**
- Consumes: `query[T]` helper, `repoKey`, `s.repo.ConfigGet`, `profile.NewFileStore`, `model.Identity`, `model.Profile`, `model.ProfileScope*`.
- Produces:
  - `func (s *Service) Identity(ctx context.Context) (model.Identity, error)`
  - `func (s *Service) Profiles(ctx context.Context) ([]model.Profile, error)` — global rows then repo rows, each tagged with its scope.
  - `func (s *Service) AddProfile(ctx context.Context, p model.Profile) (model.Profile, error)` — routed by `p.Scope`.
  - `func (s *Service) RemoveProfile(ctx context.Context, scope model.ProfileScope, id string) error`
  - `var ProfileStatePath string` (test override)
  - `func (s *Service) SetProfileStores(global, repo profile.Store)` (test injection)

- [ ] **Step 1: Add Service fields**

In `internal/domain/service.go`, in the `Service` struct (next to `bookmark`):

```go
	profileGlobal profile.Store // lazily resolved; nil disables profiles
	profileRepo   profile.Store // lazily resolved; nil disables profiles
```

Add `"github.com/gigagit/gg/internal/profile"` to the import block.

- [ ] **Step 2: Write the failing test `internal/domain/identity_test.go`**

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityDistinguishesScopes(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	dir := newDomainRepo(t) // see note below
	svc := Open(dir)
	ctx := context.Background()

	// No local identity set → LocalSet false, but effective may inherit global.
	id, err := svc.Identity(ctx)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	if id.LocalSet {
		t.Fatalf("expected no local identity, got %q", id.LocalName)
	}
}
```

NOTE: reuse whatever real-repo helper the `domain` test package already has
(grep `func newDomainRepo\|func newRepo\|func newTestRepo` in
`internal/domain/*_test.go`). If none builds a real repo with a commit, build
one inline like `internal/git/repo_test.go`'s `newTestRepo` (init, write,
add, commit using `GIT_AUTHOR_*` env). Name the helper to match the package.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/domain/ -run TestIdentity`
Expected: FAIL — `Identity` undefined.

- [ ] **Step 4: Write `internal/domain/identity.go`**

```go
package domain

import (
	"context"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/model"
)

// Identity reads the current git user identity, keeping the global and
// repo-local values distinct (each with a set flag) plus the effective merged
// value, under a Read reservation.
func (s *Service) Identity(ctx context.Context) (model.Identity, error) {
	return query(ctx, s, "identity", func(ctx context.Context) (model.Identity, error) {
		var id model.Identity
		id.GlobalName, _, _ = s.repo.ConfigGet(ctx, git.ConfigGlobal, "user.name")
		id.GlobalEmail, id.GlobalSet, _ = s.repo.ConfigGet(ctx, git.ConfigGlobal, "user.email")
		if id.GlobalName != "" {
			id.GlobalSet = true
		}
		id.LocalName, _, _ = s.repo.ConfigGet(ctx, git.ConfigLocal, "user.name")
		id.LocalEmail, id.LocalSet, _ = s.repo.ConfigGet(ctx, git.ConfigLocal, "user.email")
		if id.LocalName != "" {
			id.LocalSet = true
		}
		id.EffectiveName, _, _ = s.repo.ConfigGet(ctx, git.ConfigEffective, "user.name")
		id.EffectiveEmail, _, _ = s.repo.ConfigGet(ctx, git.ConfigEffective, "user.email")
		return id, nil
	})
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/domain/ -run TestIdentity`
Expected: PASS.

- [ ] **Step 6: Write the failing test `internal/domain/profilestore_test.go`**

```go
package domain

import (
	"context"
	"testing"

	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/profile"
)

func TestProfilesMergeBothScopes(t *testing.T) {
	g := profile.NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	r := profile.NewFileStore(t.TempDir(), model.ProfileScopeRepo)
	svc := New(nil) // no git ops needed for profile CRUD
	svc.SetProfileStores(g, r)
	ctx := context.Background()

	if _, err := svc.AddProfile(ctx, model.Profile{Name: "Work", GitName: "A", GitEmail: "a@x", Scope: model.ProfileScopeGlobal}); err != nil {
		t.Fatalf("add global: %v", err)
	}
	if _, err := svc.AddProfile(ctx, model.Profile{Name: "OSS", GitName: "B", GitEmail: "b@x", Scope: model.ProfileScopeRepo}); err != nil {
		t.Fatalf("add repo: %v", err)
	}

	all, err := svc.Profiles(ctx)
	if err != nil {
		t.Fatalf("profiles: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("len = %d, want 2: %#v", len(all), all)
	}
	// Global rows come first, tagged correctly.
	if all[0].Scope != model.ProfileScopeGlobal || all[1].Scope != model.ProfileScopeRepo {
		t.Fatalf("scopes = %v,%v", all[0].Scope, all[1].Scope)
	}

	if err := svc.RemoveProfile(ctx, model.ProfileScopeRepo, "oss"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if all, _ := svc.Profiles(ctx); len(all) != 1 || all[0].Name != "Work" {
		t.Fatalf("after remove = %#v", all)
	}
}
```

- [ ] **Step 7: Run test to verify it fails**

Run: `go test ./internal/domain/ -run TestProfiles`
Expected: FAIL — `SetProfileStores`/`AddProfile`/`Profiles` undefined.

- [ ] **Step 8: Write `internal/domain/profilestore.go`** (mirrors `bookmarkstore.go`, two stores)

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/profile"
)

// ProfileStatePath overrides the profile root dir. "" uses the default XDG
// location. cmd/gg leaves it ""; tests point it at a temp dir.
var ProfileStatePath string

// SetProfileStores injects both scope stores (tests).
func (s *Service) SetProfileStores(global, repo profile.Store) {
	s.mu.Lock()
	s.profileGlobal = global
	s.profileRepo = repo
	s.mu.Unlock()
}

func (s *Service) profileStores(ctx context.Context) (global, repo profile.Store) {
	s.mu.Lock()
	if s.profileGlobal != nil || s.profileRepo != nil {
		g, r := s.profileGlobal, s.profileRepo
		s.mu.Unlock()
		return g, r
	}
	s.mu.Unlock()

	base := ProfileStatePath
	if base == "" {
		base = profileBaseDir()
	}
	if base == "" {
		return nil, nil // profiles disabled (no state dir)
	}
	g := profile.NewFileStore(filepath.Join(base, "global"), model.ProfileScopeGlobal)
	key := "unknown"
	if cd, err := s.GitCommonDir(ctx); err == nil {
		key = repoKey(strings.TrimSpace(cd))
	}
	r := profile.NewFileStore(filepath.Join(base, key), model.ProfileScopeRepo)

	s.mu.Lock()
	s.profileGlobal, s.profileRepo = g, r
	s.mu.Unlock()
	return g, r
}

// profileBaseDir resolves <state>/gg/profile cross-platform (mirrors
// bookmarkBaseDir). "" when no home/state dir exists.
func profileBaseDir() string {
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return filepath.Join(lad, "gg", "profile")
		}
	}
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "gg", "profile")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "gg", "profile")
}

// Profiles lists global rows then repo rows, each tagged with its scope.
func (s *Service) Profiles(ctx context.Context) ([]model.Profile, error) {
	global, repo := s.profileStores(ctx)
	var out []model.Profile
	if global != nil {
		gs, err := global.List()
		if err != nil {
			return nil, err
		}
		out = append(out, gs...)
	}
	if repo != nil {
		rs, err := repo.List()
		if err != nil {
			return nil, err
		}
		out = append(out, rs...)
	}
	return out, nil
}

// AddProfile routes to the store matching p.Scope.
func (s *Service) AddProfile(ctx context.Context, p model.Profile) (model.Profile, error) {
	global, repo := s.profileStores(ctx)
	st := global
	if p.Scope == model.ProfileScopeRepo {
		st = repo
	}
	if st == nil {
		return model.Profile{}, os.ErrInvalid
	}
	return st.Add(p)
}

// RemoveProfile removes id from the store matching scope.
func (s *Service) RemoveProfile(ctx context.Context, scope model.ProfileScope, id string) error {
	global, repo := s.profileStores(ctx)
	st := global
	if scope == model.ProfileScopeRepo {
		st = repo
	}
	if st == nil {
		return os.ErrInvalid
	}
	return st.Remove(id)
}
```

- [ ] **Step 9: Run tests to verify they pass**

Run: `go test ./internal/domain/ -run 'TestProfiles|TestIdentity'`
Expected: PASS.

- [ ] **Step 10: Confirm archtest still passes** (frontends must not import `profile`)

Run: `go test ./internal/archtest/...`
Expected: PASS. If `internal/profile` needs to be in the forbidden-import list for `tui`/`cli`, add it alongside `bookmark`/`shelf` (grep the archtest for `internal/bookmark` to find the list).

- [ ] **Step 11: Commit**

```bash
git add internal/domain/identity.go internal/domain/profilestore.go internal/domain/service.go internal/domain/identity_test.go internal/domain/profilestore_test.go
git commit -m "feat(domain): Identity query + two-scope profile CRUD

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: TUI identity & profiles sub-surface

**REQUIRED SUB-SKILL:** invoke `adding-tui-windows` before writing this task — the surface is a popup sub-screen with a list + create/edit/delete + a local/global apply prompt, not a single menu row. Follow its panel/popup/modal taxonomy and key-routing guidance.

**Files:**
- Create: `internal/tui/identity_popup.go` (the sub-surface: struct, open, update, render)
- Modify: `internal/tui/settings_popup.go` (add an "Identity & profiles" menu row that opens it)
- Modify: `internal/tui/op.go` (add `identityRefreshedMsg` + `reloadIdentityCmd`, mirroring `refsRefreshedMsg`/`reloadRefsCmd`)
- Modify: `internal/tui/model.go` (handle `identityRefreshedMsg`; add `pendingIdentityReload bool`; in `opFinishedMsg` route to `reloadIdentityCmd` when set, instead of a full `loadCmd`)
- Modify: `internal/tui/load.go` (or wherever `Snapshot` is applied) — load initial `Identity` into the Model on startup load, OR lazily fetch when the popup opens (lazy is simpler; see Step 2)
- Test: `internal/tui/identity_popup_test.go`

**Interfaces:**
- Consumes: `m.svc.Identity`, `m.svc.Profiles`, `m.svc.AddProfile`, `m.svc.RemoveProfile`, `engine.SetIdentity`, `m.startOp`, `newTextField`, `m.pushLayer`/`popLayer`/`layerOf`, `renderWindow`/`winRow`/`popupBox`.
- Produces: `identityView` popup type registered in the layer stack; `m.pendingIdentityReload`.

Design (from the spec — implement against `adding-tui-windows`):

- The Settings menu (`,`) becomes a real two-row menu: "Set up agent skills" and "Identity & profiles". (Today `settingsPopup` hardcodes a single static row and treats up/down as no-ops — generalize it to a small indexed menu, or push a separate `identityView` layer on enter. Pushing a separate layer is cleaner and avoids bloating `settingsPopup`.)
- `identityView` holds: `id model.Identity`, `profiles []model.Profile`, `sel int`, an editing sub-state (`editing bool` + `name, email textField` + `editScope model.ProfileScope` for new/edit), and an apply-prompt sub-state (`applying bool` + the chosen profile/typed values).
- Render order: a "Current identity" block (Global / Repo / Effective lines, "(not set)" when unset and "inherits global …" when local unset but global set), then the profiles list grouped by scope with `[global]`/`[this repo]` tags.
- Keys: `up`/`down` move; `enter` on a profile → apply prompt "Apply to: [r] this repo / [g] globally"; `e` → edit-identity text fields → same apply prompt; `n` new profile (prompts name/email + scope); `r` rename (remove old id + add new); `d` delete (`RemoveProfile`); `esc` backs out one sub-state then closes.
- Apply = `m.startOp(engine.SetIdentity{Name, Email, Global})` with `m.pendingIdentityReload = true` set first, so the post-op refresh re-reads only the identity (not a full Snapshot).

- [ ] **Step 1: Invoke the `adding-tui-windows` skill** and confirm the popup-vs-modal choice (a stacked popup layer, like `bookmark_popup.go`) and the key-routing site (`topLayer()`/`overlayTop()` path).

- [ ] **Step 2: Write the failing render-path test `internal/tui/identity_popup_test.go`**

This guards the green-unit/broken-render class (the Reflog lesson): assert the popup *renders* the identity and profiles through the assembled `View()`/render path, not just that state was set.

```go
package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestIdentityViewRendersCurrentAndProfiles(t *testing.T) {
	m := newTestModel(t) // reuse the package's model constructor (grep existing tests)
	m.width, m.height = 100, 40

	v := &identityView{
		id: model.Identity{
			GlobalName: "Glob", GlobalEmail: "g@x", GlobalSet: true,
			LocalSet: false,
			EffectiveName: "Glob", EffectiveEmail: "g@x",
		},
		profiles: []model.Profile{
			{Name: "Work", GitName: "W", GitEmail: "w@x", Scope: model.ProfileScopeGlobal},
			{Name: "OSS", GitName: "O", GitEmail: "o@x", Scope: model.ProfileScopeRepo},
		},
	}
	out := v.render(m, "")
	for _, want := range []string{"Glob", "g@x", "Work", "OSS", "global", "this repo"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output missing %q:\n%s", want, out)
		}
	}
}
```

NOTE: match the package's existing model-construction helper and the `render`
signature of other popups (grep `func newTestModel\|func (p \*bookmark`). Adapt
field names (`m.width`/`m.height` vs an `overlayDims` source) to what the
package actually uses.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestIdentityView`
Expected: FAIL — `identityView` undefined.

- [ ] **Step 4: Implement `internal/tui/identity_popup.go`**

Implement the `identityView` struct, `openIdentityView(m) (Model, tea.Cmd)` (fetches `Identity`+`Profiles` — either synchronously via a command yielding a msg, or eagerly; follow how `bookmark_popup`/`reflog_view` load their data), `update`, and `render`/`box` (mirror `settings_popup.go`'s `render`/`box` + `renderWindow`). Wire it into the layer stack's routing and rendering exactly as `adding-tui-windows` prescribes. Keep the apply path going through `m.startOp(engine.SetIdentity{...})` with `m.pendingIdentityReload = true`.

(Full UI code is left to the implementer guided by the skill + the design above; the render-path test and the behavior tests in Steps 6–7 pin the contract.)

- [ ] **Step 5: Wire the targeted refresh in `op.go` + `model.go`**

In `internal/tui/op.go`, add (mirroring `reloadRefsCmd`/`refsRefreshedMsg`):

```go
type identityRefreshedMsg struct {
	summary string
	id      model.Identity
	err     error
}

func (m Model) reloadIdentityCmd(summary string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		id, err := svc.Identity(context.Background())
		return identityRefreshedMsg{summary: summary, id: id, err: err}
	}
}
```

In `internal/tui/model.go`: add `pendingIdentityReload bool` to `Model`; in the `opFinishedMsg` handler capture it like `refsReload` and, when set, `return m, m.reloadIdentityCmd(m.statusMsg)` (before the full `loadCmd()` fallback); add a `case identityRefreshedMsg:` that updates `m.identity` (or the open `identityView`'s `id`) and `m.statusMsg`, mirroring the `refsRefreshedMsg` case. Reset `pendingIdentityReload = false` alongside the other `pending*` resets.

- [ ] **Step 6: Add an apply behavior test**

```go
func TestIdentityApplyStartsSetIdentityOp(t *testing.T) {
	// Drive update() with the apply keys and assert it returns a command and
	// sets pendingIdentityReload, and that the op built is engine.SetIdentity
	// with the expected Global flag. Follow the package's existing pattern for
	// asserting a started op (grep tests that check m.running / startOp).
}
```

Implement it against whatever seam the package uses to inspect a started op (e.g. a test hook, `m.running`, or by making the apply build a `engine.SetIdentity` value a helper returns). If no such seam exists, factor the op construction into a small pure helper `applyOp(name, email string, global bool) engine.SetIdentity` and unit-test that directly.

- [ ] **Step 7: Run the TUI tests**

Run: `go test ./internal/tui/...`
Expected: PASS.

- [ ] **Step 8: Manual smoke check (record for the human reviewer)**

Build and run against a scratch repo; open Settings (`,`) → Identity & profiles; verify current identity shows, create a profile, apply it to repo then global, confirm with `git config --local --get user.name` and `git config --global --get user.name` (against an isolated `GIT_CONFIG_GLOBAL` if you value your real gitconfig). Note the result in the commit/PR body — live-TTY confirmation is the project's bar for TUI work.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/identity_popup.go internal/tui/settings_popup.go internal/tui/op.go internal/tui/model.go internal/tui/identity_popup_test.go
git commit -m "feat(tui): identity & profiles sub-surface in Settings

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: Docs

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md`
- Modify: `CLAUDE.md` (package map: add `internal/profile`; note first git-config write)

- [ ] **Step 1: CHANGELOG entry** — under the current unreleased section, describe: view/edit git identity per-repo & globally from Settings; named app-profiles (global + per-repo) you can apply to either scope; new `internal/profile` side-store; first feature to write git config. Note CLI (`gg identity`/`gg profile`) is deferred.

- [ ] **Step 2: README** — add a short "Identity & profiles" subsection under the TUI/Settings docs: open with `,`, the current-identity display, creating/applying profiles, local-vs-global prompt.

- [ ] **Step 3: CLAUDE.md package map** — add a `profile` row (mirroring the `bookmark` row's wording: writable two-scope side-store of named git-identity presets, owned by `domain`, frontends never import it). Add a one-line note that this is the first feature to write git config (via `git.ConfigSet`/the `SetIdentity` op), and that `internal/config` remains read-only at runtime.

- [ ] **Step 4: Run the full suite**

Run: `./test.sh` then `./test.sh race`
Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: identity & profiles feature

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Self-Review notes

- **Spec coverage:** view/edit live identity → Tasks 2/3/5; distinguish local/global/effective → Task 2 (explicit scope flags) + Task 4 (`Identity`); named profiles two-scoped side-store → Tasks 1/4; apply onto local/global, ask each time → Task 5 apply prompt + Task 3 op; settings-popup entry → Task 5; targeted refresh (no full Snapshot) → Task 5 Step 5; tests isolate global config → Global Constraints + every global-write test; CLI deferred → not planned (intentional). All spec sections map to a task.
- **Type consistency:** `model.ProfileScope`/`ProfileScopeGlobal`/`ProfileScopeRepo`, `git.ConfigScope`/`ConfigLocal`/`ConfigGlobal`/`ConfigEffective`, `engine.SetIdentity{Name,Email,Global}`, `model.Identity` field names, `profile.Store`/`NewFileStore(root, scope)`, `domain.Identity`/`Profiles`/`AddProfile`/`RemoveProfile`/`SetProfileStores`, `ProfileStatePath` — used identically across tasks.
- **Resolved against the codebase (no longer open):** the runner's exit-code shape (`res.ExitCode`, not a `gitexec.ExitError` — Task 2 code corrected) and the single-value trim convention (`strings.TrimSpace`).
- **Verification notes still flagged for the implementer:** the domain real-repo test helper name (Task 4 Step 2 note), the archtest forbidden-import list (Task 4 Step 10), and the TUI model/render helpers + routing site (Task 5, deferred to `adding-tui-windows`). These are "match the exact local symbol names", not placeholders — the code to write is fully specified.
