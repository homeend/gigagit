package savedcompare

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

const legacyTOML = `[[previews]]
id = "abcd1234"
source = "feat/login"
target = "main"
label = "login work"
created = 2026-09-01T10:00:00Z
`

func repoFixture() model.LinkRepo { return model.LinkRepo{Name: "gigagit"} }

// THE DIRECTION GATE. PreviewAdd(source, target) renders `@target...source`,
// so a preview of source=feat/login into target=main converts to
// `gg://gigagit@main...feat/login` — target FIRST. The fixture uses two
// different names for exactly this reason: a converter that swapped the
// halves would be invisible against a symmetric fixture, and the swap is the
// defect this feature produced over and over.
func TestConvertLegacyPutsTargetFirst(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "previews.toml"), []byte(legacyTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := ConvertLegacy(dir, repoFixture())
	if err != nil {
		t.Fatalf("ConvertLegacy: %v", err)
	}
	if n != 1 {
		t.Fatalf("converted %d entries, want 1", n)
	}
	list, err := NewFileStore(dir).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("stored %d entries, want 1", len(list))
	}
	got := list[0]
	if want := "gg://gigagit@main...feat/login"; got.Left.String() != want {
		t.Fatalf("Left = %q, want %q (target first)", got.Left.String(), want)
	}
	if !got.IsSet() {
		t.Fatal("a converted preview must be a SET, not a pair")
	}
	if got.ID != "abcd1234" {
		t.Fatalf("ID = %q, want the legacy id %q carried over verbatim", got.ID, "abcd1234")
	}
	if got.Label != "login work" {
		t.Fatalf("Label = %q, want %q", got.Label, "login work")
	}
	if got.Created.IsZero() {
		t.Fatal("Created was not carried over")
	}
}

// The legacy file is REMOVED, so the conversion does not run again and an
// old build's file cannot shadow the new store.
func TestConvertLegacyRemovesTheOldFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	legacy := filepath.Join(dir, "previews.toml")
	if err := os.WriteFile(legacy, []byte(legacyTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ConvertLegacy(dir, repoFixture()); err != nil {
		t.Fatalf("ConvertLegacy: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("previews.toml still present (stat err = %v)", err)
	}
}

// Nothing to convert is not an error, and writes no file: this runs at every
// startup and must be free when there is no legacy data.
func TestConvertLegacyWithNoLegacyFileIsANoOp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	n, err := ConvertLegacy(dir, repoFixture())
	if err != nil || n != 0 {
		t.Fatalf("ConvertLegacy = (%d, %v), want (0, nil)", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "savedcompare.toml")); !os.IsNotExist(err) {
		t.Fatalf("a no-op conversion wrote savedcompare.toml (stat err = %v)", err)
	}
}

// THE RESURRECTION CASE. An older gg still on PATH recreates previews.toml
// after the conversion ran. Converting again must NOT duplicate the row: the
// dedup is on the (Left, Right) pair, so the second pass absorbs it.
func TestConvertLegacyIsIdempotentAcrossAResurrectedFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func() {
		if err := os.WriteFile(filepath.Join(dir, "previews.toml"), []byte(legacyTOML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write()
	if _, err := ConvertLegacy(dir, repoFixture()); err != nil {
		t.Fatalf("first ConvertLegacy: %v", err)
	}
	write() // an older build wrote it again
	if _, err := ConvertLegacy(dir, repoFixture()); err != nil {
		t.Fatalf("second ConvertLegacy: %v", err)
	}
	list, err := NewFileStore(dir).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("stored %d entries after a resurrected legacy file, want 1", len(list))
	}
}

// A corrupt legacy file fails loudly and leaves it in place, so the user's
// data is never removed on the strength of a parse this build got wrong.
func TestConvertLegacyRefusesACorruptFileAndKeepsIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	legacy := filepath.Join(dir, "previews.toml")
	if err := os.WriteFile(legacy, []byte("not toml ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ConvertLegacy(dir, repoFixture()); err == nil {
		t.Fatal("ConvertLegacy accepted a corrupt legacy file")
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("a corrupt legacy file was removed anyway: %v", err)
	}
}
