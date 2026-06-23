package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// closeFilesView must zero the entire cluster — no stale field survives.
func TestCloseFilesViewZeroesEverything(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	// dirty every field the way full-tree-with-preview-after-compare would
	m.filesView = &contentPopup{}
	m.filesTitle = "x"
	m.filesHash = "abc"
	m.filesLeft = model.Endpoint{Kind: model.EndpointCommit, Hash: "a"}
	m.filesRight = model.Endpoint{Kind: model.EndpointWorkTree}
	m.compareTag = "cmp:x"
	m.filesStashTag = "stash@{0}"
	m.filesTreeFocused = true
	m.filesReadInflight = true
	m.filesPreview = &contentPopup{}
	m.filesPreviewTag = "p@h"
	m.filesMode = filesModeFullTree

	m = m.closeFilesView()

	if m.filesView != nil || m.filesPreview != nil || m.filesTitle != "" ||
		m.filesHash != "" || m.inCompareMode() || m.inFullTree() || m.compareTag != "" ||
		m.filesStashTag != "" || m.filesTreeFocused || m.filesReadInflight ||
		m.filesPreviewTag != "" || m.filesLeft != (model.Endpoint{}) ||
		m.filesRight != (model.Endpoint{}) || m.filesMode != filesModeChanged {
		t.Fatalf("closeFilesView left stale state: %+v", m)
	}
}

// Switching from full-tree-with-preview into compare drops the preview + full-tree.
func TestOpenCompareDropsPreviewAndFullTree(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.filesMode = filesModeFullTree
	m.filesPreview = &contentPopup{}
	m.filesPreviewTag = "p@h"

	m, _ = m.openCompareFiles(
		model.Endpoint{Kind: model.EndpointCommit, Hash: "a"},
		model.Endpoint{Kind: model.EndpointWorkTree})

	if m.filesPreview != nil || m.filesPreviewTag != "" || m.inFullTree() ||
		!m.inCompareMode() {
		t.Fatal("openCompareFiles must drop preview+fullTree and enter compare mode")
	}
}

// toggleFullTree drops an open preview.
func TestToggleFullTreeDropsPreview(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.filesView = &contentPopup{}
	m.filesHash = "abc"
	m.filesMode = filesModeFullTree
	m.filesPreview = &contentPopup{}
	m.filesPreviewTag = "p@h"

	m, _ = m.toggleFullTree() // fullTree -> changed

	if m.filesPreview != nil || m.inFullTree() {
		t.Fatal("toggleFullTree must drop the preview and leave full-tree")
	}
}
