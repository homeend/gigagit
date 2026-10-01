package tui

import (
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/fsprobe"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/repos"
	"github.com/homeend/gigagit/internal/worktree"
)

// repoPopup is the transient repo-switcher picker opened with R. It holds an
// MRU snapshot taken at open; ctrl+d edits both the snapshot and the registry.
// It is ALWAYS fullscreen (no popupMax embed): it renders at the maximized
// width/row budget from the start, and ctrl+t falls through to update, which
// swallows it.
type repoPopup struct {
	entries []repos.Entry
	query   string // case-insensitive substring over name+path; typing extends it
	sel     int    // index into the FILTERED view
	now     time.Time
	mode    dispMode // text display mode; z cycles (cutoff default = no wrapping)
	hscroll int      // modeScroll horizontal offset
	// grouped gathers the checkouts of one project under its most recent one
	// (ctrl+g). Seeded from — and mirrored to — Model.repoGrouped, which is
	// persisted, so the choice outlives this popup and the session.
	grouped bool

	// foreign holds the async slow-filesystem verdicts (path → true when the
	// repo sits on a network/OS-bridge mount where switching crawls). nil until
	// repoFSMsg lands; rows gain a marker and the selected row a warning then.
	foreign map[string]bool
	// common holds each entry's git common dir (path → dir), read by the same
	// async probe; grouping joins a checkout with its linked worktrees through
	// it. nil until repoFSMsg lands — grouping then goes by remote name alone.
	common map[string]string
}

// repoFSMsg carries the async per-entry probe results for the switcher rows:
// the slow-filesystem verdicts and the git common dirs.
type repoFSMsg struct {
	foreign map[string]bool
	common  map[string]string
}

// probeReposCmd probes each entry's filesystem and git common dir off-thread.
// One goroutine per entry under a shared deadline: statfs on a dead network
// mount can block indefinitely, and a wedged entry must cost the popup its
// marker, not its responsiveness (unanswered entries simply stay unmarked and
// group by remote name alone).
func probeReposCmd(entries []repos.Entry) tea.Cmd {
	return func() tea.Msg {
		type verdict struct {
			path    string
			foreign bool
			common  string
		}
		ch := make(chan verdict, len(entries))
		for _, e := range entries {
			go func(p string) {
				ch <- verdict{p, fsprobe.Foreign(p), worktree.CommonDirAt(runtime.GOOS, p)}
			}(e.Path)
		}
		out := repoFSMsg{foreign: make(map[string]bool, len(entries)), common: make(map[string]string, len(entries))}
		deadline := time.After(time.Second)
		for range entries {
			select {
			case v := <-ch:
				out.foreign[v.path] = v.foreign
				if v.common != "" {
					out.common[v.path] = v.common
				}
			case <-deadline:
				return out
			}
		}
		return out
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
	m = m.pushLayer(&repoPopup{entries: entries, now: clock.Now(), grouped: m.repoGrouped})
	return m, probeReposCmd(entries), true
}

// visible returns the filtered entries in display order: MRU, regrouped by
// project when grouped is on.
func (p *repoPopup) visible() []repos.Entry {
	out := p.entries
	if p.query != "" {
		q := strings.ToLower(p.query)
		out = make([]repos.Entry, 0, len(p.entries))
		for _, e := range p.entries {
			if strings.Contains(strings.ToLower(repos.Name(e)), q) ||
				strings.Contains(strings.ToLower(e.Path), q) {
				out = append(out, e)
			}
		}
	}
	if p.grouped {
		out = repos.Group(out, p.projects())
	}
	return out
}

// projects is the grouping of ALL entries (see repos.Projects) with the
// common dirs the probe has read so far.
func (p *repoPopup) projects() map[string]repos.Project {
	return repos.Projects(p.entries, p.common)
}

// rowName is the name cell of row i in vis. Grouped, the head of a project
// with several rows shows the project name and the rows under it show none.
// A project left with a single row is no group, so — like an entry outside
// any project, and every row of the flat list — it shows its directory name.
func (p *repoPopup) rowName(vis []repos.Entry, i int) string {
	if !p.grouped {
		return repos.Name(vis[i])
	}
	proj := p.projects()
	pr, ok := proj[vis[i].Path]
	if !ok {
		return repos.Name(vis[i])
	}
	sameProject := func(j int) bool {
		if j < 0 || j >= len(vis) {
			return false
		}
		o, ok := proj[vis[j].Path]
		return ok && o.Key == pr.Key
	}
	switch {
	case sameProject(i - 1):
		return ""
	case sameProject(i + 1):
		return pr.Label
	}
	return repos.Name(vis[i])
}

// toggleGrouped flips the grouping and keeps the cursor on the row it was on.
func (p *repoPopup) toggleGrouped() {
	p.keepSel(func() { p.grouped = !p.grouped })
}

// keepSel applies change (anything that reorders the view) and puts the
// cursor back on the row it was on.
func (p *repoPopup) keepSel(change func()) {
	at := ""
	if vis := p.visible(); p.sel >= 0 && p.sel < len(vis) {
		at = vis[p.sel].Path
	}
	change()
	p.sel = 0
	for i, e := range p.visible() {
		if e.Path == at {
			p.sel = i
			break
		}
	}
}

// update handles all keys while the picker is open. It swallows everything (no
// fallthrough to global handlers). Type-to-filter, like the ctrl+p palette and
// the . action menu: every printable key (j, k, z and / included — paths hold
// slashes) extends the query, arrows/pages move, enter switches, and esc
// clears an active filter before it closes.

func (p *repoPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// Arrows/pages move the selection live while typing (no cursor reset).
	if filterMotion(msg, p.moveSel, popupFilterPage) {
		return m, nil
	}
	// Display-mode + pan keys are chords, so they never collide with the query.
	switch msg.String() {
	case "ctrl+g":
		p.toggleGrouped()
		m.repoGrouped = p.grouped
		if m.promptStore != nil {
			// A store that refuses only costs the memory, never the flip.
			if err := m.promptStore.SetRepoGrouped(p.grouped); err != nil {
				m.statusMsg = i18n.T("could not remember the grouping choice: %s", err.Error())
			}
		}
		return m, nil
	case "ctrl+p":
		// Copy the selected row's absolute path; the switcher stays open (the
		// palette, ctrl+p elsewhere, is not reachable from a popup).
		vis := p.visible()
		if p.sel < 0 || p.sel >= len(vis) {
			return m, nil
		}
		path := vis[p.sel].Path
		return m, m.copyToClipboardCmd(i18n.T("Copied absolute path: %s", path), path)
	case "ctrl+w":
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
		// First esc clears an active filter; esc with no filter closes.
		if p.query != "" {
			p.query, p.sel = "", 0
			return m, nil
		}
		m = m.popLayer()
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH, tea.KeyDelete:
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
		}
		p.sel = 0
		return m, nil
	case tea.KeySpace:
		p.query += " "
		p.sel = 0
		return m, nil
	case tea.KeyEnter:
		vis := p.visible()
		if p.sel < 0 || p.sel >= len(vis) {
			return m.popLayer(), nil
		}
		target := vis[p.sel].Path
		if samePathTUI(target, m.currentWorktree) {
			return m.popLayer(), nil // already here
		}
		if p.foreign[target] && m.confirmSlowOps() {
			// A foreign-fs switch can block the interface for a minute (the
			// snapshot's whole-tree status walk), so it confirms like every
			// other slow op — same [ui] disable_slow_op_confirm bypass,
			// default No. The switcher stays open underneath the question:
			// No returns the user to the list they were choosing from, with
			// its filter and selection intact, instead of closing everything.
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
						return m.popLayer().guardedReRoot(target, false)
					}
					return m, nil
				},
			}
			return m, nil
		}
		tm, cmd := m.popLayer().guardedReRoot(target, false)
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
		p.query += string(msg.Runes)
		p.sel = 0
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
	return st().tooltip.Render(text), x, top - 1, true
}

// box draws the picker box (modal box only).
func (p *repoPopup) box(m Model) string {
	w, termH := m.overlayDims()
	inner := popupFullInnerWidth(w)
	textW := popupTextWidth(inner)

	header := i18n.T("Switch repository")
	if p.query != "" {
		header += "  " + p.query + "█"
	}
	if p.grouped {
		header += "  " + i18n.T("[grouped]")
	}

	vis := p.visible()
	var bodyLines []string
	if len(vis) == 0 {
		bodyLines = []string{padRight(i18n.T("  (no match)"), textW)}
	} else {
		nameW, slowW, pathW := p.tableCols(textW)
		wr := make([]winRow, len(vis))
		s := st()
		for i, e := range vis {
			marker := "  "
			if samePathTUI(e.Path, m.currentWorktree) {
				marker = "● "
			}
			prefix := "  "
			var st lipgloss.Style
			if i == p.sel {
				prefix = "> "
				st = s.selectedRow
			}
			// Table layout: name, slow-fs, path, and age each start at one
			// shared column. The slow-fs column sits between name and path
			// (the row tail is what cutoff mode loses first) and is always
			// reserved, so the async probe verdicts never shift the paths.
			slow := ""
			if p.foreign[e.Path] {
				slow = i18n.T("(slow fs)")
			}
			path := e.Path
			if p.mode == modeCutoff {
				// Cutoff keeps the table rectangular: a path wider than its
				// column is middle-elided (the leaf and the path's start
				// survive) so the age column stays on the line. Wrap/scroll
				// are the "show me everything" modes and keep the full path.
				path = elidePath(path, pathW)
			}
			row := prefix + marker +
				padRight(truncate(p.rowName(vis, i), nameW), nameW) + "  " +
				padRight(slow, slowW) + "  " +
				padRight(path, pathW) + "  (" + ageString(p.now, e.LastOpened) + ")"
			wr[i] = winRow{text: row, style: st}
		}
		// Cap the visible body; renderWindow scrolls to keep p.sel in view.
		// Wrap mode hang-indents continuations at the entry name;
		// the height budget then counts display lines, not rows.
		capRows := popupResolveRowCap(true, termH, 12)
		o := winOpts{w: textW, mode: p.mode, anchor: p.sel, hscroll: p.hscroll}
		o.h = wrapContentLines(wr, o, capRows)
		bodyLines = renderWindow(wr, o)
	}

	hint := []string{i18n.T("[enter] switch"), i18n.T("[ctrl+g] group"), i18n.T("[ctrl+p] copy path"), i18n.T("[ctrl+d] forget"), i18n.T("type to filter"), i18n.T("[ctrl+w] mode"), i18n.T("[esc] close")}
	parts := []string{header, ""}
	parts = append(parts, bodyLines...)
	parts = append(parts, "")
	parts = append(parts, wrapParts(hint, textW, "  ")...)
	return popupBox(inner, strings.Join(parts, "\n"))
}

// tableCols computes the switcher's shared column widths over ALL entries (not
// the filtered view), so the table holds still while a filter narrows the rows.
// The slow-fs column is always reserved (rows must not shift when the async
// probe verdicts land); the path column is capped to the width remaining after
// the age column, so the age survives cutoff mode even for long paths.
func (p *repoPopup) tableCols(textW int) (nameW, slowW, pathW int) {
	slowW = lipgloss.Width(i18n.T("(slow fs)"))
	ageW := 0
	proj := p.projects()
	for _, e := range p.entries {
		if w := lipgloss.Width(repos.Name(e)); w > nameW {
			nameW = w
		}
		// A group head shows the project name instead; sizing for both keeps
		// the columns still across the ctrl+g toggle.
		if pr, ok := proj[e.Path]; ok {
			if w := lipgloss.Width(pr.Label); w > nameW {
				nameW = w
			}
		}
		if w := lipgloss.Width(e.Path); w > pathW {
			pathW = w
		}
		if w := lipgloss.Width("(" + ageString(p.now, e.LastOpened) + ")"); w > ageW {
			ageW = w
		}
	}
	// 4 = the "> ● " cursor/marker prefix; a runaway name may take at most a
	// third of the remaining width before it is truncated.
	if max := (textW - 4) / 3; nameW > max {
		nameW = max
	}
	// Three inter-column gaps of 2 sit between the four fields.
	if budget := textW - 4 - nameW - slowW - ageW - 6; pathW > budget {
		pathW = budget
	}
	if pathW < 1 {
		pathW = 1
	}
	return nameW, slowW, pathW
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
