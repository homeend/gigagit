package git

import "strings"

// ParseRemoteTags extracts the set of bare tag names from `git ls-remote --tags`
// output. Each line is "<sha>\trefs/tags/<name>"; the "^{}" peeled-dereference
// rows of annotated tags are dropped (they carry the same name, already added).
func ParseRemoteTags(out []byte) map[string]bool {
	const prefix = "refs/tags/"
	names := map[string]bool{}
	for _, ln := range strings.Split(string(out), "\n") {
		tab := strings.IndexByte(ln, '\t')
		if tab < 0 {
			continue
		}
		ref := strings.TrimSpace(ln[tab+1:])
		if !strings.HasPrefix(ref, prefix) {
			continue
		}
		name := strings.TrimSuffix(ref[len(prefix):], "^{}")
		if name != "" {
			names[name] = true
		}
	}
	return names
}
