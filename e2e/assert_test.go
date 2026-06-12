package e2e

import (
	"strings"
	"testing"
)

// expectOK asserts checkExpect reports no failures.
func expectOK(t *testing.T, sb *Sandbox, exp *Expect) {
	t.Helper()
	if err := exp.normalize(sb.OriginDir != ""); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if fails := checkExpect(sb, exp); len(fails) != 0 {
		t.Fatalf("unexpected failures:\n%s", strings.Join(fails, "\n"))
	}
}

// expectFail asserts checkExpect reports a failure containing want.
func expectFail(t *testing.T, sb *Sandbox, exp *Expect, want string) {
	t.Helper()
	if err := exp.normalize(sb.OriginDir != ""); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	fails := checkExpect(sb, exp)
	for _, f := range fails {
		if strings.Contains(f, want) {
			return
		}
	}
	t.Fatalf("no failure containing %q in %v", want, fails)
}

func dirtySandbox(t *testing.T) *Sandbox {
	return buildSandbox(t, localScenario([]Step{
		{Write: "a.txt", Content: "v1\n"},
		{Commit: "initial"},
		{Branch: "feature/x"},
		{Write: "a.txt", Content: "v2\n"},
		{Write: "new.txt", Content: "n\n"},
	}))
}

func TestCheckBranchAndBranches(t *testing.T) {
	sb := dirtySandbox(t)
	expectOK(t, sb, &Expect{Branch: "main", Branches: []string{"main", "feature/x"}})
	expectFail(t, sb, &Expect{Branch: "feature/x"}, "current branch")
	expectFail(t, sb, &Expect{Branches: []string{"main"}}, "branches")
}

func TestCheckCleanAndStatus(t *testing.T) {
	sb := dirtySandbox(t)
	cleanTrue := true
	expectFail(t, sb, &Expect{Clean: &cleanTrue}, "clean")
	expectOK(t, sb, &Expect{Status: &StatusExpect{
		Unstaged:  []string{"a.txt"},
		Untracked: []string{"new.txt"},
	}})
	expectFail(t, sb, &Expect{Status: &StatusExpect{Unstaged: []string{}}}, "unstaged")
}

func TestCheckFiles(t *testing.T) {
	sb := dirtySandbox(t)
	expectOK(t, sb, &Expect{Files: map[string]any{
		"a.txt":    "v2\n",
		"new.txt":  map[string]any{"unchanged": true},
		"gone.txt": map[string]any{"absent": true},
	}})
	expectFail(t, sb, &Expect{Files: map[string]any{"a.txt": "v1\n"}}, "a.txt")
	expectFail(t, sb, &Expect{Files: map[string]any{"new.txt": map[string]any{"absent": true}}}, "new.txt")
	expectFail(t, sb, &Expect{Files: map[string]any{"a.txt": map[string]any{"sha256": "deadbeef"}}}, "sha256")
}

func TestCheckStash(t *testing.T) {
	sb := buildSandbox(t, localScenario([]Step{
		{Write: "a.txt", Content: "v1\n"},
		{Commit: "initial"},
		{Write: "a.txt", Content: "v2\n"},
		{Write: "u.txt", Content: "u\n"},
		{Stash: "wip"},
	}))
	zero, one := 0, 1
	expectOK(t, sb, &Expect{
		Stashes: &one,
		Stash: []StashExpect{{Contains: map[string]string{
			"a.txt": "v2\n", // tracked change
			"u.txt": "u\n",  // untracked, stashed via -u (builder stash step)
		}}},
	})
	expectFail(t, sb, &Expect{Stashes: &zero}, "stash")
	expectFail(t, sb, &Expect{Stash: []StashExpect{{Contains: map[string]string{"a.txt": "other\n"}}}}, "a.txt")
}
