// Package gitconfdocs is the curated slice of git's config catalog behind
// the TUI's git-config explorer: for ~60 common keys it knows the real
// default, a one-line description, and the value kind (so bools/enums get
// an option picker instead of a free-text field). Pure data — no git, no
// TUI imports; a staleness test asserts every curated key still exists in
// `git help -c`.
package gitconfdocs

import "strings"

// Kind tells the explorer which editor a key gets.
type Kind int

const (
	KindBool   Kind = iota // option picker: true/false
	KindEnum               // option picker: Options
	KindString             // free-text field
	KindInt                // free-text field (digits)
)

// Doc is one curated key: real default, one-line description, value kind.
type Doc struct {
	Key     string
	Kind    Kind
	Default string
	Desc    string
	Options []string // KindEnum only
}

func boolDoc(key, def, desc string) Doc {
	return Doc{Key: key, Kind: KindBool, Default: def, Desc: desc}
}

func enumDoc(key, def, desc string, options ...string) Doc {
	return Doc{Key: key, Kind: KindEnum, Default: def, Desc: desc, Options: options}
}

// docs is the curated table, grouped by section. Defaults are git's own
// (not gg's opinions); "(none)" = git has no default for the key.
var docs = []Doc{
	boolDoc("add.ignoreErrors", "false", "continue adding files when some fail to add"),
	boolDoc("advice.detachedHead", "true", "explain what a detached HEAD is when you enter one"),
	{Key: "blame.date", Kind: KindString, Default: "iso", Desc: "date format used by git blame"},
	enumDoc("branch.autoSetupRebase", "never", "auto-configure new branches to pull with rebase", "never", "local", "remote", "always"),
	{Key: "checkout.defaultRemote", Kind: KindString, Default: "(none)", Desc: "remote to prefer when checkout <branch> is ambiguous (e.g. origin)"},
	enumDoc("color.ui", "auto", "when git colors its output", "false", "true", "always", "auto", "never"),
	boolDoc("commit.gpgSign", "false", "GPG-sign every commit"),
	{Key: "commit.template", Kind: KindString, Default: "(none)", Desc: "file used as the starting commit message"},
	boolDoc("commit.verbose", "false", "show the diff in the commit message editor"),
	enumDoc("core.autocrlf", "false", "convert CRLF↔LF on checkout/checkin (Windows interop)", "true", "false", "input"),
	boolDoc("core.commitGraph", "true", "use the commit-graph file to speed up commit walks"),
	{Key: "core.compression", Kind: KindInt, Default: "-1", Desc: "zlib level for object compression (-1 = zlib default, 0-9)"},
	{Key: "core.editor", Kind: KindString, Default: "(none)", Desc: "editor for commit messages etc. (falls back to $EDITOR, then vi)"},
	enumDoc("core.eol", "native", "line endings for text files in the working tree", "lf", "crlf", "native"),
	{Key: "core.excludesFile", Kind: KindString, Default: "~/.config/git/ignore", Desc: "extra gitignore file applied to every repo"},
	boolDoc("core.fileMode", "true", "track the executable bit of files"),
	boolDoc("core.fsmonitor", "false", "use a filesystem monitor daemon to speed up git status"),
	{Key: "core.hooksPath", Kind: KindString, Default: ".git/hooks", Desc: "directory git runs hooks from"},
	boolDoc("core.ignoreCase", "false", "treat the filesystem as case-insensitive (set by git init)"),
	{Key: "core.pager", Kind: KindString, Default: "less", Desc: "pager for long output"},
	boolDoc("core.preloadIndex", "true", "preload index contents in parallel for faster status"),
	boolDoc("core.quotePath", "true", "escape non-ASCII path bytes in output (gg disables per-command)"),
	boolDoc("core.sparseCheckout", "false", "enable sparse-checkout (populate only part of the tree)"),
	boolDoc("core.symlinks", "true", "create symlinks in the working tree (false = plain files)"),
	enumDoc("core.untrackedCache", "keep", "cache untracked-file scans to speed up git status", "true", "false", "keep"),
	{Key: "core.whitespace", Kind: KindString, Default: "(none)", Desc: "whitespace problems git diff/apply should flag"},
	{Key: "credential.helper", Kind: KindString, Default: "(none)", Desc: "program that stores/supplies HTTPS credentials"},
	enumDoc("diff.algorithm", "myers", "diff algorithm", "myers", "minimal", "patience", "histogram"),
	enumDoc("diff.colorMoved", "no", "highlight moved lines in diffs", "no", "default", "plain", "blocks", "zebra", "dimmed-zebra"),
	enumDoc("diff.renames", "true", "rename detection in diffs", "false", "true", "copies"),
	{Key: "fetch.parallel", Kind: KindInt, Default: "1", Desc: "number of parallel children for fetching submodules/multiple remotes (0 = reasonable default)"},
	boolDoc("fetch.prune", "false", "prune deleted remote branches on every fetch"),
	boolDoc("fetch.pruneTags", "false", "also prune deleted remote tags on fetch (with fetch.prune)"),
	boolDoc("fetch.writeCommitGraph", "false", "refresh the commit-graph file after every fetch (big-repo speedup; gg's notification center sets this)"),
	{Key: "gc.auto", Kind: KindInt, Default: "6700", Desc: "loose-object threshold that triggers auto gc (0 = disable)"},
	boolDoc("gc.writeCommitGraph", "true", "rewrite the commit-graph file when gc runs"),
	enumDoc("gpg.format", "openpgp", "signature format for signing", "openpgp", "x509", "ssh"),
	boolDoc("grep.lineNumber", "false", "show line numbers in git grep by default"),
	{Key: "help.autoCorrect", Kind: KindString, Default: "0", Desc: "typo handling for subcommands (0=suggest, N=run after N/10s, immediate, prompt, never)"},
	{Key: "http.postBuffer", Kind: KindInt, Default: "1048576", Desc: "buffer size before HTTPS pushes switch to chunked transfer (bytes)"},
	{Key: "init.defaultBranch", Kind: KindString, Default: "master", Desc: "branch name git init creates"},
	boolDoc("log.abbrevCommit", "false", "show abbreviated commit hashes in git log by default"),
	enumDoc("log.date", "default", "date format in git log", "default", "relative", "local", "iso", "iso-strict", "rfc", "short", "raw", "human"),
	boolDoc("maintenance.auto", "true", "run automatic background maintenance after some commands"),
	enumDoc("merge.conflictStyle", "merge", "conflict marker style (zdiff3 adds the base, minimized)", "merge", "diff3", "zdiff3"),
	enumDoc("merge.ff", "true", "allow fast-forward merges (only = refuse real merges)", "true", "false", "only"),
	{Key: "merge.tool", Kind: KindString, Default: "(none)", Desc: "tool git mergetool launches"},
	{Key: "pack.threads", Kind: KindInt, Default: "0", Desc: "threads for pack compression (0 = one per CPU)"},
	enumDoc("pull.ff", "true", "fast-forward behavior for pull (only = refuse merge pulls)", "true", "false", "only"),
	enumDoc("pull.rebase", "false", "rebase instead of merge when pulling", "false", "true", "merges", "interactive"),
	boolDoc("push.autoSetupRemote", "false", "auto set upstream on first push of a new branch"),
	enumDoc("push.default", "simple", "what git push pushes with no refspec", "nothing", "current", "upstream", "tracking", "simple", "matching"),
	boolDoc("push.followTags", "false", "also push annotated tags reachable from pushed commits"),
	boolDoc("rebase.autoStash", "false", "stash/unstash automatically around rebase (gg's SmartPull does its own)"),
	boolDoc("rebase.autoSquash", "false", "auto-reorder fixup!/squash! commits in interactive rebase"),
	boolDoc("rebase.updateRefs", "false", "move stacked branch refs along when rebasing"),
	boolDoc("rerere.enabled", "false", "remember conflict resolutions and replay them (auto-enabled when .git/rr-cache exists)"),
	{Key: "safe.directory", Kind: KindString, Default: "(none)", Desc: "repo paths exempt from the dubious-ownership check (* = all)"},
	enumDoc("status.showUntrackedFiles", "normal", "how much untracked content git status lists", "no", "normal", "all"),
	boolDoc("submodule.recurse", "false", "recurse into submodules for checkout/pull/etc."),
	{Key: "tag.sort", Kind: KindString, Default: "refname", Desc: "default sort order for git tag (e.g. -version:refname)"},
	{Key: "user.email", Kind: KindString, Default: "(none)", Desc: "author/committer email (gg: Settings → Identity & profiles)"},
	{Key: "user.name", Kind: KindString, Default: "(none)", Desc: "author/committer name (gg: Settings → Identity & profiles)"},
	{Key: "user.signingKey", Kind: KindString, Default: "(none)", Desc: "key id used for signed commits/tags"},
}

// byLower indexes the table for case-insensitive lookup (git lowercases set
// keys; the catalog and this table are camelCase).
var byLower = func() map[string]*Doc {
	m := make(map[string]*Doc, len(docs))
	for i := range docs {
		m[strings.ToLower(docs[i].Key)] = &docs[i]
	}
	return m
}()

// Lookup returns the curated doc for key (any case), or nil when the key is
// not curated (the explorer renders it read-only with default "—").
func Lookup(key string) *Doc { return byLower[strings.ToLower(key)] }

// All returns the curated table in its grouped order.
func All() []Doc { return docs }
