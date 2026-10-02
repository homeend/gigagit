# Text Templates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo forbids implementer subagents — the session that wrote the plan executes it; the final whole-branch review may be a read-only subagent). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Titled, multi-line text templates with the branch-prefix token grammar, kept in a two-scope store, filled and copied from an `alt+x` TUI window, the `gg template` CLI and a gg web view.

**Architecture:** A new records-only store package (`internal/texttmpl`, the `prefix` twin) owned by `domain`; a text-flavoured resolver in `internal/template` (`ResolveText`: unknown `<…>` stays literal); domain queries/commands the three frontends share; one layer-stack TUI window with four modes (browse, fill, rendered, form); a CLI verb family; web handlers + one static module.

**Tech Stack:** Go 1.26, go-toml/v2, Bubble Tea, vanilla JS (embedded SPA).

**Spec:** `docs/superpowers/specs/2026-10-02-text-templates-design.md`

## Global Constraints

- Work only in `/work/gigagit/.claude/worktrees/text-templates` (branch `feat/text-templates`); every command starts with `cd` to that absolute path. Never `git add -A`.
- Key is `alt+x`; `alt+t` is untouched. Inside a focused console the key goes to the agent (do NOT add it to the passthrough set at `internal/tui/console.go:375`).
- Body is edited in `$EDITOR` through `handover()` — never a raw `tea.ExecProcess`.
- List shows at most 8 rows. Variables are listed BELOW the body. `y` copies and closes the WHOLE window. Variable values are single-line.
- Limits: title ≤ 80 runes, one line; body ≤ 64 KiB (65536 bytes), non-empty after trimming trailing whitespace.
- Unknown `<…>` in a text template is literal text; a known token with bad arguments is an error.
- `<seq:…>` counters are peeked on preview and consumed only when the text is taken.
- `tui`/`cli`/`web` never import `internal/texttmpl` or `internal/template` for this feature — they go through `domain`.
- Every TUI string goes through `i18n.T` with a literal key present in all four bundles (use the `adding-translations` skill).
- CLI flags mirror `gg prefix`: scope defaults to repo, `--global` selects global; variables are `--set label=value`.
- New tests call `t.Parallel()` unless they use `t.Setenv`.

## Review Focus

1. A body with a bare `<` before a real token on the same line (`if a < b then <user:name>`) — the token must still resolve; the shared `<([^>]+)>` regexp would swallow it. Pinned in Task 1.
2. A non-ASCII title (`リリース告知`, `Отчёт`) — must get a usable ID, not an empty slug; a title with no letter or digit is refused with a clear error. Pinned in Task 2.
3. Renaming a template onto another template's title in the same scope — refused, neither row lost. Pinned in Task 2.
4. The editor exits non-zero or leaves the file empty/unchanged — nothing is saved, the temp file is removed, the window is still usable. Pinned in Task 7.
5. A filled value containing `<…>` (the user types `<seq:x>` into a variable) — inserted verbatim, never re-resolved. Pinned in Task 1.

---

### Task 1: `template.ResolveText` + `TextTokens`

**Files:**
- Create: `internal/template/text.go`
- Test: `internal/template/text_test.go`

**Interfaces:**
- Produces: `func ResolveText(tmpl string, inputs map[string]string, ctx Ctx) (string, error)`; `type TextTokenSet struct{ UserLabels, SeqNames, Automatic []string }`; `func TextTokens(tmpl string) TextTokenSet`.

- [ ] **Step 1: Write the failing tests**

```go
package template

import (
	"math/rand/v2"
	"reflect"
	"testing"
	"time"
)

func textCtx() Ctx {
	return Ctx{
		ParentBranch: "main", Repo: "gigagit", Branch: "feat/x",
		Seqs: map[string]int{"n": 7},
		Now:  func() time.Time { return time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC) },
		Rand: rand.New(rand.NewPCG(1, 2)),
	}
}

func TestResolveTextKnownTokens(t *testing.T) {
	t.Parallel()
	got, err := ResolveText("## <user:title>\nBranch: <branch> · <date> · #<seq:n:3> · <repo>",
		map[string]string{"title": "Hello"}, textCtx())
	if err != nil {
		t.Fatal(err)
	}
	want := "## Hello\nBranch: feat/x · 2026-10-02 · #007 · gigagit"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveTextUnknownTokenStaysLiteral(t *testing.T) {
	t.Parallel()
	in := "line<br>mail <me@example.com> and <div class=\"x\">"
	got, err := ResolveText(in, nil, textCtx())
	if err != nil || got != in {
		t.Fatalf("got %q, %v; want the input unchanged", got, err)
	}
}

// Review Focus 1: a bare '<' must not swallow the token that follows it.
func TestResolveTextBareAngleBeforeToken(t *testing.T) {
	t.Parallel()
	got, err := ResolveText("if a < b then <user:name>\nx > y", map[string]string{"name": "N"}, textCtx())
	if err != nil || got != "if a < b then N\nx > y" {
		t.Fatalf("got %q, %v", got, err)
	}
}

// Review Focus 5: a filled value is inserted verbatim, never re-resolved.
func TestResolveTextValueNotReResolved(t *testing.T) {
	t.Parallel()
	got, err := ResolveText("<user:v>", map[string]string{"v": "<seq:n> <user:v>"}, textCtx())
	if err != nil || got != "<seq:n> <user:v>" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestResolveTextKnownTokenErrors(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"<seq>", "<user>", "<seq:n:x>", "<user:missing>"} {
		if _, err := ResolveText(in, nil, textCtx()); err == nil {
			t.Errorf("%q: want an error", in)
		}
	}
}

func TestResolveTextUnsetBranchIsEmpty(t *testing.T) {
	t.Parallel()
	c := textCtx()
	c.Branch = ""
	got, err := ResolveText("[<branch>]", nil, c)
	if err != nil || got != "[]" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestTextTokens(t *testing.T) {
	t.Parallel()
	got := TextTokens("<user:a> <br> <date> <user:b> <user:a> <seq:n:2> <branch> <date> a < b <user:c>")
	want := TextTokenSet{
		UserLabels: []string{"a", "b", "c"},
		SeqNames:   []string{"n"},
		Automatic:  []string{"<date>", "<seq:n:2>", "<branch>"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/text-templates && rtk go test ./internal/template/ -run 'ResolveText|TextTokens'`
Expected: FAIL — `undefined: ResolveText`.

- [ ] **Step 3: Implement**

```go
package template

// textTokenRe is tokenRe for prose: a token never spans a line and never
// contains a '<', so a bare '<' ("a < b") cannot swallow the token after it.
var textTokenRe = regexp.MustCompile(`<([^<>\n]+)>`)

// textTokenKinds are the token prefixes ResolveText resolves; any other <…>
// is literal text (HTML tags, <me@example.com>).
var textTokenKinds = map[string]bool{
	"parent-branch": true, "repo": true, "branch": true, "date": true,
	"seq": true, "user": true, "random-alpha": true, "random-num": true,
}

// ResolveText is Resolve for a text template (a multi-line body). It differs
// in three ways: a <…> whose kind is not a token stays in the output as
// written; <branch> is Ctx.Branch verbatim (an unset branch is ""); a token
// never spans lines. A KNOWN token with malformed arguments is still an
// error. Substituted values are never scanned again.
func ResolveText(tmpl string, inputs map[string]string, ctx Ctx) (string, error) {
	var firstErr error
	out := textTokenRe.ReplaceAllStringFunc(tmpl, func(tok string) string {
		body := tok[1 : len(tok)-1]
		kind, _, _ := cutColon(body)
		if !textTokenKinds[kind] {
			return tok
		}
		if body == "branch" {
			return ctx.Branch
		}
		val, err := resolveToken(body, inputs, ctx)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return ""
		}
		return val
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

// TextTokenSet is what a text template asks for: the <user:…> labels to
// prompt for, the <seq:…> counters it consumes, and the other tokens it
// uses, each as written (for the "Automatic" line). All distinct, in order
// of first appearance.
type TextTokenSet struct {
	UserLabels []string
	SeqNames   []string
	Automatic  []string
}

// TextTokens scans a text template with ResolveText's token rules.
func TextTokens(tmpl string) TextTokenSet {
	var set TextTokenSet
	seen := map[string]bool{}
	add := func(list *[]string, key, v string) {
		if !seen[key] {
			seen[key] = true
			*list = append(*list, v)
		}
	}
	for _, m := range textTokenRe.FindAllStringSubmatch(tmpl, -1) {
		kind, rest, _ := cutColon(m[1])
		if !textTokenKinds[kind] {
			continue
		}
		switch kind {
		case "user":
			add(&set.UserLabels, "u:"+rest, rest)
		case "seq":
			name, _, _ := cutColon(rest)
			add(&set.SeqNames, "s:"+name, name)
			add(&set.Automatic, "a:"+m[0], m[0])
		default:
			add(&set.Automatic, "a:"+m[0], m[0])
		}
	}
	return set
}
```

Add `import "regexp"`. Note `<branch:anything>` falls to `resolveToken`'s `branch` case, which errors on an unset branch — acceptable (a known token with arguments it does not take); `<user>` errors through `resolveToken`.

- [ ] **Step 4: Run to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/text-templates && rtk go test ./internal/template/`
Expected: PASS (the existing `Resolve` tests too).

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/text-templates && git add internal/template/text.go internal/template/text_test.go && git commit -m "feat(template): ResolveText + TextTokens for text templates"
```

---

### Task 2: `model.TextTemplate` + the `texttmpl` store

**Files:**
- Create: `internal/model/texttemplate.go`, `internal/texttmpl/store.go`, `internal/texttmpl/file_store.go`
- Test: `internal/texttmpl/file_store_test.go`
- Modify: `internal/archtest` allow-lists if a new-package guard fails (run the archtest and follow its message)

**Interfaces:**
- Produces: `model.TextTemplate{ID, Title, Body string; Scope model.ProfileScope; Created time.Time}`; `texttmpl.Store` (`Add(t) (t, error)`, `Get(id)`, `List()`, `Update(id string, t) (t, error)`, `Remove(id) error`); `texttmpl.NewFileStore(root string, scope model.ProfileScope) *FileStore`; `texttmpl.ID(title string) string`; errors `ErrNotFound`, `ErrDuplicate`, `ErrNoID`.

- [ ] **Step 1: Write the failing tests**

```go
package texttmpl

import (
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestFileStoreRoundTrip(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir(), model.ProfileScopeRepo)
	body := "## <user:title>\n\nline two\n"
	added, err := fs.Add(model.TextTemplate{Title: "PR description", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if added.ID != "pr-description" || added.Scope != model.ProfileScopeRepo || added.Created.IsZero() {
		t.Fatalf("added = %+v", added)
	}
	got, err := fs.Get("pr-description")
	if err != nil || got.Body != body || got.Scope != model.ProfileScopeRepo {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if err := fs.Remove(added.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := fs.List(); len(list) != 0 {
		t.Fatalf("after remove: %+v", list)
	}
	if err := fs.Remove(added.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second remove: %v", err)
	}
}

func TestFileStoreAddDuplicateTitle(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	if _, err := fs.Add(model.TextTemplate{Title: "Note", Body: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Add(model.TextTemplate{Title: "note", Body: "b"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("err = %v, want ErrDuplicate", err)
	}
}

func TestFileStoreListSortedByTitle(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	for _, title := range []string{"beta", "Alpha", "gamma"} {
		if _, err := fs.Add(model.TextTemplate{Title: title, Body: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := fs.List()
	if len(list) != 3 || list[0].Title != "Alpha" || list[1].Title != "beta" || list[2].Title != "gamma" {
		t.Fatalf("list = %+v", list)
	}
}

func TestFileStoreUpdate(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	a, _ := fs.Add(model.TextTemplate{Title: "One", Body: "a"})
	_, _ = fs.Add(model.TextTemplate{Title: "Two", Body: "b"})

	up, err := fs.Update(a.ID, model.TextTemplate{Title: "One", Body: "changed"})
	if err != nil || up.ID != "one" || up.Body != "changed" || !up.Created.Equal(a.Created) {
		t.Fatalf("body update = %+v, %v", up, err)
	}
	up, err = fs.Update("one", model.TextTemplate{Title: "Uno", Body: "changed"})
	if err != nil || up.ID != "uno" {
		t.Fatalf("rename = %+v, %v", up, err)
	}
	if _, err := fs.Get("one"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old id still there: %v", err)
	}
	// Review Focus 3: a rename onto another title is refused, nothing lost.
	if _, err := fs.Update("uno", model.TextTemplate{Title: "Two", Body: "x"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("rename onto Two: %v", err)
	}
	if list, _ := fs.List(); len(list) != 2 {
		t.Fatalf("rows lost: %+v", list)
	}
	if _, err := fs.Update("nope", model.TextTemplate{Title: "X", Body: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
}

// Review Focus 2: a non-ASCII title gets a usable id; symbols alone do not.
func TestIDUnicode(t *testing.T) {
	t.Parallel()
	for title, want := range map[string]string{
		"PR description!": "pr-description",
		"リリース告知":          "リリース告知",
		"Отчёт за день":   "отчёт-за-день",
		"  --  ":          "",
	} {
		if got := ID(title); got != want {
			t.Errorf("ID(%q) = %q, want %q", title, got, want)
		}
	}
	fs := NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	if _, err := fs.Add(model.TextTemplate{Title: "!!!", Body: "x"}); !errors.Is(err, ErrNoID) {
		t.Fatalf("symbol-only title: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/text-templates && rtk go test ./internal/texttmpl/`
Expected: FAIL — package does not compile.

- [ ] **Step 3: Implement**

`internal/model/texttemplate.go`:

```go
package model

import "time"

// TextTemplate is a reusable, titled piece of multi-line text with <…> tokens
// (the branch-prefix grammar). Its identity is its Title (slugged into ID);
// Scope is implied by which store holds it and is set on List/Add.
type TextTemplate struct {
	ID      string       `toml:"id"`
	Title   string       `toml:"title"`
	Body    string       `toml:"body"`
	Scope   ProfileScope `toml:"-"`
	Created time.Time    `toml:"created"`
}
```

`internal/texttmpl/store.go`:

```go
// Package texttmpl is gigagit's writable registry of text templates: titled,
// multi-line texts with <…> tokens. Two scopes — global (every repo) and
// repo-specific — each a separate file-backed store (the prefix precedent).
package texttmpl

import (
	"errors"

	"github.com/homeend/gigagit/internal/model"
)

var (
	// ErrNotFound is returned by Get/Update/Remove for an unknown id.
	ErrNotFound = errors.New("text template: not found")
	// ErrDuplicate is returned when a title's id is already taken in the scope.
	ErrDuplicate = errors.New("text template: a template with this title already exists")
	// ErrNoID is returned for a title with no letter or digit to build an id from.
	ErrNoID = errors.New("text template: the title needs a letter or a digit")
)

// Store persists text templates for one scope (atomic rewrite, last-writer-wins).
type Store interface {
	Add(t model.TextTemplate) (model.TextTemplate, error)
	Get(id string) (model.TextTemplate, error)
	List() ([]model.TextTemplate, error)
	// Update replaces the template stored under id. The id follows the title;
	// Created is kept.
	Update(id string, t model.TextTemplate) (model.TextTemplate, error)
	Remove(id string) error
}
```

`internal/texttmpl/file_store.go` — copy `internal/prefix/file_store.go`'s `read`/`write` verbatim with these substitutions: file name `texttemplates.toml`, temp pattern `texttemplates-*.toml`, `index{Templates []model.TextTemplate \`toml:"templates"\`}`. Then:

```go
var slugRe = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// ID derives a template's id from its title: lower-cased, every run of
// non-letter/non-digit runes becomes one '-'. "" when the title has neither.
func ID(title string) string {
	return strings.Trim(strings.ToLower(slugRe.ReplaceAllString(title, "-")), "-")
}

func (fs *FileStore) Add(t model.TextTemplate) (model.TextTemplate, error) {
	t.ID = ID(t.Title)
	if t.ID == "" {
		return model.TextTemplate{}, ErrNoID
	}
	t.Scope = fs.scope
	if t.Created.IsZero() {
		t.Created = time.Now()
	}
	idx := fs.read()
	for _, have := range idx.Templates {
		if have.ID == t.ID {
			return model.TextTemplate{}, ErrDuplicate
		}
	}
	idx.Templates = append(idx.Templates, t)
	return t, fs.write(idx)
}

func (fs *FileStore) Get(id string) (model.TextTemplate, error) {
	for _, t := range fs.read().Templates {
		if t.ID == id {
			return t, nil
		}
	}
	return model.TextTemplate{}, ErrNotFound
}

func (fs *FileStore) List() ([]model.TextTemplate, error) {
	ts := fs.read().Templates
	sort.SliceStable(ts, func(a, b int) bool {
		return strings.ToLower(ts[a].Title) < strings.ToLower(ts[b].Title)
	})
	return ts, nil
}

func (fs *FileStore) Update(id string, t model.TextTemplate) (model.TextTemplate, error) {
	newID := ID(t.Title)
	if newID == "" {
		return model.TextTemplate{}, ErrNoID
	}
	idx := fs.read()
	at := -1
	for i, have := range idx.Templates {
		if have.ID == id {
			at = i
		} else if have.ID == newID {
			return model.TextTemplate{}, ErrDuplicate
		}
	}
	if at < 0 {
		return model.TextTemplate{}, ErrNotFound
	}
	t.ID, t.Scope, t.Created = newID, fs.scope, idx.Templates[at].Created
	idx.Templates[at] = t
	return t, fs.write(idx)
}
```

`Remove` is `prefix.FileStore.Remove` over `idx.Templates`. End with `var _ Store = (*FileStore)(nil)`.

Note for the duplicate check in `Update`: the loop must finish before deciding — a duplicate found AFTER `at` still returns `ErrDuplicate` (the code above does this because the `return` is inside the loop for any other row).

- [ ] **Step 4: Run to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/text-templates && rtk go test ./internal/texttmpl/ ./internal/model/ ./internal/archtest/`
Expected: PASS. If archtest lists allowed importers per package, add `texttmpl` exactly where `prefix` appears.

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/text-templates && git add internal/model/texttemplate.go internal/texttmpl internal/archtest && git commit -m "feat(texttmpl): two-scope text template store"
```

---

### Task 3: Domain API

**Files:**
- Create: `internal/domain/texttemplates.go`
- Modify: `internal/domain/service.go:90` (add `textGlobal, textRepo texttmpl.Store` beside `prefixGlobal`/`prefixRepo`)
- Test: `internal/domain/texttemplates_test.go`

**Interfaces:**
- Consumes: Task 1 `template.ResolveText`/`TextTokens`; Task 2 store.
- Produces (all on `*Service` unless noted):
  - `var TextTemplateStatePath string`
  - `SetTextTemplateStores(global, repo texttmpl.Store)`
  - `TextTemplates(ctx) ([]model.TextTemplate, error)` — global rows then repo rows
  - `AddTextTemplate(ctx, t model.TextTemplate) (model.TextTemplate, error)`
  - `UpdateTextTemplate(ctx, scope model.ProfileScope, id string, t model.TextTemplate) (model.TextTemplate, error)`
  - `RemoveTextTemplate(ctx, scope model.ProfileScope, id string) error`
  - `FindTextTemplate(ctx, idPrefix string, scope *model.ProfileScope) (model.TextTemplate, error)` — unique-prefix lookup; `scope == nil` means "repo wins a tie"
  - `RenderTextTemplate(ctx, body string, inputs map[string]string) (text string, seqNames []string, err error)` — peeks
  - `TakeTextTemplate(ctx, body string, inputs map[string]string) (string, error)` — bumps the counters, resolves with the consumed numbers
  - package funcs `ValidateTextTemplate(title, body string) error`, `TextTemplateTokens(body string) (userLabels, automatic []string)`
  - consts `MaxTextTemplateTitle = 80`, `MaxTextTemplateBody = 64 << 10`

- [ ] **Step 1: Write the failing tests**

Use the package's existing repo helper (`newTestRepo`-style — check `internal/domain/prefixstore_test.go` for the exact helper and how it injects `SetPrefixStores`, and mirror it).

```go
func textSvc(t *testing.T) *Service {
	t.Helper()
	svc := newPrefixTestService(t) // the helper prefixstore_test.go uses to open a real repo
	svc.SetTextTemplateStores(
		texttmpl.NewFileStore(t.TempDir(), model.ProfileScopeGlobal),
		texttmpl.NewFileStore(t.TempDir(), model.ProfileScopeRepo))
	return svc
}

func TestTextTemplatesAddListScopes(t *testing.T) {
	t.Parallel()
	svc, ctx := textSvc(t), context.Background()
	if _, err := svc.AddTextTemplate(ctx, model.TextTemplate{Title: "G", Body: "g", Scope: model.ProfileScopeGlobal}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddTextTemplate(ctx, model.TextTemplate{Title: "R", Body: "r", Scope: model.ProfileScopeRepo}); err != nil {
		t.Fatal(err)
	}
	list, err := svc.TextTemplates(ctx)
	if err != nil || len(list) != 2 || list[0].Title != "G" || list[1].Scope != model.ProfileScopeRepo {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

func TestValidateTextTemplate(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", MaxTextTemplateBody+1)
	for name, tc := range map[string]struct{ title, body string }{
		"empty title":     {"  ", "b"},
		"two-line title":  {"a\nb", "b"},
		"long title":      {strings.Repeat("t", 81), "b"},
		"empty body":      {"t", " \n\t"},
		"oversized body":  {"t", long},
		"malformed token": {"t", "x <seq> y"},
	} {
		if err := ValidateTextTemplate(tc.title, tc.body); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if err := ValidateTextTemplate("t", "ok <br> <user:a> <branch>"); err != nil {
		t.Fatalf("valid template refused: %v", err)
	}
}

func TestRenderPeeksTakeConsumes(t *testing.T) {
	t.Parallel()
	svc, ctx := textSvc(t), context.Background()
	body := "n=<seq:tt:2> <user:who>"
	in := map[string]string{"who": "me"}
	for i := 0; i < 2; i++ { // two previews: the same number
		got, seqs, err := svc.RenderTextTemplate(ctx, body, in)
		if err != nil || got != "n=01 me" || len(seqs) != 1 || seqs[0] != "tt" {
			t.Fatalf("render %d = %q %v %v", i, got, seqs, err)
		}
	}
	if got, err := svc.TakeTextTemplate(ctx, body, in); err != nil || got != "n=01 me" {
		t.Fatalf("take = %q, %v", got, err)
	}
	if got, _, _ := svc.RenderTextTemplate(ctx, body, in); got != "n=02 me" {
		t.Fatalf("after take the preview = %q, want n=02 me", got)
	}
}

func TestFindTextTemplate(t *testing.T) {
	t.Parallel()
	svc, ctx := textSvc(t), context.Background()
	_, _ = svc.AddTextTemplate(ctx, model.TextTemplate{Title: "Note", Body: "global", Scope: model.ProfileScopeGlobal})
	_, _ = svc.AddTextTemplate(ctx, model.TextTemplate{Title: "Note", Body: "repo", Scope: model.ProfileScopeRepo})
	_, _ = svc.AddTextTemplate(ctx, model.TextTemplate{Title: "Notice", Body: "n", Scope: model.ProfileScopeGlobal})

	if got, err := svc.FindTextTemplate(ctx, "note", nil); err != nil || got.Body != "repo" {
		t.Fatalf("exact id, repo wins: %+v, %v", got, err)
	}
	g := model.ProfileScopeGlobal
	if got, err := svc.FindTextTemplate(ctx, "note", &g); err != nil || got.Body != "global" {
		t.Fatalf("explicit scope: %+v, %v", got, err)
	}
	if got, err := svc.FindTextTemplate(ctx, "notic", nil); err != nil || got.Title != "Notice" {
		t.Fatalf("unique prefix: %+v, %v", got, err)
	}
	if _, err := svc.FindTextTemplate(ctx, "no", nil); err == nil {
		t.Fatal("ambiguous prefix must error")
	}
	if _, err := svc.FindTextTemplate(ctx, "zzz", nil); err == nil {
		t.Fatal("unknown id must error")
	}
}
```

(The first-sequence value is `01` when the repo's counter starts at 0 and peek returns next = 1 — confirm against `worktree.PeekSeqs` in `e2e/scenarios/prefix-resolve.toml`, which expects `01` first.)

- [ ] **Step 2: Run to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/text-templates && rtk go test ./internal/domain/ -run 'TextTemplate|RenderPeeks'`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Implement `internal/domain/texttemplates.go`**

```go
package domain

// MaxTextTemplateTitle / MaxTextTemplateBody bound a stored template.
const (
	MaxTextTemplateTitle = 80
	MaxTextTemplateBody  = 64 << 10
)

// TextTemplateStatePath overrides the text-template root dir ("" = XDG state).
var TextTemplateStatePath string

func (s *Service) SetTextTemplateStores(global, repo texttmpl.Store) {
	s.mu.Lock()
	s.textGlobal, s.textRepo = global, repo
	s.mu.Unlock()
}
```

`textTemplateStores(ctx)` is `prefixStores` with `TextTemplateStatePath`, `stateBaseDir("texttemplates")`, `texttmpl.NewFileStore`, and the `textGlobal`/`textRepo` fields. `TextTemplates` is `Prefixes` over them. Then:

```go
func (s *Service) textStore(ctx context.Context, scope model.ProfileScope) (texttmpl.Store, error) {
	global, repo := s.textTemplateStores(ctx)
	st := global
	if scope == model.ProfileScopeRepo {
		st = repo
	}
	if st == nil {
		return nil, os.ErrInvalid
	}
	return st, nil
}

func (s *Service) AddTextTemplate(ctx context.Context, t model.TextTemplate) (model.TextTemplate, error) {
	t.Title, t.Body = strings.TrimSpace(t.Title), strings.TrimRight(t.Body, " \t\r\n")
	if err := ValidateTextTemplate(t.Title, t.Body); err != nil {
		return model.TextTemplate{}, err
	}
	st, err := s.textStore(ctx, t.Scope)
	if err != nil {
		return model.TextTemplate{}, err
	}
	return st.Add(t)
}

func (s *Service) UpdateTextTemplate(ctx context.Context, scope model.ProfileScope, id string, t model.TextTemplate) (model.TextTemplate, error) {
	t.Title, t.Body = strings.TrimSpace(t.Title), strings.TrimRight(t.Body, " \t\r\n")
	if err := ValidateTextTemplate(t.Title, t.Body); err != nil {
		return model.TextTemplate{}, err
	}
	st, err := s.textStore(ctx, scope)
	if err != nil {
		return model.TextTemplate{}, err
	}
	return st.Update(id, t)
}

func (s *Service) RemoveTextTemplate(ctx context.Context, scope model.ProfileScope, id string) error {
	st, err := s.textStore(ctx, scope)
	if err != nil {
		return err
	}
	return st.Remove(id)
}

// ValidateTextTemplate checks the limits and proves the body's known tokens
// well-formed by a dry resolve (placeholder inputs, zero counters).
func ValidateTextTemplate(title, body string) error {
	title = strings.TrimSpace(title)
	switch {
	case title == "":
		return fmt.Errorf("invalid text template: the title is empty")
	case strings.ContainsAny(title, "\r\n"):
		return fmt.Errorf("invalid text template: the title must be one line")
	case utf8.RuneCountInString(title) > MaxTextTemplateTitle:
		return fmt.Errorf("invalid text template: the title is longer than %d characters", MaxTextTemplateTitle)
	case strings.TrimSpace(body) == "":
		return fmt.Errorf("invalid text template: the text is empty")
	case len(body) > MaxTextTemplateBody:
		return fmt.Errorf("invalid text template: the text is larger than %d KiB", MaxTextTemplateBody>>10)
	}
	set := template.TextTokens(body)
	inputs := map[string]string{}
	for _, l := range set.UserLabels {
		inputs[l] = "x"
	}
	tctx := template.Ctx{
		ParentBranch: "parent", Repo: "repo", Branch: "branch",
		Seqs: map[string]int{}, Now: time.Now, Rand: rand.New(rand.NewPCG(1, 2)),
	}
	if _, err := template.ResolveText(body, inputs, tctx); err != nil {
		return fmt.Errorf("invalid text template: %w", err)
	}
	return nil
}

// TextTemplateTokens returns a body's <user:…> labels and its other tokens
// as written — what a frontend prompts for and what it lists as automatic.
func TextTemplateTokens(body string) (userLabels, automatic []string) {
	set := template.TextTokens(body)
	return set.UserLabels, set.Automatic
}

// textCtx is the resolve context for this repo: <branch> and <parent-branch>
// are the current branch, <repo> the main worktree's directory name.
func (s *Service) textCtx(ctx context.Context) (template.Ctx, string) {
	branch, err := s.CurrentBranch(ctx)
	if err != nil {
		branch = ""
	}
	repo := ""
	if wts, werr := s.Worktrees(ctx); werr == nil && len(wts) > 0 && wts[0].Path != "" {
		repo = worktree.RepoName(wts[0].Path)
	}
	gitDir := ""
	if cd, cerr := s.GitCommonDir(ctx); cerr == nil {
		gitDir = strings.TrimSpace(cd)
		if repo == "" {
			repo = filepath.Base(filepath.Dir(gitDir))
		}
	}
	return template.Ctx{
		ParentBranch: branch, Repo: repo, Branch: branch,
		Now:  clock.Now,
		Rand: rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
	}, gitDir
}

func (s *Service) RenderTextTemplate(ctx context.Context, body string, inputs map[string]string) (string, []string, error) {
	tctx, gitDir := s.textCtx(ctx)
	names := template.TextTokens(body).SeqNames
	tctx.Seqs = worktree.PeekSeqs(gitDir, names)
	out, err := template.ResolveText(body, inputs, tctx)
	if err != nil {
		return "", nil, err
	}
	return out, names, nil
}

// TakeTextTemplate consumes the body's <seq:…> counters and resolves with the
// numbers THIS call was handed (another process may bump between a peek and
// here — the gg prefix resolve --bump rule).
func (s *Service) TakeTextTemplate(ctx context.Context, body string, inputs map[string]string) (string, error) {
	tctx, gitDir := s.textCtx(ctx)
	names := template.TextTokens(body).SeqNames
	// Prove it resolves before consuming anything.
	tctx.Seqs = worktree.PeekSeqs(gitDir, names)
	if _, err := template.ResolveText(body, inputs, tctx); err != nil {
		return "", err
	}
	taken := map[string]int{}
	for _, n := range names {
		v, err := config.BumpSeq(gitDir, n)
		if err != nil {
			return "", fmt.Errorf("could not advance <seq:%s>: %w", n, err)
		}
		taken[n] = v
	}
	tctx.Seqs = taken
	return template.ResolveText(body, inputs, tctx)
}

// FindTextTemplate resolves an id or a unique id prefix. An exact id beats a
// prefix; with scope nil the repo row wins when both scopes hold the id.
func (s *Service) FindTextTemplate(ctx context.Context, idPrefix string, scope *model.ProfileScope) (model.TextTemplate, error) {
	all, err := s.TextTemplates(ctx)
	if err != nil {
		return model.TextTemplate{}, err
	}
	var exact, pre []model.TextTemplate
	for _, t := range all {
		if scope != nil && t.Scope != *scope {
			continue
		}
		if t.ID == idPrefix {
			exact = append(exact, t)
		} else if strings.HasPrefix(t.ID, idPrefix) {
			pre = append(pre, t)
		}
	}
	pick := exact
	if len(pick) == 0 {
		pick = pre
	}
	switch {
	case len(pick) == 0:
		return model.TextTemplate{}, fmt.Errorf("no text template %q (gg template list shows the ids)", idPrefix)
	case len(pick) == 1:
		return pick[0], nil
	case len(exact) == 2: // the same id in both scopes: the repo row wins
		for _, t := range exact {
			if t.Scope == model.ProfileScopeRepo {
				return t, nil
			}
		}
	}
	return model.TextTemplate{}, fmt.Errorf("%q matches %d text templates — give more of the id", idPrefix, len(pick))
}
```

Check what `worktree.PeekSeqs` returns for the peek (the NEXT value vs the current one) against `worktree.ResolvePrefix` and match it so preview and take agree — the `TestRenderPeeksTakeConsumes` test is the arbiter.

- [ ] **Step 4: Run to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/text-templates && rtk go test ./internal/domain/ -run 'TextTemplate|RenderPeeks' && rtk go vet ./internal/domain/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/text-templates && git add internal/domain/texttemplates.go internal/domain/texttemplates_test.go internal/domain/service.go && git commit -m "feat(domain): text template queries, render and take"
```

---

### Task 4: CLI `gg template` + e2e + skill doc

**Files:**
- Create: `internal/cli/template.go`, `e2e/scenarios/text-templates.toml`
- Modify: `internal/cli/cli.go` (dispatch `case "template", "templates":` beside `"prefix"` at ~:153, pass `stdin`; add both names to the `commands` map at ~:212; add the usage line wherever `gg prefix` is listed in the help text — `grep -n '"prefix\|gg prefix' internal/cli/*.go`), `internal/cli/hostnudge.go` (no reload source: text templates are not a TUI source — confirm the verb falls through to nil), `internal/agentskill/using-gg.md` + `agentskill.Version`
- Test: `internal/cli/template_test.go`

**Interfaces:**
- Consumes: Task 3 domain API; `labelValues` from `internal/cli/prefix.go`.
- Produces: `func cmdTemplate(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int`.

Verbs (exit 2 = usage/unknown id, 1 = failure):

| Verb | Behaviour |
|---|---|
| `list` / `ls` (bare `gg templates`) | `id\tscope\ttitle` per line |
| `show <id> [--global]` | the raw body, a blank line, `variables: a, b` and `automatic: <date>, …` (lines omitted when empty) |
| `render <id> [--global] [--set label=value]… [--peek]` | resolved text on stdout; consumes sequences unless `--peek`; a missing label → exit 2 naming `--set label=…` |
| `add --title <t> [--global] -F <file\|->` | body from the file, `-` = stdin; prints the new id |
| `edit <id> [--global] [--title <t>] [-F <file\|->]` | at least one of `--title`/`-F`; prints the id |
| `rm` / `remove <id> [--global]` | removes |

- [ ] **Step 1: Write the failing tests** (`internal/cli/template_test.go`)

```go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplateAddListShowRenderRemove(t *testing.T) {
	dir := prefixRepo(t) // t.Setenv XDG_STATE_HOME/HOME + a repo

	code, out, errb := runCLIStdin(t, dir, "Hi <user:name>,\n<br> on <branch>\n", "template", "add", "--title", "Greeting", "-F", "-")
	if code != 0 || strings.TrimSpace(out) != "greeting" {
		t.Fatalf("add exit %d out %q err %s", code, out, errb)
	}
	if _, out, _ = runCLI(t, dir, "template", "list"); !strings.Contains(out, "greeting\trepo\tGreeting") {
		t.Fatalf("list = %q", out)
	}
	if _, out, _ = runCLI(t, dir, "templates"); !strings.Contains(out, "greeting") {
		t.Fatalf("bare templates = %q", out)
	}
	if _, out, _ = runCLI(t, dir, "template", "show", "greet"); !strings.Contains(out, "Hi <user:name>,") || !strings.Contains(out, "variables: name") || !strings.Contains(out, "automatic: <branch>") {
		t.Fatalf("show = %q", out)
	}
	code, _, errb = runCLI(t, dir, "template", "render", "greeting")
	if code != 2 || !strings.Contains(errb, "--set name=") {
		t.Fatalf("render without --set: exit %d err %q", code, errb)
	}
	code, out, errb = runCLI(t, dir, "template", "render", "greeting", "--set", "name=Ann")
	if code != 0 || !strings.HasPrefix(out, "Hi Ann,\n<br> on ") {
		t.Fatalf("render exit %d out %q err %s", code, out, errb)
	}
	if code, _, _ = runCLI(t, dir, "template", "rm", "greeting"); code != 0 {
		t.Fatalf("rm exit %d", code)
	}
	if _, out, _ = runCLI(t, dir, "template", "list"); strings.Contains(out, "greeting") {
		t.Fatalf("still listed: %q", out)
	}
}

func TestTemplateEditAndFile(t *testing.T) {
	dir := prefixRepo(t)
	f := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(f, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errb := runCLI(t, dir, "template", "add", "--global", "--title", "Note", "-F", f); code != 0 {
		t.Fatalf("add: %s", errb)
	}
	if code, _, errb := runCLIStdin(t, dir, "two\n", "template", "edit", "note", "--global", "-F", "-"); code != 0 {
		t.Fatalf("edit body: %s", errb)
	}
	code, out, errb := runCLI(t, dir, "template", "edit", "note", "--global", "--title", "Memo")
	if code != 0 || strings.TrimSpace(out) != "memo" {
		t.Fatalf("rename exit %d out %q err %s", code, out, errb)
	}
	if _, out, _ = runCLI(t, dir, "template", "render", "memo"); out != "two\n" {
		t.Fatalf("render = %q", out)
	}
}

func TestTemplateSeqPeekAndTake(t *testing.T) {
	dir := prefixRepo(t)
	runCLIStdin(t, dir, "#<seq:cli:2>", "template", "add", "--title", "Seq", "-F", "-")
	for _, want := range []string{"#01\n", "#01\n"} {
		if _, out, _ := runCLI(t, dir, "template", "render", "seq", "--peek"); out != want {
			t.Fatalf("peek = %q, want %q", out, want)
		}
	}
	if _, out, _ := runCLI(t, dir, "template", "render", "seq"); out != "#01\n" {
		t.Fatalf("take = %q", out)
	}
	if _, out, _ := runCLI(t, dir, "template", "render", "seq", "--peek"); out != "#02\n" {
		t.Fatalf("after take = %q", out)
	}
}

func TestTemplateUsageErrors(t *testing.T) {
	dir := prefixRepo(t)
	for _, args := range [][]string{
		{"template"}, {"template", "nope"}, {"template", "add"}, {"template", "add", "--title", "x"},
		{"template", "show"}, {"template", "render", "missing"}, {"template", "edit", "x"},
	} {
		if code, _, _ := runCLI(t, dir, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	if code, _, errb := runCLIStdin(t, dir, "x <seq> y", "template", "add", "--title", "Bad", "-F", "-"); code != 1 || !strings.Contains(errb, "seq") {
		t.Fatalf("malformed token: exit %d err %q", code, errb)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/text-templates && rtk go test ./internal/cli/ -run 'TestTemplate'`
Expected: FAIL — unknown command `template`.

- [ ] **Step 3: Implement `internal/cli/template.go`**

```go
package cli

const templateUsage = "usage: gg template <list|show|render|add|edit|rm> ..."

// cmdTemplate implements `gg template …`: the two-scope registry of text
// templates (titled multi-line texts with <…> tokens) and their rendering.
func cmdTemplate(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, templateUsage)
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list", "ls":
		return templateList(svc, stdout, stderr)
	case "show":
		return templateShow(svc, rest, stdout, stderr)
	case "render":
		return templateRender(svc, rest, stdout, stderr)
	case "add":
		return templateAdd(svc, rest, stdin, stdout, stderr)
	case "edit":
		return templateEdit(svc, rest, stdin, stdout, stderr)
	case "rm", "remove":
		return templateRemove(svc, rest, stderr)
	default:
		fmt.Fprintf(stderr, "template: unknown subcommand %q\n%s\n", sub, templateUsage)
		return 2
	}
}

// parseInterleaved parses fs over args, letting flags follow positionals
// (the prefix resolve loop), and returns the positionals.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, bool) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, false
		}
		if fs.NArg() == 0 {
			return pos, true
		}
		pos, args = append(pos, fs.Arg(0)), fs.Args()[1:]
	}
}

// templateScope maps --global to an explicit scope; absent = nil (repo wins a tie).
func templateScope(global bool) *model.ProfileScope {
	if !global {
		return nil
	}
	g := model.ProfileScopeGlobal
	return &g
}

// readBody reads -F's target: a file path, or stdin for "-".
func readBody(from string, stdin io.Reader) (string, error) {
	if from == "-" {
		b, err := io.ReadAll(stdin)
		return string(b), err
	}
	b, err := os.ReadFile(from)
	return string(b), err
}
```

`templateList`: `svc.TextTemplates` → `fmt.Fprintf(stdout, "%s\t%s\t%s\n", t.ID, t.Scope.String(), t.Title)`.

`templateShow`: flags `--global`; one positional else usage+2; `svc.FindTextTemplate(ctx, id, templateScope(*global))` (error → stderr, exit 2); print `t.Body` + `"\n"`; then, from `domain.TextTemplateTokens(t.Body)`, a blank line and `variables: ` + `strings.Join(labels, ", ")` and `automatic: ` + join, each only when non-empty.

`templateRender`: flags `--global`, `--peek`, `fs.Var(inputs, "set", …)` with `inputs := labelValues{}`; find (exit 2 on error); missing labels → `fmt.Fprintf(stderr, "template render: %s needs %s\n", t.ID, strings.Join(missing, " "))` with each `"--set "+l+"=…"`, exit 2; then `--peek` → `svc.RenderTextTemplate`, else `svc.TakeTextTemplate`; error → exit 1; print the text, adding a trailing `\n` only when the text does not end with one.

`templateAdd`: flags `--title`, `--global`, `-F`; no positionals; `--title` and `-F` both required else usage+2; scope repo unless `--global`; `readBody` (error → exit 1); `svc.AddTextTemplate` (error → `error: …`, exit 1); print the id.

`templateEdit`: flags `--title`, `--global`, `-F`; one positional and at least one of `--title`/`-F`, else usage+2; find (exit 2); `title := t.Title` unless `--title`; `body := t.Body` unless `-F`; `svc.UpdateTextTemplate(ctx, t.Scope, t.ID, model.TextTemplate{Title: title, Body: body})`; print the new id.

`templateRemove`: flags `--global`; one positional; find (exit 2); `svc.RemoveTextTemplate(ctx, t.Scope, t.ID)`.

Wire in `cli.go`: `case "template", "templates": if cmd == "templates" { rest = append([]string{"list"}, rest...) }; return cmdTemplate(svc, rest, stdin, stdout, stderr)`.

- [ ] **Step 4: Run to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/text-templates && rtk go test ./internal/cli/`
Expected: PASS (the parser/verb-table tests may list every command — add `template`/`templates` where they demand it).

- [ ] **Step 5: e2e scenario** (use the `writing-e2e-scenarios` skill; the harness has no stdin, so write the body with a `write` step)

`e2e/scenarios/text-templates.toml`:

```toml
name = "gg template: add a text template from a file, render it with a variable, the frozen date and a consumed sequence"

[input]
steps = [
  { write = "a.txt", content = "a\n" },
  { commit = "c1" },
  { write = "body.md", content = "Hello <user:name>\n<br> <date> #<seq:tt:2>\n" },
]

[[run]]
cmd             = ["template", "add", "--title", "Greeting", "-F", "body.md"]
exit            = 0
stdout_contains = ["greeting"]

[[run]]
cmd             = ["template", "list"]
exit            = 0
stdout_contains = ["greeting\trepo\tGreeting"]

[[run]]
cmd             = ["template", "render", "greeting"]
exit            = 2
stderr_contains = ["--set name="]

[[run]]
cmd             = ["template", "render", "greeting", "--set", "name=Ann"]
exit            = 0
stdout_contains = ["Hello Ann", "<br> 2026-01-02 #01"]

[[run]]
cmd             = ["template", "render", "greeting", "--set", "name=Ann", "--peek"]
exit            = 0
stdout_contains = ["#02"]
```

Run: `cd /work/gigagit/.claude/worktrees/text-templates && ./test.sh e2e`
Expected: green. (If the harness resolves `-F body.md` against a different cwd, use the scenario's documented path placeholder.)

- [ ] **Step 6: Skill doc**

Add a "Text templates" block to `internal/agentskill/using-gg.md` (the six verbs, `--set`, `--peek`, `-F -`), bump `agentskill.Version`, run the package's version/golden tests: `rtk go test ./internal/agentskill/`.

- [ ] **Step 7: Commit**

```bash
cd /work/gigagit/.claude/worktrees/text-templates && git add internal/cli/template.go internal/cli/template_test.go internal/cli/cli.go e2e/scenarios/text-templates.toml internal/agentskill && git commit -m "feat(cli): gg template — list, show, render, add, edit, rm"
```

---

### Task 5: TUI window — browse

Use the `adding-tui-windows` skill before starting.

**Files:**
- Create: `internal/tui/text_templates.go`
- Modify: `internal/tui/model.go` (the `alt+x` gate beside `alt+a`/`alt+t` at ~:2141; the `textTemplatesDataMsg` case beside `prefixDataMsg` at ~:3585)
- Test: `internal/tui/text_templates_test.go`

**Interfaces:**
- Consumes: Task 3 domain API.
- Produces:

```go
type ttMode int

const (
	ttBrowse ttMode = iota
	ttFill
	ttRendered
	ttForm
	ttConfirmDelete
)

type textTemplatesView struct {
	popupMax
	loading    bool
	items      []model.TextTemplate
	sel        int
	bodyScroll int // first shown display line of the body pane
	mode       ttMode

	// fill / rendered (Task 6)
	fill      templateFill
	rendered  string
	renderErr string
	seqNames  []string
	rScroll   int

	// form (Task 7)
	fTitle  textfield
	scope   model.ProfileScope
	field   int // 0 = title, 1 = scope
	formErr string
	editID  string // "" = adding
}

type textTemplatesDataMsg struct {
	items    []model.TextTemplate
	err      error
	selectID string // row to land on after a save ("" = keep the cursor)
	status   string // status line to show ("" = none)
}

func (m Model) openTextTemplates() (Model, tea.Cmd)
func (m Model) loadTextTemplatesCmd(selectID, status string) tea.Cmd
func (v *textTemplatesView) selected() (model.TextTemplate, bool)
const textTemplateListRows = 8
```

- [ ] **Step 1: Write the failing tests**

```go
package tui

func ttItems(n int) []model.TextTemplate {
	out := make([]model.TextTemplate, n)
	for i := range out {
		out[i] = model.TextTemplate{ID: fmt.Sprintf("t%02d", i), Title: fmt.Sprintf("Template %02d", i), Body: fmt.Sprintf("body %02d <user:who> <date>", i)}
	}
	return out
}

func TestTextTemplatesBrowseMoveAndClamp(t *testing.T) {
	t.Parallel()
	v := &textTemplatesView{items: ttItems(3), bodyScroll: 4}
	v.update(Model{}, tea.KeyMsg{Type: tea.KeyDown})
	if v.sel != 1 || v.bodyScroll != 0 {
		t.Fatalf("down: sel %d scroll %d (moving the selection resets the body scroll)", v.sel, v.bodyScroll)
	}
	v.update(Model{}, tea.KeyMsg{Type: tea.KeyDown})
	v.update(Model{}, tea.KeyMsg{Type: tea.KeyDown})
	if v.sel != 2 {
		t.Fatalf("sel ran past the end: %d", v.sel)
	}
	v.update(Model{}, tea.KeyMsg{Type: tea.KeyUp})
	if v.sel != 1 {
		t.Fatalf("up: %d", v.sel)
	}
}

func TestTextTemplatesBoxShowsListBodyAndVariables(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 40}
	v := &textTemplatesView{items: ttItems(12), sel: 1}
	box := ansi.Strip(v.box(m))
	for _, want := range []string{"Text templates", "> Template 01", "body 01 <user:who> <date>", "Variables: who", "Automatic: <date>"} {
		if !strings.Contains(box, want) {
			t.Errorf("box misses %q\n%s", want, box)
		}
	}
	// 8 list rows at most: rows 00–07 are shown, 08 is not.
	if !strings.Contains(box, "Template 07") || strings.Contains(box, "Template 08") {
		t.Errorf("list is not capped at 8 rows\n%s", box)
	}
}

func TestTextTemplatesEmptyAndLoading(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 40}
	if box := ansi.Strip((&textTemplatesView{loading: true}).box(m)); !strings.Contains(box, "(loading…)") {
		t.Errorf("loading box:\n%s", box)
	}
	if box := ansi.Strip((&textTemplatesView{}).box(m)); !strings.Contains(box, "(none yet — [n] to add)") {
		t.Errorf("empty box:\n%s", box)
	}
}

func TestAltXOpensTextTemplates(t *testing.T) {
	t.Parallel()
	m := newTestModel(t) // the package's standard test Model constructor
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}, Alt: true})
	if layerOf[*textTemplatesView](out.(Model)) == nil {
		t.Fatal("alt+x did not open the text templates window")
	}
}

func TestTextTemplatesDataMsgSelectsRow(t *testing.T) {
	t.Parallel()
	m := Model{}.pushLayer(&textTemplatesView{loading: true})
	out, _ := m.Update(textTemplatesDataMsg{items: ttItems(5), selectID: "t03", status: "saved"})
	mm := out.(Model)
	v := layerOf[*textTemplatesView](mm)
	if v.loading || v.sel != 3 || mm.statusMsg != "saved" {
		t.Fatalf("loading %v sel %d status %q", v.loading, v.sel, mm.statusMsg)
	}
}
```

(Use whichever ANSI-strip helper and test-Model constructor the neighbouring popup tests use — check `prefix_settings_test.go` / `branch_filter_popup_test.go` and substitute the real names.)

- [ ] **Step 2: Run to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/text-templates && rtk go test ./internal/tui/ -run 'TextTemplates|AltXOpens'`
Expected: FAIL — undefined `textTemplatesView`.

- [ ] **Step 3: Implement**

`openTextTemplates` / `loadTextTemplatesCmd` mirror `openPrefixSettings` / `loadPrefixDataCmd` over `svc.TextTemplates`.

`update`: `ctrl+c` quits; switch on `v.mode` to `updateBrowse` (this task), `updateFill`/`updateRendered` (Task 6), `updateForm`/`updateConfirm` (Task 7 — until then they return `m, nil`).

`updateBrowse`:

```go
func (v *textTemplatesView) updateBrowse(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyUp:
		if v.sel > 0 {
			v.sel--
			v.bodyScroll = 0
		}
		return m, nil
	case tea.KeyDown:
		if v.sel < len(v.items)-1 {
			v.sel++
			v.bodyScroll = 0
		}
		return m, nil
	case tea.KeyPgDown:
		v.bodyScroll += v.bodyPage(m)
		return m, nil // box() clamps
	case tea.KeyPgUp:
		v.bodyScroll = max(0, v.bodyScroll-v.bodyPage(m))
		return m, nil
	}
	switch msg.String() {
	case "j":
		return v.updateBrowse(m, tea.KeyMsg{Type: tea.KeyDown})
	case "k":
		return v.updateBrowse(m, tea.KeyMsg{Type: tea.KeyUp})
	}
	return m, nil
}
```

`box(m)` (browse mode) builds, at `textW := popupTextWidth(inner)` with `inner := popupResolveWidth(w, v.maximized, popupWideInnerWidth(w))`:

1. `i18n.T("Text templates")`, blank.
2. The list: one `winRow` per item — text `"> "`/`"  "` + title, right-aligned tag `i18n.T("[global]")` / `i18n.T("[this repo]")` (reuse the prefix keys); selected row styled `st().selectedRow`. Rendered with `renderWindow(rows, winOpts{w: textW, h: min(len(items), textTemplateListRows), anchor: v.sel})`. A title too long for the row is cut by the window in `modeCutoff`; its full text goes to the bottom bar through the popup cut-row reveal the other popups use (follow `popup-cut-row` precedent — see how `prefix_picker.go` or `branch_filter_popup.go` reports its revealed row).
3. A rule line `strings.Repeat("─", textW)`.
4. The body pane: the selected body split on `\n`, each line a `winRow` rendered in `modeWrap`; total display lines computed once, `v.bodyScroll` clamped to `[0, total-bodyH]`, the visible slice shown. `bodyH` = the popup's row budget (`popupResolveRowCap(v.maximized, termH, 12)`) — 12 lines unmaximized, the terminal's remainder when maximized. Tabs expanded to 4 spaces.
5. A rule line.
6. `i18n.T("Variables: %s", strings.Join(labels, " · "))` and `i18n.T("Automatic: %s", strings.Join(automatic, " · "))` from `domain.TextTemplateTokens(body)`, each omitted when empty, each wrapped to `textW`.
7. Blank, then the hints: `i18n.T("[enter] fill  [n] add  [e] edit  [d] delete  [ctrl+t] maximize  [esc] close")`.

Loading → `i18n.T("  (loading…)")`; empty → `i18n.T("  (none yet — [n] to add)")` and the hints `i18n.T("[n] add  [esc] close")`. Everything through `popupBox(inner, …)`; `render` is `overlayCenter(clipToHeight(below, h), v.box(m), w, h)`.

`bodyPage(m)` returns the body pane height used by `box`.

`model.go`, in the key gate at ~:2141, after the `alt+a`/`alt+t` branch:

```go
		// alt+x opens the text templates window (base panels only, like
		// alt+a/alt+t; a focused console kept the key for its program above).
		if msg.String() == "alt+x" && m.topLayer() == nil && !m.filterTyping {
			return m.openTextTemplates()
		}
```

`model.go`, beside `case prefixDataMsg:`:

```go
	case textTemplatesDataMsg:
		if v := layerOf[*textTemplatesView](m); v != nil {
			v.loading = false
			if msg.err != nil {
				m.statusMsg = i18n.T("text templates: %s", msg.err.Error())
				return m, nil
			}
			v.items = msg.items
			for i, t := range v.items {
				if msg.selectID != "" && t.ID == msg.selectID {
					v.sel = i
				}
			}
			v.sel = max(0, min(v.sel, len(v.items)-1))
			v.bodyScroll = 0
		}
		if msg.status != "" {
			m.statusMsg = msg.status
		}
		return m, nil
```

Add every new `i18n.T` key to the four bundles now (`adding-translations` skill) so the AST gates stay green per task.

- [ ] **Step 4: Run to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/text-templates && rtk go test ./internal/tui/ -run 'TextTemplates|AltXOpens|I18n|i18n'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/text-templates && git add internal/tui/text_templates.go internal/tui/text_templates_test.go internal/tui/model.go internal/i18n && git commit -m "feat(tui): text templates window — browse (alt+x)"
```

---

### Task 6: TUI — fill, rendered text, copy

**Files:**
- Modify: `internal/tui/text_templates.go`, `internal/tui/template_fill.go` (add `newTemplateFillLabels`), `internal/tui/model.go` (`textTemplateRenderedMsg` case)
- Test: `internal/tui/text_templates_test.go`

**Interfaces:**
- Consumes: `templateFill`; `Model.copyToClipboardCmd(ok, text string) tea.Cmd`; `svc.RenderTextTemplate`; `svc.BumpPrefixSeqs`.
- Produces: `func newTemplateFillLabels(labels []string) templateFill`; `type textTemplateRenderedMsg struct{ text string; seqNames []string; err error }`; `func (m Model) renderTextTemplateCmd(body string, inputs map[string]string) tea.Cmd`.

- [ ] **Step 1: Write the failing tests**

```go
func TestTextTemplatesEnterWithVariablesOpensFill(t *testing.T) {
	t.Parallel()
	v := &textTemplatesView{items: []model.TextTemplate{{ID: "a", Title: "A", Body: "<user:one> if a < b <user:two>"}}}
	m, cmd := v.update(Model{}, tea.KeyMsg{Type: tea.KeyEnter})
	if v.mode != ttFill || cmd != nil || len(v.fill.labels) != 2 || v.fill.labels[1] != "two" {
		t.Fatalf("mode %v labels %v", v.mode, v.fill.labels)
	}
	box := ansi.Strip(v.box(Model{width: 100, height: 40}))
	if !strings.Contains(box, "A — fill variables (1/2)") || !strings.Contains(box, "> one:") {
		t.Fatalf("fill box:\n%s", box)
	}
	_ = m
	// esc returns to browse
	v.update(Model{}, tea.KeyMsg{Type: tea.KeyEsc})
	if v.mode != ttBrowse {
		t.Fatalf("esc: mode %v", v.mode)
	}
}

func TestTextTemplatesEnterWithoutVariablesRenders(t *testing.T) {
	t.Parallel()
	v := &textTemplatesView{items: []model.TextTemplate{{ID: "a", Title: "A", Body: "plain"}}}
	_, cmd := v.update(Model{}, tea.KeyMsg{Type: tea.KeyEnter})
	if v.mode != ttBrowse || cmd == nil {
		t.Fatalf("no variables: want a render command and browse until it lands (mode %v, cmd nil %v)", v.mode, cmd == nil)
	}
}

func TestTextTemplateRenderedMsgShowsText(t *testing.T) {
	t.Parallel()
	v := &textTemplatesView{items: []model.TextTemplate{{ID: "a", Title: "A", Body: "x"}}}
	m := Model{width: 100, height: 40}.pushLayer(v)
	out, _ := m.Update(textTemplateRenderedMsg{text: "Hello Ann\nsecond", seqNames: []string{"n"}})
	if v.mode != ttRendered {
		t.Fatalf("mode %v", v.mode)
	}
	box := ansi.Strip(v.box(out.(Model)))
	for _, want := range []string{"A — rendered", "Hello Ann", "second", "[y] copy and close"} {
		if !strings.Contains(box, want) {
			t.Errorf("rendered box misses %q\n%s", want, box)
		}
	}
}

func TestTextTemplateRenderErrorOffersOnlyEsc(t *testing.T) {
	t.Parallel()
	v := &textTemplatesView{items: []model.TextTemplate{{ID: "a", Title: "A", Body: "x"}}}
	m := Model{width: 100, height: 40}.pushLayer(v)
	out, _ := m.Update(textTemplateRenderedMsg{err: errors.New("boom")})
	box := ansi.Strip(v.box(out.(Model)))
	if !strings.Contains(box, "boom") || strings.Contains(box, "[y]") {
		t.Fatalf("error box:\n%s", box)
	}
	mm, cmd := v.update(out.(Model), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd != nil || layerOf[*textTemplatesView](mm) == nil {
		t.Fatal("y on an error must do nothing")
	}
}

// y copies and closes the WHOLE window (user ruling).
func TestTextTemplatesYCopiesAndCloses(t *testing.T) {
	t.Parallel()
	var copied string
	v := &textTemplatesView{mode: ttRendered, rendered: "final text", items: []model.TextTemplate{{ID: "a", Title: "A"}}}
	m := Model{clipWrite: func(_ io.Writer, s string) (string, error) { copied = s; return "", nil }}.pushLayer(v)
	out, cmd := v.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if layerOf[*textTemplatesView](out) != nil {
		t.Fatal("the window is still open after y")
	}
	if cmd == nil {
		t.Fatal("no copy command")
	}
	runBatch(t, cmd) // the package helper that executes a tea.Cmd / tea.Batch
	if copied != "final text" {
		t.Fatalf("copied %q", copied)
	}
}

func TestTextTemplatesRenderedEscReturnsToBrowse(t *testing.T) {
	t.Parallel()
	v := &textTemplatesView{mode: ttRendered, rendered: "x", items: ttItems(1)}
	m := Model{}.pushLayer(v)
	out, _ := v.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if v.mode != ttBrowse || layerOf[*textTemplatesView](out) == nil {
		t.Fatalf("esc: mode %v", v.mode)
	}
}
```

(Match `clipWrite`'s real signature from `model.go:479` and the real batch-running test helper.)

- [ ] **Step 2: Run to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/text-templates && rtk go test ./internal/tui/ -run 'TextTemplate'`
Expected: FAIL.

- [ ] **Step 3: Implement**

`template_fill.go`:

```go
// newTemplateFillLabels is newTemplateFill for a caller that already knows
// the labels (a text template scans with its own token rules).
func newTemplateFillLabels(labels []string) templateFill {
	f := templateFill{labels: labels, fields: make([]textfield, len(labels))}
	for i := range f.fields {
		f.fields[i] = newTextField("")
	}
	return f
}
```

and make `newTemplateFill(value)` call it with `template.UserLabels(value)`.

`text_templates.go`:

```go
func (m Model) renderTextTemplateCmd(body string, inputs map[string]string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		text, seqs, err := svc.RenderTextTemplate(context.Background(), body, inputs)
		return textTemplateRenderedMsg{text: text, seqNames: seqs, err: err}
	}
}
```

In `updateBrowse`, `tea.KeyEnter`: no selection → nothing; `labels, _ := domain.TextTemplateTokens(t.Body)`; with labels → `v.fill = newTemplateFillLabels(labels); v.mode = ttFill`; without → `return m, m.renderTextTemplateCmd(t.Body, map[string]string{})`.

`updateFill`: `done, cancel := v.fill.handleKey(msg)`; cancel → `ttBrowse`; done → `return m, m.renderTextTemplateCmd(t.Body, v.fill.inputs())` (stay in `ttFill` until the message lands).

`model.go`:

```go
	case textTemplateRenderedMsg:
		if v := layerOf[*textTemplatesView](m); v != nil {
			v.mode, v.rScroll = ttRendered, 0
			v.rendered, v.seqNames, v.renderErr = msg.text, msg.seqNames, ""
			if msg.err != nil {
				v.rendered, v.renderErr = "", strings.Replace(msg.err.Error(), "template: ", "", 1)
			}
		}
		return m, nil
```

`updateRendered`:

```go
func (v *textTemplatesView) updateRendered(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		v.mode = ttBrowse
		return m, nil
	case tea.KeyUp:
		v.rScroll = max(0, v.rScroll-1)
		return m, nil
	case tea.KeyDown:
		v.rScroll++ // box() clamps
		return m, nil
	case tea.KeyPgUp:
		v.rScroll = max(0, v.rScroll-v.bodyPage(m))
		return m, nil
	case tea.KeyPgDown:
		v.rScroll += v.bodyPage(m)
		return m, nil
	}
	if msg.String() == "y" && v.renderErr == "" {
		svc, names := m.svc, v.seqNames
		bump := func() tea.Msg {
			if svc != nil && len(names) > 0 {
				_ = svc.BumpPrefixSeqs(context.Background(), names)
			}
			return nil
		}
		copyCmd := m.copyToClipboardCmd(i18n.T("copied the rendered text"), v.rendered)
		return m.popLayer(), tea.Batch(copyCmd, bump)
	}
	return m, nil
}
```

`box` gains two modes. Fill: title `i18n.T("%s — fill variables (%d/%d)", t.Title, v.fill.idx+1, len(v.fill.labels))`, blank, `v.fill.view(textW)…`, blank, `i18n.T("[enter/tab] next  [esc] back")`. Rendered: title `i18n.T("%s — rendered", t.Title)`, blank, the wrapped/scrolled text (the body-pane helper from Task 5, driven by `v.rScroll`) or `st().errorText.Render(v.renderErr)`, blank, `i18n.T("[y] copy and close  [↑/↓] scroll  [esc] back to templates")` — on an error `i18n.T("[esc] back to templates")`.

A copy failure is reported by the existing `clipboardCopiedMsg` handler as a status line (the window is already closed — every other copy action behaves this way).

- [ ] **Step 4: Run to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/text-templates && rtk go test ./internal/tui/ -run 'TextTemplate|TemplateFill|Prefix|I18n|i18n|ClipboardWriter'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/text-templates && git add internal/tui internal/i18n && git commit -m "feat(tui): text templates — fill variables, rendered text, y copies and closes"
```

---

### Task 7: TUI — add / edit in `$EDITOR`, delete

**Files:**
- Modify: `internal/tui/text_templates.go`, `internal/tui/model.go` (`textTemplateEditedMsg` case)
- Test: `internal/tui/text_templates_test.go`

**Interfaces:**
- Consumes: `handover`, `editorCommandAt`, `resolveEditor`, `removeTempFile`; domain add/update/remove.
- Produces:

```go
// textTemplateEditedMsg: the editor closed on the body temp file.
type textTemplateEditedMsg struct {
	path   string // the temp file
	title  string
	scope  model.ProfileScope
	editID string // "" = a new template
	before string // the body the editor was seeded with
	err    error  // the editor's exit error
}

func (m Model) editTextTemplateBodyCmd(title string, scope model.ProfileScope, editID, body string) tea.Cmd
func (m Model) saveTextTemplateCmd(msg textTemplateEditedMsg, body string) tea.Cmd
func (m Model) removeTextTemplateCmd(scope model.ProfileScope, id string) tea.Cmd
```

- [ ] **Step 1: Write the failing tests**

```go
func TestTextTemplatesFormAddFlow(t *testing.T) {
	t.Parallel()
	v := &textTemplatesView{items: ttItems(1)}
	v.update(Model{}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if v.mode != ttForm || v.editID != "" || v.scope != model.ProfileScopeGlobal {
		t.Fatalf("n: mode %v editID %q scope %v", v.mode, v.editID, v.scope)
	}
	// An empty title is refused inline, no editor.
	_, cmd := v.update(Model{}, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || v.formErr == "" {
		t.Fatalf("empty title: cmd nil %v err %q", cmd == nil, v.formErr)
	}
	for _, r := range "Standup" {
		v.update(Model{}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	v.update(Model{}, tea.KeyMsg{Type: tea.KeyDown})  // to scope
	v.update(Model{}, tea.KeyMsg{Type: tea.KeyRight}) // → this repo
	if v.scope != model.ProfileScopeRepo {
		t.Fatalf("scope toggle: %v", v.scope)
	}
	_, cmd = v.update(Model{}, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || v.formErr != "" {
		t.Fatalf("valid title must launch the editor (err %q)", v.formErr)
	}
}

func TestTextTemplatesEditSeedsFormAndLocksScope(t *testing.T) {
	t.Parallel()
	v := &textTemplatesView{items: []model.TextTemplate{{ID: "a", Title: "Alpha", Body: "b", Scope: model.ProfileScopeRepo}}}
	v.update(Model{}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if v.mode != ttForm || v.editID != "a" || v.fTitle.Value() != "Alpha" || v.scope != model.ProfileScopeRepo {
		t.Fatalf("e: %+v", v)
	}
	v.update(Model{}, tea.KeyMsg{Type: tea.KeyDown})
	v.update(Model{}, tea.KeyMsg{Type: tea.KeyRight})
	if v.scope != model.ProfileScopeRepo {
		t.Fatal("the scope must be fixed while editing")
	}
}

func ttEdited(t *testing.T, content string) (string, textTemplateEditedMsg) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "gg-x-template.md")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p, textTemplateEditedMsg{path: p, title: "New", scope: model.ProfileScopeGlobal}
}

// Review Focus 4: nothing is saved, the temp file goes, the window stays.
func TestTextTemplateEditedNothingToSave(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		content string
		mut     func(*textTemplateEditedMsg)
	}{
		"empty body":     {" \n\n", func(*textTemplateEditedMsg) {}},
		"editor failed":  {"text", func(m *textTemplateEditedMsg) { m.err = errors.New("exit status 1") }},
		"unchanged edit": {"same\n", func(m *textTemplateEditedMsg) { m.editID, m.before, m.title = "new", "same", "New" }},
	} {
		p, msg := ttEdited(t, tc.content)
		tc.mut(&msg)
		v := &textTemplatesView{mode: ttForm, items: []model.TextTemplate{{ID: "new", Title: "New", Body: "same"}}}
		out, cmd := Model{}.pushLayer(v).Update(msg)
		if cmd != nil {
			t.Errorf("%s: a save command was issued", name)
		}
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s: the temp file was not removed", name)
		}
		mm := out.(Model)
		if layerOf[*textTemplatesView](mm) == nil || v.mode != ttBrowse || mm.statusMsg == "" {
			t.Errorf("%s: window gone, or mode %v, or no status %q", name, v.mode, mm.statusMsg)
		}
	}
}

func TestTextTemplateEditedSaves(t *testing.T) {
	t.Parallel()
	p, msg := ttEdited(t, "Hello <user:x>\n\n")
	v := &textTemplatesView{mode: ttForm}
	_, cmd := Model{}.pushLayer(v).Update(msg)
	if cmd == nil {
		t.Fatal("no save command")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("the temp file was not removed")
	}
}

func TestTextTemplateEditedInvalidBodyKeepsForm(t *testing.T) {
	t.Parallel()
	_, msg := ttEdited(t, "bad <seq> token")
	v := &textTemplatesView{mode: ttForm}
	_, cmd := Model{}.pushLayer(v).Update(msg)
	if cmd != nil || v.mode != ttForm || !strings.Contains(v.formErr, "seq") {
		t.Fatalf("cmd nil %v mode %v err %q", cmd == nil, v.mode, v.formErr)
	}
}

func TestTextTemplatesDeleteNeedsConfirm(t *testing.T) {
	t.Parallel()
	v := &textTemplatesView{items: ttItems(2), sel: 1}
	_, cmd := v.update(Model{}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if v.mode != ttConfirmDelete || cmd != nil {
		t.Fatalf("d: mode %v", v.mode)
	}
	_, cmd = v.update(Model{}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if v.mode != ttBrowse || cmd != nil {
		t.Fatal("n must cancel")
	}
	v.update(Model{}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	_, cmd = v.update(Model{}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if v.mode != ttBrowse || cmd == nil {
		t.Fatal("y must issue the remove command")
	}
}
```

Plus one round-trip against a real service (the package's `testRepo(t, dir)` helper + `domain.TextTemplateStatePath` pointed at `t.TempDir()` via `svc.SetTextTemplateStores`): run `saveTextTemplateCmd` for a new template, assert `svc.TextTemplates` holds it and the returned `textTemplatesDataMsg.selectID` is its id; run it again with `editID` set and a new title, assert the rename.

- [ ] **Step 2: Run to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/text-templates && rtk go test ./internal/tui/ -run 'TextTemplate'`
Expected: FAIL.

- [ ] **Step 3: Implement**

`updateBrowse` additions: `n` → `v.fTitle = newTextField(""); v.scope = model.ProfileScopeGlobal; v.field = 0; v.formErr = ""; v.editID = ""; v.mode = ttForm`. `e` (with a selection) → the same seeded from the row (`newTextField(t.Title)`, `t.Scope`, `v.editID = t.ID`). `d` (with a selection) → `v.mode = ttConfirmDelete`.

`updateForm`: esc → browse; up/down/tab move `v.field` in `[0,1]`; on field 1 the keys `left right space h l` toggle the scope ONLY when `v.editID == ""`; enter:

```go
		title := strings.TrimSpace(v.fTitle.Value())
		body := ""
		if t, ok := v.selected(); ok && v.editID != "" {
			body = t.Body
		}
		// Validate the title alone: a placeholder body keeps the body checks quiet.
		if err := domain.ValidateTextTemplate(title, "x"); err != nil {
			v.formErr = strings.TrimPrefix(err.Error(), "invalid text template: ")
			return m, nil
		}
		v.formErr = ""
		return m, m.editTextTemplateBodyCmd(title, v.scope, v.editID, body)
```

other keys → `v.fTitle.HandleEditKey(msg)` when `v.field == 0`.

```go
// editTextTemplateBodyCmd hands the body to the user's editor: a private temp
// file ending in .md (so the editor highlights it), seeded with body.
func (m Model) editTextTemplateBodyCmd(title string, scope model.ProfileScope, editID, body string) tea.Cmd {
	f, err := os.CreateTemp("", "gg-*-template.md")
	if err != nil {
		return func() tea.Msg { return textTemplateEditedMsg{err: err} }
	}
	path := f.Name()
	_, werr := f.WriteString(body)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		removeTempFile(path)
		return func() tea.Msg { return textTemplateEditedMsg{err: werr} }
	}
	cmd := editorCommandAt(resolveEditor(), path, 0)
	return handover(cmd, func(err error) tea.Msg {
		return textTemplateEditedMsg{path: path, title: title, scope: scope, editID: editID, before: body, err: err}
	})
}
```

`model.go`:

```go
	case textTemplateEditedMsg:
		data, rerr := os.ReadFile(msg.path)
		removeTempFile(msg.path)
		v := layerOf[*textTemplatesView](m)
		if v == nil {
			return m, nil
		}
		body := strings.TrimRight(string(data), " \t\r\n")
		oldTitle := ""
		if t, ok := v.selected(); ok && msg.editID != "" {
			oldTitle = t.Title
		}
		switch {
		case msg.err != nil:
			v.mode, m.statusMsg = ttBrowse, i18n.T("text template not saved: %s", msg.err.Error())
		case rerr != nil:
			v.mode, m.statusMsg = ttBrowse, i18n.T("text template not saved: %s", rerr.Error())
		case strings.TrimSpace(body) == "":
			v.mode, m.statusMsg = ttBrowse, i18n.T("text template not saved: the text is empty")
		case msg.editID != "" && body == strings.TrimRight(msg.before, " \t\r\n") && msg.title == oldTitle:
			v.mode, m.statusMsg = ttBrowse, i18n.T("text template unchanged")
		default:
			if err := domain.ValidateTextTemplate(msg.title, body); err != nil {
				v.formErr = strings.Replace(strings.TrimPrefix(err.Error(), "invalid text template: "), "template: ", "", 1)
				return m, nil // the form stays; enter reopens the editor
			}
			v.mode, v.loading = ttBrowse, true
			return m, m.saveTextTemplateCmd(msg, body)
		}
		return m, nil
```

One wrinkle the invalid-body case must handle: reopening the editor from the form would reseed it with the OLD body and lose the user's text. Keep the rejected text: add `draft string` to the view, set `v.draft = body` in the invalid branch, and in `updateForm`'s enter use `v.draft` as the seed when it is non-empty (clear it on esc, on a successful save, and when the form is opened). Extend `TestTextTemplateEditedInvalidBodyKeepsForm` to assert `v.draft == "bad <seq> token"`.

```go
func (m Model) saveTextTemplateCmd(msg textTemplateEditedMsg, body string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ctx := context.Background()
		t := model.TextTemplate{Title: msg.title, Body: body, Scope: msg.scope}
		var saved model.TextTemplate
		var err error
		if msg.editID == "" {
			saved, err = svc.AddTextTemplate(ctx, t)
		} else {
			saved, err = svc.UpdateTextTemplate(ctx, msg.scope, msg.editID, t)
		}
		if err != nil {
			return textTemplatesDataMsg{err: err}
		}
		items, lerr := svc.TextTemplates(ctx)
		return textTemplatesDataMsg{items: items, err: lerr, selectID: saved.ID, status: i18n.T("saved text template %s", saved.Title)}
	}
}
```

A save error (a duplicate title) arrives as `textTemplatesDataMsg{err}` → status line `text templates: …`; make that handler also reload nothing and leave the rows as they were (it already returns early).

`updateConfirm`: `y` → `v.mode = ttBrowse`, `return m, m.removeTextTemplateCmd(t.Scope, t.ID)`; `n`/esc → browse. `removeTextTemplateCmd` mirrors `removePrefixCmd` and returns a `textTemplatesDataMsg` with `status: i18n.T("deleted text template %s", title)`.

`box` gains: the form (title `i18n.T("Add text template")` / `i18n.T("Edit text template")`, `viewField(cur+i18n.T("title: "), v.fTitle, v.field == 0, textW)`, the scope line as in `prefix_settings.go` — dimmed with no cursor movement effect when editing — the optional `formErr`, the tokens line `i18n.T("Tokens: <user:LABEL> <date> <date:FMT> <branch> <parent-branch> <repo> <seq:NAME:N> <random-*>")`, blank, hints `i18n.T("[↑/↓] field  [←/→] scope  [enter] edit text in $EDITOR  [esc] back")`); and the confirm (`i18n.T("Delete text template %s?", t.Title)`, blank, `i18n.T("[y] delete  [n] keep")`).

Guard the handover behind `m.quiet` if the headless harness requires it for never-ending commands (see memory: new never-ending TUI commands must be gated by `m.quiet`) — the editor command is a one-shot exec, so follow what `editFileCmd` does.

- [ ] **Step 4: Run to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/text-templates && rtk go test ./internal/tui/ -run 'TextTemplate|Handover|I18n|i18n'`
Expected: PASS (including the grep test that every terminal handover rides `handover()`).

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/text-templates && git add internal/tui internal/i18n && git commit -m "feat(tui): text templates — add and edit in \$EDITOR, delete"
```

---

### Task 8: TUI — advertise + golden screen

**Files:**
- Modify: `internal/tui/help.go` (a row beside `alt+a`/`alt+t` at ~:90), `internal/tui/footer.go` (a row beside `last-terminal` at ~:220), the command palette's command list (find it: `grep -n '"last-terminal"\|paletteCommands\|func (m Model) openCommandPalette' internal/tui/*.go`), `internal/i18n` bundles
- Create: `e2e/scenarios/tui_text_templates.toml` + `.screens`
- Test: `internal/tui/text_templates_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestTextTemplatesAdvertised(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	if !strings.Contains(ansi.Strip(m.helpText()), "alt+x") { // the help renderer the neighbouring help tests use
		t.Error("alt+x is missing from help")
	}
	found := false
	for _, c := range m.paletteCommands() { // the palette's command source
		if c.id == "text-templates" {
			found = true
		}
	}
	if !found {
		t.Error("the command palette has no text-templates entry")
	}
}

func TestAltXIgnoredUnderALayer(t *testing.T) {
	t.Parallel()
	m := newTestModel(t).pushLayer(&prefixSettingsView{})
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}, Alt: true})
	if layerOf[*textTemplatesView](out.(Model)) != nil {
		t.Fatal("alt+x opened the window over another layer")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/text-templates && rtk go test ./internal/tui/ -run 'TextTemplatesAdvertised|AltXIgnored'`
Expected: FAIL on the help/palette assertions.

- [ ] **Step 3: Implement**

- `help.go`: `r("alt+x", i18n.T("text templates: reusable multi-line texts with <user:LABEL>, <date>, <branch>, <seq:NAME> … tokens (global or this repo). enter asks for the variables and shows the rendered text; y copies it and closes. n adds, e edits (title here, the text in $EDITOR), d deletes")),`
- `footer.go`: `{"text-templates", "alt+x", i18n.T("[alt+x] templates"), func(Model) bool { return true }, scopeGlobal},`
- Palette: an entry `text-templates` labelled `i18n.T("Text templates…")` whose run is `func(m Model) (tea.Model, tea.Cmd) { return m.openTextTemplates() }`, registered where the palette builds its commands (if the palette is derived from the footer/help registry the footer row may already supply it — then only assert it).
- All new keys into the four bundles.

- [ ] **Step 4: Golden screen** (use the `writing-e2e-scenarios` skill for the `[tui]` step syntax)

`e2e/scenarios/tui_text_templates.toml`: input = one commit + `body.md` (`"## <user:title>\n\nBranch: <branch> · <date>\nTicket: <user:ticket>\n"`); `[[run]]` `template add --global --title "PR description" -F body.md` and a second repo-scope template; then `[tui]` steps: `alt+x` → screen `browse`; `enter` → screen `fill`; type `Hello`, `enter`, type `GG-1`, `enter` → screen `rendered`; `esc`, `esc`. Generate with `-update`, then READ the three screens and confirm: the list rows + scope tags, the body, `Variables: title · ticket`, `Automatic: <branch> · <date>`, a blank line above the hints, the rendered text with the frozen date.

Run: `cd /work/gigagit/.claude/worktrees/text-templates && ./test.sh e2e`
Expected: green.

- [ ] **Step 5: Run the TUI gates**

Run: `cd /work/gigagit/.claude/worktrees/text-templates && rtk go test ./internal/tui/ ./internal/i18n/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /work/gigagit/.claude/worktrees/text-templates && git add internal/tui internal/i18n e2e/scenarios/tui_text_templates.toml e2e/scenarios/tui_text_templates.screens && git commit -m "feat(tui): text templates in help, footer and palette; golden screens"
```

---

### Task 9: Web view

**Files:**
- Create: `internal/web/texttemplates.go`, `internal/web/static/texttemplates.js`
- Modify: `internal/web/server.go` (routes beside `/api/prefixes` at ~:215), `internal/web/static/index.html` (script tag + overlay container), `internal/web/static/style.css`, the palette/keys module that registers page commands and the `?` help overlay (find them: `grep -n "prefixes" internal/web/static/*.js`)
- Test: `internal/web/texttemplates_test.go`

**Interfaces:**
- Consumes: Task 3 domain API; `writeGuard`, `writeJSON`, `writeErr`, `readCtx`, `parseProfileScope` from the web package.
- Produces routes:
  - `GET /api/text-templates` → `{"templates":[{id,title,body,scope,user_labels,automatic}]}`
  - `POST /api/text-templates` `{title, body, scope}` → the row
  - `POST /api/text-templates/update` `{id, scope, title, body}` → the row
  - `POST /api/text-templates/remove` `{id, scope}` → `{"ok":true}`
  - `POST /api/text-templates/render` `{id, scope, inputs}` → `{"text":…, "seq_names":[…]}` (peeks)
  - `POST /api/text-templates/take` `{seq_names}` → `{"ok":true}` (bumps; the page calls it after a successful Copy)

- [ ] **Step 1: Write the failing handler tests**

Mirror `internal/web/prefixes_test.go`'s server/fixture helpers (`TestPrefixesAddListRemove` at :53 shows them):

- `TestTextTemplatesAddListUpdateRemove`: POST add (`global`, title `Note`, body `Hi <user:n>`) → 200 with `id == "note"`, `user_labels == ["n"]`; GET lists it; POST update with title `Memo` → `id == "memo"`; POST remove → GET is empty.
- `TestTextTemplatesRefusals`: bad scope → 400; empty title → 400; body `x <seq> y` → 400; a duplicate title → 409; unknown id on update/remove/render → 404; a write without the guard headers → the guard's refusal (as the prefix refusal test asserts).
- `TestTextTemplateRenderPeeksTakeBumps`: add body `#<seq:w:2> <user:n>`; render twice → `#01 a` both times with `seq_names == ["w"]`; POST take; render → `#02 a`; render with a missing input → 400 naming the label.

- [ ] **Step 2: Run to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/text-templates && rtk go test ./internal/web/ -run 'TextTemplate'`
Expected: FAIL — 404 on the routes.

- [ ] **Step 3: Implement the handlers**

```go
package web

type textTemplateRow struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	Scope      string   `json:"scope"` // "global" | "repo"
	UserLabels []string `json:"user_labels"`
	Automatic  []string `json:"automatic"`
}

func textTemplateRowOf(t model.TextTemplate) textTemplateRow {
	labels, auto := domain.TextTemplateTokens(t.Body)
	if labels == nil {
		labels = []string{}
	}
	if auto == nil {
		auto = []string{}
	}
	return textTemplateRow{ID: t.ID, Title: t.Title, Body: t.Body, Scope: t.Scope.String(), UserLabels: labels, Automatic: auto}
}
```

`handleTextTemplates` lists via `svc.TextTemplates(readCtx(r))`. Add/update decode the body, `parseProfileScope` (400), `domain.ValidateTextTemplate` (400), call the domain; map errors with a small helper: an error whose text contains "already exists" or "needs a letter" → 409 / 400, "not found" → 404, else 500. Because `web` must not import `texttmpl`, export sentinel checks from domain: add to `internal/domain/texttemplates.go`

```go
// IsTextTemplateNotFound / IsTextTemplateDuplicate classify store errors for
// frontends, which must not import internal/texttmpl.
func IsTextTemplateNotFound(err error) bool  { return errors.Is(err, texttmpl.ErrNotFound) }
func IsTextTemplateDuplicate(err error) bool { return errors.Is(err, texttmpl.ErrDuplicate) }
```

(with a domain test for each) and use them in the handlers. Render: resolve the scope, `svc.FindTextTemplate(ctx, id, &scope)` requiring an exact id (compare `t.ID == req.ID`, else 404), check every label is present in `inputs` (400 `missing variable <label>`), `svc.RenderTextTemplate` (resolve error → 400). Take: `svc.BumpPrefixSeqs(ctx, req.SeqNames)`, each name validated against `^[A-Za-z0-9_.-]{1,64}$` (400 otherwise) — the wire value never reaches a path unchecked.

Register the six routes; the five POSTs wrapped in `writeGuard`.

- [ ] **Step 4: Run to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/text-templates && rtk go test ./internal/web/ ./internal/domain/ -run 'TextTemplate'`
Expected: PASS.

- [ ] **Step 5: The page**

`static/texttemplates.js`, following the module shape and helpers of the prefix section in `static/settings.js` (fetch wrappers, the overlay/layer helpers in `layers.js`, the copy helper used by link copy):

- `openTextTemplates()` — an overlay `#texttemplates` (hide/show BY ID with its own `#texttemplates.hidden` rule in `style.css` — the page has no global `.hidden`): a title list (max height 8 rows, scrolling, scope badge), the selected body in a `<pre>` (wrapping, scrollable), then `Variables: …` and `Automatic: …` lines; buttons Fill, Add, Edit, Delete; keys ↑/↓, Enter, `n`, `e`, `d`, Esc.
- Fill: a dialog with one single-line `<input>` per label; submit → `POST …/render` → the rendered view (`<pre>` + Copy + Back). Copy → the page's clipboard helper; on success `POST …/take` with the returned `seq_names`, then close the overlay; on failure show the error and stay.
- Add/Edit: a dialog with the title input, a scope `<select>` (disabled on edit) and a `<textarea>`; a 400/409 shows the server's message inline.
- Delete: the page's confirm dialog, then `POST …/remove`.
- All text inserted with `textContent` (never `innerHTML`) — bodies are arbitrary user text.
- Register a palette command "Text templates…" and the `alt+x` key where the page registers its global keys; add a row to the `?` help overlay.

- [ ] **Step 6: Verify in a browser** (memory: playwright probe; must assert VISIBILITY; rebuild + restart + hard reload; identify the binary)

Build `cd /work/gigagit/.claude/worktrees/text-templates && go build -o bin/gg ./cmd/gg`, start `bin/gg web` on a scratch repo with two templates added through the CLI, and drive: open by key → the overlay is visible with both titles and the body; Fill → inputs → the rendered text is visible and contains the typed values; Add a template through the dialog → it appears in the list and in `bin/gg template list`. Run the same probe against a build WITHOUT the script tag first to prove the assertions can fail.

- [ ] **Step 7: Commit**

```bash
cd /work/gigagit/.claude/worktrees/text-templates && git add internal/web internal/domain/texttemplates.go internal/domain/texttemplates_test.go && git commit -m "feat(web): text templates view — list, fill, render, copy, add/edit/delete"
```

---

### Task 10: Docs, gates, review

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`, `CLAUDE.md` (one package-map row for `texttmpl`; extend the `template`, `cli`, `tui`, `web` rows by a few words only if needed)

- [ ] **Step 1: Docs**

CHANGELOG entry (TUI window + key, CLI verbs, web view, token rules); README: a "Text templates" subsection with the key, the CLI verbs and the token list; `docs/CLAUDE-details.md`: the store location, `ResolveText` rules, peek/take, the editor round-trip and the `draft` rule; `CLAUDE.md` row:

`| \`texttmpl\`   | Writable two-scope registry of text templates (titled multi-line texts with the prefix token grammar; records only, TOML under XDG state). Owned by \`domain\`; frontends never import it. |`

- [ ] **Step 2: Full gate**

Run: `cd /work/gigagit/.claude/worktrees/text-templates && ./test.sh race > /tmp/claude-1000/-work-gigagit/7af8f807-6413-4447-b57a-9679e0740207/scratchpad/race.log 2>&1; tail -5 /tmp/claude-1000/-work-gigagit/7af8f807-6413-4447-b57a-9679e0740207/scratchpad/race.log`
Expected: the log ends with "all green" (a zero exit from `tail` proves nothing).

- [ ] **Step 3: Commit docs**

```bash
cd /work/gigagit/.claude/worktrees/text-templates && git add CHANGELOG.md README.md docs/CLAUDE-details.md CLAUDE.md && git commit -m "docs: text templates"
```

- [ ] **Step 4: Whole-branch review**

One read-only review subagent on the most capable model over `main..feat/text-templates` (no edits, no commits, no test runs that write into the worktree), briefed with the spec path and the Review Focus list. Fix what it finds, re-run the gate.

- [ ] **Step 5: Hand over**

Build the verify binary (`go build -o bin/gg ./cmd/gg`), give the user its absolute path, and ASK before merging (`gg merge -F <msgfile> --into main feat/text-templates`, then `./build.sh install` and `./build.sh web`).
