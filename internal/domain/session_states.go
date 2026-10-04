package domain

// What an agent session is doing — working, idle, asking a question,
// stalled — read off its screen by one process-global watcher. Frontends
// subscribe and read snapshots; each keeps its own cursor into the notice
// ring, so the terminal and the page it hosts never steal each other's
// wakeups (the Broadcaster rule).

import (
	"fmt"
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
	Settle  time.Duration    // the hold an idle passed (agent_wait trusts it after this); 0 otherwise
	// ReadyAt: when the state counts as settled — an idle after the hold it
	// passed, any other state at Since.
	ReadyAt time.Time
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
	Kind  string        // "question" | "idle" | "stalled" | "report"
	Label string        // the session's label
	Dir   string        // its worktree
	Quiet time.Duration // stalled: how long nothing was printed (or, Spinning, nothing but the spinner moved)
	// Spinning: the stall is a spinner that kept ticking with no progress
	// (a hung API call), not silence.
	Spinning bool
	Text     string // report: its first line
}

// Timing rules. Variables so tests (UseStateTiming) can shrink them.
var (
	// stateGrace: the first stretch after a start is not trusted (sign-in
	// spinners read as working), so no notice is posted before it ends.
	stateGrace = 30 * time.Second
	// stallAfter: no output for this long while working is a stall.
	stallAfter = 120 * time.Second
	// spinStallAfter: only the spinner moving (glyph, timer) this long while
	// working is a stall too — a hung API call keeps the timer ticking. Long:
	// a think shows nothing else either (live capture 2026-10-04).
	spinStallAfter = 10 * time.Minute
	// stateTick serves the stall clock; output wakes the watcher itself.
	stateTick = 2 * time.Second
	// stateCoalesce: a burst of output is classified once it settles.
	stateCoalesce = 300 * time.Millisecond
	// idleSettle: agent_wait trusts an idle only after it has held this long
	// (the working→idle→working flicker guard).
	idleSettle = 2 * time.Second
	// titleSettle: the idle hold when the agent's title (or progress report)
	// says idle too — the title kept the turn working, so a short guard
	// against a redraw is enough (herdr holds 700 ms).
	titleSettle = 700 * time.Millisecond
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

// UseIdleSettle replaces agent_wait's idle settle (tests) and returns the
// restore.
func UseIdleSettle(d time.Duration) func() {
	statesMu.Lock()
	prev := idleSettle
	idleSettle = d
	statesMu.Unlock()
	return func() { statesMu.Lock(); idleSettle = prev; statesMu.Unlock() }
}

// UseTitleSettle replaces the titled idle hold (tests) and returns the
// restore.
func UseTitleSettle(d time.Duration) func() {
	statesMu.Lock()
	prev := titleSettle
	titleSettle = d
	statesMu.Unlock()
	return func() { statesMu.Lock(); titleSettle = prev; statesMu.Unlock() }
}

// stateSource is what the watcher reads: the manager in production, a fake
// feed in tests.
// stateSource is where the watcher observes sessions.
type stateSource interface {
	List() []agentsession.Info
	// Observe is the session's screen, title and progress as one
	// observation, and when it last printed.
	Observe(id agentsession.ID) (agentstate.Observation, time.Time, bool)
}

type managerSource struct{ m *agentsession.Manager }

func (s managerSource) List() []agentsession.Info { return s.m.List() }
func (s managerSource) Observe(id agentsession.ID) (agentstate.Observation, time.Time, bool) {
	sess, ok := s.m.Get(id)
	if !ok {
		return agentstate.Observation{}, time.Time{}, false
	}
	text, sig := sess.Text(), sess.Signals()
	return agentstate.Observation{Text: text, Lines: agentstate.Tail(text, stateTailLines), Title: sig.Title, Progress: sig.Progress}, sess.LastOutput(), true
}

// ruleStore holds the profiles bound to sessions at start (a command with
// its own screen_* lists). It lives outside the watcher so a start never
// depends on the watcher running.
type ruleStore struct {
	mu   sync.Mutex
	m    map[SessionID]agentstate.Profile
	seen map[SessionID]bool // ids an observe has seen live: only those are pruned
}

func newRuleStore() *ruleStore {
	return &ruleStore{m: map[SessionID]agentstate.Profile{}, seen: map[SessionID]bool{}}
}

func (s *ruleStore) bind(id SessionID, p agentstate.Profile) {
	s.mu.Lock()
	s.m[id] = p
	s.mu.Unlock()
}

func (s *ruleStore) get(id SessionID) (agentstate.Profile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.m[id]
	return p, ok
}

// keep prunes the profiles of sessions that were live and are gone. A set bound
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

	mu sync.Mutex
	// states: each session's published activity (what Get serves).
	states map[SessionID]SessionActivity
	// trackers: each running session's state over time (holds, grace,
	// stalls) — session_tracker.go.
	trackers map[SessionID]*sessionTracker
	notices  []ActivityNotice
	seq      uint64

	stop chan struct{}
	wg   sync.WaitGroup
}

func newStateWatcher(src stateSource, rules *ruleStore) *StateWatcher {
	return &StateWatcher{src: src, rules: rules, states: map[SessionID]SessionActivity{},
		trackers: map[SessionID]*sessionTracker{}, stop: make(chan struct{})}
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

// Bind gives a session its own profile (a command with screen_* lists).
func (w *StateWatcher) Bind(id SessionID, p agentstate.Profile) { w.rules.bind(id, p) }

// PostNotice appends a notice and wakes the subscribers (tests; the
// watcher posts its own through the same path).
func (w *StateWatcher) PostNotice(n ActivityNotice) {
	w.mu.Lock()
	w.post(n)
	w.mu.Unlock()
	w.bc.Signal()
}

// Wake wakes the subscribers without a notice (a report whose notice the
// gap held back: rows and tours read the level).
func (w *StateWatcher) Wake() { w.bc.Signal() }

// post numbers and stores n. Caller holds w.mu.
func (w *StateWatcher) post(n ActivityNotice) {
	w.seq++
	n.Seq = w.seq
	w.notices = append(w.notices, n)
	if len(w.notices) > noticeRing {
		w.notices = w.notices[len(w.notices)-noticeRing:]
	}
}

// profileFor: the bound profile, else the built-in one of a known agent.
// Terminals and custom commands without a bound profile are never
// classified.
func (w *StateWatcher) profileFor(info agentsession.Info) (agentstate.Profile, bool) {
	if p, bound := w.rules.get(info.ID); bound {
		return p, true
	}
	if info.Terminal || info.AgentID == "" {
		return agentstate.Profile{}, false
	}
	return agentstate.ForAgent(info.AgentID), true
}

// observe is THE entry of state detection. For each running session it
// observes (stateSource.Observe: screen, title, progress), reads the
// observation with the session's profile (agentstate: the screen, title and
// progress detectors composed per agent), and folds the reading into the
// session's tracker (sessionTracker.Step: holds, grace, stalls), publishing
// the activity and its notices. Screens are read before taking w.mu: each
// read takes its session's emulator lock, and Get must not wait on that.
// It returns how soon a pending idle wants another look (0: none).
func (w *StateWatcher) observe(now time.Time) (recheck time.Duration) {
	if w.src == nil {
		return 0
	}
	tm := currentTiming()
	type read struct {
		info      agentsession.Info
		dedicated bool
		rd        agentstate.Reading
		last      time.Time
	}
	var reads []read
	live := map[SessionID]bool{}
	for _, info := range w.src.List() {
		if info.State != agentsession.Running {
			continue
		}
		live[info.ID] = true
		prof, ok := w.profileFor(info)
		if !ok {
			continue
		}
		obs, last, ok := w.src.Observe(info.ID)
		if !ok {
			continue
		}
		reads = append(reads, read{info: info, dedicated: prof.Dedicated, rd: prof.Read(obs), last: last})
	}
	changed := false
	w.mu.Lock()
	for _, r := range reads {
		t := w.trackers[r.info.ID]
		if t == nil {
			t = &sessionTracker{}
			w.trackers[r.info.ID] = t
		}
		t.dedicated = r.dedicated
		act, notes, ch, again := t.Step(r.rd, r.info, r.last, now, tm)
		w.states[r.info.ID] = act
		for _, n := range notes {
			w.post(n)
		}
		changed = changed || ch
		if again > 0 && (recheck == 0 || again < recheck) {
			recheck = again
		}
	}
	for id := range w.trackers {
		if !live[id] {
			delete(w.trackers, id)
		}
	}
	for id := range w.states {
		if !live[id] {
			delete(w.states, id)
			changed = true
		}
	}
	w.mu.Unlock()
	w.rules.keep(live)
	if changed {
		w.bc.Signal()
	}
	return recheck
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
	// promote: a pending idle comes due (an idle agent prints nothing that
	// would wake the loop sooner than the tick).
	var settle, promote <-chan time.Time
	observe := func() {
		if d := w.observe(time.Now()); d > 0 {
			promote = time.After(d)
		} else {
			promote = nil
		}
	}
	resync()
	observe()
	tick := time.NewTicker(stateTick)
	defer tick.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-mch:
			resync()
			observe()
		case <-dirty:
			if settle == nil {
				settle = time.After(stateCoalesce)
			}
		case <-settle:
			settle = nil
			observe()
		case <-promote:
			observe()
		case <-tick.C:
			observe()
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

// SessionNoticeSeq is the running watcher's newest notice number, 0 when
// none runs; it never starts one. A frontend that starts now reads after it.
func SessionNoticeSeq() uint64 {
	statesMu.Lock()
	w := statesW
	statesMu.Unlock()
	if w == nil {
		return 0
	}
	return w.NoticeSeq()
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

// bindSessionProfile binds a profile to a session of the process-global
// manager.
func bindSessionProfile(id SessionID, p agentstate.Profile) {
	statesMu.Lock()
	s := statesRules
	statesMu.Unlock()
	s.bind(id, p)
}

// SessionProfile is the profile of a command with its own screen_* lists.
// custom is false when it has none (the agent's built-ins apply) and when a
// pattern is invalid (err says which; the built-ins apply then too). A list
// the block sets replaces the agent's built-in list of that kind; a list it
// leaves out keeps the built-in one (a Claude block with only
// screen_question must not lose the idle box, or every turn end would read
// as a stall), and the agent's own menus, title and progress parts stay
// (agentstate.WithScreen). A custom command (no known agent) has only the
// lists it sets.
func SessionProfile(tc config.ToolCommand) (p agentstate.Profile, custom bool, err error) {
	if !tc.HasScreenRules() {
		return agentstate.Profile{}, false, nil
	}
	p, err = agentstate.WithScreen(agentIDFor(tc), tc.ScreenWorking, tc.ScreenWaiting, tc.ScreenQuestion)
	if err != nil {
		return agentstate.Profile{}, false, err
	}
	return p, true, nil
}

// SessionRulesWarning is what a start tells the user when the command's
// screen rules do not compile, "" otherwise. English; the TUI words its own.
func SessionRulesWarning(tc config.ToolCommand) string {
	if _, _, err := SessionProfile(tc); err != nil {
		return fmt.Sprintf("screen rules of %s are invalid (%v) — the built-in rules apply", tc.Name, err)
	}
	return ""
}
