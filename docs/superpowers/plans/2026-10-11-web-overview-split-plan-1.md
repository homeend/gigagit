# Web overview split — plan 1 (single file, remarks, colour mark) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In gg web, every overview opens as two columns inside the viewer overlay — the overview left, the selected link's file right — with the review's remarks as read-only boxes and the right column's band marked on the overview.

**Architecture:** `view` (viewer.js) stays the ONE file the viewer shows — in the split, the right column's — so every file behaviour keeps working. A new page-local `split` holds the left column's overview; the two places an overview comes on screen (`showOverview`, `showStoredOverview`) become one door that opens the split when the box is ≥ 900px wide. The server adds the review's remarks to `/api/file-content` when asked (`review=<id>`), placed by `domain.ReviewFileNotes`.

**Tech Stack:** Go 1.26 (`internal/web`), vanilla ES modules (`internal/web/static`), node-run guarded pure JS tests, Playwright (scratch dir) for visible-behaviour checks.

**Spec:** `docs/superpowers/specs/2026-10-11-web-overview-split-design.md` (plan 2 — the stack — is a separate plan).

## Global Constraints

- W1 the split lives in the viewer overlay's box; every way in lands in it.
- W2 keys copy the TUI: Tab / shift+Tab switch columns; ↑/↓ (j/k) on the overview step links and the right follows; Enter moves right; Backspace in the right column returns left; N/P in the right column step the overview's links; a click on a link selects it and steers.
- W3 Esc closes the whole split from either column: an agent's overview → background (stays in the switcher), a stored one closes, the right file → background.
- W5 n/p in the right column move the cursor and the current band only — never the overview's selection — in the split AND in the one-document viewer.
- W6 remarks are read-only boxes (no dismiss, no copy reference); a remark on a deleted line is left out.
- W7 split only when the box is ≥ 900 CSS px wide (`SPLIT_MIN_PX = 900`).
- Web strings are English (no i18n bundles on the web).
- Hide new elements by id: every new `class="hidden"` element needs its own `#id.hidden { display: none; }` rule (memory: web hides by ID).
- Browser checks run against a FRESHLY rebuilt binary and assert VISIBILITY (`isVisible()`), never DOM contents alone.
- Never `git add -A` (a built `bin/gg` lives in the worktree); stage named files with `gg add`.
- Commit messages end with the two trailer lines:
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01XPPK9uXcnAEznruxD8saSa`.

## Review Focus

1. A quick run of ↓ on the overview (5 presses in 200ms) — only the last link's file may land in the right column, with that link's bands; no earlier file flashes in after it. (Task 4: `loadSeq` covers it; the browser check presses ↓ three times fast and asserts the last file.)
2. An agent's overview closed from the switcher (x) or `gg session overview close` while it is the split's left column — the split must collapse to the right file, bands gone, never leave a dead left column. (Task 6 browser check.)
3. Opening a plain file another way while the split is up (the switcher on a file, a pasted content link) — the split must close and the overview stay listed in the switcher. (Task 6 browser check.)
4. A remark landing for a file the reader already left (steer to a.txt, then b.txt before a.txt's answer) — a.txt's remarks must never show on b.txt. (Task 3: remarks ride the same fetch as the lines, so they share `loadSeq`; the Go test pins that remarks come with the right path.)
5. A stored overview of a WORKING review — its links open the working tree; a file changed since the review shows no remarks (the domain rule) and the split still steers. (Task 1 Go test covers the domain rule through the endpoint.)

---

## File Structure

- Modify `internal/web/filecontent.go` — `review` param → `remarks` in the body.
- Create `internal/web/filecontent_remarks_test.go` — the endpoint's tests.
- Modify `internal/web/static/viewer.js` — pure helpers (guarded sections), remarks, W5, the split.
- Modify `internal/web/static/style.css` — the two-column box, focus, `ahere`, remark box.
- Modify `internal/web/viewernotesjs_test.go` — pure JS tests.
- Create `internal/web/overviewsplitjs_test.go` — static wiring guards for the split.
- Modify `internal/web/storedoverviewjs_test.go` — keep its wiring guards true after the refactor.
- Scratch (never committed): `$CLAUDE_JOB_DIR/tmp/websplit/check.mjs` + `npm i playwright` there.
- Docs (Task 7): `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`, `internal/agentskill/gg-overview.md` (+ version), `.claude/skills/gg-overview/SKILL.md` (regenerated).

---

### Task 1: The review's remarks on `/api/file-content`

**Files:**
- Modify: `internal/web/filecontent.go` (`fileContentBody`, `noteRow`, `handleFileContent`)
- Create: `internal/web/filecontent_remarks_test.go`
- Modify: `docs/superpowers/specs/2026-10-11-web-overview-split-design.md` §6 (the endpoint becomes a parameter — one read, no ordering race)

**Interfaces:**
- Produces: `GET /api/file-content?src=&rev=&path=&review=<id>` → body gains `remarks: [{id, start, end, summary, rationale?, author, readonly: true}]` (omitted when none). An unknown review, or none placed, answers no `remarks` and the file as usual.

- [ ] **Step 1: Write the failing test**

`internal/web/filecontent_remarks_test.go`:

```go
package web

import (
	"net/http"
	"net/url"
	"testing"
)

type remarkWire struct {
	ID        string `json:"id"`
	Start     int    `json:"start"`
	End       int    `json:"end"`
	Summary   string `json:"summary"`
	Rationale string `json:"rationale"`
	Author    string `json:"author"`
	ReadOnly  bool   `json:"readonly"`
}

type contentWithRemarks struct {
	Lines   []struct{ Text string `json:"text"` } `json:"lines"`
	Remarks []remarkWire                          `json:"remarks"`
}

// Spec 2026-10-11 §6: a file a stored overview opened carries its review's
// remarks, placed on the file at the reviewed tip — new-side only, so a
// remark on a line the commit deleted is left out (W6).
func TestFileContentCarriesReviewRemarks(t *testing.T) {
	t.Parallel()
	doc := `{"version":1,"summary":"s","files":[{"path":"f.txt","annotations":[` +
		`{"newRange":[1,1],"summary":"kept","rationale":"why"},{"oldRange":[1,1],"summary":"on a deleted line"}]}]}`
	ts, _, sha, id := reviewServer(t, doc)
	var got contentWithRemarks
	q := "/api/file-content?src=commit&rev=" + sha + "&path=f.txt&review=" + url.QueryEscape(id)
	if code := getJSON(t, ts, q, &got); code != http.StatusOK {
		t.Fatalf("GET %s = %d", q, code)
	}
	if len(got.Lines) != 1 || got.Lines[0].Text != "content 2" {
		t.Fatalf("lines = %+v, want the tip's one line", got.Lines)
	}
	if len(got.Remarks) != 1 {
		t.Fatalf("remarks = %+v, want only the kept one", got.Remarks)
	}
	r := got.Remarks[0]
	if r.Summary != "kept" || r.Rationale != "why" || r.Start != 1 || r.End != 1 || r.Author != "Claude" || !r.ReadOnly || r.ID == "" {
		t.Errorf("remark = %+v", r)
	}
}

// Without review= the body is unchanged; an unknown review answers the file
// with no remarks (the viewer still shows it).
func TestFileContentRemarksNeedAKnownReview(t *testing.T) {
	t.Parallel()
	doc := `{"version":1,"summary":"s","files":[{"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"kept"}]}]}`
	ts, _, sha, _ := reviewServer(t, doc)
	for _, q := range []string{
		"/api/file-content?src=commit&rev=" + sha + "&path=f.txt",
		"/api/file-content?src=commit&rev=" + sha + "&path=f.txt&review=nope",
	} {
		var got contentWithRemarks
		if code := getJSON(t, ts, q, &got); code != http.StatusOK {
			t.Fatalf("GET %s = %d", q, code)
		}
		if len(got.Lines) != 1 || len(got.Remarks) != 0 {
			t.Errorf("GET %s = %+v, want the file and no remarks", q, got)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd internal/web && go test -run 'TestFileContent(CarriesReviewRemarks|RemarksNeedAKnownReview)' -count=1 .`
Expected: FAIL — `remarks = [], want only the kept one` (the second test passes already; that is fine — it pins the no-param shape).

- [ ] **Step 3: Implement**

In `filecontent.go`, add to `fileContentBody` after `Notes`:

```go
	// Remarks are a review's remarks on the file (review=<id>: a file a
	// stored overview opened), placed on these very bytes — read-only boxes
	// (spec 2026-10-11 §6).
	Remarks []noteRow `json:"remarks,omitempty"`
```

Add to `noteRow` after `Ref`:

```go
	ReadOnly  bool   `json:"readonly,omitempty"` // a review's remark: no dismiss, no reference
```

In `handleFileContent`, after `path, src, rev := ...` add `review := q.Get("review")`, and replace the tail

```go
	if src == "" || src == "worktree" {
		body.Notes = s.notesFor(ctx, path, data)
	}
	writeJSON(w, body)
```

with

```go
	if src == "" || src == "worktree" {
		body.Notes = s.notesFor(ctx, path, data)
	}
	if review != "" {
		body.Remarks = remarksFor(ctx, svc, review, path, data)
	}
	writeJSON(w, body)
```

and add below `notesFor`:

```go
// remarksFor is review id's remarks on path placed on data — the file at the
// reviewed tip, or the working tree for a working review — for the page's
// read-only boxes (spec 2026-10-11 §6). A remark on a line the commit
// deleted has no line here and is left out (domain.ReviewFileNotes). An
// unknown review, or one that places none, has none: the file still shows.
// The id only ever matches a stored review's id — it never reaches a git argv.
func remarksFor(ctx context.Context, svc *domain.Service, id, path string, data []byte) []noteRow {
	ns, err := svc.ReviewFileNotes(ctx, id, path, agentdocs.Lines(data))
	if err != nil || len(ns) == 0 {
		return nil
	}
	out := make([]noteRow, len(ns))
	for i, n := range ns {
		out[i] = noteRow{ID: n.Note.ID, Start: n.Range[0], End: max(n.Range[1], n.Range[0]), Summary: n.Note.Summary,
			Rationale: n.Note.Rationale, Author: n.Note.Author, ReadOnly: true}
	}
	return out
}
```

(`domain` is already imported by `filecontent.go` — `domain.MaxDiffBytes`; check the import of `context`, add it if missing.)

In the spec §6 "Server." paragraph, replace the endpoint sentence with: "`GET /api/file-content` takes `review=<id>`: the server places that review's remarks on the bytes it just read (`domain.ReviewFileNotes`) and answers them as `remarks` (the viewer's note wire plus `readonly: true`) beside the lines — one read, so a late answer can never pair one file's remarks with another's lines. An unknown review answers no remarks." In §13 Go tests, replace "a gone review is 404, a bad path is refused" with "an unknown review answers the file without remarks".

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd internal/web && go test -run 'TestFileContent' -count=1 .`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
gg add internal/web/filecontent.go internal/web/filecontent_remarks_test.go docs/superpowers/specs/2026-10-11-web-overview-split-design.md
git commit -F <msgfile>   # "web: /api/file-content carries a review's remarks (review=<id>)" + trailers
```

---

### Task 2: Pure helpers — the mark, the breakpoint, merged notes, the remark title

**Files:**
- Modify: `internal/web/static/viewer.js` (note model + overview model guarded sections)
- Modify: `internal/web/viewernotesjs_test.go`

**Interfaces:**
- Produces (overview model): `SPLIT_MIN_PX = 900`, `splitFits(boxWidth) → bool`, `hereOf(bands, curBand, line, selBand) → anchor index | -1`, `remarksReview(from) → review id | ""`.
- Produces (note model): `mergeNotes(notes, remarks) → notes[]` (by start line, an agent's note first on a tie); `noteBoxTitle(n)` titles a remark (`n.readonly`) `review · <author> · line N` / `lines a–b`.

- [ ] **Step 1: Write the failing tests**

Append to `viewernotesjs_test.go`:

```go
// Spec 2026-10-11 §7 / W7 / §6: the mark the right column's cursor puts on
// the overview, the split's breakpoint, the review a file's remarks come from.
func TestViewerSplitModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", voPureStart, voPureEnd, `
const bands = [{start: 2, end: 4, i: 0}, {start: 3, end: 6, i: 2}, {start: 9, end: 9, i: 5}];
console.log([
  hereOf(bands, -1, 3, -1),  // overlap, no current band: the first
  hereOf(bands, 1, 3, -1),   // overlap: the current band wins
  hereOf(bands, -1, 5, -1),  // only the second covers 5
  hereOf(bands, -1, 7, -1),  // none covers 7
  hereOf(bands, -1, 9, 2),   // band 2 is the selection's: no second mark
  hereOf([], -1, 1, -1),
  splitFits(899), splitFits(900), SPLIT_MIN_PX,
  remarksReview({stored: {id: "review-overview:abc"}}),
  remarksReview({stored: {id: "review-overview:abc"}, closed: true}),
  remarksReview({id: "f3"}), remarksReview(null),
].join("|"));
`)
	want := `0|2|2|-1|-1|-1|false|true|900|abc|||`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// W6: a remark's box names the review, never its internal id; the boxes a
// file draws are the agent's notes then the remarks, by start line.
func TestViewerRemarkNotesJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", vnPureStart, vnPureEnd, `
const ns = [{id: "t1", start: 4, end: 4, author: "a"}];
const rs = [{id: "review:x:0", start: 2, end: 3, author: "Claude", readonly: true}, {id: "review:x:1", start: 4, end: 4, author: "Claude", readonly: true}];
console.log([
  noteBoxTitle(rs[0]), noteBoxTitle(rs[1]),
  mergeNotes(ns, rs).map((n) => n.id).join(","),
  mergeNotes(ns, []) === ns, mergeNotes(ns, undefined) === ns,
].join("|"));
`)
	want := `review · Claude · lines 2–3|review · Claude · line 4|review:x:0,t1,review:x:1|true|true`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd internal/web && go test -run 'TestViewer(SplitModel|RemarkNotes)JS' -count=1 .`
Expected: FAIL — node `ReferenceError: hereOf is not defined` / `mergeNotes is not defined`.

- [ ] **Step 3: Implement**

In `viewer.js`, note model: replace `noteBoxTitle` with

```js
function noteBoxTitle(n) {
  const at = n.start === n.end ? "line " + n.start : "lines " + n.start + "–" + n.end;
  if (n.readonly) return "review · " + n.author + " · " + at; // a review's remark: its id is internal
  return "note " + n.id + " · " + n.author + " · " + at + (n.outdated ? " · outdated" : "");
}
```

and add before `// --- end note model ---`:

```js
// mergeNotes is every box a file draws: an agent's notes, then a review's
// remarks, by start line — an agent's note first on a tie (sort is stable).
function mergeNotes(notes, remarks) {
  if (!remarks || !remarks.length) return notes;
  return [...notes, ...remarks].sort((a, b) => a.start - b.start);
}
```

Overview model: add before `// --- end overview model ---`:

```js
// SPLIT_MIN_PX is the box width the split needs (W7): about 60 characters a
// column at the viewer's 12px monospace; a narrower box shows one document.
const SPLIT_MIN_PX = 900;
function splitFits(boxWidth) {
  return boxWidth >= SPLIT_MIN_PX;
}

// hereOf is the anchor the right column's cursor marks on the overview
// (spec §7): the band covering line — the current band (cur) wins an
// overlap, then the first — as its anchor index; -1 when no band covers it
// or it is the selection's band (selBand: the selection is already marked).
function hereOf(bands, cur, line, selBand) {
  const covers = (b) => b.start <= line && line <= b.end;
  const k = cur >= 0 && cur < bands.length && covers(bands[cur]) ? cur : bands.findIndex(covers);
  return k < 0 || k === selBand ? -1 : bands[k].i;
}

// remarksReview is the review whose remarks a file opened from overview
// way back from carries: a stored overview's review; "" otherwise.
function remarksReview(from) {
  return from && from.stored && !from.closed ? String(from.stored.id).replace(/^review-overview:/, "") : "";
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd internal/web && go test -run 'TestViewer.*JS' -count=1 .`
Expected: PASS (the existing `TestViewerOverviewModelJS` / notes tests too).

- [ ] **Step 5: Commit** — `gg add internal/web/static/viewer.js internal/web/viewernotesjs_test.go`; message "web: pure helpers for the overview split (mark, breakpoint, remarks)" + trailers.

---

### Task 3: Remarks in the file, and n/p only mark (W5, W6) — the one-document viewer

**Files:**
- Modify: `internal/web/static/viewer.js` (`fetchContent`, `openViewer`, `openAnchorAt`, `renderViewer`, `noteBoxHTML`, `stepNote`, `fileKey` d/r, foot, `viewerFileChanged`, `stepViewerAnchor`, `view` initialiser)
- Modify: `internal/web/static/style.css` (a remark box)
- Create: `internal/web/overviewsplitjs_test.go` (static wiring guards)
- Scratch: `$CLAUDE_JOB_DIR/tmp/websplit/check.mjs`

**Interfaces:**
- Consumes: Task 1's `remarks` wire; Task 2's `mergeNotes`, `remarksReview`, `noteBoxTitle`.
- Produces: `openViewer({src, rev, path, line, id, from, steer})` — `from` is set on `view` BEFORE the first render and its stored review's remarks ride the content fetch; `view.remarks`; `shownNotes()`.

- [ ] **Step 1: Set up the browser check (scratch, never committed)**

```bash
mkdir -p $CLAUDE_JOB_DIR/tmp/websplit && cd $CLAUDE_JOB_DIR/tmp/websplit && npm i playwright@1 >/dev/null
```

Write `$CLAUDE_JOB_DIR/tmp/websplit/check.mjs`:

```js
// check.mjs <gg> [scenario] — a fixture repo, a fresh gg web, assertions on
// what is VISIBLE. Exit 1 on the first failed assertion.
import { chromium } from "playwright";
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const gg = process.argv[2], only = process.argv[3] || "";
const root = mkdtempSync(join(process.env.CLAUDE_JOB_DIR + "/tmp", "websplit-"));
const repo = join(root, "repo");
const env = { ...process.env, XDG_STATE_HOME: join(root, "state"), XDG_CONFIG_HOME: join(root, "config"),
  GIT_AUTHOR_NAME: "t", GIT_AUTHOR_EMAIL: "t@t", GIT_COMMITTER_NAME: "t", GIT_COMMITTER_EMAIL: "t@t" };
const git = (...a) => execFileSync("git", ["-C", repo, ...a], { env }).toString().trim();
const ggRun = (args, input) => execFileSync(gg, args, { cwd: repo, env, input }).toString();
execFileSync("git", ["init", "-q", "-b", "main", repo], { env });
writeFileSync(join(repo, "a.txt"), "old\none\ntwo\nthree\nfour\nfive\n");
writeFileSync(join(repo, "b.txt"), "alpha\nbeta\ngamma\n");
git("add", "-A"); git("commit", "-qm", "seed");
writeFileSync(join(repo, "a.txt"), "one\ntwo\nthree\nfour\nfive\n"); // the commit deletes "old"
writeFileSync(join(repo, "b.txt"), "alpha\nbeta\ngamma\ndelta\n");
git("commit", "-qam", "change");
const sha = git("rev-parse", "HEAD");
const doc = { version: 1, summary: "## Summary\nfine",
  overview: "Start at [line two](a.txt:2), then [four](a.txt:4-5), then [beta](b.txt:2).",
  files: [{ path: "a.txt", annotations: [{ newRange: [2, 2], summary: "why two?" }, { oldRange: [1, 1], summary: "why drop old?" }] },
          { path: "b.txt", annotations: [{ newRange: [2, 2], summary: "beta?" }] }] };
const reviewId = JSON.parse(ggRun(["review", "save", `gg://${repo}@${sha}`, "--agent", "Claude Code", "--stdin", "--json"], JSON.stringify(doc))).id;

const port = 20000 + Math.floor(Math.random() * 20000);
const srv = spawn(gg, ["web", "--addr", "127.0.0.1:" + port], { cwd: repo, env, stdio: "ignore" });
const fail = (m) => { console.error("FAIL: " + m); srv.kill(); process.exit(1); };
const ok = (c, m) => { if (!c) fail(m); console.log("ok   " + m); };
await new Promise((r) => setTimeout(r, 1500));
const browser = await chromium.launch();

// page opens the app at width×900 and waits for it.
async function page(width) {
  const p = await browser.newPage({ viewport: { width, height: 900 } });
  await p.goto(`http://127.0.0.1:${port}/`);
  await p.waitForSelector("#foot");
  return p;
}
// openStored opens the review's stored overview as the ≡ Overview row does.
async function openStored(p) {
  await p.evaluate(async (id) => {
    const m = await import("/static/reviews.js");
    await m.openReview(id);
    m.openReviewOverview();
  }, reviewId);
  await p.waitForSelector("#viewer:not(.hidden)");
}
const vis = (p, sel) => p.locator(sel).first().isVisible();
const text = (p, sel) => p.locator(sel).first().innerText();
const scenarios = {};

// --- Task 3: remarks + W5 in the one-document viewer -------------------------
scenarios.narrowRemarks = async () => {
  const p = await page(800);
  await openStored(p);
  ok(await vis(p, "#viewer-body .vdoc"), "narrow: the overview shows as one document");
  await p.keyboard.press("Enter"); // the first link: a.txt:2
  await p.waitForSelector("#viewer-body .vline");
  ok(await vis(p, '#viewer-body .vnote:has-text("why two?")'), "narrow: the remark on a kept line is visible");
  ok(!(await p.locator('#viewer-body .vnote:has-text("why drop old?")').count()), "narrow: the remark on a deleted line is absent");
  ok(!(await p.locator('#viewer-body .vnote:has-text("why two?") button').count()), "narrow: the remark box has no buttons");
  ok((await text(p, '#viewer-body .vnote:has-text("why two?") .vnote-title')).startsWith("review · Claude Code · line 2"), "narrow: the remark box is titled by the review");
  await p.keyboard.press("d");
  ok((await text(p, "#op-text")).includes("review remarks are read-only"), "narrow: d on a remark says read-only");
  await p.keyboard.press("n"); // to the a.txt:4-5 band
  await p.keyboard.press("Backspace");
  await p.waitForSelector("#viewer-body .vdoc");
  ok(await vis(p, "#viewer-body a.md-anchor.asel >> text=line two"), "narrow W5: back returns to the link opened, not the band stepped to");
  await p.close();
};

for (const [name, fn] of Object.entries(scenarios)) if (!only || only === name) await fn();
await browser.close();
srv.kill();
console.log("all checks passed");
```

- [ ] **Step 2: Write the failing wiring test**

`internal/web/overviewsplitjs_test.go`:

```go
package web

import (
	"strings"
	"testing"
)

// Spec 2026-10-11 §6 / W5 / W6: a file an overview opened carries its from
// before its first render, its stored review's remarks ride the content
// read, every note reader sees the remarks, and n/p never move the
// overview's selection.
func TestViewerRemarksAndMarkOnlyBandsAreWired(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{
		`function fetchContent(src, rev, path, review = "")`,
		`"&review=" + encodeURIComponent(review)`,
		`async function openViewer({ src = "worktree", rev = "", path = "", line = 0, id = "", from = null, steer = false })`,
		`fetchContent(reg.file.source, reg.file.rev || "", reg.file.path, remarksReview(from))`,
		`view.remarks = body.remarks || [];`,
		`function shownNotes()`,
		`boxesAfter(shownNotes(), i)`,
		`noteAtLine(shownNotes(), i + 1)`,
		`nextNoteLine(shownNotes(), view.cur, dir)`,
		`"review remarks are read-only"`,
		`f.cur = a.dest; // W5: the current band only`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	for _, gone := range []string{`f.cur = f.dest = a.dest;`, "  f.sel = b.i;\n"} {
		if strings.Contains(v, gone) {
			t.Errorf("viewer.js still has %q (W5: n/p move no selection)", gone)
		}
	}
}
```

- [ ] **Step 3: Run both to verify they fail**

Run: `cd internal/web && go test -run TestViewerRemarksAndMarkOnlyBandsAreWired -count=1 .` → FAIL (lacks …).
Run: `go build -o bin/gg ./cmd/gg && node $CLAUDE_JOB_DIR/tmp/websplit/check.mjs $PWD/bin/gg narrowRemarks` (from the worktree root) → `FAIL: narrow: the remark on a kept line is visible`.

- [ ] **Step 4: Implement**

In `viewer.js`:

1. `view` initialiser: add `remarks: []` (after `notes: []`).
2. `fetchContent`:

```js
function fetchContent(src, rev, path, review = "") {
  return getJSON("/api/file-content?src=" + encodeURIComponent(src) + "&rev=" + encodeURIComponent(rev) + "&path=" + encodeURIComponent(path) +
    (review ? "&review=" + encodeURIComponent(review) : ""));
}
```

(the guard string `"&review=" + encodeURIComponent(review)` is in it.)

3. `openViewer` signature → `async function openViewer({ src = "worktree", rev = "", path = "", line = 0, id = "", from = null, steer = false })` with a doc line above it: "from is the way back an overview's link opened this file with (set before its first render: its bands and its stored review's remarks); steer marks the split's own steering (Task 4)." The content read becomes `body = await fetchContent(reg.file.source, reg.file.rev || "", reg.file.path, remarksReview(from));`. Move `view.from = view.range = null; view.rangeOwn = false;` BELOW `if (ov) return showOverview(f, ov, ovMine);` and make it

```js
  view.from = from; // an overview's link: its bands and remarks draw at once
  view.range = null;
  view.rangeOwn = false;
```

and after `view.notes = notesOf(body, view.src);` add `view.remarks = body.remarks || [];`.
In `showOverview` (document mode) add `view.from = view.range = null; view.rangeOwn = false; view.remarks = [];` to its `Object.assign`-adjacent lines (it relied on openViewer clearing them). In `showStoredOverview` add `remarks: []` to its `Object.assign`.

4. `openAnchorAt`: pass the way back into the open and drop the after-the-fact assignment. Stored branch:

```js
    const r = await openViewer({ ...storedAnchorOpen(stored.tip, a), from: back });
    if (!r.ok) return;
    armBack();
    return;
```

Agent branch:

```js
  const r = await openViewer({ src: "worktree", path: t.path, line: t.line, from });
  if (!r.ok) return;
  armBack();
  if (t.note) $("viewer-body").querySelector(`.vnote[data-note="${t.note}"]`)?.scrollIntoView({ block: "nearest" });
```

(keep the existing note scroll line where it was; remove `view.from = …`, `rerenderKeepingScroll()`, `swapFoot(true)` after the opens — openViewer now renders and paints the foot with `from` set.)

5. `shownNotes` (next to `notesOf`):

```js
// shownNotes is every box the file draws: the agent's notes, then the
// review's remarks (spec §6). Code that talks to the store keeps view.notes.
function shownNotes() {
  return mergeNotes(view.notes, view.remarks);
}
```

6. `noteBoxHTML`: a remark has no actions:

```js
    (n.readonly ? "" : `<div class="vnote-acts"><button data-nact="dismiss">dismiss</button><button data-nact="ref">copy reference</button></div>`) +
```

(and add ` remark` to the class when `n.readonly`: `` `<div class="vnote${n.outdated ? " outdated" : ""}${n.readonly ? " remark" : ""}" …` ``).

7. `renderViewer`: `noteAtLine(view.notes, i + 1)` → `noteAtLine(shownNotes(), i + 1)`; `boxesAfter(view.notes, i)` → `boxesAfter(shownNotes(), i)`.
8. `stepNote`: `nextNoteLine(view.notes, view.cur, dir)` → `nextNoteLine(shownNotes(), view.cur, dir)`; inside it `noteAtLine(view.notes, line)` → `noteAtLine(shownNotes(), line)`; the next-file landing `const t = dir > 0 ? view.notes[0] : …` → `const ns = shownNotes(); const t = dir > 0 ? ns[0] : ns[ns.length - 1];`.
9. Keys `d` / `r` in the file switch:

```js
    case "d": { const n = noteAtLine(shownNotes(), view.cur); if (!n) return false; if (n.readonly) opLine("review remarks are read-only", false); else dismissNote(n.id); break; }
    case "r": { const n = noteAtLine(shownNotes(), view.cur); if (!n) return false; if (n.readonly) opLine("review remarks are read-only", false); else copyNoteRef(n); break; }
```

and the same `n.readonly` refusal in the foot's `dismiss` / `ref` handlers (they read `noteAtLine(shownNotes(), view.cur)`).
10. `viewerFoot` (file branch): `} { notes` shows when `shownNotes().length`; `d dismiss` / `r reference` only when `view.notes.length` (an agent's notes):

```js
  const notes = (shownNotes().length ? `<span>} { notes</span>` : "") +
    (view.notes.length ? `<button data-vact="dismiss">d dismiss</button><button data-vact="ref">r reference</button>` : "");
```

11. `viewerFileChanged`: the read becomes `body = await fetchContent(view.src, view.rev, view.path, remarksReview(view.from));` and after the notes line add `view.remarks = body.missing ? view.remarks : body.remarks || [];`.
12. `stepViewerAnchor` (W5):

```js
  const b = bands[r.i], a = f.anchors[b.i];
  f.cur = a.dest; // W5: the current band only — the overview's selection (f.sel / f.dest) stays
```

(delete `f.sel = b.i;`).

In `style.css` after `.vnote-why`:

```css
/* a review's remark (spec 2026-10-11 §6): read-only, the review's colour */
.vnote.remark { border-color: var(--dim); }
.vnote.remark .vnote-title { color: var(--dim); }
```

- [ ] **Step 5: Run to verify they pass**

Run: `cd internal/web && go test -run 'TestViewer|TestStoredOverview' -count=1 .` → PASS.
Run: `go build -o bin/gg ./cmd/gg && node $CLAUDE_JOB_DIR/tmp/websplit/check.mjs $PWD/bin/gg narrowRemarks` → every `ok`, `all checks passed`.

- [ ] **Step 6: Commit** — `gg add internal/web/static/viewer.js internal/web/static/style.css internal/web/overviewsplitjs_test.go`; message "web: a review's remarks as read-only boxes; n/p only move the band (W5)" + trailers.

---

### Task 4: The split — layout, the door, steering, the overview's keys

**Files:**
- Modify: `internal/web/static/viewer.js`
- Modify: `internal/web/static/style.css`
- Modify: `internal/web/overviewsplitjs_test.go`
- Modify: `internal/web/storedoverviewjs_test.go` (only if a guarded string moved)
- Scratch: `check.mjs` (scenario `splitStored`)

**Interfaces:**
- Consumes: Task 3's `openViewer({…, from, steer})`; Task 2's `splitFits`, `stepAnchor`.
- Produces: `let split` (`{id, path, ov, left, here, seq}` | null), `ovDoc()`, `ovEl()`, `boxWidth()`, `openSplit(id, path, ov, { steer = true } = {})`, `steerSplit()`, `focusSplit(left)`, `leaveSplit()`, `paintSplitFocus()`, `raiseViewer()`.

- [ ] **Step 1: Write the failing checks**

Add to `check.mjs` (before the runner loop):

```js
// --- Task 4: the split -----------------------------------------------------
scenarios.splitStored = async () => {
  const p = await page(1400);
  await openStored(p);
  ok(await vis(p, "#viewer-ov-body .vdoc"), "split: the overview is visible in the left column");
  ok(await vis(p, "#viewer-body .vline"), "split: a file is visible in the right column");
  ok((await text(p, "#viewer-title")).includes("a.txt"), "split: the right column shows the first link's file");
  ok(await vis(p, "#viewer-ov-body a.md-anchor.asel >> text=line two"), "split: the first link is selected");
  ok(await vis(p, '#viewer-body .vnote:has-text("why two?")'), "split: the remark shows on the right");
  await p.keyboard.press("ArrowDown"); await p.keyboard.press("ArrowDown"); // fast: only the last lands
  await p.waitForFunction(() => document.querySelector("#viewer-title").innerText.includes("b.txt"));
  ok(await vis(p, "#viewer-ov-body a.md-anchor.asel >> text=beta"), "split: ↓↓ selects the third link");
  ok(await vis(p, '#viewer-body .vnote:has-text("beta?")'), "split: b.txt's remark shows, not a.txt's");
  ok(!(await p.locator('#viewer-body .vnote:has-text("why two?")').count()), "split: a.txt's remark is gone");
  ok(await p.evaluate(() => document.activeElement.id === "viewer-ov-body"), "split: the keys stay on the overview");
  await p.keyboard.press("Tab");
  ok(await p.evaluate(() => document.activeElement.id === "viewer-body"), "split: tab moves into the file");
  await p.keyboard.press("Escape");
  ok(!(await vis(p, "#viewer")), "split: esc closes the whole split");
  await p.close();
};
```

Add to `overviewsplitjs_test.go`:

```go
// Spec 2026-10-11 §3–§5: the box has a left column, the two overview doors
// open the split when the box is wide enough, and the split steers the right
// column through openViewer.
func TestOverviewSplitIsWired(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{
		`<div id="viewer-ov" class="hidden"><div id="viewer-ov-title"></div><div id="viewer-ov-body" tabindex="-1"></div></div>`,
		`<div id="viewer-file">`,
		`let split = null;`,
		`function openSplit(id, path, ov, { steer = true } = {})`,
		`if (splitFits(boxWidth())) return openSplit(f.id, f.path, `,
		`if (splitFits(boxWidth())) return openSplit("", "", `,
		`await openViewer({ ...target, from, steer: true })`,
		`if (split) return splitKey(e);`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	css := readStatic(t, "style.css")
	for _, want := range []string{`#viewer-ov.hidden { display: none; }`, `#viewer-file {`, `.md a.md-anchor.ahere`} {
		if !strings.Contains(css, want) {
			t.Errorf("style.css lacks %q", want)
		}
	}
}
```

- [ ] **Step 2: Run to verify they fail**

`cd internal/web && go test -run TestOverviewSplitIsWired -count=1 .` → FAIL. Rebuild, `node check.mjs bin/gg splitStored` → `FAIL: split: the overview is visible in the left column`.

- [ ] **Step 3: Implement the layout**

`viewerRoot.innerHTML` becomes:

```js
viewerRoot.innerHTML =
  `<div id="viewer-box">` +
  `<div id="viewer-ov" class="hidden"><div id="viewer-ov-title"></div><div id="viewer-ov-body" tabindex="-1"></div></div>` +
  `<div id="viewer-file"><div id="viewer-title"></div>` +
  `<div id="viewer-search" class="hidden search-bar"><span id="viewer-search-lead">/</span>` +
  `<input id="viewer-search-input" type="text" autocomplete="off" spellcheck="false" placeholder="find in this file — enter keeps it, ] [ step, esc clears">` +
  `<span id="viewer-search-count"></span></div>` +
  `<div id="viewer-body" tabindex="-1"></div></div></div>`;
```

`style.css`: change `#viewer-box`'s `flex-direction: column` to `flex-direction: row`, and add after the `#viewer-body` rule:

```css
/* The overview split (spec 2026-10-11 §3): the overview in a left column,
   the file in the right; equal halves, the focused column's title underlined. */
#viewer-file { flex: 1 1 0; min-width: 0; display: flex; flex-direction: column; }
#viewer-ov { flex: 1 1 0; min-width: 0; display: flex; flex-direction: column; border-right: 1px solid var(--border); }
#viewer-ov.hidden { display: none; }
#viewer-ov-title { padding: 10px 14px; border-bottom: 1px solid var(--border); white-space: nowrap; overflow: hidden; }
#viewer-ov-body { flex: 1; overflow: auto; padding: 6px 0; }
#viewer-ov-body:focus { outline: none; }
#viewer-box.split #viewer-ov-title, #viewer-box.split #viewer-title { color: var(--dim); }
#viewer-box.split .colfocus > #viewer-ov-title, #viewer-box.split .colfocus > #viewer-title { color: var(--fg); box-shadow: inset 0 -2px 0 var(--accent); }
/* the link whose band holds the right column's cursor (spec §7): the bands' amber */
.md a.md-anchor.ahere { background: color-mix(in srgb, #d4a72c 24%, transparent); font-weight: bold; text-decoration: underline; }
```

- [ ] **Step 4: Implement the state, door and steering**

Below the `view` declaration:

```js
// split is the overview beside the file (spec 2026-10-11 §4; null = none):
// id its open-files id ("" for a stored overview), path its entry's path, ov
// its document (view.ov's shape: {title, text, blocks, anchors, sel, want,
// stamp, stored?}), left whether the overview has the keys, here the anchor
// the right column's cursor marks (-1 = none), seq its refreshes' order.
let split = null;

// ovDoc is the overview the overview keys and paint act on: the split's left
// column, else document mode's; ovEl the element it is drawn in.
function ovDoc() {
  return split ? split.ov : view.ov;
}
function ovEl() {
  return split ? $("viewer-ov-body") : $("viewer-body");
}

// boxWidth is the viewer box's width — measured while it is up, else the
// width it will open at (94vw).
function boxWidth() {
  return isOpen() ? $("viewer-box").clientWidth : Math.round(window.innerWidth * 0.94);
}

// raiseViewer puts the viewer's layer and chips up (idempotent).
function raiseViewer() {
  viewerRoot.style.bottom = $("foot").offsetHeight + "px"; // the bar stays in sight
  pushLayer("viewer", viewerRoot, { onKey: viewerKey });
  swapFoot(true);
}
```

`paintAnchors`, `anchorEl`, `selectAnchor`, `copyAnchorRef`, `overviewKey` read `ovDoc()` / `ovEl()` instead of `view.ov` / `$("viewer-body")`:

```js
function paintAnchors() {
  const ov = ovDoc();
  for (const el of ovEl().querySelectorAll("a.md-anchor")) {
    const i = Number(el.dataset.a), a = ov.anchors[i];
    el.classList.toggle("asel", i === ov.sel);
    el.classList.toggle("agone", !!(a && a.missing));
    el.classList.toggle("ahere", !!split && i === split.here);
  }
}

function anchorEl(i) {
  return ovEl().querySelector(`a.md-anchor[data-a="${i}"]`);
}

function selectAnchor(i) {
  const ov = ovDoc();
  ov.sel = i;
  ov.want = ov.anchors[i]?.dest || ""; // kept while a refresh drops it, back when it returns
  paintAnchors();
  anchorEl(i)?.scrollIntoView({ block: "nearest" });
}
```

(`copyAnchorRef`: `const ov = ovDoc(); if (ov.stored) …; const a = ov.anchors[ov.sel];`.)

The left column's paint:

```js
// ovTitle is an overview's title line: a stored one by its title, an
// agent's by its id and title.
function ovTitle(ov, id) {
  return ov.stored ? "Overview: " + ov.title : "Overview " + id + " · " + ov.title;
}

function paintSplitTitle() {
  const el = $("viewer-ov-title");
  el.title = split.ov.title;
  el.textContent = ovTitle(split.ov, split.id);
}

function renderSplitOverview() {
  const body = $("viewer-ov-body"), top = body.scrollTop;
  body.innerHTML = `<div class="vdoc md">${mdHTML({ blocks: split.ov.blocks }, esc, { anchors: true })}</div>`;
  body.scrollTop = top;
  paintAnchors();
}

// paintSplitFocus shows the split's columns and puts the keys in the
// focused one; without a split the box is today's one document.
function paintSplitFocus() {
  $("viewer-box").classList.toggle("split", !!split);
  $("viewer-ov").classList.toggle("hidden", !split);
  $("viewer-ov").classList.toggle("colfocus", !!split && split.left);
  $("viewer-file").classList.toggle("colfocus", !!split && !split.left);
  if (!split) return;
  (split.left ? $("viewer-ov-body") : $("viewer-body")).focus({ preventScroll: true });
  swapFoot(true);
}

function focusSplit(left) {
  if (!split) return;
  split.left = left;
  paintSplitFocus();
}

// leaveSplit drops the left column (the viewer stays up on the right file).
function leaveSplit() {
  split = null;
  paintSplitFocus();
}
```

`paintTitle`: use `ovTitle(view.ov, view.id)` in its overview branch, and with no file (`!view.id && !view.ov`) set `el.textContent = ""` and return.

The door and steering:

```js
// openSplit puts overview ov in the left column (spec §5) — id its
// open-files id ("" = a stored overview, ov.stored set). A split already
// showing it keeps its place and selection (the new document's anchors
// replace the old, the selection kept by destination); a new one selects the
// first link that can open when none is selected and, unless told not to,
// steers the right column to it.
function openSplit(id, path, ov, { steer = true } = {}) {
  const same = !!split && (id ? split.id === id : !!split.ov.stored && !!ov.stored && split.ov.stored.id === ov.stored.id);
  if (same) {
    split.ov = { ...ov, sel: keepAnchor(ov.anchors || [], split.ov.want, split.ov.sel), want: split.ov.want, stored: ov.stored || split.ov.stored };
    split.left = true;
  } else {
    if (view.ov) Object.assign(view, { id: "", ov: null, src: "worktree", rev: "", path: "", lines: [], notes: [], remarks: [], cur: 0, placeholder: "", image: null, from: null, range: null, rangeOwn: false });
    split = { id, path, ov: { ...ov, sel: ov.sel >= 0 ? ov.sel : -1, want: ov.want || "" }, left: true, here: -1, seq: 0 };
    if (split.ov.sel < 0) split.ov.sel = stepAnchor(split.ov.anchors, -1, 1);
  }
  raiseViewer();
  paintSplitTitle();
  renderSplitOverview();
  paintSplitFocus();
  if (split.ov.sel >= 0) selectAnchor(split.ov.sel);
  if (!view.id) {
    view.placeholder = split.ov.sel >= 0 ? "" : "(no link in this overview opens a file)";
    paintTitle();
    renderViewer();
  }
  if (!same && steer) steerSplit();
  return { ok: true, notice: "" };
}

// steerSplit shows the selected link's target in the right column (spec §5):
// the file at the link's line with the overview's links as bands (a stored
// overview: at the reviewed tip, with its remarks), or a note's file at the
// note. A link that cannot open says why and leaves the right column as it
// was. The focus stays where it is; no history entry. Quick steps settle on
// the last one (openViewer's loadSeq).
async function steerSplit() {
  const sp = split;
  const a = sp && sp.ov.anchors[sp.ov.sel];
  if (!a) return;
  const why = anchorStatus(a);
  if (why) return opLine(why, false);
  let target;
  if (sp.ov.stored) target = storedAnchorOpen(sp.ov.stored.tip, a);
  else {
    const t = anchorTarget(a);
    if (!t.path) return opLine("note " + t.note + " is gone", false);
    target = { src: "worktree", path: t.path, line: t.line };
  }
  const from = { id: sp.id || sp.ov.stored.id, sel: sp.ov.sel, dest: a.dest, anchors: sp.ov.anchors, stamp: sp.ov.stamp, cur: a.note ? "" : a.dest };
  if (sp.ov.stored) from.stored = sp.ov.stored;
  const r = await openViewer({ ...target, from, steer: true });
  if (r.ok && a.note) $("viewer-body").querySelector(`.vnote[data-note="${a.note}"]`)?.scrollIntoView({ block: "nearest" });
}
```

The two doors — `showOverview` gains at its top (after `ovApplied = Math.max(ovApplied, mine);`, which moves up from the body):

```js
  if (splitFits(boxWidth())) return openSplit(f.id, f.path, { ...ov, sel: -1, want: "" });
```

and `showStoredOverview` at its top:

```js
  if (splitFits(boxWidth())) return openSplit("", "", { title: doc.title, blocks: doc.blocks, anchors: doc.anchors, text: doc.text, sel: -1, want: "", stored: doc });
```

`openViewer` in the split: its plain-file landing must not take the keys from the overview, and must leave the split when it is not the split's own steering (spec §10, the switcher / a navigate):

- just before `view.ov = null;` add:

```js
  if (split && !steer) { // another way of opening a plain file: the split steps aside (as esc)
    if (split.id) ofQueue({ op: "background", id: split.id });
    leaveSplit();
  }
```

- replace `pushLayer(...)` / `swapFoot(true)` / `viewerRoot.style.bottom = …` in openViewer with `raiseViewer();` and the focus line with `if (!split || !split.left) $("viewer-body").focus({ preventScroll: true });`.

- [ ] **Step 5: Implement the overview keys and closing**

`viewerKey`: after `if (e.ctrlKey || e.metaKey || e.altKey) return false;` add `if (split) return splitKey(e);`. Move the remainder of `viewerKey` (from the shift+↓/↑ check to the end of the switch) into a new `function fileKey(e)` (same body, the `Escape` case unchanged) and end `viewerKey` with `if (view.ov) return overviewKey(e);` / `return fileKey(e);`.

```js
// splitKey is a key in the split (W2): tab switches columns; the overview
// steps its links and steers; the file keeps every file key.
function splitKey(e) {
  if (e.key === "Tab") {
    e.preventDefault();
    focusSplit(!split.left);
    return true;
  }
  if (split.left) return splitOverviewKey(e);
  return fileKey(e);
}

// splitOverviewKey is a key on the left column (spec §8).
function splitOverviewKey(e) {
  const body = $("viewer-ov-body");
  switch (e.key) {
    case "ArrowDown": case "j": stepSplitAnchor(1); break;
    case "ArrowUp": case "k": stepSplitAnchor(-1); break;
    case "Enter": enterSplitFile(); break;
    case "r": copyAnchorRef(); break;
    case "y": copyText(split.ov.text, "overview text"); break;
    case "PageDown": case " ": body.scrollBy({ top: body.clientHeight - 40 }); break;
    case "PageUp": body.scrollBy({ top: -(body.clientHeight - 40) }); break;
    case "Home": case "g": body.scrollTop = 0; break;
    case "End": case "G": body.scrollTop = body.scrollHeight; break;
    case "Escape": closeViewer("background"); break; // W3
    default: return false;
  }
  e.preventDefault();
  return true;
}

// stepSplitAnchor is ↓ / ↑ on the overview (and N / P from the file): the
// next link that can open, shown in the right column at once.
function stepSplitAnchor(dir) {
  const i = stepAnchor(split.ov.anchors, split.ov.sel, dir);
  if (i < 0) return;
  selectAnchor(i);
  steerSplit();
}

// enterSplitFile is enter on the overview: into the right column, steering
// first when it does not show the selected link yet.
function enterSplitFile() {
  const a = split.ov.anchors[split.ov.sel];
  if (a && (!view.id || !view.from || view.from.dest !== a.dest)) steerSplit();
  focusSplit(false);
}
```

`closeViewer` takes the split down first (W3 — the right file then goes to the background, never closes):

```js
function closeViewer(how = "close") {
  if (split) {
    const sp = split;
    leaveSplit();
    if (sp.id) ofQueue({ op: "background", id: sp.id });
    how = "background";
  }
  …existing body…
```

`dropViewer` calls `leaveSplit()` first (`if (split) leaveSplit();`). `backgroundViewer`'s stored-overview early return reads `view.ov && view.ov.stored` — unchanged (null in the split).

- [ ] **Step 6: The bottom bar for the left column**

At the top of `viewerFoot`:

```js
  if (split && split.left) {
    return (
      `<span>↑↓ links</span><button data-vact="sfile">enter open</button><button data-vact="sfile">tab file</button>` +
      (split.ov.stored ? "" : `<button data-vact="aref">r reference</button>`) +
      `<button data-vact="ytext">y copy text</button><button data-vact="close">esc close</button><button data-vact="files">ctrl+\\ open files</button>`
    );
  }
```

Foot click: `case "sfile": if (split) enterSplitFile(); break;`; `aref` / `ytext` read `ovDoc()` (`if (ovDoc()) …`).

- [ ] **Step 7: Run to verify they pass**

`cd internal/web && go test -run 'TestOverviewSplitIsWired|TestViewer|TestStoredOverview' -count=1 .` → PASS. Rebuild; `node check.mjs bin/gg splitStored` and `node check.mjs bin/gg narrowRemarks` → `all checks passed`.

- [ ] **Step 8: Commit** — "web: an overview opens beside its file (the split's left column, steering, keys)" + trailers.

---

### Task 5: The right column — tab back, N/P, the mark, the mouse

**Files:**
- Modify: `internal/web/static/viewer.js`, `internal/web/overviewsplitjs_test.go`
- Scratch: `check.mjs` (scenario `splitRight`)

**Interfaces:**
- Consumes: Task 4's `split`, `focusSplit`, `stepSplitAnchor`, `paintAnchors`; Task 2's `hereOf`.
- Produces: `syncHere()` (called from `paintCursor` and `renderViewer`).

- [ ] **Step 1: Write the failing checks**

`check.mjs`:

```js
// --- Task 5: the right column ------------------------------------------------
scenarios.splitRight = async () => {
  const p = await page(1400);
  await openStored(p);
  await p.keyboard.press("Enter"); // into the file, a.txt line 2 (link 1's band)
  ok(await p.evaluate(() => document.activeElement.id === "viewer-body"), "right: enter moves into the file");
  await p.keyboard.press("n"); // to a.txt:4-5 — link 2's band
  ok(await vis(p, "#viewer-ov-body a.md-anchor.asel >> text=line two"), "right W5: n leaves the selection on link 1");
  ok(await vis(p, "#viewer-ov-body a.md-anchor.ahere >> text=four"), "right: link 2 wears the mark");
  await p.keyboard.press("ArrowUp"); await p.keyboard.press("ArrowUp"); // line 2: link 1's own band
  ok(!(await p.locator("#viewer-ov-body a.md-anchor.ahere").count()), "right: no mark on the selection's own band");
  await p.locator('#viewer-body .vline[data-i="3"]').click(); // line 4
  ok(await vis(p, "#viewer-ov-body a.md-anchor.ahere >> text=four"), "right: a click on a line marks too");
  ok(await vis(p, "#viewer-ov-body a.md-anchor.asel >> text=line two"), "right: a click never moves the selection");
  await p.keyboard.press("N");
  await p.waitForFunction(() => document.querySelector("#viewer-ov-body a.md-anchor.asel")?.innerText === "four");
  ok(await p.evaluate(() => document.activeElement.id === "viewer-body"), "right: N steps the overview, the keys stay right");
  await p.keyboard.press("Backspace");
  ok(await p.evaluate(() => document.activeElement.id === "viewer-ov-body"), "right: backspace returns to the overview");
  await p.locator("#viewer-ov-body a.md-anchor >> text=beta").click();
  await p.waitForFunction(() => document.querySelector("#viewer-title").innerText.includes("b.txt"));
  ok(await vis(p, "#viewer-ov-body a.md-anchor.asel >> text=beta"), "mouse: a click on a link selects and steers");
  await p.close();
};
```

`overviewsplitjs_test.go` gains:

```go
// Spec 2026-10-11 §7 / §8: the right column's own keys, and the mark follows
// every cursor move.
func TestOverviewSplitRightColumnIsWired(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{`function syncHere()`, `hereOf(bands, cur, view.cur, selB)`, `case "Backspace": e.preventDefault(); focusSplit(true); return true;`,
		`case "N": case "P":`, `$("viewer-ov-body").addEventListener("click"`} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
}
```

- [ ] **Step 2: Run to verify they fail** — the Go test FAILs (lacks `syncHere`); `node check.mjs bin/gg splitRight` FAILs at "n leaves the selection" or "link 2 wears the mark".

- [ ] **Step 3: Implement**

`splitKey`'s right branch:

```js
  if (split.left) return splitOverviewKey(e);
  switch (e.key) {
    case "Backspace": e.preventDefault(); focusSplit(true); return true;
    case "N": case "P": e.preventDefault(); stepSplitAnchor(e.key === "N" ? 1 : -1); return true;
  }
  return fileKey(e);
```

(`Escape` in `fileKey` already reaches `closeViewer`, which takes the split down — W3.)

The mark:

```js
// syncHere recomputes the link the right column's cursor marks on the
// overview (spec §7) and repaints the left column's links when it moved.
function syncHere() {
  if (!split) return;
  const f = view.from, bands = viewBands(), n = view.lines.length;
  const cur = f ? bandOf(bands, f.anchors, f.cur, n) : -1;
  const sel = split.ov.anchors[split.ov.sel];
  const selB = sel && f ? bandOf(bands, f.anchors, sel.dest, n) : -1;
  const here = hereOf(bands, cur, view.cur, selB);
  if (here === split.here) return;
  split.here = here;
  paintAnchors();
}
```

Call `syncHere()` at the end of `paintCursor()` and of `renderViewer()`, and in `stepViewerAnchor` after `centerCursor();`. `stepSplitAnchor` sets `split.here = -1` before `steerSplit()` (the landing recomputes it).

Mouse: at the top of the `#viewer-body` click handler: `if (split && split.left) focusSplit(false);`. New handler for the left column:

```js
$("viewer-ov-body").addEventListener("click", (e) => {
  if (!split) return;
  const anc = e.target.closest("a.md-anchor");
  focusSplit(true);
  if (!anc) return;
  e.preventDefault();
  selectAnchor(Number(anc.dataset.a));
  split.here = -1;
  steerSplit();
});
```

The right-focused bottom bar, at the top of `viewerFoot`'s file branch (after the left branch from Task 4):

```js
  if (split) {
    const bandKeysS = viewBands().length ? "<span>n p bands</span>" : "";
    const notesS = (shownNotes().length ? `<span>} { notes</span>` : "") +
      (view.notes.length ? `<button data-vact="dismiss">d dismiss</button><button data-vact="ref">r reference</button>` : "");
    return (
      `<button data-vact="sov">tab overview</button><span>N P overview links</span>` + bandKeysS +
      `<span>↑↓ j k line</span><button data-vact="find">/ find</button><span>] [ next / prev</span>` + notesS +
      `<button data-vact="wrap">w long lines</button><button data-vact="menu">. menu</button><button data-vact="close">esc close</button><button data-vact="files">ctrl+\\ open files</button>`
    );
  }
```

Foot click: `case "sov": focusSplit(true); break;`.

- [ ] **Step 4: Run to verify they pass** — Go guards PASS; rebuild; `node check.mjs bin/gg` (all scenarios so far) → `all checks passed`.

- [ ] **Step 5: Commit** — "web: the split's file column marks the overview's link it is in; N/P, tab, backspace, clicks" + trailers.

---

### Task 6: Agent overviews, live updates, narrowing, the way back

**Files:**
- Modify: `internal/web/static/viewer.js`, `internal/web/overviewsplitjs_test.go`
- Scratch: `check.mjs` (scenarios `splitAgent`, `splitNarrow`, `splitBack`)

**Interfaces:**
- Consumes: everything above.
- Produces: `refreshSplitOverview()`, `splitOverviewGone(name)`, `narrowSplit()`, `backIntoSplit(f)`.

- [ ] **Step 1: Write the failing checks**

```js
// --- Task 6: agent overviews, live updates, narrowing, the way back ----------
const addOverview = (title, text, bg = false) =>
  ggRun(["session", "overview", "add", "--title", title, ...(bg ? ["--background"] : [])], text);

scenarios.splitAgent = async () => {
  const p = await page(1400);
  addOverview("Tour", "See [two](a.txt:2) and [gamma](b.txt:3).");
  await p.waitForSelector("#viewer-ov-body .vdoc", { state: "visible" });
  ok((await text(p, "#viewer-ov-title")).includes("Tour"), "agent: overview add opens the split");
  await p.waitForFunction(() => document.querySelector("#viewer-title").innerText.includes("a.txt"));
  ok(await vis(p, "#viewer-body .vline.vanchor"), "agent: the right column draws the link's band");
  const id = (await text(p, "#viewer-ov-title")).match(/Overview (f\d+)/)[1];
  ggRun(["session", "overview", "set", id, "--title", "Tour 2"], "See [gamma](b.txt:3) only.");
  await p.waitForFunction(() => document.querySelector("#viewer-ov-title").innerText.includes("Tour 2"));
  ok(await vis(p, "#viewer-ov-body a.md-anchor >> text=gamma"), "agent: an update re-reads the left column");
  await p.keyboard.press("Escape");
  ok(!(await vis(p, "#viewer")), "agent: esc closes the split");
  ggRun(["session", "files", "focus", id]);
  await p.waitForSelector("#viewer-ov-body .vdoc", { state: "visible" });
  ok(true, "agent: files focus opens the split again (it stayed in the switcher)");
  ggRun(["session", "overview", "close", id]);
  await p.waitForFunction(() => document.querySelector("#viewer-ov").classList.contains("hidden"));
  ok(await vis(p, "#viewer-body .vline") && !(await p.locator("#viewer-body .vline.vanchor").count()),
    "agent: a closed overview leaves the file, its bands gone");
  await p.close();
};

scenarios.splitOtherOpen = async () => {
  const p = await page(1400);
  addOverview("Other", "See [two](a.txt:2).");
  await p.waitForSelector("#viewer-ov-body .vdoc", { state: "visible" });
  // a plain file opened another way (what the switcher's enter and F's "view file" call)
  await p.evaluate(async () => (await import("/static/viewer.js")).openViewer({ src: "worktree", path: "b.txt" }));
  await p.waitForFunction(() => document.querySelector("#viewer-title").innerText.includes("b.txt"));
  ok(!(await vis(p, "#viewer-ov")), "other open: another file's open closes the split, the file shows alone");
  await p.keyboard.press("Control+\\");
  ok(await vis(p, '#openfiles-list :text("Other")'), "other open: the overview stays in the switcher");
  await p.close();
};

scenarios.splitNarrow = async () => {
  const p = await page(1400);
  await openStored(p);
  await p.setViewportSize({ width: 800, height: 900 });
  await p.waitForFunction(() => document.querySelector("#viewer-ov").classList.contains("hidden"));
  ok(await vis(p, "#viewer-body .vdoc"), "narrow resize: the box shows the overview alone");
  await p.close();
};

scenarios.splitBack = async () => {
  const p = await page(800);
  await openStored(p);
  await p.keyboard.press("Enter"); // the narrow viewer opens a.txt
  await p.waitForSelector("#viewer-body .vline");
  await p.setViewportSize({ width: 1400, height: 900 });
  await p.keyboard.press("ArrowDown"); await p.keyboard.press("ArrowDown"); // the reader moves to line 4
  await p.keyboard.press("Backspace");
  await p.waitForSelector("#viewer-ov-body .vdoc", { state: "visible" });
  ok((await text(p, "#viewer-title")).includes("a.txt"), "back: the file it left is the right column");
  ok(await vis(p, '#viewer-body .vline.vcur[data-i="3"]'), "back: the file kept the reader's line");
  ok(await vis(p, "#viewer-ov-body a.md-anchor.asel >> text=line two"), "back: the link it was opened from is selected");
  await p.close();
};
```

`overviewsplitjs_test.go` gains:

```go
// Spec 2026-10-11 §5 / §10: the split follows the store, collapses when the
// box narrows, and backspace in a file comes back INTO the split.
func TestOverviewSplitLiveIsWired(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{`function refreshSplitOverview()`, `function splitOverviewGone(`, `function narrowSplit()`,
		`function backIntoSplit(f)`, `if (f && splitFits(boxWidth())) return backIntoSplit(f);`,
		`window.addEventListener("resize"`, `if (split) return; // the split adds no history entry`} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
}
```

- [ ] **Step 2: Run to verify they fail** — the Go guard FAILs; `node check.mjs bin/gg splitAgent` FAILs at "an update re-reads the left column" (no refresh yet) or later; `splitNarrow` / `splitBack` FAIL.

- [ ] **Step 3: Implement live updates**

```js
// refreshSplitOverview re-reads the left column's overview after the store
// moved it (spec §10), keeping the selection by its destination; the right
// column's bands follow. An older answer landing after a newer one drops.
async function refreshSplitOverview() {
  const sp = split;
  const mine = ++sp.seq;
  let ov;
  try {
    ov = await fetchOverview(sp.id);
  } catch {
    return;
  }
  if (split !== sp || sp.seq !== mine) return;
  sp.ov = { ...ov, sel: keepAnchor(ov.anchors || [], sp.ov.want, sp.ov.sel), want: sp.ov.want };
  paintSplitTitle();
  renderSplitOverview();
  if (view.from && !view.from.stored && view.from.id === sp.id) {
    view.from.anchors = sp.ov.anchors;
    view.from.stamp = ov.stamp;
    rerenderKeepingScroll();
  }
  swapFoot(true);
}

// splitOverviewGone: the left column's overview left the store (closed by
// the agent, or by x in a switcher) — the split collapses to the right
// column's file, its bands gone (spec §10).
function splitOverviewGone(name) {
  leaveSplit();
  if (view.from) view.from.closed = true;
  if (view.id) {
    paintTitle();
    rerenderKeepingScroll();
    swapFoot(true);
    $("viewer-body").focus({ preventScroll: true });
  } else dropViewer();
  opLine(name + " was closed", false);
}
```

In `viewerAgentDocs`, at the top:

```js
  if (split && split.id) {
    if (fromGone(stamps, closed, split.id)) splitOverviewGone("overview " + split.id + " " + JSON.stringify(split.ov.title));
    else if (overviewStale(stamps, split.id, split.ov.stamp)) refreshSplitOverview();
  }
```

and the `view.from` branch's condition becomes `if (f && !f.closed && !f.stored && !view.ov && !split) {`.
In `viewerOpenFiles`, before its body: `if (split && split.id && !files.some((f) => f.id === split.id)) splitOverviewGone("overview " + split.id + " " + JSON.stringify(split.ov.title));`.

- [ ] **Step 4: Implement narrowing**

```js
// narrowSplit: the box narrowed below the split's width (W7) — the overview
// stays as today's one document, the right column's file goes to the
// background (the TUI's narrowSplits). Widening never re-splits on its own.
function narrowSplit() {
  const sp = split;
  leaveSplit();
  if (view.id) {
    rememberPlace();
    ofQueue({ op: "background", id: view.id, line: view.cur });
  }
  loadSeq++; // a steer in flight must not land over the overview
  if (sp.ov.stored) showStoredOverview(sp.ov.stored);
  else {
    ofQueue({ op: "focus", id: sp.id });
    showOverview({ id: sp.id, path: sp.path }, sp.ov, ovApplied);
  }
  selectAnchor(sp.ov.sel);
}

window.addEventListener("resize", () => {
  if (split && isOpen() && !splitFits($("viewer-box").clientWidth)) narrowSplit();
});
```

(`showOverview`/`showStoredOverview` take the narrow branch: the box is below the breakpoint. `showOverview` sets `view.ov = {...ov, sel: -1}` — the `selectAnchor(sp.ov.sel)` after it restores the selection.)

- [ ] **Step 5: Implement the way back and history**

At the top of `anchorBack`, after `const f = view.from, id = view.id;`:

```js
  if (f && splitFits(boxWidth())) return backIntoSplit(f);
```

```js
// backIntoSplit is backspace / Back in a file an overview opened when the
// box is wide enough (spec §5): the overview comes back in the left column
// with the link selected, the file stays in the right column where the
// reader left it. The one history entry the open armed is used up.
async function backIntoSplit(f) {
  let ov;
  if (f.stored) ov = { title: f.stored.title, blocks: f.stored.blocks, anchors: f.stored.anchors, text: f.stored.text, stored: f.stored };
  else {
    try {
      ov = await fetchOverview(f.id);
    } catch (e) {
      return opLine(e.status === 404 ? "the overview was closed" : "back failed: " + (e.message || e), e.status !== 404);
    }
  }
  if (!isOpen() || view.from !== f) return; // something else took the screen meanwhile
  f.anchors = ov.anchors || [];
  if (ov.stamp) f.stamp = ov.stamp;
  openSplit(f.stored ? "" : f.id, "", { ...ov, sel: backAnchor(f.anchors, f), want: f.dest }, { steer: false });
  rerenderKeepingScroll();
  if (ownBack) history.back(); // the popstate it fires meets the split and does nothing
}
```

In the `popstate` listener, after `ownBack = …`: `if (split) return; // the split adds no history entry`.

- [ ] **Step 6: Run to verify they pass** — Go guards PASS; rebuild; `node check.mjs bin/gg` → `all checks passed` (every scenario).

- [ ] **Step 7: Commit** — "web: the split follows the agent's store, narrows to one document, and backspace comes back into it" + trailers.

---

### Task 7: Help, skill, docs — and the gate

**Files:**
- Modify: `internal/web/static/viewer.js` (`registerHelp` text), `internal/web/static/reviews.js` (the ≡ Overview help row, line ~775)
- Modify: `internal/agentskill/gg-overview.md` + its version marker; regenerate `.claude/skills/gg-overview/SKILL.md`
- Modify: `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`
- Modify: `internal/web/overviewsplitjs_test.go`

- [ ] **Step 1: Failing guard**

```go
// The split is advertised: the viewer's help entry and the ≡ Overview row's.
func TestOverviewSplitIsAdvertised(t *testing.T) {
	t.Parallel()
	if !strings.Contains(readStatic(t, "viewer.js"), "opens beside the file its selected link points to") {
		t.Error("viewer.js help lacks the split")
	}
	if !strings.Contains(readStatic(t, "reviews.js"), "beside the file") {
		t.Error("reviews.js ≡ Overview help lacks the split")
	}
}
```

Run: `cd internal/web && go test -run TestOverviewSplitIsAdvertised -count=1 .` → FAIL.

- [ ] **Step 2: Implement the text**

`registerHelp` in viewer.js — replace the overview sentence with: "an agent's overview (<code>gg session overview</code>) or a review's stored one opens beside the file its selected link points to when the window is wide enough (one document otherwise): <b>↑↓</b> step its links and the file follows, <b>enter</b> or <b>tab</b> moves into the file, <b>tab / backspace</b> back, <b>N / P</b> step the overview from the file, the link whose band holds the cursor is marked in the bands' colour, a review's remarks show as read-only boxes; <b>r</b> copies a link's reference, <b>y</b> the text, <b>esc</b> closes both (an agent's overview stays in the switcher); in the file the overview's line and range links are tinted bands and <b>n / p</b> step through them without moving the overview's selection". In reviews.js (~line 775) the ≡ Overview phrase becomes "overview (click it: the overview opens beside the file its link points to, <b>↑↓</b> walk its links, …" — keep the rest of the sentence's facts.

- [ ] **Step 3: Skill + docs**

- `internal/agentskill/gg-overview.md`: where it says the TUI shows an overview beside the file, add "— gg web does the same". Bump its version marker (`grep -n "version" internal/agentskill/gg-overview.md | head -3`; follow the existing scheme) and the embedded version if `agentskill` keeps one per skill (`grep -n "GGOverview" internal/agentskill/*.go`).
- Regenerate the dogfood copy with a throwaway test (never `gg init --update` on a branch):

```go
// internal/agentskill/zz_regen_test.go — delete after running
package agentskill
import ("os"; "testing")
func TestRegenDogfood(t *testing.T) {
	if err := os.WriteFile("../../.claude/skills/gg-overview/SKILL.md", []byte(GGOverview.SkillFile()), 0o644); err != nil { t.Fatal(err) }
}
```

Run `go test -run TestRegenDogfood ./internal/agentskill/`, then `rm internal/agentskill/zz_regen_test.go`.
- `CHANGELOG.md` (top, unreleased): "gg web: every overview opens beside its code — the viewer splits into the overview and the file its selected link points to (≥ 900px box), ↑↓ step the links and the file follows, N/P step the overview from the file, the link whose band holds the cursor is marked; a review's remarks show as read-only boxes; n/p in a file no longer move the overview's selection."
- `README.md`: the web overview paragraph (`grep -n "overview" README.md`) gains the split sentence.
- `docs/CLAUDE-details.md` "Review overview split" section: a "Web" paragraph — `view` stays the right file, `split` holds the left column, the doors (`showOverview`/`showStoredOverview` → `openSplit` at `splitFits(boxWidth())`), `steerSplit` → `openViewer({…, from, steer: true})`, remarks via `/api/file-content?review=`, `syncHere`/`hereOf` → `.ahere`, `narrowSplit`, `backIntoSplit`; gotcha: `openViewer` sets `view.from` before its first render (remarks ride the content read).

- [ ] **Step 4: Gate**

Run: `cd internal/web && go test -count=1 .` → PASS. Rebuild; `node $CLAUDE_JOB_DIR/tmp/websplit/check.mjs $PWD/bin/gg` → `all checks passed`.
Run the full race gate in the background: `./test.sh race > $CLAUDE_JOB_DIR/tmp/race.log 2>&1`; green only when the log says "all green" and `grep -c 'FAIL\|DATA RACE'` is 0.

- [ ] **Step 5: Commit** — `gg add` the named files; "docs+skill: gg web opens every overview beside its code" + trailers.
