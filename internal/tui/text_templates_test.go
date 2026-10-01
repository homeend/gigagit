package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/model"
)

func ttItems(n int) []model.TextTemplate {
	out := make([]model.TextTemplate, n)
	for i := range out {
		out[i] = model.TextTemplate{ID: fmt.Sprintf("t%02d", i), Title: fmt.Sprintf("Template %02d", i), Body: fmt.Sprintf("body %02d <user:who> <date>", i)}
	}
	return out
}

func TestTextTemplatesBrowseMoveAndClamp(t *testing.T) {
	t.Parallel()
	v := &textTemplatesView{items: ttItems(3), bodyScroll: 4}
	v.update(Model{}, keyMsg("down"))
	if v.sel != 1 || v.bodyScroll != 0 {
		t.Fatalf("down: sel %d scroll %d (moving the selection resets the body scroll)", v.sel, v.bodyScroll)
	}
	v.update(Model{}, keyMsg("down"))
	v.update(Model{}, keyMsg("down"))
	if v.sel != 2 {
		t.Fatalf("sel ran past the end: %d", v.sel)
	}
	v.update(Model{}, keyMsg("up"))
	if v.sel != 1 {
		t.Fatalf("up: %d", v.sel)
	}
}

func TestTextTemplatesBoxShowsListBodyAndVariables(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 40}
	v := &textTemplatesView{items: ttItems(12), sel: 1}
	box := plain(v.box(m))
	for _, want := range []string{"Text templates", "> Template 01", "body 01 <user:who> <date>", "Variables: who", "Automatic: <date>", "[enter] fill"} {
		if !strings.Contains(box, want) {
			t.Errorf("box misses %q\n%s", want, box)
		}
	}
	// 8 list rows at most: rows 00–07 are shown, 08 is not.
	if !strings.Contains(box, "Template 07") || strings.Contains(box, "Template 08") {
		t.Errorf("list is not capped at 8 rows\n%s", box)
	}
}

// The hints sit under a blank line (popup rule), and the box fits the screen.
func TestTextTemplatesBoxFitsAndHintsHaveBlankAbove(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a long line of template text ", 12)
	items := ttItems(12)
	items[0].Body = strings.Repeat(long+"\n", 60)
	for _, size := range [][2]int{{80, 24}, {100, 40}, {60, 20}} {
		m := Model{width: size[0], height: size[1]}
		for _, maxed := range []bool{false, true} {
			v := &textTemplatesView{items: items}
			v.maximized = maxed
			lines := strings.Split(strings.TrimRight(plain(v.box(m)), "\n"), "\n")
			if len(lines) > size[1] {
				t.Errorf("%v maxed=%v: box is %d lines tall", size, maxed, len(lines))
			}
			hint := -1
			for i, l := range lines {
				if lipgloss.Width(l) > size[0] {
					t.Errorf("%v: line %d is %d wide", size, i, lipgloss.Width(l))
				}
				if hint < 0 && strings.Contains(l, "[enter] fill") {
					hint = i
				}
			}
			if hint < 1 || strings.Trim(lines[hint-1], " ║│") != "" {
				t.Errorf("%v maxed=%v: no blank line above the hints\n%s", size, maxed, strings.Join(lines, "\n"))
			}
		}
	}
}

func TestTextTemplatesBodyScrolls(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 40}
	var b strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&b, "line %02d\n", i)
	}
	v := &textTemplatesView{items: []model.TextTemplate{{ID: "a", Title: "A", Body: b.String()}}}
	if box := plain(v.box(m)); !strings.Contains(box, "line 00") || strings.Contains(box, "line 40") {
		t.Fatalf("top of the body:\n%s", box)
	}
	v.update(m, keyMsg("pgdown"))
	if box := plain(v.box(m)); strings.Contains(box, "line 00") {
		t.Fatalf("pgdown did not scroll:\n%s", box)
	}
	for i := 0; i < 20; i++ {
		v.update(m, keyMsg("pgdown"))
	}
	if box := plain(v.box(m)); !strings.Contains(box, "line 49") {
		t.Fatalf("the scroll must clamp at the end:\n%s", box)
	}
	v.update(m, keyMsg("pgup"))
	if box := plain(v.box(m)); strings.Contains(box, "line 49") {
		t.Fatalf("pgup from the clamped end did not move:\n%s", box)
	}
}

func TestTextTemplatesEmptyAndLoading(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 40}
	if box := plain((&textTemplatesView{loading: true}).box(m)); !strings.Contains(box, "(loading…)") {
		t.Errorf("loading box:\n%s", box)
	}
	if box := plain((&textTemplatesView{}).box(m)); !strings.Contains(box, "(none yet — [n] to add)") {
		t.Errorf("empty box:\n%s", box)
	}
}

func TestAltXOpensTextTemplates(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	out, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}, Alt: true})
	if layerOf[*textTemplatesView](out.(Model)) == nil || cmd == nil {
		t.Fatal("alt+x did not open the text templates window")
	}
}

func TestTextTemplatesSwallowsKeysAndEscCloses(t *testing.T) {
	t.Parallel()
	m := loadedModel(t).pushLayer(&textTemplatesView{items: ttItems(2)})
	out, _ := m.Update(keyMsg("p")) // a global key: pull
	mm := out.(Model)
	if layerOf[*textTemplatesView](mm) == nil || mm.running {
		t.Fatal("a global key leaked through the window")
	}
	out, _ = mm.Update(keyMsg("esc"))
	if layerOf[*textTemplatesView](out.(Model)) != nil {
		t.Fatal("esc did not close the window")
	}
}

func TestTextTemplatesDataMsgSelectsRow(t *testing.T) {
	t.Parallel()
	m := Model{}.pushLayer(&textTemplatesView{loading: true})
	out, _ := m.Update(textTemplatesDataMsg{items: ttItems(5), selectID: "t03", status: "saved"})
	mm := out.(Model)
	v := layerOf[*textTemplatesView](mm)
	if v.loading || v.sel != 3 || mm.statusMsg != "saved" {
		t.Fatalf("loading %v sel %d status %q", v.loading, v.sel, mm.statusMsg)
	}
	// A shrunken list clamps the cursor.
	out, _ = mm.Update(textTemplatesDataMsg{items: ttItems(2)})
	if v := layerOf[*textTemplatesView](out.(Model)); v.sel != 1 {
		t.Fatalf("sel after shrink = %d", v.sel)
	}
}
