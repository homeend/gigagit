package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

const previewReviewWebDoc = `{"version":1,"summary":"## Summary\nok","files":[{"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"one"}]}]}`

// previewReviewWebRepo: main + feat/x one commit ahead, a saved preview
// "login" and a saved pair main..feat/x, each with one stored review.
func previewReviewWebRepo(t *testing.T) (string, *domain.Service) {
	t.Helper()
	dir := newRepoDir(t, 2)
	gitRun(t, dir, "checkout", "-b", "feat/x")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "commit", "-am", "feature")
	gitRun(t, dir, "checkout", "main")
	svc := domain.Open(dir)
	svc.UseNotesDir(t.TempDir())
	svc.UsePreviewsDir(t.TempDir())
	ctx := context.Background()
	if _, err := svc.PreviewAdd(ctx, "feat/x", "main", "login"); err != nil {
		t.Fatal(err)
	}
	set, err := svc.PreviewNotes(ctx, "feat/x", "main")
	if err != nil || !set.OK() {
		t.Fatalf("PreviewNotes %+v %v", set, err)
	}
	save := func(s domain.PreviewNoteSet, agent string) {
		if _, _, err := svc.SaveReview(ctx, domain.SaveReview{Target: domain.ScopeReviewTarget(s), Agent: agent, Text: previewReviewWebDoc}); err != nil {
			t.Fatal(err)
		}
	}
	save(set, "Claude Code")
	if _, err := svc.PairAdd(ctx, set.Base, set.Tip, "pair one"); err != nil {
		t.Fatal(err)
	}
	pair, err := svc.PairNotes(ctx, set.Base, set.Tip)
	if err != nil || !pair.OK() {
		t.Fatalf("PairNotes %+v %v", pair, err)
	}
	save(pair, "Codex")
	return dir, svc
}

type wireReview struct {
	ID    string `json:"id"`
	Agent string `json:"agent"`
	Older bool   `json:"older"`
}

func TestPreviewEntriesCarryTheirReviews(t *testing.T) {
	t.Parallel()
	_, svc := previewReviewWebRepo(t)
	ts := serve(t, New(svc))
	var pv struct {
		Entries []struct {
			ID      string       `json:"id"`
			Reviews []wireReview `json:"reviews"`
		} `json:"entries"`
	}
	if code := getJSON(t, ts, "/api/preview", &pv); code != http.StatusOK || len(pv.Entries) != 1 {
		t.Fatalf("GET /api/preview = %d %+v", code, pv)
	}
	if r := pv.Entries[0].Reviews; len(r) != 1 || r[0].Agent != "Claude Code" || r[0].Older {
		t.Fatalf("preview reviews = %+v", r)
	}
	var sc struct {
		Entries []struct {
			Kind    string       `json:"kind"`
			Reviews []wireReview `json:"reviews"`
		} `json:"entries"`
	}
	if code := getJSON(t, ts, "/api/saved-compares", &sc); code != http.StatusOK {
		t.Fatalf("GET /api/saved-compares = %d", code)
	}
	var pairRows int
	for _, e := range sc.Entries {
		if e.Kind != "pair" {
			continue
		}
		pairRows++
		if len(e.Reviews) != 1 || e.Reviews[0].Agent != "Codex" {
			t.Fatalf("pair reviews = %+v", e.Reviews)
		}
	}
	if pairRows != 1 {
		t.Fatalf("pair rows = %d", pairRows)
	}
}

func TestPreviewEntriesCarryOlderReviewsWhenMerged(t *testing.T) {
	t.Parallel()
	dir, svc := previewReviewWebRepo(t)
	gitRun(t, dir, "merge", "--ff-only", "feat/x")
	ts := serve(t, New(svc))
	var pv struct {
		Entries []struct {
			State   string       `json:"state"`
			Reviews []wireReview `json:"reviews"`
		} `json:"entries"`
	}
	getJSON(t, ts, "/api/preview", &pv)
	if len(pv.Entries) != 1 || pv.Entries[0].State != "merged" {
		t.Fatalf("entries %+v, want the merged preview", pv.Entries)
	}
	if r := pv.Entries[0].Reviews; len(r) != 1 || !r[0].Older {
		t.Fatalf("merged preview reviews = %+v, want one older", r)
	}
}

func TestOpenedScopeNotesCarryReviews(t *testing.T) {
	t.Parallel()
	_, svc := previewReviewWebRepo(t)
	ts := serve(t, New(svc))
	var pn struct {
		Reviews []wireReview `json:"reviews"`
	}
	if code := getJSON(t, ts, "/api/preview/notes?source=feat/x&target=main", &pn); code != http.StatusOK || len(pn.Reviews) != 1 {
		t.Fatalf("/api/preview/notes = %d %+v", code, pn)
	}
	set, _ := svc.PreviewNotes(context.Background(), "feat/x", "main")
	var pr struct {
		Reviews []wireReview `json:"reviews"`
	}
	if code := getJSON(t, ts, "/api/pair/notes?a="+set.Base+"&b="+set.Tip, &pr); code != http.StatusOK || len(pr.Reviews) != 1 || pr.Reviews[0].Agent != "Codex" {
		t.Fatalf("/api/pair/notes = %d %+v", code, pr)
	}
	// A range opened from a commit's Range review row shows ONE review's
	// notes: no Reviews block.
	pr.Reviews = nil
	if code := getJSON(t, ts, "/api/pair/notes?a="+set.Base+"&b="+set.Tip+"&scope=x", &pr); code != http.StatusOK || len(pr.Reviews) != 0 {
		t.Fatalf("scoped /api/pair/notes = %d %+v, want no reviews", code, pr)
	}
}

func TestReviewPreviewTargetByID(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir, svc := previewReviewWebRepo(t)
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(echoReviewTool), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(svc))
	ps, _ := svc.PreviewList(context.Background())
	pairs, _ := svc.PairList(context.Background())
	got := reviewTools(t, ts, "?target=preview&preview="+ps[0].ID)
	set, _ := svc.PreviewNotes(context.Background(), "feat/x", "main")
	if got.Range != set.Base+".."+set.Tip || got.Label != "main ... feat/x" {
		t.Fatalf("preview target = %+v", got)
	}
	if got := reviewTools(t, ts, "?target=preview&preview="+pairs[0].ID); got.Range != set.Base+".."+set.Tip {
		t.Fatalf("pair target = %+v", got)
	}
}

func TestReviewPreviewTargetRefusesNonIDs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir, svc := previewReviewWebRepo(t)
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(echoReviewTool), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(svc))
	for _, bad := range []string{"", "login", "main...feat/x", "x;rm%20-rf", "nope"} {
		var body map[string]any
		if code := getJSON(t, ts, "/api/review/tools?target=preview&preview="+bad, &body); code != http.StatusBadRequest {
			t.Errorf("preview=%q → %d, want 400", bad, code)
		}
	}
	gitRun(t, dir, "merge", "--ff-only", "feat/x") // the preview is no longer previewable
	ps, _ := svc.PreviewList(context.Background())
	if code, body := startReview(t, ts, `{"target":"preview","preview":"`+ps[0].ID+`","tool":"Echo","approve":true}`); code != http.StatusBadRequest {
		t.Fatalf("merged preview start = %d (%v), want 400", code, body)
	}
}

// A stored review (any kind) tells every open page: their sub-rows and ✎
// re-read on the live hub's "notes".
func TestReviewRunEmitsNotes(t *testing.T) {
	dir := reviewRepo(t, echoReviewTool)
	svc := reviewSvc(t, dir)
	srv := New(svc)
	ts := serve(t, srv)
	srv.liveMu.Lock()
	srv.live = newLiveHub(config.RefreshConfig{}, false, srv.opInFlight)
	srv.liveMu.Unlock()
	t.Cleanup(srv.stopLive)
	ch, cancel := srv.liveHubRef().subscribe()
	t.Cleanup(cancel)
	code, body := startReview(t, ts, `{"target":"branch","branch":"feature","tool":"Echo","approve":true}`)
	if code != http.StatusAccepted {
		t.Fatalf("start = %d (%v)", code, body)
	}
	opID, _ := body["op_id"].(string)
	readSSE(t, ts, opID, 30*time.Second)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-ch:
			for _, c := range msg.Changed {
				if c == "notes" {
					return
				}
			}
		case <-deadline:
			t.Fatal("a stored review emitted no notes event")
		}
	}
}

func TestReviewEndpointNamesThePreviewAndOlder(t *testing.T) {
	t.Parallel()
	dir, svc := previewReviewWebRepo(t)
	ts := serve(t, New(svc))
	set, _ := svc.PreviewNotes(context.Background(), "feat/x", "main")
	hs, _ := svc.PreviewReviews(context.Background(), set)
	var r struct {
		Label string `json:"label"`
		Older bool   `json:"older"`
	}
	if code := getJSON(t, ts, "/api/review/"+hs[0].ID, &r); code != http.StatusOK || r.Label != "feat/x → main" || r.Older {
		t.Fatalf("review = %d %+v", code, r)
	}
	gitRun(t, dir, "checkout", "feat/x")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "more")
	gitRun(t, dir, "checkout", "main")
	if getJSON(t, ts, "/api/review/"+hs[0].ID, &r); !r.Older {
		t.Fatalf("review after the source moved = %+v, want older", r)
	}
}
