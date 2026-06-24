package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/model"
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

// commitMarkerW is the display width of the tip-marker prefix on a commit
// identity token: two glyph cells (local ■, remote ▲) plus one separator space.
const commitMarkerW = 3

const (
	markerLocal  = "■" // tip of a local branch
	markerRemote = "▲" // tip of a tracked remote (a local branch's upstream)
)

// markers is the 2-cell, left-packed marker field for this identity: present
// markers fill from the left, missing slots are spaces, so the field is always
// exactly two display cells wide.
func (id commitIdent) markers() string {
	switch {
	case id.tip && id.remoteTip:
		return markerLocal + markerRemote
	case id.tip:
		return markerLocal + " "
	case id.remoteTip:
		return markerRemote + " "
	default:
		return "  "
	}
}

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
	name      string   // branch name (no * marker); "" when the commit has none
	tip       bool     // this commit is a local branch's tip
	remoteTip bool     // this commit is the tip of a tracked remote (upstream of a local branch)
	head      bool     // the chosen branch is the current (HEAD) branch
	extra     []string // additional local-branch tips at this commit (multi-tip)
}

// commitIdentOf derives the identity from a commit's local refs (a tip) or, when
// it decorates none, from its Source branch (lineage; from `git log --source`).
// tracked maps an upstream short ref ("origin/main") to the local branch that
// tracks it; a RefRemote in that set marks the row as a tracked remote tip. A
// nil map disables remote-tip detection.
func commitIdentOf(c model.Commit, tracked map[string]string) commitIdent {
	var locals []model.Ref
	var remoteTipName string // local branch name behind a tracked remote tip here
	for _, r := range c.Refs {
		switch r.Kind {
		case model.RefLocal:
			locals = append(locals, r)
		case model.RefRemote:
			if name, ok := tracked[r.Name]; ok {
				remoteTipName = name
			}
		}
	}
	if len(locals) == 0 {
		if remoteTipName != "" {
			return commitIdent{name: remoteTipName, remoteTip: true}
		}
		return commitIdent{name: c.Source, tip: false}
	}
	pick := 0
	for i, r := range locals {
		if r.Head {
			pick = i
			break
		}
	}
	id := commitIdent{name: locals[pick].Name, tip: true, head: locals[pick].Head, remoteTip: remoteTipName != ""}
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

// token is the display token at width commitMarkerW+w: the marker prefix, a
// separator space, then the name trimmed with … when too long, else right-padded
// so subjects stay aligned. trimmed reports whether the NAME was truncated.
func (id commitIdent) token(w int) (text string, trimmed bool) {
	name := id.label()
	var body string
	if lipgloss.Width(name) > w {
		body, trimmed = truncate(name, w), true
	} else {
		body = padRight(name, w)
	}
	return id.markers() + " " + body, trimmed
}

// fullToken is the UNtrimmed label with the marker prefix, padded to commitMarkerW+w.
func (id commitIdent) fullToken(w int) string {
	return id.markers() + " " + padRight(id.label(), w)
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
