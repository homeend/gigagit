package changeset

import "testing"

func e(st byte, p string) Entry { return Entry{Status: st, Path: p} }

func TestCompare(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		before, after []Entry
		added         []Entry
		removed       []Entry
		drifted       bool
	}{
		{
			name:   "identical sets do not drift",
			before: []Entry{e('M', "a.txt"), e('A', "b.txt")},
			after:  []Entry{e('A', "b.txt"), e('M', "a.txt")},
		},
		{
			// The motivating case: upstream deleted f3, the branch had modified
			// it, and a modify/delete conflict was resolved the wrong way, so the
			// branch now reads as ADDING a file it never added.
			name:    "status change M->A is resurrection and alarms",
			before:  []Entry{e('M', "f3.txt"), e('A', "f4.txt")},
			after:   []Entry{e('A', "f3.txt"), e('A', "f4.txt")},
			added:   []Entry{e('A', "f3.txt")},
			removed: []Entry{e('M', "f3.txt")},
			drifted: true,
		},
		{
			name:    "a path only in after alarms",
			before:  []Entry{e('M', "a.txt")},
			after:   []Entry{e('M', "a.txt"), e('A', "new.txt")},
			added:   []Entry{e('A', "new.txt")},
			drifted: true,
		},
		{
			// Upstream absorbed the change (identical fix, cherry-pick). Reported,
			// but this direction is the quiet one.
			name:    "a path only in before is reported, not alarmed",
			before:  []Entry{e('M', "a.txt"), e('M', "gone.txt")},
			after:   []Entry{e('M', "a.txt")},
			removed: []Entry{e('M', "gone.txt")},
			drifted: false,
		},
		{
			name:   "both empty",
			before: nil, after: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Compare(tc.before, tc.after)
			if !equal(got.Added, tc.added) {
				t.Errorf("Added = %v, want %v", got.Added, tc.added)
			}
			if !equal(got.Removed, tc.removed) {
				t.Errorf("Removed = %v, want %v", got.Removed, tc.removed)
			}
			if got.Drifted() != tc.drifted {
				t.Errorf("Drifted() = %v, want %v", got.Drifted(), tc.drifted)
			}
		})
	}
}

func equal(a, b []Entry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCompareIsDeterministicallyOrdered(t *testing.T) {
	t.Parallel()
	before := []Entry{e('M', "z.txt"), e('M', "a.txt")}
	after := []Entry{e('A', "z.txt"), e('A', "a.txt")}
	got := Compare(before, after)
	if len(got.Added) != 2 || got.Added[0].Path != "a.txt" || got.Added[1].Path != "z.txt" {
		t.Errorf("Added = %v, want sorted by path", got.Added)
	}
}
