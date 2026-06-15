package rebaseplan

import (
	"reflect"
	"testing"
)

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	p := Plan{Entries: []Entry{
		{Sha: "aaa", Action: Pick, Orig: "first\n"},
		{Sha: "bbb", Action: Reword, Orig: "second\n", NewMsg: "second reworded"},
		{Sha: "ccc", Action: Squash, Orig: "third\n"},
		{Sha: "ddd", Action: Drop, Orig: "fourth\n"},
	}}
	b, err := Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := Unmarshal(b)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, p) {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, p)
	}
}
