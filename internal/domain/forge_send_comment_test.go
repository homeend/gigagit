package domain

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notemeta"
)

var kindDisplay = notemeta.Display{
	"kind":     {Show: []string{"badge"}, Order: 1, Values: map[string]notemeta.Value{"issue": {Color: "red", Audience: "author"}}},
	"severity": {Show: []string{"badge"}, Order: 2, Values: map[string]notemeta.Value{"high": {Color: "red", Level: "high"}}},
	"intent":   {Show: []string{"line"}, Order: 4},
	"secret":   {}, // declared with no place: stored-only
}

func TestForgeCommentMapsDeclaredMeta(t *testing.T) {
	t.Parallel()
	n := model.Note{Source: model.NoteSourceAgent, Author: "claude", Summary: " s ", Rationale: " r ",
		Meta:    []notemeta.KV{{Key: "kind", Value: "issue"}, {Key: "severity", Value: "high"}, {Key: "intent", Value: "checked x"}, {Key: "secret", Value: "s3"}, {Key: "confidence", Value: "high"}},
		Display: kindDisplay}
	c := forgeComment(n, nil, "k1")
	if c.Summary != "s" || c.Rationale != "r" || c.Author != "claude" || c.MarkerKey != "k1" {
		t.Fatalf("parts: %+v", c)
	}
	if len(c.Badges) != 2 || c.Badges[0].Value != "issue" || c.Badges[1].Value != "high" || c.Level != "high" || c.Audience != "author" {
		t.Fatalf("badges/level/audience: %+v", c)
	}
	if len(c.Lines) != 1 || c.Lines[0] != (notemeta.Line{Label: "intent", Value: "checked x"}) {
		t.Fatalf("only the declared line goes: %+v", c.Lines)
	}
}

func TestForgeCommentLeavesAddedMetaOut(t *testing.T) {
	t.Parallel()
	n := model.Note{Source: model.NoteSourceUser, Summary: "s", Meta: []notemeta.KV{{Key: "kind", Value: "issue"}}, Display: kindDisplay,
		Added: []notemeta.Added{{Key: "status", Value: "agreed", By: "me"}, {Key: "kind", Value: "nitpick", By: "me"}}}
	body := forge.GitHubRenderer{}.RenderComment(forgeComment(n, nil, "k"))
	for _, leak := range []string{"agreed", "nitpick", "status"} {
		if strings.Contains(body, leak) {
			t.Errorf("added meta %q left gg:\n%s", leak, body)
		}
	}
	if !strings.Contains(body, "**issue**") {
		t.Errorf("the author's own badge goes:\n%s", body)
	}
}

func TestForgeCommentNeverSendsStoredOnlyOrTheDeclaration(t *testing.T) {
	t.Parallel()
	n := model.Note{Source: model.NoteSourceAgent, Summary: "s", Meta: []notemeta.KV{{Key: "secret", Value: "s3"}, {Key: "confidence", Value: "high"}, {Key: "kind", Value: "issue"}}, Display: kindDisplay}
	body := forge.GitHubRenderer{}.RenderComment(forgeComment(n, nil, "k"))
	for _, leak := range []string{"s3", "secret", "confidence", "color", "show", "red"} {
		if strings.Contains(body, leak) {
			t.Errorf("%q left gg:\n%s", leak, body)
		}
	}
}

// R13: a note with NO declaration (a converted pre-format-3 remark) shows
// every key as a line — and so sends them in <details>. Pinned on purpose.
func TestForgeCommentWithoutDeclarationSendsLines(t *testing.T) {
	t.Parallel()
	n := model.Note{Source: model.NoteSourceAgent, Summary: "s", Meta: []notemeta.KV{{Key: "severity", Value: "bug"}, {Key: "confidence", Value: "high"}}}
	c := forgeComment(n, nil, "k")
	if len(c.Badges) != 0 || len(c.Lines) != 2 || c.Lines[0].Value != "bug" || c.Lines[1].Value != "high" {
		t.Fatalf("got %+v", c)
	}
}

func TestForgeCommentSignature(t *testing.T) {
	t.Parallel()
	if c := forgeComment(model.Note{Source: model.NoteSourceAgent, Summary: "s"}, nil, "k"); c.Author != "agent" {
		t.Errorf("an unnamed agent signs as agent: %q", c.Author)
	}
	if c := forgeComment(model.Note{Source: model.NoteSourceUser, Author: "MR", Summary: "s"}, nil, "k"); c.Author != "" {
		t.Errorf("the user's own note is unsigned: %q", c.Author)
	}
}

func TestForgeCommentReplyInheritsItsRootsDeclaration(t *testing.T) {
	t.Parallel()
	root := model.Note{Display: kindDisplay}
	rep := model.Note{Source: model.NoteSourceAgent, Summary: "s", Meta: []notemeta.KV{{Key: "kind", Value: "issue"}}}
	if c := forgeComment(rep, &root, "k"); len(c.Badges) != 1 || c.Badges[0].Value != "issue" {
		t.Fatalf("got %+v", c)
	}
}

func TestReviewBodyOfSignsWithTheAgent(t *testing.T) {
	t.Parallel()
	if b := reviewBodyOf(Review{ID: "r1"}, " x "); b != (forge.ReviewBody{Summary: "x", Author: "agent", MarkerKey: "r1"}) {
		t.Fatalf("got %+v", b)
	}
}
