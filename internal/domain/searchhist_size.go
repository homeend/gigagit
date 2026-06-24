package domain

import "github.com/homeend/gigagit/internal/searchhist"

// DefaultSearchHistorySize is the per-ring entry count when config leaves it unset.
const DefaultSearchHistorySize = 20

// EffectiveSearchHistorySize maps a raw config value to the size actually used:
// <=0 falls back to the default, anything above the hard ceiling clamps down.
func EffectiveSearchHistorySize(raw int) int {
	if raw <= 0 {
		return DefaultSearchHistorySize
	}
	if raw > searchhist.MaxSize {
		return searchhist.MaxSize
	}
	return raw
}
