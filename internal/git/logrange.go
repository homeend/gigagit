package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
	"github.com/homeend/gigagit/internal/model"
)

// LogRangeMessages lists onto..branch oldest-first (git todo order) with each
// commit's full message. Records are NUL-separated (-z), fields by 0x1f, so a
// multi-line %B body parses unambiguously.
func (r *Repo) LogRangeMessages(ctx context.Context, onto, branch string) ([]model.RangeCommit, error) {
	argv := gitcmd.New("log").Arg("--reverse", "-z", "--format=%H%x1f%s%x1f%B").
		Arg(onto + ".." + branch).ToArgv()
	res, err := r.Runner.Run(ctx, "git log range", argv)
	if err != nil {
		return nil, err
	}
	var out []model.RangeCommit
	for _, rec := range strings.Split(res.Stdout, "\x00") {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		f := strings.SplitN(rec, "\x1f", 3)
		if len(f) != 3 {
			continue
		}
		out = append(out, model.RangeCommit{Hash: f[0], Subject: f[1], Message: f[2]})
	}
	return out, nil
}
