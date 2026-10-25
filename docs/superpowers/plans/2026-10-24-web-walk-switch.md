# Web walk switch + alt+w — plan

Spec: docs/superpowers/specs/2026-10-24-web-walk-switch-design.md

1. **walk.js pure decisions** (test: `TestWalkSwitchJS`, node via jsFunc)
   - `switchTarget(dir, page, order)` → dir when dir ≠ page and listed, else "".
   - `returnPlan(ret, page, order)` → `{}` (none / already here), `{path}`,
     or `{msg: "<ret> is gone — showing <page>"}`.
   - `wtCycle(order, page, covered, hidden)` → `{msg}` (first press over
     windows / one worktree) or `{path, msg}` with the TUI's lines.
2. **Carry through the reload** (ops.js): `switchCarry` may be an object →
   `ssSet("gg-open-console", JSON)`; a string stays the tour key. console.js
   `openPendingConsole()` at boot after `openPendingTour()` (app.js).
3. **console.js**: `openConsole(id, {line})` switches through `doReroot` when
   `switchTarget` says so (op running → open here + the TUI's refusal line);
   `con.ret` kept across replacing consoles; `leaveConsole()` = the own
   closes (esc, ctrl+] twice, gone, removed) → `returnPlan`; give-way
   closes stay `closeConsole()`. Walk lines passed as `line`.
4. **alt+w** in `walkKey` (KeyW): `wtCycle`; first press closes the console
   (no return), viewer (background), history, blame; ignored under any
   other layer; op running → refusal line; else `doReroot(path, {line})`.
   Test pins: `TestWalkSwitchWired`.
5. Help row (index.html / console.js registerHelp), CHANGELOG.
6. Probe (Chromium + Firefox), review subagent, gofmt, race gate, binary.
