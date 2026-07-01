package main

import "testing"

func TestIsVersionRequest(t *testing.T) {
	tests := []struct {
		tok  string
		want bool
	}{
		{"version", true},
		{"--version", true},
		{"-v", true},
		{"-V", true},
		{"status", false},
		{"", false},
		{"-version", false},
	}
	for _, tc := range tests {
		if got := isVersionRequest(tc.tok); got != tc.want {
			t.Errorf("isVersionRequest(%q) = %v, want %v", tc.tok, got, tc.want)
		}
	}
}
