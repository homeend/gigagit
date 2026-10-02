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
}

func (f *fakeStates) List() []agentsession.Info { return f.infos }
func (f *fakeStates) Text(id agentsession.ID) (string, bool) {
	t, ok := f.text[id]
	return t, ok
}
func (f *fakeStates) LastOutput(id agentsession.ID) time.Time { return f.last[id] }

var actT0 = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

// oneSession: a watcher over one running session started at actT0.
func oneSession(agentID string) (*StateWatcher, *fakeStates) {
	f := &fakeStates{
		infos: []agentsession.Info{{ID: "s1", Label: "Claude", AgentID: agentID, Dir: "/wt/a", Started: actT0}},
		text:  map[agentsession.ID]string{}, last: map[agentsession.ID]time.Time{},
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
	w.observe(late.Add(2 * time.Second))
	ns := w.Notices(0)
	if kinds(ns) != "idle" || ns[0].ID != "s1" || ns[0].Label != "Claude" || ns[0].Dir != "/wt/a" {
		t.Fatalf("notices = %+v", ns)
	}
	if w.NoticeSeq() != ns[0].Seq {
		t.Fatalf("NoticeSeq = %d, notice %d", w.NoticeSeq(), ns[0].Seq)
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
	if !custom || err != nil || len(r.Waiting) != 1 || len(r.Working) != 0 || len(r.Question) != 0 {
		t.Fatalf("one list: %+v custom=%v err=%v", r, custom, err)
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
