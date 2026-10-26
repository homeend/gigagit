package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/syntax"
)

// countingHighlighter is a Highlighter over a counting lex seam and a real
// cache, with the syntax switch at on.
func countingHighlighter(c *countingLex, on bool) *Highlighter {
	return &Highlighter{syntax: func() bool { return on }, store: newCountingStore(c, true)}
}

func TestHighlighterPlainForGood(t *testing.T) {
	t.Parallel()
	c := &countingLex{}
	big := make([]byte, MaxSyntaxBytes+1)
	for _, tc := range []struct {
		name string
		h    *Highlighter
		path string
		src  []byte
	}{
		{"switch off", countingHighlighter(c, false), "a.go", []byte("package p\n")},
		{"no lexer", countingHighlighter(c, true), "a.unknownext", []byte("x\n")},
		{"past MaxSyntaxBytes", countingHighlighter(c, true), "a.go", big},
	} {
		r, err := tc.h.Runs(context.Background(), tc.path, tc.src, TokDefer)
		if err != nil || r.Tok != nil || r.Pending || r.Sum != contentSum(tc.src) {
			t.Errorf("%s: %+v, %v; want plain, not pending, the content's sum", tc.name, r, err)
		}
	}
	if c.calls.Load() != 0 {
		t.Errorf("lexed %d times for content that is plain for good", c.calls.Load())
	}
}

func TestHighlighterDeferPendsOnlyBigUnstoredContent(t *testing.T) {
	t.Parallel()
	c := &countingLex{}
	h := countingHighlighter(c, true)
	small := []byte("package p\n")
	big := []byte("package p\n" + strings.Repeat("// x\n", InlineSyntaxBytes/5+1))
	r, err := h.Runs(context.Background(), "a.go", small, TokDefer)
	if err != nil || r.Tok == nil || r.Pending {
		t.Fatalf("small: %+v, %v; want lexed at once", r, err)
	}
	r, err = h.Runs(context.Background(), "a.go", big, TokDefer)
	if err != nil || r.Tok != nil || !r.Pending || r.Sum != contentSum(big) {
		t.Fatalf("big: %+v, %v; want pending with its sum", r, err)
	}
	if c.calls.Load() != 1 {
		t.Fatalf("lexed %d times; a pending answer must not lex", c.calls.Load())
	}
	if r, err = h.Runs(context.Background(), "a.go", big, TokOnly); err != nil || r.Tok == nil || r.Pending {
		t.Fatalf("big, TokOnly: %+v, %v; want its runs", r, err)
	}
	if r, err = h.Runs(context.Background(), "a.go", big, TokDefer); err != nil || r.Tok == nil || r.Pending {
		t.Fatalf("big, deferred again: %+v, %v; want the stored runs at once", r, err)
	}
	if c.calls.Load() != 2 {
		t.Fatalf("lexed %d times; the stored side must not lex again", c.calls.Load())
	}
}

func TestHighlighterContentRefusesBareCR(t *testing.T) {
	t.Parallel()
	c := &countingLex{}
	h := countingHighlighter(c, true)
	r, err := h.Content(context.Background(), "a.go", []byte("package p\rvar x = 1\n"), TokInline)
	if err != nil || r.Tok != nil || r.Pending {
		t.Fatalf("%+v, %v; a bare CR keeps the content plain", r, err)
	}
	if r, err = h.Content(context.Background(), "a.go", []byte("package p\r\nvar x = 1\r\n"), TokInline); err != nil || r.Tok == nil {
		t.Fatalf("CRLF: %+v, %v; want runs", r, err)
	}
}

func TestHighlighterBlameLexesTheJoinedLines(t *testing.T) {
	t.Parallel()
	c := &countingLex{}
	h := countingHighlighter(c, true)
	lines := []model.BlameLine{{Content: "package p"}, {Content: ""}, {Content: "var x = 1"}}
	r, err := h.Blame(context.Background(), "a.go", lines, TokInline)
	if err != nil || r.Tok == nil || r.Sum != contentSum([]byte("package p\n\nvar x = 1\n")) {
		t.Fatalf("%+v, %v; want the joined lines' runs and sum", r, err)
	}
	if r, err = h.Blame(context.Background(), "a.go", []model.BlameLine{{Content: "a\rb"}}, TokInline); err != nil || r.Tok != nil {
		t.Fatalf("bare CR: %+v, %v; want plain", r, err)
	}
	if r, err = h.Blame(context.Background(), "a.go", nil, TokInline); err != nil || r.Tok != nil || r.Sum != "" {
		t.Fatalf("no lines: %+v, %v; want nothing", r, err)
	}
}

// The Service's differ and Highlighter share ONE store: the bytes a diff
// lexed answer a later view of the same content without a lex.
func TestServiceDifferAndHighlighterShareTheStore(t *testing.T) {
	t.Parallel()
	svc := New(nil)
	src := []byte("package p\n\nfunc f() {}\n")
	if _, err := svc.Differ().Diff(context.Background(), Request{Path: "a.go", Old: func(context.Context) ([]byte, error) { return []byte("package p\n"), nil }, New: func(context.Context) ([]byte, error) { return src, nil }}); err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.Highlighter().store.Get(syntax.Detect("a.go"), contentSum(src)); !ok {
		t.Fatal("the diff's runs are not in the Highlighter's store")
	}
}

// The overview split's file diffs (a review's, an overview's, an agent's
// working change) take the same mode as every diff: a big file's rows now,
// its colours on a TokOnly ask.
func TestWorkingFileDiffDefersBigFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	body := "package p\n\n" + strings.Repeat("func f() int { return 1 }\n", 3000)
	if err := os.WriteFile(filepath.Join(dir, "big.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "one")
	if err := os.WriteFile(filepath.Join(dir, "big.go"), []byte(body+"var x = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := Open(dir)
	rd, err := svc.WorkingFileDiff(context.Background(), "big.go", TokDefer)
	if err != nil || !rd.Diff.TokPending || rd.Diff.NewTok != nil {
		t.Fatalf("deferred: pending=%v newTok=%v err=%v; want the rows with the colours owed", rd.Diff.TokPending, rd.Diff.NewTok != nil, err)
	}
	rd, err = svc.WorkingFileDiff(context.Background(), "big.go", TokOnly)
	if err != nil || rd.Diff.NewTok == nil {
		t.Fatalf("TokOnly: newTok=%v err=%v; want the runs", rd.Diff.NewTok != nil, err)
	}
}
