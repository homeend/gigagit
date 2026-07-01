package shelf

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

// FileStore keeps an atomic-rewrite TOML index plus content-addressed blob
// files under root: root/shelf.toml and root/blobs/<sha>.
type FileStore struct{ root string }

// NewFileStore roots a store at the per-repo directory (caller-supplied).
func NewFileStore(root string) *FileStore { return &FileStore{root: root} }

// index is the on-disk shape of shelf.toml.
type index struct {
	Buckets []model.ShelfBucket `toml:"buckets"`
	Entries []model.ShelfEntry  `toml:"entries"`
}

func (fs *FileStore) indexPath() string { return filepath.Join(fs.root, "shelf.toml") }
func (fs *FileStore) blobPath(sha string) string {
	return filepath.Join(fs.root, "blobs", sha)
}

func (fs *FileStore) read() index {
	var idx index
	data, err := os.ReadFile(fs.indexPath())
	if err != nil {
		return idx
	}
	if err := toml.Unmarshal(data, &idx); err != nil {
		return index{}
	}
	return idx
}

// write persists idx via temp-file + rename (the seq-state pattern), so a
// concurrent reader never sees a half-written index.
func (fs *FileStore) write(idx index) error {
	if err := os.MkdirAll(fs.root, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(idx)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(fs.root, "shelf-*.toml")
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
	if err := os.Rename(name, fs.indexPath()); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func normalizeBucket(b string) string {
	if b == "" {
		return DefaultBucket
	}
	return b
}

var slugRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func slug(path string) string {
	s := slugRe.ReplaceAllString(path, "-")
	return strings.Trim(strings.ToLower(s), "-")
}

func idSource(a model.FileAddress) string {
	switch a.State {
	case model.StateStaged:
		return "staged"
	case model.StateCommitted:
		return a.Commit
	default:
		return "unstaged"
	}
}

// writeBlob content-addresses data and stores it under root/blobs/<sha> (atomic,
// deduplicated). Returns the sha.
func (fs *FileStore) writeBlob(data []byte) (string, error) {
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	if err := os.MkdirAll(filepath.Join(fs.root, "blobs"), 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(fs.blobPath(sha)); err == nil {
		return sha, nil // already present
	}
	tmp, err := os.CreateTemp(filepath.Join(fs.root, "blobs"), "blob-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return "", err
	}
	tmp.Close()
	if err := os.Rename(name, fs.blobPath(sha)); err != nil {
		os.Remove(name)
		return "", err
	}
	return sha, nil
}

// putEntry appends-or-replaces e in the index (idempotent by ID) and persists.
func (fs *FileStore) putEntry(e model.ShelfEntry) (model.ShelfEntry, error) {
	idx := fs.read()
	fs.ensureBucket(&idx, e.Bucket)
	replaced := false
	for i := range idx.Entries {
		if idx.Entries[i].ID == e.ID {
			idx.Entries[i] = e
			replaced = true
			break
		}
	}
	if !replaced {
		idx.Entries = append(idx.Entries, e)
	}
	return e, fs.write(idx)
}

func (fs *FileStore) Put(bucket string, addr model.FileAddress, data []byte) (model.ShelfEntry, error) {
	if len(data) > MaxShelfBytes {
		return model.ShelfEntry{}, ErrTooLarge
	}
	bucket = normalizeBucket(bucket)
	sha, err := fs.writeBlob(data)
	if err != nil {
		return model.ShelfEntry{}, err
	}
	return fs.putEntry(model.ShelfEntry{
		ID:      fmt.Sprintf("%s-%s-%s", idSource(addr), slug(addr.Path), sha[:8]),
		Bucket:  bucket,
		Kind:    model.ShelfKindFile,
		Origin:  addr,
		SHA:     sha,
		Size:    int64(len(data)),
		Created: time.Now(),
	})
}

// PutCommit stores a commit's changed-files tar as a durable ShelfKindCommit
// entry (id: commit-<shortsha>-<blobsha8>).
func (fs *FileStore) PutCommit(bucket string, addr model.FileAddress, tar []byte, label string) (model.ShelfEntry, error) {
	if len(tar) > MaxCommitArchiveBytes {
		return model.ShelfEntry{}, ErrTooLarge
	}
	bucket = normalizeBucket(bucket)
	sha, err := fs.writeBlob(tar)
	if err != nil {
		return model.ShelfEntry{}, err
	}
	short := addr.Commit
	if len(short) > 7 {
		short = short[:7]
	}
	return fs.putEntry(model.ShelfEntry{
		ID:      fmt.Sprintf("commit-%s-%s", short, sha[:8]),
		Bucket:  bucket,
		Kind:    model.ShelfKindCommit,
		Origin:  addr,
		Label:   label,
		SHA:     sha,
		Size:    int64(len(tar)),
		Created: time.Now(),
	})
}

func (fs *FileStore) ensureBucket(idx *index, name string) {
	for _, b := range idx.Buckets {
		if b.Name == name {
			return
		}
	}
	idx.Buckets = append(idx.Buckets, model.ShelfBucket{Name: name})
}

func (fs *FileStore) Get(entryID string) ([]byte, error) {
	idx := fs.read()
	for _, e := range idx.Entries {
		if e.ID == entryID {
			return os.ReadFile(fs.blobPath(e.SHA))
		}
	}
	return nil, ErrNotFound
}

func (fs *FileStore) List(bucket string, skip, limit int) ([]model.ShelfEntry, error) {
	bucket = normalizeBucket(bucket)
	idx := fs.read()
	var es []model.ShelfEntry
	for _, e := range idx.Entries {
		if e.Bucket == bucket {
			es = append(es, e)
		}
	}
	sort.SliceStable(es, func(a, b int) bool { return es[a].Created.After(es[b].Created) })
	if skip >= len(es) {
		return nil, nil
	}
	end := skip + limit
	if limit <= 0 || end > len(es) {
		end = len(es)
	}
	return es[skip:end], nil
}

func (fs *FileStore) Buckets() ([]model.ShelfBucket, error) {
	idx := fs.read()
	var out []model.ShelfBucket
	for _, b := range idx.Buckets {
		if !b.Hidden {
			out = append(out, b)
		}
	}
	return out, nil
}

func (fs *FileStore) Remove(entryID string) error {
	idx := fs.read()
	var sha string
	kept := idx.Entries[:0]
	for _, e := range idx.Entries {
		if e.ID == entryID {
			sha = e.SHA
			continue
		}
		kept = append(kept, e)
	}
	idx.Entries = kept
	if sha == "" {
		return ErrNotFound
	}
	// Reclaim the blob only when no surviving entry references it.
	stillUsed := false
	for _, e := range idx.Entries {
		if e.SHA == sha {
			stillUsed = true
			break
		}
	}
	if !stillUsed {
		os.Remove(fs.blobPath(sha))
	}
	return fs.write(idx)
}
