package domain

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/cache"
	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/textdiff"
)

func src(b []byte) ByteSource {
	return func(context.Context) ([]byte, error) { return b, nil }
}

func TestPlainDifferComputes(t *testing.T) {
	t.Parallel()
	d := NewDiffer(DifferOptions{Enhanced: true}, nil)
	out, err := d.Diff(context.Background(), Request{Old: src([]byte("a\n")), New: src([]byte("b\n"))})
	if err != nil {
		t.Fatal(err)
	}
	if out.Binary || out.TooLarge {
		t.Fatalf("unexpected flags: %+v", out)
	}
	if len(out.Result.Blocks) != 1 {
		t.Fatalf("blocks = %v, want one change", out.Result.Blocks)
	}
}

func TestPlainDifferNilSourceIsAbsentSide(t *testing.T) {
	t.Parallel()
	d := NewDiffer(DifferOptions{}, nil)
	out, err := d.Diff(context.Background(), Request{Old: nil, New: src([]byte("x\ny\n"))})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Result.Rows) != 2 {
		t.Fatalf("nil old side should diff as all-add, got %d rows", len(out.Result.Rows))
	}
	for _, r := range out.Result.Rows {
		if r.Kind != textdiff.Add {
			t.Fatalf("nil old side: every row should be Add, got %+v", r)
		}
	}
}

func TestPlainDifferBinary(t *testing.T) {
	t.Parallel()
	d := NewDiffer(DifferOptions{}, nil)
	out, _ := d.Diff(context.Background(), Request{Old: src([]byte("\x00\x01")), New: src([]byte("ok\n"))})
	if !out.Binary {
		t.Fatal("a NUL byte must yield Binary")
	}
}

func TestPlainDifferTooLarge(t *testing.T) {
	t.Parallel()
	big := make([]byte, MaxDiffBytes+1)
	d := NewDiffer(DifferOptions{}, nil)
	out, _ := d.Diff(context.Background(), Request{Old: src(nil), New: src(big)})
	if !out.TooLarge {
		t.Fatal("an oversized side must yield TooLarge")
	}
}

func TestDiffSizeCountsRowText(t *testing.T) {
	t.Parallel()
	d := NewDiffer(DifferOptions{}, nil)
	out, _ := d.Diff(context.Background(), Request{Old: src([]byte("abc\n")), New: src([]byte("abxy\n"))})
	if out.Size() <= 0 {
		t.Fatalf("a non-empty diff must report positive Size, got %d", out.Size())
	}
	// Binary/too-large outcomes hold no rows → near-zero weight.
	bin := Diff{Binary: true}
	if bin.Size() != 0 {
		t.Fatalf("binary outcome Size = %d, want 0", bin.Size())
	}
}

func TestPlainDifferSourceErrorPropagates(t *testing.T) {
	t.Parallel()
	d := NewDiffer(DifferOptions{}, nil)
	fail := func(context.Context) ([]byte, error) { return nil, errors.New("boom") }
	if _, err := d.Diff(context.Background(), Request{Old: fail, New: src(nil)}); err == nil {
		t.Fatal("source error must propagate")
	}
}

func TestCachedServesWithoutReinvokingSources(t *testing.T) {
	t.Parallel()
	c := cache.NewFactory(0, 0).Cache("diff")
	d := NewDiffer(DifferOptions{Enhanced: true, Cached: true}, c)
	calls := 0
	counting := func(context.Context) ([]byte, error) { calls++; return []byte("a\n"), nil }
	req := Request{Key: "k", Old: counting, New: src([]byte("b\n"))}
	d.Diff(context.Background(), req)
	d.Diff(context.Background(), req)
	if calls != 1 {
		t.Fatalf("source invoked %d times, want 1 (second served from cache)", calls)
	}
}

func TestCachedEmptyKeyNeverCaches(t *testing.T) {
	t.Parallel()
	c := cache.NewFactory(0, 0).Cache("diff")
	d := NewDiffer(DifferOptions{Cached: true}, c)
	calls := 0
	counting := func(context.Context) ([]byte, error) { calls++; return []byte("a\n"), nil }
	req := Request{Key: "", Old: counting, New: src([]byte("b\n"))}
	d.Diff(context.Background(), req)
	d.Diff(context.Background(), req)
	if calls != 2 {
		t.Fatalf("Key=='' must not cache; source invoked %d times, want 2", calls)
	}
}

func TestCachedQualityNamespacing(t *testing.T) {
	t.Parallel()
	c := cache.NewFactory(0, 0).Cache("diff")
	enh := NewDiffer(DifferOptions{Enhanced: true, Cached: true}, c)
	plain := NewDiffer(DifferOptions{Enhanced: false, Cached: true}, c)
	calls := 0
	counting := func(context.Context) ([]byte, error) { calls++; return []byte("a\n"), nil }
	enh.Diff(context.Background(), Request{Key: "k", Old: counting, New: src([]byte("b\n"))})
	plain.Diff(context.Background(), Request{Key: "k", Old: counting, New: src([]byte("b\n"))})
	if calls != 2 {
		t.Fatalf("enhanced and plain must not collide on the same key; calls=%d want 2", calls)
	}
}

func TestServiceDifferShareCache(t *testing.T) {
	t.Parallel()
	// Differ() never touches the repo, so a nil repo is fine here. Two calls
	// must share one cache, so a diff cached via the first is served via the
	// second without re-invoking the source.
	s := New(nil)
	calls := 0
	counting := func(context.Context) ([]byte, error) { calls++; return []byte("a\n"), nil }
	req := Request{Key: "k", Old: counting, New: src([]byte("b\n"))}
	if _, err := s.Differ().Diff(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Differ().Diff(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("the Service's Differ must share one cache; source called %d times, want 1", calls)
	}
}

func on() bool  { return true }
func off() bool { return false }

func TestPlainDifferLexesBothSidesByPath(t *testing.T) {
	t.Parallel()
	d := NewDiffer(DifferOptions{Enhanced: true, Syntax: on}, nil)
	out, err := d.Diff(context.Background(), Request{
		Path: "a.go",
		Old:  src([]byte("package a\n// c\n")),
		New:  src([]byte("package a\n// d\nvar x = 1\n")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.OldTok) != 2 || len(out.NewTok) != 3 {
		t.Fatalf("OldTok=%d NewTok=%d, want 2 and 3 (one per source line)", len(out.OldTok), len(out.NewTok))
	}
	if len(out.NewTok[1]) == 0 || out.NewTok[1][0].Class != syntax.Comment {
		t.Errorf("new line 2 should start with a Comment run, got %+v", out.NewTok[1])
	}
}

// TestPlainDifferLexesOldSideWithOldPath pins the rename case: the old side
// must be lexed with ITS OWN name's grammar, not the new name's. The Go
// keyword assertion is the discriminator — Python's lexer reads "package" as
// a plain name, so a shared-lexer regression turns that run to Name.
func TestPlainDifferLexesOldSideWithOldPath(t *testing.T) {
	t.Parallel()
	d := NewDiffer(DifferOptions{Enhanced: true, Syntax: on}, nil)
	out, err := d.Diff(context.Background(), Request{
		Path:    "a.py",
		OldPath: "a.go",
		Old:     src([]byte("package a\n// c\n")),
		New:     src([]byte("import os\n# c\n")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.OldTok) != 2 || len(out.NewTok) != 2 {
		t.Fatalf("OldTok=%d NewTok=%d, want 2 and 2 (one per source line)", len(out.OldTok), len(out.NewTok))
	}
	if len(out.OldTok[0]) == 0 || out.OldTok[0][0].Class != syntax.Keyword {
		t.Errorf("old line 1 should start with a Keyword run (Go 'package'), got %+v", out.OldTok[0])
	}
	if len(out.OldTok[1]) == 0 || out.OldTok[1][0].Class != syntax.Comment {
		t.Errorf("old line 2 should start with a Comment run (Go '//'), got %+v", out.OldTok[1])
	}
	if len(out.NewTok[1]) == 0 || out.NewTok[1][0].Class != syntax.Comment {
		t.Errorf("new line 2 should start with a Comment run (Python '#'), got %+v", out.NewTok[1])
	}
}

// TestPlainDifferOldPathWithoutLexerLeavesOldPlain pins the per-side gate: an
// old name no lexer matches yields no old tokens while the new side still
// colours.
func TestPlainDifferOldPathWithoutLexerLeavesOldPlain(t *testing.T) {
	t.Parallel()
	d := NewDiffer(DifferOptions{Enhanced: true, Syntax: on}, nil)
	out, err := d.Diff(context.Background(), Request{
		Path:    "a.go",
		OldPath: "a.zzz",
		Old:     src([]byte("package a\n")),
		New:     src([]byte("package a\n// c\n")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.OldTok != nil {
		t.Errorf("old side has no lexer, want nil OldTok, got %+v", out.OldTok)
	}
	if len(out.NewTok) != 2 || len(out.NewTok[1]) == 0 || out.NewTok[1][0].Class != syntax.Comment {
		t.Errorf("new side should still be lexed as Go, got %+v", out.NewTok)
	}
}

func TestPlainDifferNoTokensWhenOffUnknownOrLarge(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		opts DifferOptions
		req  Request
	}{
		{"syntax off", DifferOptions{Syntax: off}, Request{Path: "a.go", Old: src([]byte("a\n")), New: src([]byte("b\n"))}},
		{"nil Syntax", DifferOptions{}, Request{Path: "a.go", Old: src([]byte("a\n")), New: src([]byte("b\n"))}},
		{"no path", DifferOptions{Syntax: on}, Request{Old: src([]byte("a\n")), New: src([]byte("b\n"))}},
		{"unknown ext", DifferOptions{Syntax: on}, Request{Path: "a.zzz", Old: src([]byte("a\n")), New: src([]byte("b\n"))}},
		{"binary", DifferOptions{Syntax: on}, Request{Path: "a.go", Old: src(make([]byte, MaxSyntaxBytes+1)), New: src([]byte("b\n"))}},
	}
	for _, c := range cases {
		out, err := NewDiffer(c.opts, nil).Diff(context.Background(), c.req)
		if err != nil {
			t.Fatal(c.name, err)
		}
		if out.OldTok != nil || out.NewTok != nil {
			t.Errorf("%s: expected no tokens, got old=%v new=%v", c.name, out.OldTok, out.NewTok)
		}
	}
}

// TestPlainDifferSkipsLexingOnCancelledContext pins the guard in front of the
// concurrent lex: a caller that has already given up must not pay for two
// chroma passes (the diff itself is already computed by then, so the rows are
// still returned).
func TestPlainDifferSkipsLexingOnCancelledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := NewDiffer(DifferOptions{Syntax: on}, nil).Diff(ctx, Request{
		Path: "a.go",
		Old:  src([]byte("package a\n")),
		New:  src([]byte("package b\n")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.OldTok != nil || out.NewTok != nil {
		t.Errorf("a cancelled context must skip lexing, got old=%v new=%v", out.OldTok, out.NewTok)
	}
}

// TestCachedDifferDoesNotCacheATokenlessCancelledDiff guards the interaction
// between the cancelled-context lex skip and the cache: the alignment does not
// check ctx, so a cancel landing mid-Compare yields a complete Result with no
// tokens. Storing that under the syntax-ON key would render the file plain on
// every later open until eviction.
func TestCachedDifferDoesNotCacheATokenlessCancelledDiff(t *testing.T) {
	t.Parallel()
	c := cache.NewFactory(0, 0).Cache("diff")
	d := NewDiffer(DifferOptions{Enhanced: true, Cached: true, Syntax: on}, c)
	req := Request{Key: "k", Path: "a.go", Old: src([]byte("package a\n")), New: src([]byte("package b\nvar x = 1\n"))}

	dead, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.Diff(dead, req); err == nil {
		t.Fatal("a diff computed under a dead context must not succeed into the cache")
	}
	out, err := d.Diff(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if out.NewTok == nil {
		t.Error("the later live call must lex: the cancelled call poisoned the cache with a token-less diff")
	}
}

func TestPlainDifferPerSideMaxSyntaxBytesCap(t *testing.T) {
	t.Parallel()
	// Non-binary text just over MaxSyntaxBytes: unlike the all-zero "binary"
	// case above, this must diff and lex normally — only the oversized side's
	// tokens are dropped, the small side still gets highlighted.
	bigOld := bytes.Repeat([]byte("// pad\n"), MaxSyntaxBytes/7+1)
	out, err := NewDiffer(DifferOptions{Syntax: on}, nil).Diff(context.Background(), Request{
		Path: "a.go",
		Old:  src(bigOld),
		New:  src([]byte("package a\n")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Binary || out.TooLarge {
		t.Fatalf("large non-binary text under MaxDiffBytes must diff normally, got Binary=%v TooLarge=%v", out.Binary, out.TooLarge)
	}
	if out.OldTok != nil {
		t.Errorf("old side over MaxSyntaxBytes must carry no tokens, got %v", out.OldTok)
	}
	if out.NewTok == nil {
		t.Error("new side under MaxSyntaxBytes must still be tokenised despite the old side being too big")
	}
}

func TestCachedDifferSeparatesSyntaxOnOff(t *testing.T) {
	t.Parallel()
	c := cache.NewFactory(0, 0).Cache("diff")
	flag := true
	d := NewDiffer(DifferOptions{Enhanced: true, Cached: true, Syntax: func() bool { return flag }}, c)
	req := Request{Key: "k", Path: "a.go", Old: src([]byte("package a\n")), New: src([]byte("package b\n"))}
	withTok, _ := d.Diff(context.Background(), req)
	flag = false
	without, _ := d.Diff(context.Background(), req)
	if withTok.NewTok == nil {
		t.Fatal("first call with syntax on should carry tokens")
	}
	if without.NewTok != nil {
		t.Fatal("turning syntax off must not serve the tokenised cache entry")
	}
}
