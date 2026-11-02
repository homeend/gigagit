package tui

import (
	"context"
	"os"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// Memory documents (spec 2026-11-02-memory-docs-design). A document with no
// file can be asked for only through gg, so gg keeps a separate presence
// (steer.MemdocsPresence) in the inbox of each worktree holding one. Other
// verbs route as they always did: the presence is read by memdoc_* routing.

// memdocInboxes is the Model's memory of those inboxes; a pointer so it
// survives the value copy.
type memdocInboxes struct {
	byWorktree map[string]string
	pending    map[string]bool
	failed     map[string]time.Time                                       // a resolve that failed: not again until the backoff passes
	resolve    func(ctx context.Context, worktree string) (string, error) // a test seam
}

func newMemdocInboxes() *memdocInboxes {
	return &memdocInboxes{byWorktree: map[string]string{}, pending: map[string]bool{}, failed: map[string]time.Time{}, resolve: domain.SessionInboxOf}
}

const (
	// memdocResolveWait bounds finding an inbox: under what the CLI waits for
	// a reply, so a slow filesystem does not read as "gg did not answer".
	memdocResolveWait = time.Second
	// memdocResolveBackoff spaces out retries for a worktree that does not
	// resolve (removed, an unmounted drive): a heartbeat must not cost a git.
	memdocResolveBackoff = time.Minute
)

// resolveBounded is resolve under memdocResolveWait, on a context of its own
// (a hung call must not outlive the wait).
func resolveBounded(resolve func(context.Context, string) (string, error), wt string) string {
	ctx, cancel := context.WithTimeout(context.Background(), memdocResolveWait)
	defer cancel()
	type result struct{ dir string }
	done := make(chan result, 1)
	go func() {
		dir, _ := resolve(ctx, wt)
		done <- result{dir}
	}()
	select {
	case r := <-done:
		return r.dir
	case <-ctx.Done():
		return ""
	}
}

type memdocInboxMsg struct {
	worktree, inbox string
}

// otherGGHolds reports whether another live gg's TUI answers for dir.
func otherGGHolds(dir string) bool {
	p, live := steer.Live(dir, steer.TUIPresence)
	return live && p.PID != os.Getpid()
}

// steerMemdoc answers a memdoc_* verb: no screen, no refusal for what the user
// is doing. A document's first add resolves the inbox off-thread and writes the
// presence BEFORE the reply, so a link the agent prints works at once.
func (m Model) steerMemdoc(c steer.Command) (Model, tea.Cmd) {
	if m.docs == nil {
		return m, m.answerSteer(c, steerFail(c, "no document store in this gg"))
	}
	rep := agentdocs.AnswerMemdoc(m.docs, domain.CheckoutKey(c.Worktree), c)
	if !rep.OK || c.Cmd != "memdoc_add" || m.memdocs == nil {
		return m, m.answerSteer(c, rep)
	}
	if _, known := m.memdocs.byWorktree[c.Worktree]; known || m.memdocs.pending[c.Worktree] {
		return m, m.answerSteer(c, rep)
	}
	m.memdocs.pending[c.Worktree] = true
	resolve, wt, pres := m.memdocs.resolve, c.Worktree, m.tuiPresence()
	answer := m.answerSteer(c, rep)
	return m, func() tea.Msg {
		dir := resolveBounded(resolve, wt)
		if dir != "" && !otherGGHolds(dir) {
			_ = steer.Touch(dir, steer.MemdocsPresence, pres)
		}
		if answer != nil {
			answer()
		}
		return memdocInboxMsg{worktree: wt, inbox: dir}
	}
}

// resolveMemdocInboxes finds the inbox of a worktree whose memory document did
// not arrive by a verb (the n window, the hosted page).
func (m Model) resolveMemdocInboxes() tea.Cmd {
	if m.memdocs == nil || m.docs == nil || !m.steerActive() {
		return nil
	}
	var cmds []tea.Cmd
	for _, wt := range m.docs.MemoryWorktrees() {
		if _, known := m.memdocs.byWorktree[wt]; known || m.memdocs.pending[wt] {
			continue
		}
		if at, failed := m.memdocs.failed[wt]; failed && time.Since(at) < memdocResolveBackoff {
			continue
		}
		m.memdocs.pending[wt] = true
		resolve, wt := m.memdocs.resolve, wt
		cmds = append(cmds, func() tea.Msg {
			dir := resolveBounded(resolve, wt)
			return memdocInboxMsg{worktree: wt, inbox: dir}
		})
	}
	return tea.Batch(cmds...)
}

func (m Model) onMemdocInbox(msg memdocInboxMsg) Model {
	if m.memdocs != nil {
		delete(m.memdocs.pending, msg.worktree)
		if msg.inbox != "" {
			m.memdocs.byWorktree[msg.worktree] = msg.inbox
			delete(m.memdocs.failed, msg.worktree)
		} else {
			m.memdocs.failed[msg.worktree] = time.Now()
		}
	}
	return m.tendMemdocPresence()
}

// tendMemdocPresence runs on the heartbeat: keep the presence of each inbox
// with a memory document, release the ones whose last document went.
func (m Model) tendMemdocPresence() Model {
	if m.memdocs == nil || m.docs == nil {
		return m
	}
	want := map[string]bool{}
	for _, wt := range m.docs.MemoryWorktrees() {
		want[wt] = true
	}
	for wt, dir := range m.memdocs.byWorktree {
		switch {
		case !want[wt]:
			releaseMemdocPresence(dir)
			delete(m.memdocs.byWorktree, wt)
		case !m.steerActive() || otherGGHolds(dir):
			releaseMemdocPresence(dir)
		default:
			_ = steer.Touch(dir, steer.MemdocsPresence, m.tuiPresence())
		}
	}
	return m
}

// releaseMemdocPresence removes dir's presence when it is this process's.
func releaseMemdocPresence(dir string) {
	if p, live := steer.Live(dir, steer.MemdocsPresence); !live || p.PID == os.Getpid() {
		steer.Remove(dir, steer.MemdocsPresence)
	}
}

// releaseMemdocInboxes drops every memdocs presence (clean exit, a rewrite).
func (m Model) releaseMemdocInboxes() {
	if m.memdocs == nil {
		return
	}
	for _, dir := range m.memdocs.byWorktree {
		releaseMemdocPresence(dir)
	}
}

// memdocOnlyInboxes are the inboxes held for memory documents alone, sorted:
// drained for memdoc_* verbs, and nothing else is answered there as gg's own.
func (m Model) memdocOnlyInboxes() []string {
	if m.memdocs == nil {
		return nil
	}
	var out []string
	for _, dir := range m.memdocs.byWorktree {
		if dir != m.steerDir && !m.keptSteer[dir] && !otherGGHolds(dir) {
			out = append(out, dir)
		}
	}
	sort.Strings(out)
	return out
}
