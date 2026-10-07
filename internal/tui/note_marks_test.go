package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/theme"
)

// prNotedModel is notedModel inside PR #7's view.
func prNotedModel(t *testing.T) Model {
	t.Helper()
	m := notedModel(t)
	m.diffLayer().forgePR = 7
	return m
}

func titleOf(t *testing.T, v *diffView, id string) string {
	t.Helper()
	byLine, _ := v.noteRowIndex()
	for _, rows := range byLine {
		for _, nl := range rows {
			if nl.rootID == id && nl.kind == noteRowTop {
				return nl.text
			}
		}
	}
	t.Fatalf("no box for %s", id)
	return ""
}

func TestSyncMarksInsideAPR(t *testing.T) {
	t.Parallel()
	m := prNotedModel(t)
	v := m.diffLayer()
	v.notes[0].Sync, v.notes[0].Group = model.SyncLocal, domain.GroupMine
	v.notes[1].Sync, v.notes[1].SendErr, v.notes[1].Group = model.SyncFailed, "HTTP 502: Bad Gateway", "review:r1"
	v.relayout(0)
	if got := titleOf(t, v, "n1"); !strings.HasPrefix(got, "○ ") {
		t.Fatalf("local note title %q", got)
	}
	if got := titleOf(t, v, "n2"); !strings.HasPrefix(got, "○! ") {
		t.Fatalf("failed note title %q", got)
	}
	byLine, _ := v.noteRowIndex()
	var errRow bool
	for _, rows := range byLine {
		for _, nl := range rows {
			if nl.rootID == "n2" && nl.errRow && strings.Contains(nl.text, "HTTP 502") {
				errRow = true
			}
			if nl.rootID == "n2" && nl.group != groupSlot("review:r1") {
				t.Fatalf("row %q carries group slot %d", nl.text, nl.group)
			}
		}
	}
	if !errRow {
		t.Fatal("a failed send shows its error inside the box")
	}
}

func TestMarksOutsideAPRAreOnlyTheOnesThatNeedAttention(t *testing.T) {
	t.Parallel()
	m := notedModel(t) // forgePR 0
	v := m.diffLayer()
	v.notes[0].Sync, v.notes[0].Group = model.SyncLocal, domain.GroupMine
	v.notes[1].Sync, v.notes[1].Group = model.SyncSending, domain.GroupMine
	v.relayout(0)
	if got := titleOf(t, v, "n1"); strings.HasPrefix(got, "○") {
		t.Fatalf("a plain local note outside a PR got a mark: %q", got)
	}
	if got := titleOf(t, v, "n2"); !strings.HasPrefix(got, "◌ ") {
		t.Fatalf("a note being sent shows ◌ everywhere: %q", got)
	}
	byLine, _ := v.noteRowIndex()
	for _, rows := range byLine {
		for _, nl := range rows {
			if nl.group != 0 {
				t.Fatalf("a group bar outside a PR: %+v", nl)
			}
		}
	}
}

func TestCarriedNoteNamesItsOrigin(t *testing.T) {
	t.Parallel()
	m := prNotedModel(t)
	v := m.diffLayer()
	v.notes[0].Origin = "a1b2c3d"
	v.notes[1].Origin = domain.OriginWorkingTree
	v.relayout(0)
	if got := titleOf(t, v, "n1"); !strings.HasSuffix(got, "· from a1b2c3d") {
		t.Fatalf("title %q", got)
	}
	if got := titleOf(t, v, "n2"); !strings.HasSuffix(got, "· from working tree") {
		t.Fatalf("title %q", got)
	}
}

// Sets the colour profile and theme (process-global): serial.
func TestGroupBarPaintsTheLeftFrameColumn(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	prevTheme := activeTheme()
	defer setTheme(prevTheme)
	setTheme(theme.Dark)
	slot := groupSlot("review:r1")
	if bar, _ := groupBarStyle(slot); bar.Render("│") == st().noteFrameUser.Render("│") {
		t.Fatal("the bar colour is indistinguishable from the frame")
	}
	bar, _ := groupBarStyle(slot)
	cell := noteBoxCell(noteLine{kind: noteRowSummary, text: "x", group: slot}, 20)
	if !strings.HasPrefix(cell, bar.Render("│")) {
		t.Fatalf("the left frame column is not the group's colour: %q", cell)
	}
	if ansi.StringWidth(cell) != 20 {
		t.Fatalf("width %d", ansi.StringWidth(cell))
	}
}
