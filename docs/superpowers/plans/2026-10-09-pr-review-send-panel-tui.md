# PR review, plan 2 (TUI): ≡ Summary / ≡ Overview rows, PR Reviews rows, the send panel, Copy note link — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the TUI the PR-review surfaces of the spec: the review view's "≡ Summary" row and, for a review with a stored overview, an "≡ Overview" row that opens the overview in the temporary overview's viewer with its anchors at the reviewed tip; the PR file list's Reviews rows with esc back to the PR; one "Send to GitHub…" panel over `PRSendCandidates` replacing every "Send review…" surface; and a link for every note (Copy note link in the `.` menu from both lines next to the box, `L` on the anchor line, `ctrl+l` in View all notes, the review view's line Copy link).

**Architecture:** Everything is in `internal/tui` over plan 1's domain surface — no domain changes except where a test proves a gap (`gotoNote` matching a reply id is the one planned). The stored overview is an `openFile` of a new source kind `srcReviewOverview` with `ov` set and a `tip` stamp: the existing viewer, keys, bands and backspace serve it; `openAnchor` opens a file at the tip through `openFileAtCommit` when the stamp is set. The send panel is a new layer type `sendPanel` (popupMax) that stays on the layer stack under the op's confirm and is removed by `forgeSendFinished` on a change; `forgeSendCmd` is the one send path. The old `sendReviewPopup` shrinks to a `verdictPopup`. Copy note link reuses `notesAtCursor` (its reach already covers the first line below the box) and `Service.NoteLinkText`.

**Tech Stack:** Go 1.26, Bubble Tea, lipgloss, the i18n TOML bundles, the e2e TUI golden harness (`e2e/scenarios/*.toml` + `.screens/`, regenerated with `-update`).

**Spec:** `docs/superpowers/specs/2026-10-09-pr-review-send-panel-design.md` (§3.2, §4.1, §5.3, §7.2, §8 TUI lines, R4, R5, R7, R9, R10, R12, R13). Plan 1 (the Interfaces this plan consumes): `docs/superpowers/plans/2026-10-09-pr-review-send-panel-core.md`.

## Global Constraints

- Worktree `/work/gigagit/.claude/worktrees/pr-review-tui`, branch `feat/pr-review-send-panel-tui`. Every command with absolute paths; never commit in the main checkout. `gg add <files>` then `git commit -F <msgfile>`; never `git add -A`; never push; never publish; agents never send to GitHub.
- Every commit message ends with the two trailers:
  `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01JeY1FfbBboTS6VJ9dgXv3U`.
- Vocabulary: **overview** = the one document kind (temporary via `gg session overview`, or stored in the review's `"overview"` key); the review's text is its **summary**. Never "tour" in new code, strings or docs.
- Every user-visible TUI string goes through `i18n.T` with a literal key present in ALL FOUR bundles `internal/i18n/lang/{ja,ko,zh,ru}.toml` (`[strings]`, inserted near related keys, never re-sorted); deleted keys are deleted from all four (the orphan gate). Gate: `go test ./internal/i18n/ ./internal/tui/ -run 'I18n|EngineProse|DecisionOptionValues|ActionMenuLabels|CheckVerbs|FooterRendersTranslated'`.
- Any test that calls `setStackedPref` calls `tempPromptStore(t, m)` first.
- New `.` menu rows in a diff are compared single vs stacked by `internal/tui/diff_menu_parity_test.go` (the whole menu): a row that reads the stack's top view instead of `v.curNoteView()` fails there.
- Tests: `t.Parallel()` unless the fixture is serial (`prSendModel` sets process env — "Serial" in the doc comment, no `t.Parallel()`). Package tests: `go test ./internal/tui/ -run '<Name>' -count=1`. Before handing over: `./test.sh race` must print the literal line `all green` (run with the sandbox disabled; it is green only on that line).
- e2e goldens: after a scenario's `screen_contains` steps pass, `go test ./e2e/ -run 'TestScenarios/<name>' -update` writes the screens; read the diff of the `.screens` dir before committing it.
- Help AND footer advertise a feature where the footer has room (the full-screen diff's footer is full: help + `.` menu only there).

## Review Focus

Five input classes the spec implies but the tasks' tests do not exercise; each line names the test added to the owning task.

1. **The send panel opened from the PR tab's details (`s`) while the PR's file list is NOT open** — the panel must list the candidates (the diff is fetched) and `enter` on a row must say "open the pull request to see the file", never open a diff of the wrong view (Task 5: `TestSendPanelEnterNeedsTheOpenPR`).
2. **A candidate deleted between the panel's open and ctrl+s** — the planner skips it (`SkipGone`) and the confirm lists it under "-"; the panel must not crash on a stale id and must stay open on abort (Task 6: `TestSendPanelSurvivesAGoneCandidate`).
3. **A PR review of an OLDER tip (the head moved on)** — the Reviews row carries "(older)" and still opens; esc returns to the PR (Task 3: `TestPRReviewsRowOlder`).
4. **An overview anchor into a file the review did not touch** — drawn plain, tab skips it, enter on it (by mouse) says why instead of opening anything (Task 2: `TestStoredOverviewUnresolvedAnchorIsPlainAndSkipped`).
5. **`L` on the first line below a note box** — copies the LINE link, not the note's; the `.` menu there still offers Copy note link (Task 7: `TestLBelowTheBoxCopiesTheLineLink`).

## Rulings written into this plan (spec amendments; the user sees them at review)

- **A1 (spec §4.1 "inside the review view", n/p).** An overview anchor opens the file at the reviewed tip in the FILE VIEWER (`openFileAtCommit`, the Other-notes precedent), with the overview's bands and `n`/`p` exactly as a temporary overview's anchor does (`anchor_bands.go`). It does not open the review's diff: the diff has no band machinery and its `n`/`p` are "next/previous change" by user ruling. The review's remarks are therefore not drawn in the opened file; esc returns to the overview, esc again to the review's file list, where the file row opens the diff with the remarks. Cost if wrong: one more esc for a reader who wants the remarks.
- **A2 (spec §5.3 "Removed: … note-send-drafts").** The spec names `note-send-drafts` with the label "Send my draft review…"; in the code that label belongs to `note-send-review` (which carries both "Send my draft review…" and "Send this AI review…" — the group rows R9 removes). `note-send-drafts` is "Send draft reply" / "Send %d draft replies": one thread's own drafts, the one-thread send R9 keeps. So: `note-send-review` is removed, `note-send-drafts` stays.
- **A3 (spec §7.2 "one row serves both").** A remark keeps its "Copy remark link" row and id row (`copy-remark-link`, `copy-remark-id`, documented in the skills and golden'd). The new `note-copy-link` row is offered for hand-written threads (stored notes and their replies, never a forge thread). On a line carrying both kinds, both rows show. `L` on a thread's lines copies that thread's link: the remark link for a remark, the note link for a note.
- **A4 (spec §7.2 View all notes "`.` row").** The popup has no `.` menu (`.` types into its filter); `ctrl+l` alone copies the link on a thread row, and the key hint says so.
- **A5 (spec §3.2 "Open + Delete only").** The preview review row menu is Open, Copy gg link, Delete since the review-links feature (help.go line 303 documents it); the PR's rows use the same menu, unchanged.
- **A6 (spec §5.3 "handOffToFilesView").** `enter` on a panel row pushes the row's diff ABOVE the panel (`openDiffForFileLine` + a `noteLanding` on the row's thread); esc on the diff returns to the panel with its ticks — the same experience without hiding a layer under a non-layer view. `handOffToFilesView` is for a popup handing off to the files VIEW; here the diff is a layer.
- **A7 (spec §5.3 "on abort the panel stays").** The panel stays on the layer stack while the plan is made and the confirm is answered; `forgeSendFinished` removes it when the result changed something (success, or a review posted whose replies failed); an abort, a refused plan or a failure before any write leaves it as it was.
- **A8 (kept body).** The panel's typed body is kept per PR under the group key `"panel"` (`keptSendBody.group`); the verdict popup keeps its own (`verdict: true`). `keptSendBody.from(req)` answers the panel's request (`Notes` + `BodySet`) with group `"panel"`.
- **A9 (`r` in the overview viewer).** A stored overview has no store id: `r` (Copy anchor reference) and the `overview-ref` row are not offered on it; `y` copies the markdown; the review link row serves references.

## File structure

- `internal/tui/review_view.go` — `reviewViewState.overview`, `reviewTreeLines` rows, the summary popup (renamed), `openReviewOverviewDoc`, `openFileAtCommitLine`.
- `internal/tui/overview.go`, `overview_keys.go`, `anchor_bands.go` — `overview.tip`, `anchor.plain`, the tip-aware `openAnchor` / `stepAnchor` / `paint`.
- `internal/tui/open_file.go`, `open_files.go`, `file_viewer.go`, `agentdocs_track.go`, `steer_overview.go` — `srcReviewOverview` and its guards.
- `internal/tui/content_popup.go`, `files_view.go` — `contentLine.summary` / `contentLine.overviewDoc`.
- `internal/tui/preview_open.go`, `review_delete.go` — `previewReturn.prNumber`, the PR-aware re-open.
- `internal/tui/send_panel.go` (new) + `send_panel_test.go` (new) — the panel.
- `internal/tui/verdict_popup.go` (new, carved out of `send_review_popup.go`, which is deleted) — the Verdict… box and `keptSendBody`.
- `internal/tui/forge_send.go`, `forge_send_menu.go`, `action_menu.go`, `pr_hub.go`, `model.go` — wiring and removals.
- `internal/tui/note_keys.go`, `note_row_menu.go`, `link.go`, `diff_view.go`, `all_notes_popup.go`, `notes_list_popup.go` — note links.
- `internal/tui/help.go` — the changed help lines.
- `internal/i18n/lang/{ja,ko,zh,ru}.toml` — keys.
- `e2e/scenarios/tui_pr_send.toml` (+ screens), `tui_pr_reviews.toml` (new), `tui_stored_overview.toml` (new), `tui_note_link.toml` (new); the six screens holding "≡ Overview".
- `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`.

---

### Task 1: The review view's first row is "≡ Summary"

**Files:**
- Modify: `internal/tui/content_popup.go:71-73` (field), `internal/tui/files_view.go:712,740,1122,1239`, `internal/tui/review_view.go:276-310,354-460`, `internal/tui/help.go:302`
- Modify: `internal/tui/review_view_test.go`, `internal/tui/shelf_note_rows_test.go` (the strings "Overview" / "≡ Overview")
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Modify (goldens): `e2e/scenarios/s108_working_review.toml`, `e2e/scenarios/tui_reading_width.toml`, and the screens `s108_working_review.screens/03-review-view.txt`, `tui_preview_review.screens/03-review-view.txt`, `tui_reading_width.screens/02-overview.txt`, `tui_reading_width.screens/03-stack-overview.txt`, `tui_review_remark_links.screens/02-review.txt`, `tui_review_remark_links.screens/03-file-menu.txt`

**Interfaces:**
- Consumes: `domain.Review.Doc.Summary` (plan 1 Task 1).
- Produces: `contentLine.summary bool` (the "≡ Summary" row; was `overview`), `reviewSummaryPopup` (was `reviewOverviewPopup`), `openReviewSummary()` (was `openReviewOverview`), `reviewSummaryLines(st)` (was `reviewOverviewLines`). Task 2 adds the sibling `contentLine.overviewDoc`.

- [ ] **Step 1: Write the failing test**

In `internal/tui/review_view_test.go`, next to `TestReviewViewTreeHasOverviewRow` (or the test at line ~55 that asserts `vis[0].overview`), add:

```go
// R12: the first row is the review's SUMMARY — the text GitHub gets — under
// that name; "Overview" is the stored overview's row (Task 2).
func TestReviewViewFirstRowIsSummary(t *testing.T) {
	t.Parallel()
	m, _ := openedReviewView(t)
	vis := m.filesView.visible()
	if len(vis) == 0 || !vis[0].summary || vis[0].text != "≡ Summary" {
		t.Fatalf("first row = %+v, want the ≡ Summary row", vis[0])
	}
	for _, l := range vis {
		if strings.Contains(l.text, "≡ Overview") {
			t.Fatalf("a review without a stored overview draws no Overview row: %q", l.text)
		}
	}
	u, _ := m.openDiffForFileLine(vis[0])
	m = u.(Model)
	p := layerOf[*reviewSummaryPopup](m)
	if p == nil {
		t.Fatal("enter on ≡ Summary must open the summary popup")
	}
	if !strings.Contains(ansi.Strip(m.View()), "Review: ") {
		t.Fatal("the summary popup keeps its Review: <label> title")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/pr-review-tui && go test ./internal/tui/ -run TestReviewViewFirstRowIsSummary -count=1`
Expected: build failure `vis[0].summary undefined` / `undefined: reviewSummaryPopup`.

- [ ] **Step 3: Rename**

In `content_popup.go` replace the field:

```go
	// summary is the review view's "≡ Summary" row: enter opens the review's
	// summary (the text GitHub gets), not a file.
	summary bool
```

Replace every `.overview` read of a `contentLine` in `files_view.go` (lines 712, 740, 1122, 1239) with `.summary`. In `review_view.go`:

- `reviewTreeLines`: `out := []contentLine{{text: "≡ " + i18n.T("Summary"), summary: true}}`; its doc comment says "the Summary row".
- Rename `reviewOverviewPopup` → `reviewSummaryPopup`, `openReviewOverview` → `openReviewSummary`, `reviewOverviewLines` → `reviewSummaryLines`; the doc comments say "summary". The `y` key's notice becomes `i18n.T("copied the review summary")`.
- `files_view.go:1239`: `if l.summary { return m.openReviewSummary() }`.
- The package comment at the top of `review_view.go` ("an "≡ Overview" row first") → "an "≡ Summary" row first".

In `help.go` line 302, replace `with ≡ Overview first (enter shows the review's summary rendered,` by `with ≡ Summary first (enter shows the review's summary rendered,` and, after `o lists those notes to open one),` add ` then — when the review stores an overview — ≡ Overview (enter opens it in the overview viewer: tab walks its anchors, enter opens the file at the reviewed commit, backspace comes back),`. (The row exists after Task 2; the help sentence lands now so the i18n key changes once.)

i18n, all four bundles: add `"Summary"` (ja `"要約"`, ko `"요약"`, zh `"摘要"`, ru `"Сводка"`); rename the key `"copied the review overview"` → `"copied the review summary"` (ja `"レビューの要約をコピーしました"`, ko `"리뷰 요약을 복사했습니다"`, zh `"已复制评审摘要"`, ru `"сводка ревью скопирована"`); update the long help key from line 302 in all four (keep each translation's wording, swap the row names and add the Overview clause).

- [ ] **Step 4: Update the existing tests and goldens**

`review_view_test.go`: every `vis[0].overview` → `vis[0].summary`; the string checks `"Overview"` in the row/title assertions → `"Summary"` (lines ~60, 102, 267, 277, 360, 434: read each; the popup TITLE stays `Review: …`, only the row text and the field change). `shelf_note_rows_test.go`: its two `"≡ Overview"` → `"≡ Summary"`. `e2e/scenarios/s108_working_review.toml` and `tui_reading_width.toml`: `screen_contains` entries `"≡ Overview"` → `"≡ Summary"`.

Run: `go test ./internal/tui/ -run 'Review|ShelfNote|I18n|ActionMenuLabels' -count=1`
Expected: PASS.

Run: `go test ./e2e/ -run 'TestScenarios/(s108_working_review|tui_preview_review|tui_reading_width|tui_review_remark_links)' -update -count=1 && git -C /work/gigagit/.claude/worktrees/pr-review-tui diff --stat -- e2e/`
Expected: the six screens change; `git diff -- e2e/` shows only `≡ Overview` → `≡ Summary` lines (read it).

- [ ] **Step 5: Run the whole package and commit**

Run: `go test ./internal/tui/ -count=1 2>&1 | tail -3`
Expected: `ok`.

```bash
cd /work/gigagit/.claude/worktrees/pr-review-tui && gg add internal/tui/content_popup.go internal/tui/files_view.go internal/tui/review_view.go internal/tui/help.go internal/tui/review_view_test.go internal/tui/shelf_note_rows_test.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml e2e/scenarios/ && git commit -F /tmp/claude-1000/-work-gigagit/4f6b5de8-7e75-45d9-9f0c-7798b02ba7f7/scratchpad/msg1.txt
```

Message: `tui(review view): the first row is ≡ Summary (R12)` + body + trailers.

---

### Task 2: The "≡ Overview" row opens the stored overview in the overview viewer

**Files:**
- Modify: `internal/tui/open_file.go:15-24` (source kinds), `internal/tui/open_files.go:280-300` (list source), `internal/tui/file_viewer.go:140-152` (title), `internal/tui/agentdocs_track.go:63-100` (`syncOverviews`), `internal/tui/steer_overview.go:69-76` (`findOverview`)
- Modify: `internal/tui/overview.go` (`overview.tip`, `anchor.plain`, `paint`), `internal/tui/overview_keys.go` (`overviewKey`, `stepAnchor`, `openAnchor`, `overviewRows`)
- Modify: `internal/tui/review_view.go` (`reviewViewMsg.overview`, `reviewViewState.overview`, `reviewTreeLines`, `openReviewOverviewDoc`, `openFileAtCommitLine`), `internal/tui/content_popup.go` (`overviewDoc`), `internal/tui/files_view.go` (the four guards + enter)
- Test: `internal/tui/review_overview_test.go` (new)
- i18n: the four bundles (`"Overview: %s"`, `"anchor %s does not resolve at the reviewed commit"`)
- e2e: `e2e/scenarios/tui_stored_overview.toml` (new) + screens

**Interfaces:**
- Consumes: `domain.ReviewOverview(ctx, id) (domain.OverviewDoc{Text string; Doc markdown.Doc; Anchors []domain.OverviewAnchor{agentdocs.Anchor; OK bool}}, error)`, `domain.ErrNoOverview`; `agentdocs.ParseOverview(text)` (same parser, same anchor order as `OverviewDoc.Anchors`); `reviewViewState.tip`.
- Produces: `srcReviewOverview fileSourceKind`; `overview.tip string`; `anchor.plain bool`; `(m Model) openReviewOverviewDoc() (Model, tea.Cmd)`; `(m Model) openFileAtCommitLine(rev, path string, line, end int) (Model, tea.Cmd)`; `contentLine.overviewDoc bool`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/review_overview_test.go`:

```go
package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/domain"
)

// reviewWithOverview saves a review on the repo's HEAD whose document holds
// an overview with one resolving anchor (a.go:1), one into a file the review
// did not touch (zzz.go:1) and one note: anchor, and opens its review view.
func reviewWithOverview(t *testing.T) (Model, string) {
	t.Helper()
	doc := `{"version":1,"summary":"## Summary\nfine","overview":"Start at [the change](a.go:1), then [outside](zzz.go:1) and [a note](note:t1).","files":[{"path":"a.go","annotations":[{"newRange":[1,1],"summary":"first line"}]}]}`
	m, id := reviewViewModel(t, doc) // review_view_test.go: a review of stackRepoModel's tip commit
	sha := m.commits[0].Hash
	m, cmd := m.openReview(id, "review")
	m = drainCmds(t, m, cmd)
	if m.filesReview == nil || m.filesReview.id != id {
		t.Fatalf("review view not open: %+v", m.filesReview)
	}
	return m, sha
}

func overviewRow(t *testing.T, m Model) contentLine {
	t.Helper()
	for _, l := range m.filesView.visible() {
		if l.overviewDoc {
			return l
		}
	}
	t.Fatal("no ≡ Overview row")
	return contentLine{}
}

// R12: a review with a stored overview draws "≡ Overview" under "≡ Summary";
// enter opens it in the overview viewer with the first anchor current.
func TestStoredOverviewRowOpensTheViewer(t *testing.T) {
	t.Parallel()
	m, _ := reviewWithOverview(t)
	vis := m.filesView.visible()
	if !vis[0].summary || vis[1].text != "≡ Overview" || !vis[1].overviewDoc {
		t.Fatalf("rows = %q / %q", vis[0].text, vis[1].text)
	}
	u, cmd := m.openDiffForFileLine(vis[1])
	m = drainCmds(t, u.(Model), cmd)
	d := topDoc(m)
	if d == nil || d.ov == nil || d.src.kind != srcReviewOverview {
		t.Fatalf("top = %T, want the overview viewer", m.topLayer())
	}
	if d.ov.tip == "" || d.ov.sel != 0 {
		t.Fatalf("tip %q sel %d: the reviewed tip is stamped and the first anchor is current", d.ov.tip, d.ov.sel)
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "the change") || !strings.Contains(view, "Overview: ") {
		t.Fatalf("view:\n%s", view)
	}
}

// Review Focus 4 + R3: an anchor outside the review's files (and a note:
// anchor) is drawn plain — not the gone colour — tab skips it, enter on it
// says why.
func TestStoredOverviewUnresolvedAnchorIsPlainAndSkipped(t *testing.T) {
	t.Parallel()
	m, _ := reviewWithOverview(t)
	u, cmd := m.openDiffForFileLine(overviewRow(t, m))
	m = drainCmds(t, u.(Model), cmd)
	d := topDoc(m)
	if len(d.ov.anchors) != 3 || d.ov.anchors[0].plain || !d.ov.anchors[1].plain || !d.ov.anchors[2].plain {
		t.Fatalf("anchors = %+v", d.ov.anchors)
	}
	if d.ov.anchors[1].missing {
		t.Fatal("unresolved is plain, never 'missing'")
	}
	rows, _ := m.viewerGeom()
	d.stepAnchor(1, rows)
	if d.ov.sel != 0 {
		t.Fatalf("tab wrapped to %d: the only resolved anchor is 0", d.ov.sel)
	}
	u, _ = m.openAnchor(d, 1)
	m = u.(Model)
	if topDoc(m) != d || !strings.Contains(m.statusMsg, "zzz.go:1") {
		t.Fatalf("enter on a plain anchor opened %v / said %q", topDoc(m) != d, m.statusMsg)
	}
}

// R5 + A1: enter on an anchor opens the file AT THE REVIEWED TIP in the
// viewer, the cursor on the line, the overview's band drawn; backspace
// returns to the overview at the same anchor.
func TestStoredOverviewAnchorOpensTheFileAtTheTip(t *testing.T) {
	t.Parallel()
	m, sha := reviewWithOverview(t)
	u, cmd := m.openDiffForFileLine(overviewRow(t, m))
	m = drainCmds(t, u.(Model), cmd)
	ov := topDoc(m)
	fv := m.topLayer().(*fileViewer)
	m, cmd = fv.update(m, tea.KeyMsg{Type: tea.KeyEnter}) // the viewer's enter → openAnchor
	m = drainCmds(t, m, cmd)
	d := topDoc(m)
	if d == nil || d == ov || d.src.kind != srcCommit || d.src.rev != sha || d.path != "a.go" {
		t.Fatalf("opened %+v", d)
	}
	if d.from != ov || d.anchorCur != "a.go:1" || len(d.bands()) != 1 || d.p.cur != 0 {
		t.Fatalf("from %v cur %q bands %d line %d", d.from == ov, d.anchorCur, len(d.bands()), d.p.cur)
	}
	m, _, ok := m.anchorBack(d)
	if !ok || topDoc(m) != ov || ov.ov.sel != 0 {
		t.Fatalf("backspace: ok %v top %v sel %d", ok, topDoc(m) == ov, ov.ov.sel)
	}
}

// The stored overview is not an agent's: the store sync must not close it,
// gg session overview must not find it, and y copies its markdown.
func TestStoredOverviewIsNotAStoreOverview(t *testing.T) {
	t.Parallel()
	m, _ := reviewWithOverview(t)
	u, cmd := m.openDiffForFileLine(overviewRow(t, m))
	m = drainCmds(t, u.(Model), cmd)
	d := topDoc(m)
	m = m.syncOverviews()
	if topDoc(m) != d {
		t.Fatal("syncOverviews closed the stored overview")
	}
	if f, _ := m.findOverview(d.id()); f != nil {
		t.Fatal("findOverview must not answer a review's overview")
	}
	rows := m.overviewRows()
	for _, r := range rows {
		if r.id == "overview-ref" {
			t.Fatal("no anchor reference for a stored overview (A9)")
		}
	}
	_, c := m.topLayer().(*fileViewer).update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if c == nil {
		t.Fatal("y copies the overview's markdown")
	}
}
```

(`reviewViewModel` saves the document on `stackRepoModel`'s tip commit with `domain.ReviewTarget{Kind: domain.ReviewRange, Range: sha + "^.." + sha}` — `internal/tui/review_view_test.go:20-35`; `a.go` is a file of that commit, `zzz.go` is not.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'StoredOverview' -count=1`
Expected: build failure on `overviewDoc`, `srcReviewOverview`, `ov.tip`, `anchor.plain`.

- [ ] **Step 3: The source kind and its guards**

`open_file.go`: add after `srcOverview`:

```go
	srcReviewOverview                 // a review's STORED overview (openFile.ov, ov.tip = the reviewed tip); rev = review id, path = "overview-<id>.md"
```

`open_files.go` (the open-files listing, ~line 295): `case srcOverview, srcReviewOverview: f.Source, f.Title = "overview", d.title`.
`file_viewer.go` `title()`: add `srcReviewOverview` to the `srcExternal, srcNote, srcOverview` case.
`agentdocs_track.go` `syncOverviews`: the first loop skips `if d.ov == nil || d.src.kind != srcOverview { continue }` — a review's overview is not the store's.
`steer_overview.go` `findOverview`: `if d.ov != nil && d.src.kind == srcOverview && d.id() == id`.

- [ ] **Step 4: The overview struct learns the tip and plain anchors**

`overview.go`:

```go
type anchor struct {
	dest    string
	target  agentdocs.Anchor
	spans   []anchorSpan
	missing bool // its file or note was not found when last checked
	// plain: a stored overview's anchor the review cannot open — a path
	// outside the review's files, a range past the file, a note: anchor
	// (R3). Drawn as text, never current, enter says why.
	plain bool
}

type overview struct {
	text    string
	anchors []anchor
	sel     int
	w       int
	mode    dispMode
	closed  bool
	// tip is the commit a STORED overview's anchors open at ("" = the
	// working tree, a temporary overview); set with srcReviewOverview.
	tip string
}
```

`paint`: a `plain` anchor's spans get `syntax.Class(0)` (plain text) whatever `sel`/`missing` say:

```go
	for i, a := range ov.anchors {
		c := mdAnchor
		switch {
		case a.plain:
			c = 0
		case i == ov.sel:
			c = mdAnchorSel
		case a.missing:
			c = mdAnchorGone
		}
		…
```

`layOut`: when anchors are re-laid for the same text, carry `plain` along with `missing` (the `len(anchors) == len(ov.anchors)` loop copies both).

`overview_keys.go`:
- `stepAnchor`: the search loop skips `as[i].plain`: `if len(as[i].spans) > 0 && !as[i].plain { d.selectAnchor(i, rows); return }`; the "first at or below the top" scan ignores plain ones the same way.
- `overviewKey` `"r"`: `if d.ov.sel < 0 || d.ov.tip != "" { return m, nil, true }`.
- `overviewRows`: the `overview-ref` row only `if d.ov.tip == ""`.
- `openAnchor`, after `gone` is defined and BEFORE the note branch:

```go
	if ov.ov.tip != "" { // a stored overview: files at the reviewed tip, nothing else
		if a.plain {
			m.statusMsg = i18n.T("anchor %s does not resolve at the reviewed commit", a.dest)
			return m, nil
		}
		m, cmd := m.openFileAtCommitLine(ov.ov.tip, a.target.Path, a.target.Start, a.target.End)
		if d := topDoc(m); d != nil {
			d.from, d.backgrounded, d.anchorCur = ov, true, a.dest
		}
		return m, cmd
	}
```

and `mark` must not touch the store for a review overview: `if ov.src.kind == srcOverview { m.docs.SetAnchorMissing(…) }`.

- [ ] **Step 5: The review view's row and the opener**

`content_popup.go`, under `summary`:

```go
	// overviewDoc is the review view's "≡ Overview" row (a review with a
	// stored overview): enter opens it in the overview viewer, not a file.
	overviewDoc bool
```

`files_view.go`: the four `.summary` guards from Task 1 become `(… .summary || … .overviewDoc)`; the enter arm: `if l.overviewDoc { return m.openReviewOverviewDoc() }` right after the `l.summary` arm.

`review_view.go`:
- `reviewViewMsg` gains `overview *domain.OverviewDoc`; in `openReviewLanding`'s read, after `ReviewOtherNotes`: `if out.review.Doc.Overview != "" { if od, oerr := svc.ReviewOverview(ctx, id); oerr == nil { out.overview = &od } }` (an error leaves no row — the summary still opens; the ledger notes it).
- `reviewViewState` gains `overview *domain.OverviewDoc`, set from the msg at line ~175.
- `reviewTreeLines`: after the Summary row, `if st.overview != nil { out = append(out, contentLine{text: "≡ " + i18n.T("Overview"), overviewDoc: true}) }`.
- New:

```go
// openReviewOverviewDoc opens the review's stored overview in the overview
// viewer (R12, R5): the same layout, anchors and keys as a temporary one,
// its anchors opening files at the reviewed tip (overview.tip). The
// document is registered among the open files (ctrl+\ lists it; backspace
// in a file it opened finds the way back), under its own source kind so the
// agent-docs sync never touches it. The first resolved anchor is current.
func (m Model) openReviewOverviewDoc() (Model, tea.Cmd) {
	st := m.filesReview
	if st == nil || st.overview == nil {
		return m, nil
	}
	src := fileSource{kind: srcReviewOverview, rev: st.id}
	path := "overview-" + st.id + ".md"
	if d := m.openFiles.find(m.currentWorktree, docKey(src, path)); d != nil {
		nm, cmd := m.bringToFront(d)
		return nm, cmd
	}
	d := m.newOpenFile(src, path)
	d.title = i18n.T("Overview: %s", reviewLabel(st.review))
	d.p.prose, d.p.mode = true, modeScroll
	d.ov = &overview{text: st.overview.Text, sel: -1, tip: st.tip}
	rows, inner := m.viewerGeom()
	d.layOut(rows, m.overviewWidth(inner))
	if len(d.ov.anchors) == len(st.overview.Anchors) { // the same parser, the same order
		for i := range d.ov.anchors {
			d.ov.anchors[i].plain = !st.overview.Anchors[i].OK
		}
	}
	d.stepAnchor(1, rows) // the first resolved anchor is current on open
	m = m.pushLayer(&fileViewer{d})
	m = m.registerDoc(d)
	return m, nil
}

// openFileAtCommitLine is openFileAtCommit landing on line (1-based, 0 =
// top), lines line..end selected as a link's range is.
func (m Model) openFileAtCommitLine(rev, path string, line, end int) (Model, tea.Cmd) {
	m, cmd := m.openFileAtCommit(rev, path)
	if d := topDoc(m); d != nil && line > 0 {
		d.pendingLine, d.pendingEnd = line, end
	}
	return m, cmd
}
```

(`openFileAtCommit` calls `loadDoc` before returning; `pendingLine` is consumed by `landPendingLine` when the content lands, so setting it right after is in time — `open_file.go:181-220`. If `loadDoc` has already filled a cached document synchronously, set the cursor directly: `d.p.cur = line-1`. Read `loadDoc` and ledger which it is.)

For a working review (`st.tip == ""`, the review of uncommitted changes), `ReviewOverview` resolves against the working tree: leave `tip` empty — the temporary path (`os.Stat` + `openFileViewerEv`) is then exactly right. `reviewLabel` says "working changes".

i18n, four bundles: `"Overview: %s"` (ja `"概要: %s"`, ko `"개요: %s"`, zh `"概览: %s"`, ru `"Обзор: %s"`), `"anchor %s does not resolve at the reviewed commit"` (ja `"アンカー %s はレビュー対象のコミットで解決できません"`, ko `"앵커 %s 은(는) 리뷰한 커밋에서 찾을 수 없습니다"`, zh `"锚点 %s 在评审的提交中无法解析"`, ru `"якорь %s не находится в проверенном коммите"`).

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/tui/ -run 'StoredOverview|Overview|Review|I18n|ActionMenuLabels' -count=1`
Expected: PASS.

- [ ] **Step 7: The e2e golden**

Create `e2e/scenarios/tui_stored_overview.toml` from `tui_review_remark_links.toml`'s shape (a commit review saved with `review save`): steps write `a.go` with 5 lines, commit; `[[run]] cmd = ["review", "save", "gg://{{cwd}}@{{rev:HEAD}}", "--agent", "Claude Code", "--stdin"]` with stdin `{"version":1,"summary":"## Summary\nfine","overview":"Start at [line two](a.go:2), then [outside](zzz.go:1).","files":[{"path":"a.go","annotations":[{"newRange":[2,2],"summary":"why two?"}]}]}` (the review link form for a commit is what `gg link` prints: check `s108_review_links.toml`'s save step and copy its link form; ledger the form used). TUI steps (`size = "160x40"`): `files` (`right`, `enter` → the commit's files; `screen_contains = ["Reviews"]`), `review` (enter on the review row → `["≡ Summary", "≡ Overview", "a.go"]`), `overview` (`down`, `enter` → `["Overview: ", "line two", "outside"]`), `anchor` (`enter` → `["View a.go @", "┃"]`), `back` (`backspace` → `["Overview: "]`).

Run: `go test ./e2e/ -run 'TestScenarios/tui_stored_overview' -update -count=1 && go test ./e2e/ -run 'TestScenarios/tui_stored_overview' -count=1`
Expected: PASS twice; read the five screens.

- [ ] **Step 8: Commit**

```bash
gg add internal/tui/open_file.go internal/tui/open_files.go internal/tui/file_viewer.go internal/tui/agentdocs_track.go internal/tui/steer_overview.go internal/tui/overview.go internal/tui/overview_keys.go internal/tui/review_view.go internal/tui/content_popup.go internal/tui/files_view.go internal/tui/review_overview_test.go internal/i18n/lang/ e2e/scenarios/tui_stored_overview.toml e2e/scenarios/tui_stored_overview.screens && git commit -F <msgfile>
```

Message: `tui(review view): ≡ Overview opens the stored overview in the overview viewer, anchors at the reviewed tip (R5, R12)`.

---

### Task 3: A PR's Reviews rows, and esc back to the pull request

**Files:**
- Modify: `internal/tui/review_view.go:48-67` (`previewReturn`), `internal/tui/review_delete.go:133-150` (`leaveReviewView`), `internal/tui/preview_open.go:63-100` (`resolvePreviewCmd` takes the title and PR)
- Test: `internal/tui/pr_reviews_rows_test.go` (new, serial: `prSendModel`)
- e2e: `e2e/scenarios/tui_pr_reviews.toml` (new) + screens

**Interfaces:**
- Consumes: `domain.PreviewReviews(ctx, set)` for a PR set (plan 1 Task 6: `ReviewHead{ID, Commit, Branch, Agent, Summary, Created, Older, …}`), `domain.ScopeReviewTarget(set)`, `domain.SaveReview`; `previewReviewLines`, `openReviewWith(id, title, back, bp)`, `prSendModel`, `openPRPreviewCmd`.
- Produces: `previewReturn.prNumber int`; `(m Model) openPreviewReturnCmd(bp *previewReturn, landNote string) tea.Cmd`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/pr_reviews_rows_test.go`:

```go
package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// savePRReviewTUI stores a review document on PR #7's scope (what /gg-review
// does on a pull request link) and returns its id.
func savePRReviewTUI(t *testing.T, m Model, doc string) string {
	t.Helper()
	ctx := context.Background()
	set, err := m.svc.PreviewNotes(ctx, "refs/gg/pr/7", "main")
	if err != nil || !set.OK() {
		t.Fatalf("PR note set: %+v err %v", set, err)
	}
	id, _, err := m.svc.SaveReview(ctx, domain.SaveReview{Target: domain.ScopeReviewTarget(set), Agent: "claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// openPR7 opens PR #7's file list (its comments answered by the fake gh).
func openPR7(t *testing.T, m Model) Model {
	t.Helper()
	open := m.openPRPreviewCmd(model.PullRequest{Number: 7, State: "open", Target: "main"})().(previewOpenMsg)
	u, cmd := m.Update(open)
	m = drainCmds(t, u.(Model), cmd)
	if m.previewOpen == nil || m.previewOpen.prNumber != 7 {
		t.Fatalf("previewOpen = %+v", m.previewOpen)
	}
	return m
}

func reviewRow(t *testing.T, m Model, id string) contentLine {
	t.Helper()
	for _, l := range m.filesView.visible() {
		if l.noteID == id {
			return l
		}
	}
	t.Fatalf("no row for review %s in:\n%s", id, m.View())
	return contentLine{}
}

// R4: a review stored on the PR is a row under Reviews in the PR's file
// list; enter opens the review view titled with the PR; esc returns to the
// PR's file list (still the PR: its number, its title), the cursor on the row.
// Serial: env (prSendModel).
func TestPRReviewsRowOpensAndReturns(t *testing.T) {
	m, _, _ := prSendModel(t)
	id := savePRReviewTUI(t, m, `{"version":1,"summary":"## Summary\nfine","files":[{"path":"big.go","annotations":[{"newRange":[5,5],"summary":"why?"}]}]}`)
	m = openPR7(t, m)
	l := reviewRow(t, m, id)
	if !strings.Contains(l.text, "claude") {
		t.Fatalf("row %q", l.text)
	}
	u, cmd := m.openDiffForFileLine(l)
	m = drainCmds(t, u.(Model), cmd)
	if m.filesReview == nil || m.filesReview.id != id || !strings.Contains(m.filesTitle, "#7") {
		t.Fatalf("review view: %+v title %q", m.filesReview, m.filesTitle)
	}
	u2, cmd := m.leaveReviewView()
	m = drainCmds(t, u2, cmd)
	if m.filesReview != nil || m.previewOpen == nil || m.previewOpen.prNumber != 7 || !strings.Contains(m.filesTitle, "#7") {
		t.Fatalf("after esc: review %v previewOpen %+v title %q", m.filesReview != nil, m.previewOpen, m.filesTitle)
	}
	vis := m.filesView.visible()
	if m.filesView.sel < 0 || m.filesView.sel >= len(vis) || vis[m.filesView.sel].noteID != id {
		t.Fatalf("cursor not on the review's row: sel %d", m.filesView.sel)
	}
}

// Review Focus 3: a review of an older head still lists, says (older), opens.
func TestPRReviewsRowOlder(t *testing.T) {
	m, dir, _ := prSendModel(t)
	id := savePRReviewTUI(t, m, `{"version":1,"summary":"old","files":[{"path":"big.go","annotations":[{"newRange":[5,5],"summary":"why?"}]}]}`)
	// The PR head moves on (a new commit on feat; refs/gg/pr/7 follows).
	runGit(t, dir, "checkout", "-q", "feat")
	writeFileT(t, dir, "big.go", strings.Repeat("x\n", 31))
	runGit(t, dir, "add", "big.go")
	runGit(t, dir, "commit", "-q", "-m", "more")
	runGit(t, dir, "update-ref", "refs/gg/pr/7", gitOut(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "checkout", "-q", "main")
	m = openPR7(t, m)
	l := reviewRow(t, m, id)
	if !strings.Contains(l.text, "(older)") {
		t.Fatalf("row %q lacks (older)", l.text)
	}
}
```

(`writeFileT`: if no such helper exists in the tui tests, write the file with `os.WriteFile(filepath.Join(dir, "big.go"), …)` inline. The fake gh's `headRefOid` is the OLD head — the prSendFixtures seed; re-seed with `forgetest.Seed(t, filepath.Join(dir, ".git", "fakegh"), prSendFixtures(newHead))` after moving the ref so the PR read agrees with the ref; `PreviewReviews` classifies against the set's tip.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'TestPRReviewsRow' -count=1`
Expected: the first test FAILS at "after esc": `previewOpen.prNumber` is 0 / the title lacks `#7` (`closeFilesView` dropped `previewOpen` when the review view opened, and `resolvePreviewCmd` only keeps a title for a pair that is still open). The second passes or fails on `(older)` — if it passes at once, that is a finding to ledger (plan 1 already classifies).

- [ ] **Step 3: Carry the PR through the return**

`review_view.go`: `previewReturn` gains `prNumber int` (comment: "the pull request the preview shows (0 = an ordinary preview): the re-open keeps its title and number"); `previewReturnHere` sets `prNumber: po.prNumber` from `m.previewOpen`.

`preview_open.go`: add

```go
// openPreviewReturnCmd re-opens the preview a review view came from, with
// the title and pull request it was opened as (a PR's file list must come
// back as the PR, not as "Merge preview: refs/gg/pr/7 → main"), the cursor
// landing on landNote's row.
func (m Model) openPreviewReturnCmd(bp *previewReturn, landNote string) tea.Cmd {
	return m.resolvePreviewCmdAs(bp.id, bp.source, bp.target, bp.title, bp.prNumber, "", "", landNote)
}
```

and refactor `resolvePreviewCmd(id, source, target, keepPath, moved, landNote)` so its body is `resolvePreviewCmdAs(id, source, target, title, prNumber, keepPath, moved, landNote)` with the current "keep the open pair's title" logic computing `title, prNumber` in the thin wrapper. (`bp.title` is `m.filesTitle` at the time: for a PR that is `PR #7 · t` — check `openPRPreviewCmd` / the title stamp so the same string comes back; if `previewOpenMsg.title` is the raw title and the files view prefixes it, store the raw `po.title` in `previewReturn` instead of `m.filesTitle` — ledger which.)

`review_delete.go` `leaveReviewView`: `return m, m.openPreviewReturnCmd(bp, id)` in place of `openPreviewLandingCmd(bp.id, bp.source, bp.target, id)`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'TestPRReviewsRow|Preview|Review' -count=1`
Expected: PASS.

- [ ] **Step 5: The e2e golden**

Create `e2e/scenarios/tui_pr_reviews.toml`: copy `tui_pr_send.toml`'s `[input]` (the PR #7 fixture; drop `snapshot-7-sent.json`) and replace the `[[run]]` by `cmd = ["review", "save", "gg://{{cwd}}@main...refs/gg/pr/7", "--agent", "Claude Code", "--stdin"]`, stdin `{"version":1,"summary":"## Summary\nfine","files":[{"path":"big.txt","annotations":[{"newRange":[5,5],"summary":"why changed?"}]}]}`, exit 0 (the PR's scope link is what `gg pr` prints as the PR link; if `review save` refuses this form, use the form plan 1's CLI test `sendPRRepo`/`saveCLIReview` used — `grep -n saveCLIReview internal/cli/*_test.go` — and ledger it). Steps: `pr-tab` (four `C-right`, `["#7"]`), `pr-files` (`enter`, `["PR #7", "Reviews", "Claude Code", "big.txt"]`), `review` (`enter` on the review row — it is the first row under the heading, the cursor starts on it: `["≡ Summary", "big.txt"]`), `back` (`esc`, `["PR #7", "Reviews"]`).

Run: `go test ./e2e/ -run 'TestScenarios/tui_pr_reviews' -update -count=1 && go test ./e2e/ -run 'TestScenarios/tui_pr_reviews' -count=1`
Expected: PASS; read the screens (the `back` screen shows the PR title row and the cursor on the review row).

- [ ] **Step 6: Commit**

```bash
gg add internal/tui/review_view.go internal/tui/review_delete.go internal/tui/preview_open.go internal/tui/pr_reviews_rows_test.go e2e/scenarios/tui_pr_reviews.toml e2e/scenarios/tui_pr_reviews.screens && git commit -F <msgfile>
```

Message: `tui(pr): a pull request's stored reviews are rows of its file list; esc from the review returns to the PR (R4)`.

---

### Task 4: A send that posted and then failed still counts as the user's change; the kept body is keyed per box

**Files:**
- Modify: `internal/tui/forge_send.go:255-280` (`forgeSendFinished`), `internal/tui/send_review_popup.go:220-260` (`keptSendBody.from`, `keptBodyFor`)
- Test: `internal/tui/forge_send_kept_test.go` (add two tests)

**Interfaces:**
- Consumes: `engine.Result.Changed`; plan 1 Task 7: a review whose `Then` replies failed returns `(Result{Changed: true}, err)` wrapped "the review was posted; replies: …", with NO `Done` event for the replies.
- Produces: `forgeSendFinished` treats `res.Changed` as "my own change" whatever `err` is (the PR re-read, `prOwnSend`, the kept body cleared); `keptSendBody.from` answers a `Notes`+`BodySet` request with group `"panel"` (Task 5 writes it); constant `sendGroupPanel = "panel"`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/forge_send_kept_test.go`:

```go
// Plan 1 Task 7: a review posted whose draft replies then failed comes back
// as (Changed, err) with no Done for the replies. It is still the user's own
// change: the PR is re-read as "my send" (not "updated") and the typed body,
// which reached GitHub, is dropped.
func TestAPostedReviewWithFailedRepliesIsStillMyChange(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.prReadSeq = 3
	sent := domain.PRSendRequest{PR: 7, Notes: []string{"n1"}, Verdict: true, Body: "typed", BodySet: true}
	m.keptSendBody = &keptSendBody{pr: 7, group: sendGroupPanel, text: "typed"}
	m, _ = m.forgeSendFinished(&forgeSendState{pr: 7, req: sent}, engine.Result{Changed: true}, errors.New("the review was posted; replies: network down"))
	if m.prOwnSend != 7 || m.prOwnSendSeq != 3 {
		t.Fatalf("own send not recorded: pr %d seq %d", m.prOwnSend, m.prOwnSendSeq)
	}
	if m.keptSendBody != nil {
		t.Fatal("the body reached GitHub: nothing to keep")
	}
}

// A8: the panel's typed body is kept under its own key, per PR; the verdict
// box keeps its own; neither claims the other's text.
func TestPanelAndVerdictKeepSeparateBodies(t *testing.T) {
	t.Parallel()
	var m Model
	m.keptSendBody = &keptSendBody{pr: 7, group: sendGroupPanel, text: "panel text"}
	if got, ok := m.keptBodyFor(7, sendGroupPanel, false); !ok || got != "panel text" {
		t.Fatalf("panel body = %q %v", got, ok)
	}
	if _, ok := m.keptBodyFor(7, "", true); ok {
		t.Fatal("the verdict box must not see the panel's text")
	}
	if _, ok := m.keptBodyFor(8, sendGroupPanel, false); ok {
		t.Fatal("another PR must not see it")
	}
	panelReq := domain.PRSendRequest{PR: 7, Notes: []string{"n1"}, Verdict: true, Body: "panel text", BodySet: true}
	if !m.keptSendBody.from(panelReq) {
		t.Fatal("the panel's own send clears its kept body")
	}
	if m.keptSendBody.from(domain.PRSendRequest{PR: 7, Verdict: true, Body: "x", BodySet: true}) {
		t.Fatal("a verdict-only send is not the panel's")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'TestAPostedReviewWithFailedRepliesIsStillMyChange|TestPanelAndVerdictKeepSeparateBodies' -count=1`
Expected: build failure `undefined: sendGroupPanel`; after defining it as a stopgap `const` locally, the first test fails on `prOwnSend` (0), the second on "the panel's own send clears its kept body".

- [ ] **Step 3: Implement**

`forge_send.go`:

```go
	if res.Changed && fs.pr != 0 { // my own change (F1), even when a later step failed: the review is posted
		m.prOwnSend, m.prOwnSendSeq = fs.pr, m.prReadSeq
		if m.keptSendBody.from(fs.req) {
			m.keptSendBody = nil // the typed body reached GitHub
		}
	}
```

`send_review_popup.go` (the `keptSendBody` block; Task 6 moves it to `verdict_popup.go` unchanged):

```go
// sendGroupPanel keys the send panel's typed body in keptSendBody: one per
// PR, whatever is ticked (A8).
const sendGroupPanel = "panel"

func (k *keptSendBody) from(req domain.PRSendRequest) bool {
	if k == nil || !req.BodySet || k.pr != req.PR {
		return false
	}
	switch {
	case len(req.Notes) > 0:
		return k.group == sendGroupPanel && !k.verdict
	case req.Verdict:
		return k.verdict
	case req.Mine:
		return k.group == domain.GroupMine
	}
	return req.Review != "" && k.group == "review:"+req.Review
}
```

(`keptBodyFor` is unchanged: it already keys on pr, group and verdict.)

- [ ] **Step 4: Run the package's send tests**

Run: `go test ./internal/tui/ -run 'Kept|Send|Forge' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/tui/forge_send.go internal/tui/send_review_popup.go internal/tui/forge_send_kept_test.go && git commit -F <msgfile>
```

Message: `tui(send): a posted review whose replies failed is still my change; the panel's body has its own kept key`.

---

### Task 5: The send panel (`sendPanel`) over `PRSendCandidates`

**Files:**
- Create: `internal/tui/send_panel.go`, `internal/tui/send_panel_test.go`
- Modify: `internal/tui/notes_list_popup.go:360-400` (`gotoNote` matches a reply id)
- i18n: four bundles

**Interfaces:**
- Consumes: `domain.PRSendCandidates(ctx, n) (domain.SendCandidates{PR, Head, Groups []SendCandidateGroup{ID, Kind "review"|"mine"|"replies", Agent, Title, Created, Slot, Rows []SendCandidate{ID, Kind "note"|"remark"|"reply", Severity, Path, Range [2]int, Side, Summary, Rationale, Sync, Code []string, Skip}}}, error)`, `domain.GroupMine`, `domain.GroupReplies`, `domain.PRSendRequest{PR, Notes, Verdict, Body, BodySet, BodyFrom}`; `forgeSendCmd`, `sendSkipReasonText`, `groupBarStyle(slot)`, `popupMax`, `popupResolveWidth`, `popupWideInnerWidth`, `popupTextWidth`, `popupBox`, `packHints`, `renderWindow`/`winRow`/`winOpts`, `viewFieldWindow`, `newTextField`, `sendGroupPanel`, `keptBodyFor`, `noteLanding`, `openDiffForFileLine`, `clock.Now()`.
- Produces: `type sendPanel struct` (a layer: `update(m, msg) (Model, tea.Cmd)`, `render(m, below) string`); `(m Model) openSendPanel(pr int) (Model, tea.Cmd)`; `sendPanelMsg{gen, pr int; cands domain.SendCandidates; err error}`; `(m Model) handleSendPanel(msg sendPanelMsg) (Model, tea.Cmd)`; `(p *sendPanel) request() (domain.PRSendRequest, bool)`; `(p *sendPanel) bodyLabel() string`; `(m Model) gotoNote(id)` lands on the thread holding id (root or reply).

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/send_panel_test.go`:

```go
package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// panelCands: one AI review (two remarks, one skipped), my notes (one), one
// draft reply — the spec's §5.3 picture.
func panelCands() domain.SendCandidates {
	t0 := time.Date(2026, 10, 9, 14, 2, 0, 0, time.UTC)
	return domain.SendCandidates{PR: 42, Head: "abc", Groups: []domain.SendCandidateGroup{
		{ID: "review:r1", Kind: "review", Agent: "Claude Code", Title: "two nits", Created: t0, Slot: 2, Rows: []domain.SendCandidate{
			{ID: "review:r1:1", Kind: "remark", Severity: "high", Path: "internal/git/pull.go", Range: [2]int{120, 134}, Side: "new", Summary: "lock released twice", Code: []string{"a", "b"}},
			{ID: "review:r1:2", Kind: "remark", Severity: "low", Path: "internal/tui/x.go", Range: [2]int{5, 5}, Side: "new", Summary: "nit", Skip: domain.SkipLinesChanged},
		}},
		{ID: domain.GroupMine, Kind: "mine", Slot: 1, Rows: []domain.SendCandidate{
			{ID: "n1", Kind: "note", Path: "README.md", Range: [2]int{12, 12}, Side: "new", Summary: "typo"},
		}},
		{ID: domain.GroupReplies, Kind: "replies", Rows: []domain.SendCandidate{
			{ID: "d1", Kind: "reply", Path: "internal/git/pull.go", Range: [2]int{120, 120}, Side: "new", Summary: "addressed"},
		}},
	}}
}

func panelModel(t *testing.T) (Model, *sendPanel) {
	t.Helper()
	m := prDiffModel(t)
	m, _ = m.handleSendPanel(sendPanelMsg{gen: m.forgeGen, pr: 7, cands: panelCands()})
	p := layerOf[*sendPanel](m)
	if p == nil {
		t.Fatal("no panel")
	}
	return m, p
}

func key(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// R7: nothing ticked on open; the title, the groups and every row show;
// a skip row shows its reason; the code excerpt sits under the current row.
func TestSendPanelOpensWithNothingTicked(t *testing.T) {
	t.Parallel()
	m, p := panelModel(t)
	if len(p.ticked) != 0 {
		t.Fatalf("ticked on open: %v", p.ticked)
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Send to GitHub — #7", "0 ticked", "Review (AI)", "Claude Code", "My notes", "Draft replies",
		"[ ] high", "internal/git/pull.go:120-134", "lock released twice", "[ ] low", "its lines changed", "README.md:12", "Body: none",
		"[space] tick", "[ctrl+s] send"} {
		if !strings.Contains(view, want) {
			t.Errorf("panel lacks %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "    a") { // the first remark is current: its code shows
		t.Errorf("no code excerpt under the current row:\n%s", view)
	}
}

// space ticks a row, never a skip row (the bottom bar says why); a toggles
// the group's sendable rows; the count follows.
func TestSendPanelTicks(t *testing.T) {
	t.Parallel()
	m, p := panelModel(t)
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace})
	if !p.ticked["review:r1:1"] || p.tickedCount() != 1 {
		t.Fatalf("space: %v", p.ticked)
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyDown}) // the skip row
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace})
	if p.ticked["review:r1:2"] || !strings.Contains(p.notice, "its lines changed") {
		t.Fatalf("a skip row ticked / notice %q", p.notice)
	}
	m, _ = p.update(m, key("a")) // all/none in the review group: it had one of one sendable ticked → none
	if p.tickedCount() != 0 {
		t.Fatalf("a with all ticked must untick: %v", p.ticked)
	}
	m, _ = p.update(m, key("a"))
	if !p.ticked["review:r1:1"] || p.ticked["review:r1:2"] || p.tickedCount() != 1 {
		t.Fatalf("a again ticks every sendable row of the group: %v", p.ticked)
	}
	if !strings.Contains(ansi.Strip(m.View()), "1 ticked") {
		t.Fatal("count")
	}
}

// b cycles none → each ticked AI review's text (newest first) → typed → none;
// with no AI review ticked, none → typed. e opens the body box prefilled.
func TestSendPanelBodyCycle(t *testing.T) {
	t.Parallel()
	m, p := panelModel(t)
	m, _ = p.update(m, key("b"))
	if p.bodyLabel() != "typed" {
		t.Fatalf("no review ticked: none → %q, want typed", p.bodyLabel())
	}
	m, _ = p.update(m, key("b"))
	if p.bodyLabel() != "none" {
		t.Fatalf("→ %q, want none", p.bodyLabel())
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace}) // tick the review's remark
	m, _ = p.update(m, key("b"))
	if !strings.HasPrefix(p.bodyLabel(), "review text (Claude Code") {
		t.Fatalf("→ %q, want the review's text", p.bodyLabel())
	}
	m, _ = p.update(m, key("e"))
	box := layerOf[*sendPanelBody](m)
	if box == nil {
		t.Fatal("e opens the body box")
	}
	box.body = newTextField("my own words")
	m, _ = box.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if p.bodyLabel() != "typed" || p.typed != "my own words" {
		t.Fatalf("after e: %q / %q", p.bodyLabel(), p.typed)
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace}) // untick the review: a review body falls back to none, typed stays
	if p.bodyLabel() != "typed" {
		t.Fatalf("typed survives an untick: %q", p.bodyLabel())
	}
}

// ctrl+s: nothing ticked says so; else one Notes request with Verdict and
// the chosen body, handed to forgeSendCmd (the panel stays until the op
// reports, A7).
func TestSendPanelRequest(t *testing.T) {
	t.Parallel()
	m, p := panelModel(t)
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if !strings.Contains(p.notice, "tick something to send") || layerOf[*sendPanel](m) == nil {
		t.Fatalf("notice %q", p.notice)
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace})
	for i := 0; i < 3; i++ {
		m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace}) // the draft reply
	m, _ = p.update(m, key("b"))                        // review text
	req, ok := p.request()
	if !ok || req.PR != 7 || !req.Verdict || strings.Join(req.Notes, ",") != "review:r1:1,d1" || req.BodyFrom != "r1" || req.BodySet {
		t.Fatalf("req = %+v ok %v", req, ok)
	}
	m, _ = p.update(m, key("b"))
	p.typed = "typed words"
	req, _ = p.request()
	if req.BodyFrom != "" || !req.BodySet || req.Body != "typed words" {
		t.Fatalf("typed req = %+v", req)
	}
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil || layerOf[*sendPanel](m) == nil || !strings.Contains(m.statusMsg, "preparing the send to #7") {
		t.Fatalf("ctrl+s: cmd %v panel %v status %q", cmd != nil, layerOf[*sendPanel](m) != nil, m.statusMsg)
	}
	if k := m.keptSendBody; k == nil || k.group != sendGroupPanel || k.text != "typed words" {
		t.Fatalf("kept = %+v", m.keptSendBody)
	}
}

// A7: the panel closes when the send changed something; an abort (nothing
// changed) leaves it with its ticks.
func TestSendPanelClosesOnAChangeStaysOnAbort(t *testing.T) {
	t.Parallel()
	m, p := panelModel(t)
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace})
	fs := &forgeSendState{pr: 7, req: domain.PRSendRequest{PR: 7, Notes: []string{"review:r1:1"}}}
	m2, _ := m.forgeSendFinished(fs, engine.Result{}, nil) // aborted
	if q := layerOf[*sendPanel](m2); q == nil || !q.ticked["review:r1:1"] {
		t.Fatal("an abort must leave the panel as it was")
	}
	m3, _ := m.forgeSendFinished(fs, engine.Result{Changed: true}, nil)
	if layerOf[*sendPanel](m3) != nil {
		t.Fatal("a change closes the panel")
	}
}

// enter opens the row's file in the PR diff above the panel, landing on
// its thread; esc on the diff returns to the panel with the ticks kept.
// Serial: env (prSendModel).
func TestSendPanelEnterOpensTheDiffAndReturns(t *testing.T) {
	m, _, head := prSendModel(t)
	id := addTUINote(t, m, head, 5, "look here")
	m = openPR7(t, m)
	m, cmd := m.openSendPanel(7)
	m = drainCmds(t, m, cmd)
	p := layerOf[*sendPanel](m)
	if p == nil || len(p.cands.Groups) != 1 || p.cands.Groups[0].Rows[0].ID != id {
		t.Fatalf("panel = %+v", p)
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace})
	m, cmd = p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = drainCmds(t, m, cmd)
	v := m.diffLayer()
	if v == nil || !v.cursorOnNote() {
		t.Fatalf("enter: diff %v on note %v", v != nil, v != nil && v.cursorOnNote())
	}
	m, _ = v.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if q := layerOf[*sendPanel](m); q == nil || !q.ticked[id] || m.diffLayer() != nil {
		t.Fatal("esc returns to the panel with its ticks")
	}
}

// Review Focus 1: opened from the PR tab's details with the PR's files not
// open, enter says so instead of opening a diff of the wrong view.
func TestSendPanelEnterNeedsTheOpenPR(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.forgeShown, m.prs = true, testPRs()
	m, _ = m.handleSendPanel(sendPanelMsg{gen: m.forgeGen, pr: 7, cands: panelCands()})
	p := layerOf[*sendPanel](m)
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || m.diffLayer() != nil || !strings.Contains(p.notice, "open the pull request") {
		t.Fatalf("notice %q", p.notice)
	}
}

// No candidates: the panel does not open, the status says so (spec §5.1).
func TestSendPanelNothingToSend(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m, _ = m.handleSendPanel(sendPanelMsg{gen: m.forgeGen, pr: 7, cands: domain.SendCandidates{PR: 7}})
	if layerOf[*sendPanel](m) != nil || !strings.Contains(m.statusMsg, "nothing to send to #7") {
		t.Fatalf("status %q", m.statusMsg)
	}
}

// A prose review's group (empty Title) still has a readable header.
func TestSendPanelGroupHeaderWithoutATitle(t *testing.T) {
	t.Parallel()
	c := panelCands()
	c.Groups[0].Title = ""
	m := prDiffModel(t)
	m, _ = m.handleSendPanel(sendPanelMsg{gen: m.forgeGen, pr: 7, cands: c})
	if !strings.Contains(ansi.Strip(m.View()), "Review (AI) · Claude Code · ") {
		t.Fatal("header")
	}
}

// gotoNote lands on the thread holding a REPLY id too (the panel's draft rows).
func TestGotoNoteByReplyID(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.svc.UseNotesDir(t.TempDir())
	root, err := m.svc.NoteAdd(context.Background(), model.Note{
		Address: model.FileAddress{State: model.StateUnstaged, Worktree: m.currentWorktree, Path: "a.txt"},
		Side:    model.NoteSideNew, Range: [2]int{18, 18}, Summary: "root"})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := m.svc.NoteReply(context.Background(), root.ID, model.Note{Summary: "reply"})
	if err != nil {
		t.Fatal(err)
	}
	m, cmd := m.applySteer(steer.Command{ID: "g-1", Cmd: "navigate", File: "a.txt", Target: &steer.Target{State: "unstaged"}, Line: &steer.Line{Side: "new", No: 1}, Wait: true})
	m = pumpDiff(t, m, cmd)
	m, ok := m.gotoNote(rep.ID)
	if !ok || !m.diffLayer().cursorOnNote() {
		t.Fatal("gotoNote by a reply id")
	}
}
```

(Imports: add `"context"`, `"github.com/homeend/gigagit/internal/engine"`, `"github.com/homeend/gigagit/internal/steer"` as the tests need. `openPR7` and `addTUINote` are Task 3's / `pr_send_serial_test.go`'s helpers.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'SendPanel|GotoNoteByReplyID' -count=1`
Expected: build failure `undefined: sendPanel` etc.; `TestGotoNoteByReplyID` compiles once the others are stubbed and FAILS (`gotoNote` matches roots only).

- [ ] **Step 3: Implement `send_panel.go`**

```go
package tui

import (
	"context"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// The send panel (spec §5.3, R6, R7): every unsent local comment of a pull
// request — each AI review's remarks, my notes, my draft replies — in one
// list, nothing ticked on open. The user ticks any mix, picks a body, and
// ctrl+s hands ONE PRSendRequest{Notes, Verdict} to forgeSendCmd: the
// existing confirm (comment / approve / request changes), progress and
// outcome. The panel stays under the confirm; forgeSendFinished removes it
// once something changed (A7).

// panelRow is one display row: a group header or a candidate.
type panelRow struct {
	group int // index into cands.Groups
	cand  int // index into the group's Rows; -1 = the header
}

type sendPanel struct {
	popupMax
	pr     int
	cands  domain.SendCandidates
	rows   []panelRow
	sel    int
	ticked map[string]bool
	// body: bodyNone, bodyReview (bodyFrom = the review id) or bodyTyped.
	body     int
	bodyFrom string
	typed    string
	code     bool   // c: the code excerpt under the current row
	notice   string // the bottom bar: a refused tick, "tick something to send"
}

const (
	bodyNone = iota
	bodyReview
	bodyTyped
)

// sendPanelMsg is PRSendCandidates' answer.
type sendPanelMsg struct {
	gen   int // m.forgeGen when asked
	pr    int
	cands domain.SendCandidates
	err   error
}

// openSendPanel reads PR pr's candidates off the UI thread.
func (m Model) openSendPanel(pr int) (Model, tea.Cmd) {
	svc := m.svc
	if svc == nil || pr == 0 {
		return m, nil
	}
	gen := m.forgeGen
	return m, func() tea.Msg {
		c, err := svc.PRSendCandidates(context.Background(), pr)
		return sendPanelMsg{gen: gen, pr: pr, cands: c, err: err}
	}
}

// handleSendPanel opens the panel, or says why there is none.
func (m Model) handleSendPanel(msg sendPanelMsg) (Model, tea.Cmd) {
	if msg.gen != m.forgeGen {
		return m, nil // asked in the repository before R
	}
	if m.modal != nil {
		return m.sendDialogBusy(), nil
	}
	if msg.err != nil {
		return m.sayInDiff(i18n.T("send: %s", firstLine(msg.err.Error()))), nil
	}
	if len(msg.cands.Groups) == 0 {
		return m.sayInDiff(i18n.T("nothing to send to #%d", msg.pr)), nil
	}
	if old := layerOf[*sendPanel](m); old != nil {
		m = m.removeLayer(old)
	}
	p := &sendPanel{pr: msg.pr, cands: msg.cands, ticked: map[string]bool{}, code: true}
	sortPanelGroups(p.cands.Groups)
	for g := range p.cands.Groups {
		p.rows = append(p.rows, panelRow{group: g, cand: -1})
		for c := range p.cands.Groups[g].Rows {
			p.rows = append(p.rows, panelRow{group: g, cand: c})
		}
	}
	p.sel = p.firstCandidate()
	if kept, ok := m.keptBodyFor(msg.pr, sendGroupPanel, false); ok {
		p.typed = kept // what the user typed last time survives an abort or a failure
	}
	return m.pushLayer(p), nil
}

// sortPanelGroups keeps the domain's order (reviews newest first, mine,
// replies) and breaks a tie between two reviews created in the same
// second by id, so the list is stable between opens (plan 1 minor).
func sortPanelGroups(gs []domain.SendCandidateGroup) {
	sort.SliceStable(gs, func(i, j int) bool {
		a, b := gs[i], gs[j]
		if a.Kind != "review" || b.Kind != "review" || !a.Created.Equal(b.Created) {
			return false
		}
		return a.ID < b.ID
	})
}

func (p *sendPanel) firstCandidate() int {
	for i, r := range p.rows {
		if r.cand >= 0 {
			return i
		}
	}
	return 0
}

func (p *sendPanel) candAt(i int) (domain.SendCandidateGroup, *domain.SendCandidate, bool) {
	if i < 0 || i >= len(p.rows) {
		return domain.SendCandidateGroup{}, nil, false
	}
	r := p.rows[i]
	g := p.cands.Groups[r.group]
	if r.cand < 0 {
		return g, nil, true
	}
	return g, &g.Rows[r.cand], true
}

func (p *sendPanel) tickedCount() int { return len(p.tickedIDs()) }

// tickedIDs are the ticked candidates in list order: the request's Notes.
func (p *sendPanel) tickedIDs() []string {
	var ids []string
	for _, r := range p.rows {
		if r.cand >= 0 {
			if id := p.cands.Groups[r.group].Rows[r.cand].ID; p.ticked[id] {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// tickedReviews are the AI reviews with a ticked remark, newest first: the
// body cycle's review choices.
func (p *sendPanel) tickedReviews() []domain.SendCandidateGroup {
	var out []domain.SendCandidateGroup
	for _, g := range p.cands.Groups {
		if g.Kind != "review" {
			continue
		}
		for _, c := range g.Rows {
			if p.ticked[c.ID] {
				out = append(out, g)
				break
			}
		}
	}
	return out
}

func reviewIDOf(g domain.SendCandidateGroup) string { return strings.TrimPrefix(g.ID, "review:") }

// settleBody drops a review body whose review is no longer ticked.
func (p *sendPanel) settleBody() {
	if p.body != bodyReview {
		return
	}
	for _, g := range p.tickedReviews() {
		if reviewIDOf(g) == p.bodyFrom {
			return
		}
	}
	p.body, p.bodyFrom = bodyNone, ""
}

// cycleBody is b: none → each ticked AI review (newest first) → typed → none.
func (p *sendPanel) cycleBody() {
	rs := p.tickedReviews()
	switch p.body {
	case bodyNone:
		if len(rs) > 0 {
			p.body, p.bodyFrom = bodyReview, reviewIDOf(rs[0])
			return
		}
		p.body = bodyTyped
	case bodyReview:
		for i, g := range rs {
			if reviewIDOf(g) == p.bodyFrom && i+1 < len(rs) {
				p.bodyFrom = reviewIDOf(rs[i+1])
				return
			}
		}
		p.body, p.bodyFrom = bodyTyped, ""
	default:
		p.body = bodyNone
	}
}

// bodyLabel is the Body row's word: none / review text (<agent> · <time>) / typed.
func (p *sendPanel) bodyLabel() string {
	switch p.body {
	case bodyReview:
		for _, g := range p.cands.Groups {
			if reviewIDOf(g) == p.bodyFrom {
				return i18n.T("review text (%s)", g.Agent+" · "+g.Created.Local().Format("15:04"))
			}
		}
		return i18n.T("none")
	case bodyTyped:
		return i18n.T("typed")
	}
	return i18n.T("none")
}

// request is what ctrl+s sends; false with nothing ticked.
func (p *sendPanel) request() (domain.PRSendRequest, bool) {
	ids := p.tickedIDs()
	if len(ids) == 0 {
		return domain.PRSendRequest{}, false
	}
	req := domain.PRSendRequest{PR: p.pr, Notes: ids, Verdict: true}
	switch p.body {
	case bodyReview:
		req.BodyFrom = p.bodyFrom
	case bodyTyped:
		req.Body, req.BodySet = strings.TrimSpace(p.typed), true
	}
	return req, true
}

func (p *sendPanel) move(d int) {
	n := len(p.rows)
	if n == 0 {
		return
	}
	p.sel = max(0, min(n-1, p.sel+d))
	p.notice = ""
}

// tick is space: the row under the cursor (a skip row says why instead).
func (p *sendPanel) tick(m Model) {
	_, c, ok := p.candAt(p.sel)
	if !ok || c == nil {
		p.tickAll()
		return
	}
	if c.Skip != "" {
		p.notice = sendSkipReasonText(sendWhere(c.Path, c.Range[0])+" "+c.Summary, c.Skip)
		return
	}
	p.ticked[c.ID] = !p.ticked[c.ID]
	if !p.ticked[c.ID] {
		delete(p.ticked, c.ID)
	}
	p.settleBody()
}

// tickAll is a: every sendable row of the cursor's group on, or — when they
// all are — off.
func (p *sendPanel) tickAll() {
	g, _, ok := p.candAt(p.sel)
	if !ok {
		return
	}
	all := true
	for _, c := range g.Rows {
		if c.Skip == "" && !p.ticked[c.ID] {
			all = false
		}
	}
	for _, c := range g.Rows {
		if c.Skip != "" {
			continue
		}
		if all {
			delete(p.ticked, c.ID)
		} else {
			p.ticked[c.ID] = true
		}
	}
	p.settleBody()
}

// openRow is enter: the row's file in the PR diff, the cursor on its thread,
// the diff above the panel (esc returns to it, A6). Only while the PR's own
// file list is open — the panel also opens from the PR tab's details.
func (p *sendPanel) openRow(m Model) (Model, tea.Cmd) {
	_, c, ok := p.candAt(p.sel)
	if !ok || c == nil {
		return m, nil
	}
	if m.openPRNumber() != p.pr || m.filesView == nil {
		p.notice = i18n.T("open the pull request to see the file")
		return m, nil
	}
	for _, l := range m.filesView.visible() {
		if l.path != c.Path {
			continue
		}
		u, cmd := m.openDiffForFileLine(l)
		m = u.(Model)
		if m.diffLayer() != nil {
			m.noteLand = &noteLanding{id: c.ID, tag: m.diffTag}
		}
		return m, cmd
	}
	p.notice = i18n.T("%s is not in the pull request's file list", c.Path)
	return m, nil
}

func (p *sendPanel) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.keptSendBody = &keptSendBody{pr: p.pr, group: sendGroupPanel, text: p.typed}
		return m.popLayer(), nil
	case tea.KeyUp:
		p.move(-1)
		return m, nil
	case tea.KeyDown:
		p.move(1)
		return m, nil
	case tea.KeyPgUp:
		p.move(-10)
		return m, nil
	case tea.KeyPgDown:
		p.move(10)
		return m, nil
	case tea.KeyHome:
		p.sel, p.notice = 0, ""
		return m, nil
	case tea.KeyEnd:
		p.sel, p.notice = max(0, len(p.rows)-1), ""
		return m, nil
	case tea.KeySpace:
		p.tick(m)
		return m, nil
	case tea.KeyEnter:
		return p.openRow(m)
	case tea.KeyCtrlS:
		req, ok := p.request()
		if !ok {
			p.notice = i18n.T("tick something to send")
			return m, nil
		}
		if !m.opsIdle() {
			p.notice = i18n.T("another operation is running — send again when it ends")
			return m, nil
		}
		if p.body == bodyTyped {
			m.keptSendBody = &keptSendBody{pr: p.pr, group: sendGroupPanel, text: p.typed}
		}
		return m.forgeSendCmd(req)
	}
	switch msg.String() {
	case "j":
		p.move(1)
	case "k":
		p.move(-1)
	case "a":
		p.tickAll()
	case "b":
		p.cycleBody()
		p.notice = ""
	case "e":
		return m.pushLayer(&sendPanelBody{panel: p, body: newTextField(p.currentBodyText(m))}), nil
	case "c":
		p.code = !p.code
	}
	return m, nil
}

// currentBodyText prefills the body box: the typed text, or — for a review
// body — nothing (the review's summary is read by the planner; editing it
// means typing one's own words).
func (p *sendPanel) currentBodyText(m Model) string { return p.typed }

// sendPanelBody is e: the typed body's box over the panel; ctrl+s keeps it
// and makes the body "typed", esc drops the edit.
type sendPanelBody struct {
	popupMax
	panel  *sendPanel
	body   textfield
	scroll int
}

func (b *sendPanelBody) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyCtrlS:
		b.panel.typed = b.body.Value()
		b.panel.body, b.panel.bodyFrom = bodyTyped, ""
		return m.popLayer(), nil
	case tea.KeyEnter:
		b.body.InsertNewline()
		return m, nil
	case tea.KeyUp:
		b.body.Up()
		return m, nil
	case tea.KeyDown:
		b.body.Down()
		return m, nil
	}
	b.body.HandleEditKey(msg)
	return m, nil
}

func (b *sendPanelBody) render(m Model, below string) string {
	w, h := m.overlayDims()
	innerW := popupResolveWidth(w, b.maximized, commitNormalWidth(w))
	contentW := popupTextWidth(innerW)
	var sb strings.Builder
	sb.WriteString(i18n.T("Review body for #%d", b.panel.pr) + "\n\n")
	sb.WriteString(viewFieldWindow("> "+i18n.T("body:")+" ", b.body, true, contentW, 10, &b.scroll) + "\n")
	sb.WriteString("\n" + packHints([]string{i18n.T("[enter] newline"), i18n.T("[ctrl+s] keep"), i18n.T("[ctrl+t] fullscreen"), i18n.T("[esc] cancel")}, contentW))
	return overlayCenter(clipToHeight(below, h), popupBox(innerW, sb.String()), w, h)
}

// render: title + count, the groups with their colour bar, the rows, the
// code excerpt under the current row, the Body row, the key hints.
func (p *sendPanel) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *sendPanel) box(m Model) string {
	w, h := m.overlayDims()
	innerW := popupResolveWidth(w, p.maximized, popupWideInnerWidth(w))
	textW := popupTextWidth(innerW)
	title := i18n.T("Send to GitHub — #%d", p.pr)
	count := i18n.T("%d ticked", p.tickedCount())
	head := title + strings.Repeat(" ", max(1, textW-lipgloss.Width(title)-lipgloss.Width(count))) + count
	var rows []winRow
	anchor := 0
	for i, r := range p.rows {
		g := p.cands.Groups[r.group]
		if r.cand < 0 {
			bar := "▌"
			if style, ok := groupBarStyle(g.Slot); ok {
				bar = style.Render("▌")
			}
			rows = append(rows, winRow{text: bar + " " + panelGroupTitle(g), prefix: ""})
		} else {
			c := g.Rows[r.cand]
			rows = append(rows, winRow{text: panelRowText(c, p.ticked[c.ID]), elide: true, elideHead: 4})
			if i == p.sel && p.code && c.Skip == "" {
				for _, l := range c.Code {
					rows = append(rows, winRow{text: "        " + sanitizeLine(l), dim: true})
				}
			}
		}
		if i == p.sel {
			anchor = len(rows) - 1
			if p.code && r.cand >= 0 {
				anchor -= len(g.Rows[r.cand].Code) // the row, not its last code line
			}
		}
	}
	listH := max(3, h-12)
	lines := renderWindow(rows, winOpts{w: textW, h: listH, anchor: anchor})
	body := i18n.T("Body: %s", p.bodyLabel()) + "   " + i18n.T("[b] change") + "  " + i18n.T("[e] edit")
	hints := packHints([]string{
		i18n.T("[space] tick"), i18n.T("[a] all/none in group"), i18n.T("[enter] open"), i18n.T("[b] body"),
		i18n.T("[e] edit body"), i18n.T("[c] code"), i18n.T("[ctrl+s] send"), i18n.T("[ctrl+t] fullscreen"), i18n.T("[esc] close"),
	}, textW)
	parts := []string{head, ""}
	parts = append(parts, lines...)
	parts = append(parts, "", body)
	if p.notice != "" {
		parts = append(parts, "▸ "+p.notice)
	}
	parts = append(parts, "", hints)
	return popupBox(innerW, strings.Join(parts, "\n"))
}

// panelGroupTitle: "Review (AI) · <agent> · <date>" (the title, when the
// review has one, after the agent), "My notes", "Draft replies".
func panelGroupTitle(g domain.SendCandidateGroup) string {
	switch g.Kind {
	case "mine":
		return i18n.T("My notes")
	case "replies":
		return i18n.T("Draft replies")
	}
	parts := []string{i18n.T("Review (AI)"), g.Agent}
	if t := strings.TrimSpace(g.Title); t != "" {
		parts = append(parts, truncate(sanitizeLine(t), 40))
	}
	parts = append(parts, g.Created.Local().Format("2006-01-02 15:04"))
	return strings.Join(parts, " · ")
}

// panelRowText: "[x] high  path:120-134  summary"; a skip row "[ ] ~  …  (reason)".
func panelRowText(c domain.SendCandidate, ticked bool) string {
	box := "[ ]"
	if ticked {
		box = "[x]"
	}
	sev := c.Severity
	if c.Skip != "" {
		sev = "~"
	}
	where := c.Path + ":" + strconv.Itoa(c.Range[0])
	if c.Range[1] > c.Range[0] {
		where += "-" + strconv.Itoa(c.Range[1])
	}
	if c.Side == "old" {
		where = c.Path + ":-" + strconv.Itoa(c.Range[0])
	}
	s := box + " " + padCell(sev, 5) + " " + where + "  " + sanitizeLine(c.Summary)
	if c.Skip != "" {
		s += "  " + sendSkipReasonText("", c.Skip)
	}
	return s
}
```

Notes for the implementer: `winRow.dim` — check `window.go:43-95` for the dim/prefix field names and use what exists (a `cls`/`emph` mask or a `dim bool`); `padCell` is in `i18n_display.go`; `sendSkipReasonText("", reason)` yields " (skipped: its lines changed)" — trim the leading space. `clock.Now()` is not needed unless the date is relative; drop the import if unused. The `(m Model) currentBodyText` unused-parameter is fine or drop `m`.

`notes_list_popup.go` `gotoNote(rootID)`: in the `thread` finder, match `r.Note.ID == rootID` OR any `rep.Note.ID == rootID` in `r.Replies` (both the stacked and the single-file loops); rename the parameter to `id` and say so in the doc comment.

i18n, four bundles (all new): `"Send to GitHub — #%d"`, `"%d ticked"`, `"Review (AI)"`, `"My notes"`, `"Draft replies"`, `"Body: %s"`, `"review text (%s)"`, `"typed"`, `"tick something to send"`, `"open the pull request to see the file"`, `"%s is not in the pull request's file list"`, `"Review body for #%d"`, `"[ctrl+s] keep"`, `"[space] tick"`, `"[a] all/none in group"`, `"[b] body"`, `"[e] edit body"`, `"[c] code"`, `"[b] change"`, `"[e] edit"`, `"[ctrl+s] send"`. Existing: `"none"`, `"[enter] open"`, `"[enter] newline"`, `"[ctrl+t] fullscreen"`, `"[esc] cancel"`, `"[esc] close"`, `"body:"`, `"nothing to send to #%d"`, `"send: %s"`, `"another operation is running — send again when it ends"`.

- [ ] **Step 4: Wire the message and the close-on-change**

`model.go` `Update`: `case sendPanelMsg: return m.handleSendPanel(msg)` (beside the `sendGroupsMsg` case Task 6 removes). `forge_send.go` `forgeSendFinished`, inside the `res.Changed && fs.pr != 0` block: `if p := layerOf[*sendPanel](m); p != nil && p.pr == fs.pr { m = m.removeLayer(p) }`. Also the ctrl+t handler: `sendPanel` and `sendPanelBody` embed `popupMax`, which the central handler finds by interface — check `popup_max.go` and add nothing unless it keeps a type list.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/tui/ -run 'SendPanel|GotoNote|I18n|ActionMenuLabels' -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gg add internal/tui/send_panel.go internal/tui/send_panel_test.go internal/tui/notes_list_popup.go internal/tui/model.go internal/tui/forge_send.go internal/i18n/lang/ && git commit -F <msgfile>
```

Message: `tui(send): the Send to GitHub panel over PRSendCandidates — tick any mix, pick a body, one request (R6, R7)`.

---

### Task 6: "Send to GitHub…" everywhere; the old send-review surfaces go

**Files:**
- Create: `internal/tui/verdict_popup.go` (from `send_review_popup.go`: `verdictPopup` + `openVerdict` + `keptSendBody` + `sendGroupPanel` + `keptBodyFor`)
- Delete: `internal/tui/send_review_popup.go`, `internal/tui/send_review_popup_test.go`
- Modify: `internal/tui/action_menu.go:157-160`, `internal/tui/pr_hub.go:51,114`, `internal/tui/forge_send_menu.go:85-105`, `internal/tui/model.go` (the `sendGroupsMsg`/`sendBodyMsg` cases), `internal/tui/help.go:151-153`
- Modify tests: `internal/tui/forge_send_menu_test.go`, `internal/tui/forge_send_kept_test.go`, `internal/tui/forge_send_fixes_test.go` (`TestSendGroupChooserRowsAreUnique` → delete), `internal/tui/help_send_test.go`, `internal/tui/pr_send_serial_test.go` (`TestSendMyDraftReviewWithAVerdict` → through the panel), `internal/tui/forge_send_test.go:141` (`TestSendReviewPopupAnswersTheBody` → the verdict popup)
- Create: `internal/tui/send_entry_test.go`
- i18n: four bundles (adds + orphan removals)
- e2e: `e2e/scenarios/tui_pr_send.toml` + screens

**Interfaces:**
- Consumes: Task 5's `openSendPanel`, `sendPanel`; Task 4's `sendGroupPanel`.
- Produces: `.` row `pr-send` "Send to GitHub…" (in a PR's diff), PR details `s`; `verdictPopup` (`openVerdict` unchanged in name); the note menu without `note-send-review`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/send_entry_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func menuIDs(m Model) []string {
	var ids []string
	for _, r := range availableActions(m) {
		ids = append(ids, r.id)
	}
	return ids
}

func hasID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// R7, R9: a PR diff's . menu offers Send to GitHub… and Verdict…; the old
// Send review… is gone, and a note's own rows are the one-note send only.
func TestPRDiffMenuOffersSendToGitHub(t *testing.T) {
	t.Parallel()
	m := prDiffModelWithNote(t) // forge_send_menu_test.go's fixture: a PR diff with a local note under the cursor
	ids := menuIDs(m)
	for _, want := range []string{"pr-send", "pr-verdict", "note-send"} {
		if !hasID(ids, want) {
			t.Errorf("menu lacks %s: %v", want, ids)
		}
	}
	for _, gone := range []string{"pr-send-review", "note-send-review"} {
		if hasID(ids, gone) {
			t.Errorf("menu still has %s", gone)
		}
	}
	var label string
	for _, r := range availableActions(m) {
		if r.id == "pr-send" {
			label = r.label
		}
	}
	if label != "Send to GitHub…" {
		t.Fatalf("label %q", label)
	}
}

// The PR details' s opens the panel; v the verdict box; the hint line says so.
func TestPRHubSOpensThePanel(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m, _ = m.openPRHub(m.prs[0])
	h := layerOf[*prHubPopup](m)
	if h == nil {
		t.Fatal("no hub")
	}
	if !strings.Contains(h.keys, "[s] send to GitHub") {
		t.Fatalf("keys %q", h.keys)
	}
	_, cmd := h.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if cmd == nil {
		t.Fatal("s must read the candidates")
	}
	m2, _ := h.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if layerOf[*verdictPopup](m2) == nil {
		t.Fatal("v opens the verdict box")
	}
}
```

Rewrite the tests that name removed things:
- `forge_send_menu_test.go`: `TestPRNoteMenuSendsALocalNote` / `…Retries…` assert `note-send` present and `note-send-review` absent (drop any `Send my draft review…` expectation); keep `TestPRNoteMenuOnAGitHubThread` (`note-reply-send`, `note-send-drafts` still there) and `TestRAndXOnAGitHubThreadInsideAPR`.
- `forge_send_kept_test.go`: `layerOf[*sendReviewPopup]` → `layerOf[*verdictPopup]`; a test that used `openSendReviewBody(7, domain.GroupMine)` now opens the panel from a `sendPanelMsg` with a `mine` group and presses `e`/ctrl+s (Task 5's `sendPanelBody`), asserting the kept body under `sendGroupPanel`.
- `forge_send_fixes_test.go`: delete `TestSendGroupChooserRowsAreUnique`; `TestARefusedSendKeepsTheTypedBody` → the verdict popup.
- `forge_send_test.go:141` `TestSendReviewPopupAnswersTheBody` → `verdictPopup`.
- `help_send_test.go`: wants `"Send to GitHub…"`, `"Verdict…"`, `"Send as GitHub comment"`, `"Reply & send"`; must NOT contain `"Send review…"`, `"Send my draft review…"`, `"Send this AI review…"`.
- `pr_send_serial_test.go` `TestSendMyDraftReviewWithAVerdict`: open the PR (`openPR7`), `openSendPanel(7)` + `drainCmds`, tick the note (space), `e` → type "LGTM" → ctrl+s, then ctrl+s on the panel, `runToModal`, options `comment,approve,request-changes,abort`, resolve `approve`, `driveOp`; the last fake-gh write is `SubmitReview` with `APPROVE` and body "LGTM"; the panel is gone afterwards (`layerOf[*sendPanel](m) == nil`).

Add to `send_panel_test.go` (Review Focus 2):

```go
// Review Focus 2: a candidate deleted after the panel opened is skipped by
// the planner (listed under "-" in the confirm); an abort leaves the panel.
// Serial: env (prSendModel).
func TestSendPanelSurvivesAGoneCandidate(t *testing.T) {
	m, _, head := prSendModel(t)
	id := addTUINote(t, m, head, 5, "look here")
	m = openPR7(t, m)
	m, cmd := m.openSendPanel(7)
	m = drainCmds(t, m, cmd)
	p := layerOf[*sendPanel](m)
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeySpace})
	if _, err := m.svc.NoteDelete(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	m, cmd = p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	nm, _ := m.Update(cmd()) // forgeSendReadyMsg: the plan is empty → an error is said, nothing runs
	m = nm.(Model)
	if layerOf[*sendPanel](m) == nil || !strings.Contains(m.statusMsg, "send: ") {
		t.Fatalf("panel %v status %q", layerOf[*sendPanel](m) != nil, m.statusMsg)
	}
}
```

(If `PRSendOp` plans a send with every item skipped as an op rather than an error, the test runs `runToModal` and resolves `abort`, then asserts the panel is still there; ledger which the domain does — plan 1's `ErrNothingToSend` says error.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'TestPRDiffMenuOffersSendToGitHub|TestPRHubSOpensThePanel|TestSendPanelSurvivesAGoneCandidate' -count=1`
Expected: FAIL (`pr-send` missing; `verdictPopup` undefined).

- [ ] **Step 3: Carve out the verdict popup, delete the rest**

Create `verdict_popup.go` holding, from `send_review_popup.go`: the type (renamed `verdictPopup`, fields `popupMax; pr int; body textfield; scroll int` — no `group`, no `verdict` flag), `openVerdict` (pushes `&verdictPopup{pr: pr, body: newTextField(kept)}`), `request()` (`PRSendRequest{PR, Verdict: true, Body: trimmed, BodySet: true}`), `update` (unchanged keys; the kept body is `&keptSendBody{pr, verdict: true, text}`), `render`/`box` (heading `Verdict on #%d`, line `no comments, only the verdict`, the body field, the "next step asks for the verdict" line, the same hints), `keptSendBody`, `sendGroupPanel`, `from`, `keptBodyFor`. Delete `send_review_popup.go` and `send_review_popup_test.go`. Grep: `grep -rn 'sendReviewPopup\|openSendReview\|handleSendGroups\|handleSendBody\|sendGroupsMsg\|sendBodyMsg\|sendGroupOptions\|sendGroupLabel' internal/tui/` must be empty afterwards.

`action_menu.go` lines 157-160:

```go
			rows = append(rows,
				actionRow{id: "pr-send", label: i18n.T("Send to GitHub…"), run: func(m Model) (tea.Model, tea.Cmd) { return m.openSendPanel(n) }},
				actionRow{id: "pr-verdict", label: i18n.T("Verdict…"), run: func(m Model) (tea.Model, tea.Cmd) { return m.openVerdict(n) }})
```

`pr_hub.go`: line 51 `cp.keys = i18n.T("[y] copy URL  [L] copy link  [r] reload  [s] send to GitHub  [v] verdict")`; line 114 `return m.openSendPanel(p.pr.Number)`.

`forge_send_menu.go`: delete the `note-send-review` block (lines 97-105) and the `label = …` lines feeding it; `note-send` and `note-send-drafts` stay.

`model.go`: drop the `sendGroupsMsg` and `sendBodyMsg` cases (Task 5 added `sendPanelMsg`).

`help.go`: line 151 (`.` on a note …) → `"on a note in a pull request's diff: Send as GitHub comment (Retry after a failure); on a GitHub thread: Reply to note, Reply & send…, Resolve / Reopen thread (on GitHub at once), Send draft reply; Delete note stays local; every send shows what will be posted and asks first"`; line 153 → `r("s / v", i18n.T("in the pull request details (and the . menu of its diff): Send to GitHub… — the send panel lists every unsent comment of the pull request (each AI review's remarks, my notes, my draft replies; nothing ticked on open): tick any mix (space, a = the whole group), pick the body (b: none / a ticked review's text / typed; e edits it), enter opens a row's file, ctrl+s sends one GitHub review through the usual confirm (comment, approve or request changes); draft replies go right after it — and Verdict… (no comments)"))`.

i18n, four bundles: add `"Send to GitHub…"`, `"[y] copy URL  [L] copy link  [r] reload  [s] send to GitHub  [v] verdict"` (replacing the old hint key), the two help keys (replacing the old ones). Delete (orphans): `"Send review…"`, `"Send my draft review…"`, `"Send this AI review…"`, `"Send review to #%d"`, `"Send which review to #%d?"`, `"my draft review · 1 note"`, `"my draft review · %d notes"`, `"%s: %s · 1 remark"`, `"%s: %s · %d remarks"`, `"my draft review"`, `"an AI review: its summary is the review body"`, `"[ctrl+s] continue"` (if `grep -rn` finds no other user), `"%s (%d)"` (same check), the old `[s] send review` hint key, the two old help keys. Run the orphan gate to find any left.

- [ ] **Step 4: Run the tests and the gates**

Run: `go test ./internal/tui/ -count=1 2>&1 | tail -5 && go test ./internal/i18n/ ./internal/tui/ -run 'I18n|EngineProse|DecisionOptionValues|ActionMenuLabels|CheckVerbs|FooterRendersTranslated' -count=1`
Expected: `ok` for both.

- [ ] **Step 5: The e2e scenario**

`e2e/scenarios/tui_pr_send.toml`: add a second `[[run]]` before `[tui]`:

```toml
[[run]]
cmd   = ["review", "save", "gg://{{cwd}}@main...refs/gg/pr/7", "--agent", "Claude Code", "--stdin"]
stdin = '{"version":1,"summary":"## Summary\ntwo nits","files":[{"path":"big.txt","annotations":[{"newRange":[5,5],"summary":"name it","meta":{"severity":"low"}}]}]}'
exit  = 0
```

(the same link form as Task 3's scenario). Change the `note-menu` step to `screen_contains = ["Send as GitHub comment", "Send to GitHub…"]`, `screen_excludes = ["Send my draft review…", "Send review…"]`, and replace the `send-confirm`/`sent` steps by: `pr-menu` (`esc`, `.`, then the letters of `to git` → `["Send to GitHub…"]`), `panel` (`enter` → `["Send to GitHub — #7", "0 ticked", "Review (AI) · Claude Code", "name it", "My notes", "why changed?", "Body: none"]`), `panel-ticked` (`space`, `down`, `down`, `space` → `["2 ticked"]` — the cursor starts on the review's remark; `down` twice reaches the note under My notes: adjust the count to the rendered rows), `panel-body` (`b` → `["Body: review text (Claude Code"]`), `send-confirm` (`C-s` → `["Send to o/r #7:", "+ big.txt:5 name it", "+ big.txt:5 why changed?", "review body:"]`), `sent` (`enter` on `comment` → `["● review · me"]`, `screen_excludes = ["○ note", "Send to GitHub — #7"]`).

Run: `go test ./e2e/ -run 'TestScenarios/tui_pr_send' -update -count=1 && go test ./e2e/ -run 'TestScenarios/tui_pr_send' -count=1`
Expected: PASS; the old `05-send-confirm.txt`/`06-sent.txt` are replaced; read the `panel*` screens — nothing ticked on open, the group bars present.

- [ ] **Step 6: Commit**

Never `git add -A`. Use: `git rm internal/tui/send_review_popup.go internal/tui/send_review_popup_test.go && gg add internal/tui/verdict_popup.go internal/tui/action_menu.go internal/tui/pr_hub.go internal/tui/forge_send_menu.go internal/tui/model.go internal/tui/help.go internal/tui/send_entry_test.go internal/tui/send_panel_test.go internal/tui/forge_send_menu_test.go internal/tui/forge_send_kept_test.go internal/tui/forge_send_fixes_test.go internal/tui/forge_send_test.go internal/tui/help_send_test.go internal/tui/pr_send_serial_test.go internal/i18n/lang/ e2e/scenarios/tui_pr_send.toml e2e/scenarios/tui_pr_send.screens && git commit -F <msgfile>`

Message: `tui(send): Send to GitHub… replaces Send review… (PR . menu, details s, help); the group rows leave the note menu (R7, R9)`.

---

### Task 7: A link for every note — Copy note link, L, View all notes, the review view's line link

**Files:**
- Modify: `internal/tui/note_keys.go` (new `noteCopyLinkRows`, `noteLinkAtCursorRow`), `internal/tui/note_row_menu.go:300-330` (`reviewRemarkLinkAtCursorRow` generalised), `internal/tui/diff_view.go:1135-1150` (`L`), `internal/tui/action_menu.go:147` (the row spliced after `noteMenuRows`), `internal/tui/all_notes_popup.go:356-370,676-684`, `internal/tui/link.go:255-275` (the review-diff arm), `internal/tui/help.go:303,336` and the View-all-notes help line
- Test: `internal/tui/note_link_test.go` (new); `internal/tui/diff_menu_parity_test.go` (assert the new rows in each context)
- i18n: four bundles
- e2e: `e2e/scenarios/tui_note_link.toml` (new) + screens

**Interfaces:**
- Consumes: `Service.NoteLinkText(ctx, id) (string, error)` (plan 1 Task 11: a note's own link `?note=<id>`, a remark's remark link, a shelf note refused, a forge id an error), `notesAtCursor()` (reach: the anchor line and the first real line below the box — `note_keys.go:285-330`), `withNoteTargetIn`, `asyncCopyLinkRow`, `reviewRemarkLinkRow`, `lineLinkFor`, `contextLinkRow`.
- Produces: `.` row `note-copy-link` "Copy note link"; `(m Model) noteLinkAtCursorRow() (actionRow, bool)` (what `L` copies on a thread's lines); View all notes `ctrl+l` on an `anNote` row; a review diff's `copy-link` row.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/note_link_test.go`:

```go
package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// worktreeNoteDiff: a note on a.txt:18 (unstaged) with the diff open on it.
func worktreeNoteDiff(t *testing.T) (Model, string) {
	t.Helper()
	m := loadedNavModel(t)
	m.svc.UseNotesDir(t.TempDir())
	n, err := m.svc.NoteAdd(context.Background(), model.Note{
		Address: model.FileAddress{State: model.StateUnstaged, Worktree: m.currentWorktree, Path: "a.txt"},
		Side:    model.NoteSideNew, Range: [2]int{18, 18}, Summary: "on 18"})
	if err != nil {
		t.Fatal(err)
	}
	m, cmd := m.applySteer(steer.Command{ID: "nl-1", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "unstaged"}, Line: &steer.Line{Side: "new", No: 18}, Wait: true})
	return pumpDiff(t, m, cmd), n.ID
}

func rowByID(m Model, id string) (actionRow, bool) {
	for _, r := range availableActions(m) {
		if r.id == id {
			return r, true
		}
	}
	return actionRow{}, false
}

// R13: the . menu offers Copy note link on the anchor line; it copies
// gg://…a.txt:18~<fp>?note=<id>.
func TestCopyNoteLinkOnTheAnchorLine(t *testing.T) {
	t.Parallel()
	m, id := worktreeNoteDiff(t)
	if !m.diffLayer().cursorOnNote() {
		t.Fatal("cursor must be on the note's line")
	}
	r, ok := rowByID(m, "note-copy-link")
	if !ok || r.label != "Copy note link" {
		t.Fatalf("row %+v ok %v", r, ok)
	}
	u, cmd := r.run(m)
	m = drainCmds(t, u.(Model), cmd)
	if !strings.Contains(m.statusMsg, "?note="+id) || !strings.Contains(m.statusMsg, "a.txt:18~") {
		t.Fatalf("status %q", m.statusMsg)
	}
}

// R13: the first line below the box reaches the same thread.
func TestCopyNoteLinkBelowTheBox(t *testing.T) {
	t.Parallel()
	m, id := worktreeNoteDiff(t)
	v := m.diffLayer()
	v.curLine++ // the first real line under the note's rows (the box rows are not cursor lines)
	for v.curLine < len(v.lines) && v.lines[v.curLine].kind != lineBody {
		v.curLine++
	}
	if m.diffLayer().cursorOnNote() {
		t.Fatal("the cursor must be on the line below the box")
	}
	r, ok := rowByID(m, "note-copy-link")
	if !ok {
		t.Fatalf("no Copy note link below the box; menu %v", menuIDs(m))
	}
	u, cmd := r.run(m)
	m = drainCmds(t, u.(Model), cmd)
	if !strings.Contains(m.statusMsg, "?note="+id) {
		t.Fatalf("status %q", m.statusMsg)
	}
}

// R13: L on the anchor line copies the note link; Review Focus 5: L on the
// line below the box copies the LINE link.
func TestLCopiesTheNoteLinkOnTheAnchorLine(t *testing.T) {
	t.Parallel()
	m, id := worktreeNoteDiff(t)
	v := m.diffLayer()
	u, cmd := v.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
	m = drainCmds(t, u.(Model), cmd)
	if !strings.Contains(m.statusMsg, "?note="+id) {
		t.Fatalf("L on the anchor line: %q", m.statusMsg)
	}
}

func TestLBelowTheBoxCopiesTheLineLink(t *testing.T) {
	t.Parallel()
	m, id := worktreeNoteDiff(t)
	v := m.diffLayer()
	v.curLine++
	for v.curLine < len(v.lines) && v.lines[v.curLine].kind != lineBody {
		v.curLine++
	}
	u, cmd := v.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
	m = drainCmds(t, u.(Model), cmd)
	if strings.Contains(m.statusMsg, "?note="+id) || !strings.Contains(m.statusMsg, "a.txt:19") {
		t.Fatalf("L below the box: %q", m.statusMsg)
	}
}

// A3: a remark keeps Copy remark link; no Copy note link is offered for it;
// a GitHub thread offers neither.
func TestRemarkKeepsItsOwnLinkRow(t *testing.T) {
	t.Parallel()
	m, _ := openedReviewView(t)
	m = openReviewDiff(t, m, "a.go")
	m, _ = m.landOnNote(1)
	ids := menuIDs(m)
	if !hasID(ids, "copy-remark-link") || hasID(ids, "note-copy-link") {
		t.Fatalf("menu %v", ids)
	}
}

// R13: the review view's diff offers the line's Copy link (the reviewed
// tip's address), which it never had.
func TestReviewDiffOffersTheLineLink(t *testing.T) {
	t.Parallel()
	m, _ := openedReviewView(t)
	tip := m.filesReview.tip
	m = openReviewDiff(t, m, "a.go")
	r, ok := rowByID(m, "copy-link")
	if !ok {
		t.Fatalf("no Copy link in a review diff; menu %v", menuIDs(m))
	}
	if !strings.Contains(r.copyText, tip) || !strings.Contains(r.copyText, "/a.go:") {
		t.Fatalf("link %q", r.copyText)
	}
}

// R13: View all notes — ctrl+l on a thread row copies the note link; the
// key hint says so.
func TestAllNotesCtrlLOnAThread(t *testing.T) {
	t.Parallel()
	m, id := worktreeNoteDiff(t)
	m = m.popLayer() // the diff
	m, cmd := m.openAllNotes()
	m = drainCmds(t, m, cmd)
	p := layerOf[*allNotesPopup](m)
	vis := p.visible()
	for i, r := range vis {
		if r.kind == anNote && r.note.Note.ID == id {
			p.sel = i
		}
	}
	if !strings.Contains(p.box(m), "[ctrl+l] copy link") {
		t.Fatal("hint")
	}
	u, c := p.update(m, tea.KeyMsg{Type: tea.KeyCtrlL})
	m = drainCmds(t, u, c)
	if !strings.Contains(m.statusMsg, "?note="+id) {
		t.Fatalf("status %q", m.statusMsg)
	}
}
```

(`actionRow.copyText` — the field `copyRow` fills; read `action_menu.go:810-825` for its name. `openReviewDiff`, `openedReviewView`, `loadedNavModel`, `pumpDiff` are existing helpers; `menuIDs`/`hasID` come from Task 6's `send_entry_test.go`.)

Add to `diff_menu_parity_test.go`, in `diffMenuParity` after the menus are compared: `if want != "" && !has(single, want) { t.Fatalf(...) }` with a new `want string` parameter per context — `"note-copy-link"` for commit note / worktree note / PR note, `"copy-link"` for the review remark — so the parity run also pins that the row exists in both modes.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'NoteLink|CopyNoteLink|LCopies|LBelow|RemarkKeeps|ReviewDiffOffers|AllNotesCtrlL|DiffMenuParity' -count=1`
Expected: FAIL — no `note-copy-link` row; L below the box: the note-link tests fail on the status; the review diff has no `copy-link`; ctrl+l on a thread does nothing. (`TestCopyNoteLinkBelowTheBox` fails only for the missing row: the reach is already there — if, after Step 3, it passes without a reach change, ledger "reach already covered the line below the box".)

- [ ] **Step 3: Implement**

`note_keys.go`:

```go
// noteCopyLinkRows is the . menu's "Copy note link" (R13): offered when a
// hand-written thread is in reach — notesAtCursor's reach, the anchor line
// and the first real line below the box. A review's remark keeps its own
// Copy remark link row (A3); a GitHub thread has no gg link. Several threads
// on the line: the chooser, as Edit note's. The link is the targeted note's
// (a root, or the visible reply when the root is hidden).
func (m Model) noteCopyLinkRows() []actionRow {
	if _, ok := m.topLayer().(*diffView); !ok || m.svc == nil {
		return nil
	}
	var ts []noteTarget
	for _, t := range m.notesAtCursor() {
		if t.forge || model.IsForgeNoteID(t.rootID) || model.IsReviewNoteID(t.rootID) {
			continue
		}
		ts = append(ts, t)
	}
	if len(ts) == 0 {
		return nil
	}
	svc := m.svc
	return []actionRow{{id: "note-copy-link", label: i18n.T("Copy note link"), run: func(m Model) (tea.Model, tea.Cmd) {
		return m.withNoteTargetIn(ts, func(m Model, t noteTarget) (tea.Model, tea.Cmd) {
			id := t.note.ID
			return m.asyncCopyLinkRow("note-copy-link", i18n.T("Copy note link"), func(ctx context.Context) (string, error) {
				return svc.NoteLinkText(ctx, id)
			}).run(m)
		})
	}}}
}
```

`action_menu.go:147`: `rows = append(rows, m.noteMenuRows()...)` then `rows = append(rows, m.noteCopyLinkRows()...)` (before `noteLinkRows`, the "Open link:" rows).

`note_row_menu.go`: generalise `reviewRemarkLinkAtCursorRow` into `noteLinkAtCursorRow`: the same walk over `m.notesAtCursor()` with the lines-hold-the-cursor rule and the no-multi-line-selection guard, but for EVERY non-forge thread: a review remark → `m.reviewRemarkLinkRow()` (needs `v.curNoteView().reviewID != ""`, as today); a stored thread → the `note-copy-link` row built for that one target (factor `noteCopyLinkRowFor(t noteTarget) actionRow` out of the function above and call it from both). Keep the old name as a one-line wrapper if other callers exist (`grep -rn reviewRemarkLinkAtCursorRow`).

`diff_view.go` `L`: `if r, ok := m.noteLinkAtCursorRow(); ok { … }` in place of `reviewRemarkLinkAtCursorRow`.

`link.go` `contextLinkText`, in the `*diffView` branch after the `diffNoteAddress` block:

```go
		// A review's diff writes no notes (diffNoteAddress refuses it) but
		// its lines are the reviewed tip's: the plain line link (R13). The
		// path is the CURSOR'S FILE's (a stack names none itself).
		if v := m.diffLayer().curNoteView(); v != nil && v.reviewID != "" && v.noteAddr.Path != "" && v.cmp == nil {
			return ranged(m.lineLinkFor(v.noteAddr, side, line, text))
		}
```

(A range review's diff has `cmp` and already goes through `compareLinkText`.)

`all_notes_popup.go`: the `tea.KeyCtrlL` arm: `anReview` as today; add `vis[p.sel].kind == anNote && !vis[p.sel].note.Note.IsShelfLevel()` → `svc.NoteLinkText(ctx, vis[p.sel].note.Note.ID)` through `asyncCopyLinkRow("note-copy-link", i18n.T("Copy note link"), …).run(m)`. The hint (line ~681): `[ctrl+l] copy link` for `anNote` rows that are not shelf-level too.

`help.go`: line 336 (`L` in a diff) append `; on a note's line L copies the NOTE's link (gg://…?note=<id> — the . menu's Copy note link, offered from the line above and the first line below the box; an agent reads it with gg note show <link>)`; line 303 append `. A hand-written note has a link too: Copy note link in its . menu, L on its line, ctrl+l on its row in View all notes`; the View all notes help line (grep `"View all notes"` in help.go) gains `ctrl+l copies a review's or a thread's link`.

i18n, four bundles: `"Copy note link"` (ja `"ノートのリンクをコピー"`, ko `"노트 링크 복사"`, zh `"复制笔记链接"`, ru `"Скопировать ссылку на заметку"`); the three changed help keys.

- [ ] **Step 4: Run the tests, the parity suite and the gates**

Run: `go test ./internal/tui/ -run 'NoteLink|CopyNoteLink|LCopies|LBelow|RemarkKeeps|ReviewDiffOffers|AllNotesCtrlL|DiffMenuParity|I18n|ActionMenuLabels|Help' -count=1`
Expected: PASS.

- [ ] **Step 5: The e2e golden**

Create `e2e/scenarios/tui_note_link.toml`: a commit with `a.txt` (5 lines); `[[run]] cmd = ["note", "add", "--commit", "{{rev:HEAD}}", "--file", "a.txt", "--new-line", "3", "--summary", "why three?", "--source", "user"]` (read `s88_notes_cli.toml` for the exact `note add` flags on a commit and copy them); steps: `files` (`right`, `enter`), `diff` (`enter` → `["why three?"]`), `menu` (`.`, then `note li` → `["Copy note link"]`), `copied` (`enter` → `["Copied link: gg://", "?note="]`).

Run: `go test ./e2e/ -run 'TestScenarios/tui_note_link' -update -count=1 && go test ./e2e/ -run 'TestScenarios/tui_note_link' -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gg add internal/tui/note_keys.go internal/tui/note_row_menu.go internal/tui/diff_view.go internal/tui/action_menu.go internal/tui/all_notes_popup.go internal/tui/link.go internal/tui/help.go internal/tui/note_link_test.go internal/tui/diff_menu_parity_test.go internal/i18n/lang/ e2e/scenarios/tui_note_link.toml e2e/scenarios/tui_note_link.screens && git commit -F <msgfile>
```

Message: `tui(notes): Copy note link from both lines next to the box, L on the anchor line, ctrl+l in View all notes, the review diff's line link (R13)`.

---

### Task 8: Docs, the gates, the race run

**Files:**
- Modify: `CHANGELOG.md` (top), `README.md` (the TUI pull-request / review paragraphs), `docs/CLAUDE-details.md` (a "PR review, plan 2 — TUI" block beside plan 1's)

**Interfaces:** none new.

- [ ] **Step 1: CHANGELOG**

Add a section above plan 1's: `PR review, plan 2 (TUI): ≡ Summary / ≡ Overview rows, PR Reviews rows, the Send to GitHub panel, Copy note link` with one bullet per task (1–7), naming the rows, keys and the removed surfaces, and the plan's rulings A1–A9 in one bullet each where they change what the user sees (A1, A2, A3, A4, A7).

- [ ] **Step 2: README**

In the pull-request section: "Send review…" → "Send to GitHub…" with one sentence on the panel (nothing ticked on open, any mix, one review, drafts after it); in the review-view section: "≡ Overview" → "≡ Summary", plus the "≡ Overview" row for a review with a stored overview (tab/enter/backspace); in the notes section: every note has a link (Copy note link, L, ctrl+l). `grep -n 'Send review\|≡ Overview' README.md` must be empty afterwards.

- [ ] **Step 3: CLAUDE-details**

Append after plan 1's block: the stored-overview viewer design (`srcReviewOverview`, `overview.tip`, `anchor.plain`, the three guards, A1), the PR return (`previewReturn.prNumber`), the panel's lifecycle (A6, A7, `sendGroupPanel`), the note-link rows and the reach, the `gotoNote` reply match.

- [ ] **Step 4: The full gates**

Run: `cd /work/gigagit/.claude/worktrees/pr-review-tui && go vet ./... && gofmt -l internal/ cmd/ e2e/ | tee /dev/stderr | wc -l`
Expected: no vet output; `0`.

Run: `go test ./internal/tui/ ./internal/i18n/ ./e2e/ -count=1 2>&1 | tail -5`
Expected: `ok` ×3.

- [ ] **Step 5: Commit**

```bash
gg add CHANGELOG.md README.md docs/CLAUDE-details.md && git commit -F <msgfile>
```

Message: `docs: PR review plan 2 (TUI) — CHANGELOG, README, CLAUDE-details`.

- [ ] **Step 6: The race gate (before the final review)**

Run in the background with the sandbox disabled: `cd /work/gigagit/.claude/worktrees/pr-review-tui && ./test.sh race > /tmp/claude-1000/-work-gigagit/4f6b5de8-7e75-45d9-9f0c-7798b02ba7f7/scratchpad/race-plan2.log 2>&1`
Expected: the log's last lines hold the literal `all green`. Anything else is red: fix, re-run.

---

## Self-review notes (done while writing)

- Spec coverage: §3.2 → Task 3; §4.1 → Tasks 1–2; §5.3 → Tasks 4–6; §7.2 → Task 7; help/footer → Tasks 1, 6, 7; i18n → every task; goldens → Tasks 1, 2, 3, 6, 7; parity → Task 7; the Then-partial refresh → Task 4; plan 1's TUI-touching minors (empty Title, equal-Created sort, reply rows' landing) → Task 5.
- Type consistency: `contentLine.summary` / `.overviewDoc` (Tasks 1–2), `overview.tip` + `anchor.plain` (Task 2), `previewReturn.prNumber` + `openPreviewReturnCmd` (Task 3), `sendGroupPanel` (Tasks 4–6), `sendPanel`/`sendPanelMsg`/`sendPanelBody`/`openSendPanel`/`handleSendPanel` (Tasks 5–6), `verdictPopup` (Task 6), `noteCopyLinkRows`/`noteLinkAtCursorRow` (Task 7).
- Not in this plan (plan 3, web): `summaryMd`/`overviewMd`, the web Reviews block, the candidates endpoint and overlay, `/api/pr/send` changes, the web note-link rows. Not in this plan (domain minors from plan 1's review, deferred): `--body-from` a prose review and settle; `NoteThread` on a reply to a remark; `PRSendCandidates` Review-per-remark; range notes past the file end; the CLI test gaps.
