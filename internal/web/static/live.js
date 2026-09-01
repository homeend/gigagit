// live.js — the page's half of live refresh: one EventSource on
// /api/events, coalesced into a single re-fetch of just the sources the
// server named. The server decides WHEN (file watch, intervals, the
// [refresh] config); this module only decides WHAT to reload for a name.
//
// Rules: never while an op is running (its own post-op refresh covers
// everything); never two refreshes at once (the runOnce("refresh") gate
// manualRefresh uses too — an `r` press and a push coalesce); a reconnect
// after a dropped stream reloads everything, since events were missed.
import { runOnce, state } from "./core.js";
import { fetchStatus, wtCount } from "./status.js";
import { reconcileStatusView } from "./files.js";
import { fetchBranches } from "./sidebar.js";
import { loadCommits, renderCommits } from "./commits.js";
import { loadRepo } from "./ops.js";

const COALESCE_MS = 150; // one burst of watcher events → one refresh
const RETRY_MS = 500; // a refresh is already running → try again after it

// Sidebar-family sources all reload through fetchBranches (one Promise.all
// over every list); the header (loadRepo) rides along for the branch name
// and ahead/behind counts.
const SIDEBAR = new Set(["branches", "remotes", "worktrees", "tags", "reflog"]);

const pending = new Set();
let timer = null;
let connected = false; // a second hello is a RECONNECT → full refresh

function connectLive() {
  const es = new EventSource("/api/events");
  es.onmessage = (m) => {
    let msg;
    try {
      msg = JSON.parse(m.data);
    } catch {
      return;
    }
    if (msg.reason === "hello") {
      state.live = { enabled: !!msg.live, watch: !!msg.watch };
      if (connected) scheduleFull(); // reconnect: events were missed meanwhile
      connected = true;
      return;
    }
    for (const src of msg.changed || []) pending.add(src);
    arm(COALESCE_MS);
  };
  // EventSource reconnects on its own; the re-hello then reloads in full.
  es.onerror = () => {};
  return es;
}

function arm(ms) {
  if (timer) return;
  timer = setTimeout(() => {
    timer = null;
    flush();
  }, ms);
}

function scheduleFull() {
  pending.add("status");
  pending.add("branches");
  pending.add("feed");
  arm(COALESCE_MS);
}

function flush() {
  if (!pending.size) return;
  if (state.op) {
    // An op owns the data: its refreshAfterOp reloads everything, so the
    // pending names are moot — drop them rather than replay stale ones.
    pending.clear();
    return;
  }
  const want = new Set(pending);
  pending.clear();
  const run = runOnce("refresh", () => refreshSources(want));
  if (!run) {
    // a manual `r` or an earlier flush is mid-flight — queue behind it
    for (const s of want) pending.add(s);
    arm(RETRY_MS);
    return;
  }
  run.catch(() => {});
}

// refreshSources reloads exactly what changed. It mirrors refreshAfterOp's
// cursor discipline: read the anchor BEFORE status (the working-tree row
// can appear or vanish and shift every index), reload the feed in
// RECONCILE mode (paged history and scroll survive), re-anchor after.
async function refreshSources(want) {
  const at = state.rows[state.cursor - wtCount()];
  const keep = at && at.hash;
  const jobs = [];
  if (want.has("status")) jobs.push(fetchStatus());
  let sidebar = false;
  for (const s of want) if (SIDEBAR.has(s)) sidebar = true;
  if (sidebar) jobs.push(fetchBranches(), loadRepo());
  await Promise.all(jobs);
  if (want.has("status")) reconcileStatusView();
  if (want.has("feed")) {
    await loadCommits(false, false);
    const last = state.rows.length + wtCount() - 1;
    const i = keep ? state.rows.findIndex((r) => r.hash === keep) : -1;
    if (i >= 0) state.cursor = i + wtCount();
    else if (state.cursor > last) state.cursor = Math.max(0, last);
  }
  renderCommits();
}

export { connectLive, refreshSources };
