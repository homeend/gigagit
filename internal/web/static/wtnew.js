// wtnew.js — the TUI's create-worktree popup (worktree_popup.go) behind every
// web "new worktree" entrance: a branch (checked out as is; e makes a NEW
// branch from it), a commit (with the keep-mode line), a tag or a remote
// branch (a new branch, named first). The server previews the names from the
// configured templates (/api/worktree/preview, <random>/<date> fixed for the
// dialog's life, <seq> counters peeked) and create-worktree bumps the counters
// only after a successful create.
import { $, esc, postJSON } from "./core.js";
import { closeLayer, mountOverlay, pushLayer } from "./layers.js";
import { doReroot, followOp, opBusy, opLine, refreshAfterOp, startOp } from "./ops.js";
import { openPrefixPicker } from "./prefixes.js";
import { registerHelp } from "./menus.js";

// --- dialog model (pure; guarded against Go) ---
// d: { from: branch|commit|tag|remote, start, existing, state:
// input|action|edit|editpath, edit, branchOverride, pathOverride, inputs,
// seed, now, preview {branch, path, error}, hook, runHook, switchOnCreate,
// keepOffered, keepLocked, keep 0|1|2, prefix {id, scope}|null }

// displayStart is the TUI's: a full sha shows as its first seven.
function displayStart(s) {
  return /^[0-9a-fA-F]{40}$/.test(s) ? s.slice(0, 7) : s;
}

// fixedBranch is the name used verbatim: the live editor (an emptied one
// falls back to the selection on a branch), a confirmed edit, the selection.
function fixedBranch(d) {
  if (d.state === "edit") return d.edit !== "" || !d.existing ? d.edit : d.start;
  if (d.branchOverride !== "") return d.branchOverride;
  return d.existing ? d.start : "";
}

// fixedPath is the path used verbatim; "" = the path template.
function fixedPath(d) {
  return d.state === "editpath" ? d.edit : d.pathOverride;
}

function previewBody(d) {
  return { from: d.from, start: d.start, branch: fixedBranch(d), path: fixedPath(d), inputs: d.inputs, seed: d.seed, now: d.now };
}

function wtTitle(d) {
  if (d.existing && d.preview.branch === d.start) return "Create worktree for " + d.start;
  return "Create worktree from " + displayStart(d.start);
}

function wtHint(d) {
  if (d.state === "input") return "[type] value  [tab/enter] next field  [esc] cancel";
  if (d.state === "edit") return "[type] edit name  [enter] done  [esc] discard";
  if (d.state === "editpath") return "[type] edit path  [enter] done  [esc] discard";
  const head = d.switchOnCreate ? "[enter/W] create & switch  [w] create only" : "[w] create  [W] create & switch";
  return head + "  [e] edit name  [E] edit path  [p] use a prefix" + (d.hook ? "  [h] hook" : "") + "  [esc] cancel";
}

function keepLabel(k) {
  return ["at this commit", "at parent, changes staged", "at parent, changes unstaged"][k] || "at this commit";
}

// modeLine is the commit entrance's keep mode; [m] is offered only where m
// does not type into a field.
function modeLine(d) {
  if (!d.keepOffered || d.state === "input") return "";
  let line = "start:  " + keepLabel(d.keep);
  if (d.keepLocked) line += "  (root/merge commit — at this commit only)";
  else if (d.state === "action") line += "  ([m] change)";
  return line;
}

function hookLine(d) {
  if (!d.hook || d.state !== "action") return "";
  return (d.runHook ? "[x]" : "[ ]") + " run post-create hook  ([h] toggle)";
}

// createCheck is startCreateFromPopup's refusal ("" = go).
function createCheck(d) {
  if (!d.existing && d.branchOverride.trim() === "") return "branch name required";
  if (d.preview.error) return "cannot create: " + d.preview.error;
  return "";
}

// createBody is the op the preview describes: the counters bump server-side
// (path_seq: the path template was used; a picked prefix's own).
function createBody(d) {
  const key = { branch: "branch", commit: "sha", tag: "tag", remote: "ref" }[d.from];
  const body = { op: "create-worktree", [key]: d.start, name: d.preview.branch, path: d.preview.path };
  if (d.pathOverride === "") body.path_seq = true;
  if (d.keepOffered && d.keep) body.mode = d.keep === 1 ? "staged" : "unstaged";
  if (d.hook && !d.runHook) body.no_hook = true;
  if (d.prefix) {
    body.prefix_id = d.prefix.id;
    body.prefix_scope = d.prefix.scope;
  }
  return body;
}
// --- end dialog model ---

const box = mountOverlay("wtnew");
box.innerHTML = `<div id="wtnew-box" tabindex="-1"></div>`;
let d = null;
let opening = false;

// openWorktreeDialog: {from, start, prefill, switchOnCreate, keepOffered,
// keepLocked}. A branch opens on its fields (or its actions); every other
// entrance opens editing the new branch's name, seeded with prefill.
async function openWorktreeDialog(o) {
  if (d || opening || opBusy()) return;
  const nd = {
    from: o.from, start: o.start, existing: o.from === "branch",
    state: o.from === "branch" ? "action" : "edit", edit: o.from === "branch" ? "" : o.prefill || "",
    branchOverride: "", pathOverride: "", inputs: {}, labels: [], field: 0, seed: "", now: "",
    preview: { branch: "", path: "", error: "" }, hook: false, runHook: true,
    switchOnCreate: !!o.switchOnCreate, keepOffered: !!o.keepOffered, keepLocked: !!o.keepLocked, keep: 0,
    prefix: null, gen: 0, applied: 0, inflight: null, note: "",
  };
  opening = true;
  let out;
  try {
    out = await postJSON("/api/worktree/preview", previewBody(nd));
  } catch (e) {
    opLine("new worktree: " + (e.message || e), true);
    return;
  } finally {
    opening = false;
  }
  nd.labels = out.labels || [];
  for (const l of nd.labels) nd.inputs[l] = "";
  nd.seed = out.seed;
  nd.now = out.now;
  nd.hook = !!out.hook;
  nd.preview = { branch: out.branch || "", path: out.path || "", error: out.error || "" };
  if (nd.existing && nd.labels.length) nd.state = "input";
  d = nd;
  pushLayer("wtnew", box, { onKey: wtKey });
  render();
}

function closeDialog() {
  d = null;
  closeLayer("wtnew");
}

// refresh re-previews after every edit; a reply that a newer edit overtook
// is dropped.
// my.applied is the generation the shown preview answers; create waits
// until it is the latest (the TUI recomputes synchronously, the page cannot).
function refresh() {
  if (!d) return;
  const my = d;
  const gen = ++my.gen;
  my.inflight = (async () => {
    let out;
    try {
      out = await postJSON("/api/worktree/preview", previewBody(my));
    } catch (e) {
      out = { branch: "", path: "", error: e.message || String(e) };
    }
    if (d !== my || gen !== my.gen) return;
    my.preview = { branch: out.branch || "", path: out.path || "", error: out.error || "" };
    my.applied = gen;
    paint();
  })();
}

function setState(st) {
  d.state = st;
  d.note = "";
  render();
  refresh();
}

function render() {
  const b = $("wtnew-box");
  const fields = d.labels
    .map((l, i) => {
      const on = d.state === "input" && i === d.field;
      return `<label class="wfield${on ? " on" : ""}"><span>${on ? "&gt; " : "  "}${esc(l)}: </span><input data-label="${esc(l)}" data-i="${i}" value="${esc(d.inputs[l])}" autocomplete="off" spellcheck="false"></label>`;
    })
    .join("");
  const line = (k, label, editing) =>
    `<div class="wline" data-w="${k}"><span class="wlabel">${label}</span>` +
    (editing ? `<input class="wedit" value="${esc(d.edit)}" autocomplete="off" spellcheck="false">` : `<span class="wval"></span>`) +
    `</div>`;
  const btn = (k, t) => `<button data-k="${k}">${t}</button>`;
  let btns;
  if (d.state === "input") btns = btn("Tab", "next field") + btn("Escape", "cancel");
  else if (d.state === "edit" || d.state === "editpath") btns = btn("Enter", "done") + btn("Escape", "discard");
  else
    btns =
      (d.switchOnCreate ? btn("W", "create &amp; switch") + btn("w", "create only") : btn("w", "create") + btn("W", "create &amp; switch")) +
      btn("e", "edit name") + btn("E", "edit path") + btn("p", "use a prefix") + btn("Escape", "cancel");
  b.innerHTML =
    `<div class="wtitle"></div>` +
    (fields ? `<div class="wfields">${fields}</div>` : "") +
    line("branch", "branch: ", d.state === "edit") +
    line("path", "path:   ", d.state === "editpath") +
    `<div class="werr hidden"></div><div class="wnote hidden"></div><div class="whook"></div><div class="wmode"></div>` +
    `<div class="whint">${esc(wtHint(d))}</div><div class="wbtns">${btns}</div>`;
  paint();
  const focus = d.state === "input" ? b.querySelector(`input[data-i="${d.field}"]`) : b.querySelector("input.wedit");
  if (focus) {
    focus.focus();
    // the caret after the prefill: p seeds a name to append to (the TUI's)
    focus.setSelectionRange(focus.value.length, focus.value.length);
  } else b.focus(); // actions: no field holds the keys, so w / e / … never type
}

// paint updates what a preview reply changes, leaving any field being typed
// in alone.
function paint() {
  if (!d) return;
  const b = $("wtnew-box");
  b.querySelector(".wtitle").textContent = wtTitle(d);
  const bv = b.querySelector('[data-w="branch"] .wval');
  if (bv) bv.textContent = d.preview.branch;
  const pv = b.querySelector('[data-w="path"] .wval');
  if (pv) pv.textContent = d.preview.path;
  const err = b.querySelector(".werr");
  err.textContent = d.preview.error ? "⚠ " + d.preview.error : "";
  err.classList.toggle("hidden", !d.preview.error);
  const note = b.querySelector(".wnote");
  note.textContent = d.note;
  note.classList.toggle("hidden", !d.note);
  b.querySelector(".whook").textContent = hookLine(d);
  b.querySelector(".wmode").textContent = modeLine(d);
}

async function create(switchAfter) {
  const my = d;
  while (d === my && my.applied !== my.gen) await my.inflight;
  if (d !== my) return;
  if (opBusy()) return; // keep the filled dialog; opBusy says why
  const why = createCheck(d);
  if (why) {
    d.note = why; // the TUI's status line; the dialog stays open
    paint();
    return;
  }
  const body = createBody(d);
  closeDialog();
  const label = "creating worktree " + body.path;
  if (!switchAfter) {
    startOp(body, label);
    return;
  }
  createAndSwitch(body, label);
}

// createAndSwitch is create & switch: re-root at the new worktree once the
// create succeeded (the TUI's pendingSwitch); a failure or a cancel stays.
async function createAndSwitch(body, label) {
  if (opBusy()) return;
  let resp;
  try {
    resp = await postJSON("/api/op", body);
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    return;
  }
  followOp(resp.op_id, label, "create-worktree", (ev) => {
    if (!ev.ok) {
      opLine("error: " + (ev.error || "operation failed"), true);
      refreshAfterOp();
      return;
    }
    opLine(ev.summary || "done");
    if (ev.changed && ev.path) doReroot(ev.path);
    else refreshAfterOp();
  });
}

// pickPrefix is p: the dialog hides behind the picker and comes back editing
// the name, seeded with the resolved prefix (the TUI's onPrefixPicked).
function pickPrefix() {
  const my = d;
  closeLayer("wtnew");
  openPrefixPicker(
    (resolved, p) => {
      if (d !== my) return;
      pushLayer("wtnew", box, { onKey: wtKey });
      if (resolved != null) {
        my.edit = resolved;
        my.prefix = { id: p.id, scope: p.scope };
        setState("edit");
      } else render();
    },
    { parent: my.start }
  );
}

// act runs one key of the TUI popup (also what every button sends).
function act(key) {
  if (!d) return false;
  if (d.state === "input") {
    if (key === "Escape") closeDialog();
    else if (key === "Enter" || key === "Tab") {
      d.field++;
      if (d.field >= d.labels.length) {
        d.field = d.labels.length - 1;
        setState("action");
      } else render();
    } else return false;
    return true;
  }
  if (d.state === "edit" || d.state === "editpath") {
    if (key === "Enter") {
      if (d.state === "edit") d.branchOverride = d.edit;
      else d.pathOverride = d.edit; // "" reverts to the template path
      setState("action");
    } else if (key === "Escape") setState("action");
    else return false;
    return true;
  }
  switch (key) {
    case "Escape":
      closeDialog();
      return true;
    case "e":
      d.edit = d.preview.branch;
      setState("edit");
      return true;
    case "E":
      d.edit = d.preview.path;
      setState("editpath");
      return true;
    case "p":
      pickPrefix();
      return true;
    case "Enter":
      create(d.switchOnCreate);
      return true;
    case "w":
      create(false);
      return true;
    case "W":
      create(true);
      return true;
    case "h":
      if (d.hook) {
        d.runHook = !d.runHook;
        paint();
      }
      return true;
    case "m":
      if (d.keepOffered && !d.keepLocked) {
        d.keep = (d.keep + 1) % 3;
        paint();
      }
      return true;
  }
  return false;
}

function wtKey(e) {
  if (e.target.closest && e.target.closest("button[data-k]") && (e.key === "Enter" || e.key === " ")) return true; // the focused button's own click
  if (e.ctrlKey || e.metaKey || e.altKey) return true; // copy / paste in a field stay the browser's
  if (act(e.key)) e.preventDefault();
  return true; // the dialog owns the keyboard; typing reaches its fields
}

box.addEventListener("input", (e) => {
  if (!d) return;
  const t = e.target;
  if (t.dataset.label != null) d.inputs[t.dataset.label] = t.value;
  else if (t.classList.contains("wedit")) d.edit = t.value;
  else return;
  d.note = "";
  refresh();
});

// A click on a field is the TUI's walk to it; on the branch / path value, e / E.
box.addEventListener("focusin", (e) => {
  const t = e.target;
  if (!d || t.dataset.i == null) return;
  if (d.state === "input" && Number(t.dataset.i) === d.field) return;
  if (d.state !== "action" && d.state !== "input") return;
  d.field = Number(t.dataset.i);
  d.state = "input";
  render();
});

box.addEventListener("click", (e) => {
  if (!d) return;
  if (e.target === box) {
    closeDialog(); // the backdrop is a cancel
    return;
  }
  const b = e.target.closest("button[data-k]");
  if (b) {
    act(b.dataset.k);
    return;
  }
  if (d.state !== "action") return;
  if (e.target.closest('[data-w="branch"]')) act("e");
  else if (e.target.closest('[data-w="path"]')) act("E");
  else if (e.target.closest(".whook")) act("h");
  else if (e.target.closest(".wmode")) act("m");
});

registerHelp({
  key: "new worktree",
  html:
    "a branch's <b>create worktree for this branch…</b> / <b>create worktree &amp; switch…</b>, a commit's <b>create worktree here…</b>, a tag's and a remote branch's rows open one dialog (the TUI's): " +
    "the path comes from <code>[worktree] path_template</code> (its <code>&lt;user:…&gt;</code> fields first), the name of a new branch from what you type. " +
    "Keys: <b>e</b> edit name (on a branch, another name cuts a NEW branch from it), <b>E</b> edit path, <b>p</b> use a prefix, <b>w</b> create, <b>W</b> create &amp; switch, " +
    "<b>h</b> run the post-create hook or not, <b>m</b> (a commit) at this commit or at its parent with the change staged / unstaged, <b>esc</b> cancel.",
});

export { openWorktreeDialog };
