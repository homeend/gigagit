package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestMiddleSlotRendersTagsWhenActive(t *testing.T) {
	m := footerModel()
	m.width = 100
	m.height = 30
	m.tags = []model.Tag{{Name: "v9.9.9", Target: "abcdef0", Annotated: true, Subject: "big"}}
	m.activeFilesTab = panelTags
	m.focus = panelTags
	out := m.View()
	if !strings.Contains(out, "v9.9.9") {
		t.Fatalf("Tags tab content not rendered:\n%s", out)
	}
	if !strings.Contains(out, "Tags") || !strings.Contains(out, "Files") {
		t.Fatalf("middle tab bar missing Files/Tags labels:\n%s", out)
	}
}

func TestMiddleSlotShowsFilesByDefault(t *testing.T) {
	m := footerModel()
	m.width = 100
	m.height = 30
	out := m.View()
	// Default (activeFilesTab unset) must render the Files tab as active.
	if !strings.Contains(out, "[Files") {
		t.Fatalf("default middle slot should show Files active:\n%s", out)
	}
}
