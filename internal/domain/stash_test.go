package domain

import (
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestParseStashList(t *testing.T) {
	in := []string{
		"stash@{0}: On main: WIP on main",
		"stash@{1}: WIP on feat: 1a2b3c add api",
		"   ", // blank-ish line ignored
	}
	got := parseStashList(in)
	want := []model.StashEntry{
		{Ref: "stash@{0}", Subject: "On main: WIP on main"},
		{Ref: "stash@{1}", Subject: "WIP on feat: 1a2b3c add api"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseStashList = %+v, want %+v", got, want)
	}
}

func TestParseStashListEmpty(t *testing.T) {
	if got := parseStashList(nil); len(got) != 0 {
		t.Errorf("nil → %+v, want empty", got)
	}
}
