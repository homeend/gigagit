package tui

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
)

// lineJumpMaxDigits caps what the prompt takes: no file gg shows reaches ten
// million lines, and an unbounded run would overflow strconv.Atoi.
const lineJumpMaxDigits = 7

// lineJump is one host's "go to line" prompt (spec 2026-10-23-go-to-line):
// : opens it, digits build the number, enter jumps, esc cancels. It is the
// in-view search's digits-only twin — the host gives it first refusal while it
// is typed and lands the line with its own recipe.
type lineJump struct {
	typing bool
	digits string
}

type jumpEvent int

const (
	jumpNotOurs   jumpEvent = iota // not typing: the host handles the key
	jumpChanged                    // a digit added or removed
	jumpIgnored                    // swallowed: while typing every key is the prompt's
	jumpCancelled                  // esc, or enter on nothing / 0: the view stays as it was
	jumpGo                         // enter on a line number: land it
)

// jumpOpenKey reports whether msg opens the prompt.
func jumpOpenKey(msg tea.KeyMsg) bool { return msg.String() == ":" }

// open starts an empty prompt.
func (j *lineJump) open() {
	j.typing = true
	j.digits = ""
}

func (j *lineJump) close() {
	j.typing = false
	j.digits = ""
}

// key handles one key while the prompt is typed; the int is the line on
// jumpGo.
func (j *lineJump) key(msg tea.KeyMsg) (jumpEvent, int) {
	if !j.typing {
		return jumpNotOurs, 0
	}
	switch msg.Type {
	case tea.KeyEsc:
		j.close()
		return jumpCancelled, 0
	case tea.KeyEnter:
		n, _ := strconv.Atoi(j.digits)
		j.close()
		if n < 1 {
			return jumpCancelled, 0
		}
		return jumpGo, n
	case tea.KeyBackspace, tea.KeyCtrlH, tea.KeyDelete:
		if j.digits != "" {
			j.digits = j.digits[:len(j.digits)-1]
		}
		return jumpChanged, 0
	case tea.KeyRunes:
		changed := false
		for _, r := range msg.Runes {
			if r < '0' || r > '9' || len(j.digits) >= lineJumpMaxDigits {
				continue
			}
			j.digits += string(r)
			changed = true
		}
		if changed {
			return jumpChanged, 0
		}
	}
	return jumpIgnored, 0
}

// badge is the header status while the prompt is typed: ":42█". Built WITHOUT
// i18n.T, like textSearch.badge — punctuation and digits only.
func (j lineJump) badge() string {
	if !j.typing {
		return ""
	}
	return ":" + j.digits + "█"
}
