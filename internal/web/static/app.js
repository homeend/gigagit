// app.js — gg web client entry: imports every module (their top-level
// code — element listeners, restored UI state — runs in the original
// single-file order), then boots. The split is mechanical; each module
// matches a section of the former monolith.
import { $ } from "./core.js";
import "./layers.js";
import { fetchStatus } from "./status.js";
import { loadRepo } from "./ops.js";
import { fetchBranches } from "./sidebar.js";
import "./versions.js";
import "./rebase.js";
import "./filehist.js";
import "./review.js";
import "./resize.js";
import { loadCommits, renderCommits } from "./commits.js";
import "./files.js";
import { focusPane } from "./keys.js";
import { fetchHealth } from "./bigrepo.js";
import "./settings.js";
import "./identity.js";
import "./prefixes.js";
import "./palette.js";

async function boot() {
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

