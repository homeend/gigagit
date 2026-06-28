package git

import (
	"context"
	"reflect"
	"testing"
)

func TestParseRemoteTags(t *testing.T) {
	out := []byte(
		"aaaaaaaaaaaa\trefs/tags/v1.0.0\n" +
			"bbbbbbbbbbbb\trefs/tags/v1.1.0\n" +
			"cccccccccccc\trefs/tags/v1.1.0^{}\n" + // peeled — must be dropped (still name v1.1.0)
			"dddddddddddd\trefs/tags/release/2024\n")
	got := ParseRemoteTags(out)
	want := map[string]bool{"v1.0.0": true, "v1.1.0": true, "release/2024": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseRemoteTags = %v, want %v", got, want)
	}
}

func TestParseRemoteTagsEmpty(t *testing.T) {
	if got := ParseRemoteTags(nil); len(got) != 0 {
		t.Fatalf("empty input should yield empty set, got %v", got)
	}
}

func TestRemoteTagsListsPushedOnly(t *testing.T) {
	clone, runner := newClonePair(t)
	repo := &Repo{Runner: runner}
	gitIn(t, clone, "tag", "v1")
	gitIn(t, clone, "push", "origin", "v1") // push v1 to origin
	gitIn(t, clone, "tag", "v2")            // local only, NOT pushed
	got, err := repo.RemoteTags(context.Background(), "origin")
	if err != nil {
		t.Fatalf("RemoteTags: %v", err)
	}
	if !got["v1"] {
		t.Errorf("v1 should be on origin: %v", got)
	}
	if got["v2"] {
		t.Errorf("v2 is local-only and must not appear: %v", got)
	}
}
