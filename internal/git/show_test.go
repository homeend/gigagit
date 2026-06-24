package git

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestShowFileAtHead(t *testing.T) {
	dir, runner := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "c1")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "c2")

	r := &Repo{Runner: runner}
	got, err := r.ShowFile(context.Background(), "HEAD", "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello v2\n" {
		t.Fatalf("HEAD content = %q", got)
	}
	got, err = r.ShowFile(context.Background(), "HEAD^", "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello v1\n" {
		t.Fatalf("HEAD^ content = %q", got)
	}
}

func TestShowFileMissingPathErrors(t *testing.T) {
	dir, runner := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "c1")

	r := &Repo{Runner: runner}
	if _, err := r.ShowFile(context.Background(), "HEAD", "nope.txt"); err == nil {
		t.Fatal("expected an error for a path not in the commit")
	}
}

func TestShowFileArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git show", gitexec.Result{Stdout: "content\n"})
	r := &Repo{Runner: f}
	if _, err := r.ShowFile(context.Background(), "abc123", "dir/file.go"); err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 1 {
		t.Fatalf("calls = %d", len(f.Calls))
	}
	got := strings.Join(f.Calls[0].Argv, " ")
	if got != "show abc123:dir/file.go" {
		t.Fatalf("argv = %q", got)
	}
}

func TestShowFileBinaryContentLossless(t *testing.T) {
	dir, runner := newTestRepo(t)
	blob := []byte{0x00, 0xff, 0xfe, 'P', 'N', 'G', 0x00, 0x01}
	if err := os.WriteFile(filepath.Join(dir, "b.bin"), blob, 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "bin")

	r := &Repo{Runner: runner}
	got, err := r.ShowFile(context.Background(), "HEAD", "b.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, blob) {
		t.Fatalf("binary content corrupted: got %x want %x", got, blob)
	}
}
