package notes

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/homeend/gigagit/internal/model"
)

// LegacyFile is the single-file layout every gg before the split wrote.
const LegacyFile = "notes.toml"

// LegacyPresent reports whether a pre-split notes.toml sits in the root —
// the preflight probe behind the split-notes migration.
func (fs *FileStore) LegacyPresent() bool {
	_, err := os.Stat(filepath.Join(fs.root, LegacyFile))
	return err == nil
}

// ConvertLegacy moves every note of notes.toml into its part and renames
// the file to notes.toml.migrated-<unix> (a backup, never deleted). It holds
// notes.toml.lock — the lock every older gg takes — so an old writer cannot
// interleave. Merging is by id, the newer Updated winning, and ignores the
// entry cap: re-running after an older gg recreated the file loses nothing.
// A corrupt file is left in place and reported (ErrCorrupt).
func (fs *FileStore) ConvertLegacy() (int, error) {
	if !fs.LegacyPresent() {
		return 0, nil
	}
	legacy := &partFile{path: filepath.Join(fs.root, LegacyFile)}
	unlock, err := legacy.lock()
	if err != nil {
		return 0, err
	}
	defer unlock()
	old, err := legacy.read()
	if err != nil {
		return 0, err
	}
	byID := make(map[string]model.Note, len(old))
	for _, n := range old {
		byID[n.ID] = n
	}
	groups := map[Part][]model.Note{}
	for _, n := range old {
		p := PartOf(n)
		if n.IsReply() {
			if root, ok := byID[n.ParentID]; ok {
				p = PartOf(root)
			}
		}
		groups[p] = append(groups[p], n)
	}
	for p, in := range groups {
		if _, err := fs.file(p).mutateCap(func(cur []model.Note) ([]model.Note, error) {
			return mergeNewer(cur, in), nil
		}, false); err != nil {
			return 0, err
		}
	}
	dst := legacy.path + ".migrated-" + strconv.FormatInt(Now().Unix(), 10)
	if err := os.Rename(legacy.path, dst); err != nil {
		return 0, err
	}
	return len(old), nil
}

// mergeNewer folds in into cur by id: a new id is appended, a known one is
// replaced only by a strictly newer Updated.
func mergeNewer(cur, in []model.Note) []model.Note {
	at := make(map[string]int, len(cur))
	for i, n := range cur {
		at[n.ID] = i
	}
	for _, n := range in {
		if i, ok := at[n.ID]; ok {
			if n.Updated.After(cur[i].Updated) {
				cur[i] = n
			}
			continue
		}
		at[n.ID] = len(cur)
		cur = append(cur, n)
	}
	return cur
}
