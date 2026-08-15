package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
)

// historyRunner serves `git log` from a MUTABLE newest-first hash list, so a test
// can rewrite history (push new commits, drop the tip) between reads the way a
// live repo does. Honors -n <limit> and --skip=<n>; other commands return empty.
type historyRunner struct {
	mu     sync.Mutex
	hashes []string
}

func newHistoryRunner(n int) *historyRunner {
	hs := make([]string, 0, n)
	for i := 0; i < n; i++ {
		hs = append(hs, "c"+strconv.Itoa(i))
	}
	return &historyRunner{hashes: hs}
}

// prepend pushes newest-first hashes onto the head of history.
func (r *historyRunner) prepend(hashes ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hashes = append(append([]string(nil), hashes...), r.hashes...)
}

// replaceAll swaps in a completely unrelated history (a rewrite).
func (r *historyRunner) replaceAll(hashes []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hashes = append([]string(nil), hashes...)
}

// window returns the hashes git would emit for -n limit --skip=skip.
func (r *historyRunner) window(limit, skip int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for i := skip; i < skip+limit && i < len(r.hashes); i++ {
		out = append(out, r.hashes[i])
	}
	return out
}

func (r *historyRunner) RunEnv(ctx context.Context, name string, argv, env []string) (gitexec.Result, error) {
	return r.Run(ctx, name, argv)
}

func (r *historyRunner) Run(ctx context.Context, name string, argv []string) (gitexec.Result, error) {
	if name != "git log" {
		return gitexec.Result{}, nil
	}
	limit, skip := 0, 0
	for i, a := range argv {
		if a == "-n" && i+1 < len(argv) {
			limit, _ = strconv.Atoi(argv[i+1])
		}
		if strings.HasPrefix(a, "--skip=") {
			skip, _ = strconv.Atoi(strings.TrimPrefix(a, "--skip="))
		}
	}
	var b strings.Builder
	for _, h := range r.window(limit, skip) {
		// logFormat = %H%x1f%P%x1f%an%x1f%at%x1f%s%x1f%D — only Hash matters here.
		b.WriteString(h + "\x1f\x1fauthor\x1f0\x1fsubject\x1f\n")
	}
	return gitexec.Result{Stdout: b.String()}, nil
}

func (r *historyRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (gitexec.Result, error) {
	return r.Run(ctx, name, argv)
}

// feedOverHistory returns a feed with small page sizes (5 initial, 10 per page)
// over a mutable history of n commits.
func feedOverHistory(n int) (*CommitFeed, *historyRunner) {
	hr := newHistoryRunner(n)
	f := New(&git.Repo{Runner: hr}).CommitFeed()
	f.SetPageSizes(5, 10)
	return f, hr
}

// loadDeep loads page 0 plus `pages` further pages — the state a user reaches by
// scrolling or by hitting ctrl+f a few times.
func loadDeep(t *testing.T, f *CommitFeed, pages int) FeedState {
	t.Helper()
	st, err := f.LoadInitial(context.Background())
	if err != nil {
		t.Fatalf("load initial: %v", err)
	}
	for i := 0; i < pages; i++ {
		var loaded bool
		st, loaded, err = f.LoadMore(context.Background())
		if err != nil || !loaded {
			t.Fatalf("load more %d: loaded=%v err=%v", i, loaded, err)
		}
	}
	return st
}

func TestRefreshKeepsPagedHistoryAndPrependsNewCommits(t *testing.T) {
	f, hr := feedOverHistory(300)
	st := loadDeep(t, f, 2) // 5 + 10 + 10 = 25 commits paged in
	if len(st.Commits) != 25 {
		t.Fatalf("deep load = %d commits, want 25", len(st.Commits))
	}

	hr.prepend("new1", "new0") // two commits land while the user reads

	st, err := f.Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(st.Commits) != 27 {
		t.Fatalf("after refresh = %d commits, want 27 (25 kept + 2 new)", len(st.Commits))
	}
	if st.Commits[0].Hash != "new1" || st.Commits[1].Hash != "new0" {
		t.Fatalf("new commits not at the head: %q %q", st.Commits[0].Hash, st.Commits[1].Hash)
	}
	if st.Commits[2].Hash != "c0" || st.Commits[26].Hash != "c24" {
		t.Fatalf("tail not preserved: [2]=%q [26]=%q", st.Commits[2].Hash, st.Commits[26].Hash)
	}
}

// TestRefreshLoadMoreContinuity is the skip-arithmetic guard: after a prepend,
// the next page must continue exactly where the kept tail ends — no gap, no dup.
func TestRefreshLoadMoreContinuity(t *testing.T) {
	f, hr := feedOverHistory(300)
	loadDeep(t, f, 2)
	hr.prepend("new1", "new0")
	if _, err := f.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	st, loaded, err := f.LoadMore(context.Background())
	if err != nil || !loaded {
		t.Fatalf("load more after refresh: loaded=%v err=%v", loaded, err)
	}
	// 25 kept + 2 prepended + a 10-commit page = the first 37 of the live walk.
	want := hr.window(37, 0)
	got := make([]string, 0, len(st.Commits))
	for _, c := range st.Commits {
		got = append(got, c.Hash)
	}
	if len(got) != len(want) {
		t.Fatalf("after page = %d commits, want %d (%q)", len(got), len(want), strings.Join(got, " "))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("commit %d = %q, want %q (full: %q)", i, got[i], want[i], strings.Join(got, " "))
		}
	}
	seen := map[string]bool{}
	for _, h := range got {
		if seen[h] {
			t.Fatalf("duplicate commit %q after refresh + page", h)
		}
		seen[h] = true
	}
}

func TestRefreshFallsBackToHardResetWithoutOverlap(t *testing.T) {
	f, hr := feedOverHistory(300)
	loadDeep(t, f, 2)

	// A rewrite (rebase/filter-branch): nothing in page 0 is recognizable.
	rewritten := make([]string, 0, 300)
	for i := 0; i < 300; i++ {
		rewritten = append(rewritten, "r"+strconv.Itoa(i))
	}
	hr.replaceAll(rewritten)

	st, err := f.Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(st.Commits) != 5 {
		t.Fatalf("fallback = %d commits, want the 5-commit page 0", len(st.Commits))
	}
	if st.Commits[0].Hash != "r0" {
		t.Fatalf("fallback head = %q, want r0", st.Commits[0].Hash)
	}
	// The reset must leave the walk offset consistent: the next page continues
	// from r5, not from somewhere in the discarded accumulation.
	st, _, err = f.LoadMore(context.Background())
	if err != nil {
		t.Fatalf("load more after fallback: %v", err)
	}
	if len(st.Commits) != 15 || st.Commits[5].Hash != "r5" {
		t.Fatalf("after fallback page: %d commits, [5]=%q", len(st.Commits), st.Commits[5].Hash)
	}
}

func TestRefreshOnEmptyFeedWalksPageZero(t *testing.T) {
	f, _ := feedOverHistory(300)
	st, err := f.Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(st.Commits) != 5 || st.Commits[0].Hash != "c0" {
		t.Fatalf("empty-feed refresh = %d commits, head %q", len(st.Commits), st.Commits[0].Hash)
	}
}

// TestRefreshDropsTheTipWhenHistoryShrinks covers amend/reset: the old tip is
// gone, the rest of the paged-in history stays.
func TestRefreshDropsTheTipWhenHistoryShrinks(t *testing.T) {
	f, hr := feedOverHistory(300)
	loadDeep(t, f, 1) // 15 commits
	hr.mu.Lock()
	hr.hashes = hr.hashes[1:] // c0 dropped (reset --hard HEAD~1)
	hr.mu.Unlock()

	st, err := f.Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(st.Commits) != 14 || st.Commits[0].Hash != "c1" {
		t.Fatalf("after drop = %d commits, head %q, want 14 headed by c1", len(st.Commits), st.Commits[0].Hash)
	}
	st, _, err = f.LoadMore(context.Background())
	if err != nil {
		t.Fatalf("load more: %v", err)
	}
	if st.Commits[13].Hash != "c14" || st.Commits[14].Hash != "c15" {
		t.Fatalf("page continuity after a drop: [13]=%q [14]=%q", st.Commits[13].Hash, st.Commits[14].Hash)
	}
}

func TestRefreshBumpsGeneration(t *testing.T) {
	f, _ := feedOverHistory(300)
	loadDeep(t, f, 0)
	before := f.Gen()
	if _, err := f.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if f.Gen() == before {
		t.Fatal("Refresh must bump gen so an in-flight LoadMore page drops")
	}
}

// realRefreshRepo builds a Service over a real repo with n commits, a
// commit(subject) hook so a test can grow history mid-flight, and the repo dir
// for tests that change REFS rather than history (tagging, moving a branch).
func realRefreshRepo(t *testing.T, n int) (*Service, func(subject string), string) {
	t.Helper()
	dir := t.TempDir()
	run := func(a ...string) {
		c := exec.Command("git", a...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
	commit := func(subject string) {
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(subject+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		run("add", ".")
		run("commit", "-m", subject)
	}
	run("init", "-b", "main")
	for i := 0; i < n; i++ {
		commit("c" + strconv.Itoa(i))
	}
	return New(&git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}), commit, dir
}

// The end-to-end shape against a real git: page in depth, commit, refresh —
// the new commit lands on top and nothing paged in is lost.
func TestRefreshAgainstRealGitKeepsDepth(t *testing.T) {
	svc, commit, _ := realRefreshRepo(t, 8)
	feed := svc.CommitFeed()
	feed.SetPageSizes(2, 2)
	st := loadDeep(t, feed, 2) // 2 + 2 + 2 = 6 of the 8 commits
	if len(st.Commits) != 6 {
		t.Fatalf("deep load = %d commits, want 6", len(st.Commits))
	}
	before := hashList(st.Commits)

	commit("c8") // history grows while the user reads

	st, err := feed.Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(st.Commits) != 7 {
		t.Fatalf("after refresh = %d commits, want 7 (6 kept + 1 new)", len(st.Commits))
	}
	if st.Commits[0].Subject != "c8" {
		t.Fatalf("head subject = %q, want c8", st.Commits[0].Subject)
	}
	if got := hashList(st.Commits[1:]); got != before {
		t.Fatalf("tail changed:\n got %q\nwant %q", got, before)
	}
	// The next page must continue past the kept tail, into the two commits that
	// were never loaded.
	st, loaded, err := feed.LoadMore(context.Background())
	if err != nil || !loaded {
		t.Fatalf("load more after refresh: loaded=%v err=%v", loaded, err)
	}
	if len(st.Commits) != 9 {
		t.Fatalf("after page = %d commits, want all 9", len(st.Commits))
	}
	seen := map[string]bool{}
	for _, c := range st.Commits {
		if seen[c.Hash] {
			t.Fatalf("duplicate commit %q (%s)", c.Hash, c.Subject)
		}
		seen[c.Hash] = true
	}
	if st.Commits[7].Subject != "c1" || st.Commits[8].Subject != "c0" {
		t.Fatalf("oldest commits = %q, %q; want c1, c0", st.Commits[7].Subject, st.Commits[8].Subject)
	}
}

func TestRefreshInvalidatesScopeCache(t *testing.T) {
	f := gitexec.NewFakeRunner()
	feed := New(&git.Repo{Runner: f}).CommitFeed()
	feed.SetPageSizes(50, 50)

	f.SetResponse("git log", gitexec.Result{Stdout: logRows(3)}) // base: 3
	feed.LoadInitial(context.Background())
	f.SetResponse("git log", gitexec.Result{Stdout: logRows(1)}) // filtered: 1
	feed.ApplyScope(context.Background(), LogScope{Grep: "x"})

	// A background refresh of the filtered scope must invalidate the cached base
	// accumulation, exactly as LoadInitial does.
	f.SetResponse("git log", gitexec.Result{Stdout: logRows(4)})
	feed.Refresh(context.Background())

	st, err := feed.ApplyScope(context.Background(), LogScope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Commits) != 4 {
		t.Fatalf("base after refresh = %d, want 4 (re-walked, not stale 3)", len(st.Commits))
	}
}

// Tagging a commit that is already on screen must show up on the next refresh.
// The tag changes the ref graph, not the commit, so the row's hash is unchanged
// — exactly the case a reconcile is tempted to treat as "nothing to do here".
// Reported from the browser: the tag was created, F5 did not show it, and only
// restarting the server did.
//
// The boundary is honest and deliberate: a refresh re-walks the FIRST page (50
// commits in the web frontend), so decorations update there. A commit paged in
// far below keeps its loaded row until something rebuilds the feed — the price
// of not re-walking the whole history on every refresh.
func TestRefreshShowsATagAddedToALoadedCommit(t *testing.T) {
	svc, _, dir := realRefreshRepo(t, 10)
	feed := svc.CommitFeed()
	feed.SetPageSizes(4, 2)    // page zero covers the newest four
	st := loadDeep(t, feed, 2) // 4 + 2 + 2 = 8 of the 10
	if len(st.Commits) != 8 {
		t.Fatalf("deep load = %d commits, want 8", len(st.Commits))
	}
	target := st.Commits[2] // inside page zero, but not the tip
	if len(target.Refs) != 0 {
		t.Fatalf("target already decorated: %+v", target.Refs)
	}

	tag := exec.Command("git", "tag", "-a", "v1", "-m", "release", target.Hash)
	tag.Dir = dir
	if out, err := tag.CombinedOutput(); err != nil {
		t.Fatalf("git tag: %v\n%s", err, out)
	}

	st, err := feed.Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	var got *model.Commit
	for i := range st.Commits {
		if st.Commits[i].Hash == target.Hash {
			got = &st.Commits[i]
		}
	}
	if got == nil {
		t.Fatalf("the tagged commit vanished from the feed")
	}
	found := false
	for _, r := range got.Refs {
		if r.Kind == model.RefTag && r.Name == "v1" {
			found = true
		}
	}
	if !found {
		t.Errorf("refs after refresh = %+v, want tag v1", got.Refs)
	}
	// The depth paged in before the refresh is still there, and nothing is
	// duplicated by taking fresh rows for the overlap.
	if len(st.Commits) != 8 {
		t.Errorf("commits after refresh = %d, want the 8 still loaded", len(st.Commits))
	}
	if len(st.Commits) != len(hashSet(st.Commits)) {
		t.Errorf("refresh duplicated rows: %s", hashList(st.Commits))
	}
}

// hashSet is a dedupe helper for the duplicate check above.
func hashSet(cm []model.Commit) map[string]bool {
	m := make(map[string]bool, len(cm))
	for _, c := range cm {
		m[c.Hash] = true
	}
	return m
}
