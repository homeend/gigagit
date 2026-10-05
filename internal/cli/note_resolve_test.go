package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestNoteResolveAndReplyOnARemark(t *testing.T) {
	dir, rid := reviewedRepo(t) // review_show_test.go: two remarks on a.txt
	remark := "review:" + rid + ":0"
	if code, _, errb := runCLI(t, dir, "note", "reply", remark, "--summary", "fixed", "--link", "HEAD"); code != 0 {
		t.Fatalf("reply: %s", errb)
	}
	if code, out, errb := runCLI(t, dir, "note", "resolve", remark, "--json"); code != 0 || !strings.Contains(out, `"root"`) {
		t.Fatalf("resolve: %d %s %s", code, out, errb)
	}
	code, out, _ := runCLI(t, dir, "review", "show", rid)
	if code != 0 || !strings.Contains(out, "resolved by") || !strings.Contains(out, "fixed") ||
		!strings.Contains(out, "1 of 2 resolved") || !strings.Contains(out, remark) {
		t.Fatalf("show:\n%s", out)
	}
	code, out, _ = runCLI(t, dir, "review", "show", "--json", rid)
	var rs domain.ReviewShow
	if code != 0 || json.Unmarshal([]byte(out), &rs) != nil || !rs.Remarks[0].Resolved ||
		len(rs.Remarks[0].Replies) != 1 || len(rs.Remarks[0].Replies[0].Link) != 40 || rs.Resolved != 1 {
		t.Fatalf("json: %s", out)
	}
	if code, _, errb := runCLI(t, dir, "note", "unresolve", remark); code != 0 {
		t.Fatalf("unresolve: %s", errb)
	}
	if code, _, errb := runCLI(t, dir, "note", "unresolve", remark); code != 1 || !strings.Contains(errb, "not resolved") {
		t.Fatalf("second unresolve: %d %s", code, errb)
	}
	if code, _, errb := runCLI(t, dir, "note", "reply", remark, "--summary", "x", "--link", "not a rev"); code == 0 || !strings.Contains(errb, "link") {
		t.Fatalf("bad link: %d %s", code, errb)
	}
	if code, _, errb := runCLI(t, dir, "note", "resolve", "forge:3"); code != 1 || !strings.Contains(errb, "GitHub") {
		t.Fatalf("forge: %d %s", code, errb)
	}
}

func TestLinkReviewRefusesNoFingerprint(t *testing.T) {
	dir, rid := reviewedRepo(t)
	if code, _, errb := runCLI(t, dir, "link", "--review", rid, "--no-fingerprint"); code != 2 || !strings.Contains(errb, "--no-fingerprint") {
		t.Fatalf("got %d %s", code, errb)
	}
}

// "review:latest:<n>" names a remark of the newest review, as `latest` names
// the review in gg review show / gg link --review.
func TestRemarkIDAcceptsLatest(t *testing.T) {
	t.Parallel()
	dir, id := reviewedRepo(t)
	if code, _, errb := runCLI(t, dir, "note", "reply", "review:latest:1", "--summary", "ok"); code != 0 {
		t.Fatalf("reply: %s", errb)
	}
	if code, out, errb := runCLI(t, dir, "note", "resolve", "review:latest:1"); code != 0 || !strings.Contains(out, "review:"+id+":1") {
		t.Fatalf("resolve: %d %q %q", code, out, errb)
	}
	code, out, _ := runCLI(t, dir, "review", "show", "--json", id)
	var rs domain.ReviewShow
	if code != 0 || json.Unmarshal([]byte(out), &rs) != nil || !rs.Remarks[1].Resolved || len(rs.Remarks[1].Replies) != 1 {
		t.Fatalf("show: %s", out)
	}
}
