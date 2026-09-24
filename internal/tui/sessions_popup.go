package tui

import (
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
	quitMode    bool
	sel         int
	query       string
	typing      bool
	mode        dispMode
	hscroll     int
	confirmKill domain.SessionID // a running session asks once before k kills it

	rows []string           // rendered rows, headers included
	ids  []domain.SessionID // parallel to rows; "" = a header row
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

// sessionStateText is "● Claude  running 12m" / "○ Codex  exited (0)".
func sessionStateText(info domain.SessionInfo) string {
	if info.State == domain.SessionExited {
		return "○ " + info.Label + "  " + i18n.T("exited (%d)", info.ExitCode)
	}
	return "● " + info.Label + "  " + i18n.T("running %s", formatElapsed(time.Since(info.Started)))
}

// openSessionsPopup opens the ctrl+\ popup; with no sessions (and not in
// quit mode) it only explains how to start one.
func (m Model) openSessionsPopup(quitMode bool) (Model, tea.Cmd) {
	if len(domain.Sessions().List()) == 0 && !quitMode {
		m.statusMsg = i18n.T("no agent sessions — start one from a worktree's . menu")
		return m, nil
	}
	p := &sessionsPopup{quitMode: quitMode}
	p.refresh()
	p.sel = p.nextSelectable(-1, +1)
	return m.pushLayer(p), nil
}

// refresh re-derives the rows from the live session list.
func (p *sessionsPopup) refresh() {
	p.rows, p.ids = sessionsPopupRows(domain.Sessions().List(), p.query)
	if p.sel >= len(p.rows) || (p.sel >= 0 && p.sel < len(p.ids) && p.ids[p.sel] == "") {
		p.sel = p.nextSelectable(-1, +1)
	}
}

// nextSelectable is the next session row from `from` in direction dir, or
// `from` when there is none.
func (p *sessionsPopup) nextSelectable(from, dir int) int {
	for i := from + dir; i >= 0 && i < len(p.ids); i += dir {
		if p.ids[i] != "" {
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
	p.refresh()
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
		p.refresh()
		return m, nil
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
		if id, ok := p.current(); ok {
			m = m.popLayer()
			return m.openConsole(id)
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
		if id, ok := p.current(); ok {
			if err := domain.Sessions().Remove(id); err != nil {
				m.statusMsg = i18n.T("only an exited session can be removed — kill it first (k)")
			}
			p.refresh()
		}
	}
	return m, nil
}

func (p *sessionsPopup) render(m Model, below string) string {
	p.refresh()
	w, h := m.overlayDims()
	inner := popupWideInnerWidth(w)
	textW := popupTextWidth(inner)
	title := i18n.T("Agent sessions")
	if p.quitMode {
		title = i18n.T("agent sessions still running: %d — quit gg?", domain.Sessions().LiveCount())
	}
	if p.typing || p.query != "" {
		title += "  /" + p.query
		if p.typing {
			title += "█"
		}
	}
	s := st()
	var body []string
	if len(p.rows) == 0 {
		body = []string{padRight(i18n.T("  (no agent sessions)"), textW)}
	} else {
		wr := make([]winRow, len(p.rows))
		for i, r := range p.rows {
			var style lipgloss.Style
			switch {
			case i == p.sel && p.ids[i] != "":
				r, style = "> "+r, s.selectedRow
			case p.ids[i] == "" && !strings.HasPrefix(r, " "):
				r, style = "  "+r, lipgloss.NewStyle().Bold(true)
			default:
				r = "  " + r
			}
			wr[i] = winRow{text: r, style: style}
		}
		rowsH := min(len(wr), max(h-10, 3))
		body = renderWindow(wr, winOpts{w: textW, h: rowsH, mode: p.mode, anchor: p.sel, hscroll: p.hscroll})
	}
	lines := append([]string{title, ""}, body...)
	lines = append(lines, "", i18n.T("[enter] open  [k] kill  [x] remove  [/] filter  [z] mode  [esc] close"))
	if p.quitMode {
		lines = append(lines, i18n.T("[Q] kill all and quit  [esc] cancel"))
	}
	box := popupBox(inner, strings.Join(lines, "\n"))
	return overlayCenter(clipToHeight(below, h), box, w, h)
}
