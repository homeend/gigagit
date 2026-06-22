package searchhist

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// FileStore keeps an atomic-rewrite TOML file under root/search.toml.
type FileStore struct{ root string }

// NewFileStore roots a store at the per-repo directory (caller-supplied).
func NewFileStore(root string) *FileStore { return &FileStore{root: root} }

// rings is the on-disk shape: scope -> phrases (newest-first).
type rings struct {
	Rings map[string][]string `toml:"rings"`
}

func (fs *FileStore) path() string { return filepath.Join(fs.root, "search.toml") }

func (fs *FileStore) read() rings {
	data, err := os.ReadFile(fs.path())
	if err != nil {
		return rings{Rings: map[string][]string{}}
	}
	var r rings
	if err := toml.Unmarshal(data, &r); err != nil || r.Rings == nil {
		return rings{Rings: map[string][]string{}}
	}
	return r
}

// All returns a copy of every ring.
func (fs *FileStore) All() map[string][]string {
	src := fs.read().Rings
	out := make(map[string][]string, len(src))
	for k, v := range src {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// Record prepends phrase (dedup-to-top), trims to the effective size, and
// rewrites the file after read-merging the on-disk state.
func (fs *FileStore) Record(scope, phrase string, size int) error {
	phrase = strings.TrimSpace(phrase)
	if phrase == "" {
		return nil
	}
	if size <= 0 {
		size = 20
	}
	if size > MaxSize {
		size = MaxSize
	}
	r := fs.read() // read-merge: pick up any sibling writes first
	ring := r.Rings[scope]
	merged := make([]string, 0, len(ring)+1)
	merged = append(merged, phrase)
	for _, p := range ring {
		if p != phrase { // dedup-to-top
			merged = append(merged, p)
		}
	}
	if len(merged) > size {
		merged = merged[:size]
	}
	r.Rings[scope] = merged
	return fs.write(r)
}

// write persists r via temp-file + rename (the seq-state / bookmark pattern).
func (fs *FileStore) write(r rings) error {
	if err := os.MkdirAll(fs.root, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(r)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(fs.root, "search-*.toml")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, fs.path()); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
