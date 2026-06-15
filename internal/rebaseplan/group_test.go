package rebaseplan

import (
	"reflect"
	"testing"
)

func TestGroups(t *testing.T) {
	p := Plan{Entries: []Entry{
		{Sha: "a", Action: Pick, Orig: "A"},
		{Sha: "b", Action: Squash, Orig: "B"},
		{Sha: "c", Action: Drop, Orig: "C"},
		{Sha: "d", Action: Reword, Orig: "D", NewMsg: "D2"},
	}}
	groups, err := p.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	want := []Group{
		{Target: 0, Squash: []int{1}}, // a with b squashed in (c dropped, skipped)
		{Target: 3, Squash: nil},      // d reworded
	}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("groups = %+v, want %+v", groups, want)
	}
}

func TestGroupsSquashFirstIsError(t *testing.T) {
	p := Plan{Entries: []Entry{{Sha: "a", Action: Squash, Orig: "A"}}}
	if _, err := p.Groups(); err == nil {
		t.Fatal("a leading squash (nothing older to meld into) must error")
	}
}

func TestMessageComposesSquash(t *testing.T) {
	p := Plan{Entries: []Entry{
		{Sha: "a", Action: Pick, Orig: "title A\n\nbody A\n"},
		{Sha: "b", Action: Squash, Orig: "msg B\n"},
		{Sha: "c", Action: Squash, Orig: "msg C\n"},
	}}
	got := p.Message(0)
	// Target message kept verbatim; a blank line; then each squashed commit's
	// message stacked line-by-line in the body.
	want := "title A\n\nbody A\n\nmsg B\nmsg C"
	if got != want {
		t.Fatalf("Message(0) = %q, want %q", got, want)
	}
}

func TestMessageRewordWins(t *testing.T) {
	p := Plan{Entries: []Entry{
		{Sha: "a", Action: Reword, Orig: "old\n", NewMsg: "new title\n\nnew body"},
		{Sha: "b", Action: Squash, Orig: "squashed\n"},
	}}
	got := p.Message(0)
	want := "new title\n\nnew body\n\nsquashed"
	if got != want {
		t.Fatalf("Message(0) = %q, want %q", got, want)
	}
}
