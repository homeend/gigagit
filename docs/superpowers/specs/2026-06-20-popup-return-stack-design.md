# Popup return-to-parent + paste-dest prefill — design

**Date:** 2026-06-20

## Problem

1. When the bookmark (`g`) / shelf (`G`) switcher opens a child popup — **paste**
   (`p`), **restore** (`p`), or the **remove** (`x`) confirm — and that child is
   dismissed (esc) or completes, the user lands on the panels, not back on the
   switcher. They want: a child popup, when closed, returns to its parent
   switcher (with its filter/selection intact), and the same after a successful
   action.
2. The "Paste bookmarked file to a new path" popup starts with an empty
   destination; it should be prefilled from the bookmark's path with a
   `_RESTORED` marker.

## Part 1 — return to the parent switcher

### Mechanism (mirrors the existing `reopenConflict` / `pendingSwitch` intent idiom)

- **`popupReturn []func(Model) (Model, tea.Cmd)`** on `Model`: a LIFO stack of
  restore closures. Real nesting is one level deep (switcher → child); a slice
  keeps it general and matches the existing surface-stack style.
- **`reopenAfterOp bool`** on `Model`: one-shot flag meaning "the running op was
  launched from a popup child — pop+apply `popupReturn` when it finishes."

### Flows

- **Open a child (paste / restore):** capture the live `*bookmarkPopup` /
  `*shelfPopup` pointer, push `func(m){ m.<switcher> = saved; return m, nil }`,
  then nil the switcher and set the child popup. Re-pointing to the *saved
  struct* preserves filter/selection/mark (paste/restore don't mutate the list).
- **Child esc:** nil the child, then pop+apply the top of `popupReturn` →
  switcher reappears unchanged. If the stack is empty, behave as today.
- **Child success (the `WriteFile` op):** nil the child, set `reopenAfterOp =
  true`, `startOp(WriteFile)`. In `opFinishedMsg`, **after** the existing
  `switchTo`/`chainSwitch` early-returns, on **both** the success and error
  branches: if `reopenAfterOp`, clear it, pop+apply `popupReturn`, and batch
  `loadCmd()` so the panels behind refresh. (`dataLoadedMsg` does not nil these
  popups — verified — so the restored popup survives the batched reload.)
- **Remove (`x`) confirm modal:** success already reopens the switcher (it
  reloads, because the list changed) — unchanged. Add the **Cancel** branch of
  the modal's `onResolve` to reopen the switcher via `loadBookmarksCmd` /
  `loadShelfCmd`. (Remove uses reload, not pointer-restore, because the list
  changed; so it does not use `popupReturn`.)

### Scope

- Covers **paste**, **restore**, **remove-confirm**.
- **Out:** `m` / `c` / enter **compare** actions open a full-screen diff via
  `openPickerDiff` (clears the surface stack); esc there returns to the panels by
  design. Not a popup; left as-is.
- Stack depth is 1 in practice; no general N-deep popup framework is built.

## Part 2 — prefill the paste destination

New pure helper `restoredPath(p string) string` (uses the `path` package, `/`
separators, matching repo-relative bookmark paths):

- **Dotfile** — basename starts with `.` (`.gitignore`, `.env.local`): append
  `_RESTORED` to the whole path → `.gitignore_RESTORED`.
- **Has an extension** — last `.` in the basename at index > 0 (`config.go`):
  insert before it → `config_RESTORED.go`.
- **No extension** (`Makefile`): append → `Makefile_RESTORED`.

`bookmarkPastePrompt` sets `bookmarkPastePopup.dest = restoredPath(b.Path)` so the
field opens prefilled (still fully editable; the mandatory-dest guard is now
satisfied by default). Scoped to the **bookmark paste** popup only — the shelf
restore popup keeps its deliberate "not prefilled" behavior.

## Testing

- `restoredPath`: `config.go`→`config_RESTORED.go`; `a/b/config.go`→
  `a/b/config_RESTORED.go`; `Makefile`→`Makefile_RESTORED`; `.gitignore`→
  `.gitignore_RESTORED`; `.env.local`→`.env.local_RESTORED`.
- paste esc → `bookmarkPopup` restored with its filter preserved.
- paste success → simulated `opFinishedMsg{}` restores `bookmarkPopup` and clears
  `reopenAfterOp`; error branch restores too.
- shelf restore esc → `shelfPopup` restored.
- remove-cancel → switcher reopened.
- paste popup opens with `dest` prefilled.
