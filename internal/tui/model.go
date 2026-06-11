// Package tui implements the gigagit terminal UI with Bubble Tea.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/config"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/observ"
)

// Model is the root Bubble Tea model.
type Model struct {
	repo          *git.Repo
	width, height int

	loading  bool
	err      error
	status   model.WorkingTreeStatus
	branches []model.Branch
	commits  []model.Commit

	worktrees       []model.Worktree
	currentWorktree string

	cfg          config.Config
	gitCommonDir string

	popup          *worktreePopup
	pendingSeqBump []string
	pendingSwitch  bool
	switchTarget   string

	running   bool
	statusMsg string
	opMsgs    chan tea.Msg
	modal     *decisionState

	focus panel
	sel   map[panel]int
}

type panel int

const (
	panelBranches panel = iota
	panelWorktrees
	panelStatus
	panelCommits
	panelCount
)

// New constructs the initial model for repo.
func New(repo *git.Repo) Model {
	return Model{repo: repo, loading: true, sel: map[panel]int{}}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return m.loadCmd() }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case dataLoadedMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.status = msg.status
			m.branches = msg.branches
			m.commits = msg.commits
			m.worktrees = msg.worktrees
			m.currentWorktree = msg.currentWorktree
			m.cfg = msg.cfg
			m.gitCommonDir = msg.gitCommonDir
			// Clamp selections so a row removed since the last load (e.g. a
			// deleted worktree) can't leave an index pointing past the end.
			for p := panel(0); p < panelCount; p++ {
				if n := m.panelLen(p); m.sel[p] >= n {
					if n > 0 {
						m.sel[p] = n - 1
					} else {
						m.sel[p] = 0
					}
				}
			}
		}
	case tea.KeyMsg:
		if m.modal != nil {
			switch msg.String() {
			case "up", "k":
				if m.modal.sel > 0 {
					m.modal.sel--
				}
			case "down", "j":
				if m.modal.sel < len(m.modal.req.Options)-1 {
					m.modal.sel++
				}
			case "enter":
				m.modal.reply <- engine.DecisionResponse{Option: m.modal.req.Options[m.modal.sel]}
				m.modal = nil
			case "esc":
				m.modal.reply <- engine.DecisionResponse{Option: abortOption(m.modal.req.Options)}
				m.modal = nil
			}
			return m, nil
		}
		if m.popup != nil {
			return m.updatePopupKey(msg)
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			if !m.running {
				m.loading = true
				return m, m.loadCmd()
			}
		case "p":
			if !m.running && !m.loading {
				return m.startOp(engine.SmartPull{Intent: engine.PullAndStay})
			}
		case "P":
			if !m.running && !m.loading && m.status.Branch != "" {
				return m.startOp(engine.Push{Remote: "origin", Branch: m.status.Branch, SetUpstream: true})
			}
		case "s":
			if !m.running && !m.loading && len(m.branches) > 0 {
				target := m.branches[m.sel[panelBranches]].Name
				return m.startOp(engine.SmartSwitch{Branch: target})
			}
		case "S":
			if !m.running && !m.loading {
				return m.startOp(engine.Stash{Message: "gg stash"})
			}
		case "u":
			if !m.running && !m.loading {
				return m.startOp(engine.UndoLastCommit{})
			}
		case "w":
			if !m.running && !m.loading {
				if mm, ok := m.openWorktreePopup(); ok {
					return mm, nil
				}
			}
		case "d":
			if !m.running && !m.loading && m.focus == panelWorktrees && len(m.worktrees) > 0 {
				wt := m.worktrees[m.sel[panelWorktrees]]
				return m.startOp(engine.RemoveWorktree{Path: wt.Path, Branch: wt.Branch})
			}
		case "enter":
			if !m.running && !m.loading && m.focus == panelWorktrees && len(m.worktrees) > 0 {
				target := m.worktrees[m.sel[panelWorktrees]].Path
				if target != "" && target != m.currentWorktree {
					return m.reRoot(target)
				}
			}
		case "tab":
			m.focus = (m.focus + 1) % panelCount
		case "up", "k":
			if m.sel[m.focus] > 0 {
				m.sel[m.focus]--
			}
		case "down", "j":
			if m.sel[m.focus] < m.panelLen(m.focus)-1 {
				m.sel[m.focus]++
			}
		}
	case opEventMsg:
		switch e := msg.event.(type) {
		case engine.Progress:
			m.statusMsg = e.Step
			if e.Detail != "" {
				m.statusMsg += ": " + e.Detail
			}
		case engine.Done:
			m.statusMsg = e.Result.Summary
		}
		return m, waitForOp(m.opMsgs)
	case opDecisionMsg:
		m.modal = &decisionState{req: msg.req, reply: msg.reply}
		return m, waitForOp(m.opMsgs)
	case opFinishedMsg:
		m.running = false
		m.opMsgs = nil
		switchTo := ""
		if msg.err != nil {
			m.statusMsg = "error: " + msg.err.Error()
		} else {
			if msg.res.Summary != "" {
				m.statusMsg = msg.res.Summary
			}
			for _, name := range m.pendingSeqBump {
				_, _ = config.BumpSeq(m.gitCommonDir, name)
			}
			if m.pendingSwitch && msg.res.Path != "" {
				switchTo = msg.res.Path
			}
		}
		m.pendingSeqBump = nil
		m.pendingSwitch = false
		if switchTo != "" {
			return m.reRoot(switchTo)
		}
		return m, m.loadCmd()
	}
	return m, nil
}

// panelLen returns the number of rows in a panel, for selection clamping.
func (m Model) panelLen(p panel) int {
	switch p {
	case panelBranches:
		return len(m.branches)
	case panelWorktrees:
		return len(m.worktrees)
	case panelStatus:
		return len(m.status.Files)
	case panelCommits:
		return len(m.commits)
	}
	return 0
}

// reRoot points the model at the repository rooted at path and triggers a full
// reload. switchTarget records where a shell should follow on exit (written to
// --cwd-file by cmd/gg). A fresh span ring is used for the new root; the cmd/gg
// panic dump still references the original repo (acceptable for a debug aid).
func (m Model) reRoot(path string) (tea.Model, tea.Cmd) {
	m.repo = &git.Repo{Runner: gitexec.NewExecRunner("git", path, observ.NewRing(200))}
	m.switchTarget = path
	m.loading = true
	// Drop selections from the old repo so the highlight doesn't land on a
	// surprising row in the newly-loaded panels.
	m.sel = map[panel]int{}
	return m, m.loadCmd()
}

// View implements tea.Model.
func (m Model) View() string {
	if m.modal != nil {
		return m.render()
	}
	if m.loading {
		return "gigagit (loading…)\n"
	}
	if m.err != nil {
		return "error: " + m.err.Error() + "\n"
	}
	return m.render()
}

var _ tea.Model = Model{}

// abortOption returns "abort" if offered, else the last option (safe default).
func abortOption(opts []string) string {
	for _, o := range opts {
		if o == "abort" {
			return o
		}
	}
	if len(opts) > 0 {
		return opts[len(opts)-1]
	}
	return ""
}
