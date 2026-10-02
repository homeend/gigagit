package domain

// What an agent session is doing — working, idle, asking a question,
// stalled — read off its screen by one process-global watcher. Frontends
// subscribe and read snapshots; each keeps its own cursor into the notice
// ring, so the terminal and the page it hosts never steal each other's
// wakeups (the Broadcaster rule).

import (
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/agentstate"
	"github.com/homeend/gigagit/internal/config"
)

// Aliases so frontends never import agentstate (archtest).
type (
	ActivityState  = agentstate.State
	ActivityOption = agentstate.Option
)

const (
	ActivityUnknown  = agentstate.Unknown
	ActivityWorking  = agentstate.Working
	ActivityIdle     = agentstate.Waiting // the labels and the wire say "idle"
	ActivityQuestion = agentstate.Question
)

// SessionActivity is what the watcher last concluded about a running
// session. (SessionState is the lifecycle: running / exited.)
type SessionActivity struct {
	State   ActivityState
	Since   time.Time        // when State was entered
	StepFor time.Duration    // the spinner's own timer, 0 when it shows none
	Stalled bool             // nothing printed for stallAfter while (apparently) busy
	Options []ActivityOption // the dialog's choices (question only); carried, never pressed
}

// Name is the protocol value: "working", "idle", "question" or "".
func (a SessionActivity) Name() string {
	if a.State == ActivityIdle {
		return "idle"
	}
	return string(a.State)
}

// ActivityNotice is one transition worth the user's attention.
type ActivityNotice struct {
	Seq   uint64
	ID    SessionID
	Kind  string        // "question" | "idle" | "stalled"
	Label string        // the session's label
	Dir   string        // its worktree
	Quiet time.Duration // stalled: how long nothing was printed
}

// Timing rules. Variables so tests (UseStateTiming) can shrink them.
var (
	// stateGrace: the first stretch after a start is not trusted (sign-in
	// spinners read as working), so no notice is posted before it ends.
	stateGrace = 30 * time.Second
	// stallAfter: no output for this long while working is a stall.
	stallAfter = 120 * time.Second
	// stateTick serves the stall clock; output wakes the watcher itself.
	stateTick = 2 * time.Second
	// stateCoalesce: a burst of output is classified once it settles.
	stateCoalesce = 300 * time.Millisecond
)

const (
	stateTailLines = 15
	noticeRing     = 64
)

// UseStateTiming replaces the grace and stall rules (tests) and returns the
// restore.
func UseStateTiming(grace, stall time.Duration) func() {
	statesMu.Lock()
	pg, ps := stateGrace, stallAfter
	stateGrace, stallAfter = grace, stall
	statesMu.Unlock()
	return func() {
		statesMu.Lock()
		stateGrace, stallAfter = pg, ps
		statesMu.Unlock()
	}
}

// stateSource is what the watcher reads: the manager in production, a fake
// feed in tests.
type stateSource interface {
	List() []agentsession.Info
	Text(id agentsession.ID) (string, bool)
	LastOutput(id agentsession.ID) time.Time
}

type managerSource struct{ m *agentsession.Manager }

func (s managerSource) List() []agentsession.Info { return s.m.List() }
func (s managerSource) Text(id agentsession.ID) (string, bool) {
	sess, ok := s.m.Get(id)
	if !ok {
		return "", false
	}
	return sess.Text(), true
}
func (s managerSource) LastOutput(id agentsession.ID) time.Time {
	if sess, ok := s.m.Get(id); ok {
		return sess.LastOutput()
	}
	return time.Time{}
}

// ruleStore holds the rules bound to sessions at start (a command with its
// own screen_* lists). It lives outside the watcher so a start never
// depends on the watcher running.
type ruleStore struct {
	mu   sync.Mutex
	m    map[SessionID]agentstate.Rules
	seen map[SessionID]bool // ids an observe has seen live: only those are pruned
}

func newRuleStore() *ruleStore {
	return &ruleStore{m: map[SessionID]agentstate.Rules{}, seen: map[SessionID]bool{}}
}

func (s *ruleStore) bind(id SessionID, r agentstate.Rules) {
	s.mu.Lock()
	s.m[id] = r
	s.mu.Unlock()
}

func (s *ruleStore) get(id SessionID) (agentstate.Rules, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.m[id]
	return r, ok
}

// keep prunes the rules of sessions that were live and are gone. A set bound
// before its session shows up in List (the start binds after the manager
// registered the session; an observe may have listed in between) is kept
// until the session has been seen.
func (s *ruleStore) keep(live map[SessionID]bool) {
	s.mu.Lock()
	for id := range live {
		s.seen[id] = true
	}
	for id := range s.m {
		if s.seen[id] && !live[id] {
			delete(s.m, id)
			delete(s.seen, id)
		}
	}
	for id := range s.seen {
		if !live[id] {
			delete(s.seen, id)
		}
	}
	s.mu.Unlock()
}

// StateWatcher classifies the running agent sessions of one manager.
type StateWatcher struct {
	src   stateSource
	rules *ruleStore
	bc    agentsession.Broadcaster

	mu       sync.Mutex
	states   map[SessionID]SessionActivity
	pendingQ map[SessionID]bool // a question seen inside the grace, not yet announced
	notices  []ActivityNotice
	seq      uint64

	stop chan struct{}
	wg   sync.WaitGroup
}

func newStateWatcher(src stateSource, rules *ruleStore) *StateWatcher {
	return &StateWatcher{src: src, rules: rules, states: map[SessionID]SessionActivity{},
		pendingQ: map[SessionID]bool{}, stop: make(chan struct{})}
}

// NewStaticStates is a watcher that watches nothing and serves the given
// states — for frontend tests (install it with UseSessionStates).
func NewStaticStates(states map[SessionID]SessionActivity) *StateWatcher {
	w := newStateWatcher(nil, newRuleStore())
	for id, a := range states {
		w.states[id] = a
	}
	return w
}

// Get is the session's activity; false for a session that is not classified
// (a terminal, a custom command, an exited one).
func (w *StateWatcher) Get(id SessionID) (SessionActivity, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	a, ok := w.states[id]
	return a, ok
}

// Subscribe wakes after every change of any session's activity and after
// every notice. The subscription belongs on its owner's state.
func (w *StateWatcher) Subscribe() (<-chan struct{}, func()) { return w.bc.Subscribe() }

// NoticeSeq is the newest notice's number: where a frontend that starts
// now begins reading.
func (w *StateWatcher) NoticeSeq() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seq
}

// Notices returns the notices newer than after, oldest first (the last
// noticeRing are kept).
func (w *StateWatcher) Notices(after uint64) []ActivityNotice {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []ActivityNotice
	for _, n := range w.notices {
		if n.Seq > after {
			out = append(out, n)
		}
	}
	return out
}

// Bind gives a session its own rules (a command with screen_* lists).
func (w *StateWatcher) Bind(id SessionID, r agentstate.Rules) { w.rules.bind(id, r) }

// PostNotice appends a notice and wakes the subscribers (tests; the
// watcher posts its own through the same path).
func (w *StateWatcher) PostNotice(n ActivityNotice) {
	w.mu.Lock()
	w.post(n)
	w.mu.Unlock()
	w.bc.Signal()
}

// post numbers and stores n. Caller holds w.mu.
func (w *StateWatcher) post(n ActivityNotice) {
	w.seq++
	n.Seq = w.seq
	w.notices = append(w.notices, n)
	if len(w.notices) > noticeRing {
		w.notices = w.notices[len(w.notices)-noticeRing:]
	}
}

// rulesFor: the bound rules, else the built-ins of a known agent. Terminals
// and custom commands without bound rules are never classified. dedicated
// reports rules that know this agent's screens (not the generic set).
func (w *StateWatcher) rulesFor(info agentsession.Info) (r agentstate.Rules, dedicated, ok bool) {
	if r, bound := w.rules.get(info.ID); bound {
		return r, true, true
	}
	if info.Terminal || info.AgentID == "" {
		return agentstate.Rules{}, false, false
	}
	return agentstate.DefaultRules(info.AgentID), agentstate.HasDefaults(info.AgentID), true
}

// observe classifies every running session once and folds the result into
// the states, posting the notices the transitions call for. An unknown
// screen changes nothing: output lands mid-redraw often enough that acting
// on it would flap.
func (w *StateWatcher) observe(now time.Time) {
	if w.src == nil {
		return
	}
	statesMu.Lock()
	grace, stall := stateGrace, stallAfter
	statesMu.Unlock()
	infos := w.src.List()
	live := map[SessionID]bool{}
	changed := false
	w.mu.Lock()
	for _, info := range infos {
		if info.State != agentsession.Running {
			continue
		}
		live[info.ID] = true
		rules, dedicated, ok := w.rulesFor(info)
		if !ok {
			continue
		}
		text, ok := w.src.Text(info.ID)
		if !ok {
			continue
		}
		lines := agentstate.Tail(text, stateTailLines)
		st := agentstate.Classify(rules, lines)
		prev := w.states[info.ID]
		next := prev
		switch st {
		case agentstate.Question:
			next.Options = agentstate.DialogOptions(text, lines)
		case agentstate.Unknown:
		default:
			next.Options = nil
		}
		note := func(kind string, quiet time.Duration) {
			w.post(ActivityNotice{ID: info.ID, Kind: kind, Label: info.Label, Dir: info.Dir, Quiet: quiet})
			changed = true
		}
		trusted := now.Sub(info.Started) >= grace
		if st != agentstate.Unknown && st != prev.State {
			next.State, next.Since = st, now
			changed = true
			switch {
			case st == agentstate.Question && trusted:
				note("question", 0)
			case st == agentstate.Question:
				w.pendingQ[info.ID] = true
			case st == agentstate.Waiting && prev.State == agentstate.Working && trusted:
				note("idle", 0)
			}
		} else if w.pendingQ[info.ID] && trusted {
			// The question that opened inside the grace is still up: the
			// user must hear about it (a trust dialog at start).
			delete(w.pendingQ, info.ID)
			if next.State == agentstate.Question {
				note("question", 0)
			}
		}
		if next.State != agentstate.Question {
			delete(w.pendingQ, info.ID)
		}
		next.StepFor = agentstate.StepDuration(lines)
		last := w.src.LastOutput(info.ID)
		stalled := !last.IsZero() && now.Sub(last) > stall &&
			(next.State == agentstate.Working || (next.State == agentstate.Unknown && dedicated))
		if stalled != prev.Stalled {
			changed = true
			if stalled {
				note("stalled", now.Sub(last))
			}
		}
		next.Stalled = stalled
		if !slices.Equal(prev.Options, next.Options) {
			changed = true
		}
		w.states[info.ID] = next
	}
	for id := range w.states {
		if !live[id] {
			delete(w.states, id)
			delete(w.pendingQ, id)
			changed = true
		}
	}
	w.mu.Unlock()
	w.rules.keep(live)
	if changed {
		w.bc.Signal()
	}
}

// run watches m until close: the manager's signal re-syncs the per-session
// subscriptions (start, exit, remove), a session's output is classified
// once it settles, and the tick serves the stall clock.
func (w *StateWatcher) run(m *agentsession.Manager) {
	defer w.wg.Done()
	mch, cancel := m.Subscribe()
	defer cancel()
	dirty := make(chan struct{}, 1)
	subs := map[SessionID]chan struct{}{}
	defer func() {
		for _, stop := range subs {
			close(stop)
		}
	}()
	resync := func() {
		live := map[SessionID]bool{}
		for _, info := range m.List() {
			if info.State != agentsession.Running {
				continue
			}
			live[info.ID] = true
			if _, ok := subs[info.ID]; ok {
				continue
			}
			sess, ok := m.Get(info.ID)
			if !ok {
				continue
			}
			ch, cancelSub := sess.Subscribe()
			stop := make(chan struct{})
			subs[info.ID] = stop
			w.wg.Add(1)
			go func() {
				defer w.wg.Done()
				defer cancelSub()
				for {
					select {
					case <-stop:
						return
					case <-ch:
						select {
						case dirty <- struct{}{}:
						default:
						}
					}
				}
			}()
		}
		for id, stop := range subs {
			if !live[id] {
				close(stop)
				delete(subs, id)
			}
		}
	}
	resync()
	w.observe(time.Now())
	tick := time.NewTicker(stateTick)
	defer tick.Stop()
	var settle <-chan time.Time
	for {
		select {
		case <-w.stop:
			return
		case <-mch:
			resync()
			w.observe(time.Now())
		case <-dirty:
			if settle == nil {
				settle = time.After(stateCoalesce)
			}
		case <-settle:
			settle = nil
			w.observe(time.Now())
		case <-tick.C:
			w.observe(time.Now())
		}
	}
}

// close stops the watcher and waits for its goroutines (every session
// subscription is cancelled by then).
func (w *StateWatcher) close() {
	close(w.stop)
	w.wg.Wait()
}

var (
	statesMu    sync.Mutex
	statesW     *StateWatcher
	statesRules = newRuleStore()
)

// SessionStates is the process-global watcher over Sessions(), started on
// first use. Like the manager it lives outside every Service, so it
// survives a repo switch. A frontend that shows states calls it once at
// startup; readers that must not start it use SessionActivityOf.
func SessionStates() *StateWatcher {
	m := Sessions()
	statesMu.Lock()
	defer statesMu.Unlock()
	if statesW == nil {
		statesW = newStateWatcher(managerSource{m}, statesRules)
		statesW.wg.Add(1)
		go statesW.run(m)
	}
	return statesW
}

// SessionActivityOf is a session's activity when a watcher is running;
// it never starts one.
func SessionActivityOf(id SessionID) (SessionActivity, bool) {
	statesMu.Lock()
	w := statesW
	statesMu.Unlock()
	if w == nil {
		return SessionActivity{}, false
	}
	return w.Get(id)
}

// UseSessionStates installs w as the process-global watcher (tests) and
// returns the restore.
func UseSessionStates(w *StateWatcher) func() {
	statesMu.Lock()
	prev := statesW
	statesW = w
	statesMu.Unlock()
	return func() {
		statesMu.Lock()
		statesW = prev
		statesMu.Unlock()
	}
}

// resetSessionStates stops the watcher of a manager that is being swapped
// out; the next SessionStates() binds the manager then current.
func resetSessionStates() {
	statesMu.Lock()
	w := statesW
	statesW = nil
	statesRules = newRuleStore()
	statesMu.Unlock()
	if w != nil && w.src != nil {
		w.close()
	}
}

// bindSessionRules binds rules to a session of the process-global manager.
func bindSessionRules(id SessionID, r agentstate.Rules) {
	statesMu.Lock()
	s := statesRules
	statesMu.Unlock()
	s.bind(id, r)
}

// SessionRules compiles a command's own screen rules. custom is false when
// it has none (the agent's built-ins apply) and when a pattern is invalid
// (err says which; the built-ins apply then too). A list the block sets
// replaces the agent's built-in list of that kind; a list it leaves out
// keeps the built-in one (a Claude block with only screen_question must
// not lose the idle box, or every turn end would read as a stall). A
// custom command (no known agent) has only the lists it sets.
func SessionRules(tc config.ToolCommand) (r agentstate.Rules, custom bool, err error) {
	if !tc.HasScreenRules() {
		return agentstate.Rules{}, false, nil
	}
	r, err = agentstate.Compile(tc.ScreenWorking, tc.ScreenWaiting, tc.ScreenQuestion)
	if err != nil {
		return agentstate.Rules{}, false, err
	}
	if id := agentIDFor(tc); agentstate.HasDefaults(id) {
		def := agentstate.DefaultRules(id)
		if len(tc.ScreenWorking) == 0 {
			r.Working = def.Working
		}
		if len(tc.ScreenWaiting) == 0 {
			r.Waiting = def.Waiting
		}
		if len(tc.ScreenQuestion) == 0 {
			r.Question = def.Question
		}
	}
	return r, true, nil
}

// SessionRulesWarning is what a start tells the user when the command's
// screen rules do not compile, "" otherwise. English; the TUI words its own.
func SessionRulesWarning(tc config.ToolCommand) string {
	if _, _, err := SessionRules(tc); err != nil {
		return fmt.Sprintf("screen rules of %s are invalid (%v) — the built-in rules apply", tc.Name, err)
	}
	return ""
}
