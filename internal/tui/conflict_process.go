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
	errMsg     string               // last failed job's message (confReporting)
	picker     *hunkPicker          // the line editor, while confPicking (owned here, not on the surface stack)
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
	case confPicking:
		if p.picker == nil {
			p.st = confListing
			return m, nil
		}
		if msg.String() == "esc" { // leave the editor without applying → back to the list
			p.picker = nil
			p.st = confListing
			return m, nil
		}
		return p.picker.update(m, msg) // enter applies via the process-aware apply
	case confReporting:
		// Any key acknowledges the error; reload to resync, back to Listing.
		p.st = confListing
		return m, m.loadCmd()
	case confWorking:
		// Cancel lands in Task 5; otherwise input is ignored while a job runs.
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
		return m, nil
	case "up", "k":
		if p.sel > 0 {
			p.sel--
		}
		return m, nil
	case "down", "j":
		if p.sel < len(p.files)-1 {
			p.sel++
		}
		return m, nil
	case "A": // mark every conflicted file resolved
		var paths []string
		for _, f := range p.files {
			paths = append(paths, f.Path)
		}
		if len(paths) == 0 {
			return m, nil
		}
		p.st = confWorking
		return m.startOp(engine.MarkAllResolved{Paths: paths})
	case "enter": // hand off to the line editor for a both-sides file
		if p.sel < 0 || p.sel >= len(p.files) {
			return m, nil
		}
		f := p.files[p.sel]
		if f.ConflictClass() != model.ConflictBothSides {
			m.statusMsg = "line editor: only for files modified on both sides"
			return m, nil
		}
		p.st = confWorking // loading the file; the picker shows when it arrives
		return m, m.loadConflictFileCmd(f.Path)
	}
	// Per-file resolve actions (continue/abort land in Task 5).
	if p.sel < 0 || p.sel >= len(p.files) {
		return m, nil
	}
	f := p.files[p.sel]
	if action, ok := conflictActionFor(f, msg.String()); ok {
		p.st = confWorking
		return m.startOp(engine.ResolveConflict{Path: f.Path, Action: action})
	}
	return m, nil
}

// conflictActionFor maps a key to the resolve action for file f, honoring the
// conflict class. ok=false when the key is not a valid action for f. Pure, so
// the gating is unit-testable without starting a job.
func conflictActionFor(f model.FileStatus, key string) (engine.ConflictAction, bool) {
	both := f.ConflictClass() == model.ConflictBothSides
	hasSide := f.ConflictHasOurs() || f.ConflictHasTheirs()
	switch key {
	case "C":
		if both {
			return engine.KeepOurs, true
		}
	case "i":
		if both {
			return engine.KeepTheirs, true
		}
	case "m":
		if both {
			return engine.MarkResolved, true
		}
	case "k":
		if !both && hasSide { // a one-sided change to keep (both-deleted has none)
			return keepModifiedAction(f), true
		}
	case "d":
		if !both {
			return engine.DeleteFile, true
		}
	case "b":
		if !both && f.ConflictHasBase() {
			return engine.KeepBase, true
		}
	}
	return 0, false
}

func (p *conflictProcess) render(m Model, below string) string {
	w, h := m.overlayDims()
	bg := clipToHeight(below, h)
	switch p.st {
	case confListing:
		return overlayCenter(bg, conflictListBox(m, p.files, p.sel, p.src, p.inProgress, p.mode, p.hscroll), w, h)
	case confPicking:
		if p.picker != nil {
			return p.picker.render(m) // the line editor owns the full screen
		}
		return below
	case confWorking:
		return overlayCenter(bg, conflictMsgBox(m, "Resolving…"), w, h)
	case confReporting:
		return overlayCenter(bg, conflictMsgBox(m, "Resolve failed:\n\n"+p.errMsg+"\n\n[any key] back to the list"), w, h)
	}
	return below
}

// finished records a started job's outcome. On failure it shows the error; on
// success it reloads so refreshed() can re-derive the list from fresh status.
func (p *conflictProcess) finished(m Model, res engine.Result, err error) (Model, tea.Cmd) {
	if err != nil {
		p.st = confReporting
		p.errMsg = err.Error()
		return m, nil
	}
	return m, m.loadCmd()
}

// refreshed re-derives the conflicted-file list from the freshly-reloaded status
// and returns to Listing. (Task 5: confFinishing + slot release when a
// continue/abort has cleared every conflict.)
func (p *conflictProcess) refreshed(m Model) (Model, tea.Cmd) {
	p.files = m.status.Conflicts()
	p.src = m.conflict
	if p.sel >= len(p.files) {
		p.sel = max(0, len(p.files)-1)
	}
	p.st = confListing
	return m, nil
}

func (p *conflictProcess) indicator(m Model) string {
	return "Resolving conflicts — [L]eave"
}

// conflictMsgBox draws a small centered message box (progress / error).
func conflictMsgBox(m Model, msg string) string {
	w, _ := m.overlayDims()
	return popupBox(popupInnerWidth(w), msg)
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

// conflictHints lists the live keys for the current selection: navigation plus
// the per-file resolve actions valid for the highlighted file, plus mark-all.
// (Task 5 adds continue/abort when the list is empty.)
func conflictHints(files []model.FileStatus, sel int, inProgress string) []string {
	if len(files) == 0 {
		return []string{"(all resolved)"}
	}
	parts := []string{"[↑/↓] file"}
	if sel >= 0 && sel < len(files) {
		f := files[sel]
		for _, a := range []struct{ key, label string }{
			{"C", "keep ours"}, {"i", "keep theirs"}, {"m", "mark resolved"},
			{"k", "keep modified"}, {"d", "delete"}, {"b", "keep base"},
		} {
			if _, ok := conflictActionFor(f, a.key); ok {
				parts = append(parts, "["+a.key+"] "+a.label)
			}
		}
	}
	parts = append(parts, "[A] resolve all")
	return parts
}
