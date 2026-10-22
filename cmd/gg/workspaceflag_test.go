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
	}{
		{"absent", []string{"status"}, "", []string{"status"}},
		{"space form", []string{"--workspace", "shop"}, "shop", []string{}},
		{"equals form", []string{"--workspace=shop", "--web"}, "shop", []string{"--web"}},
		{"no value is dropped safely", []string{"--workspace"}, "", []string{}},
		{"a subcommand keeps its own flags", []string{"repo", "--workspace", "x"}, "", []string{"repo", "--workspace", "x"}},
	}
	for _, tt := range tests {
		name, rest := extractWorkspaceFlag(tt.args)
		if name != tt.wantName || !reflect.DeepEqual(rest, tt.wantRest) {
			t.Errorf("%s: got (%q,%v) want (%q,%v)", tt.name, name, rest, tt.wantName, tt.wantRest)
		}
	}
}
