package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/template"
)

// review.go wires the . -menu "Review …" entries to the stage-3 capture lane.
// Unlike the commit-message generate lane (commit_generate.go), which lives ON
// the commit popup as sub-state fields, a review has no host popup — so the
// lane IS its own layer (reviewLane) pushed on the stack, mirroring the
// commitNamePopup layer pattern (embed popupMax, own update/render).
//
// The lane runs three sub-states in order (chooser → approval → running),
// each rendered as a centered box. Because the lane owns the whole layer, esc
// in ANY sub-state pops the lane (there is nothing behind it to return to) —
// and while running, esc also cancels the run and bumps the Model-level
// reviewGen so a ctx-killed agent's late *exec.ExitError result is dropped by
// the gen guard rather than surfaced. The gen lives on the Model (not the
// lane) so it stays monotonic across a lane being cancelled/popped and a new
// one pushed: a stale result from a previous lane can never match the live
// gen of a new lane.

// reviewLane is the review capture lane: a layer whose sub-state runs a review
// tool headless (via domain.ReviewReport) and, on success, replaces itself
// with the full-screen reviewView report.
type reviewLane struct {
	popupMax
	target    domain.ReviewTarget  // fully resolved before the lane opens (branch target resolves in an async hop first)
	cmds      []config.ToolCommand // review commands (>1 → chooser)
	choosing  bool                 // true while the numbered tool chooser is shown
	approving string               // non-empty: the resolved command awaiting first-run approval
	genCmd    config.ToolCommand   // the chosen command (approval is keyed on its CONFIG text)
	running   bool                 // true while the agent runs (spinner)
	spinFrame int                  // animated-spinner frame (advanced by reviewSpinMsg)
	spinStart time.Time            // run start, for the elapsed-seconds readout
}

// reviewDoneMsg carries the result of a headless review run. gen is the
// Model.reviewGen at dispatch time; applyReviewDone drops a result whose gen no
// longer matches (a cancelled, superseded, or repo-switched run).
type reviewDoneMsg struct {
	gen int
	res domain.ReviewResult
	err error
}

// reviewSpinMsg advances the in-flight review spinner; gen guards it against a
// finished or superseded run (the genSpinMsg pattern).
type reviewSpinMsg struct{ gen int }

// reviewTargetReadyMsg carries a BranchReviewTarget resolved off the UI thread
// (a branch review needs a ctx to find its merge-base); the Update handler
// opens the lane with the resolved target. gen is the reviewGen captured when
// the branch row dispatched — a repo switch (reRoot bumps reviewGen) during the
// merge-base resolution drops the stale target rather than opening a lane for
// the old repo's branch in the new one (the same gen-guard discipline the run
// itself uses).
type reviewTargetReadyMsg struct {
	gen    int
	target domain.ReviewTarget
	err    error
}

// reviewTargetForCommit builds the review scope for a single focused commit:
// its own change, sha^..sha. A root commit has no parent, so ^.. would fail —
// review the tip alone (git diff sha). Pure, for tests.
func reviewTargetForCommit(c model.Commit) domain.ReviewTarget {
	rng := c.Hash + "^.." + c.Hash
	if len(c.Parents) == 0 {
		rng = c.Hash
	}
	return domain.ReviewTarget{Kind: domain.ReviewRange, Range: rng, Diff: model.DiffSpec{Rev: rng}}
}

// --- menu rows (self-gating; each needs opsIdle AND a configured review tool) ---

// hasReviewTool reports whether at least one valid review command is configured.
func (m Model) hasReviewTool() bool {
	return len(m.toolCommands(string(exttool.CatReview))) > 0
}

// focusedCommitReviewRow offers "Review this commit" on the Commits panel.
func (m Model) focusedCommitReviewRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() || !m.hasReviewTool() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	target := reviewTargetForCommit(m.commits[bi])
	return actionRow{
		id:    "review-commit",
		label: "Review this commit",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startReviewLane(target)
		},
	}, true
}

// branchReviewRow offers "Review branch <name>" on the Branches panel. The
// target (<base>..<tip>) needs a ctx to resolve the merge-base, so the row
// dispatches an async hop (reviewBranchTargetCmd) that opens the lane once the
// target is known.
func (m Model) branchReviewRow() (actionRow, bool) {
	b, ok := m.selectedBranch()
	if m.focus != panelBranches || !m.opsIdle() || !ok || !m.hasReviewTool() {
		return actionRow{}, false
	}
	name := b.Name
	return actionRow{
		id:    "review-branch",
		label: "Review branch " + name,
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.reviewBranchTargetCmd(name, m.reviewGen)
		},
	}, true
}

// workingReviewRow offers "Review working changes" on the Files panel — the
// full working tree + staged diff vs HEAD (domain.WorkingReviewTarget).
func (m Model) workingReviewRow() (actionRow, bool) {
	if m.focus != panelFiles || !m.opsIdle() || !m.hasReviewTool() {
		return actionRow{}, false
	}
	return actionRow{
		id:    "review-working",
		label: "Review working changes",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startReviewLane(domain.WorkingReviewTarget())
		},
	}, true
}

// reviewBranchTargetCmd resolves a branch's review scope off the UI thread. gen
// is echoed back so a repo switch during resolution drops the stale target.
func (m Model) reviewBranchTargetCmd(tip string, gen int) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		tgt, err := svc.BranchReviewTarget(context.Background(), tip)
		return reviewTargetReadyMsg{gen: gen, target: tgt, err: err}
	}
}

// --- lane lifecycle ---

// startReviewLane pushes the lane for target and enters the first applicable
// sub-state: a chooser when >1 tool is configured, else the approval gate (or
// straight to dispatch when the sole tool is already approved).
func (m Model) startReviewLane(target domain.ReviewTarget) (Model, tea.Cmd) {
	cmds := m.toolCommands(string(exttool.CatReview))
	if len(cmds) == 0 {
		m.statusMsg = "no review tool configured (Settings → External tools)"
		return m, nil
	}
	lane := &reviewLane{target: target, cmds: cmds}
	m = m.pushLayer(lane)
	if len(cmds) > 1 {
		lane.choosing = true
		return m, nil
	}
	return m.reviewGate(lane, cmds[0])
}

// reviewGate resolves chosen against the target and applies the first-run
// approval gate (keyed on the CONFIG command text, like the commit lane's
// gateGenerate). An already-approved command dispatches straight through.
func (m Model) reviewGate(lane *reviewLane, chosen config.ToolCommand) (Model, tea.Cmd) {
	resolved, err := template.ResolveCommand(chosen.Command, nil, template.CmdCtx{Range: lane.target.Range, Repo: m.currentWorktree})
	if err != nil {
		m.statusMsg = "review: " + err.Error()
		return m.popLayer(), nil
	}
	lane.genCmd = chosen
	if !m.toolCommandApproved(chosen.Command) {
		lane.approving = resolved
		return m, nil
	}
	return m.reviewDispatch(lane, resolved)
}

// reviewDispatch arms the run: sets the spinner going and batches the headless
// review with an animated-spinner tick. reviewGen is bumped here (and read into
// the run/tick) so a later cancel that re-bumps it drops this run's result.
func (m Model) reviewDispatch(lane *reviewLane, resolved string) (Model, tea.Cmd) {
	lane.choosing = false
	lane.approving = ""
	lane.running = true
	lane.spinFrame = 0
	lane.spinStart = time.Now()
	m.reviewGen++
	gen := m.reviewGen
	ctx, cancel := context.WithCancel(context.Background())
	m.reviewCancel = cancel
	return m, tea.Batch(m.reviewRunCmd(resolved, lane.target, gen, ctx), reviewSpinTickCmd(gen))
}

// reviewRunCmd runs the resolved command headless via domain.ReviewReport
// (which persists the report and returns its path/content), synchronously
// inside the returned tea.Cmd (the stageCmd pattern). The lane goes through
// svc, never engine.OpDeps — no internal/engine import here.
func (m Model) reviewRunCmd(resolved string, target domain.ReviewTarget, gen int, ctx context.Context) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		res, err := svc.ReviewReport(ctx, target, resolved, []string{"GG_TASK=review"}, time.Now())
		return reviewDoneMsg{gen: gen, res: res, err: err}
	}
}

// reviewSpinTickCmd schedules the next spinner frame ~100ms out.
func reviewSpinTickCmd(gen int) tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return reviewSpinMsg{gen: gen} })
}

// tickReviewSpinner advances the spinner and reschedules while a matching run
// is in flight; a stale/finished/cancelled run stops the tick (no reschedule).
func (m Model) tickReviewSpinner(msg reviewSpinMsg) (Model, tea.Cmd) {
	lane := layerOf[*reviewLane](m)
	if lane == nil || !lane.running || msg.gen != m.reviewGen {
		return m, nil
	}
	lane.spinFrame++
	return m, reviewSpinTickCmd(msg.gen)
}

// applyReviewDone handles the finished run: gen-guarded, it pops the lane and
// pushes the report viewer on success, or surfaces the error. A result whose
// gen no longer matches m.reviewGen (cancelled/superseded/repo-switched) is
// dropped silently — essential because a ctx-killed agent returns
// *exec.ExitError, not context.Canceled, so only the gen check tells a
// deliberate cancel from a real failure.
func (m Model) applyReviewDone(msg reviewDoneMsg) (Model, tea.Cmd) {
	if msg.gen != m.reviewGen {
		return m, nil // stale / cancelled / superseded / repo switched
	}
	lane := layerOf[*reviewLane](m)
	if lane == nil {
		return m, nil // lane already gone
	}
	m = m.removeLayer(lane)
	m.reviewCancel = nil
	if msg.err != nil {
		m.statusMsg = "review: " + msg.err.Error()
		return m, nil
	}
	return m.pushLayer(newReviewView(reviewTitle(msg.res.Range), msg.res.Path, msg.res.Content)), nil
}

// reviewTitle names the report viewer; the working-changes target has no range.
func reviewTitle(rng string) string {
	if strings.TrimSpace(rng) == "" {
		return "Review: working changes"
	}
	return "Review: " + rng
}

// cancelReview cancels an in-flight run and bumps reviewGen so the late,
// ctx-killed result is dropped. Mirrors escGenerate's gen bump.
func (m Model) cancelReview() Model {
	if m.reviewCancel != nil {
		m.reviewCancel()
		m.reviewCancel = nil
	}
	m.reviewGen++
	return m
}

// --- layer interface ---

func (lane *reviewLane) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// esc always pops the lane (nothing sits behind it); a running lane also
	// cancels the run first so the killed result is dropped by the gen guard.
	if msg.Type == tea.KeyEsc {
		if lane.running {
			m = m.cancelReview()
		}
		return m.popLayer(), nil
	}
	switch {
	case lane.running:
		return m, nil // every key but esc/ctrl+c is swallowed while the agent works
	case lane.approving != "":
		return lane.updateApproving(m, msg)
	case lane.choosing:
		return lane.updateChoosing(m, msg)
	}
	return m, nil
}

// updateChoosing drives the numbered tool chooser: a digit 1-9 picks that row,
// enter picks the first. (esc is handled in update.)
func (lane *reviewLane) updateChoosing(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		return lane.selectChosen(m, 0)
	case tea.KeyRunes:
		for _, r := range msg.Runes {
			if r >= '1' && r <= '9' {
				return lane.selectChosen(m, int(r-'1'))
			}
		}
	}
	return m, nil
}

// selectChosen picks cmds[idx] (a no-op on an out-of-range index) and continues
// at the approval gate.
func (lane *reviewLane) selectChosen(m Model, idx int) (Model, tea.Cmd) {
	if idx < 0 || idx >= len(lane.cmds) {
		return m, nil
	}
	lane.choosing = false
	return m.reviewGate(lane, lane.cmds[idx])
}

// updateApproving drives the first-run approval box: y/enter records the
// approval (on the CONFIG command text) and dispatches; n cancels (esc, handled
// in update, does the same). Mirrors the commit lane's updateApproving.
func (lane *reviewLane) updateApproving(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		return lane.approveAndRun(m)
	case tea.KeyRunes:
		switch string(msg.Runes) {
		case "y":
			return lane.approveAndRun(m)
		case "n":
			return m.popLayer(), nil
		}
	}
	return m, nil
}

func (lane *reviewLane) approveAndRun(m Model) (Model, tea.Cmd) {
	resolved := lane.approving
	m.rememberToolApproval(lane.genCmd.Command)
	return m.reviewDispatch(lane, resolved)
}

func (lane *reviewLane) render(m Model, below string) string {
	w, h := m.overlayDims()
	var b strings.Builder
	switch {
	case lane.running:
		frames := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
		frame := frames[lane.spinFrame%len(frames)]
		elapsed := int(time.Since(lane.spinStart).Seconds())
		b.WriteString("Review\n\n")
		b.WriteString(fmt.Sprintf("%c reviewing %s… %ds  ([esc] to cancel)", frame, reviewScopeLabel(lane.target), elapsed))
	case lane.approving != "":
		b.WriteString("Run this command?  (" + lane.genCmd.Name + ")\n\n")
		b.WriteString(approvalBoxView(lane.approving, w))
	case lane.choosing:
		b.WriteString("Choose a review tool\n\n")
		for i, tc := range lane.cmds {
			b.WriteString(fmt.Sprintf("[%d] %s\n", i+1, tc.Name))
		}
		b.WriteString("\n[1-9] choose  [enter] first  [esc] cancel")
	}
	box := modalStyle.Width(popupResolveWidth(w, lane.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}

// reviewScopeLabel names the review scope for the spinner readout.
func reviewScopeLabel(t domain.ReviewTarget) string {
	if strings.TrimSpace(t.Range) == "" {
		return "working changes"
	}
	return t.Range
}
