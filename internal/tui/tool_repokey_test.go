package tui

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// An approval granted before the first repoHealthMsg must still land under
// the repo's common dir: agent_start looks approvals up by that key only.
func TestToolRepoKeyBeforeRepoHealthIsTheCommonDir(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.repoHealth.GitCommonDir = ""
	want, err := m.svc.GitCommonDir(context.Background())
	if err != nil || want == "" {
		t.Fatalf("common dir = %q %v", want, err)
	}
	if got := m.toolRepoKey(); !domain.SameCheckout(got, want) {
		t.Fatalf("toolRepoKey = %q, want the common dir %q", got, want)
	}
}
