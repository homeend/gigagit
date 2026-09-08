# Syntax Highlighting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Colour identifiers, keywords, strings, comments and numbers by file type in the TUI side-by-side diff view and the web diff table, on top of the existing add/del backgrounds and word-level emphasis.

**Architecture:** A new pure package `internal/syntax` wraps `alecthomas/chroma/v2`: it detects a lexer from the file name and lexes each whole side once into per-source-line token runs (rune offsets + a small class enum). `domain.Differ` computes those runs alongside the aligned rows and caches them with the diff, keyed by source line number so no row mapping is needed and the shared read-only rows stay untouched. The TUI composes syntax colour (foreground) with the existing emphasis mask (bold/bright) inside `styledRuns`; the web gets the runs as `left_tok`/`right_tok` on `/api/diff` rows and wraps them in CSS classes.

**Tech Stack:** Go 1.26, `github.com/alecthomas/chroma/v2` (pure Go, new dependency), Bubble Tea + lipgloss, vanilla JS/CSS in `internal/web/static`.

**Spec:** `docs/superpowers/specs/2026-09-08-hunk-parity-roadmap.md` §5 (syntax highlighting) and the spike table there.

## Global Constraints

- Develop in a worktree on branch `feat/diff-syntax-highlighting` off `main`; never commit in the main checkout. Prefix every shell command with `cd <worktree-abs-path> &&` (the shell cwd resets between calls).
- `internal/syntax` is a DAG leaf: it may import only the standard library and chroma. `internal/tui`, `internal/cli`, `internal/mcp`, `internal/web` never import `internal/git`; they reach diffs through `domain`.
- Cached `domain.Diff` rows are shared and READ-ONLY. Never add fields to `textdiff.Row`; tokens live in parallel slices on `domain.Diff`.
- Every user-visible TUI string goes through `i18n.T` with a literal key present in all four bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`). This plan adds none; if you add one, add it to all four bundles.
- Every new config key needs a `settingDoc` in `internal/config/template.go` (`TestSettingDocsCoverAllFields` enforces it) and a README `## Configuration` paragraph.
- Tests: `t.Parallel()` on new tests; real `git` in `t.TempDir()` where git is needed; `gofmt` clean. Run `./test.sh unit` before each commit and `./test.sh race` before handing the branch back.
- Commit messages end with:
  ```
  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
  ```
- Deliver a built `gg` binary from the worktree at the end (`go build -o gg ./cmd/gg`), and report its absolute path.

---

## File Structure

| File | Responsibility |
|---|---|
| Create `internal/syntax/syntax.go` | `Class` enum, `Tok`, `Detect(path)`, `Lex(lang, src)`; the only chroma importer |
| Create `internal/syntax/syntax_test.go` | detection, per-line splitting, multi-line state, CRLF, unknown language |
| Modify `internal/domain/differ.go` | `Request.Path`, `DifferOptions.Syntax`, `Diff.OldTok/NewTok`, `MaxSyntaxBytes`, cache-key suffix, `Size` |
| Modify `internal/domain/differ_test.go` | tokens present/absent, cache separation |
| Modify `internal/domain/service.go` | `SetSyntaxHighlighting`, wire `DifferOptions.Syntax` |
| Modify `internal/config/config.go`, `template.go`, `config_test.go` | `[ui] diff_syntax` |
| Modify `internal/tui/load.go`, `internal/tui/source.go` | push the setting into the Service |
| Modify `internal/web/settings.go`, `serve.go`, `reroot.go` | same for the web server (`applyUIPolicies`) |
| Modify `internal/tui/diff_view.go` | `diffView.oldTok/newTok`, `applyDiff`, `wrapSide`, every `domain.Request{…}` gains `Path` |
| Modify `internal/tui/diff_render.go` | `cellSeg.cls`, `sanitizeCell`, `styledRuns` palette, cell renderers take tokens |
| Modify `internal/tui/diff_render_test.go` | colour composition tests |
| Modify `internal/tui/history_view.go`, `bookmark_popup.go`, `bookmark_compare.go`, `shelf_actions.go` | `Path` on their requests |
| Modify `internal/web/diff.go`, `compare.go` | `Path` on requests, `left_tok`/`right_tok` on rows |
| Modify `internal/web/static/files.js`, `style.css` | `renderCell` replaces `markSpans`, `.tk-*` classes |
| Modify `internal/archtest/import_guard_test.go` | register `syntax` as a leaf |
| Modify `CHANGELOG.md`, `README.md`, `CLAUDE.md`, `docs/CLAUDE-details.md` | docs |

---

### Task 0: Worktree and dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Create the feature branch and worktree**

```bash
cd /mnt/t/others/gigagit && gg branch create feat/diff-syntax-highlighting && gg worktree add --branch feat/diff-syntax-highlighting
```

Note the worktree path it prints (expected `/mnt/t/others/gigagit.worktrees/feat-diff-syntax-highlighting`). Every later command uses `cd <that path> &&`. Verify with `git -C <path> branch --show-current`.

- [ ] **Step 2: Add chroma**

```bash
cd <wt> && go get github.com/alecthomas/chroma/v2@v2.27.0 && go mod tidy && go build ./... 
```

Expected: `go.mod` lists `github.com/alecthomas/chroma/v2 v2.27.0` and `github.com/dlclark/regexp2/v2` as indirect; build passes.

- [ ] **Step 3: Commit**

```bash
cd <wt> && git add go.mod go.sum && git commit -q -m "build: add chroma v2 for diff syntax highlighting

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 1: `internal/syntax` package

**Files:**
- Create: `internal/syntax/syntax.go`
- Create: `internal/syntax/syntax_test.go`
- Modify: `internal/archtest/import_guard_test.go:55-58`

**Interfaces:**
- Produces:
  ```go
  package syntax
  type Class uint8
  const (Plain Class = iota; Keyword; Type; Func; Name; String; Number; Comment; Operator; Punct; Attr)
  type Tok struct { Start, End int; Class Class } // rune offsets within one line, [Start,End)
  func Detect(path string) string                 // chroma lexer name, "" when unknown
  func Lex(lang string, src []byte) [][]Tok       // one slice per source line (textdiff line numbering); nil when lang unknown
  func (c Class) String() string                  // "kw","ty","fn","nm","str","num","cmt","op","pn","at"; Plain → ""
  ```

- [ ] **Step 1: Write the failing tests**

```go
package syntax

import (
	"reflect"
	"testing"
)

func TestDetectByFileName(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"main.go": true, "app.tsx": true, "x.py": true, "Dockerfile": true,
		"go.mod": true, "notes.txt": false, "": false, "archive.bin": false,
	}
	for path, want := range cases {
		if got := Detect(path) != ""; got != want {
			t.Errorf("Detect(%q) known=%v, want %v", path, got, want)
		}
	}
}

func TestLexSplitsTokensPerLine(t *testing.T) {
	t.Parallel()
	src := []byte("package x\n/* one\ntwo */\nvar s = \"hi\" // c\n")
	toks := Lex(Detect("a.go"), src)
	if len(toks) != 4 {
		t.Fatalf("lines = %d, want 4 (one per source line)", len(toks))
	}
	// line 2 is entirely inside the block comment
	if want := []Tok{{0, 6, Comment}}; !reflect.DeepEqual(toks[1], want) {
		t.Errorf("line 2 = %+v, want %+v", toks[1], want)
	}
	// line 3: the comment continues, then a plain gap is NOT emitted
	if len(toks[2]) != 1 || toks[2][0].Class != Comment || toks[2][0].Start != 0 || toks[2][0].End != 6 {
		t.Errorf("line 3 = %+v, want one Comment run [0,6)", toks[2])
	}
	// line 4: keyword, name, operator, string, comment; whitespace produces no Tok
	classes := []Class{}
	for _, tk := range toks[3] {
		classes = append(classes, tk.Class)
	}
	if want := []Class{Keyword, Name, Operator, String, Comment}; !reflect.DeepEqual(classes, want) {
		t.Errorf("line 4 classes = %v, want %v", classes, want)
	}
}

func TestLexOffsetsAreRunes(t *testing.T) {
	t.Parallel()
	toks := Lex(Detect("a.go"), []byte("s := \"héllo\" // ü\n"))
	var str Tok
	for _, tk := range toks[0] {
		if tk.Class == String {
			str = tk
		}
	}
	if str.Start != 5 || str.End != 12 {
		t.Errorf("string run = [%d,%d), want [5,12) counted in runes", str.Start, str.End)
	}
}

func TestLexUnknownLanguageIsNil(t *testing.T) {
	t.Parallel()
	if got := Lex("", []byte("x\n")); got != nil {
		t.Errorf("Lex(\"\") = %v, want nil", got)
	}
	if got := Lex("no-such-lexer", []byte("x\n")); got != nil {
		t.Errorf("Lex(unknown) = %v, want nil", got)
	}
}

func TestLexLineCountMatchesTextdiff(t *testing.T) {
	t.Parallel()
	// textdiff.splitLines: trailing \n does not add a line; \r is stripped per line.
	for _, src := range []string{"a\nb\n", "a\nb", "a\r\nb\r\n", "", "\n"} {
		toks := Lex(Detect("a.go"), []byte(src))
		want := 0
		if src != "" {
			s := src
			if s[len(s)-1] == '\n' {
				s = s[:len(s)-1]
			}
			want = 1
			for _, c := range s {
				if c == '\n' {
					want++
				}
			}
		}
		if len(toks) != want {
			t.Errorf("Lex(%q) lines = %d, want %d", src, len(toks), want)
		}
	}
}

func TestClassString(t *testing.T) {
	t.Parallel()
	if Plain.String() != "" || Keyword.String() != "kw" || Comment.String() != "cmt" {
		t.Error("Class.String must give the CSS/style suffix, empty for Plain")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd <wt> && go test ./internal/syntax/`
Expected: FAIL (package does not build: undefined Detect/Lex/Tok).

- [ ] **Step 3: Implement the package**

```go
// Package syntax lexes source text into per-line token runs for the diff
// views. Pure: no git, TUI, or domain imports — only chroma. Offsets are rune
// indices within one line so the renderers can map them to display columns
// exactly like textdiff.Span.
package syntax

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// Class is the coarse token kind the renderers colour. It deliberately
// collapses chroma's ~60 token types to a handful so both frontends carry one
// small palette.
type Class uint8

const (
	Plain Class = iota
	Keyword
	Type
	Func
	Name
	String
	Number
	Comment
	Operator
	Punct
	Attr
)

var classNames = [...]string{"", "kw", "ty", "fn", "nm", "str", "num", "cmt", "op", "pn", "at"}

// String is the short suffix used for CSS classes (web) and style lookup
// (TUI); empty for Plain.
func (c Class) String() string {
	if int(c) < len(classNames) {
		return classNames[c]
	}
	return ""
}

// Tok is one run of a single Class on one line: rune offsets [Start, End).
// Plain runs are never emitted.
type Tok struct {
	Start, End int
	Class      Class
}

// Detect returns the chroma lexer name for path, or "" when no lexer matches
// the file name. Content is never inspected (cheap, deterministic).
func Detect(path string) string {
	if path == "" {
		return ""
	}
	l := lexers.Match(path)
	if l == nil {
		return ""
	}
	return l.Config().Name
}

// MaxLineTokens bounds runs kept per line; a minified one-liner past it keeps
// its first MaxLineTokens runs and the rest render plain.
const MaxLineTokens = 2000

// Lex tokenises the whole of src with the named lexer and splits the result
// per line, numbering lines exactly as textdiff.splitLines does (a trailing
// \n adds no line; \r stays in the line and counts as a rune). nil when lang
// is unknown or src is empty.
func Lex(lang string, src []byte) [][]Tok {
	if lang == "" || len(src) == 0 {
		return nil
	}
	l := lexers.Get(lang)
	if l == nil {
		return nil
	}
	it, err := chroma.Coalesce(l).Tokenise(nil, string(src))
	if err != nil {
		return nil
	}
	// Number of lines textdiff will produce.
	nLines := strings.Count(string(src), "\n")
	if src[len(src)-1] != '\n' {
		nLines++
	}
	out := make([][]Tok, nLines)
	line, col := 0, 0
	for _, t := range it.Tokens() {
		cls := classOf(t.Type)
		val := t.Value
		for len(val) > 0 {
			nl := strings.IndexByte(val, '\n')
			seg := val
			if nl >= 0 {
				seg = val[:nl]
			}
			n := len([]rune(seg))
			if n > 0 && cls != Plain && line < nLines && len(out[line]) < MaxLineTokens {
				out[line] = append(out[line], Tok{Start: col, End: col + n, Class: cls})
			}
			col += n
			if nl < 0 {
				break
			}
			val = val[nl+1:]
			line++
			col = 0
		}
	}
	return out
}

// classOf maps a chroma token type to the coarse Class.
func classOf(t chroma.TokenType) Class {
	switch {
	// Preproc lines (#include, #define) read as directives, not comments;
	// they sit inside the Comment category so they must be tested first.
	case t.InSubCategory(chroma.CommentPreproc):
		return Attr
	case t.InCategory(chroma.Comment):
		return Comment
	case t == chroma.KeywordType:
		return Type
	case t.InCategory(chroma.Keyword):
		return Keyword
	case t.InSubCategory(chroma.LiteralString):
		return String
	case t.InSubCategory(chroma.LiteralNumber):
		return Number
	case t.InCategory(chroma.Operator):
		return Operator
	case t.InCategory(chroma.Punctuation):
		return Punct
	case t == chroma.NameClass || t == chroma.NameBuiltin || t == chroma.NameException || t == chroma.NameNamespace:
		return Type
	case t == chroma.NameFunction || t == chroma.NameFunctionMagic:
		return Func
	case t == chroma.NameDecorator || t == chroma.NameAttribute || t == chroma.NameTag:
		return Attr
	case t.InCategory(chroma.Name):
		return Name
	}
	return Plain
}
```

Notes for the implementer: in chroma v2 `TokenType.InCategory(other)` compares `t/1000` (Keyword 1000s, Name 2000s, Literal 3000s, Operator 4000s, Punctuation 5000s, Comment 6000s, Generic 7000s, Text 8000s) and `InSubCategory(other)` compares `t/100` (`LiteralString` 3100s, `LiteralNumber` 3200s, `CommentPreproc` 6100s). Verify the constants in `$(go env GOMODCACHE)/github.com/alecthomas/chroma/v2@v2.27.0/types.go` before relying on the digit layout; if a case misfires, switch it to an explicit `t.Category() == …` / `t.SubCategory() == …` comparison.

- [ ] **Step 4: Register the leaf in archtest**

In `internal/archtest/import_guard_test.go` add to the `cases` map, next to `"textdiff"`:

```go
		"syntax":      {"git", "engine", "domain", "tui", "cli", "mcp", "web", "app"},
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd <wt> && go test ./internal/syntax/ ./internal/archtest/`
Expected: PASS. If `TestLexSplitsTokensPerLine` disagrees on a class (chroma's Go lexer marks `s` as `NameOther`, `=` as `Punctuation` in some versions), adjust `classOf`, not the test's intent: `var`→Keyword, `s`→Name, `=`→Operator or Punct (accept either by changing the expected slice to whatever `classOf` maps `chroma.Punctuation`/`Operator` to, and document the choice in the test).

- [ ] **Step 6: Commit**

```bash
cd <wt> && gofmt -l internal/syntax && git add internal/syntax internal/archtest && git commit -q -m "feat(syntax): chroma-backed per-line token runs

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 2: Tokens on `domain.Diff`

**Files:**
- Modify: `internal/domain/differ.go`
- Modify: `internal/domain/differ_test.go`
- Modify: `internal/domain/service.go:150-157` (Differ) and the `Service` struct fields near `showEOLOnly`

**Interfaces:**
- Consumes: `syntax.Detect`, `syntax.Lex`, `syntax.Tok`
- Produces:
  ```go
  type Request struct { Key string; Path string; Old, New ByteSource } // Path: repo-relative path used for lexer detection ("" = no highlighting)
  type Diff struct { Result textdiff.Result; Binary, TooLarge bool; OldTok, NewTok [][]syntax.Tok } // indexed by source line-1; nil when highlighting is off/unknown/too big
  type DifferOptions struct { Enhanced, Cached bool; Syntax func() bool } // nil Syntax = never highlight
  const MaxSyntaxBytes = 1 << 20
  func (s *Service) SetSyntaxHighlighting(on bool) *Service
  ```

- [ ] **Step 1: Write the failing tests** (append to `differ_test.go`)

```go
func on() bool  { return true }
func off() bool { return false }

func TestPlainDifferLexesBothSidesByPath(t *testing.T) {
	t.Parallel()
	d := NewDiffer(DifferOptions{Enhanced: true, Syntax: on}, nil)
	out, err := d.Diff(context.Background(), Request{
		Path: "a.go",
		Old:  src([]byte("package a\n// c\n")),
		New:  src([]byte("package a\n// d\nvar x = 1\n")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.OldTok) != 2 || len(out.NewTok) != 3 {
		t.Fatalf("OldTok=%d NewTok=%d, want 2 and 3 (one per source line)", len(out.OldTok), len(out.NewTok))
	}
	if len(out.NewTok[1]) == 0 || out.NewTok[1][0].Class != syntax.Comment {
		t.Errorf("new line 2 should start with a Comment run, got %+v", out.NewTok[1])
	}
}

func TestPlainDifferNoTokensWhenOffUnknownOrLarge(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		opts DifferOptions
		req  Request
	}{
		{"syntax off", DifferOptions{Syntax: off}, Request{Path: "a.go", Old: src([]byte("a\n")), New: src([]byte("b\n"))}},
		{"nil Syntax", DifferOptions{}, Request{Path: "a.go", Old: src([]byte("a\n")), New: src([]byte("b\n"))}},
		{"no path", DifferOptions{Syntax: on}, Request{Old: src([]byte("a\n")), New: src([]byte("b\n"))}},
		{"unknown ext", DifferOptions{Syntax: on}, Request{Path: "a.zzz", Old: src([]byte("a\n")), New: src([]byte("b\n"))}},
		{"too big", DifferOptions{Syntax: on}, Request{Path: "a.go", Old: src(make([]byte, MaxSyntaxBytes+1)), New: src([]byte("b\n"))}},
	}
	for _, c := range cases {
		out, err := NewDiffer(c.opts, nil).Diff(context.Background(), c.req)
		if err != nil {
			t.Fatal(c.name, err)
		}
		if out.OldTok != nil || out.NewTok != nil {
			t.Errorf("%s: expected no tokens, got old=%v new=%v", c.name, out.OldTok, out.NewTok)
		}
	}
}

func TestCachedDifferSeparatesSyntaxOnOff(t *testing.T) {
	t.Parallel()
	c := cache.New(0, 0)
	flag := true
	d := NewDiffer(DifferOptions{Enhanced: true, Cached: true, Syntax: func() bool { return flag }}, c)
	req := Request{Key: "k", Path: "a.go", Old: src([]byte("package a\n")), New: src([]byte("package b\n"))}
	withTok, _ := d.Diff(context.Background(), req)
	flag = false
	without, _ := d.Diff(context.Background(), req)
	if withTok.NewTok == nil {
		t.Fatal("first call with syntax on should carry tokens")
	}
	if without.NewTok != nil {
		t.Fatal("turning syntax off must not serve the tokenised cache entry")
	}
}
```

Add `"github.com/homeend/gigagit/internal/syntax"` to the test imports. Check `cache.New`'s real signature in `internal/cache` (the spike memory says a factory exists: `cache.NewFactory(0,0).Cache("diff")` is the Service's way; use whichever constructor `differ_test.go` already uses for `cachedDiffer` tests, grep `cache.` in that file).

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd <wt> && go test ./internal/domain/ -run 'Differ'`
Expected: FAIL (unknown fields `Path`, `Syntax`, `OldTok`).

- [ ] **Step 3: Implement**

In `differ.go`:

```go
import "github.com/homeend/gigagit/internal/syntax"

// MaxSyntaxBytes caps each side handed to the lexer (~1 s of chroma on a
// 1 MB file). Larger sides diff normally but render uncoloured.
const MaxSyntaxBytes = 1 << 20

type Request struct {
	Key      string
	Path     string // repo-relative path; selects the lexer ("" = no highlighting)
	Old, New ByteSource
}

type Diff struct {
	Result   textdiff.Result
	Binary   bool
	TooLarge bool
	// OldTok/NewTok hold syntax runs per SOURCE line (index = line number − 1,
	// the Row.LeftNo/RightNo numbering), so no row mapping is needed and the
	// shared rows stay untouched. nil when highlighting is off, the language
	// is unknown, or the side exceeds MaxSyntaxBytes.
	OldTok, NewTok [][]syntax.Tok
}

func (d Diff) Size() int {
	n := 0
	for _, r := range d.Result.Rows {
		n += len(r.Left) + len(r.Right) + 48
	}
	for _, side := range [][][]syntax.Tok{d.OldTok, d.NewTok} {
		for _, line := range side {
			n += 24 + 12*len(line)
		}
	}
	return n
}

type DifferOptions struct {
	Enhanced bool
	Cached   bool
	Syntax   func() bool // consulted per call; nil = never highlight
}

func NewDiffer(opts DifferOptions, c cache.Cache) Differ {
	var d Differ = plainDiffer{enhanced: opts.Enhanced, syntax: opts.Syntax}
	if opts.Cached {
		d = cachedDiffer{inner: d, cache: c, enhanced: opts.Enhanced, syntax: opts.Syntax}
	}
	return d
}

type plainDiffer struct {
	enhanced bool
	syntax   func() bool
}

func (d plainDiffer) Diff(ctx context.Context, req Request) (Diff, error) {
	old, err := readSource(ctx, req.Old)
	if err != nil {
		return Diff{}, err
	}
	newB, err := readSource(ctx, req.New)
	if err != nil {
		return Diff{}, err
	}
	if len(old) > MaxDiffBytes || len(newB) > MaxDiffBytes {
		return Diff{TooLarge: true}, nil
	}
	if textdiff.IsBinary(old) || textdiff.IsBinary(newB) {
		return Diff{Binary: true}, nil
	}
	out := Diff{Result: textdiff.Compare(old, newB, textdiff.Options{Enhanced: d.enhanced})}
	if d.syntax != nil && d.syntax() {
		if lang := syntax.Detect(req.Path); lang != "" {
			if len(old) <= MaxSyntaxBytes {
				out.OldTok = syntax.Lex(lang, old)
			}
			if len(newB) <= MaxSyntaxBytes {
				out.NewTok = syntax.Lex(lang, newB)
			}
		}
	}
	return out, nil
}
```

and in `cachedDiffer.Diff` build the key as:

```go
	qkey := "p:"
	if d.enhanced {
		qkey = "e:"
	}
	if d.syntax != nil && d.syntax() {
		qkey += "s:"
	}
	qkey += req.Key
```

In `service.go`: add a field `syntaxOff atomic.Bool` beside `showEOLOnly` (zero value = highlighting ON, so callers that never configure it still get colours), the setter, and pass the option:

```go
// SetSyntaxHighlighting turns diff syntax colouring on/off for every later
// Differ call ([ui] diff_syntax). Default on; the differ reads it per call so
// a settings change needs no rebuild. Cache entries are keyed by the flag.
func (s *Service) SetSyntaxHighlighting(on bool) *Service {
	s.syntaxOff.Store(!on)
	return s
}

func (s *Service) syntaxOn() bool { return !s.syntaxOff.Load() }
```

and in `Differ()`: `NewDiffer(DifferOptions{Enhanced: true, Cached: true, Syntax: s.syntaxOn}, s.factory.Cache("diff"))`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd <wt> && go test ./internal/domain/ ./internal/archtest/`
Expected: PASS. If `TestCachedDifferSeparatesSyntaxOnOff` fails because both calls return tokens, the cache key suffix is not applied — check the order of `qkey` assembly.

- [ ] **Step 5: Commit**

```bash
cd <wt> && gofmt -l internal/domain && git add internal/domain && git commit -q -m "feat(domain): differ lexes both sides into per-line syntax runs

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 3: `[ui] diff_syntax` config key, applied by TUI and web

**Files:**
- Modify: `internal/config/config.go` (UIConfig, Defaults, overlayUI)
- Modify: `internal/config/template.go` (settingDocs)
- Modify: `internal/config/config_test.go`
- Modify: `internal/tui/load.go:89`, `internal/tui/source.go:439`
- Modify: `internal/web/settings.go:307` (`applyVersionsPolicy`), `internal/web/serve.go:41`, `internal/web/reroot.go:186`
- Modify: `README.md` `## Configuration` (after the `show_eol_only_changes` paragraph, ~line 332)

**Interfaces:**
- Produces: `UIConfig.DiffSyntax string` with values `"auto"` (default) and `"off"`; helper `func (c UIConfig) SyntaxOn() bool`.

- [ ] **Step 1: Write the failing config test** (in `config_test.go`, after `TestUIWheelStepLayers`)

```go
func TestUIDiffSyntaxLayers(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.toml")

	cfg, err := Load(missing, missing)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.DiffSyntax != "auto" || !cfg.UI.SyntaxOn() {
		t.Errorf("default diff_syntax = %q (on=%v), want auto/on", cfg.UI.DiffSyntax, cfg.UI.SyntaxOn())
	}

	g := filepath.Join(dir, "global.toml")
	writeFile(t, g, "[ui]\ndiff_syntax = \"off\"\n")
	cfg, err = Load(g, missing)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.SyntaxOn() {
		t.Error("global off must turn highlighting off")
	}

	r := filepath.Join(dir, "repo.toml")
	writeFile(t, r, "[ui]\ndiff_syntax = \"auto\"\n")
	cfg, err = Load(g, r)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.UI.SyntaxOn() {
		t.Error("repo auto must win over global off")
	}

	// An empty repo value is unset and keeps the global.
	writeFile(t, r, "[ui]\ndiff_syntax = \"\"\n")
	cfg, _ = Load(g, r)
	if cfg.UI.SyntaxOn() {
		t.Error("empty repo value must not reset the global off")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd <wt> && go test ./internal/config/ -run 'DiffSyntax|SettingDocs'`
Expected: FAIL (no field `DiffSyntax`).

- [ ] **Step 3: Implement the key**

In `UIConfig` (after `CommitSort`):

```go
	// DiffSyntax selects syntax colouring in the diff views:
	//   "auto" — colour by file name when a lexer is known. THE DEFAULT.
	//   "off"  — plain text (only add/del backgrounds and word emphasis).
	// Empty = unset (zero-is-unset overlay rule); resolved to the default.
	DiffSyntax string `toml:"diff_syntax"`
```

Add `func (c UIConfig) SyntaxOn() bool { return c.DiffSyntax != "off" }` below the struct. In `Defaults()` set `DiffSyntax: "auto"` in the UI block. In `overlayUI`:

```go
	if src.DiffSyntax != "" {
		dst.DiffSyntax = src.DiffSyntax
	}
```

In `template.go` `settingDocs`, after the `commit_sort` row (or the last `ui` row):

```go
	{"ui", "diff_syntax", "auto", "syntax colouring in diff views: auto (by file name) or off"},
```

- [ ] **Step 4: Apply it in the TUI and the web**

`internal/tui/load.go` right after `svc.SetShowEOLOnlyChanges(cfg.UI.ShowEOLOnlyChanges)`:

```go
		svc.SetSyntaxHighlighting(cfg.UI.SyntaxOn())
```

Same line in `internal/tui/source.go` after its `SetShowEOLOnlyChanges` call (settings reload path).

`internal/web/settings.go`: extend `applyVersionsPolicy` so the one config load pushes both policies, and rename it to say so:

```go
// applyUIPolicies loads the effective config once and pushes the Service-side
// policies it carries: branch-version snapshots and diff syntax colouring.
func applyUIPolicies(ctx context.Context, svc *domain.Service, activeRepoPath string) {
	cfg, err := config.Load(config.DefaultGlobalPath(), activeRepoPath)
	if err != nil {
		return
	}
	svc.SetVersionsPolicy(engine.VersionsPolicy{Enabled: !cfg.Versions.Disabled, MaxAgeDays: cfg.Versions.MaxAgeDays})
	svc.SetSyntaxHighlighting(cfg.UI.SyntaxOn())
}
```

Rename the three call sites (`serve.go:41`, `reroot.go:186`, `settings.go:235`) with `grep -rn applyVersionsPolicy internal/web` and `sed -i 's/applyVersionsPolicy/applyUIPolicies/g' internal/web/*.go`.

- [ ] **Step 5: README**

After the `show_eol_only_changes` paragraph in `## Configuration` add:

```markdown
`[ui] diff_syntax` (default `"auto"`) colours code in the diff views by file
type — keywords, types, strings, numbers and comments each get a colour, on
top of the add/del backgrounds and the word-level emphasis. The language is
picked from the file name (about 300 lexers, via chroma); a file with no
known lexer, or a side larger than 1 MB, renders plain. Set `"off"` to
disable everywhere (TUI and `gg web`).
```

- [ ] **Step 6: Run tests**

Run: `cd <wt> && go test ./internal/config/ ./internal/web/ ./internal/tui/ 2>&1 | tail -5`
Expected: PASS (`TestSettingDocsCoverAllFields` now sees the doc).

- [ ] **Step 7: Commit**

```bash
cd <wt> && gofmt -l internal && git add internal/config internal/tui/load.go internal/tui/source.go internal/web README.md && git commit -q -m "feat(config): [ui] diff_syntax toggles diff syntax colouring

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 4: TUI rendering

**Files:**
- Modify: `internal/tui/diff_view.go` (struct `diffView`, `wrapSide`, `applyDiff`, all four `domain.Request{…}` literals)
- Modify: `internal/tui/diff_render.go` (`cellSeg`, `wrapCells`, `sanitizeSpans`→`sanitizeCell`, `styledRuns`, `segCell`, `scrollCell`, `diffCell`, `hotEmphBody`, `diffPaneLines`)
- Modify: `internal/tui/diff_render_test.go`
- Modify: `internal/tui/history_view.go:97,117`, `bookmark_popup.go:537,659`, `bookmark_compare.go:78,106`, `shelf_actions.go:143,186` (add `Path:`)

**Interfaces:**
- Consumes: `domain.Diff.OldTok/NewTok`, `syntax.Tok`, `syntax.Class`
- Produces (package-private):
  ```go
  type cellSeg struct { disp []rune; emph []bool; cls []syntax.Class }
  func sanitizeCell(s string, spans []textdiff.Span, toks []syntax.Tok) (disp []rune, emph []bool, cls []syntax.Class)
  func styledRuns(disp []rune, emph []bool, cls []syntax.Class, base lipgloss.Style) string
  func tokAt(side [][]syntax.Tok, no int) []syntax.Tok   // nil when no==0 or out of range
  var syntaxStyles [11]lipgloss.Style                     // indexed by syntax.Class; Plain = zero style
  ```

- [ ] **Step 1: Write the failing tests** (append to `diff_render_test.go`)

```go
func TestSanitizeCellCarriesClassesThroughTabs(t *testing.T) {
	t.Parallel()
	// "\tif x" — the keyword `if` sits at raw runes [1,3); the tab expands to 4 cols.
	disp, emph, cls := sanitizeCell("\tif x", nil, []syntax.Tok{{Start: 1, End: 3, Class: syntax.Keyword}})
	if string(disp) != "    if x" {
		t.Fatalf("disp = %q", string(disp))
	}
	if len(cls) != len(disp) || len(emph) != len(disp) {
		t.Fatalf("masks must parallel disp: cls=%d emph=%d disp=%d", len(cls), len(emph), len(disp))
	}
	want := []syntax.Class{0, 0, 0, 0, syntax.Keyword, syntax.Keyword, 0, 0}
	for i := range want {
		if cls[i] != want[i] {
			t.Errorf("cls[%d] = %v, want %v (%v)", i, cls[i], want[i], cls)
			break
		}
	}
}

func TestStyledRunsColoursKeywordAndKeepsEmphasis(t *testing.T) {
	t.Parallel()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	disp := []rune("if x")
	cls := []syntax.Class{syntax.Keyword, syntax.Keyword, 0, 0}
	// no emphasis: keyword run wears the keyword foreground
	out := styledRuns(disp, []bool{false, false, false, false}, cls, lipgloss.NewStyle())
	if !strings.Contains(out, "38;5;"+syntaxColor(syntax.Keyword)) {
		t.Errorf("keyword run should carry its 256-colour foreground: %q", out)
	}
	// emphasis wins over syntax colour (bold + 231), so the diff stays legible
	out = styledRuns(disp, []bool{true, true, false, false}, cls, lipgloss.NewStyle())
	if !strings.Contains(out, "38;5;231") || strings.Contains(out, "38;5;"+syntaxColor(syntax.Keyword)) {
		t.Errorf("emphasised run must use diffEmph, not the syntax colour: %q", out)
	}
	if ansi.Strip(out) != "if x" {
		t.Errorf("text must be unchanged: %q", ansi.Strip(out))
	}
}

func TestDiffPaneLinesUseTokensBySourceLine(t *testing.T) {
	t.Parallel()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	v := &diffView{title: "a.go", full: []textdiff.Row{
		{Kind: textdiff.Same, Left: "package a", Right: "package a", LeftNo: 1, RightNo: 1},
		{Kind: textdiff.Add, Right: "var x = 1", RightNo: 2},
	}}
	v.oldTok = [][]syntax.Tok{{{0, 7, syntax.Keyword}}}
	v.newTok = [][]syntax.Tok{{{0, 7, syntax.Keyword}}, {{0, 3, syntax.Keyword}, {8, 9, syntax.Number}}}
	v.rebuild()
	m := renderModelWithDiff(v)
	lines := m.diffPaneLines(v, 100, 5)
	if len(lines) != 2 {
		t.Fatalf("lines = %d", len(lines))
	}
	if !strings.Contains(lines[1], "38;5;"+syntaxColor(syntax.Number)) {
		t.Errorf("row 2 right cell (new line 2) should colour the number: %q", lines[1])
	}
	if strings.Contains(lines[1], "38;5;"+syntaxColor(syntax.Keyword)+"m"+"·") {
		t.Errorf("the gap side must stay a plain filler: %q", lines[1])
	}
}
```

`syntaxColor(c)` is a test-visible helper returning the 256-colour number string used by `syntaxStyles[c]` (define it in `diff_render.go` next to the palette so tests and palette cannot drift).

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd <wt> && go test ./internal/tui/ -run 'SanitizeCell|StyledRunsColours|UseTokensBySourceLine'`
Expected: FAIL (undefined `sanitizeCell`, `oldTok`, `syntaxColor`).

- [ ] **Step 3: Implement the palette and masks in `diff_render.go`**

Replace the `diffEmph` block's neighbourhood with:

```go
// syntaxPalette is the 256-colour foreground per syntax.Class (index = Class).
// Chosen for gg's dark add/del backgrounds (22/52): mid-brightness hues that
// stay readable over both. Plain (index 0) is "" = inherit.
var syntaxPalette = [...]string{"", "141", "79", "222", "", "150", "215", "245", "252", "250", "180"}

// syntaxColor is the palette entry for c ("" for Plain / unknown).
func syntaxColor(c syntax.Class) string {
	if int(c) < len(syntaxPalette) {
		return syntaxPalette[c]
	}
	return ""
}

// syntaxStyle returns base with c's foreground applied ("" leaves base alone).
func syntaxStyle(base lipgloss.Style, c syntax.Class) lipgloss.Style {
	if col := syntaxColor(c); col != "" {
		return base.Foreground(lipgloss.Color(col))
	}
	return base
}
```

`Name` (index 4) deliberately has no colour: identifiers are most of any line, and colouring them makes the emphasis harder to see.

Change `cellSeg` to carry `cls []syntax.Class`, and `wrapCells(disp, emph, cls, tw)` to slice it alongside (`cellSeg{disp: disp[start:brk], emph: emph[start:brk], cls: cls[start:brk]}`; the empty-input case returns `[]cellSeg{{}}` unchanged).

Rename `sanitizeSpans` to `sanitizeCell` and add the class mask:

```go
func sanitizeCell(s string, spans []textdiff.Span, toks []syntax.Tok) (disp []rune, emph []bool, cls []syntax.Class) {
	s = strings.TrimSuffix(s, "\r")
	runes := []rune(s)
	cover := coverMask(len(runes), spans)
	classes := classMask(len(runes), toks)
	col := 0
	for raw, r := range runes {
		on, c := cover[raw], classes[raw]
		switch {
		case r == '\t':
			n := 4 - col%4
			for k := 0; k < n; k++ {
				disp = append(disp, ' ')
				emph = append(emph, on)
				cls = append(cls, c)
			}
			col += n
		case r < 0x20 || r == 0x7f:
			disp = append(disp, '·')
			emph = append(emph, on)
			cls = append(cls, c)
			col++
		default:
			disp = append(disp, r)
			emph = append(emph, on)
			cls = append(cls, c)
			col++
		}
	}
	return disp, emph, cls
}

// classMask marks raw rune indices [0,n) with their token class (ends clamped;
// later runs win on overlap, which never happens for coalesced lexer output).
func classMask(n int, toks []syntax.Tok) []syntax.Class {
	mask := make([]syntax.Class, n)
	for _, tk := range toks {
		lo, hi := tk.Start, tk.End
		if lo < 0 {
			lo = 0
		}
		if hi > n {
			hi = n
		}
		for i := lo; i < hi; i++ {
			mask[i] = tk.Class
		}
	}
	return mask
}
```

`styledRuns` groups by `(emph, cls)`; emphasis wins:

```go
func styledRuns(disp []rune, emph []bool, cls []syntax.Class, base lipgloss.Style) string {
	var b strings.Builder
	for i := 0; i < len(disp); {
		j := i + 1
		for j < len(disp) && emph[j] == emph[i] && cls[j] == cls[i] {
			j++
		}
		seg := string(disp[i:j])
		switch {
		case emph[i]:
			b.WriteString(base.Inherit(diffEmph).Render(seg))
		default:
			b.WriteString(syntaxStyle(base, cls[i]).Render(seg))
		}
		i = j
	}
	return b.String()
}
```

Thread tokens through the cell renderers (each gains a `toks []syntax.Tok` parameter placed right after `spans`; `segCell` reads them from `seg.cls`):

- `segCell(no, seg, gut, width, gap, hot, hotStyle)` — body becomes `styledRuns(seg.disp, seg.emph, seg.cls, base)`.
- `scrollCell(no, text, spans, toks, hOffset, gut, width, gap, hot, hotStyle)` — `disp, emph, cls := sanitizeCell(text, spans, toks)`; the window loop also collects `wcls`; delegate to `diffCell(no, text, gut, width, false, hot, hotStyle, spans, toks)` at rest.
- `diffCell(no, text, gut, width, gap, hot, hotStyle, spans, toks)` — when `len(spans) > 0 || len(toks) > 0` go through `hotEmphBody(text, spans, toks, tw, base)` where `base` is `hotStyle` when hot else `lipgloss.NewStyle()`; otherwise the old plain path (byte-identical for untokenised rows).
- `hotEmphBody(text, spans, toks, tw, base)` — `disp, emph, cls := sanitizeCell(text, spans, toks)`; both branches call `styledRuns(…, cls, base)`; the ellipsis stays `base.Render("…")`.

In `diffPaneLines` fetch the runs once per row:

```go
		lt, rt := tokAt(v.oldTok, r.LeftNo), tokAt(v.newTok, r.RightNo)
```

and pass `lt`/`rt` to the left/right calls in the `longTruncate` and `longScroll` cases (the `longWrap` case already has them inside `dr.left/right` via `wrapSide`).

- [ ] **Step 4: Implement the view side in `diff_view.go`**

Add to `diffView` after `fullBlocks`:

```go
	oldTok     [][]syntax.Tok // syntax runs per OLD source line (index = LeftNo-1); nil = plain
	newTok     [][]syntax.Tok // syntax runs per NEW source line (index = RightNo-1); nil = plain
```

`applyDiff` copies them in the default branch: `v.oldTok, v.newTok = out.OldTok, out.NewTok`.

Add the lookup helper (in `diff_render.go`):

```go
// tokAt returns the syntax runs of source line no (1-based) or nil for a gap
// (no == 0) or a line the lexer did not cover.
func tokAt(side [][]syntax.Tok, no int) []syntax.Tok {
	if no <= 0 || no > len(side) {
		return nil
	}
	return side[no-1]
}
```

`wrapSide` gains `toks []syntax.Tok`:

```go
func wrapSide(text string, spans []textdiff.Span, toks []syntax.Tok, kind textdiff.Kind, right bool, tw int) []cellSeg {
	if (!right && kind == textdiff.Add) || (right && kind == textdiff.Del) {
		return nil
	}
	disp, emph, cls := sanitizeCell(text, spans, toks)
	return wrapCells(disp, emph, cls, tw)
}
```

and its caller in `relayout` passes `tokAt(v.oldTok, r.LeftNo)` / `tokAt(v.newTok, r.RightNo)`.

Add `Path:` to every `domain.Request{…}` literal in the TUI. The value is the file path the diff is for:

| File:line | `Path:` value |
|---|---|
| `diff_view.go:426` and `:468` | `f.Path` |
| `diff_view.go:524` | `line.path` |
| `diff_view.go:578` | `line.path` |
| `history_view.go:97`, `:117` | the path variable the surrounding function diffs (read the ~20 lines above each; it is the history view's file path field) |
| `bookmark_popup.go:537`, `:659` | the bookmark's `Path` |
| `bookmark_compare.go:78`, `:106` | the compared file path |
| `shelf_actions.go:143`, `:186` | the shelf entry's `Path` |

Use `grep -n 'domain.Request{' internal/tui/*.go` to confirm the list is complete (12 literals as of this plan).

- [ ] **Step 5: Run the TUI tests**

Run: `cd <wt> && go build ./... && go test ./internal/tui/ 2>&1 | tail -15`
Expected: PASS, including the pre-existing `TestRenderDiffViewPanes` (untokenised rows are byte-identical to before) and `TestSanitizeSpansMapsThroughTabExpansion` (rename its call to `sanitizeCell(..., nil)` and drop the third return with `_`).

- [ ] **Step 6: Headless look**

```bash
cd <wt> && go build -o gg ./cmd/gg && ./tui-capture.sh --help | head -5
```

Follow the `driving-tui-headless` skill: open a diff of a `.go` file in a fixture repo and confirm the snapshot text is unchanged (the capture strips colours, so this only guards layout regressions).

- [ ] **Step 7: Commit**

```bash
cd <wt> && gofmt -l internal/tui && git add internal/tui && git commit -q -m "feat(tui): colour diff cells by syntax class under the emphasis mask

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 5: Web rendering

**Files:**
- Modify: `internal/web/diff.go` (`diffRow`, `writeDiffJSON`, the three `domain.Request{…}` at :92, :157, :209)
- Modify: `internal/web/compare.go:332` (`Path:` on the request; its writer shares `writeDiffJSON` or mirrors it — check `compare.go:289` comment and apply the same two fields there)
- Modify: `internal/web/static/files.js` (`markSpans` → `renderCell`, five call sites at 588/605/609/617/619)
- Modify: `internal/web/static/style.css`
- Test: `internal/web/diff_test.go` (create if absent; follow the server fixture used by `compare_test.go:97 TestCompareRevDiff`)

**Interfaces:**
- Produces JSON: each row may carry `left_tok` / `right_tok`: arrays of `[start, end, class]` with `class` being the `syntax.Class.String()` suffix (`"kw"`, `"str"`, …); omitted when empty.

- [ ] **Step 1: Write the failing Go test**

```go
func TestDiffJSONCarriesSyntaxRuns(t *testing.T) {
	t.Parallel()
	srv, repo := newTestServer(t) // reuse whatever helper TestCompareRevDiff uses to get a server over a real repo
	sha := commitFile(t, repo, "a.go", "package a\nvar x = 1\n") // helper that writes+commits and returns the sha; copy the pattern from compare_test.go
	rr := getJSON(t, srv, "/api/diff?sha="+sha+"&path=a.go&status=A")
	rows := rr["rows"].([]any)
	second := rows[1].(map[string]any)
	toks, ok := second["right_tok"].([]any)
	if !ok || len(toks) == 0 {
		t.Fatalf("row 2 should carry right_tok, got %v", second)
	}
	first := toks[0].([]any)
	if first[2] != "kw" || first[0].(float64) != 0 || first[1].(float64) != 3 {
		t.Errorf("first run should be [0,3,\"kw\"] for `var`, got %v", first)
	}
	if _, has := second["left_tok"]; has {
		t.Errorf("an all-add row has no old line, so no left_tok: %v", second)
	}
}
```

Adapt the helper names to the ones that exist in `compare_test.go` (read its first 60 lines); do not invent new fixtures if one is there.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd <wt> && go test ./internal/web/ -run DiffJSONCarriesSyntaxRuns`
Expected: FAIL (no `right_tok` key).

- [ ] **Step 3: Implement the JSON**

```go
type diffRow struct {
	Kind       string      `json:"kind"`
	Left       string      `json:"left"`
	Right      string      `json:"right"`
	LeftNo     int         `json:"left_no"`
	RightNo    int         `json:"right_no"`
	LeftSpans  [][2]int    `json:"left_spans,omitempty"`
	RightSpans [][2]int    `json:"right_spans,omitempty"`
	LeftTok    []tokTriple `json:"left_tok,omitempty"`
	RightTok   []tokTriple `json:"right_tok,omitempty"`
	Hunk       *int        `json:"hunk,omitempty"`
}

// tokTriple is one syntax run on the wire: [start, end, class-suffix].
type tokTriple [3]any

func tokTriples(side [][]syntax.Tok, no int) []tokTriple {
	if no <= 0 || no > len(side) || len(side[no-1]) == 0 {
		return nil
	}
	out := make([]tokTriple, len(side[no-1]))
	for i, tk := range side[no-1] {
		out[i] = tokTriple{tk.Start, tk.End, tk.Class.String()}
	}
	return out
}
```

In `writeDiffJSON` set `LeftTok: tokTriples(d.OldTok, row.LeftNo), RightTok: tokTriples(d.NewTok, row.RightNo)`. Add `Path: path` (commit branch), `Path: path` in `handleRevDiff` (:157) and `handleWorktreeDiff` (:209), and the compared path at `compare.go:332`. Import `internal/syntax` in `diff.go`.

- [ ] **Step 4: Implement the client**

In `files.js` replace `markSpans` with:

```js
// renderCell escapes one cell's text and wraps (a) word-diff spans in
// <mark class="l|r"> and (b) syntax runs in <span class="tk-…">. Both are
// rune ranges over the raw line; a mark wins over a syntax class inside it so
// the emphasis stays legible, matching the TUI.
function renderCell(text, spans, toks, side) {
  if ((!spans || !spans.length) && (!toks || !toks.length)) return esc(text);
  const rs = runes(text);
  const emph = new Array(rs.length).fill(false);
  for (const [a, b] of spans || []) for (let i = a; i < b && i < rs.length; i++) emph[i] = true;
  const cls = new Array(rs.length).fill("");
  for (const [a, b, c] of toks || []) for (let i = a; i < b && i < rs.length; i++) cls[i] = c;
  let out = "";
  for (let i = 0; i < rs.length; ) {
    let j = i + 1;
    while (j < rs.length && emph[j] === emph[i] && cls[j] === cls[i]) j++;
    const seg = esc(rs.slice(i, j).join(""));
    if (emph[i]) out += `<mark class="${side}">${seg}</mark>`;
    else if (cls[i]) out += `<span class="tk-${cls[i]}">${seg}</span>`;
    else out += seg;
    i = j;
  }
  return out;
}
```

and update the five call sites: `renderCell(text, spans, toks, side)` where `toks` is `pureAdd ? r.right_tok : r.left_tok` in the single-column branch, `r.left_tok` / `r.right_tok` in the others. `grep -n 'markSpans' internal/web/static/*.js` must return nothing afterwards.

In `style.css` after the `tr.change td.side.r` rule:

```css
/* syntax classes (see internal/syntax Class.String); same hues as the TUI palette */
table.diff .tk-kw  { color: #b48bff; }
table.diff .tk-ty  { color: #5fd7af; }
table.diff .tk-fn  { color: #ffd787; }
table.diff .tk-str { color: #afd787; }
table.diff .tk-num { color: #ffaf5f; }
table.diff .tk-cmt { color: #8a8a8a; font-style: italic; }
table.diff .tk-op  { color: #d0d0d0; }
table.diff .tk-pn  { color: #bcbcbc; }
table.diff .tk-at  { color: #d7af87; }
```

- [ ] **Step 5: Run tests and check in a browser**

Run: `cd <wt> && go test ./internal/web/ 2>&1 | tail -5`
Expected: PASS.

Then, per the `playwright-web-verification` memory: `go build -o gg ./cmd/gg && ./gg web` in a fixture repo, hard-reload, open a `.go` file's diff, and confirm `document.querySelector('table.diff .tk-kw')` is non-null and coloured (`getComputedStyle(...).color`). Confirm the binary is yours first (`curl /api/repo`).

- [ ] **Step 6: Commit**

```bash
cd <wt> && gofmt -l internal/web && git add internal/web && git commit -q -m "feat(web): /api/diff carries syntax runs; diff table colours them

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 6: Docs, full test run, binary

**Files:**
- Modify: `CHANGELOG.md` (top of `[Unreleased]`), `README.md` (diff-view feature bullet near where `[f]`/`ctrl+w` are described), `CLAUDE.md` (package map row), `docs/CLAUDE-details.md` (a paragraph next to the `textdiff`/differ notes)

- [ ] **Step 1: CHANGELOG entry** (first bullet under `## [Unreleased]`)

```markdown
- **Diff views colour code by file type.** The TUI side-by-side diff and the
  `gg web` diff table now colour keywords, types, function names, strings,
  numbers, comments and operators, chosen by file name through chroma's
  ~300 lexers. Each side is lexed whole (block comments and raw strings keep
  their state across lines) and the runs are cached with the diff, keyed by
  source line so the shared aligned rows stay untouched. Word-level emphasis
  still wins inside a changed row. Sides over 1 MB or files with no known
  lexer render plain. `[ui] diff_syntax = "off"` disables it. New pure
  package `internal/syntax`.
```

- [ ] **Step 2: CLAUDE.md package-map row** (one line, after `textdiff`)

```markdown
| `syntax`     | Pure chroma wrapper: `Detect(path)` → lexer, `Lex(lang, src)` → per-source-line token runs (`Tok{Start,End,Class}`, rune offsets, coarse `Class` enum). DAG leaf; `domain.Differ` attaches runs to `Diff.OldTok/NewTok`. |
```

- [ ] **Step 3: docs/CLAUDE-details.md** — next to the differ/textdiff notes add:

```markdown
**Diff syntax runs.** `domain.Request.Path` selects the lexer; `plainDiffer`
lexes each side ≤ `MaxSyntaxBytes` (1 MB) when `DifferOptions.Syntax()` is
true (Service: `SetSyntaxHighlighting`, default on, `[ui] diff_syntax`). Runs
live on `Diff.OldTok/NewTok` indexed by source line-1 (never on
`textdiff.Row`, whose cached instances are shared read-only); cache keys gain
an `s:` segment so toggling never serves the wrong entry. TUI: `sanitizeCell`
builds parallel emph/class masks over the tab-expanded runes; `styledRuns`
groups by (emph, class) and emphasis wins. Web: `left_tok`/`right_tok`
`[start,end,cls]` triples → `.tk-<cls>` spans via `renderCell`.
```

- [ ] **Step 4: README feature mention** — in the diff-view paragraph that lists `f` / `ctrl+w`, add one sentence: "Code is syntax-coloured by file type (see `[ui] diff_syntax`)."

- [ ] **Step 5: Full gates**

```bash
cd <wt> && ./test.sh unit 2>&1 | tail -8
cd <wt> && ./test.sh e2e 2>&1 | tail -5
```

Expected: all green. Then the race gate on a quiet machine (memory: `race-gate-needs-a-quiet-machine`): `nohup ./test.sh race > /tmp/claude-1000/-mnt-t-others-gigagit/a145f667-53f5-4103-95d2-a26fb6444dcf/scratchpad/race.log 2>&1 &` and poll the log.

- [ ] **Step 6: Commit docs, build, deliver**

```bash
cd <wt> && git add CHANGELOG.md README.md CLAUDE.md docs/CLAUDE-details.md && git commit -q -m "docs: diff syntax highlighting

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
cd <wt> && go build -o gg ./cmd/gg && ls -l gg
```

Report the absolute path of `<wt>/gg` to the user (and send it with SendUserFile). Do not merge; the user merges.

---

## Self-review notes

- Spec §5 coverage: engine package ✔ (T1), domain sidecars + cache + size guard ✔ (T2), config keys ✔ (T3, `diff_syntax`; `diff_syntax_theme` deliberately dropped: the TUI has no theme infrastructure and the web has one dark palette, so a theme key would be a stub), TUI compositor ✔ (T4), web classes ✔ (T5), "render plain first, restyle when tokens arrive" ✘ replaced by a synchronous lex under a 1 MB cap (≤ ~1 s worst case, typical files < 100 ms), recorded in T2 and the CHANGELOG; revisit if the diff open latency shows.
- Type consistency: `syntax.Tok{Start, End int; Class Class}`, `Diff.OldTok/NewTok [][]syntax.Tok`, `sanitizeCell(s, spans, toks) (disp, emph, cls)`, `styledRuns(disp, emph, cls, base)`, `tokAt(side, no)`, `tokTriples(side, no)`, `renderCell(text, spans, toks, side)` used identically across tasks.
- Every `domain.Request{…}` literal (12 TUI + 4 web) gains `Path:`; a missing one only loses colour for that surface, never breaks.
