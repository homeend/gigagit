package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/preflight"
)

func TestSilentFeatureRaisesNoNotice(t *testing.T) {
	t.Parallel()
	vs := []preflight.Verdict{
		{Feature: preflight.Feature{ID: "forge", Silent: true}, State: preflight.Unsatisfiable},
		{Feature: preflight.Feature{ID: "versions"}, State: preflight.Unsatisfiable},
	}
	got := noticesForVerdicts(vs, "k")
	if len(got) != 1 || got[0].id != noticeFeatureDisabledPrefix+"versions" {
		t.Fatalf("notices = %+v", got)
	}
}
