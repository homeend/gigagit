package gitexec

import (
	"context"
	"slices"
	"testing"
)

// A running git is listed (with its pid) while it runs and gone once it exits.
func TestInFlightListsARunningGit(t *testing.T) {
	r := NewExecRunner("git", t.TempDir(), nil)
	var seen []Proc
	_, err := r.Stream(context.Background(), "probe version", []string{"version"}, func(string) {
		if seen == nil {
			seen = InFlight()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(seen, func(p Proc) bool { return p.Name == "probe version" })
	if i < 0 || seen[i].PID == 0 || seen[i].Start.IsZero() {
		t.Fatalf("a running git must be listed with its pid: %+v", seen)
	}
	if slices.ContainsFunc(InFlight(), func(p Proc) bool { return p.Name == "probe version" }) {
		t.Fatal("an exited git must leave the list")
	}
	if _, err := r.Run(context.Background(), "probe run", []string{"version"}); err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(InFlight(), func(p Proc) bool { return p.Name == "probe run" }) {
		t.Fatal("Run must untrack its git on exit")
	}
}
