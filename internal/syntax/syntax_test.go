package syntax

import (
	"reflect"
	"testing"
)

func TestDetectByFileName(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"main.go": true, "app.tsx": true, "x.py": true, "Dockerfile": true,
		"go.mod": true, "notes.txt": false, "": false, "archive.bin": false,
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

func TestLexLineCountMatchesTextdiff(t *testing.T) {
	t.Parallel()
	// textdiff.splitLines: trailing \n does not add a line; \r is stripped per line.
	for _, src := range []string{"a\nb\n", "a\nb", "a\r\nb\r\n", "", "\n"} {
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

func TestClassString(t *testing.T) {
	t.Parallel()
	if Plain.String() != "" || Keyword.String() != "kw" || Comment.String() != "cmt" {
		t.Error("Class.String must give the CSS/style suffix, empty for Plain")
	}
}
