package git

import (
	"strconv"
	"strings"
)

// VersionRefPrefix is the namespace of branch-version snapshot refs:
// refs/gg/versions/<branch>/<unix-ts>-<op>. Outside refs/heads|tags|remotes,
// so never pushed/fetched; shared by all worktrees; pins objects against gc.
const VersionRefPrefix = "refs/gg/versions/"

// VersionRef builds the snapshot ref name for branch at unix time ts caused
// by opToken (a protocol value like "rebase"). Collisions are resolved by the
// caller bumping ts.
func VersionRef(branch, opToken string, unix int64) string {
	return VersionRefPrefix + branch + "/" + strconv.FormatInt(unix, 10) + "-" + opToken
}

// ParseVersionRef splits a snapshot ref back into branch, op token, and
// timestamp. Parsing is from the END: the last path segment is always
// <ts>-<op>; everything between the prefix and it is the branch name.
func ParseVersionRef(ref string) (branch, opToken string, unix int64, ok bool) {
	rest, found := strings.CutPrefix(ref, VersionRefPrefix)
	if !found {
		return "", "", 0, false
	}
	i := strings.LastIndex(rest, "/")
	if i <= 0 || i == len(rest)-1 {
		return "", "", 0, false
	}
	branch, seg := rest[:i], rest[i+1:]
	tsStr, op, found := strings.Cut(seg, "-")
	if !found || op == "" {
		return "", "", 0, false
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return "", "", 0, false
	}
	return branch, op, ts, true
}
