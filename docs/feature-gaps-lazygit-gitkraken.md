# Feature gaps vs lazygit & GitKraken

Comparison of gigagit (`gg`) against lazygit and GitKraken, prioritised for
usefulness in very large monorepos. Grounded in an audit of `internal/engine`,
`internal/git`, and `internal/cli` (2026-06-23).

**Already in gg** (do not re-list as gaps): smart pull/push/switch/merge/rebase,
commit/amend/reword, cherry-pick, revert, reset, interactive rebase
(pick/reword/squash/drop + reorder), conflict editor (hunk/line picker),
line/region staging, stage/unstage, discard, tags (ls/create/rm/checkout/push),
remotes (ls/fetch/prune/delete-remote-branch/checkout-tracking), worktrees,
stash (file-selection create + apply/pop/drop), bookmarks, shelf, compare-trees,
reflog recovery (reset/checkout dangling), identity & profiles, blame, file
history, fast-forward, undo, repo switcher, clipboard actions, external editor.

## Verified gaps

| # | Feature | lazygit | GitKraken | Monorepo value | Notes |
|---|---------|:------:|:---------:|:--------------:|-------|
| 1 | Sparse-checkout / partial-clone management | – | yes | **Critical** | THE defining 100GB-repo feature. Already named in M3 roadmap. Check out a subtree, manage cone patterns. Highest leverage item. |
| 2 | Commit/log search & path-scoped filtering (message, author, path) | yes | yes | **Critical** | `LogScope` only filters by branch today. "Commits touching `dir/`" + author/message search is daily bread in a 1.46M-commit repo. Extends existing `CommitFeed`/`LogScope`. |
| 3 | Force-push (with lease + plain) | yes | yes | High | `git.Push` has no force path, so rebase/amend/reword can't be published. A missing leg on shipped flows. **(implementing now)** |
| 4 | Fuzzy file finder / jump-to-file | – | yes | High | Tab-cycling 81k–100k files is painful. `ls-files`-backed filterable jump popup reusing the `g`/`G`/`R` switcher pattern. |
| 5 | git bisect | yes | yes | High | Regression hunting over huge history; worktree-awareness makes the checkout cost bearable (bisect in a throwaway worktree). |
| 6 | Submodule support | yes | yes | Medium | Large repos vendor via submodules; invisible in gg today. |
| 7 | Git LFS awareness | – | yes | Medium | Show pointer vs hydrated state; avoid diffing blobs. |
| 8 | GPG/SSH commit signing | yes | yes | Medium | Often *required* in enterprise monorepos. Natural extension of identity/profiles. |
| 9 | Custom commands / keybindings | yes | – | Medium | lazygit's signature extensibility; repo-specific flows without a gg release. |
| 10 | PR / forge integration (GitHub/GitLab) | – | yes | Lower | Heavy; arguably out of scope for a git-binary client. |
| 11 | Full multi-lane commit graph | yes | yes | Lower | Single-line was a deliberate perf choice (`--date-order` cost). Visual polish, not a monorepo win. |
| 12 | Custom patches (build from N commits, move between commits) | yes | – | Lower | Overlaps interactive-rebase + compare-trees. |

## Recommended order

**Monorepo-defining:** 1 (sparse-checkout) → 2 (path-scoped/searchable log).
**Close shipped-flow gaps:** 3 (force-push) → 4 (file finder) → 5 (bisect).
**Breadth:** 6 (submodules) → 8 (signing) → 7 (LFS) → 9 (custom commands).

Caveats: #10 (forge) and #11 (full graph) are the flashy GitKraken items but
neither serves the huge-monorepo thesis — ranked last despite marketing weight.
#3 is less a new feature than a missing leg on rebase/amend/reword.
