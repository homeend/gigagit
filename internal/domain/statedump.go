package domain

import (
	"fmt"
	"io"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/buildinfo"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/repogate"
)

// WriteProcessState writes the process-wide half of the emergency state dump
// (the TUI's alt+U): the git subprocesses still running, the git slot ceiling,
// every busy repo reservation, the session's recent failures and every
// goroutine's stack. It runs NO git — it is read when git is what hangs — and
// takes only short-held locks. Argv is redacted; no file contents appear.
func WriteProcessState(w io.Writer, now time.Time) {
	fmt.Fprintf(w, "\n== process ==\ngg %s · %s/%s · %d goroutines\n", buildinfo.Version, runtime.GOOS, runtime.GOARCH, runtime.NumGoroutine())

	held, capacity, waiting := gitexec.Slots()
	fmt.Fprintf(w, "\n== git subprocesses (slots %d/%d held, %d waiting for a slot) ==\n", held, capacity, waiting)
	procs := gitexec.InFlight()
	if len(procs) == 0 {
		fmt.Fprintln(w, "(none running)")
	}
	for _, p := range procs {
		fmt.Fprintf(w, "pid %-7d %8s  %s: git %s\n", p.PID, dumpAge(now, p.Start), p.Name, strings.Join(observ.Redact(p.Argv), " "))
	}

	fmt.Fprintln(w, "\n== repo reservations ==")
	gates := repogate.All()
	if len(gates) == 0 {
		fmt.Fprintln(w, "(none held)")
	}
	for key, entries := range gates {
		fmt.Fprintln(w, key)
		for _, e := range entries {
			state := "holds"
			if e.Waiting {
				state = "waits"
			} else if e.Long {
				state = "holds (long-lived; excluded ops are refused, not queued)"
			}
			fmt.Fprintf(w, "  %s %-9s %8s  %s\n", state, e.Mode, dumpAge(now, e.Since), e.Label)
		}
	}

	fmt.Fprintln(w, "\n== recent failures (this session) ==")
	fails := observ.SessionFailures()
	if len(fails) == 0 {
		fmt.Fprintln(w, "(none)")
	}
	if len(fails) > 20 {
		fails = fails[len(fails)-20:]
	}
	for _, f := range fails {
		fmt.Fprintf(w, "%s  %s  %s\n", f.Time.Format(time.TimeOnly), f.Source, oneLineDetail(f.Detail))
	}

	fmt.Fprintln(w, "\n== goroutines ==")
	if p := pprof.Lookup("goroutine"); p != nil {
		_ = p.WriteTo(w, 2)
	}
}

func dumpAge(now, t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	return now.Sub(t).Round(100 * time.Millisecond).String()
}

func oneLineDetail(s string) string {
	s = strings.ReplaceAll(s, "\n", " ⏎ ")
	if r := []rune(s); len(r) > 300 {
		s = string(r[:300]) + "…"
	}
	return s
}

// EndGitProcesses SIGTERMs every git subprocess this process is running — the
// emergency unlock's step after its dump: an abandoned read that hangs keeps
// its coalesced flight, git slot and repo reservation, so a reload would only
// join it. Returns how many were signalled.
func EndGitProcesses() int { return gitexec.CancelInFlight() }
