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
	"github.com/homeend/gigagit/internal/observ"
)

// settingsPopup is the generic Settings surface opened with `,`. v1 has a
// single menu entry (agent-skill setup); the menu/picker split exists so
// future options have a home.
type settingsPopup struct {
	picker       bool      // false = menu screen, true = agent picker
	errorsView   bool      // true = session-errors viewer screen
	ratesView    bool      // true = refresh-rates viewer screen
	ratesSel     int       // selected row in the Refresh rates editor
	ratesEditing bool      // an interval field is open
	ratesField   textfield // the inline numeric editor
	dets         []agentinit.Detection
	checked      []bool
	sel          int      // selection within the agent picker list
	menuSel      int      // selection within the top-level menu (independent of sel)
	mode         dispMode // text display mode; z cycles (cutoff default)
	hscroll      int      // modeScroll horizontal offset
}

const (
	settingsMenuAgents      = "Set up agent skills (using-gg)"
	settingsMenuIdentity    = "Identity & profiles"
	settingsMenuPrefixes    = "Branch prefixes"
	settingsMenuHook        = "Worktree post-create hook"
	settingsMenuOpLog       = "Operation log"
	settingsMenuErrors      = "Session errors"
	settingsMenuAutoRefresh = "Auto-refresh"
	settingsMenuRemoteTags  = "Auto remote-tag refresh"
	settingsMenuRates       = "Refresh rates"
	settingsMenuCommitSort  = "Commit sort"
	settingsMenuShowGraph   = "Show graph"
	settingsMenuRepoLoc     = "Repo settings location"
	settingsMenuCommitGraph = "Commit-graph"
	settingsMenuGitConfig   = "Git config explorer"
)

// settingsMenu is the top-level menu order.
var settingsMenu = []string{settingsMenuAgents, settingsMenuIdentity, settingsMenuPrefixes, settingsMenuHook, settingsMenuOpLog, settingsMenuErrors, settingsMenuAutoRefresh, settingsMenuRemoteTags, settingsMenuRates, settingsMenuCommitSort, settingsMenuShowGraph, settingsMenuRepoLoc, settingsMenuCommitGraph, settingsMenuGitConfig}

// commitSortModes is the cycle order for the "Commit sort" menu toggle:
// date-order (default; git --date-order, perfect lanes) → plain (fast, git's
// lazy order).
var commitSortModes = []string{"date-order", "plain"}

// settingsMenuLabel renders one menu row. The operation-log row is dynamic: it
// shows the on/off state and the log filename, so the menu both reveals whether
// logging is enabled and tells the user where to find it.
func settingsMenuLabel(m Model, i int) string {
	if settingsMenu[i] == settingsMenuOpLog {
		path := "(no state dir)"
		on := false
		if m.opLog != nil {
			on = m.opLog.on
			if m.opLog.path != "" {
				path = m.opLog.path
			}
		}
		if on {
			return settingsMenuOpLog + ": on — " + path
		}
		return settingsMenuOpLog + ": off (" + path + ")"
	}
	if settingsMenu[i] == settingsMenuErrors {
		path := defaultErrLogPath()
		if path == "" {
			path = "(no state dir)"
		}
		n := len(observ.SessionFailures())
		if n == 0 {
			return settingsMenuErrors + ": none — " + path
		}
		return fmt.Sprintf("%s: %d — %s", settingsMenuErrors, n, path)
	}
	if settingsMenu[i] == settingsMenuAutoRefresh {
		if m.cfg.Refresh.Enabled {
			return settingsMenuAutoRefresh + ": on"
		}
		return settingsMenuAutoRefresh + ": off"
	}
	if settingsMenu[i] == settingsMenuRemoteTags {
		if m.cfg.Refresh.DisableRemoteTagsAuto {
			return settingsMenuRemoteTags + ": off"
		}
		return settingsMenuRemoteTags + ": on"
	}
	if settingsMenu[i] == settingsMenuCommitSort {
		return settingsMenuCommitSort + ": " + m.commitSort()
	}
	if settingsMenu[i] == settingsMenuShowGraph {
		if m.showGraphConfigured() {
			return settingsMenuShowGraph + ": on"
		}
		return settingsMenuShowGraph + ": off"
	}
	if settingsMenu[i] == settingsMenuCommitGraph {
		if !m.repoHealthKnown {
			return settingsMenuCommitGraph + ": (checking…)"
		}
		// The git option is named so the row maps to the config explorer's
		// fetch.writeCommitGraph line — "auto-refresh" alone is unfindable there.
		switch {
		case !m.repoHealth.HasCommitGraph:
			return settingsMenuCommitGraph + ": missing — enter writes + sets fetch.writeCommitGraph"
		case m.repoHealth.WriteCommitGraphValue == "true":
			return settingsMenuCommitGraph + ": present, auto-refresh on (fetch.writeCommitGraph)"
		default:
			return settingsMenuCommitGraph + ": present, auto-refresh off — enter sets fetch.writeCommitGraph"
		}
	}
	return settingsMenu[i]
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
		m.statusMsg = "show graph → " + next + " (not saved: no repo config path)"
	} else if err := config.SetShowGraph(m.repoConfigPath, next); err != nil {
		m.statusMsg = "show graph → " + next + " (not saved: " + err.Error() + ")"
	} else {
		m.statusMsg = "show graph: " + next
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
		m.statusMsg = "commit sort → " + next + " (not saved: no repo config path)"
	} else if err := config.SetCommitSort(m.repoConfigPath, next); err != nil {
		m.statusMsg = "commit sort → " + next + " (not saved: " + err.Error() + ")"
	} else {
		m.statusMsg = "commit sort: " + next + " — reloading commits…"
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
		m.statusMsg = "operation log: " + err.Error()
		return m
	}
	m.cfg.Debug.LogOperations = m.opLog.on // keep the in-memory view in sync
	if perr := config.SetGlobalDebugLogOperations(config.DefaultGlobalPath(), m.opLog.on); perr != nil {
		m.statusMsg = "operation log toggled but not saved: " + perr.Error()
		return m
	}
	if m.opLog.on {
		m.statusMsg = "operation log on — " + m.opLog.path
	} else {
		m.statusMsg = "operation log off"
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
		m.statusMsg = "auto-refresh toggled but not saved: " + err.Error()
		return m
	}
	if want {
		m.statusMsg = "auto-refresh on (per-source intervals from [refresh])"
	} else {
		m.statusMsg = "auto-refresh off"
	}
	return m
}

// toggleAutoRemoteTags flips the auto remote-tag refresh switch (inverted flag),
// persisting to the global config so it survives restarts (mirrors toggleAutoRefresh).
func (m Model) toggleAutoRemoteTags() Model {
	wantDisabled := !m.cfg.Refresh.DisableRemoteTagsAuto
	m.cfg.Refresh.DisableRemoteTagsAuto = wantDisabled
	if err := config.SetGlobalDisableRemoteTagsAuto(config.DefaultGlobalPath(), wantDisabled); err != nil {
		m.statusMsg = "auto remote-tag refresh toggled but not saved: " + err.Error()
		return m
	}
	if wantDisabled {
		m.statusMsg = "auto remote-tag refresh off"
	} else {
		m.statusMsg = "auto remote-tag refresh on"
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

// update handles all keys while the settings popup is open.
func (p *settingsPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		if p.errorsView {
			p.errorsView = false
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
		if p.picker {
			p.picker = false
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
	if !p.picker && !p.errorsView && !p.ratesView {
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
			case settingsMenuAgents:
				return m.openAgentPicker(), nil
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
			case settingsMenuRepoLoc:
				return m.openRepoConfigLocation(), nil
			case settingsMenuCommitGraph:
				if !m.repoHealthKnown {
					m.statusMsg = "still checking the repo — try again in a moment"
					return m, nil
				}
				if m.repoHealth.HasCommitGraph && m.repoHealth.WriteCommitGraphValue == "true" {
					m.statusMsg = "commit-graph present and auto-refresh already on"
					return m, nil
				}
				if m.running {
					return m, nil // an op is already in flight
				}
				// Same code path as the notice's "write + keep fresh" action.
				return m.startCommitGraphWriteAndEnable()
			case settingsMenuGitConfig:
				return m.openGitConfigExplorer()
			case settingsMenuRates:
				p.ratesView = true
				p.ratesSel = 0
				p.ratesEditing = false
				p.sel = 0
				p.hscroll = 0
				return m, nil
			case settingsMenuErrors:
				p.errorsView = true
				p.sel = 0
				p.hscroll = 0
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
		m.statusMsg = fmt.Sprintf("agent skills: %d installed, %d refreshed", installed, refreshed)
		if failed > 0 {
			m.statusMsg += fmt.Sprintf(", %d failed", failed)
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
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	// The errors viewer holds long, path-heavy rows (git stderr, the errors.log
	// location), so it scales wide like the bookmark/shelf switchers — most
	// errors and the log path then fit on one line instead of wrapping ugly.
	if p.errorsView || p.ratesView {
		inner = popupWideInnerWidth(w)
	}
	textW := popupTextWidth(inner)
	var b strings.Builder
	if p.errorsView {
		b.WriteString("Session errors\n\n")
		fs := observ.SessionFailures()
		anyTrunc := false
		if len(fs) == 0 {
			b.WriteString("  no errors this session\n")
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
			_, termH := m.overlayDims()
			capRows := termH - 12
			if capRows < 3 {
				capRows = 3
			}
			h := len(fs)
			if p.mode == modeWrap {
				total := 0
				for _, r := range wr {
					total += len(wrapWidth(r.text, textW, 1<<20))
				}
				h = total
			}
			if h > capRows {
				h = capRows
			}
			for _, line := range renderWindow(wr, winOpts{w: textW, h: h, mode: p.mode, anchor: p.sel, hscroll: p.hscroll}) {
				b.WriteString(line + "\n")
			}
		}
		// Wrap the path so its basename stays visible on a narrow popup; a raw
		// line would be truncated by popupBox and the user could never see where
		// errors.log lives.
		if path := defaultErrLogPath(); path != "" {
			b.WriteString("\n")
			for _, seg := range wrapWidth("full history: "+path, textW, 1<<20) {
				b.WriteString(seg + "\n")
			}
		}
		// Advertise z only when it does something: an entry too long to fit (in
		// any mode) is what wrap/scroll reveal. Otherwise the hint is a lie.
		if anyTrunc {
			b.WriteString("\n[z] mode  [esc] back")
		} else {
			b.WriteString("\n[esc] back")
		}
	} else if p.ratesView {
		b.WriteString("Refresh rates\n\n")
		if !m.cfg.Refresh.Enabled {
			b.WriteString("  auto-refresh is OFF — enable it in Settings → Auto-refresh\n\n")
		}
		// Column header. "file-watch" labels the [x]/[ ] checkbox column so it is not
		// an unexplained box; the legend below the table says what it means.
		b.WriteString(fmt.Sprintf("  %-11s %-11s %-16s %s\n", "window", "file-watch", "refresh", "avg read"))
		for i, it := range scheduledItems {
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
					valCell = "watch"
				} else {
					// drvfs: watch unavailable → falls back to the interval
					secs, on := scheduledInterval(m.cfg.Refresh, it)
					if on {
						valCell = fmt.Sprintf("watch (9p→%ds)", secs)
					} else {
						valCell = "watch (9p→off)"
					}
				}
			} else {
				secs, on := scheduledInterval(m.cfg.Refresh, it)
				if on {
					valCell = fmt.Sprintf("every %ds", secs)
				} else {
					valCell = "off"
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
			b.WriteString(fmt.Sprintf("%s%-11s %-11s %-16s %s\n", prefix, name, watchBox, valCell, avgStr))
		}
		if p.ratesEditing {
			b.WriteString("\n[0-9] edit  [enter] save  [esc] cancel   (0 = off)")
		} else {
			b.WriteString("\nfile-watch = auto-detect .git changes instantly (else poll on the interval)")
			b.WriteString("\n[↑/↓] select  [enter] edit interval  [space]/[w] file-watch  [esc] back")
		}
	} else if !p.picker {
		b.WriteString("Settings\n\n")
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
		for _, line := range renderWindow(wr, winOpts{w: textW, h: len(settingsMenu), mode: p.mode, anchor: p.menuSel, hscroll: p.hscroll}) {
			b.WriteString(line + "\n")
		}
		b.WriteString("\n[↑/↓] select  [enter] open/toggle  [esc] close")
	} else {
		b.WriteString("Set up agent skills\n\n")
		if len(p.dets) == 0 {
			b.WriteString("  no supported agents detected\n")
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
				wr[i] = winRow{text: fmt.Sprintf("%s%s %s — %s", prefix, box, d.Agent.Label, d.Status), style: st}
			}
			h := len(p.dets)
			if h > 12 {
				h = 12
			}
			for _, line := range renderWindow(wr, winOpts{w: textW, h: h, mode: p.mode, anchor: p.sel, hscroll: p.hscroll}) {
				b.WriteString(line + "\n")
			}
		}
		b.WriteString("\n[space] toggle  [enter] apply  [z] mode  [esc] back")
	}
	return popupBox(inner, strings.TrimRight(b.String(), "\n"))
}
