package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
)

// histRunner fakes the file-history walk: Stream emits min(-n, total) commits
// c000, c001, … (newest first), then — when block is set — waits for release
// or ctx cancellation. "git rev-parse" answers the pinned start sha.
type histRunner struct {
	total   int
	block   bool
	release chan struct{}
	ctxDone chan struct{}

	mu    sync.Mutex
	walks [][]string
}

func newHistRunner(total int, block bool) *histRunner {
	return &histRunner{total: total, block: block, release: make(chan struct{}), ctxDone: make(chan struct{})}
}

func (r *histRunner) Run(ctx context.Context, name string, argv []string) (gitexec.Result, error) {
	if name == "git rev-parse" {
		return gitexec.Result{Stdout: "feedface\n"}, nil
	}
	return gitexec.Result{}, fmt.Errorf("histRunner: no %q", name)
}

func (r *histRunner) RunEnv(ctx context.Context, name string, argv, _ []string) (gitexec.Result, error) {
	return r.Run(ctx, name, argv)
}

func (r *histRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (gitexec.Result, error) {
	r.mu.Lock()
	r.walks = append(r.walks, argv)
	r.mu.Unlock()
	n := r.total
	for i, a := range argv {
		if a == "-n" && i+1 < len(argv) {
			if v, err := strconv.Atoi(argv[i+1]); err == nil && v < n {
				n = v
			}
		}
	}
	for i := 0; i < n; i++ {
		onLine(fmt.Sprintf("c%03d\x1f\x1fAda\x1f%d\x1fsubject %d", i, 1700000000-i, i))
		onLine("M\ta.go")
	}
	if r.block {
		select {
		case <-r.release:
		case <-ctx.Done():
			close(r.ctxDone)
			return gitexec.Result{}, ctx.Err()
		}
	}
	return gitexec.Result{}, nil
}

func (r *histRunner) walk(i int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.walks[i]
}

func fileHistModel(r *histRunner) (Model, *historyView) {
	m := Model{width: 100, height: 30, svc: domain.New(&git.Repo{Runner: r})}
	h := newHistoryView(navContext{path: "a.go"})
	return m.pushLayer(h), h
}

// drainHistory feeds a walk's messages to the model until its done message.
func drainHistory(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	msg := cmd().(historyChunkMsg)
	for {
		m, _ = m.onHistoryChunk(msg)
		if msg.done {
			return m
		}
		msg = waitHistoryChunk(msg.next)().(historyChunkMsg)
	}
}

// The newest commits land, get selected and start their diff while git is
// still walking; the header counts them until the walk ends.
func TestHistoryStreamFirstBatchBeforeDone(t *testing.T) {
	t.Parallel()
	r := newHistRunner(2, true)
	m, h := fileHistModel(r)
	msg := m.loadHistoryListCmd(h)().(historyChunkMsg)
	if msg.done {
		t.Fatal("first batch must arrive while the walk still runs")
	}
	m, next := m.onHistoryChunk(msg)
	if len(h.commits) != 2 || h.sel != 0 || h.loading || !h.streaming {
		t.Fatalf("after first batch: commits=%d sel=%d loading=%v streaming=%v", len(h.commits), h.sel, h.loading, h.streaming)
	}
	if h.diffTag == "" || next == nil {
		t.Fatal("first batch must select row 0 and load its diff, and re-arm the walk")
	}
	if out := h.render(m, ""); !strings.Contains(out, "loading… 2 found") {
		t.Fatalf("header must count the found commits while loading:\n%s", out)
	}
	close(r.release)
	done := waitHistoryChunk(msg.next)().(historyChunkMsg)
	if !done.done {
		t.Fatalf("expected the done message, got %+v", done)
	}
	m, _ = m.onHistoryChunk(done)
	if h.streaming || h.more {
		t.Fatalf("after done: streaming=%v more=%v", h.streaming, h.more)
	}
	// Only the header: the right pane still shows its diff placeholder.
	if hdr := strings.SplitN(h.render(m, ""), "\n", 2)[0]; strings.Contains(hdr, "loading…") {
		t.Fatalf("header still says loading after the walk ended: %q", hdr)
	}
}

// esc stops the git walk at once.
func TestHistoryEscCancelsWalk(t *testing.T) {
	t.Parallel()
	r := newHistRunner(1, true)
	m, h := fileHistModel(r)
	msg := m.loadHistoryListCmd(h)().(historyChunkMsg)
	m, _ = m.onHistoryChunk(msg)
	m, _ = h.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.topLayer() != nil {
		t.Fatal("esc must pop the history view")
	}
	select {
	case <-r.ctxDone:
	case <-time.After(2 * time.Second):
		t.Fatal("esc did not cancel the git walk")
	}
}

// A chunk for a view that went another way (repo switch, clearLayers) cancels
// the walk and is not re-armed.
func TestHistoryChunkForGoneViewCancels(t *testing.T) {
	t.Parallel()
	r := newHistRunner(1, true)
	m, h := fileHistModel(r)
	msg := m.loadHistoryListCmd(h)().(historyChunkMsg)
	m = m.clearLayers()
	_, cmd := m.onHistoryChunk(msg)
	if cmd != nil {
		t.Fatal("a gone view's walk must not be re-armed")
	}
	select {
	case <-r.ctxDone:
	case <-time.After(2 * time.Second):
		t.Fatal("a gone view's walk was not cancelled")
	}
}

// A superseded walk's chunks are dropped.
func TestHistoryStaleGenDropped(t *testing.T) {
	t.Parallel()
	m, h := fileHistModel(newHistRunner(0, false))
	h.gen = 2
	m, cmd := m.onHistoryChunk(historyChunkMsg{view: h, gen: 1, commits: histFixture().commits, done: true})
	if len(h.commits) != 0 || cmd != nil {
		t.Fatalf("stale chunk applied: commits=%d", len(h.commits))
	}
}

// The whole walk ends in one list, in git's order.
func TestHistoryStreamDrainsAll(t *testing.T) {
	t.Parallel()
	r := newHistRunner(45, false) // > historyBatchMax: several batches
	m, h := fileHistModel(r)
	_ = drainHistory(t, m, m.loadHistoryListCmd(h))
	if len(h.commits) != 45 || h.commits[0].Hash != "c000" || h.commits[44].Hash != "c044" {
		t.Fatalf("drained %d commits, first=%v", len(h.commits), h.commits)
	}
}

// A full page offers load more; ↓ on the last commit re-walks from the
// PINNED start with a page more, drops what is shown, and steps onto the
// first older commit.
func TestHistoryLoadMorePinsStartAndSkipsShown(t *testing.T) {
	t.Parallel()
	r := newHistRunner(historyPage+5, false)
	m, h := fileHistModel(r)
	m = drainHistory(t, m, m.loadHistoryListCmd(h))
	if len(h.commits) != historyPage || !h.more {
		t.Fatalf("first page: %d commits, more=%v", len(h.commits), h.more)
	}
	h.sel = len(h.commits) - 1 // the row ends the list: render with it in view
	if out := h.render(m, ""); !strings.Contains(out, "load 200 older commits") {
		t.Fatalf("a full page must offer load more:\n%s", out)
	}
	m, cmd := h.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if cmd == nil {
		t.Fatal("j on the last commit of a full page must start load more")
	}
	m = drainHistory(t, m, cmd)
	argv := strings.Join(r.walk(1), " ")
	if !strings.Contains(argv, " feedface ") || !strings.Contains(argv, "-n 400") {
		t.Fatalf("load more must walk from the pinned sha with a page more: %s", argv)
	}
	if len(h.commits) != historyPage+5 || h.commits[historyPage].Hash != fmt.Sprintf("c%03d", historyPage) {
		t.Fatalf("load more appended wrong commits: %d, [200]=%v", len(h.commits), h.commits[historyPage].Hash)
	}
	if h.sel != historyPage {
		t.Fatalf("selection must step onto the first older commit, sel=%d", h.sel)
	}
	if h.more {
		t.Fatal("a short second page must clear load more")
	}
}

// A short page is all history: no load-more row, and j on the last commit
// does nothing.
func TestHistoryShortPageOffersNoLoadMore(t *testing.T) {
	t.Parallel()
	r := newHistRunner(5, false)
	m, h := fileHistModel(r)
	m = drainHistory(t, m, m.loadHistoryListCmd(h))
	if h.more {
		t.Fatal("5 < page: more must be false")
	}
	h.sel = len(h.commits) - 1
	if out := h.render(m, ""); strings.Contains(out, "older commits") {
		t.Fatalf("a short page must not offer load more:\n%s", out)
	}
	if _, cmd := h.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}); cmd != nil {
		t.Fatal("j on the last commit of a short page must do nothing")
	}
}

// Final review #1: a stopped walk must release the cmd still waiting for its
// next message — Bubble Tea runs that wait in its own goroutine, which would
// otherwise block forever on every esc mid-walk.
func TestHistoryWaitReturnsAfterStop(t *testing.T) {
	t.Parallel()
	r := newHistRunner(1, true)
	m, h := fileHistModel(r)
	msg := m.loadHistoryListCmd(h)().(historyChunkMsg)
	m, _ = m.onHistoryChunk(msg)
	got := make(chan tea.Msg, 1)
	go func() { got <- waitHistoryChunk(msg.next)() }()
	_, _ = h.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	select {
	case v := <-got:
		if v != nil {
			t.Fatalf("a stopped walk's wait must yield nil, got %#v", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the pending wait never returned after the walk stopped (goroutine leak)")
	}
}

// Final review #2: a view dropped by any teardown (clearLayers here — a
// cherry-pick hand-off, gg session navigate) stops its walk at the end of
// that same Update, not when git happens to find the next commit.
func TestHistoryTeardownStopsWalk(t *testing.T) {
	t.Parallel()
	r := newHistRunner(1, true)
	m, h := fileHistModel(r)
	m.histWalks = &historyWalks{}
	cmd := m.loadHistoryListCmd(h)
	msg := cmd().(historyChunkMsg)
	m, _ = m.onHistoryChunk(msg)
	m = m.clearLayers()
	mm, _ := m.Update(struct{}{})
	_ = mm
	select {
	case <-r.ctxDone:
	case <-time.After(2 * time.Second):
		t.Fatal("a torn-down view's walk kept running")
	}
}

// …while a view merely parked (under a console, or by a files-view hand-off)
// keeps walking: it comes back.
func TestHistorySweepKeepsParkedWalks(t *testing.T) {
	t.Parallel()
	for _, park := range []string{"console", "files"} {
		h := &historyView{}
		stopped := false
		h.cancel = func() { stopped = true }
		m := Model{histWalks: &historyWalks{views: []*historyView{h}}}
		switch park {
		case "console":
			m.console = &consoleState{ret: &consoleReturn{}}
			m.consoleParked = &consoleParked{layers: []layer{h}}
		case "files":
			m.filesReturnLayers = []layer{h}
		}
		m.sweepHistoryWalks()
		if stopped {
			t.Fatalf("%s-parked view's walk was stopped", park)
		}
	}
}

// Final review #3: load more steps onto the first new commit only if the
// cursor is still on the old last commit — never from wherever the user moved
// it while git re-walked the first page.
func TestHistoryLoadMoreAdvanceOnlyFromBoundary(t *testing.T) {
	t.Parallel()
	r := newHistRunner(historyPage+5, false)
	m, h := fileHistModel(r)
	m = drainHistory(t, m, m.loadHistoryListCmd(h))
	h.sel = len(h.commits) - 1
	m, cmd := h.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m, _ = h.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m, _ = h.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	_ = drainHistory(t, m, cmd)
	if h.sel != historyPage-3 {
		t.Fatalf("the user moved to %d; load more moved the cursor to %d", historyPage-3, h.sel)
	}
}
