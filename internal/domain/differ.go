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

// InlineSyntaxBytes is the largest side a TokDefer request still lexes
// before answering (chroma ≈ 1.3 µs/byte → under ~0.1 s): ordinary files
// arrive coloured, only big ones show plain first.
const InlineSyntaxBytes = 64 << 10

// TokMode says when a Diff call lexes.
type TokMode uint8

const (
	// TokInline lexes every side before Diff returns (the default).
	TokInline TokMode = iota
	// TokDefer answers the alignment at once: a side over InlineSyntaxBytes
	// that the token store does not hold yet is left plain and marks the
	// diff TokPending; a TokOnly request for the same Request colours it.
	TokDefer
	// TokOnly answers only the sums and runs of both sides: no alignment.
	TokOnly
)

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
	// Tok selects when syntax runs are produced; the zero value lexes before
	// returning, as every caller always had it.
	Tok TokMode
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
	// OldRaw/NewRaw are the ORIGINAL bytes of a side that decoded as an
	// image (nil otherwise): the web serves them at full resolution.
	OldRaw, NewRaw []byte
	// OldTok/NewTok hold syntax runs per SOURCE line (index = line number − 1,
	// the Row.LeftNo/RightNo numbering), so no row mapping is needed and the
	// shared rows stay untouched. nil when highlighting is off, the language
	// is unknown, or the side exceeds MaxSyntaxBytes.
	OldTok, NewTok [][]syntax.Tok
	// TokPending: a TokDefer call left at least one lexable side plain; a
	// TokOnly call with the same Request answers its runs.
	TokPending bool
	// OldSum/NewSum name each side's bytes (contentSum: hex SHA-256, "" for
	// an absent side). Runs from a TokOnly call belong to this diff only
	// when both sums are equal — a working-tree side can change between the
	// two calls.
	OldSum, NewSum string
	// oldLen/newLen are the text sides' byte sizes (the inline threshold and
	// MaxSyntaxBytes are checked on a cached alignment without re-reading).
	oldLen, newLen int
}

// Size implements cache.Sized: the diff's approximate heap weight in bytes for
// the cache byte budget — the row text on both sides (the dominant cost) plus
// a small per-row overhead. Binary/too-large outcomes hold no rows.
//
// The cached rows AND the token store's OldTok/NewTok are shared across
// every hit (the loader aliases them into the view); treat all of it as
// READ-ONLY — an
// in-place mutation of a cached Row or Tok slice would corrupt the cache for
// all later opens.
func (d Diff) Size() int {
	n := len(d.OldRaw) + len(d.NewRaw)
	for _, img := range []image.Image{d.OldImg, d.NewImg} {
		if img != nil {
			b := img.Bounds()
			n += 4 * b.Dx() * b.Dy() // the shrunk RGBA working copy
		}
	}
	for _, r := range d.Result.Rows {
		n += len(r.Left) + len(r.Right) + 48 // 48 ≈ Row + slice-header overhead
	}
	return n // OldTok/NewTok are never in a cached Diff: they live in the token store
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
	// Tokens caches syntax runs by content (see tokStore); nil = runs are
	// shared while a lex runs but not kept. Ignored when Highlighter is set.
	Tokens cache.Cache
	// Highlighter, when set, is the store the differ lexes through — the
	// Service's one, shared with every other surface that shows a file.
	Highlighter *Highlighter
}

// NewDiffer composes a Differ: a plainDiffer(Enhanced) optionally wrapped by a
// caching decorator over c (c may be nil when Cached is false).
func NewDiffer(opts DifferOptions, c cache.Cache) Differ {
	toks := newTokStore(opts.Tokens)
	if opts.Highlighter != nil {
		toks = opts.Highlighter.store
	}
	p := plainDiffer{enhanced: opts.Enhanced, syntax: opts.Syntax, toks: toks}
	if opts.Cached {
		return cachedDiffer{inner: p, cache: c, enhanced: opts.Enhanced}
	}
	return p
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
	toks     *tokStore // nil (a bare plainDiffer{}) = a fresh uncached store per call
}

func (d plainDiffer) store() *tokStore {
	if d.toks != nil {
		return d.toks
	}
	return newTokStore(nil)
}

func (d plainDiffer) Diff(ctx context.Context, req Request) (Diff, error) {
	if req.Tok == TokOnly {
		return d.tokOnly(ctx, req, nil)
	}
	out, old, newB, err := d.align(ctx, req)
	if err != nil {
		return Diff{}, err
	}
	d.attach(ctx, &out, req, func() ([]byte, []byte, error) { return old, newB, nil })
	return out, nil
}

// align reads both sides and compares them: the rows, the binary/too-large
// verdicts and each side's content sum, never syntax runs. It also hands
// back the bytes it read so the caller can lex them without a second read.
func (d plainDiffer) align(ctx context.Context, req Request) (Diff, []byte, []byte, error) {
	old, newB, err := readSides(ctx, req)
	if err != nil {
		return Diff{}, nil, nil, err
	}
	if len(old) > MaxDiffBytes || len(newB) > MaxDiffBytes {
		return Diff{TooLarge: true}, nil, nil, nil
	}
	if textdiff.IsBinary(old) || textdiff.IsBinary(newB) {
		out := Diff{Binary: true, OldBytes: len(old), NewBytes: len(newB), OldSum: contentSum(old), NewSum: contentSum(newB)}
		out.OldImg, out.OldKind, out.OldDim = decodeImage(old)
		out.NewImg, out.NewKind, out.NewDim = decodeImage(newB)
		if out.OldImg != nil {
			out.OldRaw = old
		}
		if out.NewImg != nil {
			out.NewRaw = newB
		}
		return out, old, newB, nil
	}
	out := Diff{
		Result: textdiff.Compare(old, newB, textdiff.Options{Enhanced: d.enhanced}),
		OldSum: contentSum(old), NewSum: contentSum(newB),
		oldLen: len(old), newLen: len(newB),
	}
	return out, old, newB, nil
}

func readSides(ctx context.Context, req Request) ([]byte, []byte, error) {
	old, err := readSource(ctx, req.Old)
	if err != nil {
		return nil, nil, err
	}
	newB, err := readSource(ctx, req.New)
	if err != nil {
		return nil, nil, err
	}
	return old, newB, nil
}

// sideLangs is each side's grammar: a rename (or a two-sided compare of
// different files) can put a .go old side opposite a .py new side, and
// lexing one with the other's grammar produces nonsense runs. "" = no lexer
// (unknown language, or highlighting off).
func (d plainDiffer) sideLangs(req Request) (oldLang, newLang string) {
	if d.syntax == nil || !d.syntax() {
		return "", ""
	}
	newLang = syntax.Detect(req.Path)
	oldLang = newLang
	if req.OldPath != "" {
		oldLang = syntax.Detect(req.OldPath)
	}
	return oldLang, newLang
}

// lexable reports whether a side can ever carry runs: a known language, a
// present side, and no larger than MaxSyntaxBytes. Anything else is plain
// for good and never pending.
func lexable(lang, sum string, n int) bool {
	return lang != "" && sum != "" && n <= MaxSyntaxBytes
}

// attach puts syntax runs on an aligned text diff, side by side: a side the
// token store already holds is attached as is; a side to lex now (TokInline,
// or at most InlineSyntaxBytes) is lexed — both concurrently, the worst case
// being one side's time; a larger side in TokDefer mode is left for a
// TokOnly request and marks the diff TokPending. read yields the sides'
// bytes and is called only when something must be lexed. A lex the context
// stopped leaves its side plain.
func (d plainDiffer) attach(ctx context.Context, out *Diff, req Request, read func() ([]byte, []byte, error)) {
	if out.Binary || out.TooLarge {
		return
	}
	oldLang, newLang := d.sideLangs(req)
	st := d.store()
	type side struct {
		lang, sum string
		n         int
		toks      *[][]syntax.Tok
		lexNow    bool
	}
	sides := []*side{
		{lang: oldLang, sum: out.OldSum, n: out.oldLen, toks: &out.OldTok},
		{lang: newLang, sum: out.NewSum, n: out.newLen, toks: &out.NewTok},
	}
	need := false
	for _, s := range sides {
		if !lexable(s.lang, s.sum, s.n) {
			continue
		}
		if toks, ok := st.Get(s.lang, s.sum); ok {
			*s.toks = toks
			continue
		}
		if req.Tok == TokInline || s.n <= InlineSyntaxBytes {
			s.lexNow, need = true, true
			continue
		}
		out.TokPending = true
	}
	if !need {
		return
	}
	old, newB, err := read()
	if err != nil {
		return
	}
	srcs := [][]byte{old, newB}
	var wg sync.WaitGroup
	for i, s := range sides {
		if !s.lexNow {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if toks, err := st.Lex(ctx, s.lang, s.sum, srcs[i]); err == nil {
				*s.toks = toks
			}
		}()
	}
	wg.Wait()
}

// tokOnly answers just the runs of both sides (TokOnly): the sums, and every
// lexable side lexed through the token store — no alignment. sums, when
// non-nil, are the content sums and lengths a cached alignment already knows,
// letting a fully stored pair answer without reading the sides.
func (d plainDiffer) tokOnly(ctx context.Context, req Request, known *Diff) (Diff, error) {
	oldLang, newLang := d.sideLangs(req)
	st := d.store()
	if known != nil {
		out := Diff{OldSum: known.OldSum, NewSum: known.NewSum}
		okOld, okNew := true, true
		if lexable(oldLang, known.OldSum, known.oldLen) {
			out.OldTok, okOld = st.Get(oldLang, known.OldSum)
		}
		if lexable(newLang, known.NewSum, known.newLen) {
			out.NewTok, okNew = st.Get(newLang, known.NewSum)
		}
		if okOld && okNew && !known.Binary && !known.TooLarge {
			return out, nil
		}
	}
	old, newB, err := readSides(ctx, req)
	if err != nil {
		return Diff{}, err
	}
	out := Diff{OldSum: contentSum(old), NewSum: contentSum(newB)}
	if len(old) > MaxDiffBytes || len(newB) > MaxDiffBytes || textdiff.IsBinary(old) || textdiff.IsBinary(newB) {
		return out, nil
	}
	var wg sync.WaitGroup
	var oldErr, newErr error
	if lexable(oldLang, out.OldSum, len(old)) {
		wg.Add(1)
		go func() { defer wg.Done(); out.OldTok, oldErr = st.Lex(ctx, oldLang, out.OldSum, old) }()
	}
	if lexable(newLang, out.NewSum, len(newB)) {
		wg.Add(1)
		go func() { defer wg.Done(); out.NewTok, newErr = st.Lex(ctx, newLang, out.NewSum, newB) }()
	}
	wg.Wait()
	if oldErr != nil {
		return Diff{}, oldErr
	}
	if newErr != nil {
		return Diff{}, newErr
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
	inner    plainDiffer
	cache    cache.Cache
	enhanced bool
}

// Diff caches the ALIGNMENT under the request key (the runs live in the
// token store, keyed by content, so highlighting on/off and a walk through
// history share them). A call that must lex reads the sides again when the
// alignment came from the cache.
func (d cachedDiffer) Diff(ctx context.Context, req Request) (Diff, error) {
	if req.Key == "" { // uncacheable (e.g. working-tree diff): compute directly
		return d.inner.Diff(ctx, req)
	}
	qkey := "p:"
	if d.enhanced {
		qkey = "e:"
	}
	qkey += req.Key
	if req.Tok == TokOnly {
		var known *Diff
		if v, ok := d.cache.Get(qkey); ok {
			k := v.(Diff)
			known = &k
		}
		return d.inner.tokOnly(ctx, req, known)
	}
	var old, newB []byte
	loaded := false
	out, err := cache.Load[Diff](d.cache, qkey, func() (Diff, error) {
		al, o, n, err := d.inner.align(ctx, req)
		old, newB, loaded = o, n, true
		return al, err
	})
	if err != nil {
		return Diff{}, err
	}
	d.inner.attach(ctx, &out, req, func() ([]byte, []byte, error) {
		if loaded {
			return old, newB, nil
		}
		return readSides(ctx, req)
	})
	// A context that died mid-call can leave a side plain that a live call
	// would colour; the caller that cancelled discards this anyway, so say
	// so rather than hand back a half-coloured diff.
	if ctx.Err() != nil {
		return Diff{}, ctx.Err()
	}
	return out, nil
}
