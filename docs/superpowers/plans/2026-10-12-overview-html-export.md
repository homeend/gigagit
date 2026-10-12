# Overview HTML Export Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (project rule: NO implementer subagents — this session implements; only the final review may be a read-only subagent). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Export any overview (agent, or a stored commit/range/preview/branch/PR review overview) as ONE self-contained `.html` file — the split view (overview left, anchored code right, file ↔ diff toggle) working offline from `file://`.

**Architecture:** `domain.ExportOverviewHTML` gathers the overview's block tree, anchors, and every anchored file's frozen lines + syntax runs + diff rows into an `overviewhtml.Export` value; the new DAG-leaf package `internal/overviewhtml` renders that into one page that inlines gg web's `style.css`, `markdown.js` and `splitdiff.js` (read from a new shared `internal/web/static` Go package) plus its own `export.js`/`export.css`. Web (download), CLI (`--out`) and TUI (path popup → `engine.ExportFile`) all call the one domain builder.

**Tech Stack:** Go 1.26 (`embed`, `encoding/json`), vanilla JS (classic script, no modules at runtime), node (JS syntax gate), Playwright chromium + firefox (file:// browser check).

**Spec:** `docs/superpowers/specs/2026-10-12-overview-html-export-design.md` (read it with this plan; the amendments in "Spec amendments" below win where they differ).

## Global Constraints

- R1: content = anchored files in full at their frozen version + their diff; files no anchor names are NOT included.
- R2: entry points web, CLI and TUI, all over `domain.ExportOverviewHTML`.
- R3: NO notes — no review summaries, line notes, remarks or temporary-note text in the output; a `note:t<n>` anchor only links to the note's lines.
- The output makes no network request: no `<script src`, no `<link`, no `fetch(`, no `/api/`; the only `http(s)://` strings may come from the overview's own text (markdown links).
- No absolute path or worktree path enters the page.
- Page cap `overviewhtml.MaxBytes = 16 << 20`.
- `internal/overviewhtml` and `internal/web/static` are DAG leaves (archtest).
- Every TUI string through `i18n.T` with keys in all four bundles (ja/ko/zh/ru); engine/CLI prose English.
- CLI agent-facing change → update `internal/agentskill/using-gg.md` and bump `agentskill.Version`.
- Tests use real git (`newRealRepo`, `sendRepo`/`savePRReview`); new tests call `t.Parallel()`.
- Commit with `gg add <paths>` + `git commit -F <msgfile>` (gg commit has no -F); never `git add -A`; never `git checkout -- <file>`.
- Commit message trailer:
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01W6oqyQtsQgc7hqGUfr6zst`.

## Spec amendments (found while planning — code facts)

1. **No `data:` module imports.** The page inlines ONE classic `<script>`: each reused module is wrapped by Go as `const MD = (() => { <body without export keywords>; return { … }; })();` (`markdown.js` and `splitdiff.js` have no imports — checked). Works over `file://` in every browser with no module loading at all. The spec's fallback becomes the design.
2. **Theme:** gg web is dark-only (`style.css` `:root` has no light set); the export is dark like gg web. No `prefers-color-scheme`.
3. **CLI flag** follows `gg commit export-patch`: `--out <path>` (default `<ExportDefaultDir>/<slug>.html`), `--out -` = stdout, `--force` overwrites. Not `-o`.
4. **TUI entry** is a `.` menu row on an overview (single and split pane), not a palette row (the `.` menu is where overview actions live: `overviewRows`/`splitOverviewRows`).
5. **Agent source:** the caller's `*domain.Service` must be the overview's worktree (web/TUI already scope overviews to their root; the CLI runs in the caller's worktree, the same one `overview_show` names). So `OverviewSource` has no `Dir`; `domain.AgentOverviewSource(docs, id, root)` builds it from an `agentdocs.Store` for web and TUI.
6. **Web key** is `E` (x closes an overview) plus an `E export html` foot chip on overview foots.
7. **Stored-review title:** `"Review overview: " + reviewLabel` (domain port of the TUI's `reviewLabel`: working changes / scope label / `branch short` / `short`).

## Coordination

`feat/permanent-overviews` (another session; spec `d563a001`, not merged) stores an agent's overview ON A COMMIT as an overview-only review document in the same notes store (tag `overview`). If it lands as a `domain.Review` with `Doc.Overview`, `OverviewSource{ReviewID}` exports it unchanged; whichever branch merges second adds the export row/verb to that feature's surfaces (`gg overview export` alias, its `.` menu "Open overview" view). Check `git log main --oneline | grep -i "permanent overview"` before Task 10 and note the outcome in CHANGELOG.

## Review Focus

1. A file whose text contains `</script>` or `<!--` — the data blob must stay inert and the page must still render (test in Task 3).
2. An anchor to a file that is gone, binary or past its end — it must render plain (not a dead link) and the export must still succeed (tests in Tasks 4 and 5).
3. An overview whose anchors name the same file many times / in ranges — the file is included once, every anchor bands its own lines (Task 4 test + Task 9 browser check).
4. A review whose anchored file did not change in the range — `D` must say "unchanged in this range", not show an empty pane (Task 3 JS + Task 9 check).
5. A huge anchored file set — the export must fail with the cap message naming the largest files, not write a 200 MB file (Task 3 test).

## File Structure

| File | Responsibility |
|---|---|
| `internal/web/static/embed.go` (new) | `package static`; `//go:embed *.js *.css *.html`; `var FS embed.FS` |
| `internal/web/static.go` (modify) | read/serve through `static.FS` |
| `internal/web/server.go:215` (modify) | `/static/` file server over `static.FS` with `StripPrefix` |
| `internal/overviewhtml/export.go` (new) | data types, `Toks`, `Rows`, `Slug`, `ErrTooLarge` |
| `internal/overviewhtml/render.go` (new) | `Render`, `scopeModule`, page assembly, size cap |
| `internal/overviewhtml/assets/page.html`, `export.js`, `export.css` (new) | the page skeleton, its script, its layout CSS |
| `internal/domain/overview_export.go` (new) | `OverviewSource`, `NoteSpan`, `ExportOverviewHTML`, `AgentOverviewSource`, `reviewTitle` |
| `internal/domain/review_overview.go` (modify) | factor `overviewFileBytes` out of `overviewFileLines` |
| `internal/web/overview_export_http.go` (new) | `GET /api/overview-export` |
| `internal/web/static/viewer.js` (modify) | `E` key + foot chip → download |
| `internal/cli/review_export.go` (new), `internal/cli/review.go` (modify) | `gg review export` |
| `internal/cli/session_overview.go` (modify) | `gg session overview export` |
| `internal/tui/overview_export.go` (new), `overview_keys.go`, `export_patch.go`, `model.go` (modify) | `.` menu row + reuse of the export path popup |
| `internal/i18n/*.toml` (modify) | new TUI strings |
| `internal/archtest/import_guard_test.go` (modify) | leaf rules |
| `e2e/scenarios/review_export_html.toml` (new) | CLI e2e |
| docs: `CHANGELOG.md`, `README.md`, `CLAUDE.md`, `docs/CLAUDE-details.md`, `internal/agentskill/using-gg.md` | docs |

---

### Task 1: Shared static package

**Files:**
- Create: `internal/web/static/embed.go`
- Modify: `internal/web/static.go`, `internal/web/server.go:215`, `internal/archtest/import_guard_test.go`
- Test: `internal/web/static/embed_test.go`

**Interfaces:**
- Produces: `static.FS embed.FS` (import `github.com/homeend/gigagit/internal/web/static`), files at the FS root (`"markdown.js"`, `"style.css"`, …).

- [ ] **Step 1: Write the failing test** — `internal/web/static/embed_test.go`:

```go
package static

import "testing"

func TestFSHoldsTheSharedAssets(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"index.html", "style.css", "markdown.js", "splitdiff.js", "app.js"} {
		if b, err := FS.ReadFile(name); err != nil || len(b) == 0 {
			t.Errorf("%s: %v (%d bytes)", name, err, len(b))
		}
	}
	if _, err := FS.ReadFile("embed.go"); err == nil {
		t.Error("the Go source must not be embedded")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/web/static/` — Expected: FAIL (no Go files / `FS` undefined).

- [ ] **Step 3: Implement** `internal/web/static/embed.go`:

```go
// Package static is gg web's embedded page: the SPA gg web serves and the
// shared painters (markdown.js, splitdiff.js, style.css) the overview HTML
// export inlines. A package of its own so a package outside internal/web can
// embed the same bytes (go:embed cannot reach a parent directory). DAG leaf.
package static

import "embed"

//go:embed *.js *.css *.html
var FS embed.FS
```

`internal/web/static.go`: drop the `//go:embed static` var; import `github.com/homeend/gigagit/internal/web/static`; `static.FS.ReadFile("index.html")`. `server.go:215`: `mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static.FS)))`.

Archtest (`TestLayeringDAG` cases): add
```go
		// web/static is gg web's embedded page; it imports nothing of gg.
		"web/static": {"config", "model", "git", "engine", "domain", "tui", "cli", "mcp", "web", "app", "markdown", "syntax"},
```

- [ ] **Step 4: Run** `go test ./internal/web/... ./internal/archtest/` — Expected: PASS (existing web tests prove `/static/` and `/` still serve).

- [ ] **Step 5: Commit** — `web: the embedded page becomes package web/static (shared with the overview export)`.

---

### Task 2: `overviewhtml` data types and conversions

**Files:**
- Create: `internal/overviewhtml/export.go`
- Test: `internal/overviewhtml/export_test.go`
- Modify: `internal/archtest/import_guard_test.go`

**Interfaces:**
- Consumes: `markdown.Block`, `syntax.Tok`/`syntax.Name`, `textdiff.Row`/`textdiff.Kind`.
- Produces (exact):

```go
type Export struct {
	Title      string           `json:"title"`
	Repo       string           `json:"repo"`
	Kind       string           `json:"kind"` // "agent" | "review"
	Left       string           `json:"left"`
	Right      string           `json:"right"`
	ExportedAt string           `json:"exported_at"`
	Version    string           `json:"version"`
	Blocks     []markdown.Block `json:"blocks"`
	Anchors    []Anchor         `json:"anchors"`
	Files      []File           `json:"files"`
}
type Anchor struct {
	Dest    string `json:"dest"`
	Path    string `json:"path,omitempty"`
	Start   int    `json:"start,omitempty"`
	End     int    `json:"end,omitempty"`
	Label   string `json:"label,omitempty"`
	Missing bool   `json:"missing"`
}
type File struct {
	Path        string    `json:"path"`
	Status      string    `json:"status,omitempty"`
	OldPath     string    `json:"old_path,omitempty"`
	Lines       []Line    `json:"lines"`
	Changed     bool      `json:"changed"`
	Diff        []DiffRow `json:"diff,omitempty"`
	Placeholder string    `json:"placeholder,omitempty"`
}
type Line struct {
	Text string `json:"text"`
	Tok  []Tok  `json:"tok,omitempty"`
}
type Tok [3]any // [start, end, class] — /api/file-content's wire triple
type DiffRow struct { // /api/diff's row shape, the fields splitdiff.js reads
	Kind       string   `json:"kind"`
	Left       string   `json:"left"`
	Right      string   `json:"right"`
	LeftNo     int      `json:"left_no"`
	RightNo    int      `json:"right_no"`
	LeftSpans  [][2]int `json:"left_spans,omitempty"`
	RightSpans [][2]int `json:"right_spans,omitempty"`
	LeftTok    []Tok    `json:"left_tok,omitempty"`
	RightTok   []Tok    `json:"right_tok,omitempty"`
}
func Toks(side [][]syntax.Tok, no int) []Tok
func Lines(text []string, tok [][]syntax.Tok) []Line
func Rows(rows []textdiff.Row, oldTok, newTok [][]syntax.Tok) []DiffRow
func Slug(title string) string // file name stem: [a-z0-9-], ≤ 60, "overview" when empty
```

- [ ] **Step 1: Write the failing tests** — `export_test.go`:

```go
package overviewhtml

import (
	"encoding/json"
	"testing"

	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/textdiff"
)

func TestToksDropsUnstyledAndOutOfRange(t *testing.T) {
	t.Parallel()
	side := [][]syntax.Tok{{{Start: 0, End: 2, Class: syntax.Keyword}, {Start: 3, End: 4, Class: syntax.Name}}}
	got := Toks(side, 1)
	if len(got) != 1 || got[0][0] != 0 || got[0][1] != 2 || got[0][2] != syntax.Keyword.String() {
		t.Fatalf("Toks = %v", got)
	}
	if Toks(side, 0) != nil || Toks(side, 2) != nil || Toks(nil, 1) != nil {
		t.Fatal("out of range must be nil")
	}
}

func TestRowsMatchTheDiffWire(t *testing.T) {
	t.Parallel()
	rows := []textdiff.Row{
		{Kind: textdiff.Same, Left: "a", Right: "a", LeftNo: 1, RightNo: 1},
		{Kind: textdiff.Changed, Left: "b", Right: "B", LeftNo: 2, RightNo: 2, LeftSpans: []textdiff.Span{{Start: 0, End: 1}}},
		{Kind: textdiff.Add, Right: "c", RightNo: 3},
	}
	b, _ := json.Marshal(Rows(rows, nil, nil))
	want := `[{"kind":"same","left":"a","right":"a","left_no":1,"right_no":1},` +
		`{"kind":"change","left":"b","right":"B","left_no":2,"right_no":2,"left_spans":[[0,1]]},` +
		`{"kind":"add","left":"","right":"c","left_no":0,"right_no":3}]`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
}

func TestSlug(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"Review overview: main 1a2b3c4": "review-overview-main-1a2b3c4",
		"  ":                             "overview",
		"Ünïcode / why?":                 "n-code-why",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Slug(string(make([]byte, 200)) + "x"); len(got) > 60 {
		t.Errorf("too long: %q", got)
	}
}
```

(`syntax.Keyword` — confirm the exact constant name with `grep -n "Keyword\|= iota" internal/syntax/*.go` and use whatever class `String()`s to `"kw"`.)

- [ ] **Step 2: Run** `go test ./internal/overviewhtml/` — Expected: FAIL (undefined).

- [ ] **Step 3: Implement** `export.go` — the types above, plus:

```go
// unstyled are classes style.css paints nothing for (internal/web's
// unstyledOnWire — keep the two in step): sending them costs bytes only.
var unstyled = map[syntax.Class]bool{syntax.Name: true}

func Toks(side [][]syntax.Tok, no int) []Tok {
	if no <= 0 || no > len(side) || len(side[no-1]) == 0 {
		return nil
	}
	var out []Tok
	for _, tk := range side[no-1] {
		if !unstyled[tk.Class] {
			out = append(out, Tok{tk.Start, tk.End, tk.Class.String()})
		}
	}
	return out
}

func Lines(text []string, tok [][]syntax.Tok) []Line {
	out := make([]Line, len(text))
	for i, l := range text {
		out[i] = Line{Text: l, Tok: Toks(tok, i+1)}
	}
	return out
}

func kindOf(k textdiff.Kind) string {
	switch k {
	case textdiff.Changed:
		return "change"
	case textdiff.Del:
		return "del"
	case textdiff.Add:
		return "add"
	}
	return "same"
}

func spans(s []textdiff.Span) [][2]int {
	if len(s) == 0 {
		return nil
	}
	out := make([][2]int, len(s))
	for i, sp := range s {
		out[i] = [2]int{sp.Start, sp.End}
	}
	return out
}

func Rows(rows []textdiff.Row, oldTok, newTok [][]syntax.Tok) []DiffRow {
	out := make([]DiffRow, len(rows))
	for i, r := range rows {
		out[i] = DiffRow{Kind: kindOf(r.Kind), Left: r.Left, Right: r.Right, LeftNo: r.LeftNo, RightNo: r.RightNo,
			LeftSpans: spans(r.LeftSpans), RightSpans: spans(r.RightSpans),
			LeftTok: Toks(oldTok, r.LeftNo), RightTok: Toks(newTok, r.RightNo)}
	}
	return out
}

func Slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
		if b.Len() >= 60 {
			break
		}
	}
	if b.Len() == 0 {
		return "overview"
	}
	return b.String()
}
```

Archtest case:
```go
		// overviewhtml renders an export from plain data: no git, no domain, no frontend.
		"overviewhtml": {"config", "model", "git", "engine", "domain", "tui", "cli", "mcp", "web", "app", "i18n", "agentdocs"},
```

- [ ] **Step 4: Run** `go test ./internal/overviewhtml/ ./internal/archtest/` — Expected: PASS.

- [ ] **Step 5: Commit** — `overviewhtml: the export's data shape (the gg web wire's file/diff rows)`.

---

### Task 3: `overviewhtml.Render` — the self-contained page

**Files:**
- Create: `internal/overviewhtml/render.go`, `internal/overviewhtml/assets/page.html`, `assets/export.js`, `assets/export.css`
- Test: `internal/overviewhtml/render_test.go`

**Interfaces:**
- Consumes: `static.FS` (Task 1), `Export` (Task 2).
- Produces: `func Render(e Export) ([]byte, error)`; `const MaxBytes = 16 << 20`; `type TooLargeError struct{ Size int; Largest []string }` (`Error()` = `"overview export is N MB, over the 16 MB cap; largest files: a (x MB), b (y MB), c (z MB)"`).

- [ ] **Step 1: Write the failing tests** — `render_test.go`:

```go
package overviewhtml

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/markdown"
)

func fixture() Export {
	doc := markdown.Parse("See [the loop](a.go:2-3).")
	return Export{
		Title: "Why </script> broke", Repo: "gigagit", Kind: "review", Left: "1a2b3c4", Right: "5d6e7f8",
		ExportedAt: "2026-10-12 10:00", Version: "v0.0.0-test", Blocks: doc.Blocks,
		Anchors: []Anchor{{Dest: "a.go:2-3", Path: "a.go", Start: 2, End: 3}},
		Files: []File{{Path: "a.go", Status: "M", Changed: true,
			Lines: []Line{{Text: "package a"}, {Text: `x := "</script><!--"`}, {Text: "y"}},
			Diff:  []DiffRow{{Kind: "same", Left: "package a", Right: "package a", LeftNo: 1, RightNo: 1}}}},
	}
}

var dataRE = regexp.MustCompile(`(?s)<script type="application/json" id="gg-data">(.*?)</script>`)

func TestRenderIsOneSelfContainedPage(t *testing.T) {
	t.Parallel()
	out, err := Render(fixture())
	if err != nil {
		t.Fatal(err)
	}
	page := string(out)
	for _, bad := range []string{"<script src", "<link", "fetch(", "/api/", "import ", "export function", "http://", "https://"} {
		if strings.Contains(page, bad) {
			t.Errorf("page contains %q", bad)
		}
	}
	if n := strings.Count(strings.ToLower(page), "</script>"); n != 2 { // the data blob + the one script
		t.Errorf("</script> count = %d, want 2 (file text must stay escaped)", n)
	}
	m := dataRE.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("no data blob")
	}
	var back Export
	if err := json.Unmarshal([]byte(m[1]), &back); err != nil {
		t.Fatalf("blob: %v", err)
	}
	if back.Files[0].Lines[1].Text != `x := "</script><!--"` || back.Title != "Why </script> broke" {
		t.Errorf("round trip lost text: %+v", back.Files[0].Lines[1])
	}
	if !strings.Contains(page, "<title>Why &lt;/script&gt; broke</title>") {
		t.Error("title not escaped into <title>")
	}
}

func TestScopeModuleLeavesNoModuleSyntax(t *testing.T) {
	t.Parallel()
	got, err := scopeModule("MD", "export function a() {}\nexport const B = 1;\nfunction c() {}\nexport { c };\n", []string{"a", "B", "c"})
	if err != nil {
		t.Fatal(err)
	}
	want := "const MD = (() => {\nfunction a() {}\nconst B = 1;\nfunction c() {}\n\nreturn { a, B, c };\n})();\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if _, err := scopeModule("X", "import { y } from './y.js';\n", nil); err == nil {
		t.Error("an import must be refused")
	}
}

func TestRenderRefusesTooLarge(t *testing.T) {
	t.Parallel()
	e := fixture()
	big := strings.Repeat("x", 1<<20)
	for i := range 20 {
		e.Files = append(e.Files, File{Path: filepath.Join("d", string(rune('a'+i))+".txt"), Lines: []Line{{Text: big}}})
	}
	_, err := Render(e)
	var tl *TooLargeError
	if !errors.As(err, &tl) || len(tl.Largest) != 3 || !strings.Contains(err.Error(), "16 MB cap") {
		t.Fatalf("err = %v", err)
	}
}

// The inlined script must at least parse: node --check on the extracted script.
func TestRenderScriptParses(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := Render(fixture())
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindStringSubmatch(string(out))
	if m == nil {
		t.Fatal("no script")
	}
	f := filepath.Join(t.TempDir(), "page.js")
	if err := os.WriteFile(f, []byte(m[1]), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command(node, "--check", f).CombinedOutput(); err != nil {
		t.Fatalf("node --check: %v\n%s", err, b)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/overviewhtml/` — Expected: FAIL (`Render`, `scopeModule` undefined).

- [ ] **Step 3: Implement.**

`render.go`:

```go
package overviewhtml

import (
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"

	"github.com/homeend/gigagit/internal/web/static"
)

//go:embed assets
var assets embed.FS

const MaxBytes = 16 << 20

type TooLargeError struct {
	Size    int
	Largest []string // "path (x MB)", largest first, at most 3
}

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("overview export is %d MB, over the %d MB cap; largest files: %s",
		mb(e.Size), MaxBytes>>20, strings.Join(e.Largest, ", "))
}

func mb(n int) int { return (n + 1<<20 - 1) >> 20 }

// The reused painters and the names the page script calls on them.
var modules = []struct{ name, file string; exports []string }{
	{"MD", "markdown.js", []string{"mdHTML"}},
	{"SD", "splitdiff.js", []string{"unifiedRows", "lineRows", "rowOfLine"}},
}

var (
	exportLine = regexp.MustCompile(`(?m)^export \{[^}]*\};?[ \t]*$`)
	exportKw   = regexp.MustCompile(`(?m)^export (function|const|let|class) `)
	moduleSyn  = regexp.MustCompile(`(?m)^\s*(import|export)\b`)
)

// scopeModule turns an import-free ES module into a block-scoped classic
// script: its exports become the fields of const name.
func scopeModule(name, src string, exports []string) (string, error) {
	body := exportLine.ReplaceAllString(src, "")
	body = exportKw.ReplaceAllString(body, "$1 ")
	if loc := moduleSyn.FindStringIndex(body); loc != nil {
		return "", fmt.Errorf("%s: module syntax left: %q", name, strings.TrimSpace(body[loc[0]:min(loc[1]+40, len(body))]))
	}
	return "const " + name + " = (() => {\n" + body + "\nreturn { " + strings.Join(exports, ", ") + " };\n})();\n", nil
}

func Render(e Export) ([]byte, error) {
	if size := approxSize(e); size > MaxBytes {
		return nil, tooLarge(e, size)
	}
	css, err := static.FS.ReadFile("style.css")
	if err != nil {
		return nil, err
	}
	own, _ := assets.ReadFile("assets/export.css")
	var script strings.Builder
	for _, m := range modules {
		src, err := static.FS.ReadFile(m.file)
		if err != nil {
			return nil, err
		}
		s, err := scopeModule(m.name, string(src), m.exports)
		if err != nil {
			return nil, err
		}
		script.WriteString(s)
	}
	js, _ := assets.ReadFile("assets/export.js")
	script.Write(js)
	data, err := json.Marshal(e) // escapes < > & U+2028 U+2029: the blob cannot close its tag
	if err != nil {
		return nil, err
	}
	for _, part := range []string{string(css), string(own), script.String()} {
		if l := strings.ToLower(part); strings.Contains(l, "</script") || strings.Contains(l, "</style") {
			return nil, fmt.Errorf("an inlined asset closes its own tag")
		}
	}
	page, _ := assets.ReadFile("assets/page.html")
	out := strings.NewReplacer(
		"{{TITLE}}", html.EscapeString(e.Title),
		"{{CSS}}", string(css)+"\n"+string(own),
		"{{DATA}}", string(data),
		"{{SCRIPT}}", script.String(),
	).Replace(string(page))
	if len(out) > MaxBytes {
		return nil, tooLarge(e, len(out))
	}
	return []byte(out), nil
}

func fileSize(f File) int {
	n := 0
	for _, l := range f.Lines {
		n += len(l.Text) + 16*len(l.Tok)
	}
	for _, r := range f.Diff {
		n += len(r.Left) + len(r.Right) + 16*(len(r.LeftTok)+len(r.RightTok))
	}
	return n
}

func approxSize(e Export) int {
	n := 0
	for _, f := range e.Files {
		n += fileSize(f)
	}
	return n
}

func tooLarge(e Export, size int) error {
	fs := append([]File(nil), e.Files...)
	sort.SliceStable(fs, func(i, j int) bool { return fileSize(fs[i]) > fileSize(fs[j]) })
	var big []string
	for _, f := range fs[:min(3, len(fs))] {
		big = append(big, fmt.Sprintf("%s (%d MB)", f.Path, mb(fileSize(f))))
	}
	return &TooLargeError{Size: size, Largest: big}
}
```

(`splitdiff.js` must export `unifiedRows`, `lineRows`, `rowOfLine` — it does: `export { … lineRows, …, rowOfLine, …, unifiedRows }`. The `scopeModule` test plus `TestRenderScriptParses` guard a future edit adding an import.)

`assets/page.html`:

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="generator" content="gg overview export">
<title>{{TITLE}}</title>
<style>
{{CSS}}
</style>
</head>
<body>
<header id="x-top"><div id="x-title"></div><div id="x-meta"></div></header>
<main id="x-split">
  <section id="x-ov" class="md" tabindex="0"></section>
  <div id="x-drag" title="drag to resize"></div>
  <section id="x-code">
    <div id="x-codehead"><select id="x-files"></select><span id="x-status"></span><button id="x-flip" type="button">D file / diff</button></div>
    <div id="viewer-body" tabindex="0"></div>
  </section>
</main>
<footer id="x-foot">click a link · n / p next / previous link · D file / diff</footer>
<script type="application/json" id="gg-data">{{DATA}}</script>
<script>
{{SCRIPT}}
</script>
</body>
</html>
```

`assets/export.css`:

```css
/* The exported overview page's own layout; colours, .vline, .tk-*, .md-* come from gg web's style.css. */
#x-top { padding: 8px 12px; border-bottom: 1px solid var(--border); background: var(--bg-alt); }
#x-title { font-weight: bold; color: var(--accent); }
#x-meta, #x-foot, .x-dim { color: var(--dim); font-size: 12px; }
#x-foot { padding: 4px 12px; border-top: 1px solid var(--border); }
#x-split { flex: 1; min-height: 0; display: flex; --ov-w: 40%; }
#x-ov { width: var(--ov-w); overflow: auto; padding: 12px 16px; }
#x-drag { flex: none; width: 5px; cursor: col-resize; background: var(--border); }
#x-code { flex: 1; min-width: 0; display: flex; flex-direction: column; }
#x-codehead { display: flex; gap: 10px; align-items: center; padding: 6px 12px; border-bottom: 1px solid var(--border); background: var(--bg-alt); }
#x-codehead select, #x-codehead button { background: var(--bg); color: var(--fg); border: 1px solid var(--border); font: inherit; padding: 1px 6px; }
#viewer-body { flex: 1; overflow: auto; outline: none; }
.x-notice { padding: 16px; color: var(--dim); }
@media (max-width: 899px) {
  #x-split { flex-direction: column; }
  #x-ov { width: auto; max-height: 45vh; border-bottom: 1px solid var(--border); }
  #x-drag { display: none; }
}
```

`assets/export.js` (a classic script; `MD` and `SD` are defined just above it):

```js
// export.js — the exported overview page: gg web's split view, offline.
// The data is the JSON blob #gg-data (overviewhtml.Export); MD is
// markdown.js, SD splitdiff.js, both inlined above by overviewhtml.Render.
// No storage, no network: the page is a snapshot.
(() => {
  "use strict";
  const D = JSON.parse(document.getElementById("gg-data").textContent);
  const $ = (id) => document.getElementById(id);
  const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
  const files = new Map((D.files || []).map((f) => [f.path, f]));
  const anchors = D.anchors || [];
  const live = (a) => a && !a.missing && files.has(a.path);
  const st = { file: "", diff: false, anchor: -1 };

  // cell paints one line: intraline marks, then syntax runs (files.js renderCell, no search hits).
  function cell(text, toks, spans, side) {
    if ((!spans || !spans.length) && (!toks || !toks.length)) return esc(text);
    const rs = Array.from(text);
    const emph = new Array(rs.length).fill(false), cls = new Array(rs.length).fill("");
    for (const [a, b] of spans || []) for (let i = a; i < b && i < rs.length; i++) emph[i] = true;
    for (const [a, b, c] of toks || []) {
      if (!/^[a-z]{2,3}$/.test(c)) continue;
      for (let i = a; i < b && i < rs.length; i++) cls[i] = c;
    }
    let out = "";
    for (let i = 0; i < rs.length; ) {
      let j = i + 1;
      while (j < rs.length && emph[j] === emph[i] && cls[j] === cls[i]) j++;
      const seg = esc(rs.slice(i, j).join(""));
      out += emph[i] ? `<mark class="${side}">${seg}</mark>` : cls[i] ? `<span class="tk-${cls[i]}">${seg}</span>` : seg;
      i = j;
    }
    return out;
  }

  function band() {
    const a = anchors[st.anchor];
    if (!a || a.path !== st.file || !a.start) return null;
    return { start: a.start, end: a.end || a.start };
  }
  const inBand = (b, n) => !!b && n >= b.start && n <= b.end;

  function header() {
    $("x-title").textContent = D.title || "Overview";
    const range = D.left && D.right ? D.left + " → " + D.right : "";
    $("x-meta").textContent = [D.repo, range, "exported " + D.exported_at + " by gg " + D.version].filter(Boolean).join(" · ");
    const sel = $("x-files");
    sel.innerHTML = [...files.keys()].map((p) => `<option value="${esc(p)}">${esc(p)}</option>`).join("");
  }

  function paintOverview() {
    const ov = $("x-ov");
    ov.innerHTML = MD.mdHTML({ blocks: D.blocks }, esc, { anchors: true }) || `<p class="x-dim">(empty overview)</p>`;
    for (const el of Array.from(ov.querySelectorAll("a.md-anchor"))) {
      if (!live(anchors[Number(el.dataset.a)])) el.replaceWith(...Array.from(el.childNodes)); // plain, as gg web draws it
    }
  }

  function paintAnchorMarks() {
    for (const el of $("x-ov").querySelectorAll("a.md-anchor")) el.classList.toggle("asel", Number(el.dataset.a) === st.anchor);
  }

  function paintCode() {
    const body = $("viewer-body"), f = files.get(st.file);
    $("x-files").value = st.file;
    if (!f) {
      $("x-status").textContent = "";
      body.innerHTML = `<div class="x-notice">${files.size ? "pick a file" : "this overview links to no code"}</div>`;
      return;
    }
    $("x-status").textContent = (f.status ? f.status + " " : "") + (f.old_path ? f.old_path + " → " : "") + (st.diff ? "diff" : "file");
    if (f.placeholder) {
      body.innerHTML = `<div class="x-notice">${esc(f.placeholder)}</div>`;
      return;
    }
    const b = band();
    let html = "";
    if (st.diff) {
      if (!f.changed) {
        body.innerHTML = `<div class="x-notice">unchanged in this range</div>`;
        return;
      }
      for (const r of SD.unifiedRows({ rows: f.diff })) {
        const sign = r.kind === "add" ? "+" : r.kind === "del" ? "−" : " ";
        const ranged = r.nu && inBand(b, r.nu) ? " vrange" : "";
        html += `<div class="vline vd-${r.kind}${ranged}" data-n="${r.nu || ""}"><span class="vno">${r.kind === "del" ? r.old : r.nu}</span>` +
          `<span class="vsign">${sign}</span><span class="vtext">${cell(r.text, r.tok, r.spans, r.kind === "del" ? "l" : "r") || " "}</span></div>`;
      }
    } else {
      (f.lines || []).forEach((l, i) => {
        const n = i + 1;
        html += `<div class="vline${inBand(b, n) ? " vrange" : ""}" data-n="${n}"><span class="vno">${n}</span>` +
          `<span class="vtext">${cell(l.text, l.tok, null, "r") || " "}</span></div>`;
      });
    }
    body.innerHTML = html;
  }

  function scrollToBand() {
    const b = band(), body = $("viewer-body");
    if (!b) return void (body.scrollTop = 0);
    const el = body.querySelector(`.vline[data-n="${b.start}"]`);
    if (el) el.scrollIntoView({ block: "center" });
  }

  function openAnchor(i) {
    const a = anchors[i];
    if (!live(a)) return;
    Object.assign(st, { anchor: i, file: a.path });
    paintAnchorMarks();
    paintCode();
    scrollToBand();
    const link = $("x-ov").querySelector(`a.md-anchor[data-a="${i}"]`);
    if (link) link.scrollIntoView({ block: "nearest" });
  }

  function step(dir) {
    const n = anchors.length;
    for (let k = 1; k <= n; k++) {
      const i = (((st.anchor < 0 ? (dir > 0 ? -1 : 0) : st.anchor) + dir * k) % n + n) % n;
      if (live(anchors[i])) return openAnchor(i);
    }
  }

  function flip() {
    st.diff = !st.diff;
    paintCode();
    scrollToBand();
  }

  function wire() {
    $("x-ov").addEventListener("click", (e) => {
      const a = e.target.closest("a.md-anchor");
      if (!a) return;
      e.preventDefault();
      openAnchor(Number(a.dataset.a));
    });
    $("x-files").addEventListener("change", (e) => {
      Object.assign(st, { file: e.target.value, anchor: -1 });
      paintAnchorMarks();
      paintCode();
      $("viewer-body").scrollTop = 0;
    });
    $("x-flip").addEventListener("click", flip);
    document.addEventListener("keydown", (e) => {
      if (e.ctrlKey || e.metaKey || e.altKey || e.target.tagName === "SELECT") return;
      if (e.key === "n") step(1);
      else if (e.key === "p") step(-1);
      else if (e.key === "D") flip();
      else return;
      e.preventDefault();
    });
    const drag = $("x-drag"), split = $("x-split");
    drag.addEventListener("pointerdown", (e) => {
      drag.setPointerCapture(e.pointerId);
      const move = (ev) => {
        const r = split.getBoundingClientRect();
        const pct = Math.min(80, Math.max(15, ((ev.clientX - r.left) / r.width) * 100));
        split.style.setProperty("--ov-w", pct + "%");
      };
      const up = () => { drag.removeEventListener("pointermove", move); drag.removeEventListener("pointerup", up); };
      drag.addEventListener("pointermove", move);
      drag.addEventListener("pointerup", up);
    });
  }

  header();
  paintOverview();
  wire();
  const first = anchors.findIndex(live);
  if (first >= 0) openAnchor(first);
  else {
    st.file = files.keys().next().value || "";
    paintCode();
  }
})();
```

Note on `TestRenderIsOneSelfContainedPage`'s `"http://"`/`"https://"` check: `markdown.js` holds the regex `/^https?:\/\/…/` (escaped slashes, so no literal `https://`) — if the check trips on a comment in `markdown.js`/`splitdiff.js`, narrow the test to strings outside those two files rather than editing gg web's sources.

- [ ] **Step 4: Run** `go test ./internal/overviewhtml/` — Expected: PASS (all four tests).

- [ ] **Step 5: Commit** — `overviewhtml: Render — one page inlining gg web's painters, the data blob and the split view script`.

---

### Task 4: Domain builder — stored review overviews

**Files:**
- Create: `internal/domain/overview_export.go`
- Modify: `internal/domain/review_overview.go` (factor `overviewFileBytes`)
- Test: `internal/domain/overview_export_test.go`

**Interfaces:**
- Consumes: `ReviewOverview`, `ReviewFileDiff`, `Review`, `RepoName`, `SyntaxHighlighting`, `IsBinary`, `HasBareCR`, `MaxDiffBytes`, `MaxSyntaxBytes`, `splitTextLines`, `reviewShort`, `NoteScopeLabel`, `clock.Now`, `buildinfo.String`, `overviewhtml.*`.
- Produces:

```go
type NoteSpan struct {
	Path       string
	Start, End int
}
type OverviewSource struct {
	ReviewID  string              // a stored review's overview; else the agent fields below
	Title     string              // agent overview title
	Text      string              // agent overview markdown
	NoteSpans map[string]NoteSpan // note id ("t3") → its lines, resolved by the caller
}
// ExportOverviewHTML returns the page and its file name (<slug>.html).
func (s *Service) ExportOverviewHTML(ctx context.Context, src OverviewSource) (data []byte, name string, err error)
```

- [ ] **Step 1: Write the failing test** — `overview_export_test.go` (reuses `sendRepo`, `savePRReview`, `overviewDoc` from `review_overview_test.go`):

```go
package domain

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/overviewhtml"
)

var exportBlobRE = regexp.MustCompile(`(?s)<script type="application/json" id="gg-data">(.*?)</script>`)

func exportBlob(t *testing.T, page []byte) overviewhtml.Export {
	t.Helper()
	m := exportBlobRE.FindSubmatch(page)
	if m == nil {
		t.Fatal("no data blob")
	}
	var e overviewhtml.Export
	if err := json.Unmarshal(m[1], &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestExportStoredOverview(t *testing.T) {
	t.Parallel()
	svc, _, dir := sendRepo(t)
	rid := savePRReview(t, svc, overviewDoc)
	page, name, err := svc.ExportOverviewHTML(context.Background(), OverviewSource{ReviewID: rid})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, ".html") || !strings.HasPrefix(name, "review-overview-") {
		t.Errorf("name = %q", name)
	}
	e := exportBlob(t, page)
	if e.Kind != "review" || len(e.Files) != 1 || e.Files[0].Path != "big.go" {
		t.Fatalf("files = %+v", e.Files)
	}
	f := e.Files[0]
	if !f.Changed || len(f.Diff) == 0 || len(f.Lines) < 25 || f.Placeholder != "" {
		t.Errorf("big.go: changed=%v diff=%d lines=%d ph=%q", f.Changed, len(f.Diff), len(f.Lines), f.Placeholder)
	}
	missing := map[string]bool{}
	for _, a := range e.Anchors {
		missing[a.Dest] = a.Missing
	}
	for dest, want := range map[string]bool{"big.go:5": false, "big.go:24-25": false, "big.go": false, "other.go:1": true, "big.go:900": true, "note:t3": true} {
		if missing[dest] != want {
			t.Errorf("%s missing = %v, want %v", dest, missing[dest], want)
		}
	}
	if e.Left == "" || e.Right == "" {
		t.Errorf("range labels empty: %q %q", e.Left, e.Right)
	}
	s := string(page)
	if strings.Contains(s, "check this") { // the review's line note (R3)
		t.Error("a review note leaked into the export")
	}
	if strings.Contains(s, dir) {
		t.Error("the worktree path leaked into the export")
	}
}

func TestExportStoredOverviewAbsent(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	rid := savePRReview(t, svc, `{"version":1,"summary":"no overview","files":[]}`)
	// (if savePRReview refuses an empty files list, use the files array of overviewDoc without its "overview" key)
	if _, _, err := svc.ExportOverviewHTML(context.Background(), OverviewSource{ReviewID: rid}); err != ErrNoOverview {
		t.Fatalf("err = %v, want ErrNoOverview", err)
	}
}
```

(`sendRepo` returns `(*Service, *fakeForge, dir string)`.)

- [ ] **Step 2: Run** `go test ./internal/domain/ -run TestExportStored` — Expected: FAIL (undefined).

- [ ] **Step 3: Implement.**

In `review_overview.go`, split `overviewFileLines`:

```go
// overviewFileBytes reads path as the review's tip has it: the working file
// for a working review, else the file at the tip commit.
func (s *Service) overviewFileBytes(ctx context.Context, r Review, path string) ([]byte, error) {
	ref := model.FileRef{Source: model.SourceUnstaged, Path: path}
	if r.Kind != ReviewOnWorktree {
		t, err := s.reviewTarget(ctx, r)
		if err != nil {
			return nil, err
		}
		tip := t.Commit
		if t.Pair != nil {
			tip = t.Pair.B
		}
		ref = model.FileRef{Source: model.SourceCommit, Locator: tip, Path: path}
	}
	return s.ResolveBytes(ctx, ref)
}

func (s *Service) overviewFileLines(ctx context.Context, r Review, path string) ([]string, bool) {
	data, err := s.overviewFileBytes(ctx, r, path)
	if err != nil {
		return nil, false
	}
	return splitTextLines(data)
}
```

`overview_export.go`:

```go
package domain

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/buildinfo"
	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/markdown"
	"github.com/homeend/gigagit/internal/overviewhtml"
	"github.com/homeend/gigagit/internal/syntax"
)

// NoteSpan is where a temporary note sits: what a note:t<n> anchor links to.
type NoteSpan struct {
	Path       string
	Start, End int
}

// OverviewSource names the overview to export: a stored review's (ReviewID)
// or an agent's in-memory one (Title + Text; NoteSpans resolve its note
// anchors — the note store lives in the live TUI / web process). The
// service must be the agent overview's worktree.
type OverviewSource struct {
	ReviewID  string
	Title     string
	Text      string
	NoteSpans map[string]NoteSpan
}

// exportFile is one anchored file's frozen bytes and diff, read by kind.
type exportReader struct {
	read func(path string) ([]byte, error)
	diff func(path string) (ReviewFileDiff, error)
}

// ExportOverviewHTML builds the self-contained page for src (spec
// 2026-10-12-overview-html-export): the overview, every anchored file at its
// frozen version with its diff, no notes. name is <slug>.html.
func (s *Service) ExportOverviewHTML(ctx context.Context, src OverviewSource) ([]byte, string, error) {
	var (
		e       overviewhtml.Export
		doc     markdown.Doc
		anchors []agentdocs.Anchor
		ok      []bool // per anchor: may it open a file (before the file reads)
		rd      exportReader
	)
	if src.ReviewID != "" {
		ov, err := s.ReviewOverview(ctx, src.ReviewID)
		if err != nil {
			return nil, "", err
		}
		r, err := s.Review(ctx, src.ReviewID)
		if err != nil {
			return nil, "", err
		}
		doc = ov.Doc
		for _, a := range ov.Anchors {
			anchors, ok = append(anchors, a.Anchor), append(ok, a.OK)
		}
		e.Kind, e.Title = "review", "Review overview: "+reviewTitleLabel(r)
		rd = exportReader{
			read: func(p string) ([]byte, error) { return s.overviewFileBytes(ctx, r, p) },
			diff: func(p string) (ReviewFileDiff, error) { return s.ReviewFileDiff(ctx, src.ReviewID, p) },
		}
	} else {
		doc, anchors = agentdocs.ParseOverview(src.Text)
		for i, a := range anchors {
			if a.Note != "" {
				sp, found := src.NoteSpans[a.Note]
				if found {
					anchors[i].Path, anchors[i].Start, anchors[i].End = sp.Path, sp.Start, sp.End
				}
				ok = append(ok, found)
				continue
			}
			ok = append(ok, a.Path != "")
		}
		e.Kind, e.Title = "agent", src.Title
		rd = exportReader{
			read: func(p string) ([]byte, error) { return s.WorktreeFile(ctx, p) },
			diff: func(p string) (ReviewFileDiff, error) { return s.WorkingFileDiff(ctx, p) },
		}
	}
	e.Blocks = doc.Blocks
	files := map[string]*overviewhtml.File{}
	lineCount := map[string]int{}
	var order []string
	for i, a := range anchors {
		if !ok[i] || a.Path == "" {
			continue
		}
		if _, seen := files[a.Path]; seen {
			continue
		}
		f, n, err := s.exportFile(rd, a.Path, &e)
		if err != nil {
			continue // gone: its anchors are marked missing below
		}
		files[a.Path], lineCount[a.Path] = f, n
		order = append(order, a.Path)
	}
	for i, a := range anchors {
		f := files[a.Path]
		missing := !ok[i] || f == nil
		if !missing && a.Start > 0 && f.Placeholder == "" {
			missing = max(a.Start, a.End) > lineCount[a.Path]
		}
		e.Anchors = append(e.Anchors, overviewhtml.Anchor{Dest: a.Dest, Path: a.Path, Start: a.Start, End: a.End, Label: a.Label, Missing: missing})
	}
	for _, p := range order {
		e.Files = append(e.Files, *files[p])
	}
	e.Repo, _ = s.RepoName(ctx)
	e.ExportedAt = clock.Now().Format("2006-01-02 15:04")
	e.Version = buildinfo.String()
	page, err := overviewhtml.Render(e)
	if err != nil {
		return nil, "", err
	}
	return page, overviewhtml.Slug(e.Title) + ".html", nil
}

// exportFile reads one anchored path: its lines (with syntax runs when
// highlighting is on) and its diff; a binary or too-large file is a
// placeholder. n is the line count (0 for a placeholder). The first diff's
// side labels become the page's range.
func (s *Service) exportFile(rd exportReader, path string, e *overviewhtml.Export) (*overviewhtml.File, int, error) {
	data, err := rd.read(path)
	if err != nil {
		return nil, 0, err
	}
	f := &overviewhtml.File{Path: path}
	if d, derr := rd.diff(path); derr == nil {
		f.Status, f.OldPath, f.Changed = d.Status, d.OldPath, d.Changed
		if e.Left == "" {
			e.Left, e.Right = d.Left, d.Right
		}
		if d.Changed && !d.Diff.Binary && !d.Diff.TooLarge {
			f.Diff = overviewhtml.Rows(d.Diff.Result.Rows, d.Diff.OldTok, d.Diff.NewTok)
		}
	}
	switch {
	case len(data) > MaxDiffBytes:
		f.Placeholder = fmt.Sprintf("%s is too large to include (%d bytes)", path, len(data))
		return f, 0, nil
	case IsBinary(data):
		f.Placeholder = fmt.Sprintf("%s is a binary file (%d bytes)", path, len(data))
		return f, 0, nil
	}
	lines, _ := splitTextLines(data)
	var tok [][]syntax.Tok
	if s.SyntaxHighlighting() && len(data) <= MaxSyntaxBytes && !HasBareCR(data) {
		if lang := syntax.Detect(path); lang != "" {
			tok = syntax.Lex(lang, data)
		}
	}
	f.Lines = overviewhtml.Lines(lines, tok)
	return f, len(lines), nil
}

// reviewTitleLabel names a review the way the TUI's reviewLabel does.
func reviewTitleLabel(r Review) string {
	switch {
	case r.Kind == ReviewOnWorktree:
		return "working changes"
	case r.Preview != "":
		return NoteScopeLabel(r.Preview)
	case r.Branch != "":
		return r.Branch + " " + reviewShort(r.Commit)
	}
	return reviewShort(r.Commit)
}

```

Verify `splitTextLines`'s signature (`grep -n "func splitTextLines" internal/domain/*.go`) — it returns `([]string, bool)`; ignore the bool here (a file with a bare CR still has lines).

- [ ] **Step 4: Run** `go test ./internal/domain/ -run 'TestExportStored|TestReviewOverview'` — Expected: PASS.

- [ ] **Step 5: Commit** — `domain: ExportOverviewHTML for stored review overviews (files at the tip + base→tip diffs, no notes)`.

---

### Task 5: Domain builder — agent overviews

**Files:**
- Modify: `internal/domain/overview_export.go`
- Test: `internal/domain/overview_export_test.go`

**Interfaces:**
- Produces: `func AgentOverviewSource(docs *agentdocs.Store, id, root string) (OverviewSource, error)` — `ErrNoAgentOverview` (`errors.New("no such overview")`) when `id` is not in `docs` under `root`.

- [ ] **Step 1: Write the failing tests** (append):

```go
func TestExportAgentOverview(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	commitFile(t, dir, "a.txt", "one\ntwo\nthree\n", "add a")
	commitFile(t, dir, "img.bin", "\x00\x01\x02", "add bin")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTWO\nthree\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	text := "Look at [two](a.txt:2), [the note](note:t1), [gone](nope.txt:1), [bin](img.bin), [far](a.txt:40)."
	page, name, err := svc.ExportOverviewHTML(context.Background(), OverviewSource{
		Title: "My investigation", Text: text,
		NoteSpans: map[string]NoteSpan{"t1": {Path: "a.txt", Start: 3, End: 4}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if name != "my-investigation.html" {
		t.Errorf("name = %q", name)
	}
	e := exportBlob(t, page)
	if e.Kind != "agent" || e.Left != "HEAD" || e.Right != "working tree" {
		t.Errorf("kind/range = %q %q %q", e.Kind, e.Left, e.Right)
	}
	got := map[string]overviewhtml.Anchor{}
	for _, a := range e.Anchors {
		got[a.Dest] = a
	}
	if a := got["note:t1"]; a.Missing || a.Path != "a.txt" || a.Start != 3 || a.End != 4 {
		t.Errorf("note anchor = %+v", a)
	}
	if !got["nope.txt:1"].Missing || !got["a.txt:40"].Missing || got["a.txt:2"].Missing || got["img.bin"].Missing {
		t.Errorf("missing flags = %+v", got)
	}
	if len(e.Files) != 2 || e.Files[0].Path != "a.txt" || !e.Files[0].Changed || len(e.Files[0].Lines) != 4 {
		t.Fatalf("files = %+v", e.Files)
	}
	if e.Files[1].Placeholder == "" || e.Files[1].Changed {
		t.Error("img.bin must be an unchanged placeholder (D says \"unchanged in this range\")")
	}
}

func TestAgentOverviewSourceResolvesNotes(t *testing.T) {
	t.Parallel()
	docs := agentdocs.New()
	n, err := docs.AddNote("r", "a.txt", []string{"two", "three"}, 2, 3, "look", "", "")
	if err != nil {
		t.Fatal(err)
	}
	o, err := docs.AddOverview("r", "/w", "Tour", "[n](note:"+n.ID+") and [a](a.txt:1)")
	if err != nil {
		t.Fatal(err)
	}
	src, err := AgentOverviewSource(docs, o.ID, "r")
	if err != nil || src.Title != "Tour" || src.Text != o.Text {
		t.Fatalf("src = %+v %v", src, err)
	}
	if got := src.NoteSpans[n.ID]; got != (NoteSpan{Path: "a.txt", Start: 2, End: 3}) {
		t.Errorf("note span = %+v", got)
	}
	if _, err := AgentOverviewSource(docs, o.ID, "other"); err != ErrNoAgentOverview {
		t.Errorf("another root: %v", err)
	}
	if _, err := AgentOverviewSource(docs, "f999999", "r"); err != ErrNoAgentOverview {
		t.Errorf("unknown id: %v", err)
	}
}
```

(`AddNote(root, path, lines, start, end, summary, rationale, author)` and `AddOverview(root, dir, title, text)` are agentdocs' signatures; if `AddNote` rejects lines that do not match a file, it does not read disk — it takes `lines` as given.)

(Imports for the test file: `os`, `path/filepath`, `agentdocs`.)

- [ ] **Step 2: Run** `go test ./internal/domain/ -run 'TestExportAgent|TestAgentOverviewSource'` — Expected: FAIL.

- [ ] **Step 3: Implement** — `ExportOverviewHTML` already handles the agent branch (Task 4); add:

```go
// ErrNoAgentOverview: no overview with that id in this worktree.
var ErrNoAgentOverview = errors.New("no such overview")

// AgentOverviewSource is overview id's export source from the in-memory
// store a TUI or gg web holds, its note anchors resolved there; root is
// the caller's CheckoutKey.
func AgentOverviewSource(docs *agentdocs.Store, id, root string) (OverviewSource, error) {
	o, ok := docs.Overview(id)
	if !ok || o.Root != root {
		return OverviewSource{}, ErrNoAgentOverview
	}
	src := OverviewSource{Title: o.Title, Text: o.Text, NoteSpans: map[string]NoteSpan{}}
	for _, a := range o.Anchors {
		if a.Note == "" {
			continue
		}
		if n, found := docs.FindNote(a.Note); found && n.Root == root {
			src.NoteSpans[a.Note] = NoteSpan{Path: n.Path, Start: n.Start, End: n.End}
		}
	}
	return src, nil
}
```

Add `"errors"` to the file's imports.

- [ ] **Step 4: Run** `go test ./internal/domain/ -run 'TestExport|TestAgentOverviewSource|TestReviewOverview|TestWorkingFileDiff'` — Expected: PASS.

- [ ] **Step 5: Commit** — `domain: export an agent overview (working tree + HEAD→disk diffs; note anchors resolved by the caller)`.

---

### Task 6: Web — `GET /api/overview-export` + `E` in the viewer

**Files:**
- Create: `internal/web/overview_export_http.go`
- Test: `internal/web/overview_export_http_test.go`
- Modify: `internal/web/static/viewer.js` (`overviewKey`, `splitOverviewKey`, `viewerFoot`, the `data-vact` switch)

**Interfaces:**
- Consumes: `domain.ExportOverviewHTML`, `domain.AgentOverviewSource`, `s.docs`, `s.docsRoot(ctx)`, `s.service()`, `readCtx`, `writeErr`, `fileIDRE`.
- Produces: `GET /api/overview-export?id=f<n>` | `?review=<id>` → `200 text/html` with `Content-Disposition: attachment; filename="<name>"`; `400` bad query; `404` gone / no overview; `413` too large.

- [ ] **Step 1: Write the failing test** — `overview_export_http_test.go`, on the helpers `agentdocs_overview_web_test.go` uses (`noteSrv` → a server over a repo holding `f.txt`, `serve` → an `httptest.Server`):

```go
package web

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func getRaw(t *testing.T, base, path string) (*http.Response, string) {
	t.Helper()
	res, err := http.Get(base + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func TestOverviewExportAgent(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	o, err := s.docs.AddOverview(root, s.service().Root(), "My Tour", "see [here](f.txt:1)")
	if err != nil {
		t.Fatal(err)
	}
	res, body := getRaw(t, serve(t, s).URL, "/api/overview-export?id="+o.ID)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("code %d: %s", res.StatusCode, body)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("content type %q", ct)
	}
	if cd := res.Header.Get("Content-Disposition"); cd != `attachment; filename="my-tour.html"` {
		t.Errorf("disposition %q", cd)
	}
	if !strings.Contains(body, `id="gg-data"`) || !strings.Contains(body, `"path":"f.txt"`) {
		t.Errorf("body lacks the blob or the file")
	}
}

func TestOverviewExportErrors(t *testing.T) {
	t.Parallel()
	s, _ := noteSrv(t)
	base := serve(t, s).URL
	other, _ := s.docs.AddOverview("/elsewhere", "/elsewhere", "T", "x")
	for path, want := range map[string]int{
		"/api/overview-export":                     http.StatusBadRequest,
		"/api/overview-export?id=bogus":            http.StatusBadRequest,
		"/api/overview-export?id=f999999":          http.StatusNotFound,
		"/api/overview-export?id=" + other.ID:      http.StatusNotFound,
		"/api/overview-export?review=deadbeef":     http.StatusNotFound,
		"/api/overview-export?id=f1&review=x":      http.StatusBadRequest,
	} {
		if res, body := getRaw(t, base, path); res.StatusCode != want {
			t.Errorf("%s: %d, want %d (%s)", path, res.StatusCode, want, body)
		}
	}
}
```

(If `noteSrv`'s server is guarded by a Host/Origin check that `http.Get` trips, use the request helper `getJSON` wraps — `grep -n "func getJSON" -A15 internal/web/*_test.go` — and read the body raw the same way.)

- [ ] **Step 2: Run** `go test ./internal/web/ -run TestOverviewExport` — Expected: FAIL (404 from the mux).

- [ ] **Step 3: Implement** `overview_export_http.go`:

```go
package web

import (
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/overviewhtml"
)

// GET /api/overview-export?id=f7 | ?review=<id>: the overview as one
// self-contained HTML file, sent as a download (spec
// 2026-10-12-overview-html-export).
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/overview-export", s.handleOverviewExport)
	})
}

func (s *Server) handleOverviewExport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, review := q.Get("id"), q.Get("review")
	ctx := readCtx(r)
	var src domain.OverviewSource
	switch {
	case id != "" && review == "" && fileIDRE.MatchString(id):
		var err error
		if src, err = domain.AgentOverviewSource(s.docs, id, s.docsRoot(r.Context())); err != nil {
			writeErr(w, http.StatusNotFound, errors.New("no overview "+id))
			return
		}
	case review != "" && id == "" && isGitArgSafe(review):
		src = domain.OverviewSource{ReviewID: review}
	default:
		writeErr(w, http.StatusBadRequest, errors.New("want ?id=<overview id> or ?review=<review id>"))
		return
	}
	page, name, err := s.service().ExportOverviewHTML(ctx, src)
	var tl *overviewhtml.TooLargeError
	switch {
	case errors.Is(err, domain.ErrNoOverview), errors.Is(err, domain.ErrReviewNotFound):
		writeErr(w, http.StatusNotFound, err)
		return
	case errors.As(err, &tl):
		writeErr(w, http.StatusRequestEntityTooLarge, err)
		return
	case err != nil:
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="`+name+`"`) // Slug: [a-z0-9-] only
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	_, _ = w.Write(page)
}
```

(`isGitArgSafe` is the web's existing guard used by `handleFileContent`; confirm review ids pass it — they are hex/ids; if a review id format fails it, use a `^[A-Za-z0-9_.-]{1,80}$` regexp instead.)

`viewer.js` — add near `copyAnchorRef`:

```js
// exportOverviewHTML downloads the overview on screen as one self-contained
// HTML file (spec 2026-10-12-overview-html-export): E, and the foot chip.
async function exportOverviewHTML() {
  const ov = split ? split.ov : view.ov;
  if (!ov) return;
  const q = ov.stored ? "review=" + encodeURIComponent(String(ov.stored.id).replace(/^review-overview:/, "")) : "id=" + encodeURIComponent(split ? split.id : view.id);
  opLine("exporting the overview…", false);
  let res;
  try {
    res = await fetch("/api/overview-export?" + q);
  } catch (e) {
    return opLine("export failed: " + (e.message || e), true);
  }
  if (!res.ok) {
    let msg = res.statusText;
    try { msg = (await res.json()).error || msg; } catch {}
    return opLine("export failed: " + msg, true);
  }
  const name = (/filename="([^"]+)"/.exec(res.headers.get("Content-Disposition") || "") || [])[1] || "overview.html";
  const url = URL.createObjectURL(await res.blob());
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10000);
  opLine("exported " + name, false);
}
```

(Check `writeErr`'s JSON key with `grep -n "func writeErr" -A6 internal/web/*.go` and read that key instead of `.error` if it differs.)

Keys: in `overviewKey` and `splitOverviewKey` add `case "E": exportOverviewHTML(); break;`. Foot: in `viewerFoot`, add `<button data-vact="xhtml">E export html</button>` before the `esc close` / `esc background` chip in the three overview branches (`split && split.left`, `view.ov && view.ov.stored`, `view.ov`). In the `data-vact` switch add `case "xhtml": exportOverviewHTML(); break;`. Help: add the `E` line wherever the viewer's overview keys are listed in the `?` help (`grep -n "copy text\|y copy" internal/web/static/*.js internal/web/static/index.html`).

- [ ] **Step 4: Run** `go test ./internal/web/` — Expected: PASS (new tests + the existing static/JS guards, e.g. `markdownjs_test.go`).

- [ ] **Step 5: Commit** — `web: E exports the overview as one HTML file (GET /api/overview-export)`.

---

### Task 7: CLI — `gg review export` and `gg session overview export`

**Files:**
- Create: `internal/cli/review_export.go`, `internal/cli/review_export_test.go`
- Modify: `internal/cli/review.go` (route `export`), `internal/cli/session_overview.go` (route + usage), `internal/cli/session_overview_test.go`

**Interfaces:**
- Consumes: `reviewArgID`, `runOperation`, `cliDecider`, `finish`, `engine.ExportFile`, `engine.ErrWriteCancelled`, `steerOverviews`, `steerNotes`, `overviewCaller`, `domain.ExportOverviewHTML`.
- Produces: `gg review export [--out <path>|-] [--force] <review-link|id|latest>`; `gg session overview export <f-id> [--out <path>|-] [--force]`.

- [ ] **Step 1: Write the failing tests.**

`review_export_test.go` (helpers from `review_show_test.go`: `gitRun`, `runGit`, `openCLIService`, `runCLI`):

```go
package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

const exportReviewDoc = `{"version":1,"summary":"looks fine","overview":"Start at [line two](a.txt:2).",` +
	`"files":[{"path":"a.txt","annotations":[{"newRange":[1,2],"summary":"two lines"}]}]}`

// exportRepo is reviewedRepo with an overview in the stored review.
func exportRepo(t *testing.T, doc string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "config", "user.name", "t")
	gitRun(t, dir, "config", "user.email", "t@t")
	for _, body := range []string{"a\nb\nc\n", "A\nB\nc\n"} {
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", ".")
		gitRun(t, dir, "commit", "-q", "-m", "c")
	}
	head := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
	svc := openCLIService(t, dir)
	tg := domain.ReviewTarget{Kind: domain.ReviewRange, Range: head + "^.." + head, Commit: head}
	id, _, err := svc.SaveReview(context.Background(), domain.SaveReview{Target: tg, Agent: "Claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	return dir, id
}

func TestReviewExportToStdoutAndFile(t *testing.T) {
	t.Parallel()
	dir, _ := exportRepo(t, exportReviewDoc)
	code, page, errb := runCLI(t, dir, "review", "export", "--out", "-", "latest")
	if code != 0 || !strings.Contains(page, `id="gg-data"`) || !strings.Contains(page, `"path":"a.txt"`) || strings.Contains(page, "two lines") {
		t.Fatalf("stdout export = %d %q", code, errb)
	}
	out := filepath.Join(t.TempDir(), "x.html")
	if code, _, errb := runCLI(t, dir, "review", "export", "--out", out, "latest"); code != 0 {
		t.Fatalf("file export = %d %q", code, errb)
	}
	if b, err := os.ReadFile(out); err != nil || len(b) == 0 {
		t.Fatalf("file: %v", err)
	}
	if err := os.WriteFile(out, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errb := runCLI(t, dir, "review", "export", "--out", out, "latest"); code != 2 || !strings.Contains(errb, "--force") {
		t.Fatalf("no --force = %d %q", code, errb)
	}
	if code, _, errb := runCLI(t, dir, "review", "export", "--force", "--out", out, "latest"); code != 0 {
		t.Fatalf("--force = %d %q", code, errb)
	}
}

func TestReviewExportWithoutOverview(t *testing.T) {
	t.Parallel()
	dir, id := reviewedRepo(t) // showReviewDoc has no overview
	if code, _, errb := runCLI(t, dir, "review", "export", "--out", "-", id); code != 1 || !strings.Contains(errb, "has no overview") {
		t.Fatalf("= %d %q", code, errb)
	}
}
```

Append to `session_overview_test.go` (helpers there: `livePresence`, `runSessionIn`; the fake answers BOTH commands the export posts):

```go
func TestSessionOverviewExport(t *testing.T) {
	t.Parallel()
	repo, _ := exportRepo(t, exportReviewDoc) // any repo holding a.txt
	inbox := t.TempDir()
	livePresence(t, inbox)
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		for {
			select {
			case <-done:
				return
			default:
			}
			for _, c := range steer.Drain(inbox) {
				r := steer.Reply{ID: c.ID, OK: true}
				switch c.Cmd {
				case "overview_show":
					r.Overviews = []steer.Overview{{ID: "f7", Title: "T", Text: "[n](note:t1) [a](a.txt:1)"}}
				case "note_list":
					r.Notes = []steer.FileNote{{ID: "t1", Path: "a.txt", Start: 2, End: 2}}
				}
				_ = steer.PostReply(inbox, r)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	var out, errb bytes.Buffer
	code := runSessionIn(inbox, openCLIService(t, repo), []string{"overview", "export", "f7", "--out", "-"}, nil, &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	page := out.String()
	if !strings.Contains(page, `"dest":"note:t1","path":"a.txt","start":2,"end":2`) || strings.Contains(page, `"missing":true`) {
		t.Fatalf("note anchor not resolved:\n%s", page[:min(len(page), 400)])
	}
}
```

(Add imports `bytes`, `time`, `steer` to `session_overview_test.go` as needed. The anchor's JSON field order follows `overviewhtml.Anchor`: dest, path, start, end, label, missing.)

- [ ] **Step 2: Run** `go test ./internal/cli/ -run 'TestReviewExport|TestSessionOverviewExport'` — Expected: FAIL.

- [ ] **Step 3: Implement.**

`review.go`, beside `show`/`save`:

```go
	if len(rest) > 0 && rest[0] == "export" {
		return reviewExport(svc, rest[1:], stdout, stderr)
	}
```

`review_export.go`:

```go
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

const reviewExportUsage = "usage: gg review export [--out <path>|-] [--force] <review-link|id|latest>"

// reviewExport writes a stored review's overview as one self-contained HTML
// file: the overview, its anchored files at the tip and their diffs, no notes.
func reviewExport(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "", "write here (default: the export dir); - = stdout")
	force := fs.Bool("force", false, "overwrite an existing file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, reviewExportUsage)
		return 2
	}
	ctx := context.Background()
	svc, id, _, code := reviewArgID(ctx, svc, fs.Arg(0), stderr)
	if code != 0 {
		return code
	}
	page, name, err := svc.ExportOverviewHTML(ctx, domain.OverviewSource{ReviewID: id})
	if errors.Is(err, domain.ErrNoOverview) {
		fmt.Fprintf(stderr, "review %s has no overview\n", id)
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return writeExport(ctx, svc, "review export", page, name, *out, *force, stdout, stderr)
}

// writeExport writes page to out ("-" = stdout; "" = <export dir>/<name>)
// through engine.ExportFile: an existing different file needs --force.
func writeExport(ctx context.Context, svc *domain.Service, verb string, page []byte, name, out string, force bool, stdout, stderr io.Writer) int {
	if out == "-" {
		_, _ = stdout.Write(page)
		return 0
	}
	target := out
	if target == "" {
		dir, err := svc.ExportDefaultDir(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", verb, err)
			return 1
		}
		target = filepath.Join(dir, name)
	}
	if abs, err := filepath.Abs(target); err == nil {
		target = abs
	}
	policy := map[string]string{"overwrite": "cancel"}
	if force {
		policy["overwrite"] = "overwrite"
	}
	res, err := runOperation(ctx, svc, engine.ExportFile{Path: target, Data: page}, cliDecider{policy: policy}, stderr)
	if errors.Is(err, engine.ErrWriteCancelled) {
		fmt.Fprintf(stderr, "%s: %s already exists; pass --force to overwrite\n", verb, target)
		return 2
	}
	return finish(res, err, stdout, stderr)
}
```

`session_overview.go`: add `"       gg session overview export <id> [--out <path>|-] [--force]"` to `overviewUsage`, `case "export": return sessionOverviewExport(dir, svc, args[1:], stdout, stderr)` to the switch, and:

```go
// sessionOverviewExport writes an agent overview as one HTML file: its text
// from the live session, its note anchors' lines from the session's notes,
// the files read here (the caller's worktree is the overview's).
func sessionOverviewExport(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session overview export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "", "write here (default: the export dir); - = stdout")
	force := fs.Bool("force", false, "overwrite an existing file")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || !fileIDPattern.MatchString(pos[0]) {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	r, code, ok := steerOverviews(dir, steer.Command{Cmd: "overview_show", FileID: pos[0], Worktree: overviewCaller(svc)}, stdout, stderr)
	if !ok {
		return code
	}
	if len(r.Overviews) != 1 {
		fmt.Fprintln(stderr, "no overview "+pos[0])
		return 1
	}
	o := r.Overviews[0]
	src := domain.OverviewSource{Title: o.Title, Text: o.Text, NoteSpans: map[string]domain.NoteSpan{}}
	if nr, _, nok := steerNotes(dir, steer.Command{Cmd: "note_list"}, io.Discard, stderr); nok {
		for _, n := range nr.Notes {
			src.NoteSpans[n.ID] = domain.NoteSpan{Path: n.Path, Start: n.Start, End: n.End}
		}
	}
	ctx := context.Background()
	page, name, err := svc.ExportOverviewHTML(ctx, src)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return writeExport(ctx, svc, "session overview export", page, name, *out, *force, stdout, stderr)
}
```

(Check `steerNotes`' signature with `grep -n "func steerNotes" internal/cli/*.go` and match it. A failed note list leaves note anchors plain — the export still succeeds.)

- [ ] **Step 4: Run** `go test ./internal/cli/` — Expected: PASS.

- [ ] **Step 5: Commit** — `cli: gg review export / gg session overview export write the overview as one HTML file`.

---

### Task 8: TUI — `.` menu row "Export as HTML"

**Files:**
- Create: `internal/tui/overview_export.go`, `internal/tui/overview_export_test.go`
- Modify: `internal/tui/overview_keys.go` (`overviewRows`, `splitOverviewRows`), `internal/tui/export_patch.go` (title field), `internal/tui/model.go:1529-1536`, `internal/i18n/{ja,ko,zh,ru}.toml`

**Interfaces:**
- Consumes: `domain.AgentOverviewSource`, `domain.ExportOverviewHTML`, `svc.ExportDefaultDir`, `m.docs`, `domain.CheckoutKey(m.currentWorktree)`, `openFile.src.kind` (`srcOverview` / `srcReviewOverview`), `openFile.src.rev` (review id), `openFile.id()`.
- Produces: `actionRow{id: "overview-export"}` in both menus; `patchResolvedMsg.title` / `exportPatchPopup.title` (empty = "Export as patch").

- [ ] **Step 1: Write the failing test** — `overview_export_test.go`, on the fixtures `overview_keys_test.go` (`tourModel`: an agent overview "Tour" in the viewer) and `split_overview_test.go` (`reviewSplitModel`: a stored overview in a split) already use; `rowByID` is in `action_menu.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/i18n"
)

// runExportRow runs the overview-export row and returns its resolved message.
func runExportRow(t *testing.T, m Model, rows []actionRow) patchResolvedMsg {
	t.Helper()
	row, ok := rowByID(rows, "overview-export")
	if !ok || row.label != i18n.T("Export as HTML") {
		t.Fatalf("no export row in %d rows", len(rows))
	}
	_, cmd := row.run(m)
	if cmd == nil {
		t.Fatal("the row ran no command")
	}
	msg, ok := cmd().(patchResolvedMsg)
	if !ok {
		t.Fatalf("msg = %T", cmd())
	}
	return msg
}

func TestOverviewMenuExportsAnAgentOverview(t *testing.T) {
	t.Parallel()
	m, _, _ := tourModel(t)
	msg := runExportRow(t, m, m.overviewRows())
	if msg.err != nil || msg.title != i18n.T("Export overview as HTML") ||
		!strings.HasSuffix(msg.defaultPath, "tour.html") || !strings.Contains(string(msg.data), `id="gg-data"`) {
		t.Fatalf("msg = err %v title %q path %q", msg.err, msg.title, msg.defaultPath)
	}
	nm, _ := m.Update(msg)
	p, ok := nm.(Model).topLayer().(*exportPatchPopup)
	if !ok || !strings.Contains(p.render(nm.(Model), ""), i18n.T("Export overview as HTML")) {
		t.Fatalf("top layer = %T", nm.(Model).topLayer())
	}
}

func TestOverviewMenuExportsAStoredOverview(t *testing.T) {
	t.Parallel()
	m, _ := reviewSplitModel(t)
	msg := runExportRow(t, m, m.overviewRows())
	if msg.err != nil || !strings.HasPrefix(filepath.Base(msg.defaultPath), "review-overview-") {
		t.Fatalf("msg = err %v path %q", msg.err, msg.defaultPath)
	}
}
```

(Add `path/filepath` to the imports. If `topLayer()` is not how the model exposes the pushed popup, use the accessor `export_patch_test.go` uses: `grep -n "exportPatchPopup" internal/tui/*_test.go`.)

- [ ] **Step 2: Run** `go test ./internal/tui/ -run TestOverviewMenuExportsHTML` — Expected: FAIL.

- [ ] **Step 3: Implement.**

`export_patch.go`: add `title string` to `patchResolvedMsg` and `exportPatchPopup`; in `render`:

```go
	title := p.title
	if title == "" {
		title = i18n.T("Export as patch")
	}
	b.WriteString(title + "\n\n")
```

`model.go` `case patchResolvedMsg:` → `p := &exportPatchPopup{data: msg.data, title: msg.title}`; the error status keeps "export patch: %s" for patches and uses `i18n.T("export: %s", …)` when `msg.title != ""`.

`overview_export.go`:

```go
package tui

import (
	"context"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// overviewExportRow is the . menu's "Export as HTML" for overview d (spec
// 2026-10-12-overview-html-export): the page is built off-thread, then the
// export path popup asks where to write it (engine.ExportFile).
func (m Model) overviewExportRow(d *openFile) actionRow {
	return actionRow{id: "overview-export", label: i18n.T("Export as HTML"), run: func(m Model) (tea.Model, tea.Cmd) {
		var src domain.OverviewSource
		if d.src.kind == srcReviewOverview {
			src = domain.OverviewSource{ReviewID: d.src.rev}
		} else {
			if m.docs == nil {
				return m, nil
			}
			s, err := domain.AgentOverviewSource(m.docs, d.id(), domain.CheckoutKey(m.currentWorktree))
			if err != nil {
				m.statusMsg = i18n.T("export: %s", err.Error())
				return m, nil
			}
			src = s
		}
		svc, title := m.svc, i18n.T("Export overview as HTML")
		return m, func() tea.Msg {
			ctx := context.Background()
			data, name, err := svc.ExportOverviewHTML(ctx, src)
			if err != nil {
				return patchResolvedMsg{err: err, title: title}
			}
			dir, err := svc.ExportDefaultDir(ctx)
			if err != nil {
				return patchResolvedMsg{err: err, title: title}
			}
			return patchResolvedMsg{data: data, defaultPath: filepath.Join(dir, name), title: title}
		}
	}}
}
```

`overview_keys.go`: in `overviewRows`, inside `if d.ov != nil { … }` append `m.overviewExportRow(d)` after the anchor rows; in `splitOverviewRows`, append it before `cp` (Copy overview). (Confirm `srcReviewOverview`'s `src.rev` is the review id: `reviewOf` compares `ov.src.rev == st.id` — yes.)

i18n: add to all four bundles `"Export as HTML"`, `"Export overview as HTML"`, `"export: %s"` (translations: ja `HTMLとしてエクスポート` / `概要をHTMLとしてエクスポート` / `エクスポート: %s`; ko `HTML로 내보내기` / `개요를 HTML로 내보내기` / `내보내기: %s`; zh `导出为 HTML` / `将概览导出为 HTML` / `导出：%s`; ru `Экспорт в HTML` / `Экспорт обзора в HTML` / `экспорт: %s`). Follow `adding-translations` skill for the bundle format and verb-agreement gate.

- [ ] **Step 4: Run** `go test ./internal/tui/ ./internal/i18n/` — Expected: PASS (incl. the i18n AST gates and `diff_menu_parity_test.go`).

- [ ] **Step 5: Commit** — `tui: . menu Export as HTML on an overview (single + split pane)`.

---

### Task 9: e2e scenario + browser check over `file://`

**Files:**
- Create: `e2e/scenarios/review_export_html.toml`
- Scratch (not committed): `<scratchpad>/pw/export_check.mjs`

- [ ] **Step 1: Write the e2e scenario** (`writing-e2e-scenarios` skill for the schema):

```toml
name = "review export: a stored overview becomes one self-contained HTML page — anchored files and their diffs, no notes"

[input]
steps = [
  { write = "a.txt", content = "one\ntwo\nthree\nfour\nfive\n" },
  { write = "b.txt", content = "alpha\nbeta\ngamma\n" },
  { commit = "add a and b" },
]

[[run]]
cmd   = ["review", "save", "gg://{{cwd}}@{{rev:HEAD}}", "--agent", "Claude Code", "--stdin"]
stdin = '{"version":1,"summary":"## Summary\nfine","overview":"Start at [line two](a.txt:2), then [outside](zzz.go:1).","files":[{"path":"a.txt","annotations":[{"newRange":[2,2],"summary":"why two?"}]},{"path":"b.txt","annotations":[{"newRange":[2,2],"summary":"beta?"}]}]}'
exit  = 0

[[run]]
cmd   = ["review", "export", "--out", "-", "latest"]
exit  = 0
stdout_contains = ['id="gg-data"', '"path":"a.txt"', '"dest":"zzz.go:1"', '"missing":true']
stdout_excludes = ['why two?', '"path":"b.txt"', '<script src', '/api/']
```

- [ ] **Step 2: Run** `go test ./e2e/ -run 'TestScenarios/review_export_html'` (check the exact runner name with `grep -n "func Test" e2e/*_test.go`) — Expected: PASS.

- [ ] **Step 3: Browser check (Chromium + Firefox, `file://`).** Build `go build -o bin/gg ./cmd/gg` in the worktree; in a temp repo run the scenario's two `review save` + `review export --out <scratch>/x.html latest` with `bin/gg`. Script `<scratchpad>/pw/export_check.mjs` (Playwright already cached under `~/.cache/ms-playwright`; `npm i playwright` in `<scratchpad>/pw` if the package is missing):

```js
import { chromium, firefox } from "playwright";
const file = "file://" + process.argv[2];
for (const bt of [chromium, firefox]) {
  const b = await bt.launch();
  const p = await b.newPage({ viewport: { width: 1400, height: 900 } });
  const errs = [];
  p.on("pageerror", (e) => errs.push(String(e)));
  p.on("request", (r) => { if (!r.url().startsWith("file:") && !r.url().startsWith("data:")) errs.push("network: " + r.url()); });
  await p.goto(file);
  const band = p.locator("#viewer-body .vline.vrange");
  if (!(await band.first().isVisible())) errs.push("no visible band after load");
  await p.locator("#x-ov a.md-anchor").first().click();
  if ((await band.count()) !== 1) errs.push("band count " + (await band.count()));
  await p.keyboard.press("Shift+D");
  if (!(await p.locator("#viewer-body .vline.vd-add, #viewer-body .vline.vd-same").first().isVisible())) errs.push("diff not visible after D");
  if ((await p.locator("#x-ov a.md-anchor").count()) !== 1) errs.push("the missing anchor must render plain");
  await p.screenshot({ path: process.argv[2] + "." + bt.name() + ".png" });
  await p.setViewportSize({ width: 700, height: 900 });
  if (!(await p.locator("#x-code").isVisible())) errs.push("narrow: code pane hidden");
  console.log(bt.name(), errs.length ? errs : "OK");
  await b.close();
}
```

Run `node export_check.mjs <scratch>/x.html`; Expected: `chromium OK`, `firefox OK`. Read both PNGs to confirm the split looks like gg web's. **Watch it fail:** comment out the `paintCode();` call inside `openAnchor` in `export.js` (an uncommitted edit), rebuild, re-export, run the script — it must report "no visible band after load" for both browsers; then undo the edit BY HAND (Edit tool — never `git checkout --`), rebuild, re-export, and re-run to `OK`.

- [ ] **Step 4: Commit** — `e2e: review export writes a self-contained overview page`.

---

### Task 10: Docs + skill + full gate

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md` (package map rows `overviewhtml`, `web` mention of `web/static`), `docs/CLAUDE-details.md` (new "Overview HTML export" section: data shape, scopeModule rule, R1–R3, amendments), `internal/agentskill/using-gg.md` (+ `agentskill.Version` bump), `docs/superpowers/specs/2026-10-12-overview-html-export-design.md` (fold in the amendments)

- [ ] **Step 1:** CHANGELOG entry under the unreleased section: "Export an overview as one self-contained HTML file — gg web `E`, TUI `.` menu Export as HTML, `gg review export`, `gg session overview export`; anchored files + their diffs, no notes; works offline from file://."
- [ ] **Step 2:** README: one paragraph under the overview / review section with the three entry points.
- [ ] **Step 3:** `using-gg.md`: document `gg review export [--out <path>|-] [--force] <id|latest>` and `gg session overview export <f-id> [--out …]` ("hand the user a shareable page of your overview"); bump `agentskill.Version`; run `go test ./internal/agentskill/`.
- [ ] **Step 4:** CLAUDE.md map: add row `| \`overviewhtml\` | Pure overview → one self-contained HTML page: the export data shape (gg web's file/diff wire rows), \`Render\` inlines web/static's style.css + markdown.js + splitdiff.js (as block-scoped classic scripts) + its own split-view script and the JSON blob; 16 MB cap. DAG leaf (markdown, syntax, textdiff, web/static). |` and note in the `web` row that the embedded page is `web/static` (a package of its own). CLAUDE-details: the section above.
- [ ] **Step 5:** Spec: apply the seven amendments to the spec text.
- [ ] **Step 6: Full gate.** `./test.sh` then `./test.sh race` (start the race gate early; green ONLY when the log says "all green" — `| tail` exits 0 on FAIL).
- [ ] **Step 7: Commit** — `docs: overview HTML export (changelog, readme, skill v<N>, package map, details)`.
- [ ] **Step 8:** Final whole-branch review by a read-only review subagent on the most capable model; fix findings in this session; then ask the user before merging (`gg merge -F <msgfile> --into main feat/overview-html-export`), then `./build.sh install`, then `./build.sh web`.
