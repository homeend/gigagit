// gitconfig.js — the git-config explorer, the TUI's config popup in the
// browser. One overlay: the curated catalog with each key's effective value
// and where it came from, an editor per key, and the keys git has set that
// gg does not curate, listed read-only.
//
// Everything registers itself (menus.js / layers.js, task 0) rather than
// editing palette.js, index.html or style.css, so the feature is one file
// plus its endpoints.
import { $, esc, getJSON, postJSON } from "./core.js";
import { closeLayer, mountOverlay, pushLayer } from "./layers.js";
import { registerHelp, registerRows } from "./menus.js";
import { followOp, opBusy, opLine } from "./ops.js";

let cfg = null; // {catalog, extra, filter, open} while the overlay is up

// The sheet hides every surface by id — there is no global `.hidden` rule, and
// an element that relies on one is plainly visible (that bug has shipped
// here). style.css belongs to nobody in this wave, so the overlay brings its
// own rules, its own `.hidden` among them; the recipe is the settings
// overlay's, so this looks like the rest of the app rather than like a
// stylesheet-less form.
function injectStyle() {
  if ($("gg-gitconfig-style")) return;
  const st = document.createElement("style");
  st.id = "gg-gitconfig-style";
  st.textContent = `
#gg-gitconfig {
  position: fixed; inset: 0; background: rgba(0,0,0,.55);
  display: flex; align-items: center; justify-content: center; z-index: 11;
}
#gg-gitconfig.hidden { display: none; }
#gg-gitconfig .box {
  background: var(--bg-alt); border: 1px solid var(--accent); border-radius: 6px;
  padding: 18px 24px; width: 860px; max-width: 94vw; max-height: 86vh;
  overflow-y: auto; font-size: 13px;
}
#gg-gitconfig h2 { margin: 0 0 10px; font-size: 15px; }
#gg-gitconfig h3 {
  margin: 22px 0 8px; padding-top: 12px; border-top: 1px solid var(--border);
  font-size: 12px; color: var(--dim); text-transform: uppercase; letter-spacing: .05em;
}
#gg-gitconfig input, #gg-gitconfig select {
  background: var(--bg); color: var(--fg); border: 1px solid var(--border);
  border-radius: 4px; padding: 2px 8px; font: inherit; font-size: 12px;
}
#gg-gitconfig input:focus { outline: none; border-color: var(--accent); }
#gg-gitconfig #gc-filter { width: 100%; margin-bottom: 8px; }
#gg-gitconfig button {
  background: var(--bg); color: var(--fg); border: 1px solid var(--border);
  border-radius: 4px; padding: 2px 12px; font: inherit; font-size: 12px; cursor: pointer;
}
#gg-gitconfig button:hover:not(:disabled) { border-color: var(--accent); }
#gg-gitconfig button:disabled { opacity: .45; cursor: default; }
#gg-gitconfig .crow {
  display: flex; gap: 10px; align-items: baseline; padding: 3px 4px;
  border-radius: 3px; cursor: pointer;
}
#gg-gitconfig .crow:hover { background: color-mix(in srgb, var(--accent) 10%, transparent); }
#gg-gitconfig .crow.sel { background: color-mix(in srgb, var(--accent) 16%, transparent); }
#gg-gitconfig .ckey { flex: none; width: 250px; color: var(--accent); }
#gg-gitconfig .cval { flex: 1 1 auto; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
#gg-gitconfig .cscope { flex: none; color: var(--dim); font-size: 11px; min-width: 58px; text-align: right; }
#gg-gitconfig .cdesc { color: var(--dim); font-size: 11px; padding: 0 4px 6px 260px; }
#gg-gitconfig .ced {
  margin: 4px 0 10px; padding: 10px 12px; border: 1px solid var(--border);
  border-radius: 4px; background: var(--bg);
}
#gg-gitconfig .ced .line { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; margin: 4px 0; }
#gg-gitconfig .ced .lbl { color: var(--dim); font-size: 11px; min-width: 92px; }
#gg-gitconfig .ced input[type=text] { flex: 1 1 260px; }
#gg-gitconfig .opt.on { border-color: var(--accent); color: var(--accent); background: color-mix(in srgb, var(--accent) 12%, var(--bg)); }
#gg-gitconfig .foot { margin-top: 12px; color: var(--dim); font-size: 11px; }
#gg-gitconfig .none { color: var(--dim); }
`;
  document.head.append(st);
}


function scopeTag(row) {
  if (row.scope === "repo") return "repo";
  if (row.scope === "global") return "global";
  return "default";
}


// valueText renders a value for display. An empty string IS a value git can
// hold, so it is shown as such rather than as nothing at all.
function valueText(row) {
  if (row.scope === "default") return row.default || "(none)";
  return row.effective === "" ? '""' : row.effective;
}


function matches(row, q) {
  if (!q) return true;
  return (row.key + " " + (row.desc || "") + " " + row.effective).toLowerCase().includes(q);
}


function renderConfig() {
  const el = mountOverlay("gg-gitconfig");
  if (!cfg) return;
  const q = (cfg.filter || "").trim().toLowerCase();
  const shown = cfg.catalog.filter((r) => matches(r, q));
  const extra = cfg.extra.filter((r) => matches(r, q));
  const rows = shown
    .map((r) => {
      const open = cfg.open === r.key;
      return (
        `<div class="crow${open ? " sel" : ""}" data-key="${esc(r.key)}">` +
        `<span class="ckey">${esc(r.key)}</span>` +
        `<span class="cval">${esc(valueText(r))}</span>` +
        `<span class="cscope">${esc(scopeTag(r))}</span></div>` +
        (open ? `<div class="cdesc">${esc(r.desc || "")}</div>` + editorHTML(r) : "")
      );
    })
    .join("");
  const extraHTML = extra
    .map(
      (r) =>
        `<div class="crow" style="cursor:default">` +
        `<span class="ckey">${esc(r.key)}</span>` +
        `<span class="cval">${esc(valueText(r))}</span>` +
        `<span class="cscope">${esc(scopeTag(r))}</span></div>`
    )
    .join("");
  el.innerHTML =
    `<div class="box">` +
    `<h2>git config</h2>` +
    `<input id="gc-filter" placeholder="filter keys, values and descriptions" value="${esc(cfg.filter || "")}">` +
    `<div id="gc-rows">${rows || '<div class="none">no key matches</div>'}</div>` +
    (extra.length
      ? `<h3>set here, not in gg's catalog</h3>` +
        `<div class="foot">gg edits the keys it documents; these are shown so the picture is complete — ` +
        `change them with <code>git config</code> or in the config file.</div>` +
        `<div>${extraHTML}</div>`
      : "") +
    `<div class="foot">click a key to edit it · a value can live in this repo or globally — ` +
    `the repo's wins · esc closes</div>` +
    `</div>`;
  const f = $("gc-filter");
  f.oninput = () => {
    cfg.filter = f.value;
    const at = f.selectionStart;
    renderConfig();
    const nf = $("gc-filter");
    nf.focus();
    nf.setSelectionRange(at, at);
  };
  el.querySelectorAll(".crow[data-key]").forEach((row) => {
    row.onclick = (e) => {
      if (e.target.closest(".ced")) return; // clicks inside the editor are its own
      const key = row.dataset.key;
      cfg.open = cfg.open === key ? "" : key;
      renderConfig();
    };
  });
  if (cfg.open) wireEditor();
}


// editorHTML builds the per-key editor. A closed value set (bool, enum) gets
// buttons — there is no reason to let someone type `yes` into a boolean — and
// everything else gets a field.
function editorHTML(r) {
  const cur = r.scope === "default" ? "" : r.effective;
  let control;
  if (r.kind === "bool" || r.kind === "enum") {
    const opts = r.kind === "bool" ? ["true", "false"] : r.options || [];
    control = opts
      .map((o) => `<button class="opt${o === cur ? " on" : ""}" data-opt="${esc(o)}">${esc(o)}</button>`)
      .join(" ");
  } else {
    control = `<input type="text" id="gc-val" value="${esc(cur)}" placeholder="${esc(r.default || "")}">`;
  }
  return (
    `<div class="ced" data-key="${esc(r.key)}">` +
    `<div class="line"><span class="lbl">git's default</span><span>${esc(r.default || "(none)")}</span></div>` +
    `<div class="line"><span class="lbl">this repo</span><span>${r.local_set ? esc(r.local || '""') : '<span class="none">unset</span>'}</span>` +
    (r.local_set ? ` <button data-unset="repo">unset</button>` : "") +
    `</div>` +
    `<div class="line"><span class="lbl">global</span><span>${r.global_set ? esc(r.global || '""') : '<span class="none">unset</span>'}</span>` +
    (r.global_set ? ` <button data-unset="global">unset</button>` : "") +
    `</div>` +
    `<div class="line"><span class="lbl">value</span>${control}</div>` +
    `<div class="line"><span class="lbl"></span>` +
    `<button data-save="repo">save to this repo</button>` +
    `<button data-save="global">save globally</button></div>` +
    `</div>`
  );
}


function wireEditor() {
  const ed = document.querySelector("#gg-gitconfig .ced");
  if (!ed) return;
  const key = ed.dataset.key;
  const row = cfg.catalog.find((r) => r.key === key);
  // A button-driven key has no field: the click IS the value, and picking one
  // writes it immediately to the scope the user then names. Rather than a
  // two-step dance, the buttons set a pending value the save rows use.
  let pending = row.scope === "default" ? row.default : row.effective;
  ed.querySelectorAll("[data-opt]").forEach((b) => {
    b.onclick = () => {
      pending = b.dataset.opt;
      ed.querySelectorAll("[data-opt]").forEach((o) => o.classList.toggle("on", o === b));
    };
  });
  ed.querySelectorAll("[data-save]").forEach((b) => {
    b.onclick = () => {
      const field = $("gc-val");
      writeConfig(key, field ? field.value : pending, b.dataset.save === "global", false);
    };
  });
  ed.querySelectorAll("[data-unset]").forEach((b) => {
    b.onclick = () => writeConfig(key, "", b.dataset.unset === "global", true);
  });
}


// writeConfig posts the change and follows its op. The KEY is checked again
// server-side against the same catalog this list came from — the browser
// deciding what may be written would be no boundary at all.
async function writeConfig(key, value, global, unset) {
  if (opBusy()) {
    opLine("git config: an operation is already running", true);
    return;
  }
  let resp;
  try {
    resp = await postJSON("/api/gitconfig", { key, value, global, unset });
  } catch (e) {
    opLine("git config: " + (e.message || e), true);
    return;
  }
  followOp(resp.op_id, (unset ? "unsetting " : "setting ") + key, "set-git-config", (ev) => {
    if (!ev.ok) {
      opLine("error: " + (ev.error || "operation failed"), true);
      return;
    }
    opLine(ev.summary || "done");
    // Re-read: the row's scope, effective value and unset buttons all change,
    // and the truth is what git now reports, not what was posted.
    if (cfg) loadConfig();
  });
}


async function loadConfig() {
  let out;
  try {
    out = await getJSON("/api/gitconfig");
  } catch (e) {
    opLine("git config: " + (e.message || e), true);
    return;
  }
  if (!cfg) return; // closed while the read was in flight
  cfg.catalog = out.catalog || [];
  cfg.extra = out.extra || [];
  renderConfig();
}


function openGitConfig() {
  injectStyle();
  cfg = { catalog: [], extra: [], filter: "", open: "" };
  const el = mountOverlay("gg-gitconfig");
  el.onclick = (e) => {
    if (e.target === el) closeGitConfig(); // backdrop
  };
  renderConfig();
  pushLayer("gg-gitconfig", el, {
    onKey: (e) => {
      if (e.key === "Escape") {
        closeGitConfig();
        return true;
      }
      return false; // typing belongs to the filter field
    },
  });
  loadConfig();
}


function closeGitConfig() {
  cfg = null;
  closeLayer("gg-gitconfig");
}


registerRows("menu", () => [{ label: "git config…", act: () => openGitConfig() }]);

registerHelp({
  key: "git config",
  html:
    "☰ → <b>git config…</b>: gg's documented git settings with each key's effective value and " +
    "where it comes from (repo · global · git's default). Editing writes with <code>git config</code> " +
    "at the scope you pick; keys gg does not document are listed read-only",
});

export { openGitConfig };
