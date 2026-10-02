package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// sessionsPopup is the ctrl+\ list of every agent session this gg process
// owns, grouped repo → worktree. In quit mode it is what quitting with live
// sessions opens (Task 8's quit guard).
type sessionsPopup struct {
	popupMax    // ctrl+t fills the screen (width and rows)
	quitMode    bool
	sel         int
	query       string
	typing      bool
	mode        dispMode
	hscroll     int
	confirmKill domain.SessionID // a running session asks once before k kills it

	tab           int                 // tabSessions | tabTasks | tabFiles (task_tab.go)
	agentSel      int                 // the Agents tab's cursor while another tab shows
	fileSel       int                 // the Open files tab's cursor while another tab shows
	nAgentRows    int                 // the Agents tab's row count (headers included)
	nSessions     int                 // the Agents tab's session rows
	nFiles        int                 // the Open files tab's row count
	taskRows      []taskRow           // the Headless tab's rows
	taskSel       int                 // its cursor
	hist          []domain.TaskRecord // the task history, read on open and on each task change (disk I/O: never per frame)
	confirmCancel domain.TaskID       // a live task asks once before k cancels it

	rows  []string           // the Agents or Open files tab's rows, headers included
	ids   []domain.SessionID // parallel to rows; "" = not a session row
	files []*openFile        // parallel to rows; nil = not an open-file row
}

// sessionsPopupRows lays the sessions out repo → worktree → session. A
// session matches query (case-insensitive) on its label, worktree or repo;
// a header shows only when a session under it does.
func sessionsPopupRows(list []domain.SessionInfo, query string) (rows []string, ids []domain.SessionID) {
	q := strings.ToLower(query)
	byRepo := map[string]map[string][]domain.SessionInfo{}
	for _, info := range list {
		if q != "" && !strings.Contains(strings.ToLower(info.Label+" "+info.Dir+" "+info.Repo), q) {
			continue
		}
		dirs := byRepo[info.Repo]
		if dirs == nil {
			dirs = map[string][]domain.SessionInfo{}
			byRepo[info.Repo] = dirs
		}
		dirs[info.Dir] = append(dirs[info.Dir], info)
	}
	repos := make([]string, 0, len(byRepo))
	for r := range byRepo {
		repos = append(repos, r)
	}
	sort.Strings(repos)
	for _, r := range repos {
		rows, ids = append(rows, r), append(ids, "")
		dirs := make([]string, 0, len(byRepo[r]))
		for d := range byRepo[r] {
			dirs = append(dirs, d)
		}
		sort.Strings(dirs)
		for _, d := range dirs {
			rows, ids = append(rows, "  "+filepath.Base(d)+"  "+d), append(ids, "")
			for _, info := range byRepo[r][d] {
				rows, ids = append(rows, "    "+sessionStateText(info)), append(ids, info.ID)
			}
		}
	}
	return rows, ids
}

// sessionStateText is "● Claude  running 12m" / "○ Codex  exited (0)"; the
// row is wide, so a known activity is appended: "running 12m · idle 3m".
func sessionStateText(info domain.SessionInfo) string {
	if info.State == domain.SessionExited {
		return "○ " + info.Label + "  " + i18n.T("exited (%d)", info.ExitCode)
	}
	row := "● " + info.Label + "  " + i18n.T("running %s", formatElapsed(time.Since(info.Started)))
	if act := sessionActivityText(info.ID); act != "" {
		row += " · " + act
	}
	return row
}

// openSessionsPopup opens the ctrl+\ popup; with no sessions (and not in
// quit mode) it only explains how to start one.
func (m Model) openSessionsPopup(quitMode bool) (Model, tea.Cmd) {
	var hist []domain.TaskRecord
	if !quitMode {
		hist = domain.Tasks().History()
	}
	if len(domain.Sessions().List()) == 0 && !quitMode && len(m.openFiles.list(m.currentWorktree)) == 0 &&
		len(domain.Tasks().List()) == 0 && len(hist) == 0 {
		m.statusMsg = i18n.T("no agent sessions — start one from the . menu of a worktree or a checked-out branch")
		return m, nil
	}
	p := &sessionsPopup{quitMode: quitMode, hist: hist}
	switch {
	case quitMode:
		if domain.Sessions().LiveCount() == 0 {
			p.tab = tabTasks // quitting with only tasks alive
		}
	case len(domain.Sessions().List()) == 0 && len(m.openFiles.list(m.currentWorktree)) == 0:
		p.tab = tabTasks // only tasks to show
	case len(domain.Sessions().List()) == 0:
		p.tab = tabFiles // no sessions, some open files
	case newestTaskTime(hist).After(newestSessionTime()):
		p.tab = tabTasks // the freshest thing is a task
	}
	p.refresh(m)
	p.sel = p.nextSelectable(-1, +1)
	return m.pushLayer(p), nil
}

// refresh re-derives the rows: the live session list on the Agents tab, the
// worktree's open files on the Open files tab (never in quit mode, which is
// only about ending sessions). Both counts are kept for the tab strip.
func (p *sessionsPopup) refresh(m Model) {
	var removed map[domain.TaskID]bool
	if m.taskTrack != nil {
		removed = m.taskTrack.removed
	}
	p.taskRows = taskRows(domain.Tasks().List(), p.hist, p.query, removed)
	if p.taskSel >= len(p.taskRows) {
		p.taskSel = max(len(p.taskRows)-1, 0)
	}
	agentRows, agentIDs := sessionsPopupRows(domain.Sessions().List(), p.query)
	var fileRows []string
	var docs []*openFile
	if !p.quitMode {
		q := strings.ToLower(p.query)
		for _, d := range m.openFiles.list(m.currentWorktree) {
			row := openFileRowText(d, m.docShown(d))
			if q != "" && !strings.Contains(strings.ToLower(row), q) {
				continue
			}
			fileRows, docs = append(fileRows, row), append(docs, d)
		}
	}
	p.nAgentRows, p.nFiles, p.nSessions = len(agentRows), len(docs), 0
	for _, id := range agentIDs {
		if id != "" {
			p.nSessions++
		}
	}
	if p.tab == tabFiles {
		p.rows, p.ids, p.files = fileRows, make([]domain.SessionID, len(docs)), docs
	} else {
		p.rows, p.ids, p.files = agentRows, agentIDs, make([]*openFile, len(agentRows))
	}
	if p.sel >= len(p.rows) || (p.sel >= 0 && !p.selectable(p.sel)) {
		p.sel = p.nextSelectable(-1, +1)
	}
}

// openFileRowText is "● path  :12  working tree" — ● on screen, ○ in the
// background; the line is the cursor's.
func openFileRowText(d *openFile, shown bool) string {
	mark := "○ "
	if shown {
		mark = "● "
	}
	if d.ov != nil {
		return mark + d.title + "  " + i18n.T("overview · %d anchors", len(d.ov.anchors))
	}
	if d.src.kind == srcExternal || d.src.kind == srcNote {
		name := d.title
		if name == "" {
			name = d.path
		}
		if d.src.kind == srcNote {
			return mark + name + "  " + i18n.T("AI review")
		}
		return mark + name + "  " + i18n.T("AI result")
	}
	row := mark + d.path
	if docLoaded(d) {
		row += fmt.Sprintf("  :%d", d.p.cur+1)
	}
	switch n := len(d.notes); {
	case n == 1:
		row += "  " + i18n.T("1 note")
	case n > 1:
		row += "  " + i18n.T("%d notes", n)
	}
	switch d.src.kind {
	case srcCommit:
		return row + "  " + i18n.T("@ %s", shortHash(d.src.rev))
	case srcShelf:
		return row + "  " + i18n.T("shelf")
	}
	return row + "  " + i18n.T("working tree")
}

func (p *sessionsPopup) selectable(i int) bool {
	return i >= 0 && i < len(p.ids) && (p.ids[i] != "" || p.files[i] != nil)
}

// currentFile is the open file under the cursor, if it is a file row.
func (p *sessionsPopup) currentFile() (*openFile, bool) {
	if p.sel < 0 || p.sel >= len(p.files) || p.files[p.sel] == nil {
		return nil, false
	}
	return p.files[p.sel], true
}

// nextSelectable is the next session row from `from` in direction dir, or
// `from` when there is none.
func (p *sessionsPopup) nextSelectable(from, dir int) int {
	for i := from + dir; i >= 0 && i < len(p.ids); i += dir {
		if p.selectable(i) {
			return i
		}
	}
	if from < 0 {
		return 0
	}
	return from
}

func (p *sessionsPopup) current() (domain.SessionID, bool) {
	if p.sel < 0 || p.sel >= len(p.ids) || p.ids[p.sel] == "" {
		return "", false
	}
	return p.ids[p.sel], true
}

func (p *sessionsPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	p.refresh(m)
	key := msg.String()
	if key == "ctrl+c" && !p.quitMode {
		return m, tea.Quit
	}
	if p.typing {
		switch msg.Type {
		case tea.KeyEsc:
			p.typing, p.query = false, ""
		case tea.KeyEnter:
			p.typing = false
		case tea.KeyBackspace:
			if r := []rune(p.query); len(r) > 0 {
				p.query = string(r[:len(r)-1])
			}
		case tea.KeySpace:
			p.query += " "
		case tea.KeyRunes:
			p.query += string(msg.Runes)
		}
		p.refresh(m)
		return m, nil
	}
	if key == "tab" || key == "shift+tab" {
		dir := +1
		if key == "shift+tab" {
			dir = -1
		}
		p.switchTab(m, dir)
		return m, nil
	}
	if p.tab == tabTasks && !p.quitMode {
		switch key {
		case "esc":
			return m.popLayer(), nil
		case "/":
			p.typing = true
			return m, nil
		case "z", "shift+left", "shift+right":
			// shared with the sessions tab below
		default:
			return p.updateTasks(m, key)
		}
	}
	if key != "k" && key != "y" {
		p.confirmKill = ""
	}
	switch key {
	case "esc":
		return m.popLayer(), nil
	case "Q":
		if p.quitMode {
			m = m.popLayer()
			m.quitConfirmed = true
			m.statusMsg = i18n.T("ending agent sessions…")
			return m, killAllAndQuitCmd()
		}
	case "z":
		p.mode, p.hscroll = p.mode.next(), 0
	case "shift+left":
		if p.mode == modeScroll && p.hscroll > 0 {
			p.hscroll = max(p.hscroll-m.hscrollStep(), 0)
		}
	case "shift+right":
		if p.mode == modeScroll {
			p.hscroll += m.hscrollStep()
		}
	case "up", "ctrl+p":
		p.sel = p.nextSelectable(p.sel, -1)
	case "down", "ctrl+n", "j":
		p.sel = p.nextSelectable(p.sel, +1)
	case "/":
		p.typing = true
	case "enter":
		if d, ok := p.currentFile(); ok {
			m = m.popLayer()
			return m.bringToFront(d)
		}
		if id, ok := p.current(); ok {
			m = m.popLayer()
			return m.openSessionAnywhere(id) // another repo's session switches there first
		}
	case "k", "y":
		id, ok := p.current()
		if !ok {
			return m, nil
		}
		s, ok := domain.Sessions().Get(id)
		if !ok || s.Info().State != domain.SessionRunning {
			return m, nil
		}
		if p.confirmKill != id {
			p.confirmKill = id
			m.statusMsg = i18n.T("press k again to kill %s", s.Info().Label)
			return m, nil
		}
		p.confirmKill = ""
		m = m.killSession(id)
	case "x":
		if d, ok := p.currentFile(); ok {
			m = m.closeDoc(d)
			p.refresh(m)
			return m, nil
		}
		if id, ok := p.current(); ok {
			if err := domain.Sessions().Remove(id); err != nil {
				m.statusMsg = i18n.T("only an exited session can be removed — kill it first (k)")
			}
			p.refresh(m)
		}
	}
	return m, nil
}

func (p *sessionsPopup) render(m Model, below string) string {
	p.refresh(m)
	w, h := m.overlayDims()
	inner := popupResolveWidth(w, p.maxed(), popupWideInnerWidth(w))
	textW := popupTextWidth(inner)
	s := st()

	// The header: the quit question (quit mode), then the tab strip — the
	// panels' tab convention (the active tab bracketed), the active one bold
	// and the other dim, each with its row count — and a rule under it.
	var head []string
	if p.quitMode {
		head = append(head, i18n.T("agents and AI tasks still running: %d — quit gg?", liveWork()), "")
	}
	strip := p.tabStrip(s)
	if p.typing || p.query != "" {
		q := "  /" + p.query
		if p.typing {
			q += "█"
		}
		strip += q
	}
	head = append(head, strip, s.dim.Render(strings.Repeat("─", textW)))

	// Fixed height: every tab gets the tallest tab's row count, so switching
	// never resizes the box.
	rowsH := min(max(p.sessionRowCount(), p.taskRowCount(), p.fileRowCount(), 1), max(h-10, 3))
	if p.maxed() { // maximised: every row the screen holds
		rowsH = max(h-11, 3)
		if p.quitMode {
			rowsH = max(rowsH-2, 3)
		}
	}
	var body, hints []string
	switch p.tab {
	case tabTasks:
		body = p.renderTaskRows(m, textW, rowsH)
		hints = []string{p.taskHint()}
	case tabFiles:
		body = p.renderSessionRows(textW, rowsH)
		hints = []string{i18n.T("[enter] open  [x] close  [/] filter  [z] mode  [tab] agents  [ctrl+t] full  [esc] close")}
	default:
		body = p.renderSessionRows(textW, rowsH)
		hints = []string{i18n.T("[enter] open  [k] kill  [x] remove  [/] filter  [z] mode  [tab] AI tasks  [ctrl+t] full  [esc] close")}
	}
	if p.quitMode {
		hints = append(hints, i18n.T("[Q] kill all and quit  [esc] cancel"))
	}
	for len(body) < rowsH+1 { // +1: the tasks tab's table header
		body = append(body, padRight("", textW))
	}
	lines := append(append(head, body...), "")
	lines = append(lines, hints...)
	box := popupBox(inner, strings.Join(lines, "\n"))
	return overlayCenter(clipToHeight(below, h), box, w, h)
}

// tabs is the popup's tab cycle: quit mode has no Open files tab.
func (p *sessionsPopup) tabs() []int {
	if p.quitMode {
		return []int{tabSessions, tabTasks}
	}
	return []int{tabSessions, tabTasks, tabFiles}
}

// switchTab steps dir tabs along the cycle; the Agents and Open files tabs
// each keep their own cursor.
func (p *sessionsPopup) switchTab(m Model, dir int) {
	switch p.tab {
	case tabSessions:
		p.agentSel = p.sel
	case tabFiles:
		p.fileSel = p.sel
	}
	tabs := p.tabs()
	i := 0
	for j, t := range tabs {
		if t == p.tab {
			i = j
		}
	}
	p.tab = tabs[(i+dir+len(tabs))%len(tabs)]
	p.confirmKill, p.confirmCancel = "", ""
	switch p.tab {
	case tabSessions:
		p.sel = p.agentSel
	case tabFiles:
		p.sel = p.fileSel
	}
	p.refresh(m)
}

// tabStrip is "[Agents 2]  AI tasks 5  Open files 3" with the active tab
// bold and bracketed, the others dim.
func (p *sessionsPopup) tabStrip(s *styles) string {
	bold := lipgloss.NewStyle().Bold(true)
	var parts []string
	for _, t := range p.tabs() {
		var label string
		switch t {
		case tabSessions:
			label = i18n.T("Agents %d", p.sessionCount())
		case tabTasks:
			label = i18n.T("AI tasks %d", len(p.taskRows))
		default:
			label = i18n.T("Open files %d", p.nFiles)
		}
		if t == p.tab {
			parts = append(parts, bold.Render("["+label+"]"))
		} else {
			parts = append(parts, s.dim.Render(" "+label+" "))
		}
	}
	return strings.Join(parts, " ")
}

// sessionCount counts the Agents tab's sessions (not the repo and worktree
// headers), whichever tab shows.
func (p *sessionsPopup) sessionCount() int { return p.nSessions }

// sessionRowCount / taskRowCount / fileRowCount are each tab's body height
// (1 for the empty-state line).
func (p *sessionsPopup) sessionRowCount() int { return max(p.nAgentRows, 1) }
func (p *sessionsPopup) taskRowCount() int    { return max(len(p.taskRows), 1) }
func (p *sessionsPopup) fileRowCount() int    { return max(p.nFiles, 1) }

// renderSessionRows lays out the Agents or Open files tab's rows in rowsH
// lines.
func (p *sessionsPopup) renderSessionRows(textW, rowsH int) []string {
	if len(p.rows) == 0 {
		if p.tab == tabFiles {
			return []string{padRight(i18n.T("  (no open files)"), textW)}
		}
		return []string{padRight(i18n.T("  (no agent sessions)"), textW)}
	}
	s := st()
	wr := make([]winRow, len(p.rows))
	for i, r := range p.rows {
		var style lipgloss.Style
		switch {
		case i == p.sel && p.selectable(i):
			r, style = "> "+r, s.selectedRow
		case !p.selectable(i) && !strings.HasPrefix(r, " "):
			r, style = "  "+r, lipgloss.NewStyle().Bold(true)
		default:
			r = "  " + r
		}
		// An open-file row keeps its file name: "> ● " is the lead-in,
		// the path loses its middle.
		wr[i] = winRow{text: r, style: style, elide: i < len(p.files) && p.files[i] != nil, elideHead: 4}
	}
	return renderWindow(wr, winOpts{w: textW, h: rowsH, mode: p.mode, anchor: p.sel, hscroll: p.hscroll})
}

// newestSessionTime is when the newest agent session started (zero: none).
func newestSessionTime() time.Time {
	var t time.Time
	for _, info := range domain.Sessions().List() {
		if info.Started.After(t) {
			t = info.Started
		}
	}
	return t
}

// newestTaskTime is the newest AI-task event — submitted, started or ended —
// over the live tasks and the history (zero: none).
func newestTaskTime(hist []domain.TaskRecord) time.Time {
	var t time.Time
	later := func(c time.Time) {
		if c.After(t) {
			t = c
		}
	}
	for _, info := range domain.Tasks().List() {
		later(info.Submitted)
		later(info.Started)
		later(info.Ended)
	}
	for _, r := range hist {
		later(r.Started)
		later(r.Ended)
	}
	return t
}
