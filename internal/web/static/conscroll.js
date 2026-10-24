// conscroll.js — the console's scroll mode and mouse routing as pure
// functions (the TUI's console_scroll.go / console_mouse.go); console.js
// runs them. Nothing here touches the DOM.

// wheelPlan is where a wheel notch goes: to the program when it takes the
// mouse, as ↑/↓ on an alternate screen without it, else into scroll mode.
function wheelPlan(frame) {
  if (frame && frame.mouse) return "mouse";
  if (frame && frame.alt) return "keys";
  return "scroll";
}

// cellAt is the 0-based cell under a pointer offset in the grid.
function cellAt(px, py, cell) {
  return { x: Math.max(0, Math.floor(px / cell.w)), y: Math.max(0, Math.floor(py / cell.h)) };
}

// scrollAction maps a key in scroll mode: a move, mark (space starts a line
// range), copy (enter), leave (esc, q), send (any other key leaves and goes
// to the agent), or null for a bare modifier.
function scrollAction(key) {
  const a = { ArrowUp: "up", ArrowDown: "down", PageUp: "pgup", PageDown: "pgdn", g: "top", G: "bottom", " ": "mark", Enter: "copy", Escape: "leave", q: "leave" }[key];
  if (a) return a;
  if (key === "Shift" || key === "Control" || key === "Alt" || key === "Meta" || key === "CapsLock") return null;
  return "send";
}

function scrollMove(cur, n, action, page) {
  switch (action) {
    case "up":
      return Math.max(0, cur - 1);
    case "down":
      return Math.min(n - 1, cur + 1);
    case "pgup":
      return Math.max(0, cur - page);
    case "pgdn":
      return Math.min(n - 1, cur + page);
    case "top":
      return 0;
    case "bottom":
      return n - 1;
  }
  return cur;
}

// scrollTop keeps the cursor line inside the rows shown.
function scrollTop(top, cur, rows, n) {
  let t = top;
  if (cur < t) t = cur;
  if (cur >= t + rows) t = cur - rows + 1;
  return Math.min(Math.max(0, t), Math.max(0, n - rows));
}

// linesText is the plain text of history rows a..b (either order).
function linesText(lines, a, b) {
  const from = Math.min(a, b), to = Math.max(a, b);
  const out = [];
  for (let i = from; i <= to; i++) out.push((lines[i] || []).map((r) => r.t).join("").replace(/\s+$/, ""));
  return out.join("\n");
}

export { cellAt, linesText, scrollAction, scrollMove, scrollTop, wheelPlan };
