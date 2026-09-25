package taskhist

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/filelock"
)

// FileStore keeps tasks.toml plus <id>.result / <id>.tail under root,
// rewritten atomically (temp+rename) under a process mutex and the shared
// cross-process lock: every gg process on the machine records its tasks
// here.
type FileStore struct {
	root string
	mu   sync.Mutex
}

// NewFileStore roots a store at root (XDG resolution is domain's job).
func NewFileStore(root string) *FileStore { return &FileStore{root: root} }

type index struct {
	Tasks []Record `toml:"tasks"`
}

func (fs *FileStore) path() string { return filepath.Join(fs.root, "tasks.toml") }

// read parses the index. A missing file is empty; a corrupt one is an
// error — treating it as empty would let the next Add wipe the history.
func (fs *FileStore) read() ([]Record, error) {
	data, err := os.ReadFile(fs.path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var idx index
	if err := toml.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("taskhist: %s is corrupt: %w", fs.path(), err)
	}
	return idx.Tasks, nil
}

func (fs *FileStore) List() ([]Record, error) { return fs.read() }

func (fs *FileStore) write(rs []Record) error {
	data, err := toml.Marshal(index{Tasks: rs})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(fs.root, "tasks-*.toml")
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

// locked runs f under the process mutex and the file lock.
func (fs *FileStore) locked(f func() error) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if err := os.MkdirAll(fs.root, 0o700); err != nil {
		return err
	}
	unlock, err := filelock.Acquire(fs.path() + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	return f()
}

func (fs *FileStore) Add(r Record, result, tail string) error {
	if r.ID == "" {
		return errors.New("taskhist: a record needs an id")
	}
	return fs.locked(func() error {
		before, err := fs.read()
		if err != nil {
			return err
		}
		if result != "" {
			r.ResultFile = r.ID + ".result"
			if err := os.WriteFile(filepath.Join(fs.root, r.ResultFile), []byte(result), 0o600); err != nil {
				return err
			}
		}
		if tail = TrimTail(tail); tail != "" {
			r.TailFile = r.ID + ".tail"
			if err := os.WriteFile(filepath.Join(fs.root, r.TailFile), []byte(tail), 0o600); err != nil {
				return err
			}
		}
		out := make([]Record, 0, len(before)+1)
		out = append(out, r)
		for _, x := range before {
			if x.ID != r.ID {
				out = append(out, x)
			}
		}
		for _, x := range out[min(len(out), Max):] {
			fs.removeFiles(x)
		}
		return fs.write(out[:min(len(out), Max)])
	})
}

func (fs *FileStore) removeFiles(r Record) {
	for _, f := range []string{r.ResultFile, r.TailFile} {
		if f != "" {
			os.Remove(filepath.Join(fs.root, f))
		}
	}
}

func (fs *FileStore) text(id string, pick func(Record) string) (string, error) {
	rs, err := fs.read()
	if err != nil {
		return "", err
	}
	for _, r := range rs {
		if r.ID != id {
			continue
		}
		name := pick(r)
		if name == "" {
			return "", nil
		}
		b, err := os.ReadFile(filepath.Join(fs.root, name))
		return string(b), err
	}
	return "", fmt.Errorf("taskhist: no task %s", id)
}

func (fs *FileStore) Result(id string) (string, error) {
	return fs.text(id, func(r Record) string { return r.ResultFile })
}

func (fs *FileStore) Tail(id string) (string, error) {
	return fs.text(id, func(r Record) string { return r.TailFile })
}

func (fs *FileStore) Remove(id string) error {
	return fs.locked(func() error {
		rs, err := fs.read()
		if err != nil {
			return err
		}
		out := rs[:0:0]
		for _, r := range rs {
			if r.ID == id {
				fs.removeFiles(r)
				continue
			}
			out = append(out, r)
		}
		return fs.write(out)
	})
}
