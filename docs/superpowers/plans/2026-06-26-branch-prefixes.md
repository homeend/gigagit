# Branch Prefixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users select a reusable, possibly-templated branch-name *skeleton* ("prefix") when creating a branch or worktree, fill any interactive `<user:…>` labels, and append free text — backed by a writable two-scope store managed in Settings and a `gg prefix` CLI.

**Architecture:** A new `internal/prefix` two-scope file store (global + per-repo, `prefixes.toml`, atomic rewrite) is owned by `internal/domain` (mirrors `internal/profile` exactly). Frontends never import `internal/prefix`. The TUI adds a select-only picker popup (reached via `p` in both create popups) that runs a reusable `templateFill` step then inserts the resolved string into the branch-name field; a Settings sub-screen manages CRUD. A `gg prefix` CLI mirrors `gg bookmark`.

**Tech Stack:** Go 1.26, Bubble Tea (Elm-style value-receiver `Model`), `github.com/pelletier/go-toml/v2`, the existing `internal/template` resolver.

## Global Constraints

- Module path `github.com/homeend/gigagit`; Go 1.26.
- **A git verb is one invocation** via `gitcmd`/`gitexec`; never shell out directly. (This feature adds no git verbs.)
- **`internal/tui` and `internal/cli` never import `internal/git` or `internal/prefix`** — they reach storage through `internal/domain` (archtest-guarded, mirror profile/bookmark/shelf).
- **TUI `Model` is a value receiver** with pointer fields for state that must persist across the copy; layers on the stack are pointers.
- Tests use a real `git` in `t.TempDir()` or `FakeRunner`; follow TDD.
- Token vocabulary is the existing engine's: `<user:LABEL>`, `<seq:NAME>`/`<seq:NAME:N>`, `<date:FMT>`, `<parent-branch>`, `<repo>`, `<random-alpha:N>`, `<random-num:N>`. **`<branch>` is disallowed in a prefix.** No new tokens/aliases.
- Branch off `main`; the human merges. Commit at the end of each task.
- Spec: `docs/superpowers/specs/2026-06-26-branch-prefixes-design.md`.

---

### Task 1: `model.Prefix` + `internal/prefix` store

**Files:**
- Create: `internal/model/prefix.go`
- Create: `internal/prefix/store.go`
- Create: `internal/prefix/file_store.go`
- Test: `internal/prefix/file_store_test.go`

**Interfaces:**
- Produces: `model.Prefix{ID, Value string; Scope model.ProfileScope}`; `prefix.Store` interface (`Add`, `Get`, `List`, `Remove`); `prefix.NewFileStore(root string, scope model.ProfileScope) *FileStore`; `prefix.PrefixID(value string) string`; `prefix.ErrNotFound`.

- [ ] **Step 1: Write `model.Prefix`**

`internal/model/prefix.go`:

```go
package model

import "time"

// Prefix is a reusable branch-name skeleton (possibly templated). Its identity
// is its Value (slugged into ID); Scope is implied by which store holds it and
// is set on List/Add. Reuses ProfileScope (global vs repo).
type Prefix struct {
	ID      string       `toml:"id"`
	Value   string       `toml:"value"`
	Scope   ProfileScope `toml:"-"`
	Created time.Time    `toml:"created"`
}
```

- [ ] **Step 2: Write the failing store test**

`internal/prefix/file_store_test.go`:

```go
package prefix

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestFileStoreRoundTrip(t *testing.T) {
	fs := NewFileStore(t.TempDir(), model.ProfileScopeRepo)

	added, err := fs.Add(model.Prefix{Value: "feat/"})
	if err != nil {
		t.Fatal(err)
	}
	if added.ID == "" {
		t.Fatal("want non-empty ID")
	}
	if added.Scope != model.ProfileScopeRepo {
		t.Fatalf("scope = %v, want repo", added.Scope)
	}

	list, err := fs.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Value != "feat/" {
		t.Fatalf("list = %+v", list)
	}
	if list[0].Scope != model.ProfileScopeRepo {
		t.Fatalf("listed scope = %v", list[0].Scope)
	}

	if err := fs.Remove(added.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := fs.List(); len(list) != 0 {
		t.Fatalf("after remove, list = %+v", list)
	}
}

func TestFileStoreAddIsIdempotentBySlug(t *testing.T) {
	fs := NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	_, _ = fs.Add(model.Prefix{Value: "feat/"})
	_, _ = fs.Add(model.Prefix{Value: "feat/"})
	if list, _ := fs.List(); len(list) != 1 {
		t.Fatalf("want 1 entry, got %d", len(list))
	}
}

func TestRemoveUnknownIsErrNotFound(t *testing.T) {
	fs := NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	if err := fs.Remove("nope"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/prefix/ -run TestFileStore -v`
Expected: FAIL (package/types do not exist).

- [ ] **Step 4: Write `prefix.Store`**

`internal/prefix/store.go`:

```go
// Package prefix is gigagit's writable registry of reusable branch-name
// prefixes (skeletons). Two scopes — global (every repo) and repo-specific —
// each a separate file-backed store. The Store interface is the fixed API.
package prefix

import (
	"errors"

	"github.com/homeend/gigagit/internal/model"
)

// ErrNotFound is returned by Get/Remove for an unknown id.
var ErrNotFound = errors.New("prefix: not found")

// Store persists prefix records for one scope (atomic rewrite, last-writer-wins).
type Store interface {
	Add(p model.Prefix) (model.Prefix, error)
	Get(id string) (model.Prefix, error)
	List() ([]model.Prefix, error)
	Remove(id string) error
}
```

- [ ] **Step 5: Write `prefix.FileStore`**

`internal/prefix/file_store.go` (mirror of `profile.FileStore`, `prefixes.toml`):

```go
package prefix

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/model"
)

// FileStore keeps an atomic-rewrite TOML registry under root/prefixes.toml,
// all rows belonging to one scope.
type FileStore struct {
	root  string
	scope model.ProfileScope
}

func NewFileStore(root string, scope model.ProfileScope) *FileStore {
	return &FileStore{root: root, scope: scope}
}

type index struct {
	Prefixes []model.Prefix `toml:"prefixes"`
}

func (fs *FileStore) path() string { return filepath.Join(fs.root, "prefixes.toml") }

func (fs *FileStore) read() index {
	var idx index
	data, err := os.ReadFile(fs.path())
	if err != nil {
		return idx
	}
	if err := toml.Unmarshal(data, &idx); err != nil {
		return index{}
	}
	for i := range idx.Prefixes {
		idx.Prefixes[i].Scope = fs.scope
	}
	return idx
}

func (fs *FileStore) write(idx index) error {
	if err := os.MkdirAll(fs.root, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(idx)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(fs.root, "prefixes-*.toml")
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

// PrefixID derives a stable id from a prefix value (slug). Same value → same id,
// so Add is idempotent.
func PrefixID(value string) string {
	return strings.Trim(strings.ToLower(slugRe.ReplaceAllString(value, "-")), "-")
}

func (fs *FileStore) Add(p model.Prefix) (model.Prefix, error) {
	p.ID = PrefixID(p.Value)
	p.Scope = fs.scope
	if p.Created.IsZero() {
		p.Created = time.Now()
	}
	idx := fs.read()
	for i := range idx.Prefixes {
		if idx.Prefixes[i].ID == p.ID {
			idx.Prefixes[i] = p
			return p, fs.write(idx)
		}
	}
	idx.Prefixes = append(idx.Prefixes, p)
	return p, fs.write(idx)
}

func (fs *FileStore) Get(id string) (model.Prefix, error) {
	for _, p := range fs.read().Prefixes {
		if p.ID == id {
			return p, nil
		}
	}
	return model.Prefix{}, ErrNotFound
}

func (fs *FileStore) List() ([]model.Prefix, error) {
	ps := fs.read().Prefixes
	sort.SliceStable(ps, func(a, b int) bool { return ps[a].Created.After(ps[b].Created) })
	return ps, nil
}

func (fs *FileStore) Remove(id string) error {
	idx := fs.read()
	kept := idx.Prefixes[:0]
	found := false
	for _, p := range idx.Prefixes {
		if p.ID == id {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	if !found {
		return ErrNotFound
	}
	idx.Prefixes = kept
	return fs.write(idx)
}

var _ Store = (*FileStore)(nil)
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/prefix/ ./internal/model/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/model/prefix.go internal/prefix/
git commit -m "feat(prefix): two-scope file store for branch-name prefixes"
```

---

### Task 2: domain wiring + token validation

**Files:**
- Create: `internal/domain/prefixstore.go`
- Modify: `internal/domain/service.go` (add `prefixGlobal`, `prefixRepo prefix.Store` fields next to `profileGlobal`/`profileRepo` around line 43)
- Test: `internal/domain/prefixstore_test.go`

**Interfaces:**
- Consumes: `prefix.Store`, `prefix.NewFileStore`, `model.Prefix`, `template.Resolve`/`UserLabels`/`SeqNames`, `model.ProfileScopeGlobal/Repo`.
- Produces: `(*Service).Prefixes(ctx) ([]model.Prefix, error)`; `(*Service).AddPrefix(ctx, model.Prefix) (model.Prefix, error)`; `(*Service).RemovePrefix(ctx, model.ProfileScope, id string) error`; `(*Service).SetPrefixStores(global, repo prefix.Store)`; `domain.PrefixStatePath string`; `domain.ValidatePrefixValue(value string) error`.

- [ ] **Step 1: Add service fields**

In `internal/domain/service.go`, beside the profile fields (~line 43):

```go
	prefixGlobal prefix.Store // lazily resolved; nil disables prefixes
	prefixRepo   prefix.Store // lazily resolved; nil disables prefixes
```

Add `"github.com/homeend/gigagit/internal/prefix"` to that file's imports.

- [ ] **Step 2: Write the failing domain test**

`internal/domain/prefixstore_test.go`:

```go
package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/prefix"
)

func TestPrefixesMergeGlobalThenRepoTagged(t *testing.T) {
	s := &Service{}
	g := prefix.NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	r := prefix.NewFileStore(t.TempDir(), model.ProfileScopeRepo)
	_, _ = g.Add(model.Prefix{Value: "feat/"})
	_, _ = r.Add(model.Prefix{Value: "jira/"})
	s.SetPrefixStores(g, r)

	got, err := s.Prefixes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Scope != model.ProfileScopeGlobal || got[1].Scope != model.ProfileScopeRepo {
		t.Fatalf("scopes = %v, %v", got[0].Scope, got[1].Scope)
	}
}

func TestAddPrefixRoutesByScopeAndValidates(t *testing.T) {
	s := &Service{}
	g := prefix.NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	r := prefix.NewFileStore(t.TempDir(), model.ProfileScopeRepo)
	s.SetPrefixStores(g, r)

	if _, err := s.AddPrefix(context.Background(), model.Prefix{Value: "feat/", Scope: model.ProfileScopeRepo}); err != nil {
		t.Fatal(err)
	}
	if list, _ := r.List(); len(list) != 1 {
		t.Fatalf("repo store len = %d, want 1", len(list))
	}
	if list, _ := g.List(); len(list) != 0 {
		t.Fatalf("global store len = %d, want 0", len(list))
	}

	if _, err := s.AddPrefix(context.Background(), model.Prefix{Value: "<branch>/x"}); err == nil {
		t.Fatal("want error for <branch> token")
	}
}

func TestValidatePrefixValue(t *testing.T) {
	ok := []string{
		"feat/",
		"john_smith/ISSUE-<user:issue-id>",
		"john_smith/sandbox-<seq:sandbox_seq:4>",
		"wt/<date:yyyy-MM-dd>/",
		"<parent-branch>/<random-alpha:4>",
	}
	for _, v := range ok {
		if err := ValidatePrefixValue(v); err != nil {
			t.Errorf("ValidatePrefixValue(%q) = %v, want nil", v, err)
		}
	}
	bad := []string{
		"",
		"<branch>",
		"x-<branch>-y",
		"<date>",      // missing format → engine errors
		"<bogus:1>",   // unknown token
	}
	for _, v := range bad {
		if err := ValidatePrefixValue(v); err == nil {
			t.Errorf("ValidatePrefixValue(%q) = nil, want error", v)
		}
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/domain/ -run 'Prefix|ValidatePrefix' -v`
Expected: FAIL (undefined symbols).

- [ ] **Step 4: Write `prefixstore.go`**

`internal/domain/prefixstore.go` (mirror `profilestore.go`, add validation):

```go
package domain

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/prefix"
	"github.com/homeend/gigagit/internal/template"
)

// PrefixStatePath overrides the prefix root dir. "" uses the default XDG
// location. cmd/gg leaves it ""; tests point it at a temp dir.
var PrefixStatePath string

// SetPrefixStores injects both scope stores (tests).
func (s *Service) SetPrefixStores(global, repo prefix.Store) {
	s.mu.Lock()
	s.prefixGlobal = global
	s.prefixRepo = repo
	s.mu.Unlock()
}

func (s *Service) prefixStores(ctx context.Context) (global, repo prefix.Store) {
	s.mu.Lock()
	if s.prefixGlobal != nil || s.prefixRepo != nil {
		g, r := s.prefixGlobal, s.prefixRepo
		s.mu.Unlock()
		return g, r
	}
	s.mu.Unlock()

	base := PrefixStatePath
	if base == "" {
		base = prefixBaseDir()
	}
	if base == "" {
		return nil, nil
	}
	g := prefix.NewFileStore(filepath.Join(base, "global"), model.ProfileScopeGlobal)
	key := "unknown"
	if cd, err := s.GitCommonDir(ctx); err == nil {
		key = repoKey(strings.TrimSpace(cd))
	}
	r := prefix.NewFileStore(filepath.Join(base, key), model.ProfileScopeRepo)

	s.mu.Lock()
	s.prefixGlobal, s.prefixRepo = g, r
	s.mu.Unlock()
	return g, r
}

// prefixBaseDir resolves <state>/gg/prefix cross-platform (mirrors profileBaseDir).
func prefixBaseDir() string {
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return filepath.Join(lad, "gg", "prefix")
		}
	}
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "gg", "prefix")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "gg", "prefix")
}

// Prefixes lists global rows then repo rows, each tagged with its scope.
func (s *Service) Prefixes(ctx context.Context) ([]model.Prefix, error) {
	global, repo := s.prefixStores(ctx)
	var out []model.Prefix
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

// AddPrefix validates the value's tokens, then routes to the scope's store.
func (s *Service) AddPrefix(ctx context.Context, p model.Prefix) (model.Prefix, error) {
	if err := ValidatePrefixValue(p.Value); err != nil {
		return model.Prefix{}, err
	}
	global, repo := s.prefixStores(ctx)
	st := global
	if p.Scope == model.ProfileScopeRepo {
		st = repo
	}
	if st == nil {
		return model.Prefix{}, os.ErrInvalid
	}
	return st.Add(p)
}

// RemovePrefix removes id from the store matching scope.
func (s *Service) RemovePrefix(ctx context.Context, scope model.ProfileScope, id string) error {
	global, repo := s.prefixStores(ctx)
	st := global
	if scope == model.ProfileScopeRepo {
		st = repo
	}
	if st == nil {
		return os.ErrInvalid
	}
	return st.Remove(id)
}

// ValidatePrefixValue rejects an empty value, the <branch> token, and any
// unknown/malformed token. Well-formedness is proven by a dry resolve with
// placeholder inputs for every <user:…> label and 0 for every <seq:…> counter.
func ValidatePrefixValue(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("invalid prefix: empty value")
	}
	if strings.Contains(value, "<branch>") {
		return fmt.Errorf("invalid prefix: <branch> is not allowed in a prefix")
	}
	inputs := map[string]string{}
	for _, l := range template.UserLabels(value) {
		inputs[l] = "x"
	}
	seqs := map[string]int{}
	for _, n := range template.SeqNames(value) {
		seqs[n] = 0
	}
	ctx := template.Ctx{
		ParentBranch: "parent",
		Repo:         "repo",
		Seqs:         seqs,
		Now:          time.Now,
		Rand:         rand.New(rand.NewPCG(1, 2)),
	}
	if _, err := template.Resolve(value, inputs, ctx); err != nil {
		return fmt.Errorf("invalid prefix: %w", err)
	}
	return nil
}
```

> Note: if `Service` has no `mu`/`repoKey`/`GitCommonDir`, they already exist (used by `profilestore.go`). This file reuses them.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/domain/ -run 'Prefix|ValidatePrefix' -v`
Expected: PASS.

- [ ] **Step 6: Run the archtest guard**

Add `internal/prefix` to the archtest forbidden-import lists for `internal/tui` and `internal/cli` if those tests enumerate packages explicitly. Locate with:

Run: `grep -rn "internal/profile" internal/archtest/`
Then mirror each `internal/profile` entry with `internal/prefix`. Run: `go test ./internal/archtest/ -v` → PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/domain/prefixstore.go internal/domain/service.go internal/domain/prefixstore_test.go internal/archtest/
git commit -m "feat(domain): prefix stores, Prefixes/AddPrefix/RemovePrefix, token validation"
```

---

### Task 3: `gg prefix` CLI

**Files:**
- Create: `internal/cli/prefix.go`
- Modify: `internal/cli/cli.go` (add `case "prefix": return cmdPrefix(svc, rest, stdout, stderr)` next to `case "bookmark"` ~line 74)
- Test: `internal/cli/prefix_test.go`

**Interfaces:**
- Consumes: `(*domain.Service).Prefixes/AddPrefix/RemovePrefix`, `prefix.PrefixID`, `model.Prefix`, `model.ProfileScopeGlobal/Repo`.
- Produces: `cmdPrefix(svc *domain.Service, args []string, stdout, stderr io.Writer) int`.

- [ ] **Step 1: Write the failing CLI test**

`internal/cli/prefix_test.go` (follow the existing CLI tests' harness — they set `domain.PrefixStatePath = t.TempDir()` and build a `*domain.Service`; copy the setup from `bookmark_test.go`):

```go
package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestPrefixAddListRemove(t *testing.T) {
	svc := newTestService(t) // same helper bookmark_test.go uses
	domain.PrefixStatePath = t.TempDir()
	t.Cleanup(func() { domain.PrefixStatePath = "" })

	var out, errb bytes.Buffer
	if code := cmdPrefix(svc, []string{"add", "feat/", "--global"}, &out, &errb); code != 0 {
		t.Fatalf("add code=%d err=%s", code, errb.String())
	}

	out.Reset()
	if code := cmdPrefix(svc, []string{"ls"}, &out, &errb); code != 0 {
		t.Fatalf("ls code=%d", code)
	}
	if !strings.Contains(out.String(), "feat/") || !strings.Contains(out.String(), "global") {
		t.Fatalf("ls out = %q", out.String())
	}

	out.Reset()
	if code := cmdPrefix(svc, []string{"rm", "feat/"}, &out, &errb); code != 0 {
		t.Fatalf("rm code=%d err=%s", code, errb.String())
	}
	out.Reset()
	_ = cmdPrefix(svc, []string{"ls"}, &out, &errb)
	if strings.Contains(out.String(), "feat/") {
		t.Fatalf("still listed after rm: %q", out.String())
	}
}

func TestPrefixAddRejectsBranchToken(t *testing.T) {
	svc := newTestService(t)
	domain.PrefixStatePath = t.TempDir()
	t.Cleanup(func() { domain.PrefixStatePath = "" })
	var out, errb bytes.Buffer
	if code := cmdPrefix(svc, []string{"add", "x-<branch>"}, &out, &errb); code == 0 {
		t.Fatalf("want non-zero exit for <branch>")
	}
}
```

> If `newTestService` does not exist in the cli test package, copy the service-construction pattern from `internal/cli/bookmark_test.go` (search it: `grep -n "func newTestService\|domain.New\|BookmarkStatePath" internal/cli/bookmark_test.go`).

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/ -run TestPrefix -v`
Expected: FAIL (`cmdPrefix` undefined).

- [ ] **Step 3: Write `cmdPrefix`**

`internal/cli/prefix.go`:

```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/prefix"
)

// cmdPrefix implements `gg prefix <ls|add|rm> ...`: the writable two-scope
// registry of branch-name prefixes (skeletons) selectable at create time.
func cmdPrefix(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg prefix <ls|add|rm> ...")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "ls", "list":
		return prefixList(svc, rest, stdout, stderr)
	case "add":
		return prefixAdd(svc, rest, stdout, stderr)
	case "rm", "remove":
		return prefixRemove(svc, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "prefix: unknown subcommand %q\n", sub)
		return 2
	}
}

func prefixList(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if err := flag.NewFlagSet("prefix ls", flag.ContinueOnError).Parse(args); err != nil {
		return 2
	}
	ps, err := svc.Prefixes(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, p := range ps {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", p.ID, p.Scope.String(), p.Value)
	}
	return 0
}

func prefixAdd(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("prefix add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	global := fs.Bool("global", false, "store in the global (every-repo) scope")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg prefix add <value> [--global]")
		return 2
	}
	scope := model.ProfileScopeRepo
	if *global {
		scope = model.ProfileScopeGlobal
	}
	stored, err := svc.AddPrefix(context.Background(), model.Prefix{Value: fs.Arg(0), Scope: scope})
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintln(stdout, stored.Value)
	return 0
}

func prefixRemove(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("prefix rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	global := fs.Bool("global", false, "remove from the global scope (default: repo)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg prefix rm <value> [--global]")
		return 2
	}
	id := prefix.PrefixID(fs.Arg(0))
	scope := model.ProfileScopeRepo
	if *global {
		scope = model.ProfileScopeGlobal
	}
	if err := svc.RemovePrefix(context.Background(), scope, id); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}
```

- [ ] **Step 4: Wire dispatch**

In `internal/cli/cli.go`, after `case "bookmark":`:

```go
	case "prefix":
		return cmdPrefix(svc, rest, stdout, stderr)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run TestPrefix -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/prefix.go internal/cli/cli.go internal/cli/prefix_test.go
git commit -m "feat(cli): gg prefix ls|add|rm"
```

---

### Task 4: TUI `templateFill` reusable resolution step

**Files:**
- Create: `internal/tui/template_fill.go`
- Test: `internal/tui/template_fill_test.go`

**Interfaces:**
- Consumes: `template.UserLabels`, `template.Resolve`, `template.Ctx`, `worktree.PeekSeqs`, `textfield`, `newTextField`.
- Produces:
  - `type templateFill struct { labels []string; fields []textfield; idx int }`
  - `newTemplateFill(value string) templateFill` — builds a field per `<user:LABEL>`.
  - `(*templateFill) needsInput() bool` — `len(labels) > 0`.
  - `(*templateFill) inputs() map[string]string` — label→value.
  - `(*templateFill) handleKey(msg tea.KeyMsg) (done, cancel bool)` — tab/enter advance; on last field enter → `done`; esc → `cancel`.
  - `(*templateFill) view(contentWidth int) []string` — one `viewField` per label.

This is a *pure* helper (no Model). Resolution itself stays at the call site via `template.Resolve(value, fill.inputs(), ctx)`.

- [ ] **Step 1: Write the failing test**

`internal/tui/template_fill_test.go`:

```go
package tui

import (
	"math/rand/v2"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/template"
)

func typeRunes(f *templateFill, s string) {
	for _, r := range s {
		f.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestTemplateFillNoLabelsFastPath(t *testing.T) {
	f := newTemplateFill("feat/")
	if f.needsInput() {
		t.Fatal("literal prefix should need no input")
	}
	out, err := template.Resolve("feat/", f.inputs(), neutralFillCtx())
	if err != nil || out != "feat/" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestTemplateFillCollectsLabelThenResolves(t *testing.T) {
	f := newTemplateFill("john/ISSUE-<user:issue-id>")
	if !f.needsInput() {
		t.Fatal("want needsInput")
	}
	typeRunes(&f, "1234")
	done, cancel := f.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !done || cancel {
		t.Fatalf("done=%v cancel=%v", done, cancel)
	}
	out, err := template.Resolve("john/ISSUE-<user:issue-id>", f.inputs(), neutralFillCtx())
	if err != nil {
		t.Fatal(err)
	}
	if out != "john/ISSUE-1234" {
		t.Fatalf("out = %q", out)
	}
}

func neutralFillCtx() template.Ctx {
	return template.Ctx{ParentBranch: "p", Repo: "r", Seqs: map[string]int{}, Now: time.Now, Rand: rand.New(rand.NewPCG(1, 2))}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TemplateFill -v`
Expected: FAIL (`templateFill` undefined).

- [ ] **Step 3: Write `template_fill.go`**

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/template"
)

// templateFill collects values for a prefix's interactive <user:LABEL> tokens
// (in first-appearance order) so the call site can template.Resolve the prefix.
// Pure: it never touches the Model.
type templateFill struct {
	labels []string
	fields []textfield
	idx    int
}

func newTemplateFill(value string) templateFill {
	labels := template.UserLabels(value)
	f := templateFill{labels: labels, fields: make([]textfield, len(labels))}
	for i := range f.fields {
		f.fields[i] = newTextField("")
	}
	return f
}

func (f *templateFill) needsInput() bool { return len(f.labels) > 0 }

func (f *templateFill) inputs() map[string]string {
	out := make(map[string]string, len(f.labels))
	for i, l := range f.labels {
		out[l] = f.fields[i].Value()
	}
	return out
}

// handleKey routes one key. tab/enter advance; enter on the last field returns
// done=true; esc returns cancel=true. Other keys edit the focused field.
func (f *templateFill) handleKey(msg tea.KeyMsg) (done, cancel bool) {
	switch msg.Type {
	case tea.KeyEsc:
		return false, true
	case tea.KeyTab, tea.KeyEnter:
		if f.idx >= len(f.fields)-1 {
			return true, false
		}
		f.idx++
		return false, false
	default:
		if f.idx >= 0 && f.idx < len(f.fields) {
			f.fields[f.idx].HandleEditKey(msg)
		}
		return false, false
	}
}

func (f *templateFill) view(contentWidth int) []string {
	lines := make([]string, len(f.labels))
	for i, l := range f.labels {
		cursor := "  "
		if i == f.idx {
			cursor = "> "
		}
		lines[i] = viewField(cursor+l+": ", f.fields[i], i == f.idx, contentWidth)
	}
	return lines
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run TemplateFill -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/template_fill.go internal/tui/template_fill_test.go
git commit -m "feat(tui): templateFill — collect a prefix's <user> labels"
```

---

### Task 5: prefix picker popup + create-branch wiring

**Files:**
- Create: `internal/tui/prefix_picker.go`
- Modify: `internal/tui/branch_popup.go` (add `p` key → open picker; seed `name` on pick)
- Test: `internal/tui/prefix_picker_test.go`

**Interfaces:**
- Consumes: `domain.Service.Prefixes`, `templateFill`, `template.Resolve`, `template.Ctx`, `worktree.PeekSeqs`, `pushLayer`/`popLayer`, `winRow`/`renderWindow`/`popupBox`/`popupWideInnerWidth`/`popupTextWidth`/`selectedRow`/`overlayCenter`/`clipToHeight`, `model.Prefix`, `model.ProfileScopeRepo`.
- Produces:
  - `type prefixPicker struct { items []model.Prefix; rows []string; sel int; filter string; filtering bool; resolve func(value string, inputs map[string]string) (string, []string, error); onPick func(m Model, resolved string, seqNames []string) (Model, tea.Cmd); fill *templateFill; fillValue string }`
  - `func (m Model) openPrefixPicker(resolve func(string, map[string]string) (string, []string, error), onPick func(Model, string, []string) (Model, tea.Cmd)) tea.Cmd`
  - `prefixesLoadedMsg{ items []model.Prefix; err error }` handled in the model Update to push the picker.

> The `resolve` closure returns `(resolvedString, seqNamesUsed, error)` so the
> caller (create popup) can add the prefix's `<seq>` names to `pendingSeqBump`.

- [ ] **Step 1: Write the failing picker test**

`internal/tui/prefix_picker_test.go`:

```go
package tui

import (
	"math/rand/v2"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/template"
)

func testResolve(value string, inputs map[string]string) (string, []string, error) {
	ctx := template.Ctx{ParentBranch: "main", Repo: "repo", Seqs: map[string]int{}, Now: time.Now, Rand: rand.New(rand.NewPCG(1, 2))}
	out, err := template.Resolve(value, inputs, ctx)
	return out, template.SeqNames(value), err
}

func newTestPrefixPicker(items []model.Prefix, onPick func(Model, string, []string) (Model, tea.Cmd)) *prefixPicker {
	p := &prefixPicker{items: items, resolve: testResolve, onPick: onPick}
	for _, it := range items {
		p.rows = append(p.rows, it.Value)
	}
	return p
}

func TestPickerLiteralPrefixInsertsImmediately(t *testing.T) {
	var got string
	onPick := func(m Model, resolved string, seq []string) (Model, tea.Cmd) {
		got = resolved
		return m.popLayer(), nil
	}
	p := newTestPrefixPicker([]model.Prefix{{Value: "feat/"}}, onPick)
	m := Model{}
	m = m.pushLayer(p)
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if got != "feat/" {
		t.Fatalf("got %q, want feat/", got)
	}
}

func TestPickerTemplatedPrefixCollectsThenInserts(t *testing.T) {
	var got string
	onPick := func(m Model, resolved string, seq []string) (Model, tea.Cmd) {
		got = resolved
		return m.popLayer(), nil
	}
	p := newTestPrefixPicker([]model.Prefix{{Value: "john/ISSUE-<user:issue-id>"}}, onPick)
	m := Model{}
	m = m.pushLayer(p)
	// Enter selects → enters fill mode (label issue-id).
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	for _, r := range "1234" {
		m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter}) // finish fill
	if got != "john/ISSUE-1234" {
		t.Fatalf("got %q", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestPicker -v`
Expected: FAIL (`prefixPicker` undefined).

- [ ] **Step 3: Write `prefix_picker.go`**

```go
package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/model"
)

// prefixPicker is a select-only quick-switcher of branch-name prefixes (global
// + repo). On select it runs the templateFill step (if the prefix has <user:>
// labels) then hands the resolved string to onPick. resolve returns the
// resolved string plus the prefix's <seq> names so the opener can bump them.
type prefixPicker struct {
	items  []model.Prefix
	rows   []string // display strings, parallel to items
	sel    int
	filter string
	filtering bool

	resolve func(value string, inputs map[string]string) (string, []string, error)
	onPick  func(m Model, resolved string, seqNames []string) (Model, tea.Cmd)

	fill      *templateFill // non-nil while collecting labels
	fillValue string        // the prefix value being filled
}

type prefixesLoadedMsg struct {
	items   []model.Prefix
	resolve func(value string, inputs map[string]string) (string, []string, error)
	onPick  func(m Model, resolved string, seqNames []string) (Model, tea.Cmd)
	err     error
}

// openPrefixPicker loads prefixes off-thread; the resulting prefixesLoadedMsg
// (handled in Update) pushes the picker. The opener supplies resolve+onPick.
func (m Model) openPrefixPicker(
	resolve func(string, map[string]string) (string, []string, error),
	onPick func(Model, string, []string) (Model, tea.Cmd),
) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ps, err := svc.Prefixes(context.Background())
		return prefixesLoadedMsg{items: ps, resolve: resolve, onPick: onPick, err: err}
	}
}

func newPrefixPicker(msg prefixesLoadedMsg) *prefixPicker {
	p := &prefixPicker{items: msg.items, resolve: msg.resolve, onPick: msg.onPick}
	for _, it := range msg.items {
		p.rows = append(p.rows, it.Value)
	}
	return p
}

func (p *prefixPicker) visibleIdx() []int {
	var idx []int
	q := strings.ToLower(p.filter)
	for i, row := range p.rows {
		if q == "" || strings.Contains(strings.ToLower(row), q) {
			idx = append(idx, i)
		}
	}
	return idx
}

func (p *prefixPicker) selected() (model.Prefix, bool) {
	vis := p.visibleIdx()
	if p.sel < 0 || p.sel >= len(vis) {
		return model.Prefix{}, false
	}
	return p.items[vis[p.sel]], true
}

func (p *prefixPicker) moveSel(d int) {
	n := p.sel + d
	if hi := len(p.visibleIdx()) - 1; n > hi {
		n = hi
	}
	if n < 0 {
		n = 0
	}
	p.sel = n
}

func (p *prefixPicker) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// Fill sub-mode: collect <user> labels, then resolve + hand off.
	if p.fill != nil {
		done, cancel := p.fill.handleKey(msg)
		if cancel {
			p.fill = nil
			return m, nil
		}
		if done {
			return p.finish(m, p.fillValue, p.fill.inputs())
		}
		return m, nil
	}
	if p.filtering {
		switch msg.Type {
		case tea.KeyEsc:
			p.filtering, p.filter, p.sel = false, "", 0
		case tea.KeyEnter:
			p.filtering = false
		case tea.KeyBackspace, tea.KeyCtrlH:
			if r := []rune(p.filter); len(r) > 0 {
				p.filter, p.sel = string(r[:len(r)-1]), 0
			}
		case tea.KeyRunes:
			p.filter += string(msg.Runes)
			p.sel = 0
		}
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyUp:
		p.moveSel(-1)
	case tea.KeyDown:
		p.moveSel(1)
	case tea.KeyEnter:
		it, ok := p.selected()
		if !ok {
			return m, nil
		}
		f := newTemplateFill(it.Value)
		if f.needsInput() {
			p.fill, p.fillValue = &f, it.Value
			return m, nil
		}
		return p.finish(m, it.Value, map[string]string{})
	case tea.KeyRunes:
		switch msg.String() {
		case "/":
			p.filtering = true
		case "k":
			p.moveSel(-1)
		case "j":
			p.moveSel(1)
		}
	}
	return m, nil
}

// finish resolves the prefix and calls onPick with the resolved string + the
// prefix's <seq> names.
func (p *prefixPicker) finish(m Model, value string, inputs map[string]string) (Model, tea.Cmd) {
	resolved, seqNames, err := p.resolve(value, inputs)
	if err != nil {
		m.statusMsg = "prefix: " + err.Error()
		return m.popLayer(), nil
	}
	return p.onPick(m, resolved, seqNames)
}

func (p *prefixPicker) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *prefixPicker) box(m Model) string {
	w, _ := m.overlayDims()
	inner := popupWideInnerWidth(w)
	textW := popupTextWidth(inner)

	if p.fill != nil {
		parts := []string{"Fill " + p.fillValue, ""}
		parts = append(parts, p.fill.view(textW)...)
		parts = append(parts, "", "[tab] next  [enter] done  [esc] back")
		return popupBox(inner, strings.Join(parts, "\n"))
	}

	header := "Branch prefixes"
	if p.filtering {
		header += "  /" + p.filter + "█"
	} else if p.filter != "" {
		header += "  /" + p.filter
	}
	vis := p.visibleIdx()
	var body []string
	if len(vis) == 0 {
		body = []string{padRight("  (none — add in Settings → Branch prefixes)", textW)}
	} else {
		wr := make([]winRow, len(vis))
		for n, i := range vis {
			prefix := "  "
			var st lipgloss.Style
			if n == p.sel {
				prefix, st = "> ", selectedRow
			}
			tag := "[global]"
			if p.items[i].Scope == model.ProfileScopeRepo {
				tag = "[this repo]"
			}
			wr[n] = winRow{text: prefix + p.rows[i] + "  " + tag, style: st}
		}
		h := len(vis)
		if h > 12 {
			h = 12
		}
		body = renderWindow(wr, winOpts{w: textW, h: h, anchor: p.sel})
	}
	parts := append([]string{header, ""}, body...)
	parts = append(parts, "", "[enter] use  [/] filter  [esc] cancel")
	return popupBox(inner, strings.Join(parts, "\n"))
}
```

- [ ] **Step 4: Handle `prefixesLoadedMsg` in the model Update**

Find where `bookmarksLoadedMsg` is handled in `internal/tui/model.go` (`grep -n "bookmarksLoadedMsg" internal/tui/model.go`) and add a sibling case:

```go
	case prefixesLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "prefixes: " + msg.err.Error()
			return m, nil
		}
		m = m.pushLayer(newPrefixPicker(msg))
		return m, nil
```

- [ ] **Step 5: Run the picker tests to verify they pass**

Run: `go test ./internal/tui/ -run TestPicker -v`
Expected: PASS.

- [ ] **Step 6: Wire `p` into the create-branch popup**

In `internal/tui/branch_popup.go`, add a `tctx` + resolve/onPick. First add a fixed seq snapshot + ctx helper to the popup struct. Replace the `branchPopup` struct and `update`/`box` additions:

The branch popup treats every rune as name text, so binding the picker to a
plain `p` would steal that letter from names. Use **`ctrl+p`** (an unambiguous
chord that never appears in a branch name) to open the picker. In `update`, add
its case before the `default` that edits the field:

```go
	case tea.KeyCtrlP:
		return m, m.openPrefixPicker(p.resolvePrefix(m), p.onPrefixPicked())
```

So the full `update` switch becomes:

```go
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m = m.popLayer()
	case tea.KeyEnter:
		// (create path — see Step 6 below)
	case tea.KeyCtrlP:
		return m, m.openPrefixPicker(p.resolvePrefix(m), p.onPrefixPicked())
	case tea.KeySpace:
		// unchanged: dropped (branch names cannot contain spaces)
	default:
		p.name.HandleEditKey(msg)
	}
	return m, nil
```

Add helpers to `branch_popup.go`:

```go
import (
	"math/rand/v2"
	"time"

	"github.com/homeend/gigagit/internal/template"
	"github.com/homeend/gigagit/internal/worktree"
)

// resolvePrefix returns a closure resolving a prefix value against a ctx seeded
// from this popup (parent branch + now + a fresh rand + peeked seqs).
func (p *branchPopup) resolvePrefix(m Model) func(string, map[string]string) (string, []string, error) {
	gitDir := m.gitCommonDir
	parent := p.startPoint
	repo := worktree.RepoName(m.mainWorktreeRoot())
	now := time.Now()
	seed := rand.Uint64()
	return func(value string, inputs map[string]string) (string, []string, error) {
		names := worktree.Templates{Branch: value}.SeqNames()
		ctx := template.Ctx{
			ParentBranch: parent,
			Repo:         repo,
			Seqs:         worktree.PeekSeqs(gitDir, names),
			Now:          func() time.Time { return now },
			Rand:         rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)),
		}
		out, err := template.Resolve(value, inputs, ctx)
		return out, names, err
	}
}

// onPrefixPicked seeds the name field with the resolved prefix (cursor at end)
// and records the prefix's <seq> names to bump on create.
func (p *branchPopup) onPrefixPicked() func(Model, string, []string) (Model, tea.Cmd) {
	return func(m Model, resolved string, seqNames []string) (Model, tea.Cmd) {
		p.name = newTextField(resolved)
		p.prefixSeqNames = seqNames
		return m.popLayer(), nil
	}
}
```

Add `prefixSeqNames []string` to the `branchPopup` struct. In the `KeyEnter`
create path, set the bump before starting the op:

```go
	case tea.KeyEnter:
		if p.name.Value() == "" {
			return m, nil
		}
		op := engine.CreateBranch{Name: p.name.Value(), StartPoint: p.startPoint}
		if p.switchAfter {
			m.pendingSwitchBranch = p.name.Value()
		}
		m.pendingSeqBump = p.prefixSeqNames
		m = m.popLayer()
		return m.startOp(op)
```

Update the `box` footer hint to mention the prefix key:

```go
	b.WriteString("[type] name  [ctrl+p] use prefix  [enter] create  [esc] cancel")
```

> Confirm `newTextField` places the cursor at end of the seeded text (it does —
> see `cursor-aware-textfields`). If not, set the cursor to `len([]rune(resolved))`.

- [ ] **Step 7: Write a create-branch wiring test**

Add to `internal/tui/prefix_picker_test.go`:

```go
func TestBranchPopupPrefixSeedsName(t *testing.T) {
	bp := &branchPopup{startPoint: "main"}
	onPick := bp.onPrefixPicked()
	m := Model{}
	m, _ = onPick(m, "feat/login", []string{"sandbox_seq"})
	if bp.name.Value() != "feat/login" {
		t.Fatalf("name = %q", bp.name.Value())
	}
	if len(bp.prefixSeqNames) != 1 || bp.prefixSeqNames[0] != "sandbox_seq" {
		t.Fatalf("seqNames = %v", bp.prefixSeqNames)
	}
}
```

- [ ] **Step 8: Run the tui tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestPicker|TestBranchPopupPrefix|TemplateFill' -v`
Expected: PASS. Then `go build ./cmd/gg` → success.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/prefix_picker.go internal/tui/branch_popup.go internal/tui/prefix_picker_test.go internal/tui/model.go
git commit -m "feat(tui): prefix picker popup + create-branch 'p' to use a prefix"
```

---

### Task 6: create-worktree popup wiring

**Files:**
- Modify: `internal/tui/worktree_popup.go` (add `p` in `stAction`; pick → seed `stEdit`; union prefix seq names into `consumedSeqNames`)
- Test: `internal/tui/worktree_popup_test.go` (add cases)

**Interfaces:**
- Consumes: `openPrefixPicker`, `templateFill` (indirectly), the popup's existing `tctx()`.
- Produces: worktree popup `[p] use a prefix & edit` behavior.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/worktree_popup_test.go`:

```go
func TestWorktreePopupPrefixSeedsEdit(t *testing.T) {
	p := &worktreePopup{startPoint: "main", branchTmpl: "b/<random-alpha:4>", pathTmpl: "../<repo>.worktrees/<branch>", state: stAction}
	onPick := p.onPrefixPicked()
	m := Model{}
	m, _ = onPick(m, "feat/login", []string{"sandbox_seq"})
	if p.state != stEdit {
		t.Fatalf("state = %v, want stEdit", p.state)
	}
	if p.editBuf.Value() != "feat/login" {
		t.Fatalf("editBuf = %q", p.editBuf.Value())
	}
	if !containsAll(p.consumedSeqNames(), []string{"sandbox_seq"}) {
		t.Fatalf("consumed = %v", p.consumedSeqNames())
	}
}

func containsAll(have, want []string) bool {
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestWorktreePopupPrefix -v`
Expected: FAIL (`onPrefixPicked` undefined on worktreePopup).

- [ ] **Step 3: Add `p` handling + helpers to `worktree_popup.go`**

Add a `prefixSeqNames []string` field to `worktreePopup`. In `update`, in the
`stAction` switch (next to `case "e"`):

```go
		case "p":
			if p.existing {
				return m, nil // existing mode checks out the branch as-is; no new name
			}
			return m, m.openPrefixPicker(p.resolvePrefix(), p.onPrefixPicked())
```

The seq-peek for prefix-only counters needs the git common dir, which the popup
does not currently store. Add a `gitCommonDir string` field to `worktreePopup`
and set it in both `openWorktreePopup` and `openWorktreeAt` from `m.gitCommonDir`
(alongside the other fields). Then add the resolve helper (reusing `tctx()`),
importing `internal/config`:

```go
// resolvePrefix resolves a prefix value against this popup's fixed ctx, peeking
// any prefix-only <seq> counters into the ctx snapshot so the result is stable.
func (p *worktreePopup) resolvePrefix() func(string, map[string]string) (string, []string, error) {
	return func(value string, inputs map[string]string) (string, []string, error) {
		names := worktree.Templates{Branch: value}.SeqNames()
		ctx := p.tctx()
		for _, n := range names {
			if _, ok := ctx.Seqs[n]; !ok {
				ctx.Seqs[n] = config.PeekSeq(p.gitCommonDir, n)
			}
		}
		out, err := template.Resolve(value, inputs, ctx)
		return out, names, err
	}
}
```

Then add the pick handler:

```go
// onPrefixPicked seeds stEdit with the resolved prefix so the user appends the
// tail, and records the prefix's <seq> names for the create-time bump.
func (p *worktreePopup) onPrefixPicked() func(Model, string, []string) (Model, tea.Cmd) {
	return func(m Model, resolved string, seqNames []string) (Model, tea.Cmd) {
		p.editBuf = newTextField(resolved)
		p.state = stEdit
		p.prefixSeqNames = seqNames
		p.recompute()
		return m.popLayer(), nil
	}
}
```

Extend `consumedSeqNames` to union the prefix's seq names:

```go
func (p *worktreePopup) consumedSeqNames() []string {
	var base []string
	if p.existing || p.branchOverride != "" {
		base = worktree.Templates{Path: p.pathTmpl}.SeqNames()
	} else {
		base = p.seqNames
	}
	return appendDistinctAll(base, p.prefixSeqNames)
}
```

Add a small helper (or reuse `worktree.appendDistinct` if exported; otherwise
define locally):

```go
func appendDistinctAll(dst, extra []string) []string {
	seen := map[string]bool{}
	for _, x := range dst {
		seen[x] = true
	}
	for _, x := range extra {
		if !seen[x] {
			seen[x] = true
			dst = append(dst, x)
		}
	}
	return dst
}
```

Update the `stAction` footer hint in `box` to include the new key:

```go
		if p.existing {
			b.WriteString("[w] create  [W] create & switch  [esc] cancel")
		} else {
			b.WriteString("[w] create  [W] create & switch  [e] edit name  [p] use a prefix  [esc] cancel")
		}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/tui/ -run TestWorktreePopupPrefix -v`
Expected: PASS.

- [ ] **Step 5: Run the full TUI package + build**

Run: `go test ./internal/tui/ && go build ./cmd/gg`
Expected: PASS + build success.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/worktree_popup.go internal/tui/worktree_popup_test.go
git commit -m "feat(tui): create-worktree 'p' to use a prefix then edit"
```

---

### Task 7: Settings "Branch prefixes" sub-screen

**Files:**
- Create: `internal/tui/prefix_settings.go`
- Modify: `internal/tui/settings_popup.go` (add menu entry + route to the new view)
- Test: `internal/tui/prefix_settings_test.go`

**Interfaces:**
- Consumes: `domain.Service.Prefixes/AddPrefix/RemovePrefix`, `model.Prefix`, `model.ProfileScope`, `textfield`, `viewField`, `winRow`/`renderWindow`/`popupBox`.
- Produces: `type prefixSettingsView struct{...}`; `(Model).openPrefixSettings() (Model, tea.Cmd)`; `prefixDataMsg{items []model.Prefix; err error}`.

- [ ] **Step 1: Add the Settings menu entry**

In `internal/tui/settings_popup.go`:

```go
	settingsMenuPrefixes = "Branch prefixes"
```

```go
var settingsMenu = []string{settingsMenuAgents, settingsMenuIdentity, settingsMenuPrefixes, settingsMenuOpLog}
```

In the menu `tea.KeyEnter` switch (next to `case settingsMenuIdentity:`):

```go
			case settingsMenuPrefixes:
				return m.openPrefixSettings()
```

- [ ] **Step 2: Write the failing settings test**

`internal/tui/prefix_settings_test.go`:

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

func TestPrefixSettingsFormBuildsValidEntry(t *testing.T) {
	v := &prefixSettingsView{mode: pfForm}
	v.fValue = newTextField("feat/")
	v.scope = model.ProfileScopeGlobal
	p, ok := v.formPrefix()
	if !ok {
		t.Fatal("want ok")
	}
	if p.Value != "feat/" || p.Scope != model.ProfileScopeGlobal {
		t.Fatalf("p = %+v", p)
	}
}

func TestPrefixSettingsBrowseDeleteSelectsValid(t *testing.T) {
	v := &prefixSettingsView{
		items: []model.Prefix{{ID: "feat", Value: "feat/", Scope: model.ProfileScopeRepo}},
		mode:  pfBrowse,
	}
	// 'd' on the only row should target id "feat" scope repo — assert via the
	// helper that computes the delete target.
	id, scope, ok := v.deleteTarget()
	if !ok || id != "feat" || scope != model.ProfileScopeRepo {
		t.Fatalf("id=%q scope=%v ok=%v", id, scope, ok)
	}
	_ = tea.KeyMsg{}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestPrefixSettings -v`
Expected: FAIL (`prefixSettingsView` undefined).

- [ ] **Step 4: Write `prefix_settings.go`**

```go
package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/model"
)

// prefixSettingsView is the Settings sub-surface that manages branch prefixes
// (browse + add/edit + delete, Global|Repo). Mirrors identityView's structure.
type prefixSettingsView struct {
	loading bool
	items   []model.Prefix
	sel     int
	mode    pfMode

	fValue textfield
	scope  model.ProfileScope
	field  int // 0 = value, 1 = scope
}

type pfMode int

const (
	pfBrowse pfMode = iota
	pfForm
)

type prefixDataMsg struct {
	items []model.Prefix
	err   error
}

func (m Model) openPrefixSettings() (Model, tea.Cmd) {
	m = m.pushLayer(&prefixSettingsView{loading: true})
	return m, m.loadPrefixDataCmd()
}

func (m Model) loadPrefixDataCmd() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ps, err := svc.Prefixes(context.Background())
		return prefixDataMsg{items: ps, err: err}
	}
}

func (m Model) addPrefixCmd(p model.Prefix) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		if _, err := svc.AddPrefix(context.Background(), p); err != nil {
			return prefixDataMsg{err: err}
		}
		ps, err := svc.Prefixes(context.Background())
		return prefixDataMsg{items: ps, err: err}
	}
}

func (m Model) removePrefixCmd(scope model.ProfileScope, id string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		_ = svc.RemovePrefix(context.Background(), scope, id)
		ps, err := svc.Prefixes(context.Background())
		return prefixDataMsg{items: ps, err: err}
	}
}

func (v *prefixSettingsView) deleteTarget() (id string, scope model.ProfileScope, ok bool) {
	if v.sel < 0 || v.sel >= len(v.items) {
		return "", 0, false
	}
	p := v.items[v.sel]
	return p.ID, p.Scope, true
}

func (v *prefixSettingsView) formPrefix() (model.Prefix, bool) {
	val := strings.TrimSpace(v.fValue.Value())
	if val == "" {
		return model.Prefix{}, false
	}
	return model.Prefix{Value: val, Scope: v.scope}, true
}

func (v *prefixSettingsView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if v.mode == pfForm {
		return v.updateForm(m, msg)
	}
	return v.updateBrowse(m, msg)
}

func (v *prefixSettingsView) updateBrowse(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyUp:
		if v.sel > 0 {
			v.sel--
		}
		return m, nil
	case tea.KeyDown:
		if v.sel < len(v.items)-1 {
			v.sel++
		}
		return m, nil
	}
	switch msg.String() {
	case "n", "a":
		v.fValue = newTextField("")
		v.scope = model.ProfileScopeGlobal
		v.field = 0
		v.mode = pfForm
		return m, nil
	case "e":
		if v.sel < 0 || v.sel >= len(v.items) {
			return m, nil
		}
		p := v.items[v.sel]
		v.fValue = newTextField(p.Value)
		v.scope = p.Scope
		v.field = 0
		v.mode = pfForm
		return m, nil
	case "d":
		id, scope, ok := v.deleteTarget()
		if !ok {
			return m, nil
		}
		if v.sel > 0 {
			v.sel--
		}
		return m, m.removePrefixCmd(scope, id)
	}
	return m, nil
}

func (v *prefixSettingsView) updateForm(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		v.mode = pfBrowse
		return m, nil
	case tea.KeyUp:
		if v.field > 0 {
			v.field--
		}
		return m, nil
	case tea.KeyDown, tea.KeyTab:
		if v.field < 1 {
			v.field++
		}
		return m, nil
	case tea.KeyEnter:
		p, ok := v.formPrefix()
		if !ok {
			m.statusMsg = "prefix value is required"
			return m, nil
		}
		v.mode = pfBrowse
		return m, m.addPrefixCmd(p)
	}
	if v.field == 1 { // scope toggle
		switch msg.String() {
		case "left", "right", " ", "h", "l":
			if v.scope == model.ProfileScopeGlobal {
				v.scope = model.ProfileScopeRepo
			} else {
				v.scope = model.ProfileScopeGlobal
			}
		}
		return m, nil
	}
	v.fValue.HandleEditKey(msg)
	return m, nil
}

func (v *prefixSettingsView) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), v.box(m), w, h)
}

func (v *prefixSettingsView) box(m Model) string {
	w, _ := m.overlayDims()
	inner := popupWideInnerWidth(w)
	textW := popupTextWidth(inner)

	if v.mode == pfForm {
		scopeCursor := "  "
		if v.field == 1 {
			scopeCursor = "> "
		}
		scopeVal := "global (every repo)"
		if v.scope == model.ProfileScopeRepo {
			scopeVal = "this repo only"
		}
		cur := "  "
		if v.field == 0 {
			cur = "> "
		}
		parts := []string{
			"Add / edit branch prefix", "",
			viewField(cur+"value: ", v.fValue, v.field == 0, textW),
			scopeCursor + "scope: " + scopeVal,
			"",
			"Tokens: <user:LABEL> <seq:NAME:N> <date:FMT> <parent-branch> <repo> <random-*>",
			"",
			"[↑/↓] field  [←/→] scope  [enter] save  [esc] back",
		}
		return popupBox(inner, strings.Join(parts, "\n"))
	}

	parts := []string{"Branch prefixes", ""}
	if v.loading {
		parts = append(parts, "  (loading…)")
		return popupBox(inner, strings.Join(parts, "\n"))
	}
	if len(v.items) == 0 {
		parts = append(parts, "  (none yet — [n] to add)")
	} else {
		wr := make([]winRow, len(v.items))
		for i, p := range v.items {
			prefix := "  "
			var st lipgloss.Style
			if i == v.sel {
				prefix, st = "> ", selectedRow
			}
			tag := "[global]"
			if p.Scope == model.ProfileScopeRepo {
				tag = "[this repo]"
			}
			wr[i] = winRow{text: prefix + p.Value + "  " + tag, style: st}
		}
		h := len(v.items)
		if h > 10 {
			h = 10
		}
		parts = append(parts, renderWindow(wr, winOpts{w: textW, h: h, anchor: v.sel})...)
	}
	parts = append(parts, "", "[n] add  [e] edit  [d] delete  [esc] back")
	return popupBox(inner, strings.Join(parts, "\n"))
}
```

- [ ] **Step 5: Handle `prefixDataMsg` in the model Update**

Next to the `identityDataMsg` handler (`grep -n "identityDataMsg" internal/tui/model.go`), add:

```go
	case prefixDataMsg:
		if v := m.topPrefixSettings(); v != nil {
			v.loading = false
			if msg.err != nil {
				m.statusMsg = "prefixes: " + msg.err.Error()
			} else {
				v.items = msg.items
				if v.sel >= len(v.items) {
					v.sel = len(v.items) - 1
				}
				if v.sel < 0 {
					v.sel = 0
				}
			}
		}
		return m, nil
```

Add a `topPrefixSettings()` accessor mirroring how `identityDataMsg` finds its
view (`grep -n "identityView" internal/tui/model.go` to see the accessor
pattern; if the identity handler type-asserts the top layer, do the same):

```go
func (m Model) topPrefixSettings() *prefixSettingsView {
	if v, ok := m.topLayer().(*prefixSettingsView); ok {
		return v
	}
	return nil
}
```

> If there is no `topLayer()` helper, mirror exactly how `identityDataMsg` reaches
> its `*identityView` (it may iterate `m.layers`); copy that mechanism.

- [ ] **Step 6: Run the settings tests + full TUI + build**

Run: `go test ./internal/tui/ -run TestPrefixSettings -v && go test ./internal/tui/ && go build ./cmd/gg`
Expected: PASS + build success.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/prefix_settings.go internal/tui/settings_popup.go internal/tui/prefix_settings_test.go internal/tui/model.go
git commit -m "feat(tui): Settings → Branch prefixes (add/edit/delete, Global|Repo)"
```

---

### Task 8: docs, help/footer, agent skill

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md` (keybinding table + CLI section)
- Modify: `CLAUDE.md` (package map: add `internal/prefix`)
- Modify: `internal/tui/help.go` (advertise `p` in create-branch + create-worktree)
- Modify: `internal/tui/footer.go` (if it lists popup keys for these surfaces)
- Modify: `internal/agentskill/using-gg.md` + bump `internal/agentskill` version marker
- Test: existing `internal/agentskill` version test (if any) + `go test ./...`

**Interfaces:** none (docs/strings).

- [ ] **Step 1: CHANGELOG entry**

Add under the top/unreleased section of `CHANGELOG.md`:

```markdown
### Added
- **Branch prefixes** — a writable two-scope (global + per-repo) registry of
  reusable, templated branch-name skeletons. Press `ctrl+p` in the create-branch
  (`b`/`B`) popup, or `p` in the create-worktree popup, to pick one; interactive
  `<user:…>` labels are collected, the template is resolved, and the result seeds
  the branch name for you to complete. Manage them in Settings (`,`) → Branch
  prefixes, or via `gg prefix ls | add <value> [--global] | rm <value>`.
```

- [ ] **Step 2: README — keybindings + CLI**

In the README keybinding/popup section, add rows noting `ctrl+p` = "use a branch
prefix" in the create-branch dialog and `p` = "use a branch prefix" in the
create-worktree dialog. In the CLI command list, add:

```
gg prefix ls                    List branch prefixes (global + repo)
gg prefix add <value> [--global]  Add a prefix (default scope: this repo)
gg prefix rm <value> [--global]   Remove a prefix
```

- [ ] **Step 3: CLAUDE.md package map**

Add a row to the `internal/` package table:

```markdown
| `prefix` | Writable two-scope registry of reusable, templated branch-name prefixes ("skeletons") behind a fixed `Store` (default impl: atomic-rewrite `prefixes.toml` under XDG state, global + per-repo keyed by git common dir). Owned by `domain` (`Prefixes`/`AddPrefix`/`RemovePrefix` + `ValidatePrefixValue`); frontends never import it (archtest-guarded). Surfaced by the TUI `p` picker in both create popups, the Settings → Branch prefixes manager, and `gg prefix`. |
```

- [ ] **Step 4: help.go**

Locate the create-branch and create-worktree help entries (`grep -n "worktree\|branch" internal/tui/help.go`) and add the keybinding lines: for create-branch, `ctrl+p — use a saved branch prefix`; for create-worktree, `p — use a saved branch prefix (then edit)`. Per the project convention, every keybinding lands in help.go AND the footer.

- [ ] **Step 5: footer.go**

If `internal/tui/footer.go` enumerates the keys for these popups, add `p`
(keeping it tight — the footer truncates). Verify with `grep -n "edit name\|create & switch" internal/tui/footer.go`.

- [ ] **Step 6: agent skill**

Add a short `gg prefix` section to `internal/agentskill/using-gg.md` (mirror the
`gg bookmark` section), then bump `agentskill.Version` (`grep -rn "Version" internal/agentskill/*.go`). Run any version-marker test:

Run: `go test ./internal/agentskill/ -v`
Expected: PASS (update the expected version if the test pins it).

- [ ] **Step 7: Full suite + build**

Run: `./test.sh` (vet+gofmt → unit → e2e).
Expected: PASS. Then `go build ./cmd/gg`.

- [ ] **Step 8: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md internal/tui/help.go internal/tui/footer.go internal/agentskill/
git commit -m "docs: branch prefixes — changelog, README, CLAUDE.md, help, agent skill"
```

---

## Final verification

- [ ] `./test.sh race` passes (run before merge).
- [ ] `go build -o ./gg ./cmd/gg` in the worktree; smoke-test:
  - `./gg prefix add 'john_smith/ISSUE-<user:issue-id>'`
  - `./gg prefix ls` shows it tagged `repo`.
  - In the TUI: `b` → `p` → pick it → fill `issue-id` → name seeded → append text → create.
  - Settings (`,`) → Branch prefixes → add/edit/delete round-trip.
- [ ] Provide the user the worktree's absolute `./gg` path for manual testing.
```
