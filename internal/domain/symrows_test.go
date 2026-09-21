package domain

import (
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestSymmetricRowsAlignsTheUnionOfMembers(t *testing.T) {
	t.Parallel()
	ep, err := model.CommitEndpoint("1111111111111111111111111111111111111111")
	if err != nil {
		t.Fatal(err)
	}
	left := boundedSetWith(ep,
		[]string{"both-differ", "both-same", "only-left", "left-deletes", "both-delete", "left-deletes-only"},
		map[string]bool{"both-differ": true, "both-same": true, "only-left": true})
	right := boundedSetWith(ep,
		[]string{"both-same", "only-right", "both-differ", "left-deletes", "both-delete"},
		map[string]bool{"both-differ": true, "both-same": true, "only-right": true, "left-deletes": true})
	c := LinkComparison{Left: left, Right: right, Files: []model.CommitFile{
		{Status: "M", Path: "both-differ"}, {Status: "A", Path: "left-deletes"},
		{Status: "D", Path: "only-left"}, {Status: "A", Path: "only-right"},
	}}
	got, ok := c.SymmetricRows()
	if !ok {
		t.Fatal("two bounded sets must align")
	}
	want := []SymRow{
		{"both-delete", MemberDeleted, MemberDeleted, false},
		{"both-differ", MemberPresent, MemberPresent, true},
		{"both-same", MemberPresent, MemberPresent, false},
		{"left-deletes", MemberDeleted, MemberPresent, true},
		{"left-deletes-only", MemberDeleted, MemberAbsent, false},
		{"only-left", MemberPresent, MemberAbsent, true},
		{"only-right", MemberAbsent, MemberPresent, true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows:\n got %+v\nwant %+v", got, want)
	}
}

// A set with a nil has map ("every member has bytes") answers Has true for ANY
// path: membership must come from the path list, or a non-member reads present.
func TestSymmetricRowsMembershipIsThePathListNotHas(t *testing.T) {
	t.Parallel()
	ep, _ := model.CommitEndpoint("1111111111111111111111111111111111111111")
	c := LinkComparison{
		Left:  boundedSetWith(ep, []string{"a"}, nil),
		Right: boundedSetWith(ep, []string{"b"}, nil),
	}
	got, _ := c.SymmetricRows()
	want := []SymRow{{"a", MemberPresent, MemberAbsent, false}, {"b", MemberAbsent, MemberPresent, false}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSymmetricRowsNeedTwoBoundedSets(t *testing.T) {
	t.Parallel()
	ep, _ := model.CommitEndpoint("1111111111111111111111111111111111111111")
	bounded := boundedSetWith(ep, []string{"a"}, nil)
	for _, c := range []LinkComparison{
		{Left: unboundedSet(ep), Right: bounded},
		{Left: bounded, Right: unboundedSet(ep)},
	} {
		if rows, ok := c.SymmetricRows(); ok || rows != nil {
			t.Fatalf("an unbounded side has no members to align: %+v %v", rows, ok)
		}
	}
}
