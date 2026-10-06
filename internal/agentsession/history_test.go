package agentsession

import (
	"regexp"
	"runtime"
	"testing"
)

func historySession(t *testing.T, script string, cols, rows int) *Session {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	s, err := start("h", StartSpec{Dir: t.TempDir(), Cols: cols, Rows: rows, Argv: []string{"sh", "-c", script}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.kill(); <-s.Done() })
	return s
}

func TestHistoryHoldsScrollbackThenScreen(t *testing.T) {
	t.Parallel()
	s := historySession(t, `i=0; while [ $i -lt 30 ]; do echo "line $i"; i=$((i+1)); done; printf 'END'; sleep 5`, 20, 5)
	waitText(t, s, "END")
	h := s.History()
	if h.Len() != 31 || h.Width() != 20 {
		t.Fatalf("len=%d width=%d", h.Len(), h.Width())
	}
	if got := h.Text(0, 0, 0, 19); got != "line 0" {
		t.Fatalf("row 0 = %q", got)
	}
	if got := h.Text(30, 0, 30, 19); got != "END" {
		t.Fatalf("last row = %q", got)
	}
}

func TestHistoryStaysFrozen(t *testing.T) {
	t.Parallel()
	s := historySession(t, `echo first; read _; i=0; while [ $i -lt 20 ]; do echo "more $i"; i=$((i+1)); done; printf 'END'; sleep 5`, 20, 5)
	waitText(t, s, "first")
	h := s.History()
	before := h.Text(0, 0, h.Len()-1, 19)
	taken := h.Taken()
	s.SendText("\r")
	waitText(t, s, "END")
	if after := h.Text(0, 0, h.Len()-1, 19); after != before {
		t.Fatalf("snapshot changed:\n%q\n%q", before, after)
	}
	if !s.LastOutput().After(taken) {
		t.Fatal("LastOutput did not pass the snapshot time")
	}
}

func TestHistoryTextStreamAndTrim(t *testing.T) {
	t.Parallel()
	s := historySession(t, `printf 'abc   \r\ndefgh\r\nij'; sleep 5`, 20, 5)
	waitText(t, s, "ij")
	h := s.History()
	if got := h.Text(0, 1, 2, 0); got != "bc\ndefgh\ni" {
		t.Fatalf("stream = %q", got)
	}
	if got := h.Text(2, 0, 0, 1); got != "bc\ndefgh\ni" {
		t.Fatalf("reversed ends = %q", got)
	}
	if h.RowWidth(0) != 3 || h.RowWidth(3) != 0 {
		t.Fatalf("row widths %d %d", h.RowWidth(0), h.RowWidth(3))
	}
}

func TestHistoryTextSkipsWideHalves(t *testing.T) {
	t.Parallel()
	s := historySession(t, `printf '漢字ok'; sleep 5`, 20, 5)
	waitText(t, s, "ok")
	h := s.History()
	if got := h.Text(0, 0, 0, 19); got != "漢字ok" {
		t.Fatalf("wide row = %q", got)
	}
	if got := h.Text(0, 1, 0, 2); got != "漢字" { // from a right half to a left half
		t.Fatalf("partial wide = %q", got)
	}
}

func TestHistoryWordAt(t *testing.T) {
	t.Parallel()
	s := historySession(t, `printf 'foo bar.baz  q'; sleep 5`, 20, 5)
	waitText(t, s, "q")
	h := s.History()
	if c0, c1 := h.WordAt(0, 5); c0 != 4 || c1 != 10 {
		t.Fatalf("word at 5 = %d..%d", c0, c1)
	}
	if c0, c1 := h.WordAt(0, 11); c0 <= c1 {
		t.Fatalf("blank gave a word %d..%d", c0, c1)
	}
}

func TestHistoryWordAtWideGlyphs(t *testing.T) {
	t.Parallel()
	s := historySession(t, `printf 'a 漢字x b'; sleep 5`, 20, 5)
	waitText(t, s, "b")
	h := s.History()
	// 漢 at 2-3, 字 at 4-5, x at 6: a click on 漢's right half takes the word.
	if c0, c1 := h.WordAt(0, 3); c0 != 2 || c1 != 6 {
		t.Fatalf("wide word = %d..%d", c0, c1)
	}
}

var (
	sgrReverse   = regexp.MustCompile(`\x1b\[(?:[0-9]*;)*7(?:;[0-9]*)*m`)
	sgrUnderline = regexp.MustCompile(`\x1b\[(?:[0-9]*;)*4(?::[0-9])?(?:;[0-9]*)*m`)
)

func TestHistoryRowMarks(t *testing.T) {
	t.Parallel()
	s := historySession(t, `printf 'hello'; sleep 5`, 20, 5)
	waitText(t, s, "hello")
	h := s.History()
	plain := h.Row(0, RowMarks{})
	sel := h.Row(0, RowMarks{SelFrom: 1, SelTo: 3})
	cur := h.Row(0, RowMarks{Cursor: true})
	if !sgrReverse.MatchString(sel) || sgrReverse.MatchString(plain) {
		t.Fatalf("reverse missing/extra: plain=%q sel=%q", plain, sel)
	}
	if !sgrUnderline.MatchString(cur) || sgrUnderline.MatchString(plain) {
		t.Fatalf("cursor underline missing: %q", cur)
	}
}
