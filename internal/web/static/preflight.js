// preflight.js — part of gg's web client. The migration consent gate: what
// internal/tui/preflight.go asks at a terminal prompt before the TUI starts,
// this asks in a panel before the SPA renders anything else. See app.js
// (the entry module) for where preflightGate() is called — it must run
// FIRST, before applyStoredLayout/loadRepo/etc., exactly as the TUI's
// Preflight runs before the TUI's own event loop starts.
//
// Only core.js is imported: this module runs before any other feature
// module's state exists, so it must not depend on anything that assumes
// boot has already happened.
import { $, getJSON, postJSON, state } from "./core.js";

// preflightGate fetches /api/preflight and, when anything is pending, walks
// the consent panel one migration at a time. It resolves to false only when
// the user quits — mirroring internal/tui/preflight.go's Preflight, which
// returns proceed=false only on Quit (or an unsatisfiable Required feature,
// which the web server itself cannot even have booted past). A fetch
// failure is treated as "proceed": a probe failure must never lock the user
// out, same as the TUI comment states.
async function preflightGate() {
  let body;
  try {
    body = await getJSON("/api/preflight");
  } catch {
    return true;
  }
  state.preflight = body;
  const pending = body.pending || [];
  if (!pending.length) return true;

  for (const m of pending) {
    const proceed = await askMigration(m);
    if (!proceed) return false;
  }
  // Refresh what the rest of the app gates on (disabled feature ids) now
  // that every migration has been decided.
  try {
    state.preflight = await getJSON("/api/preflight");
  } catch {
    // leave the last-known state; nothing here is safety-critical
  }
  return true;
}

// askMigration shows one migration's consent panel and resolves to whether
// launch should proceed — true for Migrate (once applied) or Skip, false
// for Quit. There is deliberately no suppression of any kind: a browser
// reload is this surface's "next launch", and the TUI's own comment says a
// feature silently left off is exactly the outcome this gate exists to
// prevent, so it is asked again every time, same as the terminal.
function askMigration(m) {
  return new Promise((resolve) => {
    $("preflight-title").textContent = m.feature + " needs a one-time migration";
    $("preflight-body").textContent =
      m.consequence + "\nThis discards " + m.refs.length + " entries and cannot be undone.";
    const migrateBtn = $("preflight-migrate");
    const skipBtn = $("preflight-skip");
    const quitBtn = $("preflight-quit");
    migrateBtn.disabled = false;
    migrateBtn.textContent = "Migrate";
    $("preflight").classList.remove("hidden");

    const cleanup = () => {
      migrateBtn.onclick = null;
      skipBtn.onclick = null;
      quitBtn.onclick = null;
    };

    migrateBtn.onclick = async () => {
      migrateBtn.disabled = true;
      migrateBtn.textContent = "migrating…";
      try {
        await postJSON("/api/preflight/migrate", { feature: m.feature });
      } catch (e) {
        $("preflight-body").textContent = "migration failed: " + (e.message || e);
        migrateBtn.disabled = false;
        migrateBtn.textContent = "Migrate";
        return; // panel stays up — the user can retry, skip, or quit
      }
      cleanup();
      $("preflight").classList.add("hidden");
      resolve(true);
    };

    skipBtn.onclick = () => {
      cleanup();
      $("preflight").classList.add("hidden");
      resolve(true); // proceed with the feature disabled, exactly like the TUI's default case
    };

    quitBtn.onclick = () => {
      cleanup();
      // A browser tab cannot terminate the server process the way the TUI's
      // Quit exits gg — the honest equivalent is to stop initializing the
      // app and say so, leaving the tab safe to close or reload.
      $("preflight-title").textContent = "gg web did not start";
      $("preflight-body").textContent = "Close this tab, or reload the page to be asked again.";
      $("preflight-actions").classList.add("hidden");
      resolve(false);
    };
  });
}

// featureDisabled reports whether id was reported disabled by the last
// /api/preflight fetch — the hook other modules use to hide their own entry
// points (e.g. the "previous versions…" rows) for a feature preflight
// turned off.
function featureDisabled(id) {
  return !!(state.preflight && state.preflight.disabled && state.preflight.disabled.includes(id));
}

export { askMigration, featureDisabled, preflightGate };
