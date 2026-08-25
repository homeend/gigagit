package tui

import (
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestReopenSamePairIsNoop(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	left := model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[1].Hash}
	right := model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[0].Hash}
	m, cmd := m.openCompareFiles(left, right)
	if cmd == nil {
		t.Fatal("first open must start a load")
	}
	view := m.filesView
	m2, cmd2 := m.openCompareFiles(left, right)
	if cmd2 != nil {
		t.Fatal("re-opening the same pair must not reload")
	}
	if m2.filesView != view {
		t.Fatal("re-opening the same pair must keep the existing view")
	}
}

func TestReopenDifferentPairReloads(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	left := model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[1].Hash}
	right := model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[0].Hash}
	m, _ = m.openCompareFiles(left, right)
	firstTag := m.compareTag
	other := model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[2].Hash}
	m2, cmd := m.openCompareFiles(other, right)
	if cmd == nil {
		t.Fatal("a different pair must reload")
	}
	if m2.compareTag == firstTag {
		t.Fatal("the tag must change for a different pair")
	}
}

func TestFailedCompareLoadIsRetryable(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	left := model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[1].Hash}
	right := model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[0].Hash}
	m, _ = m.openCompareFiles(left, right)
	u, _ := m.Update(compareFilesMsg{tag: m.compareTag, err: errors.New("boom")})
	m = u.(Model)
	if m.compareTag != "" {
		t.Fatalf("a failed load must clear the tag, got %q", m.compareTag)
	}
	m2, cmd := m.openCompareFiles(left, right)
	if cmd == nil || m2.compareTag == "" {
		t.Fatal("the same pair must re-open after a failure")
	}
}
