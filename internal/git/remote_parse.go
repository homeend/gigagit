package git

import (
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// ParseRemoteBranches parses `git for-each-ref refs/remotes` output formatted as:
//
//	%(refname:lstrip=2)\x00%(objectname:short)\x00%(committerdate:unix)
//
// one ref per line. The remote's default symref (listed as the bare remote name,
// e.g. "origin", or explicitly as "origin/HEAD") is dropped — it is a pointer,
// not a branch.
func ParseRemoteBranches(data []byte) ([]model.RemoteBranch, error) {
	var out []model.RemoteBranch
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\x00")
		if len(f) < 2 {
			continue
		}
		name := f[0]
		remote, branch, ok := strings.Cut(name, "/")
		if !ok {
			continue // bare remote name == the default symref short form
		}
		if branch == "HEAD" {
			continue // explicit origin/HEAD symref
		}
		rb := model.RemoteBranch{
			Name:   name,
			Remote: remote,
			Branch: branch,
			Hash:   f[1],
		}
		if len(f) >= 3 {
			rb.UnixTime, _ = strconv.ParseInt(strings.TrimSpace(f[2]), 10, 64)
		}
		out = append(out, rb)
	}
	return out, nil
}
