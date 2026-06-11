package observ

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteDumpRedactsAndOmitsContent(t *testing.T) {
	d := Dump{
		GeneratedAt: time.Now(),
		GGVersion:   "9.9.9",
		GitVersion:  "git version 2.45.0",
		Repo: RepoInfo{
			WorktreePath: "/repo",
			Branch:       "main",
			Head:         "abc123",
		},
		WorkingTree: DumpCounts{Untracked: 2},
		Recent: []Span{
			{Name: "git push", Args: []string{"push", "https://u:secrettoken@h/r.git"}},
		},
		Errors: []string{"boom"},
	}
	path := filepath.Join(t.TempDir(), "dump.json")
	if err := WriteDump(path, d); err != nil {
		t.Fatalf("WriteDump: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secrettoken") {
		t.Fatal("dump leaked a secret token")
	}
	// Must be valid JSON.
	var back Dump
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("dump is not valid JSON: %v", err)
	}
	if back.GGVersion != "9.9.9" {
		t.Fatalf("round-trip lost version: %+v", back)
	}
}
