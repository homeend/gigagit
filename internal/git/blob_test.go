package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// hashObject is git's own answer for path's blob id in dir's repo.
func hashObject(t *testing.T, dir, path string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "hash-object", path).Output()
	if err != nil {
		t.Fatalf("hash-object %s: %v", path, err)
	}
	return strings.TrimSpace(string(out))
}

func initRepo(t *testing.T, format string) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "--object-format="+format, dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

// The in-process blob id is git's, in both object formats, for every kind
// of content a review meets.
func TestHashWorktreeFileMatchesGit(t *testing.T) {
	t.Parallel()
	files := map[string][]byte{
		"text.txt":   []byte("one\ntwo\n"),
		"noeol.txt":  []byte("one\ntwo"),
		"empty.txt":  {},
		"binary.dat": {0, 1, 2, 'x', 0},
		"crlf.txt":   []byte("a\r\nb\r\n"),
	}
	for _, format := range []string{"sha1", "sha256"} {
		dir := initRepo(t, format)
		for name, data := range files {
			p := filepath.Join(dir, name)
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := HashWorktreeFile(format, p)
			if err != nil {
				t.Fatalf("%s %s: %v", format, name, err)
			}
			if want := hashObject(t, dir, name); got.ID != want {
				t.Errorf("%s %s: id %s, want %s", format, name, got.ID, want)
			}
			if b := BlobOf(format, data); b != got {
				t.Errorf("%s %s: BlobOf %+v != HashWorktreeFile %+v", format, name, b, got)
			}
			if BlobFormatOf(got.ID) != format {
				t.Errorf("%s %s: BlobFormatOf(%s) = %s", format, name, got.ID, BlobFormatOf(got.ID))
			}
		}
	}
}

func TestBlobOfLinesAndBinary(t *testing.T) {
	t.Parallel()
	cases := []struct {
		data   string
		lines  int
		binary bool
	}{
		{"", 0, false}, {"a\n", 1, false}, {"a\nb", 2, false}, {"a\x00b\n", 1, true},
	}
	for _, c := range cases {
		b := BlobOf("sha1", []byte(c.data))
		if b.Lines != c.lines || b.Binary != c.binary {
			t.Errorf("%q: lines %d binary %v, want %d %v", c.data, b.Lines, b.Binary, c.lines, c.binary)
		}
	}
}

// A symlink is hashed as git stores it: its target string.
func TestHashWorktreeFileSymlinkIsItsTarget(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := initRepo(t, "sha1")
	if err := os.Symlink("text.txt", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	got, err := HashWorktreeFile("sha1", filepath.Join(dir, "link"))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != BlobOf("sha1", []byte("text.txt")).ID {
		t.Fatalf("symlink id %s is not the target string's blob", got.ID)
	}
}

func TestHashWorktreeFileRefusesADirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := HashWorktreeFile("sha1", dir); err != ErrNotAFile {
		t.Fatalf("err = %v, want ErrNotAFile", err)
	}
	if _, err := HashWorktreeFile("sha1", filepath.Join(dir, "absent")); !os.IsNotExist(err) {
		t.Fatalf("absent file err = %v, want not-exist", err)
	}
}

func TestObjectFormat(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"sha1", "sha256"} {
		dir := initRepo(t, format)
		r := &Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(10))}
		got, err := r.ObjectFormat(context.Background())
		if err != nil || got != format {
			t.Errorf("ObjectFormat = %q, %v; want %q", got, err, format)
		}
	}
}
