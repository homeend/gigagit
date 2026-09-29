package gitexec

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// lookPathCache maps name+PATH(+PATHEXT) to exec.LookPath's answer.
var lookPathCache sync.Map

// resolveBinary returns the path exec would run for name, looking it up once
// per PATH instead of on every invocation. exec.Command("git") stats every
// PATH entry each time; gg runs git constantly, and under `go test` each of
// those stats also lands in the test log that cmd/go re-checks after the run
// (see test.sh). The key carries PATH, so a changed PATH looks up afresh. A
// name that is already a path, or that is not found, passes through unchanged
// so exec reports exactly what it always did.
func resolveBinary(name string) string {
	if filepath.Base(name) != name {
		return name
	}
	key := name + "\x00" + os.Getenv("PATH") + "\x00" + os.Getenv("PATHEXT")
	if p, ok := lookPathCache.Load(key); ok {
		return p.(string)
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return name
	}
	lookPathCache.Store(key, p)
	return p
}
