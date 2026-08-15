// uistate.js — part of gg's web client. The layout this page remembers
// (folded sidebar sections, pane widths, sidebar hidden, graph mode) is kept
// SERVER-SIDE, in gg's machine-local state file, because `gg web` binds a
// random loopback port: every run is a different origin, so anything stored in
// localStorage is unreachable the next time gg starts.
//
// This module is transport only — it never touches the DOM and imports no
// feature module, so nothing here can create an import cycle. Each feature
// module applies its own slice of the state (see applyUIState callers in
// app.js) and calls saveUI() when the user changes it.
import { state } from "./core.js";

// loaded is the server's answer, or null until it arrives / if it failed.
// loaded.saved === false means "nothing stored yet": a first run, where each
// module keeps its own defaults instead of reading zero values as a layout.
let loaded = null;
let pending = null; // merged patch awaiting its PUT
let timer = null;

async function loadUIState() {
  try {
    const resp = await fetch("/api/uistate", { headers: { Accept: "application/json" } });
    if (!resp.ok) throw new Error(String(resp.status));
    loaded = await resp.json();
  } catch {
    loaded = null; // remembering is best-effort: never block the UI on it
  }
  state.ui = loaded;
  return loaded;
}


// uiState returns the loaded layout, or null when there is nothing to apply
// (first run, or the read failed).
function uiState() {
  return loaded && loaded.saved ? loaded : null;
}


// saveUI merges a patch into the known layout and PUTs it, coalescing bursts
// (a resize drag fires per pixel). The full object is sent every time: the
// endpoint is a replace, so a half-populated body would drop the other prefs.
function saveUI(patch) {
  const base = loaded || { sections: [], sidebar_hidden: false, sidebar_width: 0, files_width: 0, graph: "svg" };
  loaded = { ...base, ...patch, saved: true };
  state.ui = loaded;
  pending = { ...loaded };
  clearTimeout(timer);
  timer = setTimeout(flushUI, 250);
}


async function flushUI() {
  if (!pending) return;
  const body = pending;
  pending = null;
  try {
    await fetch("/api/uistate", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
  } catch {
    // Best-effort: a failed save costs the user their layout, not their work.
  }
}

export { loadUIState, saveUI, uiState };
