# A Pull Request Shows Only Its Own Notes — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (this repo: native execution, no implementer subagents). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A PR's view shows only GitHub's threads, notes written for that PR and AI reviews run on that PR — no carried notes, no other notes on its commits, no reviews of its commits.

**Architecture:** A PR note records `Note.Preview = "<base>...refs/gg/pr/<n>"` (what `PreviewNoteSet.Pair()` already names a PR set). `PreviewNoteSet.scope()` stops exempting PR sets; a new set method `owns(preview)` matches a PR set by PR number (`PRScopeNumber`), every other set by equality. Carried notes are deleted. Every writer (TUI form, web, CLI, MCP, batches, reviews) stamps the scope.

**Tech Stack:** Go 1.26, Bubble Tea TUI, vanilla-JS web page, real-git tests.

**Spec:** `docs/superpowers/specs/2026-10-08-pr-note-scope-design.md`

## Global Constraints

- The PR scope's shape is `<base>...refs/gg/pr/<n>`; match by PR number only, never the whole string (spec §2).
- No conversion of existing notes (spec §5).
- Every new TUI string through `i18n.T` with all four bundles; deleting a key deletes it in all four bundles (orphan gate).
- Engine/CLI prose stays English.
- `gg commit` has no `-F`: `gg add <paths>` then `git commit -F <msgfile>`. Never `git add -A`. Never `git checkout -- <file>` to undo a probe.
- Commit messages end with the attribution lines (Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com> / Claude-Session: https://claude.ai/code/session_01HrWwwbq8gwtTBR1ExUNHhi).
- Nothing is sent to GitHub.

## Review Focus

1. **The base moved** — a note stamped `main...refs/gg/pr/7`, then the PR's set is built with a base sha (closed PR / TUI) or `origin/main`: the note still shows (Task 1 test `TestPRNoteMatchesAnyBaseSpelling`).
2. **Two PRs share commits** (stacked PRs): a note written in PR #7 on a commit PR #8 also contains never shows in #8 (Task 1 test `TestPRNoteOfAnotherPRStaysOut`).
3. **The send id check** — `gg pr send --note <id>` / web send with the id of a plain note on a PR commit is refused as not the PR's (Task 2 test `TestPlanSendRefusesANoteNotWrittenForThePR`).
4. **gg web on a PR note whose commit is not the PR's tip** — the stamp is refused, the note stays plain (Task 5 test `TestWebPRNoteOffTheTipIsNotStamped`).
5. **A TUI "Review (AI)" run on a PR** is stored with the PR stamp and appears in that PR's view, not as a commit review (Task 3 test `TestPRReviewIsStampedAndShows`).

---

### Task 1: The PR scope on the read side (domain)

**Files:**
- Modify: `internal/domain/previewnotes.go` (`scope()`, new `owns`, `loadPreviewNotes`)
- Modify: `internal/domain/pairnotes.go` (new `PRScopeNumber`, `NoteScopeLabel`)
- Modify: `internal/domain/forge_send_test.go` (`addPRNote` stamps the PR scope)
- Test: `internal/domain/pr_note_scope_test.go` (new)

**Interfaces:**
- Produces: `func PRScopeNumber(scope string) (int, bool)`; `func (set PreviewNoteSet) owns(preview string) bool`; `scope()` returns `Pair()` for every set; `NoteScopeLabel("<x>...refs/gg/pr/7") == "PR #7"`.

- [ ] **Step 1: Write the failing tests** — `internal/domain/pr_note_scope_test.go`:

```go
package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// addScopedNote is a root note on commit:path:line written in scope ("" = plain).
func addScopedNote(t *testing.T, svc *Service, commit, path string, line int, scope, sum string) string {
	t.Helper()
	n, err := svc.NoteAdd(context.Background(), model.Note{Source: model.NoteSourceUser, Summary: sum, Preview: scope,
		Address: model.FileAddress{State: model.StateCommitted, Commit: commit, Path: path},
		Side:    model.NoteSideNew, Range: [2]int{line, line}})
	if err != nil {
		t.Fatal(err)
	}
	return n.ID
}

func idsAt(t *testing.T, svc *Service, set PreviewNoteSet, path string) map[string]bool {
	t.Helper()
	got, err := svc.PreviewNotesAt(context.Background(), set, path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, r := range got {
		out[r.Note.ID] = true
	}
	return out
}

// Spec §1: the PR shows the note written for it and nothing else stored on
// its commits — a plain note, another review's note.
func TestPRShowsOnlyItsOwnNotes(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	mine := addScopedNote(t, svc, head, "big.go", 5, "main..."+git.PRRef(7), "for the PR")
	plain := addScopedNote(t, svc, head, "big.go", 25, "", "a commit note")
	other := addScopedNote(t, svc, head, "big.go", 5, "main...feat", "a branch review's note")
	ids := idsAt(t, svc, prNoteSetOf(t, svc), "big.go")
	if !ids[mine] || ids[plain] || ids[other] {
		t.Fatalf("PR view ids %v: mine %s, plain %s, other %s", ids, mine, plain, other)
	}
	counts, total, err := svc.PreviewNoteCounts(context.Background(), prNoteSetOf(t, svc))
	if err != nil || counts["big.go"] != 1 || total != 1 {
		t.Fatalf("counts %v total %d err %v", counts, total, err)
	}
}

// Review Focus 1: the base is any spelling, and it moves.
func TestPRNoteMatchesAnyBaseSpelling(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	base := revParse(t, repoDir(t, svc), "main")
	bySha := addScopedNote(t, svc, head, "big.go", 5, base+"..."+git.PRRef(7), "written over the base sha")
	byName := addScopedNote(t, svc, head, "big.go", 25, "origin/main..."+git.PRRef(7), "written by an agent")
	ids := idsAt(t, svc, prNoteSetOf(t, svc), "big.go")
	if !ids[bySha] || !ids[byName] {
		t.Fatalf("PR view ids %v", ids)
	}
}

// Review Focus 2: stacked PRs share commits; a note keeps to its own PR.
func TestPRNoteOfAnotherPRStaysOut(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	addScopedNote(t, svc, head, "big.go", 5, "main..."+git.PRRef(8), "PR 8's note")
	if ids := idsAt(t, svc, prNoteSetOf(t, svc), "big.go"); len(ids) != 0 {
		t.Fatalf("PR 7 shows %v", ids)
	}
}

func TestPRScopeNumber(t *testing.T) {
	t.Parallel()
	for scope, want := range map[string]int{
		"main..." + git.PRRef(7): 7, "abc123..." + git.PRRef(12): 12,
		"main...feat": 0, "a1..b2": 0, "": 0, git.PRRef(7): 0,
	} {
		n, ok := PRScopeNumber(scope)
		if (want == 0) == ok || n != want {
			t.Errorf("PRScopeNumber(%q) = %d %v, want %d", scope, n, ok, want)
		}
	}
	if got := NoteScopeLabel("0123abc..." + git.PRRef(7)); got != "PR #7" {
		t.Fatalf("label %q", got)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/domain/ -run 'TestPRShowsOnlyItsOwnNotes|TestPRNoteMatchesAnyBaseSpelling|TestPRNoteOfAnotherPRStaysOut|TestPRScopeNumber' -count=1`
Expected: FAIL — `undefined: PRScopeNumber` (compile error).

- [ ] **Step 3: Implement**

In `internal/domain/pairnotes.go`, beside `IsPreviewScope`:

```go
// PRScopeNumber is the pull request a note scope names:
// "<base>...refs/gg/pr/<n>", whatever the base's spelling — the base moves
// with the PR's target, the note stays the PR's (spec 2026-10-08 §2).
func PRScopeNumber(scope string) (int, bool) {
	_, src, ok := strings.Cut(scope, "...")
	if !ok {
		return 0, false
	}
	return git.ParsePRRef(strings.TrimSpace(src))
}
```

and in `NoteScopeLabel`, first:

```go
	if n, ok := PRScopeNumber(scope); ok {
		return "PR #" + strconv.Itoa(n)
	}
```

(add `strconv` / `git` imports if missing).

In `internal/domain/previewnotes.go`, replace `scope()` and add `owns`:

```go
// scope is the review whose notes the set shows (Note.Preview): a scope
// shows the notes written IN it and no others — not a plain note on one of
// its commits, which is the commit's, and not another review's. A pull
// request's set is no exception (spec 2026-10-08): its notes record
// "<base>...refs/gg/pr/<n>" and owns matches them by number.
func (set PreviewNoteSet) scope() string { return set.Pair() }

// owns reports whether a root note written in preview belongs to this set:
// a pull request's by its number (the base half moves), any other by name.
func (set PreviewNoteSet) owns(preview string) bool {
	if n, ok := git.ParsePRRef(set.Source); ok {
		m, ok := PRScopeNumber(preview)
		return ok && m == n
	}
	return preview == set.scope()
}
```

In `loadPreviewNotes`, the `ofReview` loop's test becomes `set.owns(n.Preview)`:

```go
		for _, n := range all {
			if !n.IsReply() && set.owns(n.Preview) {
				ofReview[n.ID] = true
			}
		}
```

In `internal/domain/forge_send_test.go`, `addPRNote` stamps the PR (an existing note for PR #7, as an agent would write it):

```go
func addPRNote(t *testing.T, svc *Service, commit, path string, line int, sum string) string {
	t.Helper()
	n, err := svc.NoteAdd(context.Background(), model.Note{Source: model.NoteSourceUser, Summary: sum,
		Preview: "main..." + git.PRRef(7),
		Address: model.FileAddress{State: model.StateCommitted, Commit: commit, Path: path},
		Side:    model.NoteSideNew, Range: [2]int{line, line}})
```

- [ ] **Step 4: Run them to see them pass**

Run: `go test ./internal/domain/ -run 'TestPRShowsOnlyItsOwnNotes|TestPRNoteMatchesAnyBaseSpelling|TestPRNoteOfAnotherPRStaysOut|TestPRScopeNumber' -count=1`
Expected: PASS. (`TestPRShowsOnlyItsOwnNotes`'s counts assertion may still fail until Task 2 removes carried notes — if `plain` is counted as carried, carry that assertion to Task 2 and ledger a ruling.)

- [ ] **Step 5: Run the domain package; fix the tests that pinned the exemption**

Run: `go test ./internal/domain/ -count=1 2>&1 | tail -40`
Expected: the only failures are tests asserting the old rule (carried notes, reviews of a PR commit — Tasks 2 and 3 rewrite those). Any other failure is a bug: debug it here.

- [ ] **Step 6: Commit**

```bash
gg add internal/domain/previewnotes.go internal/domain/pairnotes.go internal/domain/forge_send_test.go internal/domain/pr_note_scope_test.go
git commit -F <msgfile>   # "fix(notes): a pull request's view shows only notes written for it"
```

---

### Task 2: Carried notes are gone

**Files:**
- Delete: `internal/domain/forge_carried.go`, `internal/domain/forge_carried_test.go`
- Modify: `internal/domain/previewnotes.go` (`prExtras`, `PreviewNotesAll`, `PreviewNoteCounts`), `internal/domain/service.go` (`carriedCache`), `internal/domain/notes.go` (`Origin` field, cache reset at ~739), `internal/domain/notewire.go` (`Origin`), `internal/domain/forge_send.go` (`PRNotes` comment), `internal/domain/pr_reviews.go` (comment at 62)
- Modify: `internal/cli/note.go:714-716`, `internal/tui/diff_notes.go:418,463-472`, `internal/web/static/notebox.js:46-48`
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (delete `"from %s"` and `"from working tree"` if no other call site uses them — check with grep first)
- Modify tests: `internal/domain/pr_reviews_test.go` (`TestPRCountsIncludeCarriedNotesAndRemarks`, `TestPreviewNotesAtLeavesTheCachesAlone`), `internal/tui/note_marks_test.go` (`TestCarriedNoteNamesItsOrigin` — delete: the feature is gone), any web notebox test naming `origin`
- Test: `internal/domain/pr_note_scope_test.go` (add two tests)

**Interfaces:**
- Consumes: Task 1's `owns`.
- Produces: no `carriedNotes`, no `ResolvedNote.Origin`, no wire `origin`.

- [ ] **Step 1: Write the failing tests** (append to `pr_note_scope_test.go`):

```go
// Spec §1: a note from elsewhere whose lines reappear in the PR is not the
// PR's (the carried notes of the 2026-10-07 spec are gone).
func TestPRCarriesNoNoteFromElsewhere(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	mainTip := revParse(t, repoDir(t, svc), "main")
	elsewhere := addScopedNote(t, svc, mainTip, "big.go", 10, "", "line 10 is the same in the PR")
	set := prNoteSetOf(t, svc)
	if ids := idsAt(t, svc, set, "big.go"); ids[elsewhere] {
		t.Fatal("a note from main's tip shows in the PR")
	}
	all, err := svc.PreviewNotesAll(context.Background(), set)
	if err != nil || len(all) != 0 {
		t.Fatalf("PreviewNotesAll = %v, %v", all, err)
	}
}

// Review Focus 3: the send refuses a note the PR does not show.
func TestPlanSendRefusesANoteNotWrittenForThePR(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	plain := addScopedNote(t, svc, head, "big.go", 5, "", "a commit note")
	if _, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Notes: []string{plain}}); !errors.Is(err, ErrSendRequest) {
		t.Fatalf("planSend of a plain note: %v", err)
	}
}
```

(add `errors` to the imports.)

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/domain/ -run 'TestPRCarriesNoNoteFromElsewhere|TestPlanSendRefusesANoteNotWrittenForThePR' -count=1`
Expected: FAIL — the carried note shows (`a note from main's tip shows in the PR`). If `TestPlanSendRefuses…` already passes after Task 1, record it: it pins Task 1's filter, no RED of its own.

- [ ] **Step 3: Delete carried notes**

- `git rm internal/domain/forge_carried.go internal/domain/forge_carried_test.go`.
- `prExtras`:

```go
// prExtras is what a path's notes gain beyond the store's own, in a FRESH
// slice (the remark slice is a cached instance): a PR's AI reviews' remarks
// (plan 3, T1), then the forge's threads.
func (s *Service) prExtras(ctx context.Context, set PreviewNoteSet, path string, forge []ResolvedNote) []ResolvedNote {
	var out []ResolvedNote
	out = append(out, s.prReviewNotes(ctx, set)[path]...)
	return append(out, forge...)
}
```

- `PreviewNotesAll`: delete the `carriedNotes` loop.
- `PreviewNoteCounts`: delete the `carriedNotes` loop; comment says "its AI reviews' drawn remarks".
- `service.go`: delete `carriedCache` and its comment; `notes.go:~739` delete `s.carriedCache = nil`.
- `notes.go`: delete `ResolvedNote.Origin` and its comment; `notewire.go`: delete `Origin` (field, comment words, assignment at 98).
- `forge_send.go` `PRNotes` comment: "local notes written for the PR, GitHub threads, draft replies".
- `pr_reviews.go:62` comment: "Cached per tip:base:notes generation: READ-ONLY."
- `cli/note.go:714-716`: delete the `(from …)` lines.
- `tui/diff_notes.go`: `title := owner.noteBoxTitle(r)`; delete `noteOrigin`.
- `web/static/notebox.js`: delete the carried comment and `from`; the return drops `+ from`.
- i18n: `grep -rn 'T("from %s"\|T("from working tree"' internal` — if no hits remain, delete both keys from all four bundles.
- Tests: delete `TestCarriedNoteNamesItsOrigin`; rewrite `TestPRCountsIncludeCarriedNotesAndRemarks` → `TestPRCountsIncludeItsNotesAndRemarks` (drop the `mainTip` note, want 3 = 1 note + 2 remarks); rewrite `TestPreviewNotesAtLeavesTheCachesAlone` to use only `prReviewNotes` (`before` = 2). Task 3 changes how those remarks attach — keep `saveHeadReview` until then.

- [ ] **Step 4: Run them to see them pass**

Run: `go test ./internal/domain/ -run 'TestPRCarriesNoNoteFromElsewhere|TestPlanSendRefusesANoteNotWrittenForThePR|TestPRShowsOnlyItsOwnNotes' -count=1`
Expected: PASS.

- [ ] **Step 5: Build and the affected packages**

Run: `go build ./... && go vet ./internal/domain/ ./internal/tui/ ./internal/cli/ ./internal/web/ && go test ./internal/i18n/ ./internal/tui/ -run 'I18n|TestNote' -count=1`
Expected: builds; the i18n gates pass (no orphan key).

- [ ] **Step 6: Commit** — "fix(notes): a pull request carries no note from elsewhere"

---

### Task 3: A PR's AI reviews are the ones run on it

**Files:**
- Modify: `internal/domain/pr_reviews.go` (`prReviews` comment, `prReviewHeads`)
- Modify: `internal/domain/review.go:87-99` (`ScopeReviewTarget` comment)
- Modify tests: `internal/domain/pr_reviews_test.go` (`saveHeadReview` → a review saved on the PR; `TestPRDiffShowsItsReviewsRemarks`, `TestPRDiffLeavesOutAReviewOfAnotherCommit`, `TestPreviewNoteGroupsByPath`), `internal/domain/sha_file_test.go` (`TestPRNoteGroupsReadEachFileOnce` if it saves a commit review)
- Test: `internal/domain/pr_note_scope_test.go` (add two tests)

**Interfaces:**
- Consumes: `ScopeReviewTarget(set)` (now stamps `set.Pair()` for a PR set through `scope()`), `owns`.
- Produces: `prReviewHeads` = `NoteCounts.PreviewReviews` owned by the set, sorted by scope.

- [ ] **Step 1: Write the failing tests** (append):

```go
// Review Focus 5: a review run on the PR (the TUI/web/CLI path:
// ScopeReviewTarget) is stored with the PR's stamp and shows in the PR.
func TestPRReviewIsStampedAndShows(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	set := prNoteSetOf(t, svc)
	tgt := ScopeReviewTarget(set)
	if n, ok := PRScopeNumber(tgt.Preview); !ok || n != 7 {
		t.Fatalf("review target preview %q", tgt.Preview)
	}
	rid, _, err := svc.SaveReview(context.Background(), SaveReview{Target: tgt, Agent: "claude", Text: twoRemarks})
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.prReviews(context.Background(), set); len(got) != 1 || got[0].ID != rid {
		t.Fatalf("PR reviews = %+v", got)
	}
}

// Spec §1: a review of a commit that is in the PR is the commit's, not the PR's.
func TestPRLeavesOutAReviewOfItsCommit(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	saveHeadReview(t, svc, head, twoRemarks) // a commit review of the PR's head
	set := prNoteSetOf(t, svc)
	if got := svc.prReviews(context.Background(), set); len(got) != 0 {
		t.Fatalf("PR reviews = %+v", got)
	}
	for _, r := range mustNotesAt(t, svc, set, "big.go") {
		if strings.HasPrefix(r.Group, "review:") {
			t.Fatalf("a commit review's remark drew in the PR: %+v", r)
		}
	}
}

func mustNotesAt(t *testing.T, svc *Service, set PreviewNoteSet, path string) []ResolvedNote {
	t.Helper()
	got, err := svc.PreviewNotesAt(context.Background(), set, path)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
```

(add `strings` to the imports; `Review` has an `ID` field — check `review.go`'s type and adjust the field name if it differs.)

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/domain/ -run 'TestPRReviewIsStampedAndShows|TestPRLeavesOutAReviewOfItsCommit' -count=1`
Expected: `TestPRLeavesOutAReviewOfItsCommit` FAILS (the commit review shows). `TestPRReviewIsStampedAndShows` may already pass after Task 1 (the stamp rides `scope()`, and the suffix half finds it) — record that.

- [ ] **Step 3: Implement**

```go
// prReviews are the reviews that belong to a PR's view: the ones run ON the
// PR — saved with its scope "<base>...refs/gg/pr/<n>" (spec 2026-10-08). A
// review of one of the PR's commits is that commit's.
...
// prReviewHeads are the PR's review heads: the reviews saved on a scope the
// set owns (NoteCounts.PreviewReviews), in scope order.
func (s *Service) prReviewHeads(ctx context.Context, set PreviewNoteSet) []ReviewHead {
	c, err := s.NoteCounts(ctx)
	if err != nil {
		return nil
	}
	scopes := make([]string, 0, len(c.PreviewReviews))
	for sc := range c.PreviewReviews {
		if set.owns(sc) {
			scopes = append(scopes, sc)
		}
	}
	sort.Strings(scopes) // no map order
	var out []ReviewHead
	for _, sc := range scopes {
		out = append(out, c.PreviewReviews[sc]...)
	}
	return out
}
```

`ScopeReviewTarget`'s comment: drop "A pull request's set has no portable scope name, so its review is stored untagged (as before)"; say "a pull request's review records the PR's scope".

Rewrite `saveHeadReview`'s callers that mean "a review of the PR": add

```go
// savePRReview is a review run on PR #7 (ScopeReviewTarget, the TUI/web path).
func savePRReview(t *testing.T, svc *Service, doc string) string {
	t.Helper()
	rid, _, err := svc.SaveReview(context.Background(), SaveReview{
		Target: ScopeReviewTarget(prNoteSetOf(t, svc)), Agent: "claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	return rid
}
```

and use it in `TestPRDiffShowsItsReviewsRemarks`, `TestPRCountsIncludeItsNotesAndRemarks`, `TestPreviewNoteGroupsByPath`, `TestPreviewNotesAtLeavesTheCachesAlone` and any send-group test in `forge_send*_test.go` that groups by `review:<id>` (grep `saveHeadReview`). `TestPRDiffLeavesOutAReviewOfAnotherCommit` stays as it is (still true).

- [ ] **Step 4: Run them to see them pass**

Run: `go test ./internal/domain/ -count=1 2>&1 | tail -20`
Expected: the whole domain package passes.

- [ ] **Step 5: Commit** — "fix(reviews): a pull request shows the reviews run on it, not its commits' reviews"

---

### Task 4: The TUI stamps a note written in a PR

**Files:**
- Modify: `internal/tui/note_popup.go:120-124`
- Modify tests: `internal/tui/preview_notes_test.go` (`TestPRDiffNoteRecordsNoPreview` → `TestPRDiffNoteRecordsThePR`), `internal/tui/pr_send_serial_test.go` (`addTUINote` stamps the PR)

**Interfaces:**
- Consumes: `PreviewNoteSet.Pair()` (a PR set's is `<base>...refs/gg/pr/<n>`).

- [ ] **Step 1: Rewrite the test first**

```go
// A pull request's diff is a preview over refs/gg/pr/<n>: its notes record
// the PR's scope, so the PR's view shows them (spec 2026-10-08 §3).
func TestPRDiffNoteRecordsThePR(t *testing.T) {
	t.Parallel()
	m := previewDiffModel(t, nil)
	set := m.diffLayer().previewSet
	set.Source, set.Target = "refs/gg/pr/42", "main"
	m.previewOpen = &previewOpenState{prNumber: 42}
	v := m.diffLayer()
	for i, ln := range v.lines {
		if ln.Row.RightNo == 2 {
			v.curLine = i
		}
	}
	tm, _ := m.openNotePopup(noteAdd)
	p, ok := tm.(Model).topLayer().(*notePopup)
	if !ok {
		t.Fatal("the note form must open")
	}
	if p.preview != "main...refs/gg/pr/42" {
		t.Fatalf("a PR diff's note recorded preview %q", p.preview)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/tui/ -run TestPRDiffNoteRecordsThePR -count=1`
Expected: FAIL — `recorded preview ""`.

- [ ] **Step 3: Implement** — `note_popup.go`:

```go
		// Written in a preview, a pair or a pull request's diff: the note
		// records the scope it belongs to (a PR's: "<base>...refs/gg/pr/<n>").
		if set := m.previewNoteSet(); set != nil {
			p.preview = set.Pair()
		}
```

and `addTUINote` sets `Preview: "main...refs/gg/pr/7"`.

- [ ] **Step 4: Run the TUI package**

Run: `go test ./internal/tui/ -count=1 2>&1 | tail -20`
Expected: PASS (a failure naming a PR send/count test is a fixture that writes an unstamped PR note: stamp it like `addTUINote`).

- [ ] **Step 5: Commit** — "fix(tui): a note written in a pull request records the PR"

---

### Task 5: gg web stamps a note written in a PR

**Files:**
- Modify: `internal/domain/forge_send.go` (new `PRNoteScope` beside `PRNotes`)
- Modify: `internal/web/notes.go` (`noteReq.PR`, `handleNoteAdd`)
- Modify: `internal/web/static/files.js:3355` (send `pr`), `files.js:2813-2822` (comment)
- Modify tests: `internal/web/prsend_test.go` (`addWebNote` sends `"pr":7`)
- Test: `internal/web/pr_note_scope_test.go` (new), `internal/web/prs_static_test.go` or the nearest static guard (the page sends `pr`)

**Interfaces:**
- Produces: `func (s *Service) PRNoteScope(ctx context.Context, n int, commit string) string` — PR n's scope when commit is its tip, else "".

- [ ] **Step 1: Write the failing tests** — `internal/web/pr_note_scope_test.go`:

```go
package web

import (
	"context"
	"fmt"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// storedPreview is the scope a stored note recorded.
func storedPreview(t *testing.T, srv *Server, id string) string {
	t.Helper()
	n, err := srv.service().NoteGet(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return n.Preview
}

// Spec §3: a note the page writes in PR #7's diff records PR #7.
func TestWebPRNoteIsStamped(t *testing.T) {
	ts, srv, head := sendServerSrv(t)
	id := addWebNote(t, ts, head, "pr7.txt", 1, "for the PR")
	if n, ok := domain.PRScopeNumber(storedPreview(t, srv, id)); !ok || n != 7 {
		t.Fatalf("preview %q", storedPreview(t, srv, id))
	}
}

// Review Focus 4: a commit that is not the PR's tip takes no PR stamp.
func TestWebPRNoteOffTheTipIsNotStamped(t *testing.T) {
	ts, srv, head := sendServerSrv(t)
	parent := head + "~1" // resolved by the handler's rev
	code, out := postJSONAny(t, ts, "/api/notes/add",
		fmt.Sprintf(`{"path":"README.md","rev":%q,"state":"commit","side":"new","line":1,"summary":"x","pr":7}`, parent))
	id, _ := out["id"].(string)
	if code != 200 || id == "" {
		t.Fatalf("add = %d %v", code, out)
	}
	if p := storedPreview(t, srv, id); p != "" {
		t.Fatalf("an off-tip note recorded %q", p)
	}
}
```

Notes for the executor: `sendServer` returns `(ts, wf, head)`; add `sendServerSrv` returning the `*Server` too (thread `srv` out of `sendServerWith`), and use whatever single-note read the service has (`NoteGet` or the store lookup `NoteByID` — grep `func (s \*Service) Note` and pick the existing one). `README.md` must be a path at `head~1` in `prFixture` — check the fixture and pick a path that exists there.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/web/ -run 'TestWebPRNoteIsStamped|TestWebPRNoteOffTheTipIsNotStamped' -count=1`
Expected: FAIL — `preview ""` for the first (the second may pass: record it as a pin).

- [ ] **Step 3: Implement**

`internal/domain/forge_send.go`, after `PRNotes`:

```go
// PRNoteScope is the scope a note written in PR n's view records
// ("<base>...refs/gg/pr/<n>", spec 2026-10-08 §3) — only when commit is the
// PR's tip, the one commit its view writes notes on; "" otherwise. The PR is
// read from the cache when it is there (its view is open).
func (s *Service) PRNoteScope(ctx context.Context, n int, commit string) string {
	pr, _, ok := s.PRDetailsCached(n)
	if !ok {
		var err error
		if pr, err = s.PullRequest(ctx, n); err != nil {
			return ""
		}
	}
	prev, err := s.PRPreview(ctx, pr)
	if err != nil || !prev.Set.OK() || prev.Set.Tip != commit {
		return ""
	}
	return prev.Set.Pair()
}
```

`internal/web/notes.go` `noteReq`:

```go
	// PR is the pull request whose diff the page wrote the note in (0 =
	// none): the server names its scope — the page never sends a PR's names
	// back as refs (previews.js).
	PR int `json:"pr"`
```

`handleNoteAdd`:

```go
	preview := s.notePreview(r.Context(), req.Preview, addr)
	if req.PR > 0 && addr.State == model.StateCommitted {
		preview = s.service().PRNoteScope(r.Context(), req.PR, addr.Commit)
	}
	n := model.Note{
		...
		Preview: preview,
	}
```

`files.js` request body (beside `preview:`):

```js
          preview: noteScopeSpec(ad.ctx.preview),
          pr: (ad.ctx.preview && ad.ctx.preview.pr) || 0,
```

and `noteScopeSpec`'s comment: "A pull request's names are display names, never refs: the request carries its number (`pr`) and the server names the scope."

`addWebNote` (prsend_test.go) adds `"pr":7` to its JSON.

Static guard (in the closest existing files.js source-guard test, else a new one in `pr_note_scope_test.go`):

```go
func TestPageSendsThePRWithANote(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "files.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "pr: (ad.ctx.preview && ad.ctx.preview.pr) || 0,") {
		t.Fatal("files.js's note add does not send the PR number")
	}
}
```

- [ ] **Step 4: Run the web package**

Run: `go test ./internal/web/ -count=1 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 5: Browser probe** (Review Focus: what the user sees). Use the scratchpad probe (`probe.mjs`) pattern: a repo with PR #7 fetched, a plain note on its head written through `/api/notes/add` without `pr`, then the PR's diff opened. **Old build** (`~/go/bin/gg`, main `565eae1b`): the plain note's box is visible in the PR's diff. **New build** (`<worktree>/bin/gg`): it is not, and a note added through the page's own form in that diff is visible. Rebuild `bin/gg` first; assert visibility (box bounding box non-zero), not DOM presence.

- [ ] **Step 6: Commit** — "fix(web): a note written in a pull request records the PR"

---

### Task 6: CLI pins, docs, skill

**Files:**
- Modify tests: `internal/cli/prsend_test.go` (`addCLINote` writes with `--preview main...refs/gg/pr/7` instead of `--rev`)
- Test: `internal/cli/pr_note_scope_test.go` (new) — `gg pr notes 7` lists a stamped note and not a plain one
- Modify: `internal/agentskill/using-gg.md` (~line 838: `gg pr notes` without "(from <origin>)"; one line on writing for a PR), `internal/agentskill/agentskill.go` (`Version` 154 → 155)
- Modify: `README.md:519`, `CHANGELOG.md` (new top section), `docs/CLAUDE-details.md` (PR note rule), `docs/superpowers/specs/2026-10-07-github-write-design.md` (a "Superseded by 2026-10-08-pr-note-scope-design.md" line under §1.4)

- [ ] **Step 1: Write the failing test** — `internal/cli/pr_note_scope_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

// Spec §1: `gg pr notes` lists what the PR's view holds — a note written for
// the PR, never a plain note on its head.
func TestPRNotesListsOnlyThePRsNotes(t *testing.T) {
	dir, head, _ := prSendRepo(t)
	mine := addCLINote(t, dir, head, 5) // --preview main...refs/gg/pr/7
	var out, errb strings.Builder
	if code := Run(dir, []string{"note", "add", "--rev", head, "--file", "big.go", "--new-line", "25",
		"--summary", "a commit note", "--source", "user"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("note add: %s", errb.String())
	}
	out.Reset()
	if code := Run(dir, []string{"pr", "notes", "7"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("pr notes: %s", errb.String())
	}
	if !strings.Contains(out.String(), mine[:8]) || strings.Contains(out.String(), "a commit note") {
		t.Fatalf("pr notes:\n%s", out.String())
	}
}
```

(`prSendRepo` = the fixture builder above `addCLINote` in `prsend_test.go` — use its real name; match how `gg pr notes` prints ids.)

- [ ] **Step 2: Run it** — `go test ./internal/cli/ -run TestPRNotesListsOnlyThePRsNotes -count=1`. Expected: FAIL until `addCLINote` stamps (`--preview main...refs/gg/pr/7 --file big.go --new-line N`, no `--rev`); then PASS. Domain Tasks 1–3 already narrow the read, so the RED here is the fixture: record it.

- [ ] **Step 3: Docs**

- `using-gg.md`: replace the `gg pr notes` bullet's "(with `(from <origin>)` when carried from another commit or the working tree)" with nothing; add after it: "A note or review FOR a pull request is written with `--preview <base>...refs/gg/pr/<n>` (after `gg pr fetch <n>`); it shows only in that PR's view, and a note on a PR's commit written any other way does not show there."
- `agentskill.go`: `const Version = 155`.
- `README.md:519`: `# what the PR's view holds: notes written for it, GitHub threads, draft replies`.
- `CHANGELOG.md` top section "Pull requests show only their own notes": a PR's view shows GitHub's threads, notes written in that PR and reviews run on it — no longer notes from elsewhere whose lines reappear in the PR ("carried"), other notes on its commits, or reviews of its commits; notes written in a PR (terminal UI, gg web, `--preview <base>...refs/gg/pr/<n>`) record the PR; older notes written in a PR view stay on their commit.
- `docs/CLAUDE-details.md`: in the PR notes section, the rule + `PRScopeNumber` / `owns` + "carried notes removed 2026-10-08".
- Old spec §1.4: "> Superseded 2026-10-08 by `2026-10-08-pr-note-scope-design.md`: a PR shows only notes written for it."

- [ ] **Step 4: Full CLI + skill gates** — `go test ./internal/cli/ ./internal/agentskill/ -count=1 2>&1 | tail`. Expected: PASS.

- [ ] **Step 5: Commit** — "docs: a pull request shows only its own notes" (+ the CLI test commit).

---

## Finish

- Race gate: `./test.sh race > <workspace>/race.log 2>&1` — green only on "all green" in the log.
- Final review: one read-only subagent on the most capable model over the branch, with this plan's Review Focus.
- Ask before merging (`gg merge -F <msgfile> --into main feat/pr-note-scope`); after: `./build.sh install`, `./build.sh web`, `gg init --update`, remove the worktree and branch, check main clean.
