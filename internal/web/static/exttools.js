// exttools.js — the External tools window. On top, the template updates the
// bar's notice announces (take the new template / keep mine); below, one tool
// at a time with its commands grouped by what they are for: added ones with
// their standing, ones not yet added with a tick box (the add appends them to
// the global config). Commands no detected tool offers sit under "Your own
// commands".
import { $, esc, getJSON, postJSON } from "./core.js";
import { POPUP_PAGE, closeLayer, pushLayer } from "./layers.js";

let data = null; // last GET /api/exttools payload
let tools = []; // buildTools(data, ticks)
let ticks = {}; // a not-added row's box, by rowKey: survives a re-read
let area = "tools"; // the keys' area: "updates" | "tools" | "cmds"
let toolIx = 0; // the tool on the right
let rowIx = 0; // the command under the cursor, in that tool's rows
let updIx = 0; // the update under the cursor, in the band's rows
let openOffer = ""; // offer_id whose new template text is shown
let msg = null; // the footer's last result: { text, err }
let showKept = false; // the quiet band lists the updates answered "keep mine"
let busy = false; // a write in flight: its keys do nothing more

// purposeOf is what a command category is for, in the words a reader knows;
// order is the list's. A category gg does not know keeps its own name, last.
function purposeOf(category) {
  const known = {
    conflict: [0, "Resolve conflicts", "opens when a merge or rebase stops on conflicts"],
    conflict_complete: [1, "Resolve and finish", "resolves, then continues the paused merge or rebase"],
    commit_message: [2, "Commit message", "writes the message for your staged changes"],
    review: [3, "Review changes", "reviews the current changes"],
    session: [4, "Agent session", "an interactive agent in a gg console"],
  }[category];
  return known ? { order: known[0], label: known[1], note: known[2] } : { order: 5, label: category, note: "" };
}

// variantName is a catalog name without the tool it repeats: the words in its
// closing brackets ("Claude (yolo, headless)" → "Yolo, headless"), else Default.
function variantName(name) {
  const m = /\(([^()]+)\)\s*$/.exec(name);
  return m ? m[1].charAt(0).toUpperCase() + m[1].slice(1) : "Default";
}

function rowKey(category, name) {
  return category + "\0" + name;
}

// buildTools folds the three lists the server sends into what the window
// shows: a row per command, under its tool. A configured row reads the block
// in the config, not the catalog's text, and carries that block's standing.
function buildTools(d, tickState) {
  const cmds = new Map((d.commands || []).map((c) => [rowKey(c.category, c.name), c]));
  const offers = new Map((d.template_offers || []).map((o) => [rowKey(o.category, o.name), o]));
  const row = (category, name, variant, cmd, fallback) => {
    const key = rowKey(category, name);
    const o = offers.get(key);
    const problem = cmd && !cmd.valid ? cmd.problem : "";
    let state = cmd ? "added" : "new";
    if (o && o.status === "update") state = o.declined ? "kept" : "update";
    else if (o && o.status === "unsupported") state = "unsupported";
    else if (problem) state = "broken";
    const p = purposeOf(category);
    return {
      key, category, name, variant, state, problem,
      purpose: p.label, note: p.note, order: p.order,
      command: cmd ? cmd.command : fallback.command,
      approved: cmd ? cmd.approved : true,
      why: o && o.status === "unsupported" ? o.reason : "",
      offer_id: o && o.offer_id ? o.offer_id : "",
      details: cmd ? [cmd.mode, cmd.per_file ? "per-file" : "", cmd.when_op, (cmd.frontends || []).join(", ")].filter(Boolean).join(" · ") : "",
      tool: fallback ? fallback.tool : "",
      ticked: cmd ? false : key in tickState ? tickState[key] : !!fallback.checked,
    };
  };
  const out = [];
  const seen = new Set();
  for (const w of d.wizard || []) {
    let t = out.find((x) => x.id === w.tool);
    if (!t) out.push((t = { id: w.tool, label: w.label, own: false, rows: [] }));
    seen.add(rowKey(w.category, w.name));
    t.rows.push(row(w.category, w.name, variantName(w.name), cmds.get(rowKey(w.category, w.name)), w));
  }
  const own = (d.commands || []).filter((c) => !seen.has(rowKey(c.category, c.name)));
  if (own.length) {
    out.push({ id: "", label: "Your own commands", own: true, rows: own.map((c) => row(c.category, c.name, c.name, c, null)) });
  }
  for (const t of out) t.rows = t.rows.map((r, i) => [r, i]).sort((a, b) => a[0].order - b[0].order || a[1] - b[1]).map((x) => x[0]);
  return out;
}

// toolCount is the left list's count: how many of the tool's commands are set up.
function toolCount(t) {
  if (t.own) return String(t.rows.length);
  return t.rows.filter((r) => r.state !== "new").length + " of " + t.rows.length;
}

// pendingOffers are the updates still to answer — the notice's count.
function pendingOffers(d) {
  return (d.template_offers || []).filter((o) => o.status === "update" && !o.declined);
}

function keptOffers(d) {
  return (d.template_offers || []).filter((o) => o.status === "update" && o.declined);
}

// bandOffers are the band's rows: the pending ones, and a kept one while it is
// open for another look.
function bandOffers(d, open) {
  return [...pendingOffers(d), ...keptOffers(d).filter((o) => o.offer_id === open)];
}

// wizardAddRows names what an add writes: the ticked rows, by key (the server
// resolves each against a fresh detection).
function wizardAddRows(ts) {
  return ts.flatMap((t) => t.rows.filter((r) => r.state === "new" && r.ticked).map((r) => ({ tool: r.tool, category: r.category, name: r.name })));
}

function addStatus(res) {
  return res.written + (res.written === 1 ? " command" : " commands") + " added to " + res.path;
}

async function loadExtTools() {
  data = await getJSON("/api/exttools");
  tools = buildTools(data, ticks);
  if (toolIx >= tools.length) toolIx = Math.max(0, tools.length - 1);
}

async function openExtToolsView() {
  openOffer = "";
  msg = null;
  data = null;
  ticks = {};
  toolIx = 0;
  rowIx = 0;
  updIx = 0;
  showKept = false;
  try {
    await loadExtTools();
  } catch (e) {
    return;
  }
  area = pendingOffers(data).length ? "updates" : "tools";
  renderExtTools();
  // The button that opened this window would take space and enter as clicks.
  if (document.activeElement && document.activeElement.blur) document.activeElement.blur();
  pushLayer("exttools", $("exttools"), { onKey: extToolsKey });
}

// The areas Tab walks: the band only while it has rows to act on.
function keyAreas() {
  return bandOffers(data, openOffer).length ? ["updates", "tools", "cmds"] : ["tools", "cmds"];
}

function extToolsKey(e) {
  if (e.key === "Escape") {
    if (openOffer) {
      setOpen("");
    } else {
      closeLayer("exttools");
    }
    e.preventDefault();
    return true;
  }
  if (!tools.length) return true;
  const plain = !e.ctrlKey && !e.metaKey && !e.altKey;
  const steps = { ArrowDown: 1, ArrowUp: -1, PageDown: POPUP_PAGE, PageUp: -POPUP_PAGE, Home: -Infinity, End: Infinity };
  let used = true;
  if (e.key === "Tab") {
    const as = keyAreas();
    area = as[(as.indexOf(area) + (e.shiftKey ? as.length - 1 : 1)) % as.length];
    renderExtTools();
  } else if (e.key in steps) {
    moveCursor(steps[e.key]);
  } else if (area === "updates" && (e.key === "Enter" || e.key === " " || (e.key === "u" && plain))) {
    const o = bandOffers(data, openOffer)[updIx];
    if (o) setOpen(openOffer === o.offer_id ? "" : o.offer_id);
  } else if (area === "updates" && openOffer && (e.key === "t" || e.key === "k") && plain) {
    answerOffer(e.key === "t" ? "take" : "keep", openOffer);
  } else if (e.key === " ") {
    if (area === "cmds") toggleTick(rowIx);
  } else if (e.key === "u" && plain && area !== "updates") {
    const r = tools[toolIx].rows[rowIx];
    if (r && r.offer_id) setOpen(r.offer_id);
  } else if (e.key === "Enter") {
    addTicked();
  } else {
    used = false;
  }
  if (used) e.preventDefault();
  return true; // swallow app hotkeys under the overlay
}

function clamp(v, max) {
  return Math.max(0, Math.min(max, v));
}

function moveCursor(delta) {
  if (area === "updates") {
    const os = bandOffers(data, openOffer);
    updIx = clamp(updIx + delta, os.length - 1);
    if (openOffer && os[updIx]) openOffer = os[updIx].offer_id; // the open review follows the cursor
  } else if (area === "tools") {
    const next = clamp(toolIx + delta, tools.length - 1);
    if (next !== toolIx) rowIx = 0;
    toolIx = next;
  } else {
    rowIx = clamp(rowIx + delta, tools[toolIx].rows.length - 1);
  }
  renderExtTools();
}

// setOpen shows an update's new template, or none. Opening moves the keys to
// the band, on that update.
function setOpen(id) {
  openOffer = id;
  if (id) {
    area = "updates";
    updIx = Math.max(0, bandOffers(data, id).findIndex((o) => o.offer_id === id));
  } else {
    const n = bandOffers(data, "").length;
    if (!n && area === "updates") area = "tools";
    updIx = clamp(updIx, n - 1);
  }
  renderExtTools();
}

function toggleTick(i) {
  const r = tools[toolIx] && tools[toolIx].rows[i];
  if (!r || r.state !== "new") return;
  ticks[r.key] = !r.ticked;
  r.ticked = !r.ticked;
  renderExtTools();
}

// reread takes the list again after a write, refused or not: what the server
// now holds decides what the window shows. A failed read says so beside the
// write's own result.
async function reread() {
  try {
    await loadExtTools();
  } catch (e) {
    msg = { text: (msg ? msg.text + "; " : "") + "could not read the list again: " + String(e.message || e), err: true };
  }
}

// addTicked appends the ticked rows and stays open on the result: they now
// read "added" in the list. The boxes are kept: a row the user unticked must
// not come back ticked by its default.
async function addTicked() {
  if (busy || !data) return;
  const rows = wizardAddRows(tools);
  if (!rows.length) {
    msg = { text: "nothing ticked: tick a command that reads \"not added\"", err: false };
    renderExtTools();
    return;
  }
  busy = true;
  try {
    const res = await postJSON("/api/exttools/add", { rows });
    msg = { text: addStatus(res), err: false };
  } catch (e) {
    msg = { text: String(e.message || e), err: true };
  }
  await reread();
  busy = false;
  renderExtTools();
}

// answerOffer writes an update's answer; the next one to answer opens, so a
// run of updates is one key each. A refused answer opens nothing: the list is
// read again, so an offer that has gone is gone from the window too.
async function answerOffer(act, id) {
  if (busy) return;
  const o = (data.template_offers || []).find((x) => x.offer_id === id);
  busy = true;
  let answered = false;
  try {
    await postJSON(act === "take" ? "/api/exttools/update" : "/api/exttools/keep", { offer_id: id });
    msg = { text: (act === "take" ? "updated " : "kept yours for ") + (o ? o.name : "the command"), err: false };
    answered = true;
  } catch (e) {
    msg = { text: String(e.message || e), err: true };
  }
  await reread();
  document.dispatchEvent(new CustomEvent("gg:notices")); // the bar's count follows
  const next = pendingOffers(data);
  if (answered && next.length) {
    updIx = clamp(updIx, next.length - 1);
    openOffer = next[updIx].offer_id;
  } else {
    if (answered || !bandOffers(data, openOffer).some((x) => x.offer_id === openOffer)) openOffer = "";
    const n = bandOffers(data, openOffer).length;
    updIx = clamp(updIx, n - 1);
    if (!n && area === "updates") area = "tools";
  }
  busy = false;
  renderExtTools();
}

function offerLineHTML(o, i, band) {
  const r = rowOfOffer(o);
  const what = r ? `<small>${esc(r.purpose)} · ${esc(r.variant)}</small>` : `<small>${esc(o.name)}</small>`;
  const open = o.offer_id === openOffer;
  const cls = band && area === "updates" && i === updIx ? " sel" : "";
  return `<div class="xt-urow${cls}"${band ? ` data-u="${i}"` : ""}>
      <span class="xt-what">${esc(o.tool_label || "")} ${what}</span>
      <span class="xt-why">${esc(o.reason)}</span>
      <button data-review="${esc(o.offer_id)}">${open ? "hide" : "review"}</button>
    </div>`;
}

function reviewHTML(o) {
  return `<div class="xt-review">
      <div class="xt-lbl">new template, written to ${esc(o.path)}:</div>
      <pre>${esc(o.new_text)}</pre>
      <div class="xt-btns">
        <button class="xt-primary" data-answer="take" data-offer="${esc(o.offer_id)}">take new</button>
        <button data-answer="keep" data-offer="${esc(o.offer_id)}">keep mine</button>
        <button data-review="${esc(o.offer_id)}">later</button>
        <span class="xt-hint"><kbd>t</kbd> take <kbd>k</kbd> keep <kbd>u</kbd> later</span>
      </div>
    </div>`;
}

function rowOfOffer(o) {
  const key = rowKey(o.category, o.name);
  for (const t of tools) {
    const r = t.rows.find((x) => x.key === key);
    if (r) return r;
  }
  return null;
}

function bandHTML() {
  const os = bandOffers(data, openOffer);
  const kept = keptOffers(data);
  if (!os.length) {
    const toggle = kept.length ? " data-showkept" : "";
    const rows = showKept ? kept.map((o, i) => offerLineHTML(o, i, false)).join("") : "";
    return `<section class="xt-updates xt-quiet"><div class="xt-uhead"${toggle}>No updates to review${kept.length ? ` · ${kept.length} kept as yours` : ""}</div>${rows}</section>`;
  }
  const n = pendingOffers(data).length;
  const head = n
    ? `${n} template update${n === 1 ? "" : "s"} to review <span class="xt-note">Newer templates exist for commands gg wrote into your config. Nothing changes until you answer each one.</span>`
    : `A kept update <span class="xt-note">You answered "keep mine"; nothing changes unless you take it.</span>`;
  const rows = os.map((o, i) => offerLineHTML(o, i, true) + (o.offer_id === openOffer ? reviewHTML(o) : "")).join("");
  return `<section class="xt-updates${area === "updates" ? " xt-focus" : ""}"><div class="xt-uhead">${head}</div>${rows}</section>`;
}

function toolsHTML() {
  return tools
    .map((t, i) => {
      const dot = t.rows.some((r) => r.state === "update") ? `<span class="xt-dot" title="has an update"></span>` : "";
      const sep = t.own && tools.length > 1 ? `<div class="xt-sep"></div>` : "";
      // Add writes every tool's ticks, so a tool not on the right still shows its own.
      const n = t.rows.filter((r) => r.state === "new" && r.ticked).length;
      const ticked = n ? `<span class="xt-ticked" title="${n} ticked to add">+${n}</span>` : "";
      return `${sep}<div class="xt-trow${i === toolIx ? " sel" : ""}" data-t="${i}">${esc(t.label)}${dot}<span class="xt-n">${ticked}${esc(toolCount(t))}</span></div>`;
    })
    .join("");
}

function stateHTML(r) {
  switch (r.state) {
    case "added":
      return `<span class="xt-st xt-added">added</span>`;
    case "kept":
      return `<span class="xt-st xt-added">added, kept yours</span>`;
    case "update":
      return `<span class="xt-st xt-update">update available</span>`;
    case "new":
      return `<span class="xt-st">not added</span>`;
    case "unsupported":
      return `<span class="xt-st xt-bad">agent version unsupported</span>`;
    default:
      return `<span class="xt-st xt-bad">broken</span>`;
  }
}

function listHTML(t) {
  let last = "";
  return t.rows
    .map((r, i) => {
      const head = r.purpose !== last ? `<div class="xt-ghead">${esc(r.purpose)}${r.note ? `<small>${esc(r.note)}</small>` : ""}</div>` : "";
      last = r.purpose;
      const box = r.state === "new" ? `<input type="checkbox" tabindex="-1" data-tick="${i}"${r.ticked ? " checked" : ""}>` : "<span></span>";
      const flag = r.approved ? "" : `<span class="xt-flag" title="asks for your OK before its first run in this repository">not yet approved</span>`;
      return `${head}<div class="xt-crow${i === rowIx ? " sel" : ""}" data-r="${i}">${box}<span>${esc(r.variant)}${flag}</span>${stateHTML(r)}</div>`;
    })
    .join("");
}

function detailHTML(r) {
  if (!r) return "";
  const where = r.state === "new" ? "would be added to " + (data.global_config_path || "the global config") : r.details;
  const problem = r.problem || r.why;
  const review = r.offer_id ? `<button data-review="${esc(r.offer_id)}">review update</button>` : "";
  return `${problem ? `<div class="xt-st xt-bad">${esc(problem)}</div>` : ""}
    <div class="xt-meta"><span>${esc(r.name)}</span>${review}<span class="xt-where">${esc(where)}</span></div>
    <pre>${esc(r.command)}</pre>`;
}

function footerHTML() {
  const n = wizardAddRows(tools).length;
  const enter = area === "updates" ? "review" : "add ticked";
  return `<footer class="xt-foot">
      <span class="xt-keys"><span><kbd>tab</kbd> next area</span><span><kbd>↑</kbd><kbd>↓</kbd> move</span><span><kbd>space</kbd> tick</span><span><kbd>u</kbd> review update</span><span><kbd>enter</kbd> ${enter}</span><span><kbd>esc</kbd> close</span></span>
      <span class="xt-grow"></span>
      ${msg ? `<span class="xt-msg${msg.err ? " xt-bad" : ""}">${esc(msg.text)}</span>` : ""}
      <button class="xt-primary" data-add${n ? "" : " disabled"}>${n ? `Add ${n} ticked` : "Nothing ticked"}</button>
    </footer>`;
}

function renderExtTools() {
  if (!data) return;
  const box = $("exttools-box");
  if (!tools.length) {
    box.innerHTML = `<header class="xt-head"><h2>External tools</h2><button class="xt-close" data-close>close</button></header>
      <div class="xt-empty">No known tools found on this machine.</div>`;
    return;
  }
  // The cursor on an update shows that command in the list below.
  if (area === "updates") {
    const o = bandOffers(data, openOffer)[updIx];
    const key = o && rowKey(o.category, o.name);
    const ti = o ? tools.findIndex((t) => t.rows.some((r) => r.key === key)) : -1;
    if (ti >= 0) {
      toolIx = ti;
      rowIx = tools[ti].rows.findIndex((r) => r.key === key);
    }
  }
  const t = tools[toolIx];
  rowIx = clamp(rowIx, t.rows.length - 1);
  const set = tools.flatMap((x) => x.rows).filter((r) => r.state !== "new").length;
  const found = tools.filter((x) => !x.own).length;
  // A re-render replaces the panes: each keeps its scroll, the cursor stays in view.
  const scrolls = [".xt-updates", ".xt-tools", ".xt-list"].map((s) => {
    const el = box.querySelector(s);
    return [s, el ? el.scrollTop : 0];
  });
  box.innerHTML = `
    <header class="xt-head"><h2>External tools</h2><span class="xt-sum">${set} command${set === 1 ? "" : "s"} set up across ${found} tool${found === 1 ? "" : "s"} found here</span><button class="xt-close" data-close>close</button></header>
    ${bandHTML()}
    <div class="xt-main">
      <nav class="xt-tools${area === "tools" ? " xt-focus" : ""}">${toolsHTML()}</nav>
      <div class="xt-cmds">
        <div class="xt-list${area === "cmds" ? " xt-focus" : ""}">${listHTML(t)}</div>
        <div class="xt-detail">${detailHTML(t.rows[rowIx])}</div>
      </div>
    </div>
    ${footerHTML()}`;
  for (const [s, top] of scrolls) {
    const el = box.querySelector(s);
    if (el) el.scrollTop = top;
  }
  for (const s of [".xt-urow.sel", ".xt-trow.sel", ".xt-crow.sel"]) {
    const el = box.querySelector(s);
    if (el) el.scrollIntoView({ block: "nearest" });
  }
}

$("exttools").addEventListener("click", (e) => {
  if (e.target === $("exttools")) {
    closeLayer("exttools");
    return;
  }
  const hit = (attr) => e.target.closest(`[${attr}]`);
  let el;
  if (hit("data-close")) {
    closeLayer("exttools");
  } else if ((el = hit("data-answer"))) {
    answerOffer(el.dataset.answer, el.dataset.offer);
  } else if ((el = hit("data-review"))) {
    const id = el.dataset.review;
    setOpen(openOffer === id ? "" : id);
  } else if (hit("data-showkept")) {
    showKept = !showKept;
    renderExtTools();
  } else if (hit("data-add")) {
    addTicked();
  } else if ((el = hit("data-u"))) {
    updIx = Number(el.dataset.u);
    area = "updates";
    // An open review follows the cursor, as with the keys: t and k answer the row shown.
    const o = bandOffers(data, openOffer)[updIx];
    if (openOffer && o) openOffer = o.offer_id;
    renderExtTools();
  } else if ((el = hit("data-t"))) {
    const i = Number(el.dataset.t);
    if (i !== toolIx) rowIx = 0;
    toolIx = i;
    area = "tools";
    renderExtTools();
  } else if ((el = hit("data-r"))) {
    rowIx = Number(el.dataset.r);
    area = "cmds";
    if (e.target.matches("[data-tick]")) toggleTick(rowIx);
    else renderExtTools();
  }
});

export { openExtToolsView };
