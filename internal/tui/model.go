// Package tui implements the gigagit terminal UI with Bubble Tea.
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/config"
	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/model"
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
	pendingSwitchBranch string        // branch to SmartSwitch to after a successful op (B = create-and-switch)
	contentPopup        *contentPopup // generic read-only viewer (help window)

	mark      *markState   // the m-key mark; nil = none (see mark.go)
	pairPopup *pairOpPopup // two-row operation picker; nil = closed

	filesView        *contentPopup // commit files tree replacing the left column; nil = closed
	filesTitle       string        // "Files <short-hash> <subject>", updated with the content
	filesHash        string        // commit the view wants; gates stale async results
	filesTreeFocused bool          // true = the tree side owns vertical movement (←/→/tab)

	diffView    *diffView // full-screen side-by-side diff; nil = closed
	diffTag     string    // request key of the wanted diff; gates stale async results
	diffPartial bool      // session default for new diffs (false = full); the f key toggles it
	diffWrap    bool      // session: word-wrap long lines in the diff view (w toggles)

	svc              *domain.Service    // command layer; m.repo == svc.Repo()
	feed             *domain.CommitFeed // single source of truth for commits
	commitsExhausted bool               // false → "Commits N+", true → "Commits N"
	opCancel         context.CancelFunc // cancels the in-flight op's context; nil when idle
	loadGen          int                // bumped per superseding load; stale dataLoadedMsg are dropped

	running   bool
	statusMsg string
	opMsgs    chan tea.Msg
	modal     *decisionState

	focus         panel
	lastLeftPanel panel // ←'s return target; zero value = panelBranches
	sel           map[panel]int
	sortModes     map[panel]sortMode // per-panel display order (zero value = default)
	headTimes     map[string]int64   // worktree HEAD sha -> committer time (date sort)

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
	svc := domain.New(repo)
	return Model{
		repo:      repo,
		svc:       svc,
		feed:      svc.CommitFeed(),
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
		if m.filesView != nil && msg.Width > 0 && msg.Width < 40 {
			// The narrow layout has no left column; without this the view
			// would keep capturing keys while invisible.
			m.filesView = nil
			m.filesTreeFocused = false
			m.statusMsg = "files view closed: terminal too narrow"
		}
		if m.diffView != nil && msg.Width > 0 {
			if msg.Width < 60 {
				m.diffView = nil
				m.diffTag = ""
				m.statusMsg = "diff closed: terminal too narrow"
			} else {
				// Re-wrap at the new width, keeping the viewport anchored to
				// the logical line currently at the top.
				v := m.diffView
				topLine := 0
				if v.offset < len(v.disp) {
					topLine = v.disp[v.offset].line
				}
				w, _ := m.overlayDims()
				v.relayout(w)
				if topLine < len(v.lineStart) {
					v.offset = v.lineStart[topLine]
				}
				v.scroll(0, m.diffBodyRows())
			}
		}
	case diffMsg:
		if m.diffView == nil || msg.tag != m.diffTag {
			return m, nil // view closed, or a stale result
		}
		m.diffView = msg.view
		return m, nil
	case commitFilesMsg:
		if m.filesView == nil || msg.hash != m.filesHash {
			return m, nil // view closed, or a stale result from fast movement
		}
		if msg.err != nil {
			m.statusMsg = "files: " + msg.err.Error()
			if len(m.filesView.lines) == 1 && m.filesView.lines[0].text == "(loading…)" {
				m.filesView.lines = []contentLine{{text: "(load failed)"}}
			}
			return m, nil
		}
		// Only lines and cursor are replaced; the search query intentionally
		// survives the commit change (track one file through history).
		m.filesView.lines = commitFileLines(msg.files)
		m.filesView.sel = 0
		m.filesTitle = "Files " + shortHash(msg.hash) + " " + msg.subject
		return m, nil
	case commitsPagedMsg:
		if m.feed != nil && msg.gen == m.feed.Gen() {
			st := m.feed.Snapshot()
			m.commits = st.Commits
			m.commitsExhausted = st.Exhausted
		}
		return m, nil
	case dataLoadedMsg:
		if msg.gen != m.loadGen {
			return m, nil // superseded by a newer load
		}
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.status = msg.status
			m.branches = msg.branches
			m.commits = msg.commits
			m.commitsExhausted = msg.commitsExhausted
			if msg.commitErr != nil {
				m.statusMsg = "commits: " + msg.commitErr.Error()
			}
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
		// Routing invariant: the diff view is checked immediately after the
		// modal here, in the MouseMsg arm, and in render() — the key owner
		// must be the top visible surface (background ops will rely on it).
		if m.diffView != nil {
			return m.updateDiffViewKey(msg)
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
		if m.contentPopup != nil {
			return m.updateContentPopupKey(msg)
		}
		if m.pairPopup != nil {
			return m.updatePairPopupKey(msg)
		}
		if m.filesView != nil {
			return m.updateFilesViewKey(msg)
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
				m.loadGen++
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
			if m.canSwitchBranch() {
				b, _ := m.selectedBranch()
				return m.startOp(engine.SmartSwitch{Branch: b.Name})
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
			if m.canOpenWorktreePopup() {
				if mm, ok := m.openWorktreePopup(true); ok {
					return mm, nil
				}
			}
		case "W": // worktree on a NEW branch from the selected one
			if m.canOpenWorktreePopup() {
				if mm, ok := m.openWorktreePopup(false); ok {
					return mm, nil
				}
			}
		case "b":
			if m.focus == panelBranches && m.canOpenBranchPopup() {
				if mm, ok := m.openBranchPopup(false); ok {
					return mm, nil
				}
			}
		case "B":
			if m.focus == panelBranches && m.canOpenBranchPopup() {
				if mm, ok := m.openBranchPopup(true); ok {
					return mm, nil
				}
			}
		case "d":
			switch m.focus {
			case panelWorktrees:
				if m.canDeleteWorktree() {
					wt, _ := m.selectedWorktree()
					return m.startOp(engine.RemoveWorktree{Path: wt.Path, Branch: wt.Branch})
				}
			case panelBranches:
				if m.canDeleteBranch() {
					b, _ := m.selectedBranch()
					return m.startOp(engine.DeleteBranch{Name: b.Name})
				}
			}
		case "enter":
			if m.focus == panelWorktrees && m.canEnterWorktree() {
				wt, _ := m.selectedWorktree()
				return m.reRoot(wt.Path)
			}
			if m.focus == panelStatus && m.canShowFileDiff() {
				bi, _ := m.backingIndex(panelStatus)
				f := m.status.Files[bi]
				m.diffView = &diffView{title: f.Path, context: "HEAD → working tree", loading: true, partial: m.diffPartial, wrap: m.diffWrap}
				m.diffTag = "status:" + f.Path
				return m, m.loadStatusDiffCmd(f)
			}
		case "tab":
			m = m.rememberLeftFocus()
			m.focus = (m.focus + 1) % panelCount
		case "shift+tab":
			m = m.rememberLeftFocus()
			m.focus = (m.focus - 1 + panelCount) % panelCount
		case "right":
			if m.focus != panelCommits {
				m = m.rememberLeftFocus()
				m.focus = panelCommits
			}
		case "left":
			// No-op when already in the left column, and when the narrow
			// layout has no left column to focus.
			if m.focus == panelCommits && (m.width <= 0 || m.width >= 40) {
				m.focus = m.lastLeftPanel
			}
		case "pgdown":
			if n := m.panelLen(m.focus); n > 0 {
				m.sel[m.focus] += m.pageStep()
				if m.sel[m.focus] > n-1 {
					m.sel[m.focus] = n - 1
				}
			}
			if m.focus == panelCommits {
				if cmd := m.maybeLoadMoreCommits(); cmd != nil {
					return m, cmd
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
		case "?":
			m.contentPopup = newContentPopup("Help — keys", helpContent())
		case "l":
			if m.focus == panelCommits && m.canShowCommitFiles() {
				if m.width > 0 && m.width < 40 {
					m.statusMsg = "terminal too narrow for the files view"
					return m, nil
				}
				bi, _ := m.backingIndex(panelCommits)
				c := m.commits[bi]
				m.filesView = &contentPopup{lines: []contentLine{{text: "(loading…)"}}}
				m.filesTitle = "Files " + shortHash(c.Hash) + " " + c.Subject
				m.filesHash = c.Hash
				m.filesTreeFocused = false // always open on the commit list
				return m, m.loadCommitFilesCmd(c)
			}
		case "m":
			if m.canMark() {
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
			if m.focus == panelCommits {
				if cmd := m.maybeLoadMoreCommits(); cmd != nil {
					return m, cmd
				}
			}
		}
	case tea.MouseMsg:
		return m.handleMouse(msg)
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
		if m.opCancel != nil {
			m.opCancel() // op already returned; this only frees the ctx
			m.opCancel = nil
		}
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
		m.loadGen++
		return m, m.loadCmd()
	}
	return m, nil
}

// rememberLeftFocus records the focused panel as ←'s return target when it
// is one of the left-column panels. Called before any focus reassignment.
func (m Model) rememberLeftFocus() Model {
	if m.focus != panelCommits {
		m.lastLeftPanel = m.focus
	}
	return m
}

// panelLen returns the number of rows in a panel, for selection clamping.
func (m Model) panelLen(p panel) int {
	_, idx := m.panelView(p)
	return len(idx)
}

// maybeLoadMoreCommits returns a cmd to page in more commits when the Commits
// selection nears the end and no commits filter is active; nil otherwise. The
// feed owns the "is there more / am I already loading" decision.
func (m Model) maybeLoadMoreCommits() tea.Cmd {
	if m.feed == nil {
		return nil
	}
	if m.filterTyping && m.filterPanel == panelCommits {
		return nil
	}
	if m.filterActive(panelCommits) {
		return nil
	}
	if !m.feed.NeedsMore(m.sel[panelCommits]) {
		return nil
	}
	return m.loadMoreCmd()
}

// reRoot points the model at the repository rooted at path and triggers a full
// reload. switchTarget records where a shell should follow on exit (written to
// --cwd-file by cmd/gg). A fresh span ring is used for the new root; the cmd/gg
// panic dump still references the original repo (acceptable for a debug aid).
func (m Model) reRoot(path string) (tea.Model, tea.Cmd) {
	m.svc = domain.Open(path)
	m.repo = m.svc.Repo()
	m.feed = m.svc.CommitFeed()
	m.switchTarget = path
	m.loading = true
	// Drop selections from the old repo so the highlight doesn't land on a
	// surprising row in the newly-loaded panels.
	m.sel = map[panel]int{}
	m.mark = nil      // a mark from the old repo must not re-attach by name in the new one
	m.filesView = nil // the new repo has a different commit list
	m.filesHash = ""
	m.filesTreeFocused = false
	m.diffView = nil // the new repo invalidates any open diff
	m.diffTag = ""
	m.loadGen++
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

// wheelStep is the configured rows-per-mouse-wheel-tick ([ui] wheel_step),
// defaulting to 3 before the first config load (m.cfg is zero until
// dataLoadedMsg arrives).
func (m Model) wheelStep() int {
	if s := m.cfg.UI.WheelStep; s > 0 {
		return s
	}
	return 3
}
