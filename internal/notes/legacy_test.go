package notes

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/model"
)

func writeLegacy(t *testing.T, root string, ns []model.Note) {
	t.Helper()
	data, err := toml.Marshal(index{Notes: ns})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, LegacyFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestConvertLegacySplitsEveryKindLosslessly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	prevRoot := commitNote("r0000000", "main...feat", 1)
	prevReply := commitNote("p0000000", "", 2) // a reply carries no Preview
	prevReply.ParentID = prevRoot.ID
	shelf := noteAt("s0000000", 3)
	shelf.Address = model.FileAddress{State: model.StateShelf, ShelfID: "e1"}
	legacy := []model.Note{
		liveNote("w0000000", "/repo", 0), prevRoot, prevReply, shelf,
		commitNote("c0000000", "", 4), liveNote("u0000000", "", 5),
	}
	writeLegacy(t, root, legacy)
	fs := NewFileStore(root)
	fs.SetPolicy(Policy{MaxEntries: 1}) // the conversion must ignore the cap
	if !fs.LegacyPresent() {
		t.Fatal("LegacyPresent = false with notes.toml on disk")
	}

	n, err := fs.ConvertLegacy()
	if err != nil || n != len(legacy) {
		t.Fatalf("ConvertLegacy = %d, %v; want %d", n, err, len(legacy))
	}
	all, err := fs.LoadAll()
	if err != nil || len(all) != len(legacy) {
		t.Fatalf("LoadAll after conversion = %d, %v", len(all), err)
	}
	if prev, _ := fs.Load(PartPreviews); len(prev) != 2 {
		t.Fatalf("previews part = %+v, want the root AND its reply", prev)
	}
	if un, _ := fs.Load(WorktreePart("")); len(un) != 1 {
		t.Fatalf("unscoped part = %+v", un)
	}
	if fs.LegacyPresent() {
		t.Fatal("notes.toml must be renamed away")
	}
	backups, _ := filepath.Glob(filepath.Join(root, LegacyFile+".migrated-*"))
	if len(backups) != 1 {
		t.Fatalf("want one backup, got %v", backups)
	}
}

// An older gg (an installed gg mcp) may recreate notes.toml after the split:
// the next conversion merges it — the newer Updated wins, nothing is lost.
func TestConvertLegacyMergesARecreatedFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fs := NewFileStore(root)
	kept := commitNote("c0000000", "", 1)
	kept.Updated = time.Unix(1_800_000_000, 0).UTC()
	kept.Summary = "newer, in the part"
	if err := fs.Put(kept); err != nil {
		t.Fatal(err)
	}
	stale := kept
	stale.Summary = "older, from the old gg"
	stale.Updated = kept.Updated.Add(-time.Hour)
	writeLegacy(t, root, []model.Note{stale, commitNote("n0000000", "", 2)})

	if _, err := fs.ConvertLegacy(); err != nil {
		t.Fatal(err)
	}
	com, _ := fs.Load(PartCommits)
	if len(com) != 2 {
		t.Fatalf("commits = %+v, want the existing note + the new one", com)
	}
	for _, n := range com {
		if n.ID == kept.ID && n.Summary != "newer, in the part" {
			t.Fatalf("an older copy overwrote a newer note: %+v", n)
		}
	}
}

func TestConvertLegacyRefusesACorruptFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, LegacyFile), []byte("notes = [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := NewFileStore(root)
	if _, err := fs.ConvertLegacy(); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("ConvertLegacy = %v, want ErrCorrupt", err)
	}
	if !fs.LegacyPresent() {
		t.Fatal("a corrupt legacy file must be left where it is")
	}
}

func TestConvertLegacyWithNothingToDo(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "absent")
	fs := NewFileStore(root)
	if n, err := fs.ConvertLegacy(); err != nil || n != 0 {
		t.Fatalf("ConvertLegacy on no file = %d, %v", n, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("nothing to convert must create nothing")
	}
}
