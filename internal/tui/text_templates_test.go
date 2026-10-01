package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

func TestTextTemplatesEnterWithVariablesOpensFill(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 40}
	v := &textTemplatesView{items: []model.TextTemplate{{ID: "a", Title: "A", Body: "<user:one> if a < b <user:two>"}}}
	_, cmd := v.update(m, keyMsg("enter"))
	if v.mode != ttFill || cmd != nil || len(v.fill.labels) != 2 || v.fill.labels[1] != "two" {
		t.Fatalf("mode %v labels %v", v.mode, v.fill.labels)
	}
	box := plain(v.box(m))
	for _, want := range []string{"A — fill variables (1/2)", "> one:", "[enter/tab] next"} {
		if !strings.Contains(box, want) {
			t.Errorf("fill box misses %q\n%s", want, box)
		}
	}
	v.update(m, keyMsg("x"))
	_, cmd = v.update(m, keyMsg("enter")) // to the second field
	if cmd != nil || !strings.Contains(plain(v.box(m)), "A — fill variables (2/2)") {
		t.Fatalf("enter on the first field must move on:\n%s", plain(v.box(m)))
	}
	_, cmd = v.update(m, keyMsg("enter")) // the last field: render
	if cmd == nil || v.fill.inputs()["one"] != "x" {
		t.Fatalf("enter on the last field must render (inputs %v)", v.fill.inputs())
	}
	v.update(m, keyMsg("esc"))
	if v.mode != ttBrowse {
		t.Fatalf("esc: mode %v", v.mode)
	}
}

func TestTextTemplatesEnterWithoutVariablesRenders(t *testing.T) {
	t.Parallel()
	v := &textTemplatesView{items: []model.TextTemplate{{ID: "a", Title: "A", Body: "plain"}}}
	_, cmd := v.update(Model{}, keyMsg("enter"))
	if v.mode != ttBrowse || cmd == nil {
		t.Fatalf("no variables: want a render command (mode %v, cmd nil %v)", v.mode, cmd == nil)
	}
	// Nothing selected: nothing happens.
	if _, cmd := (&textTemplatesView{}).update(Model{}, keyMsg("enter")); cmd != nil {
		t.Fatal("enter on an empty list issued a command")
	}
}

func TestTextTemplateRenderedMsgShowsText(t *testing.T) {
	t.Parallel()
	v := &textTemplatesView{items: []model.TextTemplate{{ID: "a", Title: "A", Body: "x"}}}
	m := Model{width: 100, height: 40}.pushLayer(v)
	out, _ := m.Update(textTemplateRenderedMsg{text: "Hello Ann\nsecond", seqNames: []string{"n"}})
	if v.mode != ttRendered {
		t.Fatalf("mode %v", v.mode)
	}
	box := plain(v.box(out.(Model)))
	for _, want := range []string{"A — rendered", "Hello Ann", "second", "[y] copy and close"} {
		if !strings.Contains(box, want) {
			t.Errorf("rendered box misses %q\n%s", want, box)
		}
	}
}

func TestTextTemplateRenderErrorOffersOnlyEsc(t *testing.T) {
	t.Parallel()
	v := &textTemplatesView{items: []model.TextTemplate{{ID: "a", Title: "A", Body: "x"}}}
	m := Model{width: 100, height: 40}.pushLayer(v)
	out, _ := m.Update(textTemplateRenderedMsg{err: errors.New("template: boom")})
	box := plain(v.box(out.(Model)))
	if !strings.Contains(box, "boom") || strings.Contains(box, "template: boom") || strings.Contains(box, "[y]") {
		t.Fatalf("error box:\n%s", box)
	}
	mm, cmd := v.update(out.(Model), keyMsg("y"))
	if cmd != nil || layerOf[*textTemplatesView](mm) == nil {
		t.Fatal("y on an error must do nothing")
	}
}

// y copies and closes the WHOLE window (user ruling).
func TestTextTemplatesYCopiesAndCloses(t *testing.T) {
	t.Parallel()
	var copied string
	v := &textTemplatesView{mode: ttRendered, rendered: "final text", items: []model.TextTemplate{{ID: "a", Title: "A"}}}
	m := Model{clipWrite: func(_ io.Writer, s string) (string, error) { copied = s; return "", nil }}.pushLayer(v)
	out, cmd := v.update(m, keyMsg("y"))
	if layerOf[*textTemplatesView](out) != nil {
		t.Fatal("the window is still open after y")
	}
	if cmd == nil {
		t.Fatal("no copy command")
	}
	var ok string
	for _, msg := range runCmds(cmd) {
		if c, is := msg.(clipboardCopiedMsg); is {
			ok = c.ok
		}
	}
	if copied != "final text" || ok == "" {
		t.Fatalf("copied %q, status %q", copied, ok)
	}
}

func TestTextTemplatesRenderedEscReturnsToBrowse(t *testing.T) {
	t.Parallel()
	v := &textTemplatesView{mode: ttRendered, rendered: "x", items: ttItems(1)}
	m := Model{}.pushLayer(v)
	out, _ := v.update(m, keyMsg("esc"))
	if v.mode != ttBrowse || layerOf[*textTemplatesView](out) == nil {
		t.Fatalf("esc: mode %v", v.mode)
	}
}

func TestTextTemplatesRenderedScrolls(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 30}
	var b strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&b, "row %02d\n", i)
	}
	v := &textTemplatesView{mode: ttRendered, rendered: b.String(), items: ttItems(1)}
	if box := plain(v.box(m)); !strings.Contains(box, "row 00") || len(strings.Split(box, "\n")) > 31 {
		t.Fatalf("rendered box must fit and start at the top:\n%s", box)
	}
	v.update(m, keyMsg("down"))
	if box := plain(v.box(m)); strings.Contains(box, "row 00") {
		t.Fatalf("down did not scroll:\n%s", box)
	}
}

func ttType(v *textTemplatesView, m Model, text string) {
	for _, r := range text {
		v.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestTextTemplatesFormAddFlow(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 40}
	v := &textTemplatesView{items: ttItems(1)}
	v.update(m, keyMsg("n"))
	if v.mode != ttForm || v.editID != "" || v.scope != model.ProfileScopeGlobal {
		t.Fatalf("n: mode %v editID %q scope %v", v.mode, v.editID, v.scope)
	}
	if box := plain(v.box(m)); !strings.Contains(box, "Add text template") || !strings.Contains(box, "$EDITOR") {
		t.Fatalf("form box:\n%s", box)
	}
	// An empty title is refused inline, no editor.
	_, cmd := v.update(m, keyMsg("enter"))
	if cmd != nil || v.formErr == "" {
		t.Fatalf("empty title: cmd nil %v err %q", cmd == nil, v.formErr)
	}
	ttType(v, m, "Standup")
	v.update(m, keyMsg("down"))  // to scope
	v.update(m, keyMsg("right")) // → this repo
	if v.scope != model.ProfileScopeRepo {
		t.Fatalf("scope toggle: %v", v.scope)
	}
	_, cmd = v.update(m, keyMsg("enter"))
	if cmd == nil || v.formErr != "" {
		t.Fatalf("valid title must prepare the editor (err %q)", v.formErr)
	}
	draft, ok := cmd().(textTemplateDraftMsg)
	if !ok || draft.err != nil || draft.title != "Standup" || draft.scope != model.ProfileScopeRepo || draft.editID != "" {
		t.Fatalf("draft = %+v", draft)
	}
	defer os.Remove(draft.path)
	if data, err := os.ReadFile(draft.path); err != nil || len(data) != 0 || !strings.HasSuffix(draft.path, ".md") {
		t.Fatalf("a new template starts from an empty .md file: %q %v %s", data, err, draft.path)
	}
	// esc leaves the form.
	v.update(m, keyMsg("esc"))
	if v.mode != ttBrowse {
		t.Fatalf("esc: mode %v", v.mode)
	}
}

func TestTextTemplatesEditSeedsFormAndLocksScope(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 40}
	v := &textTemplatesView{items: []model.TextTemplate{{ID: "a", Title: "Alpha", Body: "the body", Scope: model.ProfileScopeRepo}}}
	v.update(m, keyMsg("e"))
	if v.mode != ttForm || v.editID != "a" || v.fTitle.Value() != "Alpha" || v.scope != model.ProfileScopeRepo {
		t.Fatalf("e: mode %v id %q title %q scope %v", v.mode, v.editID, v.fTitle.Value(), v.scope)
	}
	v.update(m, keyMsg("down"))
	v.update(m, keyMsg("right"))
	if v.scope != model.ProfileScopeRepo {
		t.Fatal("the scope must be fixed while editing")
	}
	_, cmd := v.update(m, keyMsg("enter"))
	draft := cmd().(textTemplateDraftMsg)
	defer os.Remove(draft.path)
	if data, _ := os.ReadFile(draft.path); string(data) != "the body" || draft.before != "the body" || draft.editID != "a" {
		t.Fatalf("the editor must be seeded with the stored text: %q %+v", data, draft)
	}
	// e on an empty list does nothing.
	empty := &textTemplatesView{}
	empty.update(m, keyMsg("e"))
	empty.update(m, keyMsg("d"))
	if empty.mode != ttBrowse {
		t.Fatalf("e/d on an empty list: mode %v", empty.mode)
	}
}

func TestTextTemplateDraftMsgError(t *testing.T) {
	t.Parallel()
	v := &textTemplatesView{mode: ttForm}
	out, cmd := Model{}.pushLayer(v).Update(textTemplateDraftMsg{err: errors.New("disk full")})
	if cmd != nil || !strings.Contains(out.(Model).statusMsg, "disk full") {
		t.Fatalf("status %q", out.(Model).statusMsg)
	}
}

func ttEdited(t *testing.T, content string) textTemplateEditedMsg {
	t.Helper()
	p := filepath.Join(t.TempDir(), "gg-x-template.md")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return textTemplateEditedMsg{textTemplateDraftMsg: textTemplateDraftMsg{path: p, title: "New", scope: model.ProfileScopeGlobal}}
}

// Nothing is saved, the temp file goes, the window stays usable.
func TestTextTemplateEditedNothingToSave(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		content string
		mut     func(*textTemplateEditedMsg)
	}{
		"empty body":     {" \n\n", func(*textTemplateEditedMsg) {}},
		"editor failed":  {"text", func(m *textTemplateEditedMsg) { m.err = errors.New("exit status 1") }},
		"unchanged edit": {"same\n", func(m *textTemplateEditedMsg) { m.editID, m.before = "new", "same" }},
	} {
		msg := ttEdited(t, tc.content)
		tc.mut(&msg)
		v := &textTemplatesView{mode: ttForm, items: []model.TextTemplate{{ID: "new", Title: "New", Body: "same"}}}
		out, cmd := Model{}.pushLayer(v).Update(msg)
		if cmd != nil {
			t.Errorf("%s: a save command was issued", name)
		}
		if _, err := os.Stat(msg.path); !os.IsNotExist(err) {
			t.Errorf("%s: the temp file was not removed", name)
		}
		mm := out.(Model)
		if layerOf[*textTemplatesView](mm) == nil || v.mode != ttBrowse || mm.statusMsg == "" {
			t.Errorf("%s: window gone, or mode %v, or no status %q", name, v.mode, mm.statusMsg)
		}
	}
}

// The file is removed even when the window was closed meanwhile.
func TestTextTemplateEditedWithoutWindowRemovesFile(t *testing.T) {
	t.Parallel()
	msg := ttEdited(t, "text")
	if _, cmd := (Model{}).Update(msg); cmd != nil {
		t.Fatal("no window: nothing to save")
	}
	if _, err := os.Stat(msg.path); !os.IsNotExist(err) {
		t.Fatal("the temp file was not removed")
	}
}

func TestTextTemplateEditedInvalidBodyKeepsFormAndDraft(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 40}
	msg := ttEdited(t, "bad <seq> token\n")
	v := &textTemplatesView{mode: ttForm, fTitle: newTextField("New")}
	out, cmd := m.pushLayer(v).Update(msg)
	if cmd != nil || v.mode != ttForm || !strings.Contains(v.formErr, "seq") || strings.Contains(v.formErr, "template:") {
		t.Fatalf("cmd nil %v mode %v err %q", cmd == nil, v.mode, v.formErr)
	}
	if v.draft != "bad <seq> token" {
		t.Fatalf("the rejected text must be kept for the next editor run: %q", v.draft)
	}
	// enter reopens the editor on the rejected text, not on the old body.
	_, cmd = v.update(out.(Model), keyMsg("enter"))
	draft := cmd().(textTemplateDraftMsg)
	defer os.Remove(draft.path)
	if data, _ := os.ReadFile(draft.path); string(data) != "bad <seq> token" {
		t.Fatalf("editor reseeded with %q", data)
	}
	// esc drops the draft.
	v.update(out.(Model), keyMsg("esc"))
	if v.draft != "" {
		t.Fatal("esc must drop the draft")
	}
}

func TestTextTemplatesSaveRoundTrip(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	v := &textTemplatesView{mode: ttForm}
	m = m.pushLayer(v)
	title := "Round trip " + t.Name()

	msg := ttEdited(t, "Hello <user:x>\n\n")
	msg.title = title
	out, cmd := m.Update(msg)
	if cmd == nil || !v.loading {
		t.Fatal("no save command")
	}
	data, ok := cmd().(textTemplatesDataMsg)
	if !ok || data.err != nil || data.selectID == "" || data.status == "" {
		t.Fatalf("save = %+v", data)
	}
	out, _ = out.(Model).Update(data)
	saved, ok := v.selected()
	if !ok || saved.Title != title || saved.Body != "Hello <user:x>" || v.mode != ttBrowse {
		t.Fatalf("after save: %+v mode %v", saved, v.mode)
	}

	// Edit: a new title renames, the body changes.
	edit := ttEdited(t, "Bye\n")
	edit.title, edit.editID, edit.before = title+" 2", saved.ID, saved.Body
	_, cmd = out.(Model).Update(edit)
	data = cmd().(textTemplatesDataMsg)
	all, _ := m.svc.TextTemplates(context.Background())
	var titles []string
	for _, tt := range all {
		titles = append(titles, tt.Title)
	}
	if data.err != nil || !strings.Contains(strings.Join(titles, "|"), title+" 2") || strings.Contains(strings.Join(titles, "|")+"|", title+"|") {
		t.Fatalf("rename: err %v titles %v", data.err, titles)
	}

	// Delete (after the confirm) removes it.
	v.onData(data)
	v.update(out.(Model), keyMsg("d"))
	_, cmd = v.update(out.(Model), keyMsg("y"))
	if data = cmd().(textTemplatesDataMsg); data.err != nil || data.status == "" {
		t.Fatalf("remove = %+v", data)
	}
	for _, tt := range data.items {
		if tt.Title == title+" 2" {
			t.Fatal("still stored after delete")
		}
	}
}

func TestTextTemplatesDeleteNeedsConfirm(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 40}
	v := &textTemplatesView{items: ttItems(2), sel: 1}
	_, cmd := v.update(m, keyMsg("d"))
	if v.mode != ttConfirmDelete || cmd != nil {
		t.Fatalf("d: mode %v", v.mode)
	}
	if box := plain(v.box(m)); !strings.Contains(box, "Delete text template Template 01?") {
		t.Fatalf("confirm box:\n%s", box)
	}
	_, cmd = v.update(m, keyMsg("n"))
	if v.mode != ttBrowse || cmd != nil {
		t.Fatal("n must cancel")
	}
	v.update(m, keyMsg("d"))
	_, cmd = v.update(m, keyMsg("y"))
	if v.mode != ttBrowse || cmd == nil {
		t.Fatal("y must issue the remove command")
	}
}
