import { suspectServerDown } from "./serverdown.js";

// core.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.

const ROW_H = 22;


// tabId names this page LOAD to the server (open files: which tab shows
// which file). Never stored: a duplicated tab copies sessionStorage and must
// still be a tab of its own.
const tabId =
  globalThis.crypto && typeof crypto.randomUUID === "function"
    ? crypto.randomUUID()
    : Math.random().toString(36).slice(2) + Date.now().toString(36);

const state = {
  rows: [],
  sessions: [], // the agent sessions of this gg (sidebar sub-rows, web attach)
  hosted: false, // /api/repo: a TUI serves this page from its own process — a switch here switches the terminal too
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
  // each list's display order (list name -> sort mode); see sortlist.js
  sorts: {},
  // status-file paths marked for batch actions (status mode only; a Set so
  // any module may add/remove — mutation, not reassignment)
  marked: new Set(),
  branches: [],
  worktrees: [],
  tags: [],
  tagsTruncated: false,
  stashes: [],
  bookmarks: [], // gg's own store: live references to a file or a commit
  shelf: [],     // gg's own store: frozen copies (a file's bytes, a commit's files)
  previews: [],  // saved merge previews: (source → target) pairs, recomputed from the live tips
  savedCompares: [], // the Previews tab's other kinds: commit pairs (@a..b) and comparisons (two links)
  previewsDisabled: false, // the previews store is unavailable (no state dir)
  previewsStale: false, // the last previews fetch failed: the rows stand, but nothing may be concluded from them
  // {id, source, target, sourceHash, targetHash, tip} while a preview owns the
  // compare screen, so a refresh can tell whether its tips moved. tip is the
  // source hash under the name the NOTE lane uses: a preview note is an
  // ordinary commit note written against it.
  previewOpen: null,
  // path → root-note count for the open preview's gathered set (the file
  // list's ◆N badges). null means "not known yet" — draw no badge at all.
  previewCounts: null,
  sidebar: true,
  filesHidden: false, // the file list's » control: folded to a strip (a stored preference, /api/uistate)
  op: null, // {id, es: EventSource} while an operation is live
  lastDiff: null,
  diffImgLayout: "side", // an image pair's layout (w): side | stacked | single — page memory, the TUI's session default
  diffImgOld: false,     // one at a time: the OLD side is up (tab / a click flips; reset per diff)
  stack: null, // the stacked diff view's live stack (stackview.js), else null
  diffPartial: false,      // the f toggle: true = changed lines only (a stored preference, /api/uistate)
  textMode: "wrap",        // the w cycle: how long lines show — scroll | wrap | cutoff (a stored preference, /api/uistate)
  // The blame overlay's d/D recent-lines highlight: span in whole minutes,
  // last = the text the prompt prefills. Session-only (never /api/uistate).
  blameRecent: { on: false, f: null, last: "7d" },
  diffFolds: new Set(),    // fold start indexes the reader unfolded in the open diff (reset per diff)
  diffCtx: null, // {path, rev, state} — the file the diff pane currently shows, else null
  diffLinkCtx: null, // {path, oldPath, cmpSides, gen} — a link-only context for a diff with no diffCtx (an entry compare)
  notes: [],                 // resolved notes for the open diff (GET /api/notes)
  noteCounts: { by_path: {}, by_commit: {}, by_commit_path: {}, plain_by_commit_path: {}, scopes_by_commit: {}, reviews: [], working_reviews: [] },
  // Stored AI reviews (reviews.js): the open commit's review rows, the review
  // row esc returned to, and the review view's overlay on the files screen.
  commitReviews: null,
  reviewSel: "",
  review: null,
  noteCollapsed: new Set(),  // root ids of the folded note threads of the open diff
  noteCollapsedFor: "",      // the diff that set was seeded for (path + rev)
  notesAgentOff: false,      // the TUI's `a`: hide agent-written notes
  diffRow: null,             // {side, no} — the clicked diff row `c` anchors on
  diffRange: null,           // {side, first, last} — the marked range of lines (shift+click a line number)
  // Attention bands an agent painted (gg session highlight), keyed by
  // attnKey below. Each value is a list of {side, start, end, tone}. Cleared
  // by highlight_clear and by a `status`/`all` steer reload — never by the
  // interval refresh, which no agent asked for, and never by the
  // `notes`-only reload every note mutation auto-posts.
  attention: new Map(),
  diffBlockIdx: -1,
  detailGen: 0,
  dragBranch: null, // name of the branch being dragged, else null
  dragPreview: null, // {id, kind} of the Previews row being dragged, else null
  solo: "", // branch the commit list is narrowed to ("" = every branch)
  // A parked (backgrounded) long task, and then its result until collected:
  // {label, status: running|done|failed|cancelled, title, noteId, report, error}
  task: null,
  cfilter: null, // {q, matches: [feedIdx...]} while the commits quick filter (/) is active, else null
  gotoGen: 0, flashHash: "",
};


const $ = (id) => document.getElementById(id);


// attnKey is the ONE key builder for state.attention: the renderer (files.js)
// and the steer commands that write the marks (live.js) both call it, so the
// key a `gg session highlight` writes and the key diffHTML reads are the same
// bytes. It lives here, with `state`, rather than in either caller. `rev` is
// the FULL commit sha for a commit diff — state.diffCtx.rev holds the feed's
// full hash, and the wire carries a full one too.
function attnKey(ctx) {
  if (!ctx || !ctx.path) return "";
  return `${ctx.state || "unstaged"}\0${ctx.rev || ""}\0${ctx.path}`;
}


// Destructive decision options render red in the modal (the ctx-menu
// danger precedent). Options are English protocol values — i18n never
// translates them — so a client-side set is reliable.
const DANGER_OPTIONS = new Set([
  "force", "force-with-lease", "force-delete", "reset", "delete", "drop",
  "unlock-and-remove", "discard", "overwrite", "hard",
  "abort merge", "abort rebase", "abort cherry-pick", "abort revert",
  "kill", "kill and remove",
]);


// Listed in the order the sidebar draws them (index.html) — nothing reads the
// order (every use is a filter/forEach over the whole set), but a list that
// disagrees with the screen is a trap for the next reader.
const SECTIONS = ["branches", "remotes", "worktrees", "previews", "prs", "tags", "stashes", "reflog", "bookmarks", "shelf"];


// localStorage can throw (private mode); persistence is best-effort.
function lsGet(k) { try { return localStorage.getItem(k); } catch { return null; } }

function lsSet(k, v) { try { localStorage.setItem(k, v); } catch {} }


// sessionStorage-backed: the big-repo banner's "not now" dismissal is
// per-tab-session only (re-evaluated next visit), unlike localStorage's
// gg.graph override which persists across sessions.
function ssGet(k) { try { return sessionStorage.getItem(k); } catch { return null; } }

function ssSet(k, v) { try { sessionStorage.setItem(k, v); } catch {} }


// apiFetch is fetch for our own API. A REJECTED fetch (refused, reset — a
// TypeError, never an HTTP status) is what a dead server looks like, so it
// starts the liveness probe (serverdown.js); the caller still gets the error.
async function apiFetch(url, init) {
  try {
    return await fetch(url, init);
  } catch (err) {
    if (err instanceof TypeError) suspectServerDown();
    throw err;
  }
}


async function getJSON(url) {
  const resp = await apiFetch(url);
  const body = await resp.json();
  if (!resp.ok) {
    const err = new Error(body.error || resp.statusText);
    err.data = body; // structured refusals, as postJSON keeps them (a link comparison's `side`)
    err.status = resp.status; // a 404 is an ANSWER ("no such entry"), a 500 is not
    throw err;
  }
  return body;
}


async function postJSON(url, body) {
  const resp = await apiFetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const data = await resp.json();
  if (!resp.ok) {
    const err = new Error(data.error || resp.statusText);
    err.data = data; // structured refusals (e.g. reroot's repairable handshake)
    throw err;
  }
  return data;
}


function esc(s) {
  return String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
}


function runes(s) {
  return Array.from(s);
}


// --- path elision (port of internal/tui/elide.go) ---
// The whole UI is monospace, so "display columns" are just characters here and
// the arithmetic matches the TUI's exactly. Keep the two in sync: the browser
// check in the CDP harness compares this against the Go implementation's
// output over a shared case table.

// splitPathSegs tokenizes a path (no trailing separator) into {sep, text}
// segments, preserving each segment's preceding separator run verbatim.
function splitPathSegs(s) {
  const segs = [];
  let cur = { sep: "", text: "" };
  for (const r of runes(s)) {
    if (r === "/" || r === "\\") {
      if (cur.text !== "") { segs.push(cur); cur = { sep: "", text: "" }; }
      cur.sep += r;
    } else {
      cur.text += r;
    }
  }
  segs.push(cur);
  return segs;
}


// elidePath shortens a filesystem path to at most n columns by dropping WHOLE
// segments from the middle, marked by a single "…". Segments survive by
// priority: the final one (the file/repo name) first, then the directory just
// before it, then the path's FIRST segment, then alternating right/left
// working inward. The dropped run is always contiguous, so the result reads
// head + "…" + tail. When not even "…/<name>" fits, the name itself is cut in
// the middle.
function elidePath(s, n) {
  if (n <= 0) return "";
  if (runes(s).length <= n) return s;
  if (n === 1) return "…";
  const trimmed = s.replace(/[/\\]+$/, "");
  const trail = s.slice(trimmed.length);
  if (trimmed === "") return runes(s).slice(0, n).join(""); // a pure separator run
  const segs = splitPathSegs(trimmed);
  const last = segs.length - 1;
  if (last === 0) return elideNameMiddle(segs[0].text, n - runes(trail).length) + trail;
  // build renders segs[:keepL], a "…" for the dropped middle, then segs[keepR:].
  // The "…" borrows the first dropped segment's separator so it slots into the
  // path ("/mnt/…/name"); with no prefix kept it leads bare ("…/name").
  const build = (keepL, keepR) => {
    let b = "";
    for (let i = 0; i < keepL; i++) b += segs[i].sep + segs[i].text;
    if (keepL < keepR) {
      if (keepL > 0) b += segs[keepL].sep;
      b += "…";
    }
    for (let i = keepR; i < segs.length; i++) b += segs[i].sep + segs[i].text;
    return b + trail;
  };
  if (runes(build(0, last)).length > n) {
    return elideNameMiddle(segs[last].text, n - runes(trail).length) + trail;
  }
  // Grow both kept runs inward, right first, in strict priority order. A side
  // closes permanently once its next segment no longer fits (widths only grow);
  // skipping it for a deeper segment would leave a second gap.
  let keepL = 0, keepR = last, leftNext = 0, rightNext = last - 1;
  let leftOpen = true, rightOpen = true;
  while (leftOpen || rightOpen) {
    if (rightOpen) {
      if (rightNext >= keepL && runes(build(keepL, rightNext)).length <= n) {
        keepR = rightNext;
        rightNext--;
      } else {
        rightOpen = false;
      }
    }
    if (leftOpen) {
      if (leftNext < keepR && runes(build(leftNext + 1, keepR)).length <= n) {
        keepL = leftNext + 1;
        leftNext++;
      } else {
        leftOpen = false;
      }
    }
  }
  return build(keepL, keepR);
}


// elideNameMiddle cuts a bare name (no separators) to at most n columns by
// dropping its MIDDLE: the beginning plus the extension — or, with no
// extension, the ending — survive around a "…".
function elideNameMiddle(s, n) {
  if (n <= 0) return "";
  const r = runes(s);
  if (r.length <= n) return s;
  if (n === 1) return "…";
  let tail = "";
  const dot = s.lastIndexOf("."); // >0: a dotfile is a name, not an extension
  if (dot > 0) tail = s.slice(dot);
  if (tail === "" || runes(tail).length + 2 > n) {
    tail = tailWidth(s, Math.floor((n - 1) / 3));
  }
  return r.slice(0, n - 1 - runes(tail).length).join("") + "…" + tail;
}


// tailWidth returns the longest trailing run of s at most w columns wide.
function tailWidth(s, w) {
  const r = runes(s);
  for (let lo = 0; lo < r.length; lo++) {
    if (r.length - lo <= w) return r.slice(lo).join("");
  }
  return "";
}


// elideNoteSummary fits a shelf note's summary ("Recycled from /x/wt-a
// (feat/y)") into n columns: the trailing "(branch)" group stays whole — a
// branch's slashes are not a path to cut — the words before the path stay
// whole while the path keeps a few columns, and the path itself is cut in its
// middle. Port of the TUI's elideNoteSummary (shelf_note_view.go).
function elideNoteSummary(s, n) {
  if (runes(s).length <= n) return s;
  const i = s.lastIndexOf(" (");
  if (i > 0 && s.endsWith(")")) {
    const group = s.slice(i);
    const room = n - runes(group).length;
    if (room >= 2) {
      const body = s.slice(0, i);
      const j = body.search(/[/\\]/);
      if (j > 0) {
        const left = room - runes(body.slice(0, j)).length;
        if (left >= 8) return body.slice(0, j) + elidePath(body.slice(j), left) + group;
      }
      return elidePath(body, room) + group;
    }
    // Not even the group fits: the worktree's name says more than a sliver of
    // the branch would ("…/x)").
    return elidePath(s.slice(0, i), n);
  }
  return elidePath(s, n);
}
// --- end path elision ---


// charWidth measures one monospace column in CSS pixels, so a pixel budget can
// be turned into the column count elidePath takes. Measured once against the
// body font and cached; the probe is removed immediately.
let charW = 0;
function charWidth() {
  if (charW) return charW;
  const probe = document.createElement("span");
  probe.textContent = "0".repeat(100);
  probe.style.cssText = "position:absolute;visibility:hidden;white-space:pre;";
  document.body.appendChild(probe);
  charW = probe.getBoundingClientRect().width / 100 || 7.8;
  probe.remove();
  return charW;
}


// A starting point for the worktree-path prompt, not a decision: a sibling of
// the MAIN worktree named <repo>-<branch>. Anchoring on the main worktree
// (git lists it first) rather than the served one keeps new worktrees from
// nesting inside each other when you create one while inside another — the
// same anchor the TUI's template resolver uses. Slashes in a branch name
// become dashes so `feat/x` does not imply a directory.
function defaultWorktreePath(branch) {
  const main = (state.worktrees[0] && state.worktrees[0].path) || (state.repo && state.repo.worktree) || "";
  if (!main) return "";
  const sep = main.includes("\\") && !main.includes("/") ? "\\" : "/";
  const cut = main.lastIndexOf(sep);
  const parent = cut > 0 ? main.slice(0, cut) : main;
  const name = cut >= 0 ? main.slice(cut + 1) : main;
  return parent + sep + name + "-" + branch.replace(/[^\w.-]+/g, "-");
}

// --- session activity (pure; guarded against Go) ---
// What an agent session is doing, as the server classified it from its
// screen (domain.SessionStates): s.agent_state is working | idle | question,
// s.since when that began, s.stalled when it printed nothing for two
// minutes while apparently busy, or showed only its spinner for eleven minutes. Labels mirror the TUI's: never "waiting".
function activityAge(iso, now) {
  const s = Math.max(0, Math.floor((now - Date.parse(iso)) / 1000));
  return s < 60 ? s + "s" : s < 3600 ? Math.floor(s / 60) + "m" : Math.floor(s / 3600) + "h";
}
// activityLabel: "working 7m" | "idle 3m" | "needs input" | "stalled · working 7m"
// | "stalled · no output" | "" (unknown and not stalled, or exited); and
// "reported 2m" / "done 2m" while an agent_report is unanswered (s.report_at
// is on the wire only until someone types into the session) — a question
// still wins, the user must act.
function activityLabel(s, now) {
  if (!s || s.state === "exited") return "";
  if (s.report_at && s.agent_state !== "question") return (s.report_final ? "done " : "reported ") + activityAge(s.report_at, now);
  let label = "";
  if (s.agent_state === "working" || s.agent_state === "idle") label = s.agent_state + (s.since ? " " + activityAge(s.since, now) : "");
  else if (s.agent_state === "question") label = "needs input";
  if (s.stalled) return "stalled · " + (label || "no output");
  return label;
}
// activityAttn: the attention colour — the agent waits for a decision,
// looks stuck, or reported a result nobody answered yet.
function activityAttn(s) {
  return !!s && (s.agent_state === "question" || !!s.stalled || !!s.report_at);
}
// reportLine: the unanswered report's first line for a wide row, "" under
// a question.
function reportLine(s) {
  return s && s.report_line && s.agent_state !== "question" ? s.report_line : "";
}
// noticeText: the toast for one activity notice {kind, label, worktree, quiet_s, spinning, text}.
function noticeText(n) {
  const wt = String(n.worktree || "").split(/[\\/]/).filter(Boolean).pop() || n.worktree;
  const who = n.label + " in " + wt;
  if (n.kind === "question") return who + " needs your input";
  if (n.kind === "report") return who + " reports: " + (n.text || "");
  if (n.kind === "idle") return who + " finished its turn — idle";
  const q = n.quiet_s || 0;
  const age = q < 60 ? q + "s" : Math.floor(q / 60) + "m";
  if (n.spinning) return who + " has shown only its spinner for " + age + " — stalled?";
  return who + " has printed nothing for " + age + " — stalled?";
}
// --- end session activity ---

// --- single-flight task gate (pure; guarded against node) ---
//
// The same background task can be started many times over: a reload answers
// to r, the footer chip AND the palette, and a held r repeats the keydown.
// Every start fans the same ten requests at a server that answers them under
// ONE per-repo gate, so the presses do not reload faster — they queue behind
// each other and each one re-walks the feed underneath the list. Measured on
// a 7.6k-commit repo before this gate existed: four presses turned a 3.4s
// /api/status into 3.4 → 5.5 → 7.6 → 9.9s, with four `?reset=1` walks
// throwing away each other's pages.
//
// runOnce keys a task by TYPE. While one is in flight the next start is
// DROPPED rather than queued: these tasks are idempotent reloads, so what
// the running one brings back is exactly what the second press wanted, and
// queueing would only replay the pile-up later.
//
// The timeout is a backstop, not the mechanism — the promise settling is
// what normally frees the type. It exists for a start whose promise never
// settles at all (a fetch with no abort behind a wedged server); without it
// one lost task would wedge its type for the life of the page. Generous on
// purpose: this targets ~100GB monorepos, where an honest reload can run for
// tens of seconds, and releasing early would hand back the pile-up.
const RUN_ONCE_TIMEOUT = 60000;

let runSeq = 0;
const runningTasks = new Map(); // type -> {id, at}

// runOnce returns the task's promise, or null when a task of this type is
// already running (the caller decides what to say about the dropped start).
function runOnce(type, fn, opts = {}) {
  const now = opts.now || Date.now;
  const timeout = opts.timeout === undefined ? RUN_ONCE_TIMEOUT : opts.timeout;
  const live = runningTasks.get(type);
  if (live && now() - live.at < timeout) return null;
  const rec = { id: ++runSeq, at: now() };
  runningTasks.set(type, rec);
  // Identity-checked release: once a timed-out task's slot has been taken by
  // a newer start, the straggler settling must not free the NEWER one.
  const release = () => {
    if (runningTasks.get(type) === rec) runningTasks.delete(type);
  };
  let p;
  try {
    p = fn();
  } catch (e) {
    release(); // a synchronous throw is a finished task too
    throw e;
  }
  return Promise.resolve(p).then(
    (v) => { release(); return v; },
    (e) => { release(); throw e; },
  );
}

// --- end single-flight task gate ---


export { $, DANGER_OPTIONS, ROW_H, SECTIONS, activityAttn, activityLabel, attnKey, charWidth, defaultWorktreePath, elideNameMiddle, elideNoteSummary, elidePath, esc, getJSON, lsGet, lsSet, noticeText, postJSON, reportLine, runOnce, runes, splitPathSegs, ssGet, ssSet, state, tabId };

// fmtBytes is the TUI's byte count for a placeholder: "597.0 KB", "1.2 MB", "312 B".
export function fmtBytes(n) {
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + " MB";
  if (n >= 1 << 10) return (n / (1 << 10)).toFixed(1) + " KB";
  return n + " B";
}
