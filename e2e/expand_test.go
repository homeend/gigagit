package e2e

import (
	"slices"
	"testing"
)

func TestExpandArgs(t *testing.T) {
	t.Parallel()
	// filepath.ToSlash only rewrites os.PathSeparator, so this asserts the
	// POSIX case; a Windows run rewrites its own backslashes the same way.
	got := ExpandArgs([]string{"diff", "gg://{{cwd}}/a.txt", "plain"}, "/tmp/sb/local")
	want := []string{"diff", "gg:///tmp/sb/local/a.txt", "plain"}
	if !slices.Equal(got, want) {
		t.Errorf("ExpandArgs = %v, want %v", got, want)
	}
	// No token: the slice is returned untouched.
	in := []string{"status"}
	if out := ExpandArgs(in, "/x"); !slices.Equal(out, in) {
		t.Errorf("ExpandArgs = %v, want %v", out, in)
	}
	// Windows drive-letter cwd: ToSlash rewrites the backslashes, and a
	// leading "/" is prepended so the result reads as an absolute link path
	// (the same rule ruling P2 gives the link's own local form).
	got = ExpandArgs([]string{"gg://{{cwd}}/a.txt"}, `C:\work\repo`)
	want = []string{"gg:///C:/work/repo/a.txt"}
	if !slices.Equal(got, want) {
		t.Errorf("ExpandArgs (drive letter) = %v, want %v", got, want)
	}
}
