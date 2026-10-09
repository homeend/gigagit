package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNoteShowByIDAndByLink(t *testing.T) {
	dir, head, _ := sendPRRepo(t)
	id := addCLINote(t, dir, head, 5)
	if code, _, errs := runCLI(t, dir, "note", "reply", id, "--summary", "and so"); code != 0 {
		t.Fatalf("reply: %s", errs)
	}
	code, out, errs := runCLI(t, dir, "note", "show", id)
	if code != 0 || !strings.Contains(out, "note "+id) || !strings.Contains(out, "big.go:5") || !strings.Contains(out, "and so") {
		t.Fatalf("exit %d out=%q err=%q", code, out, errs)
	}
	code, link, errs := runLinkCLI(t, dir, "--note", id)
	link = strings.TrimSpace(link)
	if code != 0 || !strings.Contains(link, "?note="+id) {
		t.Fatalf("gg link --note = %q (%s)", link, errs)
	}
	code, outJ, _ := runCLI(t, dir, "note", "show", "--json", link)
	if code != 0 {
		t.Fatalf("show by link: %d", code)
	}
	var got struct {
		Note struct {
			ID string `json:"id"`
		} `json:"note"`
		Replies []struct {
			Summary string `json:"summary"`
		} `json:"replies"`
	}
	if err := json.Unmarshal([]byte(outJ), &got); err != nil || got.Note.ID != id || len(got.Replies) != 1 || got.Replies[0].Summary != "and so" {
		t.Fatalf("json %q: %v", outJ, err)
	}
	// reply and resolve by link
	if code, _, errs := runCLI(t, dir, "note", "reply", link, "--summary", "again"); code != 0 {
		t.Fatalf("reply by link: %s", errs)
	}
	if code, _, errs := runCLI(t, dir, "note", "resolve", link); code != 0 {
		t.Fatalf("resolve by link: %s", errs)
	}
	_, out, _ = runCLI(t, dir, "note", "show", id)
	if !strings.Contains(out, "resolved") || !strings.Contains(out, "again") {
		t.Fatalf("after reply+resolve:\n%s", out)
	}
}

func TestNoteShowGoneNote(t *testing.T) {
	dir, _, _ := sendPRRepo(t)
	code, _, errs := runCLI(t, dir, "note", "show", "deadbeef")
	if code == 0 || !strings.Contains(errs, "deadbeef") {
		t.Fatalf("exit %d: %s", code, errs)
	}
}
