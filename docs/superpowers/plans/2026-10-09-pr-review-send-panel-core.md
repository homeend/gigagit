# PR Review Core (plan 1 of 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The domain, document, engine, CLI and skill side of the PR-review
feature: the stored overview in a review, a PR's reviews from the PR, the
send candidates + mixed sends (`Then`), the note link, and the skills.

**Architecture:** Everything the TUI (plan 2) and the web (plan 3) will call
is built here behind domain functions with exact names (Interfaces blocks),
each test-first. The review document gains one optional key; the engine's
one send op gains a second phase run after the review's writes; the link
grammar gains one hint kind. No frontend code changes in this plan except
the mechanical `Summary` rename's call sites.

**Tech Stack:** Go 1.26, the repo's real-git test helpers (`newRealRepo`,
`sendRepo`, `forgetest`), `go test ./internal/...`, `./test.sh race` before
merge.

**Spec:** `docs/superpowers/specs/2026-10-09-pr-review-send-panel-design.md`
(sections §0–§3, §5.1–§5.2, §5.5, §6, §7.1, §7.3 are this plan's).

## Global Constraints

- `internal/notebatch` is a stdlib-only leaf: it must not import
  `agentdocs` (spec §2.1). `internal/tui` and `internal/cli` never import
  `internal/git` (archtest).
- Agents never send: nothing in this plan posts to a forge without a human's
  confirm at a terminal (`runPRSend` refuses inside a gg session).
- Every user-visible TUI string is an `i18n.T` literal present in all four
  bundles — this plan adds none; the TUI touches are a field rename only.
- The review document's `version` stays 1; no migration.
- Vocabulary (spec §0): the review's text is its **summary** (`ReviewDoc.Summary`,
  JSON `"summary"`); the stored walk is its **overview** (`ReviewDoc.Overview`,
  JSON `"overview"`). Never "tour" in code, docs or strings.
- Version bumps at the end (Task 13): `Version` 160→161, `ReviewVersion`
  16→17, `GGReviewVersion` 3→4, `GGCrossReviewVersion` 4→5,
  `GGOverviewVersion` 1→2; then `gg init --update`.
- Worktree: `.claude/worktrees/pr-review-send-panel` (branch
  `feat/pr-review-send-panel`). Commit with `gg add <files>` then
  `git commit -F <msgfile>`; never `git add -A`; never push.

## Review Focus

1. **A prose review (`Doc == nil`) through `gg review show`** — must print its
   text as the summary and no overview, never panic (Task 5 test).
2. **A `?note=<id>` link to a deleted note** — `gg open`/`#` must refuse with
   `note <id> is not here`, not open the diff silently (Task 11 test).
3. **`--body-from` naming a review the PR does not own** — refused with
   `review <id> is not in this PR`, nothing posted (Task 8 test).
4. **An overview anchor into a file the review renamed** — the anchor names
   the NEW path; the old path draws plain (Task 3 test).
5. **A full send of a review with an overview** — the review stays listed,
   its remarks moved; a review without one is removed as today (Task 4 test).

---

### Task 1: Rename `ReviewDoc.Overview` to `Summary` (and `ReviewShow`)

**Files:**
- Modify: `internal/notebatch/review.go:51-55,268-269`
- Modify: `internal/domain/review_link.go:431,469`, `internal/domain/forge_send_front.go:79`,
  `internal/domain/forge_send_body.go:80`, `internal/domain/review_threads.go:174`
- Modify: `internal/cli/review_show.go:120`, `internal/cli/review.go:460`
- Modify: `internal/tui/diff_stack_keys.go:37`, `internal/tui/review_view.go:384,459`
- Modify: `internal/web/reviews.go:177`, `internal/web/review.go:412` (the Go
  field only; the wire key `overviewMd` stays until plan 3)
- Test: `internal/cli/review_show_test.go` (the JSON key), `internal/notebatch/review_test.go`

**Interfaces:**
- Produces: `notebatch.ReviewDoc{Summary string; Meta []MetaKV; Files []ReviewFile}`;
  `domain.ReviewShow.Summary string \`json:"summary"\``. After this task the
  name `Overview` is FREE on both types; Task 2 and Task 5 take it for the
  stored overview.

- [ ] **Step 1: Write the failing test** — append to `internal/cli/review_show_test.go`:

```go
// The review's text is its SUMMARY on the wire (spec §0): "overview" is
// the stored walk, added later.
func TestReviewShowJSONSaysSummary(t *testing.T) {
	t.Parallel()
	dir, _ := reviewedRepo(t) // review_show_test.go's fixture: one stored review on HEAD
	code, out, errs := runCLI(t, dir, "review", "show", "--json", "latest")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["summary"]; !ok {
		t.Fatalf("no \"summary\" key: %s", out)
	}
	if _, ok := got["overview"]; ok {
		t.Fatalf("\"overview\" must not carry the summary any more: %s", out)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/cli -run TestReviewShowJSONSaysSummary -count=1`
Expected: FAIL — `"overview" must not carry the summary any more`.

- [ ] **Step 3: Rename the field everywhere**

```bash
cd /work/gigagit/.claude/worktrees/pr-review-send-panel
# notebatch: the struct, its doc comment, ParseReview, Canonical
sed -i 's/^\tOverview string$/\tSummary  string/' internal/notebatch/review.go
sed -i 's/doc := ReviewDoc{Overview: raw.Summary}/doc := ReviewDoc{Summary: raw.Summary}/' internal/notebatch/review.go
sed -i 's/Summary: d.Overview, Meta: toCanonMeta(d.Meta)/Summary: d.Summary, Meta: toCanonMeta(d.Meta)/' internal/notebatch/review.go
sed -i 's|// ReviewDoc is a parsed review document. Overview is the markdown summary.|// ReviewDoc is a parsed review document. Summary is the review'"'"'s own\n// markdown text (what a forge gets as the review body).|' internal/notebatch/review.go
# every caller of the document's summary
grep -rl 'Doc\.Overview\|doc\.Overview' internal --include='*.go' | xargs sed -i 's/Doc\.Overview/Doc.Summary/g; s/doc\.Overview/doc.Summary/g'
# the ReviewShow wire
sed -i 's/\tOverview string             `json:"overview"`/\tSummary  string             `json:"summary"`/' internal/domain/review_link.go
sed -i 's/Link: link, Overview: r.Text,/Link: link, Summary: r.Text,/; s/out.Overview, out.Meta = r.Doc.Summary, metaMap(r.Doc.Meta)/out.Summary, out.Meta = r.Doc.Summary, metaMap(r.Doc.Meta)/' internal/domain/review_link.go
sed -i 's/strings.TrimRight(rs.Overview, "\\n")/strings.TrimRight(rs.Summary, "\\n")/' internal/cli/review_show.go
gofmt -w internal/notebatch/review.go internal/domain/review_link.go
go build ./... && go vet ./internal/notebatch ./internal/domain ./internal/cli ./internal/tui ./internal/web
```

Then grep for anything missed and fix by hand:

```bash
grep -rn '\.Overview\b' internal --include='*.go' | grep -v _test | grep -iv 'agentdocs\|docs\.Overview\|OverviewWire\|o\.Overview\|ov\.Overview'
```

Expected: no line that refers to a review document or `ReviewShow`.

- [ ] **Step 4: Run the whole suite for the three packages**

Run: `go test ./internal/notebatch ./internal/domain ./internal/cli ./internal/mcp ./internal/tui ./internal/web -count=1 2>&1 | tail -8`
Expected: all `ok`. A test that asserted the JSON key `"overview"` (grep
`internal/mcp/*_test.go internal/cli/*_test.go` for `"overview"`) is
updated to `"summary"` — that is the only legitimate failure.

- [ ] **Step 5: Commit**

```bash
gg add internal/notebatch internal/domain internal/cli internal/mcp internal/tui internal/web
printf '%s\n' "refactor: the review document's text is its Summary (ReviewDoc, ReviewShow \"summary\")" "" "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" "Claude-Session: https://claude.ai/code/session_01JeY1FfbBboTS6VJ9dgXv3U" > /tmp/m && git commit -q -F /tmp/m
```

---

### Task 2: The `"overview"` field of the review document

**Files:**
- Modify: `internal/notebatch/review.go` (struct, `rawReview`, `ParseReview`, `canonDoc`, `Canonical`)
- Test: `internal/notebatch/review_test.go`

**Interfaces:**
- Produces: `notebatch.ReviewDoc.Overview string` (JSON `"overview"`, optional);
  `const notebatch.MaxOverviewBytes = 64 << 10`; a document whose overview is
  longer fails `ParseReview` with an error wrapping `ErrNotReviewDoc`.

- [ ] **Step 1: Write the failing tests** — append to `internal/notebatch/review_test.go`:

```go
func TestReviewDocOverviewRoundTrips(t *testing.T) {
	in := `{"version":1,"summary":"s","overview":"The result.\n\n[the parser](a.go:3-4)","files":[]}`
	doc, err := ParseReview([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Overview != "The result.\n\n[the parser](a.go:3-4)" {
		t.Fatalf("overview = %q", doc.Overview)
	}
	again, err := ParseReview(doc.Canonical())
	if err != nil {
		t.Fatal(err)
	}
	if again.Overview != doc.Overview {
		t.Fatalf("Canonical dropped the overview: %q", again.Overview)
	}
}

// Without the key nothing changes: the canonical bytes are what they were.
func TestReviewDocWithoutOverviewIsUnchanged(t *testing.T) {
	doc, err := ParseReview([]byte(`{"version":1,"summary":"s","files":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Overview != "" {
		t.Fatalf("overview = %q, want none", doc.Overview)
	}
	if strings.Contains(string(doc.Canonical()), "overview") {
		t.Fatalf("Canonical wrote an empty overview:\n%s", doc.Canonical())
	}
}

func TestReviewDocOverviewTooLongIsRefused(t *testing.T) {
	big := strings.Repeat("x", MaxOverviewBytes+1)
	_, err := ParseReview([]byte(`{"version":1,"summary":"s","overview":"` + big + `","files":[]}`))
	if !errors.Is(err, ErrNotReviewDoc) || !strings.Contains(err.Error(), "overview exceeds 64 KiB") {
		t.Fatalf("err = %v", err)
	}
}
```

(Add `"errors"` and `"strings"` to the test file's imports if missing.)

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/notebatch -run 'TestReviewDocOverview|TestReviewDocWithoutOverview' -count=1`
Expected: build failure — `doc.Overview undefined`, `MaxOverviewBytes undefined`.

- [ ] **Step 3: Implement**

In `internal/notebatch/review.go`:

```go
// MaxOverviewBytes caps a review's stored overview (spec §2.1). It equals
// agentdocs.MaxOverviewBytes — pinned by a domain test, since this package
// must stay stdlib-only.
const MaxOverviewBytes = 64 << 10

// ReviewDoc is a parsed review document. Summary is the review's own
// markdown text (what a forge gets as the review body); Overview is the
// optional stored walk — markdown whose links are anchors into the reviewed
// change (spec §2), local only.
type ReviewDoc struct {
	Summary  string
	Overview string
	Meta     []MetaKV
	Files    []ReviewFile
}
```

In `rawReview` add, right after `Summary string \`json:"summary"\``:

```go
	Overview string          `json:"overview"`
```

In `ParseReview`, after the empty-summary check:

```go
	if len(raw.Overview) > MaxOverviewBytes {
		return ReviewDoc{}, notDoc("overview exceeds 64 KiB (%d bytes)", len(raw.Overview))
	}
	doc := ReviewDoc{Summary: raw.Summary, Overview: raw.Overview}
```

(replacing the existing `doc := ReviewDoc{Summary: raw.Summary}` line). In
`canonDoc` add after `Summary`:

```go
	Overview string      `json:"overview,omitempty"`
```

and in `Canonical`: `c := canonDoc{Version: 1, Summary: d.Summary, Overview: d.Overview, Meta: toCanonMeta(d.Meta)}`.
Update the package doc comment's shape line to
`{"version":1,"summary":"<markdown>","overview":"<markdown, optional>","meta":{…},`.

- [ ] **Step 4: Run the package**

Run: `go test ./internal/notebatch -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/notebatch
printf '%s\n' "feat(notebatch): the review document's optional \"overview\" (64 KiB cap)" "" "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" "Claude-Session: https://claude.ai/code/session_01JeY1FfbBboTS6VJ9dgXv3U" > /tmp/m && git commit -q -F /tmp/m
```

---

### Task 3: `domain.ReviewOverview` — the stored overview resolved against the reviewed tip

**Files:**
- Create: `internal/domain/review_overview.go`
- Test: `internal/domain/review_overview_test.go`

**Interfaces:**
- Consumes: `agentdocs.ParseOverview(text) (markdown.Doc, []agentdocs.Anchor)`,
  `Service.ReviewFiles(ctx, r) ([]model.CommitFile, error)`,
  `Service.reviewTarget(ctx, r) (model.LinkTarget, error)`, `splitTextLines`.
- Produces:

```go
// OverviewAnchor is one anchor of a stored overview with its verdict.
type OverviewAnchor struct {
	agentdocs.Anchor
	OK bool // its path is one of the review's files and its lines exist at the tip
}

// OverviewDoc is a review's stored overview, parsed and resolved.
type OverviewDoc struct {
	Text    string
	Doc     markdown.Doc
	Anchors []OverviewAnchor
}

func (d OverviewDoc) Unresolved() []string // the Dest of every anchor that is not OK, in order
func (s *Service) ReviewOverview(ctx context.Context, id string) (OverviewDoc, error) // ErrNoOverview when the review has none
var ErrNoOverview = errors.New("the review has no overview")
```

- [ ] **Step 1: Write the failing tests** — `internal/domain/review_overview_test.go`:

```go
package domain

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/notebatch"
)

// The two 64 KiB caps are one number (notebatch cannot import agentdocs).
func TestOverviewLimitsAgree(t *testing.T) {
	if notebatch.MaxOverviewBytes != agentdocs.MaxOverviewBytes {
		t.Fatalf("notebatch %d != agentdocs %d", notebatch.MaxOverviewBytes, agentdocs.MaxOverviewBytes)
	}
}

const overviewDoc = `{"version":1,"summary":"looks fine",
"overview":"Lines 5 and 25 changed.\n\n[the first](big.go:5) and [the second](big.go:24-25); [not in the PR](other.go:1); [past the end](big.go:900); [a note](note:t3); [the file](big.go)",
"files":[{"path":"big.go","annotations":[{"newRange":[5,5],"summary":"check this"}]}]}`

func TestReviewOverviewResolvesAgainstTheTip(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	rid := savePRReview(t, svc, overviewDoc)
	ov, err := svc.ReviewOverview(context.Background(), rid)
	if err != nil {
		t.Fatal(err)
	}
	var ok, bad []string
	for _, a := range ov.Anchors {
		if a.OK {
			ok = append(ok, a.Dest)
		} else {
			bad = append(bad, a.Dest)
		}
	}
	if !slices.Equal(ok, []string{"big.go:5", "big.go:24-25", "big.go"}) {
		t.Errorf("resolved = %v", ok)
	}
	if !slices.Equal(bad, []string{"other.go:1", "big.go:900", "note:t3"}) {
		t.Errorf("plain = %v", bad)
	}
	if !slices.Equal(ov.Unresolved(), bad) {
		t.Errorf("Unresolved() = %v", ov.Unresolved())
	}
	if ov.Text == "" || len(ov.Doc.Blocks) == 0 {
		t.Errorf("text/doc empty: %+v", ov)
	}
}

func TestReviewOverviewAbsent(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	rid := savePRReview(t, svc, twoRemarks)
	if _, err := svc.ReviewOverview(context.Background(), rid); !errors.Is(err, ErrNoOverview) {
		t.Fatalf("err = %v, want ErrNoOverview", err)
	}
}

// Review Focus 4: an anchor names the NEW path of a renamed file; the old
// path is not a file of the review and draws plain.
func TestReviewOverviewRenamedFile(t *testing.T) {
	t.Parallel()
	dir, _ := newRealRepo(t)
	commitFile(t, dir, "old.go", "package a\nfunc A() {}\n", "add")
	runGitIn(t, dir, "mv", "old.go", "new.go")
	runGitIn(t, dir, "commit", "-q", "-m", "rename")
	tip := revParse(t, dir, "HEAD")
	_, svc := newRealRepoAt(t, dir)
	svc.UseNotesDir(t.TempDir())
	tg := ReviewTarget{Kind: ReviewRange, Range: tip + "^.." + tip, Label: tip[:7]}
	rid, _, err := svc.SaveReview(context.Background(), SaveReview{Target: tg, Agent: "c",
		Text: `{"version":1,"summary":"s","overview":"[new](new.go:2) [old](old.go:2)","files":[]}`})
	if err != nil {
		t.Fatal(err)
	}
	ov, err := svc.ReviewOverview(context.Background(), rid)
	if err != nil {
		t.Fatal(err)
	}
	if !ov.Anchors[0].OK || ov.Anchors[1].OK {
		t.Fatalf("anchors = %+v", ov.Anchors)
	}
}
```

Check the sibling test files for the exact names of `commitFile`,
`runGitIn`, `revParse`, `newRealRepoAt` (all used in `forge_send_test.go`).

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/domain -run 'TestOverviewLimitsAgree|TestReviewOverview' -count=1`
Expected: build failure — `svc.ReviewOverview undefined`.

- [ ] **Step 3: Implement** — `internal/domain/review_overview.go`:

```go
package domain

import (
	"context"
	"errors"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/markdown"
	"github.com/homeend/gigagit/internal/model"
)

// ErrNoOverview: the review document carries no "overview".
var ErrNoOverview = errors.New("the review has no overview")

// OverviewAnchor is one anchor of a stored overview with its verdict: OK
// when its path is one of the review's files and its lines exist in that
// file at the reviewed tip (the working tree for a working review). A note
// anchor (note:t<n>) means nothing in a stored review: never OK.
type OverviewAnchor struct {
	agentdocs.Anchor
	OK bool
}

// OverviewDoc is a review's stored overview (spec §2): the same document
// kind as a temporary overview — the same grammar, parsed by the same
// parser — resolved against the reviewed change instead of the files on
// disk. Every frontend draws it with the temporary overview's renderer;
// an anchor that is not OK draws plain.
type OverviewDoc struct {
	Text    string
	Doc     markdown.Doc
	Anchors []OverviewAnchor
}

// Unresolved lists the destinations drawn plain, in document order: what
// `gg review save` reports so the agent can fix the text.
func (d OverviewDoc) Unresolved() []string {
	var out []string
	for _, a := range d.Anchors {
		if !a.OK {
			out = append(out, a.Dest)
		}
	}
	return out
}

// ReviewOverview parses and resolves review id's stored overview.
func (s *Service) ReviewOverview(ctx context.Context, id string) (OverviewDoc, error) {
	r, err := s.Review(ctx, id)
	if err != nil {
		return OverviewDoc{}, err
	}
	if r.Doc == nil || r.Doc.Overview == "" {
		return OverviewDoc{}, ErrNoOverview
	}
	doc, anchors := agentdocs.ParseOverview(r.Doc.Overview)
	out := OverviewDoc{Text: r.Doc.Overview, Doc: doc}
	files, err := s.ReviewFiles(ctx, r)
	if err != nil {
		return OverviewDoc{}, err
	}
	inReview := make(map[string]bool, len(files))
	for _, f := range files {
		inReview[f.Path] = true
	}
	lineCount := map[string]int{} // path → lines at the tip (-1 = unreadable)
	count := func(path string) int {
		if n, ok := lineCount[path]; ok {
			return n
		}
		n := -1
		if lines, ok := s.overviewFileLines(ctx, r, path); ok {
			n = len(lines)
		}
		lineCount[path] = n
		return n
	}
	for _, a := range anchors {
		oa := OverviewAnchor{Anchor: a}
		switch {
		case a.Note != "" || a.Path == "" || !inReview[a.Path]:
			// plain
		case a.Start == 0:
			oa.OK = true
		default:
			n := count(a.Path)
			last := max(a.End, a.Start)
			oa.OK = n >= 0 && last <= n
		}
		out.Anchors = append(out.Anchors, oa)
	}
	return out, nil
}

// overviewFileLines reads path as the review's tip has it: the working file
// for a working review, else the file at the tip commit.
func (s *Service) overviewFileLines(ctx context.Context, r Review, path string) ([]string, bool) {
	ref := model.FileRef{Source: model.SourceUnstaged, Path: path}
	if r.Kind != ReviewOnWorktree {
		t, err := s.reviewTarget(ctx, r)
		if err != nil {
			return nil, false
		}
		tip := t.Commit
		if t.Pair != nil {
			tip = t.Pair.B
		}
		ref = model.FileRef{Source: model.SourceCommit, Locator: tip, Path: path}
	}
	data, err := s.ResolveBytes(ctx, ref)
	if err != nil {
		return nil, false
	}
	return splitTextLines(data)
}
```

If `markdown.Doc` has no `Blocks` field, read `internal/markdown` for the
tree's root field and adjust the test's emptiness check.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/domain -run 'TestOverviewLimitsAgree|TestReviewOverview' -count=1`
Expected: PASS. Then `go test ./internal/archtest -count=1` (domain may
import agentdocs — agentdocs is a leaf; the guard pins what agentdocs
imports, not who imports it) — expected PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/domain/review_overview.go internal/domain/review_overview_test.go
printf '%s\n' "feat(domain): ReviewOverview resolves a review's stored overview against the reviewed tip" "" "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" "Claude-Session: https://claude.ai/code/session_01JeY1FfbBboTS6VJ9dgXv3U" > /tmp/m && git commit -q -F /tmp/m
```

---

### Task 4: A send never carries, and never deletes, the overview

**Files:**
- Modify: `internal/domain/forge_send_ledger.go:376-392` (the `whole && reviewDone` arm)
- Test: `internal/domain/forge_send_ledger_test.go` (or the file holding the
  settle tests — `grep -l "allRemarksMoved\|settleSends" internal/domain/*_test.go`)

**Interfaces:**
- Consumes: `reviewDocOf(n model.Note) Review`, `allRemarksMoved(r Review) bool`.
- Produces: the settle rule — a review note whose document has an overview
  is never removed by a send; its summary is stamped sent instead.

- [ ] **Step 1: Write the failing tests** — append to
  `internal/domain/forge_settle_race_test.go`, next to
  `TestSettleRemovesASentReviewInOneLockedWrite`, whose mechanics these
  copy: the review note is stamped as a whole send, the fake forge reports
  the posted threads (their bodies carry the send marker), and
  `PRRevalidate` runs the settle pass.

```go
// settleSentReview stamps review rid as sent whole to PR #7 with every
// remark posted, and makes the fake forge show those threads.
func settleSentReview(t *testing.T, svc *Service, ff *fakeForge, rid string) {
	t.Helper()
	ctx := context.Background()
	r, _ := svc.Review(ctx, rid)
	fps := r.remarkFPs()
	at := settleT0.Add(-time.Minute)
	if err := svc.notesStore(ctx).Edit(rid, func(n *model.Note) error {
		n.Send = &model.NoteSend{PR: 7, Review: "PRR_done", At: at}
		for i, fp := range fps {
			n.RemarkSends = append(n.RemarkSends, model.RemarkSend{RemarkFP: fp,
				Send: model.NoteSend{PR: 7, Review: "PRR_done", Thread: fmt.Sprintf("PRRT_%d", i), At: at}})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ff.mu.Lock()
	for i, fp := range fps {
		th := fmt.Sprintf("PRRT_%d", i)
		ff.comments = append(ff.comments, model.ForgeComment{ID: "PRRC_" + th, Kind: model.ForgeCommentInline, Path: "big.go",
			Line: 5, Body: "x\n\n" + forge.SendMarker(RemarkKey(rid, fp)), ThreadID: th, ReviewID: "PRR_done"})
	}
	ff.mu.Unlock()
	svc.invalidateNoteCounts()
	if _, err := svc.PRRevalidate(ctx, 7); err != nil {
		t.Fatal(err)
	}
}

// A review with a stored overview survives a full send (spec §2.3): every
// remark moved, the summary stamped, the review still listed. One without
// an overview is removed as before.
func TestSettleKeepsAReviewWithAnOverview(t *testing.T) {
	t.Parallel()
	svc, ff, head := sendRepo(t)
	svc.forgeNow = func() time.Time { return settleT0 }
	ctx := context.Background()
	doc := `{"version":1,"summary":"s","overview":"[here](big.go:5)","files":[{"path":"big.go","annotations":[{"newRange":[5,5],"summary":"x"}]}]}`
	rid, _, err := svc.SaveReview(ctx, SaveReview{Target: ReviewTarget{Kind: ReviewRange, Range: head + "^.." + head, Label: "feat"}, Agent: "claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	settleSentReview(t, svc, ff, rid)
	r, err := svc.Review(ctx, rid)
	if err != nil {
		t.Fatalf("the review was removed: %v", err)
	}
	if !allRemarksMoved(r) || !r.summarySent(7) {
		t.Fatalf("remarks moved %v, summary sent %v", allRemarksMoved(r), r.summarySent(7))
	}
	// …and the overview never reaches a body.
	if b := reviewSendBody(r); strings.Contains(b, "[here](big.go:5)") {
		t.Fatalf("the send body carries the overview: %q", b)
	}
}

func TestSettleRemovesAReviewWithoutAnOverview(t *testing.T) {
	t.Parallel()
	svc, ff, head := sendRepo(t)
	svc.forgeNow = func() time.Time { return settleT0 }
	ctx := context.Background()
	doc := `{"version":1,"summary":"s","files":[{"path":"big.go","annotations":[{"newRange":[5,5],"summary":"x"}]}]}`
	rid, _, err := svc.SaveReview(ctx, SaveReview{Target: ReviewTarget{Kind: ReviewRange, Range: head + "^.." + head, Label: "feat"}, Agent: "claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	settleSentReview(t, svc, ff, rid)
	if _, err := svc.Review(ctx, rid); !errors.Is(err, ErrReviewNotFound) {
		t.Fatalf("a fully sent review without an overview must be removed; err = %v", err)
	}
}
```

(`settleT0`, `RemarkKey`, `forge.SendMarker`, `remarkFPs`,
`invalidateNoteCounts` and `forgeNow` are what the neighbouring test uses;
add `"fmt"`, `"strings"`, `"time"` and the `forge` import if the file lacks them.)

- [ ] **Step 2: Run them to see the first fail**

Run: `go test ./internal/domain -run 'TestSettleKeepsAReviewWithAnOverview|TestSettleRemovesAReviewWithoutAnOverview' -count=1`
Expected: the Keeps test FAILS with `the review was removed`; the Removes test passes already.

- [ ] **Step 3: Implement** — in the `whole && reviewDone` arm of
  `forge_send_ledger.go` replace

```go
		if allRemarksMoved(r) {
			return true, false
		}
```

with

```go
		// Every remark moved: the review is on GitHub whole — removed,
		// unless its document holds a stored overview, which never leaves
		// the machine (spec §2.3): then it stays, remarks moved, summary
		// stamped, and reads as "on GitHub".
		if allRemarksMoved(r) && (r.Doc == nil || r.Doc.Overview == "") {
			return true, false
		}
```

- [ ] **Step 4: Run the tests and the package**

Run: `go test ./internal/domain -count=1 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/domain
printf '%s\n' "feat(domain): a fully sent review keeps its stored overview locally" "" "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" "Claude-Session: https://claude.ai/code/session_01JeY1FfbBboTS6VJ9dgXv3U" > /tmp/m && git commit -q -F /tmp/m
```

---

### Task 5: `gg review save` reports unresolved anchors; `gg review show` prints the overview

**Files:**
- Modify: `internal/cli/review_save.go:95-115`, `internal/cli/review_show.go:104-125`
- Modify: `internal/domain/review_link.go` (`ReviewShow` gains `Overview`)
- Test: `internal/cli/review_save_test.go`, `internal/cli/review_show_test.go`

**Interfaces:**
- Consumes: `Service.ReviewOverview` (Task 3), `ReviewShow.Summary` (Task 1).
- Produces: `domain.ReviewShow.Overview string \`json:"overview,omitempty"\`` =
  the stored overview's text ("" when none); `gg review save --json` prints
  `{"id","link","warn","unresolved":[…]}`; text mode prints one
  `unresolved: <dest>` line per plain anchor on stderr. `gg_review_show`
  (MCP) carries `"overview"` through `ReviewShow` with no MCP change.

- [ ] **Step 1: Write the failing tests** — append to `internal/cli/review_save_test.go`:

```go
func TestReviewSaveReportsUnresolvedAnchors(t *testing.T) {
	t.Parallel()
	dir, link := reviewSaveRepo(t) // main...feat/x; f.txt is the preview's one changed file
	doc := `{"version":1,"summary":"s","overview":"[ok](f.txt:1) [gone](nope.go:3) [note](note:t1)","files":[]}`
	code, _, errs := runCLIStdin(t, dir, doc, "review", "save", link, "--agent", "c", "--stdin")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if !strings.Contains(errs, "unresolved: nope.go:3\n") || !strings.Contains(errs, "unresolved: note:t1\n") || strings.Contains(errs, "unresolved: f.txt:1") {
		t.Fatalf("stderr = %q", errs)
	}
	code, outJ, _ := runCLIStdin(t, dir, doc, "review", "save", link, "--agent", "c", "--stdin", "--json")
	if code != 0 {
		t.Fatalf("json exit %d", code)
	}
	var got struct {
		ID         string   `json:"id"`
		Unresolved []string `json:"unresolved"`
	}
	if err := json.Unmarshal([]byte(outJ), &got); err != nil || got.ID == "" {
		t.Fatalf("json %q: %v", outJ, err)
	}
	if !slices.Equal(got.Unresolved, []string{"nope.go:3", "note:t1"}) {
		t.Fatalf("unresolved = %v", got.Unresolved)
	}
}
```

and to `internal/cli/review_show_test.go`:

```go
func TestReviewShowPrintsTheOverview(t *testing.T) {
	t.Parallel()
	dir, link := reviewSaveRepo(t)
	doc := `{"version":1,"summary":"the summary","overview":"Walk: [here](f.txt:1)","files":[]}`
	if code, _, errs := runCLIStdin(t, dir, doc, "review", "save", link, "--agent", "c", "--stdin"); code != 0 {
		t.Fatalf("save: %s", errs)
	}
	code, out, _ := runCLI(t, dir, "review", "show", "latest")
	if code != 0 || !strings.Contains(out, "the summary\n") || !strings.Contains(out, "\nOverview\nWalk: [here](f.txt:1)") {
		t.Fatalf("show =\n%s", out)
	}
	_, outJ, _ := runCLI(t, dir, "review", "show", "--json", "latest")
	var got struct {
		Summary  string `json:"summary"`
		Overview string `json:"overview"`
	}
	if err := json.Unmarshal([]byte(outJ), &got); err != nil || got.Summary != "the summary" || got.Overview != "Walk: [here](f.txt:1)" {
		t.Fatalf("json %q: %v", outJ, err)
	}
}

// Review Focus 1: a prose review has no document — its text is the summary,
// there is no overview, nothing panics. The CLI refuses to save prose, so
// the review is stored through the service the CLI itself opens.
func TestReviewShowProseReview(t *testing.T) {
	t.Parallel()
	dir, _ := reviewedRepo(t) // review_show_test.go: one document review on HEAD
	svc := openCLIService(t, dir)
	head := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
	tg := domain.ReviewTarget{Kind: domain.ReviewRange, Range: head + "^.." + head, Commit: head}
	if _, _, err := svc.SaveReview(context.Background(), domain.SaveReview{Target: tg, Agent: "c", Text: "just prose, not a document"}); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runCLI(t, dir, "review", "show", "latest")
	if code != 0 || !strings.Contains(out, "just prose, not a document") || strings.Contains(out, "\nOverview\n") {
		t.Fatalf("exit %d out=%q err=%q", code, out, errs)
	}
}
```

(`reviewedRepo`, `runGit`, `openCLIService`, `runCLI`, `runCLIStdin` are
the package's existing helpers: `review_show_test.go`, `cli_test.go`,
`batch_test.go`.)

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/cli -run 'TestReviewSaveReportsUnresolvedAnchors|TestReviewShowPrintsTheOverview|TestReviewShowProseReview' -count=1`
Expected: FAIL (no `unresolved:` lines; no `Overview` section; JSON lacks `overview`).

- [ ] **Step 3: Implement**

`internal/domain/review_link.go`, `ReviewShow`: add after `Summary`:

```go
	// Overview is the stored overview's markdown (spec §2), "" when none.
	Overview string `json:"overview,omitempty"`
```

and in `ReviewShow()` after `out.Summary, out.Meta = …`: `out.Overview = r.Doc.Overview` (inside the `if r.Doc != nil` block).

`internal/cli/review_show.go`, `printReviewShow`, after the meta block and before the remarks:

```go
	if strings.TrimSpace(rs.Overview) != "" {
		fmt.Fprintf(w, "\nOverview\n%s\n", strings.TrimRight(rs.Overview, "\n"))
	}
```

`internal/cli/review_save.go`, replace the block from `rl, lerr := tsvc.ReviewLink(ctx, id)` to the `--json` print with:

```go
	rl, lerr := tsvc.ReviewLink(ctx, id)
	var unresolved []string
	if ov, oerr := tsvc.ReviewOverview(ctx, id); oerr == nil {
		unresolved = ov.Unresolved()
	} else if !errors.Is(oerr, domain.ErrNoOverview) {
		fmt.Fprintln(stderr, "warning: overview:", oerr)
	}
	if *asJSON {
		_ = json.NewEncoder(stdout).Encode(struct {
			ID         string   `json:"id"`
			Link       string   `json:"link"`
			Warn       string   `json:"warn"`
			Unresolved []string `json:"unresolved,omitempty"`
		}{id, rl, warn, unresolved})
	} else {
		fmt.Fprintln(stdout, "review:", id)
		if lerr == nil {
			fmt.Fprintln(stdout, rl)
		}
		for _, d := range unresolved {
			fmt.Fprintln(stderr, "unresolved:", d)
		}
	}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/cli ./internal/mcp -count=1 2>&1 | tail -3`
Expected: PASS (an MCP test that pins `gg_review_show`'s keys may need the
new optional key — only if it asserts the full key set).

- [ ] **Step 5: Commit**

```bash
gg add internal/cli internal/domain
printf '%s\n' "feat(cli): gg review save lists unresolved overview anchors; gg review show prints the overview" "" "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" "Claude-Session: https://claude.ai/code/session_01JeY1FfbBboTS6VJ9dgXv3U" > /tmp/m && git commit -q -F /tmp/m
```

---

### Task 6: `PreviewReviews` answers for a pull request's set

**Files:**
- Modify: `internal/domain/preview_reviews.go:9-75`
- Test: `internal/domain/pr_note_scope_test.go:170-178` (rename + flip), `internal/domain/pr_reviews_test.go`

**Interfaces:**
- Consumes: `prReviewHeads(ctx, set) []ReviewHead`, `classifyScopeReviews`.
- Produces: `PreviewReviews(ctx, set)` returns, for a PR set, its reviews
  classified current / `Older: true` / omitted exactly as for a preview.

- [ ] **Step 1: Write the failing tests** — replace `TestPreviewReviewsStayOffAPR` in `pr_note_scope_test.go` with:

```go
// A PR's stored reviews are listed from the PR (spec §3.1, R4), classified
// as a preview's: current at the head, older once the head moved on while
// both commits exist, omitted once one is gone.
func TestPreviewReviewsOfAPR(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	rid := savePRReview(t, svc, twoRemarks)
	ctx := context.Background()
	set := prNoteSetOf(t, svc)
	got, err := svc.PreviewReviews(ctx, set)
	if err != nil || len(got) != 1 || got[0].ID != rid || got[0].Older {
		t.Fatalf("current: %+v, %v", got, err)
	}
	// The head moves on (a new commit on the PR branch): the review is older.
	dir := repoDir(t, svc)
	runGitIn(t, dir, "checkout", "-q", "feat")
	commitFile(t, dir, "other.go", "package other // v2\n", "more")
	runGitIn(t, dir, "update-ref", git.PRRef(7), revParse(t, dir, "HEAD"))
	runGitIn(t, dir, "checkout", "-q", "main")
	svc.invalidateNoteCounts()
	got, err = svc.PreviewReviews(ctx, prNoteSetOf(t, svc)) // re-resolves the PR at its new head
	if err != nil || len(got) != 1 || !got[0].Older {
		t.Fatalf("older: %+v, %v (head was %s)", got, err, head[:7])
	}
}
```

`prNoteSetOf` reads the PR from the fake forge (`ff.byNum[7].HeadSHA` is
the OLD head): update it before the second call —
`ff.byNum[7] = model.PullRequest{Number: 7, State: "open", Target: "main", HeadSHA: revParse(t, dir, git.PRRef(7)), NodeID: "PR_7"}`
under `ff.mu`.

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/domain -run TestPreviewReviewsOfAPR -count=1`
Expected: FAIL — `current: [], <nil>`.

- [ ] **Step 3: Implement** — in `preview_reviews.go`:

Replace the PR early-return in `PreviewReviews`:

```go
func (s *Service) PreviewReviews(ctx context.Context, set PreviewNoteSet) ([]ReviewHead, error) {
	if _, pr := git.ParsePRRef(set.Source); pr {
		// A pull request's reviews are the ones saved for it by number
		// (prReviewHeads), classified like a preview's (spec §3.1).
		return s.classifyHeads(ctx, s.prReviewHeads(ctx, set), set.Tip, set.commitSet()), nil
	}
	sc := set.scope()
	if sc == "" {
		return nil, nil
	}
	return s.classifyScopeReviews(ctx, sc, set.Tip, set.commitSet())
}
```

and factor the loop of `classifyScopeReviews` into

```go
// classifyHeads keeps the heads gg can still show exactly (R2): one of tip
// is current; any other needs both its commits (Older). in names commits
// known to exist without a git call.
func (s *Service) classifyHeads(ctx context.Context, heads []ReviewHead, tip string, in map[string]bool) []ReviewHead {
	if len(heads) == 0 {
		return nil
	}
	exists := func(sha string) bool {
		if sha == "" {
			return false
		}
		if in[sha] {
			return true
		}
		_, found, err := s.ResolveRev(ctx, sha+"^{commit}")
		return err == nil && found
	}
	out := make([]ReviewHead, 0, len(heads))
	for _, h := range heads {
		if tip != "" && h.Commit == tip {
			out = append(out, h)
			continue
		}
		base, _, _ := strings.Cut(h.Scope, "..")
		if !exists(h.Commit) || !exists(base) {
			continue
		}
		h.Older = true
		out = append(out, h)
	}
	return out
}
```

with `classifyScopeReviews` reduced to the `NoteCounts` read + `return s.classifyHeads(ctx, c.PreviewReviews[scope], tip, in), nil`.
Update the doc comment on `PreviewReviews` (drop "A pull request's set has none here").

- [ ] **Step 4: Run the package**

Run: `go test ./internal/domain -count=1 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/domain
printf '%s\n' "feat(domain): PreviewReviews lists a pull request's reviews (current / older / gone)" "" "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" "Claude-Session: https://claude.ai/code/session_01JeY1FfbBboTS6VJ9dgXv3U" > /tmp/m && git commit -q -F /tmp/m
```

---

### Task 7: Engine — `SendPlan.Then`, replies after the review under one confirm

**Files:**
- Modify: `internal/engine/send_forge.go` (`SendPlan`, `describePlan`, `review`, `actions`)
- Test: `internal/engine/send_forge_test.go`

**Interfaces:**
- Produces: `engine.SendPlan.Then *SendPlan` — a `SendActions` plan the op
  runs after the review's writes succeeded, listed in the one confirm. Not
  run after abort, a review failure, or an interrupted review. A failure
  inside it is returned as `the review was posted; replies: N of M failed: …`
  with `Result.Changed == true`.

- [ ] **Step 1: Write the failing tests** — append to `send_forge_test.go`:

```go
func planWithThen() SendPlan {
	p := plan2()
	p.Then = &SendPlan{Target: p.Target, PR: p.PR, Mode: SendActions, Items: []SendItem{
		{Key: "d1", Label: "reply: addressed", Kind: SendReply, ThreadID: "T9", Body: "addressed"},
	}}
	return p
}

// A Then plan runs after the review's writes, inside the review's confirm.
func TestSendReviewThenRepliesUnderOneConfirm(t *testing.T) {
	t.Parallel()
	w, l := &fakeWriter{}, newLedger()
	res, err, asked := runSend(t, planWithThen(), w, l, OptComment)
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 {
		t.Fatalf("asked %d times, want one confirm", len(asked))
	}
	if !strings.Contains(asked[0].Prompt, "then, as replies:") || !strings.Contains(asked[0].Prompt, "reply: addressed") {
		t.Fatalf("the confirm must list the replies:\n%s", asked[0].Prompt)
	}
	last := w.calls[len(w.calls)-1]
	if !strings.HasPrefix(last, "Reply  T9 addressed") && !strings.Contains(last, "T9 addressed") {
		t.Fatalf("the reply must be the last write: %v", w.calls)
	}
	if !strings.Contains(res.Summary, "sent 2 comments") || !strings.Contains(res.Summary, "1 repl") {
		t.Fatalf("summary = %q", res.Summary)
	}
	if _, ok := l.stamps["d1"]; !ok {
		t.Fatal("the reply must be stamped")
	}
}

func TestSendReviewAbortRunsNoThen(t *testing.T) {
	t.Parallel()
	w, l := &fakeWriter{}, newLedger()
	if _, err, _ := runSend(t, planWithThen(), w, l, "abort"); err != nil {
		t.Fatal(err)
	}
	if len(w.calls) != 0 || len(l.stamps) != 0 {
		t.Fatalf("abort wrote %v / stamped %v", w.calls, l.stamps)
	}
}

func TestSendReviewFailureRunsNoThen(t *testing.T) {
	t.Parallel()
	w, l := &fakeWriter{fail: map[string]error{"SubmitReview": errors.New("boom")}}, newLedger()
	if _, err, _ := runSend(t, planWithThen(), w, l, OptComment); err == nil {
		t.Fatal("want the submit error")
	}
	for _, c := range w.calls {
		if strings.HasPrefix(c, "Reply  T9") || strings.Contains(c, "T9 addressed") {
			t.Fatalf("a failed review must not post its replies: %v", w.calls)
		}
	}
	if _, stamped := l.stamps["d1"]; stamped {
		t.Fatal("a reply of a failed review must not be stamped")
	}
}

func TestSendReviewThenFailureKeepsTheReview(t *testing.T) {
	t.Parallel()
	w, l := &fakeWriter{fail: map[string]error{"Reply": errors.New("502")}}, newLedger()
	p := planWithThen()
	p.Items[0].Replies = nil // the fake fails the FIRST Reply: make it the Then one
	res, err, _ := runSend(t, p, w, l, OptComment)
	if err == nil || !strings.Contains(err.Error(), "the review was posted") || !strings.Contains(err.Error(), "1 of 1 failed") {
		t.Fatalf("err = %v", err)
	}
	if !res.Changed {
		t.Fatal("the review went: Changed must be true")
	}
	if l.failed["d1"] == "" {
		t.Fatal("the failed reply must be marked failed")
	}
}
```

(`plan2()` is the file's existing two-item review plan; its first item
carries a reply, hence the `Replies = nil` line in the last test. If
`DecisionRequest`'s prompt field is not `Prompt`, use the field `runSend`'s
other tests read.)

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/engine -run 'TestSendReview.*Then|TestSendReviewAbortRunsNoThen|TestSendReviewFailureRunsNoThen' -count=1`
Expected: build failure — `p.Then undefined`.

- [ ] **Step 3: Implement** — `send_forge.go`:

`SendPlan`, after `Skipped`:

```go
	// Then is a SendActions plan run AFTER this review's writes succeeded
	// (spec §5.2, R6): the draft replies ticked beside new comments. One
	// op, one reservation, one confirm (describePlan lists it); never run
	// after an abort, a failed or an interrupted review. nil = none.
	Then *SendPlan
```

`describePlan`, before the `return`:

```go
	if p.Then != nil && len(p.Then.Items) > 0 {
		fmt.Fprintf(&b, "then, as replies:\n")
		for _, it := range p.Then.Items {
			fmt.Fprintf(&b, "  + %s\n", it.Label)
		}
		for _, s := range p.Then.Skipped {
			fmt.Fprintf(&b, "  - %s (skipped: %s)\n", s.Label, s.Reason)
		}
	}
```

In `review()`, replace the block from `res := Result{Changed: true}.WithSummary(…)` to the end with:

```go
	res := Result{Changed: true}.WithSummary("sent %d comments to %s", len(p.Items), p.Target)
	var thenErr error
	if p.Then != nil && len(p.Then.Items) > 0 {
		var sent, failed int
		sent, failed, thenErr = op.runActions(ctx, deps, *p.Then)
		res = res.AppendSummary("; %d replies", sent)
		if failed > 0 {
			res = res.AppendSummary(" (%d failed)", failed)
		}
	}
	if err := op.Ledger.Settle(ctx); err != nil {
		res = res.AppendSummary("; local copies are removed at the next refresh (%v)", err)
	}
	var unresolved int
	for _, it := range p.Items {
		if it.Resolve {
			if err := op.Writer.Resolve(ctx, threads[it.Key]); err != nil {
				unresolved++
			}
		}
	}
	if unresolved > 0 {
		res = res.AppendSummary("; %d threads could not be resolved", unresolved)
	}
	if thenErr != nil {
		return res, fmt.Errorf("the review was posted; replies: %w", thenErr)
	}
	deps.emit(ctx, Done{Result: res})
	return res, nil
```

and split `actions()`: keep its confirm + `dropGone` + the final summary,
and move the loop into

```go
// runActions posts p's replies / resolves one by one with no confirm (the
// caller confirmed — actions() for its own plan, review() for a Then plan
// under the review's confirm). It drops the items another send took
// meanwhile and stamps each reply as it goes; the error names the failures.
func (op SendToForge) runActions(ctx context.Context, deps OpDeps, p SendPlan) (sent, failed int, err error) {
	var live bool
	if p, live = op.dropGone(ctx, p); !live {
		return 0, 0, ErrSentMeanwhile
	}
	var firstErr error
	for _, it := range p.Items {
		deps.emit(ctx, Progress{Step: "sending", Detail: it.Label})
		var e error
		switch it.Kind {
		case SendReply:
			if it.Key != "" {
				_ = op.Ledger.Stamp(ctx, it.Key, model.NoteSend{PR: p.PR, Thread: it.ThreadID, At: op.now()})
			}
			var c forge.CommentRef
			if c, e = op.Writer.Reply(ctx, "", it.ThreadID, it.Body); e == nil && it.Key != "" {
				_ = op.Ledger.Stamp(ctx, it.Key, model.NoteSend{PR: p.PR, Thread: it.ThreadID, Comment: c.ID, URL: c.URL, At: op.now()})
			}
		case SendResolve:
			e = op.Writer.Resolve(ctx, it.ThreadID)
		case SendUnresolve:
			e = op.Writer.Unresolve(ctx, it.ThreadID)
		}
		if e != nil {
			failed++
			if firstErr == nil {
				firstErr = e
			}
			if it.Key != "" {
				op.Ledger.Fail(ctx, []string{it.Key}, "", e)
			}
			continue
		}
		sent++
	}
	if failed > 0 {
		return sent, failed, fmt.Errorf("%d of %d failed: %w", failed, len(p.Items), firstErr)
	}
	return sent, 0, nil
}
```

`actions()` becomes: the `ErrNothingToSend` check, the confirm as today, then

```go
	sent, failed, err := op.runActions(ctx, deps, p)
	settleErr := op.Ledger.Settle(ctx)
	if err != nil {
		return Result{Changed: sent > 0 || failed < len(p.Items)}, err
	}
	res := Result{Changed: true}.WithSummary("sent %d actions to %s", sent, p.Target)
	if settleErr != nil {
		res = res.AppendSummary("; local copies are removed at the next refresh (%v)", settleErr)
	}
	deps.emit(ctx, Done{Result: res})
	return res, nil
```

(`dropGone` moved inside `runActions`, so remove it from `actions()`.)
Also extend `planKeys` so a review-phase `fail()` never touches Then keys:
it must NOT include them — `planKeys(p)` reads only `p.Key`/`p.Items`,
which is already the case; leave it and add a comment saying so.

- [ ] **Step 4: Run the engine package**

Run: `go test ./internal/engine -count=1 2>&1 | tail -3`
Expected: PASS, including the existing `TestSendActions…` tests (the
summary text `sent N actions` is unchanged).

- [ ] **Step 5: Commit**

```bash
gg add internal/engine
printf '%s\n' "feat(engine): SendPlan.Then — replies posted after the review under one confirm" "" "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" "Claude-Session: https://claude.ai/code/session_01JeY1FfbBboTS6VJ9dgXv3U" > /tmp/m && git commit -q -F /tmp/m
```

---

### Task 8: Domain planner — `BodyFrom`, a verdict on a notes send, drafts as `Then`

**Files:**
- Modify: `internal/domain/forge_send.go:45-60` (request), `:95-140` (`planSend`), `:222-240` (`noteKinds`), `:354-460` (`planReview` default branch)
- Test: `internal/domain/forge_send_test.go`

**Interfaces:**
- Consumes: `engine.SendPlan.Then` (Task 7), `reviewSendBody`, `typedBody`, `prOwnsReview`, `summarySent`.
- Produces: `PRSendRequest.BodyFrom string \`toml:"body_from,omitempty" json:"body_from,omitempty"\``;
  `planSend` builds one `SendReview` plan for any mix of remarks and notes,
  with `Verdict = req.Verdict`, the body by precedence `BodySet → Body`,
  `BodyFrom → the review's summary (+ Key = review id, the summary-sent
  skip)`, else none; draft replies in `req.Notes` beside other notes become
  `plan.Then`. `ErrMixedSend` stays for resolve/unresolve with new
  comments, and for drafts with `--review`/`--mine`.

- [ ] **Step 1: Write the failing tests** — append to `forge_send_test.go`:

```go
func TestPlanSendMixesReviewsNotesAndAVerdict(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	rid := savePRReview(t, svc, twoRemarks)
	mine := addPRNote(t, svc, head, "big.go", 25, "mine too")
	p, err := svc.planSend(context.Background(), PRSendRequest{PR: 7,
		Notes: []string{"review:" + rid + ":0", mine}, Verdict: true, BodyFrom: rid})
	if err != nil {
		t.Fatal(err)
	}
	if p.Mode != engine.SendReview || !p.Verdict || len(p.Items) != 2 || p.Key != rid {
		t.Fatalf("plan = %+v", p)
	}
	if !strings.Contains(p.Body, "looks fine") {
		t.Fatalf("body = %q, want the review's summary", p.Body)
	}
}

func TestPlanSendTypedBodyBeatsBodyFrom(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	rid := savePRReview(t, svc, twoRemarks)
	mine := addPRNote(t, svc, head, "big.go", 25, "mine")
	p, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Notes: []string{mine}, BodyFrom: rid, Body: "typed", BodySet: true})
	if err != nil {
		t.Fatal(err)
	}
	if p.Body != "typed" || p.Key != "" {
		t.Fatalf("body %q key %q", p.Body, p.Key)
	}
}

// Review Focus 3: --body-from a review the PR does not own is refused.
func TestPlanSendBodyFromAForeignReview(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	mine := addPRNote(t, svc, head, "big.go", 25, "mine")
	tg := ReviewTarget{Kind: ReviewRange, Range: head + "^.." + head, Label: head[:7]} // the commit's own review, not the PR's
	other, _, err := svc.SaveReview(context.Background(), SaveReview{Target: tg, Agent: "c", Text: twoRemarks})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.planSend(context.Background(), PRSendRequest{PR: 7, Notes: []string{mine}, BodyFrom: other})
	if err == nil || !strings.Contains(err.Error(), "is not in this PR") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlanSendDraftsBesideNotesBecomeThen(t *testing.T) {
	t.Parallel()
	svc, ff, head := sendRepo(t)
	ff.comments = []model.ForgeComment{{ID: "C1", Kind: model.ForgeCommentInline, ThreadID: "T1", Path: "big.go", Side: model.NoteSideNew, Line: 5, StartLine: 5, Body: "please", Author: "carol"}}
	mine := addPRNote(t, svc, head, "big.go", 25, "mine")
	d, err := svc.NoteReply(context.Background(), "forge:C1", model.Note{Source: model.NoteSourceUser, Author: "me", Summary: "done"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Notes: []string{mine, d.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Mode != engine.SendReview || len(p.Items) != 1 || p.Then == nil || len(p.Then.Items) != 1 || p.Then.Items[0].Key != d.ID || p.Then.Mode != engine.SendActions {
		t.Fatalf("plan = %+v then = %+v", p, p.Then)
	}
	// Drafts alone are still the actions plan, with no Then.
	p2, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Notes: []string{d.ID}})
	if err != nil || p2.Mode != engine.SendActions || p2.Then != nil {
		t.Fatalf("drafts alone: %+v %v", p2, err)
	}
	// A resolve with a new comment is still mixed.
	if _, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Notes: []string{mine}, Resolve: []string{"T1"}}); !errors.Is(err, ErrMixedSend) {
		t.Fatalf("resolve + note: %v", err)
	}
	if _, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Notes: []string{d.ID}, Mine: true}); !errors.Is(err, ErrMixedSend) {
		t.Fatalf("draft + --mine: %v", err)
	}
}
```

(`forge_drafts_test.go` seeds the same `ForgeComment` literal shape; a
draft reply needs the PR's comments read first — if `NoteReply` on
`forge:C1` fails with "unknown thread", call `svc.PRCommentsRefresh(ctx, 7)`
before it, as that file does.)

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/domain -run 'TestPlanSendMixes|TestPlanSendTypedBody|TestPlanSendBodyFrom|TestPlanSendDrafts' -count=1`
Expected: build failure — `BodyFrom` unknown field.

- [ ] **Step 3: Implement**

`PRSendRequest`: after `BodySet`:

```go
	// BodyFrom: a Notes send whose body is this review's summary (the panel's
	// "review text" choice); its summary is stamped sent as a Review send's
	// is. Ignored when BodySet.
	BodyFrom string `toml:"body_from,omitempty" json:"body_from,omitempty"`
```

`noteKinds` → split into ids:

```go
// noteKinds splits a --note list into the draft replies to GitHub threads
// and everything else (local notes, remarks, GitHub ids, unknown ids — the
// review planner names an unknown one). A store that cannot be read is an
// error, never a guess.
func (s *Service) noteKinds(ctx context.Context, ids []string) (drafts, others []string, err error) {
	if len(ids) == 0 {
		return nil, nil, nil
	}
	byID, err := s.storedNotes(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, id := range ids {
		if n, ok := byID[id]; ok && n.IsForgeReply() {
			drafts = append(drafts, id)
		} else {
			others = append(others, id)
		}
	}
	return drafts, others, nil
}
```

`planSend`, replace from `drafts, other, err := s.noteKinds(…)` to `plan, err = s.planReview(…)`:

```go
	drafts, others, err := s.noteKinds(ctx, req.Notes)
	if err != nil {
		return engine.SendPlan{}, err
	}
	actionsOnly := len(req.Resolve)+len(req.Unresolve) > 0 || (len(drafts) > 0 && len(others) == 0)
	if actionsOnly {
		if req.Review != "" || req.Mine || req.Verdict || len(others) > 0 {
			return engine.SendPlan{}, ErrMixedSend
		}
		return s.planActions(ctx, plan, req)
	}
	if len(drafts) > 0 && (req.Review != "" || req.Mine) {
		return engine.SendPlan{}, ErrMixedSend
	}
	if rev, _, _ := s.PRInterrupted(ctx, pr.Number); rev != "" {
		return engine.SendPlan{}, ErrInterruptedPending
	}
	reviewReq := req
	reviewReq.Notes = others
	plan, err = s.planReview(ctx, plan, pr, reviewReq)
	if err == nil && len(drafts) > 0 {
		// The ticked replies go after the review, under its confirm (R6).
		then, terr := s.planActions(ctx, engine.SendPlan{Target: plan.Target, PR: plan.PR, PRID: plan.PRID, Head: plan.Head}, PRSendRequest{PR: req.PR, Notes: drafts})
		if terr != nil {
			return engine.SendPlan{}, terr
		}
		plan.Then = &then
	}
```

(the rest — the `ErrNothingToSend` check — stays). `planReview` default
branch: before the `for _, id := range req.Notes` loop add

```go
		plan.Verdict, plan.Body = req.Verdict, typedBody(req)
		if req.BodyFrom != "" && !req.BodySet {
			r, err := s.Review(ctx, req.BodyFrom)
			if err != nil {
				return engine.SendPlan{}, err
			}
			if !s.prOwnsReview(ctx, prev.Set, r.ID) {
				return engine.SendPlan{}, fmt.Errorf("%w: review %s is not in this PR", ErrSendRequest, r.ID)
			}
			plan.Key, plan.Body = r.ID, reviewSendBody(r)
			if r.summarySent(pr.Number) {
				plan.Body = ""
				plan.Skipped = append(plan.Skipped, engine.SendSkip{Label: "review summary", Reason: SkipOnGitHub})
			}
		}
```

The CLI's `TestPlanSendPlacesEachNote` asserts `!p.Verdict` for a plain
notes send — still true (`req.Verdict` false).

- [ ] **Step 4: Run the package**

Run: `go test ./internal/domain -count=1 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/domain
printf '%s\n' "feat(domain): a notes send takes a verdict, a body from a review, and drafts as the Then plan" "" "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" "Claude-Session: https://claude.ai/code/session_01JeY1FfbBboTS6VJ9dgXv3U" > /tmp/m && git commit -q -F /tmp/m
```

---

### Task 9: `PRSendCandidates` and `gg pr notes --json` extras

**Files:**
- Create: `internal/domain/forge_send_candidates.go`
- Modify: `internal/domain/notewire.go` (`WireNote.Severity`, `WireNote.Code`), `internal/cli/prsend.go:228-250` (`prNotes`)
- Test: `internal/domain/forge_send_candidates_test.go`, `internal/cli/prsend_test.go`

**Interfaces:**
- Consumes: `PRNotes`, `PRPreview`, `sendPlacer`, `noteItem`, `remarkItem`,
  `prReviewHeads`, `GroupSlot`, `Review`.
- Produces (exact, plans 2 and 3 read these):

```go
type SendCandidates struct {
	PR     int
	Head   string
	Groups []SendCandidateGroup
}
type SendCandidateGroup struct {
	ID      string    // "review:<id>" | GroupMine | GroupReplies
	Kind    string    // "review" | "mine" | "replies"
	Agent   string    // a review's agent
	Title   string    // a review's summary, first line
	Created time.Time // a review's creation
	Slot    int       // GroupSlot(ID); 0 for replies
	Rows    []SendCandidate
}
type SendCandidate struct {
	ID        string // note id | review:<id>:<n> | draft reply id
	Kind      string // "note" | "remark" | "reply"
	Severity  string // remark meta "severity" ("" when none)
	Path      string
	Range     [2]int
	Side      string // "new" | "old"
	Summary   string
	Rationale string
	Sync      model.SyncState
	Code      []string // ≤ 4 lines at the tip (old side: the base); nil when unreadable
	Skip      string   // "" = sendable; else a Skip* reason: listed, not tickable
}
const GroupReplies = "replies"
func (s *Service) PRSendCandidates(ctx context.Context, n int) (SendCandidates, error)
```

`WireNote` gains `Severity string \`json:"severity,omitempty"\`` and
`Code []string \`json:"code,omitempty"\``; `gg pr notes --json` fills both.

- [ ] **Step 1: Write the failing tests** — `internal/domain/forge_send_candidates_test.go`:

```go
package domain

import (
	"context"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

const sevRemarks = `{"version":1,"summary":"Looks fine\n\nmore","files":[
 {"path":"big.go","annotations":[{"newRange":[5,5],"summary":"check this","rationale":"why","meta":{"severity":"bug"}}]},
 {"path":"other.go","annotations":[{"newRange":[1,1],"summary":"a file the PR does not change"}]}]}`

func TestPRSendCandidatesGroupsAndRows(t *testing.T) {
	t.Parallel()
	svc, ff, head := sendRepo(t)
	ff.comments = []model.ForgeComment{{ID: "C1", Kind: model.ForgeCommentInline, ThreadID: "T1", Path: "big.go", Side: model.NoteSideNew, Line: 5, StartLine: 5, Body: "please", Author: "carol"}}
	rid := savePRReview(t, svc, sevRemarks)
	mine := addPRNote(t, svc, head, "big.go", 25, "mine")
	ctx := context.Background()
	d, err := svc.NoteReply(ctx, "forge:C1", model.Note{Source: model.NoteSourceUser, Author: "me", Summary: "done"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := svc.PRSendCandidates(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if c.PR != 7 || c.Head != head {
		t.Fatalf("header %+v", c)
	}
	var ids []string
	for _, g := range c.Groups {
		ids = append(ids, g.ID)
	}
	if !slices.Equal(ids, []string{"review:" + rid, GroupMine, GroupReplies}) {
		t.Fatalf("groups = %v", ids)
	}
	rev := c.Groups[0]
	if rev.Kind != "review" || rev.Agent != "claude" || rev.Title != "Looks fine" || rev.Slot == 0 || len(rev.Rows) != 2 {
		t.Fatalf("review group = %+v", rev)
	}
	first := rev.Rows[0]
	if first.ID != "review:"+rid+":0" || first.Kind != "remark" || first.Severity != "bug" || first.Path != "big.go" || first.Range != [2]int{5, 5} || first.Skip != "" {
		t.Fatalf("row 0 = %+v", first)
	}
	if !slices.Equal(first.Code, []string{"line 5 changed"}) {
		t.Fatalf("code = %v", first.Code)
	}
	if second := rev.Rows[1]; second.Path != "other.go" || second.Skip != SkipNotInPR {
		t.Fatalf("a remark on a file the PR does not change must be listed with SkipNotInPR, got %+v", second)
	}
	if m := c.Groups[1]; m.Kind != "mine" || len(m.Rows) != 1 || m.Rows[0].ID != mine || m.Rows[0].Kind != "note" {
		t.Fatalf("mine = %+v", m)
	}
	if r := c.Groups[2]; r.Kind != "replies" || len(r.Rows) != 1 || r.Rows[0].ID != d.ID || r.Rows[0].Kind != "reply" {
		t.Fatalf("replies = %+v", r)
	}
}

func TestPRSendCandidatesEmpty(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	c, err := svc.PRSendCandidates(context.Background(), 7)
	if err != nil || len(c.Groups) != 0 {
		t.Fatalf("%+v %v", c, err)
	}
}
```

Append to `internal/cli/prsend_test.go`:

```go
func TestPRNotesJSONCarriesSeverityAndCode(t *testing.T) {
	dir, head, _ := sendPRRepo(t)
	id := addCLINote(t, dir, head, 5)
	out, _, code := runPR(t, dir, "notes", "7", "--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, `"code":["line 5 changed"]`) {
		t.Fatalf("no code excerpt for %s:\n%s", id, out)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/domain -run TestPRSendCandidates -count=1; go test ./internal/cli -run TestPRNotesJSONCarriesSeverityAndCode -count=1`
Expected: build failure in domain (`PRSendCandidates undefined`); the CLI test fails on the missing key.

- [ ] **Step 3: Implement** — `internal/domain/forge_send_candidates.go`:

```go
package domain

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// GroupReplies is the send panel's third group: the draft replies to
// GitHub threads (spec §5.1).
const GroupReplies = "replies"

// SendCandidates is everything the send panel lists for PR n (spec §5.1):
// every unsent local comment, grouped by source, each with what the
// planner would do with it today.
type SendCandidates struct {
	PR     int
	Head   string
	Groups []SendCandidateGroup
}

// SendCandidateGroup is one source: an AI review, the user's own notes, or
// the draft replies.
type SendCandidateGroup struct {
	ID      string // "review:<id>" | GroupMine | GroupReplies
	Kind    string // "review" | "mine" | "replies"
	Agent   string
	Title   string
	Created time.Time
	Slot    int
	Rows    []SendCandidate
}

// SendCandidate is one row: a note, a remark or a draft reply.
type SendCandidate struct {
	ID        string
	Kind      string // "note" | "remark" | "reply"
	Severity  string
	Path      string
	Range     [2]int
	Side      string
	Summary   string
	Rationale string
	Sync      model.SyncState
	Code      []string
	Skip      string
}

// PRSendCandidates lists PR n's send candidates: each AI review newest
// first, then "my notes", then the draft replies; rows by path then line.
// The PR's diff must be fetched (PRNotes says so otherwise). A row the
// planner would skip is listed with its reason (never hidden), so the
// panel can say why it cannot be ticked.
func (s *Service) PRSendCandidates(ctx context.Context, n int) (SendCandidates, error) {
	byPath, err := s.PRNotes(ctx, n)
	if err != nil {
		return SendCandidates{}, err
	}
	pr, err := s.PullRequest(ctx, n)
	if err != nil {
		return SendCandidates{}, err
	}
	prev, err := s.PRPreview(ctx, pr)
	if err != nil {
		return SendCandidates{}, err
	}
	out := SendCandidates{PR: n, Head: prev.Set.Tip}
	pl, err := s.sendPlacer(ctx, prev.Set, prev.Files)
	if err != nil {
		return SendCandidates{}, err
	}
	groups := map[string]*SendCandidateGroup{}
	group := func(id, kind string) *SendCandidateGroup {
		if g, ok := groups[id]; ok {
			return g
		}
		g := &SendCandidateGroup{ID: id, Kind: kind, Slot: GroupSlot(id)}
		if kind == "replies" {
			g.Slot = 0
		}
		groups[id] = g
		return g
	}
	reviews := map[string]Review{}
	for _, p := range PreviewNotePaths(byPath) {
		for _, r := range byPath[p] {
			// Draft replies hang under forge threads.
			for _, rep := range r.Replies {
				if rep.Note.IsForgeReply() && rep.Sync != model.SyncSending && rep.Sync != model.SyncForge {
					row := s.candidateRow(ctx, rep, "reply", pl, prev.Set)
					g := group(GroupReplies, "replies")
					g.Rows = append(g.Rows, row)
				}
			}
			if r.Note.Source == model.NoteSourceForge || r.Note.IsForgeReply() || r.Sync == model.SyncSending || r.Sync == model.SyncForge {
				continue
			}
			kind := "note"
			if model.IsReviewNoteID(r.Note.ID) {
				kind = "remark"
			}
			gkind := "mine"
			if r.Group != GroupMine {
				gkind = "review"
			}
			g := group(r.Group, gkind)
			if gkind == "review" && g.Agent == "" {
				rid := strings.TrimPrefix(r.Group, "review:")
				rv, ok := reviews[rid]
				if !ok {
					rv, _ = s.Review(ctx, rid)
					reviews[rid] = rv
				}
				g.Agent, g.Created = rv.Agent, rv.Created
				if rv.Doc != nil {
					g.Title, _, _ = strings.Cut(strings.TrimSpace(rv.Doc.Summary), "\n")
				}
			}
			g.Rows = append(g.Rows, s.candidateRow(ctx, r, kind, pl, prev.Set))
		}
	}
	// Order: reviews newest first, mine, replies; rows by path then line.
	var revs []*SendCandidateGroup
	for id, g := range groups {
		if g.Kind == "review" {
			revs = append(revs, g)
		}
		_ = id
	}
	sort.Slice(revs, func(i, j int) bool { return revs[i].Created.After(revs[j].Created) })
	for _, g := range revs {
		out.Groups = append(out.Groups, *g)
	}
	if g, ok := groups[GroupMine]; ok {
		out.Groups = append(out.Groups, *g)
	}
	if g, ok := groups[GroupReplies]; ok {
		out.Groups = append(out.Groups, *g)
	}
	for i := range out.Groups {
		rows := out.Groups[i].Rows
		sort.SliceStable(rows, func(a, b int) bool {
			if rows[a].Path != rows[b].Path {
				return rows[a].Path < rows[b].Path
			}
			return rows[a].Range[0] < rows[b].Range[0]
		})
	}
	return out, nil
}

// candidateRow builds one row: its skip reason is what the planner would
// say today (noteItem / remarkItem against a scratch plan), its code the
// lines at the tip (the base for an old-side note), at most four.
func (s *Service) candidateRow(ctx context.Context, r ResolvedNote, kind string, pl *sendPlace, set PreviewNoteSet) SendCandidate {
	n := r.Note
	row := SendCandidate{ID: n.ID, Kind: kind, Path: n.Address.Path, Range: r.Range, Side: string(n.Side),
		Summary: n.Summary, Rationale: n.Rationale, Sync: r.Sync, Severity: severityOf(n.Tags)}
	if kind != "reply" {
		var scratch engine.SendPlan
		if kind == "remark" {
			rid, i, _ := model.ParseReviewNoteID(n.ID)
			if rv, err := s.Review(ctx, rid); err == nil {
				s.remarkItem(ctx, &scratch, rv, i, pl)
			}
		} else {
			noteItem(&scratch, r, pl)
		}
		if len(scratch.Skipped) > 0 {
			row.Skip = scratch.Skipped[0].Reason
		}
	}
	if r.Range[0] > 0 {
		lines := pl.lines(n.Side == model.NoteSideOld, n.Address.Path)
		lo, hi := r.Range[0]-1, min(r.Range[1], r.Range[0]+3)
		if lo >= 0 && hi <= len(lines) && lo < hi {
			row.Code = append([]string(nil), lines[lo:hi]...)
		}
	}
	return row
}

// severityOf reads the "severity: <v>" tag a remark's meta folds into.
func severityOf(tags []string) string {
	for _, t := range tags {
		if v, ok := strings.CutPrefix(t, "severity: "); ok {
			return v
		}
	}
	return ""
}
```

Check `remarkItem`'s signature is `(ctx, plan *engine.SendPlan, r Review, i int, pl *sendPlace)` (it is at `forge_send.go:499`) and that `noteItem` is `(plan *engine.SendPlan, r ResolvedNote, pl *sendPlace)`; `pl.lines(old bool, path string) []string` exists on `sendPlace`.

`notewire.go`: add to `WireNote` after `GroupSlot`:

```go
	// Severity is a remark's meta "severity"; Code the ≤ 4 lines the note
	// is about, at the PR's tip (gg pr notes --json only).
	Severity string   `json:"severity,omitempty"`
	Code     []string `json:"code,omitempty"`
```

and in `ToWireNote` set `w.Severity = severityOf(r.Note.Tags)` beside the `Group` assignment.

`prNotes` in `cli/prsend.go`, before the loop: `codes := map[string][]string{}` filled from `svc.PRSendCandidates(ctx, n)` (ignore its error: a candidates failure must not break `gg pr notes`) by `codes[row.ID] = row.Code` over every group's rows; inside the `--json` branch after `w := domain.ToWireNotePreview(r, true)`: `w.Code = codes[w.ID]`.

- [ ] **Step 4: Run the packages**

Run: `go test ./internal/domain ./internal/cli -count=1 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/domain internal/cli
printf '%s\n' "feat(domain,cli): PRSendCandidates — the send panel's rows; gg pr notes --json gains severity and code" "" "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" "Claude-Session: https://claude.ai/code/session_01JeY1FfbBboTS6VJ9dgXv3U" > /tmp/m && git commit -q -F /tmp/m
```

---

### Task 10: CLI — `gg pr send --note … [--verdict] [--body | --body-from]`

**Files:**
- Modify: `internal/cli/pr.go:15-26` (usage), `internal/cli/prsend.go:46-90` (`parsePRSend`)
- Test: `internal/cli/prsend_test.go`

**Interfaces:**
- Consumes: `PRSendRequest.BodyFrom` (Task 8), the `runPRAt` / `sendPRRepo` test harness, `forgetest.Writes`.
- Produces: the parse rule — exactly one of `--note`, `--review`, `--mine`,
  `--verdict` alone, `--finish`, `--discard`; `--note` may add `--verdict`
  and one of `--body` / `--body-from`; `--body-from` with anything but
  `--note` is a usage error.

- [ ] **Step 1: Write the failing tests** — append to `prsend_test.go`:

```go
func TestParsePRSendNoteWithVerdictAndBodyFrom(t *testing.T) {
	req, err := parsePRSend([]string{"7", "--note", "a", "--note", "b", "--verdict", "--body-from", "r1"}, io.Discard)
	if err != nil || req.PR != 7 || len(req.Notes) != 2 || !req.Verdict || req.BodyFrom != "r1" || req.BodySet {
		t.Fatalf("req %+v err %v", req, err)
	}
	for _, bad := range [][]string{
		{"7", "--body-from", "r1"},                       // body-from needs --note
		{"7", "--review", "r1", "--body-from", "r1"},     // not with --review
		{"7", "--note", "a", "--body", "x", "--body-from", "r1"}, // one body source
		{"7", "--mine", "--verdict"},                     // verdict rides --note only
	} {
		if _, err := parsePRSend(bad, io.Discard); err == nil {
			t.Errorf("%v: want a usage error", bad)
		}
	}
	// --verdict alone is still the verdict-only send.
	if req, err := parsePRSend([]string{"7", "--verdict"}, io.Discard); err != nil || !req.Verdict || len(req.Notes) != 0 {
		t.Fatalf("verdict alone: %+v %v", req, err)
	}
}

// The spec's end-to-end send (§5.5): a remark, a draft reply, a verdict and
// the review's summary as the body — one review, then one reply.
func TestPRSendMixedAnsweredAtATerminal(t *testing.T) {
	dir, head, fixtures := sendPRRepo(t)
	// A thread on big.go:5 to reply to, seeded into the PR snapshot.
	thread := `{"id":"PRRT_1","path":"big.go","line":5,"startLine":null,"originalLine":5,"originalStartLine":null,"diffSide":"RIGHT",
"subjectType":"LINE","isResolved":false,"isOutdated":false,"comments":{"pageInfo":{"hasNextPage":false},"nodes":[{"id":"PRRC_1",
"replyTo":null,"author":{"login":"carol"},"body":"please","diffHunk":"","createdAt":"2026-10-07T10:00:00Z","updatedAt":"2026-10-07T10:00:00Z",
"pullRequestReview":{"id":"PRR_1"}}]}}`
	reseedSnapshot(t, fixtures, head, thread)
	rid := saveCLIReview(t, dir, `{"version":1,"summary":"Looks fine","files":[{"path":"big.go","annotations":[{"newRange":[5,5],"summary":"check"}]}]}`)
	out, errs, code := runPR(t, dir, "reply", "7", "PRRC_1", "done")
	if code != 0 {
		t.Fatalf("reply: %s %s", out, errs)
	}
	draft := strings.Fields(out)[0] // "<id> draft reply to <thread>"
	out, errs, code = runPRAt(t, dir, "comment\n", "send", "7", "--note", "review:"+rid+":0", "--note", draft, "--verdict", "--body-from", rid)
	if code != 0 {
		t.Fatalf("exit %d: %s\n%s", code, errs, out)
	}
	var ops []string
	for _, w := range forgetest.Writes(t, fixtures) {
		ops = append(ops, w.Op)
	}
	if strings.Join(ops, ",") != "StartReview,AddThread,SubmitReview,Reply" {
		t.Fatalf("writes = %v", ops)
	}
	if !strings.Contains(out, "sent 1 comments to o/r #7") || !strings.Contains(out, "1 replies") {
		t.Fatalf("stdout = %q", out)
	}
}
```

Two helpers beside `sendPRRepo`:

```go
// reseedSnapshot rewrites the fake gh's PR snapshot with the given review
// threads (the same JSON sendPRRepo seeds, threads interpolated).
func reseedSnapshot(t *testing.T, fixtures, head, threads string) {
	t.Helper()
	snap := fmt.Sprintf(`{"data":{"repository":{"pullRequest":{"id":"PR_7","number":7,"title":"t","body":"","author":{"login":"ann"},
"state":"OPEN","isDraft":false,"reviewDecision":"","headRefName":"feat","isCrossRepository":false,"headRepositoryOwner":{"login":"ann"},
"headRepository":{"name":"r"},"baseRefName":"main","baseRefOid":"","headRefOid":"%s","url":"https://github.com/o/r/pull/7",
"createdAt":"2026-10-01T10:00:00Z","updatedAt":"2026-10-06T10:00:00Z","viewerDidAuthor":false,"viewerLatestReview":null,
"reviewThreads":{"pageInfo":{"hasNextPage":false},"nodes":[%s]},"comments":{"pageInfo":{"hasNextPage":false},"nodes":[]},
"reviews":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}}}`, head, threads)
	forgetest.Seed(t, fixtures, map[string]string{"snapshot-7.json": snap})
}

// saveCLIReview stores doc as PR #7's review through the CLI and returns its id.
func saveCLIReview(t *testing.T, dir, doc string) string {
	t.Helper()
	code, link, errb := runCLI(t, dir, "link", "--pr", "7")
	if code != 0 {
		t.Fatalf("link --pr: %s", errb)
	}
	code, out, errb := runCLIStdin(t, dir, doc, "review", "save", strings.TrimSpace(link), "--agent", "c", "--stdin", "--json")
	if code != 0 {
		t.Fatalf("review save: %s", errb)
	}
	var got struct{ ID string `json:"id"` }
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.ID == "" {
		t.Fatalf("review save json %q: %v", out, err)
	}
	return got.ID
}
```

(`gg link --pr <n>` is the PR link form gg-review.md documents; if
`cmdLink` spells the flag differently, `grep -n '"pr"' internal/cli/link.go`.)

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/cli -run 'TestParsePRSendNoteWithVerdictAndBodyFrom|TestPRSendMixedAnsweredAtATerminal' -count=1`
Expected: FAIL — `flag provided but not defined: -body-from`.

- [ ] **Step 3: Implement** — `parsePRSend`:

```go
	bodyFrom := fs.String("body-from", "", "with --note: the review whose summary is the body")
	…
	hasNotes := len(notes) > 0
	kinds := 0
	for _, on := range []bool{hasNotes, *review != "", *mine, *verdict && !hasNotes, *finish, *discard} {
		if on {
			kinds++
		}
	}
	if kinds != 1 {
		return domain.PRSendRequest{}, errors.New("name exactly one of --note, --review, --mine, --verdict, --finish, --discard (--note may add --verdict)")
	}
	if *verdict && !hasNotes && (*review != "" || *mine || *finish || *discard) {
		return domain.PRSendRequest{}, errors.New("--verdict rides --note, or stands alone")
	}
	bodySet := false
	fs.Visit(func(f *flag.Flag) { bodySet = bodySet || f.Name == "body" })
	if *bodyFrom != "" && (!hasNotes || bodySet) {
		return domain.PRSendRequest{}, errors.New("--body-from goes with --note and replaces --body")
	}
	req := domain.PRSendRequest{PR: n, Review: *review, Mine: *mine, Notes: notes, Verdict: *verdict,
		Body: *body, BodySet: bodySet, BodyFrom: *bodyFrom, Finish: *finish, Discard: *discard}
	return req, nil
```

(replacing the old `kinds` loop, the `req :=` literal and the `fs.Visit` that set `BodySet`). Usage line in `pr.go`:

```
       gg pr send <n> --note <id>… [--verdict] [--body <text> | --body-from <review>]
       gg pr send <n> (--review <id> | --mine | --verdict) [--body <text>]
       gg pr send <n> --finish | --discard
```

- [ ] **Step 4: Run the CLI package**

Run: `go test ./internal/cli -count=1 2>&1 | tail -3`
Expected: PASS (`TestPRSendUsage` may pin the old usage text — update its expectation to the new lines).

- [ ] **Step 5: Commit**

```bash
gg add internal/cli
printf '%s\n' "feat(cli): gg pr send --note takes --verdict and --body-from; mixed send test against the fake gh" "" "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" "Claude-Session: https://claude.ai/code/session_01JeY1FfbBboTS6VJ9dgXv3U" > /tmp/m && git commit -q -F /tmp/m
```

---

### Task 11: The note link — `?note=<id>` (model, domain)

**Files:**
- Modify: `internal/model/link.go:129-132` (hint kinds), `internal/domain/linkresolve.go:137-143` (the hint check), `internal/domain/linkdesc.go:131` (describe)
- Create: `internal/domain/note_link.go`
- Test: `internal/model/link_test.go`, `internal/domain/note_link_test.go`

**Interfaces:**
- Consumes: `linkTextIn(repo, model.Link)`, `Service.LinkRepo`, `linkSideLines`, `model.LineFingerprint`, `model.BlockFingerprint`, `storedNotes`, `ReviewRemarkLink`.
- Produces:

```go
// model
const NoteHintKind = "note" // ?note=<id>: a stored note (a root or a reply)
// domain
var ErrNoteLinkGone = errors.New("note is not here")
func (s *Service) NoteLinkText(ctx context.Context, id string) (string, error) // a note → its ?note= link; a remark id → ReviewRemarkLink; a forge id → error
```

`ResolveLink` refuses a `?note=` link whose note the chosen checkout's
store does not hold (`model.ErrLink` wrapping `ErrNoteLinkGone`, message
`note <id> is not here`). `linkdesc` says `note: <id> <author> · <summary>`.

- [ ] **Step 1: Write the failing tests** — append to `internal/model/link_test.go`:

```go
func TestLinkNoteHintRoundTrips(t *testing.T) {
	l, err := ParseLink("gg://repo/a.go:12?note=3091b73a")
	if err != nil || l.Hint.Kind != NoteHintKind || l.Hint.ID != "3091b73a" || l.Line != 12 {
		t.Fatalf("%+v %v", l, err)
	}
	if got := l.String(); got != "gg://repo/a.go:12?note=3091b73a" {
		t.Fatalf("String = %q", got)
	}
}
```

Create `internal/domain/note_link_test.go`:

```go
package domain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestNoteLinkTextCommittedNote(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	id := addPRNote(t, svc, head, "big.go", 5, "here")
	link, err := svc.NoteLinkText(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(link, "@"+head) || !strings.HasSuffix(link, "/big.go:5?note="+id) {
		t.Fatalf("link = %q", link)
	}
	// A reply links its own id at the thread's anchor.
	rep, err := svc.NoteReply(context.Background(), id, model.Note{Source: model.NoteSourceUser, Author: "me", Summary: "and"})
	if err != nil {
		t.Fatal(err)
	}
	if rl, err := svc.NoteLinkText(context.Background(), rep.ID); err != nil || !strings.HasSuffix(rl, "/big.go:5?note="+rep.ID) {
		t.Fatalf("reply link = %q, %v", rl, err)
	}
}

func TestNoteLinkTextWorkingNoteCarriesAFingerprint(t *testing.T) {
	t.Parallel()
	dir, _ := newRealRepo(t)
	commitFile(t, dir, "w.txt", "one\ntwo\n", "w")
	writeFile(t, dir, "w.txt", "one\ntwo changed\n") // uncommitted (conflict_test.go's helper)
	_, svc := newRealRepoAt(t, dir)
	svc.UseNotesDir(t.TempDir())
	n, err := svc.NoteAdd(context.Background(), model.Note{Source: model.NoteSourceUser, Summary: "w",
		Address: model.FileAddress{State: model.StateUnstaged, Worktree: dir, Path: "w.txt"}, Side: model.NoteSideNew, Range: [2]int{2, 2}})
	if err != nil {
		t.Fatal(err)
	}
	link, err := svc.NoteLinkText(context.Background(), n.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := ":2~" + model.LineFingerprint("two changed") + "?note=" + n.ID
	if !strings.HasSuffix(link, want) {
		t.Fatalf("link = %q, want suffix %q", link, want)
	}
}

func TestNoteLinkTextRemarkAndForge(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	rid := savePRReview(t, svc, twoRemarks)
	link, err := svc.NoteLinkText(context.Background(), "review:"+rid+":0")
	if err != nil || !strings.Contains(link, "?review="+rid) {
		t.Fatalf("remark: %q %v", link, err)
	}
	if _, err := svc.NoteLinkText(context.Background(), "forge:C1"); err == nil {
		t.Fatal("a forge comment has no local link")
	}
}

// Review Focus 2: a link to a deleted note refuses to open.
func TestResolveNoteLinkOfAGoneNote(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	id := addPRNote(t, svc, head, "big.go", 5, "here")
	link, err := svc.NoteLinkText(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	l, err := model.ParseLink(link)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveLink(context.Background(), l, ResolveOpts{Cwd: svc}); err != nil {
		t.Fatalf("a live note must resolve: %v", err)
	}
	if err := svc.NoteRemove(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	_, err = ResolveLink(context.Background(), l, ResolveOpts{Cwd: svc})
	if !errors.Is(err, ErrNoteLinkGone) || !errors.Is(err, model.ErrLink) || !strings.Contains(err.Error(), "note "+id+" is not here") {
		t.Fatalf("err = %v", err)
	}
	if desc := svc.DescribeLink(context.Background(), l); !strings.Contains(desc, "note") {
		t.Fatalf("DescribeLink = %q, want the note named", desc)
	}
}
```

(`DescribeLink(ctx, l) string` is linkdesc.go's exported entry; the
"note: <id> <author> · <summary>" arm feeds it through `linkDescFields`.
Run the description BEFORE the removal if the gone case describes nothing.)

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/model -run TestLinkNoteHintRoundTrips -count=1; go test ./internal/domain -run 'TestNoteLinkText|TestResolveNoteLinkOfAGoneNote' -count=1`
Expected: model FAIL (`bad gg link`: unknown hint kind); domain build failure.

- [ ] **Step 3: Implement**

`model/link.go`: add `"note": true` to `linkHintKinds`, the constant
`NoteHintKind = "note"` beside `ReviewHintKind`, and a paragraph in the
hint doc comment: *"note" names a STORED NOTE (a root or a reply) by id;
the address is the note's own anchor, so every hint-blind verb lands on
the line; the consumer opens the thread. Machine-local like "review".*

`domain/note_link.go`:

```go
package domain

import (
	"context"
	"errors"
	"fmt"

	"github.com/homeend/gigagit/internal/model"
)

// ErrNoteLinkGone: a ?note= link whose note this store no longer holds.
var ErrNoteLinkGone = errors.New("note is not here")

// NoteLinkText is note id's gg:// link (spec §7.1): the note's own anchor
// — its commit, index or working-tree target, path and first line, with
// the line's fingerprint when uncommitted — carrying ?note=<id>. A reply
// links its own id at its thread's anchor. A review remark id yields the
// remark's review link (one row serves both); a GitHub comment has none.
func (s *Service) NoteLinkText(ctx context.Context, id string) (string, error) {
	switch {
	case model.IsReviewNoteID(id):
		return s.ReviewRemarkLink(ctx, id)
	case model.IsForgeNoteID(id):
		return "", fmt.Errorf("%s is a GitHub comment: it has no local link", id)
	}
	byID, err := s.storedNotes(ctx)
	if err != nil {
		return "", err
	}
	n, ok := byID[id]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNoteLinkGone, id)
	}
	repo, err := s.LinkRepo(ctx)
	if err != nil {
		return "", err
	}
	l := model.Link{Repo: repo, Path: n.Address.Path, Side: n.Side, Line: n.Range[0],
		Hint: model.LinkHint{Kind: model.NoteHintKind, ID: n.ID}}
	if n.Range[1] > n.Range[0] {
		l.End = n.Range[1]
	}
	switch n.Address.State {
	case model.StateCommitted:
		l.Target = model.LinkTarget{State: model.StateCommitted, Commit: n.Address.Commit}
	case model.StateStaged:
		l.Target = model.LinkTarget{State: model.StateStaged}
	default:
		l.Target = model.LinkTarget{State: model.StateUnstaged}
	}
	if l.Side == "" {
		l.Side = model.NoteSideNew
	}
	if n.Address.State != model.StateCommitted && l.Line > 0 {
		if lines, ok := linkSideLines(ctx, s, l, n.Address.Path); ok && l.Line <= len(lines) {
			if l.End > l.Line && l.End <= len(lines) {
				l.Fingerprint = model.BlockFingerprint(lines[l.Line-1 : l.End])
			} else {
				l.Fingerprint = model.LineFingerprint(lines[l.Line-1])
			}
		}
	}
	return linkTextIn(repo, l)
}

// checkNoteHint refuses a ?note= link whose note the chosen checkout's
// store does not hold (a deleted note): the line would open, the thread
// the user meant would not be there.
func checkNoteHint(ctx context.Context, svc *Service, res Resolved) error {
	byID, err := svc.storedNotes(ctx)
	if err != nil {
		return nil // no store here: the address still opens, the hint is dropped with a notice
	}
	if _, ok := byID[res.Hint.ID]; !ok {
		return fmt.Errorf("%w: %w: note %s is not here", model.ErrLink, ErrNoteLinkGone, res.Hint.ID)
	}
	return nil
}
```

(`model.Link.End` and `.Fingerprint` are the range and fingerprint fields —
link.go:157-162.) In `ResolveLink` (linkresolve.go) after the review-hint block add:

```go
	if res.Hint.Kind == model.NoteHintKind {
		if err := checkNoteHint(ctx, reviewHintService(ctx, res.Checkout, opts), res); err != nil {
			return Resolved{}, err
		}
	}
```

`linkdesc.go`, a new case beside `model.ReviewHintKind`:

```go
	case model.NoteHintKind:
		if byID, err := s.storedNotes(ctx); err == nil {
			if n, ok := byID[l.Hint.ID]; ok {
				return "note", n.ID, strings.TrimSpace(n.Author + " · " + cutLabel(n.Summary))
			}
		}
```

- [ ] **Step 4: Run the packages**

Run: `go test ./internal/model ./internal/domain ./internal/linknav -count=1 2>&1 | tail -4`
Expected: PASS (`linknav` treats every hint as a place already: `Command` copies `res.Hint` through — no table to extend).

- [ ] **Step 5: Commit**

```bash
gg add internal/model internal/domain internal/linknav
printf '%s\n' "feat(model,domain): the note link — gg://…?note=<id>, built from the note's anchor, refused once the note is gone" "" "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" "Claude-Session: https://claude.ai/code/session_01JeY1FfbBboTS6VJ9dgXv3U" > /tmp/m && git commit -q -F /tmp/m
```

---

### Task 12: CLI — `gg note show`, a note link in place of an id, `gg link --note`

**Files:**
- Create: `internal/cli/note_show.go`
- Modify: `internal/cli/note.go:30-80` (dispatcher), `internal/cli/link.go` (flag), `internal/cli/review_show.go` (beside `linkReview`)
- Test: `internal/cli/note_show_test.go`

**Interfaces:**
- Consumes: `Service.NoteLinkText` (Task 11), `storedNotes` (through a new
  exported `Service.NoteThread`), `resolveLinkArg`, `openLinkTarget`, `printNote`.
- Produces: `gg note show <id|link> [--json]`; `gg note reply|resolve|unresolve <note-link> …`
  (the link stands for the id and names the checkout); `gg link --note <id>`;
  `domain.Service.NoteThread(ctx, id) (root model.Note, replies []model.Note, resolved *model.ThreadResolution, err error)`.

- [ ] **Step 1: Write the failing tests** — `internal/cli/note_show_test.go`:

```go
package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNoteShowByIDAndByLink(t *testing.T) {
	dir, head, _ := sendPRRepo(t)
	id := addCLINote(t, dir, head, 5)
	if code, _, errs := runCLI(t, dir, "note", "reply", id, "--summary", "and so"); code != 0 {
		t.Fatalf("reply: %s", errs)
	}
	code, out, errs := runCLI(t, dir, "note", "show", id)
	if code != 0 || !strings.Contains(out, "note "+id) || !strings.Contains(out, "big.go:5") || !strings.Contains(out, "and so") {
		t.Fatalf("exit %d out=%q err=%q", code, out, errs)
	}
	code, link, errs := runLinkCLI(t, dir, "--note", id)
	link = strings.TrimSpace(link)
	if code != 0 || !strings.Contains(link, "?note="+id) {
		t.Fatalf("gg link --note = %q (%s)", link, errs)
	}
	code, outJ, _ := runCLI(t, dir, "note", "show", "--json", link)
	if code != 0 {
		t.Fatalf("show by link: %d", code)
	}
	var got struct {
		Note    struct{ ID string `json:"id"` } `json:"note"`
		Replies []struct{ Summary string `json:"summary"` } `json:"replies"`
	}
	if err := json.Unmarshal([]byte(outJ), &got); err != nil || got.Note.ID != id || len(got.Replies) != 1 || got.Replies[0].Summary != "and so" {
		t.Fatalf("json %q: %v", outJ, err)
	}
	// reply and resolve by link
	if code, _, errs := runCLI(t, dir, "note", "reply", link, "--summary", "again"); code != 0 {
		t.Fatalf("reply by link: %s", errs)
	}
	if code, _, errs := runCLI(t, dir, "note", "resolve", link); code != 0 {
		t.Fatalf("resolve by link: %s", errs)
	}
	_, out, _ = runCLI(t, dir, "note", "show", id)
	if !strings.Contains(out, "resolved") || !strings.Contains(out, "again") {
		t.Fatalf("after reply+resolve:\n%s", out)
	}
}

func TestNoteShowGoneNote(t *testing.T) {
	dir, _, _ := sendPRRepo(t)
	code, _, errs := runCLI(t, dir, "note", "show", "deadbeef")
	if code == 0 || !strings.Contains(errs, "deadbeef") {
		t.Fatalf("exit %d: %s", code, errs)
	}
}
```

(`runCLI(t, dir, args...) (code, out, errb)` is `cli_test.go`'s runner,
`runLinkCLI` is `link_test.go`'s with the same shape; `sendPRRepo` and
`addCLINote` are `prsend_test.go`'s.)

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/cli -run 'TestNoteShow' -count=1`
Expected: FAIL — `note: unknown subcommand "show"`.

- [ ] **Step 3: Implement**

`internal/domain/note_link.go`, add:

```go
// NoteThread is note id's thread: the root (id itself, or its parent when
// id is a reply), the replies in creation order, and the thread's
// resolution (nil = open). ErrNoteLinkGone when nothing holds id.
func (s *Service) NoteThread(ctx context.Context, id string) (root model.Note, replies []model.Note, resolved *model.ThreadResolution, err error) {
	byID, err := s.storedNotes(ctx)
	if err != nil {
		return model.Note{}, nil, nil, err
	}
	n, ok := byID[id]
	if !ok {
		return model.Note{}, nil, nil, fmt.Errorf("%w: %s", ErrNoteLinkGone, id)
	}
	root = n
	if n.ParentID != "" {
		if p, ok := byID[n.ParentID]; ok {
			root = p
		}
	}
	for _, x := range byID {
		if x.ParentID == root.ID {
			replies = append(replies, x)
		}
	}
	sort.Slice(replies, func(i, j int) bool { return replies[i].Created.Before(replies[j].Created) })
	if st := s.notesStore(ctx); st != nil {
		if all, err := st.LoadAllResolved(); err == nil {
			for i := range all {
				if all[i].Root == root.ID {
					resolved = &all[i]
					break
				}
			}
		}
	}
	return root, replies, resolved, nil
}
```

(add `"sort"` to the imports). `internal/cli/note_show.go`:

```go
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

const noteShowUsage = "usage: gg note show [--json] <note-id|note-link>"

// noteShow prints one thread — what an agent handed a ?note= link reads.
func noteShow(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	asJSON := false
	var pos []string
	for _, a := range args {
		if a == "--json" || a == "-json" {
			asJSON = true
		} else {
			pos = append(pos, a)
		}
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, noteShowUsage)
		return 2
	}
	ctx := context.Background()
	id := pos[0]
	if isLinkArg(id) {
		res, err := resolveLinkArg(ctx, svc, id, linkShapes{Ref: true, Pair: true}, "note show")
		if err != nil {
			return linkExit("note show", err, stderr)
		}
		if res.Hint.Kind != model.NoteHintKind {
			fmt.Fprintln(stderr, "note show: the link names no note (a note link carries ?note=<id>)")
			return 2
		}
		svc, id = openLinkTarget(res), res.Hint.ID
	}
	root, replies, resolved, err := svc.NoteThread(ctx, id)
	if err != nil {
		return noteIDExit("note show", id, svc, err, stderr)
	}
	link, _ := svc.NoteLinkText(ctx, root.ID)
	if asJSON {
		return jsonOut(stdout, stderr, map[string]any{"note": root, "replies": replies, "resolved": resolved, "link": link})
	}
	state := "open"
	if resolved != nil {
		state = "resolved"
	}
	where := root.Address.Path
	if root.Range[0] > 0 {
		where += fmt.Sprintf(":%d", root.Range[0])
		if root.Range[1] > root.Range[0] {
			where += fmt.Sprintf("-%d", root.Range[1])
		}
	}
	fmt.Fprintf(stdout, "note %s · %s · %s · %s\n", root.ID, root.Author, where, state)
	fmt.Fprintln(stdout, root.Summary)
	if t := strings.TrimSpace(root.Rationale); t != "" {
		fmt.Fprintln(stdout, "    "+strings.ReplaceAll(t, "\n", "\n    "))
	}
	if link != "" {
		fmt.Fprintln(stdout, "    "+link)
	}
	for _, r := range replies {
		fmt.Fprintf(stdout, "  ↳ %s (%s): %s\n", r.Author, r.ID, r.Summary)
		if t := strings.TrimSpace(r.Rationale); t != "" {
			fmt.Fprintln(stdout, "      "+strings.ReplaceAll(t, "\n", "\n      "))
		}
	}
	return 0
}
```

(`jsonOut` exists in the package — `prNotes` uses it; `linkShapes`,
`resolveLinkArg`, `linkExit`, `openLinkTarget`, `noteIDExit` are the
dispatcher's own helpers.) In `cmdNote`, before the generic link peel:

```go
	if sub == "show" {
		return noteShow(svc, rest, stdout, stderr)
	}
	// A NOTE link (?note=<id>) in place of an id: for reply / resolve /
	// unresolve it names the thread and its checkout — the id replaces it.
	if len(rest) > 0 && isLinkArg(rest[0]) && (sub == "reply" || sub == "resolve" || sub == "unresolve") {
		if l, err := model.ParseLink(rest[0]); err == nil && l.Hint.Kind == model.NoteHintKind {
			res, err := resolveLinkArg(context.Background(), svc, rest[0], linkShapes{Ref: true, Pair: true}, "note "+sub)
			if err != nil {
				return linkExit("note "+sub, err, stderr)
			}
			svc = openLinkTarget(res)
			rest = append([]string{res.Hint.ID}, rest[1:]...)
		}
	}
```

and `show` in the usage line. `gg link --note <id>`: in `cmdLink` add
`note := fs.String("note", "", "a stored NOTE's link: --note <id>")`,
refuse it combined with a path/target like `--review` is refused, and
dispatch to

```go
// linkNote prints note id's link (gg link --note).
func linkNote(svc *domain.Service, id string, stdout, stderr io.Writer) int {
	ctx := context.Background()
	text, err := svc.NoteLinkText(ctx, id)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	svc.RecordCopiedLink(ctx, text)
	fmt.Fprintln(stdout, text)
	return 0
}
```

Update `linkUsage` with `       gg link --note <id>  (a stored note's link)`.

- [ ] **Step 4: Run the CLI package**

Run: `go test ./internal/cli -count=1 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/cli internal/domain
printf '%s\n' "feat(cli): gg note show; a note link in place of an id for reply/resolve; gg link --note" "" "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" "Claude-Session: https://claude.ai/code/session_01JeY1FfbBboTS6VJ9dgXv3U" > /tmp/m && git commit -q -F /tmp/m
```

---

### Task 13: Skills, versions, docs

**Files:**
- Modify: `internal/agentskill/gg-review.md`, `gg-cross-review.md`, `gg-overview.md`, `reviewing-with-gg.md`, `using-gg.md`, `agentskill.go:36-55`
- Modify: `CHANGELOG.md`, `README.md` (the PR section and the review section), `docs/CLAUDE-details.md`
- Test: `internal/agentskill/agentskill_test.go` (the version markers follow the constants — no new test), `go test ./internal/agentskill`

**Interfaces:**
- Consumes: everything above; the CLI flags and keys as named in Tasks 5, 10, 12.
- Produces: the skill text agents follow (spec §2.4, §7.3, §6).

- [ ] **Step 1: gg-review.md** — insert a step between "Write the review document" (3) and "Store it" (4), renumbering the rest:

```markdown
4. **Overview (when asked).** Only when the user's request says
   "overview", "tour" or "walk me through" (or `/gg-review <link> overview
   …`): add an `"overview"` string to the document — the gg-overview kind,
   stored with the review: first line the result in one sentence, then
   each point with its anchor `[the parser](internal/x/parse.go:40-60)`,
   `[this check](src/a.go:120)`, in the order you would explain it, one
   `path:N` or `path:N-M` per place (lines of the NEW version at the
   reviewed tip, paths repo-relative in slash form), no `note:` anchors,
   ≤ 100 anchors, ≤ 64 KiB; last the open questions. The summary stays the
   review — the verdict and the findings go there; the overview only walks.
   Otherwise leave the key out.
```

and in "Store it": *`gg review save` prints `unresolved: <dest>` for every
anchor whose file is not in the reviewed change or whose lines do not
exist at the tip — fix the text and save again.*

- [ ] **Step 2: gg-cross-review.md** — the same step before "Store it"
  (step 7 → the overview is written by YOU on the merged document, never by
  the reviewers), renumber 7–9 to 8–10.

- [ ] **Step 3: gg-overview.md** — after the "Notes and overviews are
  TEMPORARY" paragraph add: *A review can carry an overview of its own:
  `/gg-review <link> overview …` writes the same document into the review's
  `"overview"` field (reviewing-with-gg's "Review document"), and gg keeps it
  with the review, anchored at the reviewed change.*

- [ ] **Step 4: reviewing-with-gg.md** — in "Review document" add after the
  JSON block's explanation of `"summary"`:

```markdown
`"overview"` (optional) is a stored overview — the gg-overview document
kind, markdown whose links are anchors into the reviewed change: `path`,
`path:N`, `path:N-M` (lines of the new version at the reviewed tip, paths
repo-relative in slash form; no `note:` anchors), ≤ 100 anchors, ≤ 64 KiB.
It is local only — never part of what gg sends to GitHub — and gg shows it
as its own "≡ Overview" row in the review view, walked with tab and enter
like a temporary overview. Write one only when the user asked for an
overview / tour / walk-through. `gg review save` prints `unresolved:
<dest>` for an anchor that names no file of the reviewed change or lines
past its end; `gg review show` prints the overview after the summary and
its JSON carries it as `"overview"` (the review's text is `"summary"`).
```

In "Checking another agent's review" add a paragraph:

```markdown
A pasted NOTE link (`gg://…?note=<id>`) is one thread, not a review:
`gg note show <link>` prints it — the root, its replies with their ids,
whether it is resolved — and `gg note reply <link> --summary "…"` /
`gg note resolve <link>` answer it (the link stands for the id and names
the checkout). A line link with no `?note=` names a line, not a thread:
`gg note list --file <path>` shows what hangs there.
```

- [ ] **Step 5: using-gg.md** — in the `gg pr` section replace the `gg pr
  send` line(s) with the new usage (Task 10's three lines) and add: *`--note`
  may mix review remarks, your notes and draft replies: one GitHub review,
  then the replies, under one confirm; `--verdict` makes the confirm offer
  comment / approve / request changes; `--body-from <review>` posts that
  review's summary as the body (`--body <text>` a text of your own).* In
  `gg pr notes --json` add `severity` and `code` to the field list. Under
  "Review notes" document `gg note show <id|link> [--json]` and that
  reply/resolve/unresolve accept a note link. Under "gg links" add
  `gg link --note <id>` and the `?note=<id>` hint. Under the `gg review
  show` entry: *prints the overview after the summary; JSON `"summary"` is
  the review's text, `"overview"` the stored overview.*

- [ ] **Step 6: versions** — `agentskill.go`: `Version = 161`,
  `ReviewVersion = 17`, `GGReviewVersion = 4`, `GGCrossReviewVersion = 5`,
  `GGOverviewVersion = 2`. The `<!-- gg:<skill>:vN -->` markers are rendered
  from these constants (`agentskill.go`), not written in the .md files —
  nothing else to edit.

Run: `go test ./internal/agentskill ./internal/agentinit ./internal/cli -count=1 2>&1 | tail -3`
Expected: PASS. Then from the worktree: `go run ./cmd/gg init --update` and
check the installed copies under `~/.claude/skills/` carry the new markers.

- [ ] **Step 7: CHANGELOG / README / CLAUDE-details**

`CHANGELOG.md`: a new top section `## PR review, plan 1: the stored
overview, PR reviews, mixed sends, note links (core)` with `### Added`
bullets for: the `"overview"` document key and `gg review save`'s
`unresolved:` report, `gg review show`'s overview + the `"summary"` key
rename (note it as a wire change for agents), `PreviewReviews` for PRs,
`gg pr send --note … --verdict --body-from` and replies after the review
under one confirm, `gg pr notes --json` `severity`/`code`, the note link
(`gg link --note`, `gg note show`, links in place of ids), the settle rule
for reviews with an overview. `### Changed`: `ReviewDoc.Summary`.

`README.md`: in the pull-request section, the new `gg pr send` form; in
the review section, one paragraph on the stored overview (what it is, who
writes it, that it never goes to GitHub) and `gg note show` / `gg link
--note`. The TUI/web surfaces (the rows, the panel, the menus) are plans 2
and 3 — do not describe them yet.

`docs/CLAUDE-details.md`: a short block under the review notes / forge
send material: the `"overview"` key and `ReviewOverview`, `SendPlan.Then`
and `runActions`, `PRSendCandidates`, the `?note=` hint and
`checkNoteHint`, the settle rule.

- [ ] **Step 8: Full gate and commit**

Run: `./test.sh race 2>&1 | tail -3` — expected `all green`.

```bash
gg add internal/agentskill CHANGELOG.md README.md docs/CLAUDE-details.md
printf '%s\n' "docs(skills): the stored overview step, note links, gg pr send flags; skill versions bumped" "" "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" "Claude-Session: https://claude.ai/code/session_01JeY1FfbBboTS6VJ9dgXv3U" > /tmp/m && git commit -q -F /tmp/m
```

Then the whole-branch review (a read-only reviewer subagent on the most
capable model, per CLAUDE.md), fix what it finds, and hand the branch to
the user for the merge. Plans 2 (TUI) and 3 (web) are written after this
plan is merged, against the names in the Interfaces blocks above.
