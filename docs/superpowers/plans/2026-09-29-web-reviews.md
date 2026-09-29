# Reviews in gg web — Implementation Plan

> **For agentic workers:** executed INLINE by the session that wrote it (project rule: never subagents). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** gg web lists a commit's and a branch's stored AI reviews, opens one in a review view (overview + files + the review's read-only notes in the diffs) and deletes one after a confirm — the TUI's behaviour.

**Architecture:** Three read routes on the web server over existing domain reads (`NoteCounts.Reviews`, `ReviewsForCommit`, `Review`, `ReviewRevs`, `ReviewFiles`, `ReviewFileCounts`, `ReviewOtherNotes`, `ReviewNotesFor`); delete reuses `POST /api/notes/remove`. The page overlays a `state.review` on the existing commit (single-commit review) or compare (range review) files mode, so diff URLs, the S stack and numstat keep working; only the note lane, the list rows and the Overview pane are review-specific. Pure row/label helpers live in a new `static/reviews.js`.

**Tech Stack:** Go 1.26 (`internal/web`, `internal/domain`), vanilla ES modules (`internal/web/static`), node for pure-JS guards, playwright for the browser probe.

**Spec:** `docs/superpowers/specs/2026-09-29-web-reviews-design.md`

## Global Constraints

- Web is a domain-only frontend (archtest): no `internal/git`/`internal/notes` imports in `internal/web`.
- Every wire value that can reach git argv passes `isGitArgSafe` / `isHexSha`; a review id never reaches argv.
- Row text: `└ <YYYY-MM-DD HH:MM local> <agent>` (commit), `└ Review: <YYYY-MM-DD HH:MM> <agent>` (branch) — absolute dates, never "2h".
- Review rows carry NO `data-i` (files list) and NO `data-n` (branches), are not draggable.
- The review view's note lane is exclusive: only the review's notes; add/edit/reply/remove refuse.
- Delete confirm: options `["cancel", "delete"]` (esc = cancel).
- Reviews are never read on cursor movement — only on an explicit open or a counts refresh.
- No subagents; never `git add -A` (a built `gg` may be in the tree); never push.

## Review Focus

1. A commit with NO reviews renders byte-identically to today (no heading, no empty section) — Task 4 asserts it.
2. Opening another commit / compare / the working tree after a review drops the review overlay (no stale Overview row, notes lane back to normal) — Task 5's `reviewActive` identity rule + wiring test.
3. A review deleted elsewhere (TUI, another tab) while its view is open: counts refresh removes rows; the open view's next read 404s → leaves like esc — Task 7.
4. A range review whose base is unresolvable falls back to the commit view (domain already does); the page must not assume `range` — Task 3 test covers `range:false` shape.
5. Review notes on a path absent from the reviewed files appear only under "Other notes", never in a diff — Task 3 test.

---

### Task 1: Review notes are read-only on the wire

**Files:**
- Modify: `internal/domain/notewire.go` (ToWireNote)
- Test: `internal/domain/notewire_test.go` (create or append)

- [ ] **Step 1: failing test** — `TestToWireNoteMarksReviewNotesReadOnly`: `ToWireNote(ResolvedNote{Note: model.Note{ID: "review:abc:0", Source: model.NoteSourceAgent}})` → `ReadOnly == true`; an ordinary agent note id `"n1"` → `ReadOnly == false`.
- [ ] **Step 2:** `go test ./internal/domain -run TestToWireNoteMarksReviewNotesReadOnly` → FAIL.
- [ ] **Step 3:** in `ToWireNote` after the forge block: `if model.IsReviewNoteID(r.Note.ID) { w.ReadOnly = true }`.
- [ ] **Step 4:** test PASS; `go test ./internal/domain ./internal/cli ./internal/mcp` green.
- [ ] **Step 5:** commit `feat(domain): a review's notes are read-only on the wire`.

### Task 2: Review heads on counts and on a commit

**Files:**
- Create: `internal/web/reviews.go` (routes + wire helpers for Tasks 2–3)
- Modify: `internal/web/notes.go` (`handleNoteCounts` adds `reviews`), `internal/web/commits.go` (`handleCommitFiles` adds `reviews`)
- Test: `internal/web/reviews_test.go`

**Interfaces — Produces:**
```go
type reviewHeadWire struct {
	ID      string `json:"id"`
	Commit  string `json:"commit"`
	Branch  string `json:"branch,omitempty"`
	Agent   string `json:"agent"`
	Summary string `json:"summary"`
	Created string `json:"created"` // RFC3339 UTC, "" when zero
}
func reviewHeads(hs []domain.ReviewHead) []reviewHeadWire // never nil
func reviewHeadsOf(rs []domain.Review) []reviewHeadWire  // never nil
```
Test fixture helper (reviews_test.go): `reviewServer(t, text string) (ts *httptest.Server, svc *domain.Service, sha, id string)` — `newRepoDir(t, 2)`, `svc.UseNotesDir(t.TempDir())`, `SaveReview` of HEAD with `ReviewTarget{Kind: domain.ReviewRange, Range: sha+"^.."+sha, Label: sha[:7]}`, agent "Claude". Doc constant `webReviewDoc` = the TUI's `reviewViewDoc` retargeted to `f.txt` (note on newRange [1,1]) plus a note on `zzz.go`.

- [ ] **Step 1: failing tests** — `TestNoteCountsCarryReviews` (GET /api/notes/counts → `reviews` has one entry with id, commit=sha, agent "Claude", non-empty created); `TestNoteCountsReviewsIsArrayWhenNone` (notesServer → `"reviews":[]` in raw body); `TestCommitFilesCarryReviews` (GET /api/commit/sha → reviews[0].id == id; the parent commit → `reviews: []`).
- [ ] **Step 2:** run `go test ./internal/web -run 'Reviews|CarryReviews'` → FAIL.
- [ ] **Step 3:** implement helpers; counts: `"reviews": reviewHeads(c.Reviews)`; commit: `rs, _ := svc.ReviewsForCommit(ctx, sha); body["reviews"] = reviewHeadsOf(rs)` (best-effort; the commit is a full sha only when the page sent one — resolve with `svc.ResolveRev` first if `sha` is short; ReviewsForCommit compares full shas).
- [ ] **Step 4:** tests PASS.
- [ ] **Step 5:** commit `feat(web): review heads on note counts and on a commit`.

### Task 3: The review view's reads

**Files:** `internal/web/reviews.go`, `internal/web/reviews_test.go`

**Interfaces — Produces:**
- `GET /api/review/{id}` → JSON:
  `{id, agent, created, branch, commit, label, structured bool, base, tip, range bool, files:[{path,status,old_path}], counts:{path:n}, summaries:{path:text}, overviewMd, meta, text, notes int, note_files int, other:[{path,line,summary}]}`
  - 404 on `domain.ErrReviewNotFound`.
  - `label` = branch + " " + commit[:7], or commit[:7].
  - `overviewMd` = `markdown.Parse(doc.Overview)`; prose → `markdown.Parse(r.Text)`, `structured:false`, files/counts empty.
  - `other[].line` uses addReviewDoc's form ("2-3", "-4").
- `GET /api/review/notes?id=&path=&status=&old=` → `{"notes":[wireNote…]}` with `read_only:true`.
  - Builds the diff through `svc.Differ().Diff` with the SAME key/sources `/api/diff` uses: commit review key `tip+"^.."+tip+":"+path`, range key `base+".."+tip+":"+path`; then `svc.ReviewNotesFor(ctx, id, path, d)`.
  - 400 on unsafe path/old; 404 unknown id.

- [ ] **Step 1: failing tests** —
  - `TestReviewViewShape`: structured, range false, tip == sha, files contain f.txt, counts["f.txt"]==1, summaries["f.txt"]=="adds A", other has zzz.go, notes==2, note_files==2, overviewMd non-null.
  - `TestReviewViewProse`: text "just prose" → structured false, files empty.
  - `TestReviewViewUnknown404`.
  - `TestReviewNotesReadOnlyOnFile`: `/api/review/notes?id=&path=f.txt&status=M` → 1 note, read_only true, id prefix "review:", line 1.
  - `TestReviewNotesOtherPathEmpty`: path=g.txt → 0 notes.
  - `TestReviewNotesRejectsUnsafePath`: path=`-x` → 400.
- [ ] **Step 2:** run → FAIL (404 route).
- [ ] **Step 3:** implement (register in `init()` via `RegisterRoutes`; `readCtx(r)` for reads).
- [ ] **Step 4:** PASS; `go vet ./internal/web`.
- [ ] **Step 5:** commit `feat(web): review view reads (/api/review/{id}, /api/review/notes)`.

### Task 4: Reviews rows on a commit's file list

**Files:**
- Create: `internal/web/static/reviews.js` — pure section between `// --- reviews pure ---` / `// --- end reviews pure ---` markers: `reviewStamp(iso)` → `"YYYY-MM-DD HH:MM"` local ("" for bad input), `reviewRowText(r)` → `"└ " + [stamp, agent].filter(Boolean).join(" ")` falling back to the summary, `branchReviewText(r)` → `"└ Review: " + …`, `branchReviews(reviews, b)` → reviews with `r.branch === b.name && b.hash && r.commit.startsWith(b.hash)`.
- Modify: `internal/web/static/commits.js` (`openCommit`/`openCommitByHash` store `state.commitReviews = body.reviews || []`; `openStashDetail` sets `[]`), `internal/web/static/files.js` (`renderFiles` commit branch), `style.css` (`li.rev`).
- Test: `internal/web/reviewsjs_test.go`

- [ ] **Step 1: failing tests** — node test over the pure section: stamp format, row text with/without agent, fallback to summary, branchReviews matching a short hash and rejecting another branch / an old tip. Static test: files.js contains `data-review=` and the review row branch never emits `data-i`; renderFiles emits the `Reviews` heading only when `state.commitReviews.length`.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3:** implement. In `renderFiles` (commit mode, not review mode): prefix `<li class="sect">Reviews</li>` + `<li class="rev" data-review="${esc(id)}" title="${esc(summary)}">${esc(reviewRowText(r))}</li>` per review, then a `<li class="sect">Files</li>` heading only when reviews exist (TUI's layout). Clicks: files-list click handler routes `li[data-review]` → `openReview(id, {from: "commit"})` (Task 5). Contextmenu → `reviewMenu(id, x, y)` (Task 7).
- [ ] **Step 4:** PASS.
- [ ] **Step 5:** commit `feat(web): a commit's reviews above its files`.

### Task 5: The review view

**Files:** `static/reviews.js` (openReview, overview pane, reviewActive, esc back), `static/files.js` (renderFiles review branch, openFile guard, commitDiffCtx/rowNoteCtx/noteQuery/notesFor lane, write refusals, drillOut back), `static/keys.js` (n/p, cursor onto Overview), `static/stackview.js` (numstat in review = existing modes — no change expected), `style.css`, `index.html` (none expected: the Overview renders into `#diff-body`).

**Interfaces — Produces:**
```js
// state.review = { id, data /* /api/review/{id} payload */, files /* the array assigned to state.files */,
//                  cmp /* state.compare object for a range review, else null */, onOverview: bool,
//                  back: {kind:"commit", sha, title, reviewId} | {kind:"list"} }
function reviewActive()   // state.review && layout !== "list" && (cmp ? state.compare === cmp : state.files === files)
async function openReview(id, back)
function showReviewOverview()
function stepReviewFile(dir)  // n/p in the list
function leaveReview()        // esc from the files stage of a review
```
Behaviour:
- `openReview`: `const gen = ++state.detailGen`; files pane shows "Opening the review…" (enter files stage first, title "Review …"); fetch `/api/review/{id}`; 404 → `opLine("the review is gone", true)` and `leaveReview()`; else set mode: single → `filesMode="commit"`, `fileSha=tip`, `files=data.files`; range → `filesMode="compare"`, `state.compare={a: base.slice(0,7), b: tip.slice(0,7), aHash: base, bHash: tip, all: files, filter: "all"}`, `state.files = files`. Title `Review <label>`, meta `agent · date · N notes on M files`. `onOverview = true`; open the diff layout with the Overview.
- `renderFiles` in review: first `<li class="rov${sel}" data-ov="1">≡ Overview</li>`, then file rows (`data-i`) with `◆n` from `data.counts` and, under a noted file, `<li class="rsum">` (dim summary, no data attrs). `.sel` goes to Overview when `onOverview`.
- Clicking a file row / j,k off Overview sets `onOverview=false` (openFile path); clicking Overview or k at index 0 → `onOverview=true`, `showReviewOverview()` (tears down the stack, renders into `#diff-body`: markdown via `mdHTML`, meta, "Other notes" list, Copy button → `copyText(data.text, "the review")`).
- Note lane: `commitDiffCtx(f)` returns `{path, rev: tip, state: "commit", notes: true, review: id, compare: false}` when `reviewActive()`; `rowNoteCtx` goes through it (before the compare arms); `noteQuery` with `ctx.review` → `path, id, status, old`; `notesFor` → `/api/review/notes?`. `addNotePrompt` refuses when `ad.ctx && ad.ctx.review` ("a review's notes are read-only"); `readOnlyNote` message says "a review's notes are read-only" for `review:` ids.
- keys: in the files pane of a review, `n`/`p` → `stepReviewFile(±1)` (next file with count > 0, stays at ends, opens it); `moveCursor(-1)` at index 0 → Overview.
- esc (`drillOut`): diff → files keeps the review (unchanged path); files → `leaveReview()`: `back.kind==="commit"` → `openCommitByHash(back.sha, back.title)` then select the review row (`state.reviewSel = back.reviewId` read by renderFiles to add `.sel`); else the list stage.

- [ ] **Step 1: failing tests** — static wiring test (`reviewsview_test.go`): files.js `noteQuery` has a `ctx.review` arm, `notesFor` maps it to `/api/review/notes?`, `addNotePrompt` refuses on `ctx.review`, `rowNoteCtx` consults the review ctx before the compare arms; reviews.js has `++state.detailGen` in openReview and `reviewActive` checks identity (`state.files === ` / `state.compare === `). Node test of `reviewActive` with fake state objects (pure: takes state as a parameter — `reviewActiveIn(st)`), and `nextNotedFile(files, counts, from, dir)`.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3:** implement.
- [ ] **Step 4:** PASS; `./test.sh unit` web package green.
- [ ] **Step 5:** commit `feat(web): the review view (overview, files, read-only review notes)`.

### Task 6: Branch review sub-rows

**Files:** `static/sidebar.js` (renderBranches, click, contextmenu), `static/files.js` (`refreshNoteCounts` keeps `reviews` and calls `renderBranches`), `style.css` (`li.brev`), `reviewsjs_test.go` (static asserts).

- [ ] **Step 1: failing tests** — static: sidebar.js renders `class="brev"` rows with `data-review` and without `data-n`/`draggable`; the click listener opens `li.brev` via `openReview`; refreshNoteCounts stores `reviews`.
- [ ] **Step 2:** FAIL.
- [ ] **Step 3:** implement: after each branch `<li>`, `branchReviews(state.noteCounts.reviews || [], b).map(r => <li class="brev" data-review=… title=summary>esc(branchReviewText(r))</li>)`; click → `openReview(id, {kind: "list"})`; contextmenu on `li.brev` → `reviewMenu`. Guards: the existing listeners bail on `!li.dataset.n` already — the new branch runs before them. Drag handlers: verify they read `data-n` and skip the sub-row.
- [ ] **Step 4:** PASS.
- [ ] **Step 5:** commit `feat(web): a branch's reviews as sub-rows`.

### Task 7: Delete review

**Files:** `static/reviews.js` (`reviewMenu`, `deleteReview`), `static/files.js` (Overview row contextmenu), tests.

Behaviour: `reviewMenu(id, x, y)` → `showCtxMenu([{label: "Delete review", danger: true, act: () => confirmDeleteReview(id)}], x, y)`. Confirm: `showLocalConfirm("Delete review by <agent> (<stamp>)?", ["cancel", "delete"], cb)`; on "delete": `postJSON("/api/notes/remove", {id})` (404 treated as done), then `refreshNoteCounts()`; if the open commit shows it → drop from `state.commitReviews` and `renderFiles()`; if the open review is it → `leaveReview()`; `toast`/`opLine("review deleted")`. Uses `runOnce("review-delete", …)`.

- [ ] **Step 1: failing tests** — static: reviews.js calls `/api/notes/remove`, confirm options are exactly `["cancel", "delete"]`, the Overview row and both review row kinds have a contextmenu route to `reviewMenu`. Go: `TestNoteRemoveDeletesReview` (POST remove with review id → GET /api/review/{id} 404, counts reviews empty).
- [ ] **Step 2:** FAIL.
- [ ] **Step 3:** implement.
- [ ] **Step 4:** PASS.
- [ ] **Step 5:** commit `feat(web): delete a review (confirm, cancel default)`.

### Task 8: Browser probe, docs, gate

- [ ] **Step 1:** build the UNFIXED main binary and this branch's binary; scratch clone of a test repo (`/tmp/claude-…/scratchpad/webrev-repo`), save a structured review with `gg review`-free path: `gg note` cannot write a review — use a tiny Go test-free route: the CLI's `gg apply` of a review document if supported, else a scratch Go program under the scratchpad calling `domain.SaveReview` (check `gg review --help` / `gg note --help` first; pick the CLI route when one exists).
- [ ] **Step 2:** playwright script (scratchpad): open `gg web` on the scratch repo; assert visible: commit's `li.rev` text matches `/└ \d{4}-\d\d-\d\d \d\d:\d\d Claude/`; branch `li.brev`; click → Overview pane shows the markdown heading; click f.txt → a `tr.note` with the note summary visible; `c` does nothing (no prompt); right-click Overview → Delete review → cancel keeps it; delete → rows gone. Check binary md5 + `/api/review/x` 404-JSON vs unfixed 404-text. Run against UNFIXED build first → assertions fail.
- [ ] **Step 3:** CHANGELOG (Unreleased: web reviews), `docs/CLAUDE-details.md` web section (routes + overlay rule), README web bullet if it lists web features; help text via `registerHelp({key: "reviews", …})` in reviews.js.
- [ ] **Step 4:** `./test.sh race` green.
- [ ] **Step 5:** commit docs; hand the user a verify binary; ask before merging.
