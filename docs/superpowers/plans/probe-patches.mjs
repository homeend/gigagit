// probe-patches.mjs — the browser check for task 1 (patches).
//
//   node probe-patches.mjs <cdp-port> <app-url>
//
// Run against the UNFIXED build first: it must fail (no export row, no apply
// row, /api/commit-patch 404). Then against the built branch.
//
// What it proves, in the browser and not in Go:
//   1. the commit menu carries "export as patch…" and the endpoint behind it
//      answers with a real format-patch mailbox (`From ` + Content-Disposition)
//   2. the ☰ menu carries "apply a patch…" and "copy a bookmark or shelf
//      entry…", and both rows are on screen, not clipped past the viewport
//   3. applying a real .patch through that prompt reports the commit it made
//   4. the file menu inside a commit carries the per-file export row, and the
//      working-tree file menu does NOT
//   5. no console errors / thrown exceptions — an ES-module export mistake in
//      patch.js shows up here and nowhere else
const PORT = Number(process.argv[2] || 9222);
const APP = process.argv[3];
const PATCH = process.argv[4] || "/tmp/probe-patch/side.patch";

async function connect() {
  for (let i = 0; i < 40; i++) {
    try {
      const tabs = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json();
      const page = tabs.find((t) => t.type === "page");
      if (page) return page.webSocketDebuggerUrl;
    } catch {}
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error("no CDP page — is chrome running with --remote-debugging-port?");
}

const ws = new WebSocket(await connect());
await new Promise((res) => (ws.onopen = res));

let id = 0;
const pending = new Map();
const errors = [];
ws.onmessage = (m) => {
  const msg = JSON.parse(m.data);
  if (msg.id && pending.has(msg.id)) { pending.get(msg.id)(msg); pending.delete(msg.id); }
  if (msg.method === "Runtime.exceptionThrown")
    errors.push("EXCEPTION: " + (msg.params.exceptionDetails.exception?.description || msg.params.exceptionDetails.text));
  if (msg.method === "Runtime.consoleAPICalled" && msg.params.type === "error")
    errors.push("CONSOLE: " + msg.params.args.map((a) => a.value || a.description).join(" "));
};

const send = (method, params = {}) =>
  new Promise((res) => { const n = ++id; pending.set(n, res); ws.send(JSON.stringify({ id: n, method, params })); });

const evaluate = async (expression) => {
  const r = await send("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true });
  if (r.result?.exceptionDetails)
    return "EVAL-ERROR: " + (r.result.exceptionDetails.exception?.description || r.result.exceptionDetails.text);
  return r.result?.result?.value;
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

await send("Runtime.enable");
await send("Page.enable");
await send("Emulation.setDeviceMetricsOverride", { width: 1400, height: 900, deviceScaleFactor: 1, mobile: false });
await send("Page.navigate", { url: APP });
await sleep(5000);

const out = {};

const openMenu = (selector) => `(() => {
  const el = document.querySelector(${JSON.stringify(selector)});
  if (!el) return "NO ROW";
  const r = el.getBoundingClientRect();
  el.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: r.x + 30, clientY: r.y + 5 }));
  return [...document.querySelectorAll("#ctx-menu > *")]
    .map((n) => n.tagName === "BUTTON" ? (n.classList.contains("danger") ? "!" : "") + n.textContent : "--")
    .join(" | ");
})()`;

const clickMenuRow = (re) => `(() => {
  const b = [...document.querySelectorAll("#ctx-menu button")].find((x) => ${re}.test(x.textContent));
  if (!b) return "ROW MISSING";
  b.click();
  return b.textContent;
})()`;

const submitPrompt = (value) => `(() => {
  const i = document.getElementById("prompt-input");
  if (${JSON.stringify(value)} !== null) i.value = ${JSON.stringify(value)};
  document.getElementById("prompt-ok").click();
  return 1;
})()`;

const opText = `(document.getElementById("op-text") || {}).textContent || ""`;

// Geometry, not class names.
const rowVisible = (re) => `(() => {
  const b = [...document.querySelectorAll("#ctx-menu button")].find((x) => ${re}.test(x.textContent));
  if (!b) return "ROW MISSING";
  const r = b.getBoundingClientRect();
  return JSON.stringify({ h: b.offsetHeight, onScreen: r.bottom <= window.innerHeight + 0.5 && r.top >= 0 });
})()`;

// --- steps -----------------------------------------------------------------

out.booted = await evaluate(`JSON.stringify({
  branches: document.querySelectorAll("#branches-list li").length,
  commits: document.querySelectorAll("#commits-window .crow").length,
})`);

// 0. the WORKING-TREE file menu must NOT carry the per-file export row: there
// is no commit there to take a file's change from. Checked before anything
// opens a commit, while the files pane still shows the status sections.
out.fileMenuWorkingTree = await evaluate(`(async () => {
  // The files pane fills only once the Working tree row is opened.
  const wt = document.querySelector('#commits-window .crow[data-i="0"]');
  if (!wt) return "NO WORKING TREE ROW";
  wt.click();
  await new Promise((r) => setTimeout(r, 1500));
  const f = [...document.querySelectorAll("#files-list li")].find((n) => !n.classList.contains("sect"));
  if (!f) return "NO FILE ROW";
  const b = f.getBoundingClientRect();
  f.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: b.x + 20, clientY: b.y + 5 }));
  return [...document.querySelectorAll("#ctx-menu button")].map((n) => n.textContent).join(" | ");
})()`);
out.fileExportRowInWorkingTree = /export this file's diff/.test(String(out.fileMenuWorkingTree));
await evaluate(`document.body.dispatchEvent(new MouseEvent("click", { bubbles: true }))`);
await sleep(300);

// 1. the commit menu row + the bytes behind it
out.commitMenu = await evaluate(openMenu('#commits-window .crow[data-i="1"]'));
out.exportRowPresent = /export as patch/.test(String(out.commitMenu));
out.exportRowVisible = await evaluate(rowVisible("/export as patch/"));

// The download itself is not worth wiring up headless; the bytes are. Read the
// commit's own hash off the row and fetch the endpoint the row navigates to.
out.patchBytes = await evaluate(`(async () => {
  const feed = await (await fetch("/api/commits")).json();
  const row = (feed.rows || []).find((x) => /^[0-9a-f]{40}$/.test(x.hash || ""));
  if (!row) return "NO COMMIT IN THE FEED";
  const sha = row.hash;
  const r = await fetch("/api/commit-patch?sha=" + encodeURIComponent(sha));
  const t = await r.text();
  return JSON.stringify({
    status: r.status,
    disposition: r.headers.get("content-disposition"),
    head: t.slice(0, 5),
    isMailbox: t.startsWith("From "),
  });
})()`);

// Close the commit menu the way a user does — an outside click — so the layer
// stack agrees it is gone. Faking it with a class would leave ☰ toggling the
// stale layer closed instead of opening the global menu.
await evaluate(`document.body.dispatchEvent(new MouseEvent("click", { bubbles: true }))`);
await sleep(300);

// 2. the ☰ menu rows
out.globalMenu = await evaluate(`(() => {
  document.getElementById("menu-btn").click();
  return [...document.querySelectorAll("#ctx-menu > *")]
    .map((n) => n.tagName === "BUTTON" ? n.textContent : "--").join(" | ");
})()`);
out.applyRowVisible = await evaluate(rowVisible("/apply a patch/"));
out.copyEntryRowVisible = await evaluate(rowVisible("/copy a bookmark or shelf entry/"));

// 3. apply a real patch through the prompt
out.applyClicked = await evaluate(clickMenuRow("/apply a patch/"));
await sleep(400);
out.promptTitle = await evaluate(`(document.getElementById("prompt-title")||{}).textContent || "NO PROMPT"`);
await evaluate(submitPrompt(PATCH));
await sleep(2500);
// The file is a format-patch mailbox, so the ENGINE asks how to land it and
// its question parks in the browser modal — exactly the behaviour the client
// is supposed to leave to it. Answering it here is the proof it arrived.
out.modalPrompt = await evaluate(
  `(document.getElementById("modal-prompt")||{}).textContent || "NO MODAL"`
);
out.modalOptions = await evaluate(
  `[...document.querySelectorAll("#modal-options button")].map((b) => b.textContent).join(" | ")`
);
out.modalAnswered = await evaluate(`(() => {
  const b = [...document.querySelectorAll("#modal-options button")].find((x) => /^commits$/.test(x.textContent));
  if (!b) return "MODAL OPTION MISSING";
  b.click();
  return b.textContent;
})()`);
await sleep(4000);
out.applyOp = await evaluate(opText);
out.appliedCommit = await evaluate(`(async () => {
  const feed = await (await fetch("/api/commits?refresh=1")).json();
  return (feed.rows || []).map((r) => r.subject).slice(0, 3).join(" | ");
})()`);

// 4. the per-file row: present inside a commit, absent in the working tree
out.fileMenuAtCommit = await evaluate(`(async () => {
  const row = document.querySelector('#commits-window .crow[data-i="1"]');
  if (!row) return "NO COMMIT ROW";
  row.click();
  await new Promise((r) => setTimeout(r, 1500));
  const f = document.querySelector("#files-list li");
  if (!f) return "NO FILE ROW";
  const b = f.getBoundingClientRect();
  f.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: b.x + 20, clientY: b.y + 5 }));
  return [...document.querySelectorAll("#ctx-menu button")].map((n) => n.textContent).join(" | ");
})()`);
out.fileExportRowPresent = /export this file's diff/.test(String(out.fileMenuAtCommit));

// 5. copy an entry to a temp dir: shelf a file, then reach the row from ☰ and
// check the prompt opens prefilled with the server's own `<repo>.tmp/…` path.
// The prefill is the whole point — a browser cannot work it out itself.
out.shelved = await evaluate(`(async () => {
  const feed = await (await fetch("/api/commits")).json();
  const row = (feed.rows || []).find((x) => /^[0-9a-f]{40}$/.test(x.hash || ""));
  const r = await fetch("/api/shelf", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ sha: row.hash, label: "probe entry" }),
  });
  return r.status;
})()`);
await evaluate(`document.body.dispatchEvent(new MouseEvent("click", { bubbles: true }))`);
await sleep(300);
out.copyEntryOpened = await evaluate(`(() => { document.getElementById("menu-btn").click(); return 1; })()`);
await evaluate(clickMenuRow("/copy a bookmark or shelf entry/"));
await sleep(1200);
out.entryPicker = await evaluate(`[...document.querySelectorAll("#ctx-menu > *")]
  .map((n) => n.tagName === "BUTTON" ? n.textContent : "--").join(" | ")`);
out.entryPicked = await evaluate(`(() => {
  const b = [...document.querySelectorAll("#ctx-menu button")][0];
  if (!b) return "NO ENTRY";
  b.click();
  return b.textContent;
})()`);
await sleep(1200);
out.copyPrompt = await evaluate(`JSON.stringify({
  title: (document.getElementById("prompt-title")||{}).textContent || "NO PROMPT",
  value: (document.getElementById("prompt-input")||{}).value || "",
})`);
// Confirm it, so the op name on the wire is exercised too and not just the
// prompt that would send it.
await evaluate(submitPrompt(null));
await sleep(3000);
out.copyOp = await evaluate(opText);

// 6. actually CLICK an export row, so downloadPatch itself runs and not only
// the endpoint it points at. Last, because it assigns window.location: the
// attachment header means the browser saves rather than navigates, but a
// regression there would take the page down and every earlier step with it.
await evaluate(`document.body.dispatchEvent(new MouseEvent("click", { bubbles: true }))`);
await sleep(300);
out.exportClicked = await evaluate(`(() => {
  const row = document.querySelector('#commits-window .crow[data-i="1"]');
  if (!row) return "NO COMMIT ROW";
  const r = row.getBoundingClientRect();
  row.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: r.x + 30, clientY: r.y + 5 }));
  const b = [...document.querySelectorAll("#ctx-menu button")].find((x) => /export as patch/.test(x.textContent));
  if (!b) return "ROW MISSING";
  b.click();
  return b.textContent;
})()`);
await sleep(2000);
out.exportOp = await evaluate(opText);
out.pageSurvived = await evaluate(`document.querySelectorAll("#commits-window .crow").length`);

out.errors = errors; // must be []
console.log(JSON.stringify(out, null, 2));
ws.close();
