package git

import (
	"context"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
	"github.com/homeend/gigagit/internal/model"
)

// fileLogArgv is the one argv FileLog and FileLogStream share.
// core.quotepath=false keeps non-ASCII paths raw in the --name-status lines
// (git otherwise octal-quotes them), so the parsed Path round-trips through
// ShowFile. -z is avoided here: it would mangle the --format/name-status
// interleave the parser relies on.
func fileLogArgv(rev, path string, limit int) []string {
	return gitcmd.New("log").
		Config("core.quotepath=false").
		ArgIf(rev != "", rev).
		Arg("--follow", "-M", "--name-status", "--format="+logFormat, "-n", strconv.Itoa(limit), "--", path).
		ToArgv()
}

// FileLog returns the commits that touched path, newest first, following the
// file across renames. rev "" starts from HEAD. One invocation. limit bounds
// history depth for very large repos.
func (r *Repo) FileLog(ctx context.Context, rev, path string, limit int) ([]model.FileCommit, error) {
	res, err := r.Runner.Run(ctx, "git log (file history)", fileLogArgv(rev, path, limit))
	if err != nil {
		return nil, err
	}
	return ParseFileLog([]byte(res.Stdout)), nil
}

// FileLogStream is FileLog handing each commit to emit as git prints it
// (newest first), so a caller can show the newest commits while git is still
// walking a huge history. One invocation; emit runs on the reader goroutine
// and may block (git then blocks on its full stdout pipe).
func (r *Repo) FileLogStream(ctx context.Context, rev, path string, limit int, emit func(model.FileCommit)) error {
	var p fileLogParser
	_, err := r.Runner.Stream(ctx, "git log (file history)", fileLogArgv(rev, path, limit), func(line string) {
		if fc, ok := p.line(line); ok {
			emit(fc)
		}
	})
	if err != nil {
		return err
	}
	if fc, ok := p.flush(); ok {
		emit(fc)
	}
	return nil
}

// ParseFileLog parses interleaved `git log --name-status --format=<logFormat>`
// output: a format line (contains \x1f) opens a commit; the following
// tab-bearing line is that commit's name-status for the followed file.
func ParseFileLog(data []byte) []model.FileCommit {
	var p fileLogParser
	var out []model.FileCommit
	for _, line := range strings.Split(string(data), "\n") {
		if fc, ok := p.line(line); ok {
			out = append(out, fc)
		}
	}
	if fc, ok := p.flush(); ok {
		out = append(out, fc)
	}
	return out
}

// fileLogParser is the line-at-a-time parser behind ParseFileLog and
// FileLogStream. A commit completes at its name-status line — not at the next
// commit's format line — so a stream never holds the newest hit back until git
// finds the next one. A commit with no status line (a merge) completes at the
// next format line or at flush.
type fileLogParser struct {
	open *model.FileCommit
}

// line feeds one output line and returns the commit it completed, if any.
func (p *fileLogParser) line(line string) (model.FileCommit, bool) {
	if line == "" {
		return model.FileCommit{}, false
	}
	if strings.Contains(line, "\x1f") {
		f := strings.Split(line, "\x1f")
		if len(f) < 5 {
			return model.FileCommit{}, false
		}
		prev, had := p.flush()
		fc := model.FileCommit{Commit: model.Commit{Hash: f[0], Author: f[2], Subject: f[4]}}
		if ps := strings.Fields(f[1]); len(ps) > 0 {
			fc.Commit.Parents = ps
		}
		if t, err := strconv.ParseInt(f[3], 10, 64); err == nil {
			fc.Commit.UnixTime = t
		}
		p.open = &fc
		return prev, had
	}
	if p.open == nil || !strings.Contains(line, "\t") {
		return model.FileCommit{}, false
	}
	nf := strings.Split(line, "\t")
	fc := p.open
	fc.Status = nf[0][:1]
	switch {
	case (fc.Status == "R" || fc.Status == "C") && len(nf) >= 3:
		fc.OldPath = nf[1]
		fc.Path = nf[2]
	case len(nf) >= 2:
		fc.Path = nf[1]
	}
	return p.flush()
}

// flush returns the open commit, if any, and closes it.
func (p *fileLogParser) flush() (model.FileCommit, bool) {
	if p.open == nil {
		return model.FileCommit{}, false
	}
	fc := *p.open
	p.open = nil
	return fc, true
}
