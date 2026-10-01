package agentsession

import (
	"os"
	"strconv"
	"sync"
	"time"
)

var procTag = sync.OnceValue(func() string {
	return strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
})

// ProcTag names this gg process uniquely on the machine: <pid>-<start
// unixnano>. A session's GG_SESSION_ID is <ProcTag>/<id>, and a TUI's session
// registry file is <ProcTag>.json, so an id resolves to its registry.
func ProcTag() string { return procTag() }
