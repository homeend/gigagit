package agentsession

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/charmbracelet/x/xpty"
)

// Session is one running (or exited) program in a PTY.
type Session struct {
	mu   sync.Mutex
	info Info
	pty  xpty.Pty
	emu  *vt.SafeEmulator
	cmd  *exec.Cmd

	// ioMu serialises every emulator mutation (Write, keys, paste, resize)
	// with closeIO, so nothing feeds the emulator after its pipe is closed,
	// and with snapshot reads: SafeEmulator locks each call, but CellAt
	// returns a live cell pointer and Width/Render are separate calls.
	ioMu   sync.Mutex
	closed bool

	changed      chan struct{}
	done         chan struct{}
	outDone      chan struct{} // closed when pumpOut has drained the PTY
	closeOnce    sync.Once
	cursorHidden atomic.Bool // DECTCEM state, fed by the emulator callback

	taps    map[chan []byte]struct{} // raw-output subscribers (tap.go), under mu
	trace   *os.File                 // raw-output recording (StartSpec.TracePath); written by pumpOut only
	traceEv *os.File                 // <trace>.events: "offset cols rows" at start and every resize (under ioMu)
	traced  atomic.Int64             // bytes written to trace so far
	lastOut atomic.Int64             // UnixNano of pumpOut's latest read
	osc     oscFilter                // pumpOut-only: keeps UTF-8 in OSC payloads away from x/ansi's C1 parsing
	job     uintptr                  // Windows job object handle; 0 elsewhere
}

func start(id ID, spec StartSpec) (*Session, error) {
	if len(spec.Argv) == 0 {
		return nil, errors.New("agentsession: empty argv")
	}
	bin, err := exec.LookPath(spec.Argv[0])
	if err != nil {
		return nil, err
	}
	cols, rows := max(spec.Cols, 20), max(spec.Rows, 5)
	p, err := xpty.NewPty(cols, rows)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, spec.Argv[1:]...)
	cmd.Dir = spec.Dir
	if spec.Cwd != "" {
		cmd.Dir = spec.Cwd
	}
	cmd.Env = append(append(childEnv(os.Environ()), spec.Env...), "GG_SESSION_ID="+string(id), "TERM=xterm-256color")
	var trace, traceEv *os.File
	if spec.TracePath != "" {
		if trace, err = os.Create(spec.TracePath); err != nil {
			_ = p.Close()
			return nil, err
		}
		if traceEv, err = os.Create(spec.TracePath + ".events"); err != nil {
			_ = trace.Close()
			_ = p.Close()
			return nil, err
		}
		fmt.Fprintf(traceEv, "0 %d %d\n", cols, rows)
	}
	prepareCmd(cmd, spec.CmdLine)
	if err := p.Start(cmd); err != nil {
		_ = p.Close()
		if trace != nil {
			_ = trace.Close()
			_ = traceEv.Close()
		}
		return nil, err
	}
	if up, ok := p.(*xpty.UnixPty); ok {
		_ = up.Slave().Close() // the child holds its own copy; ours would keep the master from ever reporting end-of-stream
	}
	emu := vt.NewSafeEmulator(cols, rows)
	emu.SetScrollbackSize(ScrollbackLines)
	s := &Session{
		info: Info{ID: id, Label: spec.Label, AgentID: spec.AgentID, Repo: spec.Repo, Dir: spec.Dir,
			Started: time.Now(), State: Running},
		pty: p, emu: emu, cmd: cmd, trace: trace, traceEv: traceEv,
		changed: make(chan struct{}, 1),
		done:    make(chan struct{}),
		outDone: make(chan struct{}),
	}
	// Callbacks run inside emu.Write under the emulator's lock: store only.
	emu.SetCallbacks(vt.Callbacks{CursorVisibility: func(v bool) { s.cursorHidden.Store(!v) }})
	attachJob(s)
	go s.pumpOut()
	go s.pumpIn()
	go s.wait()
	return s, nil
}

// childEnv drops the variables that describe gg's own host terminal rather
// than the console the child runs in: an agent seeing TMUX assumes it is a
// tmux pane (Claude prints tmux scroll hints). TERM is set explicitly.
func childEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "TMUX", "TMUX_PANE", "TERM", "GG_SESSION_ID":
			continue
		}
		out = append(out, kv)
	}
	return out
}

// pumpOut copies the child's output into the emulator. Linux reports EIO on
// the master once every slave fd is closed: that is end-of-stream, not a
// failure — any read error ends the pump.
func (s *Session) pumpOut() {
	defer close(s.outDone)
	if s.trace != nil {
		defer s.trace.Close()
		defer s.traceEv.Close()
	}
	buf := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			if s.trace != nil {
				_, _ = s.trace.Write(buf[:n])
				s.traced.Add(int64(n))
			}
			s.lastOut.Store(time.Now().UnixNano())
			s.withEmu(func() { _, _ = s.emu.Write(s.osc.filter(buf[:n])) })
			s.feedTaps(buf[:n])
			s.signal()
		}
		if err != nil {
			return
		}
	}
}

// pumpIn drains what the emulator emits (query replies, encoded keys,
// bracketed pastes) into a queue that a separate writer feeds to the child.
// The emulator's output is a synchronous io.Pipe written UNDER the
// emulator's lock (inside Write for replies, inside SendKey for keys), so
// this reader must never stall: a child that stops reading its stdin would
// otherwise block pumpOut (Write holds the lock) and the caller of SendKey.
// After a PTY write error the writer keeps draining and discarding until
// the emulator is closed. Ends at emulator Close (Read → EOF).
func (s *Session) pumpIn() {
	q := make(chan []byte, 1024)
	go func() {
		dead := false
		for p := range q {
			if !dead {
				if _, err := s.pty.Write(p); err != nil {
					dead = true
				}
			}
		}
	}()
	defer close(q)
	buf := make([]byte, 4096)
	for {
		n, err := s.emu.Read(buf)
		if n > 0 {
			q <- append([]byte(nil), buf[:n]...)
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) wait() {
	err := xpty.WaitProcess(context.Background(), s.cmd)
	code := 0
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		code = ee.ExitCode() // -1 when killed by a signal
	default:
		code = -1
	}
	// Let the child's last output reach the screen before closing. ConPTY
	// never ends the stream on its own (xpty holds the pipe's write end until
	// Close), and a grandchild holding a PTY open would keep it readable, so
	// the drain ends once output goes quiet, and is bounded.
	drainOutput(s.outDone, &s.lastOut, drainQuiet, 2*time.Second)
	s.mu.Lock()
	s.info.State, s.info.ExitCode = Exited, code
	s.mu.Unlock()
	s.closeIO()
	s.signal()
	close(s.done)
}

// drainQuiet is how long the output must stay silent after the exit before
// the drain gives up waiting for end-of-stream.
const drainQuiet = 150 * time.Millisecond

// drainOutput returns at end-of-stream, once no output has arrived for quiet
// (counted from the call at the earliest), or after bound.
func drainOutput(eof <-chan struct{}, last *atomic.Int64, quiet, bound time.Duration) {
	began := time.Now()
	deadline := time.NewTimer(bound)
	defer deadline.Stop()
	tick := time.NewTicker(quiet / 5)
	defer tick.Stop()
	for {
		select {
		case <-eof:
			return
		case <-deadline.C:
			return
		case now := <-tick.C:
			since := began
			if t := time.Unix(0, last.Load()); t.After(since) {
				since = t
			}
			if now.Sub(since) >= quiet {
				return
			}
		}
	}
}

func (s *Session) closeIO() {
	s.closeOnce.Do(func() {
		s.ioMu.Lock()
		s.closed = true
		// End pumpIn by closing the emulator's output pipe rather than calling
		// Emulator.Close: Close writes a flag that the (unlockable, blocked)
		// Read in pumpIn reads unsynchronised. Closing the writer makes that
		// Read return EOF with no shared-field write.
		if pw, ok := s.emu.InputPipe().(*io.PipeWriter); ok {
			_ = pw.CloseWithError(io.EOF)
		}
		s.ioMu.Unlock()
		_ = s.pty.Close()
	})
}

// withEmu runs f against the emulator unless the session has been closed.
func (s *Session) withEmu(f func()) {
	s.ioMu.Lock()
	defer s.ioMu.Unlock()
	if !s.closed {
		f()
	}
}

// signal marks the screen dirty without ever blocking the pump.
func (s *Session) signal() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

// Info returns a snapshot of the session's metadata.
func (s *Session) Info() Info { s.mu.Lock(); defer s.mu.Unlock(); return s.info }

// Changed receives a value when the screen (or state) changed since the
// last receive; bursts coalesce into one pending signal.
func (s *Session) Changed() <-chan struct{} { return s.changed }

// Done is closed once the exit has been recorded.
func (s *Session) Done() <-chan struct{} { return s.done }

// screenText is the visible grid as plain text, one line per row.
func (s *Session) screenText() string {
	s.ioMu.Lock() // CellAt hands out a live cell pointer: read it with writes excluded
	defer s.ioMu.Unlock()
	var b strings.Builder
	w, h := s.emu.Width(), s.emu.Height()
	for y := range h {
		for x := range w {
			if c := s.emu.CellAt(x, y); c != nil && c.Content != "" {
				b.WriteString(c.Content)
			} else {
				b.WriteByte(' ')
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}
