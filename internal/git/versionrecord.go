package git

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// metaTrailerKey is the ONE trailer a version snapshot carries. It is one key
// holding every field rather than a key per field on purpose: git 2.43 returns
// every value in every atom when a for-each-ref format string contains more
// than one %(trailers:key=…), which would silently corrupt every record.
const metaTrailerKey = "Gg-Meta"

// VersionMeta is what a snapshot records beyond its tip. Ours is the
// contribution being frozen, Other the tip it is landing on or against, and
// Base their merge base — which cannot be recomputed later, because after a
// merge merge-base(target, source) returns the source's tip, not the fork
// point. One-branch ops (amend, reset, undo-commit, delete-branch, restore)
// leave every field but Op empty.
type VersionMeta struct {
	Op             string
	Ours           string
	Other          string
	Base           string
	Source, Target string
}

// HasPreview reports whether this record can render a frozen preview.
func (m VersionMeta) HasPreview() bool { return m.Ours != "" && m.Base != "" }

// FormatVersionMeta renders the trailer value. The two branch names go LAST
// because a refname may contain most characters; the first four fields are
// positional. A one-branch op (every field but Op empty) renders as just Op:
// TrimRight drops the trailing separators, and strings.Fields on the parse
// side would have collapsed them anyway.
func FormatVersionMeta(m VersionMeta) string {
	return strings.TrimRight(strings.Join([]string{m.Op, m.Ours, m.Other, m.Base, m.Source, m.Target}, " "), " ")
}

// ParseVersionMeta reads a trailer value back. A record is either just Op (a
// one-branch op: amend, reset, undo-commit, delete-branch, restore) or all
// six fields (a two-branch op, Source/Target included — they are not
// optional on the wire). strings.Fields collapses the empty fields Format
// emits for a one-branch op, which is why those are the only two valid
// counts: anything else (including four or five fields, a two-branch record
// missing its branch names) is a parse failure, not a partial parse.
func ParseVersionMeta(s string) (VersionMeta, bool) {
	f := strings.Fields(strings.TrimSpace(s))
	switch len(f) {
	case 1:
		return VersionMeta{Op: f[0]}, true
	case 6:
		return VersionMeta{Op: f[0], Ours: f[1], Other: f[2], Base: f[3], Source: f[4], Target: f[5]}, true
	default:
		return VersionMeta{}, false
	}
}

// WriteVersionSnapshot creates the synthetic commit a version ref points at:
// tree = tip's own tree (so the commit is empty and `git log --all -p` shows
// no diff), first parent = tip (every reader unwraps it), plus Ours as a second
// parent when it differs — the merge case, where Ours is a live branch someone
// may delete. The message is the truth; parents only pin objects against gc.
func (r *Repo) WriteVersionSnapshot(ctx context.Context, tip string, m VersionMeta, unix int64) (string, error) {
	f, err := os.CreateTemp("", "gg-version-msg")
	if err != nil {
		return "", err
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	body := fmt.Sprintf("gg version snapshot (%s)\n\n%s: %s\n", m.Op, metaTrailerKey, FormatVersionMeta(m))
	if _, err := f.WriteString(body); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	b := gitcmd.New("commit-tree").Arg(tip+"^{tree}").Arg("-p", tip)
	if m.Ours != "" && m.Ours != tip {
		b = b.Arg("-p", m.Ours)
	}
	argv := b.Arg("-F", name).ToArgv()

	// Fixed identity and dates: commit-tree needs an identity a fresh repo may
	// not have, and pinning both makes the object deterministic.
	ts := strconv.FormatInt(unix, 10) + " +0000"
	env := []string{
		"GIT_AUTHOR_NAME=gg", "GIT_AUTHOR_EMAIL=gg@localhost", "GIT_AUTHOR_DATE=" + ts,
		"GIT_COMMITTER_NAME=gg", "GIT_COMMITTER_EMAIL=gg@localhost", "GIT_COMMITTER_DATE=" + ts,
	}
	res, err := r.Runner.RunEnv(ctx, "git commit-tree", argv, env)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
