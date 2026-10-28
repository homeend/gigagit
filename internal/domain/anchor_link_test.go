package domain

import (
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/model"
)

func TestAnchorLink(t *testing.T) {
	t.Parallel()
	sha := "0123456789abcdef0123456789abcdef01234567"
	for _, c := range []struct {
		repo   model.LinkRepo
		commit string
		a      agentdocs.Anchor
		want   string
	}{
		{model.LinkRepo{Abs: "/w/wt2"}, "", agentdocs.Anchor{Path: "a.go", Start: 3, End: 4}, "gg:///w/wt2/a.go:3-4"},
		{model.LinkRepo{Name: "babel"}, sha, agentdocs.Anchor{Path: "x/b.go", Start: 7}, "gg://babel/x/b.go@" + sha + ":7"},
		{model.LinkRepo{Name: "babel"}, sha, agentdocs.Anchor{Path: "b.go", Start: 7, End: 7}, "gg://babel/b.go@" + sha + ":7"},
		{model.LinkRepo{Name: "babel"}, sha, agentdocs.Anchor{Path: "b.go"}, "gg://babel/b.go@" + sha},
	} {
		got, err := AnchorLink(c.repo, c.commit, c.a)
		if err != nil || got != c.want {
			t.Errorf("AnchorLink(%+v, %q, %+v) = %q, %v; want %q", c.repo, c.commit, c.a, got, err, c.want)
		}
	}
}
