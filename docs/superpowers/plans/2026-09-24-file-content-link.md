# File Content Links Implementation Plan

> **For agentic workers:** This repo's CLAUDE.md forbids subagents: THIS
> session executes every task sequentially (superpowers:executing-plans).
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A "Copy file link" row on file rows (TUI + web) copies
`gg://<repo>/<path>?view=content` after checking the file exists on disk, and
`gg open` / `gg session navigate` land such a link in the TUI's content viewer.

**Architecture:** `view=content` joins the closed `?<kind>=<id>` hint set, so
the link grammar gains a hint kind, not a new shape. Every producer builds it
with its existing link builder plus `model.ContentHint`; every consumer that
is not a navigate refuses it through the CLI's one per-verb gate
(`linkShapes`). The TUI lands it with a new `steerNavigate` arm that stats the
file and pushes the existing `contentPopup` over the disk bytes; the web page
refuses it for now.

**Tech Stack:** Go 1.26, Bubble Tea, vanilla JS (web static), node (JS parity
test), real `git` fixtures.

**Spec:** `docs/superpowers/specs/2026-09-24-file-content-link-design.md`

## Global Constraints

- Link text: `gg://<repo>/<path>?view=content`; v2 adds `:<line>` before `?`.
- Hint kind `"view"`, id `"content"` — the only accepted id.
- A content link has NO `@target`, no `old:` side, no `#hunk`, and a path.
- Missing-file message, everywhere: `<path> is not in the working tree`.
- Web landing refusal: `content links are not supported in gg web yet`.
- CLI exits: usage/shape error 2, missing file 1.
- TUI strings through `i18n.T` with literal keys in ja/ko/zh/ru; CLI/steer prose English.
- `internal/tui` / `internal/cli` never import `internal/git`.
- New tests call `t.Parallel()`.

## Review Focus

1. A commit-files row for a file DELETED since (or in) that commit — the user
   expects the "not in the working tree" notice, not a silently absent row.
   (Task 5 test `TestCopyFileLinkMissingFileSaysSo`.)
2. A dirty file opened by an agent must show the DISK text, not HEAD.
   (Task 4 test asserts `EDITED` is in the popup.)
3. A content link pasted into `gg diff` / `gg note add` / `gg show` must be
   refused, not silently treated as the working-tree diff. (Task 3.)
4. A web page as the only live session: `gg open` must fail visibly, not hang
   or land on a diff. (Task 6 `toSteerWire` refusal + Task 3 `--web` refusal.)
5. The local-form link (no remote) carries no path until resolve — an
   address-less content link must be refused there, not navigate to nothing.
   (Task 2.)

---

### Task 1: model — the `view=content` hint

**Files:**
- Modify: `internal/model/link.go` (hint set ~L112, `parseLinkHint` ~L536, `ParseLink` target switch ~L326 and path split ~L420)
- Test: `internal/model/link_test.go`

**Interfaces:**
- Produces: `model.ContentHintKind = "view"`, `model.ContentHintID = "content"`,
  `model.ContentHint LinkHint`, `func (l Link) IsContent() bool`.

- [ ] **Step 1: Write the failing tests** — append to `link_test.go`:

```go
func TestContentLinkRoundTrips(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"gg://gigagit/a/b.go?view=content",
		"gg://gigagit/a/b.go:12?view=content", // v2 shape: parsed now, landed later
		"gg:///mnt/t/repo/a/b.go?view=content",
	} {
		l, err := ParseLink(in)
		if err != nil {
			t.Fatalf("ParseLink(%q) = %v", in, err)
		}
		if !l.IsContent() || l.Hint != ContentHint {
			t.Errorf("ParseLink(%q).Hint = %+v, want the content hint", in, l.Hint)
		}
		if l.Target.State != StateUnstaged {
			t.Errorf("ParseLink(%q) target = %v, want the working tree", in, l.Target.State)
		}
		if got := l.String(); got != in {
			t.Errorf("String() = %q, want %q", got, in)
		}
	}
}

func TestContentLinkRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, in, want string }{
		{"commit target", "gg://gigagit/a.go@" + fullSHA + "?view=content", "working tree"},
		{"staged target", "gg://gigagit/a.go@staged?view=content", "working tree"},
		{"ref target", "gg://gigagit/a.go@ref:main?view=content", "working tree"},
		{"pair target", "gg://gigagit/a.go@main..dev?view=content", "working tree"},
		{"preview target", "gg://gigagit/a.go@main...dev?view=content", "working tree"},
		{"old side", "gg://gigagit/a.go:old:3?view=content", "old"},
		{"hunk", "gg://gigagit/a.go#2?view=content", "hunk"},
		{"no path", "gg://gigagit?view=content", "file path"},
		{"other id", "gg://gigagit/a.go?view=blame", "content"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseLink(tc.in)
			if err == nil || !errors.Is(err, ErrLink) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ParseLink(%q) = %v, want an ErrLink mentioning %q", tc.in, err, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/model -run 'TestContentLink' -count=1`
Expected: FAIL — `undefined: ContentHint` (compile error).

- [ ] **Step 3: Implement** in `internal/model/link.go`:

Add `"view": true` to `linkHintKinds` and extend its doc comment:

```go
// "view" is not a surface the link was copied FROM but the one it lands ON:
// view=content opens the file's working-tree content in the viewer instead
// of its diff. Only a working-tree file link may carry it (ParseLink).
var linkHintKinds = map[string]bool{"bookmark": true, "shelf": true, "stash": true, "preview": true, "view": true}

// The content hint: gg://<repo>/<path>[:<line>]?view=content names a file's
// CONTENT on disk in the worktree — never a diff, never a commit.
const (
	ContentHintKind = "view"
	ContentHintID   = "content"
)

// ContentHint is the one value a content link's hint may hold.
var ContentHint = LinkHint{Kind: ContentHintKind, ID: ContentHintID}

// IsContent reports whether l is a content link.
func (l Link) IsContent() bool { return l.Hint.Kind == ContentHintKind }
```

In `parseLinkHint`, after the id checks and before the final return:

```go
	if kind == ContentHintKind && id != ContentHintID {
		return LinkHint{}, fmt.Errorf("%w: a view hint reads view=content, got %q", ErrLink, s)
	}
```

and change its unknown-kind message to `(want bookmark, shelf, stash, preview or view)`.

In `ParseLink`, immediately AFTER the `switch { case !hasTarget: … }` target
block (before the local-form `if strings.HasPrefix(head, "/")`):

```go
	// A content link names the file ON DISK: no target, no diff side, no hunk.
	// Its path is checked below, once the remote form has split it off (the
	// local form only learns its path in domain.ResolveLink, which refuses an
	// address-less one there).
	if l.IsContent() {
		switch {
		case l.Target.State != StateUnstaged:
			return linkErr("a content link names the file in the working tree; drop the @ target")
		case l.Side == NoteSideOld:
			return linkErr("a content link has no old side; drop \"old:\"")
		case l.Hunk > 0:
			return linkErr("a content link names a file, not a hunk; drop \"#%d\"", l.Hunk)
		}
	}
```

And after `l.Repo.Name, l.Path = name, path` (remote form), next to the
existing "needs a file path" check:

```go
	if l.IsContent() && l.Path == "" {
		return linkErr("a content link needs a file path")
	}
```

- [ ] **Step 4: Run to verify they pass** — plus the whole package:

Run: `go test ./internal/model -count=1`
Expected: PASS (existing hint-kind tests still pass; if one pins the old
"want bookmark, shelf or stash" wording, update it to the new list).

- [ ] **Step 5: Commit**

```bash
git add internal/model/link.go internal/model/link_test.go
git commit -m "feat(model): view=content hint — the content-link grammar"
```

---

### Task 2: domain — refuse an address-less content link

**Files:**
- Modify: `internal/domain/linkresolve.go` (hint-only switch ~L525)
- Test: `internal/domain/linkresolve_test.go`

**Interfaces:**
- Consumes: `model.ContentHintKind` (Task 1).
- Produces: `ResolveLink` returns an `ErrLink` "a content link needs a file
  path" for `gg:///<checkout>?view=content`; a content link WITH a path
  resolves exactly like the plain working-tree link, `Resolved.Hint ==
  model.ContentHint`.

- [ ] **Step 1: Write the failing test** — in `linkresolve_test.go`, reuse the
file's existing real-repo helper (the one its local-form tests use; check the
top of the file for its name, e.g. `resolveRepo(t)`), then:

```go
func TestResolveLinkContentLink(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hi\n")
	svc := Open(dir)
	abs := filepath.ToSlash(dir)
	with, err := model.ParseLink("gg://" + abs + "/README.md?view=content")
	if err != nil {
		t.Fatal(err)
	}
	res, err := ResolveLink(context.Background(), with, ResolveOpts{Cwd: svc})
	if err != nil {
		t.Fatalf("ResolveLink(content link with a path) = %v", err)
	}
	if res.Addr.Path != "README.md" || res.Hint != model.ContentHint {
		t.Errorf("resolved = %+v, want README.md with the content hint", res)
	}
	bare, err := model.ParseLink("gg://" + abs + "?view=content")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveLink(context.Background(), bare, ResolveOpts{Cwd: svc}); err == nil ||
		!errors.Is(err, model.ErrLink) || !strings.Contains(err.Error(), "needs a file path") {
		t.Fatalf("ResolveLink(address-less content link) = %v, want an ErrLink naming the missing path", err)
	}
}
```

(`abs` on Windows is `C:/…`; `"gg://" + abs` is the accepted drive-head alias.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain -run TestResolveLinkContentLink -count=1`
Expected: FAIL — the bare link errors with "has no way to check a view hint",
not "needs a file path".

- [ ] **Step 3: Implement** — in the `rel == "" && hintOnlyTarget(...)` switch,
add before `default:`:

```go
		case model.ContentHintKind:
			// A content link names a FILE's content; with no path it names
			// nothing. (The remote form is refused by ParseLink already; the
			// local form only learns its path here.)
			return Resolved{}, fmt.Errorf("%w: a content link needs a file path", model.ErrLink)
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/domain -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/linkresolve.go internal/domain/linkresolve_test.go
git commit -m "feat(domain): an address-less content link names nothing"
```

---

### Task 3: CLI — `gg link --content`, the per-verb gate, `gg open --web`

**Files:**
- Modify: `internal/cli/link.go` (`runLink` flags ~L44-L135, `linkUsage` L23, `linkShapes` ~L539, `resolveLinkArg` ~L562)
- Modify: `internal/cli/open.go` (L62 allowance; `--web` refusal after resolve)
- Modify: `internal/cli/session.go` (L372 navigate allowance)
- Test: `internal/cli/link_content_test.go` (new)

**Interfaces:**
- Consumes: `model.ContentHint`, `model.ContentHintKind` (Task 1); `domain.Service.WorktreeFilesPresent(ctx, []string) (map[string]bool, error)` (exists).
- Produces: `linkShapes.Content bool`; `gg link --content <path>`.

- [ ] **Step 1: Write the failing tests** — new file `internal/cli/link_content_test.go`:

```go
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func TestLinkContentPrintsTheContentLink(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	code, out, errb := runLinkCLI(t, dir, "--content", "README.md")
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb)
	}
	got := strings.TrimSpace(out)
	if !strings.HasSuffix(got, "/README.md?view=content") {
		t.Fatalf("stdout = %q, want …/README.md?view=content", got)
	}
	l, err := model.ParseLink(got)
	if err != nil || !l.IsContent() {
		t.Fatalf("ParseLink(%q) = %+v, %v — want a content link", got, l, err)
	}
}

func TestLinkContentMissingFileExits1(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	code, out, errb := runLinkCLI(t, dir, "--content", "README.md")
	if code != 1 || out != "" || !strings.Contains(errb, "README.md is not in the working tree") {
		t.Fatalf("exit=%d stdout=%q stderr=%q, want 1 and the not-in-working-tree message", code, out, errb)
	}
}

func TestLinkContentUsageErrors(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	for _, args := range [][]string{
		{"--content"},                             // no path
		{"--content", "README.md:3"},              // a line is v2
		{"--content", "README.md#1"},              // never a hunk
		{"--content", "--cached", "README.md"},    // a target
		{"--content", "--rev", "HEAD", "README.md"},
		{"--content", "--ref", "main", "README.md"},
		{"--content", "--pair", "HEAD..HEAD", "README.md"},
		{"--content", "--bookmark", "b1", "README.md"}, // one landing
	} {
		if code, _, errb := runLinkCLI(t, dir, args...); code != 2 {
			t.Errorf("gg link %v: exit = %d (stderr %q), want 2", args, code, errb)
		}
	}
}

// Every link-taking verb that is not a navigate refuses a content link: it
// names a file's content, and diffing or anchoring on it would silently mean
// the working-tree diff instead.
func TestContentLinkRefusedByNonNavigateVerbs(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	link := "gg://" + filepath.ToSlash(dir) + "/README.md?view=content"
	svc := domain.Open(dir)
	for _, verb := range []string{"diff", "show", "anchor", "highlight"} {
		_, err := resolveLinkArg(t.Context(), svc, link, linkShapes{Ref: true, Pair: true}, verb)
		if err == nil || !strings.Contains(err.Error(), "content link") {
			t.Errorf("%s: resolveLinkArg = %v, want the content-link refusal", verb, err)
		}
	}
	if _, err := resolveLinkArg(t.Context(), svc, link, linkShapes{Ref: true, Pair: true, Content: true}, "open"); err != nil {
		t.Errorf("open: resolveLinkArg = %v, want the content link accepted", err)
	}
}

func TestOpenWebRefusesAContentLink(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	link := "gg://" + filepath.ToSlash(dir) + "/README.md?view=content"
	code := cmdOpen(domain.Open(dir), []string{"--web", link}, &out, &errb)
	if code != 2 || !strings.Contains(errb.String(), "content links are not supported in gg web yet") {
		t.Fatalf("exit=%d stderr=%q, want 2 and the web refusal", code, errb.String())
	}
}
```

(If `resolveLinkArg` resolves through `RepoStatePath`, a parallel test is
fine: the cwd service wins for the link's own checkout. If the file's other
tests guard `RepoStatePath`, follow their pattern.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/cli -run 'TestLinkContent|TestContentLinkRefused|TestOpenWebRefuses' -count=1`
Expected: FAIL — compile error `unknown field Content in struct literal`.

- [ ] **Step 3: Implement.**

`linkShapes` gains the field and `resolveLinkArg` the gate (after the Pair/Ref checks):

```go
type linkShapes struct {
	Ref     bool // a tip is a single commit, so most verbs take it
	Pair    bool // BOUNDED; a verb needing one commit must refuse it
	Content bool // ?view=content: a file's CONTENT; only a navigate lands it
}
```

```go
	if res.Hint.Kind == model.ContentHintKind && !allow.Content {
		return domain.Resolved{}, fmt.Errorf("%w: a content link names a file's content, not a diff, so %s cannot take it; hand it to `gg open`", model.ErrLink, verb)
	}
```

`open.go` L62 and `session.go` L372 pass `linkShapes{Ref: true, Pair: true, Content: true}`.
In `cmdOpen`, right after the `resolveLinkArg` error check:

```go
	if *web && res.Hint.Kind == model.ContentHintKind {
		fmt.Fprintln(stderr, "open: content links are not supported in gg web yet")
		return 2
	}
```

`runLink`: add the flag and the checks.

```go
	content := fs.Bool("content", false, "address the file's CONTENT on disk (?view=content), not a diff")
```

After the `--bookmark`/`--shelf` exclusivity check:

```go
	if *content {
		if set > 0 || *bookmark != "" || *shelf != "" {
			fmt.Fprintf(stderr, "link: --content names the file on disk; it takes no target or other hint\n%s\n", linkUsage)
			return 2
		}
		if len(pos) != 1 || strings.ContainsAny(pos[0], "#") || linkArgHasLine(pos[0]) {
			fmt.Fprintf(stderr, "link: --content needs one file path, with no :<line> or #<hunk>\n%s\n", linkUsage)
			return 2
		}
		hint = model.ContentHint
	}
```

with the helper (a trailing `:<digits>` is a line; a Windows drive colon is not):

```go
// linkArgHasLine reports whether a path argument ends in ":<n>" or ":old:<n>"
// — the line suffix --content refuses in v1.
func linkArgHasLine(s string) bool {
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return false
	}
	_, err := strconv.Atoi(s[i+1:])
	return err == nil
}
```

After `buildLink` succeeds and BEFORE `RecordLink`:

```go
	if *content {
		present, perr := svc.WorktreeFilesPresent(ctx, []string{l.Path})
		if perr != nil {
			fmt.Fprintln(stderr, "error:", perr)
			return 1
		}
		if !present[l.Path] {
			fmt.Fprintf(stderr, "error: %s is not in the working tree\n", l.Path)
			return 1
		}
	}
```

`linkUsage` first line gains `| --content` after `--pair <a>..<b>`:
`… | --pair <a>..<b> | --content] [--bookmark <id> | --shelf <id>]`.
Add `strconv` to the imports if not present.

- [ ] **Step 4: Run to verify they pass** — plus the package:

Run: `go test ./internal/cli -count=1`
Expected: PASS. A usage-text golden test, if any, is updated to the new line.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/link.go internal/cli/open.go internal/cli/session.go internal/cli/link_content_test.go
git commit -m "feat(cli): gg link --content; only navigate verbs take a content link"
```

---

### Task 4: TUI — the steered landing

**Files:**
- Modify: `internal/tui/steer_nav.go` (`steerNavigate` switch ~L172, `navigateLanded` ~L230)
- Modify: `internal/tui/file_finder.go` (add the working-tree loader beside `loadFileContentLayerCmd` ~L436)
- Test: `internal/tui/steer_content_test.go` (new)

**Interfaces:**
- Consumes: `model.ContentHintKind` (Task 1); `domain.Service.WorktreeFilesPresent`, `WorktreeFile` (exist); `updateThreadCtx`, `updateThreadGitTimeout`, `busyOr`, `newContentPopup`, `pushLayer`, `layerOf`, `fileContentLayerMsg` (exist).
- Produces: `func (m Model) openWorktreeContent(path string) (Model, tea.Cmd)` — Task 5 does not need it, v2 will.

- [ ] **Step 1: Write the failing tests** — new file `internal/tui/steer_content_test.go`:

```go
package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// navRepo's a.txt carries an unstaged edit ("EDITED" on line 18): the viewer
// must show the DISK bytes, which HEAD does not have.
func TestSteerContentLinkOpensTheDiskContent(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	c := steer.Command{ID: "c-1", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "unstaged"}, HintKind: model.ContentHintKind, HintID: model.ContentHintID, Wait: true}
	nm, cmd := m.applySteer(c)
	nm = pumpDiff(t, nm, cmd)
	cp := layerOf[*contentPopup](nm)
	if cp == nil {
		t.Fatal("no content popup after a content-link navigate")
	}
	var text []string
	for _, l := range cp.lines {
		text = append(text, l.text)
	}
	if !strings.Contains(strings.Join(text, "\n"), "EDITED") {
		t.Errorf("popup shows %q, want the working-tree bytes (EDITED)", text)
	}
	if !strings.Contains(nm.View(), "EDITED") {
		t.Error("the screen does not show the file's disk content")
	}
	r, ok := steer.AwaitReply(nm.steerDir, "c-1", time.Second)
	if !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok", r, ok)
	}
}

func TestSteerContentLinkMissingFileFails(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	c := steer.Command{ID: "c-2", Cmd: "navigate", File: "gone.txt",
		Target: &steer.Target{State: "unstaged"}, HintKind: model.ContentHintKind, HintID: model.ContentHintID, Wait: true}
	nm, cmd := m.applySteer(c)
	runSteerCmd(t, cmd)
	if layerOf[*contentPopup](nm) != nil {
		t.Error("a missing file must not open a popup")
	}
	r, ok := steer.AwaitReply(nm.steerDir, "c-2", time.Second)
	if !ok || r.OK || r.Error != "gone.txt is not in the working tree" {
		t.Fatalf("reply = %+v ok=%v, want ok:false and the not-in-working-tree message", r, ok)
	}
}

// The `gg open` launch path: the pure converter carries the hint through.
func TestStartAtContentLinkCarriesTheHint(t *testing.T) {
	t.Parallel()
	l, err := model.ParseLink("gg:///mnt/t/repo/a.txt?view=content")
	if err != nil {
		t.Fatal(err)
	}
	l.Repo.Abs, l.Path = "/mnt/t/repo", "a.txt" // what linknav.AtLink hands the launcher
	c, ok := steerCommandForLink(l)
	if !ok || c.File != "a.txt" || c.HintKind != model.ContentHintKind {
		t.Fatalf("steerCommandForLink = %+v, %v — want a.txt with the content hint", c, ok)
	}
}
```

(If `pumpDiff` only feeds `diffMsg`, feed the batch through `flattenCmd`
+ `Update` inline instead — the popup fills on `fileContentLayerMsg`.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui -run 'TestSteerContentLink|TestStartAtContentLink' -count=1`
Expected: the two navigate tests FAIL (the command falls to
`steerNavigateStatusFile`: no popup, "a.txt"… lands on the diff; gone.txt
answers "not in the working-tree diff"). The start-at test may already pass —
it pins the converter.

- [ ] **Step 3: Implement.**

`file_finder.go`, beside `loadFileContentLayerCmd`:

```go
// openWorktreeContent pushes the "View <path>" content popup over the file's
// WORKING-TREE bytes — what a content link (?view=content) lands on. The
// file finder's "View content" shows HEAD; a content link names the file as
// it is on disk, uncommitted edits included.
func (m Model) openWorktreeContent(path string) (Model, tea.Cmd) {
	cp := newContentPopup(i18n.T("View %s", path), []contentLine{{text: i18n.T("(loading…)")}})
	cp.charWrap = true // a file's text: column-exact wrap
	m = m.pushLayer(cp)
	return m, m.loadWorktreeContentLayerCmd(path)
}

// loadWorktreeContentLayerCmd is loadFileContentLayerCmd over the disk bytes;
// it delivers the same fileContentLayerMsg, so the same tag-gate fills it.
func (m Model) loadWorktreeContentLayerCmd(path string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		data, err := svc.WorktreeFile(context.Background(), path)
		if err != nil {
			return fileContentLayerMsg{path: path, err: err}
		}
		if len(data) > domain.MaxDiffBytes {
			return fileContentLayerMsg{path: path, lines: []contentLine{{text: i18n.T("(file too large to preview)")}}}
		}
		return fileContentLayerMsg{path: path, lines: fileContentLines(data)}
	}
}
```

`steer_nav.go` — in `steerNavigate`, directly after `case c.Step != "":`:

```go
	case c.HintKind == model.ContentHintKind:
		return m.steerNavigateContent(c)
```

and the handler (next to `steerNavigateHintOnly`):

```go
// steerNavigateContent lands a content link (?view=content): the file's
// working-tree bytes in the content viewer, never its diff. Presence is a
// stat (no git), asked on the Update thread under the same deadline
// steerNavigateRef uses, so a missing file is refused before anything moves.
func (m Model) steerNavigateContent(c steer.Command) (Model, tea.Cmd) {
	if c.File == "" {
		return m, m.answerSteer(c, steerFail(c, "a content link needs a file path"))
	}
	ctx, cancel := updateThreadCtx(updateThreadGitTimeout)
	defer cancel()
	present, err := m.svc.WorktreeFilesPresent(ctx, []string{c.File})
	if err := busyOr(err); err != nil {
		return m, m.answerSteer(c, steerFail(c, "checking "+c.File+": "+err.Error()))
	}
	if !present[c.File] {
		return m, m.answerSteer(c, steerFail(c, c.File+" is not in the working tree"))
	}
	m = m.steerToPanels()
	m, load := m.openWorktreeContent(c.File)
	m, reply := m.navigateLanded(c, "opened "+c.File)
	return m, tea.Batch(load, reply)
}
```

In `navigateLanded`'s `switch c.HintKind`, before `default:`:

```go
	case model.ContentHintKind:
		// The landing IS the hint: the viewer is already open.
		return m, reply
```

- [ ] **Step 4: Run to verify they pass** — plus the package:

Run: `go test ./internal/tui -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/steer_nav.go internal/tui/file_finder.go internal/tui/steer_content_test.go
git commit -m "feat(tui): a content link lands in the content viewer"
```

---

### Task 5: TUI — the "Copy file link" row

**Files:**
- Create: `internal/tui/file_link.go`
- Modify: `internal/tui/action_menu.go` (the two `insertCopyLinkRow` splices, L63 and L182)
- Modify: `internal/tui/model.go` (Update: the `fileLinkCheckedMsg` case, next to `clipboardCopiedMsg` ~L3763)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Test: `internal/tui/file_link_test.go`

**Interfaces:**
- Consumes: `model.ContentHint` (Task 1); `hintedLinkFor`, `copyToClipboardCmd`, `insertAfterID`, `rowByID`, `availableActions` (exist).
- Produces: row id `copy-file-link`; `fileLinkCheckedMsg{path, text string; present bool; err error}`.

- [ ] **Step 1: Write the failing tests** — `internal/tui/file_link_test.go`:

```go
package tui

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runFileLinkRow(t *testing.T, m Model) (Model, string) {
	t.Helper()
	var copied string
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	rows := availableActions(m)
	r, ok := rowByID(rows, "copy-file-link")
	if !ok {
		t.Fatal(`no "Copy file link" row`)
	}
	li, _ := rowIndex(rows, "copy-link")
	fi, _ := rowIndex(rows, "copy-file-link")
	if fi != li+1 {
		t.Errorf("Copy file link at %d, want right after Copy link (%d)", fi, li)
	}
	tm, cmd := r.run(m)
	m = tm.(Model)
	for _, msg := range flattenCmd(t, cmd) {
		tm, next := m.Update(msg)
		m = tm.(Model)
		for _, msg2 := range flattenCmd(t, next) {
			tm, _ := m.Update(msg2)
			m = tm.(Model)
		}
	}
	return m, copied
}

func TestCopyFileLinkCopiesTheContentLink(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t) // Files panel, a.txt selected
	m.focus = panelFiles
	m, copied := runFileLinkRow(t, m)
	if !strings.HasSuffix(copied, "/a.txt?view=content") {
		t.Fatalf("copied %q, want …/a.txt?view=content", copied)
	}
	if !strings.Contains(m.View(), "?view=content") {
		t.Error("the status bar does not show the copied link")
	}
}

func TestCopyFileLinkMissingFileSaysSo(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.focus = panelFiles
	if err := os.Remove(filepath.Join(m.currentWorktree, "a.txt")); err != nil {
		t.Fatal(err)
	}
	m, copied := runFileLinkRow(t, m)
	if copied != "" {
		t.Errorf("copied %q for a missing file, want nothing", copied)
	}
	if !strings.Contains(m.View(), "a.txt is not in the working tree") {
		t.Errorf("the screen never says the file is missing (statusMsg=%q)", m.statusMsg)
	}
}
```

`rowIndex` — add to the test file if the package has none:

```go
func rowIndex(rows []actionRow, id string) (int, bool) {
	for i, r := range rows {
		if r.id == id {
			return i, true
		}
	}
	return -1, false
}
```

Add one more test for the commit files view — open a's commit files view
the way an existing files-view menu test does (search `filesTreeFocused = true`
in `*_test.go` for the fixture), select `a.txt`, and assert the same suffix.
Name it `TestCopyFileLinkFromTheCommitFilesView`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui -run 'TestCopyFileLink' -count=1`
Expected: FAIL — `no "Copy file link" row`.

- [ ] **Step 3: Implement.** New file `internal/tui/file_link.go`:

```go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// fileLinkCheckedMsg carries the "Copy file link" presence check: the link
// is copied only when the file is on disk in this worktree.
type fileLinkCheckedMsg struct {
	path, text string
	present    bool
	err        error
}

// fileRowPath is the path of the file ROW under the cursor — a Files/Staged
// panel row or a files-view tree row (commit, stash, shelf) — or false on
// anything else (a diff, history or blame on top, a directory heading, a
// commit row). A files-view row deleted in its commit still counts: its
// "Copy file link" then says the file is not in the working tree, which is
// the answer the user asked for.
func (m Model) fileRowPath() (string, bool) {
	switch m.topLayer().(type) {
	case *historyView, *blameView:
		return "", false
	}
	if m.diffLayer() != nil {
		return "", false
	}
	if v := m.filesView; v != nil {
		if !m.filesTreeFocused {
			return "", false
		}
		vis := v.visible()
		if v.sel < 0 || v.sel >= len(vis) || vis[v.sel].path == "" {
			return "", false
		}
		return vis[v.sel].path, true
	}
	if m.stashView != nil && m.focus == panelCommits {
		return "", false
	}
	switch m.focus {
	case panelFiles, panelStaged:
		if b, ok := m.focusedBookmark(); ok && b.Path != "" {
			return b.Path, true
		}
	}
	return "", false
}

// contextFileLinkRow is the . menu's "Copy file link": the file's CONTENT
// link (?view=content) — no commit, the file as it is on disk — copied only
// after a stat proves the file is there.
func (m Model) contextFileLinkRow() (actionRow, bool) {
	path, ok := m.fileRowPath()
	if !ok {
		return actionRow{}, false
	}
	text, ok := m.hintedLinkFor(model.FileAddress{State: model.StateUnstaged, Worktree: m.currentWorktree, Path: path}, model.ContentHint)
	if !ok {
		return actionRow{}, false
	}
	svc := m.svc
	return actionRow{
		id:    "copy-file-link",
		label: i18n.T("Copy file link"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, func() tea.Msg {
				present, err := svc.WorktreeFilesPresent(context.Background(), []string{path})
				return fileLinkCheckedMsg{path: path, text: text, present: err == nil && present[path], err: err}
			}
		},
	}, true
}
```

`action_menu.go` — at BOTH splices, directly after the `contextLinkRow` block:

```go
		if r, ok := m.contextFileLinkRow(); ok {
			rows = insertAfterID(rows, "copy-link", r)
		}
```

(second splice uses `out` instead of `rows`).

`model.go` Update, next to `case clipboardCopiedMsg:`:

```go
	case fileLinkCheckedMsg:
		switch {
		case msg.err != nil:
			m.statusMsg = i18n.T("error: %s", msg.err.Error())
		case !msg.present:
			m.statusMsg = i18n.T("%s is not in the working tree", msg.path)
		default:
			return m, m.copyToClipboardCmd(i18n.T("Copied link: %s", msg.text), msg.text)
		}
		return m, nil
```

i18n — add to each bundle next to `"Copy link"`:

```toml
# ja.toml
"Copy file link" = "ファイルリンクをコピー"
"%s is not in the working tree" = "%s は作業ツリーにありません"
# ko.toml
"Copy file link" = "파일 링크 복사"
"%s is not in the working tree" = "%s 파일이 작업 트리에 없습니다"
# zh.toml
"Copy file link" = "复制文件链接"
"%s is not in the working tree" = "%s 不在工作树中"
# ru.toml
"Copy file link" = "Скопировать ссылку на файл"
"%s is not in the working tree" = "%s нет в рабочем дереве"
```

- [ ] **Step 4: Run to verify they pass** — plus the i18n gates:

Run: `go test ./internal/tui ./internal/i18n -count=1`
Expected: PASS, including `i18n_scan_test`, `menu_labels_test`, `render_order_test`.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/file_link.go internal/tui/file_link_test.go internal/tui/action_menu.go internal/tui/model.go internal/i18n/lang
git commit -m "feat(tui): Copy file link — the file's content link, checked on disk"
```

---

### Task 6: web — the row, the presence endpoint, the landing refusal

**Files:**
- Create: `internal/web/worktreepresent.go`, `internal/web/worktreepresent_test.go`
- Modify: `internal/web/server.go` (route, near `GET /api/linkhist` L157)
- Modify: `internal/web/steer.go` (`toSteerWire` hint block ~L222)
- Modify: `internal/web/linkbase.go` (origin ~L65)
- Modify: `internal/web/static/links.js` (`linkHintKindOK` L61, `linkFor`, the `registerRows("file")` contributor L226)
- Modify: `internal/web/linksjs_test.go` (parity cases + wiring checks), `internal/web/steer_test.go`

**Interfaces:**
- Consumes: `model.ContentHintKind` (Task 1); `domain.Service.WorktreeFilesPresent`.
- Produces: `GET /api/worktree-present?path=<p>` → `{"present": bool}`; 400 when `path` is empty.

- [ ] **Step 1: Write the failing tests.**

`worktreepresent_test.go`:

```go
package web

import (
	"net/http"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestWorktreePresentEndpoint(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	var got struct{ Present bool }
	if code := getJSON(t, ts, "/api/worktree-present?path=f.txt", &got); code != http.StatusOK || !got.Present {
		t.Fatalf("f.txt: code=%d present=%v, want 200 true", code, got.Present)
	}
	got.Present = true
	if code := getJSON(t, ts, "/api/worktree-present?path=gone.txt", &got); code != http.StatusOK || got.Present {
		t.Fatalf("gone.txt: code=%d present=%v, want 200 false", code, got.Present)
	}
	if code := getJSON(t, ts, "/api/worktree-present?path=../../etc/passwd", &got); code != http.StatusOK || got.Present {
		t.Fatalf("escaping path: code=%d present=%v, want 200 false", code, got.Present)
	}
	if code := getJSON(t, ts, "/api/worktree-present", nil); code != http.StatusBadRequest {
		t.Fatalf("no path: code=%d, want 400", code)
	}
}
```

`steer_test.go` — append:

```go
func TestSteerWireRefusesAContentLink(t *testing.T) {
	t.Parallel()
	_, err := toSteerWire(steer.Command{Cmd: "navigate", File: "a.txt", HintKind: "view", HintID: "content"})
	if err == nil || err.Error() != "content links are not supported in gg web yet" {
		t.Fatalf("toSteerWire = %v, want the web refusal", err)
	}
}
```

`linksjs_test.go` — in `TestLinkForJSMatchesGo`'s `cases`, add:

```go
		{Name: "content link, remote", Repo: "gigagit", Path: "a/b.go", State: "unstaged", HintKind: "view", HintID: "content"},
		{Name: "content link, local", Worktree: "/mnt/t/repo", Path: "a/b.go", State: "unstaged", HintKind: "view", HintID: "content"},
		{Name: "content link, untracked is the working tree", Repo: "gigagit", Path: "n.txt", State: "untracked", HintKind: "view", HintID: "content"},
		{Name: "content link on a commit refuses", Repo: "gigagit", Path: "a/b.go", Rev: fullSha, State: "commit", HintKind: "view", HintID: "content"},
		{Name: "content link on staged refuses", Repo: "gigagit", Path: "a/b.go", State: "staged", HintKind: "view", HintID: "content"},
		{Name: "content link with no path refuses", Repo: "gigagit", State: "unstaged", HintKind: "view", HintID: "content"},
		{Name: "view hint with another id refuses", Repo: "gigagit", Path: "a/b.go", State: "unstaged", HintKind: "view", HintID: "blame"},
```

and mirror the rule in the Go reference `wantLinkPreview` (right after its
hint-OK check):

```go
	if hintKind == model.ContentHintKind &&
		(hintID != model.ContentHintID || path == "" || st == "staged" || st == "commit" || source != "" || side == "old") {
		return ""
	}
```

In `TestLinksJSIsWiredEverywhere`'s `checks`, add:

```go
		{"links.js", `"copy file link"`, "file rows must offer the content link"},
		{"links.js", "/api/worktree-present", "the content link is copied only after the presence check"},
		{"links.js", "is not in the working tree", "a missing file must say so on the status line"},
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/web -run 'TestWorktreePresent|TestSteerWireRefusesAContent|TestLinkForJSMatchesGo|TestLinksJSIsWired' -count=1`
Expected: FAIL — 404 on the endpoint, no refusal, JS parity mismatches, missing wiring substrings.

- [ ] **Step 3: Implement.**

`worktreepresent.go`:

```go
package web

import (
	"errors"
	"net/http"
	"strings"
)

// handleWorktreePresent serves GET /api/worktree-present?path=<p>: is that
// file on disk in this worktree? The web's "copy file link" asks it before
// copying a content link. A stat, no git (domain.WorktreeFilesPresent); an
// escaping path is simply absent.
func (s *Server) handleWorktreePresent(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" {
		writeErr(w, http.StatusBadRequest, errors.New("path required"))
		return
	}
	got, err := s.service().WorktreeFilesPresent(r.Context(), []string{p})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]bool{"present": got[p]})
}
```

`server.go`, beside the linkhist routes:

```go
	mux.HandleFunc("GET /api/worktree-present", s.handleWorktreePresent)
```

`steer.go` `toSteerWire`, inside `if c.HintKind != "" {` after the id check:

```go
		if c.HintKind == model.ContentHintKind {
			// The page has no content viewer yet (spec: v1 is TUI-only).
			return w, errors.New("content links are not supported in gg web yet")
		}
```

`linkbase.go` L65: `if l.Hint.Kind != "" && l.Hint.Kind != model.ContentHintKind {`
(a view hint is not a saved surface to be "copied from").

`static/links.js`:

```js
function linkHintKindOK(kind) {
  return kind === "bookmark" || kind === "shelf" || kind === "stash" || kind === "preview" || kind === "view";
}
```

In `linkFor`, right after the `if (hint && !(linkHintKindOK…)) return "";` line:

```js
  // A content link (?view=content) names the file ON DISK: a working-tree
  // path, no target, no old side (internal/model ParseLink refuses the rest).
  if (hint && hint.kind === "view") {
    const st0 = (ctx && ctx.state) || "unstaged";
    if (hint.id !== "content" || !(ctx && ctx.path) || preview || pair ||
        (st0 !== "unstaged" && st0 !== "untracked") || side === "old") return "";
  }
```

In the `registerRows("file", …)` contributor, build the second row and return both:

```js
  // "copy file link": the file's CONTENT link — no commit, the file as it is
  // on disk — copied only after the server says the file is there.
  const flink = linkFor(state.repo, state.worktree, {
    path: ctx.path,
    state: "unstaged",
    hint: { kind: "view", id: "content" },
  });
  const rows = link ? [copyLinkRow(link, linkDesc("file", ctx.path, ""))] : [];
  if (flink) rows.push({ label: "copy file link", act: () => copyFileLink(ctx.path, flink) });
  return rows;
```

with, above `registerRows("file"`:

```js
function copyFileLink(path, link) {
  fetch("/api/worktree-present?path=" + encodeURIComponent(path))
    .then((r) => (r.ok ? r.json() : { present: false }))
    .then((j) => {
      if (j.present) copyLink(link, linkDesc("file", path, ""));
      else opLine(path + " is not in the working tree", true);
    })
    .catch(() => opLine("copy failed (server unreachable)", true));
}
```

(`opLine` must be imported in links.js if it is not already — check the
file's import line from `layers.js`.)

- [ ] **Step 4: Run to verify they pass** — plus the package:

Run: `go test ./internal/web -count=1`
Expected: PASS (node present: the parity test runs; absent: it skips — say so in the final report).

- [ ] **Step 5: Browser check (visibility, against the UNFIXED build first).**
Build `go build -o /tmp/claude-1000/gg-fcl ./cmd/gg` from `main` first, then
from this branch; for each: start `gg web --addr 127.0.0.1:<free port>` in a
scratch repo with a committed file and a second commit that deletes another
file; `curl /api/repo` to prove the port is this binary. In the browser, open
the commit's files, right-click the surviving file, and assert the
`copy file link` row is VISIBLE; click it and read the clipboard (or the op
line "copied gg link"); right-click the deleted file, click the row, assert
the op line shows `<path> is not in the working tree`. Unfixed build: the row
is absent (check fails). Fixed build: both assertions pass.

- [ ] **Step 6: Commit**

```bash
git add internal/web
git commit -m "feat(web): copy file link on file rows; a steered page refuses content links"
```

---

### Task 7: docs, agent skill, e2e

**Files:**
- Modify: `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go` (`Version` 91 → 92)
- Modify: `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`
- Create: `e2e/scenarios/s93_content_links.toml`

- [ ] **Step 1: e2e scenario** (`s93_content_links.toml`):

```toml
name = "content links: gg link --content prints ?view=content, refuses a missing file, and non-navigate verbs refuse the link"

[input]
steps = [
  { write = "f1.txt", content = "v1\n" },
  { commit = "c1" },
  { write = "f1.txt", content = "dirty\n" },
]

[[run]]
cmd             = ["link", "--content", "f1.txt"]
exit            = 0
stdout_contains = ["gg:///", "/f1.txt?view=content"]

[[run]]
cmd  = ["link", "--content", "gone.txt"]
exit = 1

[[run]]
cmd  = ["link", "--content", "--rev", "HEAD", "f1.txt"]
exit = 2

[[run]]
cmd  = ["diff", "gg://{{cwd}}/f1.txt?view=content"]
exit = 2

[[run]]
cmd             = ["link", "resolve", "gg://{{cwd}}/f1.txt?view=content"]
exit            = 0
stdout_contains = ["f1.txt"]
```

Run: `go test ./e2e -run 'TestScenarios/s93' -count=1` (use the harness's
real test/subtest name — `grep -n "func Test" e2e/*.go`).
Expected: PASS.

- [ ] **Step 2: Agent skill.** In `using-gg.md`, under "### gg links", after
the `gg open <link>` paragraph, add:

```markdown
A **content link** names a file as it is ON DISK in the worktree, not a
diff: `gg://<repo>/<path>?view=content`. `gg link --content <path>` prints
one (exit 1 when the file is not in the working tree; `--content` takes no
target flag, hint flag, `:<line>` or `#<hunk>`). `gg open` / `gg session
navigate` show it in the TUI's content viewer; `gg open --web` refuses it
(exit 2) — the web page has no viewer yet. Every other link-taking verb
(`gg diff`, `gg show`, `gg note …`, `gg session highlight add`) refuses it
(exit 2). When the user asks you to "open" a file for them, this is the link
to build: `gg link --content <path>` → `gg open <link>`.
```

and add `| --content` to the `gg link [...]` synopsis line. Bump
`agentskill.Version` to 92.

- [ ] **Step 3: CHANGELOG / README / CLAUDE-details.** CHANGELOG (Unreleased):
one entry — "Copy file link (TUI `.` menu, web file-row menu) copies a
`?view=content` link to the file on disk; `gg link --content`; `gg open`
lands it in the TUI content viewer." README: the menu row + `gg link
--content` next to the existing link docs. `docs/CLAUDE-details.md` (links
section): the `view` hint kind — lands, never copied-from; working-tree only
(ParseLink + ResolveLink refusals); `linkShapes.Content` gate; web refuses in
`toSteerWire`; v2 = line focus.

- [ ] **Step 4: Full gates.**

Run: `./test.sh` then `./test.sh race` (start race immediately; do not wait
for other sessions).
Expected: all green. `go build ./cmd/gg` into the worktree and deliver the
binary path.

- [ ] **Step 5: Commit**

```bash
git add internal/agentskill CHANGELOG.md README.md docs/CLAUDE-details.md e2e/scenarios/s93_content_links.toml
git commit -m "docs: content links — skill v92, changelog, readme, e2e"
```
