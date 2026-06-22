// Package searchhist is gigagit's per-repo store of recent search phrases
// ("search history"): named rings of strings, newest-first. The Store interface
// is the fixed API; the file-backed implementation is swappable. It holds no git
// state — frontends reach it only through internal/domain.
package searchhist

// MaxSize is the hard ceiling on entries kept per ring, regardless of config.
const MaxSize = 1000

// Store persists per-scope history rings for one repo. Safe for sequential use
// by one process; cross-process writes read-merge then atomically rewrite, so
// the common interleaved case does not lose a sibling's entries.
type Store interface {
	// All returns every ring, newest-first, keyed by scope. Empty map when none.
	All() map[string][]string
	// Record prepends phrase to scope's ring (dedup-to-top), trims to size
	// (capped at MaxSize), and persists. No-op when phrase is empty/blank.
	Record(scope, phrase string, size int) error
}
