package cli

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
)

// cmdBatch implements `gg batch [--keep-going]`: a script of gg commands on
// stdin (one per line; blank lines and #-comments skipped; a leading "gg "
// token is tolerated), all run against ONE shared service. Each command's
// output is framed as
//
//	#<idx> ok <cmdline>      or      #<idx> !<exit> <cmdline>
//
// with the command's stdout verbatim and each stderr line prefixed "! ",
// then a trailer "#done <n> ok[, <m> failed[ (stopped)]]". Sub-commands
// read an empty stdin, so a decision fork fails loud instead of blocking.
// Exit: 0 all ok, 1 any failure, 2 usage/parse error (nothing framed).
func cmdBatch(svc *domain.Service, workdir string, args []string, stdin io.Reader, stdout, stderr io.Writer, cwdFile string) int {
	fs := flag.NewFlagSet("batch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	keepGoing := fs.Bool("keep-going", false, "run every line even after one fails")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "usage: gg batch [--keep-going]   (commands on stdin, one per line)")
		return 2
	}

	type batchLine struct {
		echo string
		argv []string
	}
	var lines []batchLine
	sc := bufio.NewScanner(stdin)
	for sc.Scan() {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		argv, err := tokenizeBatchLine(raw)
		if err != nil {
			fmt.Fprintf(stderr, "batch: %v in %q\n", err, raw)
			return 2
		}
		if len(argv) > 0 && argv[0] == "gg" { // agents paste the prefix
			argv = argv[1:]
			raw = strings.TrimSpace(strings.TrimPrefix(raw, "gg"))
		}
		if len(argv) == 0 {
			continue
		}
		lines = append(lines, batchLine{echo: raw, argv: argv})
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintln(stderr, "batch: reading stdin:", err)
		return 2
	}

	ok, failed := 0, 0
	stopped := false
	for i, ln := range lines {
		// Buffer the whole section so the header can carry the outcome.
		// stdout goes in verbatim; stderr through the "! " prefixer. The
		// wrap in syncWriter mirrors Run: an operation goroutine may write
		// progress while the main goroutine writes errors.
		var section bytes.Buffer
		errW := &syncWriter{w: &prefixWriter{w: &section, prefix: "! "}}
		code := runOne(svc, workdir, ln.argv[0], ln.argv[1:], strings.NewReader(""), &section, errW, cwdFile)
		if code == 0 {
			ok++
			fmt.Fprintf(stdout, "#%d ok %s\n", i+1, ln.echo)
		} else {
			failed++
			fmt.Fprintf(stdout, "#%d !%d %s\n", i+1, code, ln.echo)
		}
		io.Copy(stdout, &section)
		// Defensive: every current command newline-terminates its last write, so
		// this is a no-op today. It guards the framing grammar against a future
		// command whose last write doesn't end in "\n" — without it, the next
		// line's "#<idx> ..." header would run together with the prior output.
		if b := section.Bytes(); len(b) > 0 && b[len(b)-1] != '\n' {
			io.WriteString(stdout, "\n")
		}
		if code != 0 && !*keepGoing {
			if i < len(lines)-1 {
				stopped = true
			}
			break
		}
	}

	trailer := fmt.Sprintf("#done %d ok", ok)
	if failed > 0 {
		trailer += fmt.Sprintf(", %d failed", failed)
		if stopped {
			trailer += " (stopped)"
		}
	}
	fmt.Fprintln(stdout, trailer)
	if failed > 0 {
		return 1
	}
	return 0
}

// prefixWriter inserts prefix at the start of every line written through
// it. Writes may split lines across calls; the prefix is emitted lazily at
// the first byte of each line.
type prefixWriter struct {
	w       io.Writer
	prefix  string
	midline bool
}

func (p *prefixWriter) Write(b []byte) (int, error) {
	written := 0
	for written < len(b) {
		if !p.midline {
			if _, err := io.WriteString(p.w, p.prefix); err != nil {
				return written, err
			}
			p.midline = true
		}
		rest := b[written:]
		nl := bytes.IndexByte(rest, '\n')
		if nl < 0 {
			n, err := p.w.Write(rest)
			return written + n, err
		}
		n, err := p.w.Write(rest[:nl+1])
		written += n
		if err != nil {
			return written, err
		}
		p.midline = false
	}
	return written, nil
}
