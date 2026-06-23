// Package fuzzy provides a pure file-path subsequence matcher with ranked results.
// It has no project imports (stdlib only), in the family of internal/textdiff and
// internal/commitgraph.
package fuzzy

import (
	"container/heap"
	"sort"
	"strings"
)

// Match pairs a candidate string with its score (higher = better match).
type Match struct {
	S     string
	Score int
}

// isBoundary reports whether b is a path/word separator character.
func isBoundary(b byte) bool {
	return b == '/' || b == '_' || b == '-' || b == '.' || b == ' '
}

// Score reports whether query is a case-insensitive subsequence of candidate and
// returns a rank score (higher = better). Bonuses: a match at a path/word boundary,
// a contiguous run, and a match in the basename (after the last '/'); mild length
// penalty so tighter/shorter paths win ties.
//
// ok=false means query is not a subsequence of candidate (no match).
func Score(query, candidate string) (int, bool) {
	if query == "" {
		return 0, true
	}
	q := strings.ToLower(query)
	c := strings.ToLower(candidate)
	lastSlash := strings.LastIndexByte(c, '/')
	score, qi, prev := 0, 0, -2
	for ci := 0; ci < len(c) && qi < len(q); ci++ {
		if q[qi] != c[ci] {
			continue
		}
		b := 1
		if ci == prev+1 {
			b += 3 // contiguous run bonus
		}
		if ci == 0 || isBoundary(c[ci-1]) {
			b += 5 // word/path boundary bonus
		}
		if ci > lastSlash {
			b += 2 // basename bonus
		}
		score += b
		prev = ci
		qi++
	}
	if qi != len(q) {
		return 0, false
	}
	score -= len(c) / 64 // mild length penalty
	return score, true
}

// matchHeap is a min-heap of Match values where h[0] is always the element
// that would be evicted first: lowest score, and for equal scores, the
// lexicographically largest path (since path-asc is the tiebreak, a larger
// path is the "worst" and should leave first).
type matchHeap []Match

func (h matchHeap) Len() int { return len(h) }
func (h matchHeap) Less(i, j int) bool {
	if h[i].Score != h[j].Score {
		return h[i].Score < h[j].Score // lower score floats to root (evicted first)
	}
	return h[i].S > h[j].S // equal score: larger path is "worse" → root
}
func (h matchHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *matchHeap) Push(x any)        { *h = append(*h, x.(Match)) }
func (h *matchHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// Rank filters candidates to those matching query and sorts best-first (ties
// broken by path for determinism), keeping at most limit results (limit<=0 = all).
//
// When query is empty, all candidates match with score 0 and original order is
// preserved (no sort), capped to limit.
//
// When limit > 0 and query is non-empty, Rank uses a bounded top-N heap selection
// (O(n log limit)) rather than sorting the full match set (O(n log n)), so that
// large candidate sets (100k+ paths) complete well within the 30ms budget.
func Rank(query string, candidates []string, limit int) []Match {
	if query == "" {
		// Empty query: all candidates match, preserve original order, cap.
		out := make([]Match, 0, len(candidates))
		for _, c := range candidates {
			out = append(out, Match{S: c, Score: 0})
			if limit > 0 && len(out) == limit {
				break
			}
		}
		return out
	}

	if limit <= 0 {
		// No limit: collect all matches then sort.
		out := make([]Match, 0, len(candidates))
		for _, c := range candidates {
			if s, ok := Score(query, c); ok {
				out = append(out, Match{S: c, Score: s})
			}
		}
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].Score != out[j].Score {
				return out[i].Score > out[j].Score
			}
			return out[i].S < out[j].S
		})
		return out
	}

	// Bounded top-N: maintain a min-heap of size limit.
	// When the heap is full, only insert if the new score exceeds the minimum.
	h := make(matchHeap, 0, limit+1)
	heap.Init(&h)
	for _, c := range candidates {
		s, ok := Score(query, c)
		if !ok {
			continue
		}
		m := Match{S: c, Score: s}
		if h.Len() < limit {
			heap.Push(&h, m)
		} else if s > h[0].Score || (s == h[0].Score && c < h[0].S) {
			// Replace the weakest entry.
			heap.Pop(&h)
			heap.Push(&h, m)
		}
	}
	// Extract and sort best-first.
	out := []Match(h)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].S < out[j].S
	})
	return out
}
