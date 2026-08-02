package domain

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/repogate"
)

// LogScope re-exports git.LogScope for the commit feed's scope (empty Branches =
// all local branches).
type LogScope = git.LogScope

// Snapshot is the git-read half of a TUI load: everything loadCmd needs from
// git, fetched in parallel under one Read reservation. config.Load and
// repos.Touch are deliberately NOT here — they are not git reads and stay in
// the frontend. Commits are owned by CommitFeed, not Snapshot.
type Snapshot struct {
	Status          model.WorkingTreeStatus
	Branches        []model.Branch
	RemoteBranches  []model.RemoteBranch
	Worktrees       []model.Worktree
	Tags            []model.Tag
	Reflog          []model.ReflogEntry
	CurrentWorktree string // git toplevel; "" if TopLevel failed
	GitCommonDir    string // "" if it failed
	HeadTimes       map[string]int64
	Conflict        ConflictState // source of any in-progress conflict (zero if none)
}

// query runs fn under a Read reservation on s's gate, coalescing concurrent
// calls with the same key. The reservation is outermost-via-singleflight: the
// leader acquires it and runs fn; followers share the result without
// acquiring.
func query[T any](ctx context.Context, s *Service, key string, fn func(context.Context) (T, error)) (T, error) {
	v, err := s.flight.Do(key, func() (any, error) {
		res, e := s.gateFor(ctx).Acquire(ctx, repogate.Read, "read "+key)
		if e != nil {
			return nil, e
		}
		defer res.Release()
		out, ferr := fn(ctx)
		if ferr != nil {
			observ.NoteFailure("query "+key, ferr)
		}
		return out, ferr
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return v.(T), nil
}

// queryQuiet is query without the failure seam: it runs fn under a Read
// reservation + singleflight but does NOT record errors to observ. Use it for
// opt-in NETWORK reads (e.g. RemoteTags) where a recurring background failure
// (offline) would otherwise flood errors.log every interval. The error is still
// returned so a manual caller can surface it.
func queryQuiet[T any](ctx context.Context, s *Service, key string, fn func(context.Context) (T, error)) (T, error) {
	v, err := s.flight.Do(key, func() (any, error) {
		res, e := s.gateFor(ctx).Acquire(ctx, repogate.Read, "read "+key)
		if e != nil {
			return nil, e
		}
		defer res.Release()
		return fn(ctx)
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return v.(T), nil
}

// RemoteTags returns the set of tag names present on the default remote (origin
// if configured, else the first remote). NETWORK read; routed through queryQuiet
// so background polls never spam the failure seam. No remote → empty set, nil.
func (s *Service) RemoteTags(ctx context.Context) (map[string]bool, error) {
	return queryQuiet(ctx, s, "remote-tags", func(ctx context.Context) (map[string]bool, error) {
		names, err := s.repo.RemoteNames(ctx)
		if err != nil {
			return nil, err
		}
		remote := pickDefaultRemote(names)
		if remote == "" {
			return map[string]bool{}, nil
		}
		return s.repo.RemoteTags(ctx, remote)
	})
}

// RemoteTagsFresh is RemoteTags WITHOUT singleflight coalescing: it always issues
// its own ls-remote under a Read reservation, so the caller's context (e.g. a 5s
// timeout) fully governs it — a coalesced follower of the background remote-tags
// lookup could not be cancelled by its own context. Like RemoteTags it resolves
// origin-or-first and records NO failure (a timeout/offline must not spam the
// session error log).
func (s *Service) RemoteTagsFresh(ctx context.Context) (map[string]bool, error) {
	res, err := s.gateFor(ctx).Acquire(ctx, repogate.Read, "read remote-tags-fresh")
	if err != nil {
		return nil, err
	}
	defer res.Release()
	names, err := s.repo.RemoteNames(ctx)
	if err != nil {
		return nil, err
	}
	remote := pickDefaultRemote(names)
	if remote == "" {
		return map[string]bool{}, nil
	}
	return s.repo.RemoteTags(ctx, remote)
}

// pickDefaultRemote returns "origin" if present, else the first remote, else "".
func pickDefaultRemote(names []string) string {
	for _, n := range names {
		if n == "origin" {
			return "origin"
		}
	}
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

// Snapshot fetches the six startup reads. Status/Branches/Worktrees are
// fatal (the first failure is returned); CommitTimes/TopLevel/GitCommonDir are
// best-effort (failures leave zero values). Commits are owned by CommitFeed.
func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	return query(ctx, s, "snapshot", s.loadSnapshot)
}

func (s *Service) loadSnapshot(ctx context.Context) (Snapshot, error) {
	var (
		snap     Snapshot
		mu       sync.Mutex
		firstErr error
		wg       sync.WaitGroup
	)
	fatal := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}
	run := func(f func()) { wg.Add(1); go func() { defer wg.Done(); f() }() }

	run(func() {
		st, err := s.statusFiltered(ctx)
		if err != nil {
			fatal(err)
			return
		}
		mu.Lock()
		snap.Status = st
		mu.Unlock()
	})
	run(func() {
		bs, err := s.repo.Branches(ctx)
		if err != nil {
			fatal(err)
			return
		}
		mu.Lock()
		snap.Branches = bs
		mu.Unlock()
	})
	run(func() {
		// RemoteBranches is best-effort: a repo with no remotes (or a failing
		// for-each-ref) must not block startup.
		if rbs, err := s.repo.RemoteBranches(ctx); err == nil {
			mu.Lock()
			snap.RemoteBranches = rbs
			mu.Unlock()
		}
	})
	run(func() {
		// Tags is best-effort: a repo with no tags (or a failing for-each-ref)
		// must not block startup.
		if tags, err := s.repo.Tags(ctx); err == nil {
			mu.Lock()
			snap.Tags = tags
			mu.Unlock()
		}
	})
	run(func() {
		// Reflog is best-effort: a repo with no reflog must not block startup.
		// The startup read uses the default cap; the TUI re-reads with its
		// configured limit on refresh.
		if rl, err := s.repo.ReflogEntries(ctx, defaultReflogLimit); err == nil {
			mu.Lock()
			snap.Reflog = rl
			mu.Unlock()
		}
	})
	run(func() {
		// Worktrees is fatal; CommitTimes (best-effort) depends on its result,
		// so it runs here after Worktrees returns.
		wts, err := s.repo.Worktrees(ctx)
		if err != nil {
			fatal(err)
			return
		}
		mu.Lock()
		snap.Worktrees = wts
		mu.Unlock()
		shas := make([]string, 0, len(wts))
		for _, w := range wts {
			if w.Head != "" {
				shas = append(shas, w.Head)
			}
		}
		if times, terr := s.repo.CommitTimes(ctx, shas); terr == nil {
			mu.Lock()
			snap.HeadTimes = times
			mu.Unlock()
		}
	})
	run(func() {
		if top, err := s.repo.TopLevel(ctx); err == nil {
			mu.Lock()
			snap.CurrentWorktree = top
			mu.Unlock()
		}
	})
	run(func() {
		if gcd, err := s.repo.GitCommonDir(ctx); err == nil {
			mu.Lock()
			snap.GitCommonDir = gcd
			mu.Unlock()
		}
	})

	wg.Wait()
	if firstErr != nil {
		return Snapshot{}, firstErr
	}
	// Attribute any conflict to the merge/rebase in progress. Serial (after the
	// parallel reads) because it needs the resolved Status, and cheap because it
	// short-circuits unless Status actually has unmerged files.
	snap.Conflict = s.conflictState(ctx, snap.Status)
	return snap, nil
}

// logPage is the gated, singleflighted commit-page read the CommitFeed uses.
// The singleflight key includes the feed generation, scope, and skip. The gen is
// load-bearing: without it, a reload (C) that reuses a scope still in flight from
// a just-cancelled load (A) would coalesce onto A and inherit A's context.Canceled
// — blanking the panel. A distinct gen per load makes that impossible while still
// coalescing genuine concurrent reads of the same page within one generation.
func (s *Service) logPage(ctx context.Context, limit, skip int, scope LogScope, gen int, dateOrder bool) ([]model.Commit, error) {
	key := "commits:" + scopeKey(scope) + ":" + strconv.Itoa(gen) + ":" + strconv.Itoa(limit) + ":" + strconv.Itoa(skip) + ":" + strconv.FormatBool(dateOrder)
	return query(ctx, s, key, func(ctx context.Context) ([]model.Commit, error) {
		return s.repo.LogScoped(ctx, limit, skip, scope, dateOrder)
	})
}

// scopeKey is the stable cache/singleflight discriminator for a scope. It folds
// ref selection (branches + upstreams) AND the content filters, so two scopes
// that differ only by filter never collide.
func scopeKey(scope LogScope) string {
	base := "all"
	if len(scope.Branches) > 0 {
		base = strings.Join(scope.Branches, ",")
	}
	if len(scope.Upstreams) > 0 {
		base += "|up:" + strings.Join(scope.Upstreams, ",")
	}
	if len(scope.Paths) > 0 || scope.Author != "" || scope.Grep != "" || scope.Since != "" || scope.Until != "" {
		base += "\x00f:" + strings.Join(scope.Paths, "\x00") +
			"\x00a:" + scope.Author +
			"\x00g:" + scope.Grep +
			"\x00s:" + scope.Since +
			"\x00u:" + scope.Until
	}
	return base
}

// Status is a single gated read for the CLI status command. It runs the
// EOL-only reconcile (statusFiltered) so the CLI and the TUI agree.
func (s *Service) Status(ctx context.Context) (model.WorkingTreeStatus, error) {
	return query(ctx, s, "status", s.statusFiltered)
}

// Branches is a single gated read for the local branch list. The TUI uses it
// for a targeted refresh after a ref-only op (e.g. create-worktree) that does
// not warrant a full Snapshot (status walk + commit feed) on a huge repo.
func (s *Service) Branches(ctx context.Context) ([]model.Branch, error) {
	return query(ctx, s, "branches", s.repo.Branches)
}

// Worktrees is a single gated read for the CLI worktree commands.
func (s *Service) Worktrees(ctx context.Context) ([]model.Worktree, error) {
	return query(ctx, s, "worktrees", s.repo.Worktrees)
}

// RemoteBranches lists remote-tracking branches (refs/remotes).
func (s *Service) RemoteBranches(ctx context.Context) ([]model.RemoteBranch, error) {
	return query(ctx, s, "remote-branches", s.repo.RemoteBranches)
}

// Tags is a single gated read for the CLI tag commands and the TUI Tags tab.
func (s *Service) Tags(ctx context.Context) ([]model.Tag, error) {
	return query(ctx, s, "tags", s.repo.Tags)
}

// defaultReflogLimit caps the HEAD reflog read when no [ui] reflog_limit is set.
const defaultReflogLimit = 200

// Reflog returns the HEAD reflog entries (newest first), capped at limit
// (<=0 ⇒ defaultReflogLimit). Read reservation, singleflighted. The startup
// Snapshot uses the default; the TUI passes its configured limit on refresh.
func (s *Service) Reflog(ctx context.Context, limit int) ([]model.ReflogEntry, error) {
	if limit <= 0 {
		limit = defaultReflogLimit
	}
	return query(ctx, s, "reflog", func(ctx context.Context) ([]model.ReflogEntry, error) {
		return s.repo.ReflogEntries(ctx, limit)
	})
}

// ShowFile returns the raw blob of path at rev (git show rev:path), under a
// Read reservation, coalesced per (rev, path).
func (s *Service) ShowFile(ctx context.Context, rev, path string) ([]byte, error) {
	return query(ctx, s, "showfile:"+rev+":"+path, func(ctx context.Context) ([]byte, error) {
		return s.repo.ShowFile(ctx, rev, path)
	})
}

// CommitFiles returns the files changed by commit hash, under a Read
// reservation, coalesced per hash.
func (s *Service) CommitFiles(ctx context.Context, hash string) ([]model.CommitFile, error) {
	return query(ctx, s, "commit-files:"+hash, func(ctx context.Context) ([]model.CommitFile, error) {
		return s.repo.CommitFiles(ctx, hash)
	})
}

// TreeFiles returns every file in commit hash's tree (the full checked-out file
// set at that commit), under a Read reservation, coalesced per hash.
func (s *Service) TreeFiles(ctx context.Context, hash string) ([]model.CommitFile, error) {
	return query(ctx, s, "tree-files:"+hash, func(ctx context.Context) ([]model.CommitFile, error) {
		return s.repo.TreeFiles(ctx, hash)
	})
}

// CompareFiles returns the files that differ between two endpoints (left =
// older, right = newer), under a Read reservation. The singleflight key
// includes both endpoints; live endpoints (working tree / index) change
// underfoot, so callers that need strict freshness should not rely on a long
// coalesce window. Either endpoint may be a frozen EndpointShelf side, which
// is delegated to shelfCompareFiles BEFORE the query() wrapper below — see
// its doc comment for why that dispatch must not nest inside a held
// reservation.
func (s *Service) CompareFiles(ctx context.Context, left, right model.Endpoint) ([]model.CommitFile, error) {
	if left.Kind == model.EndpointShelf || right.Kind == model.EndpointShelf {
		return s.shelfCompareFiles(ctx, left, right)
	}
	return query(ctx, s, "compare-files:"+left.CacheTag()+":"+right.CacheTag(), func(ctx context.Context) ([]model.CommitFile, error) {
		files, err := s.repo.DiffTreeFiles(ctx, left, right)
		if err != nil {
			return nil, err
		}
		// `git diff` omits untracked files, so a comparison whose newer side is the
		// working tree would miss brand-new files. Add them as added ("A") entries
		// (an untracked file is new relative to both a commit and the index).
		if right.Kind == model.EndpointWorkTree {
			untracked, err := s.repo.UntrackedFiles(ctx)
			if err != nil {
				return nil, err
			}
			seen := make(map[string]bool, len(files))
			for _, f := range files {
				seen[f.Path] = true
			}
			for _, p := range untracked {
				if !seen[p] {
					files = append(files, model.CommitFile{Status: "A", Path: p})
				}
			}
		}
		return files, nil
	})
}

// TopLevel returns the repo's working-tree root, under a Read reservation.
func (s *Service) TopLevel(ctx context.Context) (string, error) {
	return query(ctx, s, "toplevel", func(ctx context.Context) (string, error) {
		return s.repo.TopLevel(ctx)
	})
}

// CurrentBranch returns the checked-out branch name, under a Read reservation.
func (s *Service) CurrentBranch(ctx context.Context) (string, error) {
	return query(ctx, s, "current-branch", s.repo.CurrentBranch)
}

// RevParse resolves rev to a full object id, under a Read reservation.
func (s *Service) RevParse(ctx context.Context, rev string) (string, error) {
	return query(ctx, s, "revparse:"+rev, func(ctx context.Context) (string, error) {
		return s.repo.RevParse(ctx, rev)
	})
}

// LastCommitMessage returns HEAD's full commit message, under a Read reservation.
func (s *Service) LastCommitMessage(ctx context.Context) (string, error) {
	return query(ctx, s, "last-commit-message", s.repo.LastCommitMessage)
}

// FileLog returns up to limit commits touching path at rev, newest first,
// under a Read reservation, coalesced per (rev, path, limit).
func (s *Service) FileLog(ctx context.Context, rev, path string, limit int) ([]model.FileCommit, error) {
	return query(ctx, s, "filelog:"+rev+":"+path+":"+strconv.Itoa(limit), func(ctx context.Context) ([]model.FileCommit, error) {
		return s.repo.FileLog(ctx, rev, path, limit)
	})
}

// CommitMessage returns rev's full commit message, under a Read reservation.
// Backs the reword popup's pre-fill.
func (s *Service) CommitMessage(ctx context.Context, rev string) (string, error) {
	return query(ctx, s, "commit-message:"+rev, func(c context.Context) (string, error) {
		return s.repo.CommitMessage(c, rev)
	})
}

// CommitRange lists onto..branch oldest-first with full messages, under a Read
// reservation. Backs the interactive-rebase editor.
func (s *Service) CommitRange(ctx context.Context, onto, branch string) ([]model.RangeCommit, error) {
	return query(ctx, s, "commit-range:"+onto+".."+branch, func(c context.Context) ([]model.RangeCommit, error) {
		return s.repo.LogRangeMessages(c, onto, branch)
	})
}

// WorktreeFile reads the working-tree bytes of a path under a Read reservation.
// Backs the conflict hunk picker (marker text) and hunk staging (the new side).
func (s *Service) WorktreeFile(ctx context.Context, path string) ([]byte, error) {
	return query(ctx, s, "worktree-file:"+path, func(c context.Context) ([]byte, error) {
		return s.repo.ReadWorktreeFile(c, path)
	})
}

// cachedBlame wraps a blame result so it can report its heap weight to the
// byte-budgeted cache (a bare []model.BlameLine cannot implement Sized).
type cachedBlame struct{ lines []model.BlameLine }

func (b cachedBlame) Size() int {
	n := 0
	for _, l := range b.lines {
		n += len(l.Hash) + len(l.Author) + len(l.Summary) + len(l.Content) + 64
	}
	return n
}

// Blame returns per-line blame for path at rev under a Read reservation,
// coalesced per (rev, path). Blame at a committed rev is immutable by
// (rev, path), so it is memoized in the "blame" LRU (a hit skips both the
// reservation and the git run — git blame is expensive on large repos).
// Working-tree blame (rev == "") is never cached: the file changes under live
// edits, exactly as the Differ leaves working-tree diffs uncached.
func (s *Service) Blame(ctx context.Context, rev, path string) ([]model.BlameLine, error) {
	load := func() ([]model.BlameLine, error) {
		return query(ctx, s, "blame:"+rev+":"+path, func(ctx context.Context) ([]model.BlameLine, error) {
			return s.repo.Blame(ctx, rev, path)
		})
	}
	if rev == "" {
		return load()
	}
	v, err := s.factory.Cache("blame").GetOrLoad("blame:"+rev+":"+path, func() (any, error) {
		lines, e := load()
		if e != nil {
			return nil, e
		}
		return cachedBlame{lines}, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(cachedBlame).lines, nil
}

// GitCommonDir returns the git common dir path, under a Read reservation.
func (s *Service) GitCommonDir(ctx context.Context) (string, error) {
	return query(ctx, s, "gitcommondir", s.repo.GitCommonDir)
}

// GitDir returns this worktree's git dir path, under a Read reservation.
func (s *Service) GitDir(ctx context.Context) (string, error) {
	return query(ctx, s, "gitdir", s.repo.GitDir)
}

// LsFiles returns every tracked file (paths relative to the working-tree root),
// under a Read reservation, singleflighted.
func (s *Service) LsFiles(ctx context.Context) ([]string, error) {
	return query(ctx, s, "ls-files", func(ctx context.Context) ([]string, error) {
		return s.repo.LsFiles(ctx)
	})
}

// CommitTimes returns the committer unix-second timestamp for each given SHA,
// under a Read reservation. Best-effort: the caller may ignore the error (as
// Snapshot's worktrees arm does). The key incorporates the SHA set so parallel
// calls for different sets coalesce only when the inputs match.
func (s *Service) CommitTimes(ctx context.Context, shas []string) (map[string]int64, error) {
	return query(ctx, s, "commitTimes:"+strings.Join(shas, ","), func(ctx context.Context) (map[string]int64, error) {
		return s.repo.CommitTimes(ctx, shas)
	})
}

// CommitLookup resolves rev to its short-sha + subject, reporting found=false
// when no such commit exists. Missing is an EXPECTED state here (a bookmarked
// or shelved commit may have been gc'd), so it is not an error and is never
// recorded to the failure log (queryQuiet); only a context cancellation
// propagates as err. Backs the TUI's cherry-pick lane probe.
func (s *Service) CommitLookup(ctx context.Context, rev string) (model.LogLine, bool, error) {
	line, err := queryQuiet(ctx, s, "commitLookup:"+rev, func(ctx context.Context) (model.LogLine, error) {
		return s.repo.CommitLine(ctx, rev)
	})
	if err != nil {
		if ctx.Err() != nil {
			return model.LogLine{}, false, ctx.Err()
		}
		return model.LogLine{}, false, nil
	}
	return line, true, nil
}

// ResolveRev resolves rev to its FULL commit sha, reporting found=false when
// git cannot resolve it. Missing is an expected state here (a typo, a gc'd
// sha), so it is not an error and never recorded to the failure log
// (queryQuiet) — the CommitLookup convention. CommitLookup stays the
// display-facing short-sha read; this exists for callers that must match
// feed rows by full hash (the web goto-sha).
func (s *Service) ResolveRev(ctx context.Context, rev string) (string, bool, error) {
	sha, err := queryQuiet(ctx, s, "resolveRev:"+rev, func(ctx context.Context) (string, error) {
		return s.repo.ResolveCommit(ctx, rev)
	})
	if err != nil {
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		return "", false, nil
	}
	return sha, true, nil
}

// BranchVersions lists a branch's recorded pre-operation snapshots, newest
// first, under a Read reservation.
func (s *Service) BranchVersions(ctx context.Context, branch string) ([]model.BranchVersion, error) {
	return query(ctx, s, "branch-versions:"+branch, func(ctx context.Context) ([]model.BranchVersion, error) {
		infos, err := s.repo.ForEachRef(ctx, strings.TrimSuffix(git.VersionRefPrefix, "/")+"/"+branch)
		if err != nil {
			return nil, err
		}
		var out []model.BranchVersion
		for _, info := range infos {
			b, op, ts, ok := git.ParseVersionRef(info.Ref)
			if !ok || b != branch { // prefix match may over-catch nested names
				continue
			}
			out = append(out, model.BranchVersion{Ref: info.Ref, Hash: info.Hash, Subject: info.Subject, Op: op, Unix: ts})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Unix != out[j].Unix {
				return out[i].Unix > out[j].Unix
			}
			return out[i].Ref > out[j].Ref
		})
		return out, nil
	})
}

// AllVersionBranches groups every recorded version by branch, marking
// branches that no longer exist (deleted-branch recovery entry point).
func (s *Service) AllVersionBranches(ctx context.Context) ([]model.VersionedBranch, error) {
	return query(ctx, s, "version-branches", func(ctx context.Context) ([]model.VersionedBranch, error) {
		infos, err := s.repo.ForEachRef(ctx, strings.TrimSuffix(git.VersionRefPrefix, "/"))
		if err != nil {
			return nil, err
		}
		byBranch := map[string]*model.VersionedBranch{}
		for _, info := range infos {
			b, _, ts, ok := git.ParseVersionRef(info.Ref)
			if !ok {
				continue
			}
			row := byBranch[b]
			if row == nil {
				row = &model.VersionedBranch{Branch: b}
				byBranch[b] = row
			}
			row.Count++
			if ts > row.LatestUnix {
				row.LatestUnix = ts
			}
		}
		if len(byBranch) == 0 {
			return nil, nil
		}
		branches, err := s.repo.Branches(ctx)
		if err != nil {
			return nil, err
		}
		exists := map[string]bool{}
		for _, b := range branches {
			exists[b.Name] = true
		}
		out := make([]model.VersionedBranch, 0, len(byBranch))
		for _, row := range byBranch {
			row.Deleted = !exists[row.Branch]
			out = append(out, *row)
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].LatestUnix != out[j].LatestUnix {
				return out[i].LatestUnix > out[j].LatestUnix
			}
			return out[i].Branch < out[j].Branch
		})
		return out, nil
	})
}

// DiffNoIndex returns the unified diff between two absolute filesystem paths
// (`git diff --no-index`), under a Read reservation. "" means identical.
// Backs the MCP gg_compare_file tool, which materializes both sides to temp
// files first.
func (s *Service) DiffNoIndex(ctx context.Context, a, b string) (string, error) {
	return query(ctx, s, "diffNoIndex:"+a+":"+b, func(ctx context.Context) (string, error) {
		return s.repo.DiffNoIndex(ctx, a, b)
	})
}
