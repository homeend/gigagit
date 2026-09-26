package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// review.go wires the . -menu "Review …" entries to review AI tasks: each
// row resolves its target, then opens the task launch dialog
// (task_launch_popup.go). A result lands in applyReviewResult: saved in the
// reviews dir and shown in the report viewer, or announced by a notice when
// the viewer would get in the way.

// reviewTargetReadyMsg carries a BranchReviewTarget resolved off the UI thread
// (a branch review needs a ctx to find its merge-base); the Update handler
// opens the dialog with the resolved target. svc is the Service it was
// resolved against: a repo switch meanwhile (a new Service) drops it.
type reviewTargetReadyMsg struct {
	svc    *domain.Service
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
	// Label = "<short> <subject>": the commit TITLE the user recognizes, plus
	// the short sha to keep the report filename unique-ish. Range stays hex.
	label := shortHash(c.Hash)
	if s := strings.TrimSpace(c.Subject); s != "" {
		label += " " + s
	}
	return domain.ReviewTarget{Kind: domain.ReviewRange, Range: rng, Label: label, Diff: model.DiffSpec{Rev: rng}}
}

// --- menu rows (self-gating; each needs opsIdle AND a configured review tool) ---

// hasReviewTool reports whether at least one valid review command is configured.
func (m Model) hasReviewTool() bool {
	return len(domain.TaskChoices(m.cfg, exttool.CatReview, "tui")) > 0
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
		label: i18n.T("Review this commit"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startReview(target)
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
		label: i18n.T("Review branch %s", name),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.reviewBranchTargetCmd(name)
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
		label: i18n.T("Review working changes"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startReview(domain.WorkingReviewTarget())
		},
	}, true
}

// markedRangeReviewRow offers "Review marked range (AI)" on the Commits panel
// when 2+ commits are ◉-marked. It reviews EXACTLY what "Compare selection"
// shows (compareSelectionEndpoints): older..newer for 2 marks, oldest^..newest
// for 3+ — so marking-then-reviewing and marking-then-comparing scope the same
// changes. Refused when either endpoint is a WIP row (working tree / staged): a
// review needs a commit-to-commit range, and the endpoint hashes are feed
// commit shas (pure hex) so Range stays injection-safe without ResolveCommit.
func (m Model) markedRangeReviewRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() || !m.hasReviewTool() {
		return actionRow{}, false
	}
	if len(m.validCompareKeys()) < 2 {
		return actionRow{}, false
	}
	left, right, _, ok := m.compareSelectionEndpoints()
	if !ok || left.Kind() != model.EndpointCommit || right.Kind() != model.EndpointCommit {
		return actionRow{}, false
	}
	rng := left.Hash() + ".." + right.Hash()
	target := domain.ReviewTarget{
		Kind:  domain.ReviewRange,
		Range: rng,
		Label: m.markedRangeLabel(),
		Diff:  model.DiffSpec{Rev: rng},
	}
	return actionRow{
		id:    "review-marked-range",
		label: i18n.T("Review marked range (AI)"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startReview(target)
		},
	}, true
}

// markedRangeLabel builds the human label for a marked commit range from the
// oldest and newest MARKED commits: "<oldShort>..<newShort> — <newSubject>"
// (the newest = the range tip). Built from the commits themselves, not the
// endpoint hashes (whose left may carry a trailing "^" for 3+ marks). "" if no
// commit marks resolve (the caller then falls back to the hex Range).
func (m Model) markedRangeLabel() string {
	oldest, newest := -1, -1
	oldestRank, newestRank := -1, 1<<30
	for _, k := range m.validCompareKeys() {
		r := m.compareKeyRank(k)
		if r < 0 || r >= len(m.commits) {
			continue // WIP sentinel or unknown key — not a commit
		}
		if r > oldestRank {
			oldestRank, oldest = r, r
		}
		if r < newestRank {
			newestRank, newest = r, r
		}
	}
	if oldest < 0 || newest < 0 {
		return ""
	}
	lbl := shortHash(m.commits[oldest].Hash) + ".." + shortHash(m.commits[newest].Hash)
	if s := strings.TrimSpace(m.commits[newest].Subject); s != "" {
		lbl += " — " + s
	}
	return lbl
}

// reviewBranchTargetCmd resolves a branch's review scope off the UI thread.
func (m Model) reviewBranchTargetCmd(tip string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		tgt, err := svc.BranchReviewTarget(context.Background(), tip)
		return reviewTargetReadyMsg{svc: svc, target: tgt, err: err}
	}
}

// startReview opens the task launch dialog for a review of target.
func (m Model) startReview(target domain.ReviewTarget) (Model, tea.Cmd) {
	return m.openTaskLaunch(taskLaunch{kind: exttool.CatReview, review: target})
}

// applyReviewResult saves a review result where review reports live and
// opens it in the report viewer — or, when the viewer would get in the way
// (another checkout, a focused console, the conflict window), announces it.
func (m Model) applyReviewResult(info domain.TaskInfo) (Model, tea.Cmd) {
	if !m.canShowResult(info) {
		return m.stickyNotice(i18n.T("%s ready — ctrl+\\", info.Key))
	}
	label := strings.TrimPrefix(info.Key, "review — ")
	path, err := m.svc.SaveReviewReport(context.Background(), label, info.Result, time.Now())
	if err != nil { // the reviews dir failed: still show it, from the task's result file
		return m.openResultViewer(info.ID, ".md", reviewTitle(label), info.Result, nil)
	}
	return m.openResultFile(path, reviewTitle(label), nil)
}

// canShowResult: a result may open its viewer now — it belongs to the
// checkout on screen and nothing owns the keyboard.
func (m Model) canShowResult(info domain.TaskInfo) bool {
	return m.taskHere(info) && m.proc == nil && !(m.console != nil && m.console.focused) && m.modal == nil
}

// reviewTitle names the report viewer from the human label (branch name /
// "<short> <subject>" / range / "working changes"). An empty label (a target
// that set neither Label nor Range) falls back to "working changes", and so
// does the literal "working changes" label itself (domain's untranslated
// fallback) — both take the translated sibling key
// instead of running it through the generic "Review: %s" format.
func reviewTitle(label string) string {
	if strings.TrimSpace(label) == "" || label == "working changes" {
		return i18n.T("Review: working changes")
	}
	return i18n.T("Review: %s", label)
}
