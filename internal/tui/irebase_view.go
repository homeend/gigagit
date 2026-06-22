package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/rebaseplan"
)

// irebaseRow is one editable commit in the editor (displayed newest-first).
type irebaseRow struct {
	sha     string
	subject string
	orig    string // original full message
	action  rebaseplan.Action
	newMsg  string // reword: the new message
}

// irebaseEditor is the GitKraken-style interactive-rebase surface. Rows are
// newest-first for display; plan() reverses to git todo order (oldest-first).
type irebaseEditor struct {
	branch, onto string
	ggBin        string
	rows         []irebaseRow
	orig         []irebaseRow // for Reset
	sel          int
	reword       *commitPopup // non-nil while editing a reword message
}

// newIrebaseEditor builds the editor from oldest-first range commits.
func newIrebaseEditor(branch, onto string, commits []model.RangeCommit, ggBin string) *irebaseEditor {
	rows := make([]irebaseRow, 0, len(commits))
	for i := len(commits) - 1; i >= 0; i-- { // reverse → newest-first
		c := commits[i]
		rows = append(rows, irebaseRow{sha: c.Hash, subject: c.Subject, orig: c.Message, action: rebaseplan.Pick})
	}
	orig := append([]irebaseRow(nil), rows...)
	return &irebaseEditor{branch: branch, onto: onto, ggBin: ggBin, rows: rows, orig: orig}
}

// plan reverses the newest-first rows back to git todo order (oldest-first).
func (e *irebaseEditor) plan() rebaseplan.Plan {
	entries := make([]rebaseplan.Entry, 0, len(e.rows))
	for i := len(e.rows) - 1; i >= 0; i-- {
		r := e.rows[i]
		entries = append(entries, rebaseplan.Entry{Sha: r.sha, Action: r.action, Orig: r.orig, NewMsg: r.newMsg})
	}
	return rebaseplan.Plan{Entries: entries}
}

func (e *irebaseEditor) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// Reword sub-mode owns input while open.
	if e.reword != nil {
		submit, cancel := e.reword.applyEditKey(msg)
		switch {
		case cancel:
			e.reword = nil
		case submit:
			if strings.TrimSpace(e.reword.title.Value()) == "" {
				m.statusMsg = "title required"
				return m, nil
			}
			e.rows[e.sel].action = rebaseplan.Reword
			e.rows[e.sel].newMsg = e.reword.message()
			e.reword = nil
		}
		return m, nil
	}
	switch msg.String() {
	case "esc":
		return m.popLayer(), nil
	case "down", "j":
		if e.sel < len(e.rows)-1 {
			e.sel++
		}
	case "up", "k":
		if e.sel > 0 {
			e.sel--
		}
	case "p":
		e.rows[e.sel].action = rebaseplan.Pick
	case "d":
		e.rows[e.sel].action = rebaseplan.Drop
	case "s":
		// Squash melds into the older neighbor (the row below, newest-first).
		// The oldest row (last) has nothing older — refuse.
		if e.sel == len(e.rows)-1 {
			m.statusMsg = "squash: the oldest commit has nothing to squash into"
			return m, nil
		}
		e.rows[e.sel].action = rebaseplan.Squash
	case "r":
		t, d := splitMessage(e.rows[e.sel].orig)
		if e.rows[e.sel].action == rebaseplan.Reword && e.rows[e.sel].newMsg != "" {
			t, d = splitMessage(e.rows[e.sel].newMsg)
		}
		e.reword = &commitPopup{title: newTextField(t), desc: newTextField(d)}
	case "ctrl+up":
		if e.sel > 0 {
			e.rows[e.sel-1], e.rows[e.sel] = e.rows[e.sel], e.rows[e.sel-1]
			e.sel--
		}
	case "ctrl+down":
		if e.sel < len(e.rows)-1 {
			e.rows[e.sel+1], e.rows[e.sel] = e.rows[e.sel], e.rows[e.sel+1]
			e.sel++
		}
	case "R":
		e.rows = append([]irebaseRow(nil), e.orig...)
		e.sel = 0
	case "enter":
		op := engine.InteractiveRebase{Branch: e.branch, Onto: e.onto, Plan: e.plan(), GGBin: e.ggBin}
		m = m.popLayer()
		return m.startOp(op)
	}
	return m, nil
}

func (e *irebaseEditor) render(m Model, _ string) string {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	var b strings.Builder
	b.WriteString("Interactive rebase: " + e.branch + " onto " + e.onto + "\n\n")
	for i, r := range e.rows {
		cur := "  "
		if i == e.sel {
			cur = "> "
		}
		action := padRight("["+string(r.action)+"]", 10)
		subj := r.subject
		if r.action == rebaseplan.Reword && r.newMsg != "" {
			first, _ := splitMessage(r.newMsg)
			subj = first + "  (reworded)"
		}
		line := cur + action + " " + shortHash(r.sha) + "  " + subj
		if i == e.sel {
			b.WriteString(selectedRow.Render(truncate(line, w)))
		} else {
			b.WriteString(truncate(line, w))
		}
		b.WriteString("\n")
	}
	if e.reword != nil {
		b.WriteString("\nReword:\n")
		b.WriteString(renderCommitFields(e.reword, popupContentWidth(w)))
		b.WriteString("\n[tab] switch field  [enter] newline/next  [ctrl+s] set  [esc] cancel")
	} else {
		b.WriteString("\n[p]ick [r]eword [s]quash [d]rop  [ctrl+↑/↓] move  [enter] start  [R]eset  [esc] cancel")
	}
	return b.String()
}
