package domain

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/homeend/gigagit/internal/cache"
	"github.com/homeend/gigagit/internal/syntax"
)

// largeGo is a Go source over InlineSyntaxBytes (a deferred side); smallGo
// one well under it (always lexed inline).
var (
	largeGoOld = []byte("package p\n\n" + strings.Repeat("func f() int { return 1 }\n", 3000))
	largeGoNew = []byte("package p\n\nvar x = 2\n" + strings.Repeat("func f() int { return 1 }\n", 3000))
	smallGoOld = []byte("package p\n\nfunc f() int { return 1 }\n")
	smallGoNew = []byte("package p\n\nfunc f() int { return 2 }\n")
)

func syntaxOn() bool { return true }

// countSrc is a ByteSource that counts its reads.
func countSrc(b []byte, n *atomic.Int32) ByteSource {
	return func(context.Context) ([]byte, error) { n.Add(1); return b, nil }
}

// storeOf reaches the token store inside a NewDiffer result (tests only).
func storeOf(t *testing.T, d Differ) *tokStore {
	t.Helper()
	switch v := d.(type) {
	case cachedDiffer:
		return v.inner.toks
	case plainDiffer:
		return v.toks
	}
	t.Fatalf("unknown Differ %T", d)
	return nil
}

func TestDifferDeferLargeIsPending(t *testing.T) {
	t.Parallel()
	if len(largeGoOld) <= InlineSyntaxBytes {
		t.Fatalf("fixture %d bytes is not over InlineSyntaxBytes", len(largeGoOld))
	}
	out, err := NewDiffer(DifferOptions{Enhanced: true, Syntax: syntaxOn}, nil).Diff(context.Background(),
		Request{Path: "a.go", Old: src(largeGoOld), New: src(largeGoNew), Tok: TokDefer})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Result.Rows) == 0 || !out.TokPending || out.OldTok != nil || out.NewTok != nil {
		t.Fatalf("rows=%d pending=%v old=%v new=%v; want rows, pending, no runs", len(out.Result.Rows), out.TokPending, out.OldTok != nil, out.NewTok != nil)
	}
	if out.OldSum != contentSum(largeGoOld) || out.NewSum != contentSum(largeGoNew) {
		t.Fatalf("sums = %q / %q; want the sides' content sums", out.OldSum, out.NewSum)
	}
}

func TestDifferDeferSmallIsInline(t *testing.T) {
	t.Parallel()
	out, err := NewDiffer(DifferOptions{Syntax: syntaxOn}, nil).Diff(context.Background(),
		Request{Path: "a.go", Old: src(smallGoOld), New: src(smallGoNew), Tok: TokDefer})
	if err != nil {
		t.Fatal(err)
	}
	if out.TokPending || out.OldTok == nil || out.NewTok == nil {
		t.Fatalf("pending=%v old=%v new=%v; a small file is coloured in its first answer", out.TokPending, out.OldTok != nil, out.NewTok != nil)
	}
}

func TestDifferTokOnlyEqualsInline(t *testing.T) {
	t.Parallel()
	d := NewDiffer(DifferOptions{Enhanced: true, Syntax: syntaxOn}, nil)
	req := Request{Path: "a.go", Old: src(largeGoOld), New: src(largeGoNew)}
	inline, err := d.Diff(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.Tok = TokOnly
	only, err := d.Diff(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(only.OldTok, inline.OldTok) || !reflect.DeepEqual(only.NewTok, inline.NewTok) || only.NewTok == nil {
		t.Fatal("TokOnly runs differ from the inline diff's")
	}
	if len(only.Result.Rows) != 0 || only.OldSum != inline.OldSum || only.NewSum != inline.NewSum || only.TokPending {
		t.Fatalf("TokOnly: rows=%d sums %q/%q vs %q/%q pending=%v", len(only.Result.Rows), only.OldSum, only.NewSum, inline.OldSum, inline.NewSum, only.TokPending)
	}
}

func TestDifferTokOnlyRenameUsesOldLang(t *testing.T) {
	t.Parallel()
	py := []byte("def f():\n    return 1\n")
	out, err := NewDiffer(DifferOptions{Syntax: syntaxOn}, nil).Diff(context.Background(),
		Request{Path: "a.py", OldPath: "a.go", Old: src(smallGoOld), New: src(py), Tok: TokOnly})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.OldTok, syntax.Lex("go", smallGoOld)) || !reflect.DeepEqual(out.NewTok, syntax.Lex("python", py)) {
		t.Fatal("each side must be lexed with its own path's language")
	}
}

func TestDifferDeferAbsentSide(t *testing.T) {
	t.Parallel()
	d := NewDiffer(DifferOptions{Syntax: syntaxOn}, nil)
	req := Request{Path: "a.go", Old: nil, New: src(largeGoNew), Tok: TokDefer}
	out, err := d.Diff(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if out.OldSum != "" || out.OldTok != nil || !out.TokPending || out.NewSum == "" {
		t.Fatalf("oldSum=%q oldTok=%v pending=%v newSum=%q", out.OldSum, out.OldTok != nil, out.TokPending, out.NewSum)
	}
	req.Tok = TokOnly
	only, err := d.Diff(context.Background(), req)
	if err != nil || only.OldTok != nil || only.NewTok == nil {
		t.Fatalf("TokOnly on an added file: err=%v old=%v new=%v", err, only.OldTok != nil, only.NewTok != nil)
	}
}

func TestDifferDeferSyntaxOff(t *testing.T) {
	t.Parallel()
	d := NewDiffer(DifferOptions{Syntax: func() bool { return false }}, nil)
	req := Request{Path: "a.go", Old: src(largeGoOld), New: src(largeGoNew), Tok: TokDefer}
	out, _ := d.Diff(context.Background(), req)
	if out.TokPending {
		t.Fatal("highlighting off: nothing can be pending")
	}
	req.Tok = TokOnly
	only, _ := d.Diff(context.Background(), req)
	if only.OldTok != nil || only.NewTok != nil {
		t.Fatal("highlighting off: TokOnly must answer no runs")
	}
}

func TestDifferDeferNeverPendingWithoutLexer(t *testing.T) {
	t.Parallel()
	d := NewDiffer(DifferOptions{Syntax: syntaxOn}, nil)
	huge := bytes.Repeat([]byte("// pad\n"), MaxSyntaxBytes/7+1)
	cases := map[string]Request{
		"binary":       {Path: "a.go", Old: src(append([]byte{0}, largeGoOld...)), New: src(largeGoNew)},
		"unknown lang": {Path: "a.unknownext", Old: src(largeGoOld), New: src(largeGoNew)},
		"over the cap": {Path: "a.go", Old: src(huge), New: src(append(huge, 'x', '\n'))},
	}
	for name, req := range cases {
		req.Tok = TokDefer
		out, err := d.Diff(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if out.TokPending {
			t.Errorf("%s: pending, but no colours can ever come", name)
		}
	}
}

func TestDifferAdjacentRevisionsLexSharedSideOnce(t *testing.T) {
	t.Parallel()
	d := NewDiffer(DifferOptions{Syntax: syntaxOn, Cached: true, Tokens: cache.NewFactory(0, 0).Cache("diff-tok")}, cache.NewFactory(0, 0).Cache("diff"))
	st := storeOf(t, d)
	var lexed atomic.Int32
	st.lex = func(ctx context.Context, lang string, b []byte) ([][]syntax.Tok, error) {
		lexed.Add(1)
		return syntax.LexCtx(ctx, lang, b)
	}
	a, b, c := smallGoOld, smallGoNew, []byte("package p\n\nfunc f() int { return 3 }\n")
	if _, err := d.Diff(context.Background(), Request{Key: "A..B", Path: "a.go", Old: src(a), New: src(b)}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Diff(context.Background(), Request{Key: "B..C", Path: "a.go", Old: src(b), New: src(c)}); err != nil {
		t.Fatal(err)
	}
	if n := lexed.Load(); n != 3 {
		t.Fatalf("lexed %d sides; want 3 (B is shared by both diffs)", n)
	}
}

func TestDifferRevisitAfterTokOnlyIsColoured(t *testing.T) {
	t.Parallel()
	d := NewDiffer(DifferOptions{Enhanced: true, Syntax: syntaxOn, Cached: true, Tokens: cache.NewFactory(0, 0).Cache("diff-tok")}, cache.NewFactory(0, 0).Cache("diff"))
	var reads atomic.Int32
	req := Request{Key: "k", Path: "a.go", Old: countSrc(largeGoOld, &reads), New: countSrc(largeGoNew, &reads)}
	req.Tok = TokDefer
	if first, _ := d.Diff(context.Background(), req); !first.TokPending {
		t.Fatal("first deferred diff should be pending")
	}
	req.Tok = TokOnly
	if _, err := d.Diff(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	before := reads.Load()
	req.Tok = TokDefer
	again, err := d.Diff(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if again.TokPending || again.OldTok == nil || again.NewTok == nil {
		t.Fatalf("revisit: pending=%v; want the stored colours attached", again.TokPending)
	}
	if n := reads.Load() - before; n != 0 {
		t.Fatalf("revisit read the sides %d times; want 0 (alignment and colours both cached)", n)
	}
}
