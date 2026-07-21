package git

import (
	"context"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
	"github.com/homeend/gigagit/internal/model"
)

// logFormat separates fields with \x1f (unit separator); one commit per line.
// %D carries ref decorations ("HEAD -> main, feature, tag: v1"); the trailing %S
// (needs --source) carries the branch each commit was reached from in the walk.
const logFormat = "%H%x1f%P%x1f%an%x1f%at%x1f%s%x1f%D%x1f%S"

// LogScope selects and narrows the walk. Branches selects refs (empty → all
// local branches plus HEAD). Upstreams are extra remote-tracking refs (e.g.
// "origin/main") appended to the walk so a branch's remote tip shows even when
// the local branch is behind. A scope ref that no longer resolves is skipped
// (--ignore-missing), not an error: a remote-tracking ref can vanish between
// scope application and the walk — git push --delete drops it immediately,
// and the feed re-walks its retained scope before the remote-branches list
// refreshes. Paths/Author/Grep/Since/Until further
// FILTER the result with native `git log` flags; any of them being set makes the
// feed a non-contiguous subset of history (path scope also narrows to commits
// that touched those paths). Branches/Upstreams alone do NOT count as a filter.
type LogScope struct {
	Branches  []string
	Upstreams []string
	Paths     []string
	Author    string
	Grep      string
	Since     string
	Until     string
}

// filtered reports whether any content-narrowing filter is active.
// Ref selection (Branches/Upstreams) alone does not count — it selects refs,
// not filters history.
func (s LogScope) filtered() bool {
	return len(s.Paths) > 0 || s.Author != "" || s.Grep != "" || s.Since != "" || s.Until != ""
}

// LogScoped returns up to limit commits (newest-first; --date-order when
// dateOrder is set, else git's default order) reachable from the scope's refs,
// skipping the first skip. --decorate=full forces %D to populate with FULL
// refnames (refs/heads/…, refs/remotes/…, refs/tags/…) so a local branch whose
// name contains a slash (e.g. feat/foo) is classified by namespace, not by a
// fragile "contains /" heuristic that mistakes it for a remote-tracking ref.
func (r *Repo) LogScoped(ctx context.Context, limit, skip int, scope LogScope, dateOrder bool) ([]model.Commit, error) {
	b := gitcmd.New("log").
		Arg("-n", strconv.Itoa(limit)).
		ArgIf(dateOrder, "--date-order").
		Arg("--ignore-missing").
		Arg("--decorate=full", "--decorate-refs-exclude=refs/gg/*", "--source", "--format="+logFormat).
		ArgIf(skip > 0, "--skip="+strconv.Itoa(skip)).
		ArgIf(scope.Author != "", "--author="+scope.Author).
		ArgIf(scope.Since != "", "--since="+scope.Since).
		ArgIf(scope.Until != "", "--until="+scope.Until)
	if scope.Grep != "" {
		b = b.Arg("--grep="+scope.Grep, "-i")
	}
	if len(scope.Branches) == 0 {
		// All local branches PLUS HEAD, so a detached HEAD's commits still show
		// (git dedupes HEAD when it is already on a branch).
		b = b.Arg("--branches", "HEAD")
	} else {
		b = b.Arg(scope.Branches...)
	}
	if len(scope.Upstreams) > 0 {
		b = b.Arg(scope.Upstreams...)
	}
	if len(scope.Paths) > 0 {
		b = b.Arg("--")
		b = b.Arg(scope.Paths...)
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
// -z makes the output NUL-separated and unquoted, so non-ASCII paths arrive as
// raw UTF-8 (git otherwise wraps them in quotes with octal escapes, which then
// breaks `git show <rev>:<path>`).
func (r *Repo) CommitFiles(ctx context.Context, hash string) ([]model.CommitFile, error) {
	argv := gitcmd.New("log").
		Arg("-1", "-m", "--first-parent", "--root", "--name-status", "-M", "-z", "--format=", hash).
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

// ParseNameStatus parses the NUL-separated (`-z`) form of `--name-status`. The
// stream is a flat sequence of NUL-terminated tokens: a status token followed
// by one path token (M/A/D/T/U) or, for renames and copies (R/C), two path
// tokens (old then new). Because `-z` disables git's path quoting, non-ASCII
// paths arrive verbatim as raw UTF-8. The status letter is the first byte of
// the status token; empty tokens (e.g. a leading record separator from the
// empty --format header) are skipped.
func ParseNameStatus(data []byte) []model.CommitFile {
	toks := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
	var out []model.CommitFile
	for i := 0; i < len(toks); {
		status := toks[i]
		i++
		if status == "" {
			continue
		}
		letter := status[:1]
		if letter == "R" || letter == "C" {
			if i+1 >= len(toks) { // need old + new; malformed tail → stop
				break
			}
			out = append(out, model.CommitFile{Status: letter, OldPath: toks[i], Path: toks[i+1]})
			i += 2
			continue
		}
		if i >= len(toks) { // need a path; malformed tail → stop
			break
		}
		out = append(out, model.CommitFile{Status: letter, Path: toks[i]})
		i++
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

// parseDecorations splits a full-refname `%D` value (from `--decorate=full`,
// e.g. "HEAD -> refs/heads/main, tag: refs/tags/v1, refs/remotes/origin/main")
// into refs. Empty → nil. Classification is by ref namespace, so a slash-named
// local branch (refs/heads/feat/foo) is correctly RefLocal, not RefRemote. The
// HEAD-pointed branch carries Head=true.
func parseDecorations(d string) []model.Ref {
	d = strings.TrimSpace(d)
	if d == "" {
		return nil
	}
	var refs []model.Ref
	for _, tok := range strings.Split(d, ", ") {
		tok = strings.TrimSpace(tok)
		head := false
		if rest, ok := strings.CutPrefix(tok, "HEAD -> "); ok {
			head, tok = true, rest
		}
		switch {
		case tok == "":
			continue
		case tok == "HEAD": // detached HEAD
			refs = append(refs, model.Ref{Name: "HEAD", Kind: model.RefHead})
		case strings.HasPrefix(tok, "tag: "):
			name := strings.TrimPrefix(strings.TrimPrefix(tok, "tag: "), "refs/tags/")
			refs = append(refs, model.Ref{Name: name, Kind: model.RefTag})
		case strings.HasPrefix(tok, "refs/heads/"):
			refs = append(refs, model.Ref{Name: strings.TrimPrefix(tok, "refs/heads/"), Kind: model.RefLocal, Head: head})
		case strings.HasPrefix(tok, "refs/remotes/"):
			refs = append(refs, model.Ref{Name: strings.TrimPrefix(tok, "refs/remotes/"), Kind: model.RefRemote})
		default:
			// Unknown namespace (refs/stash, refs/notes/…, or a bare name from an
			// older git ignoring --decorate=full): keep the token, treat as local.
			refs = append(refs, model.Ref{Name: tok, Kind: model.RefLocal, Head: head})
		}
	}
	return refs
}
