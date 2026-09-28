// Package fuzzy ranks candidate strings (file paths, branch names) against a
// query written in fzf's extended-search syntax, scored by fzf's own matcher
// (github.com/junegunn/fzf/src/algo, imported in-process — no fzf binary is
// involved, so every platform ranks the same way). Stdlib + fzf only.
//
// Query syntax (space-separated terms, ALL must match):
//
//	darwin     fuzzy: the letters in order, any distance apart (a subsequence)
//	'darwin    exact: the literal substring
//	^docs      prefix
//	.md$       suffix
//	^name$     the whole string
//	!term      NOT: reject candidates containing the literal term
//	           (!^pre, !suf$ likewise)
//
// A term with only an upper-case letter or more is matched case-sensitively
// (fzf's smart case); lower-case terms match either case.
package fuzzy

import (
	"container/heap"
	"sort"
	"strings"
	"unicode"

	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

// Match pairs a candidate string with its score (higher = better match).
type Match struct {
	S     string
	Score int
}

// Kind is how a term matches.
type Kind int

const (
	Fuzzy  Kind = iota // a subsequence
	Exact              // a literal substring ('term)
	Prefix             // ^term
	Suffix             // term$
	Equal              // ^term$
)

func (k Kind) String() string {
	return [...]string{"fuzzy", "exact", "prefix", "suffix", "equal"}[k]
}

// Term is one space-separated unit of a query.
type Term struct {
	Text   string
	Kind   Kind
	Negate bool // !term: the term must NOT match

	caseSensitive bool
	pattern       []rune
}

func (t Term) String() string {
	s := t.Kind.String() + ":" + t.Text
	if t.Negate {
		s = "!" + s
	}
	return s
}

// Query is a parsed query: the terms a candidate must satisfy, all of them.
type Query struct {
	Terms []Term
}

// Parse splits query into terms on spaces and reads each term's markers. A
// marker with nothing behind it (a query still being typed) yields no term.
func Parse(query string) Query {
	var q Query
	for _, word := range strings.Fields(query) {
		t := Term{Kind: Fuzzy}
		if strings.HasPrefix(word, "!") {
			t.Negate = true
			word = word[1:]
		}
		switch {
		case strings.HasPrefix(word, "'"):
			t.Kind, word = Exact, word[1:]
		case strings.HasPrefix(word, "^") && strings.HasSuffix(word, "$") && len(word) > 1:
			t.Kind, word = Equal, word[1:len(word)-1]
		case strings.HasPrefix(word, "^"):
			t.Kind, word = Prefix, word[1:]
		case strings.HasSuffix(word, "$"):
			t.Kind, word = Suffix, word[:len(word)-1]
		}
		if word == "" {
			continue
		}
		if t.Negate && t.Kind == Fuzzy {
			t.Kind = Exact // fzf: !term rejects the literal, not a subsequence
		}
		t.Text = word
		t.caseSensitive = strings.IndexFunc(word, unicode.IsUpper) >= 0
		t.pattern = []rune(word)
		if !t.caseSensitive {
			t.pattern = []rune(strings.ToLower(word))
		}
		q.Terms = append(q.Terms, t)
	}
	return q
}

// bonus scheme: path separators are word boundaries.
var _ = algo.Init("path")

// matcher scores candidates against one query, reusing fzf's scratch slab
// across calls (the slab is what keeps FuzzyMatchV2 allocation-free).
type matcher struct {
	q    Query
	slab *util.Slab
}

func newMatcher(q Query) *matcher {
	return &matcher{q: q, slab: util.MakeSlab(100*1024, 2048)}
}

// score reports whether candidate satisfies every term and the sum of the
// terms' scores (a negated term contributes nothing).
func (m *matcher) score(candidate string) (int, bool) {
	chars := util.ToChars([]byte(candidate))
	total := 0
	for i := range m.q.Terms {
		t := &m.q.Terms[i]
		var fn algo.Algo
		switch t.Kind {
		case Fuzzy:
			fn = algo.FuzzyMatchV2
		case Exact:
			fn = algo.ExactMatchNaive
		case Prefix:
			fn = algo.PrefixMatch
		case Suffix:
			fn = algo.SuffixMatch
		case Equal:
			fn = algo.EqualMatch
		}
		res, _ := fn(t.caseSensitive, false, true, &chars, t.pattern, false, m.slab)
		matched := res.Start >= 0
		if matched == t.Negate {
			return 0, false
		}
		if matched {
			total += res.Score
		}
	}
	return total, true
}

// Score reports whether candidate matches query and its score (higher =
// better). ok=false means no match. Rank is the bulk form.
func Score(query, candidate string) (int, bool) {
	return newMatcher(Parse(query)).score(candidate)
}

// better orders matches best-first: score descending, then the shorter
// candidate (a tighter hit), then the path for determinism.
func better(a, b Match) bool {
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	if len(a.S) != len(b.S) {
		return len(a.S) < len(b.S)
	}
	return a.S < b.S
}

// matchHeap is a min-heap of Match values where h[0] is always the element
// that would be evicted first: the worst under better.
type matchHeap []Match

func (h matchHeap) Len() int           { return len(h) }
func (h matchHeap) Less(i, j int) bool { return better(h[j], h[i]) }
func (h matchHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *matchHeap) Push(x any)        { *h = append(*h, x.(Match)) }
func (h *matchHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// Rank filters candidates to those matching query and sorts best-first,
// keeping at most limit results (limit<=0 = all).
//
// When query is empty (or only markers), all candidates match with score 0
// and original order is preserved (no sort), capped to limit.
//
// When limit > 0, Rank uses a bounded top-N heap selection (O(n log limit))
// rather than sorting the full match set, so that large candidate sets
// (100k+ paths) stay cheap.
func Rank(query string, candidates []string, limit int) []Match {
	q := Parse(query)
	if len(q.Terms) == 0 {
		out := make([]Match, 0, len(candidates))
		for _, c := range candidates {
			out = append(out, Match{S: c, Score: 0})
			if limit > 0 && len(out) == limit {
				break
			}
		}
		return out
	}
	m := newMatcher(q)

	if limit <= 0 {
		out := make([]Match, 0, len(candidates))
		for _, c := range candidates {
			if s, ok := m.score(c); ok {
				out = append(out, Match{S: c, Score: s})
			}
		}
		sort.SliceStable(out, func(i, j int) bool { return better(out[i], out[j]) })
		return out
	}

	h := make(matchHeap, 0, limit+1)
	heap.Init(&h)
	for _, c := range candidates {
		s, ok := m.score(c)
		if !ok {
			continue
		}
		mt := Match{S: c, Score: s}
		if h.Len() < limit {
			heap.Push(&h, mt)
		} else if better(mt, h[0]) {
			heap.Pop(&h)
			heap.Push(&h, mt)
		}
	}
	out := make([]Match, len(h))
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = heap.Pop(&h).(Match)
	}
	return out
}
