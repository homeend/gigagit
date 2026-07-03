package git

import (
	"context"
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestShowNumstatArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git show", gitexec.Result{})
	r := &Repo{Runner: f}
	if _, err := r.ShowNumstat(context.Background(), "abc123", []string{"a.go"}); err != nil {
		t.Fatalf("ShowNumstat: %v", err)
	}
	want := []string{"show", "--numstat", "-z", "--format=", "abc123", "--", "a.go"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestShowPatchArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git show", gitexec.Result{Stdout: "PATCH"})
	r := &Repo{Runner: f}
	out, err := r.ShowPatch(context.Background(), "abc123", nil)
	if err != nil || out != "PATCH" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	want := []string{"show", "--patch", "--format=", "abc123"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}
