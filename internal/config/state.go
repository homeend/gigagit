package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// seqState is the on-disk shape of <gitDir>/gg/state.toml. Each counter value is
// the last consumed number (absent = 0 = nothing consumed yet).
type seqState struct {
	Seq map[string]int `toml:"seq"`
}

func statePath(gitDir string) string {
	return filepath.Join(gitDir, "gg", "state.toml")
}

func readSeqState(gitDir string) (seqState, error) {
	st := seqState{Seq: map[string]int{}}
	data, err := os.ReadFile(statePath(gitDir))
	if os.IsNotExist(err) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := toml.Unmarshal(data, &st); err != nil {
		return st, fmt.Errorf("config: parsing %s: %w", statePath(gitDir), err)
	}
	if st.Seq == nil {
		st.Seq = map[string]int{}
	}
	return st, nil
}

// PeekSeq returns the next value the named counter will produce (1-based, so 1
// when unset). It does not mutate state.
func PeekSeq(gitDir, name string) int {
	st, err := readSeqState(gitDir)
	if err != nil {
		return 1
	}
	return st.Seq[name] + 1
}

// BumpSeq increments the named counter and persists it atomically, returning the
// newly consumed number (which equals the PeekSeq value taken just before).
func BumpSeq(gitDir, name string) (int, error) {
	st, err := readSeqState(gitDir)
	if err != nil {
		return 0, err
	}
	next := st.Seq[name] + 1
	st.Seq[name] = next
	if err := writeSeqState(gitDir, st); err != nil {
		return 0, err
	}
	return next, nil
}

// writeSeqState marshals st to <gitDir>/gg/state.toml via a temp file + rename so
// a concurrent reader never sees a half-written file. os.Rename replaces an
// existing target on all platforms gigagit supports.
func writeSeqState(gitDir string, st seqState) error {
	dir := filepath.Join(gitDir, "gg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(st)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "state-*.toml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, statePath(gitDir)); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
