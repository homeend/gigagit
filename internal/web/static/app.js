// app.js — gg web client entry: imports every module (their top-level
// code — element listeners, restored UI state — runs in the original
// single-file order), then boots. The split is mechanical; each module
// matches a section of the former monolith.
import { $ } from "./core.js";
import { preflightGate } from "./preflight.js";
import "./layers.js";
// The registries (op rows / help) must exist before any feature module runs.
import "./menus.js";
import { fetchStatus } from "./status.js";
import { applySidebarHidden, loadRepo } from "./ops.js";
import { applyStoredSections, fetchBranches } from "./sidebar.js";
import { applyStoredSorts } from "./sortlist.js";
import { loadUIState, uiState } from "./uistate.js";
import "./versions.js";
import "./rebase.js";
import "./filehist.js";
import "./review.js";
import { applyStoredWidths } from "./resize.js";
import { applyGraphMode, loadCommits, renderCommits } from "./commits.js";
import { applyDiffView, refreshNoteCounts } from "./files.js";
import { focusPane } from "./keys.js";
import { fetchHealth } from "./bigrepo.js";
import "./settings.js";
import "./identity.js";
import "./prefixes.js";
import "./exttools.js";
import "./sessionerrors.js";
import "./palette.js";
// Feature modules: imported for their side effects only (they register their
// own menu rows and help rows — see menus.js).
import "./patch.js";
import "./locks.js";
import "./conflicts.js";
import "./notifications.js";
import "./gitconfig.js";
import "./agentsetup.js";
import "./commitai.js";
import "./search.js";
import "./remoteheads.js";
import "./links.js";
import { fetchPreviews } from "./previews.js";
import { applyStartAt, connectLive } from "./live.js";

// applyStoredLayout puts back the layout gg remembered for this machine:
// folded sections, pane widths, the sidebar toggle, the graph mode. It runs
// FIRST so the page settles into the remembered shape before any data lands,
// and it is a no-op on a first run (saved=false) — each module then keeps the
// defaults its own top-level code already applied.
async function applyStoredLayout() {
  await loadUIState();
  const ui = uiState();
  if (!ui) return;
  applyStoredSorts(ui.sorts); // BEFORE the sections: they draw the sort chips
  applyStoredSections(ui.sections);
  applyStoredWidths(ui.sidebar_width, ui.files_width);
  if (ui.sidebar_hidden) applySidebarHidden(true);
  applyGraphMode(ui.graph);
  applyDiffView(ui.diff_view);
}


async function boot() {
  // The migration consent gate runs FIRST, before anything else touches the
  // repo — mirroring internal/tui/preflight.go's Preflight, which runs
  // before the TUI's own event loop starts. Quit stops initialization here;
  // the panel itself explains that closing or reloading the tab is safe.
  if (!(await preflightGate())) return;
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
  const firstLoad = [
    fetchStatus().then(() => renderCommits()),
    fetchBranches(),
    fetchPreviews(), // its own fetch: previews.js cannot ride sidebar.js
    refreshNoteCounts(), // the ◆N badges, best-effort like the rest
  ];
  for (const p of firstLoad) p.catch(() => {});
  await fetchHealth(true);
  await loadCommits(false);
  focusPane();
  connectLive(); // after the first full load: pushes only name what to RE-fetch
  // `gg open --web <link>`: land where the server was started. Only once
  // EVERY first-load fetch has settled: a preview landing must find its
  // SAVED row in state.previews (openPreviewForPair falls back to a
  // show-once preview otherwise), the same previews-seen clause the TUI's
  // startAtReady gate has. Nothing above waits on this.
  await Promise.allSettled(firstLoad);
  applyStartAt().catch(() => {});
}

boot().catch((e) => {
  $("repo-name").textContent = "error: " + (e.message || e);
});

