package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/repos"
)

// repoPopup is the transient repo-switcher picker opened with R. It holds an
// MRU snapshot taken at open; ctrl+d edits both the snapshot and the registry.
type repoPopup struct {
	entries []repos.Entry
	query   string // case-insensitive substring over name+path
	sel     int    // index into the FILTERED view
	now     time.Time
}

// openRepoPopup snapshots the registry. With no known repos it sets a status
// hint instead of opening an empty picker.
func (m Model) openRepoPopup() (Model, bool) {
	entries := repos.Load(m.statePath)
	if len(entries) == 0 {
		m.statusMsg = "no known repositories yet (gg records them as you open repos)"
		return m, false
	}
	m.repoPopup = &repoPopup{entries: entries, now: time.Now()}
	return m, true
}

// popupVisible returns the filtered entries in display order.
func (m Model) popupVisible() []repos.Entry {
	p := m.repoPopup
	if p == nil {
		return nil
	}
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

// updateRepoPopupKey handles all keys while the picker is open. It swallows
// everything (no fallthrough to global handlers).
func (m Model) updateRepoPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.repoPopup
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.repoPopup = nil
		return m, nil
	case tea.KeyUp:
		if p.sel > 0 {
			p.sel--
		}
		return m, nil
	case tea.KeyDown:
		if p.sel < len(m.popupVisible())-1 {
			p.sel++
		}
		return m, nil
	case tea.KeyEnter:
		vis := m.popupVisible()
		m.repoPopup = nil
		if p.sel < 0 || p.sel >= len(vis) {
			return m, nil
		}
		target := vis[p.sel].Path
		if samePathTUI(target, m.currentWorktree) {
			return m, nil // already here
		}
		return m.reRoot(target)
	case tea.KeyCtrlD:
		vis := m.popupVisible()
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
		if n := len(m.popupVisible()); p.sel >= n && n > 0 {
			p.sel = n - 1
		}
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
		}
		p.sel = 0
		return m, nil
	case tea.KeySpace:
		p.query += " "
		p.sel = 0
		return m, nil
	case tea.KeyRunes:
		p.query += string(msg.Runes)
		p.sel = 0
		return m, nil
	}
	return m, nil
}

// renderRepoPopup draws the picker box (composited by render via overlayCenter).
func (m Model) renderRepoPopup() string {
	p := m.repoPopup
	var b strings.Builder
	b.WriteString("Switch repository")
	if p.query != "" {
		b.WriteString("  /" + p.query)
	}
	b.WriteString("\n\n")
	vis := m.popupVisible()
	if len(vis) == 0 {
		b.WriteString("  (no match)\n")
	}
	for i, e := range vis {
		marker := "  "
		if samePathTUI(e.Path, m.currentWorktree) {
			marker = "● "
		}
		cursor := "  "
		if i == p.sel {
			cursor = "> "
		}
		b.WriteString(fmt.Sprintf("%s%s%s  %s  (%s)\n",
			cursor, marker, repos.Name(e), e.Path, ageString(p.now, e.LastOpened)))
	}
	b.WriteString("\n[enter] switch  [ctrl+d] forget  [esc] cancel")

	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	return modalStyle.Width(inner).Render(strings.TrimRight(b.String(), "\n")) + "\n"
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
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
