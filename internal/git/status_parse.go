// Package git contains thin git verb wrappers and pure output parsers.
package git

import (
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// ParseStatusV2 parses `git status --porcelain=v2 -z --branch` output.
func ParseStatusV2(data []byte) (model.WorkingTreeStatus, error) {
	var st model.WorkingTreeStatus
	tokens := splitNUL(data)
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok == "" {
			continue
		}
		switch tok[0] {
		case '#':
			parseBranchHeader(tok, &st)
		case '1':
			fields := strings.SplitN(tok, " ", 9)
			if len(fields) >= 9 {
				xy := fields[1]
				st.Files = append(st.Files, model.FileStatus{
					Path:     fields[8],
					Staged:   xy[0],
					Unstaged: xy[1],
					Kind:     model.KindTracked,
				})
			}
		case '2':
			// Rename/copy: original path is the next NUL-separated token.
			fields := strings.SplitN(tok, " ", 10)
			orig := ""
			if i+1 < len(tokens) {
				orig = tokens[i+1]
				i++
			}
			if len(fields) >= 10 {
				xy := fields[1]
				st.Files = append(st.Files, model.FileStatus{
					Path:     fields[9],
					OrigPath: orig,
					Staged:   xy[0],
					Unstaged: xy[1],
					Kind:     model.KindTracked,
				})
			}
		case 'u':
			fields := strings.SplitN(tok, " ", 11)
			if len(fields) >= 11 {
				xy := fields[1]
				st.Files = append(st.Files, model.FileStatus{
					Path:     fields[10],
					Staged:   xy[0],
					Unstaged: xy[1],
					Kind:     model.KindUnmerged,
				})
			}
		case '?':
			st.Files = append(st.Files, model.FileStatus{
				Path:     strings.TrimSpace(tok[1:]),
				Staged:   '?',
				Unstaged: '?',
				Kind:     model.KindUntracked,
			})
		case '!':
			st.Files = append(st.Files, model.FileStatus{Path: strings.TrimSpace(tok[1:]), Kind: model.KindIgnored})
		}
	}
	return st, nil
}

func parseBranchHeader(tok string, st *model.WorkingTreeStatus) {
	switch {
	case strings.HasPrefix(tok, "# branch.head "):
		st.Branch = strings.TrimPrefix(tok, "# branch.head ")
	case strings.HasPrefix(tok, "# branch.upstream "):
		st.Upstream = strings.TrimPrefix(tok, "# branch.upstream ")
	case strings.HasPrefix(tok, "# branch.ab "):
		parts := strings.Fields(strings.TrimPrefix(tok, "# branch.ab "))
		for _, p := range parts {
			if len(p) < 2 {
				continue
			}
			n, _ := strconv.Atoi(p[1:])
			if p[0] == '+' {
				st.Ahead = n
			} else if p[0] == '-' {
				st.Behind = n
			}
		}
	}
}

func splitNUL(data []byte) []string {
	parts := strings.Split(string(data), "\x00")
	// Trailing NUL produces an empty final element; drop it.
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}
