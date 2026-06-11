# M2 Worktree A1 — Config + Template Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the pure, I/O-light foundation for worktree management — a `config` package (global + per-repo TOML with field-level overlay, plus machine-local `<seq>` counters) and a pure `template` resolver — both fully unit-tested.

**Architecture:** Two new packages under `internal/`. `internal/template` is **pure** (no I/O): it parses templated strings (`<parent-branch>`, `<repo>`, `<branch>`, `<date:FMT>`, `<random-alpha:N>`, `<random-num:N>`, `<seq:NAME:N>`, `<user:LABEL>`) and resolves them from an injected `Ctx` (with injected `Now`/`Rand` for determinism). `internal/config` loads the two TOML files into a typed `Config` by **explicit field-by-field merge** (defaults → global → repo, repo wins), and reads/writes the `<seq>` counters in `<repo>/.git/gg/state.toml`. Nothing here touches `git`; downstream plans (A2/A3) consume both packages.

**Tech Stack:** Go 1.26, `github.com/pelletier/go-toml/v2` (new dep), `math/rand/v2` (stdlib, seedable). Module is `github.com/gigagit/gg`.

**Spec:** `docs/superpowers/specs/2026-06-11-worktree-management-design.md` §3, §4, §5, §12.

**Conventions (read before starting):**
- TDD red→green. After each task: `go test ./...`, `go vet ./...`, and `gofmt -l internal` must be clean (empty output from `gofmt -l`).
- LF line endings only (the repo enforces this via `.gitattributes`; the working drive is Windows-mounted — never reintroduce CRLF).
- Commit messages end with a trailing `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` line.
- Plain `fmt.Errorf` for errors (matches existing packages like `internal/git`). No custom error types unless a task says so.
- Existing test style: standard library `testing`, table-driven where natural, `t.TempDir()` for filesystem, no third-party assert libraries. See `internal/git/repo_test.go` and `internal/model/model_test.go` for the house style.

---

## File Structure

**`internal/template/` (pure resolver):**
- `template.go` — `Ctx` struct, `Resolve`, and the token dispatch.
- `tokens.go` — `UserLabels`, `SeqNames` (parsing helpers), the shared token regex, and the `<date:FMT>` → Go-layout mapping.
- `template_test.go`, `tokens_test.go` — unit tests.

**`internal/config/` (TOML config + counters):**
- `config.go` — `Config`/`WorktreeConfig` types, built-in `Defaults()`, `Load`, and `DefaultGlobalPath`.
- `state.go` — `PeekSeq` / `BumpSeq` over `<gitDir>/gg/state.toml`.
- `config_test.go`, `state_test.go` — unit tests.

Each file has one responsibility. `template` never imports `config`; `config` never imports `template`. Neither imports `internal/git`.

---

## Task 1: Add the go-toml dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the dependency**

Run from the repo root:
```bash
go get github.com/pelletier/go-toml/v2@v2.2.3
```
Expected: `go.mod` gains a `require github.com/pelletier/go-toml/v2 v2.2.3` line; `go.sum` gains its hashes. (If v2.2.3 is unavailable, use the latest `v2.x` `go get` resolves and note the version in the commit message.)

- [ ] **Step 2: Verify the module still builds**

Run: `go build ./...`
Expected: success, no output. (No code uses the dep yet; this just confirms the module graph is intact.)

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "build: add github.com/pelletier/go-toml/v2 dependency

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: Template token parsing — `UserLabels` and `SeqNames`

The resolver and the create-flow need to know, *before* resolving, which `<user:LABEL>` fields to render and which `<seq:NAME>` counters to read/bump. These two functions parse a template string and return those, distinct and in order of first appearance.

**Files:**
- Create: `internal/template/tokens.go`
- Test: `internal/template/tokens_test.go`

- [ ] **Step 1: Write the failing test**

```go
package template

import (
	"reflect"
	"testing"
)

func TestUserLabels(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		want []string
	}{
		{"none", "b/from-<parent-branch>", nil},
		{"one", "issue/<user:issue-id>", []string{"issue-id"}},
		{"distinct in order", "<user:user>/fix/<user:issue-id>", []string{"user", "issue-id"}},
		{"dedup repeated", "<user:id>-<user:id>", []string{"id"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := UserLabels(tc.tmpl)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("UserLabels(%q) = %v, want %v", tc.tmpl, got, tc.want)
			}
		})
	}
}

func TestSeqNames(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		want []string
	}{
		{"none", "b/<parent-branch>", nil},
		{"padded", "deploy-<seq:deploy:4>", []string{"deploy"}},
		{"unpadded", "n<seq:issue>", []string{"issue"}},
		{"distinct in order", "<seq:b>-<seq:a>-<seq:b>", []string{"b", "a"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SeqNames(tc.tmpl)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("SeqNames(%q) = %v, want %v", tc.tmpl, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/template/ -run 'TestUserLabels|TestSeqNames' -v`
Expected: FAIL — `undefined: UserLabels` / `undefined: SeqNames` (compile error).

- [ ] **Step 3: Write minimal implementation**

```go
// Package template resolves gigagit's worktree naming templates. It is pure:
// it performs no I/O and draws all time/randomness from an injected Ctx, so
// resolution is deterministic and fully unit-testable.
package template

import "regexp"

// tokenRe matches a single <...> token, capturing the inside (no '>' allowed,
// so tokens never span). Used by every parsing/resolution function.
var tokenRe = regexp.MustCompile(`<([^>]+)>`)

// UserLabels returns the distinct <user:LABEL> labels in order of first
// appearance, so a frontend knows which input fields to render.
func UserLabels(tmpl string) []string {
	return distinctTokenArgs(tmpl, "user")
}

// SeqNames returns the distinct <seq:NAME> counter names in order of first
// appearance, so the create flow knows which counters to peek and bump.
func SeqNames(tmpl string) []string {
	return distinctTokenArgs(tmpl, "seq")
}

// distinctTokenArgs scans tmpl for tokens of the form <prefix:ARG...> and
// returns the first colon-separated segment after the prefix (for seq this is
// the NAME, ignoring any :N padding; for user it is the LABEL), distinct and
// ordered.
func distinctTokenArgs(tmpl, prefix string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range tokenRe.FindAllStringSubmatch(tmpl, -1) {
		body := m[1]
		p, rest, ok := cutColon(body)
		if !ok || p != prefix {
			continue
		}
		arg, _, _ := cutColon(rest) // for "user" rest has no further colon; for seq, drop :N
		if arg == "" {
			arg = rest
		}
		if !seen[arg] {
			seen[arg] = true
			out = append(out, arg)
		}
	}
	return out
}

// cutColon splits s on the first ':' into (before, after, found).
func cutColon(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}
```

Note on `<user:LABEL>`: a label may itself contain no colon, so `cutColon(rest)` returns `("", "", false)` and we fall back to `rest`. For `<seq:NAME:N>`, `rest` is `NAME:N`, and `cutColon` yields `NAME`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/template/ -run 'TestUserLabels|TestSeqNames' -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/template
git add internal/template/tokens.go internal/template/tokens_test.go
git commit -m "feat(template): parse <user:> labels and <seq:> names

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: `<date:FMT>` human-token → Go layout mapping

`<date:yyyy-MM-dd HH:mm:ss>` must map human tokens to Go's reference layout before formatting. This is a pure string transform; isolating it keeps `Resolve` readable.

**Files:**
- Modify: `internal/template/tokens.go`
- Modify: `internal/template/tokens_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/template/tokens_test.go`:
```go
func TestGoLayout(t *testing.T) {
	tests := []struct{ in, want string }{
		{"yyyy-MM-dd", "2006-01-02"},
		{"yyyy/MM/dd HH:mm:ss", "2006/01/02 15:04:05"},
		{"HH:mm", "15:04"},
		{"yyyyMMdd", "20060102"},
	}
	for _, tc := range tests {
		if got := goLayout(tc.in); got != tc.want {
			t.Errorf("goLayout(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/template/ -run TestGoLayout -v`
Expected: FAIL — `undefined: goLayout`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/template/tokens.go`:
```go
import "strings" // add to the existing import block; do not duplicate the block

// dateLayoutReplacer maps human date tokens to Go's reference-time layout.
// Order matters only in that longer tokens are listed before shorter ones that
// could be a prefix; the current token set has no such overlap, but the ordered
// slice keeps the mapping explicit and stable.
var dateLayoutReplacer = strings.NewReplacer(
	"yyyy", "2006",
	"MM", "01",
	"dd", "02",
	"HH", "15",
	"mm", "04",
	"ss", "05",
)

// goLayout converts a human date format (yyyy MM dd HH mm ss, with arbitrary
// separators) into Go's reference-time layout string.
func goLayout(human string) string {
	return dateLayoutReplacer.Replace(human)
}
```

(`strings.NewReplacer` scans left-to-right and does not re-examine already-replaced text, so `MM`→`01` and `mm`→`04` do not collide, and digits introduced by one replacement are never re-matched.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/template/ -run TestGoLayout -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/template
git add internal/template/tokens.go internal/template/tokens_test.go
git commit -m "feat(template): map human date tokens to Go layout

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: `Ctx` and `Resolve` — substitution tokens + unknown-token error

The core resolver. This task handles the substitution/computed tokens that do **not** need randomness: `<parent-branch>`, `<repo>`, `<branch>` (sanitized, path-only), `<date:FMT>`, `<seq:NAME:N>`, `<user:LABEL>`, plus the unknown-token error and the missing-input error. (`<random-*>` lands in Task 5.)

**Files:**
- Create: `internal/template/template.go`
- Test: `internal/template/template_test.go`

- [ ] **Step 1: Write the failing test**

```go
package template

import (
	"strings"
	"testing"
	"time"
)

func fixedCtx() Ctx {
	return Ctx{
		ParentBranch: "main",
		Repo:         "aaa",
		Seqs:         map[string]int{"issue": 42},
		Now:          func() time.Time { return time.Date(2026, 6, 11, 14, 5, 9, 0, time.UTC) },
	}
}

func TestResolveSubstitutionTokens(t *testing.T) {
	tests := []struct {
		name   string
		tmpl   string
		inputs map[string]string
		ctx    Ctx
		want   string
	}{
		{"parent and repo", "<repo>/from-<parent-branch>", nil, fixedCtx(), "aaa/from-main"},
		{"date", "d-<date:yyyy-MM-dd HH:mm>", nil, fixedCtx(), "d-2026-06-11 14:05"},
		{"seq padded", "i-<seq:issue:4>", nil, fixedCtx(), "i-0042"},
		{"seq unpadded", "i-<seq:issue>", nil, fixedCtx(), "i-42"},
		{"seq missing is zero", "i-<seq:unknown:3>", nil, fixedCtx(), "i-000"},
		{"user input", "issue/<user:issue-id>", map[string]string{"issue-id": "777"}, fixedCtx(), "issue/777"},
		{"user reused once", "<user:id>-<user:id>", map[string]string{"id": "x"}, fixedCtx(), "x-x"},
		{"literal passthrough", "no-tokens-here", nil, fixedCtx(), "no-tokens-here"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Resolve(tc.tmpl, tc.inputs, tc.ctx)
			if err != nil {
				t.Fatalf("Resolve(%q) error: %v", tc.tmpl, err)
			}
			if got != tc.want {
				t.Fatalf("Resolve(%q) = %q, want %q", tc.tmpl, got, tc.want)
			}
		})
	}
}

func TestResolveBranchToken(t *testing.T) {
	// <branch> substitutes the sanitized resolved branch, path-template only.
	ctx := fixedCtx()
	ctx.Branch = "issue/123"
	got, err := Resolve("../<repo>.worktrees/<branch>", nil, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "../aaa.worktrees/issue-123" {
		t.Fatalf("got %q, want ../aaa.worktrees/issue-123", got)
	}
}

func TestResolveErrors(t *testing.T) {
	t.Run("unknown token", func(t *testing.T) {
		_, err := Resolve("x-<bogus>", nil, fixedCtx())
		if err == nil || !strings.Contains(err.Error(), "bogus") {
			t.Fatalf("want unknown-token error mentioning the token, got %v", err)
		}
	})
	t.Run("branch without ctx.Branch", func(t *testing.T) {
		_, err := Resolve("p/<branch>", nil, fixedCtx()) // ctx.Branch == ""
		if err == nil || !strings.Contains(err.Error(), "path") {
			t.Fatalf("want path-only error for <branch>, got %v", err)
		}
	})
	t.Run("missing user input", func(t *testing.T) {
		_, err := Resolve("<user:missing>", nil, fixedCtx())
		if err == nil || !strings.Contains(err.Error(), "missing") {
			t.Fatalf("want missing-input error, got %v", err)
		}
	})
	t.Run("bad seq pad", func(t *testing.T) {
		_, err := Resolve("<seq:issue:x>", nil, fixedCtx())
		if err == nil {
			t.Fatalf("want error for non-numeric seq padding")
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/template/ -run 'TestResolve' -v`
Expected: FAIL — `undefined: Ctx` / `undefined: Resolve`.

- [ ] **Step 3: Write minimal implementation**

```go
package template

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"
)

// Ctx carries everything Resolve needs beyond the <user:> inputs. Now and Rand
// are injected so resolution is deterministic in tests. The resolver never
// mutates any field (notably Seqs).
type Ctx struct {
	ParentBranch string         // <parent-branch>
	Repo         string         // <repo>
	Branch       string         // <branch> (path templates only); "" means unset
	Seqs         map[string]int // current <seq:NAME> values, supplied by the caller
	Now          func() time.Time
	Rand         *rand.Rand
}

// Resolve substitutes every <...> token in tmpl. inputs supplies <user:LABEL>
// values. Unknown tokens, a <branch> token with an unset Ctx.Branch, a missing
// user input, or malformed token arguments are returned as errors (never
// silently passed through).
func Resolve(tmpl string, inputs map[string]string, ctx Ctx) (string, error) {
	var firstErr error
	out := tokenRe.ReplaceAllStringFunc(tmpl, func(tok string) string {
		body := tok[1 : len(tok)-1] // strip < >
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

func resolveToken(body string, inputs map[string]string, ctx Ctx) (string, error) {
	prefix, rest, hasColon := cutColon(body)
	switch prefix {
	case "parent-branch":
		return ctx.ParentBranch, nil
	case "repo":
		return ctx.Repo, nil
	case "branch":
		if ctx.Branch == "" {
			return "", fmt.Errorf("template: <branch> is only valid in path templates")
		}
		return sanitizeSegment(ctx.Branch), nil
	case "date":
		if !hasColon {
			return "", fmt.Errorf("template: <date> requires a format, e.g. <date:yyyy-MM-dd>")
		}
		return ctx.Now().Format(goLayout(rest)), nil
	case "seq":
		return resolveSeq(rest, ctx)
	case "user":
		if !hasColon {
			return "", fmt.Errorf("template: <user> requires a label, e.g. <user:issue-id>")
		}
		v, ok := inputs[rest]
		if !ok {
			return "", fmt.Errorf("template: missing input for <user:%s>", rest)
		}
		return v, nil
	case "random-alpha", "random-num":
		return resolveRandom(prefix, rest, hasColon, ctx)
	default:
		return "", fmt.Errorf("template: unknown token <%s>", body)
	}
}

// resolveSeq handles <seq:NAME> and <seq:NAME:N>. The value comes from ctx.Seqs
// (0 if absent); N zero-pads.
func resolveSeq(rest string, ctx Ctx) (string, error) {
	name, padStr, hasPad := cutColon(rest)
	if name == "" {
		return "", fmt.Errorf("template: <seq> requires a name, e.g. <seq:issue>")
	}
	n := ctx.Seqs[name]
	if !hasPad {
		return strconv.Itoa(n), nil
	}
	pad, err := strconv.Atoi(padStr)
	if err != nil || pad < 0 {
		return "", fmt.Errorf("template: <seq:%s:%s> padding must be a non-negative integer", name, padStr)
	}
	return fmt.Sprintf("%0*d", pad, n), nil
}

// sanitizeSegment makes a branch name safe as a single path segment ('/' -> '-').
func sanitizeSegment(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}
```

Note: `resolveRandom` is referenced here but implemented in Task 5; for this task's tests to compile, add a temporary stub at the bottom of `template.go`:
```go
// resolveRandom is fully implemented in the next task.
func resolveRandom(prefix, rest string, hasColon bool, ctx Ctx) (string, error) {
	return "", fmt.Errorf("template: %s not yet implemented", prefix)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/template/ -run 'TestResolve' -v`
Expected: PASS (all subtests; the random stub is not exercised by these tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/template
git add internal/template/template.go internal/template/template_test.go
git commit -m "feat(template): resolve substitution, date, seq, and user tokens

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5: `<random-alpha:N>` and `<random-num:N>`

Replace the stub with a real, deterministic-under-injection random generator.

**Files:**
- Modify: `internal/template/template.go`
- Modify: `internal/template/template_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/template/template_test.go`:
```go
func TestResolveRandom(t *testing.T) {
	mk := func() Ctx {
		c := fixedCtx()
		c.Rand = rand.New(rand.NewPCG(1, 2)) // seeded: deterministic
		return c
	}

	alpha, err := Resolve("b-<random-alpha:4>", nil, mk())
	if err != nil {
		t.Fatalf("alpha error: %v", err)
	}
	if len(alpha) != len("b-")+4 {
		t.Fatalf("alpha length wrong: %q", alpha)
	}
	for _, r := range strings.TrimPrefix(alpha, "b-") {
		if r < 'a' || r > 'z' {
			t.Fatalf("random-alpha produced non-lowercase-letter %q in %q", r, alpha)
		}
	}

	num, err := Resolve("n-<random-num:6>", nil, mk())
	if err != nil {
		t.Fatalf("num error: %v", err)
	}
	digits := strings.TrimPrefix(num, "n-")
	if len(digits) != 6 {
		t.Fatalf("num length wrong: %q", num)
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			t.Fatalf("random-num produced non-digit %q in %q", r, num)
		}
	}

	// Same seed -> same output (determinism for tests/preview reproducibility).
	a1, _ := Resolve("<random-alpha:8>", nil, mk())
	a2, _ := Resolve("<random-alpha:8>", nil, mk())
	if a1 != a2 {
		t.Fatalf("seeded random not deterministic: %q vs %q", a1, a2)
	}
}

func TestResolveRandomErrors(t *testing.T) {
	c := fixedCtx()
	c.Rand = rand.New(rand.NewPCG(1, 2))
	for _, tmpl := range []string{"<random-alpha>", "<random-alpha:0>", "<random-num:-1>", "<random-alpha:x>"} {
		if _, err := Resolve(tmpl, nil, c); err == nil {
			t.Errorf("Resolve(%q): want error, got nil", tmpl)
		}
	}
}
```

Add `"math/rand/v2"` to the test file's import block (alias not needed — the package name is `rand`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/template/ -run TestResolveRandom -v`
Expected: FAIL — the stub returns a "not yet implemented" error.

- [ ] **Step 3: Write minimal implementation**

Replace the `resolveRandom` stub in `internal/template/template.go` with:
```go
const lowerAlpha = "abcdefghijklmnopqrstuvwxyz"
const digits = "0123456789"

// resolveRandom handles <random-alpha:N> (N lowercase letters) and
// <random-num:N> (N digits), drawing from ctx.Rand so seeded runs are
// reproducible. N must be a positive integer.
func resolveRandom(prefix, rest string, hasColon bool, ctx Ctx) (string, error) {
	if !hasColon {
		return "", fmt.Errorf("template: <%s> requires a length, e.g. <%s:4>", prefix, prefix)
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n <= 0 {
		return "", fmt.Errorf("template: <%s:%s> length must be a positive integer", prefix, rest)
	}
	alphabet := lowerAlpha
	if prefix == "random-num" {
		alphabet = digits
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[ctx.Rand.IntN(len(alphabet))]
	}
	return string(b), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/template/ -v`
Expected: PASS (entire `template` package, including the earlier tasks).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/template
git add internal/template/template.go internal/template/template_test.go
git commit -m "feat(template): resolve <random-alpha> and <random-num>

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 6: `Config` types, `Defaults`, and `DefaultGlobalPath`

The typed config shape, the built-in defaults, and the XDG-aware global path. No file loading yet.

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

```go
package config

import (
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	d := Defaults()
	if d.Worktree.PathTemplate != "../<repo>.worktrees/<branch>" {
		t.Errorf("path default = %q", d.Worktree.PathTemplate)
	}
	if d.Worktree.DefaultBranchTemplate != "b/from-<parent-branch>-<random-alpha:4>" {
		t.Errorf("branch default = %q", d.Worktree.DefaultBranchTemplate)
	}
}

func TestDefaultGlobalPathXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got := DefaultGlobalPath(); got != filepath.Join("/xdg", "gg", "config.toml") {
		t.Errorf("xdg path = %q", got)
	}
}

func TestDefaultGlobalPathHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/u")
	if got := DefaultGlobalPath(); got != filepath.Join("/home/u", ".config", "gg", "config.toml") {
		t.Errorf("home path = %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run 'TestDefaults|TestDefaultGlobalPath' -v`
Expected: FAIL — `undefined: Defaults` / `undefined: DefaultGlobalPath`.

- [ ] **Step 3: Write minimal implementation**

```go
// Package config loads gigagit's global and per-repo TOML configuration and
// manages machine-local per-repo state (the <seq> counters). The committed
// config is read-only at runtime; only the local state file is written.
package config

import (
	"os"
	"path/filepath"
)

// WorktreeConfig configures worktree creation. TOML keys are snake_case.
type WorktreeConfig struct {
	PathTemplate          string   `toml:"path_template"`
	DefaultBranchTemplate string   `toml:"default_branch_template"`
	BranchTemplates       []string `toml:"branch_templates"`
}

// Config is the merged gigagit configuration.
type Config struct {
	Worktree WorktreeConfig `toml:"worktree"`
}

// Defaults returns the built-in configuration used when no files set a field.
func Defaults() Config {
	return Config{
		Worktree: WorktreeConfig{
			PathTemplate:          "../<repo>.worktrees/<branch>",
			DefaultBranchTemplate: "b/from-<parent-branch>-<random-alpha:4>",
		},
	}
}

// DefaultGlobalPath returns the global config path, honoring $XDG_CONFIG_HOME
// and falling back to ~/.config/gg/config.toml.
func DefaultGlobalPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "gg", "config.toml")
}
```

Note: `os.UserHomeDir` reads `$HOME` on Linux/macOS, so the `t.Setenv("HOME", ...)` test is deterministic on this CI platform.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run 'TestDefaults|TestDefaultGlobalPath' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/config
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): config types, defaults, and XDG global path

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 7: `Load` — field-level overlay of defaults → global → repo

Load merges three layers field-by-field. Each file is decoded into its **own** value; a field set in a higher layer (repo > global > defaults) wins, and a file that sets one field never wipes sibling fields. Missing files are not errors. This explicit Go-side merge avoids depending on the TOML library's nested-decode semantics.

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go` (add `"os"` to the import block):
```go
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMissingFilesYieldDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "nope-global.toml"), filepath.Join(dir, "nope-repo.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Config has a slice field and is not comparable with ==; check the fields
	// that Defaults() populates.
	if cfg.Worktree.PathTemplate != Defaults().Worktree.PathTemplate {
		t.Errorf("missing files should yield default path, got %q", cfg.Worktree.PathTemplate)
	}
}

func TestLoadGlobalOnly(t *testing.T) {
	dir := t.TempDir()
	g := filepath.Join(dir, "global.toml")
	writeFile(t, g, "[worktree]\npath_template = \"G/<branch>\"\n")
	cfg, err := Load(g, filepath.Join(dir, "missing-repo.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Worktree.PathTemplate != "G/<branch>" {
		t.Errorf("global path not applied: %q", cfg.Worktree.PathTemplate)
	}
	// Field the global did not set falls back to default.
	if cfg.Worktree.DefaultBranchTemplate != Defaults().Worktree.DefaultBranchTemplate {
		t.Errorf("unset field should keep default, got %q", cfg.Worktree.DefaultBranchTemplate)
	}
}

func TestLoadRepoWinsFieldLevel(t *testing.T) {
	dir := t.TempDir()
	g := filepath.Join(dir, "global.toml")
	r := filepath.Join(dir, "repo.toml")
	// Global sets BOTH scalar fields; repo overrides only path_template.
	writeFile(t, g, "[worktree]\npath_template = \"G/<branch>\"\ndefault_branch_template = \"g-default\"\n")
	writeFile(t, r, "[worktree]\npath_template = \"R/<branch>\"\n")
	cfg, err := Load(g, r)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Worktree.PathTemplate != "R/<branch>" {
		t.Errorf("repo should win path_template, got %q", cfg.Worktree.PathTemplate)
	}
	// CRITICAL: repo setting one field must NOT wipe the global's other field.
	if cfg.Worktree.DefaultBranchTemplate != "g-default" {
		t.Errorf("global default_branch_template should survive, got %q", cfg.Worktree.DefaultBranchTemplate)
	}
}

func TestLoadRepoBranchTemplates(t *testing.T) {
	dir := t.TempDir()
	r := filepath.Join(dir, "repo.toml")
	writeFile(t, r, "[worktree]\nbranch_templates = [\"issue/<user:id>\", \"b/<parent-branch>\"]\n")
	cfg, err := Load(filepath.Join(dir, "missing.toml"), r)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Worktree.BranchTemplates) != 2 || cfg.Worktree.BranchTemplates[0] != "issue/<user:id>" {
		t.Errorf("branch_templates not loaded: %v", cfg.Worktree.BranchTemplates)
	}
}

func TestLoadMalformedTOMLErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.toml")
	writeFile(t, bad, "this is not = = valid toml [[[")
	if _, err := Load(bad, filepath.Join(dir, "missing.toml")); err == nil {
		t.Fatal("malformed global TOML should error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestLoad -v`
Expected: FAIL — `undefined: Load`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/config/config.go` (add `"github.com/pelletier/go-toml/v2"` to the import block):
```go
// Load builds the effective Config: built-in defaults, overlaid by any field the
// global file sets, then overlaid by any field the repo file sets (repo wins).
// A missing file is skipped (not an error); a present-but-malformed file errors.
func Load(globalPath, repoPath string) (Config, error) {
	cfg := Defaults()

	for _, path := range []string{globalPath, repoPath} {
		layer, ok, err := decodeFile(path)
		if err != nil {
			return Config{}, err
		}
		if ok {
			overlayWorktree(&cfg.Worktree, layer.Worktree)
		}
	}
	return cfg, nil
}

// decodeFile reads and decodes one config file. ok is false (no error) when the
// file does not exist.
func decodeFile(path string) (Config, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, err
	}
	var c Config
	if err := toml.Unmarshal(data, &c); err != nil {
		return Config{}, false, fmt.Errorf("config: parsing %s: %w", path, err)
	}
	return c, true, nil
}

// overlayWorktree copies each non-empty field of src onto dst (field-level
// overlay: an unset field in src leaves dst untouched).
func overlayWorktree(dst *WorktreeConfig, src WorktreeConfig) {
	if src.PathTemplate != "" {
		dst.PathTemplate = src.PathTemplate
	}
	if src.DefaultBranchTemplate != "" {
		dst.DefaultBranchTemplate = src.DefaultBranchTemplate
	}
	if len(src.BranchTemplates) > 0 {
		dst.BranchTemplates = src.BranchTemplates
	}
}
```

Add `"fmt"` to the import block as well.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestLoad -v`
Expected: PASS (all subtests, especially `TestLoadRepoWinsFieldLevel`).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/config
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): field-level overlay of defaults/global/repo

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 8: `<seq>` counters — `PeekSeq` / `BumpSeq` over local state

The 1-based per-repo counters, stored in `<gitDir>/gg/state.toml` (inside `.git/`, so git never tracks them). `PeekSeq` returns the next value (1 when unset); `BumpSeq` consumes it and returns the same number; writes are atomic (temp file + rename) and overwrite an existing state file.

**Files:**
- Create: `internal/config/state.go`
- Test: `internal/config/state_test.go`

- [ ] **Step 1: Write the failing test**

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPeekSeqUnsetIsOne(t *testing.T) {
	gitDir := t.TempDir()
	if n := PeekSeq(gitDir, "issue"); n != 1 {
		t.Fatalf("unset PeekSeq = %d, want 1 (1-based)", n)
	}
}

func TestBumpSeqSequence(t *testing.T) {
	gitDir := t.TempDir()
	// First consume -> 1, matching the prior Peek.
	if p := PeekSeq(gitDir, "issue"); p != 1 {
		t.Fatalf("peek before first bump = %d, want 1", p)
	}
	n, err := BumpSeq(gitDir, "issue")
	if err != nil {
		t.Fatalf("bump: %v", err)
	}
	if n != 1 {
		t.Fatalf("first BumpSeq = %d, want 1", n)
	}
	// Now next peek is 2, and second bump consumes 2.
	if p := PeekSeq(gitDir, "issue"); p != 2 {
		t.Fatalf("peek after first bump = %d, want 2", p)
	}
	n2, _ := BumpSeq(gitDir, "issue")
	if n2 != 2 {
		t.Fatalf("second BumpSeq = %d, want 2", n2)
	}
}

func TestBumpSeqPersistsAndIsIsolatedPerName(t *testing.T) {
	gitDir := t.TempDir()
	if _, err := BumpSeq(gitDir, "issue"); err != nil { // issue -> 1
		t.Fatal(err)
	}
	if _, err := BumpSeq(gitDir, "issue"); err != nil { // issue -> 2
		t.Fatal(err)
	}
	if _, err := BumpSeq(gitDir, "deploy"); err != nil { // deploy -> 1 (separate)
		t.Fatal(err)
	}
	// A fresh read (new process simulation) sees the persisted values.
	if p := PeekSeq(gitDir, "issue"); p != 3 {
		t.Errorf("persisted issue next = %d, want 3", p)
	}
	if p := PeekSeq(gitDir, "deploy"); p != 2 {
		t.Errorf("persisted deploy next = %d, want 2", p)
	}
}

// Regression: writing must overwrite an existing state.toml (os.Rename replace
// semantics), since the dev/CI drive is Windows-mounted and cross-platform is
// a hard requirement.
func TestBumpSeqOverwritesExistingFile(t *testing.T) {
	gitDir := t.TempDir()
	statePath := filepath.Join(gitDir, "gg", "state.toml")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("[seq]\nissue = 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := BumpSeq(gitDir, "issue")
	if err != nil {
		t.Fatalf("bump over existing file: %v", err)
	}
	if n != 6 {
		t.Fatalf("BumpSeq over existing = %d, want 6", n)
	}
	if p := PeekSeq(gitDir, "issue"); p != 7 {
		t.Fatalf("peek after = %d, want 7", p)
	}
}

// The state file lives under .git/gg/, never the committed config.
func TestBumpSeqWritesUnderGitDirOnly(t *testing.T) {
	gitDir := t.TempDir()
	if _, err := BumpSeq(gitDir, "issue"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(gitDir, "gg", "state.toml")); err != nil {
		t.Fatalf("state.toml not written under gitDir/gg: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run Seq -v`
Expected: FAIL — `undefined: PeekSeq` / `undefined: BumpSeq`.

- [ ] **Step 3: Write minimal implementation**

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// seqState is the on-disk shape of <gitDir>/gg/state.toml. Each counter value is
// the last consumed number (absent = 0 = nothing consumed yet).
type seqState struct {
	Seq map[string]int `toml:"seq"`
}

func statePath(gitDir string) string {
	return filepath.Join(gitDir, "gg", "state.toml")
}

func readSeqState(gitDir string) (seqState, error) {
	st := seqState{Seq: map[string]int{}}
	data, err := os.ReadFile(statePath(gitDir))
	if os.IsNotExist(err) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := toml.Unmarshal(data, &st); err != nil {
		return st, fmt.Errorf("config: parsing %s: %w", statePath(gitDir), err)
	}
	if st.Seq == nil {
		st.Seq = map[string]int{}
	}
	return st, nil
}

// PeekSeq returns the next value the named counter will produce (1-based, so 1
// when unset). It does not mutate state.
func PeekSeq(gitDir, name string) int {
	st, err := readSeqState(gitDir)
	if err != nil {
		return 1
	}
	return st.Seq[name] + 1
}

// BumpSeq increments the named counter and persists it atomically, returning the
// newly consumed number (which equals the PeekSeq value taken just before).
func BumpSeq(gitDir, name string) (int, error) {
	st, err := readSeqState(gitDir)
	if err != nil {
		return 0, err
	}
	next := st.Seq[name] + 1
	st.Seq[name] = next
	if err := writeSeqState(gitDir, st); err != nil {
		return 0, err
	}
	return next, nil
}

// writeSeqState marshals st to <gitDir>/gg/state.toml via a temp file + rename so
// a concurrent reader never sees a half-written file. os.Rename replaces an
// existing target on all platforms gigagit supports.
func writeSeqState(gitDir string, st seqState) error {
	dir := filepath.Join(gitDir, "gg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(st)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "state-*.toml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, statePath(gitDir)); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run Seq -v`
Expected: PASS (all subtests, including overwrite and persistence).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/config
git add internal/config/state.go internal/config/state_test.go
git commit -m "feat(config): 1-based per-repo <seq> counters in .git/gg/state.toml

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 9: Full-package verification

A final guard that the two packages build, vet, and test cleanly together, and that formatting is clean.

**Files:** none (verification only)

- [ ] **Step 1: Run the full suite**

Run: `go test ./...`
Expected: PASS for all packages (the new `internal/template` and `internal/config` plus all pre-existing packages).

- [ ] **Step 2: Vet**

Run: `go vet ./...`
Expected: no output.

- [ ] **Step 3: Format check**

Run: `gofmt -l internal`
Expected: no output (empty). If any file is listed, run `gofmt -w internal` and amend the relevant commit.

- [ ] **Step 4: Confirm purity boundary**

Run: `go list -deps ./internal/template | grep -E 'gigagit/gg/internal/(git|config)' || echo CLEAN`
Expected: `CLEAN` — the `template` package must not depend on `git` or `config`.

No commit needed if everything is already committed.

---

## Self-Review Notes (plan author)

- **Spec coverage:** §3 template API (`Resolve`/`UserLabels`/`SeqNames`/`Ctx`) → Tasks 2,4,5; §3 config loader → Tasks 6,7; §4 TOML files + precedence (field-level overlay per the updated spec) → Task 7; §4 local state + `PeekSeq`/`BumpSeq` → Task 8; §5 every token (`parent-branch`, `repo`, `branch` sanitize, `date` mapping, `random-*`, `seq`, `user`, unknown-token error) → Tasks 2–5; §12 testing (table-driven, injected Now/Rand, label extraction/reuse, date mapping, sanitization, unknown-token, config precedence/missing-files-defaults, seq round-trip/padding/persistence/not-in-committed-config) → all tasks. The `git check-ref-format` branch validation in §5 is deliberately **out of A1** (it is git I/O); it belongs to the create flow in A2/A3.
- **Out of scope for A1 (correctly):** the `git worktree add` verb, the `CreateWorktree` engine op, all TUI/CLI/shell-integration — those are A2/A3.
