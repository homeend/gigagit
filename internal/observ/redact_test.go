package observ

import (
	"reflect"
	"testing"
)

func TestRedactStripsCredentialConfig(t *testing.T) {
	in := []string{"-c", "credential.helper=store --file=/tmp/x", "push"}
	got := Redact(in)
	want := []string{"-c", "credential.helper=<redacted>", "push"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRedactStripsUserinfoInURLs(t *testing.T) {
	in := []string{"clone", "https://alice:secrettoken@example.com/repo.git"}
	got := Redact(in)
	want := []string{"clone", "https://alice:<redacted>@example.com/repo.git"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRedactLeavesPlainArgsUntouched(t *testing.T) {
	in := []string{"status", "--porcelain=v2", "-z"}
	got := Redact(in)
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("got %v, want unchanged %v", got, in)
	}
}
