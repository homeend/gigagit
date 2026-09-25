// Package taskhist is gigagit's machine-local history of AI tasks (commit
// message, review, conflict agents): the last Max records across every
// repository, each with its result text and output tail in files beside
// the index. Owned by internal/domain — frontends reach it only through
// domain.Tasks().
package taskhist

import (
	"crypto/rand"
	"encoding/hex"
	"time"
	"unicode/utf8"
)

// Max is how many records are kept (spec ruling 4).
const Max = 50

// MaxTail caps a record's kept output tail.
const MaxTail = 64 << 10

// Record is one finished task. Times are UTC. ResultFile and TailFile are
// base names under the store root, "" when there is none.
type Record struct {
	ID         string    `toml:"id"`
	Key        string    `toml:"key"`
	Kind       string    `toml:"kind"`
	Agent      string    `toml:"agent"`
	Repo       string    `toml:"repo"`
	Worktree   string    `toml:"worktree"`
	Mode       string    `toml:"mode"`
	State      string    `toml:"state"`
	Started    time.Time `toml:"started"`
	Ended      time.Time `toml:"ended"`
	ExitCode   int       `toml:"exit_code"`
	Err        string    `toml:"err"`
	ResultFile string    `toml:"result_file"`
	TailFile   string    `toml:"tail_file"`
}

// Store persists records newest first.
type Store interface {
	List() ([]Record, error)
	// Add stores r with its result and tail texts ("" = none), prepends it
	// and prunes to Max, deleting the pruned records' files.
	Add(r Record, result, tail string) error
	Result(id string) (string, error)
	Tail(id string) (string, error)
	Remove(id string) error
}

// NewID is a sortable, filename-safe unique id.
func NewID(now time.Time) string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return now.UTC().Format("20060102-150405.000000") + "-" + hex.EncodeToString(b[:])
}

// TrimTail keeps the last MaxTail bytes of s, starting on a rune boundary.
func TrimTail(s string) string {
	if len(s) <= MaxTail {
		return s
	}
	s = s[len(s)-MaxTail:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}
