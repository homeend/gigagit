package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// ParseGitVersion extracts major.minor.patch from `git version` output. Git
// appends vendor suffixes ("2.39.3 (Apple Git-145)", "2.43.0.windows.1") and
// may omit the patch entirely, so only the first three dot-separated numeric
// fields are read and a missing patch is 0.
func ParseGitVersion(s string) ([3]int, error) {
	var out [3]int
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) < 3 || fields[0] != "git" || fields[1] != "version" {
		return out, fmt.Errorf("unrecognised git version line %q", strings.TrimSpace(s))
	}
	parts := strings.Split(fields[2], ".")
	for i := 0; i < 3 && i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			if i == 0 {
				return [3]int{}, fmt.Errorf("unrecognised git version %q", fields[2])
			}
			break // vendor suffix: stop at the first non-numeric field
		}
		out[i] = n
	}
	if out == [3]int{} {
		return out, fmt.Errorf("unrecognised git version %q", fields[2])
	}
	return out, nil
}

// GitVersion reports the installed git binary's version.
func (r *Repo) GitVersion(ctx context.Context) ([3]int, error) {
	res, err := r.Runner.Run(ctx, "git version", gitcmd.New("version").ToArgv())
	if err != nil {
		return [3]int{}, err
	}
	return ParseGitVersion(res.Stdout)
}
