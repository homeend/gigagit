# Agent wait + report channel (stage 3b) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (native, this session; NO implementer subagents — project rule) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A parent agent blocks on `agent_wait` until a worker it started goes idle, asks a question, exits or reports; a worker hands its result back with `agent_report`; the human sees `reported`/`done` on the worker's row and one notice.

**Architecture:** `agentsession.Session` learns `LastInput()` (the `LastOutput` twin). `domain` grows a report store + per-caller delivery marks inside the spawn registry (`agentreport.go`) and a level-triggered, consumed-once long-poll `AgentWait` (`agentwait.go`) woken by the state watcher, the session manager and the report store. MCP gets `agent_wait`/`agent_report` (host + forwarder), the CLI their twins, TUI + web a badge rule read from one domain helper (`SessionReportOf`) and a `report` notice kind on the existing ring.

**Tech Stack:** Go 1.26, go-sdk MCP, Bubble Tea, vanilla JS (pure sections tested under node), Playwright for the browser check.

**Spec:** `docs/superpowers/specs/2026-10-03-agent-wait-report-design.md`

## Global Constraints

- Worktree `/work/gigagit/.claude/worktrees/agent-wait-report`, branch `feat/agent-wait-report`; every command `cd`s there (the shell cwd resets).
- Commit: `gg add <files>` then `git commit -F <msgfile>`; message ends with `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` + `Claude-Session: https://claude.ai/code/session_01VGLjRP4v2My6Wa2d9xXfGz`. Never `git add -A`.
- Every TUI string through `i18n.T` with a literal key in ja/ko/zh/ru (gates: `go test ./internal/i18n/ ./internal/tui/ -run 'I18n|EngineProse|DecisionOptionValues|ActionMenuLabels|CheckVerbs'`).
- `internal/agentsession` stays a DAG leaf; `domain` is the only owner of the report store; `tui`/`web`/`cli`/`mcp` never import `agentsession` for this (archtest).
- Protocol words are English and fixed: events `idle | question | exit | report`; `until` adds `any`; wire fields `report_at`, `report_final`, `report_line`, `timed_out`.
- Limits: `MaxReportBytes = MaxSendBytes` (64 KiB), 20 reports kept per session, `timeout_s` 1 … 600, default 45, `idleSettle` 2 s.
- No sending keys to a live real agent whose screen was not read first (live verification task).
- `./test.sh race` must print the literal "all green" line before the merge ask.

## Review Focus

1. A worker that exits **inside** the 30 s grace, having reported — the parent's wait must deliver `report` then `exit` even though no notice was ever posted (Task 3 test `TestAgentWaitExitedWorkerReportThenExit`).
2. Parent sends, worker ignores it, screen unchanged — wait must NOT return the stale idle; it times out with the old `since` (Task 3 `TestAgentWaitFreshness`).
3. Two concurrent `any` waits by one parent — each event delivered to exactly one of them, no double delivery, no deadlock (Task 3 `TestAgentWaitTwoWaitersSplitEvents`).
4. A human answers a worker's question in the TUI console (input not via `agent_send`) — the parent's pending `question` becomes stale, and the row's report badge clears on that input (Task 1 `TestLastInputStampedByEveryInputPath`, Task 6 `TestSessionBadgeClearsOnInput`).
5. Report text with CR/LF/ANSI noise and a 64 KiB body — the notice/first line is one clean line ≤ 120 runes and the store keeps the body intact (Task 2 `TestReportFirstLine`).

---

### Task 1: `Session.LastInput()`

**Files:**
- Modify: `internal/agentsession/session.go` (struct field + accessor near `LastOutput`)
- Modify: `internal/agentsession/io.go` (`SendKey`, `SendText`, `Paste`)
- Test: `internal/agentsession/session_test.go` (append)

**Interfaces:**
- Produces: `func (s *Session) LastInput() time.Time` — zero before the first input; set by every input path, never by `Touch`.

- [ ] **Step 1: Write the failing test**

Append to `internal/agentsession/session_test.go` (use the file's existing shell-session helper; if none fits, start `sh -c 'sleep 60'` through `NewManager().Start` the way `TestManagerStart…` does):

```go
func TestLastInputStampedByEveryInputPath(t *testing.T) {
	m := NewManager()
	s, err := m.Start(StartSpec{Label: "sh", Dir: t.TempDir(), Cols: 80, Rows: 24, Argv: []string{"sh", "-c", "sleep 60"}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.KillAll(context.Background())
	if !s.LastInput().IsZero() {
		t.Fatal("LastInput must be zero before any input")
	}
	s.Touch()
	if !s.LastInput().IsZero() {
		t.Fatal("Touch (a frontend showing the session) is not input")
	}
	before := time.Now()
	s.SendText("a")
	t1 := s.LastInput()
	if t1.Before(before) {
		t.Fatalf("SendText did not stamp: %v", t1)
	}
	time.Sleep(2 * time.Millisecond)
	s.Paste("b")
	t2 := s.LastInput()
	if !t2.After(t1) {
		t.Fatal("Paste did not restamp")
	}
	time.Sleep(2 * time.Millisecond)
	s.SendKey(Key{Code: uv.KeyEnter})
	if !s.LastInput().After(t2) {
		t.Fatal("SendKey did not restamp")
	}
}
```

(Match the real `StartSpec` field names and the `Key` type alias used in `io.go`; the test file's imports already have `uv`/`time`/`context` or add them.)

- [ ] **Step 2: Run it — expect a compile failure**

Run: `cd /work/gigagit/.claude/worktrees/agent-wait-report && go test ./internal/agentsession -run TestLastInputStampedByEveryInputPath 2>&1 | head -5`
Expected: `s.LastInput undefined`.

- [ ] **Step 3: Implement**

`session.go`: next to `lastOut atomic.Int64` add `lastIn atomic.Int64 // UnixNano of the latest input (SendKey/SendText/Paste)`; after `LastOutput` add:

```go
// LastInput is when anyone last typed into the child (SendKey, SendText,
// Paste — a parent's agent_send and a human's console alike); the zero
// time before the first input. Touch is not input.
func (s *Session) LastInput() time.Time {
	n := s.lastIn.Load()
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}
```

`io.go`: in `SendKey` (after the `running` check), `SendText` and `Paste` (inside the `if s.running()`), add `s.lastIn.Store(time.Now().UnixNano())` right after `s.Touch()`.

- [ ] **Step 4: Run the package tests**

Run: `cd /work/gigagit/.claude/worktrees/agent-wait-report && go test ./internal/agentsession 2>&1 | tail -3`
Expected: `ok`.

- [ ] **Step 5: Commit**

`gg add internal/agentsession/session.go internal/agentsession/io.go internal/agentsession/session_test.go` → `feat(agentsession): Session.LastInput — when anyone last typed into the child`.

---

### Task 2: Report store, `agent_report` verb, notice, level reads

**Files:**
- Create: `internal/domain/agentreport.go`
- Modify: `internal/domain/agentspawn.go` (registry fields + `prune`)
- Modify: `internal/domain/agentverbs.go` (`AgentEntry`, `AgentScreenResult`, `AgentList`, `AgentScreen`)
- Modify: `internal/domain/session_states.go` (`ActivityNotice.Text`; `PostNotice` must `Signal` — verify)
- Test: `internal/domain/agentreport_test.go`

**Interfaces:**
- Produces:
  - `type AgentReport struct { Seq uint64; Text string; Final bool; At time.Time }` (json `seq,text,final,at`)
  - `const MaxReportBytes = MaxSendBytes`, `const maxReportsKept = 20`
  - `func AgentReportVerb(caller, text string, final bool) (AgentReport, error)`
  - `func AgentReports(target string) ([]AgentReport, error)` (oldest first, copy)
  - `func latestReport(full string) (AgentReport, bool)`
  - `func SessionReportOf(id SessionID) (AgentReport, bool)` — latest report while unanswered (`At` after the session's `LastInput`, or no input yet)
  - `func ReportFirstLine(text string) string` (exported: TUI + web use it; ≤ 120 runes, one line)
  - registry: `reports map[string][]AgentReport`, `reportSeq uint64`, `marks map[string]map[string]deliveryMark`, `bc agentsession.Broadcaster`; `type deliveryMark struct { idleSince, questionSince time.Time; reportSeq uint64; exit bool }`; `func (r *spawnRegistry) markOf(caller, worker string) deliveryMark`, `func (r *spawnRegistry) setMark(caller, worker string, m deliveryMark)` (both under `r.mu`)
  - `AgentEntry.ReportAt time.Time` (`report_at,omitzero`), `AgentEntry.ReportFinal bool` (`report_final,omitempty`); `AgentScreenResult.Report *AgentReport`
  - `ActivityNotice.Text string` (kind `report`)

- [ ] **Step 1: Write the failing tests**

`internal/domain/agentreport_test.go`:

```go
package domain

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestAgentReportStoredAndRead(t *testing.T) {
	_, wt, _, ov := spawnFixture(t, 4)
	res, sess, err := SpawnAgent(context.Background(), SpawnSpec{Req: AgentStartRequest{Caller: ov, Worktree: wt, Tool: "Sleeper", Prompt: "x"}, Cols: 80, Rows: 24, MCPURL: "http://x", Approved: approveAll})
	if err != nil {
		t.Fatal(err)
	}
	restore := UseSessionStates(NewStaticStates(nil))
	defer restore()
	seq0 := SessionStates().NoticeSeq()
	rep, err := AgentReportVerb(res.ID, "merged feat/x\ntwo tests skipped\n", false)
	if err != nil || rep.Seq == 0 || rep.Final || rep.Text != "merged feat/x\ntwo tests skipped" {
		t.Fatalf("report = %+v %v", rep, err)
	}
	if _, err := AgentReportVerb(res.ID, "  \n", false); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("an empty report must be refused: %v", err)
	}
	if _, err := AgentReportVerb(res.ID, strings.Repeat("x", MaxReportBytes+1), false); err == nil {
		t.Fatal("an oversized report must be refused")
	}
	list, err := AgentReports(res.ID)
	if err != nil || len(list) != 1 || list[0].Seq != rep.Seq {
		t.Fatalf("reports = %+v %v", list, err)
	}
	if got, ok := SessionReportOf(sess.Info().ID); !ok || got.Seq != rep.Seq {
		t.Fatalf("SessionReportOf = %+v %v", got, ok)
	}
	// Level reads.
	var row AgentEntry
	for _, e := range AgentList(ov) {
		if e.ID == res.ID {
			row = e
		}
	}
	if row.ReportAt.IsZero() || row.ReportFinal {
		t.Fatalf("row %+v", row)
	}
	sc, _ := AgentScreen(res.ID)
	if sc.Report == nil || sc.Report.Seq != rep.Seq {
		t.Fatalf("screen report = %+v", sc.Report)
	}
	// One notice of kind report, with the first line.
	ns := SessionStates().Notices(seq0)
	if len(ns) != 1 || ns[0].Kind != "report" || ns[0].Text != "merged feat/x" || ns[0].ID != sess.Info().ID {
		t.Fatalf("notices = %+v", ns)
	}
	// Answered: input after the report clears the badge view, not the store.
	time.Sleep(2 * time.Millisecond)
	sess.SendText("ok")
	if _, ok := SessionReportOf(sess.Info().ID); ok {
		t.Fatal("an answered report must not show on the row")
	}
	if list, _ = AgentReports(res.ID); len(list) != 1 {
		t.Fatal("answering must not drop the report")
	}
	// A final report replaces the badge; a later non-final one is kept too.
	fin, _ := AgentReportVerb(res.ID, "done", true)
	if got, ok := SessionReportOf(sess.Info().ID); !ok || !got.Final || got.Seq != fin.Seq {
		t.Fatalf("final badge = %+v %v", got, ok)
	}
	for _, e := range AgentList(ov) {
		if e.ID == res.ID && !e.ReportFinal {
			t.Fatal("report_final must follow the latest report")
		}
	}
}

func TestAgentReportCapKeepsSeqs(t *testing.T) {
	_, wt, _, ov := spawnFixture(t, 4)
	res, _, err := SpawnAgent(context.Background(), SpawnSpec{Req: AgentStartRequest{Caller: ov, Worktree: wt, Tool: "Sleeper", Prompt: "x"}, Cols: 80, Rows: 24, MCPURL: "http://x", Approved: approveAll})
	if err != nil {
		t.Fatal(err)
	}
	defer UseSessionStates(NewStaticStates(nil))()
	for i := 0; i < maxReportsKept+5; i++ {
		if _, err := AgentReportVerb(res.ID, "r", false); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := AgentReports(res.ID)
	if len(list) != maxReportsKept || list[0].Seq != 6 || list[len(list)-1].Seq != uint64(maxReportsKept+5) {
		t.Fatalf("kept %d, first %d, last %d", len(list), list[0].Seq, list[len(list)-1].Seq)
	}
}

func TestAgentReportRefusesExitedAndPrunesOnRemove(t *testing.T) {
	_, wt, _, ov := spawnFixture(t, 4)
	res, sess, err := SpawnAgent(context.Background(), SpawnSpec{Req: AgentStartRequest{Caller: ov, Worktree: wt, Tool: "Sleeper", Prompt: "x"}, Cols: 80, Rows: 24, MCPURL: "http://x", Approved: approveAll})
	if err != nil {
		t.Fatal(err)
	}
	defer UseSessionStates(NewStaticStates(nil))()
	if _, err := AgentReportVerb(res.ID, "before exit", true); err != nil {
		t.Fatal(err)
	}
	_ = Sessions().Kill(sess.Info().ID)
	<-sess.Done()
	if _, err := AgentReportVerb(res.ID, "after exit", false); err == nil || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("an exited session cannot report: %v", err)
	}
	if list, _ := AgentReports(res.ID); len(list) != 1 {
		t.Fatal("reports must outlive the exit")
	}
	_ = Sessions().Remove(sess.Info().ID)
	if _, err := AgentReports(res.ID); err == nil {
		t.Fatal("a removed session has no reports")
	}
	r := registry()
	r.mu.Lock()
	r.prune()
	_, kept := r.reports[res.ID]
	r.mu.Unlock()
	if kept {
		t.Fatal("prune must drop the removed session's reports")
	}
}

func TestReportFirstLine(t *testing.T) {
	cases := map[string]string{
		"merged\r\nnext":                 "merged",
		"\n\n  lead blank\nx":            "lead blank",
		"\x1b[31mred\x1b[0m tail\n":      "red tail",
		strings.Repeat("é", 130):          strings.Repeat("é", 119) + "…",
		"":                                "",
	}
	for in, want := range cases {
		if got := ReportFirstLine(in); got != want {
			t.Errorf("ReportFirstLine(%q) = %q, want %q", in, got, want)
		}
	}
}
```

(`Sessions().Remove` — use the Manager's actual remove method name, `Remove` or `KillAndRemove`; check `internal/agentsession/manager.go`.)

- [ ] **Step 2: Run — expect compile failures**

Run: `cd /work/gigagit/.claude/worktrees/agent-wait-report && go test ./internal/domain -run 'TestAgentReport|TestReportFirstLine' 2>&1 | head -5`
Expected: `undefined: AgentReportVerb` (and friends).

- [ ] **Step 3: Implement the store**

`internal/domain/agentspawn.go` — extend the registry:

```go
type spawnRegistry struct {
	mu      sync.Mutex
	tokens  map[string]string // token -> full session id
	records map[string]SpawnRecord
	pending int // reserved slots whose start has not finished
	// Stage 3b: what workers reported and what each caller's wait already
	// delivered. Pruned with the records (a removed session forgets both).
	reports   map[string][]AgentReport        // full id -> oldest first, ≤ maxReportsKept
	reportSeq uint64                          // process-global, monotonic
	marks     map[string]map[string]deliveryMark // caller -> worker -> delivered
	bc        agentsession.Broadcaster        // wakes waiters on a report
}

func newSpawnRegistry() *spawnRegistry {
	return &spawnRegistry{tokens: map[string]string{}, records: map[string]SpawnRecord{},
		reports: map[string][]AgentReport{}, marks: map[string]map[string]deliveryMark{}}
}
```

In `prune()`, after the tokens loop:

```go
	for id := range r.reports {
		if !listed[id] {
			delete(r.reports, id)
		}
	}
	for caller, byWorker := range r.marks {
		if !listed[caller] {
			delete(r.marks, caller)
			continue
		}
		for id := range byWorker {
			if !listed[id] {
				delete(byWorker, id)
			}
		}
	}
```

Create `internal/domain/agentreport.go`:

```go
package domain

// The report channel (agent orchestration stage 3b): a worker's result
// sentence, stored on its session and read by the parent's agent_wait, by
// agent_list/agent_screen and by the human on the session row.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// AgentReport is one agent_report.
type AgentReport struct {
	Seq   uint64    `json:"seq"`
	Text  string    `json:"text"`
	Final bool      `json:"final"`
	At    time.Time `json:"at"`
}

// MaxReportBytes caps one report's text; maxReportsKept the reports kept
// per session (the oldest drops; Seq stays monotonic so delivery marks
// survive the drop).
const (
	MaxReportBytes = MaxSendBytes
	maxReportsKept = 20
	reportLineMax  = 120
)

// deliveryMark is what one caller's agent_wait already returned about one
// worker: the Since of the idle/question it delivered, the highest report
// seq, and whether the exit went out.
type deliveryMark struct {
	idleSince, questionSince time.Time
	reportSeq                uint64
	exit                     bool
}

func (r *spawnRegistry) markOf(caller, worker string) deliveryMark { // under r.mu
	return r.marks[caller][worker]
}

func (r *spawnRegistry) setMark(caller, worker string, m deliveryMark) { // under r.mu
	if r.marks[caller] == nil {
		r.marks[caller] = map[string]deliveryMark{}
	}
	r.marks[caller][worker] = m
}

// AgentReportVerb records caller's report about itself and tells the
// waiters and the human. Only a running session reports (its token stops
// working at exit anyway).
func AgentReportVerb(caller, text string, final bool) (AgentReport, error) {
	text = strings.TrimRight(text, "\r\n")
	if strings.TrimSpace(text) == "" {
		return AgentReport{}, errors.New("the report is empty")
	}
	if len(text) > MaxReportBytes {
		return AgentReport{}, fmt.Errorf("the report is larger than %d bytes", MaxReportBytes)
	}
	s, err := sessionOf(caller)
	if err != nil {
		return AgentReport{}, err
	}
	info := s.Info()
	if info.State != SessionRunning {
		return AgentReport{}, fmt.Errorf("%s has exited", caller)
	}
	r := registry()
	r.mu.Lock()
	r.prune()
	r.reportSeq++
	rep := AgentReport{Seq: r.reportSeq, Text: text, Final: final, At: time.Now()}
	list := append(r.reports[caller], rep)
	if len(list) > maxReportsKept {
		list = list[len(list)-maxReportsKept:]
	}
	r.reports[caller] = list
	r.mu.Unlock()
	r.bc.Signal()
	SessionStates().PostNotice(ActivityNotice{ID: info.ID, Kind: "report", Label: info.Label, Dir: info.Dir, Text: ReportFirstLine(text)})
	return rep, nil
}

// AgentReports is every kept report of target, oldest first.
func AgentReports(target string) ([]AgentReport, error) {
	if _, err := sessionOf(target); err != nil {
		return nil, err
	}
	r := registry()
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]AgentReport(nil), r.reports[target]...), nil
}

// latestReport is target's newest report.
func latestReport(full string) (AgentReport, bool) {
	r := registry()
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.reports[full]
	if len(list) == 0 {
		return AgentReport{}, false
	}
	return list[len(list)-1], true
}

// SessionReportOf is the report the session's row shows: the latest one,
// while nobody has typed into the session since (spec §5.1 — a report is
// news until somebody talks to the worker). Frontends add the question
// precedence themselves.
func SessionReportOf(id SessionID) (AgentReport, bool) {
	s, ok := Sessions().Get(id)
	if !ok {
		return AgentReport{}, false
	}
	rep, ok := latestReport(FullSessionID(id))
	if !ok || s.LastInput().After(rep.At) {
		return AgentReport{}, false
	}
	return rep, true
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// ReportFirstLine is the notice/switcher line: the first non-blank line,
// control sequences stripped, cut to reportLineMax runes with an ellipsis.
func ReportFirstLine(text string) string {
	text = ansiRe.ReplaceAllString(text, "")
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(strings.ReplaceAll(line, "\r", ""))
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) <= reportLineMax {
			return line
		}
		rs := []rune(line)
		return string(rs[:reportLineMax-1]) + "…"
	}
	return ""
}
```

`internal/domain/session_states.go`: add `Text string // report: its first line` to `ActivityNotice`; confirm `PostNotice` → `post` signals `w.bc` (read `post`; if it does not, add `w.bc.Signal()` after appending to the ring).

`internal/domain/agentverbs.go`:
- `AgentEntry`: after `Stalled` add
  ```go
	// The latest agent_report, when any: when it came and whether the
	// worker called it final.
	ReportAt    time.Time `json:"report_at,omitzero"`
	ReportFinal bool      `json:"report_final,omitempty"`
  ```
- `AgentScreenResult`: add `Report *AgentReport // the latest report, nil when none`.
- `AgentList`: after the activity block: `if rep, ok := latestReport(full); ok { e.ReportAt, e.ReportFinal = rep.At, rep.Final }`.
- `AgentScreen`: before `return res, nil`: `if rep, ok := latestReport(target); ok { res.Report = &rep }`.

- [ ] **Step 4: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/agent-wait-report && go test ./internal/domain -run 'TestAgentReport|TestReportFirstLine|TestSpawn|TestAgentList|TestStates' 2>&1 | tail -5`
Expected: `ok`.

- [ ] **Step 5: Commit**

`gg add internal/domain/agentreport.go internal/domain/agentreport_test.go internal/domain/agentspawn.go internal/domain/agentverbs.go internal/domain/session_states.go` → `feat(domain): the report channel — agent_report stored on the worker, read by list/screen, one notice`.

---

### Task 3: `AgentWait`

**Files:**
- Create: `internal/domain/agentwait.go`
- Modify: `internal/domain/session_states.go` (`idleSettle` var + `UseIdleSettle` seam; test helper `setStatic` lives in the test file)
- Test: `internal/domain/agentwait_test.go`

**Interfaces:**
- Consumes: Task 1 `Session.LastInput()`; Task 2 registry fields, `markOf/setMark`, `latestReport`, `AgentReport`.
- Produces:
  ```go
  type AgentWaitResult struct {
  	ID       string           `json:"id,omitempty"`
  	Event    string           `json:"event,omitempty"` // idle | question | exit | report
  	TimedOut bool             `json:"timed_out,omitempty"`
  	Activity string           `json:"activity,omitempty"`
  	Since    time.Time        `json:"since,omitzero"`
  	Stalled  bool             `json:"stalled,omitempty"`
  	Options  []ActivityOption `json:"options,omitempty"`
  	ExitCode *int             `json:"exit_code,omitempty"`
  	Report   *AgentReport     `json:"report,omitempty"`
  }
  const WaitDefaultTimeout = 45 * time.Second; const WaitMaxTimeout = 600 * time.Second
  func AgentWait(ctx context.Context, caller, target, until string, timeout time.Duration) (AgentWaitResult, error)
  func UseIdleSettle(d time.Duration) func()
  ```

- [ ] **Step 1: Write the failing tests**

`internal/domain/agentwait_test.go` (the static watcher is mutated under its own lock + signalled — an in-package helper):

```go
package domain

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// setStatic changes a static watcher's view of one session and wakes its
// subscribers — the test's stand-in for the classifier.
func setStatic(w *StateWatcher, id SessionID, a SessionActivity) {
	w.mu.Lock()
	w.states[id] = a
	w.mu.Unlock()
	w.bc.Signal()
}

// waitFixture: an overseer with two workers and a static watcher.
func waitFixture(t *testing.T) (ov string, w1, w2 string, s1, s2 *AgentSession, w *StateWatcher) {
	t.Helper()
	_, wt, _, ov := spawnFixture(t, 6)
	main := Sessions().List()[0].Dir
	spawn := func(dir string) (string, *AgentSession) {
		res, sess, err := SpawnAgent(context.Background(), SpawnSpec{Req: AgentStartRequest{Caller: ov, Worktree: dir, Tool: "Sleeper", Prompt: "x"}, Cols: 80, Rows: 24, MCPURL: "http://x", Approved: approveAll})
		if err != nil {
			t.Fatal(err)
		}
		return res.ID, sess
	}
	w1, s1 = spawn(wt)
	w2, s2 = spawn(addWT(t, main, "job2"))
	w = NewStaticStates(nil)
	t.Cleanup(UseSessionStates(w))
	t.Cleanup(UseIdleSettle(50 * time.Millisecond))
	return
}

func wait(t *testing.T, caller, id, until string, d time.Duration) AgentWaitResult {
	t.Helper()
	res, err := AgentWait(context.Background(), caller, id, until, d)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAgentWaitFreshness(t *testing.T) {
	ov, w1, _, s1, _, w := waitFixture(t)
	// Idle entered BEFORE the parent's input: stale → times out.
	setStatic(w, s1.Info().ID, SessionActivity{State: ActivityIdle, Since: time.Now().Add(-time.Minute)})
	time.Sleep(2 * time.Millisecond)
	s1.SendText("go")
	res := wait(t, ov, w1, "idle", 300*time.Millisecond)
	if !res.TimedOut || res.Activity != "idle" || res.ID != w1 {
		t.Fatalf("stale idle must not be delivered: %+v", res)
	}
	// Idle entered AFTER the input, held past the settle: delivered once.
	setStatic(w, s1.Info().ID, SessionActivity{State: ActivityIdle, Since: time.Now()})
	res = wait(t, ov, w1, "idle", time.Second)
	if res.TimedOut || res.Event != "idle" {
		t.Fatalf("fresh idle: %+v", res)
	}
	if res = wait(t, ov, w1, "idle", 200*time.Millisecond); !res.TimedOut {
		t.Fatalf("the same idle must not be delivered twice: %+v", res)
	}
}

func TestAgentWaitIdleSettle(t *testing.T) {
	ov, w1, _, s1, _, w := waitFixture(t)
	t.Cleanup(UseIdleSettle(400 * time.Millisecond))
	start := time.Now()
	setStatic(w, s1.Info().ID, SessionActivity{State: ActivityIdle, Since: start})
	res := wait(t, ov, w1, "idle", 2*time.Second)
	if res.TimedOut || time.Since(start) < 350*time.Millisecond {
		t.Fatalf("idle delivered before it settled: %+v after %v", res, time.Since(start))
	}
}

func TestAgentWaitQuestionCarriesOptions(t *testing.T) {
	ov, w1, _, s1, _, w := waitFixture(t)
	done := make(chan AgentWaitResult, 1)
	go func() { done <- wait(t, ov, w1, "any", 3*time.Second) }()
	time.Sleep(100 * time.Millisecond)
	setStatic(w, s1.Info().ID, SessionActivity{State: ActivityQuestion, Since: time.Now(), Options: []ActivityOption{{Key: "1", Label: "Yes"}}})
	res := <-done
	if res.Event != "question" || len(res.Options) != 1 || res.Activity != "question" {
		t.Fatalf("%+v", res)
	}
}

func TestAgentWaitOrderReportExitQuestionIdle(t *testing.T) {
	ov, w1, _, s1, _, w := waitFixture(t)
	setStatic(w, s1.Info().ID, SessionActivity{State: ActivityQuestion, Since: time.Now()})
	if _, err := AgentReportVerb(w1, "half way", false); err != nil {
		t.Fatal(err)
	}
	if res := wait(t, ov, w1, "any", time.Second); res.Event != "report" || res.Report == nil || res.Report.Text != "half way" {
		t.Fatalf("report first: %+v", res)
	}
	if res := wait(t, ov, w1, "any", time.Second); res.Event != "question" {
		t.Fatalf("then the question: %+v", res)
	}
	_ = Sessions().Kill(s1.Info().ID)
	<-s1.Done()
	if res := wait(t, ov, w1, "any", time.Second); res.Event != "exit" || res.ExitCode == nil {
		t.Fatalf("then the exit: %+v", res)
	}
	if res := wait(t, ov, w1, "any", 200*time.Millisecond); !res.TimedOut {
		t.Fatalf("nothing left: %+v", res)
	}
}

func TestAgentWaitExitedWorkerReportThenExit(t *testing.T) {
	// Review focus 1: reported and exited inside the grace — no notice ever
	// existed, the wait still delivers both.
	ov, w1, _, s1, _, _ := waitFixture(t)
	if _, err := AgentReportVerb(w1, "done", true); err != nil {
		t.Fatal(err)
	}
	_ = Sessions().Kill(s1.Info().ID)
	<-s1.Done()
	if res := wait(t, ov, w1, "any", time.Second); res.Event != "report" || !res.Report.Final {
		t.Fatalf("%+v", res)
	}
	if res := wait(t, ov, w1, "exit", time.Second); res.Event != "exit" {
		t.Fatalf("%+v", res)
	}
}

func TestAgentWaitAnyIsDirectChildrenOnly(t *testing.T) {
	ov, w1, w2, s1, s2, w := waitFixture(t)
	setStatic(w, s2.Info().ID, SessionActivity{State: ActivityIdle, Since: time.Now()})
	if res := wait(t, ov, "", "idle", time.Second); res.ID != w2 || res.Event != "idle" {
		t.Fatalf("any → the idle child: %+v", res)
	}
	// A grandchild's idle is not the overseer's business.
	gc, gs, err := SpawnAgent(context.Background(), SpawnSpec{Req: AgentStartRequest{Caller: w1, Worktree: Sessions().List()[0].Dir, Tool: "Sleeper", Prompt: "x"}, Cols: 80, Rows: 24, MCPURL: "http://x", Approved: approveAll})
	if err != nil && !strings.Contains(err.Error(), "may not start") {
		t.Fatal(err)
	}
	if err == nil {
		setStatic(w, gs.Info().ID, SessionActivity{State: ActivityIdle, Since: time.Now()})
		if res := wait(t, ov, "", "idle", 300*time.Millisecond); !res.TimedOut {
			t.Fatalf("grandchild %s delivered to the overseer: %+v", gc, res)
		}
		// … but an explicit id reaches any descendant.
		if res := wait(t, ov, gc, "idle", time.Second); res.Event != "idle" {
			t.Fatalf("%+v", res)
		}
	}
	_ = s1
}

func TestAgentWaitRefusals(t *testing.T) {
	ov, w1, _, _, _, _ := waitFixture(t)
	if _, err := AgentWait(context.Background(), w1, "", "any", time.Second); err == nil || !strings.Contains(err.Error(), "no workers") {
		t.Fatalf("a childless any-wait: %v", err)
	}
	if _, err := AgentWait(context.Background(), ov, w1, "soon", time.Second); err == nil {
		t.Fatal("unknown until")
	}
	if _, err := AgentWait(context.Background(), ov, w1, "any", WaitMaxTimeout+time.Second); err == nil {
		t.Fatal("timeout over the cap")
	}
	if _, err := AgentWait(context.Background(), w1, ov, "any", time.Second); err == nil || !strings.Contains(err.Error(), "not an agent you started") {
		t.Fatalf("reach: %v", err)
	}
	if _, err := AgentWait(context.Background(), ov, "p/none", "any", time.Second); err == nil {
		t.Fatal("unknown session")
	}
}

func TestAgentWaitCtxCancelReturnsPromptly(t *testing.T) {
	ov, w1, _, _, _, _ := waitFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := AgentWait(ctx, ov, w1, "any", 30*time.Second); done <- err }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled wait must fail, not time out")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the wait outlived its ctx")
	}
}

func TestAgentWaitTwoWaitersSplitEvents(t *testing.T) {
	// Review focus 3: two any-waits of one parent; each event to exactly one.
	ov, _, _, s1, s2, w := waitFixture(t)
	var mu sync.Mutex
	var got []string
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := wait(t, ov, "", "idle", 3*time.Second)
			mu.Lock()
			got = append(got, res.ID+":"+res.Event)
			mu.Unlock()
		}()
	}
	time.Sleep(100 * time.Millisecond)
	setStatic(w, s1.Info().ID, SessionActivity{State: ActivityIdle, Since: time.Now()})
	setStatic(w, s2.Info().ID, SessionActivity{State: ActivityIdle, Since: time.Now()})
	wg.Wait()
	if len(got) != 2 || got[0] == got[1] || strings.Contains(got[0]+got[1], "timed") {
		t.Fatalf("%v", got)
	}
}

func TestAgentWaitDefaultTimeout(t *testing.T) {
	if _, err := parseWaitUntil("any"); err != nil {
		t.Fatal(err)
	}
	if d := clampWaitTimeout(0); d != WaitDefaultTimeout {
		t.Fatalf("default = %v", d)
	}
}
```

- [ ] **Step 2: Run — expect compile failures**

Run: `cd /work/gigagit/.claude/worktrees/agent-wait-report && go test ./internal/domain -run TestAgentWait 2>&1 | head -5`
Expected: `undefined: AgentWait`.

- [ ] **Step 3: Implement**

`session_states.go`, in the timing `var` block: `idleSettle = 2 * time.Second // an idle younger than this is not yet a turn's end (flicker guard)` and

```go
// UseIdleSettle replaces the wait's idle settle (tests) and returns the restore.
func UseIdleSettle(d time.Duration) func() {
	statesMu.Lock()
	prev := idleSettle
	idleSettle = d
	statesMu.Unlock()
	return func() { statesMu.Lock(); idleSettle = prev; statesMu.Unlock() }
}
```

Create `internal/domain/agentwait.go`:

```go
package domain

// agent_wait (stage 3b): a parent's long-poll on its workers. Level-
// triggered — the predicate is re-evaluated on every wake over the state
// watcher, the session lifecycle and the report store — and consumed once
// per (caller, worker): wait returns what is new; agent_list says what is.

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// AgentWaitResult is agent_wait's answer: one event, or a timeout carrying
// the named worker's current activity.
type AgentWaitResult struct {
	ID       string           `json:"id,omitempty"`
	Event    string           `json:"event,omitempty"` // idle | question | exit | report
	TimedOut bool             `json:"timed_out,omitempty"`
	Activity string           `json:"activity,omitempty"`
	Since    time.Time        `json:"since,omitzero"`
	Stalled  bool             `json:"stalled,omitempty"`
	Options  []ActivityOption `json:"options,omitempty"`
	ExitCode *int             `json:"exit_code,omitempty"`
	Report   *AgentReport     `json:"report,omitempty"`
}

const (
	WaitDefaultTimeout = 45 * time.Second
	WaitMaxTimeout     = 600 * time.Second
	waitTick           = 500 * time.Millisecond // the settle clock; events wake the loop themselves
)

var waitEvents = []string{"report", "exit", "question", "idle"} // delivery order

func parseWaitUntil(s string) (map[string]bool, error) {
	want := map[string]bool{}
	switch s {
	case "", "any":
		for _, e := range waitEvents {
			want[e] = true
		}
	case "idle", "question", "exit", "report":
		want[s] = true
	default:
		return nil, fmt.Errorf("until must be idle, question, exit, report or any (not %q)", s)
	}
	return want, nil
}

func clampWaitTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return WaitDefaultTimeout
	}
	return d
}

// AgentWait blocks until target (or, with target "", any direct child of
// caller) has a new event among until, or timeout passes. A ctx end is an
// error (the client hung up), a timeout a normal answer.
func AgentWait(ctx context.Context, caller, target, until string, timeout time.Duration) (AgentWaitResult, error) {
	want, err := parseWaitUntil(until)
	if err != nil {
		return AgentWaitResult{}, err
	}
	timeout = clampWaitTimeout(timeout)
	if timeout > WaitMaxTimeout {
		return AgentWaitResult{}, fmt.Errorf("timeout_s is 1 … %d", int(WaitMaxTimeout.Seconds()))
	}
	candidates := func() []string { return childrenOf(caller) }
	if target != "" {
		if err := reach(caller, target); err != nil {
			return AgentWaitResult{}, err
		}
		candidates = func() []string { return []string{target} }
	} else if len(candidates()) == 0 {
		return AgentWaitResult{}, errors.New("you have no workers: nothing to wait for")
	}
	w := SessionStates()
	sch, scancel := w.Subscribe()
	defer scancel()
	mch, mcancel := Sessions().Subscribe()
	defer mcancel()
	rch, rcancel := registry().bc.Subscribe()
	defer rcancel()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	tick := time.NewTicker(waitTick)
	defer tick.Stop()
	for {
		for _, id := range candidates() {
			res, ok, err := nextWaitEvent(w, caller, id, want, time.Now())
			if err != nil {
				if target != "" {
					return AgentWaitResult{}, err
				}
				continue // a child removed mid-wait
			}
			if ok {
				return res, nil
			}
		}
		select {
		case <-ctx.Done():
			return AgentWaitResult{}, ctx.Err()
		case <-timer.C:
			res := AgentWaitResult{TimedOut: true}
			if target != "" {
				res.ID = target
				if s, err := sessionOf(target); err == nil {
					if a, ok := w.Get(s.Info().ID); ok {
						res.Activity, res.Since, res.Stalled = a.Name(), a.Since, a.Stalled
					}
				}
			}
			return res, nil
		case <-sch:
		case <-mch:
		case <-rch:
		case <-tick.C:
		}
	}
}

// childrenOf: caller's direct children in start order.
func childrenOf(caller string) []string {
	r := registry()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune()
	var out []string
	for _, in := range Sessions().List() {
		full := FullSessionID(in.ID)
		if rec, ok := r.records[full]; ok && rec.Parent == caller {
			out = append(out, full)
		}
	}
	return out
}

// nextWaitEvent is the first undelivered event of id for caller, in
// delivery order; ok false when there is none. The mark is advanced under
// the registry lock so two waiters never return the same event.
func nextWaitEvent(w *StateWatcher, caller, id string, want map[string]bool, now time.Time) (AgentWaitResult, bool, error) {
	s, err := sessionOf(id)
	if err != nil {
		return AgentWaitResult{}, false, err
	}
	info := s.Info()
	res := AgentWaitResult{ID: id}
	a, classified := w.Get(info.ID)
	if classified {
		res.Activity, res.Since, res.Stalled, res.Options = a.Name(), a.Since, a.Stalled, a.Options
	}
	lastIn := s.LastInput()
	if lastIn.IsZero() {
		lastIn = info.Started
	}
	statesMu.Lock()
	settle := idleSettle
	statesMu.Unlock()

	r := registry()
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.markOf(caller, id)
	if want["report"] {
		for _, rep := range r.reports[id] {
			if rep.Seq > m.reportSeq {
				m.reportSeq = rep.Seq
				r.setMark(caller, id, m)
				rep := rep
				res.Event, res.Report = "report", &rep
				return res, true, nil
			}
		}
	}
	if want["exit"] && info.State == SessionExited && !m.exit {
		m.exit = true
		r.setMark(caller, id, m)
		code := info.ExitCode
		res.Event, res.ExitCode = "exit", &code
		return res, true, nil
	}
	if !classified || !a.Since.After(lastIn) {
		return AgentWaitResult{}, false, nil
	}
	if want["question"] && a.State == ActivityQuestion && !m.questionSince.Equal(a.Since) {
		m.questionSince = a.Since
		r.setMark(caller, id, m)
		res.Event = "question"
		return res, true, nil
	}
	if want["idle"] && a.State == ActivityIdle && now.Sub(a.Since) >= settle && !m.idleSince.Equal(a.Since) {
		m.idleSince = a.Since
		r.setMark(caller, id, m)
		res.Event = "idle"
		return res, true, nil
	}
	return AgentWaitResult{}, false, nil
}
```

(Note `Options` on a non-question result: clear them unless `res.Event == "question"` — add `if res.Event != "question" { res.Options = nil }` before each non-question return, or simply assign `res.Options` only in the question branch.)

- [ ] **Step 4: Run the tests (also with -race)**

Run: `cd /work/gigagit/.claude/worktrees/agent-wait-report && go test -race ./internal/domain -run 'TestAgentWait|TestAgentReport' 2>&1 | tail -5`
Expected: `ok`. If `TestAgentWaitAnyIsDirectChildrenOnly` cannot spawn a grandchild (spawned agents may not spawn), the `err == nil` guard skips that half — acceptable; the direct-children half must pass.

- [ ] **Step 5: Commit**

`gg add internal/domain/agentwait.go internal/domain/agentwait_test.go internal/domain/session_states.go` → `feat(domain): AgentWait — a parent's long-poll on its workers, fresh since the last input, delivered once`.

---

### Task 4: MCP `agent_wait` + `agent_report` (host and forwarder)

**Files:**
- Modify: `internal/mcp/agenttools.go`, `internal/mcp/agentforward.go`, `internal/mcp/agenthost.go` (the instructions string mentions the two verbs)
- Test: `internal/mcp/agenthost_test.go`, `internal/mcp/agentforward_test.go`

**Interfaces:**
- Consumes: `domain.AgentWait`, `domain.AgentReportVerb`, `domain.AgentWaitResult`, `domain.AgentReport`.
- Produces: tools `agent_wait {id?, until?, timeout_s?}` → `domain.AgentWaitResult`; `agent_report {text, final?}` → `domain.AgentReport`; `agentScreenOut.Report *domain.AgentReport` (`report,omitempty`).

- [ ] **Step 1: Write the failing tests**

Append to `internal/mcp/agenthost_test.go`:

```go
func TestAgentWaitAndReportOverHTTP(t *testing.T) {
	url, tok, full := hostEnv(t, nil)
	cs, err := agentClient(t, url, tok)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	// A manual session reports about itself; the report is on its screen result.
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "agent_report", Arguments: map[string]any{"text": "merged feat/x", "final": true}})
	if err != nil || res.IsError || !strings.Contains(resultText(res), `"final":true`) {
		t.Fatalf("agent_report = %v %s", err, resultText(res))
	}
	res, _ = cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "agent_screen", Arguments: map[string]any{"id": full}})
	if res.IsError || !strings.Contains(resultText(res), `"report":{`) {
		t.Fatalf("agent_screen = %s", resultText(res))
	}
	// A childless any-wait is refused; a wait on a stranger is refused; a
	// short wait on oneself times out with the shape.
	res, _ = cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "agent_wait", Arguments: map[string]any{"timeout_s": 1}})
	if !res.IsError || !strings.Contains(resultText(res), "no workers") {
		t.Fatalf("any-wait = %s", resultText(res))
	}
	res, _ = cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "agent_wait", Arguments: map[string]any{"id": full, "until": "idle", "timeout_s": 1}})
	if !res.IsError || !strings.Contains(resultText(res), "not an agent you started") {
		t.Fatalf("wait on oneself = %s", resultText(res))
	}
	res, _ = cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "agent_wait", Arguments: map[string]any{"id": full, "until": "later"}})
	if !res.IsError || !strings.Contains(resultText(res), "until must be") {
		t.Fatalf("bad until = %s", resultText(res))
	}
}
```

Extend `TestForwarderCallsTheTUI` (or add `TestForwarderHasWaitAndReport`) to assert `toolNames` contains `agent_wait` and `agent_report`.

- [ ] **Step 2: Run — expect failures**

Run: `cd /work/gigagit/.claude/worktrees/agent-wait-report && go test ./internal/mcp -run 'TestAgentWaitAndReportOverHTTP|TestForwarder' 2>&1 | tail -5`
Expected: FAIL (`agent_report` unknown tool / names missing).

- [ ] **Step 3: Implement**

`agenttools.go`:

```go
type agentWaitIn struct {
	ID       string `json:"id,omitempty" jsonschema:"a session id; omitted: any worker you started"`
	Until    string `json:"until,omitempty" jsonschema:"idle | question | exit | report | any (default)"`
	TimeoutS int    `json:"timeout_s,omitempty" jsonschema:"seconds to wait, 1-600 (default 45); on timed_out simply call again"`
}
type agentReportIn struct {
	Text  string `json:"text" jsonschema:"your result for whoever started you: what changed, what was skipped, what they must do (up to 64 KiB)"`
	Final bool   `json:"final,omitempty" jsonschema:"this is your last word on the task (the row says done)"`
}
```

`agentScreenOut`: add `Report *domain.AgentReport \`json:"report,omitempty" jsonschema:"the agent's latest agent_report"\``; set it in the handler.

In `RegisterAgentTools`:

```go
	sdk.AddTool(srv, toolAgentWait(),
		func(ctx context.Context, req *sdk.CallToolRequest, in agentWaitIn) (*sdk.CallToolResult, domain.AgentWaitResult, error) {
			who, err := caller(req)
			if err != nil {
				return nil, domain.AgentWaitResult{}, err
			}
			res, err := domain.AgentWait(ctx, who, in.ID, in.Until, time.Duration(in.TimeoutS)*time.Second)
			return nil, res, err
		})
	sdk.AddTool(srv, toolAgentReport(),
		func(_ context.Context, req *sdk.CallToolRequest, in agentReportIn) (*sdk.CallToolResult, domain.AgentReport, error) {
			who, err := caller(req)
			if err != nil {
				return nil, domain.AgentReport{}, err
			}
			return domain.AgentReportVerb(who, in.Text, in.Final)
		})
```

Wrap the last `return` to match the three-value shape. Tool defs:

```go
func toolAgentWait() *sdk.Tool {
	return &sdk.Tool{Name: "agent_wait", Description: "Block until a worker you started has news: idle (its turn ended), question (it waits for a decision — options included), exit, or report (its agent_report). Returns one event, each once; timed_out means call again.", Annotations: readOnlyAnnotations()}
}
func toolAgentReport() *sdk.Tool {
	return &sdk.Tool{Name: "agent_report", Description: "Report your result to whoever started you (and to the user's session row). final marks your last word; you stay running until killed."}
}
```

`agentforward.go`: `addForward[agentWaitIn](srv, s.agent, toolAgentWait())`, `addForward[agentReportIn](srv, s.agent, toolAgentReport())`. Update the comment "six agent tools" → "eight". `agenthost.go` instructions string: append "A worker reports its result with agent_report; a parent waits for it with agent_wait."

- [ ] **Step 4: Run the package**

Run: `cd /work/gigagit/.claude/worktrees/agent-wait-report && go test ./internal/mcp 2>&1 | tail -3`
Expected: `ok`.

- [ ] **Step 5: Commit**

`gg add internal/mcp/agenttools.go internal/mcp/agentforward.go internal/mcp/agenthost.go internal/mcp/agenthost_test.go internal/mcp/agentforward_test.go` → `feat(mcp): agent_wait and agent_report over the agent channel`.

---

### Task 5: CLI `gg agent wait` / `report` / `screen --reports` / list column

**Files:**
- Modify: `internal/cli/agent.go`
- Test: `internal/cli/agent_test.go`

**Interfaces:**
- Consumes: the two tools; `domain.AgentWaitResult` JSON shape; `domain.AgentReport`.
- Produces: verbs `wait [<id>] [--until <w>] [--timeout <s>]` (exit 0 event / 3 timeout / 1 failure / 2 usage-refusal), `report [--final] [-F <file>|-F -] [<text>…]`, `screen <id> [--reports]`; `list` text rows gain `reported`/`done` after the stalled column when `report_at` is set.

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/agent_test.go`:

```go
func TestAgentReportAndWaitCLI(t *testing.T) {
	dir, full := agentEnvFor(t, nil)
	code, out, _ := runAgentCLI(t, dir, "", "report", "--final", "merged", "feat/x")
	if code != 0 || !strings.Contains(out, "reported #") {
		t.Fatalf("report = %d %q", code, out)
	}
	code, out, _ = runAgentCLI(t, dir, "two lines\nsecond\n", "report", "-F", "-")
	if code != 0 {
		t.Fatalf("report -F - = %d %q", code, out)
	}
	code, out, _ = runAgentCLI(t, dir, "", "screen", full, "--reports")
	if code != 0 || !strings.Contains(out, "merged feat/x") || !strings.Contains(out, "second") || strings.Index(out, "merged") > strings.Index(out, "second") {
		t.Fatalf("screen --reports = %d %q", code, out)
	}
	code, out, _ = runAgentCLI(t, dir, "", "list")
	if code != 0 || !strings.Contains(out, "  reported  ") {
		t.Fatalf("list = %d %q", code, out)
	}
	code, out, _ = runAgentCLI(t, dir, "", "list", "--json")
	if code != 0 || !strings.Contains(out, `"report_at":`) || strings.Contains(out, `"report_final":true`) {
		t.Fatalf("list --json = %d %q (the latest report is not final)", code, out)
	}
	// wait: a childless any-wait is a refusal (2); a bad flag is usage (2).
	if code, _, errOut := runAgentCLI(t, dir, "", "wait", "--timeout", "1"); code != 2 || !strings.Contains(errOut, "no workers") {
		t.Fatalf("wait = %d %q", code, errOut)
	}
	if code, _, _ := runAgentCLI(t, dir, "", "wait", "--until"); code != 2 {
		t.Fatal("--until without a value is usage")
	}
	if code, _, _ := runAgentCLI(t, dir, "", "report"); code != 2 {
		t.Fatal("report without text is usage")
	}
}

func TestAgentWaitCLITimesOutWith3(t *testing.T) {
	dir, full := agentEnvFor(t, nil)
	// The session waits on a worker: spawn one through the host's starter is
	// out of reach here, so wait on a stranger id that exists → refusal 2,
	// and prove the timeout path through the domain seam instead:
	_ = full
	restore := domain.UseSessionStates(domain.NewStaticStates(nil))
	defer restore()
	code, out, _ := runAgentCLIWait(t, dir, "--timeout", "1")
	if code != 2 || out != "" {
		t.Fatalf("childless = %d %q", code, out)
	}
}
```

Replace the second test by a real one if `agentEnvFor` can hand the session a child: extend `agentEnvFor`'s starter to spawn a `sh -c 'sleep 600'` child via `domain.SpawnAgent` with the fixture config — if that is more than ~15 lines, keep the exit-code mapping covered by a unit test on `waitExitCode(res domain.AgentWaitResult) int` (0 for an event, 3 for `TimedOut`). Drop `runAgentCLIWait` if unused.

- [ ] **Step 2: Run — expect failures**

Run: `cd /work/gigagit/.claude/worktrees/agent-wait-report && go test ./internal/cli -run 'TestAgentReportAndWaitCLI|TestAgentWaitCLI' 2>&1 | tail -5`
Expected: FAIL (`unknown verb "report"`).

- [ ] **Step 3: Implement**

`cmdAgent`: usage string → `<start|list|screen|send|kill|task|wait|report>`; add cases:

```go
	case "wait":
		return agentWait(c, rest, stdout, stderr) // its own ctx: timeout + slack
	case "report":
		return agentReport(ctx, c, rest, stdin, stdout, stderr)
```

`agentOneID` for `screen`: accept `--reports` anywhere; when set, decode `Report`s via a second call? No — `agent_screen` carries only the latest; add `reports` to the screen output instead: in Task 4's `agentScreenOut` the field is the latest. **Ruling for the plan:** `gg agent screen --reports` calls `agent_screen` and prints the latest report under a `report:` line; the full list is `gg agent list --json`'s business only when needed later. To keep the spec's "oldest first, all kept", add to Task 4 an `agentScreenOut.Reports []domain.AgentReport \`json:"reports,omitempty"\`` filled from `domain.AgentReports` — do that (5 lines in Task 4's handler; add it now and amend Task 4's commit if already made).

```go
// agentWait: gg agent wait [<id>] [--until <what>] [--timeout <s>] — exit
// 0 on an event, 3 on a timeout (loop on it), 1 on a channel failure, 2 on
// a refusal or usage.
func agentWait(c *agentlink.Client, args []string, stdout, stderr io.Writer) int {
	const usage = "usage: gg agent wait [<id>] [--until idle|question|exit|report|any] [--timeout <seconds>]"
	id, until, timeout := "", "", 0
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--until" || a == "-until":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, usage)
				return 2
			}
			i++
			until = args[i]
		case strings.HasPrefix(a, "--until="):
			until = strings.TrimPrefix(a, "--until=")
		case a == "--timeout" || a == "-timeout":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, usage)
				return 2
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil {
				fmt.Fprintln(stderr, usage)
				return 2
			}
			timeout = n
		case strings.HasPrefix(a, "--timeout="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--timeout="))
			if err != nil {
				fmt.Fprintln(stderr, usage)
				return 2
			}
			timeout = n
		case strings.HasPrefix(a, "-"):
			fmt.Fprintln(stderr, usage)
			return 2
		default:
			if id != "" {
				fmt.Fprintln(stderr, usage)
				return 2
			}
			id = a
		}
	}
	slack := time.Duration(timeout)*time.Second + 15*time.Second
	if timeout == 0 {
		slack = domain.WaitDefaultTimeout + 15*time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), slack)
	defer cancel()
	in := map[string]any{"id": id, "until": until, "timeout_s": timeout}
	var out domain.AgentWaitResult
	if code := call(ctx, c, "wait", "agent_wait", in, &out, stderr); code != 0 {
		return code
	}
	printWaitResult(stdout, out)
	return waitExitCode(out)
}

func waitExitCode(out domain.AgentWaitResult) int {
	if out.TimedOut {
		return 3
	}
	return 0
}

func printWaitResult(w io.Writer, out domain.AgentWaitResult) {
	if out.TimedOut {
		fmt.Fprintln(w, "timed out")
	} else {
		fmt.Fprintln(w, "event: "+out.Event)
	}
	if out.ID != "" {
		fmt.Fprintln(w, "id: "+out.ID)
	}
	if out.Activity != "" {
		fmt.Fprintln(w, activityLine(out.Activity, out.Options))
	}
	if out.ExitCode != nil {
		fmt.Fprintf(w, "exit code: %d\n", *out.ExitCode)
	}
	if out.Report != nil {
		fmt.Fprintln(w, reportLines(*out.Report))
	}
}

// reportLines: "report #3 (final) 12:04:05:" then the text, indented.
func reportLines(r domain.AgentReport) string {
	head := fmt.Sprintf("report #%d", r.Seq)
	if r.Final {
		head += " (final)"
	}
	head += " " + r.At.Local().Format("15:04:05") + ":"
	return head + "\n  " + strings.ReplaceAll(strings.TrimRight(r.Text, "\n"), "\n", "\n  ")
}

// agentReport: gg agent report [--final] [-F <file>|-F -] [<text>…]
func agentReport(ctx context.Context, c *agentlink.Client, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	const usage = "usage: gg agent report [--final] (<text>… | -F <file> | -F -)"
	final, file := false, ""
	var words []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--final" || a == "-final":
			final = true
		case a == "-F" || a == "--file":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, usage)
				return 2
			}
			i++
			file = args[i]
		default:
			words = append(words, a)
		}
	}
	text := strings.Join(words, " ")
	if file != "" {
		if text != "" {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		var b []byte
		var err error
		if file == "-" {
			b, err = io.ReadAll(stdin)
		} else {
			b, err = os.ReadFile(file)
		}
		if err != nil {
			fmt.Fprintln(stderr, "agent report:", err)
			return 1
		}
		text = string(b)
	}
	if strings.TrimSpace(text) == "" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	var out domain.AgentReport
	if code := call(ctx, c, "report", "agent_report", map[string]any{"text": text, "final": final}, &out, stderr); code != 0 {
		return code
	}
	fmt.Fprintf(stdout, "reported #%d\n", out.Seq)
	return 0
}
```

`agentOneID` (screen): split flags — `--reports` anywhere; decode `Reports []domain.AgentReport \`json:"reports"\`` and `Report *domain.AgentReport`; print `activity:` line, then (no `--reports`) `report: <first line>` when `Report != nil`, then the text; with `--reports` print each `reportLines(r)` (oldest first) and NO screen text.

`agentList` text rows: after the stalled column print `reported` / `done` (by `ReportFinal`) when `!ReportAt.IsZero()`, padded like the other columns (read the current format string).

- [ ] **Step 4: Run the package**

Run: `cd /work/gigagit/.claude/worktrees/agent-wait-report && go test ./internal/cli -run 'TestAgent' 2>&1 | tail -3`
Expected: `ok`.

- [ ] **Step 5: Commit**

`gg add internal/cli/agent.go internal/cli/agent_test.go internal/mcp/agenttools.go` (if Task 4 amended) → `feat(cli): gg agent wait / report / screen --reports`.

---

### Task 6: TUI — badge, popup line, notice, i18n, help

**Files:**
- Modify: `internal/tui/session_activity.go`, `sessions_popup.go` (`sessionStateText`), `worktree_sessions.go:56`, `console.go:205`, `help.go` (one row), `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Test: `internal/tui/session_activity_test.go` (append)

**Interfaces:**
- Consumes: `domain.SessionReportOf`, `domain.SessionActivityOf`, `domain.ActivityNotice.Text`.
- Produces: `func sessionBadge(id domain.SessionID, now time.Time) (text string, attn bool)`; `sessionActivityText(id)` keeps its name and now returns `sessionBadge(id, now).text`; `reportText(r domain.AgentReport, now) string`; `sessionReportLine(id) string` (first line for the popup row, "" when the badge does not show); `activityNoticeText` case `report`.
- i18n keys (all four bundles): `"reported %s"`, `"done %s"`, `"%s in %s reports: %s"`, `"A session row says reported or done once its agent reported a result (agent_report); the badge stays until someone types into that session"`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/session_activity_test.go`:

```go
func TestSessionBadgePrecedence(t *testing.T) {
	restore := domain.UseSessionManager(agentsession.NewManager())
	defer restore()
	m := domain.Sessions()
	s, err := m.Start(domain.SessionStartSpec{Label: "w", AgentID: "claude", Repo: "r", Dir: t.TempDir(), Cols: 80, Rows: 24, Argv: []string{"sh", "-c", "sleep 60"}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.KillAll(context.Background())
	id := s.Info().ID
	now := time.Now()
	w := domain.NewStaticStates(map[domain.SessionID]domain.SessionActivity{id: {State: domain.ActivityIdle, Since: now.Add(-3 * time.Minute)}})
	defer domain.UseSessionStates(w)()
	if text, attn := sessionBadge(id, now); text != "idle 3m00s" || attn {
		t.Fatalf("no report: %q %v", text, attn)
	}
	if _, err := domain.AgentReportVerb(domain.FullSessionID(id), "merged feat/x\nmore", false); err != nil {
		t.Fatal(err)
	}
	if text, attn := sessionBadge(id, now.Add(2*time.Minute)); !strings.HasPrefix(text, "reported 2m") || !attn {
		t.Fatalf("non-final report: %q %v", text, attn)
	}
	if line := sessionReportLine(id); line != "merged feat/x" {
		t.Fatalf("report line = %q", line)
	}
	if _, err := domain.AgentReportVerb(domain.FullSessionID(id), "all done", true); err != nil {
		t.Fatal(err)
	}
	if text, _ := sessionBadge(id, now.Add(2*time.Minute)); !strings.HasPrefix(text, "done ") {
		t.Fatalf("final report: %q", text)
	}
	// A question beats the report.
	w2 := domain.NewStaticStates(map[domain.SessionID]domain.SessionActivity{id: {State: domain.ActivityQuestion, Since: now}})
	restore2 := domain.UseSessionStates(w2)
	if text, attn := sessionBadge(id, now); text != "needs input" || !attn {
		t.Fatalf("question must win: %q %v", text, attn)
	}
	restore2()
}

func TestSessionBadgeClearsOnInput(t *testing.T) {
	restore := domain.UseSessionManager(agentsession.NewManager())
	defer restore()
	m := domain.Sessions()
	s, err := m.Start(domain.SessionStartSpec{Label: "w", AgentID: "claude", Repo: "r", Dir: t.TempDir(), Cols: 80, Rows: 24, Argv: []string{"sh", "-c", "sleep 60"}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.KillAll(context.Background())
	id := s.Info().ID
	defer domain.UseSessionStates(domain.NewStaticStates(nil))()
	if _, err := domain.AgentReportVerb(domain.FullSessionID(id), "done", true); err != nil {
		t.Fatal(err)
	}
	if text, _ := sessionBadge(id, time.Now()); !strings.HasPrefix(text, "done ") {
		t.Fatalf("%q", text)
	}
	time.Sleep(2 * time.Millisecond)
	s.SendText("thanks")
	if text, _ := sessionBadge(id, time.Now()); text != "" {
		t.Fatalf("answered report must clear (no activity → empty): %q", text)
	}
	if line := sessionReportLine(id); line != "" {
		t.Fatalf("line must clear too: %q", line)
	}
}

func TestActivityNoticeTextReport(t *testing.T) {
	n := domain.ActivityNotice{Kind: "report", Label: "claude", Dir: "/a/b/wt", Text: "merged feat/x"}
	if got := activityNoticeText(n); got != "claude in wt reports: merged feat/x" {
		t.Fatalf("%q", got)
	}
}
```

(`tui` tests must not import `agentsession`? Check `archtest` — if the TUI test files already import `agentsession` for `NewManager` (the e2e/console tests do), fine; otherwise use the existing helper that installs a fresh manager.)

- [ ] **Step 2: Run — expect compile failure**

Run: `cd /work/gigagit/.claude/worktrees/agent-wait-report && go test ./internal/tui -run 'TestSessionBadge|TestActivityNoticeTextReport' 2>&1 | head -5`
Expected: `undefined: sessionBadge`.

- [ ] **Step 3: Implement**

`session_activity.go`:

```go
// reportText is the badge for an unanswered report: "reported 2m" or
// "done 2m" (ages from the report).
func reportText(r domain.AgentReport, now time.Time) string {
	if r.Final {
		return i18n.T("done %s", formatElapsed(now.Sub(r.At)))
	}
	return i18n.T("reported %s", formatElapsed(now.Sub(r.At)))
}

// sessionBadge is the row's word and whether it wants the attention
// colour: a question first, then an unanswered report, then the activity.
func sessionBadge(id domain.SessionID, now time.Time) (string, bool) {
	a, aok := domain.SessionActivityOf(id)
	if aok && a.State == domain.ActivityQuestion {
		return activityText(a, now), true
	}
	if rep, ok := domain.SessionReportOf(id); ok {
		return reportText(rep, now), true
	}
	if aok {
		return activityText(a, now), activityAttn(a)
	}
	return "", false
}

// sessionActivityText is the label for a session row, "" when unknown.
func sessionActivityText(id domain.SessionID) string {
	text, _ := sessionBadge(id, time.Now())
	return text
}

// sessionReportLine is the report's first line for the wide popup row,
// "" once answered (or when a question has the row).
func sessionReportLine(id domain.SessionID) string {
	if a, ok := domain.SessionActivityOf(id); ok && a.State == domain.ActivityQuestion {
		return ""
	}
	if rep, ok := domain.SessionReportOf(id); ok {
		return domain.ReportFirstLine(rep.Text)
	}
	return ""
}
```

`activityNoticeText`: add `case "report": return i18n.T("%s in %s reports: %s", n.Label, wt, n.Text)`.

`sessionDecorators`: replace `if a, ok := domain.SessionActivityOf(id); ok && activityAttn(a)` with `if _, attn := sessionBadge(id, time.Now()); attn`.

`sessions_popup.go` `sessionStateText`: after appending the badge, `if line := sessionReportLine(info.ID); line != "" { row += " — " + line }` (the popup's cut-row → bottom-bar convention shows the rest).

`help.go`: next to the existing session-badge help row add a row with the new long key.

Bundles: add the four keys next to `"needs input"` in ja/ko/zh/ru:
- ja: `"reported %s" = "報告済み %s"`, `"done %s" = "完了 %s"`, `"%s in %s reports: %s" = "%s (%s) の報告: %s"`, help: `"セッション行は、エージェントが結果を報告 (agent_report) すると「報告済み」または「完了」と表示します。誰かがそのセッションに入力するまで残ります"`
- ko: `"보고됨 %s"`, `"완료 %s"`, `"%s (%s)의 보고: %s"`, help: `"세션 행은 에이전트가 결과를 보고(agent_report)하면 보고됨 또는 완료라고 표시합니다. 누군가 그 세션에 입력할 때까지 유지됩니다"`
- zh: `"已报告 %s"`, `"完成 %s"`, `"%s (%s) 报告: %s"`, help: `"代理报告结果 (agent_report) 后，会话行显示“已报告”或“完成”；直到有人向该会话输入为止"`
- ru: `"доложил %s"`, `"готово %s"`, `"%s в %s докладывает: %s"`, help: `"Строка сессии показывает «доложил» или «готово», когда агент доложил результат (agent_report); значок держится, пока кто-нибудь не напишет в эту сессию"`

- [ ] **Step 4: Run the TUI tests + i18n gates**

Run: `cd /work/gigagit/.claude/worktrees/agent-wait-report && go test ./internal/tui -run 'TestSessionBadge|TestActivityNotice|TestSessionActivity|I18n|EngineProse|DecisionOptionValues|ActionMenuLabels|CheckVerbs|Help' 2>&1 | tail -5 && go test ./internal/i18n 2>&1 | tail -2`
Expected: `ok` ×2.

- [ ] **Step 5: Commit**

`gg add internal/tui/session_activity.go internal/tui/session_activity_test.go internal/tui/sessions_popup.go internal/tui/help.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml internal/domain/agentreport.go internal/domain/agentreport_test.go` → `feat(tui): session rows say reported / done; one notice per report`.

---

### Task 7: Web — wire, badge, switcher line, toast, browser check

**Files:**
- Modify: `internal/web/sessions_http.go` (`sessionWire`, `sessionsWireWith` signature), `internal/web/live.go` (`activityNoticeWire.Text`, the notices mapping), static `core.js` (pure section), `openfiles.js:64`, `live.js` (nothing if `noticeText` covers it), `internal/web/attach_browser_test.go`
- Test: `internal/web/session_states_test.go`, `internal/web/activityjs_test.go`; Playwright `scratchpad/pw/checkreport.mjs` (session scratchpad, not committed)

**Interfaces:**
- Consumes: `domain.SessionReportOf`, `domain.ReportFirstLine`, `ActivityNotice.Text`.
- Produces: wire `report_at` (omitzero), `report_final` (omitempty), `report_line` (omitempty) — present only while the badge shows; notice wire `text`; `sessionsWireWith(list, tasks, activity, report func(domain.SessionID) (domain.AgentReport, bool))`; JS `activityLabel` / `activityAttn` / `noticeText` extended.

- [ ] **Step 1: Write the failing Go tests**

In `internal/web/session_states_test.go` add:

```go
func TestSessionsWireCarriesAnUnansweredReport(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	list := []domain.SessionInfo{{ID: "s1", Label: "Claude", Started: at.Add(-time.Hour)}, {ID: "s2", Label: "Codex", Started: at.Add(-time.Hour)}}
	rep := func(id domain.SessionID) (domain.AgentReport, bool) {
		if id == "s1" {
			return domain.AgentReport{Seq: 3, Text: "merged feat/x\nmore", Final: true, At: at}, true
		}
		return domain.AgentReport{}, false
	}
	none := func(domain.SessionID) (domain.SessionActivity, bool) { return domain.SessionActivity{}, false }
	ws := sessionsWireWith(list, nil, none, rep)
	b, _ := json.Marshal(ws[0])
	if !strings.Contains(string(b), `"report_at":"2026-10-03T01:00:00Z"`) || !strings.Contains(string(b), `"report_final":true`) || !strings.Contains(string(b), `"report_line":"merged feat/x"`) {
		t.Fatalf("%s", b)
	}
	if b, _ = json.Marshal(ws[1]); strings.Contains(string(b), "report") {
		t.Fatalf("no report must mean no fields: %s", b)
	}
}
```

Fix the existing `sessionsWireWith` callers/tests to pass a fourth arg (`func(domain.SessionID) (domain.AgentReport, bool) { return domain.AgentReport{}, false }`).

In `activityjs_test.go` `TestActivityModelJS` extend the script and `want`:

```js
r.push(activityLabel({ state: "running", agent_state: "idle", since, report_at: new Date(now - 120000).toISOString(), report_final: true }, now));
r.push(activityLabel({ state: "running", agent_state: "working", since, report_at: new Date(now - 30000).toISOString() }, now));
r.push(activityLabel({ state: "running", agent_state: "question", since, report_at: new Date(now - 30000).toISOString() }, now));
r.push([{ report_at: "x" }, { report_at: "x", agent_state: "question" }].map((s) => activityAttn(s)).join(","));
r.push(noticeText({ kind: "report", label: "claude", worktree: "/a/wt", text: "merged feat/x" }));
```
→ append to `want`: `|done 2m|reported 30s|needs input|true,true|claude in wt reports: merged feat/x`.

In `TestActivityOnTheRowsJS` add a switcher (openfiles) row with `report_line: "merged feat/x"` and assert its `meta` contains `done` and `— merged feat/x`.

- [ ] **Step 2: Run — expect failures**

Run: `cd /work/gigagit/.claude/worktrees/agent-wait-report && go test ./internal/web -run 'TestSessionsWire|TestActivity' 2>&1 | tail -8`
Expected: compile error (signature) then JS mismatch.

- [ ] **Step 3: Implement**

`sessions_http.go`: fields

```go
	// An unanswered agent_report (spec 3b §5.1): when, final, first line.
	ReportAt    time.Time `json:"report_at,omitzero"`
	ReportFinal bool      `json:"report_final,omitempty"`
	ReportLine  string    `json:"report_line,omitempty"`
```

`sessionsWire` → `sessionsWireWith(list, tasks, domain.SessionActivityOf, domain.SessionReportOf)`; in the loop after the activity block: `if rep, ok := report(info.ID); ok { w.ReportAt, w.ReportFinal, w.ReportLine = rep.At, rep.Final, domain.ReportFirstLine(rep.Text) }`.

`live.go`: `Text string \`json:"text,omitempty"\`` on `activityNoticeWire`; copy `n.Text` where notices are mapped.

`core.js` pure section:

```js
// activityLabel: "working 7m" | "idle 3m" | "needs input" | "stalled · …" |
// "reported 2m" / "done 2m" (an unanswered agent_report; a question wins) | "".
function activityLabel(s, now) {
  if (!s || s.state === "exited") return "";
  if (s.report_at && s.agent_state !== "question") return (s.report_final ? "done " : "reported ") + activityAge(s.report_at, now);
  …existing…
}
function activityAttn(s) {
  return !!s && (s.agent_state === "question" || !!s.stalled || !!s.report_at);
}
// noticeText …
  if (n.kind === "report") return who + " reports: " + (n.text || "");
```

`openfiles.js:64`: `meta: … : [ageOf(s.started, now), activityLabel(s, now), s.report_line && s.agent_state !== "question" ? "— " + s.report_line : ""].filter(Boolean).join(" · ")` — the dash joined by " · " reads `done 2m · — merged…`; instead build: `const act = activityLabel(s, now); const line = s.report_line && s.agent_state !== "question" ? act + " — " + s.report_line : act; meta: … [ageOf(...), line].filter(Boolean).join(" · ")`.

- [ ] **Step 4: Run the web Go tests**

Run: `cd /work/gigagit/.claude/worktrees/agent-wait-report && go test ./internal/web 2>&1 | tail -3`
Expected: `ok`.

- [ ] **Step 5: Browser check — host a reporting worker**

`attach_browser_test.go`, after the Claude-shaped session: a "Worker" (AgentID `claude`, `sh -c 'sleep 600'`) that reports 3 s after the test starts:

```go
	wk, err := domain.Sessions().Start(domain.SessionStartSpec{Label: "Worker", AgentID: "claude", Repo: "r", Dir: root, Cols: 80, Rows: 24, Argv: []string{"sh", "-c", "sleep 600"}})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(3 * time.Second)
		_, _ = domain.AgentReportVerb(domain.FullSessionID(wk.Info().ID), "merged feat/x — two tests skipped\nsee the log", true)
	}()
```

`scratchpad/pw/checkreport.mjs` (copy `checkstates.mjs`'s prologue; `row()` matches `Worker`):
1. wait ≤ 6 s for the Worker sub-row to read `done` and be visible, in the attention colour (compare with the `sh` row's colour);
2. a toast `Worker in <repo dir name> reports: merged feat/x — two tests skipped` is visible;
3. ctrl+\ switcher row contains `done` and `— merged feat/x`;
4. `/api/sessions` entry has `report_at`, `report_final: true`, `report_line`;
5. POST `/api/session-input` Enter to the Worker → within 2 s the row no longer reads `done` (reads `running …`) and the wire has no `report_at`;
6. no page errors.

`run.sh` twin `runreport.sh` calling `checkreport.mjs`. Run on the branch: expect 6/6 PASS. Run on the unfixed build (a detached temp worktree of `main` with ONLY `attach_browser_test.go` copied over — the worker start compiles there because `AgentReportVerb` … does not exist on main; so instead copy the test but replace the report call by nothing: the check must then FAIL on 1–5 because no badge/toast/fields exist). Record both outputs in the ledger.

- [ ] **Step 6: Commit**

`gg add internal/web/sessions_http.go internal/web/live.go internal/web/static/core.js internal/web/static/openfiles.js internal/web/session_states_test.go internal/web/activityjs_test.go internal/web/attach_browser_test.go` → `feat(web): session rows say reported / done; a toast per report; the browser check hosts a reporting worker`.

---

### Task 8: Skill v131, docs, package map

**Files:**
- Modify: `internal/agentskill/using-gg.md` (§"Starting another agent"), `internal/agentskill/agentskill.go` (`Version` 130 → 131), `.claude/skills/using-gg/SKILL.md` (via `go run ./cmd/gg init --update`), `CHANGELOG.md`, `README.md` (agent orchestration paragraph), `CLAUDE.md` (`domain` row: add "`AgentWait` + the report store"; `agentsession` row: "`LastInput`"), `docs/CLAUDE-details.md` (new section "Agent orchestration — wait + report (stage 3b, 2026-10-03)")

- [ ] **Step 1: Skill text** — replace the `agent_list` sentence "Poll `agent_list` to wait for a worker…" with a pointer to `agent_wait`, and add after `agent_kill`:

```
- `agent_wait {id?, until?, timeout_s?}` / `gg agent wait [<id>] [--until
  idle|question|exit|report|any] [--timeout <s>]` — block until a worker
  you started has news, then return ONE event: `report` (its
  `agent_report`, text included), `exit` (with `exit_code`), `question`
  (with `options` — answer with `agent_send`), `idle` (its turn ended
  without a report — read `agent_screen`). Without `id`: any worker you
  started directly. Each event comes once; `timed_out: true` (default
  after 45 s, keep it under your client's tool timeout) just means call
  again. The CLI exits 0 on an event, 3 on a timeout.
- `agent_report {text, final?}` / `gg agent report [--final] (<text> | -F
  <file> | -F -)` — **a worker's last act**: your result for whoever
  started you (what changed, what you skipped, what they must do; up to
  64 KiB). `final` says you are done. You stay running until killed — do
  not exit on your own; the parent reads your screen and may ask more.
  The user's session row shows `done`/`reported` until someone types into
  your session.
```

And the parent loop in two sentences under the list: "Parent loop: `agent_wait` → on `report` read it, on `question` answer with `agent_send`, on `timed_out` call again; `agent_kill {remove: true}` when done."

- [ ] **Step 2: Bump + sync**

Run: `cd /work/gigagit/.claude/worktrees/agent-wait-report && sed -i 's/Version = 130/Version = 131/' internal/agentskill/agentskill.go && go run ./cmd/gg init --update >/dev/null && go test ./internal/agentskill ./internal/cli -run 'Skill|Dogfood' 2>&1 | tail -3`
Expected: `ok` (the dogfood copy in sync).

- [ ] **Step 3: Docs** — CHANGELOG entry under Unreleased: "Agent orchestration 3b: `agent_wait` (long-poll on a worker: idle / question / exit / report, once each, fresh since the last input) and `agent_report` (a worker's result on its session; rows say `reported`/`done` until someone types into it; one notice); `gg agent wait|report`, `gg agent screen --reports`." README: two sentences in the agent orchestration paragraph. CLAUDE.md rows. CLAUDE-details section: the delivery conditions, the order, the badge rule, the 20/64 KiB/45 s/600 s numbers, the deferred AI-task bridge.

- [ ] **Step 4: Full gate**

Run: `cd /work/gigagit/.claude/worktrees/agent-wait-report && ./test.sh > /tmp/claude-1000/-work-gigagit/d744a241-5c9e-4e67-88c6-9877e3df0e22/scratchpad/gate-3b.log 2>&1; grep -c 'all green' /tmp/claude-1000/-work-gigagit/d744a241-5c9e-4e67-88c6-9877e3df0e22/scratchpad/gate-3b.log`
Expected: `1`. Then start `./test.sh race` the same way into `race-3b.log` (background) and check for the literal `all green` line before the merge ask.

- [ ] **Step 5: Commit**

`gg add internal/agentskill/using-gg.md internal/agentskill/agentskill.go .claude/skills/using-gg/SKILL.md CHANGELOG.md README.md CLAUDE.md docs/CLAUDE-details.md` → `docs: agent wait + report — skill v131, changelog, README, package map, details`.

---

### Task 9: Live verification (manual, no commit unless a rule changes)

- [ ] Build `<worktree>/bin/gg` (`cd <worktree> && go build -o bin/gg ./cmd/gg`), run it in the user's own tmux session `claude-3b` on a scratch repo; from the TUI start a Claude console (Start agent), and in it ask Claude to: `agent_start` a worker (Claude, in a worktree) with the brief "run `gg agent report --final 'hello from the worker'` then wait", then `agent_wait`. Read every screen (`agent_screen` / tmux capture) before any key. Expect: parent's `agent_wait` returns `event: report`, the worker's sub-row reads `done 5s` in the attention colour, the status line shows the notice, typing into the worker's console clears the badge. Record the outcome in the ledger. If a rule from the spec proves wrong in practice, stop and tell the user before changing it.

---

## Self-review

- Spec coverage: §3.1–3.5 → Tasks 1, 3, 4, 5; §4 → Tasks 2, 4, 5; §5.1–5.3 → Tasks 6, 7 (popup/switcher line on the row — ruling recorded in the plan intro, amend spec §5.3 wording in Task 8's docs commit); §6 → Task 8; §7 components all assigned; §8 deferred untouched; §9 tests mapped per task.
- Placeholders: none; every step has code or an exact command.
- Type consistency: `AgentReport` json `seq/text/final/at` used by Tasks 2, 4, 5, 7; `sessionsWireWith` 4-arg signature in Task 7 only; `ReportFirstLine` exported in Task 2 and consumed by Tasks 6 and 7.
- Review Focus 1–5 each has a named test (Tasks 3, 3, 3, 1+6, 2).
