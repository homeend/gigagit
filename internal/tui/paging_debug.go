package tui

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Temporary diagnostic instrumentation for the held-End commit-paging runaway.
// Gated by GG_PAGING_DEBUG=<file>; a no-op when unset. REMOVE before merge.

var (
	pdbgOnce sync.Once
	pdbgFile *os.File
	pdbgMu   sync.Mutex
	pdbgT0   time.Time
)

func pdbg(format string, args ...any) {
	pdbgOnce.Do(func() {
		if path := os.Getenv("GG_PAGING_DEBUG"); path != "" {
			f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
			if err == nil {
				pdbgFile = f
				pdbgT0 = time.Now()
			}
		}
	})
	if pdbgFile == nil {
		return
	}
	pdbgMu.Lock()
	defer pdbgMu.Unlock()
	ms := time.Since(pdbgT0).Milliseconds()
	fmt.Fprintf(pdbgFile, "%8dms | "+format+"\n", append([]any{ms}, args...)...)
}
