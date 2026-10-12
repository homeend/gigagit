# Note meta — plan 3: the web

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (this repo: NO implementer subagents — the session that wrote the plan executes it; only the final review is a read-only subagent). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** gg web shows note meta the way the TUI does since plan 2. That covers chips, the audience marker, the frame colour, line rows and details rows on every note box, counts in the review view, the meta row of the opened send-panel row, and the `.psev` colour. It also adds the Mark menu and the "note-edits unavailable" notice.

**Architecture:**
- The server already sends `WireNote.Display` (the `notemeta.Resolved`) on every stored note.
- This plan adds the same `display` object to the shapes that lack it:
  - viewer note rows (temporary notes and review remarks)
  - send-panel candidate rows
  - the review's "other notes" rows
- It also adds the review's counts to `/api/review/{id}`.
- One new import-free module, `static/notemeta.js`, turns a `display` into HTML: chips, marker, line rows, details rows, counts and the title fit. It is node-tested like `notebox.js`.
- `files.js`, `viewer.js`, `reviews.js` and `prsendpanel.js` only call it.
- Mark is a new `POST /api/notes/mark` over `SetNoteMeta` / `UnsetNoteMeta`.

**Tech Stack:** Go 1.26 (`internal/web`, `internal/domain`, `internal/notemeta`). Plain ES modules plus CSS (`internal/web/static`). Node for the pure-JS tests. Playwright, in scratch only, for the browser check.

**Spec:** `docs/superpowers/specs/2026-10-12-note-meta-design.md`
- §7 is the web.
- Also read §3.2/§3.3 (places, colours, styles), §5.4 (Mark), §6 (the TUI behaviour the web mirrors), §10 (newer format), R9, R12 and R14.
- The TUI reference is plan 2: `docs/superpowers/plans/2026-10-12-note-meta-2-tui.md` and `internal/tui/note_meta.go`.

## Global Constraints

- **The web is English-only.** It has no i18n layer. The words are `→ author`, `for you`, `agreed`, `disagreed`, `deferred`, `Mark`, `clear`, `meta:`, `added:`, `display problem:` and `N for author` / `N for you`.
- **Agent keys and values are data, shown verbatim.** Every key, value, glyph, label and problem goes through `esc()`.
- **Colours are a 7-name whitelist:** `red orange yellow green blue purple grey`. A colour name maps to a class (`mc-red`); it is never interpolated into a style or class unchecked. An unknown or empty colour means the place's default (badge: grey).
- **Styles:** `filled` means a coloured background with the page `--bg` as the text colour, and one column of padding each side. `text` means coloured text.
- **Spacing mirrors the TUI.**
  - One space next to a filled chip; two spaces between two text badges.
  - Two spaces before the audience marker.
  - `" · "` (`META_TITLE_SEP`) between the badge run and the title words.
- **A stale or outdated note stays dim.** Dim wins over the frame colour.
- **Meta never triggers anything (R12).** Mark tags only: it does not resolve, send, tick, fold, filter or hide.
- **R9:** the send panel still opens with nothing ticked, with no filter and no dimming. Meta shows only in an opened (expanded) row; the collapsed row's `.psev` only changes colour.
- **Mark placement:**
  - Mark lives only in the diff note-box context menu (`files.js`).
  - It is not on Files-view note rows (user ruling: "Open + Delete only").
  - It is not on viewer boxes.
  - It is never on a forge thread.
- **Every new hideable element gets its own `#id.hidden { display: none; }` rule** (memory web-hidden-class-is-per-id).
- **Note writes go through `noteWrite`**, which is gated by `runOnce("note-write", …)`.
- **Git hygiene:**
  - Never `git add -A`, because a built `bin/gg` may sit in the worktree. Stage named files with `gg add`, then `git commit -F <msgfile>`.
  - Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01ND2Z67BkFHxPR3QUdTF79h`.
- **Gates:** `./test.sh race` must end with `all green`. Browser checks run on a freshly rebuilt binary and assert VISIBILITY.

### Deviation for the user to rule on (plan review)

- **Spec §7 says `--meta-*` "with light and dark values on `:root`".** gg web has ONE palette, the dark one (`:root` only, no light mode; `--note-group-*` precedent). A `prefers-color-scheme: light` block would recolour only the chips on a dark page, which is worse contrast.
- **This plan ships the dark values only, pinned to `theme.Dark` by a test.** Adding a light block later is one CSS block plus a test row once the page gets a light theme.

## Review Focus

1. **A long title in a narrow pane (split layout, a deep path) must keep the chips and the path's file name and line.** The order is the TUI's fit: path elided in the middle to at least 8 columns, then the words before the path, then the path's tail; chips last. Task 4's node test pins `fitNoteTitle`; Task 10's browser check opens the split layout at 900px and asserts the file name and line are visible.
2. **A stale note whose declaration sets `frame: red` must still draw dim**, in both border and title. Task 4 orders the CSS; Task 10 asserts `getComputedStyle(box).borderTopColor` equals the `--dim` colour.
3. **A reply under a declared root.** The reply shows its OWN badge row, lines and details, through the root's declaration (`ToWireNoteIn`). Task 5's wiring test and Task 10's browser check cover a reply with `kind=issue`.
4. **A mark inside a review view must refresh the header counts and the file-row tails**, as the TUI re-reads both; `emitNotes` refreshes only the note lanes. Task 9 adds `refreshReviewMeta()`, pinned by a wiring test and the browser check.
5. **A notes file a newer gg wrote:**
   - Mark answers 409 with the store's message, and the Mark rows are hidden when `featureDisabled("note-edits")`.
   - The notification centre shows `note-edits is unavailable in this repository`, and dismissing it uses the TUI's id `feature_disabled_note-edits`.
   - Tasks 2 and 3 cover this in Go.

---

## File Structure

- **Modify `internal/web/filecontent.go`:** `noteRow` gains `Display *notemeta.Resolved`. `notesFor` resolves temporary notes.
- **Modify `internal/web/reviewdiff.go`:** `remarkRows` sets `Display`.
- **Modify `internal/web/prsend.go`:** `candidateRowWire.Display`.
- **Modify `internal/web/reviews.go`:**
  - `/api/review/{id}` gains `meta_counts` and `meta_by_file`. It uses `ReviewFileMetaCounts` in place of `ReviewFileCounts`.
  - `other` rows become `map[string]any` and gain `display`.
- **Modify `internal/web/notes.go`:** `POST /api/notes/mark`, and `noteErrStatus` maps `notes.ErrNewerFormat` to 409.
- **Create `internal/web/notemeta_notice.go`:** the note-edits notice builder.
- **Modify `internal/web/notifications.go`:** add the id to `dismissableNotices`.
- **Modify `internal/web/preflight.go`:** add `domain.FeatureNoteEdits` to `gatedFeatures`.
- **Create `internal/web/notemeta_web_test.go`:** the Go tests of Tasks 1–3.
- **Create `internal/web/static/notemeta.js`:** pure and import-free (chips, marker, rows, counts, fit, severity colour).
- **Create `internal/web/notemetajs_test.go`:** runs `notemeta.js` under node.
- **Create `internal/web/notemeta_static_test.go`:** wiring guards for files.js, viewer.js, reviews.js, prsendpanel.js, style.css and index.html.
- **Modify `internal/web/static/style.css`:**
  - the `--meta-*` tokens
  - chip, frame, row and counts classes
  - `#files-counts`
  - the collapsed-box rule
- **Modify `internal/web/style_groups_test.go`:** pin `--meta-*` to `theme.Dark`.
- **Modify `internal/web/static/index.html`:** add `<span id="files-counts" class="hidden">` to `#files-header`.
- **Modify `internal/web/static/files.js`:**
  - `noteBoxHTML`: title, frame, rows and replies
  - the Mark rows
  - `setFilesCounts`
  - `enterFilesStage` clears the counts line
- **Modify `internal/web/static/notebox.js`:** `noteTitleParts` (the title split into words and place). `noteTitle` stays its join.
- **Modify `internal/web/static/viewer.js`:** `noteBoxHTML` shows chips, marker, frame and rows.
- **Modify `internal/web/static/reviews.js`:** the counts line, file-row tails, other-notes chips, and `refreshReviewMeta`.
- **Modify `internal/web/static/prsendpanel.js` and `prsendrows.js`:** the opened row's meta row and the `.psev` colour.
- **Scratch, never committed:** `$SCRATCH/notemeta-web/check.mjs` plus `npm i playwright` there. `$SCRATCH` is the session scratchpad.
- **Docs (Task 10):** `CHANGELOG.md`, `README.md` (the notes paragraph, if it names the TUI only), `docs/CLAUDE-details.md`.

---

### Task 1: Meta on every wire shape the page draws

**Files:**
- Modify: `internal/web/filecontent.go:109-138` (`noteRow`, `notesFor`)
- Modify: `internal/web/reviewdiff.go:43-54` (`remarkRows`)
- Modify: `internal/web/prsend.go:271-310` (`candidateRowWire`, `candidatesWire`)
- Modify: `internal/web/reviews.go:165-200` (`handleReview…`: counts, other rows)
- Test: `internal/web/notemeta_web_test.go`

**Interfaces:**
- Consumes:
  - `domain.NoteMeta(n model.Note, root *model.Note) notemeta.Resolved`
  - `notemeta.Resolve(meta, display, added)`
  - `domain.ReviewMetaCounts(r domain.Review) notemeta.Counts`
  - `svc.ReviewFileMetaCounts(ctx, id) (map[string]int, map[string]notemeta.Counts, error)`
  - `domain.ReviewOtherNote.Meta`, `domain.SendCandidate`'s `Meta`, both `notemeta.Resolved`
- Produces (JSON):
  - `noteRow.display` (object, omitted when empty)
  - `candidateRow.display`
  - review body `meta_counts` (`notemeta.Counts`, omitted when empty)
  - review body `meta_by_file` (`{path: notemeta.Counts}`, omitted when empty)
  - `other[i].display`
  - `counts` keeps its meaning (◆N per file)

- [ ] **Step 1: Write the failing tests**

`internal/web/notemeta_web_test.go`:

```go
package web

import (
	"context"
	"net/http"
	"testing"

	"github.com/homeend/gigagit/internal/notemeta"
)

// metaReviewDoc declares kind as a filled badge counted per value, and
// audience through `as`; the first remark is placed, the zzz.go one is not.
const metaReviewDoc = `{"version":1,"summary":"s",
 "display":{"kind":{"show":["badge","count"],"values":{"issue":{"color":"red"}}},
            "audience":{"as":"audience","show":["count"]},
            "confidence":{"show":["line"]}},
 "files":[
 {"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"A is unused","meta":{"kind":"issue","audience":"author","confidence":"high"}}]},
 {"path":"zzz.go","annotations":[{"newRange":[1,1],"summary":"elsewhere","meta":{"kind":"issue"}}]}]}`

// The review body carries the header's counts (every remark) and each
// file's counts (placed remarks only — what ◆N counts).
func TestReviewBodyCarriesMetaCounts(t *testing.T) {
	t.Parallel()
	ts, _, _, id := reviewServer(t, metaReviewDoc)
	var got struct {
		Counts     map[string]int             `json:"counts"`
		MetaCounts notemeta.Counts            `json:"meta_counts"`
		MetaByFile map[string]notemeta.Counts `json:"meta_by_file"`
	}
	if code := getJSON(t, ts, "/api/review/"+id, &got); code != http.StatusOK {
		t.Fatalf("GET review = %d", code)
	}
	if len(got.MetaCounts.Groups) != 1 || got.MetaCounts.Groups[0].Values[0].N != 2 || got.MetaCounts.Author != 1 {
		t.Fatalf("meta_counts = %+v, want kind issue 2 + 1 for author", got.MetaCounts)
	}
	f := got.MetaByFile["f.txt"]
	if len(f.Groups) != 1 || f.Groups[0].Values[0].N != 1 || f.Groups[0].Values[0].Color != "red" {
		t.Fatalf("meta_by_file[f.txt] = %+v, want issue 1 red", f)
	}
	if got.Counts["f.txt"] != 1 {
		t.Fatalf("counts = %v, ◆N must keep its meaning", got.Counts)
	}
}

// A remark the view could not place still shows its badges in Other notes.
func TestReviewOtherRowsCarryDisplay(t *testing.T) {
	t.Parallel()
	ts, _, _, id := reviewServer(t, metaReviewDoc)
	var got struct {
		Other []struct {
			Summary string             `json:"summary"`
			Display *notemeta.Resolved `json:"display"`
		} `json:"other"`
	}
	getJSON(t, ts, "/api/review/"+id, &got)
	if len(got.Other) != 1 || got.Other[0].Display == nil || len(got.Other[0].Display.Badges) != 1 ||
		got.Other[0].Display.Badges[0].Value != "issue" {
		t.Fatalf("other = %+v, want the zzz.go remark with its issue badge", got.Other)
	}
}

// The viewer's remark boxes carry the resolved display (lines included).
func TestRemarkRowsCarryDisplay(t *testing.T) {
	t.Parallel()
	_, svc, _, id := reviewServer(t, metaReviewDoc)
	ns, err := svc.ReviewFileNotes(context.Background(), id, "f.txt", []string{"A"})
	if err != nil || len(ns) != 1 {
		t.Fatalf("ReviewFileNotes = %v %v", ns, err)
	}
	row := remarkRows(ns)[0]
	if row.Display == nil || len(row.Display.Badges) != 1 || row.Display.Audience != "author" ||
		len(row.Display.Lines) != 1 || row.Display.Lines[0].Value != "high" {
		t.Fatalf("remark row display = %+v", row.Display)
	}
}
```

Before running: check that `newRepoDir(t, 2)` writes `f.txt` with an "A" first line, using `sed -n 37,50p internal/web/server_test.go`. If its content differs, use that first line in `ReviewFileNotes`'s `lines`.

Also add a send-panel test next to `internal/web/prsendpanel_test.go`'s existing candidate test. Reuse its setup, which builds a PR fixture with a review annotation `"meta":{"severity":"bug"}`.

- Copy that test's setup into `TestSendCandidateRowsCarryDisplay`.
- Change the fixture's annotation meta to `{"severity":"high"}` and add a doc display `{"severity":{"show":["badge"],"values":{"high":{"color":"red"}}}}`.
- Assert `rev.Rows[1].Display != nil && rev.Rows[1].Display.Badges[0].Color == "red"`.
- Add `Display *notemeta.Resolved` to that file's local row struct (`prsendpanel_test.go:8`).

- [ ] **Step 2: Run them to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/note-meta-web && go test ./internal/web -run 'TestReviewBodyCarriesMetaCounts|TestReviewOtherRowsCarryDisplay|TestRemarkRowsCarryDisplay|TestSendCandidateRowsCarryDisplay' -count=1`

Expected: FAIL. Either a compile error (`row.Display undefined`), or `meta_counts = {}`.

- [ ] **Step 3: Implement**

`filecontent.go`: add the field to `noteRow`, and set it in `notesFor`.

```go
	Side      string `json:"side,omitempty"`     // "old": a remark on a line the review deleted (the diff lays it there)
	// Display is what the note's meta shows (spec 2026-10-12-note-meta §7):
	// chips, frame, line and details rows. Omitted when there is none.
	Display *notemeta.Resolved `json:"display,omitempty"`
}

// rowDisplay is res as the wire's optional object.
func rowDisplay(res notemeta.Resolved) *notemeta.Resolved {
	if res.Empty() {
		return nil
	}
	return &res
}
```

In `notesFor`'s loop:

```go
		out[i] = noteRow{ID: n.ID, Start: n.Start, End: n.End, Summary: n.Summary, Rationale: n.Rationale,
			Author: n.Author, Outdated: n.Outdated, Ref: agentdocs.NoteReference(n),
			Display: rowDisplay(notemeta.Resolve(n.Meta, n.Display, n.Added))}
```

`reviewdiff.go` `remarkRows`: add `Display: rowDisplay(domain.NoteMeta(n.Note, nil))` to the literal. The domain fills a remark's `Meta`, its effective `Display` and its `Added` (`remarkMeta`).

`prsend.go`: add `Display *notemeta.Resolved `json:"display,omitempty"`` to `candidateRowWire`, and `Display: rowDisplay(r.Meta)` to the literal.

`reviews.go`:

```go
	if c, byFile, err := svc.ReviewFileMetaCounts(ctx, rv.ID); err == nil {
		if c != nil {
			out["counts"] = c
		}
		if len(byFile) > 0 {
			out["meta_by_file"] = byFile
		}
	}
	if mc := domain.ReviewMetaCounts(rv); len(mc.Groups) > 0 || mc.Author > 0 || mc.Reviewer > 0 {
		out["meta_counts"] = mc
	}
```

This replaces the `ReviewFileCounts` call. The `other` rows become `map[string]any`. Every existing value stays a string, because reviews.js compares `n.replies === "1"`. Then:

```go
			if d := rowDisplay(o.Meta); d != nil {
				row["display"] = d
			}
```

Add this after the `Outdated` reassignment, so an outdated row keeps its chips. Add the `notemeta` imports.

- [ ] **Step 4: Run them to verify they pass**

Run: the Step 2 command, then `go test ./internal/web -count=1 2>&1 | tail -3`.

Expected: the 4 tests PASS, and the package passes.

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/note-meta-web
gg add internal/web/filecontent.go internal/web/reviewdiff.go internal/web/prsend.go internal/web/reviews.go internal/web/notemeta_web_test.go internal/web/prsendpanel_test.go
git commit -F <msgfile>   # "web: note meta on the viewer, send-panel and review wire shapes" + trailers
```

---

### Task 2: `POST /api/notes/mark`

**Files:**
- Modify: `internal/web/notes.go` (route, handler, `noteErrStatus`)
- Test: `internal/web/notemeta_web_test.go`

**Interfaces:**
- Consumes: `svc.SetNoteMeta(ctx, id, by string, kv []notemeta.KV) ([]notemeta.Problem, error)`, `svc.UnsetNoteMeta(ctx, id, by string, keys []string) error`, `notemeta.StatusKey`, `model.IsForgeNoteID(id) bool`, `notes.ErrNewerFormat`.
- Produces: `POST /api/notes/mark {"id": "...", "status": "agreed"|"disagreed"|"deferred"|""}`. `""` = clear. It answers `{"ok": true}`, and `emitNotes()`. Errors:
  - 400: no id, or a status outside the four.
  - 409: a forge id, or a newer notes file.
  - 404: an unknown note or remark.

- [ ] **Step 1: Write the failing test**

Append to `notemeta_web_test.go`:

```go
// Mark tags a remark and a stored note with status; clear removes it; a
// forge thread, a free-form status and a missing id are refused.
func TestWebMarkTagsAndClears(t *testing.T) {
	t.Parallel()
	ts, _, _, id := reviewServer(t, webReviewDoc)
	remark := "review:" + id + ":0"
	status := func() string {
		var got struct {
			Notes []struct {
				Display *notemeta.Resolved `json:"display"`
			} `json:"notes"`
		}
		getJSON(t, ts, "/api/review/notes?id="+id+"&path=f.txt&status=M", &got)
		if len(got.Notes) != 1 || got.Notes[0].Display == nil || len(got.Notes[0].Display.Badges) == 0 {
			return ""
		}
		return got.Notes[0].Display.Badges[0].Value
	}
	if code, out := postJSONAny(t, ts, "/api/notes/mark", `{"id":"`+remark+`","status":"disagreed"}`); code != http.StatusOK {
		t.Fatalf("mark = %d %v", code, out)
	}
	if s := status(); s != "disagreed" {
		t.Fatalf("after mark the remark's badge = %q, want disagreed (the built-in status declaration)", s)
	}
	if code, out := postJSONAny(t, ts, "/api/notes/mark", `{"id":"`+remark+`","status":""}`); code != http.StatusOK {
		t.Fatalf("clear = %d %v", code, out)
	}
	if s := status(); s != "" {
		t.Fatalf("after clear the badge = %q, want none", s)
	}
	for _, c := range []struct {
		body string
		want int
	}{
		{`{"id":"forge:C1","status":"agreed"}`, http.StatusConflict},
		{`{"id":"` + remark + `","status":"lgtm"}`, http.StatusBadRequest},
		{`{"status":"agreed"}`, http.StatusBadRequest},
		{`{"id":"review:` + id + `:9","status":"agreed"}`, http.StatusNotFound},
	} {
		if code, out := postJSONAny(t, ts, "/api/notes/mark", c.body); code != c.want {
			t.Errorf("mark %s = %d %v, want %d", c.body, code, out, c.want)
		}
	}
}
```

Also add a newer-format case:

- Use `TestWebMarkRefusesANewerNotesFile`.
- Build a stored note: `svc.UseNotesDir(dir)`, then add a note through `postJSONAny(t, ts, "/api/notes/add", …)`. Copy the add body from an existing `/api/notes/add` test, using `grep -n '"/api/notes/add"' internal/web/*_test.go`.
- Rewrite the part file's `format = 3` line to `format = 4`. Find the file with `filepath.Glob(filepath.Join(dir, "*.toml"))` plus `dir/worktrees/*.toml`.
- Mark that note and assert `409`.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/web -run 'TestWebMark' -count=1`

Expected: FAIL. The route does not exist, so the answer is 404 or 405 instead of 200.

- [ ] **Step 3: Implement**

`notes.go`: add a route next to resolve.

```go
		mux.HandleFunc("POST /api/notes/mark", writeGuard(s.handleNoteMark))
```

Add the handler after `handleNoteResolve`:

```go
// handleNoteMark is the note box's Mark (spec 2026-10-12-note-meta §5.4, R12):
// the reader's status tag — agreed / disagreed / deferred, or "" to clear —
// written as the `status` meta key, shown through the built-in declaration.
// It is a tag and nothing else, and only those three words: no frontend gets
// a generic meta editor (R5). A GitHub thread is GitHub's, never marked.
func (s *Server) handleNoteMark(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad request body"))
		return
	}
	switch req.Status {
	case "", "agreed", "disagreed", "deferred":
	default:
		writeErr(w, http.StatusBadRequest, fmt.Errorf("unknown mark %q", req.Status))
		return
	}
	if req.ID == "" {
		writeErr(w, http.StatusBadRequest, errors.New("id is required"))
		return
	}
	if model.IsForgeNoteID(req.ID) {
		writeErr(w, http.StatusConflict, errors.New("a GitHub thread cannot be marked"))
		return
	}
	svc, ctx := s.service(), r.Context()
	by := s.noteAuthor(ctx, "")
	var err error
	if req.Status == "" {
		err = svc.UnsetNoteMeta(ctx, req.ID, by, []string{notemeta.StatusKey})
	} else {
		_, err = svc.SetNoteMeta(ctx, req.ID, by, []notemeta.KV{{Key: notemeta.StatusKey, Value: req.Status}})
	}
	if err != nil {
		writeErr(w, noteErrStatus(err), err)
		return
	}
	s.emitNotes()
	writeJSON(w, map[string]any{"ok": true})
}
```

In `noteErrStatus`, add `errors.Is(err, notes.ErrNewerFormat)` to the 409 case, and add `notes`, `notemeta`, `fmt` and `json` imports as needed. Check first that `internal/web` may import `internal/notes`, using `grep -rn "internal/notes\"" internal/web/*.go | head -2` and `internal/archtest`. If the arch test forbids it, use `errors.Is(err, domain.ErrNotesNewer)` instead, after adding to `internal/domain/note_meta.go`:

```go
// ErrNotesNewer is notes.ErrNewerFormat for frontends (domain-only).
var ErrNotesNewer = notes.ErrNewerFormat
```

- [ ] **Step 4: Run them to verify they pass**

Run: `go test ./internal/web -run 'TestWebMark' -count=1 && go test ./internal/archtest -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

Stage `internal/web/notes.go`, `internal/web/notemeta_web_test.go` and `internal/domain/note_meta.go` (if touched). Message: "web: POST /api/notes/mark — the note box's status tag".

---

### Task 3: The note-edits notice and gate

**Files:**
- Create: `internal/web/notemeta_notice.go`
- Modify: `internal/web/notifications.go:58-61` (`dismissableNotices`)
- Modify: `internal/web/preflight.go:22` (`gatedFeatures`)
- Test: `internal/web/notemeta_web_test.go`

**Interfaces:**
- Consumes: `svc.Preflight(ctx) ([]preflight.Verdict, error)`, `domain.FeatureNoteEdits`, `RegisterNotice`, `noticeWire`, `noticeAction`.
- Produces:
  - A notice with id `feature_disabled_note-edits`. That is the TUI's own id (`noticeFeatureDisabledPrefix + id`), so a dismissal in either frontend silences both.
  - Title: `note-edits is unavailable in this repository`.
  - Detail: the verdict's reason and remedy, `fmt.Sprintf`'d.
  - One action: `{label: "Never for this repo", dismiss: "feature_disabled_note-edits"}`.
  - `/api/preflight`'s `disabled` lists `note-edits` when it is off.

- [ ] **Step 1: Write the failing test**

```go
// A notes file a newer gg wrote: the centre says note-edits is unavailable
// (the TUI's own notice id) and /api/preflight reports the feature off, so
// the page hides Mark.
func TestNoteEditsUnavailableNotice(t *testing.T) {
	t.Parallel()
	svc, dir := newerNotesService(t) // helper below
	ts := serve(t, New(svc))
	var got noticesResp
	if code := getJSON(t, ts, "/api/notifications", &got); code != http.StatusOK {
		t.Fatalf("notifications = %d", code)
	}
	var found *noticeWire
	for i := range got.Notices {
		if got.Notices[i].ID == "feature_disabled_note-edits" {
			found = &got.Notices[i]
		}
	}
	if found == nil || found.Title != "note-edits is unavailable in this repository" || len(found.Detail) == 0 {
		t.Fatalf("notices = %+v, want the note-edits notice", got.Notices)
	}
	var pf struct {
		Disabled []string `json:"disabled"`
	}
	getJSON(t, ts, "/api/preflight", &pf)
	if !slices.Contains(pf.Disabled, "note-edits") {
		t.Fatalf("disabled = %v, want note-edits", pf.Disabled)
	}
	if code, out := postJSONAny(t, ts, "/api/notifications/dismiss", `{"id":"feature_disabled_note-edits"}`); code != http.StatusOK {
		t.Fatalf("dismiss = %d %v", code, out)
	}
	_ = dir
}
```

`newerNotesService(t)` builds a `domain.Open(newRepoDir(t, 1))` with `UseNotesDir(dir)`, adds one note through `svc` (`svc.NoteAdd`; take the exact call from an existing domain test via `grep -rn "func (s \*Service) NoteAdd\|AddNote(" internal/domain/*.go | head -3`), and rewrites that part file's `format = 3` to `format = 4`. It also needs a promptstore for dismiss. Check what `serve`/`New` give the dismiss handler: if `s.promptStore()` is nil in tests, read how `notifications_test.go:179` sets one (`XDG_STATE_HOME` via `t.Setenv` is not parallel-safe, so copy whatever that test does) and do the same. Share the helper with Task 2's newer-format test.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web -run TestNoteEditsUnavailableNotice -count=1`

Expected: FAIL — `want the note-edits notice`.

- [ ] **Step 3: Implement**

`internal/web/notemeta_notice.go`:

```go
package web

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/preflight"
)

// noticeNoteEdits is the TUI's own feature-disabled notice id for note-edits
// (internal/tui/notify.go: noticeFeatureDisabledPrefix + the feature id), so
// a dismissal in either frontend silences both. PERMANENT.
const noticeNoteEdits = "feature_disabled_" + domain.FeatureNoteEdits

func init() { RegisterNotice(noteEditsNotice) }

// noteEditsNotice says a notes file a newer gg wrote keeps every note
// readable but refuses writes (spec 2026-10-12-note-meta §10) — the web's
// half of the TUI's standing notice.
func noteEditsNotice(ctx context.Context, _ *Server, svc *domain.Service, _ model.RepoHealth, _ map[string]bool) []noticeWire {
	vs, err := svc.Preflight(ctx)
	if err != nil {
		return nil
	}
	for _, v := range vs {
		if v.Feature.ID != domain.FeatureNoteEdits || v.State == preflight.Satisfied {
			continue
		}
		detail := []string{fmt.Sprintf(v.Reason.Format, v.Reason.Args...)}
		if v.Remedy.Format != "" {
			detail = append(detail, fmt.Sprintf(v.Remedy.Format, v.Remedy.Args...))
		}
		return []noticeWire{{
			ID: noticeNoteEdits, Title: domain.FeatureNoteEdits + " is unavailable in this repository", Detail: detail,
			Actions: []noticeAction{{Label: "Never for this repo", Dismiss: noticeNoteEdits}},
		}}
	}
	return nil
}
```

Then:
- Add `noticeNoteEdits: true` to `dismissableNotices`.
- Change `gatedFeatures` to `[]string{domain.FeatureVersions, domain.FeatureNoteEdits}`.
- Check how notifications.js renders a `dismiss` action, with `grep -n "dismiss" internal/web/static/notifications.js | head`. If it only knows `op` actions, the existing `narrow_fetch_refspec` notice shows the pattern for a "never" row; reuse it.

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/web -run 'TestNoteEditsUnavailableNotice|TestNotif|TestPreflight' -count=1`

Expected: PASS. The existing preflight tests still say `disabled = none` on a fresh repo.

- [ ] **Step 5: Commit**

Stage the three Go files and the test. Message: "web: the note-edits unavailable notice (the TUI's id) and gate".

---

### Task 4: `notemeta.js` — the pure HTML builders, plus tokens and classes

**Files:**
- Create: `internal/web/static/notemeta.js`
- Create: `internal/web/notemetajs_test.go`
- Modify: `internal/web/static/style.css` (tokens on `:root`; classes near `.notebox`)
- Modify: `internal/web/style_groups_test.go` (pin the tokens)
- Modify: `internal/web/static/notebox.js` (`noteTitleParts`)
- Modify: `internal/web/noteboxjs_test.go` (titles unchanged + parts)

**Interfaces:**
- Produces (all exported from `notemeta.js`; `esc` is passed in, as with `mdHTML(tree, esc)`):
  - `META_COLOURS`: the 7 names.
  - `metaWord(key, value) → string`: verbatim.
  - `audienceWord(a) → "→ author" | "for you" | ""`.
  - `badgeRunHTML(d, esc) → string`: chips plus marker, `""` when none. A chip is `<span class="mchip mc-<c> ms-filled|ms-text">`.
  - `badgeRunCols(d) → int`: the run's width in columns, for the fit.
  - `titleMetaHTML(d, esc) → string`: the badge run plus `<span class="msep"> · </span>`, or `""`.
  - `metaLinesHTML(d, esc) → string`: `<div class="mline">label: value</div>` per line.
  - `metaDetailsHTML(d, esc) → string`: `<div class="mdetail">…</div>` rows for meta/added/problems.
  - `frameClass(d) → "" | "mf-<c>"`.
  - `countsText(c) → string`: `"2 issue · 1 for author"`.
  - `countsRunHTML(c, esc) → string`: `issue <span class="mc-red ms-text">1</span> …`.
  - `severityColour(d, severity) → "" | "<c>"`.
  - `fitNoteTitle(badgeCols, words, at, room, elide) → {words, at}`.
- Produces (`notebox.js`): `noteTitleParts(n, path, preview, nowMs) → {words, at, tail}`, with `noteTitle(...) === words + at + tail` joined exactly as today.
- CSS:
  - tokens `--meta-red … --meta-grey`
  - `.mchip`, `.mc-<c>`, `.ms-filled`, `.ms-text`, `.msep`, `.maud`
  - `.notebox.mf-<c>`, `.vnote.mf-<c>`
  - `.mline`, `.mdetail`, `.mcounts`

- [ ] **Step 1: Write the failing node test**

`internal/web/notemetajs_test.go` follows `TestNoteboxJS`: copy the file to `notemeta.mjs` in a temp dir and run a runner that prints JSON.

```go
package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// notemeta.js is import-free so node can pin it against the wire shape the
// server sends (notemeta.Resolved / notemeta.Counts JSON).
func TestNotemetaJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	dir := t.TempDir()
	src, err := os.ReadFile(filepath.Join("static", "notemeta.js"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notemeta.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	const runner = `
import * as M from "./notemeta.mjs";
const esc = (s) => String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
const elide = (s, n) => s.length <= n ? s : n <= 1 ? "…" : "…" + s.slice(s.length - n + 1);
const d = { badges: [
  { key: "kind", value: "issue", color: "red", style: "filled" },
  { key: "severity", value: "high", color: "orange", style: "text", glyph: "!" },
  { key: "status", value: "disagreed", color: "red", style: "text" },
  { key: "x", value: "<b>", color: "crimson", style: "text" } ],
  audience: "author", frame: "red",
  lines: [{ label: "confidence", value: "high" }],
  stored: [{ key: "tool", value: "lint" }],
  added: [{ key: "status", from: "agreed", to: "disagreed", by: "MR" }, { key: "k", to: "v" }],
  problems: ["display.kind.values.issue.color: \"crimson\" is not a colour"] };
const out = {
  run: M.badgeRunHTML(d, esc),
  runCols: M.badgeRunCols(d),
  title: M.titleMetaHTML(d, esc),
  none: M.titleMetaHTML(undefined, esc) + "|" + M.badgeRunHTML({}, esc) + "|" + M.metaLinesHTML(null, esc),
  lines: M.metaLinesHTML(d, esc),
  details: M.metaDetailsHTML(d, esc),
  frame: [M.frameClass(d), M.frameClass({ frame: "crimson" }), M.frameClass(null)],
  aud: [M.audienceWord("author"), M.audienceWord("reviewer"), M.audienceWord("x")],
  counts: M.countsText({ groups: [{ key: "kind", label: "kind", values: [{ value: "issue", color: "red", n: 2 }, { value: "good", n: 1 }] }], author: 1, reviewer: 3 }),
  countsRun: M.countsRunHTML({ groups: [{ key: "kind", values: [{ value: "issue", color: "red", n: 2 }] }] }, esc),
  countsNone: M.countsText(null) + "|" + M.countsRunHTML({}, esc),
  sev: [M.severityColour(d, "high"), M.severityColour(d, "low"), M.severityColour(null, "high")],
  fitWhole: M.fitNoteTitle(10, "note · bot · ", "a/b/c.go R12", 60, elide),
  fitPath: M.fitNoteTitle(10, "note · bot · ", "internal/deep/dir/file.go R12", 40, elide),
  fitWords: M.fitNoteTitle(10, "note · somebody long · ", "internal/deep/dir/file.go R12", 28, elide),
  fitTiny: M.fitNoteTitle(10, "note · bot · ", "internal/deep/dir/file.go R12", 14, elide),
  fitNoRoom: M.fitNoteTitle(10, "note · bot · ", "a.go R1", 0, elide),
};
console.log(JSON.stringify(out));
`
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(runner), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, raw)
	}
	var got struct {
		Run, Title, None, Lines, Details, Counts, CountsRun, CountsNone string
		RunCols                                                          int
		Frame, Aud, Sev                                                  []string
		FitWhole, FitPath, FitWords, FitTiny, FitNoRoom                  struct{ Words, At string }
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	want := `<span class="mchip mc-red ms-filled"> issue </span> ` +
		`<span class="mchip mc-orange ms-text">! high</span>  ` +
		`<span class="mchip mc-red ms-text">disagreed</span>  ` +
		`<span class="mchip mc-grey ms-text">&lt;b&gt;</span>  ` +
		`<span class="maud">→ author</span>`
	if got.Run != want {
		t.Errorf("run =\n%s\nwant\n%s", got.Run, want)
	}
	// " issue " 7 + 1 + "! high" 6 + 2 + "disagreed" 9 + 2 + "<b>" 3 + 2 + "→ author" 8
	if got.RunCols != 40 {
		t.Errorf("runCols = %d, want 40", got.RunCols)
	}
	if got.Title != want+`<span class="msep"> · </span>` {
		t.Errorf("title = %s", got.Title)
	}
	if got.None != "||" {
		t.Errorf("no display must draw nothing: %q", got.None)
	}
	if got.Lines != `<div class="mline">confidence: high</div>` {
		t.Errorf("lines = %s", got.Lines)
	}
	wantD := `<div class="mdetail">meta: tool: lint</div>` +
		`<div class="mdetail">added: status: agreed → disagreed (MR) · k: v</div>` +
		`<div class="mdetail">display problem: display.kind.values.issue.color: &quot;crimson&quot; is not a colour</div>`
	if got.Details != wantD {
		t.Errorf("details =\n%s\nwant\n%s", got.Details, wantD)
	}
	if got.Frame[0] != "mf-red" || got.Frame[1] != "" || got.Frame[2] != "" {
		t.Errorf("frame = %v (an unknown colour has no class)", got.Frame)
	}
	if got.Aud[0] != "→ author" || got.Aud[1] != "for you" || got.Aud[2] != "" {
		t.Errorf("aud = %v", got.Aud)
	}
	if got.Counts != "2 issue · 1 good · 1 for author · 3 for you" {
		t.Errorf("counts = %q", got.Counts)
	}
	if got.CountsRun != `issue <span class="mc-red ms-text">2</span>` || got.CountsNone != "|" {
		t.Errorf("countsRun = %q none = %q", got.CountsRun, got.CountsNone)
	}
	if got.Sev[0] != "orange" || got.Sev[1] != "" || got.Sev[2] != "" {
		t.Errorf("sev = %v", got.Sev)
	}
	// The TUI's fit: whole when it fits; the path elided (≥8) first; then the
	// words before it; then the path's tail; badges never.
	if got.FitWhole.Words != "note · bot · " || got.FitWhole.At != "a/b/c.go R12" {
		t.Errorf("fitWhole = %+v", got.FitWhole)
	}
	if got.FitPath.Words != "note · bot · " || got.FitPath.At != "…/dir/file.go R12" {
		t.Errorf("fitPath = %+v (40-10 badge cols = 30 room; words 13 → path 17)", got.FitPath)
	}
	// words 23 + path ≥ 8 > 18: the path stops at 8, the words take the rest.
	if got.FitWords.At != "….go R12" || got.FitWords.Words != "note · so…" {
		t.Errorf("fitWords = %+v", got.FitWords)
	}
	if got.FitTiny.Words != "" || got.FitTiny.At != "…R12" {
		t.Errorf("fitTiny = %+v", got.FitTiny)
	}
	if got.FitNoRoom.Words != "note · bot · " || got.FitNoRoom.At != "a.go R1" {
		t.Errorf("room 0 = unknown width: render whole, got %+v", got.FitNoRoom)
	}
}
```

The fit expectations were worked by hand through Step 3's algorithm, with the test's tail-keeping `elide` (core's `elidePath` keeps the file name instead). The test pins the ORDER: the path goes first down to 8 columns, then the words, then the path's tail.

Write the token-pin test in `style_groups_test.go`:

```go
// The meta colours (spec 2026-10-12-note-meta §6.4/§7) are the TUI's Dark
// meta_* roles: the web has one palette, the dark one.
func TestWebMetaColoursAreTheDarkTheme(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	d := theme.Dark
	for name, c := range map[string]string{"red": d.MetaRed, "orange": d.MetaOrange, "yellow": d.MetaYellow,
		"green": d.MetaGreen, "blue": d.MetaBlue, "purple": d.MetaPurple, "grey": d.MetaGrey} {
		m := regexp.MustCompile(`--meta-` + name + `:\s*([^;]+);`).FindStringSubmatch(string(b))
		if m == nil || !strings.EqualFold(strings.TrimSpace(m[1]), c) {
			t.Errorf("--meta-%s = %v, want %s", name, m, c)
		}
		for _, sel := range []string{".mc-" + name + ".ms-filled", ".mc-" + name + ".ms-text", ".notebox.mf-" + name, ".vnote.mf-" + name} {
			if !strings.Contains(string(b), sel) {
				t.Errorf("style.css lacks %s", sel)
			}
		}
	}
	// Dim wins over the frame: the stale/outdated rules come AFTER the frame rules.
	s := string(b)
	if strings.LastIndex(s, ".notebox.mf-") > strings.Index(s, ".notebox.stale {") {
		t.Errorf("a .notebox.mf-* rule follows .notebox.stale — a stale note would take the frame colour")
	}
}
```

Extend `noteboxjs_test.go`'s runner with `noteTitleParts` on the same inputs. Assert `words + at + tail === noteTitle(...)` for every input, plus one exact split: for `n3` (`note · a.go R4`) that is `{words: "note · ", at: "a.go R4", tail: ""}`.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/web -run 'TestNotemetaJS|TestWebMetaColours|TestNoteboxJS' -count=1`

Expected: FAIL. `notemeta.js` does not exist, the tokens are missing, and `noteTitleParts` is not exported.

- [ ] **Step 3: Implement**

`internal/web/static/notemeta.js`:

```js
// notemeta.js — a note's meta as HTML (spec 2026-10-12-note-meta §7): the
// chips, the audience marker, the line and details rows, the counts, and the
// title fit. The server sends the RESOLVED display (notemeta.Resolved) on
// every note; nothing here decides what shows, it only draws it the way the
// TUI does (internal/tui/note_meta.go). Import-free on purpose, so
// notemetajs_test.go runs it under node; callers pass esc in.

// META_COLOURS is the fixed palette (spec §3.3): a name off this list is
// never turned into a class — it falls to the place's default.
export const META_COLOURS = ["red", "orange", "yellow", "green", "blue", "purple", "grey"];
export const META_TITLE_SEP = " · ";
const MIN_PATH_COLS = 8;

const colourOr = (c, dflt) => (META_COLOURS.includes(c) ? c : dflt);
const cols = (s) => [...String(s)].length;

// metaWord is a value as shown: agent data, verbatim (escaped by the caller).
export function metaWord(key, value) {
  return String(value == null ? "" : value);
}

// audienceWord is gg's marker for who a note is for (spec §3.3).
export function audienceWord(a) {
  return a === "author" ? "→ author" : a === "reviewer" ? "for you" : "";
}

// chipText is a badge's text: the glyph (if any), the value; a filled chip
// carries one column of fill each side.
function chipText(b) {
  const v = (b.glyph ? b.glyph + " " : "") + metaWord(b.key, b.value);
  return b.style === "filled" ? " " + v + " " : v;
}

// runParts is the badge run as [text, cls] pieces with the TUI's spacing:
// one space beside a filled chip, two between text badges, two before the
// marker.
function runParts(d) {
  const out = [];
  const bs = (d && d.badges) || [];
  bs.forEach((b, i) => {
    if (i > 0) out.push([b.style === "filled" || bs[i - 1].style === "filled" ? " " : "  ", ""]);
    out.push([chipText(b), `mchip mc-${colourOr(b.color, "grey")} ms-${b.style === "filled" ? "filled" : "text"}`]);
  });
  const m = audienceWord(d && d.audience);
  if (m) {
    if (out.length) out.push(["  ", ""]);
    out.push([m, "maud"]);
  }
  return out;
}

export function badgeRunHTML(d, esc) {
  return runParts(d).map(([t, c]) => (c ? `<span class="${c}">${esc(t)}</span>` : t)).join("");
}

export function badgeRunCols(d) {
  return runParts(d).reduce((n, [t]) => n + cols(t), 0);
}

// titleMetaHTML is what leads a note's title: the run, then " · ".
export function titleMetaHTML(d, esc) {
  const run = badgeRunHTML(d, esc);
  return run ? run + `<span class="msep">${META_TITLE_SEP}</span>` : "";
}

// metaLinesHTML is one dim "label: value" row per line entry (spec §3.2).
export function metaLinesHTML(d, esc) {
  return ((d && d.lines) || []).map((l) => `<div class="mline">${esc(l.label + ": " + l.value)}</div>`).join("");
}

// metaDetailsHTML is the details rows: stored meta, the added changes, the
// display problems (the TUI's metaDetailTexts, word for word).
export function metaDetailsHTML(d, esc) {
  if (!d) return "";
  const rows = [];
  if ((d.stored || []).length) rows.push("meta: " + d.stored.map((kv) => kv.key + ": " + kv.value).join(" · "));
  if ((d.added || []).length)
    rows.push(
      "added: " +
        d.added.map((c) => c.key + ": " + (c.from ? c.from + " → " + c.to : c.to) + (c.by ? " (" + c.by + ")" : "")).join(" · ")
    );
  for (const p of d.problems || []) rows.push("display problem: " + p);
  return rows.map((r) => `<div class="mdetail">${esc(r)}</div>`).join("");
}

// frameClass is the box's border colour class, "" for none (or a colour off
// the palette). The stylesheet lets stale/outdated win over it.
export function frameClass(d) {
  return d && META_COLOURS.includes(d.frame) ? "mf-" + d.frame : "";
}

// countsText is the header's counts in plain words (spec §6.2).
export function countsText(c) {
  if (!c) return "";
  const parts = [];
  for (const g of c.groups || []) for (const v of g.values || []) parts.push(v.n + " " + metaWord(g.key, v.value));
  if (c.author > 0) parts.push(c.author + " for author");
  if (c.reviewer > 0) parts.push(c.reviewer + " for you");
  return parts.join(" · ");
}

// countsRunHTML is a file row's compact counts: "issue 1 good 1", the
// value's colour on its number.
export function countsRunHTML(c, esc) {
  const parts = [];
  for (const g of (c && c.groups) || [])
    for (const v of g.values || [])
      parts.push(`${esc(metaWord(g.key, v.value))} <span class="mc-${colourOr(v.color, "grey")} ms-text">${v.n}</span>`);
  return parts.join(" ");
}

// severityColour is the colour the note's resolved badges give its severity
// — the send panel's .psev chip — "" when no badge says.
export function severityColour(d, severity) {
  const b = ((d && d.badges) || []).find((x) => x.key === "severity" && x.value === severity);
  return b && META_COLOURS.includes(b.color) ? b.color : "";
}

// fitNoteTitle fits the words before the place and the place ("path R12")
// into room columns after badgeCols of badges — the TUI's fitNoteTitle:
// whole when it fits; else the path is elided in the middle (elide = core's
// elidePath) down to MIN_PATH_COLS, then the words are cut, then the path's
// tail. Badges are never cut here (the stylesheet's ellipsis is the backstop
// past that). room <= 0 = an unknown width: everything whole.
export function fitNoteTitle(badgeCols, words, at, room, elide) {
  if (room <= 0) return { words, at };
  const left = room - badgeCols;
  if (cols(words) + cols(at) <= left) return { words, at };
  const pathRoom = Math.max(MIN_PATH_COLS, left - cols(words));
  let a = cols(at) > pathRoom ? elide(at, pathRoom) : at;
  let w = words;
  if (cols(w) + cols(a) > left) {
    const wr = left - cols(a);
    w = wr > 1 ? [...w].slice(0, wr - 1).join("") + "…" : "";
  }
  if (cols(w) + cols(a) > left) a = elide(at, Math.max(1, left));
  return { words: w, at: a };
}
```

`notebox.js`: split `noteTitle`.

```js
// noteTitleParts is noteTitle in its three parts — the words before the
// place, the place ("path R12"), the tail after it — so a box can fit the
// title around the place (the TUI's fitNoteTitle). noteTitle is their join.
export function noteTitleParts(n, path, preview, nowMs) {
  const side = n.side === "old" ? "L" : "R";
  const lines = n.range && n.range[1] > n.range[0] ? n.range[0] + "-" + n.range[1] : n.line;
  if (n.read_only) {
    const at = n.file_level ? path + " (file)" : path + " " + side + lines;
    const pre = ["review", n.author, noteAge(n.created, nowMs)].filter(Boolean).join(" · ");
    return { words: pre + " · ", at, tail: n.resolved ? " · resolved" : "" };
  }
  const stale = n.status === "stale" || n.status === "outdated";
  const word = stale ? (preview ? " (outdated)" : " (stale)") : "";
  return {
    words: (n.source === "agent" ? "agent note" : "note") + (n.author ? " · " + n.author : "") + " · ",
    at: path + " " + side + lines,
    tail: word + (n.resolved ? " · resolved" : ""),
  };
}

export function noteTitle(n, path, preview, nowMs) {
  const p = noteTitleParts(n, path, preview, nowMs);
  return p.words + p.at + p.tail;
}
```

Check: the forge branch joined `[…, at, resolved?"resolved":""]` with `" · "`, so `pre + " · " + at + " · resolved"` is the same string. `pre` is never empty, because "review" is always there.

`style.css`:
- Add to `:root` after the note-group line:
  `/* note meta (spec 2026-10-12-note-meta §7): the TUI's meta_* roles (Dark); style_groups_test.go pins them */`
  `--meta-red: #E06C75; --meta-orange: #E5964B; --meta-yellow: #E5C07B; --meta-green: #98C379; --meta-blue: #61AFEF; --meta-purple: #C678DD; --meta-grey: #7F848E;`
- Put the frame rules BEFORE `.notebox.stale` (insert them right after `.notebox.agent`), and the rest after `.notetext`:

```css
.notebox.mf-red { border-color: var(--meta-red); } .notebox.mf-orange { border-color: var(--meta-orange); }
.notebox.mf-yellow { border-color: var(--meta-yellow); } .notebox.mf-green { border-color: var(--meta-green); }
.notebox.mf-blue { border-color: var(--meta-blue); } .notebox.mf-purple { border-color: var(--meta-purple); }
.notebox.mf-grey { border-color: var(--meta-grey); }
```

```css
/* note meta (spec 2026-10-12-note-meta §7): chips in the title, line and
   details rows in the box. A filled chip is the colour as background with the
   page's background as text; a text chip is the colour as text. */
.mchip { white-space: pre; }
.mc-red.ms-text { color: var(--meta-red); } .mc-red.ms-filled { background: var(--meta-red); color: var(--bg); }
.mc-orange.ms-text { color: var(--meta-orange); } .mc-orange.ms-filled { background: var(--meta-orange); color: var(--bg); }
.mc-yellow.ms-text { color: var(--meta-yellow); } .mc-yellow.ms-filled { background: var(--meta-yellow); color: var(--bg); }
.mc-green.ms-text { color: var(--meta-green); } .mc-green.ms-filled { background: var(--meta-green); color: var(--bg); }
.mc-blue.ms-text { color: var(--meta-blue); } .mc-blue.ms-filled { background: var(--meta-blue); color: var(--bg); }
.mc-purple.ms-text { color: var(--meta-purple); } .mc-purple.ms-filled { background: var(--meta-purple); color: var(--bg); }
.mc-grey.ms-text { color: var(--meta-grey); } .mc-grey.ms-filled { background: var(--meta-grey); color: var(--bg); }
.maud, .msep { color: var(--dim); white-space: pre; }
.mline, .mdetail { color: var(--dim); font-size: .92em; }
.mdetail { margin-top: 4px; }
.notereply .mrun { margin: 2px 0; }
/* a stale/outdated box keeps its chips readable but dim, like the TUI */
.notebox.stale .mchip, .notebox.outdated .mchip, .vnote.outdated .mchip { opacity: .7; }
```

- Add `.vnote.mf-*` frame rules (the same seven with `.vnote.`). Place them BEFORE `.vnote.outdated`, and `.vnote.remark` keeps its own rule after them. Decide: a remark box with a declared frame takes the frame. `.vnote.remark` sets `border-color: var(--dim)` — so put the `.vnote.mf-*` rules AFTER `.vnote.remark` but BEFORE `.vnote.outdated`. `.vnote.outdated` dims by opacity, which the frame does not undo.
- Collapsed boxes: extend the `table.diff tr.note.collapsed …` rule list with `table.diff tr.note.collapsed .mline, table.diff tr.note.collapsed .mdetail, table.diff tr.note.collapsed .mrun`.

Title layout: `.notetitle` is `position:absolute` with nowrap and ellipsis. Keep it, and make the words/place spans inline. The fit is done in JS (Task 5); the ellipsis stays the backstop.

- [ ] **Step 4: Run them to verify they pass**

Run: the Step 2 command, plus `go test ./internal/web/static -count=1` (the embed test).

Expected: PASS.

- [ ] **Step 5: Commit**

Stage `notemeta.js`, `notebox.js`, `style.css`, `notemetajs_test.go`, `noteboxjs_test.go` and `style_groups_test.go`. Message: "web: notemeta.js — chips, rows, counts and the title fit; meta colour tokens".

---

### Task 5: The diff note box (files.js) — chips, frame, rows, replies

**Files:**
- Modify: `internal/web/static/files.js:2757-2815` (`noteBoxHTML`)
- Create: `internal/web/notemeta_static_test.go`

**Interfaces:**
- Consumes:
  - from `notemeta.js` (Task 4): `titleMetaHTML`, `badgeRunHTML`, `badgeRunCols`, `metaLinesHTML`, `metaDetailsHTML`, `frameClass`, `fitNoteTitle`
  - from `notebox.js`: `noteTitleParts`
  - from `core.js`: `elidePath`, `charWidth`
- Produces: `noteTitleRoom(cols, n) → int` (files.js, internal) — the title's column budget from `#diff-body`'s width. The value is 0 when it is not laid out.

- [ ] **Step 1: Write the failing wiring test**

`internal/web/notemeta_static_test.go`:

```go
package web

import (
	"strings"
	"testing"
)

// Spec 2026-10-12-note-meta §7: the diff note box draws the resolved
// display — chips before the title, the frame class, line rows under the
// title, details rows after the body, and each reply its own run.
func TestDiffNoteBoxDrawsMeta(t *testing.T) {
	t.Parallel()
	f := readStatic(t, "files.js")
	for _, want := range []string{
		`from "./notemeta.js"`,
		`titleMetaHTML(n.display, esc)`,
		`frameClass(n.display)`,
		`metaLinesHTML(n.display, esc)`,
		`metaDetailsHTML(n.display, esc)`,
		`fitNoteTitle(badgeRunCols(n.display)`,
		"`<div class=\"mrun\">${badgeRunHTML(r.display, esc)}</div>`",
		`metaLinesHTML(r.display, esc)`,
		`metaDetailsHTML(r.display, esc)`,
	} {
		if !strings.Contains(f, want) {
			t.Errorf("files.js lacks %s", want)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web -run TestDiffNoteBoxDrawsMeta -count=1`

Expected: FAIL, with every line missing.

- [ ] **Step 3: Implement**

In files.js, extend the `notebox.js` import with `noteTitleParts`, and add:

```js
import { badgeRunCols, badgeRunHTML, fitNoteTitle, frameClass, metaDetailsHTML, metaLinesHTML, titleMetaHTML } from "./notemeta.js";
```

Add above `noteBoxHTML`:

```js
// noteTitleRoom is a box title's budget in columns: the diff body's width
// (half of it for a box in one pane of the split layout), less the box's
// border inset and the title's own padding, at the title's .92em. 0 = not
// laid out yet: the title renders whole and the CSS ellipsis is the backstop.
function noteTitleRoom(cols, n) {
  const body = $("diff-body");
  const px = body ? body.clientWidth : 0;
  if (px <= 0) return 0;
  const boxPx = (cols === 4 && !n.file_level ? px / 2 : px) - 64;
  return Math.max(0, Math.floor(boxPx / (charWidth() * 0.92)) - 2);
}
```

In `noteBoxHTML`, replace the `title` construction and the title div:

```js
  const parts = noteTitleParts(n, nc.ctx ? nc.ctx.path : "", prev, Date.now());
  const meta = rootOn ? n.display : null; // a hidden root's chips go with it
  const fit = fitNoteTitle(badgeRunCols(n.display) + (mark ? 3 : 0), parts.words, parts.at + parts.tail,
    rootOn ? noteTitleRoom(cols, n) : 0, elidePath);
  …
  let box =
    `<div class="notebox ${kind}${stale ? " " + cls : ""}${slot}${meta && frameClass(n.display) ? " " + frameClass(n.display) : ""}">` +
    `<div class="notetitle" data-collapse="${esc(n.id)}" title="${esc(parts.words + parts.at + parts.tail)} — click (or z) to collapse / expand">` +
    `<span class="notefold"></span>` +
    (mark ? `<span class="notemark ${mark.cls}" title="${esc(mark.tip)}">${mark.glyph}</span> ` : "") +
    (meta ? titleMetaHTML(n.display, esc) : "") +
    `${esc(fit.words + fit.at)}</div>`;
```

- `fit.at` carries the tail. Elision runs on `at + tail`, which keeps the tail (` (stale)`), because `elidePath` keeps the end. Ruling: the TUI elides the path alone, and the web elides place+tail as one string — cost: ` · resolved` may eat path columns.
- The tooltip carries the full title, so nothing is lost.

The body: line rows come first under the title, then the summary, rationale and link, then the details.

```js
  if (rootOn) box += metaLinesHTML(n.display, esc) + part(n, "") + text(n) + linkLine(n) + metaDetailsHTML(n.display, esc);
```

Each reply gets its own badge row, lines and details:

```js
  for (const r of reps) {
    const run = badgeRunHTML(r.display, esc);
    box +=
      `<div class="notereply" data-note="${esc(r.id)}">` +
      (run ? `<div class="mrun">${badgeRunHTML(r.display, esc)}</div>` : "") +
      metaLinesHTML(r.display, esc) +
      part(r, "↳ " + (r.author ? r.author + ": " : "")) + text(r) + linkLine(r) +
      metaDetailsHTML(r.display, esc) +
      `</div>`;
  }
```

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/web -run 'TestDiffNoteBoxDrawsMeta|TestNotebox|TestNotes' -count=1`. Also run `node --check internal/web/static/files.js`, and grep that every new import name is exported.

Expected: PASS.

- [ ] **Step 5: Commit**

Message: "web: the diff note box shows note meta (chips, frame, rows, replies)".

---

### Task 6: The viewer's note boxes (temporary notes and remarks)

**Files:**
- Modify: `internal/web/static/viewer.js:1813-1822` (`noteBoxHTML`)
- Modify: `internal/web/notemeta_static_test.go`

**Interfaces:**
- Consumes: `titleMetaHTML`, `metaLinesHTML`, `metaDetailsHTML`, `frameClass` (Task 4); `noteRow.display` (Task 1).

- [ ] **Step 1: Write the failing wiring test**

```go
// The viewer's boxes (an agent's temporary notes, a review's remarks) draw
// the same display.
func TestViewerNoteBoxDrawsMeta(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{
		`from "./notemeta.js"`,
		`frameClass(n.display)`,
		`<div class="vnote-title">${titleMetaHTML(n.display, esc)}${esc(noteBoxTitle(n))}</div>`,
		`metaLinesHTML(n.display, esc)`,
		`metaDetailsHTML(n.display, esc)`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %s", want)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web -run TestViewerNoteBoxDrawsMeta -count=1`

Expected: FAIL.

- [ ] **Step 3: Implement**

```js
import { frameClass, metaDetailsHTML, metaLinesHTML, titleMetaHTML } from "./notemeta.js";
…
function noteBoxHTML(n, acts = !n.readonly) {
  const fc = frameClass(n.display);
  return (
    `<div class="vnote${n.outdated ? " outdated" : ""}${n.readonly ? " remark" : ""}${fc ? " " + fc : ""}" data-note="${esc(n.id)}">` +
    `<div class="vnote-title">${titleMetaHTML(n.display, esc)}${esc(noteBoxTitle(n))}</div>` +
    metaLinesHTML(n.display, esc) +
    `<div class="vnote-sum">${esc(n.summary)}</div>` +
    (n.rationale ? `<div class="vnote-why">${esc(n.rationale)}</div>` : "") +
    metaDetailsHTML(n.display, esc) +
    (acts ? `<div class="vnote-acts"><button data-nact="dismiss">dismiss</button><button data-nact="ref">copy reference</button></div>` : "") +
    `</div>`
  );
}
```

The viewer title holds no path (`line 12`), so it needs no fit. The `.vnote-title` colour (accent or dim) stays on the words; chips carry their own colour.

Check `viewernotesjs_test.go` and `overviewsplitjs_test.go` for string pins on the old `noteBoxHTML` body (`grep -n "vnote-title" internal/web/*_test.go`). Update any pin to the new line, and ledger it.

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/web -count=1 2>&1 | tail -3`

Expected: `ok`.

- [ ] **Step 5: Commit**

Message: "web: the viewer's note and remark boxes show note meta".

---

### Task 7: The review view — counts line, file-row tails, other notes

**Files:**
- Modify: `internal/web/static/index.html` (`#files-header`)
- Modify: `internal/web/static/style.css` (`#files-counts`)
- Modify: `internal/web/static/files.js` (`setFilesCounts`, `enterFilesStage`)
- Modify: `internal/web/static/reviews.js` (`setReviewHeader`, `renderReviewFiles`, `reviewSummaryHTML`)
- Modify: `internal/web/notemeta_static_test.go`

**Interfaces:**
- Consumes: `d.meta_counts`, `d.meta_by_file`, `other[i].display` (Task 1); `countsText`, `countsRunHTML`, `badgeRunHTML` (Task 4).
- Produces: `setFilesCounts(text)` (files.js, exported). A `""` text hides the line.

- [ ] **Step 1: Write the failing wiring test**

```go
// The review view: the header's counts on a line of their own, each file's
// compact counts after its ◆N, the unplaced remarks' chips in Other notes.
func TestReviewViewDrawsMetaCounts(t *testing.T) {
	t.Parallel()
	idx, css := readStatic(t, "index.html"), readStatic(t, "style.css")
	f, r := readStatic(t, "files.js"), readStatic(t, "reviews.js")
	for _, c := range []struct{ src, file, want string }{
		{idx, "index.html", `<span id="files-counts" class="hidden"></span>`},
		{css, "style.css", `#files-counts.hidden { display: none; }`},
		{f, "files.js", `export function setFilesCounts(text)`},
		{f, "files.js", `setFilesCounts(""); // a review sets its counts`},
		{r, "reviews.js", `setFilesCounts(countsText(d.meta_counts))`},
		{r, "reviews.js", `countsRunHTML((d.meta_by_file || {})[f.path], esc)`},
		{r, "reviews.js", `badgeRunHTML(n.display, esc)`},
	} {
		if !strings.Contains(c.src, c.want) {
			t.Errorf("%s lacks %s", c.file, c.want)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web -run TestReviewViewDrawsMetaCounts -count=1`

Expected: FAIL.

- [ ] **Step 3: Implement**

`index.html`: add `<span id="files-counts" class="hidden"></span>` right after `<span id="files-meta" class="hidden"></span>`.

`style.css`, next to `#files-meta`:

```css
/* A review's note-meta counts (spec 2026-10-12-note-meta §6.2): their own
   line under the meta line, cut at the pane's edge (the full text is the tooltip). */
#files-counts { flex: 1 0 100%; font-size: 11px; opacity: .8; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
#files-counts.hidden { display: none; }
.rcounts { margin-left: 8px; font-size: .9em; color: var(--dim); white-space: nowrap; }
```

`files.js`:

```js
// setFilesCounts draws (or hides) a review's note-meta counts line.
export function setFilesCounts(text) {
  const el = $("files-counts");
  if (!el) return;
  el.textContent = text || "";
  el.title = text || "";
  el.classList.toggle("hidden", !text);
}
```

In `enterFilesStage`, after `setFilesDesc("")`, add: `setFilesCounts(""); // a review sets its counts`.

`reviews.js`:
- Import `setFilesCounts` from files.js and `{ badgeRunHTML, countsRunHTML, countsText }` from `./notemeta.js`.
- In `setReviewHeader`, after `setFilesMeta(...)`, add `setFilesCounts(countsText(d.meta_counts));`.
- In `renderReviewFiles`, after `noteBadgeHTML(counts[f.path])`, add the tail:

```js
      reviewCountsTailHTML(d, f.path) +
```

```js
// reviewCountsTailHTML is a file row's compact meta counts ("issue 1 good 1",
// placed remarks only — what ◆N counts). The path keeps its budget: the tail
// is the first thing the row's CSS ellipsis takes.
function reviewCountsTailHTML(d, path) {
  const run = countsRunHTML((d.meta_by_file || {})[path], esc);
  return run ? `<span class="rcounts">${run}</span>` : "";
}
```

In `reviewSummaryHTML`'s `other.map`, put the chips first:

```js
            const run = badgeRunHTML(n.display, esc);
            return `<li>${run ? run + "  " : ""}${esc(head + (thread ? " — " + thread : ""))}</li>`;
```

**Ruling: file-row tails rely on the row's ellipsis.** The path budget (`fileCols`) is unchanged, so the tail shows "only while it fits", and it is clipped first because it sits after the path. Cost: a long path in a narrow pane hides the tail, which is the TUI's rule too.

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/web -run 'TestReviewViewDrawsMetaCounts|TestReview' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

Message: "web: review view note-meta counts (header line, file rows) and Other-notes chips".

---

### Task 8: The send panel — the opened row's meta row, and `.psev` colour

**Files:**
- Modify: `internal/web/static/prsendrows.js:49-51` (carry `display`)
- Modify: `internal/web/static/prsendpanel.js:82-95`
- Modify: `internal/web/static/style.css` (`.pmeta`, `.psev.mc-*`)
- Modify: `internal/web/notemeta_static_test.go`
- Modify: `internal/web/prsendjs_test.go` (if `panelRows` output is pinned field-for-field)

**Interfaces:**
- Consumes: `candidateRow.display` (Task 1); `badgeRunHTML`, `metaLinesHTML`, `severityColour` (Task 4).

- [ ] **Step 1: Write the failing wiring test**

```go
// R9: the send panel keeps nothing ticked and no filter; meta shows only in an
// OPENED row, and the collapsed row's severity chip takes the badge colour.
func TestSendPanelDrawsMetaInOpenedRows(t *testing.T) {
	t.Parallel()
	p, rows := readStatic(t, "prsendpanel.js"), readStatic(t, "prsendrows.js")
	for _, want := range []string{
		`severityColour(r.display, r.severity)`,
		`<div class="pmeta">${badgeRunHTML(r.display, esc)}${metaLinesHTML(r.display, esc)}</div>`,
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prsendpanel.js lacks %s", want)
		}
	}
	if !strings.Contains(rows, `display: r.display || null`) {
		t.Errorf("prsendrows.js does not carry the row's display")
	}
	if strings.Contains(p, "display") && strings.Contains(p, "ticked.add(") && strings.Contains(p, "r.display &&") {
		t.Errorf("meta must decide nothing about ticking (R9, R12)")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web -run TestSendPanelDrawsMetaInOpenedRows -count=1`

Expected: FAIL.

- [ ] **Step 3: Implement**

`prsendrows.js` row literal: add `display: r.display || null`. If `prsendjs_test.go` deep-compares rows, add `display: null` to its expected rows (check its assertions).

`prsendpanel.js`:

```js
import { badgeRunHTML, metaLinesHTML, severityColour } from "./notemeta.js";
…
      const sc = r.severity ? severityColour(r.display, r.severity) : "";
      const sev = r.severity ? `<span class="psev ${esc(r.severity)}${sc ? " mc-" + sc + " ms-filled" : ""}">${esc(r.severity)}</span>` : "";
      …
      const metaRow = open && r.display && (badgeRunHTML(r.display, esc) || metaLinesHTML(r.display, esc))
        ? `<div class="pmeta">${badgeRunHTML(r.display, esc)}${metaLinesHTML(r.display, esc)}</div>` : "";
      const more =
        open && (r.rationale || r.code.length || metaRow)
          ? `<div class="pmore">${metaRow}${r.rationale ? `<div class="prat">${esc(r.rationale)}</div>` : ""}${r.code.length ? `<pre class="pcode">${esc(r.code.join("\n"))}</pre>` : ""}</div>`
          : "";
```

`severityColour` only reads the colour from the whitelist, and the class is built from that whitelisted name.

`style.css`:
- `#prsendpanel-list .psev.ms-filled` must beat the base `.psev` background, so add `#prsendpanel-list .psev.mc-red.ms-filled { background: var(--meta-red); }` for each of the seven. Specificity: id + 3 classes beats id + 1 class.
- Add `#prsendpanel-list .pmeta { white-space: normal; }`.

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/web -run 'TestSendPanel|TestPRSend|TestPrsend' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

Message: "web: send panel — meta in the opened row, .psev takes the severity colour".

---

### Task 9: The Mark menu, and the review counts after a mark

**Files:**
- Modify: `internal/web/static/files.js` (the note ctx menu at about line 3797; a `markNote` helper)
- Modify: `internal/web/static/reviews.js` (`refreshReviewMeta`)
- Modify: `internal/web/notemeta_static_test.go`

**Interfaces:**
- Consumes: `POST /api/notes/mark` (Task 2); `featureDisabled` from `preflight.js`; `noteWrite(label, path, body, saved)`.
- Produces: `refreshReviewMeta()` (reviews.js, exported). It re-reads `/api/review/{id}` and repaints the header counts and file rows; without an open review it does nothing.

- [ ] **Step 1: Write the failing wiring test**

```go
// Mark (spec §5.4, R12): a header and four rows in the diff note box's
// menu — never on a GitHub thread, hidden while note-edits is off — and a
// mark inside a review view re-reads the review's counts.
func TestDiffNoteMenuMarks(t *testing.T) {
	t.Parallel()
	f, r := readStatic(t, "files.js"), readStatic(t, "reviews.js")
	for _, want := range []string{
		`import { featureDisabled } from "./preflight.js";`,
		`{ header: "Mark" }`,
		`for (const s of ["agreed", "disagreed", "deferred"])`,
		`{ label: "clear", act: () => markNote(n, "") }`,
		`!String(rootId).startsWith("forge:") && n.source !== "forge" && !featureDisabled("note-edits")`,
		`noteWrite("mark", "/api/notes/mark", { id: n.id, status }, () => refreshReviewMeta())`,
	} {
		if !strings.Contains(f, want) {
			t.Errorf("files.js lacks %s", want)
		}
	}
	if !strings.Contains(r, `export async function refreshReviewMeta()`) {
		t.Errorf("reviews.js lacks refreshReviewMeta")
	}
	// Open + Delete only on Files-view note rows (user ruling).
	if i := strings.Index(r, "function notedRowMenu"); i >= 0 && strings.Contains(r[i:i+800], "/api/notes/mark") {
		t.Errorf("a Files-view note row must not offer Mark")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web -run TestDiffNoteMenuMarks -count=1`

Expected: FAIL.

- [ ] **Step 3: Implement**

files.js: import `featureDisabled` (check it is not already imported) and `refreshReviewMeta` from reviews.js (add it to the existing reviews.js import list). Add near `resolveNote`:

```js
// markNote is Mark (spec 2026-10-12-note-meta §5.4, R12): the reader's
// status tag on the note, "" clears it. A tag only — it resolves, sends,
// folds and filters nothing; the note lanes reload, and a review view
// re-reads its counts.
function markNote(n, status) {
  noteWrite("mark", "/api/notes/mark", { id: n.id, status }, () => refreshReviewMeta());
}
```

In the note menu, after the Resolve/Reopen row:

```js
  if (!String(rootId).startsWith("forge:") && n.source !== "forge" && !featureDisabled("note-edits")) {
    noteRows.push({ sep: true }, { header: "Mark" });
    for (const s of ["agreed", "disagreed", "deferred"]) noteRows.push({ label: s, act: () => markNote(n, s) });
    noteRows.push({ label: "clear", act: () => markNote(n, "") }, { sep: true });
  }
```

**Ruling: Mark acts on the right-clicked part (`n`, a root or a reply).** A reply carries its own badge row; the TUI marks the thread target at the cursor. Cost: one id change if the user wants roots only.

reviews.js:

```js
// refreshReviewMeta re-reads the open review after a mark: the header's
// counts and each file's tail follow the new status (the TUI re-reads both).
// A review closed or replaced meanwhile is left alone.
export async function refreshReviewMeta() {
  const rv = state.review;
  if (!rv) return;
  let d;
  try {
    d = await getJSON("/api/review/" + encodeURIComponent(rv.id));
  } catch {
    return;
  }
  if (state.review !== rv) return;
  rv.data.meta_counts = d.meta_counts;
  rv.data.meta_by_file = d.meta_by_file;
  rv.data.other = d.other;
  setFilesCounts(countsText(d.meta_counts));
  renderFiles();
}
```

`renderFiles` in review mode calls `renderReviewFiles`. Verify with `grep -n "renderReviewFiles()" internal/web/static/files.js`.

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/web -count=1 2>&1 | tail -3`

Expected: `ok`.

- [ ] **Step 5: Commit**

Message: "web: Mark ▸ agreed / disagreed / deferred / clear in the note box menu".

---

### Task 10: Browser check (proved against the unfixed build), docs, gate

**Files:**
- Scratch: `$SCRATCH/notemeta-web/check.mjs`
- Modify: `CHANGELOG.md`, `docs/CLAUDE-details.md`, `README.md` (only if its notes paragraph names the TUI surface alone)

- [ ] **Step 1: Write the browser check**

Set up:

```bash
SCRATCH=/tmp/claude-1000/-work-gigagit/35aa0a8f-6206-45e1-8e57-18ef8f60ff68/scratchpad
mkdir -p $SCRATCH/notemeta-web && cd $SCRATCH/notemeta-web && [ -d node_modules/playwright ] || npm i playwright@1 >/dev/null
```

`check.mjs <gg>` follows the overview-split plan's check:
- fresh `XDG_STATE_HOME` and `XDG_CONFIG_HOME`
- `git init`
- spawn `gg web --addr 127.0.0.1:<port>`
- `ok(cond, msg)` exits 1 on the first failure.

The fixture is built by the gg under test, through its CLI only:
1. Commit `deep/dir/structure/that/is/long/configuration.ts`, with 12 lines changed, and `a.txt`.
2. `gg note add --rev HEAD --file deep/…/configuration.ts --new-line 12 --summary "the race" --meta kind=issue --meta severity=high --meta audience=author --meta confidence=high --display '{"kind":{"show":["badge","frame"],"style":"filled","values":{"issue":{"color":"red"}}},"severity":{"show":["badge"],"style":"text","values":{"high":{"color":"orange"}}},"audience":{"as":"audience"},"confidence":{"show":["line"]}}'`
3. A second note on `a.txt` line 1, declaring `frame` red, then make it STALE. Rewrite `a.txt` line 1 and commit, so a note on the older rev is stale in the newer diff. Alternatively, use the working tree: note on the working-tree line and then edit the line. Use whichever the CLI makes stale. Find out first with `gg note add --help` and an existing stale test (`grep -rn '"stale"' internal/web/*_test.go | head -3`).
4. A reply to note 1: `gg note reply <id> --summary "agreed, fixing" --meta kind=issue`. Check `gg note reply --help` for `--meta`.

Scenarios, each asserting `isVisible()` and geometry, never DOM text alone:
- **chips:** Open the commit (`import("/static/commits.js").openCommitByHash(sha)`), then the configuration.ts diff. Then:
  - `.notebox .mchip.mc-red.ms-filled` is visible, with text ` issue `.
  - Its computed `background-color` equals `rgb(224, 108, 117)` (#E06C75).
  - `.maud` reads `→ author`.
- **frame:** That box's `border-top-color` is `rgb(224, 108, 117)`.
- **line row:** `.notebox .mline` with text `confidence: high` is visible and sits BELOW `.notetitle` (by its rect's top) and ABOVE `.notesum`.
- **reply:** `.notereply .mrun .mchip` is visible.
- **stale dim:** The stale `a.txt` box's `border-top-color` equals the computed `--dim` colour, not red.
- **title fit:** At viewport 900×900 in the split layout, the configuration.ts note's `.notetitle` innerText contains `configuration.ts R12` and starts with ` issue `. Its `scrollWidth <= clientWidth + 1` (no clipping).
- **Mark:**
  - Right-click the note box; `#ctx-menu` is visible.
  - `elementFromPoint` at the `disagreed` button's centre is that button (the z-index memory).
  - Click it, then wait for `.notebox .mchip:has-text("disagreed")` to be visible.
  - Right-click again and click `clear`; the chip's count goes back to 0.
- **review counts:**
  - `gg review save gg://<repo>@<sha> --agent t --stdin --json` with a doc that declares `kind` (badge+count, `issue` red) and two remarks (`kind=issue`, one on a line the commit has, one on `zzz.go`).
  - Open it via `import("/static/reviews.js").openReview(id)`.
  - `#files-counts` is visible with text `2 issue`.
  - The configuration.ts / a.txt row's `.rcounts` is visible with `issue 1`.
  - The Summary's Other notes `li .mchip` is visible.
- **viewer remark:** As the split check does: the stored overview's link opens the file, and `.vnote.remark .mchip` is visible.
- **send panel:** Skipped — it needs a PR. The Go wire test (Task 1) and the wiring test (Task 8) cover it; ledger the gap.

- [ ] **Step 2: Prove it bites — run it against the UNFIXED build**

```bash
cd /work/gigagit && go build -o $SCRATCH/notemeta-web/gg-main ./cmd/gg   # main = ce036aa0, no web meta
md5sum $SCRATCH/notemeta-web/gg-main
cd $SCRATCH/notemeta-web && node check.mjs $SCRATCH/notemeta-web/gg-main
```

Expected: FAIL at `chips`, because no `.mchip` exists. Run each later scenario on its own (`node check.mjs <gg> <name>`) and record that every one fails on main's build. The CLI half of the fixture works on main (plan 1 shipped the flags).

- [ ] **Step 3: Run it against the branch build**

```bash
cd /work/gigagit/.claude/worktrees/note-meta-web && go build -o bin/gg ./cmd/gg && md5sum bin/gg
cd $SCRATCH/notemeta-web && node check.mjs /work/gigagit/.claude/worktrees/note-meta-web/bin/gg
```

Expected: `all checks passed`. Also screenshot the diff note box and the review view (`page.screenshot`), and Read the PNGs.

- [ ] **Step 4: Docs**

- **CHANGELOG.md:** add "Note meta in gg web" under the unreleased section. It lists:
  - chips, marker, frame, line and details rows on diff, viewer and remark boxes
  - review counts (header line and file rows) and Other-notes chips
  - send panel opened rows and the `.psev` colour
  - Mark in the note box menu
  - the note-edits notice
- **docs/CLAUDE-details.md:** extend "Note meta in the TUI" with a web paragraph:
  - `notemeta.js` is the one builder, and `noteRow`/candidate/other rows carry `display`
  - `/api/review/{id}` carries `meta_counts` and `meta_by_file`
  - `POST /api/notes/mark`
  - the shared notice id
  - the dark-only tokens
- **README:** update only if its notes section says the TUI alone shows meta (`grep -n "meta" README.md | head`).

- [ ] **Step 5: The gate**

```bash
cd /work/gigagit/.claude/worktrees/note-meta-web && ./test.sh race > $SCRATCH/notemeta-web/race.log 2>&1; tail -5 $SCRATCH/notemeta-web/race.log
```

Expected: the log ends with `all green`. Then commit the docs: "docs: note meta in gg web".

---

## Self-review notes

- **Spec §7 coverage:**
  - chips in `noteTitle` and `noteBoxTitle`: Tasks 5 and 6
  - border class: Tasks 4 and 5
  - line rows: Tasks 5 and 6
  - counts in the header and file rows: Task 7
  - expanded send row: Task 8
  - Mark menu: Task 9
  - CSS tokens: Task 4 (dark only; deviation stated)
  - `.psev`: Task 8
  - §10 notice: Task 3
  - spec §11 web tests (wiring plus browser): Tasks 5–10
- **Out of scope:**
  - the overview HTML export (`internal/overviewhtml` draws no note boxes)
  - the forge renderer (plan 4)
  - the TUI's deferred minors
