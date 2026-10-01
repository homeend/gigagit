# Agent docs on the web — Plan 2: overviews in gg web

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (this repo forbids implementer subagents). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An agent's overview documents (`gg session overview …`) live in the shared `agentdocs` store; the TUI keeps its layout and selection per document but reads text, title and anchors from the store; the gg web page lists, shows and walks overviews (tab / shift+tab / enter / single click / backspace / r / y); a standalone `gg web` answers the overview verbs itself.

**Architecture:** `agentdocs` gains `Overview` (id from `NextFileSeq`, root, title, text, anchors with `Missing`), the anchor grammar (`ParseAnchorDest`) and anchor extraction (`ParseOverview` over `markdown.ParseWith`), the limits and the reply prose. The TUI's `srcOverview` documents are built from store overviews and follow the store's signal (text/title changes re-lay out; a removal closes the document). The web follow pass lists every overview of the served root under the store's id (pinned), `GET /api/overview` serves the parsed tree plus anchors, and `viewer.js` gains a document mode.

**Tech Stack:** Go 1.26, Bubble Tea, net/http + SSE, ES modules (node-run pure tests), playwright for the browser check.

**Spec:** `docs/superpowers/specs/2026-10-01-agent-docs-web-design.md` (plan 2 = "Web — overviews"; plan 1 merged as `538de655`).

## Global Constraints

- The overview spec's rulings hold (`2026-09-30-agent-overview-documents-design.md`): anchors are a file, a line/range, or a note (`note:t<n>`) — never another overview; memory only; an overview is an open file (`f<n>`), never evicted, esc backgrounds, X / x closes; several overviews, replaceable by id (`set`); a visited file stays open after back; back is step by step.
- Limits (moved to the store, same text): text ≤ 64 KiB (`the text is over 64 KiB`), title required on add and one line ≤ 200 runes, ≤ 20 overviews per root (`20 overviews are open; remove one first`), anchors past 100 stay plain text.
- One shared copy (ruling 1): the TUI and the page it hosts show the same overviews with the same ids; closing in one closes in the other. Selection, layout width and the way back are per frontend (per tab on the web).
- Single click opens an anchor in the browser (ruling 2); the TUI keeps its double click.
- `agentdocs` may import `steer`, `textdiff`, `markdown` (archtest allowlist grows by `markdown`).
- Web: overview verbs are answered before `toSteerWire` and the `c.Background` branch; `overview_add` without `--background` is behind the op-in-flight gate (falls back to the background with `added f7 in the background (<why>)`); the rest skip it.
- `mdHTML` without the `anchors` option paints an `anchor` inline as plain text — PR / review rendering never changes.
- Commits: `gg add <paths>` + `git commit -F <msgfile>`, never `git add -A`; the Co-Authored-By / Claude-Session lines. Race gate "all green" before asking to merge. Browser checks assert visibility and run against the unfixed build first.

## Review Focus

1. **Ids across the two lists** — a hosted page lists a TUI overview under the SAME `f<n>`; that id must never collide with a plain file the page opened itself (both draw from `Shared().NextFileSeq()`).
2. **Closing from the other side** — an overview the browser closes (x) must leave the TUI, and the one on the TUI's screen closes as X would with `overview f7 was closed in the browser`; an overview the TUI closes must close any tab showing it (`overview f7 was closed`).
3. **`set` while someone reads** — a replaced text re-lays out in the TUI keeping the selected anchor by destination, and repaints a tab showing it without losing the tab's way back.
4. **Missing anchors** — checked by the store (stat + note lookup within the root, off the UI thread in the TUI, in the request on the web) and painted struck-through on both sides; a note anchor of another root counts as missing.
5. **Back in the browser** — backspace in a file opened from an anchor returns to the overview with that anchor selected; with the overview closed meanwhile it says `the overview was closed` and drops the way back.

---

## File structure

| File | Responsibility |
|------|----------------|
| `internal/agentdocs/overviews.go` (new) | `Overview`, `Anchor`, `ParseAnchorDest`, `ParseOverview`, `AddOverview`, `SetOverview`, `RemoveOverview`, `Overview`, `Overviews`, `CheckAnchors`, `SetAnchorMissing` |
| `internal/agentdocs/reply.go` | `AnchorReference`, `OverviewWire`, `OverviewUnresolved` |
| `internal/agentdocs/overviews_test.go` (new) | grammar (moved from `tui/overview_test.go`), limits, check, signal |
| `internal/archtest/import_guard_test.go` | allow `markdown` |
| `internal/tui/overview.go`, `overview_keys.go`, `steer_overview.go` | build docs from store overviews; store-backed verbs; missing via the store |
| `internal/tui/agentdocs_track.go` | the change handler also syncs overviews (re-lay out, add, remove) |
| `internal/tui/open_files.go` | `closeDoc` of an overview → `RemoveOverview` |
| `internal/cli/session_overview.go` | drop the "need a gg TUI" refusal |
| `internal/web/openfiles.go` | `ensureOpenID` (a given id), overview entries skipped by `due` (already: worktree only), `ofKeyOf` refuses `src=overview` opens by path |
| `internal/web/agentdocs_follow.go` | overview half of the follow pass; remove entries whose overview left |
| `internal/web/overview_http.go` (new) | `GET /api/overview?id=` |
| `internal/web/steer_overviews.go` (new) | `overview_add|set|list|show|rm` |
| `internal/web/openfiles_http.go` | x on an overview → `RemoveOverview` |
| `internal/web/steer.go` | dispatch |
| `internal/web/static/markdown.js` | `anchors` option |
| `internal/web/static/viewer.js` | document mode, anchor keys/clicks, range highlight, back |
| `internal/web/static/openfiles.js`, `style.css` | switcher rows, anchor styles |
| `e2e/scenarios/s105_session_overview_web.toml` (new) | web-only overview scenario |
| docs | CHANGELOG, README, CLAUDE-details, skill (bump past 111), CLAUDE.md row |

---

### Task 1: Overviews in `agentdocs`

**Files:** Create `internal/agentdocs/overviews.go`, `internal/agentdocs/overviews_test.go`; modify `internal/agentdocs/reply.go`, `internal/archtest/import_guard_test.go`.

**Interfaces — Produces:**
- `type Anchor struct{ Dest, Path string; Start, End int; Note string; Missing bool }`
- `type Overview struct{ ID string; Seq int64; Root, Title, Text string; Anchors []Anchor }`
- `const MaxOverviewBytes = 64 << 10; MaxOverviewsPerRoot = 20; MaxOverviewTitle = 200; MaxAnchors = 100`
- `func ParseAnchorDest(dest string) (Anchor, bool)` (the TUI's `parseAnchorDest`, moved)
- `func IsAnchorDest(dest string) bool`
- `func ParseOverview(text string) (markdown.Doc, []Anchor)` — the parsed tree with anchor inlines numbered (`Text` = the index, as the TUI does today) and anchors past `MaxAnchors` turned to text; the anchors in document order.
- `func (s *Store) AddOverview(root, dir, title, text string) (Overview, error)` — `root` the key (`domain.CheckoutKey(top)`), `dir` the worktree on disk (anchors are stat'ed there)
- `func (s *Store) SetOverview(id, title, text string) (Overview, error)` (`title == ""` keeps the old one)
- `func (s *Store) RemoveOverview(id string) bool`
- `func (s *Store) Overview(id string) (Overview, bool)`; `func (s *Store) Overviews(root string) []Overview` (by seq)
- `func (s *Store) CheckAnchors(id string) bool` — stats file anchors under `root`, note anchors via the notes of the same root; true when a flag changed (signals).
- `func (s *Store) SetAnchorMissing(id string, i int, missing bool) bool` (an open that found the target gone/back)
- `func AnchorReference(o Overview, a Anchor) string` → `gg overview f7 "<title>" → <dest>`
- `func OverviewWire(o Overview, state string, withText bool) steer.Overview`

Errors, word for word from `tui/steer_overview.go`: `an overview needs text`, `the text is over 64 KiB`, `an overview needs a title`, `the title must be one line of at most 200 characters`, `20 overviews are open; remove one first`, and `no overview <id>` (Set on a missing id).

- [ ] **Step 1: failing tests** — move `TestParseAnchorDest*` cases from `internal/tui/overview_test.go` into `overviews_test.go` (rename `parseAnchorDest` → `ParseAnchorDest`, `anchorTarget{path,start,end,note}` → `Anchor{Path,Start,End,Note}`), plus:

```go
func TestAddOverviewNumbersFromTheStoreAndKeepsAnchorsInOrder(t *testing.T) {
	t.Parallel()
	s := New()
	s.NextFileSeq() // a plain file took f1
	o, err := s.AddOverview("/r", "/r", "Tour", "# T\n\n[a](a.go) then [b](b.go:3-4) and [n](note:t9) and [web](https://x.y)\n")
	if err != nil {
		t.Fatal(err)
	}
	if o.ID != "f2" || o.Title != "Tour" || len(o.Anchors) != 3 {
		t.Fatalf("overview = %+v", o)
	}
	if a := o.Anchors[1]; a.Dest != "b.go:3-4" || a.Path != "b.go" || a.Start != 3 || a.End != 4 {
		t.Fatalf("anchor 1 = %+v", a)
	}
	if o.Anchors[2].Note != "t9" {
		t.Fatalf("anchor 2 = %+v", o.Anchors[2])
	}
}

func TestOverviewLimits(t *testing.T) {
	t.Parallel()
	s := New()
	for _, tc := range []struct{ title, text, want string }{
		{"T", "  ", "an overview needs text"},
		{"T", strings.Repeat("x", MaxOverviewBytes+1), "the text is over 64 KiB"},
		{" ", "x", "an overview needs a title"},
		{"a\nb", "x", "the title must be one line of at most 200 characters"},
	} {
		if _, err := s.AddOverview("/r", "/r", tc.title, tc.text); err == nil || err.Error() != tc.want {
			t.Errorf("%q/%d: err = %v, want %q", tc.title, len(tc.text), err, tc.want)
		}
	}
	for i := 0; i < MaxOverviewsPerRoot; i++ {
		if _, err := s.AddOverview("/r", "/r", "T", "x"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AddOverview("/r", "/r", "T", "x"); err == nil || err.Error() != "20 overviews are open; remove one first" {
		t.Fatalf("cap: %v", err)
	}
	if _, err := s.AddOverview("/other", "/other", "T", "x"); err != nil {
		t.Fatal("the cap is per root")
	}
}

func TestSetKeepsTheIdAndRemoveSignals(t *testing.T) {
	t.Parallel()
	s := New()
	o, _ := s.AddOverview("/r", "/r", "T", "[a](a.go)")
	ch, cancel := s.Subscribe()
	defer cancel()
	set, err := s.SetOverview(o.ID, "", "[b](b.go) [c](c.go)")
	if err != nil || set.ID != o.ID || set.Title != "T" || len(set.Anchors) != 2 {
		t.Fatalf("set = %+v %v", set, err)
	}
	<-ch
	if _, err := s.SetOverview("f99", "", "x"); err == nil || err.Error() != "no overview f99" {
		t.Fatalf("set unknown: %v", err)
	}
	if !s.RemoveOverview(o.ID) || s.RemoveOverview(o.ID) {
		t.Fatal("remove exactly once")
	}
	<-ch
}

func TestCheckAnchorsStatsFilesAndLooksUpNotesInTheRoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "here.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New()
	root := dir // the caller's key; the store joins paths onto it
	n, _ := s.AddNote(root, "here.go", []string{"x"}, 1, 1, "s", "", "")
	other, _ := s.AddNote("/elsewhere", "o.go", []string{"x"}, 1, 1, "s", "", "")
	o, _ := s.AddOverview(root, dir, "T", "[h](here.go) [g](gone.go) [n](note:"+n.ID+") [o](note:"+other.ID+")")
	if !s.CheckAnchors(o.ID) {
		t.Fatal("the first check found missing anchors: want changed")
	}
	got, _ := s.Overview(o.ID)
	var miss []bool
	for _, a := range got.Anchors {
		miss = append(miss, a.Missing)
	}
	if fmt.Sprint(miss) != "[false true false true]" {
		t.Fatalf("missing = %v", miss)
	}
	if s.CheckAnchors(o.ID) {
		t.Fatal("an unchanged check reported a change")
	}
}
```

NOTE the root key vs the disk: callers pass `domain.CheckoutKey(top)`, case-folded on Windows/macOS — so the store keeps `dir` (the worktree on disk) per overview and `CheckAnchors` stats under it.

- [ ] **Step 2:** `go test ./internal/agentdocs/ -count=1` → FAIL (undefined).
- [ ] **Step 3: implement.** `ParseOverview` reuses the TUI's numbering walk (`overview.go:overviewLines`' `number`/`blocks` closures) over `markdown.ParseWith(text, markdown.Options{Anchor: IsAnchorDest})`, turning anchors past `MaxAnchors` into `markdown.Inline{Kind: markdown.InText, Text: flat(n.In)}` (`flat` = the text of the inlines — move `mdFlat` or write a 10-line equivalent). The store holds `overviews map[string]*ovEntry` (`Overview` + `dir`); every mutation signals after unlock. `CheckAnchors` copies the entry under the lock, stats WITHOUT the lock, then writes the flags back under the lock if the entry still exists and its text is unchanged.
- [ ] **Step 4:** `go test -race ./internal/agentdocs/ ./internal/archtest/ -count=1` → ok.
- [ ] **Step 5: commit** — `feat(agentdocs): overviews in the store — anchors, limits, check, reply prose`

---

### Task 2: The TUI's overviews come from the store

**Files:** modify `internal/tui/overview.go`, `overview_keys.go`, `steer_overview.go`, `agentdocs_track.go`, `open_files.go`, `sessions_popup.go`, `md_render.go` (cap constant → `agentdocs.MaxAnchors`); tests `overview_test.go`, `overview_doc_test.go`, `overview_keys_test.go`, `steer_overview_test.go`, new `agentdocs_overview_track_test.go`.

**Interfaces:** Consumes Task 1. Produces `newOverviewDocFrom(o agentdocs.Overview) *openFile` (seq = the store's), `(*openFile).ov.anchors[i].target` built from `agentdocs.Anchor`, `(Model).syncOverviews() tea.Cmd`.

- [ ] **Step 1: failing tests** (`agentdocs_overview_track_test.go`):

```go
// An overview the browser closes leaves the TUI; the one on screen closes
// as X would and says so.
func TestAnOverviewRemovedElsewhereClosesInTheTUI(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	m, d := addOverviewViaSteer(t, m, "Tour", "[a](a.txt:3)") // shown
	m.docs.RemoveOverview(d.id())
	m, _ = m.onAgentDocsChanged()
	if m.openFiles.find(m.currentWorktree, d.key()) != nil || topDoc(m) == d {
		t.Fatal("the overview is still open in the TUI")
	}
	if !strings.Contains(m.statusMsg, "overview "+d.id()+" was closed in the browser") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

// set made elsewhere re-lays out, keeping the selected anchor by dest.
func TestAnOverviewSetElsewhereRelaysOutKeepingTheSelection(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	m, d := addOverviewViaSteer(t, m, "Tour", "[a](a.txt:3) [b](a.txt:5)")
	rows, _ := m.viewerGeom()
	d.selectAnchor(1, rows) // a.txt:5
	if _, err := m.docs.SetOverview(d.id(), "Tour 2", "intro [x](a.txt:1) [b](a.txt:5)"); err != nil {
		t.Fatal(err)
	}
	m, _ = m.onAgentDocsChanged()
	if d.title != "Tour 2" || d.ov.text != "intro [x](a.txt:1) [b](a.txt:5)" || d.ov.sel != 1 || d.ov.anchors[1].dest != "a.txt:5" {
		t.Fatalf("title=%q sel=%d anchors=%+v", d.title, d.ov.sel, d.ov.anchors)
	}
}

// X in the TUI removes the overview from the store (the page closes it).
func TestXOnAnOverviewRemovesItFromTheStore(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	m, d := addOverviewViaSteer(t, m, "Tour", "[a](a.txt)")
	m = m.closeDoc(d)
	if _, ok := m.docs.Overview(d.id()); ok {
		t.Fatal("the store still has the overview after X")
	}
}
```

(`addOverviewViaSteer` applies an `overview_add` steer command through `applySteer` + `pumpAll` and returns the registered document — model it on `steer_overview_test.go`'s first test.)

- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: implement.**
  - `overview.go`: delete `parseAnchorDest`, `allDigits`, `isAnchorDest`; `anchorTarget` becomes `agentdocs.Anchor` fields (keep `anchor{dest, target agentdocs.Anchor, spans, missing}` or fold `missing` into `target.Missing` — choose the smaller diff, ledger it). `overviewLines` calls `agentdocs.ParseOverview(text)` and keeps only the row layout + span recovery. `newOverviewDocFrom(o)`: as `newOverviewDoc` but `d.seq` from the overview's id (`o.Seq`), `d.title = o.Title`, `d.ov.text = o.Text`, `d.ovID = o.ID`; delete `newOverviewDoc` (tests switch to the store).
  - `steer_overview.go`: `add` → `m.docs.AddOverview(domain.CheckoutKey(m.currentWorktree), m.currentWorktree, title, text)` (errors verbatim from the store), then `newOverviewDocFrom`, layout, show/background as today; `set` → `m.docs.SetOverview`; `list`/`show` → `agentdocs.OverviewWire` with the TUI's state; `rm` → `closeDoc` (which removes from the store). `checkAnchorsCmd` → off-thread `m.docs.CheckAnchors(id)`; `anchorsChecked` copies `Missing` from `m.docs.Overview(id)` and answers.
  - `overview_keys.go`: `openAnchor`'s stat stays (deferred minor: UI thread) but writes the result through `m.docs.SetAnchorMissing(id, i, gone)`; `anchorReference` → `agentdocs.AnchorReference`.
  - `open_files.go` `closeDoc`: `if d.ov != nil { m.docs.RemoveOverview(d.id()) }`.
  - `agentdocs_track.go` `syncDocNotes` → `syncAgentDocs`: for each overview doc in the current list: gone from the store → `m.closeDoc(d)` plus, when it was on screen, `m.statusMsg = i18n.T("overview %s was closed in the browser", d.id())`; text/title changed → `layOut` keeping the selection by dest (the `set` path's code, extracted into `d.relayOut(rows, width, text, title)`); `Missing` flags copied and `paint`ed. Store overviews of this root not in the list → `newOverviewDocFrom` + `registerDoc` in the background.
  - i18n: `overview %s was closed in the browser` in all four bundles (`adding-translations` skill).
- [ ] **Step 4:** targeted `go test ./internal/tui/ -run 'Overview|Anchor|Tour|Note|SharedStore' -count=1` → ok; then the whole package into a log → ok.
- [ ] **Step 5: commit** — `refactor(tui): overviews live in the shared agentdocs store; the TUI lays them out and follows the store`

---

### Task 3: CLI — overview verbs reach a web-only session

- [ ] **Step 1: failing test** `TestSessionOverviewGoesToAWebOnlySession` (shape of plan 1's note test: `newSteerServer(t, 200, `{"id":"x","ok":true,"detail":"showing f1","overviews":[{"id":"f1","title":"T","state":"shown","anchors":1}]}`)`, `liveWebPresence`, `runSessionIn(... "overview", "add", "--title", "T" ...)` with text on stdin; expect exit 0, stdout `f1`, one `overview_add` posted). Replace `TestSessionOverview…NeedsALiveTUI`'s web-only half.
- [ ] **Step 2:** run → FAIL ("overviews need a gg TUI").
- [ ] **Step 3:** `steerOverviews` keeps only `steerLive(dir, c, false, false, …)` + the `!rep.OK` print (plan 1's `steerNotes` shape).
- [ ] **Step 4:** `go test ./internal/cli/ -run SessionOverview -count=1` → ok.
- [ ] **Step 5: commit** — `feat(cli): gg session overview reaches a web-only session`

---

### Task 4: Web server — overview entries, the endpoint, the verbs

**Files:** modify `internal/web/openfiles.go`, `agentdocs_follow.go`, `openfiles_http.go`, `steer.go`; create `overview_http.go`, `steer_overviews.go`, test `agentdocs_overview_web_test.go`.

**Interfaces — Produces:** `(*openFiles).ensureOpenID(wt string, k ofKey, id string) (steer.OpenFile, string, bool)`; `(*openFiles).removeID(wt, id string) bool`; `GET /api/overview?id=` → `{id, title, text, blocks, anchors:[{dest,path,start,end,note,missing,ref}]}`; live `Reason:"agentdocs"` with `Closed: ["f7"]` (overviews that left) — `liveMsg` gains `Closed []string json:"closed,omitempty"`.

- [ ] **Step 1: failing tests:**

```go
func TestFollowPassListsOverviewsUnderTheStoresIDAndDropsGoneOnes(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	o, err := s.docs.AddOverview(root, s.service().Root(), "Tour", "[a](f.txt)")
	if err != nil {
		t.Fatal(err)
	}
	s.followDocs()
	wt := s.service().Root()
	f, ok := s.ofs.resolve(wt, o.ID, "")
	if !ok || f.ID != o.ID || f.Source != "overview" || f.Title != "Tour" {
		t.Fatalf("entry = %+v %v", f, ok)
	}
	if _, pinned, _ := s.ofs.pinnedEntry(wt, o.ID); !pinned {
		t.Fatal("an overview entry must be pinned")
	}
	s.docs.RemoveOverview(o.ID)
	s.followDocs()
	if _, ok := s.ofs.resolve(wt, o.ID, ""); ok {
		t.Fatal("an overview that left the store is still listed")
	}
}

func TestOverviewEndpointServesTheTreeAndTheAnchors(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	o, _ := s.docs.AddOverview(root, s.service().Root(), "Tour", "see [here](f.txt:1) and [gone](nope.txt)")
	var body struct {
		ID      string          `json:"id"`
		Title   string          `json:"title"`
		Blocks  json.RawMessage `json:"blocks"`
		Anchors []struct {
			Dest    string `json:"dest"`
			Missing bool   `json:"missing"`
			Ref     string `json:"ref"`
		} `json:"anchors"`
	}
	if code := getJSON(t, serve(t, s), "/api/overview?id="+o.ID, &body); code != http.StatusOK {
		t.Fatalf("code %d", code)
	}
	if body.ID != o.ID || len(body.Anchors) != 2 || body.Anchors[0].Missing || !body.Anchors[1].Missing {
		t.Fatalf("body = %+v", body)
	}
	if !strings.Contains(string(body.Blocks), `"k":"anchor"`) {
		t.Fatalf("blocks carry no anchor inline: %s", body.Blocks)
	}
	if code := getJSON(t, serve(t, s), "/api/overview?id=f999", nil); code != http.StatusNotFound {
		t.Fatalf("unknown id: %d", code)
	}
}

func TestSteerOverviewVerbsOnTheWeb(t *testing.T) {
	t.Parallel()
	s, _ := noteSrv(t)
	code, rep := steerAsk(t, s, `{"id":"1","cmd":"overview_add","title":"T","text":"[a](f.txt)","background":true}`)
	if code != http.StatusOK || !rep.OK || len(rep.Overviews) != 1 || rep.Detail != "added "+rep.Overviews[0].ID+" in the background" {
		t.Fatalf("add: %d %+v", code, rep)
	}
	id := rep.Overviews[0].ID
	if _, rep = steerAsk(t, s, `{"id":"2","cmd":"overview_set","file_id":"`+id+`","text":"[b](f.txt:1)"}`); !rep.OK || rep.Detail != "set "+id {
		t.Fatalf("set: %+v", rep)
	}
	if _, rep = steerAsk(t, s, `{"id":"3","cmd":"overview_show","file_id":"`+id+`"}`); !rep.OK || rep.Overviews[0].Text != "[b](f.txt:1)" {
		t.Fatalf("show: %+v", rep)
	}
	if _, rep = steerAsk(t, s, `{"id":"4","cmd":"overview_list"}`); !rep.OK || rep.Detail != "1 overviews" {
		t.Fatalf("list: %+v", rep)
	}
	if _, rep = steerAsk(t, s, `{"id":"5","cmd":"overview_rm","file_id":"`+id+`"}`); !rep.OK || rep.Detail != "closed "+id {
		t.Fatalf("rm: %+v", rep)
	}
	s.cur = &opRun{} // an op in flight: a foreground add falls back to the background
	if _, rep = steerAsk(t, s, `{"id":"6","cmd":"overview_add","title":"T","text":"x"}`); !rep.OK || !strings.Contains(rep.Detail, "in the background (") {
		t.Fatalf("add while busy: %+v", rep)
	}
}

func TestXOnAnOverviewEntryRemovesItFromTheStore(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	o, _ := s.docs.AddOverview(root, s.service().Root(), "T", "x")
	s.followDocs()
	postJSON(t, serve(t, s), "/api/open-files", `{"op":"close","id":"`+o.ID+`","tab":"t1","everywhere":true}`, "application/json", "", nil)
	if _, ok := s.docs.Overview(o.ID); ok {
		t.Fatal("x left the overview in the store")
	}
}
```

- [ ] **Step 2:** run → FAIL (undefined).
- [ ] **Step 3: implement.**
  - `openfiles.go`: `ofEntry` gains `title string`; `wireLocked` sets `Title`; `ensureOpenID` = `ensureOpen` with the id given (and `pinned` true, `title` set/updated); `removeID` drops an entry and its `tabShows`.
  - `agentdocs_follow.go` `followDocs`: after the notes half, `for _, o := range s.docs.Overviews(root)` → `ensureOpenID(wt, ofKey{Src: "overview", Path: "overview-" + strconv.FormatInt(o.Seq, 10) + ".md"}, o.ID)` with title; then every listed `overview` entry whose id is not in the store → `removeID` and collect it in `closed`; the fan-out carries `Closed: closed`. `setPinned` must keep overview entries pinned (pin by `Src == "overview"` too).
  - `openfiles_http.go` close: an overview entry — plain close backgrounds (pinned), `everywhere` → `s.docs.RemoveOverview(id)` then remove. `ofKeyOf`: `src=overview` → error `an overview opens by id` (opens by path are not allowed).
  - `overview_http.go`: `GET /api/overview?id=` (id must match `^f[0-9]{1,18}$`); `o, ok := s.docs.Overview(id)`; `ok && o.Root == s.docsRoot(ctx)` else 404; `s.docs.CheckAnchors(id)` (in the request goroutine); re-read; `doc, _ := agentdocs.ParseOverview(o.Text)`; answer `{id, title, text, blocks: doc.Blocks, anchors}` with `ref = AnchorReference`.
  - `steer_overviews.go`: `isOverviewVerb`; add (other-worktree refusal in the web's words; store add; `CheckAnchors`; `followDocs` so the entry exists; background / busy fallback / foreground: emit `steerWire{Cmd: "file_focus", FileID: id}` through `emitSteer` and answer `showing f7` + `; no gg web tab is open to show it` when `liveTabs() == 0`); set; list (`N overviews`); show (`OverviewWire(o, state, true)`, detail = id); rm (`closed f7`). The `state` is `shown` when a tab shows the id.
  - `steer.go`: `if isOverviewVerb(c.Cmd) { … }` beside `isNoteVerb`, before `toSteerWire`; the busy fallback is inside `steerOverviewAdd` (it reads `s.opInFlight()`).
- [ ] **Step 4:** `go test ./internal/web/ -count=1` → ok.
- [ ] **Step 5: commit** — `feat(web): overviews in gg web's list, GET /api/overview, the overview verbs answered`

---

### Task 5: The page — document mode, anchors, back

**Files:** modify `static/markdown.js`, `static/viewer.js`, `static/openfiles.js`, `static/live.js`, `static/style.css`; tests `markdownjs_test.go` (or the existing markdown JS test) and `viewernotesjs_test.go` (an overview model section).

- [ ] **Step 1: failing tests** (node, guarded sections):
  - `markdown.js`: `mdInlineHTML([{k:"anchor", url:"a.go:3", t:"0", in:[{k:"text",t:"go"}]}], esc)` → `go` (plain); with `{anchors: true}` → `<a class="md-anchor" data-a="0" href="#">go</a>`.
  - `viewer.js` overview model (`// --- overview model (pure; guarded against Go) ---`): `stepAnchor(anchors, sel, dir)` wraps and skips nothing (`[0,1,2]`, sel -1, dir 1 → 0; sel 2, dir 1 → 0; sel 0, dir -1 → 2); `anchorStatus(a)` → `no file nope.txt` / `note t9 is gone` / `""`; `anchorTarget(a)` → `{path, line, end}` (note → `{note}`).
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: implement.**
  - `markdown.js`: `mdInlineHTML(inl, esc, depth, opts)` threads `opts`; `case "anchor": out += opts && opts.anchors ? `<a class="md-anchor" data-a="${esc(str(n.t))}" href="#">${kids()}</a>` : kids();`. `mdHTML(doc, esc, opts)` passes `opts` down through `blocksHTML` → every `mdInlineHTML` call.
  - `viewer.js`: `view` gains `kind: "file" | "overview"`, `ov: {title, text, anchors, sel}`, `from: null | {id, sel}`, `range: null | {start, end}`. `openViewer` with an id whose entry `source === "overview"` → `fetchOverview(id)` (`/api/overview`) instead of `fetchContent`; `renderViewer` in overview mode paints `mdHTML({blocks}, esc, {anchors: true})` in a `.vdoc` reading column and marks `.md-anchor` elements: `sel` → `.asel`, `missing` → `.agone`. Keys in overview mode: `Tab`/`Shift+Tab` → `stepAnchor` + `scrollIntoView` of `[data-a=sel]`; `Enter` → `openAnchorAt(sel)`; `r` → `copyText(a.ref)`; `y` → `copyText(view.ov.text)`; `Escape` → `closeViewer("background")` (pinned anyway). Click on `.md-anchor` → `e.preventDefault(); openAnchorAt(Number(data-a))` (single click, ruling 2).
  - `openAnchorAt(i)`: `a.missing` → `opLine(anchorStatus(a))`, re-fetch the overview (the server re-checks); a note anchor → find the note's file: `GET /api/open-files` (its note counts) is not enough — fetch `/api/overview` anchors carry `path` for a note anchor? NO: a note's path is only in the store → the endpoint fills `path`/`start` for a live note anchor from `FindNote` (Task 4: add it to the anchor row). Then `const from = {id: view.id, sel: i}; await openViewer({src: "worktree", path, line}); view.from = from; view.range = end > line ? {start: line, end} : null; renderViewer()`.
  - Backspace in file mode with `view.from` → `const f = view.from; view.from = null; await openViewer({id: f.id}); if ok → view.ov.sel = f.sel; repaint` — when the id is gone (`ofPost focus` 404) → `opLine("the overview was closed")`.
  - Range highlight: `.vline.vrange` for `view.range.start ≤ i+1 ≤ end`; cleared when another file opens.
  - `live.js` `agentdocs`: `msg.closed` holding the shown id → `dropViewer(); opLine("overview " + id + " was closed")`; a shown overview re-fetches (text/missing may have changed) keeping `sel` by `dest`.
  - `openfiles.js`: an overview row shows its title and `overview · N anchors` (the wire's `Title`; the anchor count comes from a new `OpenFile.Anchors`? — NO wire change: show `overview` only, the count is in the TUI; Ruling if kept simpler).
  - `style.css`: `.md-anchor` (accent, underline), `.md-anchor.asel` (reverse: `background: var(--accent); color: var(--bg)`; bold), `.md-anchor.agone` (dim + line-through), `.vline.vrange` (a tint), `.vdoc` (reading column `max-width: 100ch; margin: 0 auto; padding: 8px 16px`).
  - Footer in overview mode: `tab / shift+tab anchors`, `enter open`, `r reference`, `y copy text`, `bksp back` (file mode with `from`).
  - Help: the viewer help gains the overview keys.
- [ ] **Step 4:** JS tests → ok; `go test ./internal/web/ -count=1` → ok.
- [ ] **Step 5: browser check** (playwright, visibility; old build first): standalone `gg web` → `gg session overview add --title T` with `[f](a.txt:2-3)`, `[n](note:<id>)`, `[x](nope.txt)` → the page shows the overview (the add emitted a focus) → tab selects anchor 0 (`.asel` visible) → enter opens a.txt with `.vrange` on lines 2–3 visible → Backspace returns with anchor 0 selected → a click on anchor 1 opens the noted file with its `.vnote` visible → Backspace → click on `nope.txt` → status `no file nope.txt`, `.agone` visible. Hosted: `gg --web` in a tmux session → `gg session overview add` (TUI answers) → the page's switcher lists it under the TUI's id; x in the page → `gg session overview list` says `0 overviews`.
- [ ] **Step 6: commit** — `feat(web): overviews in the browser — document mode, anchors (single click), range highlight, back`

---

### Task 6: e2e, docs, skill

- [ ] **Step 1:** `e2e/scenarios/s105_session_overview_web.toml` — `[input] web = true`; add with `--background` (stdin text `[a](a.txt:2)`), list (`f1\tbackground\t1 anchors\tT`), show, set, rm, rm again (exit 1, `no overview f1`). Run it against the CLI refusal first (RED), then GREEN.
- [ ] **Step 2: docs** — CHANGELOG ("Overviews in gg web"), README (the overview paragraph: the browser too), CLAUDE-details (overviews in the store, web endpoint, ids), `using-gg.md` (overviews work with only gg web live; one set when the TUI hosts the page), `agentskill.Version` → next, regenerate `.claude/skills/using-gg/SKILL.md`, CLAUDE.md `agentdocs` row (overviews, `markdown` import).
- [ ] **Step 3:** `./test.sh race` → "all green".
- [ ] **Step 4: commit** — `docs: overviews in gg web — changelog, readme, details, skill, package map, e2e scenario`

## Rulings made while writing the plan

- **`AddOverview` takes the worktree's directory as well as its key** — the key is case-folded on Windows/macOS; anchors are stat'ed on the real directory.
- **The note anchor's file comes from the server** (`/api/overview` fills `path`/`start` for a live note anchor) — the page has no note index of its own.
- **The web switcher shows an overview's title and `overview`, not the anchor count** — no wire change to `steer.OpenFile` (add `Anchors` only if the browser check shows the row is unclear).
