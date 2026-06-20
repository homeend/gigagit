package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
)

// confState is the conflict-resolution process's state-machine state.
type confState int

const (
	confListing   confState = iota // showing the conflicted-file list; awaiting a file + action
	confPicking                    // handed off to the full-screen line editor (Task 4)
	confWorking                    // a job is running (Task 3)
	confReporting                  // a job failed; showing the error (Task 3)
	confFinishing                  // continue/abort completed; releasing the slot (Task 5)
)

// conflictProcess resolves an in-progress merge/rebase as a process: it owns the
// interface, drives the small resolve/continue/abort jobs, and presents the
// conflicted-file list (and, later, the line editor) as passive windows. The
// conflict popup it replaces used to manage its own lifecycle and re-open
// itself; here the process owns the flow.
type conflictProcess struct {
	st         confState
	files      []model.FileStatus // conflicted files, refreshed from status after each job
	sel        int
	src        domain.ConflictState // merge/rebase parties, for the header
	inProgress string               // "merge"/"rebase"/"" — set by the probe (Task 5)
	mode       dispMode             // text display mode; z cycles
	hscroll    int                  // modeScroll horizontal offset
}

// startConflictProcess fills the active-process slot from the current conflicted
// status. A no-op when nothing is conflicted (the caller stays as it was).
func startConflictProcess(m Model) (Model, tea.Cmd) {
	files := m.status.Conflicts()
	if len(files) == 0 {
		return m, nil
	}
	m.proc = &conflictProcess{st: confListing, files: files, src: m.conflict}
	return m, nil
}

func (p *conflictProcess) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch p.st {
	case confListing:
		return p.updateListing(m, msg)
	}
	return m, nil
}

func (p *conflictProcess) updateListing(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "L", "esc": // Leave — step out, repo as-is; detection re-offers it later
		m.proc = nil
		return m, nil
	case "z":
		p.mode = p.mode.next()
		p.hscroll = 0
	case "up", "k":
		if p.sel > 0 {
			p.sel--
		}
	case "down", "j":
		if p.sel < len(p.files)-1 {
			p.sel++
		}
	}
	// Per-file resolve actions land in Task 3; continue/abort in Task 5.
	return m, nil
}

func (p *conflictProcess) render(m Model, below string) string {
	w, h := m.overlayDims()
	switch p.st {
	case confListing:
		box := conflictListBox(m, p.files, p.sel, p.src, p.inProgress, p.mode, p.hscroll)
		return overlayCenter(clipToHeight(below, h), box, w, h)
	}
	return below
}

func (p *conflictProcess) finished(m Model, res engine.Result, err error) (Model, tea.Cmd) {
	return m, nil // Task 3 advances the state machine here
}

func (p *conflictProcess) indicator(m Model) string {
	return "Resolving conflicts — [L]eave"
}

// conflictListBox draws the conflicted-file list window (popup-free; the process
// owns the state). Ported from the old renderConflictPopup so the two can
// coexist until the popup is removed.
func conflictListBox(m Model, files []model.FileStatus, sel int, src domain.ConflictState, inProgress string, mode dispMode, hscroll int) string {
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	textW := popupTextWidth(inner)
	var b strings.Builder
	b.WriteString("Resolve conflicts\n")
	if s := src.Describe(); s != "" {
		b.WriteString(conflictSrcStyle.Render(s) + "\n")
	}
	b.WriteString("\n")
	if len(files) == 0 {
		b.WriteString("  (all resolved)\n")
	} else {
		wr := make([]winRow, len(files))
		for i, f := range files {
			prefix := "  "
			var st lipgloss.Style
			if i == sel {
				prefix, st = "> ", selectedRow
			}
			wr[i] = winRow{text: fmt.Sprintf("%s%s  — %s", prefix, f.Path, f.ConflictLabel()), style: st}
		}
		h := len(files)
		if h > 12 {
			h = 12
		}
		for _, line := range renderWindow(wr, winOpts{w: textW, h: h, mode: mode, anchor: sel, hscroll: hscroll}) {
			b.WriteString(line + "\n")
		}
	}
	hintParts := append(conflictHints(files, sel, inProgress), "[L] leave", "[z] mode")
	b.WriteString("\n" + strings.Join(wrapParts(hintParts, textW, "  "), "\n"))
	return popupBox(inner, b.String())
}

// conflictHints lists the live keys for the current selection. Task 3 adds the
// per-file resolve actions; Task 5 adds continue/abort.
func conflictHints(files []model.FileStatus, sel int, inProgress string) []string {
	if len(files) == 0 {
		return []string{"(all resolved)"}
	}
	return []string{"[↑/↓] file"}
}
