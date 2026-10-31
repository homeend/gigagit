// workspace.js — the open workspace on the page (spec
// 2026-10-31-web-workspaces): identity from /api/workspace, each sleeping
// member's rows from /api/workspace/member, kept so a refresh of the member on
// screen reads no other member.
import { getJSON, state } from "./core.js";
import { pathKey } from "./walk.js";

// --- workspace model (pure; guarded against Go) ---
function samePath(a, b) {
  const k = (p) => {
    const s = String(p || "").replace(/\\/g, "/").replace(/(.)\/+$/, "$1");
    return /^[A-Za-z]:/.test(s) ? s.toLowerCase() : s;
  };
  return k(a) === k(b);
}

// ownerOf is the member a repository's worktree list belongs to: git lists the
// main checkout first, and a member is identified by it.
function ownerOf(members, worktrees) {
  if (!worktrees || !worktrees.length) return null;
  return members.find((m) => samePath(m.path, worktrees[0].path)) || null;
}

// groupedRows is one list per member in workspace order: the heading's facts and
// that member's rows under its own sort. rowsOf(path) is the member's kept read
// ({worktrees, branches, missing, error}) or undefined before its first answer.
// A read's own "missing" outranks the owner's push.
function groupedRows(members, liveIndex, list, rowsOf, sortFn) {
  return members.map((m, i) => {
    const read = rowsOf(m.path) || {};
    const missing = read.missing !== undefined ? read.missing : !!m.missing;
    return {
      label: "[" + m.label + "]" + (missing ? " (missing)" : ""),
      path: m.path,
      live: i === liveIndex,
      missing,
      error: read.error || "",
      rows: missing || read.error ? [] : sortFn(read[list] || []),
    };
  });
}

// membersToReread is the sleeping members whose change counter moved since the
// last look, or that have never been read.
function membersToReread(members, seen, livePath) {
  return members
    .filter((m) => !samePath(m.path, livePath) && seen.get(m.path) !== m.changes)
    .map((m) => m.path);
}

// headMark is a Branches row's mark: "✓:" on the branch of the worktree on
// screen, the plain star on every other group's head.
function headMark(isHead, inLiveGroup) {
  if (!isHead) return "";
  return inLiveGroup ? "✓:" : "✓";
}
// --- end workspace model ---

const rows = new Map(); // member path → its last read
const seen = new Map(); // member path → the change counter its last read answered
const inFlight = new Map(); // member path → a read is running; true = another was asked meanwhile
let defined = [];
let question = null;

function workspaceOpen() {
  return !!state.ws;
}

function wsMembers() {
  return state.ws ? state.ws.members : [];
}

function wsDefined() {
  return defined;
}

function wsQuestion() {
  return question;
}

function memberRows(path) {
  return rows.get(path);
}

// rememberLive keeps the rows of the member the page reads through today's
// routes, so they stay in place when it goes to sleep.
function rememberLive() {
  const owner = ownerOf(wsMembers(), state.worktrees);
  if (owner) rows.set(owner.path, { worktrees: state.worktrees, branches: state.branches });
  return owner;
}

function repaint() {
  document.dispatchEvent(new CustomEvent("gg:workspace"));
}

// loadMember reads one sleeping member. Two changes while one read runs cost one
// more read, not two.
async function loadMember(path) {
  if (inFlight.has(path)) {
    inFlight.set(path, true);
    return;
  }
  inFlight.set(path, false);
  const m = wsMembers().find((x) => x.path === path);
  const answered = m ? m.changes : 0;
  try {
    const r = await getJSON("/api/workspace/member?path=" + encodeURIComponent(path));
    rows.set(path, r);
    seen.set(path, answered);
    repaint();
  } catch (e) {
    if (!(e && e.data && e.data.ui_inactive)) rows.set(path, { error: String((e && e.message) || e) });
  } finally {
    const again = inFlight.get(path);
    inFlight.delete(path);
    if (again) loadMember(path);
  }
}

// loadWorkspace re-reads identity (no git) and the members that moved.
async function loadWorkspace() {
  let w;
  try {
    w = await getJSON("/api/workspace");
  } catch {
    return; // blocked or down: the next hello or activation asks again
  }
  const was = state.ws;
  state.ws = w.open || null;
  defined = w.defined || [];
  question = w.question || null;
  if (!state.ws) {
    rows.clear();
    seen.clear();
  } else {
    const owner = rememberLive();
    for (const path of membersToReread(state.ws.members, seen, owner ? owner.path : "")) loadMember(path);
  }
  if (was || state.ws) repaint();
  document.dispatchEvent(new CustomEvent("gg:workspace-question"));
}

// allWorktreePaths is every member's worktrees: what a switch can reach in place.
function allWorktreePaths() {
  const live = (state.worktrees || []).map((w) => w.path);
  if (!state.ws) return live;
  const out = [...live];
  for (const r of rows.values()) for (const w of r.worktrees || []) if (!out.some((p) => pathKey(p) === pathKey(w.path))) out.push(w.path);
  return out;
}

// memberOfPath is the member listing a worktree, else null.
function memberOfPath(path) {
  for (const m of wsMembers()) {
    if (samePath(m.path, path)) return m;
    const r = rows.get(m.path);
    if (r && (r.worktrees || []).some((w) => samePath(w.path, path))) return m;
  }
  return null;
}

export { allWorktreePaths, groupedRows, headMark, loadMember, loadWorkspace, memberOfPath, memberRows, membersToReread, ownerOf, rememberLive, samePath, workspaceOpen, wsDefined, wsMembers, wsQuestion };
