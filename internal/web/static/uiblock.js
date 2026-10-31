// uiblock.js — part of gg's web client. One active interface (spec
// 2026-10-31-one-active-interface): a page hosted by a terminal is either the
// active interface or shows this notice. The server refuses a blocked page's
// requests (uiactive.go); here the notice takes the window, and a tab that
// loads while blocked holds its boot until it is active.
import { $, getJSON, postJSON, state } from "./core.js";
import { closeLayer, topLayer } from "./layers.js";
import { closeConsole } from "./console.js";
import { closeViewer } from "./viewer.js";
import { connectLive, resumeActive } from "./live.js";
import { opLine, showLocalConfirm } from "./ops.js";
import { quitFromMenu } from "./palette.js";

let releaseBoot = null; // set while a tab that loaded blocked waits to boot

function showBlock(on) {
  $("ui-block").classList.toggle("hidden", !on);
  $("ui-block-err").textContent = "";
  if (on) $("ui-block-activate").focus();
}

// closeWindows leaves nothing to come back stale: what esc would lose, and
// nothing more. A running session goes on; only its console closes.
function closeWindows() {
  closeConsole();
  closeViewer("background");
  for (let top = topLayer(); top; top = topLayer()) closeLayer(top.id);
}

function applyUIActive(active) {
  if (active === !state.uiBlocked) return;
  state.uiBlocked = !active;
  if (!active) {
    closeWindows();
    showBlock(true);
    return;
  }
  showBlock(false);
  if (releaseBoot) {
    releaseBoot();
    releaseBoot = null;
    return;
  }
  resumeActive();
}

// checkUIActive re-reads the flag: a flip may have gone out while the stream
// was down.
async function checkUIActive() {
  const q = await getJSON("/api/ui-active").catch(() => null);
  if (q) applyUIActive(!!q.active);
}

// uiWaitActive is boot's first step: it returns once this tab may touch the
// repository. The stream starts here so a blocked tab hears the flip.
async function uiWaitActive() {
  const q = await getJSON("/api/ui-active").catch(() => null);
  if (!q || q.active) return;
  state.hosted = !!q.hosted;
  state.uiBlocked = true;
  showBlock(true);
  connectLive();
  await new Promise((resolve) => { releaseBoot = resolve; });
}

// hostedOnly: a row only a page with a terminal behind it has. A standalone gg
// web has nothing to switch to.
function hostedOnly(row) {
  return state.hosted ? [row] : [];
}

// switchToTUI is the palette's and the ☰ menu's Switch to TUI.
function switchToTUI() {
  showLocalConfirm("Switch to the TUI? This page stays blocked until you switch back.", ["cancel", "switch"], async (o) => {
    if (o !== "switch") return;
    try {
      await postJSON("/api/ui-active", { side: "tui" });
      applyUIActive(false);
    } catch (e) {
      opLine(String(e.message || e), true);
    }
  });
}

$("ui-block-activate").addEventListener("click", async () => {
  try {
    await postJSON("/api/ui-active", { side: "web" });
    applyUIActive(true);
  } catch (e) {
    $("ui-block-err").textContent = String(e.message || e);
  }
});
$("ui-block-quit").addEventListener("click", () => quitFromMenu());

export { hostedOnly, applyUIActive, checkUIActive, switchToTUI, uiWaitActive };
