// Permanent overviews on the page's rows (spec 2026-10-12-permanent-overviews
// §10): the commit rows' ≡ and menu rows, a branch's └ Overview sub-rows,
// and the one row menu they share — Open overview / Delete overview (the
// TUI's ruling). An overview opens in the viewer's stored mode
// (viewer.js openPermanentOverview).
import { postJSON, state } from "./core.js";
import { showCtxMenu } from "./layers.js";
import { opLine, showLocalConfirm } from "./ops.js";
import { refreshNoteCounts, renderFiles } from "./files.js";
import { openPermanentOverview } from "./viewer.js";

// --- overview rows (pure; guarded against Go) ---
// commitOverviews is commit hash's permanent overviews, newest first (R5:
// from any branch).
function commitOverviews(ovs, hash) {
  return newestFirst((ovs || []).filter((o) => hash && o.commit === hash));
}

// branchOverviews is b's overviews of its CURRENT tip, made on b (R5). b.hash
// is git's abbreviated sha.
function branchOverviews(ovs, b) {
  return newestFirst((ovs || []).filter((o) => b.hash && o.commit.startsWith(b.hash) && o.branch === b.name));
}

function newestFirst(ovs) {
  return ovs.slice().sort((a, b) => (b.created || "").localeCompare(a.created || ""));
}

// overviewStamp is the review rows' date: "MM-DD HH:MM" this year, else
// "YYYY-MM-DD HH:MM"; "" when unknown.
function overviewStamp(iso, now) {
  const d = new Date(iso || "");
  if (!iso || isNaN(d)) return "";
  const p = (n) => String(n).padStart(2, "0");
  const full = `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
  return d.getFullYear() === now.getFullYear() ? full.slice(5) : full;
}

// overviewRowText is a branch sub-row: "└ Overview: <title> 10-12 09:40 <agent>"
// (the TUI's branchOverviewRowBody).
function overviewRowText(o, now = new Date()) {
  return ["└ Overview:", o.title, overviewStamp(o.created, now), (o.agent || "").trim()].filter(Boolean).join(" ");
}
// --- end overview rows ---

// overviewMenu is an overview row's right-click menu: Open and Delete.
function overviewMenu(id, x, y, { done = null } = {}) {
  showCtxMenu(
    [
      { label: "Open overview", act: () => openPermanentOverview(id) },
      { label: "Delete overview", danger: true, act: () => confirmDeleteOverview(id, { done }) },
    ],
    x,
    y
  );
}

// confirmDeleteOverview asks, then removes overview id; the counts refresh
// takes its rows away and closes it where it is open. done runs after a
// delete (View all notes re-reads its list).
function confirmDeleteOverview(id, { done = null } = {}) {
  const all = ((state.noteCounts && state.noteCounts.overviews) || []).concat(state.previewOverviews || []);
  const o = all.find((x) => x.id === id);
  const what = o ? "\n◆ " + (o.agent || "agent") + " · " + o.title : "";
  showLocalConfirm("Delete this overview?" + what, ["cancel", "delete"], async (opt) => {
    if (opt !== "delete") return;
    try {
      await postJSON("/api/overview/remove", { id });
    } catch (e) {
      if (e.status !== 404) return opLine("delete overview: " + (e.message || e), true); // 404: already gone, the same outcome
    }
    // A PR view's block is filled by its own read, not the counts: the row
    // goes now (the review delete's way).
    state.previewOverviews = (state.previewOverviews || []).filter((x) => x.id !== id);
    await refreshNoteCounts();
    renderFiles();
    opLine("overview deleted", false);
    if (done) done();
  });
}

export { branchOverviews, commitOverviews, confirmDeleteOverview, overviewMenu, overviewRowText };
