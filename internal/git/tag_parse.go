package git

import (
	"strings"

	"github.com/gigagit/gg/internal/model"
)

// ParseTags parses `git for-each-ref refs/tags` output formatted as:
//
//	%(refname:short)\x00%(objecttype)\x00%(objectname:short)\x00%(*objectname:short)\x00%(contents:subject)\x00%(creatordate:unix)
//
// one ref per line. An annotated tag has objecttype "tag" and a non-empty
// peeled object (*objectname) — its real commit; a lightweight tag points
// straight at the commit (objecttype "commit", empty peel).
func ParseTags(data []byte) ([]model.Tag, error) {
	var out []model.Tag
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\x00")
		if len(f) < 3 {
			continue
		}
		annotated := f[1] == "tag"
		target := f[2]
		if annotated && len(f) >= 4 && f[3] != "" {
			target = f[3] // peeled commit
		}
		t := model.Tag{Name: f[0], Annotated: annotated, Target: target}
		if len(f) >= 5 {
			t.Subject = f[4]
		}
		out = append(out, t)
	}
	return out, nil
}
