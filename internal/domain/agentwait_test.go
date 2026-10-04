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
	main, wt, _, ov := spawnFixture(t, 6)
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

func doWait(t *testing.T, caller, id, until string, d time.Duration) AgentWaitResult {
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
	res := doWait(t, ov, w1, "idle", 300*time.Millisecond)
	if !res.TimedOut || res.Activity != "idle" || res.ID != w1 {
		t.Fatalf("stale idle must not be delivered: %+v", res)
	}
	// Idle entered AFTER the input, held past the settle: delivered once.
	setStatic(w, s1.Info().ID, SessionActivity{State: ActivityIdle, Since: time.Now()})
	res = doWait(t, ov, w1, "idle", time.Second)
	if res.TimedOut || res.Event != "idle" || res.Options != nil {
		t.Fatalf("fresh idle: %+v", res)
	}
	if res = doWait(t, ov, w1, "idle", 200*time.Millisecond); !res.TimedOut {
		t.Fatalf("the same idle must not be delivered twice: %+v", res)
	}
}

func TestAgentWaitIdleSettle(t *testing.T) {
	ov, w1, _, s1, _, w := waitFixture(t)
	t.Cleanup(UseIdleSettle(400 * time.Millisecond))
	start := time.Now()
	setStatic(w, s1.Info().ID, SessionActivity{State: ActivityIdle, Since: start})
	res := doWait(t, ov, w1, "idle", 2*time.Second)
	if res.TimedOut || time.Since(start) < 350*time.Millisecond {
		t.Fatalf("idle delivered before it settled: %+v after %v", res, time.Since(start))
	}
}

func TestAgentWaitQuestionCarriesOptions(t *testing.T) {
	ov, w1, _, s1, _, w := waitFixture(t)
	done := make(chan AgentWaitResult, 1)
	go func() { done <- doWait(t, ov, w1, "any", 3*time.Second) }()
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
	if res := doWait(t, ov, w1, "any", time.Second); res.Event != "report" || res.Report == nil || res.Report.Text != "half way" {
		t.Fatalf("report first: %+v", res)
	}
	if res := doWait(t, ov, w1, "any", time.Second); res.Event != "question" {
		t.Fatalf("then the question: %+v", res)
	}
	_ = Sessions().Kill(s1.Info().ID)
	<-s1.Done()
	if res := doWait(t, ov, w1, "any", time.Second); res.Event != "exit" || res.ExitCode == nil {
		t.Fatalf("then the exit: %+v", res)
	}
	if res := doWait(t, ov, w1, "any", 200*time.Millisecond); !res.TimedOut {
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
	if res := doWait(t, ov, w1, "any", time.Second); res.Event != "report" || !res.Report.Final {
		t.Fatalf("%+v", res)
	}
	if res := doWait(t, ov, w1, "exit", time.Second); res.Event != "exit" {
		t.Fatalf("%+v", res)
	}
}

func TestAgentWaitAnyPicksTheChildWithNews(t *testing.T) {
	ov, _, w2, _, s2, w := waitFixture(t)
	setStatic(w, s2.Info().ID, SessionActivity{State: ActivityIdle, Since: time.Now()})
	if res := doWait(t, ov, "", "idle", time.Second); res.ID != w2 || res.Event != "idle" {
		t.Fatalf("any → the idle child: %+v", res)
	}
	// Timeout of an any-wait names no worker.
	if res := doWait(t, ov, "", "idle", 200*time.Millisecond); !res.TimedOut || res.ID != "" {
		t.Fatalf("%+v", res)
	}
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
			res := doWait(t, ov, "", "idle", 3*time.Second)
			mu.Lock()
			got = append(got, res.ID+":"+res.Event)
			mu.Unlock()
		}()
	}
	time.Sleep(100 * time.Millisecond)
	setStatic(w, s1.Info().ID, SessionActivity{State: ActivityIdle, Since: time.Now()})
	setStatic(w, s2.Info().ID, SessionActivity{State: ActivityIdle, Since: time.Now()})
	wg.Wait()
	if len(got) != 2 || got[0] == got[1] || strings.Contains(got[0]+got[1], "timed") || strings.HasSuffix(got[0], ":") || strings.HasSuffix(got[1], ":") {
		t.Fatalf("%v", got)
	}
}

func TestAgentWaitDefaults(t *testing.T) {
	if _, err := parseWaitUntil("any"); err != nil {
		t.Fatal(err)
	}
	if d, err := waitTimeout(0); err != nil || d != WaitDefaultTimeout {
		t.Fatalf("default = %v %v", d, err)
	}
	for _, d := range []time.Duration{-time.Second, WaitMaxTimeout + time.Second} {
		if _, err := waitTimeout(d); err == nil {
			t.Fatalf("%v accepted", d)
		}
	}
}

// Review fix 1: a wait whose client is already gone must not consume the
// pending event — the next wait gets it.
func TestAgentWaitCancelledCallConsumesNothing(t *testing.T) {
	ov, w1, _, _, _, _ := waitFixture(t)
	if _, err := AgentReportVerb(w1, "pending", false); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if res, err := AgentWait(ctx, ov, w1, "any", time.Second); err == nil {
		t.Fatalf("a cancelled wait returned %+v", res)
	}
	if res := doWait(t, ov, w1, "any", time.Second); res.Event != "report" || res.Report.Text != "pending" {
		t.Fatalf("the event was eaten by the cancelled call: %+v", res)
	}
}

// Review fix 2: a dead worker ends every wait on it — nothing else can
// happen — and a timeout says whether the worker still runs.
func TestAgentWaitExitEndsAnyUntil(t *testing.T) {
	ov, w1, w2, s1, _, _ := waitFixture(t)
	if res := doWait(t, ov, w2, "report", 200*time.Millisecond); !res.TimedOut || res.State != "running" {
		t.Fatalf("timeout must carry the state: %+v", res)
	}
	_ = Sessions().Kill(s1.Info().ID)
	<-s1.Done()
	res := doWait(t, ov, w1, "report", time.Second)
	if res.Event != "exit" || res.ExitCode == nil || res.State != "exited" {
		t.Fatalf("until: report on a dead worker must return its exit: %+v", res)
	}
	if res = doWait(t, ov, w1, "idle", 200*time.Millisecond); !res.TimedOut || res.State != "exited" {
		t.Fatalf("after the exit went out, the timeout still says exited: %+v", res)
	}
}

// A negative timeout is refused (0 is the default for callers that give
// none); the MCP and CLI edges refuse an explicit 0 themselves.
func TestAgentWaitRefusesANegativeTimeout(t *testing.T) {
	ov, w1, _, _, _, _ := waitFixture(t)
	if _, err := AgentWait(context.Background(), ov, w1, "", -time.Second); err == nil || !strings.Contains(err.Error(), "timeout_s is 1") {
		t.Fatalf("err = %v", err)
	}
}

// The watcher's activity says which hold its idle passed; agent_wait trusts
// the idle after that, not after idleSettle.
func TestAgentWaitHonoursTheIdlesSettle(t *testing.T) {
	ov, w1, _, s1, _, w := waitFixture(t)
	t.Cleanup(UseIdleSettle(2 * time.Second))
	start := time.Now()
	setStatic(w, s1.Info().ID, SessionActivity{State: ActivityIdle, Since: start, Settle: 100 * time.Millisecond})
	res := doWait(t, ov, w1, "idle", 3*time.Second)
	if res.TimedOut || time.Since(start) > time.Second {
		t.Fatalf("titled idle waited for idleSettle: %+v after %v", res, time.Since(start))
	}
}
