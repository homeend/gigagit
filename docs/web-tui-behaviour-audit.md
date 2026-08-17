# Web ↔ TUI behaviour audit

`docs/web-tui-parity.md` answers *"is the operation wired?"*. It does not
answer *"does it behave the same?"* — and that is where the web has drifted:
a feature can be present, pass its tests, and still ask a different question,
gate on a different condition, or invent a step the TUI does not have.

This file records the second question, per feature, with the evidence.

**Method** (repeatable — do this before claiming a feature matches):

1. Find the TUI's implementation: the row builder or key handler
   (`internal/tui/commit_scope.go`, `model.go`'s key switch, the `*_popup.go`
   files) — that is the specification.
2. Read three things: **when the action is offered** (the gate), **what it
   asks** (confirm text and option labels, verbatim), and **what it does
   after** (chained operations, refreshes).
3. Compare against the web's `internal/web/static/*.js` row and its
   `internal/web/ophttp.go` case.
4. Record a delta even when the web is *safer* — an extra confirm is still a
   difference, and the user should decide, not the implementer.

Audited 2026-08-16 against `web-dev`. **Checked** areas are listed below;
everything not listed has NOT been compared yet.

## Fixed since this audit began

| Feature | Was | Now |
|---|---|---|
| Create tag from a commit | Web asked the name, then sprang a second prompt for an annotation prefilled with the subject, with a "no message (lightweight)" button | One dialog, name + message, tab between them, empty message = lightweight — the TUI's `tagPopup` |
| Push the current branch | Web pushed silently, leaving unpushed tip tags behind | 5s-budgeted check, then the TUI's *Push branch + tags* / *Push branch only* / *Cancel*, tags chained after a successful push |
| A tag added to a loaded commit | Invisible in the commit list until the server restarted | The refresh takes fresh rows for the overlap, so decorations appear |
| Reset to a commit | Straight to the engine's soft/mixed/hard picker | Asks *"Reset to `<sha>`? This moves the current branch ref."* first, as the TUI does |
| Fast-forward to a commit | Row always offered, then refused by the engine | Hidden when the commit is conclusively not ahead (the TUI's `feedDescendant` walk, ported), and it confirms *"Fast-forward to this commit?"* |

| Manual refresh (☰ / `r`) | Silent, and a RECONCILING reload | Shows *⏳ reloading…* then *reloaded*, and starts the list clean like the TUI's `r` (`hardFeed`) |

## Open deltas

| # | Feature | TUI | Web | Verdict |
|---|---|---|---|---|
| 1 | **Cherry-pick a commit** | Runs immediately; only gates merge commits (`cannot cherry-pick a merge commit`) | Shows a local confirm first | Web is more cautious; **differs** |
| 2 | **Revert a commit** | Runs immediately; gates merge commits | Shows a local confirm first | Same class as #1 |
| 3 | **Undo last commit** | Runs immediately on `u` | Shows a confirm | Web asks more |
| 4 | **Bookmark / shelf a commit** | Name popup prefilled with the subject, **`ctrl+s` inserts the short sha** | Name prompt prefilled with the subject; no sha-insert key | Missing affordance |

None of these is a bug in the sense of "produces the wrong result". They are
places where the two frontends ask different questions about the same
operation, which is exactly what makes the web feel unfamiliar to someone who
knows the TUI.

## Checked and matching

- **Push a named branch** (branch menu): neither frontend runs the tip-tag
  check — that lane is not the current branch in either.
- **Delete a branch**: both rely on the engine's own confirm + unmerged fork.
- **Delete a tag**: both add a local confirm (the engine's op is decision-free).
- **Stash drop**: both confirm locally.
- **Discard**: both confirm, and both name what is lost.
- **Reset to a remote tip**: both add a local confirm before the preset-hard op.
- **Reword**: both prefill with the commit's current full message.
- **Restore a shelf entry**: both prefill the original path and let the
  engine's overwrite decision park.

## Known simplification

The TUI's confirms for slow operations honour `[ui] disable_slow_op_confirm`;
the web does not read that setting yet, so it always asks. Someone who turned
the confirms off in the TUI still gets them in the browser.

## How to keep this honest

Every new web feature should cite the TUI function it mirrors in its commit
message, and any deliberate divergence belongs here with a reason. A test
proves the web does *something*; only this comparison proves it does the
*same* thing.
