# Task 3 Report: TUI Annotate tag action + message popup

## Changes

- **Created** `internal/tui/annotate_tag_popup.go` — the `annotateTagPopup` struct with
  `openAnnotateTagPopup()`, `update()`, `render()`, and `box()`, following the
  `renameBranchPopup` layer pattern exactly.
- **Modified** `internal/tui/tags_actions.go` — added `tagAnnotateRow()` helper at the
  top of the file (before `tagCheckoutRow`), id `"tag-annotate"`, label `"Annotate <name>"`.
- **Modified** `internal/tui/action_menu.go` — wired `tagAnnotateRow` into
  `appendCommitContextRows`, inserted after `tagPushRow` (before `graphWindowRows`).
- **Modified** `internal/tui/tags_actions_test.go` — added `tea` import +
  four new tests: `TestTagAnnotateRowPresent`, `TestAnnotatePopupPrefillsSubject`,
  `TestAnnotatePopupEmptyMessageKeepsOpen`, `TestAnnotatePopupSubmitDispatches`.

## Test command and output

```
go test ./internal/tui/ -run 'TestTagAnnotate|TestAnnotatePopup' -v
```

```
=== RUN   TestTagAnnotateRowPresent
--- PASS: TestTagAnnotateRowPresent (0.00s)
=== RUN   TestAnnotatePopupPrefillsSubject
--- PASS: TestAnnotatePopupPrefillsSubject (0.00s)
=== RUN   TestAnnotatePopupEmptyMessageKeepsOpen
--- PASS: TestAnnotatePopupEmptyMessageKeepsOpen (0.00s)
=== RUN   TestAnnotatePopupSubmitDispatches
--- PASS: TestAnnotatePopupSubmitDispatches (0.00s)
PASS
ok      github.com/gigagit/gg/internal/tui      0.021s
```

Full suite: `go test ./internal/tui/` — ok (12.3s).

## Space in the message field

The `update()` method has no `case tea.KeySpace:` — space falls into the `default`
branch and is forwarded to `p.message.HandleEditKey(msg)`, exactly as `tag_popup.go`
does for its `message` field (proven by the shipped code). The `renameBranchPopup`'s
`case tea.KeySpace: // drop` was intentionally NOT copied.

## Deviations from brief

None. The implementation matches the brief verbatim.

## Concerns

None.
