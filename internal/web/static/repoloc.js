// repoloc.js — the TUI's Settings → Repo settings location: copy or move the
// whole per-repo config between the committed .gg.toml and the private
// user-dir file. The paths are the server's; an action whose destination
// holds content asks "Overwrite <dst> ?" first.
import { $, esc, getJSON, postJSON } from "./core.js";
import { closeLayer, pushLayer } from "./layers.js";
import { opLine } from "./ops.js";

let loc = null; // GET /api/settings → repo_location
let sel = 0;
let confirm = null; // {action, text} while the overwrite question shows
let busy = false;
let onDone = null; // the opener's follow-up after a relocation

// repoLocLabel is an action id's TUI label.
function repoLocLabel(id) {
  return (
    {
      "copy-to-private": "Copy to private (user dir)",
      "move-to-private": "Move to private (user dir)",
      "copy-to-committed": "Copy to committed (.gg.toml)",
      "move-to-committed": "Move to committed (.gg.toml)",
    }[id] || "?"
  );
}

// repoLocSlot is the TUI's slotDisplay: present/absent, then the path.
function repoLocSlot(slot) {
  if (!slot || !slot.path) return "(unavailable)";
  return (slot.exists ? "present  " : "absent   ") + slot.path;
}

async function readLocation() {
  const d = await getJSON("/api/settings");
  loc = d.repo_location || { committed: {}, private: {}, actions: [] };
  if (sel >= loc.actions.length) sel = 0;
}

// openRepoLocation opens the popup over the settings panel; done runs after
// a relocation landed (the settings panel re-reads).
async function openRepoLocation(done) {
  onDone = done || null;
  sel = 0;
  confirm = null;
  try {
    await readLocation();
  } catch (e) {
    opLine("repo settings: " + (e.message || e), true);
    return;
  }
  render();
  pushLayer("repoloc", $("repoloc"), {
    onKey: (e) => {
      if (confirm) {
        // The TUI's: y / enter overwrite, anything else cancels.
        if (e.key === "y" || e.key === "Y" || e.key === "Enter") run(confirm.action, true);
        else {
          confirm = null;
          render();
        }
        e.preventDefault();
        return true;
      }
      if (e.key === "Escape") {
        closeLayer("repoloc");
      } else if (e.key === "ArrowUp" || e.key === "ArrowDown") {
        const n = loc.actions.length;
        if (n) sel = e.key === "ArrowUp" ? Math.max(0, sel - 1) : Math.min(n - 1, sel + 1);
        render();
      } else if (e.key === "Enter") {
        if (loc.actions.length) run(loc.actions[sel], false);
      } else return true;
      e.preventDefault();
      return true;
    },
  });
}

// run is the TUI's enter: the server checks the destination (409 = ask),
// copies, removes the source on a move; the popup closes on success.
async function run(action, overwrite) {
  if (busy) return;
  busy = true;
  try {
    const res = await postJSON("/api/settings/repo-location", { action, overwrite });
    confirm = null;
    if (res.err) {
      opLine(res.msg, true);
      await readLocation().catch(() => {});
      render();
      return;
    }
    opLine(res.msg);
    closeLayer("repoloc");
    if (onDone) onDone();
  } catch (e) {
    if (e.data && e.data.confirm) {
      confirm = { action, text: e.data.confirm };
    } else {
      confirm = null;
      opLine("repo settings: " + (e.message || e), true);
      await readLocation().catch(() => {});
    }
    render();
  } finally {
    busy = false;
  }
}

function render() {
  if (!loc) return;
  const rows = `
    <div class="srow"><span class="slbl">committed</span><span class="sval">${esc(repoLocSlot(loc.committed))}</span></div>
    <div class="srow"><span class="slbl">private</span><span class="sval">${esc(repoLocSlot(loc.private))}</span></div>`;
  let body;
  if (confirm) {
    body = `<div class="srow"><span class="sval">${esc(confirm.text)}</span></div>
      <div class="srow"><span class="snote khint">[y] overwrite  [n/esc] cancel</span><span class="pbtns"><button data-rl="yes">overwrite</button><button data-rl="no">cancel</button></span></div>`;
  } else if (!loc.actions.length) {
    body = `<div class="srow"><span class="snote">(nothing to move — no per-repo config here, or not in a repo)</span></div>
      <div class="srow"><span class="snote khint">[esc] close</span></div>`;
  } else {
    const acts = loc.actions
      .map((a, i) => `<div class="rlact${i === sel ? " sel" : ""}"><button data-rl-act="${esc(a)}" data-i="${i}">${esc(repoLocLabel(a))}</button></div>`)
      .join("");
    body = `${acts}
      <div class="srow"><span class="snote">active file = private if present; move deletes the source (may dirty a tracked .gg.toml)</span></div>
      <div class="srow"><span class="snote khint">[↑/↓] select  [enter] do  [esc] close</span></div>`;
  }
  $("repoloc-box").innerHTML = `<h2>Repo settings location</h2>${rows}<div class="rlbody">${body}</div>`;
}

$("repoloc").addEventListener("click", (e) => {
  if (e.target === $("repoloc")) {
    closeLayer("repoloc");
    return;
  }
  const b = e.target.closest("[data-rl], [data-rl-act]");
  if (!b) return;
  if (b.dataset.rl === "yes") run(confirm.action, true);
  else if (b.dataset.rl === "no") {
    confirm = null;
    render();
  } else {
    sel = Number(b.dataset.i);
    run(b.dataset.rlAct, false);
  }
});

export { openRepoLocation };
