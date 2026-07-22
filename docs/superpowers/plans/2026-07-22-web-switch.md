# Web Increment 2 (op transport + branches/SmartSwitch) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the streaming op transport (SSE + POST, stdlib-only) that all future web write operations ride on, proven end-to-end with `engine.SmartSwitch` from a new branches sidebar.

**Architecture:** A server-side op registry (one live `opRun`: event buffer + SSE fan-out + a channel-based `webDecider` that parks `deps.decide` calls), three HTTP endpoints (`POST /api/op`, `GET /api/op/{id}/events` SSE, `POST /api/op/{id}/decide`), a `GET /api/branches` read, and SPA wiring (sidebar, live op status line, decision modal via `EventSource`).

**Tech Stack:** Go stdlib only (`net/http` SSE — deliberately NOT WebSocket, which would be the first external dep), `internal/domain`/`internal/engine`, hand-written JS.

**Spec:** `docs/superpowers/specs/2026-07-22-web-switch-design.md` (approved, incl. the notify-only correction). Read it before any task.

## Global Constraints

- Branch `feat/web-switch` (worktree `/mnt/t/others/gigagit.worktrees/feat-web-switch`), based on `web-dev`. **Merges go to web-dev, NEVER main. Never push.**
- `internal/web` never imports `internal/git` (archtest); ops run only via `s.svc.Execute`; reads via domain queries.
- Stdlib only — no new module dependencies.
- Mutating routes (`POST /api/op`, `POST /api/op/{id}/decide`) behind the existing `writeGuard`; `branch` passes `isGitArgSafe`.
- Wire strings are English protocol (`Request.ID`/`Prompt`/`Options`, `Result.Summary`) — no localization.
- `POST /api/op` accepts ONLY `op:"switch"` this increment. The parked-decide path is tested via `engine.DeleteBranch{Name}` started through the internal registry (test-only).
- `engine.Done` and `engine.Timing` events are NOT forwarded (done is synthesized from `Execute`'s return; forwarding both would double-fire).
- `done` is ALWAYS the stream's final event; after a `done` with `changed:true` the server resets its cached CommitFeed.
- Decision timeout default 5 minutes; test seam = a `decideTimeout time.Duration` field on `Server` (zero → default).
- Tests: real git in `t.TempDir()` via existing helpers (`gitRun`, `newRepoDir`, `serve`, `getJSON`, `postJSON` in `internal/web/*_test.go`). JS untested-by-design; Task 4 verifies via Playwright + curl.
- Commands run from the worktree root `/mnt/t/others/gigagit.worktrees/feat-web-switch`.

## File Structure

| File | Responsibility |
|---|---|
| `internal/web/oprun.go` (new) | opRun (buffer/fan-out/pending/answer), webDecider, `Server.startOp`/`opByID`/`resetFeed`, event→wire mapping |
| `internal/web/oprun_test.go` (new) | registry-level tests: clean switch, parked decide, timeout, busy, replay |
| `internal/web/ophttp.go` (new) | the three HTTP handlers (start / SSE events / decide) |
| `internal/web/ophttp_test.go` (new) | HTTP tests: SSE end-to-end, forced stash conflict, validation, busy, feed reset |
| `internal/web/branches.go` (new) | `GET /api/branches` |
| `internal/web/branches_test.go` (new) | branches JSON test |
| `internal/web/server.go` (modify) | 3 routes + 2 Server fields |
| `internal/web/static/{index.html,app.js,style.css}` (modify) | sidebar, op line, modal, EventSource client |
| `CHANGELOG.md`, `CLAUDE.md` (modify) | docs |

---

### Task 1: op registry + web decider (`oprun.go`)

**Files:**
- Create: `internal/web/oprun.go`
- Create: `internal/web/oprun_test.go`
- Modify: `internal/web/server.go` (two fields on `Server`)

**Interfaces:**
- Consumes: `s.svc.Execute(ctx, op, events chan<- engine.Event, dec engine.Decider)`; `engine.Progress{Step,Detail}`, `engine.GitLine{Raw}`, `engine.DecisionNeeded{Request}`, `engine.DecisionRequest{ID,Prompt,Options}`, `engine.DecisionResponse{Option}`, `engine.Result{Summary,Changed}`; `engine.SmartSwitch{Branch}`, `engine.DeleteBranch{Name}` (tests).
- Produces (Task 2 relies on these exact signatures):
  - `func (s *Server) startOp(op engine.Operation) (*opRun, error)` — `errOpBusy` when one is live.
  - `func (s *Server) opByID(id string) *opRun` — nil when unknown.
  - `func (s *Server) resetFeed()`
  - `func (r *opRun) subscribe() (history []wireEvent, live chan wireEvent, cancel func())` — live is nil when the op is already done.
  - `func (r *opRun) decide(option string) error` — `errBadOption` / `errNotWaiting` / `errOpDone`.
  - `type wireEvent map[string]any`; `r.id string`.
  - Sentinel errors: `errOpBusy`, `errBadOption`, `errNotWaiting`, `errOpDone`.

- [ ] **Step 1: Write the failing tests**

Create `internal/web/oprun_test.go`:

```go
package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// drainRun subscribes to run and collects events (history + live) until a
// "done" event or the timeout. Fails the test on timeout.
func drainRun(t *testing.T, run *opRun, timeout time.Duration) []wireEvent {
	t.Helper()
	history, live, cancel := run.subscribe()
	defer cancel()
	events := append([]wireEvent{}, history...)
	for _, we := range events {
		if we["type"] == "done" {
			return events
		}
	}
	if live == nil {
		t.Fatalf("run finished without done event: %v", events)
	}
	deadline := time.After(timeout)
	for {
		select {
		case we, ok := <-live:
			if !ok {
				t.Fatalf("live channel closed without done: %v", events)
			}
			events = append(events, we)
			if we["type"] == "done" {
				return events
			}
		case <-deadline:
			t.Fatalf("timeout waiting for done: %v", events)
		}
	}
}

// waitDecision polls until the run parks on a decision (pending != nil).
func waitDecision(t *testing.T, run *opRun) {
	t.Helper()
	for i := 0; i < 200; i++ {
		run.mu.Lock()
		waiting := run.pending != nil
		run.mu.Unlock()
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("op never parked on a decision")
}

func findEvent(events []wireEvent, typ string) (wireEvent, bool) {
	for _, we := range events {
		if we["type"] == typ {
			return we, true
		}
	}
	return nil, false
}

// twoBranchRepo returns a repo on main with a second branch "side" whose tip
// adds side.txt.
func twoBranchRepo(t *testing.T) string {
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "side.txt"), []byte("s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "side commit")
	gitRun(t, dir, "checkout", "main")
	return dir
}

func TestOpRunCleanSwitch(t *testing.T) {
	dir := twoBranchRepo(t)
	srv := New(domain.Open(dir))
	run, err := srv.startOp(engine.SmartSwitch{Branch: "side"})
	if err != nil {
		t.Fatal(err)
	}
	events := drainRun(t, run, 15*time.Second)
	done, _ := findEvent(events, "done")
	if done == nil || done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if !strings.Contains(done["summary"].(string), "switched to side") {
		t.Errorf("summary = %v", done["summary"])
	}
	if _, hasProgress := findEvent(events, "progress"); !hasProgress {
		t.Errorf("no progress events: %v", events)
	}
	if got := gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "side" {
		t.Errorf("HEAD = %s, want side", got)
	}
}

func TestOpRunParkedDecide(t *testing.T) {
	dir := twoBranchRepo(t)
	srv := New(domain.Open(dir))
	run, err := srv.startOp(engine.DeleteBranch{Name: "side"})
	if err != nil {
		t.Fatal(err)
	}
	waitDecision(t, run)

	if err := run.decide("not-an-option"); err != errBadOption {
		t.Fatalf("bad option err = %v, want errBadOption", err)
	}
	if err := run.decide("abort"); err != nil {
		t.Fatalf("decide abort: %v", err)
	}
	events := drainRun(t, run, 15*time.Second)
	dec, _ := findEvent(events, "decision")
	if dec == nil || dec["id"] != "delete-branch" {
		t.Fatalf("decision = %v", dec)
	}
	if err := run.decide("abort"); err != errOpDone {
		t.Fatalf("decide after done err = %v, want errOpDone", err)
	}
	// abort → branch survives
	if out := gitRun(t, dir, "branch", "--list", "side"); !strings.Contains(out, "side") {
		t.Error("side was deleted despite abort")
	}
}

func TestOpRunDecisionTimeout(t *testing.T) {
	dir := twoBranchRepo(t)
	srv := New(domain.Open(dir))
	srv.decideTimeout = 50 * time.Millisecond
	run, err := srv.startOp(engine.DeleteBranch{Name: "side"})
	if err != nil {
		t.Fatal(err)
	}
	events := drainRun(t, run, 15*time.Second)
	done, _ := findEvent(events, "done")
	if done == nil || done["ok"] != false {
		t.Fatalf("done = %v, want ok=false", done)
	}
	if !strings.Contains(done["error"].(string), "timed out") {
		t.Errorf("error = %v", done["error"])
	}
	// slot released: a new op starts
	if _, err := srv.startOp(engine.SmartSwitch{Branch: "side"}); err != nil {
		t.Fatalf("startOp after timeout: %v", err)
	}
}

func TestOpRunBusy(t *testing.T) {
	dir := twoBranchRepo(t)
	srv := New(domain.Open(dir))
	run, err := srv.startOp(engine.DeleteBranch{Name: "side"})
	if err != nil {
		t.Fatal(err)
	}
	waitDecision(t, run)
	if _, err := srv.startOp(engine.SmartSwitch{Branch: "side"}); err != errOpBusy {
		t.Fatalf("second startOp err = %v, want errOpBusy", err)
	}
	if err := run.decide("abort"); err != nil {
		t.Fatal(err)
	}
	drainRun(t, run, 15*time.Second)
	if _, err := srv.startOp(engine.SmartSwitch{Branch: "side"}); err != nil {
		t.Fatalf("startOp after finish: %v", err)
	}
}

func TestOpRunReplayAfterDone(t *testing.T) {
	dir := twoBranchRepo(t)
	srv := New(domain.Open(dir))
	run, err := srv.startOp(engine.SmartSwitch{Branch: "side"})
	if err != nil {
		t.Fatal(err)
	}
	drainRun(t, run, 15*time.Second)
	history, live, cancel := run.subscribe()
	defer cancel()
	if live != nil {
		t.Error("live channel non-nil after done")
	}
	if done, ok := findEvent(history, "done"); !ok || done["ok"] != true {
		t.Fatalf("replay history missing done: %v", history)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web/ -run 'TestOpRun' 2>&1 | tail -5`
Expected: compile FAILURE (opRun, startOp, wireEvent undefined).

- [ ] **Step 3: Implement `oprun.go`**

Create `internal/web/oprun.go`:

```go
package web

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/engine"
)

// The op transport's server half: one live operation at a time streams its
// engine events into a replayable buffer fanned out to SSE subscribers, and
// parks deps.decide forks on a channel the decide endpoint feeds.

var (
	errOpBusy     = errors.New("an operation is already running")
	errBadOption  = errors.New("option not in the pending decision's list")
	errNotWaiting = errors.New("operation is not waiting on a decision")
	errOpDone     = errors.New("operation already finished")
)

const defaultDecideTimeout = 5 * time.Minute

// wireEvent is one SSE message, already shaped for the client.
type wireEvent map[string]any

type opRun struct {
	id     string
	cancel context.CancelFunc

	mu      sync.Mutex
	history []wireEvent
	subs    map[chan wireEvent]struct{}
	pending *engine.DecisionRequest // non-nil while parked on deps.decide
	answer  chan string
	done    bool
}

// startOp begins op in a background goroutine. Exactly one op may be live;
// the previous (finished) run's record is kept for late SSE reads until the
// next start replaces it.
func (s *Server) startOp(op engine.Operation) (*opRun, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.cur != nil {
		s.cur.mu.Lock()
		live := !s.cur.done
		s.cur.mu.Unlock()
		if live {
			return nil, errOpBusy
		}
	}
	s.opSeq++
	// The op must survive the HTTP request that started it: its context is
	// severed from the request and cancelled only when the run finishes.
	ctx, cancel := context.WithCancel(context.Background())
	run := &opRun{
		id:     fmt.Sprintf("op%d", s.opSeq),
		cancel: cancel,
		subs:   make(map[chan wireEvent]struct{}),
		answer: make(chan string, 1),
	}
	s.cur = run
	go s.runOpStream(ctx, run, op)
	return run, nil
}

func (s *Server) opByID(id string) *opRun {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.cur != nil && s.cur.id == id {
		return s.cur
	}
	return nil
}

// resetFeed drops the cached CommitFeed so the next /api/commits rebuilds —
// required after any op that changed state (e.g. a switch moved HEAD; the
// old feed would keep serving the previous branch's commits).
func (s *Server) resetFeed() {
	s.mu.Lock()
	s.feed = nil
	s.mu.Unlock()
}

func (s *Server) runOpStream(ctx context.Context, run *opRun, op engine.Operation) {
	events := make(chan engine.Event, 32)
	pumpDone := make(chan struct{})
	go func() {
		for ev := range events {
			if we := toWire(ev); we != nil {
				run.publish(we)
			}
		}
		close(pumpDone)
	}()
	timeout := s.decideTimeout
	if timeout <= 0 {
		timeout = defaultDecideTimeout
	}
	res, err := s.svc.Execute(ctx, op, events, webDecider{run: run, timeout: timeout})
	close(events)
	<-pumpDone
	if res.Changed {
		s.resetFeed()
	}
	done := wireEvent{"type": "done", "ok": err == nil, "changed": res.Changed, "summary": res.Summary}
	if err != nil {
		done["error"] = err.Error()
	}
	run.finish(done)
	run.cancel()
}

// toWire maps an engine event to its SSE shape. engine.Done and Timing are
// dropped: done is synthesized from Execute's return (forwarding both would
// double-fire), Timing is observability.
func toWire(ev engine.Event) wireEvent {
	switch e := ev.(type) {
	case engine.Progress:
		return wireEvent{"type": "progress", "step": e.Step, "detail": e.Detail}
	case engine.GitLine:
		return wireEvent{"type": "gitline", "raw": e.Raw}
	case engine.DecisionNeeded:
		return wireEvent{"type": "decision", "id": e.Request.ID, "prompt": e.Request.Prompt, "options": e.Request.Options}
	}
	return nil
}

// publish appends to the replay buffer and fans out to live subscribers.
// A subscriber whose buffer is full drops the event (probe-tier; the
// replay-on-attach path is the correctness backstop).
func (r *opRun) publish(we wireEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.history = append(r.history, we)
	for ch := range r.subs {
		select {
		case ch <- we:
		default:
		}
	}
}

// finish publishes the terminal event, marks the run done, and closes all
// subscriber channels.
func (r *opRun) finish(done wireEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.history = append(r.history, done)
	for ch := range r.subs {
		select {
		case ch <- done:
		default:
		}
		close(ch)
	}
	r.subs = make(map[chan wireEvent]struct{})
	r.done = true
}

// subscribe returns a copy of the history so far plus a live channel (nil
// when the run already finished — the history then ends with done).
func (r *opRun) subscribe() ([]wireEvent, chan wireEvent, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	history := slices.Clone(r.history)
	if r.done {
		return history, nil, func() {}
	}
	ch := make(chan wireEvent, 64)
	r.subs[ch] = struct{}{}
	cancel := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if _, ok := r.subs[ch]; ok {
			delete(r.subs, ch)
			close(ch)
		}
	}
	return history, ch, cancel
}

// decide feeds a parked webDecider. Option must be one of the pending
// request's options — decisions are option-lists only, project-wide.
func (r *opRun) decide(option string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return errOpDone
	}
	if r.pending == nil {
		return errNotWaiting
	}
	if !slices.Contains(r.pending.Options, option) {
		return errBadOption
	}
	select {
	case r.answer <- option:
		return nil
	default:
		return errNotWaiting // answer already queued
	}
}

func (r *opRun) setPending(req *engine.DecisionRequest) {
	r.mu.Lock()
	r.pending = req
	r.mu.Unlock()
}

// webDecider parks the op until the decide endpoint answers, the op context
// dies, or the timeout fires — an abandoned browser modal must never wedge
// the repo gate. The DecisionNeeded event was already emitted by
// deps.decide before this is called.
type webDecider struct {
	run     *opRun
	timeout time.Duration
}

func (d webDecider) Decide(ctx context.Context, req engine.DecisionRequest) (engine.DecisionResponse, error) {
	d.run.setPending(&req)
	defer d.run.setPending(nil)
	select {
	case opt := <-d.run.answer:
		return engine.DecisionResponse{Option: opt}, nil
	case <-ctx.Done():
		return engine.DecisionResponse{}, ctx.Err()
	case <-time.After(d.timeout):
		return engine.DecisionResponse{}, fmt.Errorf("decision %q timed out (no answer from the browser)", req.ID)
	}
}
```

In `internal/web/server.go`, add to the `Server` struct (after the `pageInitial`/`pageBatch` fields):

```go
	// op transport (oprun.go): one live operation at a time.
	opMu          sync.Mutex
	cur           *opRun
	opSeq         int
	decideTimeout time.Duration // test seam; zero = defaultDecideTimeout
```

(`sync` and `time` are/become imports of server.go as needed — `sync` is already imported.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/web/ -run 'TestOpRun' -v 2>&1 | tail -12`
Expected: PASS (5 tests).

- [ ] **Step 5: Package + archtest + gofmt**

Run: `go test ./internal/web/ ./internal/archtest/ 2>&1 | tail -3 && gofmt -l internal/web/`
Expected: both ok, no gofmt output.

- [ ] **Step 6: Commit**

```bash
git add internal/web/oprun.go internal/web/oprun_test.go internal/web/server.go
git commit -m "feat(web): op registry + parking web decider (transport core)"
```

---

### Task 2: HTTP surface — start / SSE events / decide

**Files:**
- Create: `internal/web/ophttp.go`
- Create: `internal/web/ophttp_test.go`
- Modify: `internal/web/server.go` (three routes)

**Interfaces:**
- Consumes (from Task 1, exact): `s.startOp(op) (*opRun, error)`, `s.opByID(id) *opRun`, `run.subscribe() ([]wireEvent, chan wireEvent, func())`, `run.decide(option) error`, `run.id`, sentinels `errOpBusy`/`errBadOption`/`errNotWaiting`/`errOpDone`; `writeGuard`, `writeErr`, `writeJSON`, `isGitArgSafe`; `engine.SmartSwitch{Branch}`.
- Produces: the three routes exactly as spec'd; Task 4's JS calls them.

- [ ] **Step 1: Write the failing tests**

Create `internal/web/ophttp_test.go`:

```go
package web

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// readSSE GETs an op event stream and decodes every `data:` line until the
// done event, EOF, or the timeout.
func readSSE(t *testing.T, ts *httptest.Server, opID string, timeout time.Duration) []wireEvent {
	t.Helper()
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(ts.URL + "/api/op/" + opID + "/events")
	if err != nil {
		t.Fatalf("GET events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("events status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %s", ct)
	}
	var events []wireEvent
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var we wireEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &we); err != nil {
			t.Fatalf("bad SSE json %q: %v", line, err)
		}
		events = append(events, we)
		if we["type"] == "done" {
			return events
		}
	}
	t.Fatalf("stream ended without done: %v (scan err %v)", events, sc.Err())
	return nil
}

func startSwitch(t *testing.T, ts *httptest.Server, branch string) string {
	t.Helper()
	var out struct {
		OpID string `json:"op_id"`
	}
	code := postJSON(t, ts, "/api/op", `{"op":"switch","branch":"`+branch+`"}`, "application/json", "", &out)
	if code != http.StatusAccepted {
		t.Fatalf("op start code = %d", code)
	}
	if out.OpID == "" {
		t.Fatal("empty op_id")
	}
	return out.OpID
}

// headBranchOf scans a commits payload for the ref decorated head:true.
// The feed is ALL-branches (subjects appear regardless of HEAD), so the
// moving HEAD decoration is the observable that proves the cached feed was
// rebuilt after a switch.
func headBranchOf(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	var commits struct {
		Rows []struct {
			Refs []struct {
				Name string `json:"name"`
				Head bool   `json:"head"`
			} `json:"refs"`
		} `json:"rows"`
	}
	if code := getJSON(t, ts, "/api/commits", &commits); code != http.StatusOK {
		t.Fatalf("commits code = %d", code)
	}
	for _, r := range commits.Rows {
		for _, ref := range r.Refs {
			if ref.Head {
				return ref.Name
			}
		}
	}
	return ""
}

func TestOpHTTPCleanSwitchAndFeedReset(t *testing.T) {
	dir := twoBranchRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	if hb := headBranchOf(t, ts); hb != "main" { // warms the feed cache too
		t.Fatalf("head branch before switch = %q, want main", hb)
	}

	opID := startSwitch(t, ts, "side")
	events := readSSE(t, ts, opID, 20*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "side" {
		t.Errorf("HEAD = %s", got)
	}
	// a stale cached feed would still decorate main as HEAD
	if hb := headBranchOf(t, ts); hb != "side" {
		t.Errorf("head branch after switch = %q, want side (feed not reset?)", hb)
	}
}

func TestOpHTTPForcedStashConflict(t *testing.T) {
	// f.txt differs on side; a dirty edit on main stashes, switches, and the
	// pop conflicts. stash-pop-conflict is NOTIFY-ONLY: decision event, then
	// done{ok:false} with no decide.
	dir := newRepoDir(t, 1) // f.txt = "content 1"
	gitRun(t, dir, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "side edit")
	gitRun(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	opID := startSwitch(t, ts, "side")
	events := readSSE(t, ts, opID, 20*time.Second)
	dec, hasDec := findEvent(events, "decision")
	if !hasDec || dec["id"] != "stash-pop-conflict" {
		t.Fatalf("expected stash-pop-conflict decision, got %v", events)
	}
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false", done)
	}
	if got := gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "side" {
		t.Errorf("HEAD = %s, want side", got)
	}
	if out := gitRun(t, dir, "stash", "list"); out == "" {
		t.Error("stash dropped — must be preserved on conflict")
	}
	var st statusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if i, ok := findFile(t, st, "f.txt"); !ok || st.Files[i].Kind != "conflicted" {
		t.Errorf("f.txt not conflicted after pop conflict: %+v", st.Files)
	}
}

func TestOpHTTPParkedDecideRoundTrip(t *testing.T) {
	dir := twoBranchRepo(t)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)
	// parked op started via the internal registry — POST /api/op only
	// accepts switch, but the decide endpoint is op-agnostic.
	run, err := srv.startOp(engine.DeleteBranch{Name: "side"})
	if err != nil {
		t.Fatal(err)
	}
	waitDecision(t, run)

	if code := postJSON(t, ts, "/api/op/"+run.id+"/decide", `{"option":"bogus"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("bogus option code = %d, want 400", code)
	}
	if code := postJSON(t, ts, "/api/op/"+run.id+"/decide", `{"option":"abort"}`, "application/json", "", nil); code != http.StatusOK {
		t.Errorf("decide code = %d, want 200", code)
	}
	events := readSSE(t, ts, run.id, 20*time.Second)
	if _, ok := findEvent(events, "decision"); !ok {
		t.Fatalf("no decision in %v", events)
	}
	if code := postJSON(t, ts, "/api/op/"+run.id+"/decide", `{"option":"abort"}`, "application/json", "", nil); code != http.StatusConflict {
		t.Errorf("decide after done code = %d, want 409", code)
	}
	if out := gitRun(t, dir, "branch", "--list", "side"); !strings.Contains(out, "side") {
		t.Error("side deleted despite abort")
	}
}

func TestOpHTTPBusyAndValidation(t *testing.T) {
	dir := twoBranchRepo(t)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	run, err := srv.startOp(engine.DeleteBranch{Name: "side"})
	if err != nil {
		t.Fatal(err)
	}
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op", `{"op":"switch","branch":"side"}`, "application/json", "", nil); code != http.StatusConflict {
		t.Errorf("busy code = %d, want 409", code)
	}
	if err := run.decide("abort"); err != nil {
		t.Fatal(err)
	}
	readSSE(t, ts, run.id, 20*time.Second)

	cases := []struct {
		body, name string
		want       int
	}{
		{`{"op":"pull","branch":"side"}`, "unknown op", http.StatusBadRequest},
		{`{"op":"switch","branch":""}`, "empty branch", http.StatusBadRequest},
		{`{"op":"switch","branch":"--evil"}`, "argv-unsafe branch", http.StatusBadRequest},
		{`{`, "bad json", http.StatusBadRequest},
	}
	for _, c := range cases {
		if code := postJSON(t, ts, "/api/op", c.body, "application/json", "", nil); code != c.want {
			t.Errorf("%s: code = %d, want %d", c.name, code, c.want)
		}
	}
	if code := postJSON(t, ts, "/api/op/nope/decide", `{"option":"x"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Errorf("decide unknown id code = %d, want 404", code)
	}
	resp, err := http.Get(ts.URL + "/api/op/nope/events")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("events unknown id code = %d, want 404", resp.StatusCode)
	}
	// write guard covers the op endpoints
	if code := postJSON(t, ts, "/api/op", `{"op":"switch","branch":"side"}`, "text/plain", "", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("op without json content type = %d, want 415", code)
	}
}
```

Also in this step, widen the existing `postJSON` helper (`internal/web/status_test.go`) to decode any 2xx — op start responds **202**, which the current `== http.StatusOK` check would silently skip, leaving `op_id` empty:

```go
	if out != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
```

(replaces the `if out != nil && resp.StatusCode == http.StatusOK {` line; no existing test changes behavior — they all decode 200s.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web/ -run 'TestOpHTTP' 2>&1 | tail -5`
Expected: FAIL — the routes 404.

- [ ] **Step 3: Implement `ophttp.go` + routes**

Create `internal/web/ophttp.go`:

```go
package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/homeend/gigagit/internal/engine"
)

type opStartRequest struct {
	Op     string `json:"op"`
	Branch string `json:"branch"`
}

// handleOpStart begins an operation and returns 202 {op_id}. Only "switch"
// exists this increment; the switch statement is where future ops land.
func (s *Server) handleOpStart(w http.ResponseWriter, r *http.Request) {
	var req opStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	var op engine.Operation
	switch req.Op {
	case "switch":
		if req.Branch == "" || !isGitArgSafe(req.Branch) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
			return
		}
		op = engine.SmartSwitch{Branch: req.Branch}
	default:
		writeErr(w, http.StatusBadRequest, fmt.Errorf("unknown op %q", req.Op))
		return
	}
	run, err := s.startOp(op)
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"op_id": run.id})
}

// handleOpEvents streams the run's events as SSE: buffered history first
// (replay), then live, closing after the terminal done event.
func (s *Server) handleOpEvents(w http.ResponseWriter, r *http.Request) {
	run := s.opByID(r.PathValue("id"))
	if run == nil {
		writeErr(w, http.StatusNotFound, errors.New("unknown operation"))
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	history, live, cancel := run.subscribe()
	defer cancel()
	for _, we := range history {
		writeSSE(w, we)
	}
	fl.Flush()
	if live == nil {
		return // already finished; history ended with done
	}
	for {
		select {
		case we, ok := <-live:
			if !ok {
				return
			}
			writeSSE(w, we)
			fl.Flush()
			if we["type"] == "done" {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func writeSSE(w http.ResponseWriter, we wireEvent) {
	b, err := json.Marshal(we)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
}

type opDecideRequest struct {
	Option string `json:"option"`
}

// handleOpDecide feeds a parked decision. Options are validated against the
// pending request's list — decisions are option-lists only.
func (s *Server) handleOpDecide(w http.ResponseWriter, r *http.Request) {
	run := s.opByID(r.PathValue("id"))
	if run == nil {
		writeErr(w, http.StatusNotFound, errors.New("unknown operation"))
		return
	}
	var req opDecideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	switch err := run.decide(req.Option); err {
	case nil:
		writeJSON(w, map[string]any{})
	case errBadOption:
		writeErr(w, http.StatusBadRequest, err)
	default: // errNotWaiting, errOpDone
		writeErr(w, http.StatusConflict, err)
	}
}
```

In `internal/web/server.go` `Handler()`, after the `/api/stage` route:

```go
	mux.HandleFunc("POST /api/op", writeGuard(s.handleOpStart))
	mux.HandleFunc("GET /api/op/{id}/events", s.handleOpEvents)
	mux.HandleFunc("POST /api/op/{id}/decide", writeGuard(s.handleOpDecide))
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/web/ -run 'TestOpHTTP' -v 2>&1 | tail -10`
Expected: PASS (4 tests).

- [ ] **Step 5: Package + archtest + gofmt**

Run: `go test ./internal/web/ ./internal/archtest/ 2>&1 | tail -3 && gofmt -l internal/web/`
Expected: both ok, no gofmt output.

- [ ] **Step 6: Commit**

```bash
git add internal/web/ophttp.go internal/web/ophttp_test.go internal/web/server.go
git commit -m "feat(web): op HTTP surface — start, SSE events, decide"
```

---

### Task 3: `GET /api/branches`

**Files:**
- Create: `internal/web/branches.go`
- Create: `internal/web/branches_test.go`
- Modify: `internal/web/server.go` (one route)

**Interfaces:**
- Consumes: `s.svc.Branches(ctx) ([]model.Branch, error)`; `model.Branch{Name, Upstream string; Ahead, Behind int; IsHead bool; Hash string; UnixTime int64}`.
- Produces: `GET /api/branches` → `{"branches":[{name,upstream,ahead,behind,is_head,hash,time}]}` — Task 4's sidebar consumes it.

- [ ] **Step 1: Write the failing test**

Create `internal/web/branches_test.go`:

```go
package web

import (
	"net/http"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestBranchesEndpoint(t *testing.T) {
	dir := twoBranchRepo(t) // on main, plus "side"
	ts := serve(t, New(domain.Open(dir)))
	var body struct {
		Branches []struct {
			Name   string `json:"name"`
			IsHead bool   `json:"is_head"`
			Hash   string `json:"hash"`
			Time   int64  `json:"time"`
		} `json:"branches"`
	}
	if code := getJSON(t, ts, "/api/branches", &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(body.Branches) != 2 {
		t.Fatalf("branches = %+v", body.Branches)
	}
	heads := 0
	for _, b := range body.Branches {
		if b.Name != "main" && b.Name != "side" {
			t.Errorf("unexpected branch %q", b.Name)
		}
		if b.Hash == "" || b.Time == 0 {
			t.Errorf("missing hash/time: %+v", b)
		}
		if b.IsHead {
			heads++
			if b.Name != "main" {
				t.Errorf("is_head on %q, want main", b.Name)
			}
		}
	}
	if heads != 1 {
		t.Errorf("heads = %d, want 1", heads)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/web/ -run 'TestBranchesEndpoint' 2>&1 | tail -3`
Expected: FAIL (404).

- [ ] **Step 3: Implement**

Create `internal/web/branches.go`:

```go
package web

import "net/http"

type branchRow struct {
	Name     string `json:"name"`
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	IsHead   bool   `json:"is_head"`
	Hash     string `json:"hash"`
	Time     int64  `json:"time"`
}

func (s *Server) handleBranches(w http.ResponseWriter, r *http.Request) {
	bs, err := s.svc.Branches(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]branchRow, 0, len(bs))
	for _, b := range bs {
		rows = append(rows, branchRow{
			Name: b.Name, Upstream: b.Upstream, Ahead: b.Ahead, Behind: b.Behind,
			IsHead: b.IsHead, Hash: b.Hash, Time: b.UnixTime,
		})
	}
	writeJSON(w, map[string]any{"branches": rows})
}
```

In `server.go` `Handler()`, after `/api/status`:

```go
	mux.HandleFunc("GET /api/branches", s.handleBranches)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/web/ -run 'TestBranchesEndpoint' -v 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 5: Package + gofmt, commit**

Run: `go test ./internal/web/ 2>&1 | tail -2 && gofmt -l internal/web/`
Expected: ok, clean.

```bash
git add internal/web/branches.go internal/web/branches_test.go internal/web/server.go
git commit -m "feat(web): GET /api/branches"
```

---

### Task 4: SPA — sidebar, op status line, decision modal; docs

**Files:**
- Modify: `internal/web/static/index.html`
- Modify: `internal/web/static/app.js`
- Modify: `internal/web/static/style.css`
- Modify: `CHANGELOG.md`, `CLAUDE.md`

No Go changes. If a Go change seems needed, STOP and report BLOCKED.

- [ ] **Step 1: index.html — sidebar, op line, modal**

Insert the sidebar as the FIRST child of `<main id="panes" class="solo">`:

```html
  <aside id="branches-pane">
    <div id="branches-header">branches</div>
    <ul id="branches-list"></ul>
  </aside>
```

Replace the footer line and add the modal overlay before `</body>`'s script tag:

```html
<footer id="foot"><span id="op-line"></span>j/k move · enter open · esc back · b branches · g graph mode · s stage · u unstage</footer>
<div id="modal" class="hidden">
  <div id="modal-box">
    <div id="modal-prompt"></div>
    <div id="modal-options"></div>
  </div>
</div>
```

- [ ] **Step 2: style.css — append**

```css
#panes.solo { grid-template-columns: 230px 1fr; }
#panes.solo.nosb { grid-template-columns: 1fr; }
#panes.solo.nosb #branches-pane { display: none; }
#panes.detail #branches-pane { display: none; }
#branches-pane { border-right: 1px solid var(--border); overflow-y: auto; }
#branches-header { padding: 4px 8px; color: var(--dim); border-bottom: 1px solid var(--border); }
#branches-list { list-style: none; }
#branches-list li { padding: 2px 10px; cursor: pointer; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
#branches-list li:hover { background: var(--bg-alt); }
#branches-list li.head { color: var(--accent); font-weight: bold; cursor: default; }
#branches-list li .ab { float: right; color: var(--dim); font-size: 11px; }
#op-line { color: var(--accent); padding-right: 12px; }
#op-line.err { color: #f27a6a; }
#modal { position: fixed; inset: 0; background: rgba(0,0,0,.55); display: flex; align-items: center; justify-content: center; z-index: 10; }
#modal.hidden { display: none; }
#modal-box { background: var(--bg-alt); border: 1px solid var(--accent); border-radius: 6px; padding: 18px 22px; max-width: 560px; }
#modal-prompt { margin-bottom: 14px; }
#modal-options { display: flex; gap: 10px; justify-content: flex-end; }
#modal-options button { background: var(--bg); color: var(--fg); border: 1px solid var(--border); border-radius: 4px; padding: 4px 14px; font: inherit; cursor: pointer; }
#modal-options button:hover { border-color: var(--accent); }
```

(Note the first rule REPLACES the existing `#panes.solo { grid-template-columns: 1fr; }` — edit that rule in place rather than appending a duplicate.)

- [ ] **Step 3: app.js — state, branches fetch/render**

State additions (after `statusEntries`):

```js
  branches: [],
  sidebar: true,
  op: null, // {id, es: EventSource} while an operation is live
  lastDiff: null,
```

(`lastDiff` already exists — do not duplicate; add the other three.)

Add after `fetchStatus`:

```js
async function fetchBranches() {
  const body = await getJSON("/api/branches");
  state.branches = body.branches || [];
  renderBranches();
}

function renderBranches() {
  $("branches-list").innerHTML = state.branches
    .map((b) => {
      const ab =
        (b.ahead ? "↑" + b.ahead : "") + (b.behind ? (b.ahead ? " " : "") + "↓" + b.behind : "");
      return (
        `<li class="${b.is_head ? "head" : ""}" data-n="${esc(b.name)}">` +
        `${b.is_head ? "✓ " : ""}${esc(b.name)}${ab ? `<span class="ab">${ab}</span>` : ""}</li>`
      );
    })
    .join("");
}
```

- [ ] **Step 4: app.js — the op client (start, events, modal, refresh)**

Add after `renderBranches`:

```js
// --- op transport client ---

function opLine(text, isErr) {
  const el = $("op-line");
  el.textContent = text || "";
  el.classList.toggle("err", !!isErr);
}

async function startSwitch(branch) {
  if (state.op) return; // one live op; the server would 409 anyway
  let resp;
  try {
    resp = await postJSON("/api/op", { op: "switch", branch });
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    return;
  }
  opLine("⟳ switching " + branch + "…");
  const es = new EventSource("/api/op/" + resp.op_id + "/events");
  state.op = { id: resp.op_id, es };
  es.onmessage = (m) => handleOpEvent(JSON.parse(m.data));
  es.onerror = () => {}; // transient; done handling closes the source
}

function handleOpEvent(ev) {
  if (ev.type === "progress") {
    opLine("⟳ " + ev.step + (ev.detail ? " " + ev.detail : "") + "…");
  } else if (ev.type === "decision") {
    showModal(ev);
  } else if (ev.type === "done") {
    // done is terminal: close the source (EventSource would auto-reconnect
    // and replay the history otherwise) and any open modal (covers
    // notify-only decisions whose op already returned).
    if (state.op) state.op.es.close();
    state.op = null;
    hideModal();
    if (ev.ok) opLine(ev.summary || "done");
    else opLine("error: " + (ev.error || "operation failed"), true);
    if (ev.changed) refreshAfterOp();
    else fetchStatus().then(renderCommits); // a failed switch may still have moved HEAD/stash state
  }
}

async function refreshAfterOp() {
  await Promise.all([loadRepo(), fetchBranches(), fetchStatus()]);
  state.rows = [];
  state.cursor = 0;
  await loadCommits(false);
}

function showModal(ev) {
  $("modal-prompt").textContent = ev.prompt;
  $("modal-options").innerHTML = (ev.options || [])
    .map((o) => `<button data-o="${esc(o)}">${esc(o)}</button>`)
    .join("");
  $("modal").classList.remove("hidden");
  $("modal").dataset.opts = JSON.stringify(ev.options || []);
}

function hideModal() {
  $("modal").classList.add("hidden");
}

async function answerModal(option) {
  if (!state.op) return hideModal();
  hideModal();
  try {
    await postJSON("/api/op/" + state.op.id + "/decide", { option });
  } catch (e) {
    opLine("error: " + (e.message || e), true);
  }
}

$("modal-options").addEventListener("click", (e) => {
  const btn = e.target.closest("button");
  if (btn) answerModal(btn.dataset.o);
});
$("branches-list").addEventListener("click", (e) => {
  const li = e.target.closest("li");
  if (!li || li.classList.contains("head")) return;
  startSwitch(li.dataset.n);
});
```

`loadRepo` — extract from `boot()`: the four lines that fetch `/api/repo` and fill `repo-name`/`repo-branch`/`repo-worktree`/title become:

```js
async function loadRepo() {
  const repo = await getJSON("/api/repo");
  $("repo-name").textContent = repo.name;
  $("repo-branch").textContent = repo.branch;
  $("repo-worktree").textContent = repo.worktree;
  document.title = "gg web — " + repo.name;
}
```

and `boot()` becomes:

```js
async function boot() {
  await loadRepo();
  await fetchStatus().catch(() => {}); // status failing must not block browse
  await fetchBranches().catch(() => {});
  await loadCommits(false);
  focusPane();
}
```

- [ ] **Step 5: app.js — keyboard: modal-first, b toggle**

At the TOP of the `keydown` listener (before the `j`/`ArrowDown` branch), insert:

```js
  if (!$("modal").classList.contains("hidden")) {
    if (e.key === "Escape") {
      const opts = JSON.parse($("modal").dataset.opts || "[]");
      if (opts.includes("abort")) answerModal("abort"); // the TUI's esc rule
    }
    e.preventDefault();
    return; // the modal owns the keyboard
  }
```

In the else-if chain, splice a new branch between the `g` branch and the `s`/`u` branch (the fragment below ends where the existing `} else if ((e.key === "s" ...` line follows — no extra closing brace):

```js
  } else if (e.key === "b" && state.layout === "list") {
    state.sidebar = !state.sidebar;
    $("panes").classList.toggle("nosb", !state.sidebar);
    renderCommits(); // list width changed
```

- [ ] **Step 6: Build + tests + curl/Playwright smoke**

Run: `node --check internal/web/static/app.js && go build -o ./gg ./cmd/gg && go test ./internal/web/ 2>&1 | tail -2`
Expected: all clean.

Smoke (scratch repo — the switch MUTATES, never smoke against a real repo):

```bash
rm -rf /tmp/ggweb-sw && git init -q -b main /tmp/ggweb-sw && cd /tmp/ggweb-sw
git -c user.email=t@t -c user.name=t commit -q --allow-empty -m init
git branch side
(cd /tmp/ggweb-sw && /mnt/t/others/gigagit.worktrees/feat-web-switch/gg web --addr 127.0.0.1:8124 &) && sleep 1
curl -s http://127.0.0.1:8124/api/branches; echo
OP=$(curl -s -X POST -H 'Content-Type: application/json' -d '{"op":"switch","branch":"side"}' http://127.0.0.1:8124/api/op | python3 -c 'import json,sys;print(json.load(sys.stdin)["op_id"])')
curl -s -N --max-time 5 http://127.0.0.1:8124/api/op/$OP/events; echo
git -C /tmp/ggweb-sw branch --show-current   # expect side
pkill -f 'gg web --addr 127.0.0.1:8124'; true
```

Expected: branches JSON lists main+side; the SSE dump ends with a `done` ok=true; current branch = side.

Playwright (visual): reuse the scratch-repo pattern from `<scratchpad>/pw/shoot3.js` — point GG at this worktree's binary and REPO at a fresh `/tmp/ggweb-sw2` two-branch repo, screenshot (1) the list screen with the sidebar, (2) after clicking `side` — header shows the new branch. Read the PNGs to verify before reporting.

- [ ] **Step 7: docs**

`CHANGELOG.md`, top of `## [Unreleased]`:

```markdown
- `gg web` gains the op transport — the streaming spine for web write
  operations: `POST /api/op` starts an engine op (`switch` →
  `engine.SmartSwitch` first), `GET /api/op/{id}/events` streams its
  progress/decision/done events over SSE (stdlib, no WebSocket dep), and
  `POST /api/op/{id}/decide` answers forks parked on a channel-based web
  Decider (5-min timeout so an abandoned modal can't wedge the repo gate).
  The SPA adds a branches sidebar (click to switch, `b` toggles), a live op
  status line, and the decision modal (esc = abort, the TUI rule). A
  successful op resets the server's commit-feed cache so `/api/commits`
  reflects the new HEAD.
```

`CLAUDE.md`, append to the `web` package-map row:

```
Increment 2 adds the op transport (`oprun.go`/`ophttp.go`): `POST /api/op`
(switch only) → one live `opRun` (replay buffer + SSE fan-out at
`GET /api/op/{id}/events`; `engine.Done`/`Timing` not forwarded — done is
synthesized from Execute's return) + `POST /api/op/{id}/decide` feeding a
parking `webDecider` (5-min timeout). `done{changed:true}` resets the
cached CommitFeed. `GET /api/branches` + SPA sidebar/status-line/decision
modal ride on it.
```

- [ ] **Step 8: Commit**

```bash
git add internal/web/static/ CHANGELOG.md CLAUDE.md
git commit -m "feat(web): branches sidebar, live op line, decision modal"
```

---

### Final verification (after all tasks)

- [ ] `./test.sh` — full staged suite green.
- [ ] `go build -o ./gg ./cmd/gg` — leave the binary for the human.
- [ ] Merge target is **web-dev** (controller merges after final review passes). NEVER main. Never push.
