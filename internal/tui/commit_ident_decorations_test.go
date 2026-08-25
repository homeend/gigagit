package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/homeend/gigagit/internal/model"
)

func TestCommitIdentOfCapturesTagsAndCount(t *testing.T) {
	t.Parallel()
	c := model.Commit{Refs: []model.Ref{
		{Name: "main", Kind: model.RefLocal, Head: true},
		{Name: "branch1", Kind: model.RefLocal},
		{Name: "branch2", Kind: model.RefLocal},
		{Name: "v1.0.0", Kind: model.RefTag},
	}}
	id := commitIdentOf(c, nil)
	if id.name != "main" || !id.tip {
		t.Fatalf("primary should be HEAD branch main: %+v", id)
	}
	if id.count != 3 {
		t.Fatalf("count = %d, want 3 (main+branch1+branch2)", id.count)
	}
	if len(id.extra) != 2 { // branch1, branch2
		t.Fatalf("extra = %v, want 2 branches", id.extra)
	}
	if len(id.tags) != 1 || id.tags[0] != "v1.0.0" {
		t.Fatalf("tags = %v, want [v1.0.0]", id.tags)
	}
}

func TestMarkerFieldArrangement(t *testing.T) {
	t.Parallel()
	// The 3 cells are [marker1][marker2-or-badge][separator]. The badge fills the
	// FILLER cell (cell 2) so a single local tip with count>=2 reads "↓³ " (NOT
	// "↓ ³" — badge must not displace the separator or glue to the name).
	cases := []struct {
		id   commitIdent
		want string
	}{
		{commitIdent{tip: true, count: 1}, markerLocal + "  "},                                // "↓  " single tip, no badge
		{commitIdent{tip: true, count: 3}, markerLocal + "³ "},                                // "↓³ " multi tip, badge in cell 2
		{commitIdent{tip: true, remoteTip: true, count: 3}, markerLocal + markerRemote + " "}, // "↓↑ " both tips → badge DROPPED (no room)
		{commitIdent{remoteTip: true, count: 0}, markerRemote + "  "},                         // "↑  " remote only
		{commitIdent{count: 0}, "   "},                                                        // "   " lineage
	}
	for _, tc := range cases {
		mf := tc.id.markerField()
		if mf != tc.want {
			t.Errorf("markerField(%+v) = %q, want %q", tc.id, mf, tc.want)
		}
		if w := lipgloss.Width(mf); w != commitMarkerW {
			t.Errorf("markerField(%+v) width %d, want %d", tc.id, w, commitMarkerW)
		}
	}
}
