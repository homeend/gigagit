package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// names extracts the short ref names in order for compact assertions.
func remoteNames(rbs []model.RemoteBranch) []string {
	out := make([]string, len(rbs))
	for i, rb := range rbs {
		out[i] = rb.Name
	}
	return out
}

func TestRemoteBranchesLocalFirst(t *testing.T) {
	tests := []struct {
		name    string
		remotes []model.RemoteBranch
		locals  []model.Branch
		want    []string
	}{
		{
			name: "matched remotes hoisted above unmatched, groups keep order",
			remotes: []model.RemoteBranch{
				{Name: "origin/aaa", Branch: "aaa"},
				{Name: "origin/main", Branch: "main"},
				{Name: "origin/zzz", Branch: "zzz"},
				{Name: "origin/feat", Branch: "feat"},
			},
			locals: []model.Branch{{Name: "main"}, {Name: "feat"}},
			want:   []string{"origin/main", "origin/feat", "origin/aaa", "origin/zzz"},
		},
		{
			name: "all matched keeps original order",
			remotes: []model.RemoteBranch{
				{Name: "origin/b", Branch: "b"},
				{Name: "origin/a", Branch: "a"},
			},
			locals: []model.Branch{{Name: "a"}, {Name: "b"}},
			want:   []string{"origin/b", "origin/a"},
		},
		{
			name: "none matched keeps original order",
			remotes: []model.RemoteBranch{
				{Name: "origin/x", Branch: "x"},
				{Name: "origin/y", Branch: "y"},
			},
			locals: []model.Branch{{Name: "main"}},
			want:   []string{"origin/x", "origin/y"},
		},
		{
			name:    "empty remotes",
			remotes: nil,
			locals:  []model.Branch{{Name: "main"}},
			want:    []string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := remoteNames(sortRemoteBranchesLocalFirst(tc.remotes, tc.locals))
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}
