# Permanent Overviews — Plan 1 (domain + CLI) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (this repo forbids implementer subagents — CLAUDE.md "NO IMPLEMENTER SUB AGENTS"). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Store an agent's overview permanently on a commit (or a pull request) as a tagged note, with a strict save check, a read model, link support, reader isolation and the `gg overview save|list|show|rm` CLI.

**Architecture:** A permanent overview is a commit-level note tagged `overview` in the existing notes store (spec §3). `notebatch.ParseOverview` validates the overview-only document; `domain.SaveOverview` resolves the target, checks every anchor and remark against the commit's tree and (for a commit) the disk, and stores or refuses as a whole. Every reader of plain commit notes skips the tag; the CLI is a thin frontend.

**Tech Stack:** Go 1.26, the system `git`, existing packages `notes`, `notebatch`, `agentdocs`, `domain`, `cli`, `model`, `git`, `gitcmd`.

**Spec:** `docs/superpowers/specs/2026-10-12-permanent-overviews-design.md`

## Global Constraints

- One git verb = one invocation, argv built with `gitcmd`, pathspecs through `literalPathspec` (CLAUDE.md conventions).
- `internal/cli` never imports `internal/git`; it reaches git through `domain`.
- Tag value is exactly `overview`; hint kind is exactly `overview` (`?overview=<id>`).
- The document is the review JSON shape with `version` 1, `title` + `overview` required, no `summary`/`meta`/file `summary`, no `oldRange` (spec §4).
- Limits: overview markdown ≤ 64 KiB (`notebatch.MaxOverviewBytes`), title ≤ 200 bytes (`agentdocs.MaxOverviewTitle`), anchors are whatever `agentdocs.ParseOverview` returns (it caps at `agentdocs.MaxAnchors` = 100).
- Refusal problem strings, verbatim (spec §5): `<dest>: a note link — link the lines instead`; `<path>: not in <sha7>`; `<path>:<a>[-<b>]: past the end (<n> lines at <sha7>)`; `<path>: uncommitted changes`; `<path>: differs from <sha7> on disk`; plus `<path>: binary — link the file, not its lines` for a line anchor into a binary file.
- A pull-request overview skips the disk check (spec §5a).
- Every new test calls `t.Parallel()`; tests use a real `git` in `t.TempDir()`.
- **Spec amendments (this plan):** `OverviewDocAt` and `OverviewFileNotes` (spec §6) move to plan 2 with their only consumers. The gg-overview skill's switch to "permanent by default" moves to plan 2 (TUI): before plan 2 the user could not open a stored overview, so the agent must keep making temporary ones until then. Plan 1 documents the new commands in using-gg only. Step 8.1 records this in the spec's §11.

## Review Focus

1. An anchor path with a leading `./` (`./a.go:1`) — expected: `not in <sha7>`, never a read outside the tree, and one odd path never breaks the check of the others (`ls-tree` is only asked about clean paths). A `..` destination is not an anchor at all (`agentdocs.ParseAnchorDest` refuses it) and stays plain text. Pinned in Task 3 (`TestSaveOverviewOddPaths`).
2. The same file linked many times with different ranges, one of them past the end — expected: exactly one problem for the bad range, one `ls-tree` and one read for the file. Pinned in Task 3 (`TestSaveOverviewReadsEachFileOnce`).
3. A detached HEAD — expected: saves on the commit with `Branch: ""` (shows on the commit only). Pinned in Task 3 (`TestSaveOverviewDetachedHead`).
4. A re-save after the title changed only in surrounding whitespace — expected: replaces (titles compare trimmed). Pinned in Task 3 (`TestSaveOverviewReplacesByTitle`).
5. `gg note rm <overview id>` — expected: works (it is a note); `gg note show`/`list` never print it as a plain note. Pinned in Task 5 (`TestNoteListSkipsOverviews`) and Task 7 (`TestOverviewRmViaNoteRm`).

---

### Task 1: The tag, the hint kind and the document parser

**Files:**
- Modify: `internal/model/note.go` (after `IsReviewNote`, ~line 295)
- Modify: `internal/model/link.go:92,134-141`
- Create: `internal/notebatch/overview.go`
- Test: `internal/model/note_overview_test.go`, `internal/notebatch/overview_test.go`

**Interfaces:**
- Produces: `model.OverviewTag = "overview"`, `(model.Note).IsOverviewNote() bool`, `model.OverviewHintKind = "overview"`; `notebatch.OverviewDoc{Title, Overview string; Files []ReviewFile}`, `notebatch.ParseOverview(data []byte) (OverviewDoc, error)`, `notebatch.ParseOverviewTitled(data []byte, fallbackTitle string) (OverviewDoc, error)` (a missing title takes the fallback — the CLI's `--title`), `(OverviewDoc).Canonical() []byte`, `notebatch.ErrNotOverviewDoc`, `notebatch.MaxOverviewTitle = 200`.

- [ ] **Step 1: Write the failing tests**

`internal/model/note_overview_test.go`:

```go
package model

import "testing"

func TestIsOverviewNote(t *testing.T) {
	t.Parallel()
	commit := FileAddress{State: StateCommitted, Commit: "abc"}
	ov := Note{Address: commit, Tags: []string{OverviewTag}}
	if !ov.IsOverviewNote() || ov.IsReviewNote() {
		t.Fatalf("overview note: IsOverviewNote=%v IsReviewNote=%v", ov.IsOverviewNote(), ov.IsReviewNote())
	}
	if (Note{Address: commit, Tags: []string{ReviewTag}}).IsOverviewNote() {
		t.Fatal("a review is not an overview")
	}
	withPath := Note{Address: FileAddress{State: StateCommitted, Commit: "abc", Path: "a.go"}, Tags: []string{OverviewTag}}
	if withPath.IsOverviewNote() {
		t.Fatal("a line note is never an overview, whatever its tags")
	}
}

func TestOverviewHintParses(t *testing.T) {
	t.Parallel()
	l, err := ParseLink("gg://github.com/o/r@0123456789abcdef0123456789abcdef01234567?overview=n12")
	if err != nil {
		t.Fatal(err)
	}
	if l.Hint.Kind != OverviewHintKind || l.Hint.ID != "n12" {
		t.Fatalf("hint = %+v", l.Hint)
	}
}
```

`internal/notebatch/overview_test.go`:

```go
package notebatch

import (
	"errors"
	"strings"
	"testing"
)

func TestParseOverviewAccepts(t *testing.T) {
	t.Parallel()
	d, err := ParseOverview([]byte("```json\n" + `{"version":1,"title":" How locks work ","overview":"[gate](a.go:3)",
"files":[{"path":"a.go","annotations":[{"newRange":[3,4],"summary":"the gate","rationale":"why"}]}]}` + "\n```"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "How locks work" || d.Overview != "[gate](a.go:3)" || len(d.Files) != 1 || d.Files[0].Notes[0].Range != [2]int{3, 4} {
		t.Fatalf("doc = %+v", d)
	}
	back, err := ParseOverview(d.Canonical())
	if err != nil || back.Title != d.Title || back.Overview != d.Overview || len(back.Files) != 1 {
		t.Fatalf("canonical round trip = %+v %v", back, err)
	}
	if strings.Contains(string(d.Canonical()), `"summary": ""`) {
		t.Fatalf("canonical carries an empty summary: %s", d.Canonical())
	}
}

func TestParseOverviewTitledFallback(t *testing.T) {
	t.Parallel()
	d, err := ParseOverviewTitled([]byte(`{"version":1,"overview":"x"}`), " From flag ")
	if err != nil || d.Title != "From flag" {
		t.Fatalf("doc = %+v %v", d, err)
	}
	d, err = ParseOverviewTitled([]byte(`{"version":1,"title":"Own","overview":"x"}`), "From flag")
	if err != nil || d.Title != "Own" {
		t.Fatalf("the document's own title must win: %+v %v", d, err)
	}
}

func TestParseOverviewRefuses(t *testing.T) {
	t.Parallel()
	for name, doc := range map[string]string{
		"no version":     `{"title":"t","overview":"x"}`,
		"no title":       `{"version":1,"overview":"x"}`,
		"no overview":    `{"version":1,"title":"t"}`,
		"a summary":      `{"version":1,"title":"t","overview":"x","summary":"s"}`,
		"meta":           `{"version":1,"title":"t","overview":"x","meta":{"verdict":"approve"}}`,
		"file summary":   `{"version":1,"title":"t","overview":"x","files":[{"path":"a","summary":"s"}]}`,
		"old range":      `{"version":1,"title":"t","overview":"x","files":[{"path":"a","annotations":[{"oldRange":[1,1],"summary":"s"}]}]}`,
		"empty remark":   `{"version":1,"title":"t","overview":"x","files":[{"path":"a","annotations":[{"newRange":[1,1],"summary":" "}]}]}`,
		"long title":     `{"version":1,"title":"` + strings.Repeat("t", 201) + `","overview":"x"}`,
		"huge overview":  `{"version":1,"title":"t","overview":"` + strings.Repeat("x", MaxOverviewBytes+1) + `"}`,
		"not json":       `the overview is in gg`,
	} {
		if _, err := ParseOverview([]byte(doc)); !errors.Is(err, ErrNotOverviewDoc) {
			t.Errorf("%s: err = %v, want ErrNotOverviewDoc", name, err)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/model/ ./internal/notebatch/ -run 'Overview' -count=1`
Expected: FAIL — `undefined: OverviewTag`, `undefined: ParseOverview`.

- [ ] **Step 3: Implement**

In `internal/model/note.go`, after `IsReviewNote`:

```go
// OverviewTag marks a commit-level note that holds a PERMANENT overview
// (spec 2026-10-12-permanent-overviews): an explanation of the code at the
// commit, not a review of its change.
const OverviewTag = "overview"

// IsOverviewNote reports a commit-level note tagged OverviewTag. It is the
// ONLY test for "this is a permanent overview"; IsReviewNote stays false for
// it, so every review reader skips it unchanged.
func (n Note) IsOverviewNote() bool { return n.IsCommitLevel() && NoteHasTag(n, OverviewTag) }
```

In `internal/model/link.go`: add `"overview"` to the `Kind` doc comment list (line 92) and to `linkHintKinds` (line 134), and after `NoteHintKind`:

```go
// OverviewHintKind is the hint a permanent overview's link carries
// (?overview=<id>): the commit it explains plus which overview.
const OverviewHintKind = "overview"
```

Create `internal/notebatch/overview.go`:

```go
package notebatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// A permanent overview's document (spec 2026-10-12 §4): the review
// document's shape with only the walk — a title, the overview markdown and
// the remarks on lines of the files at the commit. No summary, no meta, no
// old side: it explains code, it does not review a change.
//
//	{"version":1,"title":"…","overview":"<markdown>",
//	 "files":[{"path":"…","annotations":[{"newRange":[a,b],"summary":"…","rationale":"…"}]}]}

// ErrNotOverviewDoc wraps every reason a reply is not an overview document.
var ErrNotOverviewDoc = errors.New("notebatch: not a gg overview document")

// MaxOverviewTitle caps the title (equals agentdocs.MaxOverviewTitle).
const MaxOverviewTitle = 200

// OverviewDoc is a parsed overview document. Files reuse ReviewFile; every
// note's Side is "new".
type OverviewDoc struct {
	Title, Overview string
	Files           []ReviewFile
}

func notOverview(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrNotOverviewDoc, fmt.Sprintf(format, args...))
}

type rawOverview struct {
	Version  *int            `json:"version"`
	Title    string          `json:"title"`
	Overview string          `json:"overview"`
	Summary  *string         `json:"summary"`
	Meta     json.RawMessage `json:"meta"`
	Files    []struct {
		Path        string          `json:"path"`
		Summary     *string         `json:"summary"`
		Meta        json.RawMessage `json:"meta"`
		Annotations []struct {
			NewRange  []int           `json:"newRange"`
			OldRange  []int           `json:"oldRange"`
			Summary   string          `json:"summary"`
			Rationale string          `json:"rationale"`
			Meta      json.RawMessage `json:"meta"`
		} `json:"annotations"`
	} `json:"files"`
}

// ParseOverview reads an overview document (fenced or in Claude's envelope,
// like ParseReview). A document that breaks the shape fails with an error
// wrapping ErrNotOverviewDoc.
func ParseOverview(data []byte) (OverviewDoc, error) { return ParseOverviewTitled(data, "") }

// ParseOverviewTitled is ParseOverview with a fallback for a missing title.
func ParseOverviewTitled(data []byte, fallbackTitle string) (OverviewDoc, error) {
	var raw rawOverview
	if err := json.NewDecoder(bytes.NewReader(unwrap(data))).Decode(&raw); err != nil {
		return OverviewDoc{}, notOverview("%v", err)
	}
	switch {
	case raw.Version == nil || *raw.Version != 1:
		return OverviewDoc{}, notOverview("version must be 1")
	case raw.Summary != nil:
		return OverviewDoc{}, notOverview("an overview has no summary — that is a review")
	case len(bytes.TrimSpace(raw.Meta)) > 0 && string(bytes.TrimSpace(raw.Meta)) != "null":
		return OverviewDoc{}, notOverview("an overview has no meta — that is a review")
	}
	title := strings.TrimSpace(raw.Title)
	if title == "" {
		title = strings.TrimSpace(fallbackTitle)
	}
	switch {
	case title == "":
		return OverviewDoc{}, notOverview("title is empty")
	case len(title) > MaxOverviewTitle:
		return OverviewDoc{}, notOverview("title exceeds %d bytes", MaxOverviewTitle)
	case strings.TrimSpace(raw.Overview) == "":
		return OverviewDoc{}, notOverview("overview is empty")
	case len(raw.Overview) > MaxOverviewBytes:
		return OverviewDoc{}, notOverview("overview exceeds 64 KiB (%d bytes)", len(raw.Overview))
	}
	doc := OverviewDoc{Title: title, Overview: raw.Overview}
	for i, f := range raw.Files {
		if strings.TrimSpace(f.Path) == "" {
			return OverviewDoc{}, notOverview("files[%d]: path is empty", i)
		}
		if f.Summary != nil || (len(bytes.TrimSpace(f.Meta)) > 0 && string(bytes.TrimSpace(f.Meta)) != "null") {
			return OverviewDoc{}, notOverview("files[%d]: a file has no summary or meta in an overview", i)
		}
		rf := ReviewFile{Path: f.Path}
		for j, a := range f.Annotations {
			where := fmt.Sprintf("files[%d].annotations[%d]", i, j)
			if a.OldRange != nil {
				return OverviewDoc{}, notOverview("%s: oldRange — an overview remarks on lines at the commit (newRange)", where)
			}
			if a.NewRange == nil {
				return OverviewDoc{}, notOverview("%s: no newRange", where)
			}
			r, err := docRange(a.NewRange, where)
			if err != nil {
				return OverviewDoc{}, notOverview("%s: range %v is not [first,last] with 1 <= first <= last", where, a.NewRange)
			}
			if strings.TrimSpace(a.Summary) == "" {
				return OverviewDoc{}, notOverview("%s: summary is empty", where)
			}
			if len(bytes.TrimSpace(a.Meta)) > 0 && string(bytes.TrimSpace(a.Meta)) != "null" {
				return OverviewDoc{}, notOverview("%s: a remark has no meta in an overview", where)
			}
			rf.Notes = append(rf.Notes, ReviewNote{Side: "new", Range: r, Summary: a.Summary, Rationale: a.Rationale})
		}
		doc.Files = append(doc.Files, rf)
	}
	return doc, nil
}

type canonOverviewNote struct {
	NewRange  [2]int `json:"newRange"`
	Summary   string `json:"summary"`
	Rationale string `json:"rationale,omitempty"`
}

type canonOverviewFile struct {
	Path        string              `json:"path"`
	Annotations []canonOverviewNote `json:"annotations,omitempty"`
}

type canonOverview struct {
	Version  int                 `json:"version"`
	Title    string              `json:"title"`
	Overview string              `json:"overview"`
	Files    []canonOverviewFile `json:"files,omitempty"`
}

// Canonical is the document in its documented shape, indented: what gg
// stores.
func (d OverviewDoc) Canonical() []byte {
	c := canonOverview{Version: 1, Title: d.Title, Overview: d.Overview}
	for _, f := range d.Files {
		cf := canonOverviewFile{Path: f.Path}
		for _, n := range f.Notes {
			cf.Annotations = append(cf.Annotations, canonOverviewNote{NewRange: n.Range, Summary: n.Summary, Rationale: n.Rationale})
		}
		c.Files = append(c.Files, cf)
	}
	out, _ := json.MarshalIndent(c, "", "  ")
	return out
}
```

Add to `internal/domain/review_overview_test.go`'s `TestOverviewLimitsAgree`:

```go
	if notebatch.MaxOverviewTitle != agentdocs.MaxOverviewTitle {
		t.Fatalf("title caps differ: notebatch %d != agentdocs %d", notebatch.MaxOverviewTitle, agentdocs.MaxOverviewTitle)
	}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/model/ ./internal/notebatch/ ./internal/domain/ -run 'Overview|LinkHint|HintKind' -count=1`
Expected: PASS. If an existing test pins the hint-kind set (grep `linkHintKinds` / `"note": true` in `internal/model/*_test.go`), add `overview` to its expected list.

- [ ] **Step 5: Commit**

```bash
gg add internal/model/note.go internal/model/link.go internal/model/note_overview_test.go internal/notebatch/overview.go internal/notebatch/overview_test.go internal/domain/review_overview_test.go
git commit -F <msgfile>   # "model, notebatch: the overview tag, the ?overview= hint and the overview document"
```

---

### Task 2: Two git verbs — status and diff limited to paths

**Files:**
- Create: `internal/git/paths_changed.go`
- Test: `internal/git/paths_changed_test.go`

**Interfaces:**
- Produces: `(*git.Repo).StatusPaths(ctx, paths []string) ([]string, error)` — every path git status lists under the pathspec (modified, staged, deleted, untracked, a rename's both sides); `(*git.Repo).DiffNamesBetween(ctx, a, b string, paths []string) ([]string, error)` — the paths that differ between two commits under the pathspec. Both return nil without invoking git for an empty `paths`.

- [ ] **Step 1: Write the failing test**

```go
package git

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestStatusPathsAndDiffNamesBetween(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t) // README.md committed
	repo := &Repo{Runner: runner}
	ctx := context.Background()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "a\n")
	write("b.go", "b\n")
	write("c.go", "c\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "abc")
	gitRun(t, dir, "tag", "first")
	write("a.go", "a2\n") // modified
	gitRun(t, dir, "add", "a.go")
	gitRun(t, dir, "commit", "-q", "-m", "a2")
	write("b.go", "b2\n")                               // unstaged edit
	write("new.go", "n\n")                              // untracked
	if err := os.Remove(filepath.Join(dir, "c.go")); err != nil { // deleted
		t.Fatal(err)
	}
	got, err := repo.StatusPaths(ctx, []string{"a.go", "b.go", "c.go", "new.go", "README.md"})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{"b.go", "c.go", "new.go"}) {
		t.Fatalf("StatusPaths = %v, want [b.go c.go new.go]", got)
	}
	diff, err := repo.DiffNamesBetween(ctx, "first", "HEAD", []string{"a.go", "b.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(diff, []string{"a.go"}) {
		t.Fatalf("DiffNamesBetween = %v, want [a.go]", diff)
	}
}

func TestPathVerbsEmptyNeverInvokeGit(t *testing.T) {
	t.Parallel()
	fr := gitexec.NewFakeRunner()
	repo := &Repo{Runner: fr}
	if got, err := repo.StatusPaths(context.Background(), nil); got != nil || err != nil {
		t.Fatalf("StatusPaths(nil) = %v %v", got, err)
	}
	if got, err := repo.DiffNamesBetween(context.Background(), "a", "b", nil); got != nil || err != nil {
		t.Fatalf("DiffNamesBetween(nil) = %v %v", got, err)
	}
	if len(fr.Calls()) != 0 {
		t.Fatalf("git was invoked: %v", fr.Calls())
	}
}
```

(Check `gitexec.FakeRunner`'s call-list accessor name with `grep -n "func (f \*FakeRunner)" internal/gitexec/fake.go` and use it; `TestTreePathsEmptyNeverInvokesGit` in `ls_tree_paths_test.go` shows the idiom — copy it.)

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/git/ -run 'StatusPaths|PathVerbs' -count=1`
Expected: FAIL — `repo.StatusPaths undefined`.

- [ ] **Step 3: Implement**

```go
package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// StatusPaths lists the paths under the pathspec that git status reports —
// staged, unstaged, deleted, untracked, both sides of a rename: "this file
// on disk is not the committed one". Limited by pathspec so the cost is the
// question's, not the repository's (a monorepo's full status is seconds).
// An empty paths list returns nil without invoking git.
func (r *Repo) StatusPaths(ctx context.Context, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	argv := gitcmd.New("status").Arg("--porcelain=v2", "-z", "--untracked-files=all", "--").Arg(literalPathspec(paths)...).ToArgv()
	res, err := r.Runner.Run(ctx, "git status (paths)", argv)
	if err != nil {
		return nil, err
	}
	st, err := ParseStatusV2([]byte(res.Stdout))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range st.Files {
		out = append(out, f.Path)
		if f.OrigPath != "" {
			out = append(out, f.OrigPath)
		}
	}
	return out, nil
}

// DiffNamesBetween lists the paths under the pathspec whose content differs
// between commits a and b (`git diff --name-only -z a b -- <paths>`).
// An empty paths list returns nil without invoking git.
func (r *Repo) DiffNamesBetween(ctx context.Context, a, b string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	argv := gitcmd.New("diff").Arg("--name-only", "-z", "--no-renames", a, b, "--").Arg(literalPathspec(paths)...).ToArgv()
	res, err := r.Runner.Run(ctx, "git diff --name-only (paths)", argv)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range strings.Split(strings.TrimRight(res.Stdout, "\x00"), "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Run the test**

Run: `go test ./internal/git/ -run 'StatusPaths|PathVerbs' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/git/paths_changed.go internal/git/paths_changed_test.go
git commit -F <msgfile>   # "git: StatusPaths and DiffNamesBetween — status and diff names limited to a pathspec"
```

---

### Task 3: Save — target, check, store

**Files:**
- Create: `internal/domain/overview_save.go`
- Modify: `internal/domain/review_notes.go:150-212` (extract the retry loop into `saveNoteWithRetry`, SaveReview calls it)
- Test: `internal/domain/overview_save_test.go`

**Interfaces:**
- Consumes: Task 1 (`model.OverviewTag`, `notebatch.ParseOverview`), Task 2 (`s.repo.StatusPaths`, `s.repo.DiffNamesBetween`), existing `s.repo.TreePaths`, `s.ShowFile`, `splitTextLines`, `s.ResolveRev`, `s.reviewBranchName`, `agentdocs.ParseOverview`, `git.ParsePRRef`, `SameNoteScope`, `notes.NewID`, `notes.Now`.
- Produces:
  ```go
  type OverviewTarget struct {
      Commit string // full sha the overview explains
      Branch string // recorded branch ("" = shows on the commit only)
      Scope  string // a PR overview's Note.Preview ("<base>...refs/gg/pr/<n>"); "" for a commit
      PR     int    // the PR number; 0 for a commit
  }
  var ErrOverviewTarget = errors.New("a permanent overview needs a commit or a pull request")
  type OverviewRefused struct{ Problems []string } // error; Error() lists them
  type SaveOverview struct { Target OverviewTarget; Agent, Title string; Text []byte; DryRun bool }
  type OverviewSaved struct { ID string; Replaced bool; Doc notebatch.OverviewDoc }
  func (s *Service) HeadOverviewTarget(ctx) (OverviewTarget, error)
  func (s *Service) LinkOverviewTarget(ctx, res Resolved) (OverviewTarget, error)
  func (s *Service) SaveOverview(ctx, cmd SaveOverview) (OverviewSaved, string /*warn*/, error)
  func (s *Service) saveNoteWithRetry(ctx, st notes.Store, put func() (string, error)) (id, warn string, err error)
  ```

- [ ] **Step 1: Write the failing tests**

`internal/domain/overview_save_test.go`:

```go
package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// overviewRepo: main with a.go (5 lines) and b.go (3 lines), committed on
// branch main; the notes store in a temp dir.
func permOvRepo(t *testing.T) (string, *Service) {
	t.Helper()
	dir, _ := newRealRepo(t)
	commitFile(t, dir, "a.go", "1\n2\n3\n4\n5\n", "a")
	commitFile(t, dir, "b.go", "x\ny\nz\n", "b")
	_, svc := newRealRepoAt(t, dir)
	svc.UseNotesDir(t.TempDir())
	return dir, svc
}

func ovDoc(title, overview, files string) []byte {
	if files == "" {
		files = "[]"
	}
	return []byte(`{"version":1,"title":"` + title + `","overview":"` + overview + `","files":` + files + `}`)
}

func saveOv(t *testing.T, svc *Service, doc []byte) (OverviewSaved, error) {
	t.Helper()
	tg, err := svc.HeadOverviewTarget(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := svc.SaveOverview(context.Background(), SaveOverview{Target: tg, Agent: "Claude Code", Text: doc})
	return got, err
}

func problems(t *testing.T, err error) []string {
	t.Helper()
	var r *OverviewRefused
	if !errors.As(err, &r) {
		t.Fatalf("err = %v, want *OverviewRefused", err)
	}
	return r.Problems
}

func TestSaveOverviewStoresOnHeadWithBranch(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	got, err := saveOv(t, svc, ovDoc("Locks", "[three](a.go:3) [y](b.go:2) [file](b.go)",
		`[{"path":"a.go","annotations":[{"newRange":[2,4],"summary":"the gate"}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	ov, err := svc.Overview(context.Background(), got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Commit != revParse(t, dir, "HEAD") || ov.Branch != "main" || ov.Title != "Locks" || ov.Agent != "Claude Code" || ov.Kind != ReviewOnBranch {
		t.Fatalf("overview = %+v", ov)
	}
}

func TestSaveOverviewRefusesEveryProblemAtOnce(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	sha7 := revParse(t, dir, "HEAD")[:7]
	_, err := saveOv(t, svc, ovDoc("T", "[n](note:t3) [gone](zzz.go:1) [far](a.go:9) [range](b.go:2-7)",
		`[{"path":"a.go","annotations":[{"newRange":[6,6],"summary":"past"}]}]`))
	want := []string{
		"note:t3: a note link — link the lines instead",
		"zzz.go: not in " + sha7,
		"a.go:9: past the end (5 lines at " + sha7 + ")",
		"b.go:2-7: past the end (3 lines at " + sha7 + ")",
		"a.go:6: past the end (5 lines at " + sha7 + ")",
	}
	if got := problems(t, err); !slices.Equal(got, want) {
		t.Fatalf("problems =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if all, _ := svc.Overviews(context.Background()); len(all) != 0 {
		t.Fatalf("a refused save stored %d overviews", len(all))
	}
}

func TestSaveOverviewRefusesUncommittedFiles(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		dirty func(dir string)
	}{
		{"modified", func(dir string) { writeFile(t, dir, "a.go", "1\n2\nX\n4\n5\n") }},
		{"staged", func(dir string) { writeFile(t, dir, "a.go", "1\n2\nX\n4\n5\n"); runGitIn(t, dir, "add", "a.go") }},
		{"deleted", func(dir string) { _ = os.Remove(filepath.Join(dir, "a.go")) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir, svc := permOvRepo(t)
			c.dirty(dir)
			_, err := saveOv(t, svc, ovDoc("T", "[three](a.go:3) [y](b.go:2)", ""))
			if got := problems(t, err); !slices.Equal(got, []string{"a.go: uncommitted changes"}) {
				t.Fatalf("problems = %v", got)
			}
		})
	}
}

func TestSaveOverviewOlderCommitDiffersOnDisk(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	old := revParse(t, dir, "HEAD")
	commitFile(t, dir, "a.go", "1\n2\nNEW\n4\n5\n", "a changed")
	got, _, err := svc.SaveOverview(context.Background(), SaveOverview{
		Target: OverviewTarget{Commit: old}, Agent: "c", Text: ovDoc("T", "[three](a.go:3) [y](b.go:2)", "")})
	if p := problems(t, err); !slices.Equal(p, []string{"a.go: differs from " + old[:7] + " on disk"}) {
		t.Fatalf("problems = %v (saved %+v)", p, got)
	}
}

func TestSaveOverviewReplacesByTitle(t *testing.T) {
	t.Parallel()
	_, svc := permOvRepo(t)
	first, err := saveOv(t, svc, ovDoc("Locks", "[a](a.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	second, err := saveOv(t, svc, ovDoc("  Locks ", "[b](b.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || !second.Replaced {
		t.Fatalf("second = %+v, want a replace of %s", second, first.ID)
	}
	other, err := saveOv(t, svc, ovDoc("Parsing", "[a](a.go:1)", ""))
	if err != nil || other.ID == first.ID || other.Replaced {
		t.Fatalf("other = %+v %v, want a new overview", other, err)
	}
	all, _ := svc.Overviews(context.Background())
	if len(all) != 2 {
		t.Fatalf("overviews = %d, want 2", len(all))
	}
}

func TestSaveOverviewDetachedHead(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	runGitIn(t, dir, "checkout", "-q", "--detach")
	got, err := saveOv(t, svc, ovDoc("T", "[a](a.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	ov, _ := svc.Overview(context.Background(), got.ID)
	if ov.Branch != "" || ov.Kind != ReviewOnCommit {
		t.Fatalf("detached overview = %+v", ov)
	}
}

func TestSaveOverviewOddPaths(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	sha7 := revParse(t, dir, "HEAD")[:7]
	// ../a.go is no anchor (plain text); ./a.go is one, never in the tree; a.go:2 is fine.
	_, err := saveOv(t, svc, ovDoc("T", "[up](../a.go:1) [dot](./a.go:1) [ok](a.go:2)", ""))
	got := problems(t, err)
	if !slices.Equal(got, []string{"./a.go: not in " + sha7}) {
		t.Fatalf("problems = %v", got)
	}
}

func TestSaveOverviewReadsEachFileOnce(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	sha7 := revParse(t, dir, "HEAD")[:7]
	_, err := saveOv(t, svc, ovDoc("T", "[1](a.go:1) [2](a.go:2-3) [3](a.go:4) [bad](a.go:6)", ""))
	if got := problems(t, err); !slices.Equal(got, []string{"a.go:6: past the end (5 lines at " + sha7 + ")"}) {
		t.Fatalf("problems = %v", got)
	}
}

func TestSaveOverviewRefusesNonCommitLinks(t *testing.T) {
	t.Parallel()
	_, svc := permOvRepo(t)
	ctx := context.Background()
	for name, res := range map[string]Resolved{
		"working": {Addr: model.FileAddress{State: model.StateUnstaged, Path: "a.go"}},
		"staged":  {Addr: model.FileAddress{State: model.StateStaged}},
		"pair":    {Pair: &model.LinkPair{A: "a", B: "b"}, Addr: model.FileAddress{State: model.StateCommitted}, Commit: "b"},
	} {
		if _, err := svc.LinkOverviewTarget(ctx, res); !errors.Is(err, ErrOverviewTarget) {
			t.Errorf("%s: err = %v, want ErrOverviewTarget", name, err)
		}
	}
}

func TestSaveOverviewOnAPullRequest(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t) // PR #7: feat changes big.go lines 5 and 25; main checked out
	ctx := context.Background()
	set, err := svc.PreviewNotes(ctx, git.PRRef(7), "main")
	if err != nil {
		t.Fatal(err)
	}
	tg, err := svc.LinkOverviewTarget(ctx, Resolved{Preview: &set, Commit: set.Tip,
		Addr: model.FileAddress{State: model.StateCommitted, Commit: set.Tip}})
	if err != nil {
		t.Fatal(err)
	}
	if tg.Commit != head || tg.PR != 7 || tg.Branch != "" || !strings.HasSuffix(tg.Scope, git.PRRef(7)) {
		t.Fatalf("target = %+v", tg)
	}
	// big.go differs from main's on disk — a PR skips the disk check.
	got, _, err := svc.SaveOverview(ctx, SaveOverview{Target: tg, Agent: "c", Text: ovDoc("How #7 works", "[changed](big.go:25)", "")})
	if err != nil {
		t.Fatal(err)
	}
	ov, _ := svc.Overview(ctx, got.ID)
	if ov.Scope != tg.Scope || ov.Commit != head {
		t.Fatalf("stored = %+v", ov)
	}
}

func TestSaveOverviewDryRunStoresNothing(t *testing.T) {
	t.Parallel()
	_, svc := permOvRepo(t)
	tg, _ := svc.HeadOverviewTarget(context.Background())
	got, _, err := svc.SaveOverview(context.Background(), SaveOverview{Target: tg, Agent: "c", Text: ovDoc("T", "[a](a.go:1)", ""), DryRun: true})
	if err != nil || got.ID != "" || got.Doc.Title != "T" {
		t.Fatalf("dry run = %+v %v", got, err)
	}
	if all, _ := svc.Overviews(context.Background()); len(all) != 0 {
		t.Fatal("dry run stored an overview")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/domain/ -run 'SaveOverview' -count=1`
Expected: FAIL — `undefined: SaveOverview` (and `svc.Overview`, provided by Task 4 — write Task 4's `Overview`/`Overviews` minimal versions in Task 4; run this task's tests after Task 4 Step 3 if the build needs them. Order inside the session: Task 3 Step 3, then Task 4 Steps 1–4, then rerun both).

- [ ] **Step 3: Implement**

In `internal/domain/review_notes.go`, replace SaveReview's `for { … }` retry loop with a call, and add the helper below it:

```go
	id, warn, err := s.saveNoteWithRetry(ctx, st, func() (string, error) { return s.putReview(st, t, wt, cmd) })
	if err != nil {
		return "", "", err
	}
	return id, warn, nil
}

// saveNoteWithRetry runs put until it stores: a corrupt store is moved aside
// once (warn says where), a held lock is retried within reviewRetryBudget
// with jitter. Every entry-level note writer (reviews, overviews) uses it.
func (s *Service) saveNoteWithRetry(ctx context.Context, st notes.Store, put func() (string, error)) (string, string, error) {
	start := time.Now()
	elapsed := func() time.Duration {
		if reviewClock != nil {
			return reviewClock()
		}
		return time.Since(start)
	}
	warn := ""
	for {
		id, err := put()
		if err == nil {
			s.invalidateNoteCounts()
			return id, warn, nil
		}
		switch {
		case errors.Is(err, notes.ErrCorrupt) && warn == "":
			q, ok := st.(interface{ Quarantine() (string, error) })
			if !ok {
				return "", "", err
			}
			moved, qerr := q.Quarantine()
			if qerr != nil {
				return "", "", errors.Join(err, qerr)
			}
			warn = fmt.Sprintf("the note store was unreadable and was moved to %s", moved)
			continue
		case errors.Is(err, filelock.ErrHeld):
			d := reviewJitter()
			if elapsed()+d > reviewRetryBudget {
				return "", "", err
			}
			if cerr := ctx.Err(); cerr != nil {
				return "", "", cerr
			}
			reviewSleep(d)
			continue
		}
		return "", "", err
	}
}
```

(The `reviewPutHook` call stays at the top of `putReview`, so the existing retry tests keep driving it.)

Create `internal/domain/overview_save.go`:

```go
package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
	"github.com/homeend/gigagit/internal/notes"
)

// Permanent overviews (spec 2026-10-12): an agent's overview stored on a
// commit, or on a pull request, as a commit-level note tagged "overview".
// This file is the ONE writer; overview_read.go the read model.

// ErrOverviewTarget refuses a link that names no single commit or PR.
var ErrOverviewTarget = errors.New("a permanent overview needs a commit or a pull request")

// OverviewTarget is where an overview is stored.
type OverviewTarget struct {
	Commit string // full sha the overview explains
	Branch string // recorded branch ("" = shows on the commit only)
	Scope  string // a PR overview's Note.Preview; "" for a commit
	PR     int    // the PR number; 0 for a commit
}

// OverviewRefused is a save gg refused: every problem, in document order
// (overview anchors first, then remarks, then the disk check).
type OverviewRefused struct{ Problems []string }

func (e *OverviewRefused) Error() string {
	return "the overview cannot be stored permanently:\n  " + strings.Join(e.Problems, "\n  ")
}

// SaveOverview is the one command that stores an overview.
type SaveOverview struct {
	Target OverviewTarget
	Agent  string
	Title  string // used when the document has no title
	Text   []byte // the document (fenced / enveloped is fine)
	DryRun bool   // check only; store nothing
}

// OverviewSaved is a stored (or, for a dry run, checked) overview.
type OverviewSaved struct {
	ID       string // "" for a dry run
	Replaced bool   // an overview with the same commit (or PR) and title was rewritten
	Doc      notebatch.OverviewDoc
}

// HeadOverviewTarget is HEAD, with the checked-out branch when HEAD is its
// tip (none when detached).
func (s *Service) HeadOverviewTarget(ctx context.Context) (OverviewTarget, error) {
	sha, ok, err := s.ResolveRev(ctx, "HEAD")
	if err != nil {
		return OverviewTarget{}, err
	}
	if !ok {
		return OverviewTarget{}, fmt.Errorf("%w: HEAD has no commit yet", ErrOverviewTarget)
	}
	sha = strings.TrimSpace(sha)
	return OverviewTarget{Commit: sha, Branch: s.reviewBranchName(ctx, "HEAD", sha)}, nil
}

// LinkOverviewTarget is the target a resolved gg:// link names: a commit; a
// branch tip (@ref:<name>, the branch recorded while it points there); or a
// pull request's link (<base>...refs/gg/pr/<n>). Anything else is
// ErrOverviewTarget.
func (s *Service) LinkOverviewTarget(ctx context.Context, res Resolved) (OverviewTarget, error) {
	switch {
	case res.Preview != nil:
		n, ok := git.ParsePRRef(res.Preview.Source)
		if !ok || !res.Preview.OK() {
			return OverviewTarget{}, ErrOverviewTarget
		}
		return OverviewTarget{Commit: res.Preview.Tip, Scope: res.Preview.scope(), PR: n}, nil
	case res.Pair != nil:
		return OverviewTarget{}, ErrOverviewTarget
	case res.Ref != "" && res.Commit != "":
		return OverviewTarget{Commit: res.Commit, Branch: s.reviewBranchName(ctx, res.Ref, res.Commit)}, nil
	case res.Addr.State == model.StateCommitted && res.Commit != "":
		return OverviewTarget{Commit: res.Commit}, nil
	}
	return OverviewTarget{}, ErrOverviewTarget
}

// SaveOverview checks the document against the target (spec §5) and stores
// it — or refuses with *OverviewRefused and stores nothing.
func (s *Service) SaveOverview(ctx context.Context, cmd SaveOverview) (OverviewSaved, string, error) {
	if !isFullSHA(cmd.Target.Commit) {
		return OverviewSaved{}, "", ErrOverviewTarget
	}
	doc, err := notebatch.ParseOverviewTitled(cmd.Text, cmd.Title)
	if err != nil {
		return OverviewSaved{}, "", err
	}
	if p := s.checkOverview(ctx, cmd.Target, doc); len(p) > 0 {
		return OverviewSaved{}, "", &OverviewRefused{Problems: p}
	}
	if cmd.DryRun {
		return OverviewSaved{Doc: doc}, "", nil
	}
	st := s.notesStore(ctx)
	if st == nil {
		return OverviewSaved{}, "", ErrNotesDisabled
	}
	replaced := false
	id, warn, err := s.saveNoteWithRetry(ctx, st, func() (string, error) {
		var perr error
		var id string
		id, replaced, perr = putOverview(st, cmd.Target, strings.TrimSpace(cmd.Agent), doc)
		return id, perr
	})
	if err != nil {
		return OverviewSaved{}, "", err
	}
	return OverviewSaved{ID: id, Replaced: replaced, Doc: doc}, warn, nil
}

// putOverview is one save attempt: the overview with the same commit (or
// the same PR) and title is rewritten in place, keeping its id and Created.
func putOverview(st notes.Store, t OverviewTarget, agent string, doc notebatch.OverviewDoc) (string, bool, error) {
	all, err := st.LoadAll()
	if err != nil {
		return "", false, err
	}
	now := notes.Now().UTC()
	n := model.Note{Source: model.NoteSourceAgent, Author: agent,
		Address: model.FileAddress{State: model.StateCommitted, Commit: t.Commit, Branch: t.Branch},
		Side:    model.NoteSideNew, Tags: []string{model.OverviewTag}, Preview: t.Scope,
		Summary: doc.Title, Rationale: string(doc.Canonical()), Created: now, Updated: now}
	replaced := false
	for _, old := range all {
		if old.IsReply() || !old.IsOverviewNote() || strings.TrimSpace(old.Summary) != doc.Title {
			continue
		}
		same := old.Preview == "" && t.Scope == "" && old.Address.Commit == t.Commit
		if t.Scope != "" {
			same = SameNoteScope(old.Preview, t.Scope)
		}
		if same {
			n.ID, n.Created, replaced = old.ID, old.Created, true
			break
		}
	}
	if n.ID == "" {
		n.ID = notes.NewID(all)
	}
	return n.ID, replaced, st.Put(n)
}

// overviewSpot is one place the document points at: an anchor or a remark.
type overviewSpot struct {
	dest       string // as reported: the anchor's dest, or "<path>:<a>[-<b>]"
	path       string
	start, end int // 0 = the whole file
	note       bool
}

func spotDest(path string, a, b int) string {
	if b > a {
		return fmt.Sprintf("%s:%d-%d", path, a, b)
	}
	return fmt.Sprintf("%s:%d", path, a)
}

// checkOverview is spec §5: every problem, in document order. One ls-tree
// for all paths, one read per file, one status (and for an older commit one
// diff) for the disk.
func (s *Service) checkOverview(ctx context.Context, t OverviewTarget, doc notebatch.OverviewDoc) []string {
	var spots []overviewSpot
	_, anchors := agentdocs.ParseOverview(doc.Overview)
	for _, a := range anchors {
		switch {
		case a.Note != "":
			spots = append(spots, overviewSpot{dest: a.Dest, note: true})
		case a.Path != "":
			spots = append(spots, overviewSpot{dest: a.Dest, path: a.Path, start: a.Start, end: max(a.End, a.Start)})
		}
	}
	for _, f := range doc.Files {
		for _, n := range f.Notes {
			spots = append(spots, overviewSpot{dest: spotDest(f.Path, n.Range[0], n.Range[1]), path: f.Path, start: n.Range[0], end: n.Range[1]})
		}
	}
	var paths []string
	seen := map[string]bool{}
	for _, sp := range spots {
		if sp.path != "" && !seen[sp.path] {
			seen[sp.path] = true
			paths = append(paths, sp.path)
		}
	}
	sha7 := t.Commit[:7]
	var clean []string // ls-tree fails on a path outside the repo: ask only about clean ones
	for _, p := range paths {
		if cleanRepoPath(p) {
			clean = append(clean, p)
		}
	}
	inTree := map[string]bool{}
	if found, err := s.repo.TreePaths(ctx, t.Commit, clean); err == nil {
		for _, p := range found {
			inTree[p] = true
		}
	}
	type fileLines struct {
		n      int
		binary bool
	}
	lines := map[string]fileLines{}
	count := func(path string) fileLines {
		if fl, ok := lines[path]; ok {
			return fl
		}
		fl := fileLines{n: -1}
		if data, err := s.ShowFile(ctx, t.Commit, path); err == nil {
			if ls, ok := splitTextLines(data); ok {
				fl.n = len(ls)
			} else {
				fl.binary = true
			}
		}
		lines[path] = fl
		return fl
	}
	var out []string
	reported := map[string]bool{} // "not in" once per path
	for _, sp := range spots {
		switch {
		case sp.note:
			out = append(out, sp.dest+": a note link — link the lines instead")
		case !inTree[sp.path] || !cleanRepoPath(sp.path):
			if !reported[sp.path] {
				reported[sp.path] = true
				out = append(out, sp.path+": not in "+sha7)
			}
		case sp.start == 0:
			// the whole file: present is enough
		default:
			fl := count(sp.path)
			switch {
			case fl.binary:
				out = append(out, sp.path+": binary — link the file, not its lines")
			case fl.n < 0:
				out = append(out, sp.path+": not in "+sha7)
			case sp.end > fl.n:
				out = append(out, fmt.Sprintf("%s: past the end (%d lines at %s)", spotDest(sp.path, sp.start, sp.end), fl.n, sha7))
			}
		}
	}
	if t.PR != 0 {
		return out // a PR is not the checkout: no disk check (spec §5a)
	}
	var onDisk []string
	for _, p := range paths {
		if inTree[p] && cleanRepoPath(p) {
			onDisk = append(onDisk, p)
		}
	}
	dirty := map[string]bool{}
	if got, err := s.repo.StatusPaths(ctx, onDisk); err == nil {
		for _, p := range got {
			dirty[p] = true
		}
	}
	differs := map[string]bool{}
	if head, ok, err := s.ResolveRev(ctx, "HEAD"); err == nil && ok && strings.TrimSpace(head) != t.Commit {
		if got, err := s.repo.DiffNamesBetween(ctx, t.Commit, "HEAD", onDisk); err == nil {
			for _, p := range got {
				differs[p] = true
			}
		}
	}
	for _, p := range onDisk {
		switch {
		case dirty[p]:
			out = append(out, p+": uncommitted changes")
		case differs[p]:
			out = append(out, p+": differs from "+sha7+" on disk")
		}
	}
	return out
}

// cleanRepoPath: a repo-relative slash path with no "." / ".." segment and
// no leading slash — the only form an anchor may name.
func cleanRepoPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.TrimSpace(p) != p {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}
```

Check that `isFullSHA` exists in domain (`grep -n "func isFullSHA" internal/domain/*.go`) — it is used by `stampReview`, so it does.

Note on the disk loop: a path the anchors name but `TreePaths` did not find is already reported as `not in`; it is left out of the disk check so it is never reported twice.

- [ ] **Step 4: Run the tests (after Task 4 Step 3)**

Run: `go test ./internal/domain/ -run 'SaveOverview|SaveReview|ReviewRetry|Review.*Lock' -count=1`
Expected: PASS — the review retry tests prove the extraction kept behaviour.

- [ ] **Step 5: Commit** (together with Task 4 if the build needed it)

```bash
gg add internal/domain/overview_save.go internal/domain/overview_save_test.go internal/domain/review_notes.go
git commit -F <msgfile>   # "domain: SaveOverview — store a permanent overview on a commit or a PR, refuse on any problem"
```

---

### Task 4: The read model and note counts

**Files:**
- Create: `internal/domain/overview_read.go`
- Modify: `internal/domain/notes.go:66-87` (NoteCounts fields), `internal/domain/notes.go:~655-680` (the counting loop)
- Test: `internal/domain/overview_read_test.go`

**Interfaces:**
- Consumes: Task 3 (`saveOv`, `overviewRepo`, `ovDoc` test helpers; `putOverview` note shape).
- Produces:
  ```go
  var ErrOverviewNotFound = errors.New("overview not found")
  type Overview struct {
      ID, Commit, Branch, Scope, Agent, Title string
      Kind             ReviewNoteKind
      Created, Updated time.Time
      Doc              *notebatch.OverviewDoc // nil when the stored text no longer parses
  }
  type OverviewHead struct {
      ID, Commit, Branch, Scope, Agent, Title string
      Created time.Time
  }
  func (s *Service) Overviews(ctx) ([]Overview, error)            // newest first, commit + PR overviews
  func (s *Service) Overview(ctx, id string) (Overview, error)
  func (s *Service) OverviewsForCommit(ctx, sha string) ([]Overview, error) // commit overviews only (Scope == "")
  func (s *Service) RemoveOverview(ctx, id string) error
  // NoteCounts gains:
  //   Overviews        []OverviewHead            // commit overviews, newest first
  //   PreviewOverviews map[string][]OverviewHead // PR overviews by scope
  ```

- [ ] **Step 1: Write the failing tests**

```go
package domain

import (
	"context"
	"errors"
	"testing"
)

func TestOverviewsListAndRemove(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	ctx := context.Background()
	a, err := saveOv(t, svc, ovDoc("A", "[a](a.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	b, err := saveOv(t, svc, ovDoc("B", "[b](b.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	all, err := svc.Overviews(ctx)
	if err != nil || len(all) != 2 || all[0].ID != b.ID || all[1].ID != a.ID {
		t.Fatalf("Overviews = %+v %v, want B then A", all, err)
	}
	if all[0].Doc == nil || all[0].Doc.Overview != "[b](b.go:1)" {
		t.Fatalf("doc = %+v", all[0].Doc)
	}
	head := revParse(t, dir, "HEAD")
	if got, _ := svc.OverviewsForCommit(ctx, head); len(got) != 2 {
		t.Fatalf("OverviewsForCommit = %d", len(got))
	}
	if err := svc.RemoveOverview(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Overview(ctx, a.ID); !errors.Is(err, ErrOverviewNotFound) {
		t.Fatalf("after rm: %v", err)
	}
	if err := svc.RemoveOverview(ctx, "nope"); !errors.Is(err, ErrOverviewNotFound) {
		t.Fatalf("rm unknown = %v", err)
	}
}

func TestOverviewKindFollowsTheBranch(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	ctx := context.Background()
	got, err := saveOv(t, svc, ovDoc("T", "[a](a.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	if ov, _ := svc.Overview(ctx, got.ID); ov.Kind != ReviewOnBranch {
		t.Fatalf("kind = %v, want on branch", ov.Kind)
	}
	commitFile(t, dir, "c.go", "c\n", "move on")
	if ov, _ := svc.Overview(ctx, got.ID); ov.Kind != ReviewWasTip || ov.Branch != "main" {
		t.Fatalf("after the branch moved: %+v, want was-tip", ov)
	}
}

func TestNoteCountsListOverviewsApart(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	ctx := context.Background()
	got, err := saveOv(t, svc, ovDoc("T", "[a](a.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	c, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	head := revParse(t, dir, "HEAD")
	if len(c.Overviews) != 1 || c.Overviews[0].ID != got.ID || c.Overviews[0].Title != "T" || c.Overviews[0].Branch != "main" {
		t.Fatalf("Overviews = %+v", c.Overviews)
	}
	if c.ByCommit[head] != 0 || len(c.Reviews) != 0 || len(c.PreviewReviews) != 0 {
		t.Fatalf("an overview leaked into the note badges / reviews: %+v", c)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/domain/ -run 'Overviews|OverviewKind|NoteCountsListOverviews' -count=1`
Expected: FAIL — undefined `Overviews`, `ErrOverviewNotFound`, field `Overviews`.

- [ ] **Step 3: Implement**

`internal/domain/overview_read.go`:

```go
package domain

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
	"github.com/homeend/gigagit/internal/notes"
)

// ErrOverviewNotFound: no permanent overview has that id.
var ErrOverviewNotFound = errors.New("overview not found")

// Overview is one stored permanent overview, as every frontend sees it.
// Kind is a review's three commit states: on the branch (its tip is still
// the commit), was the tip, or on a commit (no branch recorded).
type Overview struct {
	ID, Commit, Branch, Scope, Agent, Title string
	Kind                                    ReviewNoteKind
	Created, Updated                        time.Time
	Doc                                     *notebatch.OverviewDoc
}

// OverviewHead is an overview without its document: what a row needs.
type OverviewHead struct {
	ID, Commit, Branch, Scope, Agent, Title string
	Created                                 time.Time
}

func overviewHead(n model.Note) OverviewHead {
	return OverviewHead{ID: n.ID, Commit: n.Address.Commit, Branch: n.Address.Branch, Scope: n.Preview,
		Agent: n.Author, Title: strings.TrimSpace(n.Summary), Created: n.Created}
}

// overviewNotes is every overview note (roots), newest first.
func (s *Service) overviewNotes(ctx context.Context) ([]model.Note, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return nil, ErrNotesDisabled
	}
	all, err := loadParts(st, notes.PartCommits, notes.PartPreviews)
	if err != nil {
		return nil, err
	}
	var out []model.Note
	for _, n := range all {
		if !n.IsReply() && n.IsOverviewNote() {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Created.After(out[b].Created) })
	return out, nil
}

func (s *Service) overviewOf(ctx context.Context, n model.Note, tips map[string]string) Overview {
	h := overviewHead(n)
	b := h.Branch
	tip, ok := tips[b]
	if !ok && b != "" {
		tip = s.branchTip(ctx, b)
		tips[b] = tip
	}
	o := Overview{ID: h.ID, Commit: h.Commit, Branch: b, Scope: h.Scope, Agent: h.Agent, Title: h.Title,
		Kind: ReviewKindOf(b, h.Commit, tip), Created: n.Created, Updated: n.Updated}
	if doc, err := notebatch.ParseOverview([]byte(n.Rationale)); err == nil {
		o.Doc = &doc
	}
	return o
}

// Overviews is every stored overview (commit and PR), newest first.
func (s *Service) Overviews(ctx context.Context) ([]Overview, error) {
	ns, err := s.overviewNotes(ctx)
	tips := map[string]string{}
	out := make([]Overview, 0, len(ns))
	for _, n := range ns {
		out = append(out, s.overviewOf(ctx, n, tips))
	}
	return out, err
}

// Overview is one stored overview by id.
func (s *Service) Overview(ctx context.Context, id string) (Overview, error) {
	ns, err := s.overviewNotes(ctx)
	if err != nil {
		return Overview{}, err
	}
	for _, n := range ns {
		if n.ID == id {
			return s.overviewOf(ctx, n, map[string]string{}), nil
		}
	}
	return Overview{}, ErrOverviewNotFound
}

// OverviewsForCommit is the commit overviews stored on sha (a PR's are its
// PR's, never its head commit's — R10), newest first.
func (s *Service) OverviewsForCommit(ctx context.Context, sha string) ([]Overview, error) {
	ns, err := s.overviewNotes(ctx)
	tips := map[string]string{}
	var out []Overview
	for _, n := range ns {
		if n.Address.Commit == sha && n.Preview == "" {
			out = append(out, s.overviewOf(ctx, n, tips))
		}
	}
	return out, err
}

// RemoveOverview deletes overview id; ErrOverviewNotFound when id is not one.
func (s *Service) RemoveOverview(ctx context.Context, id string) error {
	if _, err := s.Overview(ctx, id); err != nil {
		return err
	}
	return s.NoteRemove(ctx, id)
}
```

In `internal/domain/notes.go` NoteCounts, after `WorkingReviews`:

```go
	// Overviews are the permanent overviews stored on a commit, newest
	// first: the Commits list's ≡ and the Branches tab's overview rows. Never
	// in a ◆N badge, never in Reviews (spec 2026-10-12 R9).
	Overviews []OverviewHead
	// PreviewOverviews are a pull request's overviews, by scope (R10).
	PreviewOverviews map[string][]OverviewHead
```

In the counting loop, before `if n.IsReviewNote() {`:

```go
		if n.IsOverviewNote() {
			if n.Preview != "" {
				if c.PreviewOverviews == nil {
					c.PreviewOverviews = map[string][]OverviewHead{}
				}
				c.PreviewOverviews[n.Preview] = append(c.PreviewOverviews[n.Preview], overviewHead(n))
				continue
			}
			c.Overviews = append(c.Overviews, overviewHead(n))
			continue
		}
```

After the loop (where `c.Reviews` gets sorted, if it does — check the end of `NoteCounts`; if reviews are not sorted there, sort overviews here):

```go
	sort.SliceStable(c.Overviews, func(a, b int) bool { return c.Overviews[a].Created.After(c.Overviews[b].Created) })
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/domain/ -run 'Overview' -count=1`
Expected: PASS (Task 3's tests too).

- [ ] **Step 5: Commit**

```bash
gg add internal/domain/overview_read.go internal/domain/overview_read_test.go internal/domain/notes.go
git commit -F <msgfile>   # "domain: the permanent-overview read model; note counts list overviews apart"
```

---

### Task 5: Isolation — no plain-note reader sees an overview (R9)

**Files:**
- Modify: `internal/domain/previewnotes.go:255` (loadPreviewNotes), `internal/domain/notes.go:1215` (storedGroup), `internal/domain/notes_overview.go:112` (View all notes buckets), `internal/cli/note.go` (noteList's `kept` loop ~line 858; `note show`)
- Test: `internal/domain/overview_isolation_test.go`, `internal/cli/overview_note_test.go`

**Interfaces:**
- Consumes: Tasks 3–4.
- Produces: the guarantee that only `Overview`/`Overviews`/`OverviewsForCommit`/`NoteCounts.Overviews`/`NoteCounts.PreviewOverviews` ever return an overview note.

- [ ] **Step 1: Write the failing tests**

`internal/domain/overview_isolation_test.go`:

```go
package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// R9: an overview and its remarks are shown in that overview only.
func TestOverviewInvisibleToNoteReaders(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	ctx := context.Background()
	if _, err := saveOv(t, svc, ovDoc("T", "[a](a.go:2)",
		`[{"path":"a.go","annotations":[{"newRange":[2,2],"summary":"remark"}]}]`)); err != nil {
		t.Fatal(err)
	}
	head := revParse(t, dir, "HEAD")
	// the commit's file: no note, no remark
	got, err := svc.NotesAt(ctx, model.FileAddress{State: model.StateCommitted, Commit: head, Path: "a.go"})
	if err != nil || len(got) != 0 {
		t.Fatalf("NotesAt(a.go@HEAD) = %+v %v", got, err)
	}
	// the commit itself (a bare commit address lists entry-level notes)
	got, err = svc.NotesAt(ctx, model.FileAddress{State: model.StateCommitted, Commit: head})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range got {
		if r.Note.IsOverviewNote() {
			t.Fatalf("NotesAt(commit) returned the overview: %+v", r.Note)
		}
	}
	// View all notes: no bucket, no review
	ov, err := svc.NotesOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range ov.Commits {
		if len(c.Files) != 0 || len(c.Reviews) != 0 {
			t.Fatalf("View all notes shows the overview as notes: %+v", c)
		}
	}
	// reviews never include it
	if rs, _ := svc.Reviews(ctx); len(rs) != 0 {
		t.Fatalf("Reviews = %+v", rs)
	}
}

func TestPROverviewInvisibleToThePRNotes(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	ctx := context.Background()
	set, err := svc.PreviewNotes(ctx, git.PRRef(7), "main")
	if err != nil {
		t.Fatal(err)
	}
	tg, err := svc.LinkOverviewTarget(ctx, Resolved{Preview: &set, Commit: set.Tip,
		Addr: model.FileAddress{State: model.StateCommitted, Commit: set.Tip}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.SaveOverview(ctx, SaveOverview{Target: tg, Agent: "c",
		Text: ovDoc("How #7 works", "[c](big.go:5)", `[{"path":"big.go","annotations":[{"newRange":[5,5],"summary":"remark"}]}]`)}); err != nil {
		t.Fatal(err)
	}
	all, err := svc.PreviewNotesAll(ctx, set)
	if err != nil {
		t.Fatal(err)
	}
	for path, rs := range all {
		for _, r := range rs {
			if r.Note.IsOverviewNote() {
				t.Fatalf("PR notes list the overview under %q", path)
			}
		}
	}
	c, _ := svc.NoteCounts(ctx)
	if len(c.PreviewReviews) != 0 || len(c.Overviews) != 0 {
		t.Fatalf("a PR overview leaked: reviews %v, commit overviews %v", c.PreviewReviews, c.Overviews)
	}
	if len(c.PreviewOverviews) != 1 {
		t.Fatalf("PreviewOverviews = %v", c.PreviewOverviews)
	}
}
```

`internal/cli/overview_note_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestNoteListSkipsOverviews(t *testing.T) {
	t.Parallel()
	dir := overviewCLIRepo(t) // Task 7 helper: a.go (5 lines) committed on main
	code, out, errb := runCLIStdin(t, dir, `{"version":1,"title":"T","overview":"[a](a.go:1)"}`,
		"overview", "save", "--agent", "c", "--stdin", "--json")
	if code != 0 {
		t.Fatalf("save = %d %q", code, errb)
	}
	id := jsonField(t, out, "id")
	code, out, _ = runCLI(t, dir, "note", "list", "--rev", "HEAD")
	if code != 0 || strings.Contains(out, id) {
		t.Fatalf("note list = %d %q, must not list overview %s", code, out, id)
	}
	code, _, errb = runCLI(t, dir, "note", "show", id)
	if code == 0 || !strings.Contains(errb, "gg overview show "+id) {
		t.Fatalf("note show = %d %q", code, errb)
	}
}
```

(`jsonField` — add to `internal/cli/overview_test.go` in Task 7: unmarshal `out` into `map[string]any` and return the string at key.) Because this test needs Task 7's command, write it now and expect it to fail on `unknown command "overview"`; it passes after Task 7.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/domain/ -run 'OverviewInvisible|PROverviewInvisible' -count=1`
Expected: FAIL — the PR test finds the overview in `PreviewNotesAll` (loadPreviewNotes lets it through) and/or View all notes shows a commit bucket with the overview.

- [ ] **Step 3: Implement**

`internal/domain/previewnotes.go`, in `loadPreviewNotes`'s loop:

```go
		if n.IsReviewNote() || n.IsOverviewNote() {
			continue // a review / an overview is shown as itself, never as a line note
		}
```

`internal/domain/notes.go`, `storedGroup`:

```go
func storedGroup(n model.Note) string {
	if n.IsReviewNote() || n.IsWorkingReview() {
		return "review:" + n.ID
	}
	if n.IsOverviewNote() {
		return "overview:" + n.ID // never anyone's draft review
	}
	return GroupMine
}
```

`internal/domain/notes_overview.go`, the root scan:

```go
			if n.IsReviewNote() || n.IsWorkingReview() || n.IsOverviewNote() {
				review[n.ID] = true
			}
```

and update that map's comment to `// review and overview roots: listed apart, never as files`.

`NotesAt` with a bare commit address goes through `loadNotesAt`: find its filter (`grep -n "func (s \*Service) loadNotesAt" -A 40 internal/domain/notes.go`) and add `n.IsOverviewNote()` to its skip condition next to wherever reviews are skipped or matched; if reviews are deliberately listed there (as `gg note list --rev` prints them), skip overviews explicitly:

```go
		if n.IsOverviewNote() {
			continue // an overview is listed by gg overview, never as a note (R9)
		}
```

`internal/cli/note.go`, in noteList's `kept` loop:

```go
	for _, r := range res {
		if r.Note.IsOverviewNote() {
			continue // gg overview list (R9)
		}
		if noteTypeMatches(*typ, r.Note.Source) {
```

`gg note show <id>`: find the show path (`grep -n '"show"' internal/cli/note.go`); after the note is loaded:

```go
	if n.IsOverviewNote() {
		fmt.Fprintf(stderr, "note show: %s is an overview — gg overview show %s\n", n.ID, n.ID)
		return 1
	}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/domain/ -count=1 -run 'Overview|Notes|Preview|PR'` then `go test ./internal/domain/ -count=1`
Expected: PASS. (The CLI test passes after Task 7.)

- [ ] **Step 5: Commit**

```bash
gg add internal/domain/previewnotes.go internal/domain/notes.go internal/domain/notes_overview.go internal/domain/overview_isolation_test.go internal/cli/note.go internal/cli/overview_note_test.go
git commit -F <msgfile>   # "domain, cli: an overview is shown as itself only — never as a note, a remark or a review (R9)"
```

---

### Task 6: Links — `?overview=<id>`

**Files:**
- Modify: `internal/domain/linkresolve.go:137-149` (hint check), `:616` (address-less hint), `internal/domain/linkdesc.go:142` (description), `internal/cli/link.go:654` (print)
- Create: `internal/domain/overview_link.go`
- Test: `internal/domain/overview_link_test.go`

**Interfaces:**
- Consumes: Tasks 1, 4.
- Produces: `(s *Service) OverviewLink(ctx, id string) (string, error)` — `gg://<repo>@<sha>?overview=<id>`; `ErrOverviewLinkMismatch`; `ResolveLink` refuses an overview link whose commit is not the overview's or whose id is gone.

- [ ] **Step 1: Write the failing test**

```go
package domain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestOverviewLinkRoundTrip(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	ctx := context.Background()
	got, err := saveOv(t, svc, ovDoc("T", "[a](a.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	text, err := svc.OverviewLink(ctx, got.ID)
	if err != nil || !strings.HasSuffix(text, "?overview="+got.ID) || !strings.Contains(text, revParse(t, dir, "HEAD")) {
		t.Fatalf("link = %q %v", text, err)
	}
	l, err := model.ParseLink(text)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ResolveLink(ctx, l, ResolveOpts{Cwd: svc})
	if err != nil || res.Hint.Kind != model.OverviewHintKind || res.Hint.ID != got.ID {
		t.Fatalf("resolve = %+v %v", res.Hint, err)
	}
	// moved onto another commit: refused
	commitFile(t, dir, "c.go", "c\n", "next")
	l.Target.Commit = revParse(t, dir, "HEAD")
	if _, err := ResolveLink(ctx, l, ResolveOpts{Cwd: svc}); !errors.Is(err, ErrOverviewLinkMismatch) {
		t.Fatalf("moved link: %v", err)
	}
	// gone: refused
	if err := svc.RemoveOverview(ctx, got.ID); err != nil {
		t.Fatal(err)
	}
	l, _ = model.ParseLink(text)
	if _, err := ResolveLink(ctx, l, ResolveOpts{Cwd: svc}); !errors.Is(err, ErrOverviewNotFound) {
		t.Fatalf("gone: %v", err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/domain/ -run OverviewLinkRoundTrip -count=1`
Expected: FAIL — `undefined: OverviewLink`.

- [ ] **Step 3: Implement**

`internal/domain/overview_link.go`:

```go
package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// ErrOverviewLinkMismatch refuses an overview link moved onto another commit.
var ErrOverviewLinkMismatch = errors.New("the link does not match the overview")

// OverviewLink is overview id's gg:// link: the commit it explains (a PR's
// head for a PR overview) plus ?overview=<id>.
func (s *Service) OverviewLink(ctx context.Context, id string) (string, error) {
	o, err := s.Overview(ctx, id)
	if err != nil {
		return "", err
	}
	return s.linkText(ctx, model.Link{Target: model.LinkTarget{State: model.StateCommitted, Commit: o.Commit},
		Hint: model.LinkHint{Kind: model.OverviewHintKind, ID: o.ID}})
}

// checkOverviewHint: the overview exists in res's checkout and explains the
// commit the link names.
func checkOverviewHint(ctx context.Context, svc *Service, res Resolved) error {
	o, err := svc.Overview(ctx, res.Hint.ID)
	if err != nil {
		return err
	}
	if res.Commit == "" || !strings.HasPrefix(o.Commit, res.Commit) && !strings.HasPrefix(res.Commit, o.Commit) {
		return fmt.Errorf("%w: overview %s explains %s", ErrOverviewLinkMismatch, o.ID, shortSHA(o.Commit))
	}
	return nil
}
```

`internal/domain/linkresolve.go`, after the note-hint check (~line 149):

```go
	if res.Hint.Kind == model.OverviewHintKind {
		if err := checkOverviewHint(ctx, reviewHintService(ctx, res.Checkout, opts), res); err != nil {
			return Resolved{}, err
		}
	}
```

In the address-less hint switch (~line 616), before `case model.ContentHintKind:`:

```go
		case model.OverviewHintKind:
			// An overview IS its commit — the hint only says which overview.
			return Resolved{}, fmt.Errorf("%w: an overview hint needs the commit it explains (@<commit>); %q alone names nothing", model.ErrLink, l.Hint.ID)
```

`internal/domain/linkdesc.go`, after the review case:

```go
	case model.OverviewHintKind:
		// "overview: <id> <agent> · <title>"; a miss falls through.
		if o, err := s.Overview(ctx, l.Hint.ID); err == nil {
			return "overview", o.ID, strings.TrimSpace(o.Agent + " · " + cutLabel(o.Title))
		}
```

`internal/cli/link.go` ~654:

```go
	if res.Hint.Kind == model.OverviewHintKind {
		fmt.Fprintln(stdout, "overview "+res.Hint.ID)
	}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/domain/ ./internal/cli/ ./internal/model/ -run 'Link|Overview' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/domain/overview_link.go internal/domain/overview_link_test.go internal/domain/linkresolve.go internal/domain/linkdesc.go internal/cli/link.go
git commit -F <msgfile>   # "domain: ?overview=<id> links — resolved, described, refused when moved or gone"
```

---

### Task 7: `gg overview save|list|show|rm`

**Files:**
- Create: `internal/cli/overview.go`
- Modify: `internal/cli/cli.go` (`dispatchOne` + `commands`)
- Test: `internal/cli/overview_test.go`

**Interfaces:**
- Consumes: Tasks 3, 4, 6; `resolveLinkArg`, `openLinkTarget`, `linkExit`, `steer.NotifyReload`, `steerDirFor`.
- Produces: `cmdOverview(svc, rest, stdin, stdout, stderr) int`; test helpers `overviewCLIRepo(t) string`, `jsonField(t, out, key) string`.

- [ ] **Step 1: Write the failing tests**

```go
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func overviewCLIRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "config", "user.name", "t")
	gitRun(t, dir, "config", "user.email", "t@t")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("1\n2\n3\n4\n5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "a")
	return dir
}

func jsonField(t *testing.T, out, key string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("json %q: %v", out, err)
	}
	s, _ := m[key].(string)
	return s
}

const ovCLIDoc = `{"version":1,"title":"How a works","overview":"Start at [three](a.go:3).","files":[{"path":"a.go","annotations":[{"newRange":[3,3],"summary":"the gate"}]}]}`

func TestOverviewSaveListShowRm(t *testing.T) {
	t.Parallel()
	dir := overviewCLIRepo(t)
	code, out, errb := runCLIStdin(t, dir, ovCLIDoc, "overview", "save", "--agent", "Claude Code", "--stdin")
	if code != 0 || !strings.Contains(out, "?overview=") {
		t.Fatalf("save = %d out=%q err=%q", code, out, errb)
	}
	link := strings.TrimSpace(strings.Split(strings.TrimSpace(out), "\n")[len(strings.Split(strings.TrimSpace(out), "\n"))-1])
	code, out, _ = runCLI(t, dir, "overview", "list")
	if code != 0 || !strings.Contains(out, "How a works") || !strings.Contains(out, "on-branch") || !strings.Contains(out, "Claude Code") {
		t.Fatalf("list = %d %q", code, out)
	}
	id := strings.Fields(out)[0]
	code, out, _ = runCLI(t, dir, "overview", "show", link)
	if code != 0 || !strings.Contains(out, "How a works") || !strings.Contains(out, "Start at [three](a.go:3).") || !strings.Contains(out, "a.go:3  the gate") {
		t.Fatalf("show = %d %q", code, out)
	}
	code, out, _ = runCLI(t, dir, "overview", "show", id, "--json")
	if code != 0 || jsonField(t, out, "title") != "How a works" || jsonField(t, out, "kind") != "on-branch" {
		t.Fatalf("show --json = %d %q", code, out)
	}
	if code, _, errb = runCLI(t, dir, "overview", "rm", id); code != 0 {
		t.Fatalf("rm = %d %q", code, errb)
	}
	if code, out, _ = runCLI(t, dir, "overview", "list"); code != 0 || strings.Contains(out, id) {
		t.Fatalf("list after rm = %d %q", code, out)
	}
}

func TestOverviewSaveRefusal(t *testing.T) {
	t.Parallel()
	dir := overviewCLIRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("1\n2\nX\n4\n5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errb := runCLIStdin(t, dir, ovCLIDoc, "overview", "save", "--agent", "c", "--stdin")
	if code != 1 || out != "" || !strings.Contains(errb, "a.go: uncommitted changes") {
		t.Fatalf("save on a dirty file = %d out=%q err=%q", code, out, errb)
	}
	if _, out, _ = runCLI(t, dir, "overview", "list", "--all"); strings.TrimSpace(out) != "" {
		t.Fatalf("a refused save stored: %q", out)
	}
}

func TestOverviewSaveDryRunAndTitleFlag(t *testing.T) {
	t.Parallel()
	dir := overviewCLIRepo(t)
	doc := `{"version":1,"overview":"[a](a.go:1)"}`
	code, out, errb := runCLIStdin(t, dir, doc, "overview", "save", "--agent", "c", "--stdin", "--title", "From flag", "--dry-run")
	if code != 0 || !strings.Contains(out, "From flag") {
		t.Fatalf("dry run = %d %q %q", code, out, errb)
	}
	if _, out, _ = runCLI(t, dir, "overview", "list", "--all"); strings.TrimSpace(out) != "" {
		t.Fatalf("dry run stored: %q", out)
	}
	code, out, errb = runCLIStdin(t, dir, doc, "overview", "save", "--agent", "c", "--stdin", "--title", "From flag", "--json")
	if code != 0 || jsonField(t, out, "title") != "From flag" {
		t.Fatalf("save --title = %d %q %q", code, out, errb)
	}
}

func TestOverviewRmViaNoteRm(t *testing.T) {
	t.Parallel()
	dir := overviewCLIRepo(t)
	_, out, _ := runCLIStdin(t, dir, ovCLIDoc, "overview", "save", "--agent", "c", "--stdin", "--json")
	id := jsonField(t, out, "id")
	if code, _, errb := runCLI(t, dir, "note", "rm", id); code != 0 {
		t.Fatalf("note rm = %d %q", code, errb)
	}
	if _, out, _ = runCLI(t, dir, "overview", "list"); strings.Contains(out, id) {
		t.Fatalf("still listed after note rm: %q", out)
	}
}

func TestOverviewSaveRefusesAWorkingLink(t *testing.T) {
	t.Parallel()
	dir := overviewCLIRepo(t)
	code, _, errb := runCLIStdin(t, dir, ovCLIDoc, "overview", "save", "gg://"+dir+"/a.go", "--agent", "c", "--stdin")
	if code != 1 || !strings.Contains(errb, "a permanent overview needs a commit or a pull request") {
		t.Fatalf("working link = %d %q", code, errb)
	}
}
```

(If `gg://<abs dir>/a.go` is not the local link form the resolver accepts, build the link with `runCLI(t, dir, "link", "a.go")` and use its output — `TestReviewSave*` tests show both.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/cli/ -run 'Overview|NoteListSkipsOverviews' -count=1`
Expected: FAIL — `unknown command "overview"`.

- [ ] **Step 3: Implement**

`internal/cli/cli.go`: in `dispatchOne` add

```go
	case "overview":
		return cmdOverview(svc, rest, stdin, stdout, stderr)
```

and `"overview": true` in `commands`.

`internal/cli/overview.go`:

```go
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
	"github.com/homeend/gigagit/internal/steer"
)

const overviewUsage = `usage: gg overview save [<gg-link>] --agent <name> (--stdin | --file <path>) [--title <t>] [--json] [--dry-run]
       gg overview list [--all] [--json]
       gg overview show <id|gg-link> [--json]
       gg overview rm <id>`

// cmdOverview is `gg overview …`: permanent overviews (spec 2026-10-12).
func cmdOverview(svc *domain.Service, rest []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(rest) == 0 {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	switch rest[0] {
	case "save":
		return overviewSave(svc, rest[1:], stdin, stdout, stderr)
	case "list":
		return overviewList(svc, rest[1:], stdout, stderr)
	case "show":
		return overviewShow(svc, rest[1:], stdout, stderr)
	case "rm":
		return overviewRm(svc, rest[1:], stdout, stderr)
	}
	fmt.Fprintln(stderr, overviewUsage)
	return 2
}

func overviewSave(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("overview save", flag.ContinueOnError)
	fs.SetOutput(stderr)
	agent := fs.String("agent", "", "the agent's name (shown on the overview)")
	fromStdin := fs.Bool("stdin", false, "read the overview document from stdin")
	file := fs.String("file", "", "read the overview document from a file")
	title := fs.String("title", "", "the title, when the document has none")
	dry := fs.Bool("dry-run", false, "check only; store nothing")
	asJSON := fs.Bool("json", false, "print JSON")
	link := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		link, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || strings.TrimSpace(*agent) == "" || *fromStdin == (*file != "") {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	ctx := context.Background()
	tsvc := svc
	var target domain.OverviewTarget
	var err error
	if link == "" {
		target, err = svc.HeadOverviewTarget(ctx)
	} else {
		res, rerr := resolveLinkArg(ctx, svc, link, linkShapes{Pair: true, Ref: true}, "overview save")
		if rerr != nil {
			return linkExit("overview save", rerr, stderr)
		}
		tsvc = openLinkTarget(res)
		target, err = tsvc.LinkOverviewTarget(ctx, res)
	}
	if err != nil {
		fmt.Fprintln(stderr, "overview save:", err)
		return 1
	}
	var data []byte
	if *fromStdin {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(*file)
	}
	if err != nil {
		fmt.Fprintln(stderr, "overview save:", err)
		return 1
	}
	saved, warn, err := tsvc.SaveOverview(ctx, domain.SaveOverview{Target: target, Agent: strings.TrimSpace(*agent),
		Title: *title, Text: data, DryRun: *dry})
	var refused *domain.OverviewRefused
	switch {
	case errors.As(err, &refused):
		fmt.Fprintln(stderr, "overview save: refused — nothing stored:")
		for _, p := range refused.Problems {
			fmt.Fprintln(stderr, "  "+p)
		}
		return 1
	case errors.Is(err, notebatch.ErrNotOverviewDoc):
		fmt.Fprintln(stderr, "overview save:", err)
		return 1
	case err != nil:
		fmt.Fprintln(stderr, "overview save:", err)
		return 1
	}
	where := shortHex(target.Commit)
	if target.PR != 0 {
		where = fmt.Sprintf("PR #%d @ %s", target.PR, where)
	} else if target.Branch != "" {
		where = target.Branch + " @ " + where
	}
	if *dry {
		if *asJSON {
			_ = json.NewEncoder(stdout).Encode(map[string]any{"commit": target.Commit, "branch": target.Branch, "pr": target.PR, "title": saved.Doc.Title})
		} else {
			fmt.Fprintf(stdout, "would store %q on %s\n", saved.Doc.Title, where)
		}
		return 0
	}
	steer.NotifyReload(steerDirFor(tsvc), "notes")
	text, lerr := tsvc.OverviewLink(ctx, saved.ID)
	if *asJSON {
		_ = json.NewEncoder(stdout).Encode(map[string]any{"id": saved.ID, "link": text, "commit": target.Commit,
			"branch": target.Branch, "pr": target.PR, "title": saved.Doc.Title, "replaced": saved.Replaced, "warn": warn})
	} else {
		verb := "stored"
		if saved.Replaced {
			verb = "replaced"
		}
		fmt.Fprintf(stdout, "overview %s %s on %s: %s\n", saved.ID, verb, where, saved.Doc.Title)
		if lerr == nil {
			fmt.Fprintln(stdout, text)
		}
		if warn != "" {
			fmt.Fprintln(stderr, "warning:", warn)
		}
	}
	if lerr != nil {
		fmt.Fprintln(stderr, "warning: no link for the overview:", lerr)
	}
	return 0
}

// overviewKind is a row's state word.
func overviewKind(o domain.Overview) string {
	switch {
	case o.Scope != "":
		return "pr"
	case o.Kind == domain.ReviewOnBranch:
		return "on-branch"
	case o.Kind == domain.ReviewWasTip:
		return "was-tip"
	}
	return "commit"
}

func overviewList(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("overview list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	all := fs.Bool("all", false, "every overview in the repository (default: HEAD's)")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	ctx := context.Background()
	var ovs []domain.Overview
	var err error
	if *all {
		ovs, err = svc.Overviews(ctx)
	} else {
		head, ok, herr := svc.ResolveRev(ctx, "HEAD")
		if herr != nil || !ok {
			fmt.Fprintln(stderr, "overview list: HEAD has no commit")
			return 1
		}
		ovs, err = svc.OverviewsForCommit(ctx, strings.TrimSpace(head))
	}
	if err != nil {
		fmt.Fprintln(stderr, "overview list:", err)
		return 1
	}
	if *asJSON {
		rows := make([]map[string]any, 0, len(ovs))
		for _, o := range ovs {
			rows = append(rows, map[string]any{"id": o.ID, "commit": o.Commit, "branch": o.Branch, "kind": overviewKind(o),
				"agent": o.Agent, "title": o.Title, "created": o.Created})
		}
		_ = json.NewEncoder(stdout).Encode(rows)
		return 0
	}
	for _, o := range ovs {
		fmt.Fprintf(stdout, "%s %s %s %s %s  %s\n", o.ID, shortHex(o.Commit), overviewKind(o),
			o.Created.Local().Format("2006-01-02 15:04"), o.Agent, o.Title)
	}
	return 0
}

// overviewByArg reads an id or an overview link.
func overviewByArg(ctx context.Context, svc *domain.Service, arg string) (*domain.Service, domain.Overview, error) {
	if strings.HasPrefix(arg, "gg://") {
		res, err := resolveLinkArg(ctx, svc, arg, linkShapes{}, "overview show")
		if err != nil {
			return nil, domain.Overview{}, err
		}
		if res.Hint.Kind != model.OverviewHintKind {
			return nil, domain.Overview{}, errors.New("that link names no overview (it has no ?overview=<id>)")
		}
		tsvc := openLinkTarget(res)
		o, err := tsvc.Overview(ctx, res.Hint.ID)
		return tsvc, o, err
	}
	o, err := svc.Overview(ctx, arg)
	return svc, o, err
}

func overviewShow(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	asJSON := false
	var pos []string
	for _, a := range args {
		if a == "--json" {
			asJSON = true
			continue
		}
		pos = append(pos, a)
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	ctx := context.Background()
	tsvc, o, err := overviewByArg(ctx, svc, pos[0])
	if err != nil {
		fmt.Fprintln(stderr, "overview show:", err)
		return 1
	}
	link, _ := tsvc.OverviewLink(ctx, o.ID)
	if asJSON {
		var doc json.RawMessage
		if o.Doc != nil {
			doc = o.Doc.Canonical()
		}
		_ = json.NewEncoder(stdout).Encode(map[string]any{"id": o.ID, "link": link, "commit": o.Commit, "branch": o.Branch,
			"kind": overviewKind(o), "agent": o.Agent, "title": o.Title, "document": doc})
		return 0
	}
	fmt.Fprintf(stdout, "%s — %s (%s %s, %s)\n", o.ID, o.Title, overviewKind(o), shortHex(o.Commit), o.Agent)
	if link != "" {
		fmt.Fprintln(stdout, link)
	}
	if o.Doc == nil {
		fmt.Fprintln(stderr, "warning: the stored document no longer parses")
		return 1
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, strings.TrimRight(o.Doc.Overview, "\n"))
	for _, f := range o.Doc.Files {
		for _, n := range f.Notes {
			fmt.Fprintf(stdout, "\n%s:%s  %s\n", f.Path, rangeText(n.Range), n.Summary)
			if r := strings.TrimSpace(n.Rationale); r != "" {
				fmt.Fprintln(stdout, "    "+strings.ReplaceAll(r, "\n", "\n    "))
			}
		}
	}
	return 0
}

func rangeText(r [2]int) string {
	if r[1] > r[0] {
		return fmt.Sprintf("%d-%d", r[0], r[1])
	}
	return fmt.Sprint(r[0])
}

func overviewRm(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	if err := svc.RemoveOverview(context.Background(), args[0]); err != nil {
		fmt.Fprintln(stderr, "overview rm:", err)
		return 1
	}
	steer.NotifyReload(steerDirFor(svc), "notes")
	fmt.Fprintln(stdout, "removed overview", args[0])
	return 0
}
```

Note for the implementer: `shortHex` — use the CLI's existing short-sha helper (`grep -n "func short" internal/cli/*.go`); if none fits, add `func shortHex(s string) string { if len(s) > 7 { return s[:7] }; return s }` to this file.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/cli/ -run 'Overview|NoteList' -count=1`
Expected: PASS (Task 5's CLI test too).

- [ ] **Step 5: Commit**

```bash
gg add internal/cli/overview.go internal/cli/overview_test.go internal/cli/cli.go
git commit -F <msgfile>   # "cli: gg overview save|list|show|rm"
```

---

### Task 8: e2e scenario, docs, skills text

**Files:**
- Create: `e2e/scenarios/overview_permanent.toml`
- Modify: `internal/agentskill/using-gg.md` (a "Permanent overviews" block after `gg review save`'s, ~line 125), `internal/agentskill/reviewing-with-gg.md` (one line after the `"overview"` paragraph, ~line 312), `internal/agentskill/agentskill.go` (`Version` 163→164, `ReviewVersion` 17→18), `CHANGELOG.md`, `README.md` (CLI section), `docs/CLAUDE-details.md` (a "Permanent overviews" entry), `CLAUDE.md` (domain row: add "permanent overviews (`SaveOverview`, tag `overview`)" — one clause), `docs/superpowers/specs/2026-10-12-permanent-overviews-design.md` §11 (the amendment)

- [ ] **Step 1: Spec amendment** — in §11 replace plan 1's "and skills" with "and the using-gg / reviewing-with-gg command docs" and add to plan 2: "the gg-overview skill's switch to permanent-by-default (R1) — it ships with the surface that shows a stored overview; and §6's `OverviewDocAt` / `OverviewFileNotes`, which ship with their only consumers (the split's two sides)".

- [ ] **Step 2: e2e scenario** (load the `writing-e2e-scenarios` skill first; keys follow `e2e/scenarios/tui_stored_overview.toml`; runs cannot write files between them, so the dirty-file refusal stays in the CLI test and "was-tip" in `TestOverviewKindFollowsTheBranch`)

```toml
name = "overview: save on HEAD, list it, a past-the-end save is refused whole, show it, remove it"

# One commit adding a.txt (3 lines). An agent stores a permanent overview
# with a remark; it lists on-branch; a second save whose link runs past the
# file's end is refused and stores nothing; show prints the document and
# the remark; rm deletes it.
[input]
steps = [
  { write = "a.txt", content = "one\ntwo\nthree\n" },
  { commit = "add a" },
]

[[run]]
cmd   = ["overview", "save", "--agent", "Claude Code", "--stdin"]
stdin = '{"version":1,"title":"How a works","overview":"Start at [two](a.txt:2).","files":[{"path":"a.txt","annotations":[{"newRange":[2,2],"summary":"the middle"}]}]}'
exit  = 0
stdout_contains = ["stored on main @", "How a works", "?overview="]

[[run]]
cmd   = ["overview", "save", "--agent", "Claude Code", "--stdin"]
stdin = '{"version":1,"title":"Past the end","overview":"[far](a.txt:9)"}'
exit  = 1
stderr_contains = ["refused — nothing stored", "a.txt:9: past the end (3 lines at"]

[[run]]
cmd   = ["overview", "list"]
exit  = 0
stdout_contains = ["on-branch", "Claude Code", "How a works"]
stdout_excludes = ["Past the end"]

[[run]]
cmd   = ["overview", "list", "--all", "--json"]
exit  = 0
stdout_contains = ['"title":"How a works"']

[expect]
branch = "main"
```

(`show`/`rm` need the id the first run printed; scenarios cannot carry a value between runs, so they stay in `TestOverviewSaveListShowRm`.)

Run: `go test ./e2e/ -run 'TestScenarios/overview_permanent' -count=1`
Expected: PASS.

- [ ] **Step 3: using-gg docs** — insert after the `gg review save … --dry-run` bullet:

```markdown
- `gg overview save [<gg-link>] --agent <name> (--stdin | --file <path>) [--title <t>] [--json] [--dry-run]`
  — store a PERMANENT overview (the gg-overview document as JSON:
  `{"version":1,"title":"…","overview":"<markdown with path:N-M links>","files":[{"path":"…","annotations":[{"newRange":[a,b],"summary":"…","rationale":"…"}]}]}`;
  no summary, no meta, no oldRange) on HEAD, on the commit or branch tip a
  link names, or on a pull request (`gg link --pr <n>`). Lines are the file
  AT that commit. gg refuses the whole save — nothing stored, every problem
  listed, exit 1 — for a `note:` link, a path not in the commit, lines past
  a file's end, or (for a commit, not a PR) an anchored file that differs on
  disk: `<path>: uncommitted changes` / `<path>: differs from <sha7> on
  disk`. The same commit (or PR) and title replaces. Prints the overview's
  `gg://…?overview=<id>` link.
- `gg overview list [--all] [--json]` — HEAD's overviews (`--all`: the
  repository's): id, commit, `on-branch | was-tip | commit | pr`, date,
  agent, title.
- `gg overview show <id|link> [--json]` — the document and its remarks.
- `gg overview rm <id>` — delete one.
```

`reviewing-with-gg.md`, after the `"overview"` paragraph: `A permanent overview of code that is NOT a review — "explain how X works at this commit" — is \`gg overview save\` (using-gg), not a review.`

Bump `Version` to 164 and `ReviewVersion` to 18 in `agentskill.go`. Run `go test ./internal/agentskill/ -count=1` (golden/marker tests).

- [ ] **Step 4: CHANGELOG, README, CLAUDE-details, CLAUDE.md**

CHANGELOG (top, under the unreleased heading the file uses):

```markdown
- **Permanent overviews (CLI).** `gg overview save|list|show|rm`: an agent's
  overview stored on a commit (or a pull request) as a note tagged
  `overview` — the gg-overview document with its remarks, checked against
  the commit's tree; refused as a whole when a link names a note, a missing
  path, lines past the end, or a file with uncommitted changes. Shown only
  as itself — never as a note, a remark or a review. `?overview=<id>` links.
  (TUI and web surfaces follow.)
```

README: the same four commands in the CLI command list, one line each.
`docs/CLAUDE-details.md`: a section "Permanent overviews (2026-10-12)" with: tag `overview` + `IsOverviewNote` the only test; stored like a review (commit part; a PR overview in the previews part via its scope); `SaveOverview` check order and strings; `saveNoteWithRetry` shared with reviews; readers that skip it (`loadPreviewNotes`, `storedGroup`, `NotesOverview`, `loadNotesAt`, `gg note list/show`); `NoteCounts.Overviews` / `PreviewOverviews`; links `?overview=`.
CLAUDE.md `domain` row: add `; permanent overviews (`SaveOverview`/`Overviews`, a commit note tagged `overview`, shown only as itself)` — keep the row one line.

- [ ] **Step 5: Full gate and commit**

Run: `./test.sh` then `./test.sh race` — read the log for "all green" (memory: `| tail` exits 0 on FAIL). Unset `GG_INBOX GG_MCP_URL GG_SESSION_TOKEN GG_SESSION_ID` if this session runs inside a gg console.
Expected: all green.

```bash
gg add e2e/scenarios/overview_permanent.toml internal/agentskill/using-gg.md internal/agentskill/reviewing-with-gg.md internal/agentskill/agentskill.go CHANGELOG.md README.md docs/CLAUDE-details.md CLAUDE.md docs/superpowers/specs/2026-10-12-permanent-overviews-design.md
git commit -F <msgfile>   # "docs, e2e: permanent overviews (plan 1) — commands documented, scenario, skill versions"
```

Then the final whole-branch review (a read-only review subagent on the most capable model is allowed by CLAUDE.md), fix what it finds, and ask the user for the merge OK.
