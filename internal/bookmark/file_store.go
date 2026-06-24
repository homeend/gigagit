package bookmark

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/model"
)

// FileStore keeps an atomic-rewrite TOML registry under root/bookmarks.toml.
type FileStore struct{ root string }

// NewFileStore roots a store at the per-repo directory (caller-supplied).
func NewFileStore(root string) *FileStore { return &FileStore{root: root} }

type index struct {
	Bookmarks []model.Bookmark `toml:"bookmarks"`
}

func (fs *FileStore) path() string { return filepath.Join(fs.root, "bookmarks.toml") }

func (fs *FileStore) read() index {
	var idx index
	data, err := os.ReadFile(fs.path())
	if err != nil {
		return idx
	}
	if err := toml.Unmarshal(data, &idx); err != nil {
		return index{}
	}
	return idx
}

// write persists idx via temp-file + rename (the seq-state pattern).
func (fs *FileStore) write(idx index) error {
	if err := os.MkdirAll(fs.root, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(idx)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(fs.root, "bookmarks-*.toml")
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

var slugRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func slug(s string) string {
	return strings.Trim(strings.ToLower(slugRe.ReplaceAllString(s, "-")), "-")
}

// AddressID derives a stable id from the ADDRESS (not the SHA), so identical
// content at different places is distinct and the same place is idempotent.
func AddressID(b model.Bookmark) string {
	branch := b.Branch
	if b.IsCommit() {
		// A commit bookmark's identity is the commit itself; its branch
		// decoration is volatile display sugar, so the same commit yields one id.
		branch = ""
	}
	key := fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%s\x00%s",
		b.State, b.Worktree, branch, b.Commit, b.ShelfID, b.Path)
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%s-%s-%s", b.State.String(), slug(b.Path), hex.EncodeToString(sum[:])[:8])
}

func (fs *FileStore) Add(b model.Bookmark) (model.Bookmark, error) {
	b.ID = AddressID(b)
	if b.Created.IsZero() {
		b.Created = time.Now()
	}
	idx := fs.read()
	for i := range idx.Bookmarks {
		if idx.Bookmarks[i].ID == b.ID { // same address → idempotent replace
			idx.Bookmarks[i] = b
			return b, fs.write(idx)
		}
	}
	idx.Bookmarks = append(idx.Bookmarks, b)
	return b, fs.write(idx)
}

func (fs *FileStore) Get(id string) (model.Bookmark, error) {
	for _, b := range fs.read().Bookmarks {
		if b.ID == id {
			return b, nil
		}
	}
	return model.Bookmark{}, ErrNotFound
}

func (fs *FileStore) List(skip, limit int) ([]model.Bookmark, error) {
	bs := fs.read().Bookmarks
	sort.SliceStable(bs, func(a, b int) bool { return bs[a].Created.After(bs[b].Created) })
	if skip >= len(bs) {
		return nil, nil
	}
	end := skip + limit
	if limit <= 0 || end > len(bs) {
		end = len(bs)
	}
	return bs[skip:end], nil
}

func (fs *FileStore) Remove(id string) error {
	idx := fs.read()
	kept := idx.Bookmarks[:0]
	found := false
	for _, b := range idx.Bookmarks {
		if b.ID == id {
			found = true
			continue
		}
		kept = append(kept, b)
	}
	if !found {
		return ErrNotFound
	}
	idx.Bookmarks = kept
	return fs.write(idx)
}
