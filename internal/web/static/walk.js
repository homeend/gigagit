// walk.js — the TUI's session walks (console.go cycleSessionsIn,
// worktree_walk.go walkWorktree) as pure functions over the page's session
// list: alt+a / alt+t (agents / terminals of the repository), alt+A / alt+T
// (the viewed worktree's), alt+q / alt+Q (the viewed worktree's screens),
// alt+w (the worktrees) and the console's worktree swap.
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

// pathKey is a checkout path's identity (model.KeyOf's rule, as far as the
// page can tell): slash form, no trailing slash, a drive path case-folded.
// A spelling difference must never read as another worktree — that would
// switch the page to the worktree it is on.
function pathKey(p) {
  const s = String(p || "").replace(/\\/g, "/").replace(/(.)\/+$/, "$1");
  return /^[A-Za-z]:/.test(s) ? s.toLowerCase() : s;
}

// switchTarget is showConsoleBy's swap: a console of another LISTED
// worktree shows in it (the page switches there, the list's spelling); ""
// stays (this one, or another repository's).
function switchTarget(dir, page, order) {
  if (!dir || pathKey(dir) === pathKey(page)) return "";
  return listedPath(dir, order);
}

// listedPath is dir as the Worktrees list spells it, "" when unlisted.
function listedPath(dir, order) {
  const k = pathKey(dir);
  return order.find((p) => pathKey(p) === k) || "";
}

// returnPlan is returnView: the worktree a console was shown over comes
// back on its close — {} none, {path}, or {msg} when it is gone.
function returnPlan(ret, page, order) {
  if (!ret || pathKey(ret) === pathKey(page)) return {};
  const path = switchTarget(ret, page, order);
  return path ? { path } : { msg: wtBase(ret) + " is gone — showing " + wtBase(page) };
}

// wtCycle is alt+w (cycleWorktrees): covered = a window over the page's
// panels (hidden: one of them is a console) — the first press closes
// them; otherwise the next worktree of the Worktrees list's ring. skip: the
// rows the server could not reach as listed (a directory gone, another
// environment's notation) — passed over and named, so the ring never
// sticks behind one.
function wtCycle(order, page, covered, hidden, skip = []) {
  const n = order.length;
  if (covered) {
    let msg;
    if (hidden && n < 2) msg = "console hidden — this repository has one worktree, alt+w cycles them once there are more";
    else if (hidden) msg = "console hidden — alt+w again for the next worktree";
    else if (n < 2) msg = "this repository has one worktree — alt+w cycles them once there are more";
    else msg = wtBase(page) + " — alt+w again for the next worktree";
    return { close: true, msg };
  }
  if (n < 2) return { msg: "this repository has one worktree — alt+w cycles them once there are more" };
  const at = order.indexOf(page);
  let next = at < 0 ? 0 : (at + 1) % n;
  const tries = at < 0 ? n : n - 1;
  const skipped = [];
  for (let k = 0; k < tries && skip.includes(order[next]); k++) {
    skipped.push(wtBase(order[next]));
    next = (next + 1) % n;
  }
  if (skipped.length === tries) return { msg: "no other worktree is reachable from here" };
  const msg = wtBase(order[next]) + " — " + (next + 1) + " of " + n + " worktrees";
  return { path: order[next], msg: skipped.length ? msg + " (skipped " + skipped.join(", ") + ": not reachable from here)" : msg };
}

export { listedPath, pathKey, returnPlan, screenStops, switchTarget, walkLine, walkNext, walkRing, walkScreens, wtCycle };
