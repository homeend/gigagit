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

// Right after a repo switch the old repo's health lingers until the new
// probe lands (repoHealthKnown is false): it must not key the new repo's
// approvals.
func TestToolRepoKeyIgnoresTheOldReposHealthAfterASwitch(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.repoHealth.GitCommonDir = "/elsewhere/.git"
	m.repoHealthKnown = false
	want, _ := m.svc.GitCommonDir(context.Background())
	if got := m.toolRepoKey(); !domain.SameCheckout(got, want) {
		t.Fatalf("toolRepoKey = %q, want this repo's common dir %q", got, want)
	}
}
