// workspace.js — the open workspace on the page (spec
// 2026-10-31-web-workspaces): identity from /api/workspace, each sleeping
// member's rows from /api/workspace/member, kept so a refresh of the member on
// screen reads no other member.
import { getJSON, ssGet, ssSet, state } from "./core.js";
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
// rowActionPlan is what an action on a row does first: a row of another member
// switches, to the worktree the row names, else the member's default.
function rowActionPlan(rowMember, liveMember, rowWorktree, defaultWorktree) {
  if (!rowMember || samePath(rowMember, liveMember)) return {};
  return { switchTo: rowWorktree || defaultWorktree, member: rowMember };
}

// runRowPlan switches, waits for the arriving member's lists, then acts. A
// refused switch stops the action: its reason is already on the status line. So
// does one that answered without landing there (a repair question, a later
// switch): the action must never run in the repository still on screen.
async function runRowPlan(plan, doSwitch, landed, liveMember, run) {
  if (plan.switchTo) {
    if (!(await doSwitch(plan.switchTo))) return;
    await landed();
    if (!samePath(liveMember(), plan.member)) return;
  }
  run();
}

// pruneToMembers forgets what was kept about repositories no longer members: a
// switch to one is a leave, not a switch in place.
function pruneToMembers(map, members) {
  for (const path of [...map.keys()]) if (!members.some((m) => samePath(m.path, path))) map.delete(path);
}

// rowsOwner is the member the rows on screen belong to: by git's main checkout,
// else the member the owner says is live (git prints a real path, the workspace
// may spell it through a symlink).
function rowsOwner(ws, worktrees) {
  if (!worktrees || !worktrees.length) return null;
  return ownerOf(ws.members, worktrees) || ws.members[ws.live] || null;
}

// compareAllowed: a mark and a row of two members cannot be compared.
function compareAllowed(markMember, rowMember) {
  return !markMember || !rowMember || samePath(markMember, rowMember);
}

// defaultWorktreeOf is where a switch into a member lands when no row names a
// worktree: the last one viewed there while it is listed, else its first
// non-bare worktree, else its main checkout (domain.DefaultWorktree's rule).
function defaultWorktreeOf(member, worktrees) {
  const wts = worktrees || [];
  if (member.last_viewed && wts.some((w) => samePath(w.path, member.last_viewed))) return member.last_viewed;
  const first = wts.find((w) => !w.bare);
  return first ? first.path : member.path;
}

// walkOrder is alt+s's and alt+S's worktree order: alt+S keeps the member on
// screen; outside a workspace (liveDirs null) it is alt+s.
function walkOrder(order, shift, liveDirs) {
  if (!shift || !liveDirs) return order;
  return order.filter((p) => liveDirs.some((d) => samePath(d, p)));
}
// repoListRows is R's list: the workspaces first, ● on the open one and its
// members; the current repository is not listed. No workspace defined: the list
// as it was. Rows take {ws} (a workspace) or {path} (a repository).
function repoListRows(defined, open, repos, here) {
  if (!defined.length && !open) return repos;
  const isMember = (p) => !!open && open.members.some((m) => samePath(m.path, p));
  const out = defined.map((d) => ({
    ws: d.name,
    label: (open && open.name === d.name ? "● " : "") + d.name,
    detail: d.repos.map((r) => r.label).join(", "),
  }));
  const listed = repos.map((r) => (isMember(r.path) ? { ...r, label: "● " + r.label } : r));
  const extra = open
    ? open.members
        .filter((m) => !samePath(m.path, here) && !repos.some((r) => samePath(r.path, m.path)))
        .map((m) => ({ label: "● " + m.label, path: m.path, branch: "", slow: false, pending: false, age: "" }))
    : [];
  return [...out, ...extra, ...listed];
}

// repoEnterPlan is R's enter: a workspace opens (the open one stays), a member
// switches in place, a repository outside leaves.
function repoEnterPlan(row, open) {
  if (row.ws) return open && open.name === row.ws ? {} : { open: row.ws };
  if (!open) return { reroot: row.path };
  return open.members.some((m) => samePath(m.path, row.path)) ? { member: row.path } : { leave: row.path };
}

// The session question: the safe answer first; esc is Cancel.
const DROP_OPTIONS = ["Leave them running (hidden)", "Stop them", "Cancel"];
function dropAnswer(i) {
  return i === 0 ? "keep" : i === 1 ? "stop" : null;
}

// The launch question: one row per workspace, then the repository alone. esc
// opens it alone.
function launchOptions(q) {
  return [...q.rows.map((r) => r.label), "Open " + q.repo + " alone"];
}
function launchAnswer(q, i) {
  return i >= 0 && i < q.rows.length ? q.rows[i].name : "";
}
// --- end workspace model ---

const LEFT_KEY = "gg.leftWorkspace";
const rows = new Map(); // member path → its last read
const seen = new Map(); // member path → the change counter its last read answered
const inFlight = new Map(); // member path → a read is running; true = another was asked meanwhile
let defined = [];
let definedErr = "";
let question = null;
let leftLine = () => {};

function onLeftLine(fn) {
  leftLine = fn;
}

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
  const owner = state.ws ? rowsOwner(state.ws, state.worktrees) : null;
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

// noteLeaving carries "left workspace <name>" across the reload of a switch to a
// path outside every member.
function noteLeaving(path) {
  if (state.ws && !memberOfPath(path)) ssSet(LEFT_KEY, state.ws.name);
}

// sayLeft says it once the reloaded page knows no workspace is open.
function sayLeft(line) {
  const name = ssGet(LEFT_KEY);
  if (!name) return;
  ssSet(LEFT_KEY, "");
  if (!state.ws) line("left workspace " + name);
}

// loadWorkspace re-reads identity (no git) and the members that moved.
// fresh: every sleeping member again, as on activation (the terminal was the
// active side and told no one what moved).
async function loadWorkspace(fresh = false) {
  let w;
  try {
    w = await getJSON("/api/workspace");
  } catch {
    return; // blocked or down: the next hello or activation asks again
  }
  const was = state.ws;
  state.ws = w.open || null;
  defined = w.defined || [];
  definedErr = w.defined_error || "";
  question = w.question || null;
  if (!state.ws) {
    rows.clear();
    seen.clear();
  } else {
    if (fresh) seen.clear();
    pruneToMembers(rows, state.ws.members);
    pruneToMembers(seen, state.ws.members);
    const owner = rememberLive();
    for (const path of membersToReread(state.ws.members, seen, owner ? owner.path : "")) loadMember(path);
  }
  if (was || state.ws) repaint();
  sayLeft(leftLine);
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

// allWorktreeRows is every member's worktree rows.
function allWorktreeRows() {
  if (!state.ws) return state.worktrees || [];
  const out = [...(state.worktrees || [])];
  for (const r of rows.values()) for (const w of r.worktrees || []) if (!out.some((x) => samePath(x.path, w.path))) out.push(w);
  return out;
}

// liveDirs is the worktrees of the member on screen; null outside a workspace.
function liveDirs() {
  return state.ws ? (state.worktrees || []).map((w) => w.path) : null;
}

// liveMemberPath is the member on screen ("" outside a workspace).
function liveMemberPath() {
  const m = state.ws ? rowsOwner(state.ws, state.worktrees) : null;
  return m ? m.path : "";
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

// definedError is why the workspaces could not be read; "" = they were.
function definedError() {
  return definedErr;
}

export { definedError, noteLeaving, onLeftLine, DROP_OPTIONS, dropAnswer, launchAnswer, launchOptions, repoEnterPlan, repoListRows, allWorktreePaths, allWorktreeRows, compareAllowed, defaultWorktreeOf, liveDirs, liveMemberPath, rowActionPlan, runRowPlan, walkOrder, groupedRows, headMark, loadMember, loadWorkspace, memberOfPath, memberRows, membersToReread, ownerOf, rememberLive, samePath, workspaceOpen, wsDefined, wsMembers, wsQuestion };
