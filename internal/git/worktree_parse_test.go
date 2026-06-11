package git

import "testing"

func TestParseWorktrees(t *testing.T) {
	out := "worktree /repo\nHEAD abc123\nbranch refs/heads/main\n\n" +
		"worktree /repo/wt-feature\nHEAD def456\nbranch refs/heads/feature\n\n" +
		"worktree /repo/wt-detached\nHEAD aaa111\ndetached\n\n"
	got, err := ParseWorktrees([]byte(out))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("worktrees = %d, want 3", len(got))
	}
	if got[0].Path != "/repo" || got[0].Branch != "main" {
		t.Errorf("wt0 = %+v", got[0])
	}
	if got[2].Branch != "" || !got[2].Detached {
		t.Errorf("wt2 should be detached: %+v", got[2])
	}
}
