package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/hunkpick"
	"github.com/homeend/gigagit/internal/syntax"
)

// pickerSyntaxDoc is a two-block Go document whose sides differ in LENGTH, so
// a mapping that advanced one full-file line cursor for both sides would land
// the second block's incoming line on the wrong line number.
//
//	current side (6 lines)      incoming side (4 lines)
//	0 package main              0 package main
//	1 var a int                 1 var x int
//	2 var b int                 2 const K = 1
//	3 var c int                 3 // tail
//	4 const K = 1
//	5 var d int
func pickerSyntaxDoc() *hunkpick.Doc {
	return &hunkpick.Doc{FinalNewline: true, Items: []hunkpick.Item{
		{Literal: []string{"package main"}},
		{Block: &hunkpick.Block{
			Current:  []string{"var a int", "var b int", "var c int"},
			Incoming: []string{"var x int"},
		}},
		{Literal: []string{"const K = 1"}},
		{Block: &hunkpick.Block{
			Current:  []string{"var d int"},
			Incoming: []string{"// tail"},
		}},
	}}
}

func TestPickerLexesBothSidesWithIndependentLineNumbers(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(true)
	if len(e.curTok) != 6 {
		t.Fatalf("current side lexed %d lines, want 6", len(e.curTok))
	}
	if len(e.incTok) != 4 {
		t.Fatalf("incoming side lexed %d lines, want 4", len(e.incTok))
	}
	// Block 2's incoming line is incoming line 4 (1-based) — a comment. Had the
	// mapping used the CURRENT side's cursor it would read line 6, past the end.
	if toks := tokAt(e.incTok, 4); len(toks) == 0 || toks[0].Class != syntax.Comment {
		t.Errorf("incoming line 4 (`// tail`) runs = %v, want a leading Comment", toks)
	}
	// Block 2's current line is current line 6 — `var` is a keyword.
	if toks := tokAt(e.curTok, 6); len(toks) == 0 || toks[0].Class != syntax.Keyword {
		t.Errorf("current line 6 (`var d int`) runs = %v, want a leading Keyword", toks)
	}
}

func TestPickerSyntaxOffLeavesNoRuns(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(false)
	if e.curTok != nil || e.incTok != nil {
		t.Fatalf("syntax off must leave both sides unlexed: cur=%d inc=%d", len(e.curTok), len(e.incTok))
	}
}

func TestPickerUnknownLanguageLeavesNoRuns(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.unknownext", pickerSyntaxDoc()).withSyntax(true)
	if e.curTok != nil || e.incTok != nil {
		t.Fatalf("a path with no lexer must leave both sides unlexed: cur=%d inc=%d", len(e.curTok), len(e.incTok))
	}
}

// A side holding a BARE \r is refused, exactly as lexBlame/lexPreview refuse one.
func TestPickerBareCRSideIsNotLexed(t *testing.T) {
	t.Parallel()
	doc := &hunkpick.Doc{Items: []hunkpick.Item{
		{Literal: []string{"package main"}},
		{Block: &hunkpick.Block{Current: []string{"var a\rint"}, Incoming: []string{"var b int"}}},
	}}
	e := newConflictPicker("f.go", doc).withSyntax(true)
	if e.curTok != nil {
		t.Errorf("a bare-\\r current side must not be lexed: %d lines", len(e.curTok))
	}
	if e.incTok == nil {
		t.Errorf("the clean incoming side should still be lexed")
	}
}

// Every constructor records its path, so withSyntax needs no second argument.
func TestPickerConstructorsRecordPath(t *testing.T) {
	t.Parallel()
	d := pickerSyntaxDoc()
	for name, e := range map[string]*hunkPicker{
		"conflict": newConflictPicker("a.go", d),
		"process":  newProcessConflictPicker("b.go", d),
		"stage":    newStagePicker("c.go", d),
		"unstage":  newUnstagePicker("d.go", d),
	} {
		if e.path == "" {
			t.Errorf("%s picker did not record its path", name)
		}
	}
}

// pickerLineWith returns the first line of a rendered picker whose visible
// text contains needle, or "" when there is none.
func pickerLineWith(render, needle string) string {
	for _, l := range strings.Split(render, "\n") {
		if strings.Contains(ansi.Strip(l), needle) {
			return l
		}
	}
	return ""
}

func TestEnsureSanBuildsMasksFromTheRightSide(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(true)
	e.ensureSan()

	// Literal context takes the CURRENT side's runs: `const` is a keyword.
	lit := e.sanLit[2][0]
	if lit.text != "const K = 1" {
		t.Fatalf("literal text = %q", lit.text)
	}
	if lit.mask.empty() || lit.mask.cls[0] != syntax.Keyword {
		t.Errorf("literal `const` should be a keyword: %v", lit.mask.cls)
	}
	// Block 2's incoming line is a comment; block 2's current line is a keyword.
	if got := e.sanInc[1][0]; got.text != "// tail" || got.mask.empty() || got.mask.cls[0] != syntax.Comment {
		t.Errorf("sanInc[1][0] = %q cls=%v, want `// tail` starting Comment", got.text, got.mask.cls)
	}
	if got := e.sanCur[1][0]; got.text != "var d int" || got.mask.empty() || got.mask.cls[0] != syntax.Keyword {
		t.Errorf("sanCur[1][0] = %q cls=%v, want `var d int` starting Keyword", got.text, got.mask.cls)
	}
	// Every mask is exactly one entry per DISPLAY rune.
	if n := len([]rune(lit.text)); len(lit.mask.cls) != n || len(lit.mask.emph) != n {
		t.Errorf("mask must parallel the display runes: cls=%d emph=%d runes=%d", len(lit.mask.cls), len(lit.mask.emph), n)
	}
}

// A tab expands to the 4-column stop and the mask follows it.
func TestSanPickLineCarriesClassesThroughTabs(t *testing.T) {
	t.Parallel()
	got := sanPickLine("\tvar x", []syntax.Tok{{Start: 1, End: 4, Class: syntax.Keyword}})
	if got.text != "    var x" {
		t.Fatalf("text = %q, want %q", got.text, "    var x")
	}
	want := []syntax.Class{0, 0, 0, 0, syntax.Keyword, syntax.Keyword, syntax.Keyword, 0, 0}
	for i := range want {
		if got.mask.cls[i] != want[i] {
			t.Fatalf("cls = %v, want %v", got.mask.cls, want)
		}
	}
}

func TestSanPickLineWithoutRunsIsPlain(t *testing.T) {
	t.Parallel()
	got := sanPickLine("\tvar x", nil)
	if got.text != "    var x" {
		t.Fatalf("text = %q", got.text)
	}
	if !got.mask.empty() {
		t.Errorf("no runs must leave an empty mask: %v", got.mask)
	}
}

func TestPickerGridColoursCodeAndKeepsCursorPlain(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(true)
	m := Model{layers: &layerStack{entries: []layer{e}}, width: 100, height: 30}
	out := e.render(m, "")

	kw := "38;5;" + st().syntaxColor(syntax.Keyword)
	// The cursor starts on block 0 / current / line 0 — "var a int". Only the
	// CURSOR CELL is plain; the incoming cell sharing that row is still
	// coloured, so scope the assertion to the left half. renderTwoCol appends
	// pickerColSep raw between the two cells, so splitting on it is safe.
	cur := pickerLineWith(out, "> [ ] var a int")
	if cur == "" {
		t.Fatalf("cursor row not found:\n%s", ansi.Strip(out))
	}
	halves := strings.SplitN(cur, pickerColSep, 2)
	if len(halves) != 2 {
		t.Fatalf("cursor row has no column separator: %q", cur)
	}
	if strings.Contains(halves[0], kw) {
		t.Errorf("the cursor cell must stay plain reverse-video, no class runs: %q", halves[0])
	}
	if !strings.Contains(halves[1], kw+"mvar") {
		t.Errorf("the non-cursor cell on the SAME row should still be coloured: %q", halves[1])
	}
	// A non-cursor candidate line IS coloured.
	code := pickerLineWith(out, "[ ] var b int")
	if code == "" {
		t.Fatalf("candidate row `var b int` not found:\n%s", ansi.Strip(out))
	}
	if !strings.Contains(code, kw+"mvar") {
		t.Errorf("`var` should wear the keyword colour: %q", code)
	}
	// The literal context row is coloured too.
	lit := pickerLineWith(out, "const K = 1")
	if lit == "" || !strings.Contains(lit, kw+"mconst") {
		t.Errorf("literal context `const` should be coloured: %q", lit)
	}
}

// Without withSyntax the render is byte-identical to today's.
func TestPickerRenderUnwiredIsPlain(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	mk := func(on bool) string {
		e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(on)
		m := Model{layers: &layerStack{entries: []layer{e}}, width: 100, height: 30}
		return e.render(m, "")
	}
	off, on := mk(false), mk(true)
	if ansi.Strip(off) != ansi.Strip(on) {
		t.Errorf("colouring must not change the visible text:\n off %q\n on %q", ansi.Strip(off), ansi.Strip(on))
	}
	if strings.Contains(off, "38;5;"+st().syntaxColor(syntax.Keyword)) {
		t.Errorf("an unwired picker must render no class runs: %q", off)
	}
}

// The output pane must reuse the grid's prepared lines, not rebuild them:
// pointer identity of the mask backing array proves no re-sanitize/re-lex.
func TestOutputPaneReusesTheGridsSanLines(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(true)
	e.doc.SetAll(hunkpick.TakeCurrent)
	e.ensureOutput()

	// outLines[0] is the literal "package main"; [1..3] are block 0's current
	// lines; [4] the second literal; [5] block 1's current line.
	if len(e.outLines) != 6 {
		t.Fatalf("outLines = %d entries, want 6: %v", len(e.outLines), e.outLines)
	}
	if e.outLines[1].text != "var a int" {
		t.Fatalf("outLines[1] = %q, want %q", e.outLines[1].text, "var a int")
	}
	if e.outLines[1].mask.empty() || e.sanCur[0][0].mask.empty() {
		t.Fatal("both the grid line and the output line must carry a mask")
	}
	if &e.outLines[1].mask.cls[0] != &e.sanCur[0][0].mask.cls[0] {
		t.Error("the output pane must reuse the grid's mask, not rebuild one")
	}
	if &e.outLines[0].mask.cls[0] != &e.sanLit[0][0].mask.cls[0] {
		t.Error("literal output lines must reuse the grid's literal mask")
	}
}

// A re-pick rebuilds the assembly from the same cached lines (masks survive).
func TestOutputPaneKeepsMasksAcrossPicks(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(true)
	e.doc.SetAll(hunkpick.TakeCurrent)
	e.ensureOutput()
	e.blocks[0].ToggleSide(hunkpick.Incoming)
	e.pickRev++
	e.ensureOutput()
	for i, l := range e.outLines {
		if l.text != "" && l.mask.empty() {
			t.Fatalf("outLines[%d] (%q) lost its mask after a re-pick", i, l.text)
		}
	}
}

// An undecided region's placeholder carries no mask and is never painted.
func TestOutputPanePlaceholderIsPlain(t *testing.T) {
	t.Parallel()
	e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(true)
	e.ensureOutput()
	if !e.outLines[1].mask.empty() {
		t.Errorf("the undecided placeholder must carry no mask: %v", e.outLines[1])
	}
}

func TestRenderOutputColoursTheAssembledLines(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	kw := "38;5;" + st().syntaxColor(syntax.Keyword)
	for _, mode := range []dispMode{modeScroll, modeCutoff, modeWrap} {
		e := newConflictPicker("f.go", pickerSyntaxDoc()).withSyntax(true)
		e.mode = mode
		e.doc.SetAll(hunkpick.TakeCurrent)
		lines := e.renderOutput(60, 8)
		joined := strings.Join(lines, "\n")
		if !strings.Contains(joined, kw+"mvar") {
			t.Errorf("mode %d: the output pane should colour `var`:\n%q", mode, joined)
		}
		if !strings.Contains(ansi.Strip(joined), "var a int") {
			t.Errorf("mode %d: visible text lost:\n%q", mode, ansi.Strip(joined))
		}
		for _, l := range lines {
			if w := ansi.StringWidth(l); w != 60 {
				t.Fatalf("mode %d: line width = %d, want 60: %q", mode, w, l)
			}
		}
	}
}
