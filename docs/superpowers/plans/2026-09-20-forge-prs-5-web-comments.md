# Forge PRs 5 — review threads, note collapse and PR details in the web UI

> **For agentic workers:** executed INLINE by the session that wrote it
> (project rule: never subagents), task by task, TDD. Steps use checkbox
> (`- [ ]`) syntax for tracking.

**Goal:** a PR diff in `gg web` shows the forge's review threads as read-only
note boxes, every note box can collapse, a help-style overlay shows the PR's
description / conversation / outdated threads, comments re-poll on the `prs`
live event, and a PR diff copies a proper `gg://` link again.

**Architecture:** the domain already caches a PR's comments
(`PRCommentsRefresh` = the only network call; readers consult the cache).
The web adds a PR twin of `/api/preview/notes` keyed on the PR NUMBER, two
write-guarded POSTs that spend forge calls, and client rendering on the
existing note-box path. Title/collapse logic lives in an import-free module
so node can test it.

**Tech stack:** Go 1.26 (`internal/domain`, `internal/web`), vanilla ES
modules in `internal/web/static`, node-run `*js_test.go`, playwright 1.57
against the scratch fake-gh fixture.

**Spec:** `docs/superpowers/specs/2026-09-19-forge-prs-web-design.md` §6
(amended by Task 1 of this plan).

## Global constraints

- **R1** the wire value is the PR NUMBER (`?n=`); no ref or sha is ever read
  back from the page. `link_source`/`link_target` are sent OUT only.
- **R2** a GET never calls the forge. **Spec amendment:** §6 called
  `GET /api/pr/details` "a forge call" — that breaks R2. The PR body is not in
  `gh pr list` (only `pr view`), so details becomes a write-guarded
  **`POST /api/pr/details?n=`** (one `PullRequest` read — cached when full —
  plus one `PRCommentsRefresh`).
- **R3** no usable gh → no PR UI at all. Every new element that starts hidden
  gets its own `#id.hidden` rule (the web hides by ID).
- READ-ONLY: a forge note never offers edit / reply / remove; `E`/`R` refuse it.
- Collapse toggles a class on the `<tr>`; it never calls `renderDiff` (that
  resets `diffBlockIdx` and jolts the scroll).
- Forge-positive Go tests inject `fakeForge` via `svc.SetForgeProviders`
  (`prs_test.go` helpers `prServe`, `prFixture`); web `TestMain` already sets
  `domain.ForgeDisabled`. New tests call `t.Parallel()`.
- Every command runs with `cd /mnt/t/others/gigagit/.claude/worktrees/forge-prs-web-comments`
  or `git -C` that path; Write/Edit use that absolute prefix.
- Commits end with the two trailers (Co-Authored-By + Claude-Session).

## File map

| File | Change |
|---|---|
| `internal/domain/previewnotes.go` | `PreviewNotesAt` + `PreviewNotesAll` merge forge notes |
| `internal/domain/forge_notes.go` | `PRCommentsCached(n)` |
| `internal/domain/notewire.go` | `read_only`, `resolved`, `file_level`, `created` |
| `internal/web/prnotes.go` (new) | `GET /api/pr/notes`, `POST /api/pr/comments/refresh`, `POST /api/pr/details` |
| `internal/web/prs.go` | `handlePROpen` adds `link_source` / `link_target` |
| `static/notebox.js` (new, import-free) | `noteTitle`, `noteAge`, `seedCollapsed`, `toggleCollapsed` |
| `static/files.js` | PR note query/fetch, forge box, file-level row, collapse, read-only menu |
| `static/prdetails.js` (new) | the details overlay |
| `static/prs.js` | comment refresh after open, `details` menu row |
| `static/live.js` | `prs` event → comment refresh |
| `static/previews.js` | PR counts, `diffCtx.preview` link fields |
| `static/links.js` | PR form of `linkFor` |
| `static/keys.js`, `index.html`, `style.css` | `z`/`Z` keys, `#prdetails`, help rows, styles |

---

### Task 1: domain — forge notes on the diff-less read path; cached comments; wire fields

**Files:** `internal/domain/previewnotes.go`, `forge_notes.go`, `notewire.go`;
tests `internal/domain/forge_notes_test.go` (extend), `notewire_test.go`
(create if absent); spec §6 amendment.

**Produces:**
- `PreviewNotesAt(ctx, set, path)` — store notes + `forgeNotesFor(set, path)`.
- `PreviewNotesAll(ctx, set)` — forge threads grouped by path merged in.
- `func (s *Service) PRCommentsCached(n int) (PRComments, bool)`.
- `WireNote{ReadOnly bool "read_only,omitempty"; Resolved bool "resolved,omitempty"; FileLevel bool "file_level,omitempty"; Created string "created,omitempty"}` (RFC3339; empty for a zero time).

- [ ] **Step 1: failing tests.** Find the existing plan-3 test that seeds the
  comment cache for `PreviewNotesFor` (`grep -n "forgeComments\|PRCommentsRefresh" internal/domain/*_test.go`)
  and add, reusing its fixture helper:

```go
func TestPreviewNotesAtMergesForgeThreads(t *testing.T) {
	// fixture: a PR set (Source refs/gg/pr/7) with cached inline threads on a.go
	// 1. no stored notes            → the forge roots alone (today: nil)
	// 2. a stored note on a.go      → stored first, forge appended after
	// 3. notes disabled (NotesDisabled seam) → forge roots, nil error
}
func TestPreviewNotesAllCarriesForgeThreads(t *testing.T) // map has "a.go" and "b.txt" (file-level) with no stored notes
func TestPRCommentsCachedIsEmptyBeforeARefresh(t *testing.T) // (zero, false), then (c, true) after PRCommentsRefresh
func TestWireNoteForgeFields(t *testing.T) {
	// forgeNote(resolved line comment)  → ReadOnly, Resolved, !FileLevel, Created "2026-09-17T09:00:00Z"
	// forgeNote(file comment)           → ReadOnly, FileLevel
	// a stored user note                → none set, Created set when Note.Created non-zero
	// the reply of a forge root         → ReadOnly too (ToWireNote recurses)
}
```
  Write them fully against the helper's real names once found.
- [ ] **Step 2:** `go test ./internal/domain -run 'PreviewNotesAt|PreviewNotesAll|PRCommentsCached|WireNoteForge'` → FAIL.
- [ ] **Step 3: implement.** `PreviewNotesAt` mirrors `PreviewNotesFor`:

```go
	forge := s.forgeNotesFor(set, path)
	mine, err := s.loadPreviewNotes(ctx, set, path)
	if err != nil {
		if len(forge) > 0 && errors.Is(err, ErrNotesDisabled) {
			return forge, nil
		}
		return nil, err
	}
	if len(mine) == 0 {
		return forge, nil
	}
	… existing ShowFile / resolve …
	return append(keepResolved(resolveNotes(mine, nil, newLines)), forge...), nil
```
  `PreviewNotesAll`: after the store pass, `for _, r := range s.forgeNotesFor(set, "") { out[r.Note.Address.Path] = append(out[…], r) }`; with notes disabled and forge threads present return the forge map, nil error.
  `PRCommentsCached`: lock `forgeMu`, `e, ok := s.forgeComments[n]; return e.c, ok`.
  `ToWireNote`: `forge := r.Note.Source == model.NoteSourceForge`;
  `ReadOnly: forge`, `Resolved: slices.Contains(r.Note.Tags, model.NoteTagResolved)`,
  `FileLevel: forge && r.Range == [2]int{}`, `Created` formatted when non-zero.
- [ ] **Step 4:** the same `go test` → PASS; `go test ./internal/domain ./internal/cli ./internal/mcp` → PASS (additive wire fields; `gg note list --preview` now lists forge threads — fix any pinned output).
- [ ] **Step 5:** amend spec §6: details is `POST`, reason = R2 + body not in the listing. Commit `feat(domain): forge threads on the diff-less preview-notes read; wire note read_only/resolved/file_level/created`.

### Task 2: server — `/api/pr/notes`, `comments/refresh`, `details`, link fields

**Files:** create `internal/web/prnotes.go`, `internal/web/prnotes_test.go`; modify `internal/web/prs.go` (`handlePROpen`).

**Consumes:** Task 1; `prNumber`, `cachedPR`, `errPRNumber`, `prRevalidateBudget`, `isGitArgSafe`, `orEmptyCounts`, the write guard plan 4 used for `POST /api/pr/refresh` (same registration form).
**Produces (wire):**
- `GET /api/pr/notes?n=7[&path=a.go]` → `{notes:[WireNote], tip, counts, total}`; `path` absent = counts only. Unknown PR → 404; bad n → 400; unfetched PR → the empty shape.
- `POST /api/pr/comments/refresh?n=7` → `{changed:bool}`; forge failure → 502.
- `POST /api/pr/details?n=7` → `{pr: PullRequest(with body), hub:[…], outdated:[…], truncated:bool}`; forge failure → 502.
- `GET /api/pr/open` body gains `link_source` (`pair.Head`) and `link_target` (`pair.Base`).

- [ ] **Step 1: failing tests** (in `prnotes_test.go`, on `prServe` + `fakeForge`; extend `fakeForge` with a `comments` field and a `commentCalls` counter if it lacks them):

```go
func TestPRNotesServesCachedThreadsAndNeverCallsTheForge(t *testing.T)
	// refresh once (POST), zero the counter, GET notes?path=a.go twice →
	// the root "why 2?" with read_only:true and one reply; commentCalls == 0
func TestPRNotesCountsOnlyForm(t *testing.T)        // no path → notes [] , counts {"a.go":N,"b.txt":1}
func TestPRNotesBeforeAnyRefreshIsEmptyNotAnError(t *testing.T)
func TestPRNotesRefusesBadInput(t *testing.T)       // n=0 → 400, n=99 → 404, path="--x" → 400
func TestPRCommentsRefreshReportsChanged(t *testing.T) // first true, second false, mutated fake → true
func TestPRCommentsRefreshIsWriteGuarded(t *testing.T) // same assertion plan 4 makes for /api/pr/refresh
func TestPRDetailsCarriesBodyHubAndOutdated(t *testing.T) // outdated[0].hunk non-empty
func TestPRDetailsIsNotAGet(t *testing.T)            // GET → 405
func TestPROpenCarriesLinkPair(t *testing.T)         // link_source == "refs/gg/pr/7", link_target == "main"
```
- [ ] **Step 2:** `go test ./internal/web -run 'PRNotes|PRComments|PRDetails|PROpenCarries'` → FAIL.
- [ ] **Step 3: implement.** `prSet(ctx, svc, pr)`: `if !svc.PRFetched(ctx)[n] → zero set`; else `pair := svc.PRPair(ctx, pr); svc.PreviewNotes(ctx, pair.Head, pair.Base)` (HEAD is the source — `forgeInline` keys on `ParsePRRef(set.Source)`). The notes handler is `handlePreviewNotes`' body over that set. Refresh/details wrap `context.WithTimeout(readCtx(r), prRevalidateBudget)`; details = `svc.PullRequest(ctx, n)` then `svc.PRCommentsRefresh` then `svc.PRCommentsCached`.
- [ ] **Step 4:** tests PASS; `go test ./internal/web` PASS.
- [ ] **Step 5:** commit `feat(web): /api/pr/notes, comments/refresh and details — a PR's threads by number`.

### Task 3: client logic module `notebox.js` (node-tested)

**Files:** create `static/notebox.js`, `internal/web/noteboxjs_test.go` (copy the runner shape of `prsrowjs_test.go`).

**Produces:**
```js
export function noteAge(created, nowMs)            // "" | "3d" | "2h" | "5m" | "now" — same buckets as prsrow.js
export function noteTitle(n, path, preview, nowMs) // forge: "review · carol · 3d · a.go R3 · resolved"
                                                   // file-level: "review · erin · 3d · b.txt (file)"
                                                   // else the existing text: "agent note · x · a.go R3 (outdated|stale)"
export function seedCollapsed(notes)               // Set of root ids with n.resolved
export function toggleCollapsed(set, id)           // mutates, returns the new collapsed state
export function setAllCollapsed(set, notes, on)    // Z: every root
```
- [ ] **Step 1:** node test with a table over those five (forge/resolved/file-level/agent/stale/preview-outdated titles; age buckets; seed picks only resolved roots; toggle twice = identity; setAll on/off).
- [ ] **Step 2:** `go test ./internal/web -run NoteboxJS` → FAIL (module missing).
- [ ] **Step 3:** implement; move the existing title expression from `noteBoxHTML` into `noteTitle` verbatim for the non-forge arm.
- [ ] **Step 4:** PASS. Commit `feat(web): notebox.js — note titles and the collapse set, node-tested`.

### Task 4: client — threads in the PR diff, read-only, file-level, collapse

**Files:** `static/files.js`, `previews.js`, `keys.js`, `style.css`, `index.html` (help rows); static test `internal/web/prnotes_static_test.go`.

- [ ] **Step 1: static tests (fail first):** `files.js` contains `/api/pr/notes?`; `notebox.js` is imported where `noteTitle` is called; `style.css` has `tr.note.collapsed` and `.notebox.forge`; `index.html` help mentions `z` collapse; every identifier `files.js`/`live.js`/`prs.js` calls from another module is imported (extend `TestFetchPRsIsImportedWhereItIsCalled` with `refreshPRComments`, `openPRDetails`).
- [ ] **Step 2: `noteQuery`/`fetchNotes`.** For `preview.pr`: `q.set("n", pr)`, keep `rev` + `state=commit` (addNotePrompt builds its POST from them); URL `/api/pr/notes?`; like a preview it takes `d.counts` → `state.previewCounts` + `renderFiles()`. `previews.js`: drop the "skip counts for a PR" branch — the counts load calls `/api/pr/notes?n=` with no path.
- [ ] **Step 3: `noteBoxHTML`.** Title via `noteTitle`; class `forge` when `n.read_only`; the agent-off filter never drops a forge row; `collapsed` class on the `<tr>` when `state.noteCollapsed.has(n.id)`; title gets `▸`/`▾` and `data-collapse`. File-level (`n.file_level`): rendered by a new `fileNoteRowsHTML(cols)` emitted once in `diffHTML` right after `<colgroup>`/before the first row, full `colspan` (never the split gap) — and also on the "no changed lines" notice path above the notice.
- [ ] **Step 4: collapse.** `state.noteCollapsed = new Set()`, `state.noteCollapsedFor = ""`; in `fetchNotes` after assignment: key = `path + "\0" + rev`; when it differs, `noteCollapsed = seedCollapsed(notes)` (a re-poll keeps the user's toggles). Click on `.notetitle` → `toggleCollapsed` + `tr.classList.toggle("collapsed")`. Keys `z` (nearest note) / `Z` (all; collapse all unless all already are) through `noteKey`. CSS: `tr.note.collapsed .notesum, .notetext, .notereply {display:none}`.
- [ ] **Step 5: read-only.** Context menu on a `read_only` note: only `copy gg link to this note` (when `linkFor` yields one) + `Collapse`/`Expand`; every other note gains the Collapse row too. `editNotePrompt`/`replyNotePrompt` on a read-only target → `opLine("forge comments are read-only", true)`.
- [ ] **Step 6:** `go test ./internal/web` PASS. Commit `feat(web): review threads in the PR diff; every note box collapses (click, z/Z)`.

### Task 5: comment re-poll + PR links

**Files:** `static/prs.js`, `live.js`, `previews.js`, `links.js`; node test for `linkFor` (extend the existing links js test).

- [ ] **Step 1: failing node test:** `linkFor(repo, wt, {path:"a.go", preview:{pr:7, source:"alice:feat", target:"main", linkSource:"refs/gg/pr/7", linkTarget:"main"}}, "new", 3)` → `…/a.go@main...refs/gg/pr/7:3` (copy the exact line-suffix form from a sibling preview case); without `linkSource` → `""`.
- [ ] **Step 2:** `armPreview`/diffCtx carry `linkSource`/`linkTarget` from the open body; `linkFor` uses them instead of refusing. Go test: `model.ParseLink` of that string round-trips (add to `internal/model` link tests if no PR-form case exists).
- [ ] **Step 3:** `prs.js` `export async function refreshPRComments(n)`: `runOnce("pr-comments", …)` POST; when `changed` and `state.previewOpen?.pr === n` → `fetchNotes()` (or the counts-only load when no file is open). Called (a) at the end of a successful `showPR`, (b) from `live.js` on `prs` when a PR is open. A failure is silent (diff stays without threads).
- [ ] **Step 4:** tests PASS. Commit `feat(web): PR comments re-poll on open and on the prs event; PR diffs copy a gg link`.

### Task 6: details overlay

**Files:** create `static/prdetails.js`; `index.html` (`#prdetails` > `#prdetails-box` > `h2#prdetails-title` + `#prdetails-body`), `style.css` (`#prdetails` reuses the `#help` frame rules by selector list + **`#prdetails.hidden{display:none}`**), `prs.js` (menu row `details…`), `files.js` `renderCompareBar` (a `<button id="pr-details-chip">details</button>` when `previewBar` belongs to a PR), static test for the hidden rule and the imports.

- [ ] **Step 1:** static tests fail first (`#prdetails.hidden` rule; `openPRDetails` imported in `prs.js` and `files.js`).
- [ ] **Step 2:** `openPRDetails(n)`: mask on → `postJSON("/api/pr/details?n=")` → render with `esc()` only (plain text, `white-space: pre-wrap`): header (`#n title`, state, author, `source → target`, URL as text + copy button), Description, Conversation (hub rows: `author · age · verdict` + body), Outdated (`path Lnn (outdated)` + `<pre class="prhunk">` hunk + body + replies), truncation line. `pushLayer("prdetails", el, {onKey})`: Escape closes, j/k/arrows/PageUp/PageDown scroll the body, everything else swallowed. A failed load → `opLine(err, true)`, no layer.
- [ ] **Step 3:** PASS. Commit `feat(web): pull-request details overlay`.

### Task 7: browser verification (VISIBILITY, each check once with its guard removed)

Fixture: scratchpad `prweb/` (`run.sh`, ports 47811 gh / 47812 no-gh), rebuilt `gg` from the worktree; confirm the port is mine via `/api/repo`. Script `pw/prs5.mjs`:

1. open PR #7 → a `.notebox.forge` on `a.go` is visible with title containing `review · carol`.
2. the resolved thread (dave): title visible, `.notesum` NOT visible; click title → visible; `z` collapses again.
3. a local note added with `c` still works on the PR diff and its box has Edit in the menu; the forge box's menu has no Edit/Reply/Remove.
4. `b.txt`: the file-level row precedes the first `tr[data-no]`.
5. agent toggle `a` leaves forge boxes visible.
6. file list shows ◆ counts for `a.go`.
7. details: row menu → overlay visible, shows description text, a hub verdict, the outdated hunk text; Esc closes; chip on the compare bar opens it too.
8. re-poll: rewrite `threads-7.json` adding a thread, click the section ⟳ → the new box becomes visible without reopening.
9. copy-link row exists on a PR diff line and the copied text contains `...refs/gg/pr/7`.
10. no-gh server: no section, `#prdetails` not visible.

Guards to remove one at a time and watch fail: the `collapsed` CSS rule (2), the read-only menu branch (3), `fileNoteRowsHTML` call (4), the forge exemption in the agent filter (5), the `changed` re-fetch (8), `#prdetails.hidden` (10).

### Task 8: docs, gate, delivery

- [ ] `CHANGELOG.md`, `README.md` (web PR paragraph), `docs/web-tui-parity.md`, `docs/CLAUDE-details.md` (extend the web PR section: endpoints, R2 amendment, collapse state, link fields), help overlay rows.
- [ ] Commit; `./test.sh race` from a CLEAN tree (started immediately, in the background).
- [ ] Stripped verify binary (`go build -ldflags "-s -w"`), SendUserFile + absolute path.
- [ ] Update memory `forge-prs-feature.md`; ASK before merging. After the merge: `./build.sh install`, `./build.sh web`, remove worktree + branch.
