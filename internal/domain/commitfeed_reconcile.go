package domain

import "github.com/homeend/gigagit/internal/model"

// reconcilePage merges a freshly walked page 0 into an already-accumulated,
// newest-first commit list WITHOUT discarding the pages the user paged in.
//
// It anchors on the first page commit the accumulation already holds. Everything
// above the anchor is new history (prepended); everything the accumulation held
// above the anchor's position is gone (an amend/reset/drop) and is trimmed. The
// page must then agree with the accumulation commit-for-commit from the anchor
// down — a shared hash alone is not enough, because an interleaved or reordered
// walk would silently corrupt the list.
//
// Returns ok=false whenever reconciliation is unsafe or pointless; the caller
// then falls back to a hard reset (replace everything with the page), which is
// the behavior that predates this function:
//
//   - the accumulation or the page is empty,
//   - no page commit is known (a rewrite, or more new commits than a page holds),
//   - the alignment check fails,
//   - the page already covers the whole remaining accumulation.
//
// skipDelta is how far git's RAW walk offset moved, so the caller can keep its
// --skip bookkeeping aligned: raw rows gained at the head minus entries trimmed
// from it. It counts raw page rows (duplicates included) even though the merged
// list stays unique, because --skip counts what git emitted, not what survived
// dedupe.
func reconcilePage(loaded, page []model.Commit) (merged []model.Commit, skipDelta int, ok bool) {
	if len(loaded) == 0 || len(page) == 0 {
		return nil, 0, false
	}
	at := make(map[string]int, len(loaded))
	for i, c := range loaded {
		if _, dup := at[c.Hash]; !dup {
			at[c.Hash] = i
		}
	}
	// Anchor: the first page commit we already hold. Every page commit above it
	// is unknown by construction, so the new head can never contain a commit that
	// merely moved up — that case becomes an alignment failure below instead.
	k, i := -1, -1
	for pi, c := range page {
		if li, known := at[c.Hash]; known {
			k, i = pi, li
			break
		}
	}
	if k < 0 {
		return nil, 0, false
	}
	// The page must not already cover everything we would keep: then the merge
	// equals the page and a plain reset is cheaper and keeps skip exact.
	if len(page)-k >= len(loaded)-i {
		return nil, 0, false
	}
	// Alignment: from the anchor down, both walks must agree commit-for-commit.
	for j := 0; k+j < len(page); j++ {
		if page[k+j].Hash != loaded[i+j].Hash {
			return nil, 0, false
		}
	}
	merged = make([]model.Commit, 0, k+len(loaded)-i)
	seen := make(map[string]bool, k)
	for _, c := range page[:k] {
		if seen[c.Hash] {
			continue // git can emit one commit under several refs
		}
		seen[c.Hash] = true
		merged = append(merged, c)
	}
	// The overlap takes the FRESH rows, not the loaded ones. Same commits, but
	// not the same data: a decoration is a property of the ref graph, not of the
	// commit, so tagging or moving a branch onto a commit already on screen
	// changes its row without changing its hash. Keeping the loaded copy there
	// left a new tag invisible until the feed was rebuilt from scratch.
	merged = append(merged, page[k:]...)
	// Below what the fresh walk reached, the loaded pages stand — that is what
	// makes a deep search survive a refresh. The guard above proved the page
	// stops short of the loaded tail, so this slice is in range and non-empty.
	merged = append(merged, loaded[i+len(page)-k:]...)
	return merged, k - i, true
}
