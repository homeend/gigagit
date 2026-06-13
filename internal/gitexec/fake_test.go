package gitexec

import (
	"context"
	"sync"
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

// TestFakeRunnerConcurrent: the Snapshot fan-out calls one Runner from many
// goroutines, so Run must be safe for concurrent use (run under -race).
func TestFakeRunnerConcurrent(t *testing.T) {
	f := NewFakeRunner()
	f.SetResponse("git x", Result{Stdout: "ok"})
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, err := f.Run(context.Background(), "git x", nil); err != nil {
				t.Errorf("run: %v", err)
			}
		}()
	}
	wg.Wait()
	if len(f.Calls) != n {
		t.Fatalf("recorded %d calls, want %d", len(f.Calls), n)
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
