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

	popup               *worktreePopup
	repoPopup           *repoPopup
	settings            *settingsPopup
	initHomeDir         string // home dir for agent detection; "" skips home-scoped agents (tests)
	statePath           string // repo-registry location; "" disables recording (tests)
	pendingSeqBump      []string
	pendingSwitch       bool
	switchTarget        string
	branchPopup         *branchPopup
	pendingSwitchBranch string // branch to SmartSwitch to after a successful op (B = create-and-switch)

	mark      *markState   // the m-key mark; nil = none (see mark.go)
	pairPopup *pairOpPopup // two-row operation picker; nil = closed

	running   bool
	statusMsg string
	opMsgs    chan tea.Msg
	modal     *decisionState

	focus     panel
	sel       map[panel]int
	sortModes map[panel]sortMode // per-panel display order (zero value = default)
	headTimes map[string]int64   // worktree HEAD sha -> committer time (date sort)

	filterPanel  panel  // panel the filter is bound to (meaningful only when filterQuery != "" or filterTyping)
	filterQuery  string // case-insensitive substring; "" = no filter
	filterTyping bool   // true while /-input mode is capturing keys
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
	return Model{
		repo:      repo,
		loading:   true,
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{panelBranches: sortDateDesc},
	}
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
			m.headTimes = msg.headTimes
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
		if m.repoPopup != nil {
			return m.updateRepoPopupKey(msg)
		}
		if m.settings != nil {
			return m.updateSettingsKey(msg)
		}
		if m.branchPopup != nil {
			return m.updateBranchPopupKey(msg)
		}
		if m.pairPopup != nil {
			return m.updatePairPopupKey(msg)
		}
		// Filter-input mode captures every key (the panel label shows the query).
		if m.filterTyping {
			switch msg.Type {
			case tea.KeyCtrlC:
				return m, tea.Quit
			case tea.KeyEsc:
				m.filterTyping = false
				m.filterQuery = ""
			case tea.KeyEnter:
				m.filterTyping = false // commit: filter stays active
			case tea.KeyBackspace, tea.KeyCtrlH: // some terminals send 0x08 for Backspace
				if r := []rune(m.filterQuery); len(r) > 0 {
					m.filterQuery = string(r[:len(r)-1])
				}
				m.sel[m.filterPanel] = 0
			case tea.KeySpace:
				m.filterQuery += " "
				m.sel[m.filterPanel] = 0
			case tea.KeyRunes:
				m.filterQuery += string(msg.Runes)
				m.sel[m.filterPanel] = 0
			}
			return m, nil
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
			if !m.running && !m.loading {
				if bi, ok := m.backingIndex(panelBranches); ok {
					return m.startOp(engine.SmartSwitch{Branch: m.branches[bi].Name})
				}
			}
		case "S":
			if !m.running && !m.loading {
				return m.startOp(engine.Stash{Message: "gg stash"})
			}
		case "u":
			if !m.running && !m.loading {
				return m.startOp(engine.UndoLastCommit{})
			}
		case "w": // worktree for the selected EXISTING branch
			if !m.running && !m.loading {
				if mm, ok := m.openWorktreePopup(true); ok {
					return mm, nil
				}
			}
		case "W": // worktree on a NEW branch from the selected one
			if !m.running && !m.loading {
				if mm, ok := m.openWorktreePopup(false); ok {
					return mm, nil
				}
			}
		case "b":
			if !m.running && !m.loading && m.focus == panelBranches {
				if mm, ok := m.openBranchPopup(false); ok {
					return mm, nil
				}
			}
		case "B":
			if !m.running && !m.loading && m.focus == panelBranches {
				if mm, ok := m.openBranchPopup(true); ok {
					return mm, nil
				}
			}
		case "d":
			if !m.running && !m.loading {
				switch m.focus {
				case panelWorktrees:
					if bi, ok := m.backingIndex(panelWorktrees); ok {
						wt := m.worktrees[bi]
						return m.startOp(engine.RemoveWorktree{Path: wt.Path, Branch: wt.Branch})
					}
				case panelBranches:
					if bi, ok := m.backingIndex(panelBranches); ok {
						return m.startOp(engine.DeleteBranch{Name: m.branches[bi].Name})
					}
				}
			}
		case "enter":
			if !m.running && !m.loading && m.focus == panelWorktrees {
				if bi, ok := m.backingIndex(panelWorktrees); ok {
					target := m.worktrees[bi].Path
					if target != "" && target != m.currentWorktree {
						return m.reRoot(target)
					}
				}
			}
		case "tab":
			m.focus = (m.focus + 1) % panelCount
		case "shift+tab":
			m.focus = (m.focus - 1 + panelCount) % panelCount
		case "pgdown":
			if n := m.panelLen(m.focus); n > 0 {
				m.sel[m.focus] += m.pageStep()
				if m.sel[m.focus] > n-1 {
					m.sel[m.focus] = n - 1
				}
			}
		case "pgup":
			if m.sel[m.focus] > 0 {
				m.sel[m.focus] -= m.pageStep()
				if m.sel[m.focus] < 0 {
					m.sel[m.focus] = 0
				}
			}
		case "o":
			if !m.running && !m.loading {
				m.sortModes[m.focus] = (m.sortModes[m.focus] + 1) % sortModeCount
				if n := m.panelLen(m.focus); m.sel[m.focus] >= n && n > 0 {
					m.sel[m.focus] = n - 1
				}
			}
		case "/":
			if !m.running && !m.loading {
				m.filterPanel = m.focus
				m.filterQuery = ""
				m.filterTyping = true
				m.sel[m.focus] = 0
			}
		case "R":
			if !m.running && !m.loading {
				if mm, ok := m.openRepoPopup(); ok {
					return mm, nil
				}
				return m, nil
			}
		case ",":
			if !m.running && !m.loading {
				return m.openSettings(), nil
			}
		case "m":
			if !m.running && !m.loading {
				return m.handleMarkKey()
			}
		case "esc":
			if m.mark != nil {
				m.mark = nil
				return m, nil
			}
			// filterPanel is intentionally left set — filterActive() gates on a
			// non-empty query, so the residue is inert.
			if m.filterQuery != "" {
				m.filterQuery = ""
			}
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
		chainSwitch := ""
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
			chainSwitch = m.pendingSwitchBranch
		}
		m.pendingSeqBump = nil
		m.pendingSwitch = false
		m.pendingSwitchBranch = "" // cleared before the chained op starts, so it cannot re-fire
		if switchTo != "" {
			return m.reRoot(switchTo)
		}
		if chainSwitch != "" {
			return m.startOp(engine.SmartSwitch{Branch: chainSwitch})
		}
		return m, m.loadCmd()
	}
	return m, nil
}

// panelLen returns the number of rows in a panel, for selection clamping.
func (m Model) panelLen(p panel) int {
	_, idx := m.panelView(p)
	return len(idx)
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
	m.mark = nil // a mark from the old repo must not re-attach by name in the new one
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
