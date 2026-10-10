# PR Review — Plan 3 (Web) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (native execution by the session that wrote it; no implementer subagents). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The gg web page gets what plans 1 and 2 gave the domain and the TUI: the review wire's `summaryMd`/`overviewMd` split with "≡ Summary" / "≡ Overview" rows, a PR's stored reviews in its file list, one "Send to GitHub…" panel over `/api/pr/send/candidates`, `/api/pr/send` with `verdict`/`body_from` (kind `group` gone), and Copy note link in the note menu and the all-notes view.

**Architecture:** Every page feature reads a domain query through one small handler (`reviews.go`, `prnotes.go`, `prsend.go`, `notes.go`) and paints it in a module under `internal/web/static/`. Pure row/option builders live in import-free files (`prsendrows.js`, the guarded "pure" section of `viewer.js`, the `an*` builders of `allnotes.js`) so node pins them; wiring (routes, imports, menu rows) is pinned by grep tests, handlers by `httptest` against the fake forge (`sendServer`). The stored overview rides the temporary overview's viewer (`viewer.js`) in a *stored* mode that never touches the open-files registry.

**Tech Stack:** Go 1.26 (`net/http`, `httptest`), vanilla ES modules, node for pure-JS guards, the existing fake forge (`writerForge`) and `prFixture`.

**Spec:** `docs/superpowers/specs/2026-10-09-pr-review-send-panel-design.md` — §0, §2.3, §3.3, §4.2, §5.4, §6, §7.2, §8 (web), §10 plan 3. Plan 1's Interfaces: `docs/superpowers/plans/2026-10-09-pr-review-send-panel-core.md` (`PRSendCandidates`, `PRSendRequest.BodyFrom`, `ReviewOverview`, `NoteLinkText`, `PreviewReviews` on PR sets). Plan 2 (the TUI twin, for parity of words and behaviour): `docs/superpowers/plans/2026-10-09-pr-review-send-panel-tui.md`.

## Amendments to the spec (rulings made while planning; the executor keeps them)

- **W1 — the PR's reviews ride `/api/pr/notes`, not `/api/pr/open`.** The page already reads the PR's per-file counts from `/api/pr/notes` (`loadPRCounts`) after every open, send and note change; a `reviews` array there (the same `reviewHeadWire` shape `/api/preview/notes` answers) keeps the block fresh for free, where `/api/pr/open` is a cached/live-ref open that never re-runs on a notes change. `previewScopeReviews` drops its `po.pr ? []` guard; a PR review's way back is `{kind: "pr", pr: n, reviewId}`.
- **W2 — the candidates endpoint is `GET /api/pr/send/candidates?n=<n>`** (`?n=`, the PR family's convention through `knownPR`), not `?pr=`.
- **W3 — candidate text is plain.** `summary`/`rationale` go as text (the TUI shows the same text); no markdown trees on this wire. The `code` lines are plain strings.
- **W4 — the panel's pure row builder lives in `prsendrows.js`** beside `sendRows` (the file the spec names; it already holds the note-menu rows). Three pure functions: `panelRows`, `bodyOptions`, `panelRequest`.
- **W5 — the stored overview is a page-local viewer mode** (`view.ov.stored`), never registered with the server's open-files list (`/api/open-files`, `ofPost`): nothing to focus, refresh or close there. Its anchors open the file **at the reviewed tip** through `openViewer({src: "commit", rev: tip, …})` (that open IS registered, as the TUI registers the file it opens) and Back re-shows the kept copy. A working review (tip `""`) opens the working tree, as the TUI does. Plain anchors (`plain: true` on the wire — the domain's `!OK`) draw with the gone style, `tab` skips them, `enter` on one says "anchor %s does not resolve at the reviewed commit", and they make no band (TUI follow-up 12).
- **W6 — the panel entry is the PR menu row only.** The PR details overlay has no send button today; "Send to GitHub…" replaces the PR menu's "Send review…" (`registerRows("pr")` in `prsend.js`), "Verdict…" stays. `/api/pr/send/groups` is removed with kind `group` (its only reader was `sendReviewPick`); `PRSendGroups` stays in the domain for the CLI.
- **W7 — the panel closes on a send that changed GitHub** (`ev.ok` of the done event — the web's "Changed"), stays on an abort, a refusal or a failure, with its ticks; esc closes it and a reopen for the same PR restores the ticks and the body (page state, per PR). A row's path click opens the file in the PR diff (`landNote`) and closes the panel the same way.
- **W8 — Copy note link on the web copies the CLICKED note's link** (`/api/notes/link?id=<n.id>`): for a reply that is its thread's link with `?note=<reply id>` (the domain's rule), for a root the root's. The "copy gg link to this note" LINE link row stays. Remarks keep "copy remark link" (no note link; R13). All notes: `ctrl+l` on a note row (never a shelf note's: `target.shelf` or `target.state === "shelf"`) and on a review row (its review link), with the hint.
- **W9 — not in this plan:** the TUI's open-files protocol naming a stored overview's review id (deferred item 18, a TUI change); the gg-review/gg-cross-review skill texts (plan 1 did them). The using-gg line for `--body-from` (deferred item 1) IS here (Task 10).

## Global Constraints

- Loopback-only, domain-only frontend: `internal/web` imports `internal/domain`, never `internal/git` (`archtest`). Wire values the page sends back are checked against what the server handed it (`prSendRequest`'s `pick`).
- Every mutating route is `writeGuard`ed; `/api/pr/send/candidates` is a GET (a read). Agents never send (no gg tool or skill reaches the send routes).
- Vocabulary: "overview" = the one document kind; a review's text is its "summary"; never "tour". Wire keys: `summaryMd` (the summary), `overviewMd` + `overviewAnchors` + `overviewTip` (the stored overview).
- Pure JS stays pure: `prsendrows.js` imports nothing; the viewer's pure section stays between `viewerPureStart`/`viewerPureEnd`; `allnotes.js`'s `an*` builders stay export-free DOM-free (the `an.mjs` harness).
- Every new feature paints its own help row (`registerHelp`) and the README's gg web words follow the TUI's (plan 2's README passages).
- Tests: `go test ./internal/web/` (node tests skip without node — node IS installed here); `./test.sh race` before handing over, green only on the literal `all green`; commit per task with `gg add <files>` + `git commit -F <msgfile>`, trailers `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01SxvmdF5zQp68vkozNLGDiW`; never `git add -A`, never push, never publish.
- Worktree: `/work/gigagit/.claude/worktrees/pr-review-web` (branch `feat/pr-review-send-panel-web`, off main `127fb08a`); absolute paths in every command.

## Review Focus

1. **A ticked review is deleted while the panel is open** (another client, the TUI): the server refuses the stale remark id with 400 "is not a note of pull request #7"; the panel says so in its notice and stays with its ticks. Pinned in Task 6 (`TestWebSendRefusesForgedValues` keeps the stale-id case) and Task 8 (the panel's refusal path keeps the overlay).
2. **The PR's head moved on GitHub between opening the panel and ctrl+s**: `/api/pr/send` answers 409 `head_moved`; the page re-opens the diff on the new head and the panel, reopened, lists the candidates anew with the old ticks dropped for ids no longer listed. Pinned in Task 7 (`panelRows` ignores ticks of ids not in the candidates) and Task 8 (the `onHeadMoved` hook closes the panel).
3. **A stored overview anchor on a file outside the review's set, or a range past the file**: drawn plain, `tab` skips it, `enter` says why, no band. Pinned in Task 3 (`stepAnchor`/`anchorBands`/`anchorStatus` node tests with `plain`).
4. **A working review's stored overview** (`overviewTip == ""`): anchors open the working tree (`src: "worktree"`), bands and Back as for a temporary overview. Pinned in Task 3 (`storedAnchorOpen` pure helper) and Task 2 (`overviewTip` is `""` for a working review).
5. **A PR whose stored review was saved on an older head**: the Reviews block lists it with "older tip"; opening it shows the older review (R2 carries over); esc returns to the PR view. Pinned in Task 4 (`/api/pr/notes` reviews carry `older`; `previewReviewText` already appends "· older tip").

---

## File structure

| File | Responsibility |
|---|---|
| `internal/web/reviews.go` | `/api/review/{id}`: `summaryMd` (the summary), `overviewMd`/`overviewAnchors`/`overviewTip` (the stored overview) |
| `internal/web/review.go` | `addReviewDoc`: the AI-review done event's `summaryMd` |
| `internal/web/prnotes.go` | `/api/pr/notes`: `reviews` (the PR's stored reviews, `reviewHeads`) |
| `internal/web/prsend.go` | `/api/pr/send/candidates`; `/api/pr/send` kind `notes` with `verdict`/`body_from`; kind `group` and `/api/pr/send/groups` removed |
| `internal/web/notes.go` | `GET /api/notes/link?id=` → `{link}` (`svc.NoteLinkText`) |
| `internal/web/static/reviews.js` | "≡ Summary" row/pane (renamed), "≡ Overview" row → `openStoredOverview`; `goBack` kind `pr`; `previewScopeReviews` for a PR |
| `internal/web/static/review.js` | reads `summaryMd` from the done event |
| `internal/web/static/files.js` | `previewBack` → kind `pr`; the "≡ Overview" row's click/contextmenu; the note menu's "Copy note link" row |
| `internal/web/static/previews.js` | `loadPRCounts` keeps `state.previewReviews` |
| `internal/web/static/viewer.js` | pure: `stepAnchor`/`anchorBands`/`bandOf` skip `plain`, `anchorStatus` plain text, `storedAnchorOpen`; stored-overview mode: `openStoredOverview`, `showStoredOverview`, guards in `closeViewer`, `refreshOverview`, `openAnchorAt`, `anchorBack`, `paintTitle`, `docName` |
| `internal/web/static/prsendrows.js` | pure: `sendRows` (minus `send-review`), `panelRows`, `bodyOptions`, `panelRequest` |
| `internal/web/static/prsendpanel.js` | the overlay: fetch, state per PR, keys, render, send |
| `internal/web/static/prsend.js` | `sendToGitHub` (exported), PR menu row "Send to GitHub…", `sendReviewPick`/`sendReviewBody`/`groupCount` removed, help text |
| `internal/web/static/allnotes.js` | pure `anCopyLinkURL(row)`; `ctrl+l` + hint |
| `internal/web/static/style.css` | `#prsendpanel` rules; `.rovd` (the overview row) |
| `internal/web/static/app.js` | `import "./prsendpanel.js";` |
| `internal/web/static/index.html` | the `#prsendpanel` overlay skeleton |
| tests | `reviews_test.go`, `review_test.go`, `prsend_test.go`, `prsendjs_test.go`, `viewerjs_test.go`, `allnotesjs_test.go`, new `prnotes_reviews_test.go`, `notelink_test.go`, `prsendpanel_test.go`, `storedoverviewjs_test.go` |
| docs | `README.md` (gg web paragraph ~line 567 + the review view's web words), `CHANGELOG.md`, `docs/CLAUDE-details.md`, `internal/agentskill/using-gg.md` (+ `Version`) |

---

### Task 1: `summaryMd` on the review wire; the "≡ Summary" row and pane

**Files:**
- Modify: `internal/web/reviews.go:136,177` (`"overviewMd"` → `"summaryMd"`)
- Modify: `internal/web/review.go:412` (`addReviewDoc`)
- Modify: `internal/web/static/reviews.js` (the row at ~474, `reviewOverviewHTML` ~492, `showReviewOverview` ~520, `setDiffTitle("≡ Overview")` ~550, `rv.onOverview` everywhere, the help text ~750)
- Modify: `internal/web/static/review.js:454,479`
- Modify: `internal/web/static/files.js`, `internal/web/static/keys.js` (every `onOverview` / `showReviewOverview` / `reviewOverviewHTML` import and use — `grep -rn onOverview internal/web/static/` lists 17 sites in files.js, keys.js, reviews.js, stackview.js)
- Modify: `internal/web/static/stackview.js` (its import of `reviewOverviewHTML`/`showReviewOverview`, `state.review.onOverview` at ~121, and the comments "its Overview" → "its Summary"; the `.stk-ov` block keeps its class)
- Test: `internal/web/reviews_test.go` (`reviewViewResp.OverviewMd` → `SummaryMd`), `internal/web/review_test.go:374`, `internal/web/reviewsjs_test.go` (+ a wiring pin)

**Interfaces:**
- Produces: wire key `summaryMd` (markdown tree of the review's summary) on `/api/review/{id}` and the review done event; JS names `showReviewSummary()`, `reviewSummaryHTML()`, `state.review.onSummary`.

- [ ] **Step 1: Write the failing Go test**

In `internal/web/reviews_test.go`, rename the field in `reviewViewResp` and pin the old key's absence:

```go
	SummaryMd  any               `json:"summaryMd"`
	OverviewMd any               `json:"overviewMd"` // the STORED overview (Task 2); nil without one
```

Add to `TestReviewViewShape` (after the existing assertions):

```go
	if got.SummaryMd == nil {
		t.Fatal("the review's summary must arrive as summaryMd (R11: the review text is its summary)")
	}
	if got.OverviewMd != nil {
		t.Fatal("a review without a stored overview sends no overviewMd")
	}
```

In `internal/web/review_test.go` line ~374, `last["overviewMd"]` → `last["summaryMd"]`.

Add to `internal/web/reviewsjs_test.go` a wiring pin:

```go
// R11/R12: the review view's first row is "≡ Summary" and reads summaryMd;
// the word "overview" is kept for the stored overview alone.
func TestReviewSummaryRowIsWired(t *testing.T) {
	t.Parallel()
	js := readStatic(t, "reviews.js")
	for _, want := range []string{`≡ Summary`, `d.summaryMd`, `function showReviewSummary(`, `function reviewSummaryHTML(`, `onSummary`} {
		if !strings.Contains(js, want) {
			t.Errorf("reviews.js lacks %q", want)
		}
	}
	for _, gone := range []string{`function showReviewOverview(`, `function reviewOverviewHTML(`, `onOverview`, `setDiffTitle("≡ Overview")`} {
		if strings.Contains(js, gone) {
			t.Errorf("reviews.js still has %q", gone)
		}
	}
	if r := readStatic(t, "review.js"); !strings.Contains(r, `summaryMd`) || strings.Contains(r, `overviewMd`) {
		t.Error("review.js must read the done event's summaryMd")
	}
}
```

If `readStatic` does not exist in the test package, add it to `reviewsjs_test.go`:

```go
func readStatic(t *testing.T, f string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("static", f))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/pr-review-web && go test ./internal/web/ -run 'TestReviewViewShape|TestReviewSummaryRowIsWired|TestReviewRunEmits|TestReview' 2>&1 | tail -15`
Expected: FAIL — `summaryMd` missing / `reviews.js lacks "≡ Summary"`.

- [ ] **Step 3: Rename on the server**

`internal/web/reviews.go`: both `out["overviewMd"] = …` → `out["summaryMd"] = …` (lines ~136 and ~177; the comment on 136 becomes `// a prose review: its text is the summary`). `internal/web/review.go` `addReviewDoc`: `out["summaryMd"] = markdown.Parse(doc.Summary)`; its doc comment says "the summary as a parsed markdown tree".

- [ ] **Step 4: Rename on the page**

`reviews.js`: `rv.onOverview` → `rv.onSummary` (every site; grep `onOverview` in `reviews.js`, `files.js`, `stackview.js`); `reviewOverviewHTML` → `reviewSummaryHTML` (its bar's `title` stays, the comment says "the Summary"); `showReviewOverview` → `showReviewSummary`; the row:

```js
  let html = `<li class="rov${rv.onSummary ? " sel" : ""}" data-ov="1" title="the review's summary — the text GitHub gets — its meta and the notes it could not place on a line">≡ Summary</li>`;
```

`setDiffTitle("≡ Summary")`; `(d.summaryMd ? …mdHTML(d.summaryMd, esc)…)`; the comments "the Overview" → "the Summary" in this file; the help row's words ("… head its file list under *≡ Overview*" → "*≡ Summary*"). `review.js`: `overviewMd: ev.overviewMd` → `summaryMd: ev.summaryMd`, `mdHTML(doc.summaryMd, esc)`. `files.js`: the imports and the two uses (`showReviewOverview()` at the `data-ov` click → `showReviewSummary()`, `state.review.onOverview = false` → `onSummary`). `stackview.js`: the stacked review's prose block label "Overview" → "Summary" (grep `Overview` there; the TUI's `proseLabel()` is "Summary").

- [ ] **Step 5: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/pr-review-web && go test ./internal/web/ 2>&1 | tail -3`
Expected: `ok` (every reviews/review/stack test; node tests included).

- [ ] **Step 6: Commit**

```bash
cd /work/gigagit/.claude/worktrees/pr-review-web && gg add internal/web/reviews.go internal/web/review.go internal/web/static/reviews.js internal/web/static/review.js internal/web/static/files.js internal/web/static/stackview.js internal/web/reviews_test.go internal/web/review_test.go internal/web/reviewsjs_test.go && git commit -F <msgfile>
```
Message: `web(reviews): the review wire's summaryMd and the ≡ Summary row (R11, R12)`.

---

### Task 2: The stored overview on the review wire

**Files:**
- Modify: `internal/web/reviews.go` (`handleReview`, after `out["meta"]`)
- Modify: `internal/web/overview_http.go` (`overviewAnchor` gains `Plain bool json:"plain,omitempty"`)
- Test: `internal/web/reviews_test.go`

**Interfaces:**
- Consumes: `svc.ReviewOverview(ctx, id) (domain.OverviewDoc, error)` — `Text`, `Doc markdown.Doc` (`.Blocks`), `Anchors []OverviewAnchor{agentdocs.Anchor; OK bool}`; `svc.ReviewRevs(ctx, rv) (base, tip string, isRange bool)`; `rv.Kind == domain.ReviewOnWorktree`.
- Produces: wire keys on `/api/review/{id}` when `rv.Doc != nil && rv.Doc.Overview != ""`: `overviewMd` (`[]markdown.Block`), `overviewAnchors` (`[]overviewAnchor` with `dest,path,start,end,note,label,missing,plain`; `ref` empty), `overviewTip` (the reviewed tip, `""` for a working review). Absent otherwise.

- [ ] **Step 1: Write the failing test**

Append to `internal/web/reviews_test.go`:

```go
const webReviewDocWithOverview = `{"version":1,"summary":"fine","files":[
 {"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"A"}]}],
 "overview":"Start at [the file](f.txt:1), then [outside](nope.go:3) and [a note](note:t1)."}`

type overviewAnchorResp struct {
	Dest    string `json:"dest"`
	Path    string `json:"path"`
	Start   int    `json:"start"`
	Missing bool   `json:"missing"`
	Plain   bool   `json:"plain"`
}

// §2.3/§4.2: a review with a stored overview sends it as overviewMd with
// its anchors resolved against the reviewed tip — a file outside the
// review's set or a note: anchor is plain — and the tip the anchors open at.
func TestReviewViewCarriesTheStoredOverview(t *testing.T) {
	t.Parallel()
	ts, _, sha, id := reviewServer(t, webReviewDocWithOverview)
	var got struct {
		SummaryMd       any                  `json:"summaryMd"`
		OverviewMd      []any                `json:"overviewMd"`
		OverviewAnchors []overviewAnchorResp `json:"overviewAnchors"`
		OverviewTip     string               `json:"overviewTip"`
	}
	if code := getJSON(t, ts, "/api/review/"+id, &got); code != http.StatusOK {
		t.Fatalf("code %d", code)
	}
	if got.SummaryMd == nil || len(got.OverviewMd) == 0 || got.OverviewTip != sha {
		t.Fatalf("summaryMd %v overviewMd %d blocks tip %q (want %q)", got.SummaryMd != nil, len(got.OverviewMd), got.OverviewTip, sha)
	}
	if len(got.OverviewAnchors) != 3 {
		t.Fatalf("anchors = %+v", got.OverviewAnchors)
	}
	if a := got.OverviewAnchors[0]; a.Dest != "f.txt:1" || a.Path != "f.txt" || a.Start != 1 || a.Plain || a.Missing {
		t.Fatalf("resolved anchor = %+v", a)
	}
	if a := got.OverviewAnchors[1]; !a.Plain || !a.Missing {
		t.Fatalf("a file outside the review must be plain: %+v", a)
	}
	if a := got.OverviewAnchors[2]; !a.Plain {
		t.Fatalf("a note: anchor must be plain (R3): %+v", a)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/pr-review-web && go test ./internal/web/ -run TestReviewViewCarriesTheStoredOverview 2>&1 | tail -5`
Expected: FAIL — `overviewMd 0 blocks tip ""`.

- [ ] **Step 3: Implement**

`internal/web/overview_http.go`, the `overviewAnchor` struct gains:

```go
	// Plain: a stored overview's anchor the review cannot open (R3) — a
	// path outside its files, a range past the file, a note: anchor. Drawn
	// plain; tab skips it; enter says why.
	Plain bool `json:"plain,omitempty"`
```

`internal/web/reviews.go` `handleReview`, after `out["meta"] = reviewMetaText(rv.Doc.Meta)`:

```go
	if rv.Doc.Overview != "" {
		addStoredOverview(ctx, svc, rv, out)
	}
```

and the helper (same file):

```go
// addStoredOverview puts a review's stored overview on the view's answer
// (spec §4.2): the document's blocks, its anchors resolved against the
// reviewed change (ReviewOverview; a plain one the review cannot open), and
// the tip they open at — "" for a working review, whose anchors open the
// working tree. An unreadable overview leaves the keys out: the ≡ Overview
// row then does not show, the review still opens.
func addStoredOverview(ctx context.Context, svc *domain.Service, rv domain.Review, out map[string]any) {
	od, err := svc.ReviewOverview(ctx, rv.ID)
	if err != nil {
		return
	}
	anchors := make([]overviewAnchor, 0, len(od.Anchors))
	for _, a := range od.Anchors {
		anchors = append(anchors, overviewAnchor{Dest: a.Dest, Path: a.Path, Start: a.Start, End: a.End, Note: a.Note,
			Label: a.Label, Missing: !a.OK, Plain: !a.OK})
	}
	out["overviewMd"] = od.Doc.Blocks
	out["overviewAnchors"] = anchors
	tip := ""
	if rv.Kind != domain.ReviewOnWorktree {
		_, tip, _ = svc.ReviewRevs(ctx, rv)
	}
	out["overviewTip"] = tip
}
```

Check `markdown.Doc`'s blocks field name (`Doc.Blocks`, as `overview_http.go` uses `doc.Blocks`). If `OverviewAnchor` embeds `agentdocs.Anchor` under a different field layout, read `internal/domain/review_overview.go:19` and adjust the field access; `Missing`/`Label` are `agentdocs.Anchor` fields (see `overview_http.go:60`).

- [ ] **Step 4: Run the test**

Run: `cd /work/gigagit/.claude/worktrees/pr-review-web && go test ./internal/web/ -run 'TestReviewView' 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 5: Commit**

Message: `web(reviews): the stored overview on the review wire — overviewMd, overviewAnchors (plain = unresolved), overviewTip`.

---

### Task 3: The "≡ Overview" row and the stored-overview viewer mode

**Files:**
- Modify: `internal/web/static/viewer.js` (pure section: `stepAnchor`, `anchorStatus`, `anchorBands`, `bandOf`, new `storedAnchorOpen`; the mode: `openStoredOverview`, `showStoredOverview`, guards)
- Modify: `internal/web/static/reviews.js` (`renderReviewFiles`: the second row; `openStoredOverview` call)
- Modify: `internal/web/static/files.js` (the `data-ovdoc` click/contextmenu)
- Modify: `internal/web/static/style.css` (`#files-list li.rovd`)
- Test: `internal/web/viewerjs_test.go` (pure), new `internal/web/storedoverviewjs_test.go` (wiring)

**Interfaces:**
- Consumes: Task 2's `overviewMd`, `overviewAnchors`, `overviewTip`; `state.review.data`, `state.review.id`, `d.label`.
- Produces: `export function openStoredOverview({ id, title, blocks, anchors, tip, text })` in `viewer.js`; pure `storedAnchorOpen(tip, a) → {src, rev, path, line}`.

- [ ] **Step 1: Write the failing pure-JS test**

Append to `internal/web/viewerjs_test.go`:

```go
// R3 / TUI follow-up 12: a stored overview's plain anchor is text — tab
// skips it, it makes no band and is never the current band, enter on it
// says why; a working review's overview (tip "") opens the working tree,
// a commit's the file at the reviewed tip.
func TestViewerStoredOverviewAnchorsJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", viewerPureStart, viewerPureEnd, `
const as = [{dest:"a:1", path:"a", start:1}, {dest:"nope:3", path:"nope", start:3, plain:true, missing:true}, {dest:"a:5-6", path:"a", start:5, end:6}];
const r = [];
r.push(stepAnchor(as, 0, 1), stepAnchor(as, 2, 1), stepAnchor(as, 2, -1), stepAnchor([as[1]], -1, 1));
r.push(anchorStatus(as[1]), anchorStatus({dest:"x", missing:true, path:"x"}), anchorStatus(as[0]));
const bands = anchorBands([{dest:"a:40", path:"a", start:40, plain:true, missing:true}, ...as], "a", 100);
r.push(JSON.stringify(bands), bandOf(bands, as, "nope:3", 100));
r.push(JSON.stringify(storedAnchorOpen("", as[0])), JSON.stringify(storedAnchorOpen("abc123", as[2])));
console.log(r.join("|"));
`)
	want := `2|0|0|-1|anchor nope:3 does not resolve at the reviewed commit|no file x||[{"start":1,"end":1,"i":1},{"start":5,"end":6,"i":3}]|-1|{"src":"worktree","rev":"","path":"a","line":1}|{"src":"commit","rev":"abc123","path":"a","line":5}`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}
```

(`anchorBands`' band shape is `{start, end, i}` — confirm against `viewer.js:203-216` and adjust the expected JSON field names to what the function builds.)

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/pr-review-web && go test ./internal/web/ -run TestViewerStoredOverviewAnchorsJS 2>&1 | tail -5`
Expected: FAIL (`storedAnchorOpen is not defined`, or `stepAnchor` landing on index 1).

- [ ] **Step 3: The pure changes (inside the guarded section of `viewer.js`)**

```js
// stepAnchor is tab / shift+tab: the next anchor that can open — a plain
// one (a stored overview's unresolved anchor) is text and is passed over.
function stepAnchor(anchors, sel, dir) {
  const n = anchors.length;
  if (!n) return -1;
  let i = sel < 0 ? (dir > 0 ? 0 : n - 1) : (((sel + dir) % n) + n) % n;
  for (let k = 0; k < n; k++, i = ((i + dir) % n + n) % n) if (!anchors[i].plain) return i;
  return -1;
}

// anchorStatus is what opening a missing anchor says ("" = it opens).
function anchorStatus(a) {
  if (a.plain) return "anchor " + a.dest + " does not resolve at the reviewed commit";
  if (!a.missing) return "";
  return a.note ? "note " + a.note + " is gone" : "no file " + a.path;
}

// storedAnchorOpen is where a stored overview's anchor opens: the file at
// the reviewed tip, or — a working review (tip "") — the working tree.
function storedAnchorOpen(tip, a) {
  return { src: tip ? "commit" : "worktree", rev: tip || "", path: a.path, line: a.start || 0 };
}
```

In `anchorBands` and `bandOf`, skip `a.plain` the way they skip a note anchor (`if (a.plain || a.note || …) return;`).

- [ ] **Step 4: Run the pure test**

Run: `cd /work/gigagit/.claude/worktrees/pr-review-web && go test ./internal/web/ -run 'TestViewer' 2>&1 | tail -3`
Expected: PASS (the older viewer tests still pass: `stepAnchor` without `plain` behaves as before).

- [ ] **Step 5: Write the failing wiring test**

Create `internal/web/storedoverviewjs_test.go`:

```go
package web

import (
	"strings"
	"testing"
)

// §4.2 / R12: the review view's second row opens the stored overview in the
// viewer's stored mode — page-local, never registered as an open file; its
// anchors open the file at the reviewed tip; Back re-shows the kept copy.
func TestStoredOverviewIsWired(t *testing.T) {
	t.Parallel()
	rv := readStatic(t, "reviews.js")
	for _, want := range []string{`data-ovdoc="1"`, `≡ Overview`, `d.overviewMd`, `openStoredOverview({`} {
		if !strings.Contains(rv, want) {
			t.Errorf("reviews.js lacks %q", want)
		}
	}
	v := readStatic(t, "viewer.js")
	for _, want := range []string{`export function openStoredOverview(`, `function showStoredOverview(`, `view.ov.stored`, `storedAnchorOpen(`, `f.stored`, `if (view.ov && view.ov.stored) return Promise.resolve(true);`} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	f := readStatic(t, "files.js")
	if !strings.Contains(f, `li.dataset.ovdoc`) {
		t.Error("files.js does not route the ≡ Overview row")
	}
	if !strings.Contains(readStatic(t, "style.css"), `li.rovd`) {
		t.Error("style.css lacks the overview row's rule")
	}
}
```

- [ ] **Step 6: Run it to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/pr-review-web && go test ./internal/web/ -run TestStoredOverviewIsWired 2>&1 | tail -8`
Expected: FAIL with the missing strings listed.

- [ ] **Step 7: The viewer's stored mode (`viewer.js`, after `showOverview`)**

```js
// ---- a review's STORED overview (spec §4.2) --------------------------------
// The same document kind as an agent's temporary overview, drawn by the
// same code, with three differences: it is page-local (never registered
// with the open-files list — nothing to focus, refresh or close there), its
// anchors open the file at the reviewed tip (a working review: the working
// tree), and Back re-shows the copy kept here. A plain anchor (the review
// could not resolve it, R3) is text: tab skips it, enter says why, no band.
export function openStoredOverview({ id, title, blocks, anchors, tip, text }) {
  if (isOpen()) rememberPlace();
  loadSeq++; // an open in flight must not land over it
  showStoredOverview({ id: "review-overview:" + id, title, blocks, anchors: anchors || [], tip: tip || "", text: text || "" });
}

function showStoredOverview(doc) {
  Object.assign(view, { id: doc.id, src: "overview", rev: "", path: "", lines: [], notes: [], cur: 0, placeholder: "", image: null, from: null, range: null, rangeOwn: false });
  view.ov = { title: doc.title, blocks: doc.blocks, anchors: doc.anchors, text: doc.text, sel: -1, want: "", stored: doc };
  const first = view.ov.anchors.findIndex((a) => !a.plain);
  if (first >= 0) view.ov.sel = first; // the TUI lands on the first resolved anchor
  viewerSearchBar.reset();
  viewerRoot.style.bottom = $("foot").offsetHeight + "px";
  pushLayer("viewer", viewerRoot, { onKey: viewerKey });
  swapFoot(true);
  paintTitle();
  renderViewer();
  $("viewer-body").scrollTop = doc.scrollTop || 0;
  $("viewer-body").focus({ preventScroll: true });
}
```

Guards, each a one-line change at the named function:
- `paintTitle`: `if (view.ov) { if (view.ov.stored) { el.title = view.ov.title; el.textContent = "Overview: " + view.ov.title; return; } … }`.
- `docName`: `if (v.ov && v.ov.stored) return "overview of review " + v.ov.stored.id.slice("review-overview:".length);`.
- `closeViewer`: post nothing for a stored overview — `if (id && !(view.ov && view.ov.stored)) { rememberPlace(); … ofQueue(…) }`; then `dropViewer()`.
- `refreshOverview`: first line `if (view.ov && view.ov.stored) return Promise.resolve(true);` (nothing to re-check: the stored text is immutable).
- `overviewKey` `Escape`: `closeViewer(view.ov.stored ? "close" : escHow(true, 0))` — a stored overview closes (nothing to background).
- `openAnchorAt`: after `const a = view.ov.anchors[view.ov.sel];` and the `missing` check (which now says the plain text through `anchorStatus`), when `view.ov.stored`:
  ```js
  if (view.ov.stored) {
    const stored = { ...view.ov.stored, scrollTop: $("viewer-body").scrollTop };
    const from = { id, sel: view.ov.sel, dest: a.dest, anchors: view.ov.anchors, cur: a.dest, stored };
    const r = await openViewer(storedAnchorOpen(stored.tip, a));
    if (!r.ok) return;
    view.from = from;
    armBack();
    rerenderKeepingScroll();
    swapFoot(true);
    return;
  }
  ```
  placed BEFORE the `const t = anchorTarget(a)` line (a stored anchor never names a note).
- `anchorBack`: first lines —
  ```js
  const f = view.from, id = view.id;
  if (f && f.stored) {
    view.from = null;
    rememberPlace();
    ofQueue({ op: "background", id: view.id, line: view.cur }); // the file an anchor opened stays open
    loadSeq++;
    showStoredOverview(f.stored);
    selectAnchor(backAnchor(view.ov.anchors, f));
    return;
  }
  ```
- `viewerFoot` (overview branch): when `view.ov.stored`, drop the `r reference` and `esc background` buttons: `<button data-vact="close">esc close</button>` instead, and no `ctrl+\ open files` claim about it being listed is needed (keep the files button; it lists the registry, which is fine).
- `copyAnchorRef`: `if (view.ov.stored) return opLine("a stored overview's anchor has no reference — copy the review link instead", false);`.
- `refreshFromAnchors(f)`: first line `if (f.stored) return;`.
- `viewBands`: unchanged (`f.anchors` holds the stored anchors; plain ones make no band after Step 3).

- [ ] **Step 8: The row and its routing**

`reviews.js` `renderReviewFiles`, right after the Summary row:

```js
  if (d.overviewMd) {
    html += `<li class="rovd" data-ovdoc="1" title="the overview stored with the review: a walk through the change, its links opening the files at the reviewed commit">≡ Overview</li>`;
  }
```

and an exported opener:

```js
// openReviewOverview opens the review's stored overview (R12) in the
// viewer's stored mode: anchors open the files at the reviewed tip.
export function openReviewOverview() {
  const rv = state.review;
  if (!rv || !rv.data.overviewMd) return;
  const d = rv.data;
  openStoredOverview({ id: rv.id, title: d.label || rv.id, blocks: d.overviewMd, anchors: d.overviewAnchors || [], tip: d.overviewTip || "", text: "" });
}
```

(`import { openStoredOverview } from "./viewer.js";` — check `viewer.js` does not import `reviews.js`; it imports `commits.js`. If a cycle appears, hang the opener on `window.__ggOpenStoredOverview` the way `__ggOpenPreviewForPair` is done.)

`files.js` click handler, before the `li.dataset.ov` branch:

```js
  if (li && li.dataset.ovdoc && reviewActive()) {
    openReviewOverview();
    return;
  }
```

contextmenu: the same `li.dataset.ovdoc` → `reviewMenu(state.review.id, e.clientX, e.clientY)` (the review's menu, as the Summary row). Keyboard: where the review file list's enter handles the Summary row (grep `onSummary` in the enter path), an "overview" cursor position is NOT added — the row is mouse/enter-by-click; the TUI's `tab`-walk parity is the viewer's own. `style.css`: `#files-list li.rovd { font-weight: bold; }` next to `li.rov`.

- [ ] **Step 9: Run the package**

Run: `cd /work/gigagit/.claude/worktrees/pr-review-web && go test ./internal/web/ 2>&1 | tail -3`
Expected: `ok`.

- [ ] **Step 10: Manual check in the browser** (the executor runs it, no subagent): build `go build -o /work/gigagit/.claude/worktrees/pr-review-web/bin/gg ./cmd/gg`, in a scratch repo save a review with an overview (`gg review save <link> --agent c --stdin` with a document carrying `"overview"`), `gg web --open`, open the review, click "≡ Overview", tab, enter (the file opens at the tip with bands), backspace (the overview returns at the same scroll), esc. Record what you SAW in the commit body.

- [ ] **Step 11: Commit**

Message: `web(reviews): the ≡ Overview row opens the stored overview in the viewer's stored mode — anchors at the reviewed tip, plain anchors as text, Back to the kept copy (R5, R12, A1)`.

---

### Task 4: A PR's stored reviews in its file list

**Files:**
- Modify: `internal/web/prnotes.go` (`handlePRNotes`: `reviews`)
- Modify: `internal/web/static/previews.js` (`loadPRCounts`)
- Modify: `internal/web/static/reviews.js` (`previewScopeReviews`, `goBack` kind `pr`)
- Modify: `internal/web/static/files.js` (`previewBack`)
- Modify: `internal/web/static/prs.js` (`window.__ggOpenPRLanding = openPRLanding;`)
- Test: new `internal/web/prnotes_reviews_test.go`; `internal/web/reviewsjs_test.go` (wiring)

**Interfaces:**
- Consumes: `svc.PreviewReviews(ctx, set)` (a PR set answers its reviews: current, older, never gone); `reviewHeads(hs)`; `state.previewOpen.pr`.
- Produces: `/api/pr/notes` key `reviews` (`[]reviewHeadWire`, never null); back shape `{kind: "pr", pr, reviewId}`.

- [ ] **Step 1: Write the failing Go test**

Create `internal/web/prnotes_reviews_test.go`:

```go
package web

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

const prReviewDoc = `{"version":1,"summary":"pr review","files":[{"path":"pr7.txt","annotations":[{"newRange":[1,1],"summary":"here"}]}]}`

// savePRReviewWeb stores a review on PR #7's scope the way /gg-review does
// (plan 1's ScopeReviewTarget over the PR's note set).
func savePRReviewWeb(t *testing.T, srv *Server, doc string) string {
	t.Helper()
	svc := srv.service()
	ctx := context.Background()
	pr, ok := srv.cachedPR(svc, 7)
	if !ok {
		t.Fatal("PR #7 is not listed")
	}
	pair := svc.PRPair(ctx, pr)
	set, err := svc.PreviewNotes(ctx, pair.Head, pair.Base)
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := svc.SaveReview(ctx, domain.SaveReview{Target: domain.ScopeReviewTarget(set), Agent: "claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// R4 / W1: a PR's stored reviews ride /api/pr/notes (the counts read the page
// makes after every open and send), the preview's reviewHead shape.
// Serial: sendServer.
func TestPRNotesListsThePRsReviews(t *testing.T) {
	ts, _, srv, _, _ := sendServerFull(t)
	id := savePRReviewWeb(t, srv, prReviewDoc)
	var out struct {
		Reviews []reviewHeadResp `json:"reviews"`
	}
	if code := getJSON(t, ts, "/api/pr/notes?n=7", &out); code != 200 || len(out.Reviews) != 1 || out.Reviews[0].ID != id || out.Reviews[0].Agent != "claude" || out.Reviews[0].Older {
		t.Fatalf("= %d %+v", code, out.Reviews)
	}
}
```

(`reviewHeadResp` is in `reviews_test.go` with `Agent` but no `Older`: add `Older bool `json:"older"`` to it.)

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/pr-review-web && go test ./internal/web/ -run TestPRNotesListsThePRsReviews 2>&1 | tail -4`
Expected: FAIL — `reviews` empty.

- [ ] **Step 3: Implement the server side**

`internal/web/prnotes.go` `handlePRNotes`: the `out` map gains `"reviews": []reviewHeadWire{}`; after the counts are set (before `writeJSON`):

```go
	// The PR's stored reviews (R4): the Reviews block of its file list, the
	// shape a merge preview's block already reads.
	if hs, herr := svc.PreviewReviews(ctx, set); herr == nil {
		out["reviews"] = reviewHeads(hs)
	}
```

- [ ] **Step 4: Run the Go test**

Expected: PASS.

- [ ] **Step 5: Write the failing wiring test** (in `reviewsjs_test.go`)

```go
// R4 / W1: the PR view's Reviews block reads /api/pr/notes' reviews; a
// review opened from it returns to the PR (kind "pr"), never to a commit.
func TestPRReviewsBlockIsWired(t *testing.T) {
	t.Parallel()
	if p := readStatic(t, "previews.js"); !strings.Contains(p, `state.previewReviews = d.reviews || [];\n  renderFiles();\n}\n\n\n// loadPreviewCounts`) && !strings.Contains(strings.Split(p, "export async function loadPRCounts")[1], `state.previewReviews = d.reviews || [];`) {
		t.Error("loadPRCounts does not keep the PR's reviews")
	}
	rv := readStatic(t, "reviews.js")
	if strings.Contains(rv, `po.pr ? [] :`) {
		t.Error("previewScopeReviews still hides a pull request's reviews")
	}
	if !strings.Contains(rv, `back.kind === "pr"`) || !strings.Contains(rv, `window.__ggOpenPRLanding`) {
		t.Error("goBack has no pr arm")
	}
	if !strings.Contains(readStatic(t, "files.js"), `{ kind: "pr", pr: po.pr, reviewId }`) {
		t.Error("previewBack does not name the pull request")
	}
	if !strings.Contains(readStatic(t, "prs.js"), `window.__ggOpenPRLanding = openPRLanding;`) {
		t.Error("prs.js does not publish openPRLanding for the review's way back")
	}
}
```

(Simplify the first check to `strings.Contains(strings.SplitN(p, "export async function loadPRCounts", 2)[1], "state.previewReviews = d.reviews || [];")`.)

- [ ] **Step 6: Run it to verify it fails**, then implement:

`previews.js` `loadPRCounts`: after `state.previewGroups = …` add `state.previewReviews = d.reviews || [];`.
`reviews.js` `previewScopeReviews`: `return state.previewReviews || [];` on the `po` branch (drop `po.pr ? [] :`); its comment: "The opened merge preview's — or pull request's — reviews".
`files.js` `previewBack`: first line `if (po && po.pr) return { kind: "pr", pr: po.pr, reviewId };` before the preview arm.
`reviews.js` `goBack`, before the `preview` arm:

```js
  if (back && back.kind === "pr" && window.__ggOpenPRLanding) {
    window.__ggOpenPRLanding(back.pr).then((ok) => {
      if (!ok) return goBack({ kind: "list" });
      state.reviewSel = back.reviewId || "";
      renderFiles();
    });
    return;
  }
```

`prs.js`, next to `window.__ggRefreshPRs = refreshPRs;`: `window.__ggOpenPRLanding = openPRLanding;`.
`openReview`'s doc comment (reviews.js ~410) lists the new back shape.

- [ ] **Step 7: Run the package**

Run: `cd /work/gigagit/.claude/worktrees/pr-review-web && go test ./internal/web/ 2>&1 | tail -3`
Expected: `ok`.

- [ ] **Step 8: Commit**

Message: `web(pr): a pull request's stored reviews head its file list (R4) — /api/pr/notes reviews, Back to the PR`.

---

### Task 5: `GET /api/pr/send/candidates`

**Files:**
- Modify: `internal/web/prsend.go` (route + handler + wire types)
- Test: new `internal/web/prsendpanel_test.go`

**Interfaces:**
- Consumes: `svc.PRSendCandidates(ctx, n) (domain.SendCandidates, error)` — `PR, Head, Groups[]{ID, Kind, Agent, Title, Created, Slot, Rows[]{ID, Kind, Severity, Path, Range [2]int, Side, Summary, Rationale, Sync model.SyncState, Code []string, Skip string}}`; `svc.PRDetailsCached(n)` for `own_pr`.
- Produces: JSON `{pr, head, own_pr, groups: [{id, kind, agent, title, created, slot, rows: [{id, kind, severity, path, range: [a,b], side, summary, rationale, sync, code: [], skip}]}]}` — `groups`, `rows` and `code` never null.

- [ ] **Step 1: Write the failing test**

Create `internal/web/prsendpanel_test.go`:

```go
package web

import (
	"testing"
)

type candRowResp struct {
	ID, Kind, Severity, Path, Side, Summary, Rationale, Sync, Skip string
	Range                                                          [2]int
	Code                                                           []string
}

type candGroupResp struct {
	ID, Kind, Agent, Title, Created string
	Slot                            int
	Rows                            []candRowResp
}

type candResp struct {
	PR     int             `json:"pr"`
	Head   string          `json:"head"`
	OwnPR  bool            `json:"own_pr"`
	Groups []candGroupResp `json:"groups"`
}

// §5.1 / §5.4: the panel's list — each AI review newest first, then my
// notes, then draft replies; a row the planner would skip is listed with
// its reason; code lines ride along. Serial: sendServer.
func TestPRSendCandidatesEndpoint(t *testing.T) {
	ts, _, srv, head, _ := sendServerFull(t)
	rid := savePRReviewWeb(t, srv, `{"version":1,"summary":"Looks fine","files":[
 {"path":"pr7.txt","annotations":[{"newRange":[1,1],"summary":"check","meta":{"severity":"bug"}}]},
 {"path":"other.go","annotations":[{"newRange":[1,1],"summary":"not in the PR"}]}]}`)
	mine := addWebNote(t, ts, head, "pr7.txt", 1, "mine")
	var out candResp
	if code := getJSON(t, ts, "/api/pr/send/candidates?n=7", &out); code != 200 {
		t.Fatalf("code %d", code)
	}
	if out.PR != 7 || out.Head != head || len(out.Groups) != 2 {
		t.Fatalf("header/groups = %+v", out)
	}
	rev, my := out.Groups[0], out.Groups[1]
	if rev.ID != "review:"+rid || rev.Kind != "review" || rev.Agent != "claude" || rev.Title != "Looks fine" || rev.Slot == 0 || len(rev.Rows) != 2 {
		t.Fatalf("review group = %+v", rev)
	}
	if r := rev.Rows[0]; r.ID != "review:"+rid+":0" || r.Kind != "remark" || r.Severity != "bug" || r.Path != "pr7.txt" || r.Range != [2]int{1, 1} || r.Skip != "" || len(r.Code) != 1 {
		t.Fatalf("remark row = %+v", r)
	}
	if r := rev.Rows[1]; r.Path != "other.go" || r.Skip == "" {
		t.Fatalf("a remark on a file the PR does not change must carry its skip reason: %+v", r)
	}
	if my.ID != "mine" || my.Kind != "mine" || len(my.Rows) != 1 || my.Rows[0].ID != mine || my.Rows[0].Kind != "note" || my.Rows[0].Code == nil {
		t.Fatalf("mine = %+v", my)
	}
	if code := getJSON(t, ts, "/api/pr/send/candidates?n=99", nil); code != 404 {
		t.Errorf("unknown PR = %d, want 404", code)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/pr-review-web && go test ./internal/web/ -run TestPRSendCandidatesEndpoint 2>&1 | tail -4`
Expected: FAIL — 404 on the route.

- [ ] **Step 3: Implement**

In `prsend.go`'s `init`: `mux.HandleFunc("GET /api/pr/send/candidates", s.handlePRSendCandidates)`. Wire types and the handler:

```go
// The send panel's list (spec §5.4): the domain's SendCandidates, row for
// row — ids the page posts back are checked against the PR's notes again
// when it sends (prSendRequest), never trusted from here.
type candidateRowWire struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Severity  string   `json:"severity"`
	Path      string   `json:"path"`
	Range     [2]int   `json:"range"`
	Side      string   `json:"side"`
	Summary   string   `json:"summary"`
	Rationale string   `json:"rationale"`
	Sync      string   `json:"sync"`
	Code      []string `json:"code"`
	Skip      string   `json:"skip"`
}

type candidateGroupWire struct {
	ID      string             `json:"id"`
	Kind    string             `json:"kind"`
	Agent   string             `json:"agent"`
	Title   string             `json:"title"`
	Created string             `json:"created"`
	Slot    int                `json:"slot"`
	Rows    []candidateRowWire `json:"rows"`
}

func candidatesWire(c domain.SendCandidates) []candidateGroupWire {
	out := make([]candidateGroupWire, 0, len(c.Groups))
	for _, g := range c.Groups {
		gw := candidateGroupWire{ID: g.ID, Kind: g.Kind, Agent: g.Agent, Title: g.Title, Slot: g.Slot, Rows: []candidateRowWire{}}
		if !g.Created.IsZero() {
			gw.Created = wireTime(g.Created)
		}
		for _, r := range g.Rows {
			code := r.Code
			if code == nil {
				code = []string{}
			}
			gw.Rows = append(gw.Rows, candidateRowWire{ID: r.ID, Kind: r.Kind, Severity: r.Severity, Path: r.Path, Range: r.Range,
				Side: r.Side, Summary: r.Summary, Rationale: r.Rationale, Sync: string(r.Sync), Code: code, Skip: r.Skip})
		}
		out = append(out, gw)
	}
	return out
}

// handlePRSendCandidates is the send panel's list (spec §5.4).
func (s *Server) handlePRSendCandidates(w http.ResponseWriter, r *http.Request) {
	svc, pr, ok := s.knownPR(w, r)
	if !ok {
		return
	}
	ctx := readCtx(r)
	c, err := svc.PRSendCandidates(ctx, pr.Number)
	if err != nil {
		writeErr(w, prSendLookupStatus(err), err)
		return
	}
	own := false
	if p, _, ok := svc.PRDetailsCached(pr.Number); ok {
		own = p.ViewerDidAuthor
	}
	writeJSON(w, map[string]any{"pr": c.PR, "head": c.Head, "own_pr": own, "groups": candidatesWire(c)})
}
```

(`wireTime` exists — `reviews.go:50` uses it. `model.SyncState` is a string type; confirm with `grep -n "type SyncState" internal/model/*.go`.)

- [ ] **Step 4: Run the test**

Expected: PASS.

- [ ] **Step 5: Commit**

Message: `web(pr): GET /api/pr/send/candidates — the send panel's list (§5.4)`.

---

### Task 6: `/api/pr/send` kind `notes` with `verdict` and `body_from`; kind `group` and `/api/pr/send/groups` removed

**Files:**
- Modify: `internal/web/prsend.go`
- Test: `internal/web/prsend_test.go`

**Interfaces:**
- Consumes: `domain.PRSendRequest{Notes, Verdict, Body, BodySet, BodyFrom}`; the domain refuses a `BodyFrom` the PR does not own (`ErrSendRequest` → 400 via `sendErrStatus`).
- Produces: `prSendWire{Kind, IDs, Body, BodySet, Verdict, BodyFrom}`; kinds `notes | verdict | resolve | unresolve | finish | discard`.

- [ ] **Step 1: Write the failing tests**

In `prsend_test.go`: replace `TestWebSendGroupsListsMine` with

```go
// §5.4: the panel's send is kind notes with a verdict and the body taken
// from a review (body_from): one review op — start, the remark, submit
// with that body. Serial: sendServer.
func TestWebSendNotesWithVerdictAndBodyFrom(t *testing.T) {
	ts, wf, srv, head, _ := sendServerFull(t)
	rid := savePRReviewWeb(t, srv, prReviewDoc)
	mine := addWebNote(t, ts, head, "pr7.txt", 1, "mine too")
	code, out := postJSONAny(t, ts, "/api/pr/send?n=7", `{"kind":"notes","ids":["review:`+rid+`:0","`+mine+`"],"verdict":true,"body_from":"`+rid+`"}`)
	if code != 202 {
		t.Fatalf("start = %d %v", code, out)
	}
	plan, _ := json.Marshal(out["plan"])
	if !strings.Contains(string(plan), `"verdict":true`) || !strings.Contains(string(plan), "pr review") {
		t.Fatalf("plan = %s (want the verdict and the review's summary as the body)", plan)
	}
	evs := followDecide(t, ts, out["op_id"].(string), "comment")
	done, _ := findEvent(evs, "done")
	if done["ok"] != true || !strings.Contains(wf.writeLog(), "SubmitReview COMMENT") {
		t.Fatalf("done %v, writes %s", done, wf.writeLog())
	}
	// A typed body wins over body_from; a body_from the PR does not own is refused.
	if code, _ := postJSONAny(t, ts, "/api/pr/send?n=7", `{"kind":"notes","ids":["`+mine+`"],"body_from":"deadbeef"}`); code != 400 && code != 404 {
		t.Fatalf("a foreign body_from = %d, want a refusal", code)
	}
}
```

and in `TestWebSendRefusesForgedValues` replace the `group` case with `{"n=7", `{"kind":"group","group":"mine"}`, 400},` (the kind is gone: unknown kind → 400) and add `{"n=7", `{"kind":"notes","ids":["review:deadbeef:0"]}`, 400},` (a remark of a review the PR does not list). Add:

```go
// The groups route left with kind group (W6).
func TestWebSendGroupsRouteIsGone(t *testing.T) {
	ts, _, _ := sendServer(t)
	if code := getJSON(t, ts, "/api/pr/send/groups?n=7", nil); code != 404 && code != 405 {
		t.Fatalf("/api/pr/send/groups = %d, want gone", code)
	}
}
```

(`followDecide(…, "comment")`: the review confirm's options are the verdicts; `TestPRSendMixedAnsweredAtATerminal` in the CLI answers `comment`. If the web decision names the option differently, read the decision event's options in the test and pick `comment`.)

- [ ] **Step 2: Run them to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/pr-review-web && go test ./internal/web/ -run 'TestWebSendNotesWithVerdictAndBodyFrom|TestWebSendRefusesForgedValues|TestWebSendGroupsRouteIsGone' 2>&1 | tail -8`
Expected: FAIL (the verdict/body ignored; the groups route answers 200; kind group answers 400 already — fine).

- [ ] **Step 3: Implement**

`prSendWire`:

```go
type prSendWire struct {
	Kind    string   `json:"kind"` // notes | verdict | resolve | unresolve | finish | discard
	IDs     []string `json:"ids"`
	Body    string   `json:"body"`
	BodySet bool     `json:"body_set"`
	// Verdict and BodyFrom ride a notes send (the panel, spec §5.4): one
	// GitHub review with a verdict, its body a stored review's summary
	// unless a typed body (BodySet) wins.
	Verdict  bool   `json:"verdict"`
	BodyFrom string `json:"body_from"`
}
```

In `prSendRequest`, the `notes` arm: after `req.Notes, err = pick(notes)` add

```go
			req.Verdict, req.Body, req.BodySet = in.Verdict, in.Body, in.BodySet
			if in.BodyFrom != "" {
				if !strings.HasPrefix(in.BodyFrom, "review:") && len(in.BodyFrom) > 64 {
					return req, badSend{errors.New("bad body_from")}
				}
				req.BodyFrom = strings.TrimPrefix(in.BodyFrom, "review:") // the domain checks the PR owns it
			}
```

Delete the `case "group":` arm, `handlePRSendGroups`, its route, and the `Group` field. The file's header comment drops "a group id". `sendErrStatus` already maps `ErrSendRequest` (a foreign `BodyFrom`) to 400 and `ErrReviewNotFound`? — check: a gone review from `applyBodyFrom` is `ErrReviewNotFound`; add `errors.Is(err, domain.ErrReviewNotFound)` → `http.StatusNotFound` to `sendErrStatus`.

- [ ] **Step 4: Run the send tests**

Run: `cd /work/gigagit/.claude/worktrees/pr-review-web && go test ./internal/web/ -run 'TestWebSend|TestPRSend' 2>&1 | tail -3`
Expected: PASS (the groups-related test replaced).

- [ ] **Step 5: Commit**

Message: `web(pr): /api/pr/send kind notes takes verdict and body_from; kind group and /api/pr/send/groups are gone (§5.4, W6)`.

---

### Task 7: The pure panel builders in `prsendrows.js`

**Files:**
- Modify: `internal/web/static/prsendrows.js`
- Test: `internal/web/prsendjs_test.go`

**Interfaces:**
- Produces (all pure, exported):
  - `sendRows(n, pr)` — unchanged but without the `send-review` row (R9).
  - `panelRows(cands, ticked)` → `[{kind: "group", id, title, agent, created, slot, n, ticked, all}, {kind: "row", id, group, slot, tickable, ticked, severity, where, summary, rationale, code, skip, sync}]` — `where` = `path:start` or `path:start-end`; a skip row is `tickable: false`; a tick of an id not in `cands` is ignored (Review Focus 2).
  - `bodyOptions(cands, ticked)` → `[{value: "none", label: "no body"}, {value: "review:<id>", label: "review text (<agent>)"}…, {value: "typed", label: "typed"}]` — one per ticked review group, newest first (the group order), typed last.
  - `panelRequest(cands, ticked, body)` → `{kind: "notes", ids, verdict: true, body_from?} | {…, body, body_set: true}` — `ids` in list order; `null` when nothing is ticked; `body = {kind: "none"|"review"|"typed", from, typed}`.
  - `tickAllInGroup(cands, ticked, groupId)` → the new ticked Set: every tickable row of the group ticked, or — all already ticked — none (the TUI's `a`).

- [ ] **Step 1: Write the failing node test**

In `prsendjs_test.go`, change `TestPRSendRowsJS`'s `want` (`send-review` gone):

```go
		"local":  {"send:Send as GitHub comment"},
		"failed": {"send:Retry sending as GitHub comment"},
		"remark": {"send:Send as GitHub comment"},
```

Add:

```go
// The panel's pure builders (spec §5.4, the TUI's send_panel.go row for row).
func TestPRSendPanelBuildersJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "prsendrows.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prsendrows.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	const runner = `
import { panelRows, bodyOptions, panelRequest, tickAllInGroup } from "./prsendrows.mjs";
const cands = { pr: 42, head: "abc", groups: [
  { id: "review:r1", kind: "review", agent: "Claude Code", title: "two nits", created: "2026-10-09T14:02:00Z", slot: 2, rows: [
    { id: "review:r1:1", kind: "remark", severity: "high", path: "internal/git/pull.go", range: [120, 134], side: "new", summary: "lock released twice", rationale: "why", code: ["a", "b"], skip: "", sync: "local" },
    { id: "review:r1:2", kind: "remark", severity: "low", path: "internal/tui/x.go", range: [5, 5], side: "new", summary: "nit", code: [], skip: "its lines changed", sync: "local" } ] },
  { id: "mine", kind: "mine", agent: "", title: "", created: "", slot: 1, rows: [
    { id: "n1", kind: "note", severity: "", path: "README.md", range: [12, 12], side: "new", summary: "typo", code: [], skip: "", sync: "local" } ] },
  { id: "replies", kind: "replies", agent: "", title: "", created: "", slot: 0, rows: [
    { id: "d1", kind: "reply", severity: "", path: "internal/git/pull.go", range: [120, 120], side: "new", summary: "addressed", code: [], skip: "", sync: "local" } ] },
] };
const t0 = new Set(["review:r1:1", "d1", "gone"]);
const rows = panelRows(cands, t0);
const shape = rows.map((r) => r.kind === "group" ? "G:" + r.id + ":" + r.n + ":" + (r.all ? "all" : r.ticked ? "some" : "none") : "R:" + r.id + ":" + r.where + ":" + (r.tickable ? "t" : "x") + (r.ticked ? "+" : "-") + (r.skip ? ":" + r.skip : ""));
const opts = bodyOptions(cands, t0).map((o) => o.value + "=" + o.label);
const req1 = panelRequest(cands, t0, { kind: "review", from: "r1" });
const req2 = panelRequest(cands, t0, { kind: "typed", typed: "hello" });
const req3 = panelRequest(cands, new Set(), { kind: "none" });
const req4 = panelRequest(cands, t0, { kind: "none" });
const all = [...tickAllInGroup(cands, t0, "review:r1")].sort();
const none = [...tickAllInGroup(cands, new Set(["review:r1:1"]), "review:r1")].sort();
console.log(JSON.stringify({ shape, opts, req1, req2, req3, req4, all, none }));
`
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(runner), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := `{"shape":["G:review:r1:2:all","R:review:r1:1:internal/git/pull.go:120-134:t+","R:review:r1:2:internal/tui/x.go:5:x-:its lines changed","G:mine:1:none","R:n1:README.md:12:t-","G:replies:1:all","R:d1:internal/git/pull.go:120:t+"],` +
		`"opts":["none=no body","review:r1=review text (Claude Code)","typed=typed"],` +
		`"req1":{"kind":"notes","ids":["review:r1:1","d1"],"verdict":true,"body_from":"r1"},` +
		`"req2":{"kind":"notes","ids":["review:r1:1","d1"],"verdict":true,"body":"hello","body_set":true},` +
		`"req3":null,"req4":{"kind":"notes","ids":["review:r1:1","d1"],"verdict":true},` +
		`"all":["d1","review:r1:1"],"none":["review:r1:1"]}`
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}
```

Read the `want` twice: a group is `all` when every TICKABLE row is ticked (`review:r1`'s skip row does not count — one tickable row, ticked → all); `none` for `tickAllInGroup` on a group with everything ticked clears it, so `["review:r1:1"]` ticked alone and then "all/none" on `review:r1` → the group had its one tickable row ticked → it clears → the Set is empty → `[]`. Fix the expected `none` to `[]` accordingly: `"none":[]`. (The executor verifies the expected values against the rules below, not the other way round.)

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/pr-review-web && go test ./internal/web/ -run 'TestPRSendRowsJS|TestPRSendPanelBuildersJS' 2>&1 | tail -6`
Expected: FAIL — `send-review` still listed; `panelRows` not exported.

- [ ] **Step 3: Implement (`prsendrows.js`)**

Remove the `send-review` push from `sendRows` and its comment line ("and its group's review"). Append:

```js
// --- the send panel (spec §5.4; the TUI's send_panel.go row for row) ---
// panelRows lays the panel's list: a header per group (its colour slot, the
// agent and date for a review, the all/none state of its tickable rows),
// then its rows — a skip row (the planner would refuse it) is listed with
// the reason and is not tickable. ticked is a Set of ids; an id the
// candidates no longer list is ignored (a stale tick after a refresh).
export function panelRows(cands, ticked) {
  const out = [];
  for (const g of cands.groups || []) {
    const rows = g.rows || [];
    const tickable = rows.filter((r) => !r.skip);
    const on = tickable.filter((r) => ticked.has(r.id)).length;
    out.push({ kind: "group", id: g.id, title: g.title || "", agent: g.agent || "", created: g.created || "", slot: g.slot || 0,
      n: rows.length, ticked: on > 0, all: tickable.length > 0 && on === tickable.length, gkind: g.kind });
    for (const r of rows) {
      out.push({ kind: "row", id: r.id, group: g.id, slot: g.slot || 0, tickable: !r.skip, ticked: !r.skip && ticked.has(r.id),
        severity: r.severity || "", where: whereOf(r), summary: r.summary || "", rationale: r.rationale || "",
        code: r.code || [], skip: r.skip || "", sync: r.sync || "" });
    }
  }
  return out;
}

function whereOf(r) {
  const [a, b] = r.range || [0, 0];
  return r.path + ":" + a + (b > a ? "-" + b : "");
}

// bodyOptions is the body <select>: no body, each TICKED AI review's text
// (newest first — the groups' order), then a typed body.
export function bodyOptions(cands, ticked) {
  const opts = [{ value: "none", label: "no body" }];
  for (const g of cands.groups || []) {
    if (g.kind !== "review" || !(g.rows || []).some((r) => !r.skip && ticked.has(r.id))) continue;
    opts.push({ value: g.id, label: "review text (" + (g.agent || "AI") + ")" });
  }
  opts.push({ value: "typed", label: "typed" });
  return opts;
}

// panelRequest is what Send posts: the ticked ids in list order, a
// verdict, and the body — a review's summary (body_from), typed text
// (body + body_set) or none. null with nothing ticked.
export function panelRequest(cands, ticked, body) {
  const ids = [];
  for (const g of cands.groups || []) for (const r of g.rows || []) if (!r.skip && ticked.has(r.id)) ids.push(r.id);
  if (!ids.length) return null;
  const req = { kind: "notes", ids, verdict: true };
  if (body && body.kind === "review" && body.from) req.body_from = body.from;
  else if (body && body.kind === "typed") Object.assign(req, { body: body.typed || "", body_set: true });
  return req;
}

// tickAllInGroup is the header's checkbox (the TUI's a): every tickable row
// of the group ticked — or, all of them ticked already, none.
export function tickAllInGroup(cands, ticked, groupId) {
  const g = (cands.groups || []).find((x) => x.id === groupId);
  const next = new Set(ticked);
  if (!g) return next;
  const tickable = (g.rows || []).filter((r) => !r.skip);
  const all = tickable.length > 0 && tickable.every((r) => next.has(r.id));
  for (const r of tickable) all ? next.delete(r.id) : next.add(r.id);
  return next;
}
```

- [ ] **Step 4: Run the node tests**

Run: `cd /work/gigagit/.claude/worktrees/pr-review-web && go test ./internal/web/ -run 'TestPRSendRowsJS|TestPRSendPanelBuildersJS' 2>&1 | tail -3`
Expected: PASS (adjust the `want` only where your reading of the rules above was wrong, never the rules).

- [ ] **Step 5: Commit**

Message: `web(prsend): the panel's pure builders — panelRows, bodyOptions, panelRequest, tickAllInGroup; the note menu loses the group rows (R9)`.

---

### Task 8: The "Send to GitHub…" panel overlay

**Files:**
- Create: `internal/web/static/prsendpanel.js`
- Modify: `internal/web/static/prsend.js` (the PR menu row, removals, help text, `export sendToGitHub` stays)
- Modify: `internal/web/static/index.html` (the overlay skeleton), `style.css`, `app.js`
- Test: `internal/web/prsendjs_test.go` (`TestPRSendModuleIsWired` updated + `TestPRSendPanelIsWired`)

**Interfaces:**
- Consumes: Task 5's endpoint, Task 6's request shape, Task 7's builders; `sendToGitHub(n, body, label, onRefused)`, `onSendDone`, `onHeadMoved` (prsend.js); `landNote(id)` + `openFile`/`state.files` (files.js) to open a row's file; `pushLayer/closeLayer/mountOverlay` (layers.js); `opLine` (ops.js); `registerHelp`, `registerRows("pr")`.
- Produces: `export function openSendPanel(n)`; page state `panels: Map<pr, {ticked: Set, body}>`.

- [ ] **Step 1: Write the failing wiring test**

In `prsendjs_test.go`, `TestPRSendModuleIsWired`: drop `"/api/pr/send/groups?n=" + pr` from the wants; add a new test:

```go
// §5.4 / R7: the PR menu's Send to GitHub… opens the panel; Send review…
// and its helpers are gone; the panel module is imported, reads the
// candidates, posts through sendToGitHub, and keeps its ticks per PR.
func TestPRSendPanelIsWired(t *testing.T) {
	t.Parallel()
	js := readStatic(t, "prsend.js")
	for _, want := range []string{`label: "Send to GitHub…"`, `openSendPanel(pr.number)`, `label: "Verdict…"`, `export async function sendToGitHub(`} {
		if !strings.Contains(js, want) {
			t.Errorf("prsend.js lacks %q", want)
		}
	}
	for _, gone := range []string{`sendReviewPick`, `sendReviewBody`, `groupCount`, `"Send review…"`, `kind: "group"`} {
		if strings.Contains(js, gone) {
			t.Errorf("prsend.js still has %q", gone)
		}
	}
	p := readStatic(t, "prsendpanel.js")
	for _, want := range []string{`"/api/pr/send/candidates?n=" + n`, `panelRows(`, `bodyOptions(`, `panelRequest(`, `tickAllInGroup(`, `sendToGitHub(`, `pushLayer("prsendpanel"`, `nothing to send to #`, `registerHelp({`, `onSendDone(`, `onHeadMoved(`} {
		if !strings.Contains(p, want) {
			t.Errorf("prsendpanel.js lacks %q", want)
		}
	}
	if !strings.Contains(readStatic(t, "app.js"), `import "./prsendpanel.js";`) {
		t.Error("app.js does not import prsendpanel.js")
	}
	if !strings.Contains(readStatic(t, "index.html"), `id="prsendpanel"`) {
		t.Error("index.html has no panel overlay")
	}
	if !strings.Contains(readStatic(t, "style.css"), `#prsendpanel`) {
		t.Error("style.css has no panel rules")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**, then implement.

- [ ] **Step 3: `prsend.js`**

Remove `groupCount`, `sendReviewBody`, `sendReviewPick` and the `keptBody` arms that only they used (keep `keptBody` for the verdict: `verdictBody` and `onSendDone` stay). The PR menu:

```js
registerRows("pr", (pr) => {
  if (!pr || pr.state !== "open" || !pr.fetched) return [];
  return [
    { sep: true },
    { label: "Send to GitHub…", act: () => openSendPanel(pr.number) },
    { label: "Verdict…", act: () => verdictBody(pr.number) },
  ];
});
```

(`import { openSendPanel } from "./prsendpanel.js";` — `prsendpanel.js` imports `sendToGitHub`/`onSendDone`/`onHeadMoved` from `prsend.js`: a cycle. Avoid it: `prsendpanel.js` registers the row itself (`registerRows("pr", …)` for "Send to GitHub…") and `prsend.js` keeps only "Verdict…"; the wiring test then expects `label: "Send to GitHub…"` in `prsendpanel.js` — adjust the test's file for that one string.) The help text: "The pull request's right-click menu has <b>Send to GitHub…</b> — one panel over every unsent note, remark and draft reply, nothing ticked on open — and <b>Verdict…</b>. Right-click a note: Send as GitHub comment, and on a GitHub thread Reply &amp; send…, Resolve / Reopen on GitHub, Send draft replies."

- [ ] **Step 4: `index.html`** (next to `#prdetails`):

```html
<div id="prsendpanel" class="hidden">
  <div id="prsendpanel-box">
    <h2><span id="prsendpanel-title"></span><button id="prsendpanel-close" title="close (esc)">esc</button></h2>
    <div id="prsendpanel-notice"></div>
    <ul id="prsendpanel-list"></ul>
    <div id="prsendpanel-body">
      <label>Body <select id="prsendpanel-bodysel"></select></label>
      <textarea id="prsendpanel-typed" class="hidden" rows="4" placeholder="the review body"></textarea>
    </div>
    <div id="prsendpanel-foot"><span id="prsendpanel-hints">↑/↓ move  space tick  a all/none in group  b body  c code  enter open  ctrl+s send  esc close</span><button id="prsendpanel-send">Send</button></div>
  </div>
</div>
```

- [ ] **Step 5: `prsendpanel.js`**

```js
// prsendpanel.js — the Send to GitHub… panel (spec §5.4, R6/R7): every unsent
// local comment of a pull request — each AI review's remarks, my notes, the
// draft replies — nothing ticked on open; the user ticks any mix, picks a
// body, and ONE GitHub review goes out through the ordinary confirm
// (sendToGitHub → the decision modal). Row building is pure (prsendrows.js,
// node-tested); this file owns the fetch, the overlay, the keys and the
// page state: the ticks and the body are kept per PR while the page lives,
// so enter (a row's file in the diff) and esc do not lose them.
import { $, esc, getJSON, state } from "./core.js";
import { closeLayer, pushLayer } from "./layers.js";
import { registerHelp, registerRows } from "./menus.js";
import { opLine } from "./ops.js";
import { landNote, openFile } from "./files.js";
import { bodyOptions, panelRequest, panelRows, tickAllInGroup } from "./prsendrows.js";
import { onHeadMoved, onSendDone, sendToGitHub } from "./prsend.js";

const kept = new Map(); // pr -> { ticked: Set, body: {kind, from, typed} }
let panel = null; // { pr, cands, rows, sel, open: Set(row ids unfolded), code: bool, notice }

function keptFor(pr) {
  if (!kept.has(pr)) kept.set(pr, { ticked: new Set(), body: { kind: "none", from: "", typed: "" } });
  return kept.get(pr);
}

export async function openSendPanel(n) {
  let cands;
  try {
    cands = await getJSON("/api/pr/send/candidates?n=" + n);
  } catch (e) {
    opLine("send to #" + n + ": " + (e.message || e), true);
    return;
  }
  if (!(cands.groups || []).length) {
    opLine("nothing to send to #" + n, false);
    return;
  }
  const k = keptFor(n);
  panel = { pr: n, cands, sel: 0, open: new Set(), code: false, notice: "" };
  if (k.body.kind === "review" && !cands.groups.some((g) => g.id === "review:" + k.body.from)) k.body = { kind: "none", from: "", typed: k.body.typed };
  $("prsendpanel-title").textContent = "Send to GitHub — #" + n;
  pushLayer("prsendpanel", $("prsendpanel"), { onKey });
  render();
  panel.sel = firstCandidate();
  render();
}

function close() {
  panel = null;
  closeLayer("prsendpanel");
}

function rows() {
  return panelRows(panel.cands, keptFor(panel.pr).ticked);
}

function firstCandidate() {
  return Math.max(0, rows().findIndex((r) => r.kind === "row"));
}

function render() {
  if (!panel) return;
  const k = keptFor(panel.pr), rs = rows();
  const n = rs.filter((r) => r.kind === "row" && r.ticked).length;
  $("prsendpanel-list").innerHTML = rs
    .map((r, i) => {
      const sel = i === panel.sel ? " sel" : "";
      if (r.kind === "group") {
        const head = r.gkind === "review" ? (r.agent || "AI") + (r.title ? " · " + r.title : "") : r.gkind === "mine" ? "My notes" : "Draft replies";
        return `<li class="pgrp g${r.slot}${sel}" data-i="${i}"><input type="checkbox" data-g="${esc(r.id)}" ${r.all ? "checked" : ""} ${r.ticked && !r.all ? 'class="some"' : ""}> ${esc(head)}${r.created ? `<span class="pdate">${esc(r.created.slice(0, 10))}</span>` : ""}</li>`;
      }
      const box = r.tickable ? `<input type="checkbox" data-id="${esc(r.id)}" ${r.ticked ? "checked" : ""}>` : `<span class="pskip">–</span>`;
      const sev = r.severity ? `<span class="psev ${esc(r.severity)}">${esc(r.severity)}</span>` : "";
      const more = panel.open.has(r.id) || panel.code
        ? `<div class="pmore">${r.rationale ? `<div class="prat">${esc(r.rationale)}</div>` : ""}${r.code.length ? `<pre class="pcode">${esc(r.code.join("\n"))}</pre>` : ""}</div>`
        : "";
      return `<li class="prow g${r.slot}${sel}${r.tickable ? "" : " pdis"}" data-i="${i}">${box} ${sev}<a class="pwhere" data-id="${esc(r.id)}">${esc(r.where)}</a> ${esc(r.summary)}${r.skip ? `<span class="pskipwhy"> (skipped: ${esc(r.skip)})</span>` : ""}<span class="pfold">${r.rationale || r.code.length ? "▸" : ""}</span>${more}</li>`;
    })
    .join("");
  const sel = $("prsendpanel-bodysel");
  const opts = bodyOptions(panel.cands, k.ticked);
  const value = k.body.kind === "review" ? "review:" + k.body.from : k.body.kind;
  sel.innerHTML = opts.map((o) => `<option value="${esc(o.value)}" ${o.value === value ? "selected" : ""}>${esc(o.label)}</option>`).join("");
  if (!opts.some((o) => o.value === value)) k.body = { kind: "none", from: "", typed: k.body.typed };
  $("prsendpanel-typed").classList.toggle("hidden", k.body.kind !== "typed");
  $("prsendpanel-typed").value = k.body.typed || "";
  $("prsendpanel-send").textContent = "Send " + n;
  $("prsendpanel-send").disabled = n === 0;
  $("prsendpanel-notice").textContent = panel.notice ? "▸ " + panel.notice : "";
  $("prsendpanel-list").querySelector("li.sel")?.scrollIntoView({ block: "nearest" });
}

function move(d) {
  const n = rows().length;
  panel.sel = Math.max(0, Math.min(n - 1, panel.sel + d));
  panel.notice = "";
  render();
}

function tick(r) {
  const k = keptFor(panel.pr);
  if (r.kind === "group") k.ticked = tickAllInGroup(panel.cands, k.ticked, r.id);
  else if (!r.tickable) panel.notice = r.where + " " + r.summary + " — skipped: " + r.skip;
  else k.ticked.has(r.id) ? k.ticked.delete(r.id) : k.ticked.add(r.id);
  render();
}

// cycleBody is b: none → each ticked review's text (newest first) → typed → none.
function cycleBody() {
  const k = keptFor(panel.pr), opts = bodyOptions(panel.cands, k.ticked);
  const cur = k.body.kind === "review" ? "review:" + k.body.from : k.body.kind;
  const next = opts[(Math.max(0, opts.findIndex((o) => o.value === cur)) + 1) % opts.length].value;
  setBody(next);
}

function setBody(value) {
  const k = keptFor(panel.pr);
  if (value === "typed") k.body = { kind: "typed", from: "", typed: k.body.typed };
  else if (value.startsWith("review:")) k.body = { kind: "review", from: value.slice("review:".length), typed: k.body.typed };
  else k.body = { kind: "none", from: "", typed: k.body.typed };
  render();
  if (value === "typed") $("prsendpanel-typed").focus();
}

// openRow is enter / a path click: the row's file in the PR diff, the
// cursor on its thread; the panel closes and reopens with its ticks (A6
// on the web: the page keeps them per PR).
function openRow(r) {
  const po = state.previewOpen;
  if (!po || po.pr !== panel.pr || state.filesMode !== "compare") {
    panel.notice = "open the pull request to see the file";
    return render();
  }
  const i = (state.files || []).findIndex((f) => f.path === r.where.split(":")[0]);
  if (i < 0) {
    panel.notice = r.where.split(":")[0] + " is not in the pull request's file list";
    return render();
  }
  close();
  landNote(r.id);
  openFile(i);
}

function send() {
  const k = keptFor(panel.pr);
  if (k.body.kind === "typed") k.body.typed = $("prsendpanel-typed").value;
  const req = panelRequest(panel.cands, k.ticked, k.body);
  if (!req) {
    panel.notice = "tick something to send";
    return render();
  }
  const pr = panel.pr;
  panel.notice = "preparing the send to #" + pr + "…";
  render();
  sendToGitHub(pr, req, "sending to #" + pr, (err) => {
    if (panel && panel.pr === pr) {
      panel.notice = "send: " + (err.message || err);
      render();
    }
  });
}

// A send of this PR that reached GitHub closes the panel and drops its
// kept ticks and typed body; an abort or failure leaves everything.
onSendDone((n, ev, body) => {
  if (!ev.ok || !body || body.kind !== "notes" || !body.verdict) return; // only the panel's own send
  kept.delete(n);
  if (panel && panel.pr === n) close();
});

// The head moved: the diff re-opens on the new head; the panel's list is
// stale — it closes, the ticks stay for the reopen (ids no longer listed
// are ignored by panelRows).
onHeadMoved((n) => {
  if (panel && panel.pr === n) close();
});

function onKey(e) {
  if (!panel) return false;
  if (e.target === $("prsendpanel-typed")) {
    if (e.key === "Escape") { $("prsendpanel-typed").blur(); e.preventDefault(); return true; }
    if (e.key === "s" && e.ctrlKey) { e.preventDefault(); send(); return true; }
    return true; // typing
  }
  const rs = rows(), r = rs[panel.sel];
  switch (e.key) {
    case "Escape": close(); break;
    case "ArrowDown": case "j": move(1); break;
    case "ArrowUp": case "k": move(-1); break;
    case "PageDown": move(10); break;
    case "PageUp": move(-10); break;
    case "Home": panel.sel = 0; render(); break;
    case "End": panel.sel = rs.length - 1; render(); break;
    case " ": if (r) tick(r); break;
    case "a": if (r) { keptFor(panel.pr).ticked = tickAllInGroup(panel.cands, keptFor(panel.pr).ticked, r.kind === "group" ? r.id : r.group); render(); } break;
    case "b": cycleBody(); break;
    case "c": panel.code = !panel.code; render(); break;
    case "Enter": if (r && r.kind === "row") openRow(r); else if (r) tick(r); break;
    case "s": if (!e.ctrlKey) return false; send(); break;
    default: return false;
  }
  e.preventDefault();
  return true;
}

$("prsendpanel-close").addEventListener("click", close);
$("prsendpanel-send").addEventListener("click", send);
$("prsendpanel-bodysel").addEventListener("change", (e) => setBody(e.target.value));
$("prsendpanel-typed").addEventListener("input", (e) => { keptFor(panel.pr).body.typed = e.target.value; });
$("prsendpanel-list").addEventListener("click", (e) => {
  if (!panel) return;
  const li = e.target.closest("li");
  if (!li) return;
  const i = Number(li.dataset.i), r = rows()[i];
  panel.sel = i;
  const box = e.target.closest("input[type=checkbox]");
  if (box) { tick(r); return; }
  const where = e.target.closest("a.pwhere");
  if (where && r.kind === "row") { openRow(r); return; }
  const fold = e.target.closest(".pfold");
  if (fold && r.kind === "row") { panel.open.has(r.id) ? panel.open.delete(r.id) : panel.open.add(r.id); }
  render();
});

registerRows("pr", (pr) => {
  if (!pr || pr.state !== "open" || !pr.fetched) return [];
  return [{ label: "Send to GitHub…", act: () => openSendPanel(pr.number) }];
});

registerHelp({
  key: "Send to GitHub…",
  html:
    "a pull request's right-click menu opens one panel over every unsent local comment — each AI review's remarks, " +
    "<b>My notes</b>, <b>Draft replies</b> — nothing ticked. <b>space</b> ticks, <b>a</b> all/none in the group, " +
    "<b>b</b> cycles the body (none, a ticked review's text, typed), <b>c</b> shows the code under every row, " +
    "<b>enter</b> opens the row's file in the diff (the panel reopens with its ticks), <b>ctrl+s</b> / Send posts ONE GitHub " +
    "review behind the ordinary confirm; a skipped row says why and cannot be ticked",
});
```

Check the names exported by `files.js`: `landNote` and `openFile` are in its export list (`files.js:5367`); read `landNote`'s signature (`grep -n "^function landNote" -A6`) — if it expects a tag or a diff context, pass what `allnotes.js`' `openTarget` passes.

Row ordering of the PR menu: `prsend.js`'s `registerRows("pr")` adds `{sep}` + "Verdict…"; `prsendpanel.js`'s adds "Send to GitHub…" — registration order follows `app.js` imports (import `prsendpanel.js` right after `prsend.js` so "Send to GitHub…" lands under the separator before "Verdict…"; else move the separator into the panel's contributor). Confirm by reading `extraRows` (menus.js:44): contributors run in registration order.

- [ ] **Step 6: `style.css`**

```css
/* The Send to GitHub… panel (spec §5.4): the TUI's send_panel.go over a
   layer; a group's colour is its left bar (the note groups' palette). */
#prsendpanel { position: fixed; inset: 0; z-index: 30; background: rgba(0,0,0,.45); display: flex; align-items: center; justify-content: center; }
#prsendpanel.hidden { display: none; }
#prsendpanel-box { background: var(--bg); color: var(--fg); border: 1px solid var(--dim); border-radius: 6px; width: min(900px, 92vw); max-height: 86vh; display: flex; flex-direction: column; }
#prsendpanel-box h2 { margin: 0; padding: 8px 12px; font-size: 1em; display: flex; justify-content: space-between; }
#prsendpanel-notice { padding: 0 12px; min-height: 1.4em; color: var(--accent); }
#prsendpanel-list { list-style: none; margin: 0; padding: 0 8px; overflow: auto; flex: 1; }
#prsendpanel-list li { padding: 3px 8px; border-left: 4px solid transparent; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
#prsendpanel-list li.sel { background: var(--sel); }
#prsendpanel-list li.pgrp { font-weight: bold; margin-top: 6px; }
#prsendpanel-list li.pdis { color: var(--dim); }
#prsendpanel-list li.g1 { border-left-color: var(--note-group-1); } #prsendpanel-list li.g2 { border-left-color: var(--note-group-2); }
#prsendpanel-list li.g3 { border-left-color: var(--note-group-3); } #prsendpanel-list li.g4 { border-left-color: var(--note-group-4); }
#prsendpanel-list li.g5 { border-left-color: var(--note-group-5); } #prsendpanel-list li.g6 { border-left-color: var(--note-group-6); }
#prsendpanel-list .psev { font-size: .85em; padding: 0 4px; border-radius: 3px; background: var(--dim); color: var(--bg); margin-right: 4px; }
#prsendpanel-list .pwhere { color: var(--accent); cursor: pointer; margin-right: 6px; }
#prsendpanel-list .pdate, #prsendpanel-list .pskipwhy { color: var(--dim); margin-left: 8px; }
#prsendpanel-list .pfold { float: right; color: var(--dim); cursor: pointer; }
#prsendpanel-list .pmore { white-space: pre-wrap; color: var(--dim); padding: 2px 0 2px 24px; }
#prsendpanel-list .pcode { margin: 2px 0 0; font-family: var(--mono, monospace); }
#prsendpanel-body { padding: 6px 12px; display: flex; flex-direction: column; gap: 4px; }
#prsendpanel-foot { padding: 8px 12px; display: flex; justify-content: space-between; align-items: center; color: var(--dim); }
```

(Use the page's existing variable names — grep `--sel`, `--accent`, `--dim`, `--bg`, `--fg` in `style.css`; mirror `#prdetails`'s rules for the box when they differ.)

- [ ] **Step 7: Run the package, then the browser check**

Run: `cd /work/gigagit/.claude/worktrees/pr-review-web && go test ./internal/web/ 2>&1 | tail -3` → `ok`.
Browser: build the worktree's `bin/gg`, a scratch repo with a fake gh (`forgetest` is a Go package; use the TUI's e2e `tui_pr_send` scenario's set-up as the recipe, or a real repo with a PR you own), open the PR, right-click → Send to GitHub…: the list (nothing ticked), space/a/b/c, enter opens the file and the panel reopens with the ticks, Send shows the confirm; abort. Record what you SAW in the commit body.

- [ ] **Step 8: Commit**

Message: `web(pr): the Send to GitHub… panel (§5.4, R6, R7) replaces Send review…`.

---

### Task 9: Copy note link — the note menu and the all-notes view

**Files:**
- Modify: `internal/web/notes.go` (`GET /api/notes/link`)
- Modify: `internal/web/static/files.js` (the note menu row)
- Modify: `internal/web/static/allnotes.js` (pure `anCopyLinkURL`, `ctrl+l`, the hint)
- Test: new `internal/web/notelink_test.go`; `internal/web/allnotesjs_test.go`; `internal/web/notesjs_test.go` (wiring)

**Interfaces:**
- Consumes: `svc.NoteLinkText(ctx, id) (string, error)` — `ErrNoteLinkGone` (404), a shelf/forge note (400).
- Produces: `GET /api/notes/link?id=<id>` → `{link}`; `anCopyLinkURL(row)` → `"/api/notes/link?id=…"` | `"/api/review/<id>/link"` | `""`.

- [ ] **Step 1: Write the failing Go test**

Create `internal/web/notelink_test.go`:

```go
package web

import (
	"strings"
	"testing"
)

// R13 / §7.2 web: a note's link (gg://…?note=<id>) comes from the domain's
// one builder; a reply gets its thread's link with its own id; a gone note
// is 404; a GitHub comment or a shelf note has no link (400).
// Serial: sendServer.
func TestNoteLinkEndpoint(t *testing.T) {
	ts, _, head := sendServer(t)
	id := addWebNote(t, ts, head, "pr7.txt", 1, "here")
	var got struct {
		Link string `json:"link"`
	}
	if code := getJSON(t, ts, "/api/notes/link?id="+id, &got); code != 200 || !strings.HasSuffix(got.Link, "/pr7.txt@main...refs/gg/pr/7:1?note="+id) {
		t.Fatalf("= %d %q", code, got.Link)
	}
	code, rep := postJSONAny(t, ts, "/api/notes/reply", `{"id":"`+id+`","summary":"and"}`)
	rid, _ := rep["id"].(string)
	if code != 200 || rid == "" {
		t.Fatalf("reply = %d %v", code, rep)
	}
	if code := getJSON(t, ts, "/api/notes/link?id="+rid, &got); code != 200 || !strings.HasSuffix(got.Link, "?note="+rid) {
		t.Fatalf("reply link = %d %q", code, got.Link)
	}
	if code := getJSON(t, ts, "/api/notes/link?id=forge:C1", nil); code != 400 {
		t.Errorf("a GitHub comment = %d, want 400", code)
	}
	if code := getJSON(t, ts, "/api/notes/link?id=deadbeef", nil); code != 404 {
		t.Errorf("a gone note = %d, want 404", code)
	}
	if code := getJSON(t, ts, "/api/notes/link", nil); code != 400 {
		t.Errorf("no id = %d, want 400", code)
	}
}
```

(Check `/api/notes/reply`'s request shape in `notes.go` `handleNoteReply` — the field may be `parent` rather than `id`; read it and adjust.)

- [ ] **Step 2: Run it to verify it fails** (404 on the route).

- [ ] **Step 3: Implement**

`notes.go` `init`: `mux.HandleFunc("GET /api/notes/link", s.handleNoteLink)`. Handler:

```go
// handleNoteLink is Copy note link (R13): the note's gg:// link from the
// domain's one builder — a reply gets its thread's link with its own id.
// 404 a note that is gone; 400 a note that has no link (a GitHub comment,
// a shelf note) or no id.
func (s *Server) handleNoteLink(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeErr(w, http.StatusBadRequest, errors.New("a note id is required"))
		return
	}
	link, err := s.service().NoteLinkText(readCtx(r), id)
	switch {
	case errors.Is(err, domain.ErrNoteLinkGone), errors.Is(err, domain.ErrReviewNotFound):
		writeErr(w, http.StatusNotFound, err)
		return
	case err != nil:
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, map[string]any{"link": link})
}
```

- [ ] **Step 4: Run it** → PASS.

- [ ] **Step 5: Write the failing JS tests**

`allnotesjs_test.go`: the harness builds `an.mjs` from single functions by name (`jsFunc(t, "allnotes.js", "anBuildRows")` …): add `jsFunc(t, "allnotes.js", "anCopyLinkURL")` to `mod` and `anCopyLinkURL` to its `export { … }` line and to the driver's `import`; add to the driver's `console.log` object `links: rows.filter((r) => r.kind === "note" || r.kind === "review").map((r) => anCopyLinkURL(r))`, and to the Go `got` struct `Links []string `json:"links"`` with the expected list in the fixture's row order: `n1` (worktree note) → `/api/notes/link?id=n1`, review `r1` → `/api/review/r1/link`, `n2`, `n3`, `n4` (a missing commit's note still has a link: the store holds it) → `/api/notes/link?id=…`, the shelf entry notes `e1`, `e2` → `""`, `n5` (a shelved file's note: its file row's target has `state: "shelf"`) → `""`, `e3` → `""`. `anCopyLinkURL` must be a top-level `function` whose closing brace sits at column 0 (jsFunc's extraction rule).

`notesjs_test.go` (or `prsendjs_test.go`), a wiring pin:

```go
// R13 / W8: the note box's menu copies the clicked note's link through
// /api/notes/link; View all notes copies on ctrl+l and says so.
func TestCopyNoteLinkIsWired(t *testing.T) {
	t.Parallel()
	f := readStatic(t, "files.js")
	if !strings.Contains(f, `label: "Copy note link"`) || !strings.Contains(f, `"/api/notes/link?id=" + encodeURIComponent(n.id)`) {
		t.Error("files.js: the note menu has no Copy note link row")
	}
	a := readStatic(t, "allnotes.js")
	for _, want := range []string{`function anCopyLinkURL(`, `e.key === "l" && e.ctrlKey`, `"ctrl+l copy link"`} {
		if !strings.Contains(a, want) {
			t.Errorf("allnotes.js lacks %q", want)
		}
	}
}
```

- [ ] **Step 6: Run them to verify they fail**, then implement:

`files.js` note menu, after the remark rows and before `nlink`:

```js
  // Copy note link (R13): a thread's own link, ?note=<id>, from the
  // domain's builder — a hand-written thread or a draft reply; a remark
  // keeps its remark link, a GitHub comment has none.
  if (!remark && n.source !== "forge" && !String(n.id).startsWith("forge:"))
    noteRows.push({ label: "Copy note link", act: () => copyServerLink("/api/notes/link?id=" + encodeURIComponent(n.id), "note " + n.id) });
```

`allnotes.js`, in the `an*` pure section (next to `anVisible`):

```js
// anCopyLinkURL is what ctrl+l copies on row r: a thread's note link, a
// review's review link; "" for a shelf note (no link) and any other row.
function anCopyLinkURL(r) {
  if (!r) return "";
  if (r.kind === "review") return "/api/review/" + encodeURIComponent(r.review.id) + "/link";
  if (r.kind !== "note" || !r.target || r.target.shelf || r.target.state === "shelf") return "";
  return "/api/notes/link?id=" + encodeURIComponent(r.note.id);
}
```

`anKey`:

```js
  if (e.key === "l" && e.ctrlKey) {
    e.preventDefault();
    const url = anCopyLinkURL(visible()[an.sel]);
    if (url) copyServerLink(url, "link");
    return true;
  }
```

(`copyServerLink` is exported from `reviews.js` — `allnotes.js` already imports from `reviews.js`.) `render()`'s hints: after the `ctrl+d delete` line, `if (anCopyLinkURL(cur)) hints.push("ctrl+l copy link");`. The help row (`registerHelp` in allnotes.js ~576) mentions ctrl+l.

- [ ] **Step 7: Run the package** → `ok`.

- [ ] **Step 8: Commit**

Message: `web(notes): Copy note link — /api/notes/link, the note menu's row, ctrl+l in View all notes (R13)`.

---

### Task 10: Docs — README, CHANGELOG, CLAUDE-details, using-gg

**Files:**
- Modify: `README.md` (~line 567 gg web paragraph; the review-view section's web words — grep `≡ Overview` and `gg web` in README)
- Modify: `CHANGELOG.md` (a new top section "PR review, plan 3: the web")
- Modify: `docs/CLAUDE-details.md` (a block after plan 2's, ~line 6108)
- Modify: `internal/agentskill/using-gg.md` (`--body-from`: "a review whose every remark and summary are on GitHub is removed from the store, like `--review`; one with a stored overview is kept"), `internal/agentskill/agentskill.go` `Version` 161 → 162

- [ ] **Step 1: README** — the gg web paragraph becomes:

> In **gg web**, a pull request's diff shows the same marks and group colours as the terminal UI: right-click a note for *Send as GitHub comment*, and on a GitHub thread *Reply & send…*, *Resolve / Reopen on GitHub*, *Send draft replies*; the pull request's right-click menu has *Send to GitHub…* — one panel over every unsent note, remark and draft reply, nothing ticked on open; tick any mix, pick the body (none, a ticked review's text, typed), *Send* — and *Verdict…*. Each send opens the same confirm, listing what will be posted. A pull request's stored reviews head its file list; a review's *≡ Summary* row shows its text and, when one was stored, *≡ Overview* opens the overview whose links open the files at the reviewed commit. Right-click a note for *Copy note link*; in *View all notes* `ctrl+l` copies it.

Keep the TUI paragraphs plan 2 wrote; add "(the web page: the same, as a panel overlay)" only where the TUI text names the panel.

- [ ] **Step 2: CHANGELOG** — the section lists: `summaryMd`/`overviewMd` + the ≡ Summary / ≡ Overview rows (the viewer's stored mode, plain anchors), the PR Reviews block (`/api/pr/notes` reviews, W1), the panel (`/api/pr/send/candidates`, `prsendpanel.js`, `prsendrows.js` builders, kept ticks per PR), `/api/pr/send` `verdict`/`body_from` and the removals (kind group, the groups route, Send review…, the note menu's group rows), Copy note link (`/api/notes/link`, the menu row, ctrl+l), and the amendments W1–W8 in one line each.

- [ ] **Step 3: CLAUDE-details** — a block "PR review, plan 3 — the web" in the style of plan 2's: the wire keys, `addStoredOverview`, the viewer's stored mode and its guards (which functions check `view.ov.stored` / `f.stored`), `previewScopeReviews`/`goBack` kind pr, the candidates handler, `prSendWire`'s new fields, the panel's page state, `handleNoteLink`, `anCopyLinkURL`.

- [ ] **Step 4: using-gg + Version**, then `go test ./internal/agentskill/ ./internal/web/` and `gg init --update` (refreshes the user's installed skill copies — say so in the handover).

- [ ] **Step 5: Commit**

Message: `docs: PR review plan 3 (web) — README, CHANGELOG, CLAUDE-details, using-gg --body-from note (v162)`.

---

## After the last task

1. `./test.sh race` in the worktree, output to a log file, sandbox disabled, in the background; green only on the literal `all green` and zero `FAIL` / `DATA RACE` lines.
2. A read-only reviewer subagent (Opus) over the whole branch with this plan, the spec and the Review Focus list; fix Critical/Important test-first in ONE pass; minors to the handover.
3. Hand over for the user's "merge": `git merge-base --is-ancestor main feat/pr-review-send-panel-web && gg merge -F <msgfile> --into main feat/pr-review-send-panel-web` from `/work/gigagit`, `test -z "$(git status --short)"`, `./build.sh install`, remove the worktree and branch, update memory (`pr-review-send-panel-feature.md`). Also `./build.sh web` after the web merge (Windows exe delivery, memory rule).
