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
	// stale-answer guard: the first decide consumed the pending decision —
	// a second decide must be rejected, never queued for a future fork.
	if err := run.decide("abort"); err == nil {
		t.Fatal("second decide accepted — stale answer would auto-resolve a later fork")
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
