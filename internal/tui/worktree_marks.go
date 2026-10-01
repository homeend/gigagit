package tui

import (
	"context"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// publishedWT is the TUI's current worktree for the session registry
// (domain.PublishSessions reads it from its own goroutine).
var publishedWT atomic.Value // string

func publishedWorktree() string { s, _ := publishedWT.Load().(string); return s }

// worktreeMarkPrefix is the Worktrees-row mark column: ⚑ <agent> for a live
// claim, ⊘ for a reserve, "" otherwise.
func (m Model) worktreeMarkPrefix(path string) string {
	mk, ok := m.worktreeMarks[path]
	if !ok {
		return ""
	}
	s := ""
	if mk.Claim != nil {
		agent := mk.Claim.Agent
		if agent == "" {
			agent = i18n.T("agent")
		}
		s += "⚑ " + agent + "  "
	}
	if mk.Reserved {
		s += "⊘  "
	}
	return s
}

// worktreeClaimHint puts the selected worktree's claim (or reserve) in the
// bottom bar.
func (m Model) worktreeClaimHint() string {
	if m.focus != panelWorktrees {
		return ""
	}
	e, ok := m.selectedWorktreeEntry()
	if !ok || e.sess != "" || e.task != "" || e.wt < 0 || e.wt >= len(m.worktrees) {
		return ""
	}
	mk := m.worktreeMarks[m.worktrees[e.wt].Path]
	if mk.Claim == nil {
		if mk.Reserved {
			return i18n.T("⊘ reserved: agents never take this worktree")
		}
		return ""
	}
	c := mk.Claim
	h := i18n.T("⚑ claimed by %s since %s", c.Agent, c.Since.Local().Format("2006-01-02 15:04"))
	if c.Note != "" {
		h += " · " + c.Note
	}
	return h
}

// worktreeMarkRows are the Worktrees row's reserve / release-claim rows.
func (m Model) worktreeMarkRows(wt model.Worktree) []actionRow {
	path := wt.Path
	mk := m.worktreeMarks[path]
	var rows []actionRow
	if mk.Claim != nil {
		agent := mk.Claim.Agent
		rows = append(rows, actionRow{id: "worktree-release-claim", label: i18n.T("Release claim (%s)", agent),
			run: func(m Model) (tea.Model, tea.Cmd) { return m.confirmReleaseClaim(path, agent), nil }})
	}
	if mk.Reserved {
		rows = append(rows, actionRow{id: "worktree-unreserve", label: i18n.T("Unreserve"),
			run: func(m Model) (tea.Model, tea.Cmd) { return m.setReserved(path, false) }})
	} else {
		rows = append(rows, actionRow{id: "worktree-reserve", label: i18n.T("Reserve (no agents)"),
			run: func(m Model) (tea.Model, tea.Cmd) { return m.setReserved(path, true) }})
	}
	return rows
}

// confirmReleaseClaim asks before taking a worktree away from a live agent.
func (m Model) confirmReleaseClaim(path, agent string) Model {
	dir := shortWorktreeName(path)
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "worktree-release-claim",
			Prompt:  i18n.T("%s is working in %s. Release its claim?", agent, dir),
			Options: []string{"Release", "Cancel"},
		},
		sel: 1, // default highlight = Cancel
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt != "Release" {
				return m, nil
			}
			if _, err := m.svc.ReleaseWorktree(context.Background(), path, "", true); err != nil {
				m.statusMsg = i18n.T("release claim: %s", err.Error())
				return m, nil
			}
			m.statusMsg = i18n.T("released the claim on %s", dir)
			return m.reloadSourcesCmd([]sourceKey{srcWorktrees}, reloadOpts{})
		},
	}
	return m
}

// setReserved writes [agents] reserved through the domain's main-anchored
// config (never m.repoConfigPath, which follows the cwd worktree).
func (m Model) setReserved(path string, on bool) (tea.Model, tea.Cmd) {
	ctx := context.Background()
	ac, cfgPath, err := m.svc.AgentsConfig(ctx)
	if err == nil {
		err = m.svc.SetWorktreeReserved(ctx, cfgPath, ac.Reserved, path, on)
	}
	if err != nil {
		m.statusMsg = i18n.T("reserve: %s", err.Error())
		return m, nil
	}
	return m.reloadSourcesCmd([]sourceKey{srcWorktrees}, reloadOpts{})
}
