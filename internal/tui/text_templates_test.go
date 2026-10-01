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
	v.update(m, keyMsg("esc")) // gives up on the render, back in the fields
	if v.mode != ttFill || v.rendering {
		t.Fatalf("esc while rendering: mode %v rendering %v", v.mode, v.rendering)
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
	v := &textTemplatesView{items: []model.TextTemplate{{ID: "a", Title: "A", Body: "x"}}, rendering: true, renderGen: 3}
	m := Model{width: 100, height: 40}.pushLayer(v)
	out, _ := m.Update(textTemplateRenderedMsg{gen: 3, text: "Hello Ann\nsecond", seqNames: []string{"n"}})
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
	v := &textTemplatesView{items: []model.TextTemplate{{ID: "a", Title: "A", Body: "x"}}, rendering: true}
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

	// The repo scope: each test repo has its own store file, while the
	// global one is shared by every parallel test (last writer wins).
	msg := ttEdited(t, "Hello <user:x>\n\n")
	msg.title, msg.scope = title, model.ProfileScopeRepo
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
	edit.title, edit.editID, edit.before, edit.scope = title+" 2", saved.ID, saved.Body, model.ProfileScopeRepo
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

func TestTextTemplatesAdvertised(t *testing.T) {
	t.Parallel()
	var help strings.Builder
	for _, l := range helpContent() {
		help.WriteString(l.text + "\n")
	}
	if !strings.Contains(help.String(), "alt+x") {
		t.Error("alt+x is missing from help")
	}
	var entry *paletteCommand
	for _, c := range paletteCommands() {
		if c.keyHint == "alt+x" {
			entry = &c
		}
	}
	if entry == nil {
		t.Fatal("the command palette has no text templates entry")
	}
	// Running it from the palette closes the palette and opens the window.
	m := loadedModel(t)
	m, _ = m.openCommandPalette()
	out, cmd := entry.run(m)
	if layerOf[*textTemplatesView](out) == nil || layerOf[*commandPalette](out) != nil || cmd == nil {
		t.Fatal("the palette entry must swap the palette for the window")
	}
	footer := false
	for _, b := range globalBindings() {
		if b.key == "alt+x" {
			footer = true
		}
	}
	if !footer {
		t.Error("alt+x is missing from the footer bindings")
	}
}

func TestAltXIgnoredUnderALayer(t *testing.T) {
	t.Parallel()
	m := loadedModel(t).pushLayer(&prefixSettingsView{})
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}, Alt: true})
	if layerOf[*textTemplatesView](out.(Model)) != nil {
		t.Fatal("alt+x opened the window over another layer")
	}
}

// A render result lands only while the view still waits for THAT render:
// moving on (esc, another row, the form) must not be yanked into "rendered".
func TestTextTemplateStaleRenderIsDropped(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 40}
	v := &textTemplatesView{items: []model.TextTemplate{{ID: "a", Title: "A", Body: "plain"}, {ID: "b", Title: "B", Body: "other"}}}
	mm := m.pushLayer(v)
	_, cmd := v.update(mm, keyMsg("enter"))
	if cmd == nil || !v.rendering {
		t.Fatal("enter must start a render and wait for it")
	}
	gen := v.renderGen
	// While waiting, keys other than esc are swallowed.
	v.update(mm, keyMsg("down"))
	v.update(mm, keyMsg("n"))
	if v.sel != 0 || v.mode != ttBrowse {
		t.Fatalf("keys leaked while rendering: sel %d mode %v", v.sel, v.mode)
	}
	// esc gives up on the render; its late result is dropped.
	v.update(mm, keyMsg("esc"))
	if v.rendering || layerOf[*textTemplatesView](mm) == nil {
		t.Fatal("esc must cancel the wait and keep the window")
	}
	mm.Update(textTemplateRenderedMsg{gen: gen, text: "late"})
	if v.mode != ttBrowse || v.rendered != "" {
		t.Fatalf("a canceled render landed: mode %v text %q", v.mode, v.rendered)
	}
	// A result of an older request is dropped too.
	v.update(mm, keyMsg("enter"))
	mm.Update(textTemplateRenderedMsg{gen: gen, text: "old"})
	if v.mode != ttBrowse || !v.rendering {
		t.Fatalf("an old render landed: mode %v", v.mode)
	}
	mm.Update(textTemplateRenderedMsg{gen: v.renderGen, text: "fresh"})
	if v.mode != ttRendered || v.rendered != "fresh" || v.rendering {
		t.Fatalf("the awaited render did not land: mode %v text %q", v.mode, v.rendered)
	}
}

// A save the store refuses keeps the form open with the written text as the
// next draft — the text from the editor is never thrown away.
func TestTextTemplateSaveFailureKeepsText(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	v := &textTemplatesView{}
	m = m.pushLayer(v)
	title := "Dup " + t.Name()
	first := ttEdited(t, "first body\n")
	first.title, first.scope = title, model.ProfileScopeRepo // a per-test store file
	_, cmd := m.Update(first)
	saved := cmd()
	if d, ok := saved.(textTemplatesDataMsg); !ok || d.err != nil {
		t.Fatalf("the first save failed: %#v", saved)
	}
	out, _ := m.Update(saved)

	// The row vanishes from this view's list (another gg removed it) while
	// it is still stored: an add of the same title now reaches the store.
	v.items = nil
	v.openForm(model.TextTemplate{Scope: model.ProfileScopeRepo})
	v.fTitle = newTextField(title)
	second := ttEdited(t, "a long text written in the editor\n")
	second.title, second.scope = title, model.ProfileScopeRepo
	_, cmd = out.(Model).Update(second)
	msg := cmd()
	if _, ok := msg.(textTemplateSaveFailedMsg); !ok {
		t.Fatalf("a refused save must report as a save failure, got %T", msg)
	}
	res, _ := out.(Model).Update(msg)
	if v.mode != ttForm || v.loading || v.draft != "a long text written in the editor" || !strings.Contains(v.formErr, "already exists") || strings.Contains(v.formErr, "text template:") {
		t.Fatalf("mode %v loading %v draft %q err %q", v.mode, v.loading, v.draft, v.formErr)
	}
	// The window is usable again: esc leaves the form.
	v.update(res.(Model), keyMsg("esc"))
	if v.mode != ttBrowse || v.draft != "" {
		t.Fatalf("esc after a failed save: mode %v draft %q", v.mode, v.draft)
	}
}

// A title already used in the scope is refused in the form, before the editor.
func TestTextTemplatesFormRefusesDuplicateTitle(t *testing.T) {
	t.Parallel()
	m := Model{width: 100, height: 40}
	v := &textTemplatesView{items: []model.TextTemplate{
		{ID: "pr-description", Title: "PR description", Scope: model.ProfileScopeGlobal},
		{ID: "note", Title: "Note", Scope: model.ProfileScopeGlobal},
	}}
	v.update(m, keyMsg("n"))
	ttType(v, m, "pr description")
	if _, cmd := v.update(m, keyMsg("enter")); cmd != nil || !strings.Contains(v.formErr, "already exists") {
		t.Fatalf("duplicate title: cmd nil %v err %q", cmd == nil, v.formErr)
	}
	// The other scope is free.
	v.update(m, keyMsg("down"))
	v.update(m, keyMsg("right"))
	_, cmd := v.update(m, keyMsg("enter"))
	if cmd == nil {
		t.Fatalf("the same title in the other scope must be allowed (err %q)", v.formErr)
	}
	os.Remove(cmd().(textTemplateDraftMsg).path)
	// Editing a template under its own title is not a duplicate; onto another's is.
	v.sel = 1
	v.update(m, keyMsg("esc"))
	v.update(m, keyMsg("e"))
	if _, cmd := v.update(m, keyMsg("enter")); cmd == nil {
		t.Fatalf("keeping the title while editing was refused: %q", v.formErr)
	} else {
		os.Remove(cmd().(textTemplateDraftMsg).path)
	}
	v.fTitle = newTextField("PR Description")
	if _, cmd := v.update(m, keyMsg("enter")); cmd != nil || v.formErr == "" {
		t.Fatal("a rename onto another template's title must be refused in the form")
	}
}
