package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestTagRowsAndPanelView(t *testing.T) {
	m := footerModel()
	m.tags = []model.Tag{
		{Name: "v2.0.0", Target: "aaaaaaa", Annotated: true, Subject: "release two"},
		{Name: "v1.0.0", Target: "ccccccc", Annotated: false, Subject: "init"},
	}
	rows, idx := m.panelView(panelTags)
	if len(rows) != 2 || len(idx) != 2 {
		t.Fatalf("rows=%d idx=%d", len(rows), len(idx))
	}
	if !strings.Contains(rows[0], "v2.0.0") || !strings.Contains(rows[0], "release two") {
		t.Fatalf("annotated row wrong: %q", rows[0])
	}
	// Annotated marker ● vs lightweight ○.
	if !strings.Contains(rows[0], "●") || !strings.Contains(rows[1], "○") {
		t.Fatalf("kind markers wrong: %q | %q", rows[0], rows[1])
	}
}
