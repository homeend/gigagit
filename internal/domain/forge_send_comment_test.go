package domain

import (
	"context"
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

func TestForgeNoteOfOwnEchoLeadsWithTheSummary(t *testing.T) {
	t.Parallel()
	n := model.Note{Source: model.NoteSourceAgent, Author: "claude", Summary: "config cached forever", Rationale: "why",
		Meta: []notemeta.KV{{Key: "kind", Value: "issue"}, {Key: "severity", Value: "high"}}, Display: kindDisplay}
	c := forgeComment(n, nil, "n1")
	rnd := forge.GitHubRenderer{}
	body := rnd.RenderComment(c)
	got := forgeNote(model.ForgeComment{ID: "c1", Body: body, Path: "a.go", Line: 3}, "tip", rnd.Unrender(body))
	if got.Note.Summary != "config cached forever" {
		t.Fatalf("the echo's summary row is %q", got.Note.Summary)
	}
}

// §8.3: gg's own comment, read back in the PR view, shows from its summary
// — not from the badge words it was posted with, in an alert or plain. The
// local note is gone by then (a send hands it to GitHub): the body alone
// says. Someone else's comment is read as written.
func TestPRViewEchoOfOwnCommentDropsTheBadgeBlock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sent := func(id, summary, kind, severity string) string {
		meta := []notemeta.KV{{Key: "kind", Value: kind}}
		if severity != "" {
			meta = append(meta, notemeta.KV{Key: "severity", Value: severity})
		}
		n := model.Note{Source: model.NoteSourceAgent, Author: "claude", Summary: summary, Meta: meta, Display: kindDisplay}
		return forge.GitHubRenderer{}.RenderComment(forgeComment(n, nil, id))
	}
	ff := &fakeForge{comments: []model.ForgeComment{
		{ID: "E1", Kind: model.ForgeCommentInline, Author: "me", Path: "a.go", Side: model.NoteSideNew, Line: 3,
			Body: sent("n1", "config cached forever", "issue", "high")},
		{ID: "E2", Kind: model.ForgeCommentInline, Author: "bob", Path: "a.go", Side: model.NoteSideNew, Line: 5, Body: "**bold**\n\nrest"},
		{ID: "E3", Kind: model.ForgeCommentInline, Author: "me", Path: "a.go", Side: model.NoteSideNew, Line: 7,
			Body: sent("n3", "the catch is a no-op", "issue", "")},
	}}
	svc := newForgeSvc(t, ff)
	if _, err := svc.PRCommentsRefresh(ctx, 7); err != nil {
		t.Fatal(err)
	}
	ns, err := svc.PreviewNotesAt(ctx, prSet("7"), "a.go")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range ns {
		got[r.Note.ID] = r.Note.Summary
	}
	if got["forge:E1"] != "config cached forever" || got["forge:E2"] != "bold" || got["forge:E3"] != "the catch is a no-op" {
		t.Fatalf("summaries = %q", got)
	}
}

// A Mark on the user's OWN note is written as its own meta (by == author),
// not as added meta — it must stay home all the same (R12, R15): the
// built-in status badge is gg's, not the author's declaration.
func TestForgeCommentLeavesAnOwnMarkOut(t *testing.T) {
	t.Parallel()
	for _, d := range []notemeta.Display{nil, kindDisplay} {
		n := model.Note{Source: model.NoteSourceUser, Author: "MR", Summary: "my note", Display: d,
			Meta: []notemeta.KV{{Key: "kind", Value: "issue"}, {Key: "status", Value: "agreed"}}}
		if body := (forge.GitHubRenderer{}).RenderComment(forgeComment(n, nil, "u1")); strings.Contains(body, "agreed") {
			t.Errorf("display %v: the Mark left gg:\n%s", d != nil, body)
		}
	}
	declared := notemeta.Display{"status": {Show: []string{"badge"}}}
	n := model.Note{Source: model.NoteSourceAgent, Summary: "s", Display: declared, Meta: []notemeta.KV{{Key: "status", Value: "agreed"}}}
	if c := forgeComment(n, nil, "k"); len(c.Badges) != 1 {
		t.Errorf("a status the author declared is theirs to send: %+v", c.Badges)
	}
}

// A file-level thread's echo opens with its quote; gg still shows it from
// its summary (§8.3), the quote under it.
func TestForgeNoteOfQuotedOwnEchoLeadsWithTheSummary(t *testing.T) {
	t.Parallel()
	n := model.Note{Source: model.NoteSourceAgent, Author: "claude", Summary: "why 1?", Rationale: "r",
		Meta: []notemeta.KV{{Key: "kind", Value: "issue"}, {Key: "severity", Value: "high"}}, Display: kindDisplay}
	for _, meta := range [][]notemeta.KV{n.Meta, nil} {
		n.Meta = meta
		c := forgeComment(n, nil, "n1")
		c.Quote, c.QuoteLine = []string{"x := 1", "y := 2"}, 15
		rnd := forge.GitHubRenderer{}
		body := rnd.RenderComment(c)
		got := forgeNote(model.ForgeComment{ID: "c1", Body: body, Path: "a.go"}, "tip", rnd.Unrender(body))
		if got.Note.Summary != "why 1?" || !strings.Contains(got.Note.Rationale, "x := 1") {
			t.Errorf("meta %v: summary %q, rationale %q", meta != nil, got.Note.Summary, got.Note.Rationale)
		}
	}
}
