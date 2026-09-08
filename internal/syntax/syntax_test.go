package syntax

import (
	"reflect"
	"strings"
	"testing"
)

func TestDetectByFileName(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"main.go": true, "app.tsx": true, "x.py": true, "Dockerfile": true,
		// go.mod has no dedicated chroma lexer: its "*.mod" glob collides
		// with AMPL and Modula-2, and AMPL wins — lexing go.mod with AMPL's
		// grammar would produce nonsense runs, so Detect treats it (and
		// go.sum/go.work) as unknown.
		"go.mod": false, "notes.txt": false, "": false, "archive.bin": false,
	}
	for path, want := range cases {
		if got := Detect(path) != ""; got != want {
			t.Errorf("Detect(%q) known=%v, want %v", path, got, want)
		}
	}
}

func TestLexSplitsTokensPerLine(t *testing.T) {
	t.Parallel()
	src := []byte("package x\n/* one\ntwo */\nvar s = \"hi\" // c\n")
	toks := Lex(Detect("a.go"), src)
	if len(toks) != 4 {
		t.Fatalf("lines = %d, want 4 (one per source line)", len(toks))
	}
	// line 2 is entirely inside the block comment
	if want := []Tok{{0, 6, Comment}}; !reflect.DeepEqual(toks[1], want) {
		t.Errorf("line 2 = %+v, want %+v", toks[1], want)
	}
	// line 3: the comment continues, then a plain gap is NOT emitted
	if len(toks[2]) != 1 || toks[2][0].Class != Comment || toks[2][0].Start != 0 || toks[2][0].End != 6 {
		t.Errorf("line 3 = %+v, want one Comment run [0,6)", toks[2])
	}
	// line 4: keyword, name, "=", string, comment; whitespace produces no Tok.
	// chroma's Go lexer classifies `=` as Punctuation, not Operator (verified
	// against chroma v2.27.0's go.xml), so the third run is Punct here per
	// the task brief's guidance to match classOf's actual mapping rather
	// than weaken the other assertions.
	classes := []Class{}
	for _, tk := range toks[3] {
		classes = append(classes, tk.Class)
	}
	if want := []Class{Keyword, Name, Punct, String, Comment}; !reflect.DeepEqual(classes, want) {
		t.Errorf("line 4 classes = %v, want %v", classes, want)
	}
}

func TestLexOffsetsAreRunes(t *testing.T) {
	t.Parallel()
	toks := Lex(Detect("a.go"), []byte("s := \"héllo\" // ü\n"))
	var str Tok
	for _, tk := range toks[0] {
		if tk.Class == String {
			str = tk
		}
	}
	if str.Start != 5 || str.End != 12 {
		t.Errorf("string run = [%d,%d), want [5,12) counted in runes", str.Start, str.End)
	}
}

func TestLexUnknownLanguageIsNil(t *testing.T) {
	t.Parallel()
	if got := Lex("", []byte("x\n")); got != nil {
		t.Errorf("Lex(\"\") = %v, want nil", got)
	}
	if got := Lex("no-such-lexer", []byte("x\n")); got != nil {
		t.Errorf("Lex(unknown) = %v, want nil", got)
	}
}

func TestLexEmptySrcIsNilForKnownLanguage(t *testing.T) {
	t.Parallel()
	lang := Detect("a.go")
	if got := Lex(lang, nil); got != nil {
		t.Errorf("Lex(known, nil) = %v, want nil", got)
	}
	if got := Lex(lang, []byte("")); got != nil {
		t.Errorf("Lex(known, []byte(\"\")) = %v, want nil", got)
	}
}

func TestLexCapsRunsPerLine(t *testing.T) {
	t.Parallel()
	// A single minified line of 3000 "a+" pairs yields far more than
	// MaxLineTokens runs; Lex must cap what it keeps for that line without
	// losing line-number sync for what follows.
	long := strings.Repeat("a+", 3000) + "\n"
	src := []byte("x := 1\n" + long + "y := 2\n")
	toks := Lex(Detect("a.go"), src)
	if len(toks) != 3 {
		t.Fatalf("lines = %d, want 3", len(toks))
	}
	if len(toks[1]) != MaxLineTokens {
		t.Errorf("capped line runs = %d, want %d", len(toks[1]), MaxLineTokens)
	}
	// line 3 must still lex normally: keyword-free but a Name and a Number.
	var gotNumber bool
	for _, tk := range toks[2] {
		if tk.Class == Number {
			gotNumber = true
		}
	}
	if !gotNumber {
		t.Errorf("line 3 = %+v, want a Number run (line numbering desynced?)", toks[2])
	}
}

func TestLexLineCountMatchesTextdiff(t *testing.T) {
	t.Parallel()
	// textdiff.splitLines: trailing \n does not add a line; \r is stripped per line.
	// "a\rb\nc\n" is the lone-\r case: only \n starts a new line, so it is two
	// lines — chroma must not be allowed to normalise the \r into a third.
	for _, src := range []string{"a\nb\n", "a\nb", "a\r\nb\r\n", "a\rb\nc\n", "", "\n"} {
		toks := Lex(Detect("a.go"), []byte(src))
		want := 0
		if src != "" {
			s := src
			if s[len(s)-1] == '\n' {
				s = s[:len(s)-1]
			}
			want = 1
			for _, c := range s {
				if c == '\n' {
					want++
				}
			}
		}
		if len(toks) != want {
			t.Errorf("Lex(%q) lines = %d, want %d", src, len(toks), want)
		}
	}
}

// TestLexLoneCRDoesNotShiftLines pins the regression a nil TokeniseOptions
// caused: chroma's default EnsureLF rewrote a bare \r inside a string literal
// to \n, so chroma counted one more line than textdiff and every following
// line inherited the PREVIOUS line's runs.
func TestLexLoneCRDoesNotShiftLines(t *testing.T) {
	t.Parallel()
	src := []byte("package x\nvar a = \"p\rq\"\nfunc f() {}\n")
	toks := Lex(Detect("a.go"), src)
	if len(toks) != 3 {
		t.Fatalf("lines = %d, want 3 (the lone \\r must not start a line)", len(toks))
	}
	// Line 2's string literal spans the \r: `var a = ` is 8 runes, the
	// literal "p\rq" is 5 more.
	var str Tok
	for _, tk := range toks[1] {
		if tk.Class == String {
			str = tk
		}
	}
	if str.Start != 8 || str.End != 13 {
		t.Errorf("line 2 String run = [%d,%d), want [8,13) covering \"p\\rq\"", str.Start, str.End)
	}
	// Line 3 must be lexed as itself, not as a shifted copy of line 2.
	if len(toks[2]) == 0 || toks[2][0].Class != Keyword || toks[2][0].Start != 0 {
		t.Errorf("line 3 = %+v, want a Keyword run at offset 0 for `func`", toks[2])
	}
}

func TestClassString(t *testing.T) {
	t.Parallel()
	if Plain.String() != "" || Keyword.String() != "kw" || Comment.String() != "cmt" {
		t.Error("Class.String must give the CSS/style suffix, empty for Plain")
	}
}
