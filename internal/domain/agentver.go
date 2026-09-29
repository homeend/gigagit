package domain

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/exttool"
)

// agentVersionRun runs `<bin> <args…>` with stdin closed and a 3 s budget
// (a test seam).
var agentVersionRun = func(ctx context.Context, bin string, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = nil
	return cmd.Output()
}

type agentVerKey struct {
	path  string
	mtime time.Time
}

type agentVerVal struct {
	v  exttool.Version
	ok bool
}

var (
	agentVerMu    sync.Mutex
	agentVerCache = map[agentVerKey]agentVerVal{}
)

func resetAgentVersionCache() {
	agentVerMu.Lock()
	agentVerCache = map[agentVerKey]agentVerVal{}
	agentVerMu.Unlock()
}

// AgentVersion is the installed agent's own version, probed once per binary
// (resolved path + mtime, so an upgrade mid-session re-probes). Unknown when
// the tool declares no VersionArgs, the probe fails or times out, or the
// output holds no version.
func AgentVersion(ctx context.Context, tl exttool.Tool, bin string) (exttool.Version, bool) {
	if len(tl.VersionArgs) == 0 {
		return exttool.Version{}, false
	}
	path := bin
	if p, err := exec.LookPath(bin); err == nil {
		path = p
	}
	key := agentVerKey{path: path}
	if fi, err := os.Stat(path); err == nil {
		key.mtime = fi.ModTime()
	}
	agentVerMu.Lock()
	if v, hit := agentVerCache[key]; hit {
		agentVerMu.Unlock()
		return v.v, v.ok
	}
	agentVerMu.Unlock()
	out, err := agentVersionRun(ctx, bin, tl.VersionArgs)
	var val agentVerVal
	if err == nil {
		val.v, val.ok = exttool.ExtractVersion(out, tl.VersionRe)
	}
	agentVerMu.Lock()
	agentVerCache[key] = val
	agentVerMu.Unlock()
	return val.v, val.ok
}
