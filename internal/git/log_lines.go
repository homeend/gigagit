package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
	"github.com/homeend/gigagit/internal/model"
)

// logLineFormat is one commit per line: short sha and subject separated by
// \x1f (unit separator), which cannot appear in either field.
const logLineFormat = "%h%x1f%s"

// LogLines returns up to n history rows reachable from rev, newest first.
// rev may be a branch, sha, or range string (A..B / A...B) — passed to git
// verbatim.
func (r *Repo) LogLines(ctx context.Context, rev string, n int) ([]model.LogLine, error) {
	b := gitcmd.New("log").Arg("--format="+logLineFormat, "-n", strconv.Itoa(n), rev)
	res, err := r.Runner.Run(ctx, "git log", b.ToArgv())
	if err != nil {
		return nil, err
	}
	return parseLogLines(res.Stdout), nil
}

// CommitLine returns rev's short sha and subject.
func (r *Repo) CommitLine(ctx context.Context, rev string) (model.LogLine, error) {
	b := gitcmd.New("log").Arg("-1", "--format="+logLineFormat, rev)
	res, err := r.Runner.Run(ctx, "git log", b.ToArgv())
	if err != nil {
		return model.LogLine{}, err
	}
	lines := parseLogLines(res.Stdout)
	if len(lines) == 0 {
		return model.LogLine{}, fmt.Errorf("no commit at %q", rev)
	}
	return lines[0], nil
}

func parseLogLines(out string) []model.LogLine {
	var lines []model.LogLine
	for _, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		sha, subject, ok := strings.Cut(ln, "\x1f")
		if !ok || sha == "" {
			continue
		}
		lines = append(lines, model.LogLine{Hash: sha, Subject: subject})
	}
	return lines
}
