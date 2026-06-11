package gitcmd

import (
	"reflect"
	"testing"
)

func TestBuilderBasicArgv(t *testing.T) {
	got := New("status").Arg("--porcelain=v2", "-z").ToArgv()
	want := []string{"status", "--porcelain=v2", "-z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuilderArgIf(t *testing.T) {
	got := New("fetch").ArgIf(true, "--all").ArgIf(false, "--prune").ToArgv()
	want := []string{"fetch", "--all"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuilderDirAndConfigPrepend(t *testing.T) {
	// -C and -c must come BEFORE the subcommand. Each prepends to the front, so
	// the last-applied (Config) ends up first.
	got := New("pull").Dir("/repo").Config("pull.ff=only").ToArgv()
	want := []string{"-c", "pull.ff=only", "-C", "/repo", "pull"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
