package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every settingDocs key must appear in the populated output of an empty file.
func TestPopulateEmptyAddsAllKeys(t *testing.T) {
	out := populate("")
	for _, d := range settingDocs {
		if !strings.Contains(out, d.key) {
			t.Errorf("populate(\"\") missing key %q\n%s", d.key, out)
		}
		if d.value != nil {
			want := "# " + d.key + " = " + tomlScalar(d.value)
			if !strings.Contains(out, want) {
				t.Errorf("scalar key %q not rendered as %q\n%s", d.key, want, out)
			}
		}
	}
	if !strings.Contains(out, "[populated]") {
		t.Errorf("added lines must carry the [populated] marker:\n%s", out)
	}
	if strings.Contains(out, "wheel_step = 3\n") {
		t.Errorf("added lines must be COMMENTED, found an active line:\n%s", out)
	}
}

// An active override is preserved verbatim and never re-added.
func TestPopulateKeepsActiveOverride(t *testing.T) {
	in := "[ui]\nwheel_step = 5\n"
	out := populate(in)
	if !strings.Contains(out, "wheel_step = 5") {
		t.Errorf("active override dropped:\n%s", out)
	}
	if strings.Contains(out, "wheel_step = 3") {
		t.Errorf("present key must not be re-added with its default:\n%s", out)
	}
	if strings.Count(out, "wheel_step") != 1 {
		t.Errorf("wheel_step should appear exactly once, got:\n%s", out)
	}
}

// A key already present as a commented line is left exactly as-is (no marker,
// no duplicate).
func TestPopulateLeavesExistingCommentedKey(t *testing.T) {
	in := "[ui]\n# hscroll_step = 8   # mine\n"
	out := populate(in)
	if !strings.Contains(out, "# hscroll_step = 8   # mine") {
		t.Errorf("existing commented line altered:\n%s", out)
	}
	if strings.Count(out, "hscroll_step") != 1 {
		t.Errorf("hscroll_step must not be duplicated:\n%s", out)
	}
}

// Idempotent: populating twice yields the same content as once.
func TestPopulateIdempotent(t *testing.T) {
	once := populate("[ui]\nwheel_step = 5\n")
	twice := populate(once)
	if once != twice {
		t.Errorf("populate not idempotent:\nonce:\n%s\ntwice:\n%s", once, twice)
	}
}

// A missing section header is created and its keys added under it.
func TestPopulateCreatesMissingSection(t *testing.T) {
	in := "[ui]\nwheel_step = 5\n"
	out := populate(in)
	if !strings.Contains(out, "[refresh]") {
		t.Errorf("missing [refresh] section not created:\n%s", out)
	}
	if !strings.Contains(out, "# enabled = false") {
		t.Errorf("refresh.enabled not added under [refresh]:\n%s", out)
	}
}

// Unknown user keys are preserved.
func TestPopulatePreservesUnknownKeys(t *testing.T) {
	in := "[ui]\nmy_custom_key = 1\n"
	out := populate(in)
	if !strings.Contains(out, "my_custom_key = 1") {
		t.Errorf("unknown key dropped:\n%s", out)
	}
}

// A nil-default key (no honest scalar) is added commented and value-less.
func TestPopulateNilDefaultKeyValueless(t *testing.T) {
	out := populate("")
	if !strings.Contains(out, "# branch_templates   #") {
		t.Errorf("nil-default key must be commented + value-less:\n%s", out)
	}
}

func TestPopulateFileTopsUpAndCounts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gg.toml")
	if err := os.WriteFile(path, []byte("[ui]\nwheel_step = 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	added, err := PopulateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if added == 0 {
		t.Fatal("expected keys to be added")
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "wheel_step = 5") {
		t.Fatalf("override clobbered:\n%s", b)
	}
	// Second run is a no-op.
	added2, err := PopulateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if added2 != 0 {
		t.Fatalf("second populate should add nothing, added %d", added2)
	}
}

func TestPopulateFileMissingFileCreatesIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gg.toml")
	added, err := PopulateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if added != len(settingDocs) {
		t.Fatalf("fresh file should add all %d keys, added %d", len(settingDocs), added)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}
