package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/agentinit"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/observ"
)

// settingsPopup is the generic Settings surface opened with `,`. The
// menu/picker split predates the agent picker's move to the command palette
// (openAgentPicker is now reached only from there, via pickerFromPalette) and
// still holds many other option screens.
type settingsPopup struct {
	popupMax
	picker            bool      // false = menu screen, true = agent picker
	pickerFromPalette bool      // true = picker opened from the command palette → esc returns to base, not the menu
	errorsView        bool      // true = session-errors viewer screen
	errAutoMax        bool      // maximized was set BY the errors view (auto width) → esc restores it
	ratesView         bool      // true = refresh-rates viewer screen
	ratesSel          int       // selected row in the Refresh rates editor
	ratesEditing      bool      // an interval field is open
	ratesField        textfield // the inline numeric editor
	opsHistView       bool      // true = Operations history (branch-version snapshot) editor screen
	opsHistSel        int       // selected row: 0 = Retention, 1 = Recording
	opsHistEditing    bool      // the retention field is open
	opsHistField      textfield // the inline numeric (days) editor
	dets              []agentinit.Detection
	checked           []bool
	toolsView         bool            // true = external-tools wizard screen
	toolRows          []toolWizardRow // detected tool × catalog command rows
	toolChecked       []bool
	sel               int      // selection within the agent picker list
	menuSel           int      // selection within the top-level menu (independent of sel)
	mode              dispMode // text display mode; z cycles (cutoff default)
	hscroll           int      // modeScroll horizontal offset
}

const (
	settingsMenuTools       = "External tools"
	settingsMenuIdentity    = "Identity & profiles"
	settingsMenuPrefixes    = "Branch prefixes"
	settingsMenuHook        = "Worktree post-create hook"
	settingsMenuOpLog       = "Operation log"
	settingsMenuErrors      = "Session errors"
	settingsMenuAutoRefresh = "Auto-refresh"
	settingsMenuRemoteTags  = "Auto remote-tag refresh"
	settingsMenuRates       = "Refresh rates"
	settingsMenuOpsHist     = "Operations history"
	settingsMenuCommitSort  = "Commit sort"
	settingsMenuShowGraph   = "Show graph"
	settingsMenuLanguage    = "Language"
	settingsMenuRepoLoc     = "Repo settings location"
	settingsMenuCommitGraph = "Commit-graph"
)

// settingsMenu is the top-level menu order.
var settingsMenu = []string{settingsMenuTools, settingsMenuIdentity, settingsMenuPrefixes, settingsMenuHook, settingsMenuOpLog, settingsMenuErrors, settingsMenuAutoRefresh, settingsMenuRemoteTags, settingsMenuRates, settingsMenuOpsHist, settingsMenuCommitSort, settingsMenuShowGraph, settingsMenuLanguage, settingsMenuRepoLoc, settingsMenuCommitGraph}

// commitSortModes is the cycle order for the "Commit sort" menu toggle:
// date-order (default; git --date-order, perfect lanes) → plain (fast, git's
// lazy order).
var commitSortModes = []string{"date-order", "plain"}

// settingsMenuTitle translates a menu entry's base label. Each case is a
// literal i18n.T call so the AST catalog scan can extract the keys; the
// consts themselves stay untranslated identity values for the enter-handler
// switch.
func settingsMenuTitle(entry string) string {
	switch entry {
	case settingsMenuTools:
		return i18n.T("External tools")
	case settingsMenuIdentity:
		return i18n.T("Identity & profiles")
	case settingsMenuPrefixes:
		return i18n.T("Branch prefixes")
	case settingsMenuHook:
		return i18n.T("Worktree post-create hook")
	case settingsMenuOpLog:
		return i18n.T("Operation log")
	case settingsMenuErrors:
		return i18n.T("Session errors")
	case settingsMenuAutoRefresh:
		return i18n.T("Auto-refresh")
	case settingsMenuRemoteTags:
		return i18n.T("Auto remote-tag refresh")
	case settingsMenuRates:
		return i18n.T("Refresh rates")
	case settingsMenuOpsHist:
		return i18n.T("Operations history")
	case settingsMenuCommitSort:
		return i18n.T("Commit sort")
	case settingsMenuShowGraph:
		return i18n.T("Show graph")
	case settingsMenuLanguage:
		return i18n.T("Language")
	case settingsMenuRepoLoc:
		return i18n.T("Repo settings location")
	case settingsMenuCommitGraph:
		return i18n.T("Commit-graph")
	}
	return entry
}

// onOff renders a boolean setting state.
func onOff(b bool) string {
	if b {
		return i18n.T("on")
	}
	return i18n.T("off")
}

// settingsMenuLabel renders one menu row: translated title + live state. The
// operation-log row is dynamic: it shows the on/off state and the log
// filename, so the menu both reveals whether logging is enabled and tells the
// user where to find it.
func settingsMenuLabel(m Model, i int) string {
	entry := settingsMenu[i]
	title := settingsMenuTitle(entry)
	switch entry {
	case settingsMenuOpLog:
		path := i18n.T("(no state dir)")
		on := false
		if m.opLog != nil {
			on = m.opLog.on
			if m.opLog.path != "" {
				path = m.opLog.path
			}
		}
		if on {
			return title + ": " + i18n.T("on") + " — " + path
		}
		return title + ": " + i18n.T("off") + " (" + path + ")"
	case settingsMenuErrors:
		path := defaultErrLogPath()
		if path == "" {
			path = i18n.T("(no state dir)")
		}
		n := len(observ.SessionFailures())
		if n == 0 {
			return title + ": " + i18n.T("none") + " — " + path
		}
		return fmt.Sprintf("%s: %d — %s", title, n, path)
	case settingsMenuAutoRefresh:
		return title + ": " + onOff(m.cfg.Refresh.Enabled)
	case settingsMenuRemoteTags:
		return title + ": " + onOff(!m.cfg.Refresh.DisableRemoteTagsAuto)
	case settingsMenuCommitSort:
		return title + ": " + m.commitSort()
	case settingsMenuShowGraph:
		return title + ": " + onOff(m.showGraphConfigured())
	case settingsMenuLanguage:
		return title + ": " + i18n.ActiveName()
	case settingsMenuCommitGraph:
		if !m.repoHealthKnown {
			return title + ": " + i18n.T("(checking…)")
		}
		// The git option is named so the row maps to the config explorer's
		// fetch.writeCommitGraph line — "auto-refresh" alone is unfindable there.
		switch {
		case !m.repoHealth.HasCommitGraph:
			return title + ": " + i18n.T("missing — enter writes + sets fetch.writeCommitGraph")
		case m.repoHealth.WriteCommitGraphValue == "true":
			return title + ": " + i18n.T("present, auto-refresh on (fetch.writeCommitGraph)")
		default:
			return title + ": " + i18n.T("present, auto-refresh off — enter sets fetch.writeCommitGraph")
		}
	}
	return title
}

// showGraphConfigured resolves [ui] show_graph: anything but an explicit "off"
// (including unset / pre-load zero config) means the graph is shown.
func (m Model) showGraphConfigured() bool {
	return m.cfg.UI.ShowGraph != "off"
}

// toggleShowGraph flips the Commits panel between the lane graph and the flat
// list, persisting the choice to the repo .gg.toml so it survives restarts. The
// off state applies exactly what the . menu's "Show as list" does
// (commitListMode); an explicit "on" is written too so any set value is
// remembered per repo.
func (m Model) toggleShowGraph() Model {
	next := "off"
	if !m.showGraphConfigured() {
		next = "on"
	}
	m.cfg.UI.ShowGraph = next
	m.commitListMode = next == "off"
	if m.repoConfigPath == "" {
		m.statusMsg = i18n.T("show graph → %s (not saved: no repo config path)", next)
	} else if err := config.SetShowGraph(m.repoConfigPath, next); err != nil {
		m.statusMsg = i18n.T("show graph → %s (not saved: %s)", next, err.Error())
	} else {
		m.statusMsg = i18n.T("show graph: %s", next)
	}
	return m
}

// commitSort returns the configured commit-sort mode, defaulting to "date-order"
// before config loads or when unset.
func (m Model) commitSort() string {
	if m.cfg.UI.CommitSort == "" {
		return "date-order"
	}
	return m.cfg.UI.CommitSort
}

// cycleCommitSort advances the commit-sort mode (date-order ↔ plain), persists
// it to the repo .gg.toml, swaps the feed's page strategy, and re-walks the feed
// so the Commits panel + graph redraw in the new order.
func (m Model) cycleCommitSort() (Model, tea.Cmd) {
	cur := m.commitSort()
	next := commitSortModes[0]
	for i, mode := range commitSortModes {
		if mode == cur {
			next = commitSortModes[(i+1)%len(commitSortModes)]
			break
		}
	}
	m.cfg.UI.CommitSort = next
	if m.repoConfigPath == "" {
		m.statusMsg = i18n.T("commit sort → %s (not saved: no repo config path)", next)
	} else if err := config.SetCommitSort(m.repoConfigPath, next); err != nil {
		m.statusMsg = i18n.T("commit sort → %s (not saved: %s)", next, err.Error())
	} else {
		m.statusMsg = i18n.T("commit sort: %s — reloading commits…", next)
	}
	m.feed.SetSortMode(next)
	return m.reloadSourcesCmd([]sourceKey{srcFeed}, true, false)
}

// toggleOpLog flips the operation log, persisting the choice to the global
// config so it survives restarts (the user's persist-to-config choice).
func (m Model) toggleOpLog() Model {
	if m.opLog == nil {
		m.opLog = newOpLog()
	}
	var err error
	if m.opLog.on {
		err = m.opLog.disable()
	} else {
		err = m.opLog.enable()
	}
	if err != nil {
		m.statusMsg = i18n.T("operation log: %s", err.Error())
		return m
	}
	m.cfg.Debug.LogOperations = m.opLog.on // keep the in-memory view in sync
	if perr := config.SetGlobalDebugLogOperations(config.DefaultGlobalPath(), m.opLog.on); perr != nil {
		m.statusMsg = i18n.T("operation log toggled but not saved: %s", perr.Error())
		return m
	}
	if m.opLog.on {
		m.statusMsg = i18n.T("operation log on — %s", m.opLog.path)
	} else {
		m.statusMsg = i18n.T("operation log off")
	}
	return m
}

// toggleAutoRefresh flips the master background-refresh switch, persisting to
// the global config so it survives restarts (mirrors toggleOpLog).
func (m Model) toggleAutoRefresh() Model {
	want := !m.cfg.Refresh.Enabled
	m.cfg.Refresh.Enabled = want // in-memory flip takes effect on the next heartbeat tick
	if want {
		// Seed lastRun=now so enabling does not burst every source at once on the
		// next tick — first auto-fire is one interval out (same as startup seeding).
		now := time.Now()
		for _, it := range scheduledItems {
			m.refreshLastRun[it] = now
		}
	}
	if err := config.SetGlobalRefreshEnabled(config.DefaultGlobalPath(), want); err != nil {
		m.statusMsg = i18n.T("auto-refresh toggled but not saved: %s", err.Error())
		return m
	}
	if want {
		m.statusMsg = i18n.T("auto-refresh on (per-source intervals from [refresh])")
	} else {
		m.statusMsg = i18n.T("auto-refresh off")
	}
	return m
}

// toggleAutoRemoteTags flips the auto remote-tag refresh switch (inverted flag),
// persisting to the global config so it survives restarts (mirrors toggleAutoRefresh).
func (m Model) toggleAutoRemoteTags() Model {
	wantDisabled := !m.cfg.Refresh.DisableRemoteTagsAuto
	m.cfg.Refresh.DisableRemoteTagsAuto = wantDisabled
	if err := config.SetGlobalDisableRemoteTagsAuto(config.DefaultGlobalPath(), wantDisabled); err != nil {
		m.statusMsg = i18n.T("auto remote-tag refresh toggled but not saved: %s", err.Error())
		return m
	}
	if wantDisabled {
		m.statusMsg = i18n.T("auto remote-tag refresh off")
	} else {
		m.statusMsg = i18n.T("auto remote-tag refresh on")
	}
	return m
}

// openSettings opens the menu screen and re-reads repo health so the
// Commit-graph row's label reflects the current state, not a stale snapshot
// from startup or the last repo switch.
func (m Model) openSettings() (Model, tea.Cmd) {
	m = m.pushLayer(&settingsPopup{})
	return m, m.repoHealthCmd(m.noticeGen)
}

// openAgentPicker populates the picker from a fresh detection pass. The
// checkbox defaults encode the core rule: already-installed targets start
// checked (apply = refresh); new targets start unchecked (explicit opt-in).
func (m Model) openAgentPicker() Model {
	p := layerOf[*settingsPopup](m)
	p.dets = agentinit.Detect(m.currentWorktree, m.initHomeDir)
	p.checked = make([]bool, len(p.dets))
	for i, d := range p.dets {
		p.checked[i] = d.Status.Checked()
	}
	p.sel = 0
	p.picker = true
	return m
}

// agentStatusDisplay translates an agentinit.Status label for the
// agent-skills picker row (the sourceDisplayName pattern: the enum's
// String() lives in another package and stays English/identity there, so
// this render-time switch — keyed on the exported constants, not the string
// — is the translation point).
func agentStatusDisplay(s agentinit.Status) string {
	switch s {
	case agentinit.StatusNew:
		return i18n.T("new")
	case agentinit.StatusOutdated:
		return i18n.T("outdated")
	case agentinit.StatusUpToDate:
		return i18n.T("up to date")
	}
	return s.String()
}

// update handles all keys while the settings popup is open.
func (p *settingsPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		if p.errorsView {
			p.errorsView = false
			// Undo only an errors-view auto-maximize — the small menu must not
			// inherit a near-fullscreen box it never asked for.
			if p.errAutoMax {
				p.maximized = false
				p.errAutoMax = false
			}
			return m, nil
		}
		if p.ratesView && p.ratesEditing {
			p.ratesEditing = false
			return m, nil
		}
		if p.ratesView && !p.ratesEditing {
			p.ratesView = false
			return m, nil
		}
		if p.opsHistView && p.opsHistEditing {
			p.opsHistEditing = false
			return m, nil
		}
		if p.opsHistView && !p.opsHistEditing {
			p.opsHistView = false
			return m, nil
		}
		if p.picker {
			if p.pickerFromPalette { // launched from the palette → esc backs out to base
				m = m.popLayer()
				return m, nil
			}
			// Retained primitive: openAgentPicker's only production caller (the
			// palette) sets pickerFromPalette, so this menu-return branch is reached
			// only by tests today — kept so a future , menu row could reopen the picker.
			p.picker = false
			return m, nil
		}
		if p.toolsView {
			p.toolsView = false
			return m, nil
		}
		m = m.popLayer()
		return m, nil
	}
	switch msg.String() { // display-mode keys apply on both screens
	case "z":
		p.mode = p.mode.next()
		p.hscroll = 0
		return m, nil
	case "shift+left":
		if p.mode == modeScroll && p.hscroll > 0 {
			if p.hscroll -= m.hscrollStep(); p.hscroll < 0 {
				p.hscroll = 0
			}
		}
		return m, nil
	case "shift+right":
		if p.mode == modeScroll {
			p.hscroll += m.hscrollStep()
		}
		return m, nil
	}
	if !p.picker && !p.errorsView && !p.ratesView && !p.toolsView && !p.opsHistView {
		switch msg.Type {
		case tea.KeyUp:
			// Wrap: up on the first option lands on the last.
			p.menuSel = (p.menuSel - 1 + len(settingsMenu)) % len(settingsMenu)
			return m, nil
		case tea.KeyDown:
			// Wrap: down on the last option lands on the first.
			p.menuSel = (p.menuSel + 1) % len(settingsMenu)
			return m, nil
		case tea.KeyEnter:
			switch settingsMenu[p.menuSel] {
			case settingsMenuTools:
				return m.openToolsWizard(), nil
			case settingsMenuIdentity:
				return m.openIdentityView()
			case settingsMenuPrefixes:
				return m.openPrefixSettings()
			case settingsMenuHook:
				return m.openHookEditor(), nil
			case settingsMenuOpLog:
				return m.toggleOpLog(), nil // stays open so the state flip is visible
			case settingsMenuAutoRefresh:
				return m.toggleAutoRefresh(), nil // stays open so the state flip is visible
			case settingsMenuRemoteTags:
				return m.toggleAutoRemoteTags(), nil // stays open so the flip is visible
			case settingsMenuCommitSort:
				return m.cycleCommitSort() // stays open; re-walks the feed in the new order
			case settingsMenuShowGraph:
				m = m.toggleShowGraph() // stays open so the state flip is visible
				// A related option may be worth reconsidering now (e.g. commit
				// sort buys nothing with the graph hidden) — one follow-up, max.
				return m.maybeRelatedPrompt(settingShowGraph, m.cfg.UI.ShowGraph)
			case settingsMenuLanguage:
				return m.openLanguagePicker()
			case settingsMenuRepoLoc:
				return m.openRepoConfigLocation(), nil
			case settingsMenuCommitGraph:
				if !m.repoHealthKnown {
					m.statusMsg = i18n.T("still checking the repo — try again in a moment")
					return m, nil
				}
				if m.repoHealth.HasCommitGraph && m.repoHealth.WriteCommitGraphValue == "true" {
					m.statusMsg = i18n.T("commit-graph present and auto-refresh already on")
					return m, nil
				}
				if m.running {
					return m, nil // an op is already in flight
				}
				// Same code path as the notice's "write + keep fresh" action.
				return m.startCommitGraphWriteAndEnable()
			case settingsMenuRates:
				p.ratesView = true
				p.ratesSel = 0
				p.ratesEditing = false
				p.sel = 0
				p.hscroll = 0
				return m, nil
			case settingsMenuOpsHist:
				p.opsHistView = true
				p.opsHistSel = 0
				p.opsHistEditing = false
				p.hscroll = 0
				return m, nil
			case settingsMenuErrors:
				p.errorsView = true
				p.sel = 0
				p.hscroll = 0
				// Auto width: git stderr rows are long, and even the wide box
				// truncates them. Open maximized when the widest row demands it
				// (the pair-op autoMaxForContent precedent — decided once here,
				// never fighting a later ctrl+t), but only when the maximize is
				// ours to grant: a pre-existing manual ctrl+t stays the user's.
				if !p.maximized {
					content := 0
					for _, e := range observ.SessionFailures() {
						l := lipgloss.Width(fmt.Sprintf("> %s  %s — %s",
							e.Time.Format("15:04:05"), e.Source, e.Detail))
						if l > content {
							content = l
						}
					}
					w, _ := m.overlayDims()
					if autoMaxForContentAt(w, popupWideInnerWidth(w), content) {
						p.maximized = true
						p.errAutoMax = true
					}
				}
				return m, nil
			}
			return m, nil
		}
		return m, nil
	}
	if p.errorsView {
		fs := observ.SessionFailures()
		switch msg.Type {
		case tea.KeyUp:
			if p.sel > 0 {
				p.sel--
			}
		case tea.KeyDown:
			if p.sel < len(fs)-1 {
				p.sel++
			}
		}
		return m, nil
	}
	if p.ratesView {
		if p.ratesEditing {
			switch msg.Type {
			case tea.KeyEnter:
				secs := 0
				if v := strings.TrimSpace(p.ratesField.Value()); v != "" {
					if n, err := strconv.Atoi(v); err == nil {
						secs = n
					}
				}
				m = m.saveRefreshInterval(scheduledItems[p.ratesSel], secs)
				p.ratesEditing = false
				return m, nil
			case tea.KeyRunes:
				digits := true
				for _, r := range msg.Runes {
					if r < '0' || r > '9' {
						digits = false
					}
				}
				if digits {
					(&p.ratesField).insert(msg.Runes)
				}
				return m, nil
			default:
				(&p.ratesField).HandleEditKey(msg)
				return m, nil
			}
		}
		// space and w both toggle the focused row's file-watch checkbox (no-op on a
		// non-watch-capable row). space arrives as KeySpace (KeyRunes{' '} is
		// normalized to KeySpace at the top of the key handler, incl. on Windows).
		if msg.String() == "w" || msg.Type == tea.KeySpace {
			m2, cmd := m.toggleRefreshWatch(scheduledItems[p.ratesSel])
			return m2, cmd
		}
		switch msg.Type {
		case tea.KeyUp:
			if p.ratesSel > 0 {
				p.ratesSel--
			}
		case tea.KeyDown:
			if p.ratesSel < len(scheduledItems)-1 {
				p.ratesSel++
			}
		case tea.KeyEnter:
			cur := refreshIntervalFor(m.cfg.Refresh, scheduledItems[p.ratesSel])
			start := ""
			if cur > 0 {
				start = strconv.Itoa(cur)
			}
			p.ratesField = newTextField(start)
			p.ratesEditing = true
		}
		return m, nil
	}
	if p.opsHistView {
		if p.opsHistEditing {
			switch msg.Type {
			case tea.KeyEnter:
				days := 0
				if v := strings.TrimSpace(p.opsHistField.Value()); v != "" {
					if n, err := strconv.Atoi(v); err == nil {
						days = n
					}
				}
				m = m.saveVersionsRetention(days)
				p.opsHistEditing = false
				return m, nil
			case tea.KeyRunes:
				ok := true
				for _, r := range msg.Runes {
					if (r < '0' || r > '9') && r != '-' {
						ok = false
					}
				}
				if ok {
					(&p.opsHistField).insert(msg.Runes)
				}
				return m, nil
			default:
				(&p.opsHistField).HandleEditKey(msg)
				return m, nil
			}
		}
		switch msg.Type {
		case tea.KeyUp:
			if p.opsHistSel > 0 {
				p.opsHistSel--
			}
		case tea.KeyDown:
			if p.opsHistSel < 1 { // two rows: 0 Retention, 1 Recording
				p.opsHistSel++
			}
		case tea.KeyEnter:
			switch p.opsHistSel {
			case 0: // Retention
				start := ""
				if m.cfg.Versions.MaxAgeDays != 0 {
					start = strconv.Itoa(m.cfg.Versions.MaxAgeDays)
				}
				p.opsHistField = newTextField(start)
				p.opsHistEditing = true
			case 1: // Recording
				m = m.toggleVersionsRecording()
			}
		}
		return m, nil
	}
	if p.toolsView {
		switch msg.Type {
		case tea.KeyUp:
			if p.sel > 0 {
				p.sel--
			}
		case tea.KeyDown:
			if p.sel < len(p.toolRows)-1 {
				p.sel++
			}
		case tea.KeySpace:
			if p.sel >= 0 && p.sel < len(p.toolChecked) {
				p.toolChecked[p.sel] = !p.toolChecked[p.sel]
			}
		case tea.KeyEnter:
			m2, n, err := m.applyToolsWizard(p.toolRows, p.toolChecked, config.DefaultGlobalPath())
			p.toolsView = false
			if err != nil {
				m2.statusMsg = i18n.T("external tools: %s", err.Error())
				return m2, nil
			}
			if n == 0 {
				m2.statusMsg = i18n.T("external tools: nothing to write (already configured or unchecked)")
				return m2, nil
			}
			m2.statusMsg = i18n.T("external tools: %d command(s) written to %s", n, config.DefaultGlobalPath())
			return m2, nil
		}
		return m, nil
	}
	switch msg.Type {
	case tea.KeyUp:
		if p.sel > 0 {
			p.sel--
		}
	case tea.KeyDown:
		if p.sel < len(p.dets)-1 {
			p.sel++
		}
	case tea.KeySpace:
		if p.sel >= 0 && p.sel < len(p.checked) {
			p.checked[p.sel] = !p.checked[p.sel]
		}
	case tea.KeyEnter:
		installed, refreshed, failed := 0, 0, 0
		for i, d := range p.dets {
			if !p.checked[i] {
				continue
			}
			if err := agentinit.Install(d); err != nil {
				failed++
				continue
			}
			if d.Status == agentinit.StatusNew {
				installed++
			} else {
				refreshed++
			}
		}
		m = m.popLayer()
		m.statusMsg = i18n.T("agent skills: %d installed, %d refreshed", installed, refreshed)
		if failed > 0 {
			m.statusMsg += i18n.T(", %d failed", failed)
		}
		return m, nil
	}
	return m, nil
}

// render composites the popup over the layer beneath.
func (p *settingsPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// box draws whichever screen is active (modal box only).
func (p *settingsPopup) box(m Model) string {
	w, termH := m.overlayDims()
	inner := popupInnerWidth(w)
	// The errors viewer holds long, path-heavy rows (git stderr, the errors.log
	// location), so it scales wide like the bookmark/shelf switchers — most
	// errors and the log path then fit on one line instead of wrapping ugly.
	// The Operations history viewer's footer hints (especially Russian translations)
	// also need the wide width to avoid overflow.
	if p.errorsView || p.ratesView || p.opsHistView {
		inner = popupWideInnerWidth(w)
	}
	// The External-tools wizard goes full-screen (live feedback: the standard
	// popup was too narrow — the command preview wrapped mid-word and the
	// background bled around the box's edges). See popupFullInnerWidth.
	if p.toolsView {
		inner = popupFullInnerWidth(w)
	}
	// ctrl+t widens whichever screen is active to near-fullscreen. A no-op for
	// toolsView, which is already full-width by design.
	if p.maximized {
		inner = popupFullInnerWidth(w)
	}
	textW := popupTextWidth(inner)
	var b strings.Builder
	if p.errorsView {
		b.WriteString(i18n.T("Session errors") + "\n\n")
		fs := observ.SessionFailures()
		anyTrunc := false
		if len(fs) == 0 {
			b.WriteString("  " + i18n.T("no errors this session") + "\n")
		} else {
			wr := make([]winRow, len(fs))
			for i, e := range fs {
				prefix := "  "
				var st lipgloss.Style
				if i == p.sel {
					prefix, st = "> ", selectedRow
				}
				wr[i] = winRow{
					text:  fmt.Sprintf("%s%s  %s — %s", prefix, e.Time.Format("15:04:05"), e.Source, e.Detail),
					style: st,
				}
				if rowTruncated(wr[i].text, textW) {
					anyTrunc = true
				}
			}
			// Height budget: in wrap mode a single long entry expands to several
			// display lines, so size the viewport to the wrapped line count
			// (capped to keep the popup on-screen) — not the entry count, which
			// would leave wrap no vertical room and make z look like a no-op.
			// wrapContentLines measures with the same wrapAlign hang indent the
			// window renders with.
			_, termH := m.overlayDims()
			capRows := termH - 12
			if capRows < 3 {
				capRows = 3
			}
			o := winOpts{w: textW, mode: p.mode, anchor: p.sel, hscroll: p.hscroll, wrapAlign: true}
			o.h = wrapContentLines(wr, o, capRows)
			for _, line := range renderWindow(wr, o) {
				b.WriteString(line + "\n")
			}
		}
		// Wrap the path so its basename stays visible on a narrow popup; a raw
		// line would be truncated by popupBox and the user could never see where
		// errors.log lives.
		if path := defaultErrLogPath(); path != "" {
			b.WriteString("\n")
			for _, seg := range wrapWidth(i18n.T("full history: %s", path), textW, 1<<20) {
				b.WriteString(seg + "\n")
			}
		}
		// Advertise z only when it does something: an entry too long to fit (in
		// any mode) is what wrap/scroll reveal. Otherwise the hint is a lie.
		if anyTrunc {
			b.WriteString("\n" + i18n.T("[z] mode") + "  " + i18n.T("[esc] back"))
		} else {
			b.WriteString("\n" + i18n.T("[esc] back"))
		}
	} else if p.ratesView {
		b.WriteString(i18n.T("Refresh rates") + "\n\n")
		if !m.cfg.Refresh.Enabled {
			b.WriteString("  " + i18n.T("auto-refresh is OFF — enable it in Settings → Auto-refresh") + "\n\n")
		}
		// Column header. "file-watch" labels the [x]/[ ] checkbox column so it is not
		// an unexplained box; the legend below the table says what it means. Header
		// words are translated; padCell (not %-Ns, which pads by byte count) keeps
		// the columns aligned when a translated word is CJK (rune width != byte
		// width — see padCell's doc comment).
		b.WriteString(fmt.Sprintf("  %s %s %s %s\n",
			padCell(i18n.T("window"), 11), padCell(i18n.T("file-watch"), 11),
			padCell(i18n.T("refresh"), 16), i18n.T("avg read")))
		for i, it := range scheduledItems {
			// Row NAMES are [refresh] config keys (what a user would type in
			// .gg.toml), not display prose, so they stay English/untranslated —
			// unlike sourceDisplayName's translated status-line names.
			name := "fetch"
			if it.isRemoteTags {
				name = "remote_tags"
			} else if !it.isFetch {
				name = sourceNames[it.source]
			}
			prefix := "  "
			if i == p.ratesSel {
				prefix = "> "
			}
			var valCell string
			if p.ratesEditing && i == p.ratesSel {
				valCell = p.ratesField.View(true) + "s"
			} else if watchEligible(it) && watchOn(m.cfg.Refresh, it) {
				if m.watchSupported {
					valCell = i18n.T("watch")
				} else {
					// drvfs: watch unavailable → falls back to the interval
					secs, on := scheduledInterval(m.cfg.Refresh, it)
					if on {
						valCell = i18n.T("watch (9p→%ds)", secs)
					} else {
						valCell = i18n.T("watch (9p→off)")
					}
				}
			} else {
				secs, on := scheduledInterval(m.cfg.Refresh, it)
				if on {
					valCell = i18n.T("every %ds", secs)
				} else {
					valCell = i18n.T("off")
				}
			}
			// avg stat
			avgStr := "—"
			if s := m.refreshDur[it]; len(s) > 0 {
				avg := meanDuration(s)
				if avg < time.Second {
					avgStr = fmt.Sprintf("%dms (%d)", avg.Milliseconds(), len(s))
				} else {
					avgStr = fmt.Sprintf("%.1fs (%d)", avg.Seconds(), len(s))
				}
			}
			// File-watch checkbox: shown only for watch-capable rows so the user can
			// see at a glance which sources support file-watch and whether it is on
			// (toggle with w). Non-eligible rows (status/fetch/tags/feed) keep a blank
			// cell so the columns stay aligned.
			watchBox := ""
			if watchEligible(it) {
				if watchOn(m.cfg.Refresh, it) {
					watchBox = "[x]"
				} else {
					watchBox = "[ ]"
				}
			}
			b.WriteString(fmt.Sprintf("%s%-11s %-11s %s %s\n", prefix, name, watchBox, padCell(valCell, 16), avgStr))
		}
		if p.ratesEditing {
			b.WriteString("\n" + i18n.T("[0-9] edit  [enter] save  [esc] cancel   (0 = off)"))
		} else {
			b.WriteString("\n" + i18n.T("file-watch = auto-detect .git changes instantly (else poll on the interval)"))
			b.WriteString("\n" + i18n.T("[↑/↓] select  [enter] edit interval  [space]/[w] file-watch  [esc] back"))
		}
	} else if p.opsHistView {
		b.WriteString(i18n.T("Operations history") + "\n\n")
		labelW := maxLabelWidth(9, i18n.T("Retention"), i18n.T("Recording"))
		retention := i18n.T("keep forever")
		if m.cfg.Versions.MaxAgeDays > 0 {
			// Two-key singular/plural convention (the push-tip-tags pattern):
			// "1 days" would otherwise be grammatically wrong in English.
			retention = i18n.T("%d days", m.cfg.Versions.MaxAgeDays)
			if m.cfg.Versions.MaxAgeDays == 1 {
				retention = i18n.T("%d day", m.cfg.Versions.MaxAgeDays)
			}
		}
		retentionCell := retention
		if p.opsHistEditing && p.opsHistSel == 0 {
			retentionCell = p.opsHistField.View(true)
		}
		rows := []string{
			padCell(i18n.T("Retention"), labelW) + " " + retentionCell,
			padCell(i18n.T("Recording"), labelW) + " " + onOff(!m.cfg.Versions.Disabled),
		}
		for i, row := range rows {
			prefix := "  "
			if i == p.opsHistSel {
				prefix = "> "
			}
			b.WriteString(prefix + row + "\n")
		}
		if p.opsHistEditing {
			b.WriteString("\n" + i18n.T("[0-9/-] edit  [enter] save  [esc] cancel   (-1 = keep forever)"))
		} else {
			b.WriteString("\n" + i18n.T("[↑/↓] select  [enter] edit/toggle  [esc] back"))
		}
	} else if p.toolsView {
		// hint is computed up front (not just written at the end) because the
		// command-preview height budget below needs its line count to know how
		// much room is actually left over.
		hintParts := []string{i18n.T("[space] toggle"), i18n.T("[enter] write to global config"), i18n.T("[z] mode"), i18n.T("[esc] back")}
		hintLines := wrapParts(hintParts, textW, "  ")

		b.WriteString(i18n.T("External tools — detected") + "\n\n")
		if len(p.toolRows) == 0 {
			b.WriteString("  " + i18n.T("no known tools detected on this machine (looked for: claude, junie, meld)") + "\n")
		} else {
			wr := make([]winRow, len(p.toolRows))
			for i, row := range p.toolRows {
				prefix := "  "
				var st lipgloss.Style
				if i == p.sel {
					prefix, st = "> ", selectedRow
				}
				box := "[ ]"
				if p.toolChecked[i] {
					box = "[x]"
				}
				base := fmt.Sprintf("%s%s %s — %s: %s", prefix, box, row.det.Tool.Label, row.tmpl.Category, row.tmpl.Name)
				text, suffix := base, ""
				var deco rowDecorator
				if row.existing {
					suffix = " " + i18n.T("(configured)")
					text = base + suffix
					deco = toolConfiguredSuffixDecorator(lipgloss.Width(base), lipgloss.Width(suffix))
				}
				wr[i] = winRow{text: text, style: st, decorate: deco}
			}
			// Layout budget. The detail block (destination + command preview)
			// gets a FIXED height — the tallest any row needs (toolDetailHeights)
			// — reserved BEFORE the list, for two live-feedback bugs: on a big
			// terminal a per-selection preview height made the box visibly
			// grow/shrink while scrolling the list, and on a small terminal the
			// list was sized first (termH-10) so the preview and footer fell
			// past termH, where overlayCenter silently drops rows. The LIST is
			// what shrinks and scrolls; when both are hungry the two split the
			// leftover about evenly so neither collapses.
			const modalChrome = 4 // DoubleBorder (2 lines) + vertical Padding(1,2) (2 lines)
			const margin = 2      // breathing room + popupBox's own trailing blank line
			destH, cmdH := toolDetailHeights(p.toolRows, textW)
			overhead := 2 /* title+blank */ + 1 /* blank before detail */ + destH + 1 /* blank before hint */ + len(hintLines) + modalChrome + margin
			avail := termH - overhead
			minList := 3
			if len(p.toolRows) < minList {
				minList = len(p.toolRows)
			}
			listWant := len(p.toolRows)
			if listWant > avail/2 {
				listWant = avail / 2
			}
			if listWant < minList {
				listWant = minList
			}
			previewH := cmdH
			if previewH > avail-listWant {
				previewH = avail - listWant
			}
			if previewH < 1 {
				previewH = 1
			}
			listH := avail - previewH
			if listH > len(p.toolRows) {
				listH = len(p.toolRows)
			}
			if listH < minList {
				listH = minList
			}
			bodyLines := renderWindow(wr, winOpts{w: textW, h: listH, mode: p.mode, anchor: p.sel, hscroll: p.hscroll, wrapAlign: true})
			for _, line := range bodyLines {
				b.WriteString(line + "\n")
			}
			// Focused-row preview: the row list alone shows nothing about what
			// [enter] will actually write (destination file, generated command),
			// so the user has no way to see what they're consenting to. This
			// mirrors toolConfiguredSuffixDecorator's dim style; it recomputes
			// on every frame (GenerateCommand is a cheap string replace — no
			// caching). Both areas are padded to their fixed heights (destH /
			// previewH) so the box height cannot change with the selection.
			if p.sel >= 0 && p.sel < len(p.toolRows) {
				row := p.toolRows[p.sel]
				b.WriteString("\n")
				var destLines []string
				if row.existing {
					destLines = wrapWidth(i18n.T("already configured — skipped on apply"), textW, 1<<20)
				} else {
					destLines = wrapWidth(i18n.T("writes to: %s", config.DefaultGlobalPath()), textW, 1<<20)
				}
				for _, seg := range destLines {
					b.WriteString(dimRowStyle.Render(seg) + "\n")
				}
				for i := len(destLines); i < destH; i++ {
					b.WriteString("\n")
				}
				// Split on "\n" first (a multi-line catalog command, e.g. the
				// Claude template's backslash continuations, must not have its
				// embedded newlines absorbed into one wrapped segment), then
				// word-wrap each raw line on its own so long flags/tokens
				// never split mid-word (the live-feedback bug), then cap the
				// flattened total at the fixed preview height.
				cmd := exttool.GenerateCommand(row.tmpl, row.det.Bin)
				var cmdLines []string
				for _, ln := range strings.Split(cmd, "\n") {
					cmdLines = append(cmdLines, wrapWords(ln, textW)...)
				}
				if len(cmdLines) > previewH {
					keep := previewH - 1
					if keep < 0 {
						keep = 0
					}
					cmdLines = append(append([]string{}, cmdLines[:keep]...), "…")
				}
				for _, seg := range cmdLines {
					b.WriteString(dimRowStyle.Render(seg) + "\n")
				}
				for i := len(cmdLines); i < previewH; i++ {
					b.WriteString("\n")
				}
			}
		}
		b.WriteString("\n" + strings.Join(hintLines, "\n"))
	} else if !p.picker {
		b.WriteString(i18n.T("Settings") + "\n\n")
		wr := make([]winRow, len(settingsMenu))
		for i := range settingsMenu {
			prefix := "  "
			var st lipgloss.Style
			if i == p.menuSel {
				prefix, st = "> ", selectedRow
			}
			wr[i] = winRow{text: prefix + settingsMenuLabel(m, i), style: st}
		}
		// Same selected-row highlight as the . action menu (winRow + selectedRow).
		for _, line := range renderWindow(wr, winOpts{w: textW, h: len(settingsMenu), mode: p.mode, anchor: p.menuSel, hscroll: p.hscroll, wrapAlign: true}) {
			b.WriteString(line + "\n")
		}
		b.WriteString("\n" + i18n.T("[↑/↓] select  [enter] open/toggle  [esc] close"))
	} else {
		b.WriteString(i18n.T("Set up agent skills") + "\n\n")
		if len(p.dets) == 0 {
			b.WriteString("  " + i18n.T("no supported agents detected") + "\n")
		} else {
			wr := make([]winRow, len(p.dets))
			for i, d := range p.dets {
				prefix := "  "
				var st lipgloss.Style
				if i == p.sel {
					prefix, st = "> ", selectedRow
				}
				box := "[ ]"
				if p.checked[i] {
					box = "[x]"
				}
				wr[i] = winRow{text: fmt.Sprintf("%s%s %s — %s", prefix, box, d.Agent.Label, agentStatusDisplay(d.Status)), style: st}
			}
			h := len(p.dets)
			capRows := popupResolveRowCap(p.maximized, termH, 12)
			if h > capRows {
				h = capRows
			}
			for _, line := range renderWindow(wr, winOpts{w: textW, h: h, mode: p.mode, anchor: p.sel, hscroll: p.hscroll, wrapAlign: true}) {
				b.WriteString(line + "\n")
			}
		}
		b.WriteString("\n" + strings.Join([]string{i18n.T("[space] toggle"), i18n.T("[enter] apply"), i18n.T("[z] mode"), i18n.T("[esc] back")}, "  "))
	}
	return popupBox(inner, strings.TrimRight(b.String(), "\n"))
}
