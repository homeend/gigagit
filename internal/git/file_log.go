package git

import (
	"context"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
	"github.com/homeend/gigagit/internal/model"
)

// FileLog returns the commits that touched path, newest first, following the
// file across renames. rev "" starts from HEAD. One invocation. limit bounds
// history depth for very large repos.
func (r *Repo) FileLog(ctx context.Context, rev, path string, limit int) ([]model.FileCommit, error) {
	// core.quotepath=false keeps non-ASCII paths raw in the --name-status lines
	// (git otherwise octal-quotes them), so the parsed Path round-trips through
	// ShowFile. -z is avoided here: it would mangle the --format/name-status
	// interleave that ParseFileLog relies on.
	b := gitcmd.New("log").
		Config("core.quotepath=false").
		ArgIf(rev != "", rev).
		Arg("--follow", "-M", "--name-status", "--format="+logFormat, "-n", strconv.Itoa(limit), "--", path)
	res, err := r.Runner.Run(ctx, "git log (file history)", b.ToArgv())
	if err != nil {
		return nil, err
	}
	return ParseFileLog([]byte(res.Stdout)), nil
}

// ParseFileLog parses interleaved `git log --name-status --format=<logFormat>`
// output: a format line (contains \x1f) opens a commit; the following
// tab-bearing line is that commit's name-status for the followed file.
func ParseFileLog(data []byte) []model.FileCommit {
	var out []model.FileCommit
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		if strings.Contains(line, "\x1f") {
			f := strings.Split(line, "\x1f")
			if len(f) < 5 {
				continue
			}
			fc := model.FileCommit{Commit: model.Commit{Hash: f[0], Author: f[2], Subject: f[4]}}
			if p := strings.Fields(f[1]); len(p) > 0 {
				fc.Commit.Parents = p
			}
			if t, err := strconv.ParseInt(f[3], 10, 64); err == nil {
				fc.Commit.UnixTime = t
			}
			out = append(out, fc)
			continue
		}
		if n := len(out); n > 0 && strings.Contains(line, "\t") {
			nf := strings.Split(line, "\t")
			out[n-1].Status = nf[0][:1]
			switch {
			case (out[n-1].Status == "R" || out[n-1].Status == "C") && len(nf) >= 3:
				out[n-1].OldPath = nf[1]
				out[n-1].Path = nf[2]
			case len(nf) >= 2:
				out[n-1].Path = nf[1]
			}
		}
	}
	return out
}
