package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestTagRowsRemoteMarker(t *testing.T) {
	m := Model{
		tags: []model.Tag{
			{Name: "v1.0.0", Target: "aaaaaaa", Annotated: true},
			{Name: "v2.0.0", Target: "bbbbbbb", Annotated: true},
		},
		remoteTagNames: map[string]bool{"v1.0.0": true},
	}
	rows := m.tagRows()
	if !strings.Contains(rows[0], "▲") {
		t.Errorf("pushed tag row must carry ▲: %q", rows[0])
	}
	if strings.Contains(rows[1], "▲") {
		t.Errorf("local-only tag row must not carry ▲: %q", rows[1])
	}
}

func TestTagRowsNoMarkerWhenUnchecked(t *testing.T) {
	m := Model{tags: []model.Tag{{Name: "v1.0.0", Target: "aaaaaaa"}}} // remoteTagNames nil
	if strings.Contains(m.tagRows()[0], "▲") {
		t.Errorf("unchecked tag must not carry ▲: %q", m.tagRows()[0])
	}
}
