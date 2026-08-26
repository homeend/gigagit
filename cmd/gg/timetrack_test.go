package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/observ"
)

func TestExtractTimeTrack(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantPath string
		wantRest []string
	}{
		{"absent", []string{"status"}, "", []string{"status"}},
		{"space form", []string{"--time-track", "/tmp/x.log", "status"}, "/tmp/x.log", []string{"status"}},
		{"equals form", []string{"--time-track=/tmp/y.log"}, "/tmp/y.log", []string{}},
		{"after subcommand", []string{"pull", "--time-track", "/tmp/a.log"}, "/tmp/a.log", []string{"pull"}},
		{"no value is dropped safely", []string{"--time-track"}, "", []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path, rest := extractTimeTrack(tc.args)
			if path != tc.wantPath {
				t.Errorf("path = %q, want %q", path, tc.wantPath)
			}
			if !reflect.DeepEqual(rest, tc.wantRest) {
				t.Errorf("rest = %v, want %v", rest, tc.wantRest)
			}
		})
	}
}

func TestSetupTimeTrackAppendsAcrossRuns(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "perf.log")
	// Registered AFTER t.TempDir() so LIFO cleanup closes the sink BEFORE the
	// temp dir is removed — Windows cannot delete a file that is still open.
	t.Cleanup(func() { observ.SetSpanSink(nil) })

	if err := setupTimeTrack(logPath, []string{"status"}); err != nil {
		t.Fatalf("first setup: %v", err)
	}
	if err := setupTimeTrack(logPath, []string{"pull"}); err != nil {
		t.Fatalf("second setup: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), `"name":"gg start"`); got != 2 {
		t.Fatalf("gg start lines = %d, want 2 (append, not truncate):\n%s", got, data)
	}
	if !strings.Contains(string(data), "version=") {
		t.Fatalf("start span missing the version element:\n%s", data)
	}
}

func TestSetupTimeTrackBadPathErrors(t *testing.T) {
	t.Cleanup(func() { observ.SetSpanSink(nil) })
	bad := filepath.Join(t.TempDir(), "no-such-dir", "perf.log")
	if err := setupTimeTrack(bad, nil); err == nil {
		t.Fatal("opening a path in a missing directory must error")
	}
}
