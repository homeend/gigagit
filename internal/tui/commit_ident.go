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
// Uses titleLabel() (translated), not label() (the English identity sentinel).
func (r wipRow) text() string {
	return fmt.Sprintf("%s (%d)", r.titleLabel(), r.count)
}

// commitIdentW is the fixed display width of the commit-row identity column
// (the branch-name column that replaces the old 7-char commit id). Fixed so the
// column — and every subject after it — does not reflow as commits page in.
const commitIdentW = 16

// commitMarkerW is the display width of the tip-marker prefix on a commit
// identity token: two glyph cells (local ↓, remote ↑) plus one separator space.
const commitMarkerW = 3

const (
	markerLocal  = "↓" // tip of a local branch (pull-down)
	markerRemote = "↑" // tip of a tracked remote — a local branch's upstream (push-up)
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

// countBadge is the superscript count shown on a multi-tip marker (≥2 local
// tips): ²–⁹, or ⁺ for ≥10. Empty for <2. Always one display cell when present.
func countBadge(n int) string {
	if n < 2 {
		return ""
	}
	const sup = "⁰¹²³⁴⁵⁶⁷⁸⁹"
	supRunes := []rune(sup)
	if n >= 10 {
		return "⁺"
	}
	return string(supRunes[n])
}

// markerField is the fixed 3-cell marker area, laid out as
// [marker1][marker2-or-badge][separator]. The count badge (≥2 local tips) fills
// the FILLER cell next to a lone ↓ so it reads "↓³ "; when BOTH a local and a
// remote marker are present there is no room, so the badge is dropped (the count
// still shows via the decoration group / (+N)). Always exactly commitMarkerW (3)
// display cells.
func (id commitIdent) markerField() string {
	badge := countBadge(id.count) // "" when <2
	switch {
	case id.tip && id.remoteTip:
		return markerLocal + markerRemote + " " // "↓↑ " — no room for the badge
	case id.tip:
		if badge == "" {
			return markerLocal + "  " // "↓  "
		}
		return markerLocal + badge + " " // "↓³ " — badge in the filler cell
	case id.remoteTip:
		return markerRemote + "  " // "↑  "
	default:
		return "   " // lineage row
	}
}

// dimIdentStyle grays a lineage row's branch name (the commit belongs to that
// branch but is not its tip). 240 is a mid-gray in the 256-color cube.
var dimIdentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

// tagDecoStyle colors a tag label (⊙name) in the commit-row decoration group
// yellow. Must match the tagColorStyle probe used in commit_deco_color_test.go.
var tagDecoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))

// coloredSpan marks a rune range (absolute content columns, pre-hscroll, like
// identStart) to recolor via a specific lipgloss Style in the single-pass
// commitLineDecorator. Spans must not overlap each other or the dim range.
type coloredSpan struct {
	Start, Length int
	Style         lipgloss.Style
}

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
	tags      []string // tag names at this commit (RefTag), rendered in the deco group
	count     int      // number of local-branch tips at this commit (for the count badge)
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
	var tags []string
	for _, r := range c.Refs {
		if r.Kind == model.RefTag {
			tags = append(tags, r.Name)
		}
	}
	if len(locals) == 0 {
		if remoteTipName != "" {
			return commitIdent{name: remoteTipName, remoteTip: true, tags: tags}
		}
		return commitIdent{name: c.Source, tip: false, tags: tags}
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
	id.tags = tags
	id.count = len(locals)
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
	return id.markerField() + body, trimmed
}

// fullToken is the UNtrimmed label with the marker prefix, padded to commitMarkerW+w.
func (id commitIdent) fullToken(w int) string {
	return id.markerField() + padRight(id.label(), w)
}

const tagGlyph = "⊙" // precedes a tag name in the commit-row decoration group

type decoSpan struct{ Offset, Length int }

// commitDecoGroup renders the before-subject decoration group for an identity:
// " (branch1, branch2, ⊙v1.0.0)" with extra branches first then tags. Returns the
// group string (with a single leading space, "" when there are no extras/tags),
// the rune spans of each ⊙tag label (relative to the group string start) for the
// decorator to color yellow, and whether it collapsed to " (+N)". budget < 0
// means never collapse (full mode). When the natural group width exceeds budget
// it collapses to " (+N)" (N = extras+tags) and tagSpans is nil.
func commitDecoGroup(id commitIdent, budget int) (string, []decoSpan, bool) {
	n := len(id.extra) + len(id.tags)
	if n == 0 {
		return "", nil, false
	}
	// Build the full group and record tag spans (rune offsets within the string).
	var b strings.Builder
	b.WriteString(" (")
	var spans []decoSpan
	first := true
	write := func(s string, isTag bool) {
		if !first {
			b.WriteString(", ")
		}
		first = false
		if isTag {
			start := len([]rune(b.String()))
			lbl := tagGlyph + s
			b.WriteString(lbl)
			spans = append(spans, decoSpan{Offset: start, Length: len([]rune(lbl))})
		} else {
			b.WriteString(s)
		}
	}
	for _, br := range id.extra {
		write(br, false)
	}
	for _, tg := range id.tags {
		write(tg, true)
	}
	b.WriteString(")")
	full := b.String()
	if budget < 0 || lipgloss.Width(full) <= budget {
		return full, spans, false
	}
	return fmt.Sprintf(" (+%d)", n), nil, true
}

// commitLineDecorator restyles one visible commit line in a SINGLE pass over its
// original runes: it dims the identity column (when dim), colors the lane '●'
// node (when hasDot), and recolors any tag-label spans (colorSpans). Doing all
// three in one pass over the unstyled string avoids ANSI rune-index drift that
// chaining decorators would cause. All columns are content columns (pre-hscroll;
// content col of rune i is i+hscroll). Width is preserved — styles add no cells.
// colorSpans are absolute content columns (like identStart). They must not
// overlap the identity dim range or each other.
func commitLineDecorator(hasDot bool, dotCol int, dotColor lipgloss.Color, dim bool, identStart, identLen int, colorSpans []coloredSpan) rowDecorator {
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
			// Dim the identity (name) column on lineage rows.
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
			// Color any tag-label spans (yellow ⊙name in the deco group).
			colored := false
			for _, cs := range colorSpans {
				if col >= cs.Start && col < cs.Start+cs.Length {
					j := i
					for j < len(r) {
						c := j + hscroll
						if c < cs.Start || c >= cs.Start+cs.Length {
							break
						}
						j++
					}
					b.WriteString(cs.Style.Render(string(r[i:j])))
					i = j
					colored = true
					break
				}
			}
			if colored {
				continue
			}
			// Color the graph lane node (●).
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
