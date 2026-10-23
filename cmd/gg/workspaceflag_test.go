package main

import (
	"reflect"
	"testing"
)

func TestExtractWorkspaceFlag(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantName string
		wantRest []string
		wantErr  bool
	}{
		{"absent", []string{"status"}, "", []string{"status"}, false},
		{"space form", []string{"--workspace", "shop"}, "shop", []string{}, false},
		{"equals form", []string{"--workspace=shop", "--web"}, "shop", []string{"--web"}, false},
		{"after another launch flag", []string{"--web", "--workspace", "shop"}, "shop", []string{"--web"}, false},
		{"a subcommand keeps its own flags", []string{"repo", "--workspace", "x"}, "", []string{"repo", "--workspace", "x"}, false},
		{"no value is an error", []string{"--workspace"}, "", nil, true},
		{"an empty value is an error", []string{"--workspace="}, "", nil, true},
		{"twice is an error", []string{"--workspace", "a", "--workspace", "b"}, "", nil, true},
		{"twice, mixed forms", []string{"--workspace", "a", "--workspace=b"}, "", nil, true},
	}
	for _, tt := range tests {
		name, rest, err := extractWorkspaceFlag(tt.args)
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: err = %v", tt.name, err)
			continue
		}
		if tt.wantErr {
			continue
		}
		if name != tt.wantName || !reflect.DeepEqual(rest, tt.wantRest) {
			t.Errorf("%s: got (%q,%v) want (%q,%v)", tt.name, name, rest, tt.wantName, tt.wantRest)
		}
	}
}

// --workspace is a TUI launch flag: ahead of a subcommand it is a usage
// error, never a silently dropped flag.
func TestWorkspaceFlagMisuse(t *testing.T) {
	if !workspaceFlagMisuse("shop", []string{"status"}) {
		t.Fatal("--workspace shop status should be refused")
	}
	for _, args := range [][]string{nil, {}, {"--web"}, {"--record", "f"}} {
		if workspaceFlagMisuse("shop", args) {
			t.Fatalf("%v: a TUI launch was refused", args)
		}
	}
	if workspaceFlagMisuse("", []string{"status"}) {
		t.Fatal("no flag, no misuse")
	}
}
