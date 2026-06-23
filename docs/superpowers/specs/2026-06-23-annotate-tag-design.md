# Annotate an Existing Tag (GitKraken parity, Tags-C) — Design

**Date:** 2026-06-23
**Status:** Approved, ready for plan

## Goal

Let a user turn an existing tag into an annotated tag (or change an annotated
tag's message) — `git tag -a -f -m <msg> <tag> <target>` — from the Tags panel
`.` menu (TUI) and the CLI (`gg tag annotate <tag> -m <msg>`). gg can create
annotated tags at creation but cannot annotate an existing one. This is the last
item of the GitKraken tag-menu gap analysis.

## Background

`engine.CreateTag{Name, Commit, Message}` creates a tag; a non-empty `Message`
makes it annotated. It cannot re-tag an existing name (git refuses without
`-f`). "Annotate" is exactly "force-recreate this tag as annotated, at the same
target commit it already points to". The operation is local-only (no remote
push), reversible, and consistent with local tag create/delete having no
confirm — so no Decider confirm.

## Decisions (from brainstorming)

- **Mechanism:** add `Force bool` to `engine.CreateTag` (and the `git.CreateTag`
  verb). Annotate = `CreateTag{..., Message: msg, Force: true}` at the tag's
  current target.
- **CLI:** a new `gg tag annotate <tag> -m <msg>` subcommand.
- **TUI message popup:** required, non-empty message; prefilled with the tag's
  current subject (`model.Tag.Subject`; blank for a lightweight tag).
- **Target:** preserved. The TUI passes the in-model `tag.Target`; the CLI
  resolves it via `svc.RevParse(tag + "^{commit}")`.

## Architecture & components

### Git verb — `git.Repo.CreateTag` gains `force`

`internal/git/mutate.go`:

```go
// CreateTag creates a tag at commit (empty commit = HEAD). A non-empty message
// makes it annotated (git tag -a -m); force (-f) replaces an existing tag.
func (r *Repo) CreateTag(ctx context.Context, name, commit, message string, force bool) error {
	argv := gitcmd.New("tag").
		ArgIf(message != "", "-a", "-m", message).
		ArgIf(force, "-f").
		Arg(name).
		ArgIf(commit != "", commit).
		ToArgv()
	_, err := r.Runner.Run(ctx, "git tag", argv)
	return err
}
```

The single production caller (`engine/create_tag.go`) and ALL direct verb test
callers update to pass the new `force` arg (`false`). Direct `repo.CreateTag(...)`
callers (verified): `internal/git/tag_create_test.go` (×2),
`internal/engine/checkout_test.go` (×2), `internal/engine/delete_tag_test.go`
(×1). Op-literal `CreateTag{...}` constructors are unaffected (adding a struct
field doesn't break keyed literals).

### Engine op — `engine.CreateTag` gains `Force`

`internal/engine/create_tag.go`: add `Force bool` to the struct; pass `op.Force`
to `deps.Repo.CreateTag`. Behaviour otherwise unchanged (existing callers leave
`Force` zero = false). When `Force && Message != ""` the Summary reads
"annotated tag <name>".

### TUI — Tags `.` menu + message popup

`internal/tui/tags_actions.go` — `tagAnnotateRow` (id `tag-annotate`, label
`"Annotate " + name`), gated on a tag selected + `opsIdle`, opens the popup.

`internal/tui/annotate_tag_popup.go` (new) — modelled on `renameBranchPopup`
(single text field, implements the layer `update`/`render`):

```go
type annotateTagPopup struct {
	tag     string    // the tag being annotated (fixed)
	target  string    // its current commit, preserved
	message textfield // prefilled with the tag's current subject
}
```

- `openAnnotateTagPopup` seeds `message: newTextField(tag.Subject)`, `target:
  tag.Target`.
- `update`: `esc` cancels; `enter` with a non-empty message runs
  `startOp(engine.CreateTag{Name: tag, Commit: target, Message: msg, Force:
  true})` (empty message keeps the popup open — annotate requires a message);
  the message field accepts spaces (route the key to `message.HandleEditKey`,
  mirroring `tagPopup`'s message handling — do NOT drop space the way name
  popups do).
- `render`/`box` mirror `renameBranchPopup` ("Annotate tag <tag>", the message
  field, footer "[type] message  [enter] annotate  [esc] cancel").

Wired in `availableActions` beside the other tag rows.

### CLI — `gg tag annotate <tag> -m <msg>`

`internal/cli/tag.go`: add a `case args[0] == "annotate":` to `cmdTag`'s switch
(and list it in the unknown-subcommand help). `cmdTagAnnotate`:

```go
func cmdTagAnnotate(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tag annotate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	msg := fs.String("m", "", "annotation message (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "usage: gg tag annotate <name> -m <message>")
		return 2
	}
	if *msg == "" {
		fmt.Fprintln(stderr, "tag annotate: -m <message> is required")
		return 2
	}
	name := fs.Arg(0)
	target, err := svc.RevParse(context.Background(), name+"^{commit}")
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	res, err := runOperation(context.Background(), svc,
		engine.CreateTag{Name: name, Commit: target, Message: *msg, Force: true}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}
```

(`cmdTag` keeps its current signature — annotate has no Decider, so no `stdin`
needed, unlike `tag rm --remote`.)

## Error handling

- **Empty/missing message** (TUI: popup stays open; CLI: usage, exit 2) — annotate
  must add a message.
- **No such tag** (CLI `RevParse` fails) → error, exit 1.
- **git failure** → propagates as the op error; TUI status / CLI non-zero exit.

## Testing

- **git verb** (`internal/git/tag_create_test.go`): force argv asserts `tag -a
  -m <msg> -f <name> <commit>`; real-git — create a lightweight tag, then
  `CreateTag(name, target, msg, true)`, assert `git cat-file -t <name>` == `tag`
  (now annotated) and the target commit is unchanged.
- **engine** (`internal/engine/create_tag_test.go`): `Force:true` re-tags an
  existing name (no "already exists" error); `Force:false` still refuses a
  duplicate (existing behaviour).
- **TUI** (`internal/tui/tags_actions_test.go` / a popup test): `tag-annotate`
  row present when a tag is selected + idle; `openAnnotateTagPopup` prefills the
  message with `tag.Subject`; submitting a non-empty message dispatches
  `CreateTag{Force:true, Commit: tag.Target}`; empty message keeps the popup
  open (no op).
- **CLI** (`internal/cli/tag_test.go`): `gg tag annotate v1 -m "msg"` on a repo
  with a lightweight `v1` turns it annotated (`git cat-file -t v1` == `tag`),
  preserving its target; missing `-m` exits 2; unknown tag exits 1.
- **e2e** (`e2e/scenarios/s74_tag_annotate.toml`): full-stack smoke — a
  lightweight tag, `gg tag annotate <tag> -m <msg>` exits 0, the tag still
  exists. (The declarative harness asserts tag *names*, not annotated-ness, so
  annotated-ness is owned by the CLI/verb integration tests above.)

## Files

- Modify: `internal/git/mutate.go` (+force) + verb test callers in `internal/git/tag_create_test.go` (×2), `internal/engine/checkout_test.go` (×2), `internal/engine/delete_tag_test.go` (×1) — all pass `false`.
- Modify: `internal/engine/create_tag.go` (+Force) + `internal/engine/create_tag_test.go`.
- Modify: `internal/tui/tags_actions.go` (+ `tagAnnotateRow`), `internal/tui/action_menu.go` (wire it); Create: `internal/tui/annotate_tag_popup.go`; Modify: `internal/tui/tags_actions_test.go` (+ a popup test file or cases).
- Modify: `internal/cli/tag.go` (+ `annotate`) + `internal/cli/tag_test.go`.
- Modify: `internal/agentskill/using-gg.md` + bump `internal/agentskill/agentskill.go` `Version`, then `gg init --update`.
- Create: `e2e/scenarios/s74_tag_annotate.toml`.
- Modify: `CHANGELOG.md`, `README.md`.

## Non-goals

- Remote impact (annotate is local; a previously-pushed tag would diverge from
  the remote — the user re-pushes if they want, via `gg tag push`).
- Editing the full multi-paragraph annotation body (single-line message, like
  the create popup).
- Hide/solo tags, copy link.
