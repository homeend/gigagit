package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/gigagit/gg/internal/cache"
	"github.com/gigagit/gg/internal/textdiff"
)

func src(b []byte) ByteSource {
	return func(context.Context) ([]byte, error) { return b, nil }
}

func TestPlainDifferComputes(t *testing.T) {
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
	d := NewDiffer(DifferOptions{}, nil)
	out, _ := d.Diff(context.Background(), Request{Old: src([]byte("\x00\x01")), New: src([]byte("ok\n"))})
	if !out.Binary {
		t.Fatal("a NUL byte must yield Binary")
	}
}

func TestPlainDifferTooLarge(t *testing.T) {
	big := make([]byte, MaxDiffBytes+1)
	d := NewDiffer(DifferOptions{}, nil)
	out, _ := d.Diff(context.Background(), Request{Old: src(nil), New: src(big)})
	if !out.TooLarge {
		t.Fatal("an oversized side must yield TooLarge")
	}
}

func TestDiffSizeCountsRowText(t *testing.T) {
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
	d := NewDiffer(DifferOptions{}, nil)
	fail := func(context.Context) ([]byte, error) { return nil, errors.New("boom") }
	if _, err := d.Diff(context.Background(), Request{Old: fail, New: src(nil)}); err == nil {
		t.Fatal("source error must propagate")
	}
}

func TestCachedServesWithoutReinvokingSources(t *testing.T) {
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
