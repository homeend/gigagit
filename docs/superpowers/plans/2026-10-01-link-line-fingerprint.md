# Line Fingerprints on Uncommitted `gg://` Links — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo forbids implementer subagents — CLAUDE.md). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A link to an uncommitted line carries a fingerprint of that line; opening it re-finds the line if it moved and says when it has changed.

**Architecture:** The grammar gains `:<line>~<fp>` (`model.Link.Fingerprint`). Every consumer already resolves through `domain.ResolveLink`, so the re-find lives there once and reports a `LineAnchor`. Producers (TUI, web, `gg link`) add the fingerprint from the line's raw text; consumers surface one shared note.

**Tech Stack:** Go 1.26 (stdlib `hash/fnv`), vanilla JS (web SPA), node-driven JS tests, TOML e2e scenarios.

**Spec:** `docs/superpowers/specs/2026-10-01-link-line-fingerprint-design.md`

## Global Constraints

- Worktree `/work/gigagit/.claude/worktrees/link-line-fingerprint`, branch `feat/link-line-fingerprint`. Every command runs there (`cd <abs>` first; the shell cwd resets). Never `git add -A` (a built `bin/gg` lives there).
- Fingerprint = 8 lowercase hex of FNV-1a 32-bit over the UTF-8 bytes of the line with leading/trailing whitespace trimmed. A line empty after trimming has NO fingerprint.
- In scope only: working-tree (`StateUnstaged`), `@staged`, and `?view=content` line links. A fingerprint on a commit / pair / preview / ref link, on a hunk link, or with no line is refused by `ParseLink`.
- Nothing refuses at resolve time: `same` / `moved` / `changed`, never an error.
- Engine/CLI/reply prose is English; TUI-visible strings go through `i18n.T` with keys in all four bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`).
- TUI/CLI never import `internal/git`; reads go through `domain`.
- New tests call `t.Parallel()`. After EVERY `internal/web/static/*.js` edit run `go test ./internal/web` (it has source-pin tests).
- Commits: `gg add <paths>` then `git commit -F <msgfile>`; trailers `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_019c4Fp5r9QbEtQ5dmcGUedV`.

## Review Focus

1. **A path or checkout containing `~` or `:`** (e.g. `gg:///C:/src/a~b/f.go:3~0a1b2c3d`, `gg://r/dir~x/f.go`) must parse as before; only `<n>~<8hex>` after the last `:` is a fingerprint. → Task 1 tests.
2. **CRLF files and tabs**: the fingerprint of a line read from disk (`\r\n`), from the diff row, and from the browser must agree. → Task 1 (vectors), Task 2 (CRLF resolve), Task 7 (JS↔Go pin + browser check on a tab-indented line).
3. **Duplicate lines** (`}`, `return nil`): the nearest match wins and the note says how many matched; a tie picks the lower line. → Task 2 table.
4. **File deleted / binary / unreadable at resolve time**: resolves `changed` at the asked line, no error. → Task 2 test.
5. **A link copied from a diff's OLD side** (`:old:N` = index text; `@staged:old:N` = HEAD text) must be checked against that side's text, not the working file. → Task 2 real-repo test.

---

### Task 1: Grammar — `Link.Fingerprint` and `LineFingerprint`

**Files:**
- Modify: `internal/model/link.go` (the `Link` struct ~L140; `String()` line arm ~L250; `ParseLink` after the `splitLinkLine` calls ~L340 and before the local-form return ~L420; `splitLinkLine` ~L487)
- Create: `internal/model/linkfp.go`
- Test: `internal/model/linkfp_test.go`

**Interfaces:**
- Produces: `model.LineFingerprint(line string) string` ("" for a blank line); `Link.Fingerprint string`; `model.LinkFingerprintOK(fp string) bool`.

- [ ] **Step 1: Write the failing test** — `internal/model/linkfp_test.go`

```go
package model

import (
	"errors"
	"testing"
)

func TestLineFingerprint(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ line, want string }{
		{"alpha", "5d8b6dab"},
		{"  alpha\t", "5d8b6dab"}, // indentation and trailing space are not content
		{"alpha\r", "5d8b6dab"},   // a CRLF file's line
		{"beta", "af81e4c7"},
		{"x := 1", "fb8130f3"}, // inner whitespace IS content
		{"", ""},
		{" \t ", ""}, // a blank line has no fingerprint
	} {
		if got := LineFingerprint(c.line); got != c.want {
			t.Errorf("LineFingerprint(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}

func TestLinkFingerprintRoundTrips(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"gg://gigagit/a/b.go:33~5d8b6dab",
		"gg://gigagit/a/b.go:old:18~af81e4c7",
		"gg://gigagit/a/b.go@staged:7~5d8b6dab",
		"gg://gigagit/a/b.go@staged:old:7~5d8b6dab",
		"gg://gigagit/a/b.go:3~5d8b6dab?view=content",
		"gg:///home/u/repo/a.go:3~5d8b6dab",
		"gg:///C:/src/repo/a.go:3~5d8b6dab",
		"gg://gigagit/dir~x/b~c.go:3~5d8b6dab", // '~' in a path is still a path
		"gg://gigagit/dir~x/b~c.go:3",
		"gg://gigagit/dir~x/b~c.go",
	} {
		l, err := ParseLink(s)
		if err != nil {
			t.Errorf("ParseLink(%q) = %v", s, err)
			continue
		}
		if got := l.String(); got != s {
			t.Errorf("String(Parse(%q)) = %q", s, got)
		}
	}
	l, _ := ParseLink("gg://gigagit/a/b.go:old:18~af81e4c7")
	if l.Line != 18 || l.Side != NoteSideOld || l.Fingerprint != "af81e4c7" || l.Path != "a/b.go" {
		t.Errorf("parsed = %+v", l)
	}
	if l, _ := ParseLink("gg:///C:/src/repo/a.go:3~5d8b6dab"); l.Line != 3 || l.Fingerprint != "5d8b6dab" || l.Repo.Abs != "C:/src/repo/a.go" {
		t.Errorf("local form parsed = %+v", l)
	}
}

func TestLinkFingerprintRefusals(t *testing.T) {
	t.Parallel()
	sha := "0123456789abcdef0123456789abcdef01234567"
	for _, s := range []string{
		"gg://r/a.go:3~5D8B6DAB",                    // upper case
		"gg://r/a.go:3~5d8b6da",                     // 7 chars
		"gg://r/a.go:3~5d8b6dabc",                   // 9 chars
		"gg://r/a.go:3~",                            // empty
		"gg://r/a.go:3~zzzzzzzz",                    // not hex
		"gg://r/a.go@" + sha + ":3~5d8b6dab",        // a commit is already fixed
		"gg://r/a.go@" + sha + ".." + sha + ":3~5d8b6dab",
		"gg://r/a.go@main...feat/x:3~5d8b6dab",
		"gg://r/a.go@ref:main:3~5d8b6dab",
	} {
		if _, err := ParseLink(s); !errors.Is(err, ErrLink) {
			t.Errorf("ParseLink(%q) = %v, want ErrLink", s, err)
		}
	}
	// String never writes a fingerprint without a line.
	l := Link{Repo: LinkRepo{Name: "r"}, Path: "a.go", Fingerprint: "5d8b6dab"}
	if got := l.String(); got != "gg://r/a.go" {
		t.Errorf("String = %q", got)
	}
}
```

- [ ] **Step 2: Run it — expect a compile failure** (`LineFingerprint`, `Fingerprint` undefined)

Run: `cd /work/gigagit/.claude/worktrees/link-line-fingerprint && go test ./internal/model -run 'Fingerprint' 2>&1 | tail -5`

- [ ] **Step 3: Implement**

`internal/model/linkfp.go`:

```go
package model

import (
	"fmt"
	"hash/fnv"
	"strings"
)

// LineFingerprint is the 8-hex fingerprint an uncommitted line link carries
// (`:<line>~<fp>`): FNV-1a 32-bit over the line with its leading and trailing
// whitespace trimmed, so re-indenting (or a CRLF ending) is not a change. A
// line that is blank after trimming has none ("") — it would match every
// blank line. It is deliberately NOT NoteContextHash's sha256: the browser
// builds links synchronously, where sha256 is async-only, and 32 bits tell
// "this line" from "another line" inside one file. The JS twin is
// lineFingerprint in internal/web/static/links.js; a test pins the pair.
func LineFingerprint(line string) string {
	t := strings.TrimSpace(line)
	if t == "" {
		return ""
	}
	h := fnv.New32a()
	h.Write([]byte(t))
	return fmt.Sprintf("%08x", h.Sum32())
}

// LinkFingerprintOK reports whether fp is a well-formed fingerprint: exactly
// 8 lowercase hex characters.
func LinkFingerprintOK(fp string) bool {
	if len(fp) != 8 {
		return false
	}
	for i := 0; i < len(fp); i++ {
		if c := fp[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
```

`internal/model/link.go`:

1. `Link` struct — add after `Line`:

```go
	// Fingerprint is LineFingerprint of the line the link was copied from
	// ("" = none). Only an uncommitted line link carries one: the resolver
	// re-finds the line by it when the file has moved on.
	Fingerprint string
```

2. `String()` — in the `case l.Line > 0:` arm, after `b.WriteString(strconv.Itoa(l.Line))`:

```go
		if l.Fingerprint != "" {
			b.WriteByte('~')
			b.WriteString(l.Fingerprint)
		}
```

3. `splitLinkLine` — new signature `(rest string, side NoteSide, line int, fp string, err error)`. Replace the `num := t[i+1:]` / `Atoi` head with:

```go
	num := t[i+1:]
	hasFP := false
	if j := strings.IndexByte(num, '~'); j >= 0 {
		// "<n>~<fp>" — only when what precedes the '~' is a number: a path
		// segment like "b~c.go" after a drive colon is left alone, exactly as
		// a non-numeric tail always was.
		if _, nerr := strconv.Atoi(num[:j]); nerr == nil {
			num, fp, hasFP = num[:j], num[j+1:], true
		}
	}
	n, cerr := strconv.Atoi(num)
	if cerr != nil {
		if num == "" {
			return "", "", 0, "", fmt.Errorf("%w: a line number is missing after \":\"", ErrLink)
		}
		return t, NoteSideNew, 0, "", nil
	}
	if hasFP && !LinkFingerprintOK(fp) {
		return "", "", 0, "", fmt.Errorf("%w: a line fingerprint is 8 lowercase hex characters, got %q", ErrLink, fp)
	}
```

and thread `fp` through the remaining returns (`""` on the early ones, `fp` on the final one).

4. `ParseLink` — the two call sites become `tail, side, line, fp, err = splitLinkLine(tail)` / `head, …`; declare `var fp string`; after `l.Side, l.Line = side, line` add `l.Fingerprint = fp`. After the target `switch` (just before the `if l.IsContent()` block) add:

```go
	if l.Fingerprint != "" && l.Target.State == StateCommitted {
		return linkErr("a commit already names fixed content; drop \"~%s\"", l.Fingerprint)
	}
```

(`StateCommitted` covers commit, pair, preview and ref targets; a hunk link never reaches here with a fingerprint because `#` is stripped before the line is read and the fingerprint only parses after a line number.)

- [ ] **Step 4: Run** `go test ./internal/model 2>&1 | tail -5` — expect `ok`. Then `go build ./... && go vet ./internal/model`.

- [ ] **Step 5: Commit** — `gg add internal/model/link.go internal/model/linkfp.go internal/model/linkfp_test.go`; message `feat(model): gg:// line links carry an optional ~<fingerprint>`.

---

### Task 2: Resolver — re-find the line, report a `LineAnchor`

**Files:**
- Create: `internal/domain/linkanchor.go`, `internal/domain/linkanchor_test.go`
- Modify: `internal/domain/linkresolve.go` (`Resolved` struct ~L29; `finishLink`'s final `default:` arm and the staged case ~L625)

**Interfaces:**
- Consumes: `model.LineFingerprint`, `Link.Fingerprint`.
- Produces:

```go
const (AnchorSame = "same"; AnchorMoved = "moved"; AnchorChanged = "changed")
type LineAnchor struct { Asked int; State string; Matches int }
// Resolved.Anchor LineAnchor   (State "" = the link carried no fingerprint)
func AnchorNote(asked, line int, state string, matches int) string
func (r Resolved) AnchorNote() string
```

- [ ] **Step 1: Failing tests** — `internal/domain/linkanchor_test.go`

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestAnchorLine(t *testing.T) {
	t.Parallel()
	fp := model.LineFingerprint
	lines := []string{"alpha", "}", "beta", "}", "gamma", "}"}
	for _, c := range []struct {
		name               string
		asked              int
		fp                 string
		line               int
		state              string
		matches            int
	}{
		{"same", 3, fp("beta"), 3, AnchorSame, 1},
		{"moved, one match", 1, fp("gamma"), 5, AnchorMoved, 1},
		{"moved, nearest of several", 5, fp("}"), 4, AnchorMoved, 3},
		{"a tie picks the lower line", 3, fp("}"), 2, AnchorMoved, 3},
		{"same wins over other matches", 4, fp("}"), 4, AnchorSame, 3},
		{"changed", 3, fp("delta"), 3, AnchorChanged, 0},
		{"asked past the end, text found", 40, fp("alpha"), 1, AnchorMoved, 1},
		{"asked past the end, text gone", 40, fp("delta"), 40, AnchorChanged, 0},
	} {
		line, state, matches := anchorLine(lines, c.asked, c.fp)
		if line != c.line || state != c.state || matches != c.matches {
			t.Errorf("%s: anchorLine = %d,%q,%d want %d,%q,%d", c.name, line, state, matches, c.line, c.state, c.matches)
		}
	}
}

func TestAnchorNote(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		asked, line, matches int
		state, want          string
	}{
		{33, 33, 1, AnchorSame, ""},
		{33, 33, 0, "", ""},
		{33, 41, 1, AnchorMoved, "line 33 moved to 41"},
		{33, 41, 3, AnchorMoved, "line 33 moved to 41 (nearest of 3 matching lines)"},
		{33, 33, 0, AnchorChanged, "line 33 has changed since this link was copied"},
	} {
		if got := AnchorNote(c.asked, c.line, c.state, c.matches); got != c.want {
			t.Errorf("AnchorNote(%d,%d,%q,%d) = %q, want %q", c.asked, c.line, c.state, c.matches, got, c.want)
		}
	}
}

// The side decides WHICH text is checked: the working file, the index or HEAD.
func TestResolveLinkReanchorsOnTheRightText(t *testing.T) {
	t.Parallel()
	dir := newRepo(t) // the package's real-git helper
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("alpha\nbeta\ngamma\n")
	gitRun(t, dir, "add", "f.txt")
	gitRun(t, dir, "commit", "-m", "c1") // HEAD: beta at 2
	write("new\nalpha\nbeta\ngamma\n")
	gitRun(t, dir, "add", "f.txt") // index: beta at 3
	write("new\r\nnewer\r\nalpha\r\nbeta\r\ngamma\r\n") // working file (CRLF): beta at 4
	fp := model.LineFingerprint("beta")
	svc := Open(dir)
	for _, c := range []struct {
		name string
		l    model.Link
		line int
	}{
		{"working file", model.Link{Path: "f.txt", Side: model.NoteSideNew}, 4},
		{"unstaged old side = index", model.Link{Path: "f.txt", Side: model.NoteSideOld}, 3},
		{"staged new side = index", model.Link{Path: "f.txt", Side: model.NoteSideNew, Target: model.LinkTarget{State: model.StateStaged}}, 3},
		{"staged old side = HEAD", model.Link{Path: "f.txt", Side: model.NoteSideOld, Target: model.LinkTarget{State: model.StateStaged}}, 2},
		{"content link", model.Link{Path: "f.txt", Side: model.NoteSideNew, Hint: model.ContentHint}, 4},
	} {
		l := c.l
		l.Line, l.Fingerprint = 1, fp // copied when beta was line 1
		// The local form learns its path in the resolver: spell it as ParseLink would.
		l.Repo.Abs, l.Path = filepath.ToSlash(filepath.Join(dir, "f.txt")), ""
		res, err := ResolveLink(context.Background(), l, ResolveOpts{Cwd: svc})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if res.Line != c.line || res.Anchor.State != AnchorMoved || res.Anchor.Asked != 1 {
			t.Errorf("%s: line=%d anchor=%+v, want line %d moved from 1", c.name, res.Line, res.Anchor, c.line)
		}
	}
	// No fingerprint: untouched, no read, no anchor.
	l := model.Link{Repo: model.LinkRepo{Abs: filepath.ToSlash(filepath.Join(dir, "f.txt"))}, Side: model.NoteSideNew, Line: 1}
	res, err := ResolveLink(context.Background(), l, ResolveOpts{Cwd: svc})
	if err != nil || res.Line != 1 || res.Anchor.State != "" {
		t.Errorf("plain link: line=%d anchor=%+v err=%v", res.Line, res.Anchor, err)
	}
	// The file is gone: changed, at the asked line, no error.
	os.Remove(filepath.Join(dir, "f.txt"))
	l.Fingerprint = fp
	res, err = ResolveLink(context.Background(), l, ResolveOpts{Cwd: svc})
	if err != nil || res.Line != 1 || res.Anchor.State != AnchorChanged {
		t.Errorf("deleted file: line=%d anchor=%+v err=%v", res.Line, res.Anchor, err)
	}
}
```

Before running: check the package's helper names with `grep -n "^func newRepo\|^func gitRun\|^func newTestRepo" internal/domain/*_test.go` and use the ones that exist (an existing `ResolveLink` test in `linkresolve_test.go` shows the exact fixture + `ResolveOpts` shape — copy it).

- [ ] **Step 2: Run — expect compile failure** `go test ./internal/domain -run 'Anchor|Reanchors' 2>&1 | tail -5`

- [ ] **Step 3: Implement** — `internal/domain/linkanchor.go`

```go
package domain

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// The states of a fingerprinted line link once resolved.
const (
	AnchorSame    = "same"    // the line still holds the text
	AnchorMoved   = "moved"   // the text is on another line now
	AnchorChanged = "changed" // the text is nowhere in the file
)

// LineAnchor says what became of a fingerprinted link's line. State "" means
// the link carried no fingerprint; Resolved.Line is then the link's own line.
type LineAnchor struct {
	Asked   int    // the line the link named
	State   string // "", AnchorSame, AnchorMoved, AnchorChanged
	Matches int    // lines carrying the fingerprint (0 when changed)
}

// anchorLine finds where the fingerprinted line is now. The asked line wins
// when it still matches; otherwise the nearest match (a tie: the lower line);
// with no match the asked line stands and the state is "changed".
func anchorLine(lines []string, asked int, fp string) (line int, state string, matches int) {
	best := 0
	for i, l := range lines {
		if model.LineFingerprint(l) != fp {
			continue
		}
		matches++
		n := i + 1
		if best == 0 || abs(n-asked) < abs(best-asked) {
			best = n
		}
	}
	switch {
	case matches == 0:
		return asked, AnchorChanged, 0
	case asked >= 1 && asked <= len(lines) && model.LineFingerprint(lines[asked-1]) == fp:
		return asked, AnchorSame, matches
	}
	return best, AnchorMoved, matches
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// AnchorNote is the one sentence every consumer says about a re-anchored
// link ("" when there is nothing to say). English: it is agent-facing.
func AnchorNote(asked, line int, state string, matches int) string {
	switch state {
	case AnchorMoved:
		if matches > 1 {
			return fmt.Sprintf("line %d moved to %d (nearest of %d matching lines)", asked, line, matches)
		}
		return fmt.Sprintf("line %d moved to %d", asked, line)
	case AnchorChanged:
		return fmt.Sprintf("line %d has changed since this link was copied", asked)
	}
	return ""
}

// AnchorNote is AnchorNote for this resolution.
func (r Resolved) AnchorNote() string {
	return AnchorNote(r.Anchor.Asked, r.Line, r.Anchor.State, r.Anchor.Matches)
}

// anchorLink re-finds a fingerprinted link's line in the text its side names:
// the working file (new side, or a content link), the index (the unstaged
// diff's old side, the staged diff's new side) or HEAD (the staged diff's old
// side). Text that cannot be read — a deleted or binary file — is "changed".
func anchorLink(ctx context.Context, svc *Service, l model.Link, res *Resolved) {
	if l.Fingerprint == "" || l.Line <= 0 || res.Addr.Path == "" {
		return
	}
	ref := model.FileRef{Source: model.SourceUnstaged, Path: res.Addr.Path}
	staged, old := l.Target.State == model.StateStaged, l.Side == model.NoteSideOld
	switch {
	case staged && old:
		ref = model.FileRef{Source: model.SourceCommit, Locator: "HEAD", Path: res.Addr.Path}
	case staged || old:
		ref.Source = model.SourceStaged
	}
	res.Anchor = LineAnchor{Asked: l.Line, State: AnchorChanged}
	data, err := svc.ResolveBytes(ctx, ref)
	if err != nil || strings.IndexByte(string(data), 0) >= 0 {
		return
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimRight(strings.ReplaceAll(text, "\r", "\n"), "\n")
	var lines []string
	if text != "" {
		lines = strings.Split(text, "\n")
	}
	res.Line, res.Anchor.State, res.Anchor.Matches = anchorLine(lines, l.Line, l.Fingerprint)
}
```

(If the package already defines `abs`, drop this one — `grep -n "^func abs" internal/domain/*.go`.)

`linkresolve.go`: add to `Resolved` after `Line`:

```go
	// Anchor says what became of a FINGERPRINTED link's line (link.go's
	// ~<fp>): Line is then where the text is NOW. Zero for every other link.
	Anchor LineAnchor
```

In `finishLink`, the last `switch l.Target.State` — make the tail:

```go
	default:
		res.Addr.Worktree = c.checkout
	}
	anchorLink(ctx, opts.OpenFn(c.checkout), l, &res)
	return res, nil
```

(the committed arm's early `return res, nil` paths never carry a fingerprint — ParseLink refuses them — so only this fall-through needs the call; the plain-commit arm also falls through and `anchorLink` returns at once on an empty fingerprint).

- [ ] **Step 4: Run** `go test ./internal/domain 2>&1 | tail -5` — expect `ok`.

- [ ] **Step 5: Prove it bites** — comment out the `anchorLink(...)` call, run `go test ./internal/domain -run Reanchors`, expect FAIL on "want line 4 moved from 1"; restore the line by re-typing it (never `git checkout` the file).

- [ ] **Step 6: Commit** — `gg add internal/domain/linkanchor.go internal/domain/linkanchor_test.go internal/domain/linkresolve.go`; message `feat(domain): ResolveLink re-finds a fingerprinted line and reports the anchor`.

---

### Task 3: CLI — `gg link` emits it; every verb says the note; `link resolve` reports it

**Files:**
- Modify: `internal/cli/link.go` (flags ~L60–100; after `buildLink` ~L160; `wireResolvedLink` ~L470; `linkResolve` JSON + plain ~L520–575), `internal/cli/diff.go:92`, `internal/cli/note.go:46`, `internal/cli/session.go:582,872`, `internal/cli/show.go:43`, `internal/cli/open.go:67`
- Test: `internal/cli/link_fp_test.go`

**Interfaces:**
- Consumes: `domain.Resolved.Anchor`, `Resolved.AnchorNote()`, `model.LineFingerprint`.
- Produces: `warnAnchor(stderr io.Writer, res domain.Resolved)`; JSON fields `asked_line`, `anchor`, `anchor_matches`.

- [ ] **Step 1: Failing test** — `internal/cli/link_fp_test.go`. Model it on the nearest existing `gg link` test (`grep -n "func Test.*Link" internal/cli/link_test.go | head` — copy its repo fixture and its `run(...)`/stdout-capture helper names verbatim). Cases, each asserting exact text:

```go
// f.txt = "alpha\nbeta\ngamma\n" committed, then edited to "alpha\nbeta2\ngamma\n" (unstaged)
// 1. gg link f.txt:2            → stdout ends ":2~" + model.LineFingerprint("beta2")
// 2. gg link --cached f.txt:1   → ends "@staged:1~" + model.LineFingerprint("alpha")
// 3. gg link f.txt:old:2        → ends ":old:2~" + model.LineFingerprint("beta")   (index text)
// 4. gg link --content f.txt:3  → contains ":3~" + fp("gamma") + "?view=content"
// 5. gg link --no-fingerprint f.txt:2 → ends ":2" (no '~')
// 6. gg link --rev HEAD f.txt:2 → no '~' (a commit link)
// 7. gg link f.txt              → no '~' (no line)
// 8. blank line: file "a\n\nb\n", gg link g.txt:2 → ends ":2" (no '~')
// 9. gg diff <link from 1, after prepending a line to f.txt> → exit 0, stderr contains
//    "gg: line 2 moved to 3"
// 10. gg link resolve --json <same link> → JSON line == 3, asked_line == 2, anchor == "moved"
// 11. gg link resolve <same link> (plain) → second line ends "new:3 (line 2 moved to 3)"
// 12. after rewriting line "beta2" → "zzz": gg diff <link> stderr contains
//     "gg: line 2 has changed since this link was copied"; exit 0
```

Write each as a real assertion using the fixture helpers found above (twelve `t.Run` subtests, `t.Parallel()` on the parent).

- [ ] **Step 2: Run — expect FAIL** `go test ./internal/cli -run LinkFingerprint 2>&1 | tail -8`

- [ ] **Step 3: Implement**

`link.go` — add the flag beside the others: `noFP := fs.Bool("no-fingerprint", false, "omit the ~<fingerprint> an uncommitted line link carries")`. After the `--content` presence block and before `svc.RecordLink`, add:

```go
	if !*noFP {
		l.Fingerprint = lineLinkFingerprint(ctx, svc, l)
	}
```

and the helper:

```go
// lineLinkFingerprint reads the line an uncommitted line link names — the
// working file, the index or HEAD, by the link's side (domain.anchorLink's
// rule) — and fingerprints it. "" for every other link, an unreadable file, a
// line past the end, or a blank line: the plain form is then what is printed.
func lineLinkFingerprint(ctx context.Context, svc *domain.Service, l model.Link) string {
	if l.Line <= 0 || l.Path == "" || l.Target.State == model.StateCommitted {
		return ""
	}
	ref := model.FileRef{Source: model.SourceUnstaged, Path: l.Path}
	staged, old := l.Target.State == model.StateStaged, l.Side == model.NoteSideOld
	switch {
	case staged && old:
		ref = model.FileRef{Source: model.SourceCommit, Locator: "HEAD", Path: l.Path}
	case staged || old:
		ref.Source = model.SourceStaged
	}
	data, err := svc.ResolveBytes(ctx, ref)
	if err != nil {
		return ""
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(strings.ReplaceAll(text, "\r", "\n"), "\n")
	if l.Line > len(lines) {
		return ""
	}
	return model.LineFingerprint(lines[l.Line-1])
}
```

Add `warnAnchor` next to `resolveLinkArg`:

```go
// warnAnchor says, once, on stderr, that a fingerprinted link's line moved or
// changed. The verb then acts on the RESOLVED line.
func warnAnchor(stderr io.Writer, res domain.Resolved) {
	if n := res.AnchorNote(); n != "" {
		fmt.Fprintln(stderr, "gg: "+n)
	}
}
```

Call `warnAnchor(stderr, res)` immediately after the `if err != nil { return linkExit(...) }` block at each of the six `resolveLinkArg` call sites (diff.go:92, note.go:46, session.go:582, session.go:872, show.go:43, open.go:67).

`wireResolvedLink` — add after `Line`:

```go
	// AskedLine / Anchor / AnchorMatches describe a fingerprinted link
	// (…:N~<fp>): Line is where the text is NOW, AskedLine the line the link
	// named, Anchor "same" | "moved" | "changed".
	AskedLine     int    `json:"asked_line,omitempty"`
	Anchor        string `json:"anchor,omitempty"`
	AnchorMatches int    `json:"anchor_matches,omitempty"`
```

In `linkResolve`'s JSON arm: `w.AskedLine, w.Anchor, w.AnchorMatches = res.Anchor.Asked, res.Anchor.State, res.Anchor.Matches` (leave `AskedLine` 0 when `res.Anchor.State == ""`). In the plain arm, after `line += fmt.Sprintf(" %s:%d", res.Side, res.Line)`:

```go
		if n := res.AnchorNote(); n != "" {
			line += " (" + n + ")"
		}
```

Update `linkUsage` to mention `[--no-fingerprint]`.

- [ ] **Step 4: Run** `go test ./internal/cli 2>&1 | tail -5` — expect `ok` (existing link tests that pin exact `gg link <path>:<line>` output for a WORKING-TREE line will now see `~<fp>`; update each expectation to include the fingerprint computed with `model.LineFingerprint(<that line's text>)` — never by loosening the match).

- [ ] **Step 5: Commit** — `gg add internal/cli`; message `feat(cli): gg link fingerprints uncommitted lines; verbs report a moved or changed line`.

---

### Task 4: MCP — `gg_link_resolve` reports the anchor

**Files:**
- Modify: `internal/mcp/links.go` (`linkResolveOut` ~L33, the handler ~L130)
- Test: the existing `gg_link_resolve` test file (`grep -ln gg_link_resolve internal/mcp/*_test.go`)

- [ ] **Step 1: Failing test** — add a subtest beside the existing resolve tests: build a repo with `f.txt = "alpha\nbeta\n"`, prepend a line on disk, resolve `gg://<abs>/f.txt:2~<fp(beta)>`, assert `out.Line == 3 && out.AskedLine == 2 && out.Anchor == "moved" && out.AnchorMatches == 1`; and for a plain link assert `out.Anchor == "" && out.AskedLine == 0`.

- [ ] **Step 2: Run — expect compile failure** `go test ./internal/mcp -run LinkResolve 2>&1 | tail -5`

- [ ] **Step 3: Implement** — add to `linkResolveOut` after `Line`:

```go
	// AskedLine / Anchor / AnchorMatches: a fingerprinted link's line as the
	// link named it, and what became of it ("same" | "moved" | "changed").
	// Line is where the text is NOW.
	AskedLine     int    `json:"asked_line,omitempty"`
	Anchor        string `json:"anchor,omitempty"`
	AnchorMatches int    `json:"anchor_matches,omitempty"`
```

and in the handler after `out.Line = res.Line`:

```go
		if res.Anchor.State != "" {
			out.AskedLine, out.Anchor, out.AnchorMatches = res.Anchor.Asked, res.Anchor.State, res.Anchor.Matches
		}
```

Extend the tool `Description` with: `" A link ending :N~<fingerprint> is re-anchored: line is where that text is now, asked_line what the link named, anchor same|moved|changed."`

- [ ] **Step 4: Run** `go test ./internal/mcp 2>&1 | tail -3` — expect `ok` (a golden of the tool list, if any, must be updated by reading it, not bulk-accepted).

- [ ] **Step 5: Commit** — `gg add internal/mcp`; message `feat(mcp): gg_link_resolve reports a fingerprinted line's anchor`.

---

### Task 5: Landing — the steer command carries the anchor; TUI and web say it

**Files:**
- Modify: `internal/steer/steer.go` (`Line` ~L72), `internal/linknav/linknav.go` (every place a `steer.Line` is built from `res.Line` — the pair arm ~L190, the preview/ref arms, the tail ~L218), `internal/tui/steer_nav.go` (`steerOpenedNotice` ~L1169, `landSteer`'s detail ~L893, the parked-stack branch ~L832, and the content-link landing — `grep -n "at line" internal/tui/*.go`), `internal/i18n/lang/{ja,ko,zh,ru}.toml`, `internal/web/steer.go` (`steerWire` + `toSteerWire`), `internal/web/static/live.js` (the `if (!s.line) return;` tail ~L549 and `steerNavigateContent`)
- Test: `internal/linknav/linknav_test.go`, `internal/tui/steer_nav_test.go`, `internal/web` steer test

**Interfaces:**
- Consumes: `domain.Resolved.Anchor`, `domain.AnchorNote`.
- Produces: `steer.Line{Side, No, Asked int, Anchor string, Matches int}` (the three new fields `omitempty`); `linknav.lineOf(res domain.Resolved) steer.Line`; TUI `anchorNotice(l *steer.Line) string`.

- [ ] **Step 1: Failing tests**

`internal/linknav/linknav_test.go` — add:

```go
func TestCommandCarriesTheAnchor(t *testing.T) {
	t.Parallel()
	res := domain.Resolved{
		Addr: model.FileAddress{Path: "f.txt", State: model.StateUnstaged},
		Line: 41, Side: model.NoteSideNew,
		Anchor: domain.LineAnchor{Asked: 33, State: domain.AnchorMoved, Matches: 2},
	}
	c, err := Command(context.Background(), nil, res)
	if err != nil {
		t.Fatal(err)
	}
	if c.Line == nil || c.Line.No != 41 || c.Line.Asked != 33 || c.Line.Anchor != "moved" || c.Line.Matches != 2 {
		t.Fatalf("line = %+v", c.Line)
	}
}
```

(use `Command`'s real signature — read its declaration first; if it needs a non-nil service for this shape, build one with the package's existing fixture.)

`internal/tui/steer_nav_test.go` — add, modelled on `TestSteerNavigateLandsOnTheOldSide`:

```go
func TestSteerNavigateSaysALineMoved(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	dir := m.steerDir
	m, cmd := m.applySteer(steer.Command{
		ID: "n-fp", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "unstaged"},
		Line:   &steer.Line{Side: "new", No: 18, Asked: 12, Anchor: "moved", Matches: 1},
		Wait:   true,
	})
	m = pumpDiff(t, m, cmd)
	if !strings.Contains(m.diffNotice, "line 12 moved to 18") {
		t.Errorf("diffNotice = %q, want the move said", m.diffNotice)
	}
	r, ok := steer.AwaitReply(dir, "n-fp", 2*time.Second)
	if !ok || !r.OK || !strings.Contains(r.Detail, "line 12 moved to 18") {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}
```

plus a `changed` twin asserting `"line 12 has changed since this link was copied"` in both places.

- [ ] **Step 2: Run — expect compile failure** `go test ./internal/linknav ./internal/tui -run 'Anchor|SaysALine' 2>&1 | tail -6`

- [ ] **Step 3: Implement**

`steer.go`:

```go
type Line struct {
	Side string `json:"side"` // "new" | "old"
	No   int    `json:"no"`
	// Asked / Anchor / Matches describe a FINGERPRINTED link's line: No is
	// where its text is now, Asked the line the link named, Anchor "same" |
	// "moved" | "changed". Empty for every other command.
	Asked   int    `json:"asked,omitempty"`
	Anchor  string `json:"anchor,omitempty"`
	Matches int    `json:"matches,omitempty"`
}
```

`linknav.go` — add and use everywhere a line is built from `res`:

```go
// lineOf is the landing line of a resolution, anchor included.
func lineOf(res domain.Resolved) steer.Line {
	return steer.Line{Side: string(res.Side), No: res.Line,
		Asked: res.Anchor.Asked, Anchor: res.Anchor.State, Matches: res.Anchor.Matches}
}
```

(`line := steer.Line{Side: string(res.Side), No: res.Line}` → `line := lineOf(res)` at each site; hunk-derived lines keep their own value.)

TUI `steer_nav.go`:

```go
// anchorNotice is the TRANSLATED sentence for a re-anchored landing ("" when
// the line is where the link said). The steer reply carries the English twin
// (domain.AnchorNote).
func anchorNotice(l *steer.Line) string {
	if l == nil {
		return ""
	}
	switch l.Anchor {
	case domain.AnchorMoved:
		if l.Matches > 1 {
			return i18n.T("line %d moved to %d (nearest of %d matching lines)", l.Asked, l.No, l.Matches)
		}
		return i18n.T("line %d moved to %d", l.Asked, l.No)
	case domain.AnchorChanged:
		return i18n.T("line %d has changed since this link was copied", l.Asked)
	}
	return ""
}
```

In `landSteer`, after `m.diffNotice = steerOpenedNotice(c, no)`:

```go
	if n := anchorNotice(c.Line); n != "" {
		m.diffNotice += " — " + n
	}
```

and after `detail := "opened " + …` (before the clamp suffix):

```go
	if n := domain.AnchorNote(c.Line.Asked, c.Line.No, c.Line.Anchor, c.Line.Matches); n != "" {
		detail += "; " + n
	}
```

Apply the same two additions in the parked-stack branch (`nm.diffNotice = steerOpenedNotice(c, no)` / `"opened "+c.File+":"+strconv.Itoa(no)`) and in the content-link landing (the viewer's `at line N` status/reply).

Add the three keys to all four bundles (alphabetical position as the files are sorted):

| key | ja | ko | zh | ru |
|-----|----|----|----|----|
| `line %d moved to %d` | `%d 行目は %d 行目に移動しました` | `%d번 줄이 %d번 줄로 이동했습니다` | `第 %d 行已移到第 %d 行` | `строка %d переместилась на %d` |
| `line %d moved to %d (nearest of %d matching lines)` | `%d 行目は %d 行目に移動しました（一致する %d 行のうち最も近い行）` | `%d번 줄이 %d번 줄로 이동했습니다(일치하는 %d줄 중 가장 가까운 줄)` | `第 %d 行已移到第 %d 行（%d 个匹配行中最近的一行）` | `строка %d переместилась на %d (ближайшая из %d совпадающих строк)` |
| `line %d has changed since this link was copied` | `%d 行目はこのリンクのコピー後に変更されています` | `%d번 줄은 이 링크를 복사한 뒤 변경되었습니다` | `第 %d 行在复制此链接后已更改` | `строка %d изменилась после копирования этой ссылки` |

Web `steer.go`: add `AnchorNote string \`json:"anchor_note,omitempty"\`` to `steerWire`; where `toSteerWire` copies `c.Line` into the wire's line/side, add `w.AnchorNote = domain.AnchorNote(c.Line.Asked, c.Line.No, c.Line.Anchor, c.Line.Matches)`. `live.js`: at the top of the `if (!s.line) return;` tail's continuation (right after that line) and in `steerNavigateContent` before `openViewer`: `if (s.anchor_note) opLine(s.anchor_note, s.anchor_note.includes("changed"));`. Add a Go test beside the existing `toSteerWire` tests asserting the field for a moved line and its absence for a plain one.

- [ ] **Step 4: Run** `go test ./internal/steer ./internal/linknav ./internal/tui ./internal/web ./internal/i18n 2>&1 | tail -6` — expect all `ok` (the i18n AST gates check the three new keys).

- [ ] **Step 5: Commit** — `gg add internal/steer internal/linknav internal/tui internal/web internal/i18n`; message `feat: a landing says when a fingerprinted line moved or changed (TUI, web, steer reply)`.

---

### Task 6: TUI producers

**Files:**
- Modify: `internal/tui/link.go` (`buildLinkFor` ~L67, `linkAnchorAtCursor`, the diff arm of `contextLinkText`, `compareLinkText`), `internal/tui/file_link.go` (`fileRowPath` ~L30, `contextFileLinkRow` ~L111)
- Test: `internal/tui/link_fp_test.go`

**Interfaces:**
- Consumes: `model.LineFingerprint`.
- Produces: `buildLinkFor(addr, side, line, hunk, hint, text string)` (new last parameter: the line's raw text, "" = none); `lineLinkFor(addr, side, line, text)`; `linkAnchorAtCursor() (side, line, text, ok)`; `fileRowPath() (path, line, text, ok)`.

- [ ] **Step 1: Failing test** — `internal/tui/link_fp_test.go`

```go
package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

func fpLinkModel(t *testing.T, state model.FileState) Model {
	t.Helper()
	rows := sameRowsTUI(40)
	rows[20] = textdiff.Row{Kind: textdiff.Del, Left: "\tgone", LeftNo: 21}
	rows[10] = textdiff.Row{Kind: textdiff.Add, Right: "", RightNo: 11} // a blank line
	m := openedDiffModel(12, rows, []int{20})
	m.linkRepoName, m.currentWorktree = "gigagit", "/repo"
	v := m.diffLayer()
	v.title = "a.txt"
	v.noteAddr = model.FileAddress{State: state, Path: "a.txt", Worktree: "/repo"}
	return m
}

func TestWorkingTreeLineLinkCarriesAFingerprint(t *testing.T) {
	t.Parallel()
	m := fpLinkModel(t, model.StateUnstaged)
	v := m.diffLayer()
	v.curLine = 5
	row, _ := v.cursorRow()
	wantLink(t, m, "gg://gigagit/a.txt:6~"+model.LineFingerprint(row.Right))
	v.curLine = 20 // the old side: the index's text, tab-indented
	wantLink(t, m, "gg://gigagit/a.txt:old:21~"+model.LineFingerprint("gone"))
	v.curLine = 10 // blank: the plain form
	wantLink(t, m, "gg://gigagit/a.txt:11")

	m = fpLinkModel(t, model.StateStaged)
	m.diffLayer().curLine = 5
	wantLink(t, m, "gg://gigagit/a.txt@staged:6~"+model.LineFingerprint(row.Right))

	m = fpLinkModel(t, model.StateUntracked)
	m.diffLayer().curLine = 5
	wantLink(t, m, "gg://gigagit/a.txt:6~"+model.LineFingerprint(row.Right))
}

func TestCommittedLineLinkHasNoFingerprint(t *testing.T) {
	t.Parallel()
	m := fpLinkModel(t, model.StateCommitted)
	v := m.diffLayer()
	v.noteAddr.Commit = cmpShaA
	v.curLine = 5
	got, ok := m.contextLinkText()
	if !ok || strings.Contains(got, "~") {
		t.Fatalf("contextLinkText = %q ok=%v, want no fingerprint on a commit link", got, ok)
	}
}

// A compare against the working tree links the working side WITH a
// fingerprint and the commit side without.
func TestCompareWorkingSideCarriesAFingerprint(t *testing.T) {
	t.Parallel()
	m := compareLinkModel(t, mustCommitEndpoint(cmpShaA), model.WorkTreeEndpoint())
	v := m.diffLayer()
	v.curLine = 5
	row, _ := v.cursorRow()
	wantLink(t, m, "gg://gigagit/a.txt:6~"+model.LineFingerprint(row.Right))
	v.curLine = 20
	wantLink(t, m, "gg://gigagit/a.txt@"+cmpShaA+":21")
}
```

Update the earlier expectations this changes, by computing the fingerprint, in: `TestContextLinkTextOldSide` (`link_test.go`, `"gg://gigagit/a.txt:old:21"` → `+"~"+model.LineFingerprint("gone")`), `TestDiffViewLKeyCopiesTheCursorLink`, `TestCompareDiffLinkAgainstTheWorkingTree` (`link_compare_line_test.go`: the `a.txt:6` and `@staged:6` lines), and any other test `go test ./internal/tui` reports — each by adding the exact fingerprint, never by weakening the assertion.

- [ ] **Step 2: Run — expect FAIL** `go test ./internal/tui -run 'Fingerprint' 2>&1 | tail -8`

- [ ] **Step 3: Implement**

`buildLinkFor` gains `text string`; before `return l.String(), true`:

```go
	// An uncommitted line carries its fingerprint, so the link can be re-found
	// (or reported changed) once the file moves on. A commit is fixed already.
	if line > 0 && l.Target.State != model.StateCommitted {
		l.Fingerprint = model.LineFingerprint(text)
	}
```

`linkFor(addr, side, line, hunk)` and `hintedLinkFor` pass `""`; add:

```go
// lineLinkFor is linkFor for a LINE whose text is known: the text becomes the
// fingerprint when the address is uncommitted.
func (m Model) lineLinkFor(addr model.FileAddress, side model.NoteSide, line int, text string) (string, bool) {
	return m.buildLinkFor(addr, side, line, 0, model.LinkHint{}, text)
}
```

`linkAnchorAtCursor` returns `(model.NoteSide, int, string, bool)` — the text is `r.Left` on the old-side returns and `r.Right` on the new-side one. In `contextLinkText`'s diff arm: `side, line, text, has := m.linkAnchorAtCursor()`; the non-scope return becomes `return m.lineLinkFor(addr, side, line, text)`; `compareLinkText(side, line, text)` ends with `return m.lineLinkFor(addr, model.NoteSideNew, line, text)` (the pair arm ignores `text`).

`fileRowPath` returns `(path string, line int, text string, ok bool)`: the focused-doc arm returns `p.lines[p.cur].raw` as the text; every other return passes `""`. `contextFileLinkRow` passes it: `m.buildLinkFor(…, line, 0, model.ContentHint, text)`. Fix the other `fileRowPath` callers (`grep -n "fileRowPath()" internal/tui/*.go`) by ignoring the new value.

The `copy-file-link` path compares the disk text to the shown lines before copying (the "b + bottom-bar message" ruling) — leave that check as is; the fingerprint is taken from the shown line, which that check proves equals the disk's.

- [ ] **Step 4: Run** `go test ./internal/tui 2>&1 | tail -5` — expect `ok`. Regenerate the one golden the change touches — `go test ./e2e -run 'TestScenarios/tui_compare_line_link' ` should still PASS unchanged (its links are commit pairs); if any golden differs, read the `.actual` in full before `-update`.

- [ ] **Step 5: Commit** — `gg add internal/tui`; message `feat(tui): L / Copy link / Copy file link fingerprint an uncommitted line`.

---

### Task 7: Web producers — the JS twin and the row's raw text

**Files:**
- Modify: `internal/web/static/links.js` (inside the guarded pure section: add `lineFingerprint`; `linkFor` gains a 6th parameter `text`), `internal/web/static/files.js` (the diff-row menu ~L2973: pass the row's raw text; a `rowRawText` helper)
- Test: `internal/web/linkfpjs_test.go`

**Interfaces:**
- Produces: JS `lineFingerprint(text) → "" | 8 hex`; `linkFor(repo, worktree, ctx, side, no, text)`.

- [ ] **Step 1: Failing test** — `internal/web/linkfpjs_test.go`, the node pattern of `linkspairjs_test.go` (same `linksPureStart`/`linksPureEnd` slicing):

```go
func TestLineFingerprintJSMatchesGo(t *testing.T) {
	t.Parallel()
	// … node lookup + pure-section slice exactly as TestLinkForJSRendersACommitPair …
	lines := []string{"alpha", "  alpha\t", "alpha\r", "x := 1", "", " \t ", "héllo — ünï", "\tif err != nil {", "日本語"}
	// script: print JSON.stringify(lines.map(lineFingerprint))
	// assert got[i] == model.LineFingerprint(lines[i]) for every i
	type lcase struct{ State, Side, Text string; No int; want string }
	fp := model.LineFingerprint("x := 1")
	cases := []lcase{
		{"unstaged", "new", "x := 1", 7, "gg://gigagit/a.go:7~" + fp},
		{"unstaged", "old", "x := 1", 7, "gg://gigagit/a.go:old:7~" + fp},
		{"untracked", "new", "x := 1", 7, "gg://gigagit/a.go:7~" + fp},
		{"staged", "new", "x := 1", 7, "gg://gigagit/a.go@staged:7~" + fp},
		{"unstaged", "new", "   ", 7, "gg://gigagit/a.go:7"},       // blank: plain
		{"unstaged", "new", "x := 1", 0, "gg://gigagit/a.go"},      // no line: plain
		{"commit", "new", "x := 1", 7, "gg://gigagit/a.go@" + strings.Repeat("a", 40) + ":7"}, // a commit: never
	}
	// script: linkFor({link_repo:"gigagit"}, "", {path:"a.go", state:c.State, rev: c.State==="commit" ? "a"×40 : ""}, c.Side, c.No, c.Text)
	// assert equality AND model.ParseLink(got) succeeds
	// plus: cmpSides {left:"commit:"+a40, right:"worktree"}, side new, no 7, text → ":7~"+fp ; side old → "@"+a40+":7" (no '~')
	// plus: a content link ctx {path, state:"unstaged", hint:{kind:"view",id:"content"}}, no 3, text "alpha" → "gg://gigagit/a.go:3~5d8b6dab?view=content"
}
```

Write the script and assertions out in full, following `linkspairjs_test.go` line for line.

- [ ] **Step 2: Run — expect FAIL** `go test ./internal/web -run LineFingerprintJS 2>&1 | tail -6`

- [ ] **Step 3: Implement**

`links.js`, in the pure section before `linkFor`:

```js
// lineFingerprint is internal/model.LineFingerprint: FNV-1a 32-bit over the
// UTF-8 bytes of the trimmed line, 8 lowercase hex; "" for a blank line.
// TestLineFingerprintJSMatchesGo pins the pair.
function lineFingerprint(text) {
  const t = (text || "").trim();
  if (!t) return "";
  let h = 0x811c9dc5;
  for (const b of new TextEncoder().encode(t)) {
    h ^= b;
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h.toString(16).padStart(8, "0");
}
```

`linkFor(repo, worktree, ctx, side, no, text)`:
- the three `cmpSides` single-side recursions pass `text` through (`…, "new", no, text)`); the pair recursion does not;
- compute `let fp = "";` and, in the non-pair / non-preview `else` branch, set `fp = st !== "commit" ? lineFingerprint(text) : "";`
- the line tail becomes `s += ":" + (side === "old" ? "old:" : "") + no + (fp ? "~" + fp : "");`

(`String.prototype.trim` and Go's `strings.TrimSpace` agree on space, tab, CR, LF, VT, FF and the Unicode spaces; the vector test above includes the cases source text has.)

`files.js` — the raw text must come from the DATA row, never the DOM (the cell's `textContent` is rendered text):

```js
// rowRawText is the RAW text of a diff row's side, read from the diff's own
// rows (tr[data-i] indexes them) — the DOM cell holds rendered text. "" when
// the row cannot be found.
function rowRawText(tr, side) {
  const sec = state.stack && tr.closest(".stk-file");
  const d = sec ? (state.stack.slots[Number(sec.dataset.k)] || {}).diff : state.lastDiff;
  const r = d && d.rows ? d.rows[Number(tr.dataset.i)] : null;
  return r ? (side === "old" ? r.left : r.right) || "" : "";
}
```

and in the menu handler: `const link = linkFor(state.repo, state.worktree, rowCtx, side, no, rowRawText(row, side));`. Before relying on it, confirm what `data-i` indexes: read `rowIndex` near `const ri = (r) => rowIndex.get(r);` (files.js ~L1733) — if it is not the index into `d.rows`, build `rowRawText` from the same map instead.

The note-row link (`copy gg link to this note`) passes no text: a note's own anchor already follows its text.

- [ ] **Step 4: Run** `go test ./internal/web 2>&1 | tail -4` — expect `ok`.

- [ ] **Step 5: Browser check (Playwright, scratchpad)** — in a temp repo whose `f.go` has a tab-indented line and a line with a non-ASCII character, both modified unstaged: open the working-tree diff in `gg web` (a FRESH `XDG_STATE_HOME`, so no stacked preference leaks in), right-click each line → *copy gg link to this line*, read the link from the `/api/linkhist` POST, and assert its `~<fp>` equals `gg link f.go:<n>`'s (the Go fingerprint) for the same line; repeat stacked (`S`). Assert the menu is visible by hit-test. Then paste the link into `gg diff` after inserting a line above: stderr says `moved`.

- [ ] **Step 6: Commit** — `gg add internal/web`; message `feat(web): copy gg link to this line fingerprints an uncommitted line`.

---

### Task 8: e2e, docs, gate, review

**Files:**
- Create: `e2e/scenarios/s110_link_fingerprint.toml` (use the next free number: `ls e2e/scenarios | sort | tail -3`)
- Modify: `CHANGELOG.md`, `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go` (`Version`), `CLAUDE.md` (the `model` row: add "`:<line>~<fp>` fingerprints an uncommitted line"), `docs/CLAUDE-details.md` (the gg links section), `README.md` if it lists link shapes (`grep -n "gg://" README.md`)

- [ ] **Step 1: Scenario** (fingerprints: `beta` = `af81e4c7`)

```toml
name = "link fingerprint: an uncommitted line link follows its text and says when it changed"

[input]
steps = [
  { write = "f.txt", content = "alpha\nbeta\ngamma\n" },
  { commit = "c1" },
  { write = "f.txt", content = "alpha\nbeta\ngamma\nmore\n" },
]

[[run]]
cmd             = ["link", "f.txt:2"]
exit            = 0
stdout_contains = ["/f.txt:2~af81e4c7"]

[[run]]
cmd             = ["link", "--no-fingerprint", "f.txt:2"]
exit            = 0
stdout_excludes = ["~"]

[[run]]
cmd             = ["link", "resolve", "gg://{{cwd}}/f.txt:2~af81e4c7"]
exit            = 0
stdout_contains = ["new:2"]
stdout_excludes = ["moved", "changed"]

# the text is on line 2, the link says line 1: it follows the text
[[run]]
cmd             = ["link", "resolve", "gg://{{cwd}}/f.txt:1~af81e4c7"]
exit            = 0
stdout_contains = ["new:2 (line 1 moved to 2)"]

[[run]]
cmd             = ["diff", "gg://{{cwd}}/f.txt:1~af81e4c7"]
exit            = 0
stderr_contains = ["gg: line 1 moved to 2"]

# no line has that text
[[run]]
cmd             = ["diff", "gg://{{cwd}}/f.txt:2~00000000"]
exit            = 0
stderr_contains = ["gg: line 2 has changed since this link was copied"]

# a commit link takes no fingerprint
[[run]]
cmd  = ["link", "resolve", "gg://{{cwd}}/f.txt@HEAD:2~af81e4c7"]
exit = 2
```

Check the harness's field names first (`grep -n "stderr_contains\|stdout_excludes" e2e/*.go | head`) and use the ones that exist; `@HEAD` is not a sha, so use a 40-hex placeholder if the parser rejects `HEAD` before the fingerprint rule (either way the exit is 2).

- [ ] **Step 2: Run** `go test ./e2e -run 'TestScenarios/s110' 2>&1 | tail -5` — expect PASS.

- [ ] **Step 3: Docs** — `CHANGELOG.md` top section "## Links to uncommitted lines follow their text" (Added: the `~<fp>` form on working-tree / staged / content line links from every copy path; moved → follows and says so, changed → says so, never refuses; `gg link --no-fingerprint`; `asked_line` / `anchor` / `anchor_matches` in `gg link resolve --json` and `gg_link_resolve`. Changed: an older gg refuses a fingerprinted link). `using-gg.md`: add to the grammar block

```text
gg://<repo>/<path>[@staged]:[old:]<n>~<fp>  an UNCOMMITTED line + its fingerprint: gg re-finds the text
```

and a short paragraph: what `gg: line N moved to M` / `has changed since this link was copied` on stderr mean (tell the user; act on the resolved line), and the JSON fields. Bump `agentskill.Version` by one; build; `bin/gg init --update`.

- [ ] **Step 4: Gate** — `go build -o bin/gg ./cmd/gg && ./test.sh race > <scratchpad>/race.log 2>&1`; green ONLY on the literal `all green` line; `ls e2e/scenarios/*.screens/*.actual` must list nothing.

- [ ] **Step 5: Commit** docs + scenario; then the final whole-branch review as a read-only subagent on the most capable model (spec + plan + `git diff main...HEAD`), fix what it finds, re-run the gate.

- [ ] **Step 6: Report** `/work/gigagit/.claude/worktrees/link-line-fingerprint/bin/gg` and ASK before merging (`gg merge -F <msgfile> --into main feat/link-line-fingerprint`; then check main is clean, `./build.sh install`, `./build.sh web`).
