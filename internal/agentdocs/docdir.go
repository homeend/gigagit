package agentdocs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// The docs directory (spec 2026-10-18-gg-session-docs §2, §5): a reader
// lists it and never changes it; a writer lands a file by temp + rename.

// Doc is one directory entry a reader accepted.
type Doc struct {
	ID      string
	Kind    string
	ModTime time.Time
	Size    int64
	Over    *OverviewFile
	Note    *NoteFile
}

// Problem is an entry a reader skipped, and why.
type Problem struct{ Name, Reason string }

// ListDocs reads dir: the documents in name order plus the skipped entries.
// A missing directory is an empty list.
func ListDocs(dir string) ([]Doc, []Problem, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var docs []Doc
	var probs []Problem
	for _, e := range entries {
		id, kind, ok := DocName(e.Name())
		if !ok {
			probs = append(probs, Problem{e.Name(), "not a document name"})
			continue
		}
		d, err := readEntry(dir, id, kind)
		if err != nil {
			probs = append(probs, Problem{e.Name(), err.Error()})
			continue
		}
		docs = append(docs, d)
	}
	sort.Slice(docs, func(i, j int) bool { return FileName(docs[i].ID, docs[i].Kind) < FileName(docs[j].ID, docs[j].Kind) })
	return docs, probs, nil
}

// ReadDoc reads one document; os.ErrNotExist when absent.
func ReadDoc(dir, id, kind string) (Doc, error) {
	if !ValidID(id) {
		return Doc{}, fs.ErrNotExist
	}
	return readEntry(dir, id, kind)
}

func readEntry(dir, id, kind string) (Doc, error) {
	name := FileName(id, kind)
	st, err := os.Lstat(filepath.Join(dir, name))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Doc{}, fs.ErrNotExist
		}
		return Doc{}, fmt.Errorf("unreadable: %v", err)
	}
	if st.IsDir() {
		return Doc{}, errors.New("a directory")
	}
	if st.Size() > MaxDocBytes {
		return Doc{}, errors.New("over 512 KiB")
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Doc{}, fs.ErrNotExist
		}
		return Doc{}, fmt.Errorf("unreadable: %v", err)
	}
	d := Doc{ID: id, Kind: kind, ModTime: st.ModTime(), Size: st.Size()}
	switch kind {
	case KindOverview:
		o, err := ParseOverviewFile(data)
		if err != nil {
			return Doc{}, err
		}
		d.Over = &o
	default:
		n, err := ParseNoteFile(data)
		if err != nil {
			return Doc{}, err
		}
		d.Note = &n
	}
	return d, nil
}

// WriteDoc lands data as the document (id, kind): parsed first (a refused
// document never reaches the directory), then a temp file in dir, then a
// rename over the final name — or, with replace false, a link that fails
// when the name is taken.
func WriteDoc(dir, id, kind string, data []byte, replace bool) error {
	if !ValidID(id) {
		return fmt.Errorf("invalid document id %q", id)
	}
	if kind == KindOverview {
		if _, err := ParseOverviewFile(data); err != nil {
			return err
		}
	} else if _, err := ParseNoteFile(data); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var rnd [4]byte
	_, _ = rand.Read(rnd[:])
	tmp := filepath.Join(dir, "."+id+"."+hex.EncodeToString(rnd[:])+tmpSuffix)
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	final := filepath.Join(dir, FileName(id, kind))
	if replace {
		if err := os.Rename(tmp, final); err != nil {
			os.Remove(tmp)
			return err
		}
		return nil
	}
	err := os.Link(tmp, final)
	switch {
	case err == nil:
		os.Remove(tmp)
		return nil
	case errors.Is(err, fs.ErrExist):
		os.Remove(tmp)
		return fs.ErrExist
	}
	// No hard links here: the best this filesystem offers.
	if _, serr := os.Lstat(final); serr == nil {
		os.Remove(tmp)
		return fs.ErrExist
	}
	if rerr := os.Rename(tmp, final); rerr != nil {
		os.Remove(tmp)
		return rerr
	}
	return nil
}

// RemoveDoc unlinks one document; false when it was not there.
func RemoveDoc(dir, id, kind string) (bool, error) {
	if !ValidID(id) {
		return false, nil
	}
	err := os.Remove(filepath.Join(dir, FileName(id, kind)))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
