// core.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.

const ROW_H = 22;


const state = {
  rows: [],
  canLoadMore: false,
  loadingMore: false,
  cursor: 0,
  files: [],
  fileCursor: 0,
  fileSha: null,
  pane: "commits", // commits | files
  layout: "list", // list (commits full-width) | detail (files+diff, list hidden)
  // svg is the only graph renderer: browser rows (22px) are taller than
  // the 13px font box, so text glyphs would leave vertical gaps. g toggles
  // the graph off entirely — a flat ●-gutter list (TUI show_graph parity)
  // with the lane column's space going to subjects.
  graphMode: "svg", // svg | off
  health: null, // /api/health payload (big-repo banner), else null
  wt: null, // /api/status payload while the tree is dirty, else null
  conflict: null, // {op, source, target, desc, conflicted} while a sequencer op is paused, else null
  filesMode: "commit", // commit | status | compare
  compare: null, // {a, b, aHash, bHash, all, filter, originsError} while comparing two branches
  statusEntries: [],
  // status-file paths marked for batch actions (status mode only; a Set so
  // any module may add/remove — mutation, not reassignment)
  marked: new Set(),
  branches: [],
  worktrees: [],
  tags: [],
  tagsTruncated: false,
  stashes: [],
  sidebar: true,
  op: null, // {id, es: EventSource} while an operation is live
  lastDiff: null,
  diffCtx: null, // {path, rev} — the file the diff pane currently shows, else null
  diffBlockIdx: -1,
  detailGen: 0,
  dragBranch: null, // name of the branch being dragged, else null
  solo: "", // branch the commit list is narrowed to ("" = every branch)
  // A parked (backgrounded) long task, and then its result until collected:
  // {label, status: running|done|failed|cancelled, title, path, report, error}
  task: null,
  cfilter: null, // {q, matches: [feedIdx...]} while the commits quick filter (/) is active, else null
  gotoGen: 0, flashHash: "",
};


const $ = (id) => document.getElementById(id);


// Destructive decision options render red in the modal (the ctx-menu
// danger precedent). Options are English protocol values — i18n never
// translates them — so a client-side set is reliable.
const DANGER_OPTIONS = new Set([
  "force", "force-with-lease", "force-delete", "reset", "delete", "drop",
  "unlock-and-remove", "discard", "overwrite", "hard",
  "abort merge", "abort rebase", "abort cherry-pick", "abort revert",
]);


const SECTIONS = ["branches", "worktrees", "tags", "stashes", "reflog"];


// localStorage can throw (private mode); persistence is best-effort.
function lsGet(k) { try { return localStorage.getItem(k); } catch { return null; } }

function lsSet(k, v) { try { localStorage.setItem(k, v); } catch {} }


// sessionStorage-backed: the big-repo banner's "not now" dismissal is
// per-tab-session only (re-evaluated next visit), unlike localStorage's
// gg.graph override which persists across sessions.
function ssGet(k) { try { return sessionStorage.getItem(k); } catch { return null; } }

function ssSet(k, v) { try { sessionStorage.setItem(k, v); } catch {} }


async function getJSON(url) {
  const resp = await fetch(url);
  const body = await resp.json();
  if (!resp.ok) throw new Error(body.error || resp.statusText);
  return body;
}


async function postJSON(url, body) {
  const resp = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const data = await resp.json();
  if (!resp.ok) throw new Error(data.error || resp.statusText);
  return data;
}


function esc(s) {
  return String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
}


function runes(s) {
  return Array.from(s);
}

export { $, DANGER_OPTIONS, ROW_H, SECTIONS, esc, getJSON, lsGet, lsSet, postJSON, runes, ssGet, ssSet, state };
