package engine

import "testing"

func TestOpNameStripsPackagePrefix(t *testing.T) {
	cases := []struct {
		op   Operation
		want string
	}{
		{SmartPull{}, "SmartPull"},
		{Commit{}, "Commit"},
		{CreateWorktreeForBranch{}, "CreateWorktreeForBranch"},
		{DeleteBranch{}, "DeleteBranch"},
	}
	for _, c := range cases {
		if got := OpName(c.op); got != c.want {
			t.Errorf("OpName(%T) = %q, want %q", c.op, got, c.want)
		}
	}
}
