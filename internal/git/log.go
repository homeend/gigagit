package git

import (
	"context"
	"strconv"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
	"github.com/gigagit/gg/internal/model"
)

// logFormat separates fields with \x1f (unit separator); one commit per line.
const logFormat = "%H%x1f%P%x1f%an%x1f%at%x1f%s"

// Log returns up to limit commits reachable from HEAD, newest first, skipping
// the first skip commits. skip=0 is the head of history (omits --skip).
func (r *Repo) Log(ctx context.Context, limit, skip int) ([]model.Commit, error) {
	argv := gitcmd.New("log").
		Arg("-n", strconv.Itoa(limit), "--format="+logFormat).
		ArgIf(skip > 0, "--skip="+strconv.Itoa(skip)).
		ToArgv()
	res, err := r.Runner.Run(ctx, "git log", argv)
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
		if len(f) < 5 {
			continue
		}
		c := model.Commit{
			Hash:    f[0],
			Author:  f[2],
			Subject: f[4],
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
