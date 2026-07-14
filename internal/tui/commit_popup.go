package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/i18n"
)

// commitPopup collects a commit message as a subject (title) plus an optional
// multi-line body (description), and commits the staged index on ctrl+s.
// amend=true rewrites the last commit instead of creating a new one.
type commitPopup struct {
	popupMax
	title textfield
	desc  textfield
	field int // 0 = title, 1 = description
	amend bool

	generating bool               // a ctrl+g generate run (commit_generate.go) is in flight
	genGen     int                // generation guard: bumped on every dispatch AND every esc-cancel
	genCmd     config.ToolCommand // the commit_message tool the last/current generate run used
	spinFrame  int                // animated-spinner frame while generating (advanced by genSpinMsg)
	genStart   time.Time          // when the current generate run began, for the elapsed counter

	// Task 7 gates, run in order ahead of dispatch (see commit_generate.go's
	// startGenerate). Each is a commitPopup sub-state (it owns keys while
	// open, NOT a pushed layer) and is mutually exclusive with the others —
	// at most one is non-empty/non-nil at a time.
	choosing   []config.ToolCommand // >1 commit_message tool: numbered picker
	approving  string               // first-run approval: the resolved command awaiting Run/Cancel
	confirming string               // existing title/desc text: the resolved command awaiting Replace/Cancel
}

// message assembles the git commit message: subject alone, or subject + blank
// line + body when the body is non-empty.
func (p *commitPopup) message() string {
	t := strings.TrimSpace(p.title.Value())
	if strings.TrimSpace(p.desc.Value()) == "" {
		return t
	}
	return t + "\n\n" + p.desc.Value()
}

// splitMessage parses an existing commit message into (subject, body) for the
// amend pre-fill: the first line is the subject, the rest (after blank lines)
// the body.
func splitMessage(msg string) (title, desc string) {
	return exttool.SplitMessage(msg)
}

// applyEditKey applies one key to the popup's title/description fields and
// reports control outcomes: submit=true on ctrl+s, cancel=true on esc. Editing
// keys (tab/enter/backspace/space/runes) mutate in place and return false,false.
// ctrl+c is handled by the caller (it quits the program). Reused by F2's commit
// popup and the interactive-rebase editor's reword sub-mode.
func (p *commitPopup) applyEditKey(msg tea.KeyMsg) (submit, cancel bool) {
	switch msg.Type {
	case tea.KeyEsc:
		return false, true
	case tea.KeyCtrlS:
		return true, false
	case tea.KeyTab, tea.KeyShiftTab:
		p.field = (p.field + 1) % 2
		return false, false
	case tea.KeyEnter:
		if p.field == 0 {
			p.field = 1 // title → description
		} else {
			p.desc.InsertNewline() // newline within the body
		}
		return false, false
	case tea.KeyUp:
		if p.field == 1 {
			p.desc.Up()
		}
		return false, false
	case tea.KeyDown:
		if p.field == 1 {
			p.desc.Down()
		}
		return false, false
	}
	if p.field == 0 {
		p.title.HandleEditKey(msg)
	} else {
		p.desc.HandleEditKey(msg)
	}
	return false, false
}

// update handles one key while the commit popup is open. It swallows every key:
// esc cancels, ctrl+c quits, ctrl+s commits. ctrl+g (generate) is handled here,
// NOT in applyEditKey — reword/irebase's reword sub-mode share applyEditKey and
// have no staged index to generate a message from.
func (p *commitPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// ctrl+t (fullscreen) is handled centrally on the layer stack via popupMax
	// (this popup embeds it), so it never reaches update — do NOT handle it here.
	// Task 7 gates: each is a commitPopup sub-state that owns keys while
	// open. Checked before generating/edit keys so a digit/y/esc typed here
	// never falls through to field editing.
	if p.choosing != nil {
		return p.updateChoosing(m, msg)
	}
	if p.approving != "" {
		return p.updateApproving(m, msg)
	}
	if p.confirming != "" {
		return p.updateConfirming(m, msg)
	}
	if p.generating {
		if msg.Type == tea.KeyEsc {
			return m.escGenerate(p), nil
		}
		return m, nil // swallow every other key while a generate run is in flight
	}
	if msg.Type == tea.KeyCtrlG {
		return m.startGenerate(p)
	}
	submit, cancel := p.applyEditKey(msg)
	switch {
	case cancel:
		m = m.popLayer()
	case submit:
		if strings.TrimSpace(p.title.Value()) == "" {
			m.statusMsg = i18n.T("title required")
			return m, nil
		}
		op := engine.Commit{Message: p.message(), Amend: p.amend}
		m = m.popLayer()
		return m.startOp(op)
	}
	return m, nil
}

// render composites the commit dialog over the layer beneath.
func (p *commitPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// box draws the two-field commit dialog (modal box only). While a Task 7
// gate is open, it takes over the whole box (a distinct sub-screen, like the
// generate run itself) rather than being appended below the fields.
func (p *commitPopup) box(m Model) string {
	if p.choosing != nil {
		return p.chooseBox(m)
	}
	if p.approving != "" {
		return p.approveBox(m)
	}
	if p.confirming != "" {
		return p.confirmBox(m)
	}
	var b strings.Builder
	heading := i18n.T("Commit")
	if p.amend {
		heading = i18n.T("Amend last commit")
	}
	w, _ := m.overlayDims()
	// Wider-than-standard default (commitNormalWidth); ctrl+t maximizes to
	// popupFullInnerWidth via the shared popupMax mechanism (popupResolveWidth).
	innerW := popupResolveWidth(w, p.maximized, commitNormalWidth(w))
	contentW := innerW - modalStyle.GetHorizontalPadding()
	if contentW < 1 {
		contentW = 1
	}
	b.WriteString(heading + "\n\n")
	b.WriteString(renderCommitFields(p, contentW))
	b.WriteString("\n")
	if p.generating {
		// While a run is in flight every key but esc is swallowed, so show an
		// animated spinner + elapsed seconds (a clear "still working" signal)
		// and the cancel hint, not the full (inert) key list.
		frames := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
		frame := frames[p.spinFrame%len(frames)]
		elapsed := int(time.Since(p.genStart).Seconds())
		b.WriteString(i18n.T("%c generating message… %ds  ([esc] to cancel)", frame, elapsed))
	} else {
		b.WriteString(packHints([]string{
			i18n.T("[tab] switch field"),
			i18n.T("[enter] newline/next"),
			i18n.T("[ctrl+g] generate"),
			i18n.T("[ctrl+t] fullscreen"),
			i18n.T("[ctrl+s] commit"),
			i18n.T("[esc] cancel"),
		}, contentW))
	}

	return modalStyle.Width(innerW).Render(b.String()) + "\n"
}

// commitNormalWidth is the commit popup's NON-maximized inner width: wider than
// the shared 56-column popup (so a generated message needs fewer wrapped lines),
// capped for readability. ctrl+t maximizing is handled centrally via popupMax /
// popupResolveWidth, which widens to popupFullInnerWidth.
func commitNormalWidth(termW int) int {
	iw := termW - 8
	if iw > 96 {
		iw = 96
	}
	if iw < 40 {
		iw = 40
	}
	return iw
}

// packHints joins "[key] label" hint pairs into lines no wider than width,
// breaking ONLY between pairs so a key is never split from its label by a
// naive wrap (which reads as a dangling "[ctrl+g]" over "generate"). The pairs
// are ASCII, so byte length is display width.
func packHints(pairs []string, width int) string {
	var b strings.Builder
	line := 0
	for i, p := range pairs {
		switch {
		case i == 0:
			b.WriteString(p)
			line = len(p)
		case line+2+len(p) > width:
			b.WriteString("\n")
			b.WriteString(p)
			line = len(p)
		default:
			b.WriteString("  ")
			b.WriteString(p)
			line += 2 + len(p)
		}
	}
	return b.String()
}

// renderCommitFields draws the title/description fields with the focus cursor,
// each on a visible editable background filling contentWidth. The description's
// continuation lines align under its first line (shared viewField indent).
func renderCommitFields(p *commitPopup, contentWidth int) string {
	titleCur, descCur := "  ", "  "
	if p.field == 0 {
		titleCur = "> "
	} else {
		descCur = "> "
	}
	var b strings.Builder
	b.WriteString(viewField(titleCur+"title:       ", p.title, p.field == 0, contentWidth) + "\n")
	b.WriteString(viewField(descCur+"description: ", p.desc, p.field == 1, contentWidth) + "\n")
	return b.String()
}
