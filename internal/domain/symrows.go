package domain

import "sort"

// MemberState is what ONE side of a comparison holds for a path.
//
// Membership is not presence (ruling R6): a change-set ENUMERATES the files it
// deleted, so "this set deletes x" and "this set never touched x" are
// different facts — and the plain A/D/M listing folds them into one letter.
type MemberState string

const (
	MemberAbsent  MemberState = "absent"  // not a member of this set
	MemberPresent MemberState = "present" // a member with bytes at the set's source
	MemberDeleted MemberState = "deleted" // a member the set DELETES: no bytes
)

// SymRow is one aligned row of a bounded × bounded comparison: the same path
// on both sides, each side saying what it holds.
type SymRow struct {
	Path        string
	Left, Right MemberState
	// Differs reports that the row is in LinkComparison.Files — the listing
	// CompareSets produced. It is read from there and never re-derived, so an
	// aligned view cannot disagree with the listing beside it. A row that does
	// not differ is identical on both sides, or has no bytes on either.
	Differs bool
}

// SymmetricRows aligns the comparison's two sets over the UNION of their
// members, sorted by path. ok is false unless BOTH sets are bounded: an
// unbounded side has no member list to align against. Pure — every fact is
// already in the comparison; no git call is made.
func (c LinkComparison) SymmetricRows() (rows []SymRow, ok bool) {
	if !c.Left.Bounded() || !c.Right.Bounded() {
		return nil, false
	}
	differs := make(map[string]bool, len(c.Files))
	for _, f := range c.Files {
		differs[f.Path] = true
	}
	left, right := memberStates(c.Left), memberStates(c.Right)
	seen := make(map[string]bool, len(left)+len(right))
	paths := make([]string, 0, len(left)+len(right))
	for _, m := range []map[string]MemberState{left, right} {
		for p := range m {
			if !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	sort.Strings(paths)
	rows = make([]SymRow, 0, len(paths))
	for _, p := range paths {
		rows = append(rows, SymRow{Path: p, Left: stateOf(left, p), Right: stateOf(right, p), Differs: differs[p]})
	}
	return rows, true
}

// memberStates maps every MEMBER of a bounded set to present/deleted. The
// membership test is the path list, never Has: a set built with a nil has map
// answers Has(p) true for ANY path, member or not (compareBoundedPair's note).
func memberStates(fs FileSet) map[string]MemberState {
	out := make(map[string]MemberState, len(fs.paths))
	for _, p := range fs.paths {
		if fs.Has(p) {
			out[p] = MemberPresent
		} else {
			out[p] = MemberDeleted
		}
	}
	return out
}

func stateOf(m map[string]MemberState, p string) MemberState {
	if st, ok := m[p]; ok {
		return st
	}
	return MemberAbsent
}
