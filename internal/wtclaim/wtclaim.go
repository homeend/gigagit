// Package wtclaim is the claim an agent running inside gg holds on a
// worktree: one small TOML file in that worktree's git dir, created with
// O_EXCL so of two racing claimers exactly one wins. It dies with `git
// worktree remove`. Whether a claim is still ALIVE (its session running) is
// the caller's question — this package only stores it. DAG leaf.
package wtclaim

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/filelock"
)

// FileName is the claim file inside a worktree's git dir.
const FileName = "gg-claim"

// ErrClaimed: Create found a claim already in place.
var ErrClaimed = errors.New("worktree already claimed")

// Claim is who holds a worktree, since when, and why.
type Claim struct {
	Session string    `toml:"session"`
	Agent   string    `toml:"agent"`
	Since   time.Time `toml:"since"`
	Note    string    `toml:"note"`
	// Host is "<hostname>/<GOOS>" of the gg that wrote it: a repo shared
	// between WSL and Windows has two pid namespaces and two state dirs, so
	// only the writing host can judge its liveness.
	Host string `toml:"host"`
}

func path(gitDir string) string { return filepath.Join(gitDir, FileName) }

// Create writes c exclusively; ErrClaimed when a claim exists.
func Create(gitDir string, c Claim) error {
	data, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path(gitDir), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return ErrClaimed
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path(gitDir))
		return err
	}
	return f.Close()
}

// Read returns the claim; ok is false when there is none. A reader racing a
// Create may see an empty file: that decodes to a zero Claim with ok=true.
func Read(gitDir string) (Claim, bool, error) {
	data, err := os.ReadFile(path(gitDir))
	if errors.Is(err, os.ErrNotExist) {
		return Claim{}, false, nil
	}
	if err != nil {
		return Claim{}, false, err
	}
	var c Claim
	if err := toml.Unmarshal(data, &c); err != nil {
		return Claim{}, false, err
	}
	return c, true, nil
}

// Remove deletes the claim; nil when there is none.
func Remove(gitDir string) error {
	err := os.Remove(path(gitDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Age is how long ago the claim file was last written.
func Age(gitDir string) (time.Duration, bool) {
	st, err := os.Stat(path(gitDir))
	if err != nil {
		return 0, false
	}
	return time.Since(st.ModTime()), true
}

// WithLock runs fn holding the cross-process lock beside the claim, so a
// check-then-write (claim over a dead claim, sweep, release) can never
// remove a claim someone else created after fn's read.
func WithLock(gitDir string, fn func() error) error {
	release, err := filelock.Acquire(filepath.Join(gitDir, FileName+".lock"))
	if err != nil {
		return err
	}
	defer release()
	return fn()
}
