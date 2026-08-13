package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/fsprobe"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/repos"
)

// repoPopup is the transient repo-switcher picker opened with R. It holds an
// MRU snapshot taken at open; ctrl+d edits both the snapshot and the registry.
type repoPopup struct {
	popupMax
	entries   []repos.Entry
	query     string // case-insensitive substring over name+path
	filtering bool   // true while `/` filter sub-mode captures runes
	sel       int    // index into the FILTERED view
	now       time.Time
	mode      dispMode // text display mode; z cycles (cutoff default = no wrapping)
	hscroll   int      // modeScroll horizontal offset

	// foreign holds the async slow-filesystem verdicts (path → true when the
	// repo sits on a network/OS-bridge mount where switching crawls). nil until
	// repoFSMsg lands; rows gain a marker and the selected row a warning then.
	foreign map[string]bool
}

// repoFSMsg carries the async slow-filesystem verdicts for the switcher rows.
type repoFSMsg struct{ foreign map[string]bool }

// probeReposCmd probes each entry's filesystem off-thread. One goroutine per
// entry under a shared deadline: statfs on a dead network mount can block
// indefinitely, and a wedged entry must cost the popup its marker, not its
// responsiveness (unanswered entries simply stay unmarked).
func probeReposCmd(entries []repos.Entry) tea.Cmd {
	return func() tea.Msg {
		type verdict struct {
			path    string
			foreign bool
		}
		ch := make(chan verdict, len(entries))
		for _, e := range entries {
			go func(p string) { ch <- verdict{p, fsprobe.Foreign(p)} }(e.Path)
		}
		out := make(map[string]bool, len(entries))
		deadline := time.After(time.Second)
		for range entries {
			select {
			case v := <-ch:
				out[v.path] = v.foreign
			case <-deadline:
				return repoFSMsg{foreign: out}
			}
		}
		return repoFSMsg{foreign: out}
	}
}

// moveSel moves the cursor by d, clamped to the filtered view.
func (p *repoPopup) moveSel(d int) {
	n := p.sel + d
	if hi := len(p.visible()) - 1; n > hi {
		n = hi
	}
	if n < 0 {
		n = 0
	}
	p.sel = n
}

// openRepoPopup snapshots the registry. With no known repos it sets a status
// hint instead of opening an empty picker. The returned cmd (probe of each
// entry's filesystem) must be dispatched alongside the popup.
func (m Model) openRepoPopup() (Model, tea.Cmd, bool) {
	entries := repos.Load(m.statePath)
	if len(entries) == 0 {
		m.statusMsg = i18n.T("no known repositories yet (gg records them as you open repos)")
		return m, nil, false
	}
	m = m.pushLayer(&repoPopup{entries: entries, now: time.Now()})
	return m, probeReposCmd(entries), true
}

// visible returns the filtered entries in display order.
func (p *repoPopup) visible() []repos.Entry {
	if p.query == "" {
		return p.entries
	}
	q := strings.ToLower(p.query)
	out := make([]repos.Entry, 0, len(p.entries))
	for _, e := range p.entries {
		if strings.Contains(strings.ToLower(repos.Name(e)), q) ||
			strings.Contains(strings.ToLower(e.Path), q) {
			out = append(out, e)
		}
	}
	return out
}

// update handles all keys while the picker is open. It swallows everything (no
// fallthrough to global handlers). Navigation-first, like the finder and the
// bookmark/shelf switchers: plain keys navigate, `/` enters a filter sub-mode
// where runes (including `z`) type a query until esc/enter.

func (p *repoPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if p.filtering {
		// Arrows/pages move the selection live while typing (no cursor reset),
		// like the commit filter; j/k stay query text.
		if filterMotion(msg, p.moveSel, popupFilterPage) {
			return m, nil
		}
		switch msg.Type {
		case tea.KeyEsc:
			p.filtering, p.query, p.sel = false, "", 0
		case tea.KeyEnter:
			p.filtering = false // commit: keep the filter, leave input mode
		case tea.KeyBackspace, tea.KeyCtrlH:
			if r := []rune(p.query); len(r) > 0 {
				p.query = string(r[:len(r)-1])
			}
			p.sel = 0
		case tea.KeySpace:
			p.query += " "
			p.sel = 0
		case tea.KeyRunes:
			p.query += string(msg.Runes)
			p.sel = 0
		}
		return m, nil
	}
	// Navigation mode. Display-mode + pan keys act here (query chars while filtering).
	switch msg.String() {
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
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popLayer()
		return m, nil
	case tea.KeyUp:
		p.moveSel(-1)
		return m, nil
	case tea.KeyDown:
		p.moveSel(1)
		return m, nil
	case tea.KeyPgUp:
		p.moveSel(-popupFilterPage)
		return m, nil
	case tea.KeyPgDown:
		p.moveSel(popupFilterPage)
		return m, nil
	case tea.KeyEnter:
		vis := p.visible()
		m = m.popLayer()
		if p.sel < 0 || p.sel >= len(vis) {
			return m, nil
		}
		target := vis[p.sel].Path
		if samePathTUI(target, m.currentWorktree) {
			return m, nil // already here
		}
		if p.foreign[target] && m.confirmSlowOps() {
			// A foreign-fs switch can block the interface for a minute (the
			// snapshot's whole-tree status walk), so it confirms like every
			// other slow op — same [ui] disable_slow_op_confirm bypass,
			// default No.
			m.modal = &decisionState{
				req: engine.DecisionRequest{
					ID:      "confirm-slow-op",
					Prompt:  i18n.T("Switch to a repository on a foreign filesystem? The switch may be very slow."),
					Options: []string{"Yes", "No"},
				},
				sel:     1,
				confirm: true,
				onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
					if opt == "Yes" {
						return m.guardedReRoot(target, false)
					}
					return m, nil
				},
			}
			return m, nil
		}
		tm, cmd := m.guardedReRoot(target, false)
		return tm.(Model), cmd
	case tea.KeyCtrlD:
		vis := p.visible()
		if p.sel < 0 || p.sel >= len(vis) {
			return m, nil
		}
		victim := vis[p.sel].Path
		_ = repos.Remove(m.statePath, victim)
		kept := p.entries[:0]
		for _, e := range p.entries {
			if e.Path != victim {
				kept = append(kept, e)
			}
		}
		p.entries = kept
		if n := len(p.visible()); p.sel >= n && n > 0 {
			p.sel = n - 1
		}
		return m, nil
	case tea.KeyRunes:
		switch msg.String() {
		case "/":
			p.filtering = true
		case "j":
			p.moveSel(1)
		case "k":
			p.moveSel(-1)
		}
		return m, nil
	}
	return m, nil
}

// render composites the picker over the layer beneath, plus the slow-fs
// tooltip when the selected row warrants one.
func (p *repoPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	box := p.box(m)
	out := overlayCenter(clipToHeight(below, h), box, w, h)
	if line, x, y, ok := p.slowTooltip(m, box, w, h); ok {
		out = overlayAt(out, line, x, y, w, h)
	}
	return out
}

// slowTooltip returns the slow-filesystem warning as an overlay strip drawn
// one line above the popup box — tooltip-style, NEVER a body line: an in-body
// warning made the popup height flip with every local↔slow cursor move, and
// anchoring it under the selected row covered the row beneath (user reports).
// Above the box it covers nothing the picker draws.
func (p *repoPopup) slowTooltip(m Model, box string, termW, termH int) (line string, x, y int, ok bool) {
	vis := p.visible()
	if len(vis) == 0 || p.sel < 0 || p.sel >= len(vis) || !p.foreign[vis[p.sel].Path] {
		return "", 0, 0, false
	}
	boxLines := strings.Split(box, "\n")
	boxW := 0
	for _, l := range boxLines {
		if w := lipgloss.Width(l); w > boxW {
			boxW = w
		}
	}
	left := (termW - boxW) / 2 // mirrors overlayCenter's placement of the box
	top := (termH - len(boxLines)) / 2
	text := " " + i18n.T("⚠ this repository is mounted on a foreign filesystem — switching may be very slow") + " "
	// Center the strip on the box (left-anchoring read as lopsided — user
	// report); when the sentence would run past a screen edge, shift it back
	// on-screen (truncation would eat exactly the "very slow" tail the tooltip
	// exists for).
	need := lipgloss.Width(text)
	x = left + (boxW-need)/2
	if x+need > termW {
		x = termW - need
	}
	if x < 0 {
		x = 0
	}
	if lines := wrapWidth(text, termW-x, 1); len(lines) > 0 {
		text = lines[0]
	}
	// top-1 goes negative when the box touches the screen top; overlayAt clamps
	// to row 0, so the strip then overwrites the top border — deliberate: in a
	// cramped terminal the warning beats one border line.
	return tooltipStyle.Render(text), x, top - 1, true
}

// box draws the picker box (modal box only).
func (p *repoPopup) box(m Model) string {
	w, termH := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupInnerWidth(w))
	textW := popupTextWidth(inner)

	header := i18n.T("Switch repository")
	switch {
	case p.filtering:
		header += "  /" + p.query + "█"
	case p.query != "":
		header += "  /" + p.query
	default:
		header += i18n.T("   (press / to filter)")
	}

	vis := p.visible()
	var bodyLines []string
	if len(vis) == 0 {
		bodyLines = []string{padRight(i18n.T("  (no match)"), textW)}
	} else {
		wr := make([]winRow, len(vis))
		for i, e := range vis {
			marker := "  "
			if samePathTUI(e.Path, m.currentWorktree) {
				marker = "● "
			}
			prefix := "  "
			var st lipgloss.Style
			if i == p.sel {
				prefix = "> "
				st = selectedRow
			}
			// The slow-fs marker sits between name and path: the row tail is
			// what cutoff mode truncates first, so a suffix there would be
			// invisible at the default popup width.
			slow := ""
			if p.foreign[e.Path] {
				slow = i18n.T("(slow fs)") + "  "
			}
			row := fmt.Sprintf("%s%s%s  %s%s  (%s)", prefix, marker, repos.Name(e), slow, e.Path, ageString(p.now, e.LastOpened))
			wr[i] = winRow{text: row, style: st}
		}
		// Cap the visible body; renderWindow scrolls to keep p.sel in view.
		// Wrap mode hang-indents continuations under the name (past the "> ● "
		// column) and separates entries with a blank line; the height budget
		// then counts display lines, not rows.
		capRows := popupResolveRowCap(p.maximized, termH, 12)
		o := winOpts{w: textW, mode: p.mode, anchor: p.sel, hscroll: p.hscroll, wrapIndent: 4, wrapGap: true}
		o.h = wrapContentLines(wr, o, capRows)
		bodyLines = renderWindow(wr, o)
	}

	hint := []string{i18n.T("[enter] switch"), i18n.T("[ctrl+d] forget"), i18n.T("[/] filter"), i18n.T("[z] mode"), i18n.T("[ctrl+t] full"), i18n.T("[esc] close")}
	parts := []string{header, ""}
	parts = append(parts, bodyLines...)
	parts = append(parts, "")
	parts = append(parts, wrapParts(hint, textW, "  ")...)
	return popupBox(inner, strings.Join(parts, "\n"))
}

// samePathTUI compares two paths after trimming trailing separators; symlink
// divergence is tolerated as inequality (worst case: a no-op switch into the
// same repo).
func samePathTUI(a, b string) bool {
	return strings.TrimRight(a, "/\\") == strings.TrimRight(b, "/\\")
}

// ageString renders a coarse relative age for the picker rows.
func ageString(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return i18n.T("just now")
	case d < time.Hour:
		return i18n.T("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return i18n.T("%dh ago", int(d.Hours()))
	default:
		return i18n.T("%dd ago", int(d.Hours()/24))
	}
}
