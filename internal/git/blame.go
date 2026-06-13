package git

import (
	"context"
	"strconv"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
	"github.com/gigagit/gg/internal/model"
)

const zeroSha = "0000000000000000000000000000000000000000"

// ParseBlamePorcelain parses `git blame --porcelain` output into one BlameLine
// per source line. Porcelain emits a commit's full header (author, author-time,
// summary, …) only the first time the sha appears and an abbreviated header
// (`<sha> <orig> <final>`) thereafter, so commit metadata is cached by sha and
// reused for the repeats. The all-zero sha (a not-yet-committed line) becomes
// Hash "".
func ParseBlamePorcelain(data []byte) []model.BlameLine {
	type meta struct {
		author  string
		time    int64
		summary string
	}
	cache := map[string]*meta{}
	var out []model.BlameLine
	var cur *model.BlameLine
	curSha := ""

	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		if line[0] == '\t' {
			// Content line closes the current record.
			if cur != nil {
				cur.Content = line[1:]
				if m := cache[curSha]; m != nil {
					cur.Author = m.author
					cur.Time = m.time
					cur.Summary = m.summary
				}
				out = append(out, *cur)
				cur = nil
			}
			continue
		}
		if cur == nil {
			// A header line: "<40-hex sha> <orig> <final> [<num>]".
			f := strings.Fields(line)
			if len(f) >= 3 && len(f[0]) == 40 && isHex(f[0]) {
				sha := f[0]
				if sha == zeroSha {
					sha = ""
				}
				ln := model.BlameLine{Hash: sha}
				if n, err := strconv.Atoi(f[2]); err == nil {
					ln.LineNo = n
				}
				cur = &ln
				curSha = sha
				if _, ok := cache[curSha]; !ok {
					cache[curSha] = &meta{}
				}
			}
			continue
		}
		// Detail lines between a header and its content line populate the cache.
		switch {
		case strings.HasPrefix(line, "author "):
			cache[curSha].author = strings.TrimPrefix(line, "author ")
		case strings.HasPrefix(line, "author-time "):
			if t, err := strconv.ParseInt(strings.TrimPrefix(line, "author-time "), 10, 64); err == nil {
				cache[curSha].time = t
			}
		case strings.HasPrefix(line, "summary "):
			cache[curSha].summary = strings.TrimPrefix(line, "summary ")
		}
	}
	return out
}

func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// Blame returns one BlameLine per line of path as of rev (rev "" = working
// tree). One invocation. Blame is whole-file; the caller bounds nothing.
func (r *Repo) Blame(ctx context.Context, rev, path string) ([]model.BlameLine, error) {
	b := gitcmd.New("blame").
		Arg("--porcelain").
		ArgIf(rev != "", rev).
		Arg("--", path)
	res, err := r.Runner.Run(ctx, "git blame", b.ToArgv())
	if err != nil {
		return nil, err
	}
	return ParseBlamePorcelain([]byte(res.Stdout)), nil
}
