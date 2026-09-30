package web

import (
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/fsprobe"
	"github.com/homeend/gigagit/internal/repos"
	"github.com/homeend/gigagit/internal/worktree"
)

// The switch-repo picker's second lane. GET /api/repos answers from the
// registry file alone so the list paints at once; the branch and slow-fs
// columns need a look at each checkout's disk, and a checkout on a hung
// network mount would hold the whole list hostage. So they arrive from
// GET /api/repos/details: every registry entry is probed in its own
// goroutine, the response waits at most probeDeadline and reports the rest
// as pending, and the client re-polls while anything is pending. Re-polls
// JOIN a running probe rather than starting another (one goroutine per
// hung path, ever), and a finished verdict is memoised for probeMemo so a
// picker reopened a moment later costs nothing.

const (
	defaultProbeDeadline = 1500 * time.Millisecond
	probeMemo            = 5 * time.Second
)

// repoVerdict is what one probe of a checkout's disk reads.
type repoVerdict struct {
	branch string
	slow   bool
	common string // git common dir ("" unknown) — groups a checkout with its worktrees
}

// repoProbe is one entry's in-flight or finished verdict.
type repoProbe struct {
	done chan struct{}
	repoVerdict
	at time.Time // completion time; zero while running
}

// repoProbes is the per-path singleflight + memo behind /api/repos/details.
type repoProbes struct {
	mu sync.Mutex
	m  map[string]*repoProbe
}

// get returns the probe for path, starting one when none is running or the
// memoised verdict is older than probeMemo.
func (rp *repoProbes) get(path string, run func(string) repoVerdict, now time.Time) *repoProbe {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	if rp.m == nil {
		rp.m = map[string]*repoProbe{}
	}
	if p, ok := rp.m[path]; ok {
		select {
		case <-p.done:
			if now.Sub(p.at) < probeMemo {
				return p
			}
		default:
			return p // still running: join it
		}
	}
	p := &repoProbe{done: make(chan struct{})}
	rp.m[path] = p
	go func() {
		p.repoVerdict = run(path)
		p.at = time.Now()
		close(p.done)
	}()
	return p
}

// probeRepoDisk is the production probe: HEAD and the git common dir from
// file reads, then the foreign-mount classification. All are fail-open.
func probeRepoDisk(path string) repoVerdict {
	return repoVerdict{
		branch: domain.RepoHead(path),
		slow:   fsprobe.Foreign(path),
		common: worktree.CommonDirAt(runtime.GOOS, path),
	}
}

func (s *Server) handleRepoDetails(w http.ResponseWriter, r *http.Request) {
	run := s.probeRepo
	if run == nil {
		run = probeRepoDisk
	}
	deadline := s.probeDeadline
	if deadline == 0 {
		deadline = defaultProbeDeadline
	}
	entries := repos.Load(s.reposStatePath())
	now := time.Now()
	probes := make([]*repoProbe, len(entries))
	for i, e := range entries {
		probes[i] = s.probes.get(e.Path, run, now)
	}
	// A closed channel, not a timer's one-shot C: once the deadline is spent
	// every later entry must fall through at once, not block on its probe.
	expired := make(chan struct{})
	stop := time.AfterFunc(deadline, func() { close(expired) })
	defer stop.Stop()
	rows := make([]map[string]any, 0, len(entries))
	common := make(map[string]string, len(entries))
	for i, e := range entries {
		p := probes[i]
		select {
		case <-p.done:
		case <-expired:
		case <-r.Context().Done():
			return
		}
		row := map[string]any{"path": e.Path, "branch": "", "slow": false, "pending": true}
		select {
		case <-p.done:
			row["branch"], row["slow"], row["pending"] = p.branch, p.slow, false
			if p.common != "" {
				common[e.Path] = p.common
			}
		default:
		}
		rows = append(rows, row)
	}
	// The project grouping needs every entry's common dir at once (one entry
	// can bridge two others), so it is computed after the walk, over what has
	// landed; a pending entry groups by its remote name until it lands.
	proj := repos.Projects(entries, common)
	for i, e := range entries {
		rows[i]["project"] = projectOrNil(proj, e.Path)
	}
	writeJSON(w, map[string]any{"repos": rows})
}

// projectOrNil is path's project for the wire: null for an entry that belongs
// to none (the switcher leaves it ungrouped).
func projectOrNil(proj map[string]repos.Project, path string) any {
	if pr, ok := proj[path]; ok {
		return pr
	}
	return nil
}
