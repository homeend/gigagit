package agentdocs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeRaw(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

const okOverview = "+++\ntitle = \"T\"\n+++\nbody\n"
const okNote = "path = \"a.go\"\nstart = 1\nend = 1\nsummary = \"s\"\nlines = [\"x\"]\n"

func TestListDocsAcceptsDocumentsAndReportsTheRest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeRaw(t, dir, "b-ov.md", okOverview)
	writeRaw(t, dir, "a-note.toml", okNote)
	writeRaw(t, dir, ".hidden.md", okOverview)
	writeRaw(t, dir, "x.md.tmp", okOverview)
	writeRaw(t, dir, "readme.txt", "no")
	writeRaw(t, dir, "bad.md", "title = 1\n")
	writeRaw(t, dir, "bad-note.toml", "path = \"a.go\"\n")
	writeRaw(t, dir, "big.md", "+++\ntitle = \"T\"\n+++\n"+strings.Repeat("x", MaxDocBytes+1))
	if err := os.Mkdir(filepath.Join(dir, "sub.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	docs, probs, err := ListDocs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 || docs[0].ID != "a-note" || docs[0].Kind != KindNote || docs[0].Note == nil || docs[1].ID != "b-ov" || docs[1].Over == nil || docs[1].Over.Title != "T" {
		t.Fatalf("docs = %+v", docs)
	}
	got := map[string]string{}
	for _, p := range probs {
		got[p.Name] = p.Reason
	}
	want := map[string]string{
		".hidden.md": "not a document name", "x.md.tmp": "not a document name", "readme.txt": "not a document name",
		"bad.md": "an overview file starts with a +++ front matter block", "bad-note.toml": "a line number is 1-based",
		"big.md": "over 512 KiB", "sub.md": "a directory",
	}
	for name, reason := range want {
		if !strings.HasPrefix(got[name], reason) {
			t.Errorf("%s: reason %q, want prefix %q", name, got[name], reason)
		}
	}
	if len(got) != len(want) {
		t.Errorf("problems = %v", got)
	}
	// Nothing was touched.
	if entries, _ := os.ReadDir(dir); len(entries) != 9 {
		t.Fatalf("a reader changed the directory: %d entries", len(entries))
	}
}

func TestListDocsOfAMissingDirIsEmpty(t *testing.T) {
	t.Parallel()
	docs, probs, err := ListDocs(filepath.Join(t.TempDir(), "none"))
	if err != nil || len(docs) != 0 || len(probs) != 0 {
		t.Fatalf("%v %v %v", docs, probs, err)
	}
}

func TestWriteDocIsAtomicAndCreatesTheDirectory(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "inbox", "docs")
	if err := WriteDoc(dir, "one", KindOverview, []byte(okOverview), false); err != nil {
		t.Fatal(err)
	}
	d, err := ReadDoc(dir, "one", KindOverview)
	if err != nil || d.Over.Title != "T" || d.Size != int64(len(okOverview)) {
		t.Fatalf("ReadDoc = %+v %v", d, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
	// A NEW write refuses an existing name; a replace does not.
	if err := WriteDoc(dir, "one", KindOverview, []byte(okOverview), false); !errors.Is(err, os.ErrExist) {
		t.Fatalf("second new write: %v", err)
	}
	if err := WriteDoc(dir, "one", KindOverview, []byte("+++\ntitle = \"U\"\n+++\nbody\n"), true); err != nil {
		t.Fatal(err)
	}
	if d, _ := ReadDoc(dir, "one", KindOverview); d.Over.Title != "U" {
		t.Fatalf("replace did not land: %+v", d)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("temp file left behind after replace: %v", entries)
	}
}

func TestWriteDocRefusesABadIDAndBadData(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WriteDoc(dir, "a.b", KindOverview, []byte(okOverview), false); err == nil || err.Error() != "invalid document id \"a.b\"" {
		t.Fatalf("bad id: %v", err)
	}
	if err := WriteDoc(dir, "ok", KindNote, []byte("path = 1\n"), false); err == nil || !strings.HasPrefix(err.Error(), "toml: ") {
		t.Fatalf("bad data must be refused before any file lands: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a refused write left %v", entries)
	}
}

func TestRemoveDoc(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeRaw(t, dir, "one.toml", okNote)
	if ok, err := RemoveDoc(dir, "one", KindNote); !ok || err != nil {
		t.Fatalf("remove: %v %v", ok, err)
	}
	if ok, err := RemoveDoc(dir, "one", KindNote); ok || err != nil {
		t.Fatalf("second remove: %v %v", ok, err)
	}
	if _, err := ReadDoc(dir, "one", KindNote); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ReadDoc after remove: %v", err)
	}
}

func TestDocModTimeIsTheFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeRaw(t, dir, "one.md", okOverview)
	old := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(dir, "one.md"), old, old); err != nil {
		t.Fatal(err)
	}
	docs, _, _ := ListDocs(dir)
	if len(docs) != 1 || !docs[0].ModTime.Equal(old) {
		t.Fatalf("docs = %+v", docs)
	}
}
