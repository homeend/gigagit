# Review File & Remark Links Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
> **gigagit rule:** NO implementer subagents — this session executes every task itself (CLAUDE.md). A read-only review subagent at the end is fine.

**Goal:** From a review's file row or remark (TUI and gg web) copy a review-aware link (`…/<path>[:<line>]?review=<id>`) or the remark id (`review:<id>:<n>`); opening such a link opens the review on that file/line; `gg review show <link>` narrows to it.

**Architecture:** The domain builds every link (`ReviewFileLink`, `ReviewRemarkLink`, a `ReviewLink` field on each remark) from the review's own target + the `?review=` hint — no new grammar. Frontends only add menu rows that call it. Opening reuses the steer "pending landing" machinery: `onReviewHint` parks the file/line, the review view's file list drains it (`drainPendingFiles` / `drainPendingCompare` already run in review mode).

**Tech Stack:** Go 1.26, Bubble Tea TUI, vanilla JS gg web.

**Spec:** `docs/superpowers/specs/2026-10-07-review-links-copy-design.md`

**Worktree:** `/work/gigagit/.claude/worktrees/review-links-copy` (branch `feat/review-links-copy`). `cd` there for every command.

## Global Constraints

- Link shapes: file `gg://<repo>@<review target>/<path>?review=<id>`; remark adds `:<start>[-<end>]` (old side: the grammar's old-side form) — built ONLY by domain helpers over `reviewTarget` + `linkText` / `linkTextIn`.
- Remark line = the document note's own range (`newRange` / `oldRange`), never the display line.
- Remark id = `model.ReviewNoteIDPrefix + <reviewID> + ":" + <n>` (`ParseReviewNoteID` reads it).
- TUI labels (all through `i18n.T`, four bundles): `Copy review link to this file`, `Copy remark link`, `Copy remark id`, status `Copied remark id: %s`. ("Copy file link" is taken — the content link row.)
- Web labels: `copy review link to this file`, `copy remark link`, `copy remark id`; for a review remark these REPLACE `copy gg link to this note`.
- Remarks stay read-only; only copy actions are added.
- `internal/tui`, `internal/cli` never import `internal/git`.

## Review Focus

1. A remark on the OLD side (a removed line) — its link must carry the old side and land on the old line (Task 1 test, Task 4 test).
2. A working-changes review (no commit) — the file link must still build and open (Task 1 test; Task 3 asserts the row exists for a working review).
3. A review link whose path is not in the review (file renamed/removed later) — opening answers "not in" and leaves the review's overview up, never a stuck pending (Task 4 test).
4. A reply under a remark — the remark rows act on the thread root, not the reply (Task 3 test via a reply-at-cursor case, Task 5 JS uses `rootId`).
5. The review deleted before the copy — copy fails with the reason in the status, nothing copied (Task 1 error test; rows use `asyncCopyLinkRow`, whose error path shows it).

---

### Task 1: domain — review-aware file and remark links

**Files:**
- Modify: `internal/domain/review_link.go` (`ReviewRemark`, `reviewRemarksIn`, `ReviewShowRemark`, `ReviewShow`, new funcs)
- Test: `internal/domain/review_file_link_test.go` (create)

**Interfaces:**
- Produces:
  - `ReviewRemark.ReviewLink string` and `ReviewShowRemark.ReviewLink string \`json:"review_link,omitempty"\``
  - `func (s *Service) ReviewFileLink(ctx context.Context, id, path string) (string, error)`
  - `func (s *Service) ReviewRemarkLink(ctx context.Context, remarkID string) (string, error)` — `remarkID` = `review:<id>:<n>`; unknown n → `fmt.Errorf("review %s has no remark %d", id, n)`; malformed → `fmt.Errorf("not a review remark id: %q", remarkID)`.

- [ ] **Step 1: Failing tests.** Reuse the review fixtures of `internal/domain/review_link_test.go` / `review_threads_test.go` (find the helper that saves a review document on a commit with notes on new and old sides; e.g. `grep -n "func .*saveReview\|SaveReview(ctx" internal/domain/*_test.go`). Assert:
  - `ReviewFileLink(id, "a.txt")` parses (`model.ParseLink`) with `Path == "a.txt"`, `Hint.Kind == model.ReviewHintKind`, `Hint.ID == id`, `Line == 0`, and the same `Target` as `ReviewLink(id)`'s.
  - `ReviewRemarkLink(prefix+id+":0")` for a new-side remark with range [3,4] parses with `Line 3`, `End 4`, `Side new`, the hint; for an old-side remark `Side old`.
  - `ReviewShow(id).Remarks[i].ReviewLink` equals `ReviewRemarkLink` for the same remark, and `Link` (the plain one) is unchanged.
  - Errors: unknown review id → `ErrReviewNotFound` (wrapped); `review:<id>:99` → error mentioning `no remark 99`; `"x"` → `not a review remark id`.
  - A working review (`Kind == ReviewOnWorktree`, saved through the working-review path the existing tests use): `ReviewFileLink` succeeds and round-trips.
- [ ] **Step 2: Run — FAIL** (`go test ./internal/domain/ -run 'ReviewFileLink|ReviewRemarkLink'`).
- [ ] **Step 3: Implement.**
  - In `reviewRemarksIn`, after `rm.Link`: `l.Hint = model.LinkHint{Kind: model.ReviewHintKind, ID: r.ID}`; `if text, err := linkTextIn(repo, l); err == nil { rm.ReviewLink = text }`.
  - `ReviewShow` copies `x.ReviewLink` into `ReviewShowRemark.ReviewLink`.
  - `ReviewFileLink`: `r, err := s.Review(ctx, id)`; `t, err := s.reviewTarget(ctx, r)`; `return s.linkText(ctx, model.Link{Path: path, Target: t, Side: model.NoteSideNew, Hint: model.LinkHint{Kind: model.ReviewHintKind, ID: r.ID}})`.
  - `ReviewRemarkLink`: `rid, n, ok := model.ParseReviewNoteID(remarkID)`; `!ok` → error; `r` → `rms, err := s.ReviewRemarks(ctx, r)`; find `N == n` → `ReviewLink` (empty → `fmt.Errorf("remark %d of review %s has no link (its path cannot be spelled)", n, rid)`).
- [ ] **Step 4: PASS** — `go test ./internal/domain/ -run 'Review'`.
- [ ] **Step 5: Commit** — `feat(domain): review-aware links to a review's files and remarks`.

---

### Task 2: CLI — `gg review show` narrows to a file or remark link; prints the review link

**Files:**
- Modify: `internal/cli/review_show.go` (`reviewShow`, `printReviewShow`)
- Test: `internal/cli/review_show_test.go` (extend)

**Interfaces:**
- Consumes: `ReviewShowRemark.ReviewLink`, `ReviewFileLink`, `ReviewRemarkLink` (Task 1).

- [ ] **Step 1: Failing tests** (reuse the fixture in `review_show_test.go` that stores a review with remarks on two files): build `fl := svc.ReviewFileLink(id, "<file1>")` and `rl := svc.ReviewRemarkLink(prefix+id+":0")` through `openCLIService(t, dir)`; then
  - `gg review show <fl>` prints only remarks on file1 (assert the other file's path absent, file1's present).
  - `gg review show <rl>` prints exactly remark `[0]` (assert `[1]` absent) and its `id review:<id>:0`.
  - `gg review show --json <rl>` → `remarks` has length 1 with `review_link` == `rl`.
  - `gg review show <id>` (no link) still prints every remark and each remark's review link line.
- [ ] **Step 2: FAIL.**
- [ ] **Step 3: Implement.** In `reviewShow`, when `fs.Arg(0)` starts with `gg://`, `l, _ := model.ParseLink(arg)` (already validated by `reviewArgID`); after `rs` is read: if `l.Path != ""` keep remarks with `Path == l.Path`; if also `l.Line > 0` keep those with `Side == string(sideOf(l))` (new when empty) and `Start <= end && l.Line <= End` where `end = max(l.Line, l.End)`. Filter before both JSON and text output; `Resolved` recount on the kept remarks. `printReviewShow`: under each remark print `r.ReviewLink` (when set) on its own indented line after the plain link (same indentation as the plain link line).
- [ ] **Step 4: PASS** — `go test ./internal/cli/ -run 'ReviewShow'`.
- [ ] **Step 5: Commit** — `feat(cli): gg review show narrows to a review file or remark link`.

---

### Task 3: TUI — copy rows (file row, remark) and `L` on a remark

**Files:**
- Modify: `internal/tui/note_row_menu.go` (new rows beside `reviewViewCopyLinkRow`)
- Modify: `internal/tui/action_menu.go:121-127` (splice the file row next to `reviewViewCopyLinkRow`)
- Modify: `internal/tui/note_keys.go` (`noteMenuRows`: remark rows)
- Modify: `internal/tui/diff_view.go:1131-1149` (`L`)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Test: `internal/tui/review_link_copy_test.go` (extend)

**Interfaces:**
- Consumes: `ReviewFileLink`, `ReviewRemarkLink` (Task 1); `asyncCopyLinkRow`, `copyRow`, `fileListRowPath`, `notesAtCursor`, `replyableNoteTargets` (existing).
- Produces: `func (m Model) reviewFileCopyLinkRow() (actionRow, bool)` (id `copy-review-file-link`), `func (m Model) reviewRemarkRows() []actionRow` (ids `copy-remark-link`, `copy-remark-id`), `func (m Model) reviewRemarkLinkRow() (actionRow, bool)`.

- [ ] **Step 1: Failing tests** (helpers in `review_link_copy_test.go`: `openedReviewView`, `captureClip`, `runMenuRow`, `selectRow`, `reviewViewModel`; find how an existing test opens a review FILE's diff with stamped remarks — `grep -n "reviewID" internal/tui/*_test.go` — and reuse it):
  - `TestReviewFileRowCopiesItsReviewLink`: select a reviewed file row → `runMenuRow(t, m, "copy-review-file-link")` → copied text has `/<path>` and `?review=<id>`, and no `:<line>`.
  - The Overview row has no such row (path empty).
  - `TestReviewRemarkRowsCopyLinkAndID`: review diff with the cursor on a remark → `copy-remark-link` copies a link with `:<start>` and `?review=<id>`; `copy-remark-id` copies `review:<id>:<n>`.
  - With the cursor on a REPLY of that remark (add one through `svc.ReplyNote` or the fixture the reply tests use) → `copy-remark-id` still copies the ROOT `review:<id>:<n>`.
  - `TestReviewRemarkLKeyCopiesTheRemarkLink`: single-commit review diff, cursor on a remark, press `L` → the remark link is copied (before: notice "no gg link for this place").
  - A working review's file row offers `copy-review-file-link` (build a working review the way `review_view` working tests do).
- [ ] **Step 2: FAIL.**
- [ ] **Step 3: Implement.**
  - `reviewFileCopyLinkRow`: `st, svc := m.filesReview, m.svc`; require `st != nil && svc != nil && m.inContentWindow() && m.diffLayer() == nil`; `path, ok := m.fileListRowPath()`; `!ok || path == ""` → false; `return m.asyncCopyLinkRow("copy-review-file-link", i18n.T("Copy review link to this file"), func(ctx) { return svc.ReviewFileLink(ctx, st.id, path) }), true`. Splice right AFTER `reviewViewCopyLinkRow` in `action_menu.go`.
  - `reviewRemarkRows`: top layer is a `*diffView` with `reviewID != ""` (the field `stampReviewNotes` sets); `targets := replyableNoteTargets(m.notesAtCursor())`; take the first whose `rootID` satisfies `model.IsReviewNoteID`; none → nil. Rows: `m.asyncCopyLinkRow("copy-remark-link", i18n.T("Copy remark link"), func(ctx) { return svc.ReviewRemarkLink(ctx, rootID) })` and `m.copyRow("copy-remark-id", i18n.T("Copy remark id"), i18n.T("Copied remark id: %s", rootID), rootID)`. Append them in `noteMenuRows` after the resolve row.
  - `reviewRemarkLinkRow` = the first of `reviewRemarkRows` (the link row).
  - `L`: before `r, ok := m.contextLinkRow()`: `if r, ok := m.reviewRemarkLinkRow(); ok { nm, cmd := r.run(m); return nm.(Model), cmd }`. Update the comment: on a review remark `L` copies the remark link (the menu's `Copy remark link`).
  - i18n: add the four keys after `"Copy review link"` in each bundle:
    - ja: `"Copy review link to this file" = "このファイルへのレビューリンクをコピー"`, `"Copy remark link" = "指摘へのリンクをコピー"`, `"Copy remark id" = "指摘IDをコピー"`, `"Copied remark id: %s" = "指摘IDをコピーしました: %s"`
    - ko: `"이 파일의 리뷰 링크 복사"`, `"지적 링크 복사"`, `"지적 ID 복사"`, `"지적 ID를 복사했습니다: %s"`
    - zh: `"复制此文件的评审链接"`, `"复制评审意见链接"`, `"复制评审意见 ID"`, `"已复制评审意见 ID：%s"`
    - ru: `"Копировать ссылку на ревью этого файла"`, `"Копировать ссылку на замечание"`, `"Копировать ID замечания"`, `"Скопирован ID замечания: %s"`
- [ ] **Step 4: PASS** — `go test ./internal/tui/ -run 'Review|I18n|MenuLabel|EngineProse' && go test ./internal/i18n/`.
- [ ] **Step 5: Commit** — `feat(tui): copy a review's file link, a remark's link and id; L on a remark`.

---

### Task 4: TUI — a review link with a file/line opens the review there

**Files:**
- Modify: `internal/tui/review_hint.go` (`onReviewHint`)
- Modify: `internal/tui/review_view.go` (`openReviewWith` / `reviewViewMsg` / `handleReviewViewMsg`)
- Test: `internal/tui/review_hint_land_test.go` (create; follow the existing review-hint tests — `grep -ln "steerNavigateReview\|reviewHintMsg" internal/tui/*_test.go`)

**Interfaces:**
- Consumes: the steer `Command` fields `File`, `Line` (*steer.Line), `Side`; `pendingSteer`, `steerStageFiles`, `steerStageCompare`, `drainPendingFiles`, `drainPendingCompare`, `failPending` (existing).
- Produces: `reviewViewMsg.land *steer.Command` (nil = no landing).

- [ ] **Step 1: Failing tests:**
  - A review link with `File` + `Line` (new side) on a single-commit review → after the review loads and its file list arrives, the top layer is a diff of that file with `reviewID == id` and the cursor on that line; the steer is answered OK once (not twice).
  - Same for a range review (pair) and an old-side line.
  - `File` not in the review → the review view stays (its overview/file list up), the steer answers a failure naming the path, and `m.pendingSteer == nil` afterwards.
  - No `File` → unchanged behaviour (opens the overview; answered immediately).
- [ ] **Step 2: FAIL.**
- [ ] **Step 3: Implement.**
  - `onReviewHint`: when `c.File != ""`, build `land := c; land.HintKind, land.HintID = "", ""` and open with it (`openReviewWith(..., land)` — extend `openReviewWith`/`openReviewFrom` with a trailing `land *steer.Command`; existing callers pass nil), and DO NOT answer the steer here (the drain answers); otherwise unchanged.
  - `handleReviewViewMsg`: carry `msg.land`. In every early-return path (error / prose review) with `msg.land != nil`, answer the steer with a failure (`m.answerSteer(*msg.land, steerFail(*msg.land, "the review could not open its files: …"))`). After `open(m)`: if `msg.land != nil`, park `m.pendingSteer = &pendingSteer{cmd: *msg.land, stage: steerStageFiles, hash: msg.tip, at: time.Now()}` for a single-commit review, else `{stage: steerStageCompare, tag: m.compareTag}` (range and working) — set it on the model returned by `open`, before returning its cmd. (When `handOffToFilesView(open)` is used, wrap `open` so the parking happens inside it.)
  - The existing drains open the file with the review's stamping (they go through the files view's open path while `m.filesReview` is set) — the test asserts `reviewID`; if it is not set, route the drain's open through the same function `enter` on a review file row uses and record a Ruling.
- [ ] **Step 4: PASS** — `go test ./internal/tui/ -run 'ReviewHint|Steer|Review'`.
- [ ] **Step 5: Commit** — `feat(tui): a review link with a file or line opens the review there`.

---

### Task 5: gg web — endpoint, copy rows, landing

**Files:**
- Modify: `internal/web/reviews.go:285-296` (`handleReviewLink`: `path`, `n` params)
- Modify: `internal/web/static/files.js` (files-list menu for review file rows; note menu rows for review remarks)
- Modify: `internal/web/static/live.js:437-451` (`steerNavigateReview` lands the file/line)
- Test: `internal/web/reviews_test.go` (or the file holding `handleReviewLink` tests), a JS wiring test (extend the review JS wiring list — `grep -ln "reviewMenu\|copyServerLink" internal/web/*_test.go`)

**Interfaces:**
- Consumes: `ReviewFileLink`, `ReviewRemarkLink` (Task 1); JS `copyServerLink` (reviews.js — export it if not exported), `copyText`, `reviewActive`, `state.review.id`, `openNamedFile` (live.js).
- Produces: `GET /api/review/{id}/link?path=<p>` and `?n=<n>`.

- [ ] **Step 1: Failing tests:**
  - Go: `/api/review/<id>/link?path=a.txt` → `link` holds `/a.txt` and `?review=<id>`; `?n=0` → the remark link (`:<start>`); `?n=99` → 404 with the error; both params → 400.
  - JS wiring (strings in files.js / live.js): `copy review link to this file`, `copy remark link`, `copy remark id`, `"/link?path="`, `"/link?n="`, and in live.js `openNamedFile(` inside `steerNavigateReview`.
- [ ] **Step 2: FAIL.**
- [ ] **Step 3: Implement.**
  - `handleReviewLink`: `path, n := q.Get("path"), q.Get("n")`; both → 400; `path` → `svc.ReviewFileLink`; `n` → `strconv.Atoi` (bad → 400) → `svc.ReviewRemarkLink(ctx, model.ReviewNoteIDPrefix+rv.ID+":"+n)` (error → 404); else `ReviewLink` as today.
  - files.js commit/compare file-row menu: when `reviewActive()` and the row is a reviewed file (`f.path`), add `{ label: "copy review link to this file", act: () => copyServerLink("/api/review/" + encodeURIComponent(state.review.id) + "/link?path=" + encodeURIComponent(f.path), "review file: " + f.path) }`.
  - files.js note menu: `const remark = String(rootId).startsWith("review:")`. If `remark`: parse `const [, rid, rn] = /^review:(.+):(\d+)$/.exec(rootId)`; push `copy remark link` (`copyServerLink("/api/review/" + encodeURIComponent(rid) + "/link?n=" + rn, "remark " + rn)`) and `copy remark id` (`copyText(rootId, "remark id")`), and do NOT push `copy gg link to this note`. Store notes unchanged.
  - live.js `steerNavigateReview`: after `await openReview(...)`, `if (s.file) await openNamedFile(state.files, s, "review " + s.hint_id);`.
- [ ] **Step 4: PASS** — `go test ./internal/web/ -run 'Review|Wired|JS'`.
- [ ] **Step 5: Browser check** (playwright, as the agent-names probe in this session's scratchpad `pw/probe.mjs` did): scratch repo, a review saved with `gg review save` (2 files, remarks on both sides), `bin/gg web`; right-click a reviewed file row → `copy review link to this file` visible; right-click a remark → `copy remark link` / `copy remark id` visible and `copy gg link to this note` absent; read the clipboard through the page's copy status/toast text. Then open the remark link through the `#`/link dialog → the review is up on that file. Run the same against main's build → rows absent.
- [ ] **Step 6: Commit** — `feat(web): copy a review's file link, a remark's link and id; land review links on the file`.

---

### Task 6: docs, golden, gates

**Files:**
- Modify: `internal/agentskill/using-gg.md` (+ `Version` bump, re-render `.claude/skills/using-gg/SKILL.md` via `agentskill.UsingGG.SkillFile()` — not `gg init --update`), `README.md`, `CHANGELOG.md`, `docs/CLAUDE-details.md`
- Create: an e2e TUI scenario (next free `s1xx`) — writing-e2e-scenarios skill

- [ ] **Step 1: e2e** — a scenario that stores a review (`gg review save` with a document on stdin via `[[run]] stdin`), then `[tui]` steps: open the review (`gg session navigate`-free: use the Commits row `.` → Show review, or the review sub-row), select a file, open its diff, `.` menu → a checkpoint showing `Copy remark link` / `Copy remark id`. Golden via `-update`; read it in full.
- [ ] **Step 2: docs** — using-gg (review section): "A `?review=<id>` link with a path names a file of the review, with a line one remark; `gg review show <link>` prints just that; each remark prints its id `review:<id>:<n>` (for `gg note reply` / `resolve`) and its review link." CHANGELOG top section "Review links to files and remarks"; README review rows (TUI `.` menu rows + web); CLAUDE-details section (domain builders, landing via pendingSteer, label collision note).
- [ ] **Step 3: gates** — `./test.sh` then `./test.sh race` — "all green".
- [ ] **Step 4: Commit** — `docs: review file and remark links`.
- [ ] **Step 5: Final review** — read-only review subagent; ASK before merge.
