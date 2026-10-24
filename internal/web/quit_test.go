package web

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// quitRig is a quit guard over fakes: live work n, a kill that records, the
// stderr lines and the fan-outs it made.
type quitRig struct {
	mu      sync.Mutex
	live    int
	kills   int
	said    []string
	changed int
	g       *quitGuard
	sig     chan os.Signal
	done    chan struct{}
}

func newQuitRig(t *testing.T, live int) *quitRig {
	t.Helper()
	r := &quitRig{live: live, sig: make(chan os.Signal, 4), done: make(chan struct{})}
	r.g = newQuitGuard(
		func() int { r.mu.Lock(); defer r.mu.Unlock(); return r.live },
		func(context.Context) { r.mu.Lock(); r.kills++; r.mu.Unlock() },
		func(s string) { r.mu.Lock(); r.said = append(r.said, s); r.mu.Unlock() },
		func() { r.mu.Lock(); r.changed++; r.mu.Unlock() },
	)
	return r
}

func (r *quitRig) start(ctx context.Context) {
	go func() { r.g.run(ctx, r.sig); close(r.done) }()
}

func (r *quitRig) ended(t *testing.T) bool {
	t.Helper()
	select {
	case <-r.done:
		return true
	case <-time.After(300 * time.Millisecond):
		return false
	}
}

func (r *quitRig) snap() (kills, changed int, said string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.kills, r.changed, strings.Join(r.said, "\n")
}

func TestQuitGuardNoLiveWorkQuitsAtOnce(t *testing.T) {
	t.Parallel()
	r := newQuitRig(t, 0)
	r.start(context.Background())
	r.sig <- syscall.SIGINT
	if !r.ended(t) {
		t.Fatal("Ctrl+C with nothing live did not quit")
	}
	if k, c, _ := r.snap(); k != 0 || c != 0 {
		t.Fatalf("kills=%d changed=%d, want 0 0", k, c)
	}
}

func TestQuitGuardAsksThenSecondCtrlCKills(t *testing.T) {
	t.Parallel()
	r := newQuitRig(t, 2)
	r.start(context.Background())
	r.sig <- syscall.SIGINT
	if r.ended(t) {
		t.Fatal("first Ctrl+C with live work quit")
	}
	if a, n := r.g.state(); !a || n != 2 {
		t.Fatalf("state = %v %d, want asking 2", a, n)
	}
	k, c, said := r.snap()
	if k != 0 || c != 1 || !strings.Contains(said, "gg web: agents and AI tasks still running: 2 — press Ctrl+C again to end them and quit") {
		t.Fatalf("after the ask: kills=%d changed=%d said=%q", k, c, said)
	}
	r.sig <- syscall.SIGINT
	if !r.ended(t) {
		t.Fatal("second Ctrl+C did not quit")
	}
	if k, _, said := r.snap(); k != 1 || !strings.Contains(said, "gg web: ending agent sessions…") {
		t.Fatalf("kills=%d said=%q, want one kill and the ending line", k, said)
	}
}

func TestQuitGuardCancelAsksAgain(t *testing.T) {
	t.Parallel()
	r := newQuitRig(t, 1)
	r.start(context.Background())
	r.sig <- syscall.SIGINT
	r.ended(t)
	if err := r.g.answer(false); err != nil {
		t.Fatal(err)
	}
	if a, _ := r.g.state(); a {
		t.Fatal("still asking after cancel")
	}
	r.sig <- syscall.SIGINT
	if r.ended(t) {
		t.Fatal("Ctrl+C after cancel quit instead of asking again")
	}
	if _, c, _ := r.snap(); c != 3 { // ask, cancel, ask
		t.Fatalf("changed = %d, want 3", c)
	}
}

func TestQuitGuardPageConfirm(t *testing.T) {
	t.Parallel()
	r := newQuitRig(t, 1)
	if err := r.g.answer(true); err == nil {
		t.Fatal("confirm with no ask up was accepted")
	}
	r.start(context.Background())
	r.sig <- syscall.SIGINT
	r.ended(t)
	if err := r.g.answer(true); err != nil {
		t.Fatal(err)
	}
	if err := r.g.answer(true); err == nil {
		t.Fatal("a second confirm was accepted")
	}
	r.sig <- syscall.SIGINT // a Ctrl+C racing the page's confirm
	if !r.ended(t) {
		t.Fatal("page confirm did not quit")
	}
	if k, _, _ := r.snap(); k != 1 {
		t.Fatalf("kills = %d, want exactly one", k)
	}
}

func TestQuitGuardContextEndsWithoutAsking(t *testing.T) {
	t.Parallel()
	r := newQuitRig(t, 3)
	ctx, cancel := context.WithCancel(context.Background())
	r.start(ctx)
	cancel()
	if !r.ended(t) {
		t.Fatal("cancelled context did not end the guard")
	}
	if _, c, said := r.snap(); c != 0 || said != "" {
		t.Fatalf("asked on a cancelled context: changed=%d said=%q", c, said)
	}
}

// The page's half: GET says whether the ask is up; POST answers it, refused
// unless asked, and on a hosted page (the terminal owns quitting).
func TestQuitEndpoints(t *testing.T) {
	t.Parallel()
	srv := New(domain.Open(newRepoDir(t, 1)))
	r := newQuitRig(t, 1)
	srv.quit = r.g
	ts := serve(t, srv)
	var st struct {
		Asking bool `json:"asking"`
		Live   int  `json:"live"`
	}
	if code := getJSON(t, ts, "/api/quit", &st); code != http.StatusOK || st.Asking {
		t.Fatalf("GET before the ask: %d %+v", code, st)
	}
	if code, _ := postJSONRaw(t, ts, "/api/quit", `{"confirm":true}`); code != http.StatusConflict {
		t.Fatalf("confirm with no ask: %d, want 409", code)
	}
	r.start(context.Background())
	r.sig <- syscall.SIGINT
	r.ended(t)
	if code := getJSON(t, ts, "/api/quit", &st); code != http.StatusOK || !st.Asking || st.Live != 1 {
		t.Fatalf("GET during the ask: %d %+v", code, st)
	}
	if code, _ := postJSONRaw(t, ts, "/api/quit", `{"confirm":false}`); code != http.StatusOK {
		t.Fatalf("cancel: %d", code)
	}
	if code := getJSON(t, ts, "/api/quit", &st); code != http.StatusOK || st.Asking {
		t.Fatalf("GET after cancel: %d %+v", code, st)
	}

	hosted := New(domain.Open(newRepoDir(t, 1)))
	hosted.hosted = true
	hts := serve(t, hosted)
	if code, _ := postJSONRaw(t, hts, "/api/quit", `{"confirm":true}`); code != http.StatusConflict {
		t.Fatalf("hosted confirm: %d, want 409", code)
	}
}

// Serve holds Ctrl+C in the guard, closes the host after it, and — whatever
// ended the run — ends what is left within the grace (the TUI's run.go net).
func TestServeWiresQuitGuard(t *testing.T) {
	t.Parallel()
	src := readStaticSrcGo(t, "serve.go")
	for _, want := range []string{
		"srv.quit = defaultQuitGuard(srv)",
		"signal.Notify(sig, os.Interrupt)",
		"srv.quit.run(ctx, sig)",
		"h.Close()\n\t// Safety net",
		"killAllWork(killCtx)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("serve.go lacks %q", want)
		}
	}
	if strings.Contains(src, "signal.NotifyContext") {
		t.Error("serve.go still quits on the first Ctrl+C (signal.NotifyContext)")
	}
}
