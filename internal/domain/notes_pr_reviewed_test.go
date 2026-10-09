package domain

import (
	"reflect"
	"testing"
)

// PRReviewed names the pull requests holding a local review: an AI review
// saved on the PR, or notes written in its view — matched by number whatever
// the base's spelling; a plain merge preview or commit pair is no PR.
func TestNoteCountsPRReviewed(t *testing.T) {
	t.Parallel()
	c := NoteCounts{
		PreviewReviews: map[string][]ReviewHead{
			"main...refs/gg/pr/7": {{ID: "r1"}},
			"main...feat/x":       {{ID: "r2"}},
			"abc1234..def5678":    {{ID: "r3"}},
			"main...refs/gg/pr/9": nil, // no head: no review
		},
		ScopesByCommit: map[string][]NoteScopeCount{
			"c1": {{Scope: "origin/main...refs/gg/pr/12", N: 2}, {Scope: "main...feat/y", N: 1}},
			"c2": {{Scope: "main...refs/gg/pr/7", N: 1}},
			"c3": {{Scope: "main...refs/gg/pr/15", N: 0}},
		},
	}
	want := map[int]bool{7: true, 12: true}
	if got := c.PRReviewed(); !reflect.DeepEqual(got, want) {
		t.Fatalf("PRReviewed = %v, want %v", got, want)
	}
	if got := (NoteCounts{}).PRReviewed(); len(got) != 0 {
		t.Fatalf("empty counts: %v", got)
	}
}
