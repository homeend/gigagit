package domain

import (
	"context"

	"github.com/homeend/gigagit/internal/cache"
	"github.com/homeend/gigagit/internal/textdiff"
)

// MaxDiffBytes caps each side fed to the comparison engine; a larger side is
// reported as TooLarge instead of being aligned.
const MaxDiffBytes = 10 << 20

// ByteSource lazily yields one side's content; invoked only on a cache miss. A
// nil ByteSource means an absent side (new or deleted file), matching
// textdiff.Compare(nil, x) / Compare(x, nil).
type ByteSource func(context.Context) ([]byte, error)

// Request is one diff to compute. Key is the cache key; "" disables caching
// for this call (e.g. working-tree diffs).
type Request struct {
	Key      string
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
}

// Size implements cache.Sized: the diff's approximate heap weight in bytes for
// the cache byte budget — the row text on both sides (the dominant cost) plus
// a small per-row overhead. Binary/too-large outcomes hold no rows.
//
// The cached rows are shared across every cache hit (the loader aliases them
// into the view); treat them as READ-ONLY — an in-place mutation of a cached
// Row would corrupt the cache for all later opens.
func (d Diff) Size() int {
	n := 0
	for _, r := range d.Result.Rows {
		n += len(r.Left) + len(r.Right) + 48 // 48 ≈ Row + slice-header overhead
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
}

// NewDiffer composes a Differ: a plainDiffer(Enhanced) optionally wrapped by a
// caching decorator over c (c may be nil when Cached is false).
func NewDiffer(opts DifferOptions, c cache.Cache) Differ {
	var d Differ = plainDiffer{enhanced: opts.Enhanced}
	if opts.Cached {
		d = cachedDiffer{inner: d, cache: c, enhanced: opts.Enhanced}
	}
	return d
}

type plainDiffer struct{ enhanced bool }

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
		return Diff{Binary: true}, nil
	}
	return Diff{Result: textdiff.Compare(old, newB, textdiff.Options{Enhanced: d.enhanced})}, nil
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
}

func (d cachedDiffer) Diff(ctx context.Context, req Request) (Diff, error) {
	if req.Key == "" { // uncacheable (e.g. working-tree diff): compute directly
		return d.inner.Diff(ctx, req)
	}
	qkey := "p:" + req.Key
	if d.enhanced {
		qkey = "e:" + req.Key
	}
	return cache.Load[Diff](d.cache, qkey, func() (Diff, error) {
		return d.inner.Diff(ctx, req)
	})
}
