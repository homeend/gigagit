// Package changeset compares two base-relative change sets — the set of paths
// an operation's before and after states report as touched — and says what the
// operation itself moved. It is a DAG leaf: stdlib only, no git.
package changeset

import "sort"

// Entry is one path and the status git reported for it (A, M, D, T).
type Entry struct {
	Status byte
	Path   string
}

// Report is the difference between two change sets.
//
// Added is `after - before`: changes the OPERATION introduced to the branch's
// change set. This is the alarming direction — a path that appears, or whose
// status letter changed (M -> A is a resurrected file).
//
// Removed is `before - after`, which is usually benign: upstream absorbed the
// change (an identical fix, a cherry-pick), so it correctly cancels. Reported,
// never alarmed.
type Report struct {
	Added   []Entry
	Removed []Entry
}

// Drifted reports whether the operation introduced anything. Only the Added
// direction counts; see Report.
func (r Report) Drifted() bool { return len(r.Added) > 0 }

// Compare diffs the two sets by (status, path). Order of the inputs is
// irrelevant; both outputs are sorted by path then status so callers and tests
// see a stable order.
func Compare(before, after []Entry) Report {
	in := func(es []Entry) map[Entry]bool {
		m := make(map[Entry]bool, len(es))
		for _, e := range es {
			m[e] = true
		}
		return m
	}
	b, a := in(before), in(after)

	var rep Report
	for _, e := range after {
		if !b[e] {
			rep.Added = append(rep.Added, e)
		}
	}
	for _, e := range before {
		if !a[e] {
			rep.Removed = append(rep.Removed, e)
		}
	}
	sortEntries(rep.Added)
	sortEntries(rep.Removed)
	return rep
}

func sortEntries(es []Entry) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].Path != es[j].Path {
			return es[i].Path < es[j].Path
		}
		return es[i].Status < es[j].Status
	})
}
