package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

// commitNamePopup collects a human name when shelving or bookmarking a commit.
// The field is pre-filled with the commit subject; ctrl+s inserts the short sha
// at the cursor; enter creates the shelf entry / bookmark with the name; esc
// cancels. forShelf routes to the shelf vs bookmark create command.
type commitNamePopup struct {
	popupMax
	commit   model.Commit
	forShelf bool
	name     textfield
}

func (p *commitNamePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyCtrlS:
		sha := p.commit.Hash
		if len(sha) > 7 {
			sha = sha[:7]
		}
		p.name.insert([]rune(sha))
		return m, nil
	case tea.KeyEnter:
		label := strings.TrimSpace(p.name.Value())
		c, forShelf := p.commit, p.forShelf
		m = m.popLayer()
		if forShelf {
			return m, m.shelfAddCommitCmd(c.Hash, label)
		}
		return m, m.bookmarkAddCmd(commitBookmark(c, label))
	default:
		p.name.HandleEditKey(msg)
	}
	return m, nil
}

func (p *commitNamePopup) render(m Model, below string) string {
	title := "Bookmark this commit"
	verb := "bookmark"
	if p.forShelf {
		title, verb = "Shelf this commit", "shelf"
	}
	w, h := m.overlayDims()
	var b strings.Builder
	b.WriteString(title + "\n\n")
	b.WriteString(viewField("name: ", p.name, true, popupContentWidth(w)) + "\n\n")
	b.WriteString("[ctrl+s] insert sha   [enter] " + verb + "   [esc] cancel")
	box := modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}
