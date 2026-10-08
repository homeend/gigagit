package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/repogate"
)

// busyHolderLabel names the long-lived gate holder a refused op ran into
// (repogate.BusyError.Holder is domain's "op <OpName>" label). The capture
// ops are the only long holders today (domain.Execute marks a CaptureTask's
// hold long-lived); anything else is named generically rather than leaking
// a Go type name into the UI.
func busyHolderLabel(holder string) string {
	switch strings.TrimPrefix(holder, "op ") {
	case "ConflictAgent":
		return i18n.T("an AI agent resolving conflicts")
	case "CompleteConflict":
		return i18n.T("an AI agent resolving and completing conflicts")
	case "ReviewChanges":
		return i18n.T("an AI review")
	case "GenerateMessage":
		return i18n.T("an AI commit-message generation")
	}
	return i18n.T("a long-running AI task")
}

// busyModal is the dismiss-only notice for an op the repo gate refused
// because a headless AI task holds the repository for its whole run. The
// refusal is immediate (nothing queues behind a long hold), so this is the
// user's only sign that the key did anything — hence a modal, not just a
// status line.
func (m Model) busyModal(busy *repogate.BusyError) *decisionState {
	return &decisionState{
		req: engine.DecisionRequest{
			ID:      "repo-busy",
			Prompt:  i18n.T("This operation needs the repository to itself, but %s is running.\n\nWait for it to finish, or stop it in the Headless tab (ctrl+\\).", busyHolderLabel(busy.Holder)),
			Options: []string{"ok"},
		},
		onResolve: func(m Model, _ string) (tea.Model, tea.Cmd) { return m, nil },
	}
}
