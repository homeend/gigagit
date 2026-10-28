// walk.js — the TUI's session walks (console.go cycleSessionsIn,
// worktree_walk.go walkWorktree) as pure functions over the page's session
// list: alt+a / alt+t (agents / terminals of the repository), alt+A / alt+T
// (the viewed worktree's), alt+q / alt+Q (the viewed worktree's screens),
// alt+s (every worktree's screens, session_walk.go), alt+w (the worktrees)
// and the console's worktree swap.
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

// screenStops is alt+q's ring for the worktree at dir in the order of its
// branches rows: the page (null — the TUI's windows and plain worktree in
// one, since the console never takes the page away), then its sessions as
// their └ ● sub-rows list them (by start), agents and terminals mixed.
function screenStops(sessions, order, dir) {
  const own = [...walkRing(sessions, order, false, dir), ...walkRing(sessions, order, true, dir)];
  own.sort((a, b) => Date.parse(a.started) - Date.parse(b.started) || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
  return [null, ...own];
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

// sessionStops is alt+s's ring (session_walk.go sessionWalkRing): every
// worktree of the Worktrees list's order that runs a session, each as alt+q
// walks it — its page ({page}: the TUI's plain worktree and windows in one),
// then its sessions as screenStops lists them. A worktree running nothing is
// no stop.
function sessionStops(sessions, order) {
  const out = [];
  for (const dir of order) {
    const own = screenStops(sessions, order, dir).slice(1);
    if (own.length) out.push({ page: dir }, ...own);
  }
  return out;
}

// walkAllScreens steps alt+s's ring from the shown session, else from the
// viewed worktree's page; a viewed worktree running nothing goes to the
// next one that does in the list's order, wrapping. Another worktree's page
// in skip (not reachable from here) is passed over for the next worktree.
// {i, msg} or {msg} when nothing runs.
function walkAllScreens(stops, shown, viewed, order, skip = []) {
  const n = stops.length;
  if (!n) return { msg: "no running agent or terminal in this repository" };
  const here = (p) => pathKey(p) === pathKey(viewed);
  let at = shown ? stops.findIndex((s) => s.id === shown) : -1;
  if (at < 0) at = stops.findIndex((s) => s.page && here(s.page));
  let i = (at + 1) % n;
  if (at < 0) {
    const v = order.findIndex(here);
    i = Math.max(0, stops.findIndex((s) => s.page && order.indexOf(s.page) > v));
  }
  const unreachable = (st) => st.page && !here(st.page) && skip.some((p) => pathKey(p) === pathKey(st.page));
  for (let k = 0; k < n && unreachable(stops[i]); k++) {
    do i = (i + 1) % n;
    while (!stops[i].page);
  }
  const st = stops[i];
  return { i, msg: st.page ? wtBase(st.page) + " — your windows, " + (i + 1) + " of " + n : st.label + " in " + wtBase(st.worktree) + " — " + (i + 1) + " of " + n };
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

// --- the landing mark (the TUI's Branches cursor after a walk) ---
// landingAfterOpen is the mark after a console opens: on a Branches
// sub-row it lands there (the TUI cursor is on it when enter opens it);
// any other open leaves it, except a mark naming ANOTHER session — the
// Branches list sits beside the console, so it would name the wrong agent.
function landingAfterOpen(landing, id, fromBranches) {
  if (fromBranches) return { sid: id };
  if (landing && landing.sid && landing.sid !== id) return null;
  return landing;
}

// worktreeBranch is the branch checked out at path (alt+w's and alt+q's
// plain-stop landing row); "" when detached or unlisted.
function worktreeBranch(worktrees, path) {
  const k = pathKey(path);
  const w = (worktrees || []).find((x) => pathKey(x.path) === k);
  return w && !w.detached ? w.branch || "" : "";
}

export { landingAfterOpen, listedPath, pathKey, returnPlan, worktreeBranch, screenStops, sessionStops, switchTarget, walkAllScreens, walkLine, walkNext, walkRing, walkScreens, wtCycle };
