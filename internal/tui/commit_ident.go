package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/model"
)

// wipNodeGlyph marks a pseudo-row's node in the graph/list (hollow ◇, vs a real
// commit's ●).
const wipNodeGlyph = "◇"

// text renders a pseudo-row body, e.g. "Working tree (2)" / "Staged (1)".
func (r wipRow) text() string {
	return fmt.Sprintf("%s (%d)", r.label(), r.count)
}

// commitIdentW is the fixed display width of the commit-row identity column
// (the branch-name column that replaces the old 7-char commit id). Fixed so the
// column — and every subject after it — does not reflow as commits page in.
const commitIdentW = 16

// dimIdentStyle grays a lineage row's branch name (the commit belongs to that
// branch but is not its tip). 240 is a mid-gray in the 256-color cube.
var dimIdentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

// dimRowStyle grays an entire commit row that does NOT match the active
// @-highlight query, de-emphasizing it while keeping it visible.
var dimRowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

// dimRowDecorator dims a whole visible line (all visual lines, including wrap
// continuations). Width is preserved (a foreground style adds no cells).
func dimRowDecorator() rowDecorator {
	return func(visible string, hscroll, visualLine int) string {
		return dimRowStyle.Render(visible)
	}
}

// commitIdent is a commit row's branch-identity readout: the branch the row
// represents — BRIGHT when this commit is that branch's tip ("the last commit
// for a given branch"), GRAY when the commit is only that branch's lineage —
// plus any additional branch tips at the same commit (rendered as pills).
type commitIdent struct {
	name  string   // branch name (no * marker); "" when the commit has none
	tip   bool     // this commit is a local branch's tip
	head  bool     // the chosen branch is the current (HEAD) branch
	extra []string // additional local-branch tips at this commit (multi-tip)
}

// commitIdentOf derives the identity from a commit's local refs (a tip) or, when
// it decorates none, from its Source branch (lineage; from `git log --source`).
func commitIdentOf(c model.Commit) commitIdent {
	var locals []model.Ref
	for _, r := range c.Refs {
		if r.Kind == model.RefLocal {
			locals = append(locals, r)
		}
	}
	if len(locals) == 0 {
		return commitIdent{name: c.Source, tip: false}
	}
	pick := 0
	for i, r := range locals {
		if r.Head {
			pick = i
			break
		}
	}
	id := commitIdent{name: locals[pick].Name, tip: true, head: locals[pick].Head}
	for i, r := range locals {
		if i != pick {
			id.extra = append(id.extra, r.Name)
		}
	}
	return id
}

// label is the identity name with a leading "*" for the current branch; "" when
// the commit has no identity at all.
func (id commitIdent) label() string {
	if id.name == "" {
		return ""
	}
	if id.head {
		return "*" + id.name
	}
	return id.name
}

// token is the display token at width w: trimmed with … when the label is too
// long, else right-padded so subjects stay aligned. trimmed reports whether
// truncation happened (drives the reveal tooltip). w is the dynamic identity
// column width (see Model.commitIdentWidth), never more than commitIdentW.
func (id commitIdent) token(w int) (text string, trimmed bool) {
	s := id.label()
	if lipgloss.Width(s) > w {
		return truncate(s, w), true
	}
	return padRight(s, w), false
}

// fullToken is the UNtrimmed label, right-padded to width w. The tooltip's
// WHEN-to-reveal gate compares a row built with this against the trimmed row.
func (id commitIdent) fullToken(w int) string {
	return padRight(id.label(), w)
}

// pills renders additional-branch tips (the multi-tip case) as ‹name› chips; the
// primary branch already shows in the identity column. Empty in the common case.
func (id commitIdent) pills() string {
	var b strings.Builder
	for _, n := range id.extra {
		b.WriteString("‹" + n + "› ")
	}
	return b.String()
}

// commitLineDecorator restyles one visible commit line in a SINGLE pass over its
// original runes: it colors the lane '●' node (when hasDot) and dims the identity
// column's rune range (when dim). Doing both in one pass over the unstyled string
// avoids the ANSI rune-index drift that chaining two decorators would cause. All
// columns are content columns (the visible line already had hscroll applied, so
// content col of rune i is i+hscroll). Width is preserved.
func commitLineDecorator(hasDot bool, dotCol int, dotColor lipgloss.Color, dim bool, identStart, identLen int) rowDecorator {
	dotStyle := lipgloss.NewStyle().Foreground(dotColor)
	return func(visible string, hscroll, visualLine int) string {
		if visualLine != 0 {
			return visible // decorate only a row's first visual line
		}
		r := []rune(visible)
		var b strings.Builder
		i := 0
		for i < len(r) {
			col := i + hscroll
			if dim && col >= identStart && col < identStart+identLen {
				j := i
				for j < len(r) {
					c := j + hscroll
					if c < identStart || c >= identStart+identLen {
						break
					}
					j++
				}
				b.WriteString(dimIdentStyle.Render(string(r[i:j])))
				i = j
				continue
			}
			if hasDot && col == dotCol && r[i] == '●' {
				b.WriteString(dotStyle.Render(string(r[i])))
				i++
				continue
			}
			b.WriteRune(r[i])
			i++
		}
		return b.String()
	}
}
