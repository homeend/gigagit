package exttool

import "testing"

// Every agent with a headless review template can take a model.
func TestReviewAgentsHaveAModelFlag(t *testing.T) {
	t.Parallel()
	want := map[string]string{"claude": "--model ", "codex": "-m ", "junie": "--model=", "kimi": "-m ", "antigravity": "--model "}
	seen := 0
	for _, tl := range Builtins() {
		capture := false
		for _, c := range tl.Commands {
			if c.Category == CatReview && c.Mode == ModeCapture {
				capture = true
			}
		}
		if !capture {
			continue
		}
		seen++
		if tl.ModelFlag == "" || tl.ModelFlag != want[tl.ID] {
			t.Errorf("%s: ModelFlag %q, want %q", tl.ID, tl.ModelFlag, want[tl.ID])
		}
		if ModelFlagFor(tl.ID) != tl.ModelFlag {
			t.Errorf("ModelFlagFor(%s)", tl.ID)
		}
	}
	if seen != len(want) {
		t.Errorf("%d agents have a headless review, the table names %d", seen, len(want))
	}
	if ModelFlagFor("") != "" || ModelFlagFor("meld") != "" {
		t.Error("unknown agents have no flag")
	}
}
