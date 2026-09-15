# Preview links, steering into a preview, `gg open` — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the `gg://` grammar a merge-preview form, teach every link consumer to treat it exactly as `--preview` does, let `gg session navigate` steer a TUI or web page INTO a preview, and add `gg open <link>` — which steers a live session or launches the TUI positioned on the link.

**Architecture:** `model.LinkTarget` gains `Preview *LinkPreview{Source, Target}`, set iff the target text contains `...`. `domain.ResolveLink` fills a new `Resolved.Preview *PreviewNoteSet` by calling `PreviewNotes` on the checkout that holds BOTH branches, and fills `Addr` with the committed address on the source tip. One CLI adapter turns that `Resolved` into the existing `previewTarget`, so every §2.3 consumer produces byte-for-byte what `--preview` produces. The steer wire gains `target.state = "preview"` with `source`/`target` names; the TUI parks a new pending stage until the preview's compare view has loaded, the web page opens the preview through `previews.js`. `gg open` reuses the same navigate builder, and `cmd/gg` installs a launcher seam so `internal/cli` never imports `internal/tui`.

**Tech Stack:** Go 1.26, Bubble Tea (TUI), vanilla ES modules (web), TOML e2e scenarios, real `git` in `t.TempDir()`.

**Spec:** `docs/superpowers/specs/2026-09-10-preview-notes-design.md` — **§2 (Feature B)** is the requirement; §3 lists what is explicitly out of scope; §1 (Feature A) is already merged on this tree — read the code, not §1, for its shape.

## Global Constraints

These apply to EVERY task. They are the controller's binding rulings plus the
process rules; do not re-decide them.

**Feature rulings (binding)**

1. **Preview link target shape.** `model.LinkTarget` gains
   `Preview *LinkPreview` where `type LinkPreview struct{ Source, Target string }`.
   For a preview link `State = model.StateCommitted` and `Commit = ""`; `Preview`
   is set iff the target text contains `...`. `domain.ResolveLink`'s empty-sha
   entry guard is scoped to `Preview == nil`.
2. **`Link.Address()` is NOT meaningful for a preview link** (empty sha). The
   ONLY `.Address()` call on a `model.Link` in the whole tree is
   `internal/domain/linkresolve.go:307` inside `finishLink` — every other hit of
   `.Address()` (`internal/mcp/bookmarks.go`, `internal/mcp/compare.go`,
   `internal/tui/bookmark_popup.go`, `internal/tui/link.go:100`/`:115`,
   `internal/tui/session_snapshot.go:251`, `internal/tui/shelf.go`,
   `internal/web/bookmarks.go`, `internal/web/compare.go:243`/`:508`) is
   `bookmark.Bookmark.Address()`, a different type. So `finishLink` is the one
   place to branch, and every consumer reads `Resolved.Addr` / `Resolved.Preview`.
3. **Resolution.** The resolver fills `Resolved.Preview` by calling
   `PreviewNotes(ctx, source, target)` on the chosen checkout's service.
   Candidates lacking EITHER branch are dropped (a `containing`-style filter
   using `ResolveRev` on both names); the cwd is NOT exempt from that filter for
   preview links (it still sorts first and wins ties). `Addr` is the committed
   address on `set.Tip` plus the rel path; `Commit` is the tip.
4. **Parse refusals.** `:old:` on a preview link → `ErrLink` at parse time. A
   sha-looking pair (both halves 7..64 hex) → parse error.
5. **CLI equivalence.** ONE adapter from `domain.Resolved` (`Preview != nil`)
   into the existing `previewFlag`/`PreviewNoteSet` code path, so every §2.3
   consumer produces exactly what `--preview` produces. `--preview` together
   with a preview link → `previewUsageErr` (exit 2). `gg show <preview link>` →
   exit 2 "use gg diff". Hunk → line for a preview link uses
   `PreviewHunkAnchor`, never `resolveHunkLine`.
6. **Steer wire.** `steer.Target` gains `Source`, `Target` (json `source` /
   `target`) for `State == "preview"`; `Commit` stays EMPTY on the wire — the
   consumer resolves the tip itself. Web `toSteerWire` accepts the new state.
   Attention marks (`highlight add`) key on the tip commit via the existing
   commit path (the CLI resolves the preview link to `Resolved.Addr` = tip) —
   no new wire for marks.
7. **TUI navigate into a preview.** A new pending stage parked until the
   preview's compare file list has loaded, then the existing diff stage. When no
   saved preview matches `(source, target)` the TUI opens a show-once preview
   through `m.openPreviewCmd("", source, target, "")` — exactly what
   `preview_actions.go`'s pair dialog does for "show once". A missing branch is
   refused with ENGLISH protocol prose via `steerFail`. Navigate with NO file
   lands on the Previews entry only, answering
   `revealed preview <target>...<source>`.
8. **Startup `--at <link>`.** `tui.Run` gains a start-at `model.Link`
   parameter. The Model stores it and, once the first snapshot has loaded
   (`dataLoadedMsg`, the success arm — verified: that is the first-load message),
   converts it into the SAME `steer.Command` the CLI would post (`Wait=false`,
   `ID=""`) and feeds `steerNavigate`. Failures surface as the status message
   only (no reply file is ever written for `ID==""`).
9. **`gg open` launch path.** `internal/cli` does NOT import `internal/tui`
   (guarded by `internal/archtest/import_guard_test.go`). `internal/cli/open.go`
   resolves the link (as `gg link resolve` does), checks `steer.Live` for a
   TUI/web session in the RESOLVED checkout, posts the navigate through
   `sendSteer` (wait ≤ 2 s, `--no-wait`) and prints `steered: <checkout>`;
   otherwise it calls the package-level seam
   `var LaunchTUI func(checkout string, at model.Link) int` (nil in tests → exit
   1 with "no live gg session in <checkout> and the TUI launcher is
   unavailable"). `cmd/gg/main.go` installs the seam with a closure that
   `chdir`s and runs the EXISTING startup sequence (TopLevel check →
   `tui.Preflight` → `tui.OpenErrorLog` → `tui.Run(..., at)`): main's
   no-subcommand TUI launch is refactored into ONE `launchTUI(dir, at,
   recordPath, cwdFile) int` used by both paths — no duplication. Exit 1 on an
   unresolvable link. `gg open` targets the TUI only; a web session found live
   is steered over HTTP, which is already what `sendSteer` does.
10. **Producers.** Previews panel row `.`-menu "Copy link" (a `panelPreviews`
    branch in `contextLinkText` using `selectedPreview()`); preview diff view
    `L` / "Copy link" → the file/line form (`previewSet` + the cursor anchor; an
    old-side cursor yields line 0 on the new side); the session snapshot's
    `cursor.link` is the preview form whenever a preview is open; CLI
    `gg link --preview P [<path>[:<line>]]` (via `addPreviewFlag`; `:old:`
    refused; `#hunk` allowed). `internal/web/static/links.js` and ALL web link
    PRODUCTION stay untouched (spec §3 — a follow-up).
11. **Web consumer.** `live.js` navigate with `state === "preview"` opens the
    preview through `previews.js`' existing open paths (the saved row by pair,
    else the transient `openOnce`), then the file, then the line; reply failures
    behave exactly as today.
12. **Skill text.** `using-gg.md`'s link-grammar block gains the three preview
    rows and "`gg open <link>` shows it to the user in their gg";
    `reviewing-with-gg.md`'s last workflow step becomes "print
    `gg link --preview <P>` and hand that link back". `agentskill.Version`
    72 → 73, `agentskill.ReviewVersion` 6 → 7; regenerate the two dogfood
    `.claude/skills/*/SKILL.md` copies with a THROWAWAY `go run` that calls
    `agentskill.UsingGG.SkillFile()` / `agentskill.ReviewingWithGG.SkillFile()`.
13. **Docs.** A CHANGELOG bullet at the TOP of the `## [Unreleased]` section;
    README's link-grammar block (it DOES list the grammar, README.md lines
    ~254-272) gains the preview rows; `docs/CLAUDE-details.md` gets a short
    paragraph. `CLAUDE.md` stays untouched except the `steer` package row, which
    may gain "preview target".

**Process constraints**

- Work ONLY in `/mnt/t/others/gigagit.worktrees/feat-preview-links` (branch
  `feat/preview-links`). Prefix every shell command with
  `cd /mnt/t/others/gigagit.worktrees/feat-preview-links &&`; use absolute paths
  for every file read/write. Never touch `/mnt/t/others/gigagit`.
- Run EVERYTHING in the foreground. Never `pkill -f`. Never `git push`. Never
  `gg init --update`.
- Every commit message ends with exactly these two trailers:
  ```
  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
  ```
- `CHANGELOG.md` contains a LITERAL `<<<<<<<` around line 1402. It is content,
  not a conflict. Edit the changelog by anchoring on the `## [Unreleased]`
  heading; never "resolve conflict markers".
- **i18n:** every NEW user-visible TUI string must be a literal `i18n.T` key
  present in all four bundles (`internal/i18n/ja.toml`, `ko.toml`, `zh.toml`,
  `ru.toml`); the AST-gate tests in `internal/tui` fail otherwise. Engine/CLI
  prose, steer replies and decision option VALUES stay ENGLISH. This plan is
  designed to add NO new TUI string keys — it reuses `"error: %s"`,
  `"Copy link"`, `"Copied link: %s"` and `"▸ agent moved the focus"`. If you
  find yourself adding one, add it to all four bundles in the same commit.
- **Tests:** `internal/tui` tests call `t.Parallel()` and must NEVER use
  `t.Setenv` — inject directories (`svc.UsePreviewsDir`, `m.steerDir`) instead.
  `internal/cli`'s `previewRepo` helper DOES use `t.Setenv`, so tests built on it
  stay serial (no `t.Parallel()`). Tests use a real `git` in a `t.TempDir()`.
- **CLI link tests use the LOCAL form** `gg:///<abs>/…`: `resolveLinkArg` reads
  the package global `RepoStatePath`, which is `""` in tests (empty registry), so
  the cwd is the only candidate.
- Build/test with `./test.sh` (staged) or targeted `go test ./internal/<pkg>/...`.
  Run `./test.sh race` before the final commit of the last task.
- TDD: write the failing test, run it and SEE it fail, implement, run it and see
  it pass, commit. One commit per task.

---

### Task 1: The preview form in the link grammar

**Files:**
- Modify: `internal/model/link.go`
- Test: `internal/model/link_test.go`

**Interfaces:**
- Produces:
  - `type LinkPreview struct{ Source, Target string }`
  - `LinkTarget` gains the field `Preview *LinkPreview`
  - `func LinkRefOK(s string) bool` — exported so producers (TUI, CLI) can
    refuse a branch name the grammar cannot hold
  - `ParseLink` accepts `@<target>...<source>`; `Link.String()` renders it
- Consumes: nothing (DAG leaf).

- [ ] **Step 1: Write the failing tests**

Append to `internal/model/link_test.go`:

```go
func TestParseLinkPreviewFormsRoundTrip(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"gg://gigagit@main...feat/login",
		"gg://gigagit/internal/a.go@main...feat/login",
		"gg://gigagit/internal/a.go@main...feat/login:42",
		"gg://gigagit/internal/a.go@main...feat/login#3",
		"gg://gigagit@origin/main...origin/feat/login",
		"gg:///mnt/t/repo/a.go@main...feat/x:7",
	} {
		l, err := ParseLink(s)
		if err != nil {
			t.Fatalf("ParseLink(%q) = %v", s, err)
		}
		if l.Target.Preview == nil {
			t.Fatalf("ParseLink(%q): Target.Preview is nil", s)
		}
		if l.Target.State != StateCommitted || l.Target.Commit != "" {
			t.Errorf("ParseLink(%q): Target = %+v, want StateCommitted with no sha", s, l.Target)
		}
		if got := l.String(); got != s {
			t.Errorf("String(Parse(%q)) = %q", s, got)
		}
	}
}

func TestParseLinkPreviewSplitsThePairInGitOrder(t *testing.T) {
	t.Parallel()
	l, err := ParseLink("gg://gigagit/a.go@origin/main...origin/feat/login:9")
	if err != nil {
		t.Fatal(err)
	}
	if l.Target.Preview.Target != "origin/main" || l.Target.Preview.Source != "origin/feat/login" {
		t.Errorf("Preview = %+v, want Target=origin/main Source=origin/feat/login", *l.Target.Preview)
	}
	if l.Path != "a.go" || l.Line != 9 || l.Side != NoteSideNew {
		t.Errorf("path/line/side = %q/%d/%s", l.Path, l.Line, l.Side)
	}
}

func TestParseLinkPreviewRefusals(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"gg://gigagit/a.go@main...feat/x:old:3",                                 // the old side is the merge base
		"gg://gigagit/a.go@abc1234...def5678",                                   // a sha-looking pair
		"gg://gigagit/a.go@1234567890abcdef1234...abcdef1234567890abcd",          // longer shas, same rule
		"gg://gigagit/a.go@...feat/x",                                            // no target half
		"gg://gigagit/a.go@main...",                                              // no source half
		"gg://gigagit/a.go@main...feat@x",                                        // '@' is not expressible in a ref here
		"gg://gigagit/a.go@main...feat...x",                                      // three dots twice
	} {
		if l, err := ParseLink(s); err == nil {
			t.Errorf("ParseLink(%q) = %+v, want a refusal", s, l)
		} else if !errors.Is(err, ErrLink) {
			t.Errorf("ParseLink(%q) error %v does not wrap ErrLink", s, err)
		}
	}
}

func TestLinkRefOK(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"main", "feat/login", "origin/main", "release-1.2"} {
		if !LinkRefOK(ok) {
			t.Errorf("LinkRefOK(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "a@b", "a:b", "a#b", "a b", "a...b"} {
		if LinkRefOK(bad) {
			t.Errorf("LinkRefOK(%q) = true, want false", bad)
		}
	}
}
```

If `errors` is not yet imported in that file, add it.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-links && go test ./internal/model/ -run 'Preview|LinkRefOK' -v`
Expected: FAIL — `l.Target.Preview undefined`, `undefined: LinkRefOK`.

- [ ] **Step 3: Implement the grammar**

In `internal/model/link.go`, add the type and the field:

```go
// LinkPreview is the merge-preview half of a link target: git's three-dot
// pair, spelled exactly as `git diff <target>...<source>` reads it. The NAMES
// travel between machines; the machine-local preview id does not (spec §2.1).
type LinkPreview struct {
	Source, Target string
}
```

Change `LinkTarget`:

```go
type LinkTarget struct {
	State FileState // StateUnstaged (default), StateStaged or StateCommitted
	// Commit is set iff State == StateCommitted AND Preview is nil; 7..64 hex
	// characters — 64, not 40, because a sha-256 repository's commit ids are 64
	// hex characters and every producer writes the FULL sha.
	Commit string
	// Preview is set iff the target text carried git's three-dot pair
	// (<target>...<source>). State is StateCommitted and Commit is EMPTY: which
	// commit the preview addresses (the source tip) is a per-machine question
	// only domain.ResolveLink can answer. Link.Address() therefore yields a
	// meaningless zero-sha address for a preview link — callers use
	// domain.ResolveLink and read Resolved.Addr / Resolved.Preview instead.
	Preview *LinkPreview
}
```

Extend `Link.Address()`'s doc comment (the body is unchanged):

```go
// Address builds the FileAddress the link points at. Worktree is filled by
// the resolver, which is also the only thing that can fill Path for a PARSED
// local link (see LinkRepo.Abs).
//
// It is NOT meaningful for a PREVIEW link: the source tip is resolved per
// machine, so the returned address carries an empty commit. domain.ResolveLink
// (the only caller in the tree) branches on Target.Preview before reaching it.
```

In `Link.String()`, replace the `case StateCommitted:` arm:

```go
	case StateCommitted:
		if p := l.Target.Preview; p != nil {
			b.WriteByte('@')
			b.WriteString(p.Target)
			b.WriteString("...")
			b.WriteString(p.Source)
			break
		}
		// StateCommitted is FileState's ZERO value, so a Link nobody filled in
		// would otherwise render a bare "@". Only a real sha earns the target.
		if l.Target.Commit != "" {
			b.WriteByte('@')
			b.WriteString(l.Target.Commit)
		}
```

In `ParseLink`, replace the `default:` arm of the target switch:

```go
	default:
		if i := strings.Index(tail, "..."); i >= 0 {
			tgt, src := tail[:i], tail[i+3:]
			if tgt == "" || src == "" {
				return linkErr("a merge preview names <target>...<source>, got %q", tail)
			}
			// Both halves hex and sha-shaped means the caller pasted two commit
			// ids. A preview is a pair of BRANCH names (spec §2.1): resolving a
			// sha pair would silently address a different thing on every machine.
			if isShaLink(tgt) && isShaLink(src) {
				return linkErr("a merge preview names two branches, not two shas, got %q", tail)
			}
			if !LinkRefOK(tgt) || !LinkRefOK(src) {
				return linkErr("%q is not a pair of branch names", tail)
			}
			// The preview's old side is the merge base, which no stored address
			// names (spec §1.1) — there is nothing for "old:" to point at.
			if l.Side == NoteSideOld {
				return linkErr("a merge preview addresses the new side only; drop \"old:\"")
			}
			l.Target = LinkTarget{State: StateCommitted, Preview: &LinkPreview{Source: src, Target: tgt}}
			break
		}
		if !isHexLink(tail) || len(tail) < 7 || len(tail) > 64 {
			return linkErr("target must be \"staged\", a commit sha of 7 to 64 hex characters, or <target>...<source>, got %q", tail)
		}
		l.Target = LinkTarget{State: StateCommitted, Commit: tail}
```

Add the two helpers beside `isHexLink`:

```go
// isShaLink reports whether s has the shape ParseLink accepts as a commit id.
func isShaLink(s string) bool { return isHexLink(s) && len(s) >= 7 && len(s) <= 64 }

// LinkRefOK reports whether a branch name can ride a preview link. The
// grammar's own separators ('@', ':', '#'), whitespace and a second "..." are
// not expressible — every PRODUCER calls this and refuses to emit rather than
// print something ParseLink would reject or reparse as a different place.
func LinkRefOK(s string) bool {
	if s == "" || strings.Contains(s, "...") {
		return false
	}
	return !strings.ContainsAny(s, "@:# \t")
}
```

Update `ParseLink`'s doc comment's grammar block to list the preview rows:

```go
//	gg://<repo>[/<path>]@<target>...<source>[:<line>|#<hunk>]  a merge preview
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-links && go test ./internal/model/ -run 'Link' -v`
Expected: PASS (every pre-existing link test still passes — the hex arm is
unchanged for non-`...` targets).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-links && git add internal/model/link.go internal/model/link_test.go && git commit -m "feat(model): gg:// links can address a merge preview

The target half now accepts git's three-dot pair (<target>...<source>),
parsed into LinkTarget.Preview. State stays StateCommitted with an EMPTY
sha: which commit the preview addresses is a per-machine question only
domain.ResolveLink can answer. :old: and a sha-looking pair are refused at
parse time.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 2: Resolving a preview link to a checkout and a note set

**Files:**
- Modify: `internal/domain/linkresolve.go`
- Test: `internal/domain/linkresolve_test.go`

**Interfaces:**
- Consumes: `model.LinkTarget.Preview *model.LinkPreview` (Task 1);
  `(*Service).PreviewNotes(ctx, source, target) (PreviewNoteSet, error)` and
  `PreviewNoteSet.OK()` (already on this tree, `internal/domain/previewnotes.go`).
- Produces:
  - `Resolved` gains `Preview *PreviewNoteSet`
  - `func previewCandidates(ctx context.Context, cands []linkCandidate, p *model.LinkPreview, opts ResolveOpts) []linkCandidate`
  - Contract: for a preview link `Resolved.Addr = FileAddress{State:
    StateCommitted, Commit: set.Tip, Path: <rel>}`, `Resolved.Commit = set.Tip`,
    `Resolved.Preview = &set`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/domain/linkresolve_test.go`:

```go
// linkRepoWithBranch makes a repo with a remote name, a second commit on
// branch, and returns the checkout. The default branch really is "main":
// linkRepoWithRemote → newRealRepo → gittest.BasicRepo runs
// `git init -b main` (internal/gittest/template.go:88).
func linkRepoWithBranch(t *testing.T, name, branch string) string {
	t.Helper()
	dir := linkRepoWithRemote(t, name)
	runGitIn(t, dir, "checkout", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", ".")
	runGitIn(t, dir, "commit", "-q", "-m", "feat work")
	runGitIn(t, dir, "checkout", "-q", "main")
	return dir
}

func TestResolveLinkPreviewResolvesOnTheCheckoutWithBothBranches(t *testing.T) {
	t.Parallel()
	with := linkRepoWithBranch(t, "gigagit", "feat/x")
	without := linkRepoWithRemote(t, "gigagit") // main only
	state := filepath.Join(t.TempDir(), "repos.toml")
	// `without` is the MORE recently opened entry: only the both-branch filter
	// can keep the resolve off it.
	if err := repos.Touch(state, with, "gigagit", time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if err := repos.Touch(state, without, "gigagit", time.Unix(9000, 0)); err != nil {
		t.Fatal(err)
	}
	l, err := model.ParseLink("gg://gigagit/feat.txt@main...feat/x:2")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if !samePathLink(got.Checkout, with) {
		t.Fatalf("Checkout = %q, want the checkout holding both branches %q", got.Checkout, with)
	}
	if got.Preview == nil || !got.Preview.OK() {
		t.Fatal("Resolved.Preview must carry the resolved note set")
	}
	if got.Preview.Source != "feat/x" || got.Preview.Target != "main" {
		t.Errorf("Preview pair = %s...%s", got.Preview.Target, got.Preview.Source)
	}
	if got.Commit != got.Preview.Tip || got.Addr.Commit != got.Preview.Tip {
		t.Errorf("Commit/Addr.Commit = %q/%q, want the tip %q", got.Commit, got.Addr.Commit, got.Preview.Tip)
	}
	if got.Addr.State != model.StateCommitted || got.Addr.Path != "feat.txt" {
		t.Errorf("Addr = %+v", got.Addr)
	}
	if got.Line != 2 || got.Side != model.NoteSideNew {
		t.Errorf("line/side = %d/%s", got.Line, got.Side)
	}
}

// The cwd is NOT exempt from the both-branch filter for a preview link: a
// working-tree link short-circuits on the cwd, but a preview that the cwd
// cannot show must still resolve elsewhere.
func TestResolveLinkPreviewCwdIsNotExemptFromTheBranchFilter(t *testing.T) {
	t.Parallel()
	here := linkRepoWithRemote(t, "gigagit") // main only: cannot show the pair
	with := linkRepoWithBranch(t, "gigagit", "feat/x")
	state := filepath.Join(t.TempDir(), "repos.toml")
	if err := repos.Touch(state, with, "gigagit", time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	l, _ := model.ParseLink("gg://gigagit@main...feat/x")
	got, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state, Cwd: Open(here)})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if !samePathLink(got.Checkout, with) {
		t.Errorf("Checkout = %q, want %q — the cwd has no feat/x", got.Checkout, with)
	}
	if got.Addr.Path != "" {
		t.Errorf("Addr.Path = %q, want empty (a repo-level preview link)", got.Addr.Path)
	}
}

func TestResolveLinkPreviewCwdWinsTheTie(t *testing.T) {
	t.Parallel()
	here := linkRepoWithBranch(t, "gigagit", "feat/x")
	other := linkRepoWithBranch(t, "gigagit", "feat/x")
	state := filepath.Join(t.TempDir(), "repos.toml")
	_ = repos.Touch(state, other, "gigagit", time.Unix(9000, 0))
	l, _ := model.ParseLink("gg://gigagit@main...feat/x")
	got, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state, Cwd: Open(here)})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if !samePathLink(got.Checkout, here) {
		t.Errorf("Checkout = %q, want the cwd %q", got.Checkout, here)
	}
}

func TestResolveLinkPreviewWithNoCandidateIsUnknown(t *testing.T) {
	t.Parallel()
	here := linkRepoWithRemote(t, "gigagit")
	state := filepath.Join(t.TempDir(), "repos.toml")
	l, _ := model.ParseLink("gg://gigagit@main...feat/nope")
	_, err := ResolveLink(context.Background(), l, ResolveOpts{RegistryPath: state, Cwd: Open(here)})
	if !errors.Is(err, ErrLinkUnknownRepo) {
		t.Fatalf("err = %v, want ErrLinkUnknownRepo", err)
	}
}

// A programmatically built commit link with no sha is still refused; the guard
// must not fire for a preview link, which legitimately has none.
func TestResolveLinkStillRefusesACommitLinkWithNoSHA(t *testing.T) {
	t.Parallel()
	here := linkRepoWithRemote(t, "gigagit")
	l := model.Link{Repo: model.LinkRepo{Name: "gigagit"}, Path: "a.go",
		Target: model.LinkTarget{State: model.StateCommitted}}
	_, err := ResolveLink(context.Background(), l, ResolveOpts{Cwd: Open(here)})
	if !errors.Is(err, model.ErrLink) {
		t.Fatalf("err = %v, want an ErrLink refusal", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-links && go test ./internal/domain/ -run 'ResolveLinkPreview|ResolveLinkStillRefuses' -v`
Expected: FAIL — `got.Preview undefined (type Resolved has no field Preview)`.

- [ ] **Step 3: Implement the resolver changes**

In `internal/domain/linkresolve.go`, add the field to `Resolved`:

```go
type Resolved struct {
	Checkout string            // absolute top level of the chosen checkout
	Addr     model.FileAddress // Worktree = Checkout for the working-tree states
	Line     int
	Side     model.NoteSide
	Hunk     int
	Commit   string // the FULL sha when the link named a commit
	// Preview is the resolved merge-preview scope when the link named a pair
	// (gg://<repo>@<target>...<source>). Addr and Commit then point at the
	// preview's SOURCE TIP — the write target for a preview note (spec §1.1) —
	// and every consumer that would otherwise read a --preview argument reads
	// this instead. nil for every other link.
	Preview *PreviewNoteSet
}
```

In `ResolveLink`, scope the empty-sha guard and add the pair filter. Replace:

```go
	if l.Target.State == model.StateCommitted && l.Target.Commit == "" {
		return Resolved{}, fmt.Errorf("%w: gg link names a commit without a sha", model.ErrLink)
	}
	cands := linkCandidates(ctx, l, opts)
	if len(cands) == 0 {
		return Resolved{}, fmt.Errorf("%w: %s is not in this machine's gg history; open it once in gg", ErrLinkUnknownRepo, linkRepoLabel(l))
	}
```

with:

```go
	if l.Target.State == model.StateCommitted && l.Target.Commit == "" && l.Target.Preview == nil {
		return Resolved{}, fmt.Errorf("%w: gg link names a commit without a sha", model.ErrLink)
	}
	cands := linkCandidates(ctx, l, opts)
	if len(cands) == 0 {
		return Resolved{}, fmt.Errorf("%w: %s is not in this machine's gg history; open it once in gg", ErrLinkUnknownRepo, linkRepoLabel(l))
	}
	// A preview names two BRANCHES, so a checkout that lacks either cannot show
	// it — drop those before anything else, including the cwd. (A commit link's
	// containment filter below runs only when more than one candidate is left;
	// this one runs ALWAYS, because "the cwd has the repo but not the branch" is
	// the common case, not the tie-break case.) The cwd still sorts first, so it
	// wins any tie the filter leaves.
	if p := l.Target.Preview; p != nil {
		kept := previewCandidates(ctx, cands, p, opts)
		if len(kept) == 0 {
			return Resolved{}, fmt.Errorf("%w: no checkout of %s holds both %s and %s", ErrLinkUnknownRepo, linkRepoLabel(l), p.Target, p.Source)
		}
		cands = kept
	}
```

Scope the containment filter (it would call `ResolveRev("")` for a preview):

```go
	if len(cands) > 1 && l.Target.State == model.StateCommitted && l.Target.Preview == nil {
		if kept := containing(ctx, cands, l.Target.Commit, opts); len(kept) > 0 {
			cands = kept
		}
	}
```

Add `previewCandidates` beside `containing`:

```go
// previewCandidates keeps the checkouts that can actually SHOW the pair: both
// branch names must resolve there. ResolveRev is the same probe containing()
// uses for a commit link; a candidate whose probe errors is simply not a
// candidate, exactly as there.
func previewCandidates(ctx context.Context, cands []linkCandidate, p *model.LinkPreview, opts ResolveOpts) []linkCandidate {
	var kept []linkCandidate
	for _, c := range cands {
		// OpenFn is defaulted once at ResolveLink's entry and never nil.
		svc := opts.OpenFn(c.checkout)
		if _, found, err := svc.ResolveRev(ctx, p.Source); err != nil || !found {
			continue
		}
		if _, found, err := svc.ResolveRev(ctx, p.Target); err != nil || !found {
			continue
		}
		kept = append(kept, c)
	}
	return kept
}
```

In `finishLink`, add the preview arm as the first thing in the
`case model.StateCommitted:` branch:

```go
	switch l.Target.State {
	case model.StateCommitted:
		if p := l.Target.Preview; p != nil {
			// The tip is resolved HERE, on the chosen checkout, so every
			// consumer gets exactly the set `--preview <target>...<source>`
			// would build (ruling 3). PreviewNotes returns a ZERO set with a nil
			// error for a pair that is not previewable (merged, no common base):
			// that is not an error there, but it is one here — the caller asked
			// for this preview by name.
			set, perr := opts.OpenFn(c.checkout).PreviewNotes(ctx, p.Source, p.Target)
			if perr != nil {
				return Resolved{}, perr
			}
			if !set.OK() {
				return Resolved{}, fmt.Errorf("%w: %s does not show %s...%s (merged, or no common base)", ErrLinkUnknownRepo, c.checkout, p.Target, p.Source)
			}
			r.Preview = &set
			r.Commit, r.Addr.Commit = set.Tip, set.Tip
			return r, nil
		}
		// ResolveRev peels to ^{commit} and is also the >=7-hex → full-sha
		// expansion: no second verb exists for that. OpenFn is defaulted once
		// at ResolveLink's entry and never nil.
		full, found, err := opts.OpenFn(c.checkout).ResolveRev(ctx, l.Target.Commit)
		...
```

(The rest of the `StateCommitted` arm and the `default:` arm are unchanged.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-links && go test ./internal/domain/ -run 'ResolveLink' -v`
Expected: PASS, every pre-existing `TestResolveLink*` included.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-links && git add internal/domain/linkresolve.go internal/domain/linkresolve_test.go && git commit -m "feat(domain): ResolveLink resolves a preview link to its source tip

A preview link is filtered to the checkouts that hold BOTH branches (the
cwd included — it can hold the repo without holding the branch), then
PreviewNotes resolves the pair there. Resolved gains Preview, and Addr/
Commit point at the source tip: the write target for a preview note.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 3: The `preview` steer target on the wire and in the web endpoint

**Files:**
- Modify: `internal/steer/steer.go`
- Modify: `internal/web/steer.go`
- Test: `internal/web/steer_test.go`

**Interfaces:**
- Produces:
  - `steer.Target` gains `Source string \`json:"source,omitempty"\`` and
    `Target string \`json:"target,omitempty"\``; the documented state set becomes
    `"unstaged" | "staged" | "untracked" | "commit" | "preview"`. For
    `"preview"`, `Commit` stays EMPTY on the wire.
  - `web.steerWire` gains `Source`/`Target` with the same json names.
- Consumes: nothing from earlier tasks.

- [ ] **Step 1: Write the failing tests**

Append to `internal/web/steer_test.go`:

```go
func TestSteerWireAcceptsThePreviewTarget(t *testing.T) {
	t.Parallel()
	w, err := toSteerWire(steer.Command{
		Cmd:    "navigate",
		File:   "a.txt",
		Target: &steer.Target{State: "preview", Source: "feat/x", Target: "main"},
		Line:   &steer.Line{Side: "new", No: 4},
	})
	if err != nil {
		t.Fatalf("toSteerWire: %v", err)
	}
	if w.State != "preview" || w.Source != "feat/x" || w.Target != "main" {
		t.Errorf("wire = {state:%q source:%q target:%q}, want the preview triple", w.State, w.Source, w.Target)
	}
	if w.Commit != "" {
		t.Errorf("wire.commit = %q, want empty — the consumer resolves the tip itself", w.Commit)
	}
	if w.Side != "new" || w.Line != 4 {
		t.Errorf("wire line = %s:%d", w.Side, w.Line)
	}
}

// A preview navigate with NO file reveals the Previews entry; the "navigate
// needs a file, a commit or a step" rule must not reject it.
func TestSteerWireAcceptsAPreviewRevealWithNoFile(t *testing.T) {
	t.Parallel()
	if _, err := toSteerWire(steer.Command{
		Cmd:    "navigate",
		Target: &steer.Target{State: "preview", Source: "feat/x", Target: "main"},
	}); err != nil {
		t.Fatalf("toSteerWire: %v", err)
	}
}

func TestSteerWireRefusesABadPreviewTarget(t *testing.T) {
	t.Parallel()
	for _, tg := range []*steer.Target{
		{State: "preview", Source: "feat/x"},                 // no target half
		{State: "preview", Target: "main"},                   // no source half
		{State: "preview", Source: "--upload-pack=x", Target: "main"}, // argv injection
		{State: "preview", Source: "feat/x", Target: "--evil"},
	} {
		if _, err := toSteerWire(steer.Command{Cmd: "navigate", File: "a.txt", Target: tg}); err == nil {
			t.Errorf("toSteerWire(%+v) = nil error, want a refusal", *tg)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-links && go test ./internal/web/ -run 'SteerWire' -v`
Expected: FAIL — `unknown field Source in struct literal of type steer.Target`.

- [ ] **Step 3: Implement the wire change**

In `internal/steer/steer.go`, replace `Target`:

```go
// Target names where a file lives: the working tree (unstaged/untracked), the
// index (staged), one commit, or one MERGE PREVIEW. Values are protocol
// strings, always English.
type Target struct {
	State  string `json:"state,omitempty"`  // "unstaged" | "staged" | "untracked" | "commit" | "preview"
	Commit string `json:"commit,omitempty"` // full 40-hex sha when State == "commit"
	// Source and Target are the preview's branch NAMES, set iff State ==
	// "preview" (git's <target>...<source> order). Commit stays EMPTY for a
	// preview: which commit it shows is the CONSUMER's question — it resolves
	// the tip itself, so a tip that moved between post and apply is honoured.
	Source string `json:"source,omitempty"`
	Target string `json:"target,omitempty"`
}
```

In `internal/web/steer.go`, add the two fields to `steerWire` (right after
`Commit`):

```go
	Source  string   `json:"source,omitempty"`
	Target  string   `json:"target,omitempty"`
```

In `toSteerWire`, replace the whole `if c.Target != nil { … }` block:

```go
	if c.Target != nil {
		if c.Target.State == "preview" {
			// A preview target is NOT a note state: it names a branch pair, and
			// the page resolves the tip itself. Handled before noteState, whose
			// allowlist has no entry for it.
			if c.Target.Source == "" || c.Target.Target == "" {
				return w, errors.New("a preview target needs source and target")
			}
			if !isGitArgSafe(c.Target.Source) || !isGitArgSafe(c.Target.Target) {
				return w, errors.New("unsafe preview branch")
			}
			w.State, w.Source, w.Target = "preview", c.Target.Source, c.Target.Target
		} else {
			if _, ok := noteState(c.Target.State); !ok {
				return w, fmt.Errorf("unknown state %q", c.Target.State)
			}
			if c.Target.Commit != "" && !isGitArgSafe(c.Target.Commit) {
				return w, errors.New("unsafe target commit")
			}
			w.State = c.Target.State
			if w.State == "" {
				w.State = "unstaged"
			}
			if c.Target.Commit != "" {
				w.Commit = c.Target.Commit
			}
		}
	}
```

In the `case "navigate":` arm, relax the "needs a file" rule for a preview
reveal:

```go
		// A preview with no file is a REVEAL of the Previews entry — the one
		// navigate shape that names a place without naming a file or a commit.
		if c.File == "" && c.Commit == "" && c.Step == "" && w.State != "preview" {
			return w, errors.New("navigate needs a file, a commit or a step")
		}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-links && go test ./internal/steer/... ./internal/web/ -run 'Steer' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-links && git add internal/steer/steer.go internal/web/steer.go internal/web/steer_test.go && git commit -m "feat(steer): target.state \"preview\" carries the branch pair

steer.Target gains source/target for a merge-preview landing; commit stays
empty on the wire so the consumer resolves the tip itself. The web endpoint
validates the pair with isGitArgSafe before noteState (which has no entry
for it) and lets a file-less preview navigate through as a reveal.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 4: CLI — one adapter, and every link consumer behaves as `--preview`

**Files:**
- Modify: `internal/cli/previewflag.go` (the adapter)
- Modify: `internal/cli/linkconsume.go` (`linkDiffSpec`)
- Modify: `internal/cli/show.go` (refusal)
- Modify: `internal/cli/note.go` (`noteAdd`, `noteList`)
- Modify: `internal/cli/noteapply.go` (`noteApply`)
- Test: `internal/cli/link_preview_test.go` (new)

**Interfaces:**
- Consumes: `domain.Resolved.Preview *domain.PreviewNoteSet` (Task 2);
  `previewTarget{Source, Target string; Set domain.PreviewNoteSet; Spec
  model.DiffSpec}` and `(previewTarget).withPaths` (existing);
  `(*domain.Service).PreviewHunkAnchor(ctx, set, path, n) (model.NoteSide,
  [2]int, error)` and `domain.ErrPreviewOldSide` (existing).
- Produces:
  - `func previewTargetFromLink(res domain.Resolved) (previewTarget, bool)` —
    the ONE adapter; every consumer funnels through it.
  - `linkDiffSpec` returns the PREVIEW's patch for a preview link.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/link_preview_test.go`:

```go
package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// previewLinkFor builds the LOCAL-form preview link for dir. The local form is
// deliberate: resolveLinkArg reads the package global RepoStatePath, which is
// "" in tests (an empty registry), so the cwd is the only candidate.
func previewLinkFor(dir, path string) string {
	l := "gg://" + filepath.ToSlash(dir)
	if path != "" {
		l += "/" + path
	}
	return l + "@main...feat/x"
}

// A table over every §2.3 consumer: the preview link and --preview must
// produce exactly the same bytes / the same stored note (ruling 5).
func TestPreviewLinkMatchesThePreviewFlag(t *testing.T) {
	dir := previewRepo(t)
	fileLink := previewLinkFor(dir, "a.txt")
	cases := []struct {
		name       string
		flagArgs   []string
		linkArgs   []string
	}{
		{"diff", []string{"diff", "--preview", "main...feat/x"}, []string{"diff", fileLink}},
		{"diff --hunks --json", []string{"diff", "--preview", "main...feat/x", "--hunks", "--json"}, []string{"diff", fileLink, "--hunks", "--json"}},
		{"note list", []string{"note", "list", "--preview", "main...feat/x", "--file", "a.txt"}, []string{"note", "list", fileLink}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			codeF, outF, errF := runCLI(t, dir, c.flagArgs...)
			codeL, outL, errL := runCLI(t, dir, c.linkArgs...)
			if codeF != 0 || codeL != 0 {
				t.Fatalf("exit codes = %d (flag: %s) / %d (link: %s)", codeF, errF, codeL, errL)
			}
			if outF != outL {
				t.Errorf("--preview and the link disagree:\nflag: %q\nlink: %q", outF, outL)
			}
		})
	}
}

// note add through a preview link stores on the tip, numbered over the
// PREVIEW's patch — exactly as --preview --hunk N does.
func TestNoteAddThroughAPreviewLinkStoresOnTheTip(t *testing.T) {
	dir := previewRepo(t)
	if code, _, errb := runCLI(t, dir, "note", "add", previewLinkFor(dir, "a.txt")+"#1", "--summary", "linked preview note"); code != 0 {
		t.Fatalf("note add: %d %s", code, errb)
	}
	// The same note must be visible on the source tip's own commit view.
	tip := strings.TrimSpace(gitOut(t, dir, "rev-parse", "feat/x"))
	_, out, _ := runCLI(t, dir, "note", "list", "--rev", tip, "--file", "a.txt")
	if !strings.Contains(out, "linked preview note") {
		t.Fatalf("the note is not on the tip %s: %q", tip, out)
	}
}

// A preview link and --preview together is a usage error, never a silent
// override (ruling 5).
func TestPreviewLinkPlusPreviewFlagIsAUsageError(t *testing.T) {
	dir := previewRepo(t)
	for _, args := range [][]string{
		{"note", "add", previewLinkFor(dir, "a.txt") + ":1", "--preview", "main...feat/x", "--summary", "no"},
		{"note", "list", previewLinkFor(dir, "a.txt"), "--preview", "main...feat/x"},
	} {
		if code, _, errb := runCLI(t, dir, args...); code != 2 {
			t.Errorf("%v: exit = %d, want 2 (%s)", args, code, errb)
		}
	}
}

func TestShowRefusesAPreviewLink(t *testing.T) {
	dir := previewRepo(t)
	code, _, errb := runCLI(t, dir, "show", previewLinkFor(dir, "a.txt"))
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errb, "gg diff") {
		t.Errorf("stderr = %q, want it to point at gg diff", errb)
	}
}

// note apply through a preview link imports onto the tip, new side only.
func TestNoteApplyThroughAPreviewLinkImportsOntoTheTip(t *testing.T) {
	dir := previewRepo(t)
	// The agent-context v1 shape notebatch.Parse accepts (see
	// internal/notebatch/notebatch.go's doc comment and noteapply_test.go).
	batch := `{"files":[{"path":"a.txt","annotations":[{"newRange":[1,1],"summary":"batched"}]}]}`
	code, _, errb := runCLIStdin(t, dir, batch, "note", "apply", previewLinkFor(dir, ""), "--stdin")
	if code != 0 {
		t.Fatalf("note apply: %d %s", code, errb)
	}
	tip := strings.TrimSpace(gitOut(t, dir, "rev-parse", "feat/x"))
	_, out, _ := runCLI(t, dir, "note", "list", "--rev", tip, "--file", "a.txt")
	if !strings.Contains(out, "batched") {
		t.Fatalf("the batch did not land on the tip: %q", out)
	}
}

// note clear with a preview FILE link clears the tip's notes for that path;
// a BARE preview link is refused by noteLinkShape exactly as a bare commit
// link is (a repository link cannot carry a target here).
func TestNoteClearWithPreviewLinks(t *testing.T) {
	dir := previewRepo(t)
	if code, _, errb := runCLI(t, dir, "note", "add", previewLinkFor(dir, "a.txt")+":1", "--summary", "to clear"); code != 0 {
		t.Fatalf("note add: %d %s", code, errb)
	}
	// --yes is mandatory (note.go:753); a FILE link stands in for --file, and
	// noteClear then clears link.Addr — the tip + path (note.go:734-762).
	if code, _, errb := runCLI(t, dir, "note", "clear", previewLinkFor(dir, "a.txt"), "--yes"); code != 0 {
		t.Fatalf("note clear: %d %s", code, errb)
	}
	tip := strings.TrimSpace(gitOut(t, dir, "rev-parse", "feat/x"))
	_, out, _ := runCLI(t, dir, "note", "list", "--rev", tip, "--file", "a.txt")
	if strings.Contains(out, "to clear") {
		t.Fatalf("the note survived the clear: %q", out)
	}
	if code, _, _ := runCLI(t, dir, "note", "clear", previewLinkFor(dir, ""), "--all", "--yes"); code != 2 {
		t.Error("a BARE preview link carries a target: note clear must refuse it (exit 2)")
	}
}

// gg link resolve prints the pair for a preview link, in both forms.
func TestLinkResolvePrintsThePreviewPair(t *testing.T) {
	dir := previewRepo(t)
	_, out, errb := runCLI(t, dir, "link", "resolve", previewLinkFor(dir, "a.txt")+":2")
	if !strings.Contains(out, "preview main...feat/x") || !strings.Contains(out, "a.txt") {
		t.Fatalf("resolve = %q (%s)", out, errb)
	}
	_, jsonOut, _ := runCLI(t, dir, "link", "resolve", "--json", previewLinkFor(dir, "a.txt")+":2")
	var w struct {
		State  string `json:"state"`
		Source string `json:"source"`
		Target string `json:"target"`
		Commit string `json:"commit"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &w); err != nil {
		t.Fatalf("json: %v (%q)", err, jsonOut)
	}
	if w.Source != "feat/x" || w.Target != "main" {
		t.Errorf("json pair = %s...%s", w.Target, w.Source)
	}
	if w.Commit == "" || w.Path != "a.txt" {
		t.Errorf("json = %+v, want the tip sha and the path", w)
	}
}
```

Both helpers already exist in the package and need no new code:
`runCLIStdin(t, workdir, in string, args ...string) (int, string, string)` in
`internal/cli/batch_test.go:13`, and `gitOut(t, dir string, args ...string)
string` in `internal/cli/link_test.go:412`. `previewRepo` is
`internal/cli/preview_test.go:11` (it uses `t.Setenv`, so none of these tests
call `t.Parallel()`), and it builds `main` plus `feat/x` with `a.txt` added only
on `feat/x`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-links && go test ./internal/cli/ -run 'PreviewLink|ShowRefusesAPreview|NoteAddThroughAPreview|NoteApplyThroughAPreview|NoteClearWithPreview|LinkResolvePrintsThePreview' -v`
Expected: FAIL — `gg diff <preview link>` renders the tip's own parent→tip diff
instead of the preview's patch; `show` exits 1 instead of 2; `link resolve`
prints no pair.

- [ ] **Step 3: Implement the adapter and wire it into every consumer**

**`internal/cli/previewflag.go`** — add the adapter at the end:

```go
// previewTargetFromLink adapts a RESOLVED preview link onto the very value
// --preview produces, so every consumer below runs ONE code path (ruling 5).
// The set was already resolved by domain.ResolveLink on the link's own
// checkout; re-resolving here would cost two more rev-parse calls and could
// disagree with the address the resolver already handed out.
func previewTargetFromLink(res domain.Resolved) (previewTarget, bool) {
	if res.Preview == nil {
		return previewTarget{}, false
	}
	set := *res.Preview
	return previewTarget{Source: set.Source, Target: set.Target, Set: set, Spec: set.DiffSpec()}, true
}
```

**`internal/cli/linkconsume.go`** — branch `linkDiffSpec`:

```go
func linkDiffSpec(ctx context.Context, svc *domain.Service, res domain.Resolved) (model.DiffSpec, error) {
	var paths []string
	if res.Addr.Path != "" {
		paths = []string{res.Addr.Path}
	}
	// A PREVIEW link's patch is merge-base → source tip, never the tip commit's
	// own parent→tip change: `gg diff <preview link> --hunks` and
	// `gg note add <preview link>#N` must number the same hunks as
	// `gg diff --preview … --hunks` (ruling 1 of feature A, ruling 5 here).
	if tgt, ok := previewTargetFromLink(res); ok {
		return tgt.withPaths(paths), nil
	}
	switch res.Addr.State {
	...
```

**`internal/cli/show.go`** — in the `isLinkArg(rev)` block, BEFORE the
`res.Addr.Commit == ""` check (a preview's `Addr.Commit` is the tip, so that
check would pass and show the tip commit instead):

```go
		if res.Preview != nil {
			fmt.Fprintln(stderr, "show: that link names a merge preview, not a commit; use gg diff <link>")
			return 2
		}
```

**`internal/cli/note.go`, `noteAdd`** — inside `if link != nil { … }`, replace
the `case link.Hunk > 0:` arm:

```go
		addr = link.Addr
		pv, isPreview := previewTargetFromLink(*link)
		switch {
		case link.Hunk > 0 && isPreview:
			// PreviewHunkAnchor is the ONE place the preview's hunk numbering
			// and the old-side refusal live, shared with --preview and MCP.
			s, r, herr := svc.PreviewHunkAnchor(ctx, pv.Set, addr.Path, link.Hunk)
			if errors.Is(herr, domain.ErrPreviewOldSide) {
				fmt.Fprintln(stderr, "note add:", herr)
				return 2
			}
			if herr != nil {
				return noteExit(herr, stderr)
			}
			side, rng = s, r
		case link.Hunk > 0:
			// linkDiffSpec is the ONE place that maps a link's target onto a
			// diff spec, so `gg note add <link>#N` cannot drift from
			// `gg diff <link> --hunks`'s numbering.
			spec, err := linkDiffSpec(ctx, svc, *link)
			if err != nil {
				return noteExit(err, stderr)
			}
			s, r, err := svc.HunkRange(ctx, spec, addr.Path, link.Hunk)
			if err != nil {
				return noteExit(err, stderr)
			}
			side, rng = s, r
		case link.Line > 0:
			side, rng = link.Side, [2]int{link.Line, link.Line}
		default:
			fmt.Fprintln(stderr, "note add: the link names a file but no anchor; add :<line> or #<hunk>")
			return 2
		}
```

**`internal/cli/note.go`, `noteList`** — in the `if link != nil { … }` arm,
replace the `svc.NotesAt(ctx, link.Addr)` call:

```go
		// A link's line and hunk are ignored: `note list` is about a FILE's
		// threads, exactly as `--file` is.
		if pv, ok := previewTargetFromLink(*link); ok {
			// A preview GATHERS notes along the branch and reports stale as
			// "outdated" — the same rows --preview prints, not the tip's own.
			got, gerr := previewResolvedNotes(ctx, svc, pv.Set, link.Addr.Path)
			if gerr != nil {
				return noteExit(gerr, stderr)
			}
			res, previewWords = got, true
		} else {
			got, err := svc.NotesAt(ctx, link.Addr)
			if err != nil {
				return noteExit(err, stderr)
			}
			res = got
		}
```

**`internal/cli/noteapply.go`, `noteApply`** — in the `if link != nil { … }`
arm, replace `cached, rev = link.Addr.State == model.StateStaged, link.Addr.Commit`:

```go
		cached, rev = link.Addr.State == model.StateStaged, link.Addr.Commit
```

and then, after the `ctx := context.Background()` /
`target := domain.NoteBatchTarget{Cached: cached, Rev: rev}` /
`rule := domain.NoteSideBoth` lines, add a preview arm BEFORE the `if pf.set()`
one, and widen the warning:

```go
	ctx := context.Background()
	target := domain.NoteBatchTarget{Cached: cached, Rev: rev}
	rule := domain.NoteSideBoth
	previewed := false
	if link != nil {
		if pv, ok := previewTargetFromLink(*link); ok {
			// The same three facts --preview sets: stored on the tip, hunk
			// numbers from the PREVIEW's patch, new side only.
			spec := pv.Spec
			target = domain.NoteBatchTarget{Rev: pv.Set.Tip, Hunks: &spec}
			rule = domain.NoteSideNewOnly
			previewed = true
		}
	}
	if pf.set() {
		if cached || rev != "" {
			return previewUsageErr("note apply", stderr)
		}
		tgt, terr := resolvePreviewTarget(ctx, svc, *pf.spec)
		if terr != nil {
			fmt.Fprintln(stderr, "error:", terr)
			return 1
		}
		spec := tgt.Spec
		// Stored on the tip, numbered over the preview's own patch, new side
		// only — old-side items are SKIPPED with one warning (the --working rule).
		target = domain.NoteBatchTarget{Rev: tgt.Set.Tip, Hunks: &spec}
		rule = domain.NoteSideNewOnly
		previewed = true
	}
```

and change the warning call to `warnSkippedOldSide(stderr, skipped, previewed)`.

**`internal/cli/link.go`, `linkResolve`** — print the pair. Add the two fields
to `wireResolvedLink` (after `Commit`):

```go
	Source   string `json:"source,omitempty"`
	Target   string `json:"target,omitempty"`
```

In the `*asJSON` branch, after building `w`:

```go
		if res.Preview != nil {
			w.Source, w.Target = res.Preview.Source, res.Preview.Target
		}
```

In the text branch, replace the `line := res.Addr.State.String()` prelude:

```go
	line := res.Addr.State.String()
	if res.Preview != nil {
		// A preview's state word alone ("committed") would say nothing about
		// WHICH commit or why: name the pair, then the tip it resolved to.
		line = "preview " + res.Preview.Target + "..." + res.Preview.Source
	}
	if res.Addr.Commit != "" {
		line += " " + res.Addr.Commit
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-links && go test ./internal/cli/ -v 2>&1 | tail -40`
Expected: PASS, including every pre-existing `link`/`note`/`preview` test.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-links && git add internal/cli/ && git commit -m "feat(cli): every link consumer treats a preview link as --preview

One adapter (previewTargetFromLink) turns a resolved preview link into the
value --preview already produces, so gg diff / note add|list|apply|clear
emit the same bytes and store the same notes either way. linkDiffSpec
returns the preview's own patch, so hunk numbers agree; gg show refuses a
preview link and points at gg diff; gg link resolve prints the pair.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 5: `gg link --preview` and steering a preview link from the CLI

**Files:**
- Modify: `internal/cli/link.go` (`runLink`, `buildLink`)
- Modify: `internal/cli/session.go` (extract `navigateCommandFor`, use it)
- Test: `internal/cli/link_preview_test.go` (append)
- Test: `internal/cli/session_preview_test.go` (new)

**Interfaces:**
- Consumes: `previewTargetFromLink` (Task 4);
  `resolvePreviewTarget(ctx, svc, spec) (previewTarget, error)`,
  `addPreviewFlag(fs) previewFlag`, `previewUsageErr(verb, stderr) int`
  (existing); `steer.Target.Source/.Target` (Task 3).
- Produces:
  - `func buildLink(ctx context.Context, svc *domain.Service, workdir, pathArg string, cached bool, rev string, prev *model.LinkPreview) (model.Link, error)` — one new trailing parameter.
  - `func navigateCommandFor(ctx context.Context, svc *domain.Service, res domain.Resolved) (steer.Command, error)` — THE link→navigate builder, shared by `gg session navigate` and (Task 8) `gg open`.
  - `var errNavLinkRepoOnly, errNavLinkNoLine error` — the two refusals the
    callers map to exit 2.

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/link_preview_test.go`:

```go
func TestLinkPreviewPrintsThePreviewForm(t *testing.T) {
	dir := previewRepo(t)
	_, out, errb := runCLI(t, dir, "link", "--preview", "main...feat/x")
	if got := strings.TrimSpace(out); !strings.HasSuffix(got, "@main...feat/x") {
		t.Fatalf("link --preview = %q (%s)", out, errb)
	}
	_, out, _ = runCLI(t, dir, "link", "--preview", "main...feat/x", "a.txt:4")
	if got := strings.TrimSpace(out); !strings.HasSuffix(got, "/a.txt@main...feat/x:4") {
		t.Fatalf("link --preview <path>:<line> = %q", out)
	}
	_, out, _ = runCLI(t, dir, "link", "--preview", "main...feat/x", "a.txt#2")
	if got := strings.TrimSpace(out); !strings.HasSuffix(got, "/a.txt@main...feat/x#2") {
		t.Fatalf("link --preview <path>#<hunk> = %q", out)
	}
	// Round-trip: what it prints must parse back to the same place.
	_, out, _ = runCLI(t, dir, "link", "--preview", "main...feat/x", "a.txt:4")
	if _, err := model.ParseLink(strings.TrimSpace(out)); err != nil {
		t.Fatalf("ParseLink(%q) = %v", out, err)
	}
}

func TestLinkPreviewRefusals(t *testing.T) {
	dir := previewRepo(t)
	// :old: has no meaning in a preview.
	if code, _, _ := runCLI(t, dir, "link", "--preview", "main...feat/x", "a.txt:old:4"); code != 2 {
		t.Error("link --preview with :old: must exit 2")
	}
	// One target only.
	for _, args := range [][]string{
		{"link", "--preview", "main...feat/x", "--cached"},
		{"link", "--preview", "main...feat/x", "--rev", "HEAD"},
	} {
		if code, _, _ := runCLI(t, dir, args...); code != 2 {
			t.Errorf("%v must exit 2", args)
		}
	}
	// An unknown pair is a resolution failure, not a usage one.
	if code, _, _ := runCLI(t, dir, "link", "--preview", "main...feat/nope"); code != 1 {
		t.Error("link --preview with a missing branch must exit 1")
	}
}
```

Create `internal/cli/session_preview_test.go`:

```go
package cli

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// A preview link posts a navigate carrying the PAIR, not a sha, and no commit.
func TestSessionNavigatePreviewLinkPostsThePair(t *testing.T) {
	dir := previewRepo(t)
	inbox := t.TempDir()
	livePresence(t, inbox)
	svc := domain.Open(dir)
	var out, errb strings.Builder
	code := runSession(inbox, svc, []string{"navigate", previewLinkFor(dir, "a.txt") + ":1", "--no-wait"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	got := steer.Drain(inbox)
	if len(got) != 1 {
		t.Fatalf("posted %d commands, want 1", len(got))
	}
	c := got[0]
	if c.Cmd != "navigate" || c.File != "a.txt" {
		t.Fatalf("command = %+v", c)
	}
	if c.Target == nil || c.Target.State != "preview" {
		t.Fatalf("target = %+v, want state preview", c.Target)
	}
	if c.Target.Source != "feat/x" || c.Target.Target != "main" {
		t.Errorf("pair = %s...%s", c.Target.Target, c.Target.Source)
	}
	if c.Target.Commit != "" {
		t.Errorf("target.commit = %q, want empty — the consumer resolves the tip", c.Target.Commit)
	}
	if c.Line == nil || c.Line.Side != "new" || c.Line.No != 1 {
		t.Errorf("line = %+v", c.Line)
	}
}

// A preview link with NO file reveals the Previews entry: no file, no line.
func TestSessionNavigatePreviewRepoLinkReveals(t *testing.T) {
	dir := previewRepo(t)
	inbox := t.TempDir()
	livePresence(t, inbox)
	var out, errb strings.Builder
	if code := runSession(inbox, domain.Open(dir), []string{"navigate", previewLinkFor(dir, ""), "--no-wait"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	got := steer.Drain(inbox)
	if len(got) != 1 || got[0].File != "" || got[0].Line != nil {
		t.Fatalf("command = %+v, want a file-less reveal", got)
	}
	if got[0].Target == nil || got[0].Target.State != "preview" {
		t.Fatalf("target = %+v", got[0].Target)
	}
}

// A #hunk preview link is lowered through PreviewHunkAnchor: the posted line is
// the FIRST line of the preview patch's hunk, on the new side.
func TestSessionNavigatePreviewHunkLowersThroughPreviewHunkAnchor(t *testing.T) {
	dir := previewRepo(t)
	inbox := t.TempDir()
	livePresence(t, inbox)
	var out, errb strings.Builder
	if code := runSession(inbox, domain.Open(dir), []string{"navigate", previewLinkFor(dir, "a.txt") + "#1", "--no-wait"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	got := steer.Drain(inbox)
	if len(got) != 1 || got[0].Line == nil {
		t.Fatalf("command = %+v", got)
	}
	if got[0].Line.Side != "new" || got[0].Line.No != 1 {
		t.Errorf("line = %+v, want new:1 (a.txt is wholly added on feat/x)", got[0].Line)
	}
}

// highlight add keys the band on the TIP commit (ruling 6): the wire target is
// the ordinary commit form, so no new mark wire is needed.
func TestSessionHighlightAddPreviewLinkKeysOnTheTip(t *testing.T) {
	dir := previewRepo(t)
	inbox := t.TempDir()
	livePresence(t, inbox)
	var out, errb strings.Builder
	if code := runSession(inbox, domain.Open(dir), []string{"highlight", "add", previewLinkFor(dir, "a.txt") + ":1", "--no-wait"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	got := steer.Drain(inbox)
	if len(got) != 1 || got[0].Target == nil {
		t.Fatalf("command = %+v", got)
	}
	if got[0].Target.State != "commit" || len(got[0].Target.Commit) != 40 {
		t.Errorf("target = %+v, want the tip commit", got[0].Target)
	}
}
```

(`internal/cli/link_preview_test.go` already imports `internal/model` from
Task 4.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-links && go test ./internal/cli/ -run 'LinkPreviewPrints|LinkPreviewRefusals|SessionNavigatePreview|SessionHighlightAddPreview' -v`
Expected: FAIL — `gg link` has no `--preview` flag; the navigate posts a
`commit` target with the tip sha instead of a `preview` one.

- [ ] **Step 3: Implement**

**`internal/cli/link.go`** — extend `linkUsage`:

```go
const linkUsage = "usage: gg link [<path>[:<line>]] [--cached | --rev <commit> | --preview <id|label|<target>...<source>>]\n" +
	"       gg link resolve <gg://…> [--json]\n" +
	"quote links that carry #<hunk> — an unquoted # starts a shell comment"
```

In `runLink`, add the flag and the preview arm:

```go
	cached := fs.Bool("cached", false, "address the staged diff (HEAD → index)")
	rev := fs.String("rev", "", "address a commit's own change (parent → commit)")
	pf := addPreviewFlag(fs)
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) > 1 {
		fmt.Fprintf(stderr, "link: unexpected argument %q\n%s\n", pos[1], linkUsage)
		return 2
	}
	if *cached && *rev != "" {
		fmt.Fprintf(stderr, "link: --cached and --rev are mutually exclusive\n%s\n", linkUsage)
		return 2
	}
	if pf.set() && (*cached || *rev != "") {
		return previewUsageErr("link", stderr)
	}
	arg := ""
	if len(pos) == 1 {
		arg = pos[0]
	}
	ctx := context.Background()
	var prev *model.LinkPreview
	if pf.set() {
		// Resolve the argument (id | label | <target>...<source>) to the pair's
		// NAMES, and refuse a pair that cannot be shown — a link nobody can open
		// is worse than no link.
		tgt, terr := resolvePreviewTarget(ctx, svc, *pf.spec)
		if terr != nil {
			fmt.Fprintln(stderr, "error:", terr)
			return 1
		}
		if !model.LinkRefOK(tgt.Source) || !model.LinkRefOK(tgt.Target) {
			fmt.Fprintf(stderr, "link: %s...%s cannot be expressed in a gg link (a branch name may not contain @, : or #)\n", tgt.Target, tgt.Source)
			return 1
		}
		prev = &model.LinkPreview{Source: tgt.Source, Target: tgt.Target}
	}
	l, err := buildLink(ctx, svc, workdir, arg, *cached, *rev, prev)
```

In `buildLink`, add the parameter and the target arm:

```go
func buildLink(ctx context.Context, svc *domain.Service, workdir, pathArg string, cached bool, rev string, prev *model.LinkPreview) (model.Link, error) {
```

and replace the target `switch`:

```go
	switch {
	case prev != nil:
		// The preview's old side is the merge base, which no stored address
		// names (spec §1.1): "old:" has nothing to point at.
		if l.Side == model.NoteSideOld {
			return model.Link{}, fmt.Errorf("%w: a merge preview addresses the new side only; drop \"old:\"", model.ErrLink)
		}
		l.Target = model.LinkTarget{State: model.StateCommitted, Preview: prev}
	case cached:
		l.Target = model.LinkTarget{State: model.StateStaged}
	case rev != "":
		...
```

Update the other `buildLink` call sites (grep `buildLink(` — `runLink` is the
only production one; test call sites take `nil`).

**`internal/cli/session.go`** — add the two sentinels and the shared builder
beside `targetOf`:

```go
// The two link-shape refusals a navigate can hit. They are CALLER mistakes
// (exit 2), unlike a git failure (exit 1), and both `gg session navigate` and
// `gg open` map them the same way.
var (
	errNavLinkRepoOnly = errors.New("that link names a repository, not a place in it")
	errNavLinkNoLine   = errors.New("that link names a file but no line; add :<line> or #<hunk>")
)

// navigateCommandFor builds the navigate command a RESOLVED link names. It is
// the single builder `gg session navigate <link>` and `gg open <link>` share,
// so the two can never post different commands for the same link. svc must be
// the service for the link's OWN checkout (openLinkTarget / linkSteerDir).
func navigateCommandFor(ctx context.Context, svc *domain.Service, res domain.Resolved) (steer.Command, error) {
	c := steer.Command{Cmd: "navigate"}
	if res.Preview != nil {
		// The PAIR rides the wire, never the tip: the consumer resolves the tip
		// itself, so a tip that moved between post and apply is honoured.
		c.Target = &steer.Target{State: "preview", Source: res.Preview.Source, Target: res.Preview.Target}
		if res.Addr.Path == "" {
			return c, nil // reveal the Previews entry
		}
		c.File = res.Addr.Path
		line := steer.Line{Side: string(res.Side), No: res.Line}
		if res.Hunk > 0 {
			// PreviewHunkAnchor, never resolveHunkLine: the numbering is the
			// PREVIEW's patch (merge-base → tip), and a delete-only hunk has no
			// new side to land on.
			side, rng, err := svc.PreviewHunkAnchor(ctx, *res.Preview, res.Addr.Path, res.Hunk)
			if err != nil {
				return steer.Command{}, err
			}
			line = steer.Line{Side: string(side), No: rng[0]}
		}
		if line.No < 1 {
			return steer.Command{}, errNavLinkNoLine
		}
		c.Line = &line
		return c, nil
	}
	if res.Addr.Path == "" {
		// A link with no path reveals the commit (spec §1).
		if res.Commit == "" {
			return steer.Command{}, errNavLinkRepoOnly
		}
		c.Commit = res.Commit
		return c, nil
	}
	c.File, c.Target = res.Addr.Path, targetOf(res.Addr)
	line := steer.Line{Side: string(res.Side), No: res.Line}
	if res.Hunk > 0 {
		// No StateUntracked guard here: the grammar has no untracked target, so
		// ParseLink (the only source of a Resolved) never produces one — an
		// untracked file's link is the plain working-tree form, whose
		// index→file diff has hunks.
		l, err := resolveHunkLine(ctx, svc, res.Addr.State == model.StateStaged, res.Addr.Commit, res.Addr.Path, res.Hunk)
		if err != nil {
			return steer.Command{}, err
		}
		line = l
	}
	if line.No < 1 {
		return steer.Command{}, errNavLinkNoLine
	}
	c.Line = &line
	return c, nil
}

// navExit maps navigateCommandFor's error onto an exit code: 2 for the two
// link-shape refusals, 1 for everything else (a git failure).
func navExit(verb string, err error, stderr io.Writer) int {
	if errors.Is(err, errNavLinkRepoOnly) || errors.Is(err, errNavLinkNoLine) {
		fmt.Fprintf(stderr, "%s: %v\n", verb, err)
		return 2
	}
	fmt.Fprintln(stderr, "error:", err)
	return 1
}
```

Replace the body of `sessionNavigate`'s link arm (everything from
`c := steer.Command{Cmd: "navigate"}` to the `return sendSteer(...)`) with:

```go
			c, err := navigateCommandFor(ctx, target, res)
			if err != nil {
				return navExit("session navigate", err, stderr)
			}
			return sendSteer(dir, c, *noWait, stdout, stderr)
```

(`linkSteerDir` still runs just above it and supplies `dir, target`.)

`sessionHighlightAdd` needs NO change: `linkDiffSpec` (Task 4) already returns
the preview's patch for its `#hunk` arm, and `targetOf(res.Addr)` yields the
tip commit — ruling 6.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-links && go test ./internal/cli/ -v 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-links && git add internal/cli/ && git commit -m "feat(cli): gg link --preview, and one navigate builder for preview links

gg link --preview P [<path>[:<line>|#<hunk>]] prints the preview form and
refuses :old:. sessionNavigate's link arm moves into navigateCommandFor —
the single builder gg open will share — which posts the branch PAIR (never
a sha) and lowers a preview #hunk through PreviewHunkAnchor.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 6: TUI producers — `L`, the Previews `.`-menu row, and `cursor.link`

**Files:**
- Modify: `internal/tui/link.go`
- Test: `internal/tui/link_preview_test.go` (new)

**Interfaces:**
- Consumes: `model.LinkPreview`, `model.LinkRefOK` (Task 1);
  `(Model).previewNoteSet() *domain.PreviewNoteSet`,
  `Model.filesPreviewSet *domain.PreviewNoteSet`,
  `(Model).selectedPreview() (previewRow, bool)`,
  `(Model).noteAnchorAtCursor() (model.NoteSide, int, string, bool)` (existing).
- Produces:
  - `func (m Model) linkRepoFor(worktree string) (model.LinkRepo, bool)` —
    extracted from `linkFor`, shared by both producers.
  - `func (m Model) previewLinkFor(source, target, path string, line int) (string, bool)`
  - `contextLinkText` gains three preview branches (preview diff, preview file
    tree, Previews panel row). `contextLinkRow` and the session snapshot's
    `cursor.link` follow for free — both already read `contextLinkText`, and the
    Previews panel's `.` menu already splices `contextLinkRow` in via
    `action_menu.go:162-165`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/link_preview_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func TestPreviewLinkForBuildsTheThreeDotForm(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.linkRepoName = "gigagit"
	got, ok := m.previewLinkFor("feat/x", "main", "", 0)
	if !ok || got != "gg://gigagit@main...feat/x" {
		t.Fatalf("previewLinkFor(repo) = %q,%v", got, ok)
	}
	got, ok = m.previewLinkFor("feat/x", "main", "a.txt", 12)
	if !ok || got != "gg://gigagit/a.txt@main...feat/x:12" {
		t.Fatalf("previewLinkFor(file:line) = %q,%v", got, ok)
	}
	if _, err := model.ParseLink(got); err != nil {
		t.Fatalf("ParseLink(%q) = %v", got, err)
	}
	// A name the grammar cannot hold is refused, never emitted.
	if bad, ok := m.previewLinkFor("feat@x", "main", "", 0); ok {
		t.Errorf("previewLinkFor with '@' in a branch = %q, want a refusal", bad)
	}
	// A path the grammar cannot hold is refused too.
	if bad, ok := m.previewLinkFor("feat/x", "main", "we@ird.go", 0); ok {
		t.Errorf("previewLinkFor with '@' in the path = %q, want a refusal", bad)
	}
}

// The Previews panel row's . menu copies the PREVIEW link.
func TestPreviewsPanelRowCopiesThePreviewLink(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	m.linkRepoName = "gigagit"
	m = m.activateTab(panelPreviews)
	want := "gg://gigagit@main...feat/x"
	got, ok := m.contextLinkText()
	if !ok {
		t.Fatal("the Previews panel must offer a link")
	}
	if got != want {
		t.Errorf("contextLinkText = %q, want %q", got, want)
	}
	row, ok := m.contextLinkRow()
	if !ok {
		t.Fatal("no copy-link row on the Previews panel")
	}
	if row.copyText != want {
		t.Errorf("the . menu would copy %q, want %q", row.copyText, want)
	}
}

// Inside an open preview: the file tree gives the file form, the diff view the
// file:line form, and the session snapshot's cursor.link is the same string.
func TestPreviewDiffCopiesTheFileLineForm(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	m.linkRepoName = "gigagit"
	m = openMergePreview(t, m)
	if m.filesPreviewSet == nil {
		t.Fatal("the preview's note set must be armed on the files view")
	}
	// The file tree (no diff open yet): the FILE form, no line.
	got, ok := m.contextLinkText()
	if !ok || got != "gg://gigagit/a.txt@main...feat/x" {
		t.Fatalf("file-tree link = %q,%v", got, ok)
	}
	// Open the file's diff and land the cursor, then copy.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	m = drainMsgs(t, m, cmd, 4)
	v := m.diffLayer()
	if v == nil || v.previewSet == nil {
		t.Fatal("the preview diff must carry previewSet")
	}
	got, ok = m.contextLinkText()
	if !ok {
		t.Fatal("the preview diff must offer a link")
	}
	if !strings.HasPrefix(got, "gg://gigagit/a.txt@main...feat/x") {
		t.Fatalf("diff link = %q, want the preview form", got)
	}
	if _, err := model.ParseLink(got); err != nil {
		t.Fatalf("ParseLink(%q) = %v", got, err)
	}
	// cursor.link in the session snapshot is the SAME string: both read
	// contextLinkText.
	snap := buildSessionSnapshot(m)
	if snap.Cursor.Link != got {
		t.Errorf("cursor.link = %q, want %q", snap.Cursor.Link, got)
	}
}

// A preview whose branch names the grammar cannot hold produces no link at
// all, rather than one that reparses as a different place.
func TestPreviewLinkRefusedForAnUnexpressibleBranch(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.linkRepoName = "gigagit"
	set := domain.PreviewNoteSet{Source: "feat:x", Target: "main", Tip: strings.Repeat("a", 40)}
	m.filesPreviewSet = &set
	if got, ok := m.previewLinkFor(set.Source, set.Target, "a.txt", 0); ok {
		t.Errorf("previewLinkFor = %q, want a refusal", got)
	}
}
```

`buildSessionSnapshot(m Model) sessionSnapshot` is the snapshot builder
(`internal/tui/session_snapshot.go:182`); it fills `s.Cursor.Link` from
`contextLinkText` at line ~279.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-links && go test ./internal/tui/ -run 'PreviewLink|PreviewsPanelRowCopies|PreviewDiffCopies' -v`
Expected: FAIL — `m.previewLinkFor undefined`; the Previews panel yields no
link; the preview diff yields the tip's COMMIT form.

- [ ] **Step 3: Implement**

In `internal/tui/link.go`, extract the repo half out of `linkFor`:

```go
// linkRepoFor builds the link's repository half: the remote NAME when this
// checkout has one, else the local absolute-path form. It refuses a checkout
// path the grammar cannot hold (the first '@' is the target separator and the
// first '#' the hunk one), exactly as the CLI and web producers do.
func (m Model) linkRepoFor(worktree string) (model.LinkRepo, bool) {
	if m.linkRepoName != "" {
		return model.LinkRepo{Name: m.linkRepoName}, true
	}
	wt := worktree
	if wt == "" {
		wt = m.currentWorktree
	}
	if wt == "" {
		return model.LinkRepo{}, false
	}
	abs := filepath.ToSlash(filepath.Clean(wt))
	if !model.LinkAbsOK(abs) {
		return model.LinkRepo{}, false
	}
	return model.LinkRepo{Abs: abs}, true
}
```

and use it in `linkFor` (replacing the inline `if m.linkRepoName != "" { … } else { … }` block):

```go
	repo, ok := m.linkRepoFor(addr.Worktree)
	if !ok {
		return "", false
	}
	var l model.Link
	l.Repo = repo
```

Add the preview producer:

```go
// previewLinkFor builds the gg:// address of a place in a MERGE PREVIEW:
// the pair itself (path ""), one of its files, or a new-side line in one.
// The preview's old side is the merge base, which no address names, so there
// is no old-side form and no side parameter — every preview link is new-side.
//
// It refuses, rather than emitting something that reparses as a different
// place, when a branch NAME or the path is not expressible in the grammar.
func (m Model) previewLinkFor(source, target, path string, line int) (string, bool) {
	if !model.LinkRefOK(source) || !model.LinkRefOK(target) {
		return "", false
	}
	if path != "" && !model.LinkPathOK(path) {
		return "", false
	}
	if path == "" && line > 0 {
		return "", false
	}
	repo, ok := m.linkRepoFor("")
	if !ok {
		return "", false
	}
	l := model.Link{
		Repo: repo,
		Path: path,
		Target: model.LinkTarget{
			State:   model.StateCommitted,
			Preview: &model.LinkPreview{Source: source, Target: target},
		},
		Side: model.NoteSideNew,
		Line: line,
	}
	return l.String(), true
}
```

Rewrite `contextLinkText`'s body (the doc comment gains the three preview
notes):

```go
//  2b. a preview's diff → the PREVIEW form with the cursor line: the diff's
//      note address is a commit on the tip, but the place the user is looking
//      at is the preview, and that is the address that travels.
//  3a. a preview's FILE TREE (no diff on top) → the preview's file form. This
//      must be tested BEFORE focusedBookmark, which would otherwise hand back
//      the tip commit's file link for the same row.
//  3c. the Previews panel row → the pair's own link.
func (m Model) contextLinkText() (string, bool) {
	switch m.topLayer().(type) {
	case *historyView, *blameView:
		if b, ok := m.focusedBookmark(); ok {
			return m.linkFor(b.Address(), model.NoteSideNew, 0, 0)
		}
		return "", false
	}
	if _, ok := m.topLayer().(*diffView); ok {
		if addr, ok := m.diffNoteAddress(); ok {
			side, line, _, has := m.noteAnchorAtCursor()
			if !has {
				side, line = model.NoteSideNew, 0
			}
			// A preview diff's rows are note-addressable at the tip, but the
			// PLACE is the preview. noteAnchorsAtCursor already refuses the old
			// side inside a preview, so `has == false` on a deletion row is
			// exactly the "line 0, new side" the spec asks for.
			if set := m.previewNoteSet(); set != nil {
				return m.previewLinkFor(set.Source, set.Target, addr.Path, line)
			}
			return m.linkFor(addr, side, line, 0)
		}
		return "", false
	}
	// A preview's file list: the row is a file IN THE PREVIEW, not a file of
	// the tip commit — checked before focusedBookmark, which answers with the
	// tip's commit address for the very same row. It only INTERCEPTS: when
	// there is no focused row (the preview occupies the left column but the
	// focus is on Commits) the flow falls through to the branches below
	// exactly as it does today.
	if set := m.filesPreviewSet; set != nil && m.filesView != nil {
		if b, ok := m.focusedBookmark(); ok {
			return m.previewLinkFor(set.Source, set.Target, b.Path, 0)
		}
	}
	if b, ok := m.focusedBookmark(); ok {
		return m.linkFor(b.Address(), model.NoteSideNew, 0, 0)
	}
	if !m.inContentWindow() && m.focus == panelPreviews {
		if r, ok := m.selectedPreview(); ok {
			return m.previewLinkFor(r.rec.Source, r.rec.Target, "", 0)
		}
	}
	if !m.inContentWindow() && m.focus == panelCommits {
		if bi, ok := m.backingIndex(panelCommits); ok && bi < len(m.commits) {
			return m.linkFor(model.FileAddress{State: model.StateCommitted, Commit: m.commits[bi].Hash}, "", 0, 0)
		}
	}
	return "", false
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-links && go test ./internal/tui/ -v 2>&1 | tail -30`
Expected: PASS — including the i18n AST-gate tests (no new strings were added)
and every pre-existing `link_test.go` case.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-links && git add internal/tui/link.go internal/tui/link_preview_test.go && git commit -m "feat(tui): L and the . menu copy the PREVIEW link inside a preview

The Previews panel row, a preview's file tree and a preview's diff all now
produce the three-dot form; the session snapshot's cursor.link follows,
since it reads the same contextLinkText. linkFor's repository half is
extracted into linkRepoFor so both producers share the expressibility rule.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 7: TUI consumer — navigate into a preview

**Files:**
- Modify: `internal/tui/steer.go` (`steerEnumRefusal`)
- Modify: `internal/tui/steer_nav.go` (new stage, new arm, the drain)
- Modify: `internal/tui/preview_open.go` (answer a parked navigate on failure)
- Modify: `internal/tui/model.go` (the `compareFilesMsg` handler drains)
- Test: `internal/tui/steer_preview_test.go` (new)

**Interfaces:**
- Consumes: `steer.Target.Source/.Target` (Task 3);
  `(Model).openPreviewCmd(id, source, target, keepPath string) tea.Cmd`,
  `previewOpenState{id, source, target, …}`, `(Model).steerToPanels()`,
  `(Model).failPending(reason string)`, `steerStageDiff`,
  `(Model).openDiffForFileLine`, `(Model).answerSteer` (existing).
- Produces:
  - `const steerStagePreview steerStage` (appended to the iota block)
  - `pendingSteer` gains `source, target string`
  - `func (m Model) steerNavigatePreview(c steer.Command) (Model, tea.Cmd)`
  - `func (m Model) drainPendingPreview() (Model, tea.Cmd)`
  - `func previewStateReason(source, target string, st domain.PreviewState) string`
    — the ENGLISH twin of `previewStateNotice` (a steer reply is parsed by an
    agent and must never carry a translated string).

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/steer_preview_test.go`:

```go
package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/steer"
)

// previewSteerModel is mergePreviewModel wired for steering: a real inbox dir
// and the loading gate cleared, so applySteer is not refused outright.
func previewSteerModel(t *testing.T) (Model, string) {
	t.Helper()
	m, _, _ := mergePreviewModel(t)
	dir := t.TempDir()
	m.steerDir = dir
	m.snapshotWorktree = t.TempDir()
	m.loading = false
	m.width, m.height = 120, 40
	return m, dir
}

// A preview navigate with a file opens the preview, then the file's diff, then
// lands the line.
func TestSteerNavigateIntoASavedPreview(t *testing.T) {
	t.Parallel()
	m, _ := previewSteerModel(t)
	c := steer.Command{ID: "p-1", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "preview", Source: "feat/x", Target: "main"},
		Line:   &steer.Line{Side: "new", No: 1}, Wait: true}
	m, cmd := m.applySteer(c)
	if m.pendingSteer == nil || m.pendingSteer.stage != steerStagePreview {
		t.Fatalf("pendingSteer = %+v, want the preview stage", m.pendingSteer)
	}
	m = drainMsgs(t, m, cmd, 6)
	if m.previewOpen == nil || m.previewOpen.source != "feat/x" || m.previewOpen.target != "main" {
		t.Fatalf("previewOpen = %+v, want the commanded pair", m.previewOpen)
	}
	if m.diffLayer() == nil {
		t.Fatal("the file's diff must be open")
	}
	if m.pendingSteer != nil {
		t.Errorf("pendingSteer = %+v, want it drained", m.pendingSteer)
	}
}

// No saved preview matches the pair: the TUI opens a SHOW-ONCE one (id "").
func TestSteerNavigateOpensAShowOncePreview(t *testing.T) {
	t.Parallel()
	m, _ := previewSteerModel(t)
	m.previews = nil // no saved rows at all
	c := steer.Command{ID: "p-2", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "preview", Source: "feat/x", Target: "main"},
		Line:   &steer.Line{Side: "new", No: 1}, Wait: true}
	m, cmd := m.applySteer(c)
	m = drainMsgs(t, m, cmd, 6)
	if m.previewOpen == nil {
		t.Fatal("a show-once preview must still open")
	}
	if m.previewOpen.id != "" {
		t.Errorf("previewOpen.id = %q, want \"\" (show once)", m.previewOpen.id)
	}
}

// With no file the command only REVEALS the Previews entry.
func TestSteerNavigatePreviewWithNoFileRevealsTheEntry(t *testing.T) {
	t.Parallel()
	m, dir := previewSteerModel(t)
	c := steer.Command{ID: "p-3", Cmd: "navigate",
		Target: &steer.Target{State: "preview", Source: "feat/x", Target: "main"}, Wait: true}
	m, cmd := m.applySteer(c)
	if m.focus != panelPreviews {
		t.Errorf("focus = %v, want panelPreviews", m.focus)
	}
	if m.filesView != nil {
		t.Error("a reveal must not open the compare view")
	}
	if cmd != nil {
		cmd()
	}
	rep := readOneReply(t, dir, "p-3")
	if !rep.OK || !strings.Contains(rep.Detail, "revealed preview main...feat/x") {
		t.Errorf("reply = %+v", rep)
	}
}

// A branch the repo does not have is refused in ENGLISH protocol prose.
func TestSteerNavigatePreviewMissingBranchIsRefused(t *testing.T) {
	t.Parallel()
	m, dir := previewSteerModel(t)
	c := steer.Command{ID: "p-4", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "preview", Source: "feat/nope", Target: "main"},
		Line:   &steer.Line{Side: "new", No: 1}, Wait: true}
	m, cmd := m.applySteer(c)
	m = drainMsgs(t, m, cmd, 6)
	rep := readOneReply(t, dir, "p-4")
	if rep.OK {
		t.Fatalf("reply = %+v, want a refusal", rep)
	}
	if !strings.Contains(rep.Error, "missing branch feat/nope") {
		t.Errorf("reply.Error = %q, want English protocol prose naming the branch", rep.Error)
	}
	if m.pendingSteer != nil {
		t.Error("the pending must be cleared by the refusal")
	}
}

// A preview target with no pair is refused before anything moves.
func TestSteerPreviewTargetNeedsBothNames(t *testing.T) {
	t.Parallel()
	m, dir := previewSteerModel(t)
	c := steer.Command{ID: "p-5", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "preview", Source: "feat/x"}, Wait: true}
	m, cmd := m.applySteer(c)
	if cmd != nil {
		cmd()
	}
	if m.pendingSteer != nil {
		t.Error("a malformed target must park nothing")
	}
	rep := readOneReply(t, dir, "p-5")
	if rep.OK || !strings.Contains(rep.Error, "source and target") {
		t.Errorf("reply = %+v", rep)
	}
}

// A path the preview does not carry is answered, not left parked.
func TestSteerNavigatePreviewUnknownPathIsAnswered(t *testing.T) {
	t.Parallel()
	m, dir := previewSteerModel(t)
	c := steer.Command{ID: "p-6", Cmd: "navigate", File: "nope.txt",
		Target: &steer.Target{State: "preview", Source: "feat/x", Target: "main"},
		Line:   &steer.Line{Side: "new", No: 1}, Wait: true}
	m, cmd := m.applySteer(c)
	m = drainMsgs(t, m, cmd, 6)
	rep := readOneReply(t, dir, "p-6")
	if rep.OK || !strings.Contains(rep.Error, "nope.txt is not in preview main...feat/x") {
		t.Errorf("reply = %+v", rep)
	}
	if m.pendingSteer != nil {
		t.Error("the pending must be cleared")
	}
}
```

`readOneReply` wraps `steer.AwaitReply`, which the existing preview-steering
tests call inline (`internal/tui/preview_notes_test.go:274` and friends). There
is no shared helper yet, so add one at the top of this new file:

```go
// readOneReply reads the reply the answer command wrote for id.
func readOneReply(t *testing.T, dir, id string) steer.Reply {
	t.Helper()
	r, ok := steer.AwaitReply(dir, id, 2*time.Second)
	if !ok {
		t.Fatalf("no reply for %s", id)
	}
	return r
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-links && go test ./internal/tui/ -run 'SteerNavigatePreview|SteerNavigateIntoASaved|SteerNavigateOpensAShowOnce|SteerPreviewTarget' -v`
Expected: FAIL — `undefined: steerStagePreview`; without the enum change the
command is refused with `unknown target state "preview"`.

- [ ] **Step 3: Implement**

**`internal/tui/steer.go`** — in `steerEnumRefusal`:

```go
	if c.Target != nil {
		switch c.Target.State {
		case "", "unstaged", "staged", "untracked", "commit":
		case "preview":
			// A preview names a branch PAIR, and both halves are load-bearing:
			// a half-filled target would silently degrade into "some preview".
			if c.Target.Source == "" || c.Target.Target == "" {
				return "a preview target needs source and target"
			}
		default:
			return "unknown target state " + strconv.Quote(c.Target.State)
		}
	}
```

**`internal/tui/steer_nav.go`** — extend the stage enum and the pending:

```go
const (
	steerStageStatusRetry steerStage = iota // one status reload, then retry the lookup
	steerStageFiles                         // a commit's changed-file list
	steerStageDiff                          // the diff itself; land the cursor
	steerStagePreview                       // a merge preview's compare file list
)

// pendingSteer is a navigate command parked until the load it needs arrives.
type pendingSteer struct {
	cmd    steer.Command
	stage  steerStage
	tag    string // steerStageDiff: the m.diffTag this landing belongs to
	hash   string // steerStageFiles: the commit whose file list is loading
	source string // steerStagePreview: the pair whose compare list is loading
	target string
	at     time.Time
}
```

Add the preview arm FIRST in `steerNavigate`'s switch — before
`case c.File != "" && c.Target != nil && c.Target.State == "commit"`, because a
preview command also carries a file and would otherwise fall through to
`steerNavigateStatusFile`:

```go
func (m Model) steerNavigate(c steer.Command) (Model, tea.Cmd) {
	switch {
	case c.Step != "":
		return m.steerStep(c)
	case c.Target != nil && c.Target.State == "preview":
		return m.steerNavigatePreview(c)
	case c.File != "" && c.Target != nil && c.Target.State == "commit":
		...
```

Add the two functions at the end of the file:

```go
// steerNavigatePreview lands in a MERGE PREVIEW. With no file it only reveals
// the Previews entry; with one it opens the preview (a saved row when the pair
// has one, else a transient "show once" — the very open preview_actions.go's
// pair dialog performs) and parks until the compare file list arrives.
func (m Model) steerNavigatePreview(c steer.Command) (Model, tea.Cmd) {
	src, tgt := c.Target.Source, c.Target.Target
	// Which saved row, if any, holds this pair. "" means a show-once open.
	id, bi := "", -1
	for i, r := range m.previews {
		if r.rec.Source == src && r.rec.Target == tgt {
			id, bi = r.rec.ID, i
			break
		}
	}
	if c.File == "" {
		m = m.steerToPanels().activateTab(panelPreviews)
		if bi >= 0 {
			// "Go to" semantics, as everywhere else: a /-filter that hid the row
			// would make the landing invisible.
			m, _ = m.clearFilteringForFocus()
			for di, b := range m.displayIndices(panelPreviews) {
				if b == bi {
					m.sel[panelPreviews] = di
					break
				}
			}
		}
		m = m.steerNotice(i18n.T("▸ agent moved the focus"))
		return m, m.answerSteer(c, steerOK(c, "revealed preview "+tgt+"..."+src))
	}
	// Decided before the view moves: openDiffForFileLine refuses below 60
	// columns with an i18n status message, and a reply must be English prose.
	if m.width > 0 && m.width < 60 {
		return m, m.answerSteer(c, steerFail(c, "the terminal is too narrow for the diff view"))
	}
	// steerToPanels closes the files view, which clears previewOpen and bumps
	// previewGen — so the open below always really loads, instead of hitting
	// handlePreviewOpenMsg's same-tag reconcile arm (which issues no
	// compareFilesMsg and would leave this pending to expire).
	m = m.steerToPanels()
	m.pendingSteer = &pendingSteer{cmd: c, stage: steerStagePreview, source: src, target: tgt, at: time.Now()}
	return m, m.openPreviewCmd(id, src, tgt, "")
}

// drainPendingPreview selects the commanded path in the preview's freshly
// loaded compare list and opens its diff, advancing to the diff stage. It is
// drainPendingFiles' twin for the compare lane: a preview's file list arrives
// as a compareFilesMsg, which has no commit hash to gate on — the OPEN pair is
// the gate instead.
func (m Model) drainPendingPreview() (Model, tea.Cmd) {
	ps, po := m.pendingSteer, m.previewOpen
	if ps == nil || ps.stage != steerStagePreview || m.filesView == nil || po == nil ||
		po.source != ps.source || po.target != ps.target {
		return m, nil
	}
	// The list loads late: the user may have opened something across that gap,
	// and this drain is about to move the view.
	if why := m.steerRefusal(); why != "" {
		return m.failPending(why)
	}
	c := ps.cmd
	pair := c.Target.Target + "..." + c.Target.Source
	for i, l := range m.filesView.lines {
		if l.heading || l.path != c.File {
			continue
		}
		if m.width > 0 && m.width < 60 {
			return m.failPending("the terminal is too narrow for the diff view")
		}
		m.filesView.sel = i
		tm, cmd := m.openDiffForFileLine(l)
		m = tm.(Model)
		if m.diffLayer() == nil {
			return m.failPending("the diff could not be opened")
		}
		if c.Line == nil {
			m.pendingSteer = nil
			return m, tea.Batch(cmd, m.answerSteer(c, steerOK(c, "opened "+c.File+" in preview "+pair)))
		}
		m.pendingSteer = &pendingSteer{cmd: c, stage: steerStageDiff, tag: m.diffTag, at: time.Now()}
		return m, cmd
	}
	// Answered but NOT undone, exactly as drainPendingFiles decides: the
	// preview the agent named IS open, and that is a truthful answer to the
	// half of the command that was valid.
	return m.failPending(c.File + " is not in preview " + pair)
}
```

**`internal/tui/preview_open.go`** — add the English twin and answer a parked
navigate on both failure arms of `handlePreviewOpenMsg`:

```go
// previewStateReason is previewStateNotice's ENGLISH twin. A steer reply is
// protocol prose an agent parses and must never carry a translated string.
func previewStateReason(source, target string, st domain.PreviewState) string {
	switch st {
	case domain.PreviewMerged:
		return source + " is already merged into " + target
	case domain.PreviewMissingSource:
		return "missing branch " + source
	case domain.PreviewMissingTarget:
		return "missing branch " + target
	case domain.PreviewNoBase:
		return source + " and " + target + " have no common base"
	}
	return "the pair is not previewable"
}

// pendingPreviewFor reports whether a navigate is parked on THIS pair's open.
func (m Model) pendingPreviewFor(source, target string) bool {
	ps := m.pendingSteer
	return ps != nil && ps.stage == steerStagePreview && ps.source == source && ps.target == target
}
```

In `handlePreviewOpenMsg`:

```go
	if msg.err != nil {
		m.statusMsg = i18n.T("error: %s", msg.err.Error())
		if m.pendingPreviewFor(msg.source, msg.target) {
			return m.failPending("the merge preview failed to open: " + msg.err.Error())
		}
		return m, nil
	}
	...
	if msg.eps.Summary.State != domain.PreviewOK {
		m.statusMsg = previewStateNotice(msg.source, msg.target, msg.eps.Summary.State)
		if isOpen {
			m = m.closePreviewView() // the open pair stopped being previewable
		}
		if m.pendingPreviewFor(msg.source, msg.target) {
			return m.failPending(previewStateReason(msg.source, msg.target, msg.eps.Summary.State))
		}
		return m, nil
	}
```

**`internal/tui/model.go`** — in the `case compareFilesMsg:` arm, replace the
final `return m, nil` (after the `po.keepPath` block) with:

```go
		// A parked preview navigate waits on exactly this list.
		return m.drainPendingPreview()
```

and, in that same arm's `if msg.err != nil { … }` block, answer a pending
parked on this pair instead of leaving it to time out (the rule
`commitFilesMsg` already follows for its own stage) — add it as the last
statement of the block, replacing its `return m, nil`:

```go
			if po := m.previewOpen; po != nil && m.pendingPreviewFor(po.source, po.target) {
				return m.failPending("the preview's file list failed to load: " + msg.err.Error())
			}
			return m, nil
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-links && go test ./internal/tui/ -v 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-links && git add internal/tui/ && git commit -m "feat(tui): steer navigate can land inside a merge preview

target.state \"preview\" opens the saved row for the pair (or a show-once
preview when none is saved), parks on a new steerStagePreview until the
compare file list arrives, then falls into the existing diff stage. A
file-less command only reveals the Previews entry; a missing branch is
refused with English protocol prose, never the i18n notice.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 8: `--at <link>` at TUI startup, `gg open`, and the launcher seam

**Files:**
- Modify: `internal/tui/run.go` (`Run` signature)
- Modify: `internal/tui/model.go` (`startAt`, `startAtMsg`, `startAtFailMsg`, the `dataLoadedMsg` arm)
- Modify: `internal/tui/steer.go` (`answerSteer` surfaces an `ID==""` refusal)
- Modify: `internal/tui/steer_nav.go` (`steerCommandForLink`)
- Create: `internal/cli/open.go`
- Modify: `internal/cli/cli.go` (`commands`, `runOne`)
- Modify: `cmd/gg/main.go` (`launchTUI`, the seam, the command list)
- Test: `internal/tui/start_at_test.go` (new)
- Test: `internal/cli/open_test.go` (new)

**Interfaces:**
- Consumes: `navigateCommandFor`, `errNavLinkRepoOnly`, `errNavLinkNoLine`,
  `navExit` (Task 5); `steer.Target.Source/.Target` (Task 3);
  `routeFor(dir) sessionRoute`, `sendSteer(dir, c, noWait, stdout, stderr) int`,
  `openLinkTarget(res) *domain.Service`, `config.SessionSteerDir` (existing).
- Produces:
  - `func tui.Run(svc *domain.Service, recordPath string, at model.Link) (string, error)`
  - `func steerCommandForLink(l model.Link) (steer.Command, bool)` (tui, pure —
    a `Hunk > 0` link is a REFUSAL, never silently ignored: `gg open` lowers a
    hunk to a line before launching)
  - `var cli.LaunchTUI func(checkout string, at model.Link) int`
  - `func cmdOpen(svc *domain.Service, args []string, stdout, stderr io.Writer) int`
  - `func launchTUI(dir string, at model.Link, recordPath, cwdFile string) int` (cmd/gg)

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/start_at_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestSteerCommandForLink(t *testing.T) {
	t.Parallel()
	pv := func(s string) model.Link {
		l, err := model.ParseLink(s)
		if err != nil {
			t.Fatalf("ParseLink(%q): %v", s, err)
		}
		return l
	}
	c, ok := steerCommandForLink(pv("gg://r/a.txt@main...feat/x:4"))
	if !ok || c.Cmd != "navigate" || c.File != "a.txt" {
		t.Fatalf("preview file link = %+v,%v", c, ok)
	}
	if c.Target == nil || c.Target.State != "preview" || c.Target.Source != "feat/x" || c.Target.Target != "main" {
		t.Fatalf("target = %+v", c.Target)
	}
	if c.Line == nil || c.Line.No != 4 || c.Line.Side != "new" {
		t.Fatalf("line = %+v", c.Line)
	}
	if c.ID != "" || c.Wait {
		t.Errorf("a startup navigate must carry no id and no wait: %+v", c)
	}
	// A repo-level preview link reveals the entry: no file, no line.
	c, ok = steerCommandForLink(pv("gg://r@main...feat/x"))
	if !ok || c.File != "" || c.Line != nil || c.Target.State != "preview" {
		t.Fatalf("preview reveal = %+v,%v", c, ok)
	}
	// A hunk link is REFUSED: gg open lowers it to a line before launching.
	if _, ok := steerCommandForLink(pv("gg://r/a.txt@main...feat/x#2")); ok {
		t.Error("a #hunk link must be refused here, not silently ignored")
	}
	// A commit link with a full sha reveals the commit.
	sha := strings.Repeat("a", 40)
	c, ok = steerCommandForLink(pv("gg://r@" + sha))
	if !ok || c.Commit != sha {
		t.Fatalf("commit link = %+v,%v", c, ok)
	}
	// A working-tree file:line link.
	c, ok = steerCommandForLink(pv("gg://r/a.txt:9"))
	if !ok || c.Target == nil || c.Target.State != "unstaged" || c.Line.No != 9 {
		t.Fatalf("worktree link = %+v,%v", c, ok)
	}
}

// --at is consumed ONCE, after the first snapshot lands, and drives the same
// pipeline a steered navigate does.
func TestStartAtOpensThePreviewAfterTheFirstLoad(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	m.loading = false
	m.width, m.height = 120, 40
	at, err := model.ParseLink("gg://gigagit/a.txt@main...feat/x:1")
	if err != nil {
		t.Fatal(err)
	}
	m.startAt, m.startAtPending = at, true
	// The first full snapshot: the same message run.go's load produces.
	updated, cmd := m.Update(m.loadCmd()())
	m = updated.(Model)
	if m.startAtPending {
		t.Error("--at must be consumed by the first dataLoadedMsg")
	}
	m = drainMsgs(t, m, cmd, 8)
	if m.previewOpen == nil || m.previewOpen.source != "feat/x" {
		t.Fatalf("previewOpen = %+v, want the --at pair", m.previewOpen)
	}
}

// A refusal on the --at path has no CLI to print it: it must reach the status
// bar instead of vanishing (answerSteer writes no reply file for ID == "").
func TestStartAtFailureShowsAStatusMessage(t *testing.T) {
	t.Parallel()
	m, _ := previewSteerModel(t)
	c, ok := steerCommandForLink(mustLink(t, "gg://gigagit/a.txt@main...feat/nope:1"))
	if !ok {
		t.Fatal("the link must build a command")
	}
	m, cmd := m.applySteer(c)
	m = drainMsgs(t, m, cmd, 6)
	if m.statusMsg == "" || !strings.Contains(m.statusMsg, "missing branch feat/nope") {
		t.Errorf("statusMsg = %q, want the refusal", m.statusMsg)
	}
}

func mustLink(t *testing.T, s string) model.Link {
	t.Helper()
	l, err := model.ParseLink(s)
	if err != nil {
		t.Fatal(err)
	}
	return l
}
```

Create `internal/cli/open_test.go`:

```go
package cli

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// With a live session, gg open steers it and says so.
func TestOpenSteersALiveSession(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	inbox := steerDirFor(svc)
	livePresence(t, inbox)
	t.Cleanup(func() { steer.Discard(inbox) })
	var out, errb strings.Builder
	code := cmdOpen(svc, []string{previewLinkFor(dir, "a.txt") + ":1", "--no-wait"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "steered: ") {
		t.Errorf("stdout = %q, want a \"steered:\" line", out.String())
	}
	got := steer.Drain(inbox)
	if len(got) != 1 || got[0].Target == nil || got[0].Target.State != "preview" {
		t.Fatalf("posted = %+v, want one preview navigate", got)
	}
}

// With no session and no launcher installed, gg open exits 1 and says why.
func TestOpenWithoutASessionOrALauncherExitsOne(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	LaunchTUI = nil
	var out, errb strings.Builder
	code := cmdOpen(svc, []string{previewLinkFor(dir, "a.txt") + ":1"}, &out, &errb)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "no live gg session in") || !strings.Contains(errb.String(), "launcher is unavailable") {
		t.Errorf("stderr = %q", errb.String())
	}
}

// With a launcher installed it is called with the checkout and a fully
// resolved link — the #hunk already lowered to a line.
func TestOpenCallsTheLauncherWithAResolvedLink(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	var gotCheckout string
	var gotLink model.Link
	LaunchTUI = func(checkout string, at model.Link) int {
		gotCheckout, gotLink = checkout, at
		return 0
	}
	t.Cleanup(func() { LaunchTUI = nil })
	var out, errb strings.Builder
	if code := cmdOpen(svc, []string{previewLinkFor(dir, "a.txt") + "#1"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	// SamePath, not ==: it is the same normalisation the resolver used, and it
	// survives macOS's /private symlink in front of a t.TempDir().
	if !domain.SamePath(gotCheckout, dir) {
		t.Errorf("checkout = %q, want %q", gotCheckout, dir)
	}
	if gotLink.Target.Preview == nil || gotLink.Target.Preview.Source != "feat/x" {
		t.Fatalf("link target = %+v", gotLink.Target)
	}
	if gotLink.Hunk != 0 || gotLink.Line != 1 {
		t.Errorf("link = hunk %d line %d, want the hunk lowered to line 1", gotLink.Hunk, gotLink.Line)
	}
	if gotLink.Path != "a.txt" {
		t.Errorf("link path = %q", gotLink.Path)
	}
}

func TestOpenRefusesAnUnresolvableLink(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	LaunchTUI = nil
	var out, errb strings.Builder
	if code := cmdOpen(svc, []string{"gg://no-such-repo/a.txt:1"}, &out, &errb); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if code := cmdOpen(svc, []string{"not-a-link"}, &out, &errb); code != 2 {
		t.Errorf("a non-link argument must exit 2, got %d", code)
	}
}
```

`openCLIService(t, dir)` opens a service with the CLI's own setup
(`setupCLIService`, `internal/cli/cli.go:37`). No test helper wraps it today, so
add one beside `runCLI` in `internal/cli/cli_test.go`:

```go
// openCLIService opens dir with the setup Run gives the cwd's service.
func openCLIService(t *testing.T, dir string) *domain.Service {
	t.Helper()
	svc := domain.Open(dir)
	setupCLIService(svc)
	return svc
}
```

These `gg open` tests mutate the package global `LaunchTUI`, so they must NOT
call `t.Parallel()` (they already do not — `previewRepo` uses `t.Setenv`).

- [ ] **Step 2: Run the tests to verify they fail**

Run:
```
cd /mnt/t/others/gigagit.worktrees/feat-preview-links && go test ./internal/tui/ -run 'SteerCommandForLink|StartAt' -v; go test ./internal/cli/ -run 'TestOpen' -v
```
Expected: FAIL — `undefined: steerCommandForLink`, `m.startAt undefined`,
`undefined: cmdOpen`.

- [ ] **Step 3: Implement**

**`internal/tui/steer_nav.go`** — add the pure builder:

```go
// steerCommandForLink turns a FULLY RESOLVED link into the navigate command
// the CLI would have posted for it — the `--at` startup path's only step.
//
// It is pure: no git, no service. That is why a #<hunk> link is a REFUSAL
// rather than something silently dropped — `gg open` lowers a hunk to a line
// (through PreviewHunkAnchor / HunkRange, on the target checkout) before it
// ever reaches the launcher, so a hunk arriving here means the caller skipped
// that step.
func steerCommandForLink(l model.Link) (steer.Command, bool) {
	if l.Hunk > 0 {
		return steer.Command{}, false
	}
	c := steer.Command{Cmd: "navigate"}
	if p := l.Target.Preview; p != nil {
		c.Target = &steer.Target{State: "preview", Source: p.Source, Target: p.Target}
		if l.Path == "" {
			return c, true
		}
		if l.Line < 1 {
			return steer.Command{}, false
		}
		c.File = l.Path
		c.Line = &steer.Line{Side: "new", No: l.Line} // a preview has no old side
		return c, true
	}
	if l.Path == "" {
		// A commit reveal needs the FULL sha: the consumer compares hashes.
		if l.Target.State != model.StateCommitted || len(l.Target.Commit) < 40 {
			return steer.Command{}, false
		}
		c.Commit = l.Target.Commit
		return c, true
	}
	t := &steer.Target{State: "unstaged"}
	switch l.Target.State {
	case model.StateStaged:
		t.State = "staged"
	case model.StateCommitted:
		if len(l.Target.Commit) < 40 {
			return steer.Command{}, false
		}
		t.State, t.Commit = "commit", l.Target.Commit
	}
	if l.Line < 1 {
		return steer.Command{}, false
	}
	side := "new"
	if l.Side == model.NoteSideOld {
		side = "old"
	}
	c.File, c.Target = l.Path, t
	c.Line = &steer.Line{Side: side, No: l.Line}
	return c, true
}
```

**`internal/tui/steer.go`** — surface an `ID==""` refusal:

```go
// startAtFailMsg carries a --at startup navigate's refusal back to the UI
// thread. The startup command posts no reply file (nobody is waiting for one),
// so without this the user would watch nothing happen and be told nothing.
type startAtFailMsg struct{ reason string }

// answerSteer writes a reply off-thread. A command posted with wait:false gets
// none — nothing would ever read it, and the file would only have to be swept.
func (m Model) answerSteer(c steer.Command, r steer.Reply) tea.Cmd {
	if c.ID == "" {
		// Only the local `--at` startup navigate has no id: steer.Post fills one
		// in, and Drain discards any command that arrived without one. Its
		// refusals have no CLI to print them, so they go to the status bar.
		if !r.OK {
			reason := r.Error
			return func() tea.Msg { return startAtFailMsg{reason: reason} }
		}
		return nil
	}
	if !c.Wait || m.steerDir == "" {
		return nil
	}
	dir := m.steerDir
	return func() tea.Msg {
		_ = steer.PostReply(dir, r)
		return nil
	}
}
```

**`internal/tui/model.go`** — add the fields to `Model` (next to `pendingSteer`):

```go
	startAt        model.Link // --at: where to land once the first snapshot has loaded
	startAtPending bool       // consumed exactly once, by the first dataLoadedMsg
```

Add the message type beside the other local messages:

```go
// startAtMsg feeds the --at startup link into the steering pipeline on the
// Update goroutine, once the first snapshot has landed.
type startAtMsg struct{ cmd steer.Command }
```

In `Update`, add two arms:

```go
	case startAtMsg:
		// applySteer, not steerNavigate: the startup landing must obey the SAME
		// refusals a steered one does — maybeResumePrompt can raise a decision
		// modal in this very tick, and only applySteer runs steerRefusal and
		// steerEnumRefusal. A refusal reaches the status bar through the
		// ID == "" arm of answerSteer.
		return m.applySteer(msg.cmd)
	case startAtFailMsg:
		m.statusMsg = i18n.T("error: %s", msg.reason)
		return m, nil
```

In the `case dataLoadedMsg:` success arm, immediately after
`m.loadedOK = true`, add:

```go
			// --at is consumed exactly once, by the FIRST snapshot: the model
			// must already describe this repo before a navigate can land.
			var atCmd tea.Cmd
			if m.startAtPending {
				m.startAtPending = false
				if c, ok := steerCommandForLink(m.startAt); ok {
					atCmd = func() tea.Msg { return startAtMsg{cmd: c} }
				} else {
					// The key is the literal the AST gate checks; the ARG here is
					// an English sentence, exactly as every engine/git error
					// reaching this key is. If the gate ever flags the argument
					// too, promote it to its own key in all four bundles.
					m.statusMsg = i18n.T("error: %s", "that gg link names no place gg can open")
				}
			}
```

and add `atCmd` to each of that arm's three `tea.Batch(...)` returns:

```go
				return nm, tea.Batch(themeCmd, procCmd, previewsCmd, steerCmd, atCmd)
				...
				return m, tea.Batch(themeCmd, reload, previewsCmd, steerCmd, atCmd)
				...
			return m, tea.Batch(themeCmd, previewsCmd, steerCmd, atCmd)
```

**`internal/tui/run.go`** — the signature and the wiring:

```go
// Run launches the TUI for svc, taking over the alternate screen until the
// user quits. at is the `gg open` landing link (the zero value = none): it is
// converted into a navigate and applied once the first snapshot has loaded.
// It returns the directory the shell should switch to …
func Run(svc *domain.Service, recordPath string, at model.Link) (string, error) {
	m := New(svc)
	if at.Repo.Name != "" || at.Repo.Abs != "" {
		m.startAt, m.startAtPending = at, true
	}
	m.statePath = repos.DefaultStatePath()
	...
```

Add `"github.com/homeend/gigagit/internal/model"` to run.go's imports.

**`internal/cli/open.go`** (new):

```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// LaunchTUI launches the TUI for a checkout, positioned at a link. cmd/gg
// installs it; internal/cli must NOT import internal/tui (archtest guards the
// layering), so the launcher arrives as a seam. nil — the value every test
// sees — means no launcher is available, and `gg open` says so rather than
// pretending it opened something.
var LaunchTUI func(checkout string, at model.Link) int

const openUsage = "usage: gg open <gg://…> [--no-wait]\n" +
	"quote links that carry #<hunk> — an unquoted # starts a shell comment"

// cmdOpen is `gg open <link>`: show the link to the USER. A live gg session in
// the link's checkout (TUI or web page) is steered; otherwise the TUI is
// launched there, positioned on the link.
func cmdOpen(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	fs.SetOutput(stderr)
	noWait := fs.Bool("no-wait", false, "steer without waiting for the session's answer")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, openUsage)
		return 2
	}
	if !isLinkArg(pos[0]) {
		fmt.Fprintf(stderr, "open: %q is not a gg link (it must start with %s)\n", pos[0], model.LinkScheme)
		return 2
	}
	ctx := context.Background()
	res, err := resolveLinkArg(ctx, svc, pos[0])
	if err != nil {
		return linkExit("open", err, stderr)
	}
	// Every git question below is asked of the LINK's checkout, wherever the
	// caller ran (the headline case is `gg open` from /tmp).
	target := openLinkTarget(res)
	c, err := navigateCommandFor(ctx, target, res)
	if err != nil {
		return navExit("open", err, stderr)
	}
	dir := ""
	if cd, cderr := target.GitCommonDir(ctx); cderr == nil {
		dir = config.SessionSteerDir(cd, res.Checkout)
	}
	if r := routeFor(dir); r.tuiOK || r.webOK {
		// sendSteer is the whole routing table — a live web page is reached over
		// HTTP exactly as `gg session navigate` reaches it.
		code := sendSteer(dir, c, *noWait, stdout, stderr)
		if code == 0 {
			fmt.Fprintln(stdout, "steered: "+res.Checkout)
		}
		return code
	}
	if LaunchTUI == nil {
		fmt.Fprintf(stderr, "open: no live gg session in %s and the TUI launcher is unavailable\n", res.Checkout)
		return 1
	}
	return LaunchTUI(res.Checkout, openAtLink(res, c))
}

// openAtLink is the link handed to the launcher: the same place, fully
// resolved, with any #<hunk> ALREADY lowered to the line navigateCommandFor
// computed. The TUI's converter is pure, so everything needing a repository
// must be settled here.
func openAtLink(res domain.Resolved, c steer.Command) model.Link {
	l := model.Link{
		Repo: model.LinkRepo{Abs: filepath.ToSlash(filepath.Clean(res.Checkout))},
		Path: res.Addr.Path,
		Side: model.NoteSideNew,
	}
	if res.Preview != nil {
		l.Target = model.LinkTarget{
			State:   model.StateCommitted,
			Preview: &model.LinkPreview{Source: res.Preview.Source, Target: res.Preview.Target},
		}
	} else {
		l.Target = model.LinkTarget{State: res.Addr.State, Commit: res.Addr.Commit}
	}
	if c.Line != nil {
		l.Line = c.Line.No
		if c.Line.Side == "old" {
			l.Side = model.NoteSideOld
		}
	}
	return l
}
```

Note the import list above: `errors` is NOT needed (`navExit`, in session.go,
owns the sentinel comparisons) — drop it from `open.go`'s imports.

**`internal/cli/cli.go`** — register the verb:

```go
	case "link":
		return cmdLink(svc, workdir, rest, stdout, stderr)
	case "open":
		return cmdOpen(svc, rest, stdout, stderr)
```

and in the `commands` map: `"note": true, "skill": true, "session": true, "link": true, "open": true,`

**`cmd/gg/main.go`** — install the seam right after `cli.InitHomeDir`:

```go
	cli.RepoStatePath = repos.DefaultStatePath()
	if home, err := os.UserHomeDir(); err == nil {
		cli.InitHomeDir = home
	}
	// `gg open <link>` with no live session in the link's checkout launches the
	// TUI there. internal/cli must not import internal/tui, so the launcher is
	// installed here — and it is the SAME launchTUI the no-subcommand path runs,
	// so the two can never drift (preflight, the error log, the panic dump).
	cli.LaunchTUI = func(checkout string, at model.Link) int {
		return launchTUI(checkout, at, "", "")
	}
```

Replace the no-subcommand block (everything from
`// No subcommand: launch the TUI.` to the end of `main`) with:

```go
	// No subcommand: launch the TUI in the current directory.
	os.Exit(launchTUI(".", model.Link{}, recordPath, cwdFile))
}

// launchTUI runs the whole TUI startup sequence for dir: the runner stack, the
// panic dump, the friendly preflight, the always-on error log, and tui.Run
// positioned at `at` (the zero Link = nowhere in particular). It is shared by
// the no-subcommand path and by `gg open`'s launcher seam, so a checkout
// reached either way gets the same startup.
func launchTUI(dir string, at model.Link, recordPath, cwdFile string) int {
	if dir != "" && dir != "." {
		if err := os.Chdir(dir); err != nil {
			fmt.Fprintln(os.Stderr, "gg:", err)
			return 1
		}
	}
	// The runner stack (LimitRunner + ssh BatchMode) is built by domain — one
	// construction site shared with the repo switcher's reRoot (domain.OpenTUI);
	// only the span ring is kept here so the panic dump below can include the
	// session's git spans.
	ring := observ.NewRing(200)
	svc := domain.OpenTUIWithRing(".", ring)
	repo := svc.Repo()
	defer func() {
		if r := recover(); r != nil {
			path := filepath.Join(os.TempDir(), fmt.Sprintf("gg-panic-%d.json", time.Now().Unix()))
			_ = app.DumpRepo(context.Background(), path, repo, ring, []string{fmt.Sprintf("panic: %v", r)})
			fmt.Fprintf(os.Stderr, "gg panicked; debug dump written to %s\n", path)
			panic(r)
		}
	}()
	// Pre-flight: surface the common "not a git repository" / missing-git case as
	// a friendly message instead of launching the TUI only for it to fail with a
	// raw "git status failed (exit 128): fatal: …" dump.
	if _, err := svc.TopLevel(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, friendlyGitError(err))
		return 1
	}
	proceed, err := tui.Preflight(svc, os.Stdin, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !proceed {
		return 0
	}
	if ef, _, eerr := tui.OpenErrorLog(); eerr == nil && ef != nil {
		observ.SetFailureSink(ef)
		defer func() { observ.SetFailureSink(nil); _ = ef.Close() }()
	}
	if recordPath != "" {
		if err := checkRecordPath(recordPath); err != nil {
			fmt.Fprintln(os.Stderr, "gg: --record:", err)
			return 2
		}
	}
	cwd, err := tui.Run(svc, recordPath, at)
	if err != nil {
		fmt.Fprintln(os.Stderr, friendlyGitError(err))
		return 1
	}
	// Only write the cwd file when the user actually switched worktrees, so a
	// gg-wrapped shell stays put otherwise.
	if cwdFile != "" && cwd != "" {
		_ = os.WriteFile(cwdFile, []byte(cwd), 0o644)
	}
	return 0
}
```

Add `"github.com/homeend/gigagit/internal/model"` to `cmd/gg/main.go`'s imports,
and add `open` to the unknown-command help line:

```go
		fmt.Fprintln(os.Stderr, "commands: status commit pull push switch checkout branch stash undo merge rebase fast-forward cherry-pick revert reset discard add unstage log diff show compare preview shelf bookmark prefix worktree remote tag note session link open versions migrate review apply unlock config repo init skill batch mcp web shell-init inspect version (run `gg` with no arguments for the TUI)")
```

Finally, update the other `tui.Run(` call sites (grep it — `cmd/gg/main.go` is
the only production one) to pass `model.Link{}`.

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```
cd /mnt/t/others/gigagit.worktrees/feat-preview-links && go build ./... && go test ./internal/tui/ ./internal/cli/ ./internal/archtest/ -v 2>&1 | tail -40
```
Expected: PASS — `archtest`'s import guard still holds (`internal/cli` gained no
`internal/tui` import).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-links && git add internal/tui/ internal/cli/ cmd/gg/main.go && git commit -m "feat: gg open <link>, and the TUI's --at startup landing

gg open resolves the link, steers a live TUI or web session in that
checkout (printing \"steered: <checkout>\"), or calls the cli.LaunchTUI
seam cmd/gg installs. main's no-subcommand TUI launch becomes launchTUI,
used by both paths, so preflight/error-log/panic-dump cannot drift. The
TUI converts the link to the SAME navigate the CLI posts once the first
snapshot lands; refusals reach the status bar.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 9: The web page lands in a preview

**Files:**
- Modify: `internal/web/static/previews.js`
- Modify: `internal/web/static/live.js`
- Test: `internal/web/steerpreviewjs_test.go` (new)

**Interfaces:**
- Consumes: the `steerWire` `state`/`source`/`target` fields (Task 3);
  `previews.js`' `openPreviewEntry(e, moved)` and `openOnce(source, target,
  moved)` (module-private), `state.previews` (the `/api/preview` entries, each
  with `.id`, `.source`, `.target`).
- Produces: `export async function openPreviewByPair(source, target)` from
  `previews.js`, imported by `live.js`.

- [ ] **Step 1: Write the failing test**

`live.js` and `previews.js` both import other modules at the top level, so
neither has a pure, node-runnable slice (the `commitmetajs_test.go` /
`linksjs_test.go` "pure section" convention does not apply). Pin the wiring with
the string-assertion style `linksjs_test.go` uses instead.

Create `internal/web/steerpreviewjs_test.go`:

```go
package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The preview landing spans two modules with top-level imports on both sides,
// so there is no pure slice to run under node (see linksjs_test.go's
// convention). Pin the wiring by source assertion instead — every string below
// exists ONLY after this feature, so none of them can pass on the old file.
func TestSteerPreviewJSIsWired(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"previews.js", "export async function openPreviewByPair", "the pair opener must be exported for live.js"},
		{"previews.js", "openPreviewEntry(e,", "the saved row takes the record path"},
		{"previews.js", "await openOnce(source, target,", "an unsaved pair falls back to the transient open"},
		{"live.js", "openPreviewByPair", "the navigate handler must call it"},
		{"live.js", `from "./previews.js"`, "previews.js must be imported in live.js"},
		{"live.js", `s.state === "preview"`, "the navigate handler must branch on the preview target"},
		{"live.js", "s.source, s.target", "the pair rides the wire, not a sha"},
	}
	src := map[string]string{"previews.js": read("previews.js"), "live.js": read("live.js")}
	for _, c := range checks {
		if !strings.Contains(src[c.file], c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	// The import must be on the EXISTING previews.js import line (live.js
	// already imports fetchPreviews/reopenPreviewIfMoved from it) — a second
	// import statement for the same module is a lint smell and easy to strand.
	if n := strings.Count(src["live.js"], `from "./previews.js"`); n != 1 {
		t.Errorf("live.js imports ./previews.js %d times, want exactly 1", n)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-links && go test ./internal/web/ -run SteerPreviewJS -v`
Expected: FAIL — `previews.js: missing "export async function openPreviewByPair"`.

- [ ] **Step 3: Implement**

In `internal/web/static/previews.js`, add the exported opener right after
`openPreviewEntry`:

```js
// openPreviewByPair opens the preview a steering command named. A saved row
// for the pair takes the record path (so the page shows the label and the row
// stays selected); an unsaved pair opens transiently, exactly as the sidebar's
// "show once" does. Nothing here is saved — a steer must not write to the
// user's preview list.
export async function openPreviewByPair(source, target) {
  const e = (state.previews || []).find((p) => p.source === source && p.target === target);
  if (e) {
    await openPreviewEntry(e, "");
    return;
  }
  await openOnce(source, target, "");
}
```

In `internal/web/static/live.js`, extend the existing `previews.js` import:

```js
import { fetchPreviews, openPreviewByPair, reopenPreviewIfMoved } from "./previews.js";
```

and restructure `steerNavigate`'s body (the `if (!s.line) return;` tail is
unchanged):

```js
async function steerNavigate(s) {
  if (s.step) {
    stepNote(s.step === "next_note" ? 1 : -1);
    return;
  }
  if (s.state === "preview") {
    // The pair, never a sha: the tip is resolved here, so a tip that moved
    // between post and apply is honoured (the TUI consumer does the same).
    await openPreviewByPair(s.source, s.target);
    if (!s.file) return; // a file-less preview navigate only reveals the stage
    const i = state.files.findIndex((f) => f.path === s.file);
    if (i < 0) return;
    await openFile(i);
  } else if (!s.file) {
    if (s.commit) await openCommitByHash(s.commit, s.commit.slice(0, 8));
    return;
  } else if (s.state === "commit") {
    await openCommitByHash(s.commit, s.commit.slice(0, 8));
    const i = state.files.findIndex((f) => f.path === s.file);
    if (i < 0) return;
    await openFile(i);
  } else {
    // The working-tree stage, entered the way the palette enters it (0 = the
    // WT row): it re-reads status — the agent may have written the file a
    // moment ago — and puts the file list on screen before the diff opens.
    await openWorkingTree(0);
    const i = state.statusEntries.findIndex((f) => f.path === s.file);
    if (i < 0) return;
    await openFile(i);
  }
  if (!s.line) return;
  const side = s.side; // the server fills it whenever a line is present
  ...
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-links && go test ./internal/web/ -v 2>&1 | tail -20`
Expected: PASS (the existing JS-source tests, `linksjs_test.go` included, still
pass — `links.js` was not touched, per ruling 10).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-links && git add internal/web/static/previews.js internal/web/static/live.js internal/web/steerpreviewjs_test.go && git commit -m "feat(web): a steered navigate can land in a merge preview

live.js branches on state === \"preview\" and opens the pair through
previews.js' new openPreviewByPair — the saved row when one matches, else
the transient show-once open — then the file, then the line. Web link
PRODUCTION (links.js) is untouched: it stays a follow-up per spec §3.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 10: Skills, docs and the e2e scenario

**Files:**
- Modify: `internal/agentskill/agentskill.go` (`Version` 72→73, `ReviewVersion` 6→7)
- Modify: `internal/agentskill/using-gg.md`
- Modify: `internal/agentskill/reviewing-with-gg.md`
- Modify: `.claude/skills/using-gg/SKILL.md` (regenerated)
- Modify: `.claude/skills/reviewing-with-gg/SKILL.md` (regenerated)
- Modify: `CHANGELOG.md`
- Modify: `README.md`
- Modify: `docs/CLAUDE-details.md`
- Modify: `CLAUDE.md` (the `steer` package row only)
- Create: `e2e/scenarios/s92_preview_links.toml`

**Interfaces:**
- Consumes: every verb shipped in Tasks 1-9.
- Produces: no Go API.

- [ ] **Step 1: Write the failing e2e scenario**

Create `e2e/scenarios/s92_preview_links.toml`:

```toml
name = "preview links: gg link --preview, a link that drives note add, and gg link resolve --json"

[input]
steps = [
  { write = "a.txt", content = "alpha\nbravo\ncharlie\n" },
  { commit = "seed" },
  { branch = "feat/x" },
  { switch = "feat/x" },
  { write = "a.txt", content = "alpha\nbravo\ncharlie\nDELTA\n" },
  { commit = "add DELTA" },
]

[[run]]
cmd  = ["preview", "add", "--label", "login", "feat/x", "main"]
exit = 0

# The producer: the sandbox repo has no remote, so this is the LOCAL form.
[[run]]
cmd             = ["link", "--preview", "login"]
exit            = 0
stdout_contains = ["gg:///", "@main...feat/x"]

[[run]]
cmd             = ["link", "--preview", "login", "a.txt:4"]
exit            = 0
stdout_contains = ["/a.txt@main...feat/x:4"]

# resolve names the checkout, the PAIR, and the tip it resolved to.
[[run]]
cmd             = ["link", "resolve", "gg://{{cwd}}/a.txt@main...feat/x:4"]
exit            = 0
stdout_contains = ["preview main...feat/x", "a.txt", "new:4"]

[[run]]
cmd             = ["link", "resolve", "--json", "gg://{{cwd}}/a.txt@main...feat/x:4"]
exit            = 0
stdout_contains = ["\"source\":\"feat/x\"", "\"target\":\"main\"", "\"path\":\"a.txt\""]

# The link drives a write verb, and the note lands on the source tip — so
# --preview sees it too.
[[run]]
cmd  = ["note", "add", "gg://{{cwd}}/a.txt@main...feat/x:4", "--summary", "linked preview note"]
exit = 0

[[run]]
cmd             = ["note", "list", "--preview", "login", "--file", "a.txt"]
exit            = 0
stdout_contains = ["linked preview note", "active"]

# …and a read verb: the preview's patch, not the tip commit's own change.
[[run]]
cmd             = ["diff", "gg://{{cwd}}/a.txt@main...feat/x", "--hunks"]
exit            = 0
stdout_contains = ["a.txt", "1 @@ -"]

# gg show refuses a preview link and points at gg diff. (The harness asserts
# STDOUT only — `Run` carries stdout_contains / stdout_excludes and nothing for
# stderr, see e2e/scenario.go:110-119 — so the exit code is the assertion; the
# prose is pinned by TestShowRefusesAPreviewLink in internal/cli.)
[[run]]
cmd  = ["show", "gg://{{cwd}}/a.txt@main...feat/x"]
exit = 2

# The old side is the merge base: not addressable, refused at PARSE time.
[[run]]
cmd  = ["link", "resolve", "gg://{{cwd}}/a.txt@main...feat/x:old:1"]
exit = 2

# gg open with no live session in this checkout exits 1 (the e2e harness runs
# the CLI in-process, so cli.LaunchTUI is nil — no launcher is installed). The
# message itself is pinned by TestOpenWithoutASessionOrALauncherExitsOne.
[[run]]
cmd  = ["open", "gg://{{cwd}}/a.txt@main...feat/x:4"]
exit = 1

[expect]
branch = "feat/x"
clean  = true
```

- [ ] **Step 2: Run the scenario to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-links && ./test.sh e2e 2>&1 | tail -30`
Expected: with Tasks 1-8 complete the scenario should PASS on the first run —
it is a regression net over shipped behaviour, not a driver for new code. Before
moving on, confirm it was actually RUN (`grep s92_preview_links` in the output);
a scenario the harness never picked up is a silent pass. The harness asserts
STDOUT only (`e2e.Run` has `stdout_contains` / `stdout_excludes` and no stderr
key — `e2e/scenario.go:110-119`), which is why the two refusal runs above assert
their exit code alone.

- [ ] **Step 3: Update the skills and the docs**

**`internal/agentskill/agentskill.go`:**

```go
const Version = 73
```
```go
const ReviewVersion = 7
```

**`internal/agentskill/using-gg.md`** — in the link-grammar block (around line
103), add the three preview rows after the `gg://<repo>@<sha>` row:

```text
gg://<repo>@<target>...<source>            a merge preview: the Previews tab entry
gg://<repo>/<path>@<target>...<source>[:<line>]   a file (or new-side line) in that preview
gg://<repo>/<path>@<target>...<source>#<hunk>     a hunk of that preview's patch
```

Immediately after the block, add a paragraph:

```text
A preview link spells the branch PAIR, never a sha: the names travel between
machines, the machine-local preview id does not. Every verb that takes a link
treats a preview link exactly as `--preview` — `gg diff`, `gg note
add|list|apply|clear`, `gg session navigate`, `gg session highlight add`,
`gg link resolve`. `gg show` refuses one (use `gg diff`), and the preview's old
side is the merge base, so `:old:` is refused at parse time. Build one with
`gg link --preview <id|label|<target>...<source>> [<path>[:<line>]]`.

`gg open <link>` shows it to the user in their gg: it steers whatever gg
session is live in the link's checkout, or starts the TUI there positioned on
the link. Use it instead of launching `gg` yourself.
```

**`internal/agentskill/reviewing-with-gg.md`** — in the `## Workflow` block,
change step 7 and the paragraph below it:

```text
7. print `gg link --preview <P>` and hand that link back
```

and the last sentence of the merge-preview paragraph
("Report the preview's name back to the user so they can open it in their gg.")
becomes:

```text
When you are done, print `gg link --preview <id|label>` and hand THAT link back
to the user — they open it with `gg open <link>`, which steers their running gg
straight into the preview. A bare preview name only works on your machine; the
link works everywhere.
```

**Regenerate the dogfood copies** (ruling 12 — NEVER `gg init --update`):

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-links && cat > ./skillgen_tmp.go <<'EOF'
//go:build ignore

package main

import (
	"fmt"
	"os"

	"github.com/homeend/gigagit/internal/agentskill"
)

func main() {
	if err := os.WriteFile(".claude/skills/using-gg/SKILL.md", []byte(agentskill.UsingGG.SkillFile()), 0o644); err != nil {
		panic(err)
	}
	if err := os.WriteFile(".claude/skills/reviewing-with-gg/SKILL.md", []byte(agentskill.ReviewingWithGG.SkillFile()), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("regenerated both dogfood SKILL.md copies")
}
EOF
go run ./skillgen_tmp.go && rm ./skillgen_tmp.go
```

Verify the markers moved: `grep -o 'gg:using-gg:v[0-9]*' .claude/skills/using-gg/SKILL.md`
must print `gg:using-gg:v73`, and
`grep -o 'gg:reviewing-with-gg:v[0-9]*' .claude/skills/reviewing-with-gg/SKILL.md`
must print `gg:reviewing-with-gg:v7`. Confirm `skillgen_tmp.go` is gone
(`git status` must not list it).

**`README.md`** — in the link-grammar fence (lines ~254-272) add, after the
`gg://<repo>@<sha>` row:

```text
gg://<repo>@<target>...<source>            # a merge preview (branch names, never shas)
gg://<repo>/<path>@<target>...<source>:<n> # a file / new-side line in it
```

and, in the example block below it:

```bash
gg link --preview login a.go:12         # a link to a place in a merge preview
gg open  gg://gigagit/a.go@main...feat/login:12   # show it in the user's gg
```

**`CHANGELOG.md`** — insert a new bullet as the FIRST bullet under the
`## [Unreleased]` heading (anchor on that heading; the file contains a literal
`<<<<<<<` around line 1402 that is CONTENT, not a conflict):

```markdown
- **Preview links, steering into a preview, and `gg open`.** The `gg://`
  grammar gains a merge-preview form spelled as git's three-dot pair —
  `gg://<repo>@<target>...<source>`, plus the file, line and `#hunk` forms
  inside it. Branch NAMES, never shas: they travel between machines, the
  machine-local preview id does not. Every link-taking verb treats a preview
  link exactly as `--preview` (`gg diff`, `gg note add|list|apply|clear`,
  `gg session navigate`, `gg session highlight add`, `gg link resolve`);
  `gg show` refuses one and points at `gg diff`, and `:old:` is refused at
  parse time because a preview's old side is the merge base. Producers:
  `gg link --preview P [<path>[:<line>]]`, the TUI's `L` / `.`-menu "Copy link"
  on the Previews panel and inside a preview's file list and diff, and the
  session snapshot's `cursor.link`. `gg session navigate <preview link>` steers
  a running TUI or `gg web` page INTO the preview (opening a show-once preview
  when none is saved) through a new `target.state = "preview"` wire value that
  carries the pair, not a sha. And **`gg open <link>`** shows a link to the
  user: it steers a live gg session in the link's checkout, or launches the TUI
  there positioned on the link (`--at`, consumed once the first snapshot has
  loaded). Skills: using-gg v73, reviewing-with-gg v7.
```

**`docs/CLAUDE-details.md`** — append a short paragraph to the links section
(grep `gg://` to find it):

```markdown
### Preview links (feature B, 2026-09-15)

`model.LinkTarget.Preview *LinkPreview{Source, Target}` is set iff the target
text carries git's three-dot pair; `State` stays `StateCommitted` with an EMPTY
`Commit`, because which commit a preview addresses is a per-machine question.
`Link.Address()` is therefore meaningless for a preview link — the resolver
(`finishLink`, the ONLY `.Address()` caller on a `model.Link` in the tree)
branches first, calls `PreviewNotes` on a checkout that holds BOTH branches, and
fills `Resolved.Preview` plus an `Addr` on the source tip. `internal/cli`'s
`previewTargetFromLink` is the one adapter onto the `--preview` code path, so a
link and the flag cannot diverge; `linkDiffSpec` returns the preview's own patch
so hunk numbers agree. The steer wire's `target.state = "preview"` carries
`source`/`target` and an EMPTY `commit`: the consumer resolves the tip itself,
so a tip that moves between post and apply is honoured. The TUI parks a
`steerStagePreview` pending drained by the `compareFilesMsg` handler (a
preview's file list is a compare, not a commit file list, so there is no hash to
gate on — the open pair is the gate). `gg open` reaches the TUI through the
`cli.LaunchTUI` seam that `cmd/gg`'s `launchTUI` installs, keeping
`internal/cli` free of an `internal/tui` import.
```

**`CLAUDE.md`** — the `steer` package row only:

```markdown
| `steer`      | Live-steering protocol leaf: a per-worktree file inbox (presence with mtime liveness, temp+rename command/reply files, an fsnotify wake) that `gg session` uses to drive a running TUI or `gg web` page. Targets name the working tree, the index, a commit, or a MERGE PREVIEW (a branch pair; the consumer resolves the tip). stdlib + fsnotify only; `tui`/`cli`/`web`/`mcp` import it directly. |
```

- [ ] **Step 4: Run the full suite**

Run:
```
cd /mnt/t/others/gigagit.worktrees/feat-preview-links && ./test.sh 2>&1 | tail -40
```
Expected: PASS at every stage (vet+gofmt, unit, e2e). The
`internal/agentskill` staleness tests must be green with the bumped versions.

Then run the race gate in the foreground:

```
cd /mnt/t/others/gigagit.worktrees/feat-preview-links && ./test.sh race 2>&1 | tail -40
```
Expected: PASS.

Finally, build a verify binary for the user:

```
cd /mnt/t/others/gigagit.worktrees/feat-preview-links && go build -o ./gg ./cmd/gg && ./gg --version
```

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-links && git add internal/agentskill/ .claude/skills/ CHANGELOG.md README.md docs/CLAUDE-details.md CLAUDE.md e2e/scenarios/s92_preview_links.toml && git commit -m "docs: preview links in the skills, README, CHANGELOG and an e2e scenario

using-gg v73 teaches the three preview link rows and gg open;
reviewing-with-gg v7 ends its workflow by handing back a gg link --preview.
Both dogfood SKILL.md copies regenerated from agentskill (never via
gg init --update). s92_preview_links covers the producer, the resolver's
text and JSON output, a link driving note add onto the tip, gg show's
refusal, the :old: parse refusal and gg open with no session.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

## Self-review

**1. Spec coverage (§2)**

| Spec requirement | Task |
|---|---|
| §2.1 grammar: three preview rows, branch names, `:old:` refusal | 1 |
| §2.1 `LinkTarget.Preview`, `String()` writes the pair verbatim | 1 |
| §2.1 `Resolved.Preview` = the tip resolved on the chosen checkout; `Addr` = the tip commit address | 2 |
| §2.2 producer: TUI Previews row `.`-menu "Copy link" | 6 |
| §2.2 producer: preview diff `L` / "Copy link" → file/line form | 6 |
| §2.2 producer: `cursor.link` is the preview form while a preview is open | 6 (falls out of `contextLinkText`; asserted in `TestPreviewDiffCopiesTheFileLineForm`) |
| §2.2 producer: `gg link --preview P [<path>[:<line>]]` | 5 |
| §2.3 `gg diff`, `gg note add\|list\|apply\|clear`, `gg link resolve` accept the preview form and behave as `--preview` | 4 |
| §2.3 `gg show` refused with "use gg diff" | 4 |
| §2.3 `gg session navigate <preview link>`; the wire gains `target.state = "preview"` with the names | 3 + 5 |
| §2.3 the TUI opens the Previews entry, creating a show-once preview when none is saved, then the file and line | 7 |
| §2.3 the web page switches to the preview stage the same way | 9 |
| §2.3 attention marks key on the tip, so `highlight add` works in a preview | 4 (`linkDiffSpec`) + 5 (test) |
| §2.3 `gg open <link>`: steer a live session, else launch the TUI with `--at`; exit 1 on an unresolvable link | 8 |
| §2.4 skill text + version bumps | 10 |
| §2.5 `ParseLink`/`String` round trips, remote names, `:old:` and sha-pair refusals | 1 |
| §2.5 `ResolveLink` on a two-checkout registry; ambiguity/unknown as today | 2 |
| §2.5 each CLI consumer: link vs `--preview` produce the same output/notes (table-driven) | 4 |
| §2.5 `gg open` against a fake steer inbox; the `--at` startup navigate unit-tested in the TUI | 8 |
| §2.5 TUI/web copy producers through the clipboard seam; snapshot `cursor.link`; navigate via the inbox | 6 + 7 |
| §2.5 e2e `s92_preview_links.toml` | 10 |
| §3 out of scope: web link production, a TUI paste field, old-side preview notes, an OS URL handler | not implemented anywhere; ruling 10 keeps `links.js` untouched and Task 9's test asserts only consumption |

No gaps.

**2. Placeholder scan**

No "TBD", "similar to Task N", "add error handling", "write tests for the
above", or code step without a code block. Every helper a test leans on is
either pinned to its existing definition (`runCLIStdin`
`internal/cli/batch_test.go:13`, `gitOut` `internal/cli/link_test.go:412`,
`previewRepo` `internal/cli/preview_test.go:11`, `newCLIRepo`
`internal/cli/worktree_test.go:16`, `mergePreviewModel`
`internal/tui/preview_panel_test.go:19`, `drainMsgs` / `openMergePreview`
`internal/tui/preview_open_test.go`, `steerModel` `internal/tui/steer_test.go:20`,
`livePresence` `internal/cli/session_test.go:22`, `buildSessionSnapshot`
`internal/tui/session_snapshot.go:182`) or written out in full here
(`openCLIService`, `readOneReply`, `previewSteerModel`, `previewLinkFor`,
`linkRepoWithBranch`, `mustLink`).

**3. Type consistency**

- `model.LinkPreview{Source, Target string}` — Tasks 1, 2, 5, 6, 8 all use the
  same field names and the same `<target>...<source>` render order.
- `domain.Resolved.Preview *domain.PreviewNoteSet` — Tasks 2, 4, 5, 8.
- `previewTargetFromLink(res domain.Resolved) (previewTarget, bool)` — defined
  in Task 4, used in Tasks 4 and 5 with that exact signature.
- `navigateCommandFor(ctx, svc, res) (steer.Command, error)` + `navExit(verb,
  err, stderr) int` + `errNavLinkRepoOnly` / `errNavLinkNoLine` — defined in
  Task 5, used in Tasks 5 and 8.
- `steer.Target.Source` / `.Target` (json `source`/`target`) — Tasks 3, 5, 7, 8,
  9 agree, and `Commit` stays empty for a preview everywhere.
- `steerCommandForLink(l model.Link) (steer.Command, bool)` (tui, pure, refuses
  `Hunk > 0`) pairs with `openAtLink(res, c)` (cli), which lowers the hunk — the
  two halves of one contract, stated in both tasks' Interfaces blocks.
- `previewLinkFor(source, target, path string, line int)` (Task 6) takes no
  side parameter: a preview link is always new-side, which matches Task 1's
  parse refusal of `:old:`.
- `steerStagePreview` + `pendingSteer.source/.target` + `drainPendingPreview` +
  `previewStateReason` + `pendingPreviewFor` — all defined in Task 7 and used
  only there and in `model.go`'s `compareFilesMsg` arm (same task).
- `cli.LaunchTUI func(checkout string, at model.Link) int` and
  `tui.Run(svc, recordPath string, at model.Link)` — Task 8 defines both and
  `cmd/gg`'s `launchTUI(dir, at, recordPath, cwdFile) int` bridges them.
