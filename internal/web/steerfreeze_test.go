package web

import (
	"net/http"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// A MIXED pair — one half an id, the other a branch name — must reach the page
// as two full ids, because only that shape lands through /api/compare-links
// (the stash's untracked member, the pair's review notes). A half that names
// nothing is left alone: the page's own lane says so, as it always did.
func TestASteeredPairReachesThePageAsTwoIDs(t *testing.T) {
	dir, mainSha, featSha := linkRepo(t)
	for name, c := range map[string]struct{ a, b, wantA, wantB string }{
		"a name and an id":      {"main", featSha, mainSha, featSha},
		"two names":             {"main", "feat/x", mainSha, featSha},
		"two ids are untouched": {mainSha, featSha, mainSha, featSha},
		"a half naming nothing": {"no-such-branch", featSha, "no-such-branch", featSha},
	} {
		srv := New(domain.Open(dir))
		if err := srv.setStartAt(steer.Command{ID: "1-1", Cmd: "navigate", Target: &steer.Target{State: "pair", A: c.a, B: c.b}}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var got struct {
			Steer struct{ State, A, B string } `json:"steer"`
		}
		if code := getJSON(t, serve(t, srv), "/api/session/start-at", &got); code != http.StatusOK {
			t.Fatalf("%s: status %d", name, code)
		}
		if got.Steer.State != "pair" || got.Steer.A != c.wantA || got.Steer.B != c.wantB {
			t.Errorf("%s: wire = %+v, want %s..%s", name, got.Steer, c.wantA, c.wantB)
		}
	}
}

// The POSTED steer takes the same step as the start-at: what the hub hands the
// page is the frozen pair, not the name the agent typed.
func TestAPostedMixedPairIsFrozenBeforeThePageSeesIt(t *testing.T) {
	dir, mainSha, featSha := linkRepo(t)
	s := New(domain.Open(dir))
	s.steerDir = t.TempDir()
	h := newLiveHub(config.RefreshConfig{}, false, func() bool { return false })
	defer h.close()
	s.liveMu.Lock()
	s.live = h
	s.liveMu.Unlock()
	ch, cancel := h.subscribe()
	defer cancel()
	body := `{"id":"1-1","cmd":"navigate","target":{"state":"pair","a":"main","b":"` + featSha + `"}}`
	if code := steerPost(t, s, body, "application/json"); code != http.StatusAccepted {
		t.Fatalf("status %d", code)
	}
	select {
	case got := <-ch:
		if got.Steer == nil || got.Steer.A != mainSha || got.Steer.B != featSha {
			t.Fatalf("the page was handed %+v, want %s..%s", got.Steer, mainSha, featSha)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no steer message reached the hub")
	}
}
