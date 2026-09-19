package model

import "testing"

func TestPullRequestIsOpen(t *testing.T) {
	t.Parallel()
	for state, want := range map[string]bool{
		PRStateOpen: true, PRStateClosed: false, PRStateMerged: false, PRStateUnavailable: false,
	} {
		if got := (PullRequest{State: state}).IsOpen(); got != want {
			t.Errorf("IsOpen(%q) = %v, want %v", state, got, want)
		}
	}
}
