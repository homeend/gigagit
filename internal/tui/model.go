// Package tui implements the gigagit terminal UI with Bubble Tea.
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/commitgraph"
	"github.com/gigagit/gg/internal/config"
	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/hunkpick"
	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/rebaseplan"
	"github.com/gigagit/gg/internal/textdiff"
)

// Model is the root Bubble Tea model.
type Model struct {
	width, height int

	loading  bool
	err      error
	status   model.WorkingTreeStatus
	branches []model.Branch
	commits  []model.Commit

	worktrees       []model.Worktree
	tags            []model.Tag         // refs/tags; shown by the Tags tab in the middle slot
	reflog          []model.ReflogEntry // HEAD reflog; shown by the Reflog tab in the bottom slot
	currentWorktree string

	cfg          config.Config
	gitCommonDir string

	initHomeDir           string // home dir for agent detection; "" skips home-scoped agents (tests)
	statePath             string // repo-registry location; "" disables recording (tests)
	pendingSeqBump        []string
	pendingSwitch         bool
	switchTarget          string
	pendingCompare        *pendingCompare // focused file awaiting the compare-mode picker; nil = none
	pendingSwitchBranch   string          // branch to SmartSwitch to after a successful op (B = create-and-switch)
	pendingRefsReload     bool            // after this op, refresh only branches+worktrees (not a full Snapshot) — set by create-worktree
	pendingIdentityReload bool            // after this op (SetIdentity), re-read only the identity (not a full Snapshot) — config write changes no status/refs
	identity              model.Identity  // last-read git user identity (refreshed after SetIdentity); the identityView popup loads its own fresh copy

	mark             *markState      // the m-key mark; nil = none (see mark.go)
	fileMarks        map[string]bool // multi-selected Status file paths (keyed by path)
	commitCompareSet map[string]bool // commits toggled into the ◉ compare selection (keyed by hash)
	actionMenu       *actionMenu     // . action menu (list + run available actions); nil = closed

	stashView *stashView // stash list in the right column (over Commits); nil = closed

	conflict domain.ConflictState // source of the current conflict (merge/rebase parties), for the notice

	filesView         *contentPopup  // commit files tree replacing the left column; nil = closed
	filesTitle        string         // "Files <short-hash> <subject>", updated with the content
	filesHash         string         // commit the view wants; gates stale async results
	filesCompare      bool           // true = compare mode (filesLeft/Right) vs legacy commit-vs-parent
	filesLeft         model.Endpoint // compare mode: older side
	filesRight        model.Endpoint // compare mode: newer side
	compareTag        string         // gates stale compareFilesMsg results
	filesStashTag     string         // when the files tree is showing a stash: its ref (gates stash-file loads)
	filesTreeFocused  bool           // true = the tree side owns vertical movement (←/→/tab)
	filesReadInflight bool           // a per-commit files-view CommitFiles read is outstanding; drop further nav reads until it lands (pure-drop pacing on large repos)
	filesAllFiles     bool           // true = full-tree mode (every file at the commit, ls-tree) vs the changed set; toggled by `a`
	filesPreview      *contentPopup  // full-tree mode: read-only file content shown in the right column (nil = none)
	filesPreviewTag   string         // <path>@<hash>; gates stale ShowFile results for the preview

	diffView    *diffView   // full-screen side-by-side diff; nil = closed
	diffTag     string      // request key of the wanted diff; gates stale async results
	diffNav     diffNavKind // which list the open diff was opened from (Home/End file-stepping)
	diffNotice  string      // transient bottom-left diff-view notice (file arrival / no-file); cleared on the next key
	diffPartial bool        // session default for new diffs (false = full); the f key toggles it
	diffLong    longMode    // session: long-line mode for new diffs (0 = scroll); w cycles

	layers *layerStack // top-of-everything window pile: full-screen surfaces + centered popups; nil/empty = none

	svc                 *domain.Service    // command layer; all git access goes through svc
	feed                *domain.CommitFeed // single source of truth for commits
	commitsExhausted    bool               // false → "Commits N+", true → "Commits N"
	commitsLoading      bool               // a feed reload/page is in flight → show the loading glyph in the Commits title
	commitScopeBranches []string           // included branches for the feed; empty = all local branches
	commitGraphRows     []string           // cached single-line graph cells, parallel to the unified WIP+commits list; empty = none
	commitGraphLanes    []int              // cached node lane per unified row, parallel to the unified WIP+commits list
	wipRows             []wipRow           // 0–2 derived pseudo-rows (Working tree / Staged) shown atop the Commits feed when dirty
	commitListMode      bool               // Commits feed rendered as a flat ●-gutter list, not a graph
	commitGraphCols     int                // graph window width in LANES; 0 = use configured default
	commitGraphScroll   int                // leftmost visible lane (0-based); resets on feed reload
	opCancel            context.CancelFunc // cancels the in-flight op's context; nil when idle
	loadGen             int                // bumped per superseding load; stale dataLoadedMsg are dropped
	proc                process            // the single active long-running process; nil = none. IS the interface lock.

	running   bool
	statusMsg string
	opMsgs    chan tea.Msg
	modal     *decisionState

	focus           panel
	lastLeftPanel   panel // ←'s return target; zero value = panelBranches
	activeLeftTab   panel // which of Branches/Remotes/Worktrees shows in the shared left tab slot; zero value = panelBranches
	activeFilesTab  panel // Files or Tags in the middle slot; zero value resolves to panelFiles via middleTab()
	activeBottomTab panel // Staged or Reflog in the bottom slot; zero value resolves to panelStaged via bottomTab()
	leftMax         panel // the pinned full-column left panel (valid only when leftMaxed)
	leftMaxed       bool  // t has maximized leftMax to fill the whole left column

	remoteBranches []model.RemoteBranch // refs/remotes; shown by the Remotes tab
	shelfEntries   []model.ShelfEntry   // default bucket; shown by the Shelf tab
	sel            map[panel]int
	sortModes      map[panel]sortMode // per-panel display order (zero value = default)
	dispModes      map[panel]dispMode // per-panel text display mode (zero value = modeCutoff); z cycles
	hscroll        map[panel]int      // per-panel horizontal scroll (modeScroll); shift+←/→
	headTimes      map[string]int64   // worktree HEAD sha -> committer time (date sort)

	filterPanel  panel  // panel the filter is bound to (meaningful only when filterQuery != "" or filterTyping)
	filterQuery  string // case-insensitive substring; "" = no filter
	filterTyping bool   // true while /-input mode is capturing keys

	highlightQuery  string // Commits-panel @-highlight: case-insensitive substring; "" = no committed query
	highlightTyping bool   // true while @-input mode is capturing keys

	searchHist map[string][]string // per-scope search-history rings, newest-first (loaded at startup)

	recallScope string // active recall ring; "" = none
	recallOpen  bool   // history dropdown visible
	recallIndex int    // highlight into the ring; 0 = newest (meaningful when recallOpen)
	recallDraft string // text captured when the dropdown opened (restored on esc/back-out)
}

type panel int

const (
	panelBranches panel = iota
	panelWorktrees
	panelRemotes
	panelFiles
	panelStaged
	panelCommits
	panelTags
	panelReflog
	panelCount
)

// leftTabs is the display order of the shared left-slot tabs; the ctrl+←/→
// cycle walks this list. Enum value order is unrelated to display order.
var leftTabs = []panel{panelBranches, panelRemotes, panelWorktrees}

// filesTabs is the display/cycle order of the middle-slot tabs (the Files box).
var filesTabs = []panel{panelFiles, panelTags}

// bottomTabs is the display/cycle order of the bottom-left slot tabs (the Staged
// box shares its slot with the read-only Reflog viewer).
var bottomTabs = []panel{panelStaged, panelReflog}

// New constructs the initial model for svc.
func New(svc *domain.Service) Model {
	return Model{
		svc:           svc,
		feed:          svc.CommitFeed(),
		loading:       true,
		sel:           map[panel]int{},
		sortModes:     map[panel]sortMode{panelBranches: sortDateDesc},
		dispModes:     map[panel]dispMode{},
		hscroll:       map[panel]int{},
		activeLeftTab: panelBranches,
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return tea.Batch(m.loadCmd(), loadSearchHistCmd(m.svc)) }

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
			m.filesCompare = false
			m.compareTag = ""
			m.filesTreeFocused = false
			m.statusMsg = "files view closed: terminal too narrow"
		}
		if m.diffLayer() != nil && msg.Width > 0 {
			if msg.Width < 60 {
				m.diffView = nil
				m.diffTag = ""
				m.statusMsg = "diff closed: terminal too narrow"
			} else {
				// Re-wrap at the new width, keeping the viewport anchored to
				// the logical line currently at the top.
				v := m.diffLayer()
				topLine := 0
				if v.offset < len(v.disp) {
					topLine = v.disp[v.offset].line
				}
				w, _ := m.overlayDims()
				v.relayout(w)
				v.clampHOffset()
				if topLine < len(v.lineStart) {
					v.offset = v.lineStart[topLine]
				}
				v.scroll(0, m.diffBodyRows())
			}
		}
	case diffMsg:
		if m.diffLayer() == nil || msg.tag != m.diffTag {
			return m, nil // view closed, or a stale result
		}
		m.diffView = msg.view
		if m.diffLayer() != nil {
			m.diffLayer().loading = false // a diffMsg means the load completed (content or err); never leave the body on "(loading…)"
		}
		return m, nil
	case commitFilesMsg:
		m.filesReadInflight = false // the outstanding per-commit read has landed; nav may issue again
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
	case treeFilesMsg:
		m.filesReadInflight = false
		if m.filesView == nil || !m.filesAllFiles || msg.hash != m.filesHash {
			return m, nil // view closed, switched back to changed files, or stale
		}
		if msg.err != nil {
			m.statusMsg = "files: " + msg.err.Error()
			if len(m.filesView.lines) == 1 && m.filesView.lines[0].text == "(loading…)" {
				m.filesView.lines = []contentLine{{text: "(load failed)"}}
			}
			return m, nil
		}
		m.filesView.lines = msg.lines // pre-built off-thread
		m.filesView.sel = 0
		m.filesTitle = "Files " + shortHash(msg.hash) + " (all files) " + msg.subject
		return m, nil
	case fileContentMsg:
		if m.filesPreview == nil || msg.tag != m.filesPreviewTag {
			return m, nil // preview closed, or a stale load (another file opened)
		}
		if msg.err != nil {
			m.filesPreview.lines = []contentLine{{text: "(load failed: " + msg.err.Error() + ")"}}
			return m, nil
		}
		m.filesPreview.lines = msg.lines
		m.filesPreview.sel = 0
		return m, nil
	case compareFilesMsg:
		if m.filesView == nil || !m.filesCompare || msg.tag != m.compareTag {
			return m, nil // stale or closed
		}
		if msg.err != nil {
			m.statusMsg = "compare: " + msg.err.Error()
			if len(m.filesView.lines) == 1 && m.filesView.lines[0].text == "(loading…)" {
				m.filesView.lines = []contentLine{{text: "(load failed)"}}
			}
			return m, nil
		}
		m.filesView.lines = commitFileLines(msg.files)
		m.filesView.sel = 0
		return m, nil
	case commitsPagedMsg:
		if m.feed != nil && msg.gen == m.feed.Gen() {
			st := m.feed.Snapshot()
			m.commits = st.Commits
			m.commitsExhausted = st.Exhausted
			m.commitsLoading = false // this page's load (the latest) finished
			m = m.rebuildCommitGraph()
		}
		return m, nil
	case commitsReloadedMsg:
		if m.feed == nil || msg.gen != m.feed.Gen() {
			return m, nil // superseded by a newer reload (gen-stamped at load time)
		}
		m.commits = msg.state.Commits
		m.commitsExhausted = msg.state.Exhausted
		m.commitsLoading = false // the latest scope reload finished
		m = m.rebuildCommitGraph()
		if m.sel[panelCommits] >= len(m.commits) {
			m.sel[panelCommits] = 0
		}
		return m, nil
	case historyListMsg:
		if h := layerOf[*historyView](m); h != nil && h.listTag == msg.tag {
			h.loading = false
			h.err = msg.err
			h.commits = msg.commits
			h.sel = 0
			if len(h.commits) > 0 {
				return m, h.selectCmd(m)
			}
		}
		return m, nil
	case historyDiffMsg:
		if h := layerOf[*historyView](m); h != nil && h.diffTag == msg.tag {
			h.diff = msg.view
		}
		return m, nil
	case blameMsg:
		if b := layerOf[*blameView](m); b != nil && b.tag == msg.tag {
			b.loading = false
			b.err = msg.err
			b.lines = msg.lines
			b.blocks = groupBlame(msg.lines)
			b.sel = 0
		}
		return m, nil
	case shelfLoadedMsg:
		// A disabled shelf (no state dir) reports its reason but is not fatal.
		if msg.err != nil {
			m.statusMsg = "shelf: " + msg.err.Error()
			m.shelfEntries = nil
			m.pendingCompare = nil
		} else {
			m.shelfEntries = msg.entries
		}
		if msg.open && msg.err == nil {
			p := newShelfPopup(msg.entries)
			if pc := m.pendingCompare; pc != nil && pc.target == compareShelf {
				p.compareRef = &pc.ref
				p.compareLabel = pc.label
				m.pendingCompare = nil
			}
			if existing := m.shelfSwitcher(); existing != nil {
				*existing = *p // reopen after a remove: refresh the live switcher in place
			} else {
				m = m.pushLayer(p)
			}
		}
		return m, nil
	case shelfAddedMsg:
		if msg.err != nil {
			m.statusMsg = "shelf add: " + msg.err.Error()
		} else {
			m.statusMsg = "shelved " + msg.entry.Origin.Path + " → " + msg.entry.ID
		}
		return m, nil
	case bookmarkAddedMsg:
		if msg.err != nil {
			m.statusMsg = "bookmark: " + msg.err.Error()
		} else {
			m.statusMsg = "bookmarked " + msg.bm.Path + " → " + msg.bm.ID
		}
		return m, nil
	case bookmarksLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "bookmarks: " + msg.err.Error()
			m.pendingCompare = nil // don't let stale compare state hijack the next plain `g`
			return m, nil
		}
		p := newBookmarkPopup(msg.items)
		if pc := m.pendingCompare; pc != nil && pc.target == compareBookmark {
			p.compareRef = &pc.ref
			p.compareLabel = pc.label
			m.pendingCompare = nil
		}
		if existing := m.bookmarkSwitcher(); existing != nil {
			*existing = *p // reopen after a remove: refresh the live switcher in place
			return m, nil
		}
		return m.pushLayer(p), nil
	case searchHistLoadedMsg:
		if msg.rings != nil {
			m.searchHist = msg.rings
		}
		return m, nil
	case dataLoadedMsg:
		if msg.gen != m.loadGen {
			return m, nil // superseded by a newer load
		}
		m.loading = false
		m.commitsLoading = false // the full load (which includes the feed) is done
		m.err = msg.err
		if msg.err == nil {
			m.status = msg.status
			m.conflict = msg.conflict
			m.branches = msg.branches
			m.remoteBranches = msg.remoteBranches
			m.commits = msg.commits
			m.commitsExhausted = msg.commitsExhausted
			m = m.rebuildCommitGraph()
			if msg.commitErr != nil {
				m.statusMsg = "commits: " + msg.commitErr.Error()
			}
			m.worktrees = msg.worktrees
			m.tags = msg.tags
			m.reflog = msg.reflog
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
			// An active process advances from the freshly-reloaded state (e.g.
			// the conflict process re-derives its file list after a resolve).
			if m.proc != nil {
				return m.proc.refreshed(m)
			}
			// Conflicts are surfaced as a non-blocking notice ("press [x] to
			// resolve"); entering the resolution process is the user's choice (x),
			// so a lingering conflict never traps the interface.
		}
	case tea.KeyMsg:
		// The status line holds a transient message (an op result, an error, a
		// refusal hint). Clear it as the user moves on to the next interaction,
		// so a stale error doesn't linger across navigation and reloads; the
		// handlers below re-set it when they have something fresh to say. Gated
		// on idle so an in-flight op's "working…" notice survives stray keys.
		if !m.running {
			m.statusMsg = ""
		}
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
				opt := m.modal.req.Options[m.modal.sel]
				if r := m.modal.onResolve; r != nil {
					m.modal = nil
					return r(m, opt)
				}
				m.modal.reply <- engine.DecisionResponse{Option: opt}
				m.modal = nil
			case "esc":
				opt := abortOption(m.modal.req.Options)
				if r := m.modal.onResolve; r != nil {
					m.modal = nil
					return r(m, opt)
				}
				m.modal.reply <- engine.DecisionResponse{Option: opt}
				m.modal = nil
			}
			return m, nil
		}
		// A process owns the interface entirely: while one is active all input is
		// its own and every other window/command below is unreachable. Sits just
		// below the modal (a process's own job may still raise a decision).
		if m.proc != nil {
			return m.proc.update(m, msg)
		}
		// The action menu is a modal-like overlay: once open it owns the
		// keyboard above every content window. Checked before the surface stack
		// and the diff view so those windows can open it (with . opt-in) and the
		// menu then receives the keys instead of the underlying window.
		if m.actionMenu != nil {
			return m.updateActionMenuKey(msg)
		}
		// The layer stack (full-screen surfaces + centered popups) is global: its
		// top owns the keyboard above the diff view (mirrors the action menu and
		// render()). History/blame/rebase editors and the bookmark/shelf switchers,
		// their child popups, and the help / `?` cheat-sheet viewer all live here,
		// so esc on the cheat-sheet returns to the switcher beneath. ctrl+c is not
		// special-cased: every layer (surfaces and popups) quits on it in its own
		// update.
		if l := m.topLayer(); l != nil {
			return l.update(m, msg)
		}
		// Routing invariant: the diff view is checked immediately after the
		// modal here, in the MouseMsg arm, and in render() — the key owner
		// must be the top visible surface (background ops will rely on it).
		if m.diffLayer() != nil {
			return m.updateDiffViewKey(msg)
		}
		// Filter-input mode captures every key (the panel label shows the query).
		// Hoisted above the files-view and stash routing so a commit filter opened
		// from the files view's list side keeps receiving keystrokes; the tree's
		// own filter rides contentPopup.typing (not m.filterTyping), so the two
		// never collide.
		if m.filterTyping {
			if nm, nq, handled, commit := m.recallUpdate(scopePanel, msg, m.filterQuery); handled {
				m = nm
				m.filterQuery = nq
				m.sel[m.filterPanel] = 0
				if commit {
					m.filterTyping = false
					m, recCmd := m.recordSearch(scopePanel, m.filterQuery)
					if m.filesView != nil && m.filterPanel == panelCommits {
						mm, cmd := m.syncFilesViewToSelectedCommit()
						return mm, tea.Batch(recCmd, cmd)
					}
					return m, recCmd
				}
				return m, nil
			} else {
				m = nm // recall may have closed on a fall-through key
			}
			switch msg.Type {
			case tea.KeyCtrlC:
				return m, tea.Quit
			case tea.KeyEsc:
				m.filterTyping = false
				m.filterQuery = ""
			case tea.KeyEnter:
				m.filterTyping = false // commit: filter stays active
				m, recCmd := m.recordSearch(scopePanel, m.filterQuery)
				// With the files view open over a commit filter, point the tree at
				// the now-selected commit so "search commits → see its files" needs
				// no extra keypress.
				if m.filesView != nil && m.filterPanel == panelCommits {
					mm, cmd := m.syncFilesViewToSelectedCommit()
					return mm, tea.Batch(recCmd, cmd)
				}
				return m, recCmd
			// Arrows/pages navigate the filtered rows live (an incremental
			// picker, like the repo switcher); they stay in /-input mode and do
			// NOT reset the cursor. Vim j/k are query text here, not motions.
			case tea.KeyUp:
				if m.sel[m.filterPanel] > 0 {
					m.sel[m.filterPanel]--
				}
			case tea.KeyDown:
				if m.sel[m.filterPanel] < m.panelLen(m.filterPanel)-1 {
					m.sel[m.filterPanel]++
				}
			case tea.KeyPgUp:
				if m.sel[m.filterPanel] -= m.pageStep(); m.sel[m.filterPanel] < 0 {
					m.sel[m.filterPanel] = 0
				}
			case tea.KeyPgDown:
				m.sel[m.filterPanel] += m.pageStep()
				if n := m.panelLen(m.filterPanel); m.sel[m.filterPanel] > n-1 {
					m.sel[m.filterPanel] = n - 1
				}
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
		// @-highlight typing captures every key (the panel label shows the query).
		// Mirrors the filter loop, but: ctrl+↑/↓ jump matches, and a query edit
		// snaps the cursor to the nearest match instead of resetting it to row 0.
		if m.highlightTyping {
			if nm, nq, handled, commit := m.recallUpdate(scopePanel, msg, m.highlightQuery); handled {
				m = nm
				m.highlightQuery = nq
				m = m.snapToHighlightMatch()
				if commit {
					m.highlightTyping = false
					m, recCmd := m.recordSearch(scopePanel, m.highlightQuery)
					return m, recCmd
				}
				return m, nil
			} else {
				m = nm
			}
			switch msg.Type {
			case tea.KeyCtrlC:
				return m, tea.Quit
			case tea.KeyEsc:
				m.highlightTyping = false
				m.highlightQuery = ""
			case tea.KeyEnter:
				m.highlightTyping = false // commit: highlight stays active
				var recCmd tea.Cmd
				m, recCmd = m.recordSearch(scopePanel, m.highlightQuery)
				return m, recCmd
			case tea.KeyUp:
				if m.sel[panelCommits] > 0 {
					m.sel[panelCommits]--
				}
			case tea.KeyDown:
				if m.sel[panelCommits] < m.panelLen(panelCommits)-1 {
					m.sel[panelCommits]++
				}
			case tea.KeyCtrlUp:
				if i, ok := m.scanHighlightMatch(m.sel[panelCommits], -1, false); ok {
					m.sel[panelCommits] = i
				}
			case tea.KeyCtrlDown:
				if i, ok := m.scanHighlightMatch(m.sel[panelCommits], +1, false); ok {
					m.sel[panelCommits] = i
				}
			case tea.KeyBackspace, tea.KeyCtrlH:
				if r := []rune(m.highlightQuery); len(r) > 0 {
					m.highlightQuery = string(r[:len(r)-1])
				}
				m = m.snapToHighlightMatch()
			case tea.KeySpace:
				m.highlightQuery += " "
				m = m.snapToHighlightMatch()
			case tea.KeyRunes:
				m.highlightQuery += string(msg.Runes)
				m = m.snapToHighlightMatch()
			}
			return m, nil
		}
		if m.filesView != nil {
			return m.updateFilesViewKey(msg)
		}
		// The stash list owns the keyboard only while it is the focused (right)
		// column. When focus has moved to a left panel (← ), keys fall through to
		// the normal dispatch so the left panels stay navigable with the stash
		// list visible-but-dimmed on the right.
		if m.stashView != nil && m.focus == panelCommits {
			return m.updateStashViewKey(msg)
		}
		if msg.Type == tea.KeySpace {
			return m.handleStageKey()
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
				return m.startOp(m.pullForFocus())
			}
		case "f":
			if m.canFetchRemotes() {
				return m.startOp(engine.Fetch{})
			}
		case "P":
			if !m.running && !m.loading && m.status.Branch != "" {
				return m.startOp(engine.Push{Remote: "origin", Branch: m.status.Branch, SetUpstream: true})
			}
		case "c":
			if m.focus == panelRemotes && m.canCheckoutRemote() {
				rb, _ := m.selectedRemote()
				return m.startOp(engine.SmartCheckout{RemoteRef: rb.Name, Local: rb.Branch, Intent: engine.CheckoutStay})
			}
			if m.canCommit() {
				m = m.pushLayer(&commitPopup{})
			}
		case "C":
			if m.canAmend() {
				return m, m.amendPrefillCmd()
			}
		case "x":
			if m.opsIdle() && len(m.status.Conflicts()) > 0 {
				return startConflictProcess(m) // enter / resume from the notice
			}
		case "H":
			if m.canStageHunks() {
				bi, _ := m.backingIndex(panelFiles)
				return m, m.loadStageHunksCmd(m.status.Files[bi].Path)
			}
		case "s":
			if m.focus == panelRemotes && m.canCheckoutRemote() {
				rb, _ := m.selectedRemote()
				return m.startOp(engine.SmartCheckout{RemoteRef: rb.Name, Local: rb.Branch, Intent: engine.CheckoutSwitch})
			}
			if m.focus == panelFiles && m.opsIdle() {
				if mm, ok := m.openStashPopup(); ok {
					return mm, nil
				}
				m.statusMsg = "nothing to stash"
				return m, nil
			}
			if m.canSwitchBranch() {
				b, _ := m.selectedBranch()
				if wt, ok := m.worktreeForBranch(b.Name); ok {
					wtPath := wt.Path
					m.modal = &decisionState{
						req: engine.DecisionRequest{
							ID:      "switch-to-worktree",
							Prompt:  b.Name + " is checked out in another worktree:\n" + wtPath,
							Options: []string{"go to worktree", "cancel"},
						},
						onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
							if opt == "go to worktree" {
								return m.reRoot(wtPath)
							}
							return m, nil
						},
					}
					return m, nil
				}
				return m.startOp(engine.SmartSwitch{Branch: b.Name})
			}
		case "S":
			if m.stashView != nil { // toggle closed (focus is on a left panel here)
				return m.closeStashView(), nil
			}
			if m.opsIdle() {
				return m.openStashView()
			}
		case "u":
			if !m.running && !m.loading {
				return m.startOp(engine.UndoLastCommit{})
			}
		case "g": // open the bookmark quick-switcher (global; see openBookmarkSwitcher)
			return m.openBookmarkSwitcher()
		case "G": // open the shelf quick-switcher (global; see openShelfSwitcher)
			return m.openShelfSwitcher()
		case "z": // cycle the focused panel's text display mode
			m.dispModes[m.focus] = m.dispModes[m.focus].next()
			m.hscroll[m.focus] = 0
			return m, nil
		case "t": // toggle maximize of the focused left-column panel
			if m.canMaximizeLeft() {
				if m.leftMaxed && m.leftMax == m.focus {
					m.leftMaxed = false
				} else {
					m.leftMaxed = true
					m.leftMax = m.focus
				}
			}
			return m, nil
		case "shift+left":
			if m.focus == panelCommits && m.graphActive() {
				m.commitGraphScroll = m.clampScroll(m.commitGraphScroll - m.graphPanStep())
				return m, nil
			}
			if m.dispModes[m.focus] == modeScroll && m.hscroll[m.focus] > 0 {
				if m.hscroll[m.focus] -= m.hscrollStep(); m.hscroll[m.focus] < 0 {
					m.hscroll[m.focus] = 0
				}
			}
			return m, nil
		case "shift+right":
			if m.focus == panelCommits && m.graphActive() {
				m.commitGraphScroll = m.clampScroll(m.commitGraphScroll + m.graphPanStep())
				return m, nil
			}
			if m.dispModes[m.focus] == modeScroll {
				m.hscroll[m.focus] += m.hscrollStep()
			}
			return m, nil
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
			if m.canShowFileDiff() {
				bi, _ := m.backingIndex(m.focus)
				f := m.status.Files[bi]
				ctx := navContext{path: f.Path, rev: ""}
				bv := newBlameView(ctx)
				m = m.pushLayer(bv)
				return m, m.loadBlameCmd(ctx, bv.tag)
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
			case panelFiles:
				if !m.canDiscard() {
					return m, nil
				}
				restore, remove, n := m.discardTargets()
				if n == 0 {
					m.statusMsg = "nothing to discard (resolve conflicts first)"
					return m, nil
				}
				m.modal = &decisionState{
					req: engine.DecisionRequest{
						ID:      "discard",
						Prompt:  discardPrompt(restore, remove, n),
						Options: []string{"Discard", "Cancel"},
					},
					onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
						if opt == "Discard" {
							m.fileMarks = nil
							return m.startOp(engine.Discard{Restore: restore, Remove: remove})
						}
						return m, nil
					},
				}
				return m, nil
			}
		case "D":
			if m.focus != panelFiles || !m.opsIdle() {
				return m, nil
			}
			if !m.canDiscardAll() {
				// canDiscardAll false here means one of two refusable states;
				// explain which so the no-op isn't silent.
				if len(m.status.Conflicts()) > 0 {
					m.statusMsg = "resolve conflicts before discarding all"
				} else {
					m.statusMsg = "nothing to discard"
				}
				return m, nil
			}
			m.modal = &decisionState{
				req: engine.DecisionRequest{
					ID:      "discard-all",
					Prompt:  "Discard ALL unstaged changes? This cannot be undone.",
					Options: []string{"Discard", "Cancel"},
				},
				onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
					if opt == "Discard" {
						m.fileMarks = nil
						return m.startOp(engine.Discard{All: true})
					}
					return m, nil
				},
			}
			return m, nil
		case "enter":
			if m.focus == panelTags {
				return m.tagJumpToCommit()
			}
			if m.focus == panelWorktrees && m.canEnterWorktree() {
				wt, _ := m.selectedWorktree()
				return m.reRoot(wt.Path)
			}
			// A WIP pseudo-row opens its node-vs-parent compare (mirrors l).
			if m.focus == panelCommits && m.canShowCommitFiles() {
				if r, ok := m.wipRowAt(m.commitSelUnified()); ok {
					if m.width > 0 && m.width < 40 {
						m.statusMsg = "terminal too narrow for the files view"
						return m, nil
					}
					left, right := m.wipEndpoints(r)
					return m.openCompareFiles(left, right)
				}
			}
			if m.canShowFileDiff() {
				bi, _ := m.backingIndex(m.focus)
				f := m.status.Files[bi]
				return m.openStatusDiff(f, m.focus == panelStaged)
			}
			if m.canShowReflogFiles() {
				return m.openReflogFiles()
			}
		case "h":
			if m.canShowFileDiff() {
				bi, _ := m.backingIndex(m.focus)
				f := m.status.Files[bi]
				ctx := navContext{path: f.Path, rev: ""}
				h := newHistoryView(ctx)
				m = m.pushLayer(h)
				return m, m.loadHistoryListCmd(ctx, h.listTag)
			}
		case "tab":
			m = m.rememberLeftFocus()
			m.focus = nextInOrder(m.focusOrder(), m.focus, +1)
		case "shift+tab":
			m = m.rememberLeftFocus()
			m.focus = nextInOrder(m.focusOrder(), m.focus, -1)
		case "ctrl+left", "ctrl+right":
			// Cycle whichever tab slot currently owns focus: the top refs slot
			// (Branches · Remotes · Worktrees) or the middle files slot
			// (Files · Tags). Wraps; focuses the now-active tab.
			dir := 1
			if msg.String() == "ctrl+left" {
				dir = -1
			}
			if m.focus == panelFiles || m.focus == panelTags {
				m.activeFilesTab = nextInOrder(filesTabs, m.middleTab(), dir)
				m.focus = m.activeFilesTab
				if m.leftMaxed { // re-pin the newly shown tab so it stays full-height
					m.leftMax = m.focus
				}
				return m, nil
			}
			if m.focus == panelStaged || m.focus == panelReflog {
				m.activeBottomTab = nextInOrder(bottomTabs, m.bottomTab(), dir)
				m.focus = m.activeBottomTab
				if m.leftMaxed { // re-pin the newly shown tab so it stays full-height
					m.leftMax = m.focus
				}
				return m, nil
			}
			m.activeLeftTab = nextInOrder(leftTabs, m.activeLeftTab, dir)
			m.focus = m.activeLeftTab
			m.lastLeftPanel = m.activeLeftTab
			if m.leftMaxed { // re-pin the newly shown tab so it stays full-height
				m.leftMax = m.focus
			}
			return m, nil
		case "right":
			if m.focus != panelCommits {
				m = m.rememberLeftFocus()
				m.focus = panelCommits
			}
		case "left":
			// No-op when already in the left column, and when the narrow
			// layout has no left column to focus.
			if m.focus == panelCommits && (m.width <= 0 || m.width >= 40) {
				m.focus = m.leftReturnTarget()
			}
		case "pgdown":
			if n := m.panelLen(m.focus); n > 0 {
				m.sel[m.focus] += m.pageStep()
				if m.sel[m.focus] > n-1 {
					m.sel[m.focus] = n - 1
				}
			}
			if m.focus == panelCommits {
				if mm, cmd := m.maybeLoadMoreCommits(); cmd != nil {
					return mm, cmd
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
		case ">":
			if m.focus == panelCommits && m.graphActive() {
				m.commitGraphCols = m.clampCols(m.graphCols() + m.graphStep())
				m.commitGraphScroll = m.clampScroll(m.commitGraphScroll)
				return m, nil
			}
		case "<":
			if m.focus == panelCommits && m.graphActive() {
				m.commitGraphCols = m.clampCols(m.graphCols() - m.graphStep())
				m.commitGraphScroll = m.clampScroll(m.commitGraphScroll)
				return m, nil
			}
		case "=":
			if m.focus == panelCommits && m.graphActive() {
				m = m.snapGraphToSelected()
				return m, nil
			}
		case "/":
			if !m.running && !m.loading {
				m.highlightTyping = false // mutually exclusive with @-highlight
				m.highlightQuery = ""
				m.filterPanel = m.focus
				m.filterQuery = ""
				m.filterTyping = true
				m.sel[m.focus] = 0
				m = m.recallReset()
			}
		case "@":
			if !m.running && !m.loading && m.focus == panelCommits {
				m.filterTyping = false // mutually exclusive with the / filter
				if m.filterPanel == panelCommits {
					m.filterQuery = ""
				}
				m.highlightQuery = ""
				m.highlightTyping = true
				m = m.recallReset()
			}
		case "ctrl+up":
			if m.highlightActive() && m.focus == panelCommits {
				if i, ok := m.scanHighlightMatch(m.sel[panelCommits], -1, false); ok {
					m.sel[panelCommits] = i
				}
				return m, nil
			}
		case "ctrl+down":
			if m.highlightActive() && m.focus == panelCommits {
				if i, ok := m.scanHighlightMatch(m.sel[panelCommits], +1, false); ok {
					m.sel[panelCommits] = i
				}
				return m, nil
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
		case ".":
			// Reaches here only from the base layout (every popup/modal/view
			// returns earlier); the menu lists whatever is currently available.
			return m.openActionMenu(), nil
		case "?":
			m = m.pushLayer(newContentPopup("Help — keys", helpContent()))
		case "l":
			if m.focus == panelCommits && m.canShowCommitFiles() {
				if m.width > 0 && m.width < 40 {
					m.statusMsg = "terminal too narrow for the files view"
					return m, nil
				}
				// A pseudo-row opens its node-vs-parent compare instead of a commit's
				// files (it is not a real commit).
				if u := m.commitSelUnified(); m.isWipRow(u) {
					if r, ok := m.wipRowAt(u); ok {
						left, right := m.wipEndpoints(r)
						return m.openCompareFiles(left, right)
					}
				}
				bi, ok := m.backingIndex(panelCommits)
				if !ok {
					return m, nil
				}
				c := m.commits[bi]
				m.filesView = &contentPopup{lines: []contentLine{{text: "(loading…)"}}}
				m.filesTitle = "Files " + shortHash(c.Hash) + " " + c.Subject
				m.filesHash = c.Hash
				m.filesTreeFocused = false // always open on the commit list
				m.filesAllFiles = false    // open in changed-files mode; `a` toggles
				m.filesPreview = nil
				m.filesPreviewTag = ""
				m.filesReadInflight = true
				return m, m.loadCommitFilesCmd(c)
			}
			if m.canShowReflogFiles() {
				return m.openReflogFiles()
			}
		case "shift+down", "shift+up":
			// Grow the ◉ compare selection as a contiguous run: add the current
			// row and the landed row to the set, then move the cursor.
			if m.focus != panelCommits || !m.opsIdle() {
				break
			}
			if m.commitCompareSet == nil {
				m.commitCompareSet = map[string]bool{}
			}
			if bi, ok := m.backingIndex(panelCommits); ok {
				m.commitCompareSet[m.commits[bi].Hash] = true
			}
			if msg.String() == "shift+down" {
				if m.sel[panelCommits] < m.panelLen(panelCommits)-1 {
					m.sel[panelCommits]++
				}
			} else if m.sel[panelCommits] > 0 {
				m.sel[panelCommits]--
			}
			if bi, ok := m.backingIndex(panelCommits); ok {
				m.commitCompareSet[m.commits[bi].Hash] = true
			}
			return m, nil
		case "m":
			if m.canMark() {
				return m.handleMarkKey()
			}
		case "esc":
			if m.mark != nil {
				m.mark = nil
				return m, nil
			}
			// A committed @-highlight clears first (it never reorders/filters, so it
			// is the lighter state to drop).
			if m.highlightQuery != "" {
				m.highlightQuery = ""
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
				if mm, cmd := m.maybeLoadMoreCommits(); cmd != nil {
					return mm, cmd
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
		refsReload := false
		idReload := false
		if msg.err != nil {
			m.statusMsg = friendlyOpError(msg.err)
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
			refsReload = m.pendingRefsReload
			idReload = m.pendingIdentityReload
		}
		m.pendingSeqBump = nil
		m.pendingSwitch = false
		m.pendingSwitchBranch = "" // cleared before the chained op starts, so it cannot re-fire
		m.pendingRefsReload = false
		m.pendingIdentityReload = false
		if switchTo != "" {
			return m.reRoot(switchTo)
		}
		if chainSwitch != "" {
			return m.startOp(engine.SmartSwitch{Branch: chainSwitch})
		}
		m.loadGen++ // invalidate any in-flight full load before issuing a refresh
		if m.stashView != nil {
			// A stash op (apply/pop/drop) changed the stash list as well as the
			// working tree — refresh both.
			m.stashView.loading = true
			return m, tea.Batch(m.loadCmd(), m.loadStashListCmd(m.stashView.tag))
		}
		// A job an active process started just returned: let the process advance
		// its state machine (it typically triggers a reload itself).
		if m.proc != nil {
			return m.proc.finished(m, msg.res, msg.err)
		}
		// A ref-only op (create-worktree) changed only branches+worktrees, not the
		// working tree or commit history — a targeted refresh shows the new rows
		// fast instead of paying a full Snapshot's status walk on a huge repo.
		if refsReload {
			return m, m.reloadRefsCmd(m.statusMsg)
		}
		// SetIdentity wrote git config — no status/refs/commits changed, so skip
		// the full Snapshot and just re-read the identity for next time.
		if idReload {
			return m, m.reloadIdentityCmd(m.statusMsg)
		}
		return m, m.loadCmd()

	case identityDataMsg:
		if v := layerOf[*identityView](m); v != nil {
			v.loading = false
			if msg.err != nil {
				m.statusMsg = msg.err.Error()
			} else {
				v.id = msg.id
				v.profiles = msg.profiles
				if v.sel >= len(v.profiles) {
					v.sel = 0
				}
			}
		}
		return m, nil

	case identityRefreshedMsg:
		if msg.err == nil {
			m.identity = msg.id
		}
		if msg.summary != "" {
			m.statusMsg = msg.summary
		}
		return m, nil

	case stashFilesMsg:
		if m.stashView == nil || m.filesView == nil || msg.tag != m.filesStashTag {
			return m, nil
		}
		if msg.err != nil {
			m.statusMsg = "error: " + msg.err.Error()
			return m, nil
		}
		m.filesHash = msg.sha
		m.filesView.lines = msg.lines
		if m.filesView.sel >= len(msg.lines) {
			m.filesView.sel = 0
		}
		return m, nil

	case stashListMsg:
		if m.stashView == nil || msg.tag != m.stashView.tag {
			return m, nil
		}
		m.stashView.loading = false
		if msg.err != nil {
			m.stashView.err = msg.err
			return m, nil
		}
		m.stashView.entries = msg.entries
		if m.stashView.sel >= len(msg.entries) {
			m.stashView.sel = max(0, len(msg.entries)-1)
		}
		return m, nil

	case statusRefreshedMsg:
		m.running = false
		if msg.err != nil {
			m.statusMsg = "error: " + msg.err.Error()
			return m, nil
		}
		m.status = msg.status
		if msg.summary != "" {
			m.statusMsg = msg.summary
		}
		for _, p := range []panel{panelFiles, panelStaged} {
			if n := m.panelLen(p); n > 0 && m.sel[p] >= n {
				m.sel[p] = n - 1
			}
		}
		// The status change adds/removes WIP pseudo-rows; rebuild the graph so its
		// plane stays the same length as the unified Commits list, and clamp the
		// Commits selection if the list shrank (tree went clean).
		m = m.rebuildCommitGraph()
		if n := m.commitsTotal(); n > 0 && m.sel[panelCommits] >= n {
			m.sel[panelCommits] = n - 1
		}
		return m, nil

	case refsRefreshedMsg:
		m.running = false
		if msg.err != nil {
			m.statusMsg = "error: " + msg.err.Error()
			return m, nil
		}
		m.branches = msg.branches
		m.worktrees = msg.worktrees
		if msg.summary != "" {
			m.statusMsg = msg.summary
		}
		// A removed row (e.g. a deleted worktree) must not leave a selection
		// pointing past the end of the now-shorter list.
		for _, p := range []panel{panelBranches, panelWorktrees} {
			if n := m.panelLen(p); m.sel[p] >= n {
				m.sel[p] = max(0, n-1)
			}
		}
		return m, nil

	case editorFinishedMsg:
		if msg.err != nil {
			m.statusMsg = "edit: " + msg.err.Error()
			return m, m.reloadStatusCmd("")
		}
		return m, m.reloadStatusCmd(editedSummary(msg.path))

	case editorViewMsg:
		if msg.err != nil {
			m.statusMsg = "view: " + msg.err.Error()
			return m, nil
		}
		return m, viewExternalCmd(msg.path, msg.name)

	case editorViewFinishedMsg:
		removeTempFile(msg.path)
		if msg.err != nil {
			m.statusMsg = "view: " + msg.err.Error()
			return m, nil
		}
		m.statusMsg = viewedSummary(msg.name)
		return m, nil

	case amendPrefillMsg:
		if msg.err != nil {
			m.statusMsg = "amend: " + msg.err.Error()
			return m, nil
		}
		title, desc := splitMessage(msg.msg)
		m = m.pushLayer(&commitPopup{title: newTextField(title), desc: newTextField(desc), amend: true})
		return m, nil

	case inProgressMsg:
		if cp, ok := m.proc.(*conflictProcess); ok {
			cp.inProgress = msg.op
			// Fully resolved and no merge/rebase still in progress → done.
			if len(cp.files) == 0 && msg.op == "" {
				m.proc = nil
			}
		}
		return m, nil

	case irebaseLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "interactive rebase: " + msg.err.Error()
			return m, nil
		}
		if len(msg.commits) == 0 {
			m.statusMsg = "interactive rebase: no commits in range"
			return m, nil
		}
		ggBin, err := os.Executable()
		if err != nil {
			m.statusMsg = "interactive rebase: " + err.Error()
			return m, nil
		}
		m = m.pushLayer(newIrebaseEditor(msg.branch, msg.onto, msg.commits, ggBin))
		return m, nil

	case rebaseRangeLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "rebase: " + msg.err.Error()
			return m, nil
		}
		plan, perr := rebaseplan.BuildSingleEdit(msg.commits, msg.target, msg.edit)
		if perr != nil {
			m.statusMsg = "rebase: " + perr.Error()
			return m, nil
		}
		ggBin, err := os.Executable()
		if err != nil {
			m.statusMsg = "rebase: " + err.Error()
			return m, nil
		}
		return m.startOp(engine.InteractiveRebase{Branch: msg.branch, Onto: msg.onto, Plan: plan, GGBin: ggBin})

	case squashRangeLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "squash: " + msg.err.Error()
			return m, nil
		}
		plan, perr := rebaseplan.BuildSquash(msg.commits, msg.targets)
		if errors.Is(perr, rebaseplan.ErrNotAdjacent) {
			// Non-adjacent: offer to reorder the commits adjacent, then squash.
			branch, onto := msg.branch, msg.onto
			commits, targets := msg.commits, msg.targets
			m.modal = &decisionState{
				req: engine.DecisionRequest{
					ID:      "squash-reorder",
					Prompt:  "Selected commits aren't adjacent. Reorder them adjacent, then squash?",
					Options: []string{"Reorder & squash", "Cancel"},
				},
				onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
					if opt != "Reorder & squash" {
						return m, nil
					}
					rp, err := rebaseplan.BuildSquashReorder(commits, targets)
					if err != nil {
						m.statusMsg = "squash: " + err.Error()
						return m, nil
					}
					ggBin, err := os.Executable()
					if err != nil {
						m.statusMsg = "squash: " + err.Error()
						return m, nil
					}
					m.commitCompareSet = nil
					return m.startOp(engine.InteractiveRebase{Branch: branch, Onto: onto, Plan: rp, GGBin: ggBin})
				},
			}
			return m, nil
		}
		if perr != nil {
			// Membership / too-few failures refuse with a note.
			m.statusMsg = "squash: " + perr.Error()
			return m, nil
		}
		ggBin, err := os.Executable()
		if err != nil {
			m.statusMsg = "squash: " + err.Error()
			return m, nil
		}
		m.commitCompareSet = nil // the squash consumes the selection
		return m.startOp(engine.InteractiveRebase{Branch: msg.branch, Onto: msg.onto, Plan: plan, GGBin: ggBin})

	case conflictFileLoadedMsg:
		// In the conflict process, a load failure must return it to Listing (the
		// load is not an op, so no opFinishedMsg would otherwise un-stick Working).
		cp, inProc := m.proc.(*conflictProcess)
		fail := func(reason string) (tea.Model, tea.Cmd) {
			m.statusMsg = reason
			if inProc {
				cp.st = confListing
			}
			return m, nil
		}
		if msg.err != nil {
			return fail("conflict: " + msg.err.Error())
		}
		if textdiff.IsBinary(msg.content) {
			return fail("hunk picker: binary file")
		}
		doc, err := hunkpick.ParseConflict(msg.content)
		if err != nil {
			return fail("hunk picker: " + err.Error())
		}
		if len(doc.Blocks()) == 0 {
			return fail("hunk picker: no conflict regions found")
		}
		if inProc {
			cp.picker = newProcessConflictPicker(msg.path, doc)
			cp.st = confPicking
			return m, nil
		}
		m = m.pushLayer(newConflictPicker(msg.path, doc))
		return m, nil

	case stageHunksLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "stage hunks: " + msg.err.Error()
			return m, nil
		}
		if textdiff.IsBinary(msg.index) || textdiff.IsBinary(msg.work) {
			m.statusMsg = "stage hunks: binary file"
			return m, nil
		}
		doc := hunkpick.FromDiff(msg.index, msg.work)
		doc.SetAll(hunkpick.TakeCurrent) // default: nothing staged
		if len(doc.Blocks()) == 0 {
			m.statusMsg = "stage hunks: nothing to stage"
			return m, nil
		}
		m = m.pushLayer(newStagePicker(msg.path, doc))
		return m, nil

	case clipboardCopiedMsg:
		if msg.err != nil {
			m.statusMsg = "copy failed: " + msg.err.Error()
		} else {
			m.statusMsg = msg.ok
		}
		return m, nil
	}
	return m, nil
}

// handleStageKey toggles staging of the selected Status row: stage if the file
// has any unstaged content (untracked, or an unstaged porcelain byte), else
// unstage. Conflicted files are a no-op here (mark-resolved is F4).
func (m Model) handleStageKey() (tea.Model, tea.Cmd) {
	if !m.canStage() {
		return m, nil
	}
	bi, _ := m.backingIndex(m.focus)
	f := m.status.Files[bi]
	if f.Kind == model.KindUnmerged {
		m.statusMsg = "resolve conflicts first"
		return m, nil
	}
	// Direction is the panel: Files stages, Staged unstages.
	m.running = true
	m.statusMsg = "working…"
	return m, m.stageCmd(engine.Stage{Paths: []string{f.Path}, Unstage: m.focus == panelStaged})
}

// discardTargets resolves what d should discard: the marked file set if any,
// otherwise the cursor row. Conflicted (unmerged) files are dropped. Untracked
// paths go to Remove (git clean); every other tracked path goes to Restore
// (git restore --worktree). n is the total number of targeted paths.
func (m Model) discardTargets() (restore, remove []string, n int) {
	var files []model.FileStatus
	if len(m.fileMarks) > 0 {
		for _, f := range m.status.Files {
			if m.fileMarks[f.Path] {
				files = append(files, f)
			}
		}
	} else if bi, ok := m.backingIndex(panelFiles); ok {
		files = []model.FileStatus{m.status.Files[bi]}
	}
	for _, f := range files {
		switch f.Kind {
		case model.KindUnmerged:
			continue
		case model.KindUntracked:
			remove = append(remove, f.Path)
		default:
			restore = append(restore, f.Path)
		}
	}
	return restore, remove, len(restore) + len(remove)
}

// discardPrompt is the confirmation text for a targeted (d) discard.
func discardPrompt(restore, remove []string, n int) string {
	if n == 1 {
		all := append(append([]string{}, restore...), remove...)
		return "Discard changes to " + all[0] + "? This cannot be undone."
	}
	return fmt.Sprintf("Discard changes to %d files? This cannot be undone.", n)
}

// rememberLeftFocus records the focused panel as ←'s return target when it
// is one of the left-column panels. Called before any focus reassignment.
func (m Model) rememberLeftFocus() Model {
	if m.focus != panelCommits {
		m.lastLeftPanel = m.focus
	}
	return m
}

// focusOrder is the top-to-bottom sequence of focusable panels: the active
// Branches/Worktrees tab (the inactive one is not focusable), then Files (and
// Staged when it fits), then Commits. tab/shift+tab walk this.
// middleTab is the active middle-slot panel, defaulting to Files when unset.
func (m Model) middleTab() panel {
	if m.activeFilesTab == panelFiles || m.activeFilesTab == panelTags {
		return m.activeFilesTab
	}
	return panelFiles
}

// bottomTab is the active bottom-left slot panel, defaulting to Staged when unset.
func (m Model) bottomTab() panel {
	if m.activeBottomTab == panelStaged || m.activeBottomTab == panelReflog {
		return m.activeBottomTab
	}
	return panelStaged
}

// leftColumnPanels returns the left-column panels that exist for the current
// terminal size, independent of any maximize. Staged is present only when the
// normal split is tall enough to show it (the bodyH>=12 branch in layout). It is
// the single source of truth for left-panel reachability and render membership,
// so maximize (which zeroes the non-pinned panels' boxH) can never drop a panel
// out of the tab cycle or leave a degenerate box on screen.
func (m Model) leftColumnPanels() []panel {
	if m.width > 0 && m.width < 40 {
		return nil // narrow: single Commits column, no left column
	}
	ps := []panel{m.activeLeftTab, m.middleTab()}
	bodyH := m.height - 3
	if bodyH < 6 {
		bodyH = 6
	}
	if bodyH >= 12 {
		ps = append(ps, m.bottomTab())
	}
	return ps
}

// canMaximizeLeft reports whether t can pin the focused panel: focus is a small
// left-column panel and no full-screen surface owns the left area. The files
// view replaces the small left panels entirely, so t is inert there; the stash
// view only takes the right column, so Staged stays maximizable.
func (m Model) canMaximizeLeft() bool {
	if m.filesView != nil {
		return false
	}
	return slices.Contains(m.leftColumnPanels(), m.focus)
}

func (m Model) focusOrder() []panel {
	// While a left panel is maximized, focus collapses to that panel and Commits
	// — the other left panels are hidden, so they must not be tab targets.
	if m.leftMaxed {
		return []panel{m.leftMax, panelCommits}
	}
	order := []panel{m.activeLeftTab, m.middleTab()}
	if slices.Contains(m.leftColumnPanels(), m.bottomTab()) { // dropped on a short terminal
		order = append(order, m.bottomTab())
	}
	return append(order, panelCommits)
}

// nextInOrder returns the panel dir steps from cur within order (wrapping). If
// cur is not in order (e.g. focus left on a now-hidden tab), it returns the
// first entry.
func nextInOrder(order []panel, cur panel, dir int) panel {
	for i, p := range order {
		if p == cur {
			n := len(order)
			return order[((i+dir)%n+n)%n]
		}
	}
	return order[0]
}

// leftReturnTarget is where ← lands: the remembered left panel, except a stale
// pointer at the now-inactive Branches/Worktrees tab is redirected to the
// active tab (the one actually visible).
func (m Model) leftReturnTarget() panel {
	if m.leftMaxed { // maximized: the pinned panel is the only left target
		return m.leftMax
	}
	p := m.lastLeftPanel
	if (p == panelBranches || p == panelWorktrees) && p != m.activeLeftTab {
		p = m.activeLeftTab
	}
	if m.layout().boxH[p] <= 0 { // hidden (inactive tab, or Staged on a short terminal)
		return panelFiles
	}
	return p
}

// isFilesPanel reports whether p is one of the two working-tree file panels.
func (m Model) isFilesPanel(p panel) bool { return p == panelFiles || p == panelStaged }

// panelLen returns the number of rows in a panel, for selection clamping.
func (m Model) panelLen(p panel) int {
	idx := m.displayIndices(p)
	return len(idx)
}

// commitScopeLabel describes the Commits feed mode for the panel header.
func (m Model) commitScopeLabel() string {
	switch len(m.commitScopeBranches) {
	case 0:
		return "all"
	case 1:
		return "solo: " + m.commitScopeBranches[0]
	default:
		return fmt.Sprintf("%d branches", len(m.commitScopeBranches))
	}
}

// rebuildCommitGraph recomputes the cached single-line graph cells from
// m.commits. Called whenever m.commits changes (the lane fold needs the whole
// loaded window, so it can't be a per-render computation).
func (m Model) rebuildCommitGraph() Model {
	// Derive the WIP pseudo-rows from the current status first, so the graph plane
	// and every unified length (commitsTotal) stay in lock-step with them.
	m.wipRows = deriveWipRows(m.status)
	cs := make([]commitgraph.Commit, 0, m.commitsTotal())
	// Synthetic WIP nodes, chained Working tree → Staged → HEAD. Each parents to
	// the next wip row, the last to HEAD (m.commits[0]); an empty feed leaves the
	// last wip node parentless (a root). The hash is git-invalid (NUL) so a leak
	// would fail loudly.
	headHash := ""
	if len(m.commits) > 0 {
		headHash = m.commits[0].Hash
	}
	for i, r := range m.wipRows {
		parent := headHash
		if i+1 < len(m.wipRows) {
			parent = wipSyntheticHash(m.wipRows[i+1])
		}
		var parents []string
		if parent != "" {
			parents = []string{parent}
		}
		cs = append(cs, commitgraph.Commit{Hash: wipSyntheticHash(r), Parents: parents})
	}
	for _, c := range m.commits {
		cs = append(cs, commitgraph.Commit{Hash: c.Hash, Parents: c.Parents})
	}
	rows, _ := commitgraph.Lay(cs)
	m.commitGraphRows = make([]string, len(rows))
	m.commitGraphLanes = make([]int, len(rows))
	for i, r := range rows {
		cells := r.Cells
		if i < m.wipCount() { // hollow ◇ node for a pseudo-row, not a real ● commit
			cells = strings.Replace(cells, "●", wipNodeGlyph, 1)
		}
		m.commitGraphRows[i] = cells
		m.commitGraphLanes[i] = r.Lane
	}
	// Keep the horizontal scroll valid against the new plane: paging in older
	// commits is a strict append (newest-first lane assignment leaves existing
	// rows' lanes unchanged), so preserve the position; a genuine scope change
	// shrinks the plane and clampScroll brings an out-of-range offset back in.
	m.commitGraphScroll = m.clampScroll(m.commitGraphScroll)
	return m
}

// maybeLoadMoreCommits returns the model plus a cmd to page in more commits when
// the Commits selection nears the end and no commits filter is active; a nil cmd
// (and the model unchanged) otherwise. The feed owns the "is there more / am I
// already loading" decision. When a page is dispatched it sets commitsLoading so
// the Commits title shows the in-flight indicator until commitsPagedMsg arrives.
func (m Model) maybeLoadMoreCommits() (Model, tea.Cmd) {
	if m.feed == nil {
		return m, nil
	}
	if m.filterTyping && m.filterPanel == panelCommits {
		return m, nil
	}
	if m.filterActive(panelCommits) {
		return m, nil
	}
	if !m.feed.NeedsMore(m.sel[panelCommits]) {
		return m, nil
	}
	m.commitsLoading = true
	return m, m.loadMoreCmd()
}

// reRoot points the model at the repository rooted at path and triggers a full
// reload. switchTarget records where a shell should follow on exit (written to
// --cwd-file by cmd/gg). A fresh span ring is used for the new root; the cmd/gg
// panic dump still references the original repo (acceptable for a debug aid).
func (m Model) reRoot(path string) (tea.Model, tea.Cmd) {
	m.svc = domain.Open(path)
	m.feed = m.svc.CommitFeed()
	m.switchTarget = path
	m.loading = true
	// Drop selections from the old repo so the highlight doesn't land on a
	// surprising row in the newly-loaded panels.
	m.sel = map[panel]int{}
	m.mark = nil      // a mark from the old repo must not re-attach by name in the new one
	m.fileMarks = nil // likewise drop Status file-marks from the old repo
	m.stashView = nil // the new repo has its own stashes
	m.filesView = nil // the new repo has a different commit list
	m.filesStashTag = ""
	m.filesHash = ""
	m.filesCompare = false
	m.compareTag = ""
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

// hscrollStep is the diff scroll-mode horizontal pan distance (columns per
// ←/→), from [ui] hscroll_step; 8 until config loads.
func (m Model) hscrollStep() int {
	if s := m.cfg.UI.HScrollStep; s > 0 {
		return s
	}
	return 8
}
