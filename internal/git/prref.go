package git

import (
	"context"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// PRRefPrefix is where gg keeps fetched pull-request heads:
// refs/gg/pr/<number>. Like refs/gg/versions/ it sits outside
// refs/heads|tags|remotes — never pushed, never decorated in the graph
// (--decorate-refs-exclude=refs/gg/*). A ref here is also gg's durable record
// that the user opened that PR.
const PRRefPrefix = "refs/gg/pr/"

// PRRef names PR n's head ref.
func PRRef(n int) string { return PRRefPrefix + strconv.Itoa(n) }

// ParsePRRef is PRRef's inverse; ok=false for anything else.
func ParsePRRef(ref string) (int, bool) {
	s, found := strings.CutPrefix(ref, PRRefPrefix)
	if !found {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 || strconv.Itoa(n) != s {
		return 0, false
	}
	return n, true
}

// FetchRefspec force-fetches one server ref into one local ref
// (`git fetch --no-tags --no-write-fetch-head <remote> +<src>:<dst>`).
// remote may be a configured name or a URL.
func (r *Repo) FetchRefspec(ctx context.Context, remote, src, dst string) error {
	argv := gitcmd.New("fetch").Arg("--no-tags", "--no-write-fetch-head", remote, "+"+src+":"+dst).ToArgv()
	_, err := r.Runner.Run(ctx, "git fetch refspec", argv)
	return err
}
