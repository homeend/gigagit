// app.js — gg web client entry: imports every module (their top-level
// code — element listeners, restored UI state — runs in the original
// single-file order), then boots. The split is mechanical; each module
// matches a section of the former monolith.
import { $ } from "./core.js";
import "./layers.js";
// The registries (op rows / help) must exist before any feature module runs.
import "./menus.js";
import { fetchStatus } from "./status.js";
import { applySidebarHidden, loadRepo } from "./ops.js";
import { applyStoredSections, fetchBranches } from "./sidebar.js";
import { loadUIState, uiState } from "./uistate.js";
import "./versions.js";
import "./rebase.js";
import "./filehist.js";
import "./review.js";
import { applyStoredWidths } from "./resize.js";
import { applyGraphMode, loadCommits, renderCommits } from "./commits.js";
import "./files.js";
import { focusPane } from "./keys.js";
import { fetchHealth } from "./bigrepo.js";
import "./settings.js";
import "./identity.js";
import "./prefixes.js";
import "./exttools.js";
import "./sessionerrors.js";
import "./palette.js";
import "./notifications.js";

// applyStoredLayout puts back the layout gg remembered for this machine:
// folded sections, pane widths, the sidebar toggle, the graph mode. It runs
// FIRST so the page settles into the remembered shape before any data lands,
// and it is a no-op on a first run (saved=false) — each module then keeps the
// defaults its own top-level code already applied.
async function applyStoredLayout() {
  await loadUIState();
  const ui = uiState();
  if (!ui) return;
  applyStoredSections(ui.sections);
  applyStoredWidths(ui.sidebar_width, ui.files_width);
  if (ui.sidebar_hidden) applySidebarHidden(true);
  applyGraphMode(ui.graph);
}


async function boot() {
  await applyStoredLayout();
  await loadRepo();
  // Neither status (a MINUTE of working-tree scan on a huge repo) nor the
  // sidebar (tags alone cost ~7s with hundreds of tags — for-each-ref peels
  // and abbreviates per tag) may gate the first commits render; awaiting
  // them serially here is what showed a bare wireframe page on big repos
  // until an F5 raced past it. Each fills its own panel when it lands —
  // status additionally re-renders commits because the working-tree row is
  // status-driven and the pane has usually rendered by then. Only health
  // stays awaited: it is cheap and the [ui] show_graph default must land
  // before the first commits render.
  fetchStatus().then(() => renderCommits()).catch(() => {});
  fetchBranches().catch(() => {});
  await fetchHealth(true);
  await loadCommits(false);
  focusPane();
}

boot().catch((e) => {
  $("repo-name").textContent = "error: " + (e.message || e);
});

