# Commit message view (`i`) + open in editor (`I`) — Design

## Goal

In the Commits panel, give a one-key way to read a commit's full message:

- **`i`** opens the selected commit's full message (subject + body) in a
  **scrollable popup**.
- **`I`** opens the same message in the **external editor** (`$EDITOR`,
  read-only).

The Commit subject is already visible in the panel, but the body (the part
after the blank line) is not — long messages, trailers, and multi-paragraph
descriptions are currently only reachable by leaving the TUI.

## Scope

- **v1: the Commits panel only.** Files-view commit-list parity (after `l`) is
  a deferred follow-up.
- WIP pseudo-rows (◇ Working tree / ◇ Staged) are not real commits → both keys
  no-op on them.

## Reused plumbing (nothing new in lower layers)

- `domain.Service.CommitMessage(ctx, rev) (string, error)` already exists
  (backs the reword pre-fill) — runs `git log -1 --pretty=%B` under a Read
  reservation. Returns the **raw** `%B`, i.e. with a trailing newline (NOT
  trimmed).
- `contentPopup` is the scrollable text-popup layer
  (`newContentPopup(title, []contentLine)`, pushed via `pushLayer`; it already
  scrolls, pages with pgup/pgdn, and has `/` search). `fileContentLines(data
  []byte) []contentLine` splits bytes into rows.
- `openInEditorCmd(name, resolve func(context.Context) ([]byte, error))` writes
  the resolved bytes to a read-only temp file and yields an `editorViewMsg`
  whose handler runs the editor and **skips the working-tree status reload**
  (the distinct `editorView*` msg pair, not `editorFinishedMsg`).
- Selection: gate `m.focus == panelCommits && m.canShowCommitFiles()`, skip WIP
  via `isWipRow(m.commitSelUnified())`, then `backingIndex(panelCommits)` →
  `m.commits[bi]` — exactly the `l` handler's pattern.

## Behaviour

### `i` — scrollable message popup

1. Resolve the selected commit (gating above). On a WIP row, no-op.
2. Push a loading `contentPopup` titled **`"Commit <short7> message"`**
   (`<short7>` = first 7 chars of the full hash).
3. Return `loadCommitMessageCmd(hash, short7)`: off the UI thread, call
   `svc.CommitMessage`, `strings.TrimRight(msg, "\n")` (drops the trailing
   blank row), `fileContentLines([]byte(...))` → `commitMessageMsg{short, lines,
   err}`.
4. The `commitMessageMsg` handler finds the popup via
   `layerOf[*contentPopup](m)` and **byte-matches `cp.title == "Commit "+short+"
   message"`** before filling. A nil/mismatched popup → drop the load (the user
   closed it or switched to a different commit). On error → fill with
   `"(load failed: <err>)"`. This hash-gate is the same staleness guard as
   `fileContentLayerMsg`.

A dedicated `commitMessageMsg` type is used — `fileContentLayerMsg` is NOT
overloaded (its tag is a path, ours is a hash).

### `I` — message in external editor

1. Resolve the selected commit (same gating).
2. Return `m.openInEditorCmd("COMMIT_EDITMSG", func(ctx) ([]byte, error) { s,
   err := svc.CommitMessage(ctx, hash); return []byte(s), err })` — the
   **untrimmed** bytes (the editor shows the message as git stores it). The
   `COMMIT_EDITMSG` name nudges editors into git-commit-message highlighting.

### `.` action-menu rows (discoverability)

In the Commits-panel action menu, when a real commit is selected, add:

- **"View message"** → same as `i`.
- **"Open message in editor"** → same as `I`.

Both reuse the same helper that the keys call (single source of truth), so the
gating/WIP rules live in one place.

## Error handling

- Resolve failure (`CommitMessage` errors): `i` fills the popup with
  `"(load failed: …)"`; `I` sets `statusMsg = "view: …"` via the existing
  `editorViewMsg` error path.
- Empty message (possible for a malformed commit): popup shows a single blank
  row; harmless.

## Testing

All UI tests route through `Update` (a cmd-non-nil assertion would miss the
tag-gate mismatch, which is the real failure mode):

- `i` press on a real commit → run the returned cmd through `Update` → assert
  the top `contentPopup`'s lines contain the body text.
- Tag-gate: deliver a `commitMessageMsg` whose `short` does NOT match the open
  popup → assert the popup is unchanged (no wrong-commit fill).
- `I` press → mirror `open_external_test.go`: the resolve closure returns the
  message bytes; assert an `editorViewMsg` with those bytes' temp file.
- Guards: `i`/`I` on a WIP row and with non-Commits focus → no-op (no layer
  pushed, no cmd).

## Docs

- `CHANGELOG.md` — Added entry for `i`/`I`.
- Help (`?` content) + footer — advertise `i` (view message) and `I` (message
  in editor) on the Commits panel.
- TUI-only: no CLI / agentskill change.
