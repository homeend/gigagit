package git

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

var trackRe = regexp.MustCompile(`ahead (\d+)|behind (\d+)`)

// ParseBranches parses for-each-ref output formatted as:
//
//	%(HEAD)\x00%(refname:lstrip=2)\x00%(upstream:short)\x00%(objectname:short)\x00%(upstream:track)
//
// one ref per line.
func ParseBranches(data []byte) ([]model.Branch, error) {
	var out []model.Branch
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\x00")
		if len(f) < 4 {
			continue
		}
		b := model.Branch{
			IsHead:   strings.TrimSpace(f[0]) == "*",
			Name:     f[1],
			Upstream: f[2],
			Hash:     f[3],
		}
		if len(f) >= 5 {
			for _, m := range trackRe.FindAllStringSubmatch(f[4], -1) {
				if m[1] != "" {
					b.Ahead, _ = strconv.Atoi(m[1])
				}
				if m[2] != "" {
					b.Behind, _ = strconv.Atoi(m[2])
				}
			}
		}
		if len(f) >= 6 {
			b.UnixTime, _ = strconv.ParseInt(strings.TrimSpace(f[5]), 10, 64)
		}
		out = append(out, b)
	}
	return out, nil
}
