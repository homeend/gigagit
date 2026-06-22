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

// recallReset clears recall state (called when a typing mode opens/closes).
func (m Model) recallReset() Model {
	m.recallScope = ""
	m.recallOpen = false
	m.recallIndex = 0
	m.recallDraft = ""
	return m
}

// recallUpdate processes a key for the search-history dropdown of scope.
//   - next:     updated model (recall state mutated)
//   - newQuery: the query string the caller should now display (== curQuery when unchanged)
//   - handled:  true if the caller must NOT run its normal handling for this key
//   - commit:   true if Enter accepted an entry (caller runs its commit path on newQuery)
func (m Model) recallUpdate(scope string, msg tea.KeyMsg, curQuery string) (Model, string, bool, bool) {
	ring := m.searchHist[scope]
	altDown := msg.Alt && msg.Type == tea.KeyDown
	altUp := msg.Alt && msg.Type == tea.KeyUp

	if !m.recallOpen {
		if altDown && len(ring) > 0 {
			m.recallScope = scope
			m.recallOpen = true
			m.recallIndex = 0
			m.recallDraft = curQuery
			return m, ring[0], true, false
		}
		return m, curQuery, false, false // not our key
	}

	// Dropdown is open.
	switch {
	case altDown:
		if m.recallIndex < len(ring)-1 {
			m.recallIndex++
		}
		return m, ring[m.recallIndex], true, false
	case altUp:
		if m.recallIndex == 0 {
			draft := m.recallDraft
			m = m.recallReset()
			return m, draft, true, false // above newest -> close + restore draft
		}
		m.recallIndex--
		return m, ring[m.recallIndex], true, false
	case msg.Type == tea.KeyEnter:
		phrase := ring[m.recallIndex]
		m = m.recallReset()
		return m, phrase, true, true // accept -> caller commits on phrase
	case msg.Type == tea.KeyEsc:
		draft := m.recallDraft
		m = m.recallReset()
		return m, draft, true, false // close, restore draft, stay typing
	default:
		// Any other key (text/backspace/space/plain arrows) closes the dropdown
		// and falls through to normal handling with the previewed query intact.
		m = m.recallReset()
		return m, curQuery, false, false
	}
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
