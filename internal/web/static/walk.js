// walk.js — the TUI's session walks (console.go cycleSessionsIn,
// worktree_walk.go walkWorktree) as pure functions over the page's session
// list: alt+a / alt+t (agents / terminals of the repository), alt+A / alt+T
// (the viewed worktree's), alt+q / alt+Q (the viewed worktree's screens).
// console.js runs them; nothing here touches the DOM.

function wtBase(p) {
  return String(p).split(/[\\/]/).filter(Boolean).pop() || String(p);
}

// walkRing is the ring: running sessions of the kind in the Worktrees list's
// order (order: its row paths, top to bottom), then by start, then by id;
// dir keeps one worktree's. A session in no listed worktree is another
// repository's and stays out.
function walkRing(sessions, order, terminal, dir) {
  const at = (p) => order.indexOf(p);
  return (sessions || [])
    .filter((s) => s.state === "running" && !!s.terminal === terminal && at(s.worktree) >= 0 && (!dir || s.worktree === dir))
    .sort((a, b) => at(a.worktree) - at(b.worktree) || Date.parse(a.started) - Date.parse(b.started) || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
}

// walkNext picks the session to show: {i}, {msg} (the TUI's status line) or
// {} (a scoped walk with none: nothing). shown/focused: the console on
// screen and whether it has the keys; viewedAt: the viewed worktree's row.
function walkNext(list, shown, focused, viewedAt, idxOf, terminal, scoped) {
  if (!list.length) {
    if (scoped) return {};
    return {
      msg: terminal
        ? "no running terminal in this repository — open one from the . menu of a worktree or a checked-out branch"
        : "no running agent session in this repository — start one from the . menu of a worktree or a checked-out branch",
    };
  }
  const s = list.findIndex((x) => x.id === shown);
  if (s >= 0 && focused && list.length === 1) {
    const what = terminal ? "terminal" : "agent session";
    return { msg: scoped ? "the only running " + what + " in this worktree — already bound" : "the only running " + what + " in this repository — already focused" };
  }
  if (s >= 0 && focused) return { i: (s + 1) % list.length };
  for (let i = 0; i < list.length; i++) if (idxOf(list[i].worktree) >= viewedAt) return { i };
  return { i: 0 };
}

function walkLine(info, i, n, terminal) {
  return info.label + " in " + wtBase(info.worktree) + " — " + (terminal ? "terminal " : "agent ") + (i + 1) + " of " + n;
}

// screenStops is alt+q's ring for the worktree at dir: the page (null —
// the TUI's windows and plain worktree in one, since the console never
// takes the page away), its agents, its terminals.
function screenStops(sessions, order, dir) {
  return [null, ...walkRing(sessions, order, false, dir), ...walkRing(sessions, order, true, dir)];
}

// walkScreens steps the ring from the shown session (or the page): {i, msg}
// or {msg} when the worktree runs nothing.
function walkScreens(stops, shown, back, dir) {
  const n = stops.length;
  if (n === 1) return { msg: "no running agent or terminal in this worktree" };
  let at = stops.findIndex((s) => s && s.id === shown);
  if (at < 0) at = 0;
  const i = back ? (at - 1 + n) % n : (at + 1) % n;
  const st = stops[i];
  return { i, msg: st ? st.label + " in " + wtBase(dir) + " — " + (i + 1) + " of " + n : wtBase(dir) + " — your windows, " + (i + 1) + " of " + n };
}

export { screenStops, walkLine, walkNext, walkRing, walkScreens };
