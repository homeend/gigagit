# Open files on the web — plan 5a: the viewer — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans (inline, THIS session — the repo forbids subagents). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `gg web` shows a file's content (working tree, commit, shelf) in a full-page viewer overlay with a line cursor, search and a `.` menu, and content links land in it (steered navigate, `gg open --web`, a link pasted into `#`).

**Architecture:** One server read `GET /api/file-content` (domain reads + the syntax lexer, the `/api/blame` token shape) and one link read `GET /api/link-command` (linknav → `toSteerWire`). A new page module `static/viewer.js` owns the overlay (a `mountOverlay` layer, `pushLayer("viewer")`), with a pure, node-tested section for the line model. Entry points are menu contributors (`registerRows`) so `files.js`/`sidebar.js` barely change.

**Tech Stack:** Go 1.26 (`internal/web`), vanilla ES modules in `internal/web/static`, node for pure-JS tests, playwright for the browser check.

**Spec:** `docs/superpowers/specs/2026-09-26-open-files-web-design.md` (plan 5a row).

## Global Constraints

- Paths in titles/rows are cut in the MIDDLE (the page's existing `elidePath`-style helper — `filepathelidejs_test.go` pins it), never the file name.
- Key hints live in the app's bottom bar (`#foot`): the viewer overlay leaves `#foot` visible and swaps its chips while it is on top — no hint line inside the box.
- Every wire value through `isGitArgSafe` → 400; every JSON error via `writeErr`.
- Protocol/op-line prose is English (the web is not localized).
- New Go tests call `t.Parallel()` unless they need `isolateState(t)`.
- Browser checks must ASSERT VISIBILITY (computed style / bounding box), and run against the unfixed build first.
- After the merge: `./build.sh install` and `./build.sh web`.

## Rulings (planning — the user reviews these)

| # | Ruling | Why | Cost if wrong |
|---|--------|-----|---------------|
| W1 | `/api/file-content` wire: `{lines:[{text,tok?}], too_large?, missing?}`; `src` ∈ `worktree` (default) \| `commit` (rev = sha/rev) \| `shelf` (rev = entry id; bytes via `ShelfBlob`) | the blame endpoint's token shape, so `renderCell` paints it unchanged | a field rename |
| W2 | Over `domain.MaxDiffBytes` → `too_large:true`, no lines (the TUI's "(file too large to preview)"); a working-tree file that is gone → `missing:true` (the viewer shows `(file deleted on disk)`); lexing only when `SyntaxHighlighting()` and ≤ `MaxSyntaxBytes` and no bare CR | the TUI's rules | none |
| W3 | `#`'s prompt accepts a `gg://` link: `GET /api/link-command?link=` resolves it (linknav, this server's registry) and answers `{steer}` (a `toSteerWire` command the page runs through `steerNavigate`) or `{checkout}` when it names another checkout (op line: `that link is in <checkout> — open gg web there`) or 422 with the reason. EVERY link kind lands this way, not only content links | the page already knows how to land every steer shape; refusing the rest would be arbitrary | none |
| W4 | *view file* rows: the status list (not for a staged-only `D`/deleted file — no bytes on disk), the commit/compare list (version = `hereRev`, the row's own rev; omitted when there is none), shelf FILE entries (`e.path` set). Commit-kind shelf entries' frozen files are left for a later plan | these are the three lists the spec names | one more menu later |
| W5 | Viewer `.` menu Copy file link: a working-tree version copies the content link (at the cursor line) after the existing `/api/worktree-present` check; a commit/shelf version copies it only when the file's working-tree lines equal the shown lines (the page fetches `src=worktree` and compares text) — else op line `the file on disk differs from this version — no content link` | the TUI's disk-match rule | none |
| W6 | Viewer keys: `↑↓ j k` cursor, `pgup pgdn` page, `home end` (`g G`), click sets the cursor, `/ ] [` search (`bindSearchBar`), `w` long-line mode, `.` or right-click the menu, `esc` / backdrop close. `ctrl+]` is NOT bound in 5a (no list yet) | 5b adds the list | none |
| W7 | Footer: while the viewer is the top layer `#foot` shows `↑↓ line · / find · ] [ next/prev · w wrap · . menu · esc close` as chips (data-vact buttons handled in viewer.js); restored on close | "hints only in the bottom bar" | a visual tweak |
| W8 | A steered content navigate: `/api/worktree-present` first; absent → `navMiss(path + " is not in the working tree")`; else the viewer at the line (clamp notice `line N is past the end of <path> (M lines)` on the op line, the TUI's words) | the TUI's steerNavigateContent | none |
| W9 | `gg open --web <content link>` and a steered content navigate are accepted: the `toSteerWire` refusal and the CLI's `--web` refusal both go (tests that pin them flip) | the spec | none |

## Review Focus

1. **A file edited between the fetch and the Copy file link click** (W5) — the check fetches fresh working-tree lines at click time; pinned by the pure `sameLines` test + the browser check.
2. **Opening a second file while the viewer is open** — the overlay re-renders for the new file (one instance, `pushLayer` is idempotent); cursor/search reset. Pinned in Task 4's browser check.
3. **A CRLF file** — lines split on `\n` with a trailing `\r` trimmed (the blame rule); pinned in Task 1.
4. **Empty file** — `lines: []` → `(empty file)` notice, cursor keys are no-ops. Pinned in Task 3 (pure clamp on 0 lines).
5. **A pasted link to a place in THIS checkout of a shape the page cannot land** (e.g. a stash hint) — `steerNavigate` already degrades with an op line; `/api/link-command` does not special-case it.

---

### Task 1: `GET /api/file-content`

**Files:** Create `internal/web/filecontent.go`, `internal/web/filecontent_test.go`.

**Interfaces — Produces:** route `GET /api/file-content?src=&rev=&path=` → `fileContentBody{Lines []contentRow; TooLarge bool 'too_large,omitempty'; Missing bool 'missing,omitempty'}`, `contentRow{Text string 'text'; Tok []tokTriple 'tok,omitempty'}`; `func contentRows(path string, data []byte, lex bool) []contentRow`.

- [ ] **Step 1: failing tests**

```go
package web

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

type fcBody struct {
	Lines []struct {
		Text string `json:"text"`
		Tok  [][]any `json:"tok"`
	} `json:"lines"`
	TooLarge bool `json:"too_large"`
	Missing  bool `json:"missing"`
}

func TestFileContentWorktreeCommitAndMissing(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3) // f.txt: "content 3\n" at HEAD
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("disk 1\r\ndisk 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))
	var b fcBody
	if code := getJSON(t, ts, "/api/file-content?path=f.txt", &b); code != http.StatusOK ||
		len(b.Lines) != 2 || b.Lines[0].Text != "disk 1" || b.Lines[1].Text != "disk 2" {
		t.Fatalf("worktree: code=%d body=%+v", code, b)
	}
	b = fcBody{}
	if code := getJSON(t, ts, "/api/file-content?src=commit&rev=HEAD&path=f.txt", &b); code != http.StatusOK ||
		len(b.Lines) != 1 || b.Lines[0].Text != "content 3" {
		t.Fatalf("commit: code=%d body=%+v", code, b)
	}
	b = fcBody{}
	if code := getJSON(t, ts, "/api/file-content?path=gone.txt", &b); code != http.StatusOK || !b.Missing {
		t.Fatalf("missing: code=%d body=%+v", code, b)
	}
}

func TestFileContentRefusesUnsafeAndUnknown(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	for _, q := range []string{"", "?path=-x", "?path=f.txt&src=nope", "?path=f.txt&src=commit", "?path=f.txt&src=commit&rev=-n", "?path=f.txt&src=shelf"} {
		var e map[string]any
		if code := getAny(t, ts, "/api/file-content"+q, &e); code != http.StatusBadRequest {
			t.Errorf("%q: code %d, want 400", q, code)
		}
	}
}

func TestFileContentTokensAndTooLarge(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	write(t, dir, "a.go", "package a\n\nvar x = 1\n")
	big := make([]byte, domain.MaxDiffBytes+1)
	for i := range big {
		big[i] = 'x'
	}
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))
	var b fcBody
	if code := getJSON(t, ts, "/api/file-content?path=a.go", &b); code != http.StatusOK || len(b.Lines) != 3 || len(b.Lines[0].Tok) == 0 {
		t.Fatalf("a.go: code=%d body=%+v, want 3 lines with runs on line 1", code, b)
	}
	b = fcBody{}
	if code := getJSON(t, ts, "/api/file-content?path=big.txt", &b); code != http.StatusOK || !b.TooLarge || len(b.Lines) != 0 {
		t.Fatalf("big: code=%d too_large=%v lines=%d", code, b.TooLarge, len(b.Lines))
	}
}

func TestFileContentShelfEntry(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("shelved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := domain.Open(dir)
	ts := serve(t, New(svc))
	if code := postJSON(t, ts, "/api/shelf", `{"path":"wip.txt","state":"untracked"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("shelf add: %d", code)
	}
	es, err := svc.ShelfList(t.Context(), "", 0, 0)
	if err != nil || len(es) != 1 {
		t.Fatalf("list: %v %v", es, err)
	}
	var b fcBody
	if code := getJSON(t, ts, "/api/file-content?src=shelf&rev="+es[0].ID+"&path=wip.txt", &b); code != http.StatusOK ||
		len(b.Lines) != 1 || b.Lines[0].Text != "shelved" {
		t.Fatalf("shelf: code=%d body=%+v", code, b)
	}
}
```

(Check `newRepoDir`'s file name/content, `getAny`, `write` and `ShelfList`'s default-bucket argument against their definitions before running; adjust literals, not behaviour.)

- [ ] **Step 2:** `go test ./internal/web/ -run FileContent` — Expected: FAIL (404s / wrong bodies).
- [ ] **Step 3: implement** `filecontent.go`:

```go
package web

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/syntax"
)

// The viewer's read (open files, plan 5a): one file's lines at one version —
// the working tree (the bytes ON DISK), a commit, or a shelf entry — with the
// syntax runs /api/blame ships, so renderCell paints both alike.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/file-content", s.handleFileContent)
	})
}

type contentRow struct {
	Text string      `json:"text"`
	Tok  []tokTriple `json:"tok,omitempty"`
}

type fileContentBody struct {
	Lines    []contentRow `json:"lines"`
	TooLarge bool         `json:"too_large,omitempty"`
	Missing  bool         `json:"missing,omitempty"`
}

func (s *Server) handleFileContent(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path, src, rev := q.Get("path"), q.Get("src"), q.Get("rev")
	if path == "" || !isGitArgSafe(path) || (rev != "" && !isGitArgSafe(rev)) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid path/rev"))
		return
	}
	svc := s.service()
	ctx := readCtx(r)
	var data []byte
	var err error
	switch src {
	case "", "worktree":
		top, terr := svc.TopLevel(ctx)
		if terr == nil {
			if _, serr := os.Stat(filepath.Join(top, filepath.FromSlash(path))); errors.Is(serr, os.ErrNotExist) {
				writeJSON(w, fileContentBody{Lines: []contentRow{}, Missing: true})
				return
			}
		}
		data, err = svc.WorktreeFile(ctx, path)
	case "commit":
		if rev == "" {
			writeErr(w, http.StatusBadRequest, errors.New("a commit version needs rev"))
			return
		}
		data, err = svc.ShowFile(ctx, rev, path)
	case "shelf":
		if rev == "" {
			writeErr(w, http.StatusBadRequest, errors.New("a shelf version needs rev (the entry id)"))
			return
		}
		data, err = svc.ShelfBlob(ctx, rev)
	default:
		writeErr(w, http.StatusBadRequest, errors.New("unknown src "+src))
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if len(data) > domain.MaxDiffBytes {
		writeJSON(w, fileContentBody{Lines: []contentRow{}, TooLarge: true})
		return
	}
	writeJSON(w, fileContentBody{Lines: contentRows(path, data, svc.SyntaxHighlighting())})
}

// contentRows splits data into lines — "\n"-terminated, a trailing "\r"
// trimmed (CRLF), no phantom line after the last terminator — each with its
// syntax runs when lex is on and the file is lexable (the TUI's lexPreview
// rule: ≤ MaxSyntaxBytes, no bare CR).
func contentRows(path string, data []byte, lex bool) []contentRow {
	text := strings.TrimSuffix(string(data), "\n")
	rows := []contentRow{}
	if len(data) == 0 {
		return rows
	}
	var tok [][]syntax.Tok
	if lex && len(data) <= domain.MaxSyntaxBytes && !domain.HasBareCR(data) {
		if lang := syntax.Detect(path); lang != "" {
			tok = syntax.Lex(lang, data)
		}
	}
	for i, l := range strings.Split(text, "\n") {
		rows = append(rows, contentRow{Text: strings.TrimSuffix(l, "\r"), Tok: tokTriples(tok, i+1)})
	}
	return rows
}
```

(Check `tokTriples(side, no)`'s index convention in `diff.go:48` — 1-based `no` — and `readCtx`/`TopLevel` exist as used by `history.go`/`worktreepresent.go`.)

- [ ] **Step 4:** `go test ./internal/web/ -run FileContent` — Expected: PASS.
- [ ] **Step 5:** commit `feat(web): /api/file-content reads a file at a version for the viewer`.

---

### Task 2: `GET /api/link-command` and content landing on the wire

**Files:** Create `internal/web/linkcommand.go`, `internal/web/linkcommand_test.go`; modify `internal/web/steer.go` (drop the content refusal), `internal/web/steer_test.go` (flip the pinned refusal), `internal/cli/open.go` (drop `--web` content refusal), `internal/cli/link_content_test.go` (flip `TestOpenWebRefusesAContentLink` → `TestOpenWebAcceptsAContentLink` asserting the refusal text is gone: with no live page and `LaunchWeb == nil`, exit 1 `no live gg web page`).

**Interfaces — Produces:** `GET /api/link-command?link=` → `{steer: steerWire}` | `{checkout: string}` | 422 `{error}`.

- [ ] **Step 1: failing tests**

```go
func TestLinkCommandContentLinkIsASteer(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	link := "gg://" + filepath.ToSlash(dir) + "/f.txt:1?view=content"
	var b struct {
		Steer    *steerWire `json:"steer"`
		Checkout string     `json:"checkout"`
	}
	if code := getJSON(t, ts, "/api/link-command?link="+url.QueryEscape(link), &b); code != http.StatusOK ||
		b.Steer == nil || b.Steer.Cmd != "navigate" || b.Steer.File != "f.txt" || b.Steer.HintKind != "view" || b.Steer.Line != 1 {
		t.Fatalf("code=%d body=%+v steer=%+v", code, b, b.Steer)
	}
}

func TestLinkCommandOtherCheckoutAndGarbage(t *testing.T) {
	t.Parallel()
	here, there := newRepoDir(t, 1), newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(here)))
	var b struct {
		Steer    *steerWire `json:"steer"`
		Checkout string     `json:"checkout"`
	}
	link := "gg://" + filepath.ToSlash(there) + "/f.txt?view=content"
	if code := getJSON(t, ts, "/api/link-command?link="+url.QueryEscape(link), &b); code != http.StatusOK || b.Steer != nil || !domain.SamePath(b.Checkout, there) {
		t.Fatalf("other checkout: code=%d body=%+v", code, b)
	}
	var e map[string]any
	if code := getAny(t, ts, "/api/link-command?link=not-a-link", &e); code != http.StatusUnprocessableEntity {
		t.Fatalf("garbage: code %d, want 422", code)
	}
}
```

Plus in `steer_test.go`: the existing test asserting `content links are not supported in gg web yet` becomes `TestToSteerWireCarriesAContentHint` — `toSteerWire(Command{Cmd:"navigate", File:"a.txt", HintKind:"view", HintID:"content", Line:&Line{No:3}})` → no error, `w.HintKind=="view"`, `w.Line==3`.

- [ ] **Step 2:** `go test ./internal/web/ -run 'LinkCommand|ToSteerWire' && go test ./internal/cli/ -run OpenWeb` — Expected: FAIL.
- [ ] **Step 3: implement.** `steer.go`: delete the `if c.HintKind == model.ContentHintKind { … }` block. `open.go`: delete the `*web && res.Hint.Kind == model.ContentHintKind` block. `linkcommand.go`:

```go
// GET /api/link-command resolves a link pasted into the page's # prompt the
// way the TUI's # prompt does (linknav): a place in THIS checkout comes back
// as the steer command the page lands with steerNavigate; a link to another
// checkout comes back as that checkout, for the page to name.
func (s *Server) handleLinkCommand(w http.ResponseWriter, r *http.Request) {
	text := strings.TrimSpace(r.URL.Query().Get("link"))
	if text == "" {
		writeErr(w, http.StatusBadRequest, errors.New("link required"))
		return
	}
	svc := s.service()
	ctx := readCtx(r)
	res, err := linknav.Resolve(ctx, s.reposStatePath(), svc, text)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	top, err := svc.TopLevel(ctx)
	if err != nil || !domain.SamePath(top, res.Checkout) {
		writeJSON(w, map[string]string{"checkout": res.Checkout})
		return
	}
	if linknav.RepoOnly(res) {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("that link names this repository, not a place in it"))
		return
	}
	c, err := linknav.Command(ctx, svc, res)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	wire, err := toSteerWire(c)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	s.freezePair(ctx, &wire)
	writeJSON(w, map[string]any{"steer": wire})
}
```

(Route registered in `init()` like `linkbase.go`. Confirm archtest allows `web → linknav` — the map row `"linknav": {"tui","cli","mcp","web","app"}` lists web as an allowed importer.)

- [ ] **Step 4:** re-run — Expected: PASS; then `go test ./internal/web/ ./internal/cli/ ./internal/archtest/` PASS.
- [ ] **Step 5:** commit `feat(web): /api/link-command, and content links are no longer refused`.

---

### Task 3: viewer.js — pure line model (node-tested)

**Files:** Create `internal/web/static/viewer.js` (pure section only in this task), `internal/web/viewerjs_test.go`.

**Interfaces — Produces** (pure section, bracketed `// --- viewer model (pure; guarded against Go) ---` … `// --- end viewer model ---`):
- `clampLine(n, count)` → 0 when count is 0, else `min(max(n,1),count)`.
- `landLine(want, count)` → `{line, notice}`: `want<=0` → `{line:1, notice:""}` (1 when count>0, else 0); past the end → `{line:count, notice:"line "+want+" is past the end of "+"<path>"…}` — signature `landLine(want, count, path)`.
- `sameLines(a, b)` → true iff equal length and every `.text` equal (W5).
- `versionLabel(src, rev)` → `"working tree"` | `"@ " + rev.slice(0,7)` | `"shelf"`.

- [ ] **Step 1: failing test** — `viewerjs_test.go`, following `linksjs_test.go`'s pattern (extract the bracketed section, run it under `node` with a small driver, skip when node is absent exactly as that file does):

```go
const viewerPureStart = "// --- viewer model (pure; guarded against Go) ---"
const viewerPureEnd = "// --- end viewer model ---"

func TestViewerModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", viewerPureStart, viewerPureEnd, `
const r = [];
r.push(clampLine(5, 0), clampLine(0, 3), clampLine(9, 3), clampLine(2, 3));
r.push(JSON.stringify(landLine(0, 3, "a.txt")), JSON.stringify(landLine(7, 3, "a.txt")), JSON.stringify(landLine(2, 3, "a.txt")));
r.push(sameLines([{text:"a"}],[{text:"a"}]), sameLines([{text:"a"}],[{text:"b"}]), sameLines([],[{text:""}]));
r.push(versionLabel("worktree",""), versionLabel("commit","0123456789"), versionLabel("shelf","x"));
console.log(r.join("|"));
`)
	want := `0|1|3|2|{"line":1,"notice":""}|{"line":3,"notice":"line 7 is past the end of a.txt (3 lines)"}|{"line":2,"notice":""}|true|false|false|working tree|@ 0123456|shelf`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}
```

(`runPureJS` — reuse the helper the other `*js_test.go` files use to extract a bracketed section and run node; `grep -n "func run.*JS\|exec.Command(\"node\"" internal/web/*_test.go` to find its real name/signature and adapt this call.)

- [ ] **Step 2:** `go test ./internal/web/ -run TestViewerModelJS` — Expected: FAIL (file/section missing).
- [ ] **Step 3:** write the pure section:

```js
// viewer.js — the file viewer overlay (open files on the web, plan 5a): one
// file at one version — the working tree, a commit, a shelf entry — with a
// line cursor, the in-view search and a . menu. Content links land here.

// --- viewer model (pure; guarded against Go) ---
function clampLine(n, count) {
  if (count <= 0) return 0;
  return Math.min(Math.max(n, 1), count);
}

// landLine is where a link's line lands: past the end clamps to the last
// line and says so (the TUI's words).
function landLine(want, count, path) {
  if (want <= 0) return { line: count > 0 ? 1 : 0, notice: "" };
  if (want > count) return { line: count, notice: "line " + want + " is past the end of " + path + " (" + count + " lines)" };
  return { line: want, notice: "" };
}

function sameLines(a, b) {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i].text !== b[i].text) return false;
  return true;
}

function versionLabel(src, rev) {
  if (src === "commit") return "@ " + String(rev || "").slice(0, 7);
  if (src === "shelf") return "shelf";
  return "working tree";
}
// --- end viewer model ---
```

- [ ] **Step 4:** re-run — Expected: PASS.
- [ ] **Step 5:** commit `feat(web): viewer line model`.

---

### Task 4: the viewer overlay (render, cursor, search, footer, esc)

**Files:** Modify `internal/web/static/viewer.js`, `internal/web/static/app.js` (import `./viewer.js`), `internal/web/static/style.css`; test `internal/web/viewerjs_test.go` (wiring checks).

**Interfaces — Produces:** `export async function openViewer({src, rev, path, line})` → opens/refreshes the overlay, returns `{ok, notice}`; `export function closeViewer()`; module state `view = {src, rev, path, lines, cur}`.

- [ ] **Step 1: failing wiring test** (substring gate, the `TestLinksJSIsWiredEverywhere` style):

```go
func TestViewerJSIsWired(t *testing.T) {
	t.Parallel()
	read := func(n string) string { b, err := os.ReadFile(filepath.Join("static", n)); if err != nil { t.Fatal(err) }; return string(b) }
	for _, c := range []struct{ file, want, why string }{
		{"app.js", "./viewer.js", "the module must be imported at boot"},
		{"viewer.js", `mountOverlay("viewer")`, "the overlay is a mounted layer"},
		{"viewer.js", `pushLayer("viewer"`, "it rides the layer stack"},
		{"viewer.js", "/api/file-content", "it reads through the content endpoint"},
		{"viewer.js", "bindSearchBar(", "it has the in-view search"},
		{"viewer.js", "renderCell(", "lines paint through the shared cell renderer"},
		{"viewer.js", `$("foot")`, "the keys go in the bottom bar"},
		{"style.css", "#viewer", "the overlay is styled"},
	} {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}
```

- [ ] **Step 2:** run — Expected: FAIL.
- [ ] **Step 3: implement** (after the pure section in `viewer.js`):

```js
import { $, esc, getJSON, opLine, state } from "./core.js";
import { closeLayer, mountOverlay, pushLayer, topLayer } from "./layers.js";
import { bindSearchBar } from "./searchbar.js";
import { Search } from "./inviewsearch.js";
import { renderCell } from "./files.js";
import { cycleTextMode } from "./files.js";

const view = { src: "worktree", rev: "", path: "", lines: [], cur: 0, placeholder: "" };
const viewerSearch = new Search();

function viewerEl() {
  const el = mountOverlay("viewer");
  if (!el.firstChild) {
    el.innerHTML =
      `<div id="viewer-box"><div id="viewer-title"></div>` +
      `<div id="viewer-search" class="hidden search-bar"><span id="viewer-search-lead">/</span>` +
      `<input id="viewer-search-input" type="text" autocomplete="off" spellcheck="false" placeholder="find in this file — enter keeps it, ] [ step, esc clears">` +
      `<span id="viewer-search-count"></span></div>` +
      `<div id="viewer-body" tabindex="-1"></div></div>`;
    el.addEventListener("click", (e) => { if (e.target.id === "viewer") closeViewer(); });
    $("viewer-body").addEventListener("click", (e) => {
      const row = e.target.closest(".vline[data-i]");
      if (row) { view.cur = Number(row.dataset.i) + 1; paintCursor(); }
    });
    $("viewer-body").addEventListener("contextmenu", (e) => {
      const row = e.target.closest(".vline[data-i]");
      if (row) view.cur = Number(row.dataset.i) + 1;
      e.preventDefault();
      paintCursor();
      openViewerMenu(e.clientX, e.clientY);
    });
  }
  return el;
}

export async function openViewer({ src = "worktree", rev = "", path, line = 0 }) {
  let body;
  try {
    body = await getJSON("/api/file-content?src=" + encodeURIComponent(src) + "&rev=" + encodeURIComponent(rev) + "&path=" + encodeURIComponent(path));
  } catch (e) {
    opLine("view failed: " + (e.message || e), true);
    return { ok: false, notice: "" };
  }
  Object.assign(view, { src, rev, path, lines: body.lines || [], cur: 0 });
  view.placeholder = body.missing ? "(file deleted on disk)" : body.too_large ? "(file too large to preview)" : view.lines.length ? "" : "(empty file)";
  const landed = landLine(line, view.lines.length, path);
  view.cur = landed.line;
  viewerSearchBar.reset();
  const el = viewerEl();
  $("viewer-title").textContent = "View " + path + " (" + versionLabel(src, rev) + ")"; // CSS elides the middle (direction trick) — see style
  renderViewer();
  pushLayer("viewer", el, { onKey: viewerKey });
  swapFoot(true);
  centerCursor();
  $("viewer-body").focus({ preventScroll: true });
  if (landed.notice) opLine(landed.notice, false);
  return { ok: true, notice: landed.notice };
}

export function closeViewer() {
  closeLayer("viewer");
  swapFoot(false);
}

function renderViewer() {
  if (viewerSearch.query) viewerSearch.refind(view.lines.map((l, i) => ({ row: i, side: 0, text: l.text || "" })));
  if (view.placeholder) {
    $("viewer-body").innerHTML = `<div class="notice">${esc(view.placeholder)}</div>`;
  } else {
    $("viewer-body").innerHTML = view.lines
      .map((l, i) => `<div class="vline${i + 1 === view.cur ? " vcur" : ""}" data-i="${i}"><span class="vno">${i + 1}</span>` +
        `<span class="vtext">${renderCell(l.text, null, l.tok, "", viewerSearch.query ? viewerSearch.hitsOn(i, 0) : null) || " "}</span></div>`)
      .join("");
  }
  viewerSearchBar.paint();
}

function paintCursor() {
  for (const el of $("viewer-body").querySelectorAll(".vcur")) el.classList.remove("vcur");
  const row = $("viewer-body").querySelector(`.vline[data-i="${view.cur - 1}"]`);
  if (row) { row.classList.add("vcur"); row.scrollIntoView({ block: "nearest" }); }
}

function centerCursor() {
  const row = $("viewer-body").querySelector(`.vline[data-i="${view.cur - 1}"]`);
  if (row) row.scrollIntoView({ block: "center" });
}

function pageRows() {
  const row = $("viewer-body").querySelector(".vline");
  return row ? Math.max(1, Math.floor($("viewer-body").clientHeight / row.offsetHeight) - 1) : 10;
}

function moveCursor(delta) {
  view.cur = clampLine(view.cur + delta, view.lines.length);
  paintCursor();
}

function viewerKey(e) {
  if (e.target === $("viewer-search-input")) return true;
  if (e.ctrlKey || e.metaKey || e.altKey) return false;
  if (viewerSearchKey(e)) return true;
  switch (e.key) {
    case "ArrowDown": case "j": moveCursor(1); break;
    case "ArrowUp": case "k": moveCursor(-1); break;
    case "PageDown": moveCursor(pageRows()); break;
    case "PageUp": moveCursor(-pageRows()); break;
    case "Home": case "g": view.cur = clampLine(1, view.lines.length); paintCursor(); break;
    case "End": case "G": view.cur = view.lines.length; paintCursor(); break;
    case "w": cycleTextMode(); break;
    case ".": { const r = $("viewer-body").querySelector(".vcur")?.getBoundingClientRect(); openViewerMenu(r ? r.left + 40 : 80, r ? r.bottom : 80); break; }
    case "Escape": closeViewer(); break;
    default: return false;
  }
  e.preventDefault();
  return true;
}
```

The search bar mirrors `filehist.js`'s `blameSearchBar` / `blameSearchKey` (copy them with `viewer-` ids and `.vline` rows — `here` returns the cursor row). Footer swap:

```js
let savedFoot = null;
const VIEWER_FOOT =
  `<span>↑↓ line</span><button data-vact="find">/ find</button><span>] [ next/prev</span>` +
  `<button data-vact="wrap">w wrap</button><button data-vact="menu">. menu</button><button data-vact="close">esc close</button>`;

function swapFoot(on) {
  const foot = $("foot");
  if (on && savedFoot === null) { savedFoot = foot.innerHTML; foot.innerHTML = VIEWER_FOOT; }
  else if (!on && savedFoot !== null) { foot.innerHTML = savedFoot; savedFoot = null; }
}

$("foot").addEventListener("click", (e) => {
  const b = e.target.closest("button[data-vact]");
  if (!b) return;
  if (b.dataset.vact === "find") viewerSearchBar.open(false);
  else if (b.dataset.vact === "wrap") cycleTextMode();
  else if (b.dataset.vact === "menu") openViewerMenu(80, window.innerHeight - 60);
  else if (b.dataset.vact === "close") closeViewer();
});
```

CSS (`style.css`, next to `#blame`): `#viewer { position: fixed; inset: 0 0 var(--foot-h, 25px) 0; … z-index: 21 }` (the blame box rules with `viewer` ids), `.vline{display:flex;white-space:pre-wrap}`, `.vno{flex:none;width:4em;text-align:right;color:var(--dim);padding-right:10px}`, `.vtext{flex:1;min-width:0;overflow-wrap:anywhere}`, `.vline.vcur{background:var(--sel-bg, rgba(100,150,255,.18))}`, the `body.lm-scroll`/`lm-cut` variants the blame rules have (copy them with `#viewer-body`), and `#viewer-title` middle-elision via the page's existing path-elide helper if the title is built from spans, else `direction: rtl; text-overflow: ellipsis` is NOT allowed (it cuts the start) — use the helper `filepathelidejs_test.go` pins.

`openViewerMenu(x, y)` is a stub `showCtxMenu([], x, y)` here; Task 5 fills it.

(Imports: check `renderCell`, `cycleTextMode`, `esc`, `getJSON`, `opLine` are exported from the modules named — `grep -n "^export" static/files.js static/core.js` — and adjust. A cycle `files.js ↔ viewer.js` must not arise: viewer imports files, files must not import viewer; entry rows go through `registerRows` in viewer.js itself.)

- [ ] **Step 4:** `go test ./internal/web/ -run 'Viewer'` — Expected: PASS. Build and open `gg web` on this repo; in the devtools console `import("/static/viewer.js").then(m => m.openViewer({path:"README.md", line: 40}))` — the overlay shows README with line 40 highlighted and centred, `#foot` shows the viewer chips, `esc` restores the app footer.
- [ ] **Step 5:** commit `feat(web): the file viewer overlay`.

---

### Task 5: the viewer's `.` menu

**Files:** Modify `internal/web/static/viewer.js`; test `viewerjs_test.go` (wiring).

**Interfaces — Consumes:** `linkFor`, `copyLink`, `linkDesc` (links.js), `copyText` (layers.js), `showCtxMenu`, `openFileHistory`, `openFileBlame` (filehist.js), the diff openers (`openStatusDiff`/`openCommitByHash` — find the exported names in files.js/commits.js).

- [ ] **Step 1: failing wiring test** — add to `TestViewerJSIsWired`: `{"viewer.js", "copy file link", …}`, `{"viewer.js", "copy line", …}`, `{"viewer.js", "sameLines(", "a commit/shelf version copies a content link only when the disk matches"}`, `{"viewer.js", "openFileBlame(", …}`, `{"viewer.js", "openFileHistory(", …}`.
- [ ] **Step 2:** run — FAIL.
- [ ] **Step 3: implement**:

```js
function openViewerMenu(x, y) {
  const line = view.cur;
  const items = [];
  const flink = linkFor(state.repo, state.worktree, { path: view.path, state: "unstaged", hint: { kind: "view", id: "content" } }, "new", line);
  if (flink) items.push({ label: "copy file link" + (line ? " (line " + line + ")" : ""), act: () => copyViewerLink(flink) });
  if (line && view.lines[line - 1]) items.push({ label: "copy line", act: () => copyText(view.lines[line - 1].text, "line " + line) });
  items.push({ sep: true });
  if (view.src === "worktree") items.push({ label: "diff (HEAD ↔ working tree)", act: () => { closeViewer(); openWorktreeDiff(view.path); } });
  if (view.src === "commit") items.push({ label: "diff (this commit's change)", act: () => { closeViewer(); openCommitByHash(view.rev, view.rev.slice(0, 8)); } });
  if (view.src !== "shelf") {
    const rev = view.src === "commit" ? view.rev : "";
    items.push({ label: "file history", act: () => { closeViewer(); openFileHistory(view.path, rev); } });
    items.push({ label: "blame", act: () => { closeViewer(); openFileBlame(view.path, rev); } });
  }
  showCtxMenu(items, x, y);
}

async function copyViewerLink(flink) {
  if (view.src === "worktree") return copyFileLink(view.path, flink);
  let disk;
  try {
    disk = await getJSON("/api/file-content?path=" + encodeURIComponent(view.path));
  } catch (e) {
    return opLine("copy failed: " + (e.message || e), true);
  }
  if (disk.missing || !sameLines(disk.lines || [], view.lines)) {
    return opLine("the file on disk differs from this version — no content link", true);
  }
  copyLink(flink, linkDesc("file", view.path, ""));
}
```

`openWorktreeDiff(path)`: the working-tree row opener in files.js (find it — the status list's `openStatusDiff`-style function that takes a path; if only an index-based opener exists, locate the status row by path and call it; if the file has no change the op line says `no changes in <path>`). Closing the viewer before opening history/blame keeps one full-page overlay at a time. Check that `linkFor`'s `side`/`no` params produce `:<line>` for a content link (links.js lines 97–180).

- [ ] **Step 4:** `go test ./internal/web/ -run Viewer` — PASS; manual: right-click a line → the five rows; copy file link on a working-tree version puts `gg://…/README.md:40?view=content` on the clipboard.
- [ ] **Step 5:** commit `feat(web): the viewer's menu — copy link at a line, diff, history, blame`.

---

### Task 6: entry points — *view file* rows

**Files:** Modify `internal/web/static/viewer.js` (contributors), `internal/web/static/files.js` (pass `rev` + `deleted` in the status-row ctx if missing), `internal/web/static/sidebar.js` (none if the shelf ctx already has `id`/`path`/`kind`); test `viewerjs_test.go`.

- [ ] **Step 1: failing wiring test** — add `{"viewer.js", `registerRows("file"`, …}`, `{"viewer.js", `registerRows("shelf"`, …}`, `{"viewer.js", "view file", …}`, and a registerHelp row check `{"viewer.js", "registerHelp(", "the ? overlay lists the viewer keys"}`.
- [ ] **Step 2:** run — FAIL.
- [ ] **Step 3: implement**:

```js
registerRows("file", (ctx) => {
  if (!ctx.path || ctx.compare && !ctx.sha) return [];
  if (ctx.section === "commit") {
    return ctx.sha ? [{ label: "view file", act: () => openViewer({ src: "commit", rev: ctx.sha, path: ctx.path }) }] : [];
  }
  if (ctx.deleted) return []; // no bytes on disk
  return [{ label: "view file", act: () => openViewer({ src: "worktree", path: ctx.path }) }];
});

registerRows("shelf", (e) =>
  e.kind !== "commit" && e.path ? [{ label: "view file", act: () => openViewer({ src: "shelf", rev: e.id, path: e.path }) }] : []);

registerHelp({
  key: "view file",
  html: "the <b>.</b> menu's <b>view file</b> (a file row, a shelved file) or a pasted content link opens the file full-page: " +
    "<b>↑↓ j k</b> line, <b>/ ] [</b> find, <b>w</b> long lines, <b>.</b> menu (copy file link at the line, copy line, diff, history, blame), <b>esc</b> close",
});
```

In `files.js`: the status-row call `extraRows("file", { path: f.path, sha: "", section: f.section })` gains `deleted: f.unstaged === "D" || (f.section === "staged" && f.staged === "D")` (check the status entry's field names in the `/api/status` wire first). The commit-row ctx already carries `sha: rev`; for a deleted file in a commit (`f.status === "D"`) pass `deleted: true` so no *view file* row appears for bytes the commit removed. Put the *view file* row near the top: `extraRows` appends after built-ins — acceptable (ledger) unless the menu reads badly in the browser check, then insert it via an explicit call in files.js.

- [ ] **Step 4:** `go test ./internal/web/ -run 'Viewer|Links|Help'` — PASS (the help-coverage gate, if one exists for registerHelp, stays green).
- [ ] **Step 5:** commit `feat(web): view file in the file and shelf menus`.

---

### Task 7: landing — steered navigate, `gg open --web`, `#` paste

**Files:** Modify `internal/web/static/live.js` (content arm), `internal/web/static/commits.js` (`gotoCommitPrompt` accepts links); test `viewerjs_test.go` (wiring) + a Go test for start-at carrying the content wire.

- [ ] **Step 1: failing tests** — wiring: `{"live.js", `hint_kind === "view"`, "a content navigate lands in the viewer"}`, `{"live.js", "openViewer(", …}`, `{"commits.js", "/api/link-command", "# accepts a pasted gg:// link"}`. Go:

```go
func TestStartAtCarriesAContentLink(t *testing.T) {
	t.Parallel()
	s := New(domain.Open(newRepoDir(t, 1)))
	if err := s.setStartAt(steer.Command{Cmd: "navigate", File: "f.txt", HintKind: "view", HintID: "content", Line: &steer.Line{No: 2}}); err != nil {
		t.Fatalf("setStartAt refused a content link: %v", err)
	}
}
```

- [ ] **Step 2:** run — FAIL (wiring) / PASS for the Go one after Task 2 (it pins it; ledger if already green).
- [ ] **Step 3: implement.** `live.js`, at the top of `steerNavigate`:

```js
async function steerNavigate(s) {
  if (s.hint_kind === "view" && s.hint_id === "content") return steerNavigateContent(s);
  await steerNavigateLand(s);
  if (s.hint_kind) await revealHint(s);
}

// steerNavigateContent lands a content link: the file ON DISK in the viewer,
// at the link's line — refused, on the page, when the file is not in the
// working tree (the TUI's words).
async function steerNavigateContent(s) {
  let present = false;
  try {
    present = (await getJSON("/api/worktree-present?path=" + encodeURIComponent(s.file))).present;
  } catch { /* the viewer's own load reports a dead server */ }
  if (!present) return navMiss(s.file + " is not in the working tree");
  await openViewer({ src: "worktree", path: s.file, line: s.line || 0 });
}
```

`commits.js`:

```js
function gotoCommitPrompt() {
  openPrompt({
    title: "Goto — a commit (sha, branch, tag, any rev) or a gg:// link",
    placeholder: "e.g. a1b2c3d, main~3, gg://…",
    onSubmit: (text) => (text.trim().startsWith("gg://") ? gotoLink(text.trim()) : gotoCommit(text)),
  });
}

async function gotoLink(link) {
  let body;
  try {
    body = await getJSON("/api/link-command?link=" + encodeURIComponent(link));
  } catch (e) {
    return opLine("cannot open link: " + (e.message || e), true);
  }
  if (body.checkout) return opLine("that link is in " + body.checkout + " — open gg web there", true);
  await steerNavigate(body.steer);
}
```

(`steerNavigate` must be exported from live.js; if importing live.js from commits.js makes a cycle, move `gotoLink` into live.js and have commits.js import it. The palette's "goto commit…" row label becomes "goto commit or link…".)

- [ ] **Step 4:** `go test ./internal/web/ -run 'Viewer|StartAt|Steer'` — PASS.
- [ ] **Step 5:** commit `feat(web): content links land in the viewer — steered, gg open --web, # paste`.

---

### Task 8: browser check, docs

**Files:** `CHANGELOG.md`, `README.md` (the web section + *Open files*), `docs/CLAUDE-details.md` ("Content links": the web half), `internal/agentskill/using-gg.md` (`gg open --web` no longer refuses a content link; bump `agentskill.Version`, `gg init --update`); memory.

- [ ] **Step 1: browser check (playwright, per `playwright-web-verification`):** FIRST against the installed (unfixed) `gg web` — expect: *view file* absent, `#` + a content link errors. THEN against the branch build (`go build -o <scratch>/gg-5a ./cmd/gg`, fresh `gg web`, hard reload): (a) right-click `README.md` in the working-tree list → *view file* → `#viewer` visible (bounding box > 0, computed `display` ≠ none), title `View README.md (working tree)`, row count = the file's line count; (b) `j` ×3 moves `.vcur` to line 4; (c) `/` "gigagit" + enter → a `.hit` visible; (d) `#foot` text contains `esc close`; `esc` → `#viewer` hidden and `#foot` contains `? help` again; (e) a commit row's *view file* → title `@ <sha7>`; (f) `#` → paste `gg link --content README.md:10` output → viewer with `.vcur` on line 10; (g) `gg session navigate <content link:5>` from a shell → the page's viewer at line 5; (h) `gg open --web <content link>` with the page live → steered, lands.
- [ ] **Step 2:** docs (CHANGELOG "Web: file viewer" section; README; CLAUDE-details; using-gg + Version; regenerate SKILL.md; `go test ./internal/agentskill/`).
- [ ] **Step 3:** `./test.sh` then `./test.sh race` — both green, tails read.
- [ ] **Step 4:** commit `docs: web file viewer (open files 5a)`; update memory; ASK before merging; after merge `./build.sh install` + `./build.sh web`.
