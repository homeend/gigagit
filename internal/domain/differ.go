package domain

import (
	"context"
	"image"
	"sync"

	"github.com/homeend/gigagit/internal/cache"
	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/termimg"
	"github.com/homeend/gigagit/internal/textdiff"
)

// MaxDiffBytes caps each side fed to the comparison engine; a larger side is
// reported as TooLarge instead of being aligned.
const MaxDiffBytes = 10 << 20

// MaxSyntaxBytes caps each side handed to the lexer (~1 s of chroma on a
// 1 MB file). Larger sides diff normally but render uncoloured.
const MaxSyntaxBytes = 1 << 20

// ByteSource lazily yields one side's content; invoked only on a cache miss. A
// nil ByteSource means an absent side (new or deleted file), matching
// textdiff.Compare(nil, x) / Compare(x, nil).
type ByteSource func(context.Context) ([]byte, error)

// Request is one diff to compute. Key is the cache key; "" disables caching
// for this call (e.g. working-tree diffs). Path is the repo-relative path
// used to select a syntax lexer; "" disables highlighting for this request.
type Request struct {
	Key  string
	Path string
	// OldPath is the path of the OLD side when it differs from Path — e.g. a
	// rename, or a two-sided compare of different files; empty = same as
	// Path. It only selects the old side's lexer, never the cache key: every
	// key that caches a rename already pins the revision pair that determines
	// the old name.
	OldPath  string
	Old, New ByteSource
}

// Diff is the cacheable outcome of one comparison. For a commit diff every
// field is immutable, so the whole struct is cached — including the
// binary/too-large verdicts, which avoids re-reading the blob to re-detect
// them on a later open.
type Diff struct {
	Result   textdiff.Result // valid unless Binary or TooLarge
	Binary   bool
	TooLarge bool
	// A Binary pair's sides that decode as images (PNG/JPEG/GIF), shrunk to
	// a bounded working copy (termimg.Shrink), with their format and byte
	// size — nil/"" for an absent or undecodable side. A frontend draws an
	// image pair instead of the binary placeholder.
	OldImg, NewImg     image.Image
	OldKind, NewKind   string
	OldBytes, NewBytes int
	OldDim, NewDim     image.Point // the ORIGINAL pixel size (the working copy is shrunk)
	// OldTok/NewTok hold syntax runs per SOURCE line (index = line number − 1,
	// the Row.LeftNo/RightNo numbering), so no row mapping is needed and the
	// shared rows stay untouched. nil when highlighting is off, the language
	// is unknown, or the side exceeds MaxSyntaxBytes.
	OldTok, NewTok [][]syntax.Tok
}

// Size implements cache.Sized: the diff's approximate heap weight in bytes for
// the cache byte budget — the row text on both sides (the dominant cost) plus
// a small per-row overhead, plus OldTok/NewTok's per-line token runs. Binary/
// too-large outcomes hold no rows or tokens.
//
// The cached rows AND OldTok/NewTok are shared across every cache hit (the
// loader aliases them into the view); treat all of it as READ-ONLY — an
// in-place mutation of a cached Row or Tok slice would corrupt the cache for
// all later opens.
func (d Diff) Size() int {
	n := 0
	for _, img := range []image.Image{d.OldImg, d.NewImg} {
		if img != nil {
			b := img.Bounds()
			n += 4 * b.Dx() * b.Dy() // the shrunk RGBA working copy
		}
	}
	for _, r := range d.Result.Rows {
		n += len(r.Left) + len(r.Right) + 48 // 48 ≈ Row + slice-header overhead
	}
	for _, side := range [][][]syntax.Tok{d.OldTok, d.NewTok} {
		for _, line := range side {
			n += 24 + 24*len(line) // 24 = slice header; syntax.Tok is 2 ints + a byte, padded to 24
		}
	}
	return n
}

// Differ computes an aligned diff, possibly from cache.
type Differ interface {
	Diff(ctx context.Context, req Request) (Diff, error)
}

// DifferOptions selects quality and caching at construction.
type DifferOptions struct {
	Enhanced bool // produce intraline spans
	Cached   bool // wrap in the caching decorator
	// Syntax is consulted per call to decide whether to lex; nil means never
	// highlight. It is a func (not a bool) so a live Service setting can flip
	// without rebuilding the Differ.
	Syntax func() bool
}

// NewDiffer composes a Differ: a plainDiffer(Enhanced) optionally wrapped by a
// caching decorator over c (c may be nil when Cached is false).
func NewDiffer(opts DifferOptions, c cache.Cache) Differ {
	var d Differ = plainDiffer{enhanced: opts.Enhanced, syntax: opts.Syntax}
	if opts.Cached {
		d = cachedDiffer{inner: d, cache: c, enhanced: opts.Enhanced, syntax: opts.Syntax}
	}
	return d
}

// diffImagePx bounds a decoded side's working copy (each side): wider than
// any terminal's cell count, and 1 MB of RGBA at most, so a run of image
// commits cannot evict the text diffs from the 64 MiB diff cache.
const diffImagePx = 512

// decodeImage decodes one binary side as an image, shrunk to diffImagePx;
// nil for an absent side or anything that is no PNG/JPEG/GIF.
func decodeImage(data []byte) (image.Image, string, image.Point) {
	if len(data) == 0 {
		return nil, "", image.Point{}
	}
	img, kind, err := termimg.Decode(data)
	if err != nil {
		return nil, "", image.Point{}
	}
	b := img.Bounds()
	return termimg.Shrink(img, diffImagePx, diffImagePx), kind, image.Point{X: b.Dx(), Y: b.Dy()}
}

type plainDiffer struct {
	enhanced bool
	syntax   func() bool
}

func (d plainDiffer) Diff(ctx context.Context, req Request) (Diff, error) {
	old, err := readSource(ctx, req.Old)
	if err != nil {
		return Diff{}, err
	}
	newB, err := readSource(ctx, req.New)
	if err != nil {
		return Diff{}, err
	}
	if len(old) > MaxDiffBytes || len(newB) > MaxDiffBytes {
		return Diff{TooLarge: true}, nil
	}
	if textdiff.IsBinary(old) || textdiff.IsBinary(newB) {
		out := Diff{Binary: true, OldBytes: len(old), NewBytes: len(newB)}
		out.OldImg, out.OldKind, out.OldDim = decodeImage(old)
		out.NewImg, out.NewKind, out.NewDim = decodeImage(newB)
		return out, nil
	}
	out := Diff{Result: textdiff.Compare(old, newB, textdiff.Options{Enhanced: d.enhanced})}
	if d.syntax != nil && d.syntax() && ctx.Err() == nil {
		// Each side picks its OWN grammar: a rename (or a two-sided compare of
		// different files) can put a .go old side opposite a .py new side, and
		// lexing one with the other's grammar produces nonsense runs. A side
		// whose language is unknown simply gets no tokens.
		newLang := syntax.Detect(req.Path)
		oldLang := newLang
		if req.OldPath != "" {
			oldLang = syntax.Detect(req.OldPath)
		}
		// Both sides can each cost ~1 s of chroma at MaxSyntaxBytes, and
		// they are independent — lex them concurrently so the worst case
		// is one side's time, not their sum. Each goroutine writes only
		// its own field.
		var wg sync.WaitGroup
		if oldLang != "" && len(old) <= MaxSyntaxBytes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				out.OldTok = syntax.Lex(oldLang, old)
			}()
		}
		if newLang != "" && len(newB) <= MaxSyntaxBytes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				out.NewTok = syntax.Lex(newLang, newB)
			}()
		}
		wg.Wait()
	}
	return out, nil
}

func readSource(ctx context.Context, s ByteSource) ([]byte, error) {
	if s == nil {
		return nil, nil
	}
	return s(ctx)
}

type cachedDiffer struct {
	inner    Differ
	cache    cache.Cache
	enhanced bool
	syntax   func() bool
}

func (d cachedDiffer) Diff(ctx context.Context, req Request) (Diff, error) {
	if req.Key == "" { // uncacheable (e.g. working-tree diff): compute directly
		return d.inner.Diff(ctx, req)
	}
	qkey := "p:"
	if d.enhanced {
		qkey = "e:"
	}
	if d.syntax != nil && d.syntax() {
		qkey += "s:"
	}
	qkey += req.Key
	return cache.Load[Diff](d.cache, qkey, func() (Diff, error) {
		out, err := d.inner.Diff(ctx, req)
		// plainDiffer skips lexing when the context died (the alignment
		// itself does not check ctx, so a cancel landing mid-Compare still
		// yields a complete Result with NO tokens). Caching that under the
		// syntax-ON key would render this file plain for every later open
		// until it is evicted, so fail the load instead — nothing is
		// stored, and the caller that cancelled discards the error anyway.
		if err == nil && ctx.Err() != nil {
			return Diff{}, ctx.Err()
		}
		return out, err
	})
}
