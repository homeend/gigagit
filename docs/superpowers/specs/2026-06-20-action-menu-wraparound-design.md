# Action-menu wraparound navigation — design

**Pipeline #10 (small).** In the `.` context action menu, wrap the selection at
the ends: up-arrow on the first row goes to the last; down-arrow on the last goes
to the first.

## Change

`actionMenu.move(d)` (`internal/tui/action_menu.go`) currently clamps the
selection at `0` and `n-1`. Replace the clamp with a modulo wrap that handles
negative deltas and an empty list:

```go
func (a *actionMenu) move(d int) {
	n := len(a.visible())
	if n == 0 {
		a.sel = 0
		return
	}
	a.sel = ((a.sel+d)%n + n) % n // wrap, handling negative d
}
```

`visible()` (the filtered row set) is the length basis, so wraparound respects an
active `/` filter. No key-handler change — `move(-1)`/`move(+1)` already back the
up/down keys.

## Testing

`TestActionMenuMoveWraps`: up from the first row → last; down from the last →
first; an ordinary middle move still advances by one.

## Out of scope

Wraparound in other list surfaces (panels, pickers) — this is scoped to the `.`
action menu, which is what the user asked for.
