# Preview reviews — plan 1: domain, `gg review save`, `/gg-review`

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans — this repo forbids implementer subagents (CLAUDE.md); the session that wrote this plan executes it task by task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A review of a merge preview or commit pair is stored as the preview's own review (tagged, never a commit's), an interactive agent can store a full review document for any change link with `gg review save`, and a `/gg-review <link> [focus]` skill drives that end to end.

**Architecture:** `domain.ReviewTarget` gains `Preview` (the scope name); `SaveReview` copies it onto `Note.Preview`, which the note store already routes (`previews.toml` for a merge preview, `commits.toml` for a pair). Reads split: `NoteCounts.Reviews` stays commit-facing (untagged only) and a new `NoteCounts.PreviewReviews` holds the tagged heads by scope; `PreviewReviews(ctx, set)` classifies them current / older / gone. `domain.LinkReviewTarget` maps a resolved `gg://` link onto a target; the CLI verb and the skill sit on top.

**Tech Stack:** Go 1.26, real `git` in `t.TempDir()` tests, go:embed skills.

**Spec:** `docs/superpowers/specs/2026-10-07-preview-reviews-design.md` (§3, §4, §5).

**Worktree:** `/work/gigagit/.claude/worktrees/preview-reviews`, branch `feat/preview-reviews`. Every command runs there (`cd` with an absolute path; the shell cwd resets).

## Global Constraints

- A git verb is one invocation; frontends reach git through `domain` only (`internal/cli` never imports `internal/git`).
- `ReviewTarget.Range` stays pure hex — it is spliced unquoted into tool commands. `Label` and `Preview` are data, never executed.
- Engine/CLI prose stays English; no TUI strings in this plan.
- Tests: real git in temp dirs, `t.Parallel()` on every new test.
- Run tests with `go test ./internal/<pkg>/ -run <Name> -count=1`; the full gate is `./test.sh race` and is green only when its log says "all green".
- Commit with `git add <exact files>` + `git commit -F <msgfile>` (gg commit has no `-F`); never `git add -A`.
- Every commit message ends with:
  ```
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_012Pcaqj7TuQ3TLh8geV6H9q
  ```

## Deviations from the spec (decided while planning — tell the user)

1. **§3.3 branch rename is dropped.** Saved merge previews and a preview's line notes keep the branch NAMES they were made with and do not follow a rename (only `PreviewBranch`/`Address.Branch` do). Rewriting a review's `Preview` alone would detach it from its own row and its sibling notes. A preview review follows the same names as the rest of its preview.
2. **§4 "another repository — refused" becomes "acts on the link's checkout"**, like every other link verb (`openLinkTarget` in `internal/cli/linkconsume.go`): the review lands in the store of the repository the link names.
3. **§4 adds `--dry-run`.** A `@ref:` link means "the tip's own change" to `gg diff` (ruling R4) but "the branch vs the trunk" to a review. So the agent must not guess what to read: `gg review save <link> --dry-run` prints the target's label, kind and the exact `gg diff` argument that shows the reviewed change. The skill reads with that argument.
4. **§5 Codex:** `gg-review` installs everywhere `delegate` installs (including Codex's AGENTS.md block), following the `/delegate` precedent instead of adding a per-mode skill filter. The body works as plain instructions too.
5. **§3.2 batching:** "do the commits still exist" costs no git call when the reviewed tip is still in the preview's commit list; only a moved/rewritten tip pays one `ResolveRev` per commit. No `cat-file --batch-check` plumbing.

## Review Focus

1. A reply or resolve on a remark of a preview review (stored in `previews.toml`) must attach to the review — `Review(id)` shows it, `gg review show` prints it. → Task 1 test `TestPreviewReviewRemarkReplyAttaches`.
2. A preview review must not inflate the preview's ◆N line-note badge (`PreviewNoteCounts`). → Task 1 test `TestPreviewReviewNotCountedAsPreviewNote`.
3. Reviews stored by the old `gg review --preview` (no tag) keep marking their commit — no regression. → Task 1 test `TestUntaggedReviewStaysOnItsCommit`.
4. Branch names with a slash (`feat/x`) in the tag and the matching. → Task 2 uses `feat/x` throughout.
5. A file or line link handed to `gg review save` reviews the WHOLE change, not the file. → Task 4 test `TestReviewSaveFileLinkReviewsWholeChange`.

---

### Task 1: Tag preview reviews and keep them off commits

**Files:**
- Modify: `internal/domain/review.go` (ReviewTarget field, `ScopeReviewTarget`)
- Modify: `internal/domain/review_notes.go` (`putReview`, `Review`/`ReviewHead` fields, `reviewOf`, `reviewNotes`, `ReviewsForCommit`)
- Modify: `internal/domain/notes.go` (`NoteCounts.PreviewReviews`, the review arm of `NoteCounts`)
- Modify: `internal/domain/previewnotes.go` (`loadPreviewNotes` skips review notes)
- Modify: `internal/cli/review.go` (the `pf.set()` arm uses `ScopeReviewTarget`)
- Test: `internal/domain/preview_review_test.go` (new)

**Interfaces:**
- Produces:
  - `ReviewTarget.Preview string`
  - `func ScopeReviewTarget(set PreviewNoteSet) ReviewTarget`
  - `Review.Preview string`; `ReviewHead.Preview, ReviewHead.Scope string`; `ReviewHead.Older bool` (set by Task 2)
  - `NoteCounts.PreviewReviews map[string][]ReviewHead` — key = scope name (`"main...feat/x"`, `"abc1234..def5678"`), newest first

- [ ] **Step 1: Write the failing tests** — `internal/domain/preview_review_test.go`:

```go
package domain

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

const previewReviewDoc = `{"version":1,"summary":"## Summary\nok","files":[{"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"one"}]}]}`

// previewReviewRepo: main + branch feat/x one commit ahead, its own note store.
func previewReviewRepo(t *testing.T) (string, *Service, PreviewNoteSet) {
	t.Helper()
	dir, svc := newRealRepo(t)
	svc.UseNotesDir(t.TempDir())
	runGitIn(t, dir, "branch", "-M", "main")
	runGitIn(t, dir, "checkout", "-b", "feat/x")
	commitFile(t, dir, "f.txt", "x\n", "feature commit")
	set, err := svc.PreviewNotes(context.Background(), "feat/x", "main")
	if err != nil || !set.OK() {
		t.Fatalf("PreviewNotes: %+v %v", set, err)
	}
	return dir, svc, set
}

func savePreviewReview(t *testing.T, svc *Service, set PreviewNoteSet) string {
	t.Helper()
	id, _, err := svc.SaveReview(context.Background(), SaveReview{Target: ScopeReviewTarget(set), Agent: "Claude Code", Text: previewReviewDoc})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestScopeReviewTargetPreviewAndPair(t *testing.T) {
	t.Parallel()
	dir, svc, set := previewReviewRepo(t)
	tg := ScopeReviewTarget(set)
	if tg.Kind != ReviewRange || tg.Range != set.Base+".."+set.Tip || tg.Diff.Rev != tg.Range ||
		tg.Commit != set.Tip || tg.Preview != "main...feat/x" || tg.Label != "main ... feat/x" {
		t.Fatalf("preview target = %+v", tg)
	}
	base := revParse(t, dir, "main")
	pair, err := svc.PairNotes(context.Background(), base, set.Tip)
	if err != nil {
		t.Fatal(err)
	}
	pt := ScopeReviewTarget(pair)
	want := base[:7] + ".." + set.Tip[:7]
	if pt.Preview != want || pt.Label != want || pt.Commit != set.Tip || pt.Range != base+".."+set.Tip {
		t.Fatalf("pair target = %+v", pt)
	}
}

func TestPreviewReviewIsTaggedAndOffCommits(t *testing.T) {
	t.Parallel()
	_, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	id := savePreviewReview(t, svc, set)
	r, err := svc.Review(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.Preview != "main...feat/x" || r.Commit != set.Tip || r.Doc == nil {
		t.Fatalf("review = %+v", r)
	}
	c, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range c.Reviews {
		if h.ID == id {
			t.Fatalf("a preview review is in the commit-facing Reviews: %+v", h)
		}
	}
	heads := c.PreviewReviews["main...feat/x"]
	if len(heads) != 1 || heads[0].ID != id || heads[0].Scope != set.Base+".."+set.Tip || heads[0].Remarks != 1 {
		t.Fatalf("PreviewReviews = %+v", c.PreviewReviews)
	}
	if got, _ := svc.ReviewsForCommit(ctx, set.Tip); len(got) != 0 {
		t.Fatalf("ReviewsForCommit lists the preview review: %+v", got)
	}
	// View all notes still lists it (Reviews() is the overview's source).
	all, _ := svc.Reviews(ctx)
	if len(all) != 1 || all[0].ID != id {
		t.Fatalf("Reviews() = %+v", all)
	}
}

func TestPreviewReviewRemarkReplyAttaches(t *testing.T) {
	t.Parallel()
	_, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	id := savePreviewReview(t, svc, set)
	if _, err := svc.NoteReply(ctx, model.ReviewNoteIDPrefix+id+":0", model.Note{Summary: "fixed"}); err != nil {
		t.Fatal(err)
	}
	r, err := svc.Review(ctx, id)
	if err != nil || len(r.Replies) != 1 || r.Replies[0].Summary != "fixed" {
		t.Fatalf("replies = %+v %v", r.Replies, err)
	}
}

func TestPreviewReviewNotCountedAsPreviewNote(t *testing.T) {
	t.Parallel()
	_, svc, set := previewReviewRepo(t)
	savePreviewReview(t, svc, set)
	_, total, err := svc.PreviewNoteCounts(context.Background(), set)
	if err != nil || total != 0 {
		t.Fatalf("preview note total = %d %v, want 0 (a review is not a line note)", total, err)
	}
}

func TestUntaggedReviewStaysOnItsCommit(t *testing.T) {
	t.Parallel()
	_, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	tg := ScopeReviewTarget(set)
	tg.Preview = "" // what `gg review --preview` stored before this change
	id, _, err := svc.SaveReview(ctx, SaveReview{Target: tg, Agent: "a", Text: previewReviewDoc})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := svc.NoteCounts(ctx)
	if len(c.Reviews) != 1 || c.Reviews[0].ID != id || len(c.PreviewReviews) != 0 {
		t.Fatalf("untagged review: Reviews=%+v PreviewReviews=%+v", c.Reviews, c.PreviewReviews)
	}
	if got, _ := svc.ReviewsForCommit(ctx, set.Tip); len(got) != 1 || !strings.HasPrefix(got[0].Summary, "Review:") {
		t.Fatalf("ReviewsForCommit = %+v", got)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/preview-reviews && go test ./internal/domain/ -run 'TestScopeReviewTarget|TestPreviewReview|TestUntaggedReview' -count=1`
Expected: build failure — `undefined: ScopeReviewTarget`, `tg.Preview undefined`.

- [ ] **Step 3: Implement**

`internal/domain/review.go` — add to `ReviewTarget` after `Branch string`:

```go
	// Preview is the scope a review belongs to when it reviewed a merge
	// preview ("<target>...<source>", branch NAMES) or a commit pair
	// ("<a7>..<b7>"): Note.Preview. Such a review is its preview's, never
	// its commit's. Data only — never spliced into a command.
	Preview string
```

and below `WorkingReviewTarget`:

```go
// ScopeReviewTarget is the review of a merge preview or a commit pair: Range
// = the scope's hex pair (merge base..source tip, or a..b), Label = the human
// pair, Commit = the tip, Preview = the scope's name. A pull request's set
// has no portable scope name, so its review is stored untagged (as before).
// The ONE constructor: `gg review --preview`, `gg review save`, the TUI and
// the web all build a scope review here.
func ScopeReviewTarget(set PreviewNoteSet) ReviewTarget {
	spec := set.DiffSpec()
	label := set.Target + " ... " + set.Source
	if set.IsPair() {
		label = shortSHA(set.Base) + ".." + shortSHA(set.Tip)
	}
	return ReviewTarget{Kind: ReviewRange, Range: spec.Rev, Label: label, Diff: spec, Commit: set.Tip, Preview: set.scope()}
}
```

`internal/domain/review_notes.go`:
- `Review` struct: add `Preview string // the scope it belongs to (Note.Preview); "" = a commit's or branch's review` after `Worktree`.
- `ReviewHead` struct: add
  ```go
	// Preview is the scope a preview review belongs to ("" otherwise),
	// Scope the hex range it read; Older marks a preview review of a tip
	// the preview has since moved past (PreviewReviews sets it).
	Preview, Scope string
	Older          bool
  ```
- `putReview`: after `n := model.Note{…}` add `n.Preview = t.Preview`.
- `reviewOf`: in the non-working `Review{…}` literal add `Preview: n.Preview,`.
- `reviewNotes`: `parts := []notes.Part{notes.PartCommits, notes.PartPreviews}`.
- `ReviewsForCommit`: the filter becomes `if n.Address.Commit == sha && n.Preview == "" {` with the comment `// a preview's review is its preview's, never its commit's (spec R5)`.

`internal/domain/notes.go`:
- `NoteCounts`: after `Reviews []ReviewHead …` add
  ```go
	// PreviewReviews are the reviews written for a merge preview or a commit
	// pair, keyed by the scope name (Note.Preview), newest first. Never in
	// Reviews: a preview's review marks no commit (spec R5).
	PreviewReviews map[string][]ReviewHead
  ```
- the `if n.IsReviewNote() {` arm becomes:
  ```go
		if n.IsReviewNote() {
			// A review has its own marker (✎ in Commits, ◆ in Branches):
			// it is never counted in the ◆N note badges.
			h := ReviewHead{ID: n.ID, Commit: n.Address.Commit, Branch: n.Address.Branch,
				Agent: n.Author, Summary: n.Summary, Created: n.Created, Preview: n.Preview, Scope: n.Scope}
			h.Remarks, h.Resolved = docTally(n.Rationale, remarkRes[n.ID])
			if n.Preview != "" {
				if c.PreviewReviews == nil {
					c.PreviewReviews = map[string][]ReviewHead{}
				}
				c.PreviewReviews[n.Preview] = append(c.PreviewReviews[n.Preview], h)
				continue
			}
			c.Reviews = append(c.Reviews, h)
			continue
		}
  ```
  Then check the code after the loop: if `c.Reviews` is sorted newest-first there, sort each `c.PreviewReviews[k]` the same way right beside it (`sort.SliceStable(hs, func(a, b int) bool { return hs[a].Created.After(hs[b].Created) })`).

`internal/domain/previewnotes.go` `loadPreviewNotes`: in the second loop, first statement:
  ```go
		if n.IsReviewNote() {
			continue // a review is shown as the preview's review, never as a line note
		}
  ```

`internal/cli/review.go` `pf.set()` arm: replace the `target = domain.ReviewTarget{Kind: domain.ReviewRange, …}` literal with `target = domain.ScopeReviewTarget(tgt.Set)` (keep `arg = tgt.Spec.Rev` and the hunkSpec/preview lines; update the comment to say the review is now the preview's).

- [ ] **Step 4: Run the tests and the touched packages**

Run: `go test ./internal/domain/ -run 'TestScopeReviewTarget|TestPreviewReview|TestUntaggedReview' -count=1 && go test ./internal/domain/ ./internal/cli/ -count=1`
Expected: PASS. A pre-existing test that asserts `gg review --preview` puts its review in `ReviewsForCommit`/`NoteCounts.Reviews` is now wrong by design (R5): update its expectation to `PreviewReviews` and say so in the commit message.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/review.go internal/domain/review_notes.go internal/domain/notes.go internal/domain/previewnotes.go internal/cli/review.go internal/domain/preview_review_test.go
git commit -F /tmp/claude-1000/-work-gigagit/8d4423ea-2419-4d1a-bd9c-7cf69aaa99fc/scratchpad/msg.txt
```
Message: `feat(domain): a preview review belongs to its preview, never its commit` + body naming `ScopeReviewTarget`, `NoteCounts.PreviewReviews`, and any updated test + the trailer.

---

### Task 2: `PreviewReviews` — current, older tip, gone

**Files:**
- Create: `internal/domain/preview_reviews.go`
- Test: `internal/domain/preview_review_test.go` (append)

**Interfaces:**
- Consumes: `NoteCounts.PreviewReviews`, `ReviewHead.Scope/Older`, `PreviewNoteSet.commitSet()`, `scope()`.
- Produces: `func (s *Service) PreviewReviews(ctx context.Context, set PreviewNoteSet) ([]ReviewHead, error)` — newest first; plan 2 (TUI) and plan 3 (web) call it per preview row and for the opened preview.

- [ ] **Step 1: Write the failing tests** (append):

```go
func TestPreviewReviewsCurrentThenOlder(t *testing.T) {
	t.Parallel()
	dir, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	id := savePreviewReview(t, svc, set)
	got, err := svc.PreviewReviews(ctx, set)
	if err != nil || len(got) != 1 || got[0].ID != id || got[0].Older {
		t.Fatalf("current: %+v %v", got, err)
	}
	commitFile(t, dir, "g.txt", "y\n", "second") // the source moves on
	moved, err := svc.PreviewNotes(ctx, "feat/x", "main")
	if err != nil {
		t.Fatal(err)
	}
	got, err = svc.PreviewReviews(ctx, moved)
	if err != nil || len(got) != 1 || !got[0].Older {
		t.Fatalf("after a new commit: %+v %v, want one older review", got, err)
	}
}

func TestPreviewReviewsHidesAGoneTip(t *testing.T) {
	t.Parallel()
	dir, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	savePreviewReview(t, svc, set)
	// Rewrite the branch: the reviewed tip becomes unreachable, then pruned.
	runGitIn(t, dir, "reset", "--hard", "main")
	commitFile(t, dir, "h.txt", "z\n", "rewritten")
	runGitIn(t, dir, "reflog", "expire", "--expire=now", "--all")
	runGitIn(t, dir, "gc", "--prune=now", "--quiet")
	moved, err := svc.PreviewNotes(ctx, "feat/x", "main")
	if err != nil || !moved.OK() {
		t.Fatalf("PreviewNotes: %+v %v", moved, err)
	}
	if got, err := svc.PreviewReviews(ctx, moved); err != nil || len(got) != 0 {
		t.Fatalf("a review of a pruned tip = %+v %v, want hidden", got, err)
	}
}

func TestPreviewReviewsOnlyItsOwnScope(t *testing.T) {
	t.Parallel()
	dir, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	savePreviewReview(t, svc, set)
	runGitIn(t, dir, "branch", "other", "main")
	other, err := svc.PreviewNotes(ctx, "feat/x", "other")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.PreviewReviews(ctx, other); len(got) != 0 {
		t.Fatalf("another preview of the same source shows %+v", got)
	}
	if got, _ := svc.PreviewReviews(ctx, PreviewNoteSet{}); len(got) != 0 {
		t.Fatalf("the zero set shows %+v", got)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/domain/ -run TestPreviewReviews -count=1`
Expected: build failure — `svc.PreviewReviews undefined`.

- [ ] **Step 3: Implement** `internal/domain/preview_reviews.go`:

```go
package domain

import (
	"context"
	"strings"
)

// PreviewReviews are the reviews written for set (spec R2), newest first:
// current — its tip is the set's tip; older (Older) — the tip moved on but
// both commits the review read still exist, so the review view can open
// exactly what was reviewed; gone — a reviewed commit is no longer in the
// repository: omitted (View all notes still lists it). Matching is by the
// scope NAME, so a removed and re-added saved row, or a preview opened only
// from a link, finds the same reviews. A tip still in the set's commit list
// costs no git call; only a rewritten one is looked up.
func (s *Service) PreviewReviews(ctx context.Context, set PreviewNoteSet) ([]ReviewHead, error) {
	sc := set.scope()
	if sc == "" {
		return nil, nil
	}
	c, err := s.NoteCounts(ctx)
	if err != nil {
		return nil, err
	}
	heads := c.PreviewReviews[sc]
	if len(heads) == 0 {
		return nil, nil
	}
	in := set.commitSet()
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
		if h.Commit == set.Tip {
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
	return out, nil
}
```

Note: `heads` is the cached slice — the loop copies each element (`h` is a value), so setting `h.Older` never writes into the cache.

- [ ] **Step 4: Run** `go test ./internal/domain/ -run 'TestPreviewReview' -count=1` — Expected: PASS. If `TestPreviewReviewsHidesAGoneTip` still finds the commit, check whether `ResolveRev`'s `query` caches by key; if it does, call `s.repo.FindCommit(ctx, sha+"^{commit}")` through `queryQuiet` instead (uncached), and note why in a comment.

- [ ] **Step 5: Commit** `internal/domain/preview_reviews.go internal/domain/preview_review_test.go` — message `feat(domain): PreviewReviews — current, older tip, hidden when gone` + trailer.

---

### Task 3: `CommitReviewTarget` and `LinkReviewTarget`

**Files:**
- Create: `internal/domain/review_link_target.go`
- Modify: `internal/web/review.go` (`commitReviewTarget` delegates to domain)
- Test: `internal/domain/review_link_target_test.go` (new)

**Interfaces:**
- Consumes: `ScopeReviewTarget`, `BranchReviewTarget`, `WorkingReviewTarget`, `PairNotes`, `Resolved`.
- Produces:
  - `func (s *Service) CommitReviewTarget(ctx context.Context, sha string) (ReviewTarget, error)` — sha must be hex (7–64)
  - `func (s *Service) LinkReviewTarget(ctx context.Context, res Resolved) (ReviewTarget, error)`
  - `var ErrNoReviewChange = errors.New("the link names no change to review; give a commit, a pair, a merge preview, a branch, or a file in the working tree or the index")`

- [ ] **Step 1: Write the failing tests** — `internal/domain/review_link_target_test.go`:

```go
package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestLinkReviewTargetKinds(t *testing.T) {
	t.Parallel()
	dir, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	base := revParse(t, dir, "main")

	tg, err := svc.LinkReviewTarget(ctx, Resolved{Preview: &set, Commit: set.Tip})
	if err != nil || tg.Preview != "main...feat/x" {
		t.Fatalf("preview: %+v %v", tg, err)
	}
	tg, err = svc.LinkReviewTarget(ctx, Resolved{Pair: &model.LinkPair{A: base, B: set.Tip}, Commit: set.Tip})
	if err != nil || tg.Preview != base[:7]+".."+set.Tip[:7] || tg.Range != base+".."+set.Tip {
		t.Fatalf("pair: %+v %v", tg, err)
	}
	tg, err = svc.LinkReviewTarget(ctx, Resolved{Ref: "feat/x", Commit: set.Tip,
		Addr: model.FileAddress{State: model.StateCommitted, Commit: set.Tip}})
	if err != nil || tg.Kind != ReviewBranch || tg.Branch != "feat/x" || tg.Range != base+".."+set.Tip {
		t.Fatalf("ref: %+v %v", tg, err)
	}
	tg, err = svc.LinkReviewTarget(ctx, Resolved{Commit: set.Tip,
		Addr: model.FileAddress{State: model.StateCommitted, Commit: set.Tip, Path: "f.txt"}})
	if err != nil || tg.Range != set.Tip+"^.."+set.Tip || tg.Preview != "" || tg.Label != set.Tip[:8]+" feature commit" {
		t.Fatalf("commit (with a path): %+v %v", tg, err)
	}
	tg, err = svc.LinkReviewTarget(ctx, Resolved{Addr: model.FileAddress{State: model.StateUnstaged, Path: "f.txt"}})
	if err != nil || tg.Kind != ReviewWorking {
		t.Fatalf("working file: %+v %v", tg, err)
	}
	tg, err = svc.LinkReviewTarget(ctx, Resolved{Addr: model.FileAddress{State: model.StateStaged}})
	if err != nil || tg.Kind != ReviewWorking {
		t.Fatalf("@staged: %+v %v", tg, err)
	}
	if _, err = svc.LinkReviewTarget(ctx, Resolved{}); !errors.Is(err, ErrNoReviewChange) {
		t.Fatalf("a bare repo link: %v, want ErrNoReviewChange", err)
	}
	if _, err = svc.LinkReviewTarget(ctx, Resolved{Hint: model.LinkHint{Kind: "bookmark", ID: "x"}}); !errors.Is(err, ErrNoReviewChange) {
		t.Fatalf("a hint-only link: %v, want ErrNoReviewChange", err)
	}
}

func TestCommitReviewTargetRootCommit(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	root := revParse(t, dir, "HEAD") // BasicRepo has one commit
	tg, err := svc.CommitReviewTarget(context.Background(), root)
	if err != nil || tg.Range != root || tg.Commit != root {
		t.Fatalf("root: %+v %v", tg, err)
	}
	if _, err := svc.CommitReviewTarget(context.Background(), "main"); err == nil {
		t.Fatal("a ref name must be refused (hex only)")
	}
}
```

(Check `gittest.BasicRepo` makes exactly one commit before relying on `HEAD` being a root; if not, use `git rev-list --max-parents=0 HEAD`.)

- [ ] **Step 2: Run** `go test ./internal/domain/ -run 'TestLinkReviewTarget|TestCommitReviewTarget' -count=1` — Expected: build failure (undefined).

- [ ] **Step 3: Implement** `internal/domain/review_link_target.go`:

```go
package domain

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// ErrNoReviewChange: a link that names no change a review can read.
var ErrNoReviewChange = errors.New("the link names no change to review; give a commit, a pair, a merge preview, a branch, or a file in the working tree or the index")

var hexSHA = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// CommitReviewTarget scopes a review to ONE commit's own change, sha^..sha;
// a root commit is reviewed alone. sha must be hex — no ref name ever reaches
// the tool's <range> token. The label is "<8-char sha> <subject>", read here.
func (s *Service) CommitReviewTarget(ctx context.Context, sha string) (ReviewTarget, error) {
	if !hexSHA.MatchString(sha) {
		return ReviewTarget{}, errors.New("invalid commit")
	}
	rng := sha + "^.." + sha
	if _, ok, err := s.ResolveRev(ctx, sha+"^"); err == nil && !ok {
		rng = sha // root commit
	}
	label := sha
	if len(label) > 8 {
		label = label[:8]
	}
	if msg, err := s.CommitMessage(ctx, sha); err == nil {
		if subj := strings.TrimSpace(strings.SplitN(msg, "\n", 2)[0]); subj != "" {
			label += " " + subj
		}
	}
	return ReviewTarget{Kind: ReviewRange, Range: rng, Label: label, Diff: model.DiffSpec{Rev: rng}, Commit: sha}, nil
}

// LinkReviewTarget is the review a resolved gg:// link names: a merge preview
// or a pair → its scope (ScopeReviewTarget); a branch/tag tip → the branch
// against the trunk (BranchReviewTarget); a commit → its own change; a file in
// the working tree or the index, or @staged → the working changes. A path or
// line in the link narrows nothing: the whole change is reviewed. Call it on
// the service of the link's own checkout (res.Checkout).
func (s *Service) LinkReviewTarget(ctx context.Context, res Resolved) (ReviewTarget, error) {
	switch {
	case res.Preview != nil:
		if !res.Preview.OK() {
			return ReviewTarget{}, ErrNoReviewChange
		}
		return ScopeReviewTarget(*res.Preview), nil
	case res.Pair != nil:
		set, err := s.PairNotes(ctx, res.Pair.A, res.Pair.B)
		if err != nil {
			return ReviewTarget{}, err
		}
		if !set.OK() {
			return ReviewTarget{}, ErrNoReviewChange
		}
		return ScopeReviewTarget(set), nil
	case res.Ref != "":
		return s.BranchReviewTarget(ctx, res.Ref)
	case res.Addr.State == model.StateCommitted && res.Commit != "":
		return s.CommitReviewTarget(ctx, res.Commit)
	case res.Addr.State == model.StateStaged,
		res.Addr.State == model.StateUnstaged && res.Addr.Path != "":
		return WorkingReviewTarget(), nil
	}
	return ReviewTarget{}, ErrNoReviewChange
}
```

`internal/web/review.go`: replace the body of `commitReviewTarget` after the `isHexSha` check with `return svc.CommitReviewTarget(ctx, sha)` (keep the method and its hex check; drop the now-unused imports only if the compiler says so). Its existing web tests must stay green.

- [ ] **Step 4: Run** `go test ./internal/domain/ -run 'TestLinkReviewTarget|TestCommitReviewTarget' -count=1 && go test ./internal/web/ -run Review -count=1` — Expected: PASS.

- [ ] **Step 5: Commit** the three files + test — message `feat(domain): LinkReviewTarget — the review a gg:// link names` + trailer.

---

### Task 4: `gg review save`

**Files:**
- Create: `internal/cli/review_save.go`
- Modify: `internal/cli/review.go` (dispatch `save` like `show`; usage line)
- Test: `internal/cli/review_save_test.go` (new)

**Interfaces:**
- Consumes: `resolveLinkArg`, `openLinkTarget`, `linkExit`, `domain.LinkReviewTarget`, `svc.SaveReview`, `svc.ReviewLink`, `notebatch.ParseReview`.
- Produces: the CLI verb (used by the skill in Task 5):
  ```
  gg review save <gg-link> --agent <name> (--stdin | --file <path>) [--json]
  gg review save <gg-link> --dry-run [--json]
  ```
  `--dry-run --json` → `{"kind":"preview|pair|commit|branch|working","label":…,"range":…,"diff":…}`; `diff` is the argument for `gg diff` (the range, or `HEAD` for working changes). Save `--json` → `{"id":…,"link":…,"warn":…}`.

- [ ] **Step 1: Write the failing tests** — `internal/cli/review_save_test.go`:

```go
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const saveDoc = `{"version":1,"summary":"## Summary\nfine","files":[{"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"one"}]}]}`

// saveRepo: main + feat/x one commit ahead; returns dir and the preview link.
func saveRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "config", "user.name", "t")
	gitRun(t, dir, "config", "user.email", "t@t")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("f.txt", "a\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	gitRun(t, dir, "checkout", "-q", "-b", "feat/x")
	write("f.txt", "a\nb\n")
	gitRun(t, dir, "commit", "-q", "-am", "feature")
	code, out, errb := runCLI(t, dir, "link", "--preview", "main...feat/x")
	if code != 0 {
		t.Fatalf("link --preview = %d %q", code, errb)
	}
	return dir, strings.TrimSpace(out)
}

func TestReviewSavePreviewLink(t *testing.T) {
	t.Parallel()
	dir, link := saveRepo(t)
	code, out, errb := runCLIStdin(t, dir, saveDoc, "review", "save", link, "--agent", "Claude Code", "--stdin", "--json")
	if code != 0 {
		t.Fatalf("save = %d %q", code, errb)
	}
	var got struct{ ID, Link, Warn string }
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.ID == "" || !strings.Contains(got.Link, "?review="+got.ID) {
		t.Fatalf("json = %q %v", out, err)
	}
	// It is the preview's review: `gg review show` reads it back with its remark.
	code, out, _ = runCLI(t, dir, "review", "show", got.Link)
	if code != 0 || !strings.Contains(out, "fine") || !strings.Contains(out, "one") {
		t.Fatalf("show = %d %q", code, out)
	}
}

func TestReviewSaveDryRun(t *testing.T) {
	t.Parallel()
	dir, link := saveRepo(t)
	code, out, errb := runCLI(t, dir, "review", "save", link, "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("dry-run = %d %q", code, errb)
	}
	var got struct{ Kind, Label, Range, Diff string }
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Kind != "preview" || got.Label != "main ... feat/x" ||
		got.Diff != got.Range || !strings.Contains(got.Range, "..") {
		t.Fatalf("dry-run json = %q %v", out, err)
	}
	// Nothing was stored.
	if code, _, _ = runCLI(t, dir, "review", "show", "latest"); code == 0 {
		t.Fatal("dry-run stored a review")
	}
}

func TestReviewSaveFileLinkReviewsWholeChange(t *testing.T) {
	t.Parallel()
	dir, _ := saveRepo(t)
	code, link, errb := runCLI(t, dir, "link", "f.txt:2", "--rev", "HEAD")
	if code != 0 {
		t.Fatalf("link = %d %q", code, errb)
	}
	code, out, errb := runCLI(t, dir, "review", "save", strings.TrimSpace(link), "--dry-run", "--json")
	var got struct{ Kind, Range string }
	if code != 0 || json.Unmarshal([]byte(out), &got) != nil || got.Kind != "commit" || !strings.HasSuffix(got.Range, "^.."+strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))) {
		t.Fatalf("file link dry-run = %d %q %q", code, out, errb)
	}
}

func TestReviewSaveRefusals(t *testing.T) {
	t.Parallel()
	dir, link := saveRepo(t)
	if code, _, errb := runCLIStdin(t, dir, "just prose", "review", "save", link, "--agent", "a", "--stdin"); code != 1 || !strings.Contains(errb, "review document") {
		t.Fatalf("prose = %d %q, want 1 + a document error", code, errb)
	}
	if code, _, _ := runCLIStdin(t, dir, saveDoc, "review", "save", link, "--stdin"); code != 2 {
		t.Fatalf("no --agent = %d, want 2", code)
	}
	if code, _, _ := runCLI(t, dir, "review", "save", link, "--agent", "a"); code != 2 {
		t.Fatalf("no input = %d, want 2", code)
	}
	if code, _, _ := runCLI(t, dir, "review", "save", "gg://not a link", "--dry-run"); code != 2 {
		t.Fatalf("malformed link = %d, want 2", code)
	}
	bare := link[:strings.Index(link, "@")] // gg://<repo> with no target
	if code, _, errb := runCLI(t, dir, "review", "save", bare, "--dry-run"); code != 1 || !strings.Contains(errb, "names no change") {
		t.Fatalf("bare repo link = %d %q, want 1", code, errb)
	}
}
```

(`runCLIStdin(t, workdir, in, args...)` is in `internal/cli/batch_test.go:15`; `gitRun`/`runGit` are the helpers `review_show_test.go` uses.)

- [ ] **Step 2: Run** `go test ./internal/cli/ -run TestReviewSave -count=1` — Expected: FAIL (`save` is parsed as a revision today).

- [ ] **Step 3: Implement**

`internal/cli/review.go` `cmdReview`: beside the `show` dispatch add

```go
	if len(rest) > 0 && rest[0] == "save" {
		return reviewSave(svc, rest[1:], stdin, stdout, stderr)
	}
```

— `cmdReview` has no stdin today: change its signature to `cmdReview(svc *domain.Service, workdir string, rest []string, stdin io.Reader, stdout, stderr io.Writer) int` and its one call site `internal/cli/cli.go:188` to pass `stdin` (already in scope there, as for `cmdTemplate`). Add `reviewSaveUsage` to both usage prints.

`internal/cli/review_save.go`:

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
	"github.com/homeend/gigagit/internal/notebatch"
)

const reviewSaveUsage = "usage: gg review save <gg-link> --agent <name> (--stdin | --file <path>) [--json]\n       gg review save <gg-link> --dry-run [--json]"

// reviewSave stores a review document an agent wrote itself as the review of
// the change the link names (domain.LinkReviewTarget) — the same note the
// review lane stores, so the TUI and the web show it the same way. --dry-run
// prints what would be reviewed, and the `gg diff` argument that shows it,
// without storing anything: a skill reads the change through it rather than
// guessing (a @ref: link is the tip's own change to `gg diff`, the branch
// against the trunk to a review).
func reviewSave(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review save", flag.ContinueOnError)
	fs.SetOutput(stderr)
	agent := fs.String("agent", "", "the reviewer's name (shown on the review)")
	fromStdin := fs.Bool("stdin", false, "read the review document from stdin")
	file := fs.String("file", "", "read the review document from a file")
	dry := fs.Bool("dry-run", false, "print what the link would review; store nothing")
	asJSON := fs.Bool("json", false, "print JSON")
	// The link comes FIRST (`gg review save <link> --agent …`): flag.Parse
	// stops at the first positional, and --agent/--file take values, so
	// partitionFlags (bool-only) cannot be used.
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(stderr, reviewSaveUsage)
		return 2
	}
	link := args[0]
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(stderr, reviewSaveUsage)
		return 2
	}
	if !*dry && (strings.TrimSpace(*agent) == "" || *fromStdin == (*file != "")) {
		fmt.Fprintln(stderr, reviewSaveUsage)
		return 2
	}
	ctx := context.Background()
	res, err := resolveLinkArg(ctx, svc, link, linkShapes{Pair: true, Ref: true}, "review save")
	if err != nil {
		return linkExit("review save", err, stderr)
	}
	tsvc := openLinkTarget(res)
	target, err := tsvc.LinkReviewTarget(ctx, res)
	if err != nil {
		fmt.Fprintln(stderr, "review save:", err)
		return 1
	}
	if *dry {
		return printReviewTarget(stdout, target, *asJSON)
	}
	var data []byte
	if *fromStdin {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(*file)
	}
	if err != nil {
		fmt.Fprintln(stderr, "review save:", err)
		return 1
	}
	if _, perr := notebatch.ParseReview(data); perr != nil {
		fmt.Fprintln(stderr, "review save: not a gg review document:", perr)
		return 1
	}
	id, warn, err := tsvc.SaveReview(ctx, domain.SaveReview{Target: target, Agent: strings.TrimSpace(*agent), Text: string(data)})
	if err != nil {
		fmt.Fprintln(stderr, "review save:", err)
		return 1
	}
	rl, lerr := tsvc.ReviewLink(ctx, id)
	if *asJSON {
		_ = json.NewEncoder(stdout).Encode(map[string]string{"id": id, "link": rl, "warn": warn})
	} else {
		fmt.Fprintln(stdout, "review:", id)
		if lerr == nil {
			fmt.Fprintln(stdout, rl)
		}
	}
	if warn != "" && !*asJSON {
		fmt.Fprintln(stderr, "warning:", warn)
	}
	if lerr != nil && !errors.Is(lerr, domain.ErrReviewNotFound) {
		fmt.Fprintln(stderr, "warning: no link for the review:", lerr)
	}
	return 0
}

// reviewKind names a target for --dry-run.
func reviewKind(t domain.ReviewTarget) string {
	switch {
	case t.Kind == domain.ReviewWorking:
		return "working"
	case t.Kind == domain.ReviewBranch:
		return "branch"
	case strings.Contains(t.Preview, "..."):
		return "preview"
	case t.Preview != "":
		return "pair"
	}
	return "commit"
}

func printReviewTarget(w io.Writer, t domain.ReviewTarget, asJSON bool) int {
	diff := t.Range
	if t.Kind == domain.ReviewWorking {
		diff = "HEAD" // git diff HEAD: the working tree + the index vs HEAD
	}
	if asJSON {
		_ = json.NewEncoder(w).Encode(map[string]string{"kind": reviewKind(t), "label": t.DisplayLabel(), "range": t.Range, "diff": diff})
		return 0
	}
	fmt.Fprintf(w, "%s: %s\nread it with: gg diff %s\n", reviewKind(t), t.DisplayLabel(), diff)
	return 0
}
```

- [ ] **Step 4: Run** `go test ./internal/cli/ -run 'TestReviewSave|TestReview' -count=1` — Expected: PASS.

- [ ] **Step 5: Commit** `internal/cli/review_save.go internal/cli/review.go internal/cli/review_save_test.go` (+ the stdin call-site file) — message `feat(cli): gg review save — store a review document for any change link` + trailer.

---

### Task 5: The `/gg-review` skill and the skill updates

**Files:**
- Create: `internal/agentskill/gg-review.md`
- Modify: `internal/agentskill/agentskill.go` (skill, frontmatter extras, `All()`, versions)
- Modify: `internal/agentskill/reviewing-with-gg.md` (a "Writing a full review yourself" section)
- Modify: `internal/agentskill/using-gg.md` (`gg review save` in the CLI reference)
- Modify: `internal/cli/init.go` only if `reviewTargetSuffix`'s output is asserted with a fixed skill list
- Test: `internal/agentskill/agentskill_test.go`, `internal/agentinit/agentinit_test.go` (update expectations)

**Interfaces:**
- Produces: `agentskill.GGReview Skill`, `const GGReviewVersion = 1`; `All()` = `UsingGG, ReviewingWithGG, Delegate, GGReview`.

- [ ] **Step 1: Write the failing tests** — in `agentskill_test.go` change the `All()` expectation (line ~145) to four names ending in `gg-review`, and add:

```go
func TestGGReviewFrontmatter(t *testing.T) {
	t.Parallel()
	f := GGReview.SkillFile()
	for _, want := range []string{"name: gg-review\n", "argument-hint: \"<gg-link> [what to focus on]\"\n", "disable-model-invocation: true\n", "gg review save", "--dry-run"} {
		if !strings.Contains(f, want) {
			t.Fatalf("gg-review SKILL.md lacks %q", want)
		}
	}
	if strings.Contains(UsingGG.SkillFile(), "disable-model-invocation") {
		t.Fatal("only gg-review is user-invoked")
	}
}
```

In `agentinit_test.go`, find the tests that count installed skills / files per agent and add `gg-review` to their expected sets (e.g. a ModeSkillFile install now writes `.claude/skills/gg-review/SKILL.md`).

- [ ] **Step 2: Run** `go test ./internal/agentskill/ ./internal/agentinit/ -count=1` — Expected: FAIL (`undefined: GGReview`).

- [ ] **Step 3: Implement**

`agentskill.go`:
- `//go:embed gg-review.md` → `var ggReviewBody string`
- `const GGReviewVersion = 1` (doc: the counter for gg-review)
- `Skill` gains `front string // extra frontmatter lines, each ending in "\n"`
- `SkillFile()` writes `s.front` right after the `description:` line.
- 
```go
// GGReview is the user-invoked /gg-review <gg-link> [focus]: review the change
// a link names and store ONE review document with `gg review save`. Never
// loaded on the model's own initiative (disable-model-invocation).
var GGReview = func() Skill {
	s := newSkill("gg-review",
		"Review the change a gg:// link names and store the review in gg — an overview plus per-file remarks — with gg review save.",
		GGReviewVersion, ggReviewBody)
	s.front = "argument-hint: \"<gg-link> [what to focus on]\"\ndisable-model-invocation: true\n"
	return s
}()
```
- `All()` appends `GGReview`; update the package doc's skill list.
- Bump `Version` (using-gg) by 1 and `ReviewVersion` by 1.

`gg-review.md` (the body — plain instructions; Claude Code appends the user's text as `ARGUMENTS:`):

````markdown
# /gg-review — a full review stored in gg

The user typed `/gg-review <gg-link> [focus]`: the first word after the
command is a `gg://` link, the rest (optional) is what they want you to look
at. Review the change the link names and store ONE review — an overview plus
per-file remarks — in gg, where the user reads it beside the code. Do NOT
launch gg's TUI or `gg web`; they belong to the user.

## Steps

1. **What is reviewed.** `gg review save <link> --dry-run --json` prints
   `kind`, `label` and `diff`. A file or line in the link does not narrow the
   review: the whole change is reviewed. A refusal ("names no change") means
   the link is not a change — ask the user for a commit, pair, merge preview,
   branch or working-tree link.
2. **Read it** with the `diff` value — never guess the range:
   `gg diff --stat <diff>`, then `gg diff --hunks --json <diff>`, then
   `gg diff <diff> -- <file>` for each file that matters. For `kind` =
   `working`, also run `gg status`: untracked files are part of the review.
3. **Write the review document** — exactly the shape in the
   reviewing-with-gg skill's "Review document" section: `summary` is markdown
   with `## Summary`, `## Findings` (most important first), `## Verdict`;
   `files[].annotations[]` are the remarks, `newRange` lines of the new
   version (`oldRange` for a removed line), `meta.severity` = bug | risk |
   design | nit. Put the user's focus first in Findings when they gave one.
4. **Store it:** `gg review save <link> --agent "<your name, e.g. Claude Code>" --stdin --json`
   with the document on stdin. A "not a gg review document" error names what
   is wrong — fix the JSON and run it again.
5. **Show it.** If `gg session list` shows a live gg window for this
   worktree, open the review there: `gg session navigate <link from step 4>`.
6. **Reply** with the review link and a three-line summary (verdict first).
   Do not paste the whole review into the chat — it is in gg.

A question about ONE file or line ("what does this function do?", "is this
safe?") is not a review: answer it, and leave a note with `gg note add` if a
note helps (reviewing-with-gg skill).
````

`reviewing-with-gg.md`: add after "## Checking another agent's review":

```markdown
## Writing a full review yourself

When the user asks you to REVIEW a change (not one file, the whole change):
store one review document — the "Review document" shape below — with
`gg review save <gg-link> --agent "<your name>" --stdin`. Run
`gg review save <gg-link> --dry-run` first: it prints what is reviewed and
the `gg diff` argument that shows exactly that change. The review opens in
the user's review view (overview, files, your remarks at their lines); on a
merge preview or a commit pair it belongs to that preview. The user can also
start this with `/gg-review <gg-link> [focus]`.
```

`using-gg.md`: in the CLI reference block that lists `gg review` / `gg review show`, add the two `gg review save` lines from Task 4's Interfaces with a one-line description each.

- [ ] **Step 4: Run** `go test ./internal/agentskill/ ./internal/agentinit/ ./internal/cli/ -count=1` — Expected: PASS (fix any golden/version test the bump trips, e.g. a skill-version guard).

- [ ] **Step 5: Commit** the skill files, `agentskill.go`, tests — message `feat(skills): /gg-review — a full review stored in gg` + trailer.

---

### Task 6: Docs and the race gate

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`

- [ ] **Step 1: CHANGELOG** — an "Unreleased" entry: preview reviews belong to their preview (no ✎ on the source tip); `gg review --preview` now tags its review; new `gg review save <link> (--stdin|--file) --agent <name> [--dry-run] [--json]`; new `/gg-review` skill (run `gg init --update`).
- [ ] **Step 2: README** — the review section: `gg review save` and `/gg-review <link> [focus]`.
- [ ] **Step 3: docs/CLAUDE-details.md** — under reviews: `ReviewTarget.Preview` → `Note.Preview`; `NoteCounts.PreviewReviews` vs `Reviews` (R5); `PreviewReviews` classification (R2: current / older / gone, membership first, `ResolveRev` only for a moved tip); rename does not follow (deviation 1).
- [ ] **Step 4: Gate** — `./test.sh race 2>&1 | tee /tmp/claude-1000/-work-gigagit/8d4423ea-2419-4d1a-bd9c-7cf69aaa99fc/scratchpad/race.log; grep -c "all green" /tmp/claude-1000/-work-gigagit/8d4423ea-2419-4d1a-bd9c-7cf69aaa99fc/scratchpad/race.log` — Expected: `1`. A failure: fix, rerun.
- [ ] **Step 5: Commit** the three docs — message `docs: preview reviews, gg review save, /gg-review` + trailer.
- [ ] **Step 6: Build a verify binary** — `go build -o bin/gg ./cmd/gg` in the worktree; give the user its absolute path and a manual check: `bin/gg review save "$(bin/gg link --preview main...<branch>)" --dry-run` in a test repo.

Then the final whole-branch review (a read-only reviewer subagent on the most capable model is allowed by CLAUDE.md), then ask the user before merging.
