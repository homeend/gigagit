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
// The lane runs two FOREGROUND sub-states in order (chooser → approval), each
// rendered as a centered box; esc in either pops the lane (there is nothing
// behind it to return to). Dispatch then BACKGROUNDS the run: the lane is
// popped, m.reviewRunning goes true, and the TUI stays fully usable while the
// agent works — a blinking status segment marks it in flight, other
// external-LLM actions are refused, and the report viewer auto-pops when it
// lands. The gen lives on the Model (not the lane) so it stays monotonic across
// a lane being cancelled/popped and a new run started: a stale/ctx-killed
// result carrying an older gen (an *exec.ExitError, not context.Canceled) is
// dropped by the gen guard rather than surfaced.

// reviewLane is the review capture lane: a foreground layer whose sub-state
// picks and approves a review tool, then backgrounds the headless run (via
// domain.ReviewReport) and pops itself.
type reviewLane struct {
	popupMax
	target    domain.ReviewTarget  // fully resolved before the lane opens (branch target resolves in an async hop first)
	cmds      []config.ToolCommand // review commands (>1 → chooser)
	choosing  bool                 // true while the numbered tool chooser is shown
	approving string               // non-empty: the resolved command awaiting first-run approval
	genCmd    config.ToolCommand   // the chosen command (approval is keyed on its CONFIG text)
}

// reviewDoneMsg carries the result of a headless review run. gen is the
// Model.reviewGen at dispatch time; applyReviewDone drops a result whose gen no
// longer matches (a cancelled, superseded, or repo-switched run).
type reviewDoneMsg struct {
	gen int
	res domain.ReviewResult
	err error
}

// reviewBlinkMsg flips the running-review status indicator's blink phase. gen
// ties the tick to the run that armed it; a stale gen (a later cancel/reRoot
// bump, or the run finishing) drops it so no second parallel lane arms.
// Modeled on noticeBlinkMsg.
type reviewBlinkMsg struct{ gen int }

// reviewBlinkCmd schedules the next blink flip (~800ms; only re-armed while the
// run's gen still matches, so the tick self-stops). Modeled on noticeBlinkCmd.
func reviewBlinkCmd(gen int) tea.Cmd {
	return tea.Tick(800*time.Millisecond, func(time.Time) tea.Msg { return reviewBlinkMsg{gen: gen} })
}

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
	if m.focus != panelCommits || !m.opsIdle() || !m.hasReviewTool() || m.reviewRunning {
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
	if m.focus != panelBranches || !m.opsIdle() || !ok || !m.hasReviewTool() || m.reviewRunning {
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
	if m.focus != panelFiles || !m.opsIdle() || !m.hasReviewTool() || m.reviewRunning {
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

// reviewDispatch BACKGROUNDS the run: it pops the lane (so the TUI stays
// usable), flags m.reviewRunning with the scope label for the blinking status
// indicator, then batches the headless review with the blink tick. reviewGen is
// bumped here (and read into the run/blink) so a later cancel that re-bumps it
// drops this run's result AND stops the blink.
func (m Model) reviewDispatch(lane *reviewLane, resolved string) (Model, tea.Cmd) {
	target := lane.target
	m = m.removeLayer(lane) // the run moves to the background; the lane closes
	m.reviewRunning = true
	m.reviewRunningLabel = reviewScopeLabel(target)
	m.reviewBlink = false
	m.reviewGen++
	gen := m.reviewGen
	ctx, cancel := context.WithCancel(context.Background())
	m.reviewCancel = cancel
	return m, tea.Batch(m.reviewRunCmd(resolved, target, gen, ctx), reviewBlinkCmd(gen))
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

// applyReviewDone handles the finished background run: gen-guarded, it clears
// the running flag and, on success, auto-pops the report viewer over whatever
// the user was doing (the lane is already gone). A result whose gen no longer
// matches m.reviewGen (cancelled/superseded/repo-switched) is dropped silently
// — essential because a ctx-killed agent returns *exec.ExitError, not
// context.Canceled, so only the gen check tells a deliberate cancel from a real
// failure.
func (m Model) applyReviewDone(msg reviewDoneMsg) (Model, tea.Cmd) {
	if msg.gen != m.reviewGen {
		return m, nil // stale / cancelled / superseded / repo switched
	}
	m.reviewRunning = false
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

// cancelReview cancels an in-flight background run, clears the running flag
// (killing the blink), and bumps reviewGen so the late, ctx-killed result is
// dropped. Reachable via reRoot (a repo switch); mirrors escGenerate's gen
// bump.
func (m Model) cancelReview() Model {
	if m.reviewCancel != nil {
		m.reviewCancel()
		m.reviewCancel = nil
	}
	m.reviewRunning = false
	m.reviewGen++
	return m
}

// --- layer interface ---

func (lane *reviewLane) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// esc pops the lane (nothing sits behind it). The lane only ever holds the
	// foreground chooser/approval now — a dispatched run has already backgrounded
	// and popped the lane — so there is nothing to cancel here.
	if msg.Type == tea.KeyEsc {
		return m.popLayer(), nil
	}
	switch {
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

// reviewScopeLabel names the review scope for the running-review status label.
func reviewScopeLabel(t domain.ReviewTarget) string {
	if strings.TrimSpace(t.Range) == "" {
		return "working changes"
	}
	return t.Range
}
