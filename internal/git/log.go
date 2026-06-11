package git

import (
	"context"
	"strconv"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
	"github.com/gigagit/gg/internal/model"
)

// logFormat separates fields with \x1f (unit separator); one commit per line.
const logFormat = "%H%x1f%P%x1f%an%x1f%at%x1f%s"

// Log returns up to limit recent commits, newest first.
func (r *Repo) Log(ctx context.Context, limit int) ([]model.Commit, error) {
	argv := gitcmd.New("log").
		Arg("-n", strconv.Itoa(limit), "--format="+logFormat).ToArgv()
	res, err := r.Runner.Run(ctx, "git log", argv)
	if err != nil {
		return nil, err
	}
	return ParseLog([]byte(res.Stdout))
}

// ParseLog parses lines of "%H\x1f%P\x1f%an\x1f%at\x1f%s".
func ParseLog(data []byte) ([]model.Commit, error) {
	var out []model.Commit
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\x1f")
		if len(f) < 5 {
			continue
		}
		c := model.Commit{
			Hash:    f[0],
			Author:  f[2],
			Subject: f[4],
		}
		if p := strings.Fields(f[1]); len(p) > 0 {
			c.Parents = p
		}
		if t, err := strconv.ParseInt(f[3], 10, 64); err == nil {
			c.UnixTime = t
		}
		out = append(out, c)
	}
	return out, nil
}
