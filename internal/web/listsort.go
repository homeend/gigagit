package web

import (
	"slices"
	"sort"
	"strings"
)

// The web mirror of the TUI's per-panel sort cycle (internal/tui/viewstate.go's
// sortMode, cycled by the `o` key): git's own emission order, then name and
// date in both directions. These strings are the wire values — the client sends
// them as a `?sort=` query and stores them in the layout record — so they are
// resolved against an allowlist like every other web input.
const (
	sortDefault  = "default" // git's emission order: no reordering at all
	sortNameAsc  = "name-asc"
	sortNameDesc = "name-desc"
	sortDateAsc  = "date-asc"
	sortDateDesc = "date-desc"
)

// sortModeList is the allowlist, in the cycle order the client walks.
var sortModeList = []string{sortDefault, sortNameAsc, sortNameDesc, sortDateAsc, sortDateDesc}

// allowedSortMode resolves a wire value; anything unrecognized (including the
// empty string) falls back to the default order, never an error — a stale
// client must not be able to break a list.
func allowedSortMode(s string) string {
	for _, m := range sortModeList {
		if s == m {
			return m
		}
	}
	return sortDefault
}

// sortedRows returns rows in list order under mode — a COPY, because the
// domain hands every caller the same cached slice and nobody may mutate it
// (see domain.tagsCached). The default order needs no copy: it IS the input.
func sortedRows[T any](rows []T, mode string, name func(T) string, date func(T) int64) []T {
	if mode == sortDefault {
		return rows
	}
	out := slices.Clone(rows)
	sortRows(out, mode, name, date)
	return out
}

// sortRows orders rows in place under mode, mirroring the TUI's viewLess:
// case-insensitive name compare, stable ties, and an unknown date (0) sorting
// LAST in BOTH directions so missing data never floats to the top. Lists with
// no date at all (tags) pass a date func returning 0, which makes the date
// modes no-ops — exactly what the TUI's tagList.Date does.
func sortRows[T any](rows []T, mode string, name func(T) string, date func(T) int64) {
	if mode == sortDefault {
		return
	}
	sort.SliceStable(rows, func(a, b int) bool {
		switch mode {
		case sortNameAsc, sortNameDesc:
			na, nb := strings.ToLower(name(rows[a])), strings.ToLower(name(rows[b]))
			if na == nb {
				return false
			}
			if mode == sortNameAsc {
				return na < nb
			}
			return na > nb
		case sortDateAsc, sortDateDesc:
			da, db := date(rows[a]), date(rows[b])
			if da == 0 || db == 0 {
				return da != 0 && db == 0
			}
			if da == db {
				return false
			}
			if mode == sortDateAsc {
				return da < db
			}
			return da > db
		}
		return false
	})
}
