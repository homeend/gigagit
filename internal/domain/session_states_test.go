package domain

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/agentstate"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
)

const (
	actIdle     = "done\n────────────────────\n❯ \n────────────────────\n"
	actWorking  = "✻ Cogitating… (27s · ↓ 1.5k tokens)\n────────────────────\n❯ \n"
	actQuestion = " Do you want to proceed?\n ❯ 1. Yes\n   2. Yes, always\n   3. No\n Esc to cancel\n"
	actNoise    = "just some text\nmid redraw\n"
)

// fakeStates is a session feed with no PTY behind it.
type fakeStates struct {
	infos []agentsession.Info
	text  map[agentsession.ID]string
	last  map[agentsession.ID]time.Time
	sig   map[agentsession.ID]agentsession.Signals // absent: no title, no progress
	slow  chan struct{}                            // Text blocks on it when set (a session busy rendering)
}

func (f *fakeStates) List() []agentsession.Info { return f.infos }
func (f *fakeStates) Text(id agentsession.ID) (string, bool) {
	if f.slow != nil {
		<-f.slow
	}
	t, ok := f.text[id]
	return t, ok
}
func (f *fakeStates) LastOutput(id agentsession.ID) time.Time { return f.last[id] }
func (f *fakeStates) Signals(id agentsession.ID) (agentsession.Signals, bool) {
	if s, ok := f.sig[id]; ok {
		return s, true
	}
	return agentsession.Signals{Progress: -1}, true
}

var actT0 = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

// oneSession: a watcher over one running session started at actT0.
func oneSession(agentID string) (*StateWatcher, *fakeStates) {
	f := &fakeStates{
		infos: []agentsession.Info{{ID: "s1", Label: "Claude", AgentID: agentID, Dir: "/wt/a", Started: actT0}},
		text:  map[agentsession.ID]string{}, last: map[agentsession.ID]time.Time{},
		sig: map[agentsession.ID]agentsession.Signals{},
	}
	return newStateWatcher(f, newRuleStore()), f
}

func woken(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func kinds(ns []ActivityNotice) string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Kind
	}
	return strings.Join(out, ",")
}

func TestStatesUnknownChangesNothing(t *testing.T) {
	t.Parallel()
	w, f := oneSession("claude")
	late := actT0.Add(time.Minute)
	f.text["s1"] = actIdle
	w.observe(late)
	ch, cancel := w.Subscribe()
	defer cancel()
	f.text["s1"] = actNoise
	w.observe(late.Add(5 * time.Second))
	a, ok := w.Get("s1")
	if !ok || a.State != ActivityIdle || !a.Since.Equal(late) {
		t.Fatalf("activity = %+v %v", a, ok)
	}
	if woken(ch) {
		t.Fatal("an unknown screen woke the subscribers")
	}
	if a.Name() != "idle" {
		t.Fatalf("Name = %q", a.Name())
	}
}

func TestStatesIdleAfterWorkingPostsOnce(t *testing.T) {
	t.Parallel()
	w, f := oneSession("claude")
	late := actT0.Add(time.Minute)
	f.text["s1"] = actWorking
	w.observe(late)
	f.text["s1"] = actIdle
	w.observe(late.Add(time.Second))
	w.observe(late.Add(3 * time.Second)) // held past the idle settle
	w.observe(late.Add(4 * time.Second))
	ns := w.Notices(0)
	if kinds(ns) != "idle" || ns[0].ID != "s1" || ns[0].Label != "Claude" || ns[0].Dir != "/wt/a" {
		t.Fatalf("notices = %+v", ns)
	}
	if w.NoticeSeq() != ns[0].Seq {
		t.Fatalf("NoticeSeq = %d, notice %d", w.NoticeSeq(), ns[0].Seq)
	}
}

// An idle blip between two steps of one turn (shorter than the idle settle)
// changes nothing: no idle row, no notice. An idle that holds shows from
// when it began, so agent_wait does not add its own settle on top.
func TestStatesIdleShowsOnlyOnceItHolds(t *testing.T) {
	t.Parallel()
	w, f := oneSession("claude")
	late := actT0.Add(time.Minute)
	f.text["s1"] = actWorking
	w.observe(late)
	f.text["s1"] = actIdle
	if wait := w.observe(late.Add(time.Second)); wait != 2*time.Second {
		t.Fatalf("a pending idle asks to be looked at again in %v, want 2s", wait)
	}
	if a, _ := w.Get("s1"); a.State != ActivityWorking || !a.Since.Equal(late) {
		t.Fatalf("a fresh idle showed at once: %+v", a)
	}
	f.text["s1"] = actWorking
	if wait := w.observe(late.Add(2 * time.Second)); wait != 0 {
		t.Fatalf("nothing pending, yet wait = %v", wait)
	}
	f.text["s1"] = actIdle
	w.observe(late.Add(3 * time.Second))
	f.text["s1"] = actNoise // mid-redraw: neither cancels nor promotes
	w.observe(late.Add(4 * time.Second))
	if got := kinds(w.Notices(0)); got != "" {
		t.Fatalf("the flicker posted: %q", got)
	}
	f.text["s1"] = actIdle
	w.observe(late.Add(5 * time.Second))
	a, _ := w.Get("s1")
	if a.State != ActivityIdle || !a.Since.Equal(late.Add(3*time.Second)) {
		t.Fatalf("held idle: %+v", a)
	}
	if got := kinds(w.Notices(0)); got != "idle" {
		t.Fatalf("notices = %q", got)
	}
	// A question is never held back.
	f.text["s1"] = actWorking
	w.observe(late.Add(6 * time.Second))
	f.text["s1"] = actQuestion
	w.observe(late.Add(7 * time.Second))
	if a, _ := w.Get("s1"); a.State != ActivityQuestion {
		t.Fatalf("question held back: %+v", a)
	}
}

func TestStatesQuestionPostsAtOnceWithOptions(t *testing.T) {
	t.Parallel()
	w, f := oneSession("claude")
	late := actT0.Add(time.Minute)
	f.text["s1"] = actQuestion
	w.observe(late)
	if got := kinds(w.Notices(0)); got != "question" {
		t.Fatalf("notices = %q", got)
	}
	a, _ := w.Get("s1")
	if a.State != ActivityQuestion || len(a.Options) != 3 || a.Options[2].Key != "3" {
		t.Fatalf("activity = %+v", a)
	}
	f.text["s1"] = actIdle
	w.observe(late.Add(time.Second))
	if a, _ = w.Get("s1"); len(a.Options) != 0 {
		t.Fatalf("options kept after the question: %+v", a.Options)
	}
	if got := kinds(w.Notices(0)); got != "question" {
		t.Fatalf("question → idle posted: %q", got)
	}
}

func TestStatesGraceHoldsNotices(t *testing.T) {
	t.Parallel()
	// A question inside the grace: shown at once, announced when it ends.
	w, f := oneSession("claude")
	f.text["s1"] = actQuestion
	w.observe(actT0.Add(5 * time.Second))
	if a, _ := w.Get("s1"); a.State != ActivityQuestion {
		t.Fatalf("the badge must not wait for the grace: %+v", a)
	}
	if got := kinds(w.Notices(0)); got != "" {
		t.Fatalf("posted inside the grace: %q", got)
	}
	w.observe(actT0.Add(31 * time.Second))
	w.observe(actT0.Add(33 * time.Second))
	if got := kinds(w.Notices(0)); got != "question" {
		t.Fatalf("after the grace: %q", got)
	}
	// Answered before the grace ended: never announced.
	w, f = oneSession("claude")
	f.text["s1"] = actQuestion
	w.observe(actT0.Add(5 * time.Second))
	f.text["s1"] = actWorking
	w.observe(actT0.Add(10 * time.Second))
	f.text["s1"] = actIdle
	w.observe(actT0.Add(20 * time.Second)) // working → idle inside the grace
	w.observe(actT0.Add(40 * time.Second))
	if got := kinds(w.Notices(0)); got != "" {
		t.Fatalf("grace transitions posted: %q", got)
	}
}

func TestStatesStalledOncePerStretch(t *testing.T) {
	t.Parallel()
	w, f := oneSession("claude")
	now := actT0.Add(10 * time.Minute)
	f.text["s1"] = actWorking
	f.last["s1"] = now.Add(-121 * time.Second)
	w.observe(now)
	w.observe(now.Add(2 * time.Second))
	ns := w.Notices(0)
	if kinds(ns) != "stalled" || ns[0].Quiet < 120*time.Second {
		t.Fatalf("notices = %+v", ns)
	}
	if a, _ := w.Get("s1"); !a.Stalled {
		t.Fatal("not marked stalled")
	}
	ch, cancel := w.Subscribe()
	defer cancel()
	f.last["s1"] = now.Add(3 * time.Second)
	w.observe(now.Add(4 * time.Second))
	if a, _ := w.Get("s1"); a.Stalled {
		t.Fatal("still stalled after output resumed")
	}
	if !woken(ch) || kinds(w.Notices(0)) != "stalled" {
		t.Fatalf("resume: woken or extra notice: %q", kinds(w.Notices(0)))
	}
	// Idle sessions are silent by nature.
	w, f = oneSession("claude")
	f.text["s1"] = actIdle
	f.last["s1"] = now.Add(-time.Hour)
	w.observe(now)
	if a, _ := w.Get("s1"); a.Stalled {
		t.Fatal("an idle session stalled")
	}
}

// A hung API call keeps Claude's spinner timer ticking — output, but no
// progress. Only the spinner moving for spinStallAfter is a stall too; a long
// think stays working until then.
func TestStatesStalledWhenOnlyTheSpinnerMoves(t *testing.T) {
	t.Parallel()
	w, f := oneSession("claude")
	t0 := actT0.Add(time.Minute)
	frame := func(at time.Time, glyph string, secs int) {
		f.text["s1"] = fmt.Sprintf("● Read 2 files\n%s Slithering… (%ds · thinking with high effort)\n────────────────────\n❯ \n", glyph, secs)
		f.last["s1"] = at // the timer redraw is output
		w.observe(at)
	}
	frame(t0, "✶", 1)
	frame(t0.Add(9*time.Minute), "*", 541)
	if a, _ := w.Get("s1"); a.Stalled || kinds(w.Notices(0)) != "" {
		t.Fatalf("a 9-minute think stalled: %+v %q", a, kinds(w.Notices(0)))
	}
	frame(t0.Add(10*time.Minute+time.Second), "✻", 601)
	a, _ := w.Get("s1")
	ns := w.Notices(0)
	if !a.Stalled || kinds(ns) != "stalled" || !ns[0].Spinning || ns[0].Quiet < 10*time.Minute {
		t.Fatalf("spinner only for 10m: %+v %+v", a, ns)
	}
	// Progress (new transcript text) ends it.
	f.text["s1"] = "● Read 3 files\n✶ Slithering… (602s · thinking with high effort)\n────────────────────\n❯ \n"
	w.observe(t0.Add(10*time.Minute + 2*time.Second))
	if a, _ := w.Get("s1"); a.Stalled {
		t.Fatalf("still stalled after progress: %+v", a)
	}
}

func TestStatesStallFromUnknownNeedsDedicatedRules(t *testing.T) {
	t.Parallel()
	now := actT0.Add(10 * time.Minute)
	for agent, want := range map[string]bool{"claude": true, "mystery": false} {
		w, f := oneSession(agent)
		f.text["s1"] = actNoise
		f.last["s1"] = now.Add(-5 * time.Minute)
		w.observe(now)
		if a, _ := w.Get("s1"); a.Stalled != want {
			t.Errorf("%s: stalled = %v, want %v", agent, a.Stalled, want)
		}
	}
	// A custom command with its own rules counts as dedicated.
	w, f := oneSession("")
	r, _ := agentstate.Compile(nil, []string{`^READY$`}, nil)
	w.Bind("s1", r)
	f.text["s1"] = actNoise
	f.last["s1"] = now.Add(-5 * time.Minute)
	w.observe(now)
	if a, _ := w.Get("s1"); !a.Stalled {
		t.Error("a custom command with bound rules did not stall")
	}
}

func TestStatesDropExitedAndSkipUnclassified(t *testing.T) {
	t.Parallel()
	w, f := oneSession("claude")
	f.infos = append(f.infos,
		agentsession.Info{ID: "s2", Label: "Terminal", AgentID: "claude", Terminal: true, Started: actT0},
		agentsession.Info{ID: "s3", Label: "Custom", Started: actT0})
	for _, id := range []agentsession.ID{"s1", "s2", "s3"} {
		f.text[id] = actIdle
	}
	late := actT0.Add(time.Minute)
	w.observe(late)
	for _, id := range []SessionID{"s2", "s3"} {
		if _, ok := w.Get(id); ok {
			t.Errorf("%s was classified", id)
		}
	}
	ch, cancel := w.Subscribe()
	defer cancel()
	f.infos[0].State = agentsession.Exited
	w.observe(late.Add(time.Second))
	if _, ok := w.Get("s1"); ok {
		t.Fatal("an exited session kept its state")
	}
	if !woken(ch) {
		t.Fatal("dropping a state did not wake the subscribers")
	}
}

func TestStatesBoundRulesWin(t *testing.T) {
	t.Parallel()
	w, f := oneSession("claude")
	r, _ := agentstate.Compile(nil, []string{`^READY$`}, nil)
	w.Bind("s1", r)
	f.text["s1"] = actIdle // claude's own idle box: not the bound rule
	w.observe(actT0.Add(time.Minute))
	if a, _ := w.Get("s1"); a.State != ActivityUnknown {
		t.Fatalf("built-ins applied over bound rules: %+v", a)
	}
	f.text["s1"] = "READY\n"
	w.observe(actT0.Add(2 * time.Minute))
	if a, _ := w.Get("s1"); a.State != ActivityIdle {
		t.Fatalf("bound rule ignored: %+v", a)
	}
}

// Reading the screens (each session's emulator lock) happens outside the
// watcher's lock: a session slow to render never stalls Get, the rows'
// reader on the UI thread.
func TestStatesGetIsNotBlockedByAScreenRead(t *testing.T) {
	t.Parallel()
	w, f := oneSession("claude")
	f.text["s1"] = actIdle
	f.slow = make(chan struct{})
	done := make(chan struct{})
	go func() { w.observe(actT0.Add(time.Minute)); close(done) }()
	time.Sleep(20 * time.Millisecond) // observe is inside Text now
	got := make(chan struct{})
	go func() { w.Get("s1"); w.NoticeSeq(); close(got) }()
	select {
	case <-got:
	case <-time.After(time.Second):
		close(f.slow)
		t.Fatal("Get waited for a screen read")
	}
	close(f.slow)
	<-done
}

func TestStatesNoticeRing(t *testing.T) {
	t.Parallel()
	w, _ := oneSession("claude")
	for i := range 70 {
		w.PostNotice(ActivityNotice{ID: "s1", Kind: "idle", Label: fmt.Sprint(i)})
	}
	all := w.Notices(0)
	if len(all) != 64 || all[0].Label != "6" || all[63].Seq != 70 {
		t.Fatalf("ring: %d first %q last seq %d", len(all), all[0].Label, all[len(all)-1].Seq)
	}
	if got := w.Notices(68); len(got) != 2 || got[0].Seq != 69 {
		t.Fatalf("after 68: %+v", got)
	}
}

func TestSessionRules(t *testing.T) {
	t.Parallel()
	plain := config.ToolCommand{Category: "session", Name: "Claude", Command: "claude"}
	if _, custom, err := SessionRules(plain); custom || err != nil {
		t.Fatalf("no lists: custom=%v err=%v", custom, err)
	}
	if SessionRulesWarning(plain) != "" {
		t.Fatal("a plain command warns")
	}
	one := plain
	one.ScreenWaiting = []string{`^READY$`}
	r, custom, err := SessionRules(one)
	def := agentstate.DefaultRules("claude")
	if !custom || err != nil || len(r.Waiting) != 1 || len(r.Working) != len(def.Working) || len(r.Question) != len(def.Question) {
		t.Fatalf("one list (claude keeps its other built-ins): %+v custom=%v err=%v", r, custom, err)
	}
	bad := plain
	bad.ScreenWorking = []string{`(`}
	if _, custom, err := SessionRules(bad); custom || err == nil {
		t.Fatalf("bad pattern: custom=%v err=%v", custom, err)
	}
	if w := SessionRulesWarning(bad); !strings.Contains(w, "Claude") || !strings.Contains(w, "(") {
		t.Fatalf("warning = %q", w)
	}
}

// Every agent gg can start as a session has rules that can say something.
func TestEverySessionAgentHasRules(t *testing.T) {
	t.Parallel()
	for _, tl := range exttool.Builtins() {
		session := false
		for _, c := range tl.Commands {
			session = session || c.Category == exttool.CatSession
		}
		if !session {
			continue
		}
		r := agentstate.DefaultRules(tl.ID)
		if len(r.Waiting) == 0 || len(r.Question) == 0 {
			t.Errorf("%s: no waiting or question rules", tl.ID)
		}
	}
}

// Serial: swaps the process-global manager. The watcher must wake on a
// session's OUTPUT (the manager only signals start/exit/remove).
func TestSessionStatesWakeOnOutput(t *testing.T) {
	prevTick := stateTick
	stateTick = time.Hour
	defer func() { stateTick = prevTick }()
	m := agentsession.NewManager()
	restore := UseSessionManager(m)
	sess, err := m.Start(agentsession.StartSpec{Label: "Custom", Dir: t.TempDir(), Cols: 40, Rows: 10,
		Argv: []string{"sh", "-c", `sleep 1; printf 'READY\n'; sleep 30`}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.KillAll(t.Context())
	before := sess.SubscriberCount()
	r, _ := agentstate.Compile(nil, []string{`^READY$`}, nil)
	w := SessionStates()
	w.Bind(sess.Info().ID, r)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a, ok := SessionActivityOf(sess.Info().ID); ok && a.State == ActivityIdle {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if a, _ := SessionActivityOf(sess.Info().ID); a.State != ActivityIdle {
		t.Fatalf("activity = %+v (the tick is off: only an output wake can classify)", a)
	}
	restore()
	if got := sess.SubscriberCount(); got != before {
		t.Fatalf("subscriptions leaked: %d, was %d", got, before)
	}
	if _, ok := SessionActivityOf(sess.Info().ID); ok {
		t.Fatal("a swapped-out manager's state is still served")
	}
}

// Serial: the process-global manager. A turn's end shows once the idle has
// held the settle, with no further output and the tick off: the watcher
// looks again by itself.
func TestSessionStatesPromoteAHeldIdleWithoutOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	prevTick := stateTick
	stateTick = time.Hour
	defer func() { stateTick = prevTick }()
	defer UseIdleSettle(300 * time.Millisecond)()
	m := agentsession.NewManager()
	defer UseSessionManager(m)()
	sess, err := m.Start(agentsession.StartSpec{Label: "Custom", Dir: t.TempDir(), Cols: 40, Rows: 10,
		Argv: []string{"sh", "-c", `sleep 1; printf 'BUSY\n'; sleep 1; printf '\033[2J\033[HREADY\n'; sleep 30`}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.KillAll(t.Context())
	r, _ := agentstate.Compile([]string{`^BUSY$`}, []string{`^READY$`}, nil)
	SessionStates().Bind(sess.Info().ID, r)
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if a, ok := SessionActivityOf(sess.Info().ID); ok && a.State == ActivityIdle {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	a, _ := SessionActivityOf(sess.Info().ID)
	t.Fatalf("activity = %+v: a held idle was never promoted", a)
}

// Serial: the process-global manager. A command's own screen_* lists are
// bound at start, so a custom command (no agent id) is classified by them.
func TestStartSessionBindsScreenRules(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	restore := UseSessionManager(agentsession.NewManager())
	defer restore()
	dir := cleanDir(t)
	svc := Open(dir)
	tc := config.ToolCommand{Category: "session", Name: "Shell", Mode: "session",
		Command: `printf 'READY\n'; sleep 30`, ScreenWaiting: []string{`^READY$`}}
	s, err := svc.StartSession(context.Background(), tc, dir, "", 80, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer Sessions().KillAll(t.Context())
	SessionStates()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a, ok := SessionActivityOf(s.Info().ID); ok && a.State == ActivityIdle {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	a, ok := SessionActivityOf(s.Info().ID)
	t.Fatalf("activity = %+v %v", a, ok)
}

// A rule set bound before its session shows up in List (the start path
// binds after the manager registered it; an observe may run in between)
// must survive until the session has been seen live and gone.
func TestStatesKeepRulesBoundBeforeTheSessionIsListed(t *testing.T) {
	t.Parallel()
	w, f := oneSession("")
	f.infos = nil // not listed yet
	r, _ := agentstate.Compile(nil, []string{`^READY$`}, nil)
	w.Bind("s1", r)
	w.observe(actT0.Add(time.Minute))
	f.infos = []agentsession.Info{{ID: "s1", Label: "Custom", Dir: "/wt/a", Started: actT0}}
	f.text["s1"] = "READY\n"
	w.observe(actT0.Add(2 * time.Minute))
	if a, _ := w.Get("s1"); a.State != ActivityIdle {
		t.Fatalf("bound rules were dropped before the session was listed: %+v", a)
	}
	f.infos[0].State = agentsession.Exited
	w.observe(actT0.Add(3 * time.Minute))
	if _, ok := w.rules.get("s1"); ok {
		t.Fatal("rules of an exited session were kept")
	}
}

// A block for a known agent that sets only some lists keeps the agent's
// built-ins for the others — otherwise its idle box reads unknown and every
// turn end becomes a stall.
func TestSessionRulesMergeWithTheAgentBuiltins(t *testing.T) {
	t.Parallel()
	tc := config.ToolCommand{Category: "session", Name: "Claude", Command: "claude", ScreenQuestion: []string{`CONFIRM`}}
	r, custom, err := SessionRules(tc)
	if !custom || err != nil {
		t.Fatalf("custom=%v err=%v", custom, err)
	}
	if got := agentstate.Classify(r, agentstate.Tail(actIdle, 15)); got != agentstate.Waiting {
		t.Fatalf("claude's idle box with a partial block: %q", got)
	}
	if got := agentstate.Classify(r, agentstate.Tail("please CONFIRM", 15)); got != agentstate.Question {
		t.Fatalf("own question list: %q", got)
	}
	if got := agentstate.Classify(r, agentstate.Tail(actQuestion, 15)); got != agentstate.Unknown {
		t.Fatalf("the built-in question list must be replaced, not merged: %q", got)
	}
	// Claude's own menus stay unknown whatever the block sets: there is no
	// config list for them.
	if got := agentstate.Classify(r, agentstate.Tail("please CONFIRM\n ❯ 1. View tools\n Esc to back", 15)); got != agentstate.Unknown {
		t.Fatalf("an own menu with a partial block: %q", got)
	}
	// The title rules have no config list either: a block keeps them.
	if st, _ := agentstate.ClassifyWith(r, agentstate.Tail(actIdle, 15), agentstate.Signal{Title: "◐ x", Progress: -1}); st != agentstate.Working {
		t.Fatalf("claude's title spinner with a partial block: %q", st)
	}
	kimi := config.ToolCommand{Category: "session", Name: "Kimi", Command: "kimi", ScreenQuestion: []string{`CONFIRM`}}
	if kr, _, _ := SessionRules(kimi); !kr.ProgressBusy {
		t.Fatal("kimi's progress rule lost with a partial block")
	}
	// A custom command (no agent) with one list has only that list.
	cu := config.ToolCommand{Category: "session", Name: "X", Command: "mytool", ScreenWaiting: []string{`^READY$`}}
	r, _, _ = SessionRules(cu)
	if len(r.Working) != 0 || len(r.Question) != 0 || len(r.Waiting) != 1 {
		t.Fatalf("custom: %+v", r)
	}
}

func title(t string) agentsession.Signals { return agentsession.Signals{Title: t, Progress: -1} }

// Between two steps Claude's screen shows the empty prompt while its title
// still spins: the row stays working and nothing is pending.
func TestStatesTitleWorkingBridgesTheGap(t *testing.T) {
	t.Parallel()
	w, f := oneSession("claude")
	late := actT0.Add(time.Minute)
	f.text["s1"], f.sig["s1"] = actWorking, title("◐ topic")
	w.observe(late)
	f.text["s1"] = actIdle
	if wait := w.observe(late.Add(time.Second)); wait != 0 {
		t.Fatalf("an idle is pending under a spinning title: %v", wait)
	}
	w.observe(late.Add(5 * time.Second))
	if a, _ := w.Get("s1"); a.State != ActivityWorking || kinds(w.Notices(0)) != "" {
		t.Fatalf("activity %+v notices %q", a, kinds(w.Notices(0)))
	}
}

// The title says idle: the hold is titleSettle, the idle shows from its
// first read and carries the hold it passed.
func TestStatesTitledIdleHoldsShort(t *testing.T) {
	t.Parallel()
	w, f := oneSession("claude")
	late := actT0.Add(time.Minute)
	f.text["s1"], f.sig["s1"] = actWorking, title("◑ topic")
	w.observe(late)
	f.text["s1"], f.sig["s1"] = actIdle, title("✳ topic")
	if wait := w.observe(late.Add(time.Second)); wait != 700*time.Millisecond {
		t.Fatalf("titled idle recheck = %v, want 700ms", wait)
	}
	w.observe(late.Add(1700 * time.Millisecond))
	a, _ := w.Get("s1")
	if a.State != ActivityIdle || !a.Since.Equal(late.Add(time.Second)) || a.Settle != 700*time.Millisecond {
		t.Fatalf("activity = %+v", a)
	}
	if kinds(w.Notices(0)) != "idle" {
		t.Fatalf("notices = %q", kinds(w.Notices(0)))
	}
}

// A title that never animated ("✳" from the start: a multiplexer gg did not
// strip) says nothing: today's 2 s hold applies.
func TestStatesNeverAnimatedKeeps2s(t *testing.T) {
	t.Parallel()
	w, f := oneSession("claude")
	late := actT0.Add(time.Minute)
	f.text["s1"], f.sig["s1"] = actWorking, title("✳ topic")
	w.observe(late)
	f.text["s1"] = actIdle
	if wait := w.observe(late.Add(time.Second)); wait != 2*time.Second {
		t.Fatalf("recheck = %v, want 2s", wait)
	}
	w.observe(late.Add(3 * time.Second))
	if a, _ := w.Get("s1"); a.State != ActivityIdle || a.Settle != 2*time.Second {
		t.Fatalf("activity = %+v", a)
	}
}

// A dialog mid-turn: the title turns "✳", the screen says question — at once.
func TestStatesDialogUnderAnIdleTitleIsAQuestion(t *testing.T) {
	t.Parallel()
	w, f := oneSession("claude")
	late := actT0.Add(time.Minute)
	f.text["s1"], f.sig["s1"] = actWorking, title("◐ topic")
	w.observe(late)
	f.text["s1"], f.sig["s1"] = actQuestion, title("✳ topic")
	w.observe(late.Add(time.Second))
	if a, _ := w.Get("s1"); a.State != ActivityQuestion || len(a.Options) == 0 {
		t.Fatalf("activity = %+v", a)
	}
}

// A redraw the screen rules cannot read, under a trusted idle title, still
// becomes idle (after the short hold).
func TestStatesTitledIdleOverAnUnreadableScreen(t *testing.T) {
	t.Parallel()
	w, f := oneSession("claude")
	late := actT0.Add(time.Minute)
	f.text["s1"], f.sig["s1"] = actWorking, title("◐ topic")
	w.observe(late)
	f.text["s1"], f.sig["s1"] = actNoise, title("✳ topic")
	w.observe(late.Add(time.Second))
	w.observe(late.Add(2 * time.Second))
	if a, _ := w.Get("s1"); a.State != ActivityIdle {
		t.Fatalf("activity = %+v", a)
	}
}

// Kimi's progress report: busy keeps working over an idle box, clear goes
// idle on the short hold.
func TestStatesKimiProgress(t *testing.T) {
	t.Parallel()
	w, f := oneSession("kimi")
	late := actT0.Add(time.Minute)
	kimiIdle := " │ >                    │\n ╰────────────────────╯\n"
	f.text["s1"], f.sig["s1"] = kimiIdle, agentsession.Signals{Progress: 3}
	w.observe(late)
	if a, _ := w.Get("s1"); a.State != ActivityWorking {
		t.Fatalf("busy: %+v", a)
	}
	f.sig["s1"] = agentsession.Signals{Progress: 0}
	if wait := w.observe(late.Add(time.Second)); wait != 700*time.Millisecond {
		t.Fatalf("recheck = %v", wait)
	}
}
