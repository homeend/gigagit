package pusherr

import "testing"

func TestIsNonFastForward(t *testing.T) {
	hits := []string{
		"git push failed (exit 1): ! [rejected] br -> br (non-fast-forward)",
		"hint: Updates were rejected because the tip of your current branch is behind",
		"! [rejected] (fetch first)",
		"NON-FAST-FORWARD", // case-insensitive
	}
	for _, s := range hits {
		if !IsNonFastForward(s) {
			t.Errorf("IsNonFastForward(%q) = false, want true", s)
		}
	}
	misses := []string{
		"(stale info)",
		"pre-receive hook declined",
		"could not read Username",
		"",
	}
	for _, s := range misses {
		if IsNonFastForward(s) {
			t.Errorf("IsNonFastForward(%q) = true, want false", s)
		}
	}
}

func TestIsStaleInfo(t *testing.T) {
	if !IsStaleInfo("! [rejected] br -> br (stale info)") {
		t.Error("want stale-info hit")
	}
	if IsStaleInfo("(non-fast-forward)") {
		t.Error("non-fast-forward must not read as stale info")
	}
}

func TestIsHookRejection(t *testing.T) {
	hits := []string{
		"remote: error: GH006: Protected branch update failed",
		"! [remote rejected] main -> main (pre-receive hook declined)",
	}
	for _, s := range hits {
		if !IsHookRejection(s) {
			t.Errorf("IsHookRejection(%q) = false, want true", s)
		}
	}
	if IsHookRejection("(non-fast-forward)") {
		t.Error("non-fast-forward must not read as a hook rejection")
	}
}
