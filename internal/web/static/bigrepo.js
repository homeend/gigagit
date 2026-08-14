// bigrepo.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, getJSON, lsGet, lsSet, postJSON, ssGet, ssSet, state } from "./core.js";
import { saveUI, uiState } from "./uistate.js";
import { opLine, startOp } from "./ops.js";
import { loadCommits, renderCommits } from "./commits.js";

$("bigrepo-graphoff").onclick = async () => {
  try {
    await postJSON("/api/ui-config", { show_graph: "off", commit_sort: "plain" });
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    return; // banner stays; the action can be retried
  }
  state.graphMode = "off";
  lsSet("gg.graph", "off"); // this browser matches immediately and keeps matching
  saveUI({ graph: "off" }); // and the next run, which has a different origin
  await loadCommits(false); // sort changed server-side — reload the feed
  fetchHealth();
};

$("bigrepo-cgraph").onclick = () => startOp({ op: "commit-graph" }, "write commit-graph");

// sessionStorage survives the re-root reload, so the key is scoped per repo —
// otherwise "not now" in one repo would suppress the banner in another.
function bigrepoLaterKey() {
  return "gg.bigrepo.later:" + (state.repo ? state.repo.worktree : "");
}

$("bigrepo-later").onclick = () => {
  ssSet(bigrepoLaterKey(), "1"); // session-only, re-evaluated next visit
  setBigRepoBarHidden(true);
};

$("bigrepo-never").onclick = async () => {
  // dismiss only the ids the banner is currently showing
  const groups = bigRepoGroups();
  const ids = [];
  if (groups.includes("graphoff")) ids.push("web_graph_off_suggest");
  if (groups.includes("cgraph")) ids.push("commit_graph_recommend");
  try {
    for (const id of ids) await postJSON("/api/notice-dismiss", { id });
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    return;
  }
  fetchHealth();
};

// fetchHealth loads /api/health and re-renders the big-repo banner. With
// applyDefault (boot only), a repo-config show_graph=off becomes the initial
// graph mode when this browser has no localStorage override — [ui] show_graph
// honored as the web default, the TUI-parity fix. Failures are silent: no
// banner, existing localStorage-or-default behavior (the TUI's "health never
// surfaces errors" rule).
async function fetchHealth(applyDefault) {
  try {
    state.health = await getJSON("/api/health");
  } catch {
    state.health = null;
  }
  // A layout saved on this machine already decided the graph mode at boot;
  // the repo default only speaks for a UI that has never been told otherwise.
  if (applyDefault && state.health && !uiState() && lsGet("gg.graph") === null && state.health.show_graph === "off") {
    state.graphMode = "off";
  }
  renderBigRepoBanner();
}


// bigRepoGroups derives which action groups still apply — empty means no
// banner. "graphoff": the effective graph state is on (config not off AND no
// per-browser off override) OR the sort is not plain (either misalignment
// keeps the offer; accepting writes both keys). "cgraph": exactly the TUI
// notice's conditions.
function bigRepoGroups() {
  const h = state.health;
  if (!h || !h.big) return [];
  const groups = [];
  const graphOn = h.show_graph !== "off" && state.graphMode !== "off";
  if (!h.dismissed.web_graph_off_suggest && (graphOn || h.commit_sort !== "plain")) groups.push("graphoff");
  if (!h.dismissed.commit_graph_recommend && !h.has_commit_graph && !h.write_commit_graph_set) groups.push("cgraph");
  return groups;
}


// setBigRepoBarHidden toggles #bigrepo-bar's hidden class and re-renders the
// commits list exactly once when the visibility actually flips: hiding or
// showing the bar changes the panes' height, and the virtualized commit list
// otherwise keeps its stale render window until the next interaction (a
// blank strip). renderCommits() is already called unconditionally on window
// resize, so it is safe to call with no commits loaded yet.
function setBigRepoBarHidden(hidden) {
  const bar = $("bigrepo-bar");
  const was = bar.classList.contains("hidden");
  bar.classList.toggle("hidden", hidden);
  if (was !== hidden) renderCommits();
}


function renderBigRepoBanner() {
  if (ssGet(bigrepoLaterKey()) === "1") { setBigRepoBarHidden(true); return; }
  const groups = bigRepoGroups();
  if (!groups.length) { setBigRepoBarHidden(true); return; }
  $("bigrepo-msg").textContent =
    "big repository (" + state.health.pack_mb + " MB of packs) — commit browsing can be faster:";
  $("bigrepo-graphoff").classList.toggle("hidden", !groups.includes("graphoff"));
  $("bigrepo-cgraph").classList.toggle("hidden", !groups.includes("cgraph"));
  setBigRepoBarHidden(false);
}

export { bigRepoGroups, bigrepoLaterKey, fetchHealth, renderBigRepoBanner, setBigRepoBarHidden };
