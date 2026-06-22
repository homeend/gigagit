package git

import (
	"context"
	"strconv"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
	"github.com/gigagit/gg/internal/model"
)

// logFormat separates fields with \x1f (unit separator); one commit per line.
// %D carries ref decorations ("HEAD -> main, feature, tag: v1"); the trailing %S
// (needs --source) carries the branch each commit was reached from in the walk.
const logFormat = "%H%x1f%P%x1f%an%x1f%at%x1f%s%x1f%D%x1f%S"

// LogScope selects which refs the walk covers. Empty Branches → all local
// branches (plus HEAD); otherwise exactly the listed branch names.
type LogScope struct {
	Branches []string
}

// LogScoped returns up to limit commits (newest-first; --date-order when
// dateOrder is set, else git's default order) reachable from the scope's refs,
// skipping the first skip. --decorate (bare, short names) forces %D to populate
// across git versions.
func (r *Repo) LogScoped(ctx context.Context, limit, skip int, scope LogScope, dateOrder bool) ([]model.Commit, error) {
	b := gitcmd.New("log").
		Arg("-n", strconv.Itoa(limit)).
		ArgIf(dateOrder, "--date-order").
		Arg("--decorate", "--source", "--format="+logFormat).
		ArgIf(skip > 0, "--skip="+strconv.Itoa(skip))
	if len(scope.Branches) == 0 {
		// All local branches PLUS HEAD, so a detached HEAD's commits still show
		// (git dedupes HEAD when it is already on a branch).
		b = b.Arg("--branches", "HEAD")
	} else {
		b = b.Arg(scope.Branches...)
	}
	res, err := r.Runner.Run(ctx, "git log", b.ToArgv())
	if err != nil {
		return nil, err
	}
	return ParseLog([]byte(res.Stdout))
}

// CommitTimes returns the committer time (unix seconds) for each given commit
// in ONE invocation (`git log --no-walk --format=%H%x00%ct <sha…>`), keeping
// the cost flat for many worktrees. Empty input returns an empty map with no
// git call.
func (r *Repo) CommitTimes(ctx context.Context, shas []string) (map[string]int64, error) {
	out := map[string]int64{}
	if len(shas) == 0 {
		return out, nil
	}
	argv := gitcmd.New("log").Arg("--no-walk", "--format=%H%x00%ct").Arg(shas...).ToArgv()
	res, err := r.Runner.Run(ctx, "git log (commit times)", argv)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		f := strings.Split(strings.TrimSpace(line), "\x00")
		if len(f) != 2 {
			continue
		}
		if t, perr := strconv.ParseInt(f[1], 10, 64); perr == nil {
			out[f[0]] = t
		}
	}
	return out, nil
}

// CommitFiles returns the files changed by commit hash, in git's path order.
// One invocation. `git log -1 -m --first-parent` shows the commit's diff against
// its first parent only: for a merge that is its mainline diff, and crucially
// for a stash commit (a merge of HEAD + the index commit) it lists each file
// once instead of once per parent — `diff-tree -m --first-parent` double-lists a
// file that differs from both stash parents. --root makes the initial commit
// list its files; --format= drops the commit header so only the diff remains.
func (r *Repo) CommitFiles(ctx context.Context, hash string) ([]model.CommitFile, error) {
	argv := gitcmd.New("log").
		Arg("-1", "-m", "--first-parent", "--root", "--name-status", "-M", "--format=", hash).
		ToArgv()
	res, err := r.Runner.Run(ctx, "git log (commit files)", argv)
	if err != nil {
		return nil, err
	}
	return ParseNameStatus([]byte(res.Stdout)), nil
}

// TreeFiles lists every file in commit's tree — the full set that would exist
// if the commit were checked out — via `ls-tree -r --name-only -z`. It walks
// tree objects only (no checkout / working-tree stat), so it is cheap even on a
// huge repo and works for any commit. Each entry has an empty Status (it is the
// whole tree, not a change set); -z is NUL-separated so unusual paths survive.
func (r *Repo) TreeFiles(ctx context.Context, commit string) ([]model.CommitFile, error) {
	argv := gitcmd.New("ls-tree").Arg("-r", "--name-only", "-z", commit).ToArgv()
	res, err := r.Runner.Run(ctx, "git ls-tree (tree files)", argv)
	if err != nil {
		return nil, err
	}
	var out []model.CommitFile
	for _, p := range strings.Split(strings.TrimRight(res.Stdout, "\x00"), "\x00") {
		if p == "" {
			continue
		}
		out = append(out, model.CommitFile{Path: p})
	}
	return out, nil
}

// ParseNameStatus parses `--name-status` lines: "M\tpath" or, for renames
// and copies, "R<score>\told\tnew". Blank and malformed lines are skipped;
// the status letter is the first byte of the status field.
func ParseNameStatus(data []byte) []model.CommitFile {
	var out []model.CommitFile
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 2 || f[0] == "" || f[1] == "" {
			continue
		}
		cf := model.CommitFile{Status: f[0][:1], Path: f[1]}
		if (cf.Status == "R" || cf.Status == "C") && len(f) >= 3 && f[2] != "" {
			cf.OldPath = f[1]
			cf.Path = f[2]
		}
		out = append(out, cf)
	}
	return out
}

// ParseLog parses lines of "%H\x1f%P\x1f%an\x1f%at\x1f%s".
func ParseLog(data []byte) ([]model.Commit, error) {
	var out []model.Commit
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\x1f")
		if len(f) < 6 {
			continue
		}
		c := model.Commit{
			Hash:    f[0],
			Author:  f[2],
			Subject: f[4],
			Refs:    parseDecorations(f[5]),
		}
		if len(f) >= 7 { // %S source ref (present once the walk has --source)
			c.Source = strings.TrimSpace(f[6])
		}
		if p := strings.Fields(f[1]); len(p) > 0 {
			c.Parents = p
		}
		if t, err := strconv.ParseInt(f[3], 10, 64); err == nil {
			c.UnixTime = t
		}
		out = append(out, c)
	}
	return out, nil
}

// parseDecorations splits a `%D` value ("HEAD -> main, feature, tag: v1,
// origin/main") into refs. Empty → nil. The HEAD-pointed branch carries Head=true.
func parseDecorations(d string) []model.Ref {
	d = strings.TrimSpace(d)
	if d == "" {
		return nil
	}
	var refs []model.Ref
	for _, tok := range strings.Split(d, ", ") {
		tok = strings.TrimSpace(tok)
		switch {
		case tok == "":
			continue
		case strings.HasPrefix(tok, "HEAD -> "):
			refs = append(refs, model.Ref{Name: strings.TrimPrefix(tok, "HEAD -> "), Kind: model.RefLocal, Head: true})
		case tok == "HEAD":
			refs = append(refs, model.Ref{Name: "HEAD", Kind: model.RefHead})
		case strings.HasPrefix(tok, "tag: "):
			refs = append(refs, model.Ref{Name: strings.TrimPrefix(tok, "tag: "), Kind: model.RefTag})
		case strings.Contains(tok, "/"): // Phase-1 simplification: slashy ⇒ remote-tracking
			refs = append(refs, model.Ref{Name: tok, Kind: model.RefRemote})
		default:
			refs = append(refs, model.Ref{Name: tok, Kind: model.RefLocal})
		}
	}
	return refs
}
