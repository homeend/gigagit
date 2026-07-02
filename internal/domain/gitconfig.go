package domain

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// GitConfigRows merges git's config-key catalog (git help -c) with every
// explicitly-set local/global value (git config --list --show-scope), under
// one Read reservation. Set keys arrive lowercased while the catalog is
// camelCase, so the join is case-insensitive and the catalog form wins as
// the display key. Set keys outside the catalog (alias.*, tool sections)
// are appended after the catalog in first-seen order. A key set twice at
// one scope keeps the last value (git lists overrides last).
func (s *Service) GitConfigRows(ctx context.Context) ([]model.GitConfigRow, error) {
	return query(ctx, s, "gitconfigrows", func(ctx context.Context) ([]model.GitConfigRow, error) {
		keys, err := s.repo.ConfigKeys(ctx)
		if err != nil {
			return nil, err
		}
		settings, err := s.repo.ConfigListScoped(ctx)
		if err != nil {
			return nil, err
		}
		rows := make([]model.GitConfigRow, 0, len(keys)+8)
		index := make(map[string]int, len(keys)) // lowercase key → row index
		for _, k := range keys {
			index[strings.ToLower(k)] = len(rows)
			rows = append(rows, model.GitConfigRow{Key: k})
		}
		for _, st := range settings {
			lk := strings.ToLower(st.Key)
			i, ok := index[lk]
			if !ok {
				index[lk] = len(rows)
				i = len(rows)
				rows = append(rows, model.GitConfigRow{Key: st.Key})
			}
			switch st.Scope {
			case git.ConfigLocal:
				rows[i].LocalValue, rows[i].LocalSet = st.Value, true
			case git.ConfigGlobal:
				rows[i].GlobalValue, rows[i].GlobalSet = st.Value, true
			}
		}
		return rows, nil
	})
}
