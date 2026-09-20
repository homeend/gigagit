package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// pairNotesRepo: one line of three commits c0 → c1 → c2, each rewriting
// a.txt's only line, and a note on EACH. The pair is c0..c2, so c1's and c2's
// notes belong to it and c0's does not — three look-alike commits that must
// disagree on one fixture.
func pairNotesRepo(t *testing.T) (ts *httptest.Server, svc *domain.Service, c [3]string) {
	t.Helper()
	dir := newRepoDir(t, 1)
	svc = domain.Open(dir)
	svc.UsePreviewsDir(t.TempDir())
	svc.UseNotesDir(t.TempDir())
	svc.UseLinkHistDir(t.TempDir())
	ctx := context.Background()
	for i, body := range []string{"zero\n", "one\n", "two\n"} {
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", "-A")
		gitRun(t, dir, "commit", "-m", "c"+string(rune('0'+i)))
		c[i] = gitRun(t, dir, "rev-parse", "HEAD")
		if _, err := svc.NoteAdd(ctx, model.Note{
			Source: model.NoteSourceAgent, Author: "ada",
			Address: model.FileAddress{State: model.StateCommitted, Commit: c[i], Path: "a.txt"},
			Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "on c" + string(rune('0'+i)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	srv := New(svc)
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	return serve(t, srv), svc, c
}

type pairNotesBody struct {
	Notes  json.RawMessage `json:"notes"`
	Tip    string          `json:"tip"`
	Counts map[string]int  `json:"counts"`
	Total  int             `json:"total"`
}

func TestPairNotesCountsOnly(t *testing.T) {
	t.Parallel()
	ts, _, c := pairNotesRepo(t)
	var got pairNotesBody
	if code := getJSON(t, ts, "/api/pair/notes?a="+c[0]+"&b="+c[2], &got); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if got.Total != 2 || got.Counts["a.txt"] != 2 {
		t.Fatalf("c1 and c2 belong to c0..c2, c0 does not: total=%d counts=%v", got.Total, got.Counts)
	}
	if got.Tip != c[2] {
		t.Fatalf("tip = %q, want b %q", got.Tip, c[2])
	}
	if string(got.Notes) != "[]" {
		t.Fatalf("the counts-only form answers an empty ARRAY, got %s", got.Notes)
	}
}

func TestPairNotesAtAPath(t *testing.T) {
	t.Parallel()
	ts, _, c := pairNotesRepo(t)
	var got struct {
		Notes []struct{ Summary string } `json:"notes"`
	}
	if code := getJSON(t, ts, "/api/pair/notes?a="+c[0]+"&b="+c[2]+"&path=a.txt", &got); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	var sums []string
	for _, n := range got.Notes {
		sums = append(sums, n.Summary)
	}
	all := strings.Join(sums, ",")
	if len(sums) != 2 || !strings.Contains(all, "on c1") || !strings.Contains(all, "on c2") {
		t.Fatalf("want the notes on c1 and c2, got %v", sums)
	}
}

// Each refusal is asked with every OTHER parameter valid, so only the guard
// under test can answer 400.
func TestPairNotesGuardsItsWireValues(t *testing.T) {
	t.Parallel()
	ts, _, c := pairNotesRepo(t)
	for name, q := range map[string]string{
		"an abbreviated a": "a=" + c[0][:12] + "&b=" + c[2] + "&path=a.txt",
		"an abbreviated b": "a=" + c[0] + "&b=" + c[2][:12] + "&path=a.txt",
		"a name for b":     "a=" + c[0] + "&b=main&path=a.txt",
		"an unsafe path":   "a=" + c[0] + "&b=" + c[2] + "&path=--output=x",
	} {
		var e map[string]any
		if code := getAny(t, ts, "/api/pair/notes?"+q, &e); code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, code)
		}
	}
}

func TestPairNotesOfAnAbsentPairIsEmptyNotAnError(t *testing.T) {
	t.Parallel()
	ts, _, c := pairNotesRepo(t)
	gone := strings.Repeat("ab", 20)
	var got pairNotesBody
	if code := getJSON(t, ts, "/api/pair/notes?a="+gone+"&b="+c[2]+"&path=a.txt", &got); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if got.Total != 0 || string(got.Notes) != "[]" || got.Tip != "" {
		t.Fatalf("want the empty shape, got %+v", got)
	}
}

// Only a PAIR landing arms the note scope: the two-link form never does, even
// when its links spell the very same pair, and neither does a saved comparison.
func TestCompareLinksNamesThePairOnlyForAPairLanding(t *testing.T) {
	t.Parallel()
	ts, svc, c := pairNotesRepo(t)
	ctx := context.Background()
	saved, err := svc.PairAdd(ctx, c[0], c[2], "the pair")
	if err != nil {
		t.Fatal(err)
	}
	repo, err := svc.LinkRepo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	left, right := pairSides(repo, c[0], c[2])
	cmp, err := svc.SavedCompareAdd(ctx, left, right, "as two links")
	if err != nil {
		t.Fatal(err)
	}
	type body struct {
		Pair *struct{ A, B string } `json:"pair"`
	}
	for name, tc := range map[string]struct {
		q    string
		want bool
	}{
		"a + b":                 {"a=" + c[0] + "&b=" + c[2], true},
		"a saved pair's id":     {"id=" + saved.ID, true},
		"the same pair, typed":  {strings.TrimPrefix(cmpURL(left, right), "/api/compare-links?"), false},
		"a saved comparison id": {"id=" + cmp.ID, false},
	} {
		var got body
		if code := getAny(t, ts, "/api/compare-links?"+tc.q, &got); code != http.StatusOK {
			t.Fatalf("%s: status %d", name, code)
		}
		if (got.Pair != nil) != tc.want {
			t.Errorf("%s: pair present = %v, want %v", name, got.Pair != nil, tc.want)
		}
		if tc.want && got.Pair != nil && (got.Pair.A != c[0] || got.Pair.B != c[2]) {
			t.Errorf("%s: pair = %+v, want %s..%s", name, got.Pair, c[0], c[2])
		}
	}
}
