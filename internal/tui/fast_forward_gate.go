package tui

import "github.com/gigagit/gg/internal/model"

// feedDescendant reports whether selHash is a strict descendant of tipHash using
// ONLY the loaded commit feed's parent pointers (no git call) — the same DAG that
// draws the commit graph. descendant is true only when a parent path from selHash
// reaches tipHash. conclusive is false when the walk leaves the loaded window
// (an unknown parent, or tipHash not loaded), in which case the caller should
// fall back to showing the action and letting the op's IsAncestor guard decide.
//
// Pruning: a descendant of the tip is newer-or-equal in commit time, so any
// parent older than the tip cannot lead back to it and is skipped — this bounds
// the walk to the tip's generation.
func feedDescendant(commits []model.Commit, selHash, tipHash string) (descendant, conclusive bool) {
	if selHash == tipHash {
		return false, true // the tip itself is not "ahead" of itself
	}
	byHash := make(map[string]model.Commit, len(commits))
	for _, c := range commits {
		byHash[c.Hash] = c
	}
	tip, ok := byHash[tipHash]
	if !ok {
		return false, false // tip not in the loaded feed → inconclusive
	}
	if _, ok := byHash[selHash]; !ok {
		return false, false // selected not loaded (shouldn't happen) → inconclusive
	}

	seen := map[string]bool{}
	stack := []string{selHash}
	conclusive = true
	for len(stack) > 0 {
		h := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if h == tipHash {
			return true, true
		}
		if seen[h] {
			continue
		}
		seen[h] = true
		c := byHash[h] // present: selHash is in-map and we only push in-map parents
		for _, p := range c.Parents {
			pc, ok := byHash[p]
			if !ok {
				conclusive = false // ran off the loaded window
				continue
			}
			if pc.UnixTime < tip.UnixTime {
				continue // older than the tip → cannot lead back to it
			}
			stack = append(stack, p)
		}
	}
	return false, conclusive
}
