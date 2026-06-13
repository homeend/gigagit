package git

import (
	"strconv"
	"strings"

	"github.com/gigagit/gg/internal/model"
)

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
