// settings.js — the web Settings overlay: the TUI Settings popup's
// config-value rows, grouped. Values live server-side (/api/settings); every
// control POSTs its change and re-renders from a fresh GET, so the panel
// never shows an optimistic state a failed write would falsify. Settings the
// web itself does not consume (the TUI's background-refresh lane, its
// operation log) are captioned (TUI). The sub-editors (external tools,
// identity & profiles, branch prefixes, language) stay TUI-only for now.
import { $, esc, getJSON, postJSON, state } from "./core.js";
import { closeLayer, pushLayer } from "./layers.js";
import { startOp } from "./ops.js";

const REFRESH_SOURCES = ["status", "branches", "remotes", "worktrees", "tags", "reflog", "feed", "fetch", "remote_tags"];

async function openSettings() {
  let d;
  try {
    d = await getJSON("/api/settings");
  } catch (e) {
    return; // a failing read leaves nothing to render; the op line is not ours
  }
  state.settings = d;
  renderSettings();
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

// setOpt posts one patch, then re-reads — the re-render IS the confirmation.
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

function renderSettings() {
  const d = state.settings;
  if (!d) return;
  // One label+input per line (a vertical list, not a flow) — nine wrapped
  // pairs read as noise; a column scans.
  const rates = REFRESH_SOURCES.map(
    (src) =>
      `<label class="srate"><span>${esc(src.replace("_", " "))}</span><input type="number" min="0" data-rate="${src}" value="${d.refresh[src] ?? 0}"></label>`
  ).join("");
  $("settings-box").innerHTML = `
    <h2>settings</h2>
    <h3>commits</h3>
    <div class="srow"><span class="slbl">show graph</span>${toggleBtn("show_graph", d.show_graph)}<span class="snote">lane graph vs flat list (per repo)</span></div>
    <div class="srow"><span class="slbl">commit sort</span><button class="sact" data-act="commit-sort">${esc(d.commit_sort)}</button><span class="snote">date-order: perfect lanes · plain: fastest (per repo)</span></div>
    <div class="srow"><span class="slbl">commit-graph file</span>${commitGraphRow(d)}<span class="snote">speeds up history walks on big repos</span></div>
    <h3>refresh <span class="stui">(TUI)</span></h3>
    <div class="srow"><span class="slbl">auto-refresh</span>${toggleBtn("auto_refresh", d.auto_refresh)}<span class="snote">background refresh master switch (global)</span></div>
    <div class="srow"><span class="slbl">auto remote-tag refresh</span>${toggleBtn("remote_tags_auto", d.remote_tags_auto)}<span class="snote">▲ markers after each fetch (global)</span></div>
    <div class="srow srates"><span class="slbl">intervals (s)</span><span class="sratewrap">${rates}</span></div>
    <div class="srow"><span class="snote">0 = off · per repo · applies to the TUI's background lane</span></div>
    <h3>history &amp; logs</h3>
    <div class="srow"><span class="slbl">operations history</span>${toggleBtn("versions_enabled", d.versions_enabled)}<span class="snote">pre-operation branch snapshots (per repo)</span></div>
    <div class="srow"><span class="slbl">history retention</span><input type="number" id="s-retention" value="${d.versions_max_age_days}"> <button class="sact" data-act="retention">apply</button><span class="snote">days; -1 = keep forever</span></div>
    <div class="srow"><span class="slbl">operation log <span class="stui">(TUI)</span></span>${toggleBtn("op_log", d.op_log)}<span class="snote">${esc(d.op_log_path || "")}</span></div>
    <h3>repo</h3>
    <div class="srow"><span class="slbl">settings file</span><span class="sval">${esc(d.repo_config_path)}</span><span class="snote">${d.repo_config_private ? "machine-local (private)" : "committed .gg.toml"} · move it from the TUI</span></div>
    <div class="srow shook"><span class="slbl">worktree post-create hook</span><textarea id="s-hook" rows="3" spellcheck="false">${esc(d.hook || "")}</textarea><button class="sact" data-act="hook">save</button></div>
    <div class="srow"><span class="snote">runs after create-worktree; shown for approval before it ever executes</span></div>
    <div class="serr"></div>
    <div class="sfoot">external tools · identity &amp; profiles · branch prefixes · language: in the TUI settings (,) for now · esc closes</div>`;
}

$("settings-box").addEventListener("click", (e) => {
  const t = e.target.closest("button");
  if (!t) return;
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
    case "retention": {
      const v = Number($("s-retention").value);
      if (!Number.isInteger(v)) return;
      setOpt({ versions_max_age_days: v });
      break;
    }
    case "hook":
      setOpt({ hook: $("s-hook").value });
      break;
    case "commit-graph":
      // The op writes the graph then sets fetch.writeCommitGraph — the same
      // chain the big-repo banner runs. Close first: the op line narrates.
      closeLayer("settings");
      startOp({ op: "commit-graph" }, "writing commit-graph");
      break;
  }
});

$("settings-box").addEventListener("change", (e) => {
  const inp = e.target.closest("input[data-rate]");
  if (!inp) return;
  const v = Number(inp.value);
  if (!Number.isInteger(v) || v < 0) return;
  setOpt({ refresh: { [inp.dataset.rate]: v } });
});

// Backdrop click closes (the picker-overlay convention — settings is a
// control panel, not a report; every value is re-readable).
$("settings").addEventListener("click", (e) => {
  if (e.target === $("settings")) closeLayer("settings");
});

export { openSettings, renderSettings, setOpt };
