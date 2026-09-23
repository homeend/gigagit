package agentsession

import (
	"context"
	"errors"
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
	// with closeIO, so nothing feeds the emulator after its pipe is closed.
	ioMu   sync.Mutex
	closed bool

	changed      chan struct{}
	done         chan struct{}
	outDone      chan struct{} // closed when pumpOut has drained the PTY
	closeOnce    sync.Once
	cursorHidden atomic.Bool // DECTCEM state, fed by the emulator callback

	taps map[chan []byte]struct{} // raw-output subscribers (tap.go), under mu
	job  uintptr                  // Windows job object handle; 0 elsewhere
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
	cmd.Env = append(append(childEnv(os.Environ()), spec.Env...), "GG_SESSION_ID="+string(id), "TERM=xterm-256color")
	prepareCmd(cmd)
	if err := p.Start(cmd); err != nil {
		_ = p.Close()
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
		pty: p, emu: emu, cmd: cmd,
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
	buf := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			s.withEmu(func() { _, _ = s.emu.Write(buf[:n]) })
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
	// Let the child's last output reach the screen before closing. A
	// grandchild still holding the PTY open would keep the master readable
	// forever, so the drain is bounded.
	select {
	case <-s.outDone:
	case <-time.After(2 * time.Second):
	}
	s.mu.Lock()
	s.info.State, s.info.ExitCode = Exited, code
	s.mu.Unlock()
	s.closeIO()
	s.signal()
	close(s.done)
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
