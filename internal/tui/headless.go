package tui

import (
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/vt"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// Headless drives the real Model without a terminal, for the e2e golden
// screens (docs/superpowers/specs/2026-10-01-tui-e2e-golden-screens-design.md).
// It replaces Bubble Tea's scheduler with its own loop: a key's commands
// run one at a time, in order, until none are left — the settled state —
// and the frame is then painted into an x/vt terminal and read back as
// text. Nothing here waits on a clock: timers are parked (tick.go) and fire
// only when a step asks.
type Headless struct {
	m      Model
	w, h   int
	timers []timerMsg
	opWait *opWaitMsg // the held op waiter (op.go), nil when no op runs
	quit   bool
	// The process-global theme and language at start: parallel scenarios
	// share them, so a step that changes either fails.
	theme, lang string
}

// HeadlessOptions sizes the terminal and isolates machine-global state.
type HeadlessOptions struct {
	Width, Height int    // 0 → 160x40
	StatePath     string // repos.toml (and prompts.toml beside it); "" → none
}

const (
	settleMaxMsgs = 500
	settleMaxTime = 30 * time.Second
)

// NewHeadless builds the model as tui.Run does, in quiet mode, and settles
// its start-up: Init first, then the terminal size, as a real program
// delivers them.
func NewHeadless(svc *domain.Service, opts HeadlessOptions) (*Headless, error) {
	w, hgt := opts.Width, opts.Height
	if w <= 0 || hgt <= 0 {
		w, hgt = 160, 40
	}
	m, _ := prepareModel(svc)
	m.quiet = true
	m.statePath = opts.StatePath
	h := &Headless{m: m, w: w, h: hgt, theme: activeTheme().Name, lang: i18n.ActiveCode()}
	if err := h.settle([]tea.Cmd{h.m.Init()}); err != nil {
		return nil, fmt.Errorf("start-up: %w", err)
	}
	if err := h.deliver(tea.WindowSizeMsg{Width: w, Height: hgt}); err != nil {
		return nil, fmt.Errorf("window size: %w", err)
	}
	return h, nil
}

// Press sends one step token (a multi-rune literal is one press per rune),
// settling after each press.
func (h *Headless) Press(tok string) error {
	if len(tok) > 2 && strings.HasPrefix(tok, "<") && strings.HasSuffix(tok, ">") {
		return fmt.Errorf("%q is a recorder diagnostic, not a key (keyToken's <...> fallback)", tok)
	}
	for _, t := range splitLiteral(tok) {
		k, err := keyMsgFor(t)
		if err != nil {
			return err
		}
		if err := h.deliver(k); err != nil {
			return fmt.Errorf("key %q: %w", t, err)
		}
	}
	return nil
}

// FireTimers fires every timer parked right now, soonest first, settling
// after each; timers they create stay parked for the next call, so a
// self-re-arming tick never loops.
func (h *Headless) FireTimers() error {
	due := h.timers
	h.timers = nil
	sort.SliceStable(due, func(i, j int) bool { return due[i].due < due[j].due })
	for _, tm := range due {
		if err := h.deliver(tm.fire(time.Time{})); err != nil {
			return fmt.Errorf("timer (%s): %w", tm.due, err)
		}
	}
	return nil
}

// Screen is the settled frame as text: one line per terminal row, colours
// dropped, trailing spaces trimmed.
func (h *Headless) Screen() string {
	return strings.TrimSuffix(paintScreen(h.m.View(), h.w, h.h), "\n")
}

// deliver runs one message through the filter and Update, then settles.
func (h *Headless) deliver(msg tea.Msg) error {
	if h.quit {
		return fmt.Errorf("the TUI has quit")
	}
	cmd, err := h.update(msg)
	if err != nil {
		return err
	}
	if err := h.settle([]tea.Cmd{cmd}); err != nil {
		return err
	}
	return h.checkGlobals()
}

// checkGlobals fails when the theme or the UI language changed since start.
func (h *Headless) checkGlobals() error {
	if now := activeTheme().Name; now != h.theme {
		return fmt.Errorf("the step changed the process-global theme (%q → %q); scenarios may not", h.theme, now)
	}
	if now := i18n.ActiveCode(); now != h.lang {
		return fmt.Errorf("the step changed the process-global language (%q → %q); scenarios may not", h.lang, now)
	}
	return nil
}

// update applies quitFilter as the real program does, then Update.
func (h *Headless) update(msg tea.Msg) (tea.Cmd, error) {
	if msg = quitFilter(h.m, msg); msg == nil {
		return nil, nil
	}
	if _, ok := msg.(tea.QuitMsg); ok {
		h.quit = true
		return nil, nil
	}
	nm, cmd := h.m.Update(msg)
	m, ok := nm.(Model)
	if !ok {
		return nil, fmt.Errorf("Update returned %T, not Model", nm)
	}
	h.m = m
	return cmd, nil
}

// settle runs queued commands one at a time until none are left (spec
// §3.5): batches flatten, timers park, the op waiter is held and read only
// while the op is working — never while it waits on a decision, whose
// answer is the next key.
func (h *Headless) settle(queue []tea.Cmd) error {
	start, n := time.Now(), 0
	seen := map[string]int{}
	for {
		for len(queue) > 0 {
			if n++; n > settleMaxMsgs || time.Since(start) > settleMaxTime {
				return fmt.Errorf("no fixed point after %d messages / %s; most frequent: %s", n, time.Since(start).Round(time.Millisecond), topTypes(seen))
			}
			c := queue[0]
			queue = queue[1:]
			if c == nil {
				continue
			}
			msg, err := callBounded(c, settleMaxTime-time.Since(start))
			if err != nil {
				return err
			}
			switch v := msg.(type) {
			case nil:
				continue
			case tea.BatchMsg:
				queue = append(queue, v...)
				continue
			case timerMsg:
				h.timers = append(h.timers, v)
				continue
			case opWaitMsg:
				w := v
				h.opWait = &w
				continue
			}
			switch fmt.Sprintf("%T", msg) {
			case "tea.execMsg":
				return fmt.Errorf("a terminal handover (%T) cannot run headless", msg)
			case "tea.sequenceMsg":
				return fmt.Errorf("tea.Sequence is not supported headless")
			}
			seen[fmt.Sprintf("%T", msg)]++
			cmd, err := h.update(msg)
			if err != nil {
				return err
			}
			if h.quit {
				return nil
			}
			queue = append(queue, cmd)
		}
		// Queue empty: settled, unless an op is working (not waiting on the
		// user) — then its next message is the next thing to happen.
		if h.opWait == nil || h.m.awaitingDecision() {
			return nil
		}
		w := h.opWait
		h.opWait = nil
		queue = append(queue, func() tea.Msg { return <-w.ch })
	}
}

// callBounded runs one command, failing if it does not return within d —
// a command that blocks forever (a channel wait quiet mode missed) is named
// by its function, so the gap is obvious.
func callBounded(c tea.Cmd, d time.Duration) (tea.Msg, error) {
	done := make(chan tea.Msg, 1)
	go func() { done <- c() }()
	select {
	case msg := <-done:
		return msg, nil
	case <-time.After(d):
		return nil, fmt.Errorf("command %s blocked for %s (a never-ending command quiet mode does not gate?)", runtime.FuncForPC(reflect.ValueOf(c).Pointer()).Name(), d.Round(time.Millisecond))
	}
}

// topTypes names the three message types seen most in a settle that found
// no fixed point — the never-ending command quiet mode missed.
func topTypes(seen map[string]int) string {
	type kv struct {
		k string
		v int
	}
	var all []kv
	for k, v := range seen {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v })
	var parts []string
	for i := 0; i < len(all) && i < 3; i++ {
		parts = append(parts, fmt.Sprintf("%s×%d", all[i].k, all[i].v))
	}
	return strings.Join(parts, ", ")
}

// paintScreen writes a frame into an x/vt terminal with autowrap off and
// CRLF line ends (as tui.Run and Bubble Tea's renderer do), then reads the
// cells back, one line per row.
func paintScreen(frame string, w, hgt int) string {
	emu := vt.NewEmulator(w, hgt)
	_, _ = emu.WriteString("\x1b[?7l" + strings.ReplaceAll(frame, "\n", "\r\n"))
	var b strings.Builder
	for y := 0; y < hgt; y++ {
		var row strings.Builder
		for x := 0; x < w; x++ {
			c := emu.CellAt(x, y)
			if c == nil || c.Width == 0 { // a wide glyph's continuation cell
				continue
			}
			if c.Content == "" {
				row.WriteByte(' ')
				continue
			}
			row.WriteString(c.Content)
		}
		b.WriteString(strings.TrimRight(row.String(), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

// Close does what tui.Run does after the program ends — except killing the
// process-global task and session managers: quiet mode never starts either,
// and killing them would end parallel tests' work.
func (h *Headless) Close() {
	m := h.m
	if m.opCancel != nil {
		m.opCancel()
	}
	removeSnapshotFile(m.snapshotPath)
	m = m.closeSteerInbox()
	m = m.releaseKeptInboxes()
	m.closeWeb()
	if m.sessWatch != nil && m.sessWatch.cancel != nil {
		m.sessWatch.cancel()
	}
	if m.taskTrack != nil && m.taskTrack.cancel != nil {
		m.taskTrack.cancel()
	}
}

// opWaitMsg / awaitingDecision: placeholders until the op waiter becomes a
// descriptor (Task 6 of the plan moves both to op.go).
type opWaitMsg struct{ ch chan tea.Msg }

func (m Model) awaitingDecision() bool { return false }
