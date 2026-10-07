package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/filewatch"
	"github.com/homeend/gigagit/internal/i18n"
)

// An agent's send waits in this repository's queue until the user answers it
// here (spec 2026-10-07 §3.7). The queue file is watched (a wake-up) and
// stat-polled on the heartbeat (the truth); every waiting entry is a notice.

const pendingPollEvery = 2 * time.Second

// pendingWatchState is the queue watcher on Model (pointer fields survive
// the value copy through the model).
type pendingWatchState struct {
	path     string
	w        *filewatch.Watcher
	stat     diskStat
	lastPoll time.Time
	polling  bool
}

type pendingSendsMsg struct {
	gen  int
	list []domain.PendingSend
	stat diskStat
}

type pendingStatMsg struct {
	gen  int
	path string
	stat diskStat
}

type pendingWatchMsg struct {
	gen  int
	path string
	w    *filewatch.Watcher // nil: no watcher here (poll only)
}

type pendingWakeMsg struct{ gen int }

func pendingSendNoticeID(id string) string { return "pending_send_" + id }

// closePendingWatch drops the queue watcher and the queued sends it fed (a
// repo switch, quit).
func (m Model) closePendingWatch() Model {
	if m.pendingWatch != nil && m.pendingWatch.w != nil {
		m.pendingWatch.w.Close()
	}
	m.pendingWatch, m.pendingSends = nil, nil
	return m
}

// pendingSendsReadCmd reads the queue (gen = m.noticeGen when asked).
func (m Model) pendingSendsReadCmd(gen int) tea.Cmd {
	svc := m.svc
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()
		p, _ := svc.PendingSendsPath(ctx)
		st := statDisk(p)
		list, err := svc.PendingSends(ctx)
		if err != nil {
			return nil // best-effort, like every notice source
		}
		return pendingSendsMsg{gen: gen, list: list, stat: st}
	}
}

// pendingWatchCmd finds the queue file and, outside headless runs, watches it.
func (m Model) pendingWatchCmd(gen int) tea.Cmd {
	svc, quiet := m.svc, m.quiet
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		p, err := svc.PendingSendsPath(context.Background())
		if err != nil || p == "" {
			return nil
		}
		msg := pendingWatchMsg{gen: gen, path: p}
		if !quiet { // a never-ending listen must not run under Headless
			if w, err := filewatch.New(200 * time.Millisecond); err == nil {
				w.Set([]string{p})
				msg.w = w
			}
		}
		return msg
	}
}

func pendingListenCmd(w *filewatch.Watcher, gen int) tea.Cmd {
	return func() tea.Msg {
		if _, ok := <-w.Events(); !ok {
			return nil
		}
		return pendingWakeMsg{gen: gen}
	}
}

// pendingSendsTick is the heartbeat's stat poll.
func (m Model) pendingSendsTick(now time.Time) (Model, tea.Cmd) {
	pw := m.pendingWatch
	if pw == nil || pw.path == "" || pw.polling || now.Sub(pw.lastPoll) < pendingPollEvery {
		return m, nil
	}
	pw.polling, pw.lastPoll = true, now
	gen, path := m.noticeGen, pw.path
	return m, func() tea.Msg { return pendingStatMsg{gen: gen, path: path, stat: statDisk(path)} }
}

func (m Model) handlePendingWatch(msg pendingWatchMsg) (Model, tea.Cmd) {
	if msg.gen != m.noticeGen {
		if msg.w != nil {
			msg.w.Close()
		}
		return m, nil
	}
	if m.pendingWatch != nil && m.pendingWatch.w != nil {
		m.pendingWatch.w.Close()
	}
	m.pendingWatch = &pendingWatchState{path: msg.path, w: msg.w}
	if msg.w == nil {
		return m, nil
	}
	return m, pendingListenCmd(msg.w, msg.gen)
}

func (m Model) handlePendingStat(msg pendingStatMsg) (Model, tea.Cmd) {
	pw := m.pendingWatch
	if msg.gen != m.noticeGen || pw == nil {
		return m, nil
	}
	pw.polling = false
	if pw.stat.known && pw.stat.same(msg.stat) {
		return m, nil
	}
	return m, m.pendingSendsReadCmd(msg.gen)
}

func (m Model) handlePendingSends(msg pendingSendsMsg) (Model, tea.Cmd) {
	if msg.gen != m.noticeGen {
		return m, nil
	}
	if m.pendingWatch != nil {
		m.pendingWatch.stat = msg.stat
	}
	var waiting []domain.PendingSend
	for _, e := range msg.list {
		if e.State == domain.PendingWaiting {
			waiting = append(waiting, e)
		}
	}
	prev := m.noticeIDs()
	m.pendingSends = waiting
	m = m.rebuildNotices()
	return m.armBlinkForNew(prev)
}

// pendingSendNotices are the waiting entries as notices.
func pendingSendNotices(m Model) []notice {
	var out []notice
	for _, e := range m.pendingSends {
		e := e
		detail := []string{i18n.T("asked %s", ageString(clock.Now(), e.Created))}
		if e.Request.Event != "" {
			detail = append(detail, i18n.T("asks for: %s", optionDisplayName(e.Request.Event)))
		}
		detail = append(detail, i18n.T("Nothing is posted until you confirm."))
		out = append(out, notice{
			id: pendingSendNoticeID(e.ID), repoKey: m.repoHealth.GitCommonDir,
			title: pendingSendTitle(e), detail: detail,
			actions: []noticeAction{
				{label: i18n.T("Review and send…"), sourced: true, run: func(m Model) (Model, tea.Cmd) {
					return m.forgeSendCmd(e.Request, e.ID)
				}},
				{label: i18n.T("Reject"), sourced: true, run: func(m Model) (Model, tea.Cmd) {
					return m, m.pendingRejectCmd(e.ID)
				}},
				{label: i18n.T("Later"), sourced: true},
			},
		})
	}
	return out
}

// pendingSendTitle says who wants what sent where — one literal per shape.
func pendingSendTitle(e domain.PendingSend) string {
	r, who := e.Request, e.Requester
	switch {
	case r.Finish:
		return i18n.T("%s wants to finish the pending review on #%d", who, r.PR)
	case r.Discard:
		return i18n.T("%s wants to discard the pending review on #%d", who, r.PR)
	case r.Review != "":
		return i18n.T("%s wants to send an AI review to #%d", who, r.PR)
	case r.Mine:
		return i18n.T("%s wants to send the draft review to #%d", who, r.PR)
	case len(r.Resolve) == 1:
		return i18n.T("%s wants to resolve 1 thread on #%d", who, r.PR)
	case len(r.Resolve) > 1:
		return i18n.T("%s wants to resolve %d threads on #%d", who, len(r.Resolve), r.PR)
	case len(r.Unresolve) == 1:
		return i18n.T("%s wants to reopen 1 thread on #%d", who, r.PR)
	case len(r.Unresolve) > 1:
		return i18n.T("%s wants to reopen %d threads on #%d", who, len(r.Unresolve), r.PR)
	case len(r.Notes) == 1:
		return i18n.T("%s wants to send 1 note to #%d", who, r.PR)
	case len(r.Notes) > 1:
		return i18n.T("%s wants to send %d notes to #%d", who, len(r.Notes), r.PR)
	}
	return i18n.T("%s wants to post a verdict on #%d", who, r.PR)
}

// pendingRejectCmd answers the agent "rejected" and re-reads the queue.
func (m Model) pendingRejectCmd(id string) tea.Cmd {
	svc, gen := m.svc, m.noticeGen
	return func() tea.Msg {
		ctx := context.Background()
		_, _ = svc.PendingSendFinish(ctx, id, domain.PendingRejected, "rejected in gg")
		list, _ := svc.PendingSends(ctx)
		return pendingSendsMsg{gen: gen, list: list}
	}
}

// pendingFinishCmd writes an approved send's outcome back (sent, rejected at
// the confirm, failed) and re-reads the queue; an approver who could not
// answer leaves it waiting.
func (m Model) pendingFinishCmd(id string, res engine.Result, err error) tea.Cmd {
	svc, gen := m.svc, m.noticeGen
	return func() tea.Msg {
		ctx := context.Background()
		if st, out, waiting := domain.PendingOutcome(res, err); !waiting {
			_, _ = svc.PendingSendFinish(ctx, id, st, out)
		}
		list, _ := svc.PendingSends(ctx)
		return pendingSendsMsg{gen: gen, list: list}
	}
}
