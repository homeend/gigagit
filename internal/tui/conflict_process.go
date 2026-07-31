package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/template"
)

// confState is the conflict-resolution process's state-machine state.
type confState int

const (
	confListing   confState = iota // showing the conflicted-file list; awaiting a file + action
	confPicking                    // handed off to the full-screen line editor
	confWorking                    // a job is running
	confReporting                  // a job failed; showing the error
	// The slot is released directly in the inProgressMsg handler once the repo is
	// clean, so there is no separate "finishing" state.
	confToolPick    // choosing an external tool command ([t])
	confToolFill    // collecting <user:…> values for the chosen command
	confToolApprove // first-run approval: showing the resolved command
	confToolMark    // per-file run changed the file: offer mark-resolved
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

	toolChoices []config.ToolCommand // picker rows while confToolPick
	toolSel     int
	toolFill    *templateFill   // <user:…> collection while confToolFill
	pending     *pendingToolRun // resolved run while confToolApprove/executing (Task 7)
	toolRunning string          // capture-mode run in flight: the tool's name, for the "running" box
}

// startConflictProcess fills the active-process slot from the current
// conflicted status. A no-op when nothing is conflicted AND no sequencer op
// is paused (the caller stays as it was). With zero conflicted files and a
// paused op the process opens straight into its "all resolved — continue/
// abort" state.
func startConflictProcess(m Model) (Model, tea.Cmd) {
	files := m.status.Conflicts()
	if len(files) == 0 && m.conflict.Op == "" {
		return m, nil
	}
	m.proc = &conflictProcess{st: confListing, files: files, src: m.conflict}
	return m, m.loadInProgressCmd() // probe merge/rebase so continue/abort can be offered
}

// canContinue is true when every file is resolved and a merge/rebase is still in
// progress (so a continue is the next step). canAbort is true whenever one is.
func (p *conflictProcess) canContinue() bool { return len(p.files) == 0 && p.inProgress != "" }
func (p *conflictProcess) canAbort() bool    { return p.inProgress != "" }

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
		// Cancel always offered (never trap the user): stop the in-flight job;
		// finished(context.Canceled) re-reads and returns to Listing.
		if msg.String() == "esc" || msg.String() == "ctrl+x" {
			if m.opCancel != nil {
				m.opCancel()
			}
			return m, nil
		}
	case confToolPick:
		return p.updateToolPick(m, msg)
	case confToolFill:
		return p.updateToolFill(m, msg)
	case confToolApprove:
		return p.updateToolApprove(m, msg)
	case confToolMark:
		return p.updateToolMark(m, msg)
	}
	return m, nil
}

func (p *conflictProcess) updateListing(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "L", "esc": // Leave — step out, repo as-is; resume from the notice ([x])
		m.proc = nil
		return m, nil
	case "z":
		p.mode = p.mode.next()
		p.hscroll = 0
		return m, nil
	case "up": // not "k": k is the keep-modified action below
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
	case "c": // continue the merge/rebase once everything is resolved
		if p.canContinue() {
			p.st = confWorking
			return m.startOp(engine.ContinueOp{})
		}
		return m, nil
	case "a": // abort the merge/rebase
		if p.canAbort() {
			p.st = confWorking
			return m.startOp(engine.AbortOp{})
		}
		return m, nil
	case "enter": // hand off to the line editor for a both-sides file
		if p.sel < 0 || p.sel >= len(p.files) {
			return m, nil
		}
		f := p.files[p.sel]
		if f.ConflictClass() != model.ConflictBothSides {
			m.statusMsg = i18n.T("line editor: only for files modified on both sides")
			return m, nil
		}
		p.st = confWorking // loading the file; the picker shows when it arrives
		return m, m.loadConflictFileCmd(f.Path)
	case "t": // run an external tool on the conflicts
		if m.reviewRunning {
			m.statusMsg = i18n.T("a review is in progress — wait for it to finish")
			return m, nil
		}
		var focused *model.FileStatus
		if p.sel >= 0 && p.sel < len(p.files) {
			focused = &p.files[p.sel]
		}
		choices := conflictToolChoices(m.toolCommands("conflict"), p.src.Op, focused)
		choices = append(choices, completeToolChoices(m.toolCommands(string(exttool.CatConflictComplete)), p.src.Op)...)
		if len(choices) == 0 {
			m.statusMsg = i18n.T("no external tools configured — Settings (,) → External tools")
			return m, nil
		}
		p.toolChoices, p.toolSel = choices, 0
		p.st = confToolPick
		return m, nil
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

// updateToolPick drives the tool picker: ↑/↓ select, enter chooses, esc backs
// out to the file list. Choosing hands off to the fill/approve flow.
func (p *conflictProcess) updateToolPick(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		p.st = confListing
		return m, nil
	case "up", "k":
		if p.toolSel > 0 {
			p.toolSel--
		}
	case "down", "j":
		if p.toolSel < len(p.toolChoices)-1 {
			p.toolSel++
		}
	case "enter":
		return p.startToolRun(m)
	}
	return m, nil
}

// startToolRun resolves the chosen command's context. A command with <user:…>
// tokens collects them first; a per-file command materializes the quartet
// asynchronously; everything else goes straight to the approval gate.
func (p *conflictProcess) startToolRun(m Model) (Model, tea.Cmd) {
	if p.toolSel < 0 || p.toolSel >= len(p.toolChoices) {
		return m, nil
	}
	tc := p.toolChoices[p.toolSel]
	fill := newTemplateFill(tc.Command)
	if fill.needsInput() {
		p.toolFill = &fill
		p.st = confToolFill
		return m, nil
	}
	return p.buildToolRun(m, tc, map[string]string{})
}

// buildToolRun assembles the CmdCtx and pendingToolRun for tc; per-file
// commands go async through ConflictFileVersions (confWorking meanwhile).
// Every run gets a per-run context file (op/source/target + conflicted
// paths, byte-exact — the injection-posture channel for dynamic content
// that needs no escaping); its path rides in pending.cleanup for removal
// alongside the run's other temp files.
func (p *conflictProcess) buildToolRun(m Model, tc config.ToolCommand, inputs map[string]string) (Model, tea.Cmd) {
	ctx := template.CmdCtx{
		Op: p.src.Op, Source: p.src.Source, Target: p.src.Target,
		Repo: m.currentWorktree,
	}
	for _, f := range p.files {
		ctx.ConflictedFiles = append(ctx.ConflictedFiles, f.Path)
	}
	ctxFile, err := toolContextFile(ctx)
	if err != nil {
		p.st = confReporting
		p.errMsg = err.Error()
		return m, nil
	}
	ctx.ContextFile = ctxFile
	if !tc.PerFile {
		resolved, err := template.ResolveCommand(tc.Command, inputs, ctx)
		if err != nil {
			os.Remove(ctxFile)
			p.st = confReporting
			p.errMsg = err.Error()
			return m, nil
		}
		env := toolEnv(ctx)
		messageFile := ""
		if tc.Category == string(exttool.CatConflictComplete) {
			mf, mErr := os.CreateTemp("", "gg-overview-*.md")
			if mErr != nil {
				os.Remove(ctxFile)
				p.st = confReporting
				p.errMsg = mErr.Error()
				return m, nil
			}
			mf.Close()
			messageFile = mf.Name()
			env = append(env, "GG_MESSAGE_FILE="+messageFile, "GG_TASK=conflict_complete")
		}
		p.pending = &pendingToolRun{tc: tc, resolved: resolved, env: env, cleanup: []string{ctxFile}, messageFile: messageFile}
		return p.gateOrRun(m)
	}
	// Per-file: quartet first (async), then resolve in the toolReadyMsg handler.
	f := p.files[p.sel]
	ctx.File = f.Path
	ctx.Merged = filepath.Join(m.currentWorktree, f.Path)
	// confWorking's hint reads "esc cancel", but this async quartet build has
	// no m.opCancel wiring (a raw tea.Cmd, not an engine op) — it is a known
	// sub-second, non-cancellable window. Not worth building cancellation
	// machinery for.
	p.st = confWorking
	svc, hasBase, path := m.svc, f.ConflictHasBase(), f.Path
	return m, func() tea.Msg {
		local, base, remote, cleanup, err := svc.ConflictFileVersions(context.Background(), path, hasBase)
		if err != nil {
			os.Remove(ctxFile)
			return toolReadyMsg{err: err}
		}
		ctx.Local, ctx.Base, ctx.Remote = local, base, remote
		resolved, rerr := template.ResolveCommand(tc.Command, inputs, ctx)
		if rerr != nil {
			cleanup() // ConflictFileVersions' cleanup removes local/base/remote
			os.Remove(ctxFile)
			return toolReadyMsg{err: rerr}
		}
		return toolReadyMsg{pending: &pendingToolRun{
			tc: tc, resolved: resolved, env: toolEnv(ctx),
			cleanup: []string{local, base, remote, ctxFile}, // paths recorded for the post-run path
			file:    path, merged: ctx.Merged,
		}}
	}
}

// gateOrRun applies the first-run approval gate: an already-approved command
// (per repo, by template hash) runs immediately; otherwise the approval box
// shows the exact resolved command first.
func (p *conflictProcess) gateOrRun(m Model) (Model, tea.Cmd) {
	if m.toolCommandApproved(p.pending.tc.Command) {
		return p.runPending(m)
	}
	p.st = confToolApprove
	return m, nil
}

// runPending executes the pending command. Terminal mode hands over the real
// terminal; capture mode runs headless in the background (the TUI keeps
// drawing the "running" box) with m.opCancel wired so esc kills the run.
func (p *conflictProcess) runPending(m Model) (Model, tea.Cmd) {
	pending := p.pending
	p.pending = nil
	p.st = confWorking
	if pending.tc.Mode == "capture" {
		ctx, cancel := context.WithCancel(context.Background())
		m.opCancel = cancel
		p.toolRunning = pending.tc.Name
		return m, m.execCaptureToolCmd(ctx, pending)
	}
	return m, m.execToolCmd(pending)
}

// updateToolApprove: enter approves (persisted) and runs; esc cancels.
func (p *conflictProcess) updateToolApprove(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		p.cleanupPending()
		p.st = confListing
		return m, nil
	case "enter":
		m.rememberToolApproval(p.pending.tc.Command)
		return p.runPending(m)
	}
	return m, nil
}

// updateToolFill collects <user:…> values, then proceeds like startToolRun.
func (p *conflictProcess) updateToolFill(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	done, cancel := p.toolFill.handleKey(msg)
	if cancel {
		p.toolFill = nil
		p.st = confListing
		return m, nil
	}
	if done {
		inputs := p.toolFill.inputs()
		p.toolFill = nil
		return p.buildToolRun(m, p.toolChoices[p.toolSel], inputs)
	}
	return m, nil
}

// updateToolMark: the per-file tool changed the file — offer to mark resolved.
func (p *conflictProcess) updateToolMark(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		path := p.pending.file
		p.pending = nil
		p.st = confWorking
		return m.startOp(engine.ResolveConflict{Path: path, Action: engine.MarkResolved})
	case "n", "esc":
		p.pending = nil
		p.st = confWorking
		return m, m.loadCmd()
	}
	return m, nil
}

// cleanupPending removes a cancelled run's quartet temp files.
func (p *conflictProcess) cleanupPending() {
	if p.pending == nil {
		return
	}
	for _, f := range p.pending.cleanup {
		os.Remove(f)
	}
	removeOverviewFile(p.pending)
	p.pending = nil
}

// toolReady receives the async per-file build.
func (p *conflictProcess) toolReady(m Model, msg toolReadyMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		p.st = confReporting
		p.errMsg = msg.err.Error()
		return m, nil
	}
	p.pending = msg.pending
	return p.gateOrRun(m)
}

// removeOverviewFile discards a conflict_complete run's overview temp file
// on the exits that will not open the viewer (failure / cancel / interrupt /
// empty). It is deliberately not in pending.cleanup: the success path hands
// the file to the report viewer, whose [e] open-in-editor needs it on disk.
func removeOverviewFile(pending *pendingToolRun) {
	if pending != nil && pending.messageFile != "" {
		os.Remove(pending.messageFile)
	}
}

// toolFinished receives the tool process's exit (handed-over terminal run or
// background capture alike): clean temps, log the exit disposition, surface a
// failure (with the captured output tail — a capture run has no terminal of
// its own to have shown it), offer mark-resolved when a per-file run changed
// its file, and reload so the conflict list re-derives. An interrupt exit —
// 130/143 (a shell that survived ctrl-C/SIGTERM and propagated the code), OR
// the process itself killed by SIGINT/SIGTERM (the common ctrl-C case, since
// the signal hits the whole foreground process group — see toolInterruptExit),
// OR an esc-cancelled capture run — is a normal quit, not a failure: it takes
// the exact same success path as a zero exit, just with a status hint instead
// of silence.
func (p *conflictProcess) toolFinished(m Model, msg toolFinishedMsg) (Model, tea.Cmd) {
	if msg.script != "" {
		os.Remove(msg.script)
	}
	m.opCancel = nil // a capture run's cancel func; the run is over either way
	p.toolRunning = ""
	changed := false
	if msg.pending != nil && msg.pending.merged != "" {
		if fi, err := os.Stat(msg.pending.merged); err == nil && fi.ModTime().After(msg.preMtime) {
			changed = true
		}
	}
	if msg.pending != nil {
		for _, f := range msg.pending.cleanup {
			os.Remove(f)
		}
	}
	logToolExit(msg)
	if msg.canceled {
		removeOverviewFile(msg.pending)
		m.statusMsg = i18n.T("tool cancelled")
	} else if msg.err != nil {
		removeOverviewFile(msg.pending)
		if !toolInterruptExit(msg.err) {
			p.st = confReporting
			p.errMsg = toolExitName(msg.pending) + ": " + msg.err.Error() + outputTail(msg.output, 8)
			return m, nil
		}
		m.statusMsg = i18n.T("tool interrupted")
	}
	if msg.pending != nil && msg.pending.tc.PerFile && changed {
		p.pending = msg.pending
		p.st = confToolMark
		return m, nil
	}
	// conflict_complete: a clean exit's overview opens in the report viewer.
	// The process closes FIRST — it preempts the layer stack for keys
	// (model.go's KeyMsg routing), so a viewer pushed over it would be
	// key-dead. If the operation is still paused (the agent stopped early),
	// the ⏸ status segment and [x] lead back in, exactly as after [L] leave.
	if msg.pending != nil && msg.pending.messageFile != "" && msg.err == nil && !msg.canceled {
		data, _ := os.ReadFile(msg.pending.messageFile)
		if strings.TrimSpace(string(data)) != "" {
			m.proc = nil
			title := i18n.T("Resolution overview — %s", msg.pending.tc.Name)
			m = m.pushLayer(newReviewView(title, msg.pending.messageFile, string(data)))
			return m, m.loadCmd()
		}
		removeOverviewFile(msg.pending)
		m.statusMsg = i18n.T("%s reported no overview", msg.pending.tc.Name)
	}
	p.st = confWorking
	return m, m.loadCmd()
}

func (p *conflictProcess) render(m Model, below string) string {
	w, h := m.overlayDims()
	bg := clipToHeight(below, h)
	switch p.st {
	case confListing:
		return overlayCenter(bg, conflictListBox(m, p.files, p.sel, p.src, p.inProgress, p.mode, p.hscroll), w, h)
	case confPicking:
		if p.picker != nil {
			return p.picker.render(m, below) // the line editor owns the full screen
		}
		return below
	case confWorking:
		msg := i18n.T("Working…  [esc] cancel")
		if p.toolRunning != "" { // a capture-mode tool run owns the wait
			msg = i18n.T("Running %s…  [esc] cancel", p.toolRunning)
		}
		return overlayCenter(bg, conflictMsgBox(m, msg), w, h)
	case confReporting:
		return overlayCenter(bg, conflictMsgBox(m, i18n.T("Resolve failed:")+"\n\n"+p.errMsg+"\n\n"+i18n.T("[any key] back to the list")), w, h)
	case confToolPick:
		return overlayCenter(bg, conflictToolPickBox(m, p.toolChoices, p.toolSel), w, h)
	case confToolFill:
		var b strings.Builder
		b.WriteString(i18n.T("Tool inputs") + "\n\n")
		for _, line := range p.toolFill.view(popupContentWidth(w)) {
			b.WriteString(line + "\n")
		}
		b.WriteString("\n" + i18n.T("[tab/enter] next  [esc] cancel"))
		return overlayCenter(bg, popupBox(popupInnerWidth(w), b.String()), w, h)
	case confToolApprove:
		header := i18n.T("Run this command?  (%s)", p.pending.tc.Name) + "\n\n"
		return overlayCenter(bg, popupBox(popupInnerWidth(w), header+approvalBoxView(p.pending.resolved, w)), w, h)
	case confToolMark:
		msg := i18n.T("The tool changed %s.", p.pending.file) + "\n\n" +
			i18n.T("Mark it as resolved (git add)?") + "\n\n" +
			i18n.T("[y/enter] mark resolved  [n/esc] not now")
		return overlayCenter(bg, popupBox(popupInnerWidth(w), msg), w, h)
	}
	return below
}

// finished records a started job's outcome. On failure it shows the error; on
// success it reloads so refreshed() can re-derive the list from fresh status.
func (p *conflictProcess) finished(m Model, res engine.Result, err error) (Model, tea.Cmd) {
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return m, m.loadCmd() // cancelled — just re-read and return to the list
		}
		p.st = confReporting
		p.errMsg = err.Error()
		return m, nil
	}
	return m, m.loadCmd()
}

// refreshed re-derives the conflicted-file list from the freshly-reloaded status
// and returns to Listing, then re-probes the merge/rebase-in-progress state — the
// inProgressMsg handler releases the slot when nothing is left (no conflicts and
// no in-progress op).
func (p *conflictProcess) refreshed(m Model) (Model, tea.Cmd) {
	p.files = m.status.Conflicts()
	p.src = m.conflict
	if p.sel >= len(p.files) {
		p.sel = max(0, len(p.files)-1)
	}
	p.st = confListing
	return m, m.loadInProgressCmd()
}

func (p *conflictProcess) indicator(m Model) string {
	switch p.st {
	case confPicking:
		return i18n.T("Resolving conflicts · line editor")
	case confWorking:
		if p.toolRunning != "" {
			return i18n.T("Resolving conflicts · running %s…  [esc] cancel", p.toolRunning)
		}
		return i18n.T("Resolving conflicts · working…  [esc] cancel")
	case confReporting:
		return i18n.T("Resolving conflicts · error — [any key] back to the list")
	case confToolPick:
		return i18n.T("Resolving conflicts · choose a tool  [↑/↓] select  [enter] run  [esc] back")
	case confToolFill, confToolApprove, confToolMark:
		return i18n.T("Resolving conflicts · tool run…  [esc] back")
	default: // confListing
		if len(p.files) == 0 {
			if p.inProgress != "" {
				return i18n.T("Resolving conflicts · all resolved — [c] continue %s  [a] abort  [L] leave", opDisplayName(p.inProgress))
			}
			return i18n.T("Resolving conflicts · all resolved — [L] leave")
		}
		return i18n.T("Resolving conflicts · %d left — [↑/↓] file  per-file keys in the box  [A] all  [L] leave", len(p.files))
	}
}

// conflictMsgBox draws a small centered message box (progress / error).
func conflictMsgBox(m Model, msg string) string {
	w, _ := m.overlayDims()
	return popupBox(popupInnerWidth(w), msg)
}

// conflictSrcStyle dims the "merging X into Y" subtitle in the file list.
var conflictSrcStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

// inProgressMsg carries the result of the merge/rebase-in-progress probe.
type inProgressMsg struct{ op string }

// loadInProgressCmd probes whether a merge/rebase is in progress so the process
// can offer continue/abort and decide when it is fully done.
func (m Model) loadInProgressCmd() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		op, _ := svc.InProgressOp(context.Background())
		return inProgressMsg{op: op}
	}
}

// keepModifiedAction maps a modify/delete file to the side that has content.
func keepModifiedAction(f model.FileStatus) engine.ConflictAction {
	if f.ConflictHasTheirs() {
		return engine.KeepTheirs
	}
	return engine.KeepOurs
}

// conflictListBox draws the conflicted-file list window (popup-free; the process
// owns the state). Ported from the old renderConflictPopup so the two can
// coexist until the popup is removed.
func conflictListBox(m Model, files []model.FileStatus, sel int, src domain.ConflictState, inProgress string, mode dispMode, hscroll int) string {
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	textW := popupTextWidth(inner)
	var b strings.Builder
	b.WriteString(i18n.T("Resolve conflicts") + "\n")
	if s := describeConflict(src); s != "" {
		b.WriteString(conflictSrcStyle.Render(s) + "\n")
	}
	b.WriteString("\n")
	if len(files) == 0 {
		b.WriteString("  " + i18n.T("(all resolved)") + "\n")
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
	nTools := len(m.toolCommands("conflict")) + len(m.toolCommands(string(exttool.CatConflictComplete)))
	hintParts := append(conflictHints(files, sel, inProgress, nTools), i18n.T("[L] leave"), i18n.T("[z] mode"))
	b.WriteString("\n" + strings.Join(wrapParts(hintParts, textW, "  "), "\n"))
	return popupBox(inner, b.String())
}

// conflictHints lists the live keys for the current selection: navigation plus
// the per-file resolve actions valid for the highlighted file, plus mark-all.
// nTools gates advertising [t] tools — a config with zero usable conflict
// commands must not dangle a dead-end key in the footer.
// (Task 5 adds continue/abort when the list is empty.)
func conflictHints(files []model.FileStatus, sel int, inProgress string, nTools int) []string {
	if len(files) == 0 {
		if inProgress != "" {
			return []string{i18n.T("all resolved"), i18n.T("[c] continue %s", opDisplayName(inProgress)), i18n.T("[a] abort")}
		}
		return []string{i18n.T("(all resolved)")}
	}
	parts := []string{i18n.T("[↑/↓] file")}
	if sel >= 0 && sel < len(files) {
		f := files[sel]
		if f.ConflictClass() == model.ConflictBothSides {
			parts = append(parts, i18n.T("[enter] line editor"))
		}
		for _, a := range []struct{ key, label string }{
			{"C", i18n.T("keep ours")}, {"i", i18n.T("keep theirs")}, {"m", i18n.T("mark resolved")},
			{"k", i18n.T("keep modified")}, {"d", i18n.T("delete")}, {"b", i18n.T("keep base")},
		} {
			if _, ok := conflictActionFor(f, a.key); ok {
				parts = append(parts, "["+a.key+"] "+a.label)
			}
		}
	}
	if nTools > 0 {
		parts = append(parts, i18n.T("[t] tools"))
	}
	parts = append(parts, i18n.T("[A] resolve all"))
	if inProgress != "" {
		parts = append(parts, i18n.T("[a] abort"))
	}
	return parts
}

// conflictToolPickBox draws the external-tool picker: one row per command,
// the command's first line dimmed beneath the selection hints.
func conflictToolPickBox(m Model, choices []config.ToolCommand, sel int) string {
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	textW := popupTextWidth(inner)
	var b strings.Builder
	b.WriteString(i18n.T("Run external tool") + "\n\n")
	for i, tc := range choices {
		prefix, st := "  ", lipgloss.NewStyle()
		if i == sel {
			prefix, st = "> ", selectedRow
		}
		label := tc.Name
		if tc.PerFile {
			label += "  " + i18n.T("(this file)")
		}
		b.WriteString(st.Render(truncate(prefix+label, textW)) + "\n")
	}
	b.WriteString("\n" + i18n.T("[↑/↓] select  [enter] run  [esc] back"))
	return popupBox(inner, b.String())
}
