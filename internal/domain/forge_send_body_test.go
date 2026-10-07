package domain

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
)

func TestSendBodyUserNote(t *testing.T) {
	t.Parallel()
	got := sendBody(model.Note{Source: model.NoteSourceUser, Summary: "rename this", Rationale: "it shadows x"}, "n1", "")
	want := "rename this\n\nit shadows x\n\n" + forge.SendMarker("n1")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestSendBodyAgentNoteFoldsDetailsAndSigns(t *testing.T) {
	t.Parallel()
	got := sendBody(model.Note{Source: model.NoteSourceAgent, Author: "claude", Summary: "nil deref",
		Rationale: "p can be nil here", Tags: []string{"severity: bug"}, Confidence: 0.8}, "remark:r1:ab12", "")
	for _, s := range []string{"nil deref\n\n<details><summary>Details</summary>\n\np can be nil here",
		"- severity: bug", "confidence: 0.8", "</details>", "— claude via gg", forge.SendMarker("remark:r1:ab12")} {
		if !strings.Contains(got, s) {
			t.Errorf("body lacks %q:\n%s", s, got)
		}
	}
	if !strings.HasSuffix(got, forge.SendMarker("remark:r1:ab12")) {
		t.Errorf("the marker must be the last line:\n%s", got)
	}
}

func TestSendBodyQuotesTheLineOfAFileLevelThread(t *testing.T) {
	t.Parallel()
	q := quoteLines(15, []string{"\tx := 1"})
	got := sendBody(model.Note{Source: model.NoteSourceUser, Summary: "why 1?"}, "n2", q)
	if !strings.HasPrefix(got, "> Line 15:\n> ```\n> \tx := 1\n> ```\n\nwhy 1?") {
		t.Fatalf("got %q", got)
	}
}
