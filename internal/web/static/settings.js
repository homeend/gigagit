// settings.js — the web Settings overlay: the TUI Settings popup's
// config-value rows, grouped.
//
// Two commit models, chosen by control kind:
//   - Buttons (toggles, commit sort, the commit-graph action) apply
//     IMMEDIATELY on click — a click is the whole intent.
//   - Text fields (the nine intervals, retention, the hook textarea) apply
//     ONLY on the explicit save button. Editing any of them shows an
//     unsaved-changes bar; save disables every control for the flight of
//     ONE batched POST, then re-renders from a fresh GET (bar gone,
//     controls re-enabled). Nothing auto-commits on blur — auto-commit is
//     what made every re-render fight the user's cursor.
//
// Settings the web itself does not consume (the TUI's background-refresh
// lane, its operation log) are captioned (TUI). The sub-editors still
// TUI-only: the external-tools WIZARD, language. Identity & profiles,
// branch prefixes and the external-tools view have their own overlays
// (identity.js, prefixes.js, exttools.js).
import { $, esc, getJSON, lsSet, postJSON, state } from "./core.js";
import { closeLayer, pushLayer } from "./layers.js";
import { startOp } from "./ops.js";
import { applyGraphMode, loadCommits } from "./commits.js";
import { saveUI } from "./uistate.js";

const REFRESH_SOURCES = ["status", "branches", "remotes", "worktrees", "tags", "reflog", "feed", "fetch", "remote_tags"];

// The watch-eligible [refresh] sources — the ones gitwatch can drive from
// .git changes instead of a timer (the server's liveWatchSources).
const WATCH_SOURCES = ["worktrees", "branches", "reflog", "remotes"];

async function openSettings() {
  let d;
  try {
    d = await getJSON("/api/settings");
  } catch (e) {
    return; // a failing read leaves nothing to render; the op line is not ours
  }
  state.settings = d;
  renderSettings({ fresh: true }); // never inherit stale edits from a previous open
  pushLayer("settings", $("settings"), {
    onKey: (e) => {
      if (e.key === "Escape") {
        closeLayer("settings");
        e.preventDefault();
        return true;
      }
      // Swallow WITHOUT preventDefault (the ctx-menu convention): the app's
      // hotkeys must not fire under the overlay, but default behavior must —
      // preventDefault here is what would make every input/textarea deaf.
      return true;
    },
  });
}

// setOpt is the BUTTON path: post one patch, re-read, re-render. Pending
// text-field edits survive the rebuild (renderSettings reapplies them).
async function setOpt(patch) {
  const box = $("settings-box");
  try {
    await postJSON("/api/settings", patch);
  } catch (e) {
    const err = box.querySelector(".serr");
    if (err) err.textContent = "not saved: " + e.message;
    return;
  }
  try {
    state.settings = await getJSON("/api/settings");
  } catch {}
  renderSettings();
  applyToPage(patch);
}

// applyToPage makes the two keys the web itself consumes take effect on the
// page that just changed them — storing them was never the whole intent.
//   - show_graph: the commit list redraws in the new mode, and the browser's
//     own graph pref (uistate + the same-session cache, what the g key
//     writes) is brought in line, or the next boot would silently undo the
//     click: a saved layout outranks the repo default at boot.
//   - commit_sort: the server dropped its feed; re-read it from the top or
//     the old order stays on screen until something else reloads.
function applyToPage(patch) {
  if ("show_graph" in patch) {
    const mode = patch.show_graph === "off" ? "off" : "svg";
    applyGraphMode(mode);
    lsSet("gg.graph", mode);
    saveUI({ graph: mode });
  }
  if ("commit_sort" in patch) loadCommits(false, true).catch(() => {});
}

// pendingEdits maps field key → raw typed value for every text field whose
// content differs from the server state. Drives the unsaved bar, and lets a
// toggle's re-render carry the user's typing across the rebuild.
function pendingEdits() {
  const box = $("settings-box");
  const d = state.settings;
  const out = {};
  if (!d || !box.firstChild) return out;
  for (const src of REFRESH_SOURCES) {
    const inp = box.querySelector(`input[data-rate="${src}"]`);
    if (inp && inp.value !== String(d.refresh[src] ?? 0)) out["rate:" + src] = inp.value;
  }
  const ret = document.getElementById("s-retention");
  if (ret && ret.value !== String(d.versions_max_age_days)) out.retention = ret.value;
  const hook = document.getElementById("s-hook");
  if (hook && hook.value.trim() !== (d.hook || "").trim()) out.hook = hook.value;
  return out;
}

// savePatch turns the pending edits into the wire patch: only parseable
// values are sent; an emptied field is NOT a value and simply reverts to
// the server state when the save re-renders.
function savePatch() {
  const d = state.settings;
  const edits = pendingEdits();
  const patch = {};
  const refresh = {};
  for (const [k, raw] of Object.entries(edits)) {
    if (k.startsWith("rate:")) {
      const v = parseInt(raw, 10);
      if (Number.isInteger(v) && v >= 0) refresh[k.slice(5)] = v;
    } else if (k === "retention") {
      const v = parseInt(raw, 10);
      if (Number.isInteger(v) && v !== d.versions_max_age_days) patch.versions_max_age_days = v;
    } else if (k === "hook") {
      patch.hook = raw;
    }
  }
  if (Object.keys(refresh).length) patch.refresh = refresh;
  return patch;
}

function updateSaveBar() {
  const bar = $("settings-box").querySelector(".ssave");
  if (bar) bar.classList.toggle("hidden", Object.keys(pendingEdits()).length === 0);
}

// saveSettings: one batched POST for every edited field, with EVERY control
// locked for the flight. Success re-renders from a fresh GET — bar gone,
// controls new and enabled. Failure unlocks and names the refusal.
async function saveSettings() {
  const box = $("settings-box");
  const patch = savePatch();
  if (!Object.keys(patch).length) {
    renderSettings({ fresh: true }); // only unusable edits (emptied fields) — restore
    return;
  }
  const controls = [...box.querySelectorAll("input, textarea, button")];
  controls.forEach((el) => (el.disabled = true));
  try {
    await postJSON("/api/settings", patch);
  } catch (e) {
    controls.forEach((el) => (el.disabled = false));
    const err = box.querySelector(".serr");
    if (err) err.textContent = "not saved: " + e.message;
    return;
  }
  try {
    state.settings = await getJSON("/api/settings");
  } catch {}
  const err = box.querySelector(".serr");
  if (err) err.textContent = "";
  // Fresh render: the edits were just saved, so the server state IS the truth.
  renderSettings({ fresh: true });
}

const onOff = (b) => (b ? "on" : "off");
const toggleBtn = (key, val) => `<button class="stgl${val ? " on" : ""}" data-k="${key}">${onOff(val)}</button>`;

function commitGraphRow(d) {
  if (!d.commit_graph_known) return `<span class="sval">checking…</span>`;
  if (d.commit_graph_present && d.commit_graph_auto) return `<span class="sval">present, auto-refresh on</span>`;
  const label = d.commit_graph_present ? "keep it fresh (fetch.writeCommitGraph)" : "write + keep fresh";
  const stateTxt = d.commit_graph_present ? "present, auto-refresh off" : "missing";
  return `<span class="sval">${stateTxt}</span> <button class="sact" data-act="commit-graph">${esc(label)}</button>`;
}

function renderSettings(opts = {}) {
  const d = state.settings;
  if (!d) return;
  // A toggle's re-render must not destroy typing in progress: carry the
  // pending text-field edits and the focused control across the rebuild.
  // fresh renders (open, after a save, after an emptied-fields restore)
  // drop them — the server state is the truth there.
  const pending = opts.fresh ? {} : pendingEdits();
  const active = document.activeElement;
  let restore = null;
  if (active && $("settings-box").contains(active)) {
    restore = active.dataset && active.dataset.rate ? { rate: active.dataset.rate } : { id: active.id };
  }
  // One label+input per line (a vertical list, not a flow) — nine wrapped
  // pairs read as noise; a column scans.
  //
  // type=text + a keystroke sanitizer, NOT type=number: a number input
  // whose content goes invalid ("12e", letters on some platforms) reports
  // value="" — and Number("") is 0, so a save would silently write 0 over
  // the real value. Text inputs always report what is shown.
  // Watch-eligible rows carry a file-watch toggle: on, the source refreshes
  // on .git change and its interval is ignored (the TUI's [w] column).
  const rates = REFRESH_SOURCES.map((src) => {
    const watch = WATCH_SOURCES.includes(src)
      ? `<span class="swatch">file watch ${toggleBtn("watch:" + src, !!(d.refresh_watch || {})[src])}</span>`
      : "";
    return `<div class="sraterow"><label class="srate"><span>${esc(src.replace("_", " "))}</span><input type="text" inputmode="numeric" data-rate="${src}" value="${d.refresh[src] ?? 0}"></label>${watch}</div>`;
  }).join("");
  const watchNote = d.watch_supported
    ? "file watch: refresh on .git change instead of the interval"
    : "file watch unsupported on this filesystem (9p) — the interval is used";
  $("settings-box").innerHTML = `
    <h2>settings</h2>
    <h3>commits</h3>
    <div class="srow"><span class="slbl">show graph</span>${toggleBtn("show_graph", d.show_graph)}<span class="snote">lane graph vs flat list (per repo)</span></div>
    <div class="srow"><span class="slbl">commit sort</span><button class="sact" data-act="commit-sort">${esc(d.commit_sort)}</button><span class="snote">date-order: perfect lanes · plain: fastest (per repo)</span></div>
    <div class="srow"><span class="slbl">commit-graph file</span>${commitGraphRow(d)}<span class="snote">speeds up history walks on big repos</span></div>
    <h3>refresh</h3>
    <div class="srow"><span class="slbl">auto-refresh</span>${toggleBtn("auto_refresh", d.auto_refresh)}<span class="snote">background refresh master switch (global)</span></div>
    <div class="srow"><span class="slbl">auto remote-tag refresh <span class="stui">(TUI)</span></span>${toggleBtn("remote_tags_auto", d.remote_tags_auto)}<span class="snote">▲ markers after each fetch (global)</span></div>
    <div class="srow srates"><span class="slbl">intervals (s)</span><span class="sratewrap">${rates}</span></div>
    <div class="srow"><span class="snote">0 = off · min 10 · per repo · applies to the TUI and to this page · ${watchNote}</span></div>
    <h3>history &amp; logs</h3>
    <div class="srow"><span class="slbl">operations history</span>${toggleBtn("versions_enabled", d.versions_enabled)}<span class="snote">pre-operation branch snapshots (per repo)</span></div>
    <div class="srow"><span class="slbl">history retention</span><input type="text" inputmode="numeric" id="s-retention" value="${d.versions_max_age_days}"><span class="snote">days; -1 = keep forever</span></div>
    <div class="srow"><span class="slbl">operation log <span class="stui">(TUI)</span></span>${toggleBtn("op_log", d.op_log)}<span class="snote">${esc(d.op_log_path || "")}</span></div>
    <h3>repo</h3>
    <div class="srow"><span class="slbl">settings file</span><span class="sval">${esc(d.repo_config_path)}</span><span class="snote">${d.repo_config_private ? "machine-local (private)" : "committed .gg.toml"} · move it from the TUI</span></div>
    <div class="srow shook"><span class="slbl">worktree post-create hook</span><textarea id="s-hook" rows="3" spellcheck="false">${esc(d.hook || "")}</textarea></div>
    <div class="srow"><span class="snote">runs after create-worktree; shown for approval before it ever executes</span></div>
    <div class="ssave hidden"><span class="swarn">unsaved changes — nothing is applied until you save</span><button data-act="save">save</button></div>
    <div class="serr"></div>
    <div class="sfoot">toggles apply on click · edited fields need <b>save</b> · identity &amp; profiles, branch prefixes, external tools: in the ☰ git group · the tools wizard and language: in the TUI settings (,) for now · esc closes</div>`;
  for (const [k, raw] of Object.entries(pending)) {
    const el = k.startsWith("rate:")
      ? $("settings-box").querySelector(`input[data-rate="${k.slice(5)}"]`)
      : document.getElementById(k === "retention" ? "s-retention" : "s-hook");
    if (el) el.value = raw;
  }
  updateSaveBar();
  if (restore) {
    const el = restore.rate
      ? $("settings-box").querySelector(`input[data-rate="${restore.rate}"]`)
      : restore.id && document.getElementById(restore.id);
    if (el && el.focus) {
      el.focus();
      if (el.select) el.select();
    }
  }
}

$("settings-box").addEventListener("click", (e) => {
  const t = e.target.closest("button");
  if (!t || t.disabled) return;
  if (t.dataset.k && t.dataset.k.startsWith("watch:")) {
    const src = t.dataset.k.slice(6);
    const cur = !!(state.settings.refresh_watch || {})[src];
    setOpt({ refresh_watch: { [src]: !cur } });
    return;
  }
  if (t.dataset.k) {
    const cur = !!state.settings[t.dataset.k];
    // show_graph is an enum on the wire ("on"/"off"), a bool in the GET.
    setOpt(t.dataset.k === "show_graph" ? { show_graph: cur ? "off" : "on" } : { [t.dataset.k]: !cur });
    return;
  }
  switch (t.dataset.act) {
    case "commit-sort":
      setOpt({ commit_sort: state.settings.commit_sort === "plain" ? "date-order" : "plain" });
      break;
    case "save":
      saveSettings();
      break;
    case "commit-graph":
      // The op writes the graph then sets fetch.writeCommitGraph — the same
      // chain the big-repo banner runs. Close first: the op line narrates.
      closeLayer("settings");
      startOp({ op: "commit-graph" }, "writing commit-graph");
      break;
  }
});

// Keystroke-level sanitizing: digits only for the rates, digits plus one
// leading minus for retention. A letter never lands, so there is no invalid
// state to validate later — paste included ("input" fires for both). Every
// edit re-evaluates the unsaved bar.
$("settings-box").addEventListener("input", (e) => {
  const inp = e.target;
  if (inp instanceof HTMLInputElement) {
    if (inp.dataset.rate) {
      const clean = inp.value.replace(/\D/g, "");
      if (clean !== inp.value) inp.value = clean;
    } else if (inp.id === "s-retention") {
      const clean = inp.value.replace(/[^0-9-]/g, "").replace(/(?!^)-/g, "");
      if (clean !== inp.value) inp.value = clean;
    }
  }
  updateSaveBar();
});

// Backdrop click closes (the picker-overlay convention — settings is a
// control panel, not a report; every value is re-readable, and the bar has
// warned about anything unsaved).
$("settings").addEventListener("click", (e) => {
  if (e.target === $("settings")) closeLayer("settings");
});

export { openSettings, renderSettings, saveSettings, setOpt };
