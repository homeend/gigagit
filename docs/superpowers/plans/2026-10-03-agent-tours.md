# Agent tours (stage 4) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (native, this session; NO implementer subagents — project rule). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every worker's brief and every agent report is filed as an overview document (a "tour" with anchors) in the session's worktree; the worker's row menus (TUI `.` menu, web right-click) open it, switching to that worktree first.

**Architecture:** `domain` composes a tour's title and text from the spawn record / the latest report (`AgentTour`), knows which kinds exist (`AgentTourKinds`) and owns the size cut. `agentdocs` gains a keyed file-or-replace (`Store.FileTour`) so the TUI (automatic filing) and the web endpoint (on-demand re-filing) share one store without a TUI round trip. The TUI files the brief in `onAgentSpawned`, report tours on each activity wake (a per-session seq map), and opens a tour through `reRoot` + a pending open settled with the switch (`consoleSwitch`). The web gets `has_brief`/`has_report` on the session wire, `POST /api/agent-tour`, and two rows in the session right-click menu.

**Tech Stack:** Go 1.26, Bubble Tea, vanilla JS ES modules (pure sections tested under node), Playwright.

**Spec:** `docs/superpowers/specs/2026-10-03-agent-tours-design.md`

## Global Constraints

- Worktree `/work/gigagit/.claude/worktrees/agent-tours`, branch `feat/agent-tours`; `cd` there in every command (the shell cwd resets).
- Commit: `gg add <files>` then `git commit -F <msgfile>`; messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` + `Claude-Session: https://claude.ai/code/session_01VGLjRP4v2My6Wa2d9xXfGz`. Never `git add -A`.
- Every TUI string through `i18n.T` with literal keys in ja/ko/zh/ru. Tour TITLES are composed in `domain` (English, like engine prose — they are stored data the web shows too).
- `agentdocs` stays a leaf (markdown, steer, textdiff only); `domain` does not import `agentdocs`; `tui`/`web` import both.
- Limits: tour text ≤ `agentdocs.MaxOverviewBytes` (64 KiB) — `domain.TourMaxBytes` must equal it (a test in `tui` pins it); 20 overviews per worktree.
- A tour never opens by itself and never takes the status line except to say filing failed.
- `./test.sh race` must print the literal `all green` before the merge ask.

## Rulings made while planning (spec deviations)

1. **No WebHost seam for `/api/agent-tour`** (spec §3.5 said the web asks the TUI): the keyed `Store.FileTour` lets the web re-file a tour itself in the shared store; the TUI keeps only a report-seq map. Cost if wrong: none for users; less code.
2. **Web entry = the session sub-row right-click menu only** (spec §3.3 also named the ctrl+\ switcher rows): the switcher has no row menus today; adding one is a new interaction (ask first). Cost if wrong: one more place later.
3. **The browser check opens a tour on the served worktree** (no cross-worktree switch in Playwright); the switch path is covered by a JS wiring test and the live check. Cost if wrong: a switch-path regression found live, not in CI.

## Review Focus

1. A report while the TUI is on ANOTHER worktree: the tour is filed under the worker's root (not the TUI's current one) and appears when the user switches there (Task 3 `TestReportTourFiledUnderTheWorkersWorktree`).
2. The user closes a report tour (X) and the worker reports AGAIN — a fresh tour appears; with no new report, nothing re-appears on the next wake (Task 3 `TestClosedReportTourComesBackOnlyWithANewReport`).
3. Open brief when the worker's worktree is the current one — opens at once, no reRoot (Task 4 `TestOpenTourOnTheCurrentWorktree`).
4. A 256 KiB brief (over the 64 KiB cap) — cut at a line end, valid UTF-8, ends with the cut line, ≤ cap (Task 1 `TestAgentTourCutsALongBrief`).
5. The cap (20 overviews) in the worker's worktree — the spawn still answers OK; the status line says the brief was not filed; Open brief says the same (Task 3 `TestBriefNotFiledAtTheCap`).

---

### Task 1: `domain` — tour content

**Files:**
- Create: `internal/domain/agenttour.go`, `internal/domain/agenttour_test.go`

**Interfaces:**
- Produces:
  ```go
  const TourMaxBytes = 64 << 10 // = agentdocs.MaxOverviewBytes (pinned by a tui test)
  type AgentTourDoc struct {
  	Key   string // "brief:<full id>" | "report:<full id>" — the store key
  	Root  string // CheckoutKey(Dir)
  	Dir   string // the session's worktree
  	Title string
  	Text  string
  	Seq   uint64 // report: the report's seq; brief: 0
  }
  func AgentTourKinds(full string) (brief, report bool)
  func AgentTour(full, kind string) (AgentTourDoc, error) // kind "brief" | "report"
  ```

- [ ] **Step 1: Failing tests** — `internal/domain/agenttour_test.go`:

```go
package domain

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func tourWorker(t *testing.T, brief string) (ov, worker string, sess *AgentSession) {
	t.Helper()
	_, wt, _, ov := spawnFixture(t, 4)
	res, s, err := SpawnAgent(context.Background(), SpawnSpec{Req: AgentStartRequest{Caller: ov, Worktree: wt, Tool: "Sleeper", Prompt: brief}, Cols: 80, Rows: 24, MCPURL: "http://x", Approved: approveAll})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(UseSessionStates(NewStaticStates(nil)))
	return ov, res.ID, s
}

func TestAgentTourBriefAndReport(t *testing.T) {
	ov, w, s := tourWorker(t, "# Fix the parser\nStart at [parse](internal/x/parse.go:40-60).")
	if b, r := AgentTourKinds(w); !b || r {
		t.Fatalf("kinds before a report: brief %v report %v", b, r)
	}
	if b, _ := AgentTourKinds(ov); b {
		t.Fatal("a manual session has no brief")
	}
	d, err := AgentTour(w, "brief")
	info := s.Info()
	wantTitle := "Brief — " + info.Label + " · " + filepath.Base(info.Dir) + " (" + info.Started.Local().Format("15:04") + ")"
	if err != nil || d.Key != "brief:"+w || d.Dir != info.Dir || d.Root != CheckoutKey(info.Dir) || d.Title != wantTitle || !strings.Contains(d.Text, "parse.go:40-60") {
		t.Fatalf("brief tour = %+v %v (want title %q)", d, err, wantTitle)
	}
	if _, err := AgentTour(w, "report"); err == nil || !strings.Contains(err.Error(), "has not reported") {
		t.Fatalf("report before any: %v", err)
	}
	rep, _ := AgentReportVerb(w, "done: [check](src/a.go:10-30)", false)
	d, err = AgentTour(w, "report")
	if err != nil || d.Key != "report:"+w || !strings.HasPrefix(d.Title, "Report — ") || d.Seq != rep.Seq || d.Text != rep.Text {
		t.Fatalf("report tour = %+v %v", d, err)
	}
	AgentReportVerb(w, "all done", true)
	if d, _ = AgentTour(w, "report"); !strings.HasPrefix(d.Title, "Final report — ") || d.Text != "all done" {
		t.Fatalf("final report tour = %+v", d)
	}
	if _, r := AgentTourKinds(w); !r {
		t.Fatal("kinds after a report")
	}
	if _, err := AgentTour(w, "notes"); err == nil {
		t.Fatal("unknown kind")
	}
	if _, err := AgentTour(ov, "brief"); err == nil || !strings.Contains(err.Error(), "no brief") {
		t.Fatalf("manual session brief: %v", err)
	}
}

func TestAgentTourCutsALongBrief(t *testing.T) {
	line := strings.Repeat("é", 50) + "\n" // 101 bytes, multibyte
	_, w, _ := tourWorker(t, strings.Repeat(line, 2600)) // ~262 KiB
	d, err := AgentTour(w, "brief")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Text) > TourMaxBytes || !utf8.ValidString(d.Text) || !strings.HasSuffix(d.Text, tourCutLine) {
		t.Fatalf("cut: %d bytes, valid %v, tail %q", len(d.Text), utf8.ValidString(d.Text), d.Text[len(d.Text)-80:])
	}
	body := strings.TrimSuffix(d.Text, tourCutLine)
	if !strings.HasSuffix(body, "\n") {
		t.Fatal("the cut must fall on a line end")
	}
}
```

(`spawnFixture`'s Sleeper prompt flows into `SpawnRecord.Brief` — confirm `SpawnAgent` refuses no prompt length up to `MaxBriefBytes`; 262 KiB > 256 KiB → use `2500` lines (~252 KiB) if it refuses.)

- [ ] **Step 2: Run — expect compile failure**

Run: `cd /work/gigagit/.claude/worktrees/agent-tours && go test ./internal/domain -run TestAgentTour 2>&1 | head -3`
Expected: `undefined: AgentTourKinds`.

- [ ] **Step 3: Implement** — `internal/domain/agenttour.go`:

```go
package domain

// Agent tours (stage 4): a worker's brief and an agent's latest report as
// overview documents in the session's worktree. domain composes them; the
// frontends file them in the agent-docs store (agentdocs.Store.FileTour).

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// TourMaxBytes is the overview cap a tour's text must fit (agentdocs'
// MaxOverviewBytes; a tui test pins the two equal — domain does not import
// agentdocs).
const TourMaxBytes = 64 << 10

// tourCutLine closes a brief cut to TourMaxBytes.
const tourCutLine = "… cut here — the worker has the whole brief (agent_task).\n"

// AgentTourDoc is one tour as the store files it.
type AgentTourDoc struct {
	Key   string // "brief:<full id>" | "report:<full id>"
	Root  string // CheckoutKey(Dir): the store's worktree key
	Dir   string // the session's worktree on disk
	Title string
	Text  string
	Seq   uint64 // report: its seq; brief: 0
}

// AgentTourKinds says which tours full has: a brief (a spawned worker with a
// brief) and a report (it reported at least once).
func AgentTourKinds(full string) (brief, report bool) {
	if rec, ok := AgentRecord(full); ok && rec.Spawned && strings.TrimSpace(rec.Brief) != "" {
		brief = true
	}
	_, report = latestReport(full)
	return brief, report
}

// AgentTour composes full's tour of kind "brief" or "report".
func AgentTour(full, kind string) (AgentTourDoc, error) {
	s, err := sessionOf(full)
	if err != nil {
		return AgentTourDoc{}, err
	}
	info := s.Info()
	suffix := " — " + info.Label + " · " + filepath.Base(info.Dir) + " (" + info.Started.Local().Format("15:04") + ")"
	d := AgentTourDoc{Key: kind + ":" + full, Root: CheckoutKey(info.Dir), Dir: info.Dir}
	switch kind {
	case "brief":
		rec, ok := AgentRecord(full)
		if !ok || !rec.Spawned || strings.TrimSpace(rec.Brief) == "" {
			return AgentTourDoc{}, fmt.Errorf("%s has no brief: an agent did not start it", full)
		}
		d.Title, d.Text = "Brief"+suffix, cutTourText(rec.Brief)
	case "report":
		rep, ok := latestReport(full)
		if !ok {
			return AgentTourDoc{}, fmt.Errorf("%s has not reported", full)
		}
		d.Title, d.Text, d.Seq = "Report"+suffix, rep.Text, rep.Seq
		if rep.Final {
			d.Title = "Final report" + suffix
		}
	default:
		return AgentTourDoc{}, errors.New(`kind must be "brief" or "report"`)
	}
	return d, nil
}

// cutTourText fits text into TourMaxBytes: whole lines, then tourCutLine.
func cutTourText(text string) string {
	if len(text) <= TourMaxBytes {
		return text
	}
	limit := TourMaxBytes - len(tourCutLine)
	cut := strings.LastIndexByte(text[:limit], '\n')
	if cut < 0 {
		cut = 0
	}
	return text[:cut+1] + tourCutLine
}
```

(If `cut == 0` on a single giant line the result is just the cut line — acceptable; note it in the ledger only if a test needs it.)

- [ ] **Step 4: Run**

Run: `cd /work/gigagit/.claude/worktrees/agent-tours && go test ./internal/domain -run 'TestAgentTour' 2>&1 | tail -3`
Expected: `ok`.

- [ ] **Step 5: Commit** — `gg add internal/domain/agenttour.go internal/domain/agenttour_test.go` → `feat(domain): agent tours — a worker's brief and an agent's latest report as tour documents`.

---

### Task 2: `agentdocs` — keyed `FileTour`

**Files:**
- Modify: `internal/agentdocs/overviews.go`
- Test: `internal/agentdocs/overviews_test.go` (append)

**Interfaces:**
- Produces: `func (s *Store) FileTour(key, root, dir, title, text string) (o Overview, added bool, err error)` — the overview filed under key: replaced in place (`SetOverview`) when it is still open, filed anew (`AddOverview`) when it never was or was closed. `func (s *Store) TourID(key string) (string, bool)` — the open overview for key.

- [ ] **Step 1: Failing test**

```go
func TestFileTourIsKeyed(t *testing.T) {
	s := NewStore()
	o, added, err := s.FileTour("brief:p/s1", "/r", "/r", "Brief — a", "one")
	if err != nil || !added || o.Title != "Brief — a" {
		t.Fatalf("first file: %+v %v %v", o, added, err)
	}
	o2, added, err := s.FileTour("brief:p/s1", "/r", "/r", "Brief — b", "two")
	if err != nil || added || o2.ID != o.ID || o2.Text != "two" || o2.Title != "Brief — b" {
		t.Fatalf("refile must replace in place: %+v %v %v", o2, added, err)
	}
	if id, ok := s.TourID("brief:p/s1"); !ok || id != o.ID {
		t.Fatalf("TourID = %q %v", id, ok)
	}
	s.RemoveOverview(o.ID) // the user closed it (X)
	if _, ok := s.TourID("brief:p/s1"); ok {
		t.Fatal("a closed tour has no id")
	}
	o3, added, err := s.FileTour("brief:p/s1", "/r", "/r", "Brief — c", "three")
	if err != nil || !added || o3.ID == o.ID {
		t.Fatalf("a closed tour is filed anew: %+v %v %v", o3, added, err)
	}
	if got := len(s.Overviews("/r")); got != 1 {
		t.Fatalf("overviews = %d, want 1", got)
	}
}

func TestFileTourAtTheCap(t *testing.T) {
	s := NewStore()
	for i := 0; i < MaxOverviewsPerRoot; i++ {
		if _, err := s.AddOverview("/r", "/r", "o", "x"); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.FileTour("report:p/s1", "/r", "/r", "Report", "x"); err == nil || !strings.Contains(err.Error(), "overviews are open") {
		t.Fatalf("cap: %v", err)
	}
}
```

(Use the package's real constructor — `NewStore()` or whatever `Shared()` builds; check `store.go`.)

- [ ] **Step 2: Run** — `go test ./internal/agentdocs -run TestFileTour 2>&1 | head -3` → `undefined: FileTour`.

- [ ] **Step 3: Implement** — in `overviews.go` add a `tours map[string]string // key -> overview id` field to `Store` (init where the other maps are made), and:

```go
// FileTour files or replaces the overview a caller keeps under key (an
// agent tour: "brief:<session>", "report:<session>"): replaced in place while
// it is open, filed anew when it never was or the user closed it. added
// says which.
func (s *Store) FileTour(key, root, dir, title, text string) (Overview, bool, error) {
	if id, ok := s.TourID(key); ok {
		o, err := s.SetOverview(id, title, text)
		if err == nil {
			return o, false, nil
		}
		// closed between the two calls: file it anew
	}
	o, err := s.AddOverview(root, dir, title, text)
	if err != nil {
		return Overview{}, false, err
	}
	s.mu.Lock()
	s.tours[key] = o.ID
	s.mu.Unlock()
	return o, true, nil
}

// TourID is the open overview filed under key.
func (s *Store) TourID(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.tours[key]
	if !ok {
		return "", false
	}
	if _, open := s.overviews[id]; !open {
		delete(s.tours, key)
		return "", false
	}
	return id, true
}
```

- [ ] **Step 4: Run** — `go test ./internal/agentdocs 2>&1 | tail -2` → `ok`; `go test ./internal/archtest 2>&1 | tail -1` → `ok`.

- [ ] **Step 5: Commit** — `gg add internal/agentdocs/overviews.go internal/agentdocs/overviews_test.go` (+ store.go if the map init lives there) → `feat(agentdocs): FileTour — a keyed overview, replaced in place, filed anew once closed`.

---

### Task 3: TUI — file the brief and the report tours

**Files:**
- Create: `internal/tui/agent_tours.go`, `internal/tui/agent_tours_test.go`
- Modify: `internal/tui/agenthost.go` (`agentSpawnedMsg` already carries `id`; `onAgentSpawned` calls `fileBriefTour`), `internal/tui/session_activity.go` (`onSessionActivity` calls `fileReportTours`), `internal/tui/model.go` (pointer field `tourSeq *map[domain.SessionID]uint64` initialised in `New` next to `actSeq`), i18n ×4

**Interfaces:**
- Consumes: `domain.AgentTour`, `domain.FullSessionID`, `domain.TourMaxBytes`, `agentdocs.Store.FileTour`, `m.docs`.
- Produces: `func (m Model) fileTour(id domain.SessionID, kind string) (agentdocs.Overview, error)`, `func (m Model) fileBriefTour(id domain.SessionID) Model`, `func (m Model) fileReportTours() Model`.
- i18n keys: `"brief not filed: %s"`, `"report not filed: %s"`.

- [ ] **Step 1: Failing tests** — `internal/tui/agent_tours_test.go`. Build a Model the way `session_activity_test.go`'s `reportingSession` does, plus a spawned worker: use `domain.Open(dir).StartAgentSession(ctx, tc, wtDir, "", 80, 24, nil, "", domain.SpawnRecord{Parent: "x/ov", Brief: "# B\nsee [a](a.go:1)", Spawned: true}, "")` and a store `agentdocs.NewStore()` set on the Model (`m.docs = store`; find how tests set `m.docs`, e.g. `grep -n 'docs:' internal/tui/*_test.go`). Tests:

```go
func TestTourCapIsTheOverviewCap(t *testing.T) {
	if domain.TourMaxBytes != agentdocs.MaxOverviewBytes {
		t.Fatalf("domain.TourMaxBytes %d != agentdocs.MaxOverviewBytes %d", domain.TourMaxBytes, agentdocs.MaxOverviewBytes)
	}
}

func TestBriefTourFiledOnSpawn(t *testing.T)            // onAgentSpawned(agentSpawnedMsg{id: worker, res: ...}) → store.Overviews(CheckoutKey(wtDir)) has one titled "Brief — …"; m.statusMsg is the usual "started in" line
func TestReportTourFiledUnderTheWorkersWorktree(t *testing.T) // m.currentWorktree = main dir; AgentReportVerb(worker,…) ; m.fileReportTours() → overview under the WORKER's root, none under main
func TestSecondReportReplacesTheTour(t *testing.T)     // two reports → one overview, text = second, title "Final report — …" when final
func TestClosedReportTourComesBackOnlyWithANewReport(t *testing.T) // report → file; RemoveOverview(id); fileReportTours() again → still none; new report → one again
func TestBriefNotFiledAtTheCap(t *testing.T)           // 20 AddOverview under the worker root first; onAgentSpawned → reply sent (read it from msg.reply), status line contains "brief not filed: 20 overviews are open"
```

Write each with real assertions (store contents, titles, status line); the reply channel in `onAgentSpawned` must be buffered (`make(chan agentSpawnReply, 1)`).

- [ ] **Step 2: Run** — `go test ./internal/tui -run 'Tour' 2>&1 | head -5` → compile failures.

- [ ] **Step 3: Implement** — `internal/tui/agent_tours.go`:

```go
package tui

// Agent tours (stage 4): the TUI files a spawned worker's brief and every
// agent report as overview documents in the session's worktree
// (agentdocs.Store.FileTour, keyed per session), in the background — they
// never open by themselves. Opening them: agent_tours_open.go.

import (
	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// fileTour composes id's tour of kind and files (or replaces) it.
func (m Model) fileTour(id domain.SessionID, kind string) (agentdocs.Overview, error) {
	d, err := domain.AgentTour(domain.FullSessionID(id), kind)
	if err != nil {
		return agentdocs.Overview{}, err
	}
	o, _, err := m.docs.FileTour(d.Key, d.Root, d.Dir, d.Title, d.Text)
	return o, err
}

// fileBriefTour files a just-spawned worker's brief; a refusal (the cap)
// only says so on the status line — the worker runs either way.
func (m Model) fileBriefTour(id domain.SessionID) Model {
	if m.docs == nil || id == "" {
		return m
	}
	if _, err := m.fileTour(id, "brief"); err != nil {
		m.statusMsg = i18n.T("brief not filed: %s", err.Error())
	}
	return m
}

// fileReportTours files the report tour of every session whose latest
// report is newer than the one last filed — the level, not the notices, so a
// burst past the notice ring is not missed. A tour the user closed comes
// back only with a NEW report.
func (m Model) fileReportTours() Model {
	if m.docs == nil || m.tourSeq == nil {
		return m
	}
	for _, info := range domain.Sessions().List() {
		d, err := domain.AgentTour(domain.FullSessionID(info.ID), "report")
		if err != nil || d.Seq <= (*m.tourSeq)[info.ID] {
			continue
		}
		(*m.tourSeq)[info.ID] = d.Seq
		if _, _, err := m.docs.FileTour(d.Key, d.Root, d.Dir, d.Title, d.Text); err != nil {
			m.statusMsg = i18n.T("report not filed: %s", err.Error())
		}
	}
	return m
}
```

`onAgentSpawned`: after the existing status line: `m = m.fileBriefTour(msg.id)`. `onSessionActivity`: before the notice loop, `m = m.fileReportTours()` (it already returns early in quiet mode → no report tours headless, per spec). `model.go`: `tourSeq *map[domain.SessionID]uint64` with comment; `New`: `ts := map[domain.SessionID]uint64{}; m.tourSeq = &ts`. The refusal error text (`20 overviews are open; remove one first`) is English store prose inside a translated frame — the `friendlyOpError` precedent.

Bundles (next to `"needs input"`): ja `"brief not filed: %s" = "ブリーフを登録できません: %s"`, `"report not filed: %s" = "レポートを登録できません: %s"`; ko `"브리프를 등록하지 못했습니다: %s"`, `"보고서를 등록하지 못했습니다: %s"`; zh `"未能登记任务说明: %s"`, `"未能登记报告: %s"`; ru `"бриф не добавлен: %s"`, `"отчёт не добавлен: %s"`.

- [ ] **Step 4: Run** — `go test ./internal/tui -run 'Tour|TestSessionBadge|I18n|CheckVerbs' 2>&1 | tail -3` → `ok`.

- [ ] **Step 5: Commit** — `gg add internal/tui/agent_tours.go internal/tui/agent_tours_test.go internal/tui/agenthost.go internal/tui/session_activity.go internal/tui/model.go internal/i18n/lang/*.toml` → `feat(tui): a worker's brief and every report filed as tours in its worktree`.

---

### Task 4: TUI — Open brief / Open report

**Files:**
- Create: `internal/tui/agent_tours_open.go`
- Modify: `internal/tui/agent_start_popup.go` (`sessionMenuRows`), `internal/tui/console_scope.go` (`consoleSwitch.tour string`; `settleConsoleAfterSwitch` opens it), `internal/tui/help.go` (one row), i18n ×4
- Test: `internal/tui/agent_tours_test.go`

**Interfaces:**
- Consumes: Task 3 `fileTour`; `m.findOverview(id)`; `m.reRoot`; `checkSwitchTarget`; `m.steerRefusal()`; `m.syncOverviews()`.
- Produces: `func (m Model) openTour(id domain.SessionID, kind string) (Model, tea.Cmd)`; menu row ids `session-brief`, `session-report`.
- i18n keys: `"Open brief"`, `"Open report"`, `"no tour to open: %s"`, `"A worker's session row (. menu) opens its brief and its latest report as tours — gg switches to the worker's worktree first; anchors jump into its files"`.

- [ ] **Step 1: Failing tests** (append):

```go
func TestOpenTourOnTheCurrentWorktree(t *testing.T)   // currentWorktree = worker dir; openTour(worker,"brief") → no reRoot (m.consoleSwitch.tour == ""), top layer is a *fileViewer whose doc id == the tour's overview id
func TestOpenTourSwitchesWorktree(t *testing.T)       // currentWorktree = main; openTour → m.consoleSwitch.tour == overview id and armed; then simulate the switch landing: m.currentWorktree = workerDir; m = m.syncOverviews(); m, _ = m.settleConsoleAfterSwitch() → top layer is that tour's viewer; consoleSwitch.tour cleared
func TestOpenTourRefilesAClosedTour(t *testing.T)     // file, RemoveOverview, openTour → a new overview exists and is shown
func TestSessionMenuShowsTourRows(t *testing.T)       // spawned worker selected → rows include session-brief; after a report also session-report; a manual session without reports → neither
```

(Use the existing test helpers that select a session sub-row — see `branch_session_rows_test.go` / `branch_session_subrows_test.go` for `selectedSession` setups. If `reRoot` cannot run in a unit Model, assert on what `openTour` records instead and test `settleConsoleAfterSwitch` separately, as described.)

- [ ] **Step 2: Run** — `go test ./internal/tui -run 'OpenTour|SessionMenuShowsTourRows' 2>&1 | head -5` → FAIL (undefined).

- [ ] **Step 3: Implement** — `agent_tours_open.go`:

```go
package tui

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// openTour opens id's brief or report tour: filed again when the user
// closed it, shown at once on the current worktree, else after switching to
// the session's worktree (settled in settleConsoleAfterSwitch).
func (m Model) openTour(id domain.SessionID, kind string) (Model, tea.Cmd) {
	o, err := m.fileTour(id, kind)
	if err != nil {
		m.statusMsg = i18n.T("no tour to open: %s", err.Error())
		return m, nil
	}
	s, ok := domain.Sessions().Get(id)
	if !ok {
		return m, nil
	}
	dir := s.Info().Dir
	if filepath.Clean(dir) == filepath.Clean(m.currentWorktree) {
		m = m.syncOverviews()
		return m.showTour(o.ID), nil
	}
	if why := m.steerRefusal(); why != "" {
		m.statusMsg = why
		return m, nil
	}
	if verdict, _ := checkSwitchTarget(guardStat, guardGOOS, dir); verdict != switchOK {
		m.statusMsg = i18n.T("cannot switch: %s is not reachable from here", dir)
		return m, nil
	}
	nm, cmd := m.reRoot(dir)
	m = nm.(Model)
	m.consoleSwitch.tour = o.ID
	return m, cmd
}

// showTour pushes the viewer on the current worktree's overview id.
func (m Model) showTour(ovID string) Model {
	if d, _ := m.findOverview(ovID); d != nil {
		return m.pushLayer(&fileViewer{d})
	}
	return m
}
```

(Check `steerRefusal()` returns translated text; if it returns a protocol string, use the `openSessionAnywhere` refusal sentence instead: `i18n.T("that session runs in another repository — close what is open first, then open it again")` — or a new key `"close what is open first, then open the tour again"` in all four bundles.)

`console_scope.go`: `consoleSwitch` gains `tour string // an agent tour to open once the switch has loaded`; at the top of `settleConsoleAfterSwitch` (after the snapshot sync already ran in model.go:1746): `if t := m.consoleSwitch.tour; t != "" { m.consoleSwitch.tour = ""; m = m.showTour(t) }` — make sure the existing `m.consoleSwitch = consoleSwitch{}` reset does not drop it before use (read it first). Also `reRoot` path: confirm `consoleSwitch.armed` is set by reRoot (so settle runs); if not, set `armed = true` here.

`sessionMenuRows`: after `session-open`:

```go
		brief, report := domain.AgentTourKinds(domain.FullSessionID(id))
		if brief {
			rows = append(rows, actionRow{id: "session-brief", label: i18n.T("Open brief"), run: func(m Model) (tea.Model, tea.Cmd) {
				return m.openTour(id, "brief")
			}})
		}
		if report {
			rows = append(rows, actionRow{id: "session-report", label: i18n.T("Open report"), run: func(m Model) (tea.Model, tea.Cmd) {
				return m.openTour(id, "report")
			}})
		}
```

Help row next to the session-badge rows. Bundles:
- ja: `"Open brief" = "ブリーフを開く"`, `"Open report" = "レポートを開く"`, `"no tour to open: %s" = "開けるツアーがありません: %s"`, help `"ワーカーのセッション行 (. メニュー) からブリーフと最新レポートをツアーとして開けます — gg は先にワーカーのワークツリーへ切り替え、アンカーでそのファイルへ移動します"`
- ko: `"브리프 열기"`, `"보고서 열기"`, `"열 투어가 없습니다: %s"`, help `"워커의 세션 행(. 메뉴)에서 브리프와 최신 보고서를 투어로 엽니다 — gg는 먼저 워커의 워크트리로 전환하고 앵커로 그 파일로 이동합니다"`
- zh: `"打开任务说明"`, `"打开报告"`, `"没有可打开的导览: %s"`, help `"在工作代理的会话行 (. 菜单) 中以导览形式打开其任务说明和最新报告 —— gg 先切换到该代理的工作树，锚点可跳转到其文件"`
- ru: `"Открыть бриф"`, `"Открыть отчёт"`, `"нечего открыть: %s"`, help `"Строка сессии работника (меню .) открывает его бриф и последний отчёт как обзор — gg сначала переключается на рабочее дерево работника, якоря ведут в его файлы"`

- [ ] **Step 4: Run** — `go test ./internal/tui 2>&1 | tail -2` (whole package: menu-label / i18n gates) → passes.

- [ ] **Step 5: Commit** — `gg add internal/tui/agent_tours_open.go internal/tui/agent_tours_test.go internal/tui/agent_start_popup.go internal/tui/console_scope.go internal/tui/help.go internal/i18n/lang/*.toml` → `feat(tui): Open brief / Open report on a worker's row — switch to its worktree, show the tour`.

---

### Task 5: Web — wire, endpoint, menu rows, open after the switch

**Files:**
- Modify: `internal/web/sessions_http.go` (`sessionWire.HasBrief/HasReport`; `sessionsWire` fills them; new handler `handleAgentTour`), the mux registration file (where `/api/session-input` is registered), `internal/web/static/sessions.js` (pure `sessionMenuRows` + action), `internal/web/static/app.js` (boot: `openPendingTour()` after `applyStartAt`), `internal/web/attach_browser_test.go` (Worker report gains an anchor)
- Test: `internal/web/session_states_test.go` or a new `agenttour_http_test.go`, `internal/web/sessionsjs_test.go`; Playwright `scratchpad/pw/checktour.mjs`

**Interfaces:**
- Consumes: `domain.AgentTourKinds`, `domain.AgentTour`, `s.docs.FileTour` (the server's agent-docs store field — check its name in `server.go`).
- Produces: wire `has_brief`, `has_report` (omitempty bools); `POST /api/agent-tour {id, kind}` → `200 {overview, worktree}`, `404` unknown session, `409` no such tour (`has not reported` / `no brief`), `400` bad kind, `507`-free: the cap refusal is `409` with the store's sentence; JS `sessionMenuRows(s)` rows `{id:"brief",label:"Open brief"}` / `{id:"report",label:"Open report"}` first when present.

- [ ] **Step 1: Failing Go test** (`internal/web/agenttour_http_test.go`): start a session through `domain.Open(dir).StartAgentSession(ctx, tc, root, "", 80, 24, nil, "", domain.SpawnRecord{Parent: "x/ov", Brief: "# B\n[a](a.go:1)", Spawned: true}, "")` in a served repo (follow `session_states_test.go`'s server setup), then:
  - `GET /api/sessions` → that entry has `"has_brief":true`, no `has_report`;
  - `POST /api/agent-tour {"id": <local id>, "kind":"brief"}` → 200, `overview` non-empty, `worktree` == root; a second POST returns the SAME overview id;
  - `kind:"report"` before a report → 409 with "has not reported"; after `domain.AgentReportVerb` → 200;
  - `kind:"x"` → 400; unknown id → 404.
- [ ] **Step 2: Failing JS tests** — in `sessionsjs_test.go` extend the pure-model script: `sessionMenuRows({state:"running", has_brief:true, has_report:true}).map(r=>r.id).join(",")` → `brief,report,kill,killrm`; exited with report → `report,remove`. Add wiring rows to the file's wiring table: `{"sessions.js", "/api/agent-tour", "the tour rows ask the server to file the tour"}`, `{"sessions.js", "gg-open-tour", "a tour on another worktree opens after the switch's reload"}`, `{"app.js", "openPendingTour()", "boot opens a tour a switch left pending"}`.
- [ ] **Step 3: Run** — `go test ./internal/web -run 'AgentTour|Sessions' 2>&1 | tail -5` → FAIL.
- [ ] **Step 4: Implement**
  - `sessionWire`: `HasBrief bool \`json:"has_brief,omitempty"\``, `HasReport bool \`json:"has_report,omitempty"\``; in `sessionsWire` after `sessionsWireWith(...)`: `for i := range out { out[i].HasBrief, out[i].HasReport = domain.AgentTourKinds(domain.FullSessionID(domain.SessionID(out[i].ID))) }`.
  - Handler (POST only, the guards the other session handlers use):
    ```go
    // handleAgentTour files (or finds) a session's brief / report tour in the
    // agent-docs store and says where it lives; the page switches there and
    // opens it (stage 4).
    func (s *Server) handleAgentTour(w http.ResponseWriter, r *http.Request) {
    	var in struct{ ID, Kind string }
    	if !decodeBody(w, r, &in) { return }   // the package's JSON-body helper
    	if in.Kind != "brief" && in.Kind != "report" { httpError(w, 400, `kind must be "brief" or "report"`); return }
    	full := domain.FullSessionID(domain.SessionID(in.ID))
    	if _, ok := domain.Sessions().Get(domain.SessionID(in.ID)); !ok { httpError(w, 404, "no session "+in.ID); return }
    	d, err := domain.AgentTour(full, in.Kind)
    	if err != nil { httpError(w, 409, err.Error()); return }
    	o, _, err := s.docs.FileTour(d.Key, d.Root, d.Dir, d.Title, d.Text)
    	if err != nil { httpError(w, 409, err.Error()); return }
    	writeJSON(w, map[string]string{"overview": o.ID, "worktree": d.Dir})
    }
    ```
    (Use the real helper names from neighbouring handlers.)
  - `sessions.js` pure model:
    ```js
    function sessionMenuRows(s) {
      const tours = [];
      if (s.has_brief) tours.push({ id: "brief", label: "Open brief" });
      if (s.has_report) tours.push({ id: "report", label: "Open report" });
      return tours.concat(s.state === "exited"
        ? [{ id: "remove", label: "Remove session" }]
        : [{ id: "kill", label: "Kill session" }, { id: "killrm", label: "Kill and remove session" }]);
    }
    ```
    impure side: `registerRows("session", …)` maps `brief`/`report` → `openTour(s, r.id)` with `danger: false`; kill/remove keep `danger: r.id !== "remove"` as today (adjust the expression so tour rows are not danger).
    ```js
    // openTour asks the server to file the tour, then opens it — after
    // switching the page (a hosted page: the terminal too) when it lives in
    // another worktree; the reload reads the one-shot key back at boot.
    async function openTour(s, kind) {
      let r;
      try { r = await postJSON("/api/agent-tour", { id: s.id, kind }); }
      catch (e) { toast(String(e.message || e)); return; }
      if (state.worktree && r.worktree !== state.worktree) {
        ssSet("gg-open-tour", r.overview);
        doReroot(r.worktree);
        return;
      }
      openViewer({ id: r.overview });
    }
    function openPendingTour() {
      const id = ssGet("gg-open-tour");
      if (!id) return;
      ssSet("gg-open-tour", "");
      openViewer({ id }).catch(() => {});
    }
    ```
    (Imports: `doReroot` from ops.js, `openViewer` from viewer.js, `ssGet/ssSet/state/postJSON` from core.js, `toast` from toast.js — check which are already imported; check `ssSet` semantics for clearing.) Export `openPendingTour`; `app.js` imports it and calls it after `applyStartAt()` in `boot` (`applyStartAt().catch(() => {}).then(openPendingTour)` or a line after it).
  - `attach_browser_test.go`: the Worker's report text gains an anchor line: `"merged feat/x — two tests skipped\nsee [the log](README.md:1)"` (check the fixture repo file name in `newRepoDir`).
- [ ] **Step 5: Run** — `go test ./internal/web 2>&1 | tail -2` → `ok`.
- [ ] **Step 6: Browser check** — `scratchpad/pw/checktour.mjs` (copy `checkreport.mjs`'s prologue): wait for the Worker row to read `done`; right-click it; the menu shows **Open report** (visible); click it; the viewer shows the tour title `Final report — Worker · …` (visible) and an anchor `the log` (visible); no page errors. Run on the branch (expect all PASS) and on the build before this task (detached temp worktree at Task 4's commit + the new host test) — expect FAIL on the menu row. Ledger both.
- [ ] **Step 7: Commit** — `gg add internal/web/sessions_http.go internal/web/<mux file> internal/web/agenttour_http_test.go internal/web/static/sessions.js internal/web/static/app.js internal/web/sessionsjs_test.go internal/web/attach_browser_test.go` → `feat(web): Open brief / Open report on a session row — the page switches to the worker's worktree and opens the tour`.

---

### Task 6: Skill v132, docs, gates

**Files:** `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go` (Version 131 → 132), `.claude/skills/using-gg/SKILL.md` (via `go run ./cmd/gg init --update`), `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`, `CLAUDE.md` (`agentdocs` row: "keyed tours (`FileTour`)"; `domain` row: "agent tours")

- [ ] **Step 1: Skill** — under `agent_start`: "Write the brief as markdown and link the files and lines the worker should start from with repo-relative anchors (`[the parser](internal/x/parse.go:40-60)`, the overview anchor grammar) — the user reads it as a tour of the worker's worktree." Under `agent_report`: "Write it as markdown: the first line a one-line summary (rows and notices show it), then anchors to what you changed, most important first (`[new check](src/a.go:10-30)`) — the user opens it as a tour."
- [ ] **Step 2: Bump + sync + gate** — `sed -i 's/const Version = 131/const Version = 132/' internal/agentskill/agentskill.go && go run ./cmd/gg init --update >/dev/null && go test ./internal/agentskill ./internal/cli -run 'Skill|Dogfood' 2>&1 | tail -2` → ok.
- [ ] **Step 3: Docs** — CHANGELOG (Added: briefs and reports as tours; Open brief / Open report in the TUI `.` menu and the web session menu; skill v132), README (two sentences in the agent orchestration paragraph), CLAUDE-details section "Agent orchestration — tours (stage 4)" (keys, FileTour semantics, seq map, the three planning rulings), CLAUDE.md rows.
- [ ] **Step 4: Gates** — `./test.sh > <scratch>/gate-4.log 2>&1; grep -c 'all green' …` → 1; then `./test.sh race` → literal `all green`.
- [ ] **Step 5: Commit** — docs commit.

---

### Task 7: Live check (manual)

- [ ] The 3b shell stand-in setup (scratch repo + isolated XDG, tmux `claude-4`): overseer `gg agent start --worktree job --tool Shell --prompt-file brief.md` where `brief.md` has `# Task` + `[readme](README.md:1)` (create README.md in the scratch repo); worker reports `--final` with an anchor. From the main worktree: worker row `.` → **Open brief** → TUI switches to `job` and shows `Brief — Shell · job (HH:MM)` with the anchor; enter on the anchor opens README.md:1; esc; `.` → **Open report** shows `Final report — …`. X closes it; Open report re-files. Palette → Open in browser: the session row's right-click menu has both rows; Open report switches the page (and the terminal) and shows the tour. Read every screen before each key. Ledger the outcome; no commit unless something is fixed.

---

## Self-review

- Spec coverage: §3.1 brief tour → Tasks 1, 3; §3.2 report tour → 1, 3; §3.3 TUI opening → 4, web opening → 5 (switcher rows: planning ruling 2); §3.4 limits → 1 (cut), 3 (cap), 2 (cap error); §3.5 who files → 2, 3 (+ planning ruling 1); §4 skill → 6; §5 tests → per task + 7.
- Placeholders: Task 3/4 test bodies are specified by assertions per test name rather than full code where the Model fixture must be discovered in the codebase; each names its exact assertions.
- Type consistency: `AgentTourDoc{Key,Root,Dir,Title,Text,Seq}` used in Tasks 3 and 5; `FileTour` returns `(Overview, bool, error)` everywhere; menu ids `session-brief`/`session-report` (TUI) and `brief`/`report` (web) are separate vocabularies by design.
- Review Focus 1–5 each name a test in its owning task.
