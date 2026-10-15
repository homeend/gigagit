package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureStoreDir copies docs/specs/gg-notes-store/fixture/ (commits.toml
// and previews.toml — a PR overview lives in the previews part) into a temp
// store dir (tests never write into docs/).
func fixtureStoreDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"commits.toml", "previews.toml"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "docs", "specs", "gg-notes-store", "fixture", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The spec's fixture store (2026-10-15-gg-notes-store-format §9.2) read
// through gg's own store: exactly the two overview records are found, with
// their documents parsed; the review note, the overview-tagged FILE note and
// the reply are skipped (each fails one clause of the predicate).
func TestFixtureStoreReadsThroughGG(t *testing.T) {
	t.Parallel()
	_, svc := permOvRepo(t)
	svc.UseNotesDir(fixtureStoreDir(t))
	ctx := context.Background()
	all, err := svc.Overviews(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("Overviews = %d rows, want 2: %+v", len(all), all)
	}
	// newest first: the PR overview (2026-10-14) before the commit one
	pr, commit := all[0], all[1]
	if pr.ID != "3a9e1c20" || pr.Scope != "main...refs/gg/pr/7" || pr.Title != "PR 7: the gate, reviewed" || pr.Doc == nil {
		t.Fatalf("PR overview = %+v", pr)
	}
	if n, ok := PRScopeNumber(pr.Scope); !ok || n != 7 {
		t.Fatalf("PRScopeNumber(%q) = %d %v", pr.Scope, n, ok)
	}
	if commit.ID != "7cf0812f" || commit.Commit != "d5efa035df1f3c4c949040bebb924a35d0ea61a0" || commit.Branch != "main" || commit.Agent != "Claude Opus 5.5" || commit.Doc == nil {
		t.Fatalf("commit overview = %+v", commit)
	}
	d := commit.Doc
	if d.Title != "How the gate works" || len(d.Files) != 2 || d.Files[0].Path != "internal/x/gate.go" ||
		d.Files[0].Notes[0].Range != [2]int{3, 4} || d.Files[0].Notes[0].Rationale != "A closed door must not pass." ||
		d.Files[1].Notes[0].Range != [2]int{1, 1} || d.Files[1].Notes[0].Rationale != "" {
		t.Fatalf("document = %+v", d)
	}
	if !strings.Contains(d.Overview, "[`gate`](internal/x/gate.go:3-4)") || !strings.Contains(d.Overview, "[the config](config.toml)") {
		t.Fatalf("overview text = %q", d.Overview)
	}
	// The commit's own list has only the commit overview (a PR's belongs to its PR).
	byCommit, err := svc.OverviewsForCommit(ctx, "d5efa035df1f3c4c949040bebb924a35d0ea61a0")
	if err != nil || len(byCommit) != 1 || byCommit[0].ID != "7cf0812f" {
		t.Fatalf("OverviewsForCommit = %+v %v", byCommit, err)
	}
}

// Review focus 2: a record whose document no longer parses is still a row
// (Doc nil — the reader's "cannot render" row, spec §8.2), never an error.
func TestFixtureStoreCannotRenderRowSurvives(t *testing.T) {
	t.Parallel()
	dir := fixtureStoreDir(t)
	p := filepath.Join(dir, "previews.toml") // the PR overview's part
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// Break the PR overview's document only.
	broken := strings.Replace(string(data), `rationale = "{\n  \"version\": 1,\n  \"title\": \"PR 7`, `rationale = "not json {\n  \"version\": 1,\n  \"title\": \"PR 7`, 1)
	if broken == string(data) {
		t.Fatal("fixture text changed: the PR record's rationale was not found")
	}
	if err := os.WriteFile(p, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	_, svc := permOvRepo(t)
	svc.UseNotesDir(dir)
	all, err := svc.Overviews(context.Background())
	if err != nil || len(all) != 2 {
		t.Fatalf("Overviews = %+v %v, want 2 rows", all, err)
	}
	if all[0].ID != "3a9e1c20" || all[0].Doc != nil || all[0].Title != "PR 7: the gate, reviewed" {
		t.Fatalf("broken row = %+v, want Doc nil with its title kept", all[0])
	}
	if all[1].Doc == nil {
		t.Fatalf("the intact row lost its document: %+v", all[1])
	}
}
