package gitexec

import (
	"context"
	"testing"
)

func TestFakeRunnerReturnsConfiguredResult(t *testing.T) {
	f := NewFakeRunner()
	f.SetResponse("git status", Result{Stdout: "clean", ExitCode: 0})

	res, err := f.Run(context.Background(), "git status", []string{"status"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Stdout != "clean" {
		t.Fatalf("stdout = %q, want %q", res.Stdout, "clean")
	}
	if len(f.Calls) != 1 || f.Calls[0].Name != "git status" {
		t.Fatalf("call not recorded: %+v", f.Calls)
	}
}

func TestFakeRunnerStreamsLines(t *testing.T) {
	f := NewFakeRunner()
	f.SetResponse("git log", Result{Stdout: "line1\nline2\n"})

	var got []string
	_, err := f.Stream(context.Background(), "git log", []string{"log"}, func(l string) {
		got = append(got, l)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "line1" || got[1] != "line2" {
		t.Fatalf("streamed lines = %v, want [line1 line2]", got)
	}
}
