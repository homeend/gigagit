package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
)

// Search-history ring scopes. The panel filter and @ highlight share scopePanel.
const (
	scopePanel    = "panel"
	scopeFiletree = "filetree"
	scopeBookmark = "bookmark"
	scopeShelf    = "shelf"
)

// searchHistLoadedMsg carries the rings read once at startup.
type searchHistLoadedMsg struct{ rings map[string][]string }

// loadSearchHistCmd reads every ring from the per-repo store (off the UI goroutine).
func loadSearchHistCmd(svc *domain.Service) tea.Cmd {
	return func() tea.Msg {
		return searchHistLoadedMsg{rings: svc.SearchHistoryAll(context.Background())}
	}
}

// searchHistorySize is the effective per-ring cap from config (default 20, ≤1000).
func (m Model) searchHistorySize() int {
	return domain.EffectiveSearchHistorySize(m.cfg.UI.SearchHistorySize)
}

// recordSearch updates the in-memory ring (dedup-to-top, trim) and returns a
// fire-and-forget persist command. Empty/blank phrases are a no-op.
func (m Model) recordSearch(scope, phrase string) (Model, tea.Cmd) {
	phrase = strings.TrimSpace(phrase)
	if phrase == "" {
		return m, nil
	}
	ring := m.searchHist[scope]
	merged := make([]string, 0, len(ring)+1)
	merged = append(merged, phrase)
	for _, p := range ring {
		if p != phrase { // dedup-to-top
			merged = append(merged, p)
		}
	}
	if n := m.searchHistorySize(); len(merged) > n {
		merged = merged[:n]
	}
	// Copy the map so the value-receiver Model mutation is visible to the caller.
	next := make(map[string][]string, len(m.searchHist)+1)
	for k, v := range m.searchHist {
		next[k] = v
	}
	next[scope] = merged
	m.searchHist = next

	svc := m.svc
	rawSize := m.cfg.UI.SearchHistorySize
	cmd := func() tea.Msg {
		svc.RecordSearch(context.Background(), scope, phrase, rawSize)
		return nil
	}
	return m, cmd
}
