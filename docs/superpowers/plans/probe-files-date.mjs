// probe-files-date.mjs — the browser check for the file-list date line.
//
//   node probe-files-date.mjs <cdp-port> <app-url>
//
// Run it TWICE: once against a build WITHOUT the change (every assertion below
// must fail) and once against the built one. Copied from probe-template.mjs.
const PORT = Number(process.argv[2] || 9222);
const APP = process.argv[3];

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

// VISIBILITY, not class names: a hidden class with no #id.hidden rule leaves
// the element on screen. Measure the box and where it sits.
const metaState = `(() => {
  const el = document.getElementById("files-meta");
  if (!el) return JSON.stringify({ present: false });
  const t = document.getElementById("files-title");
  const r = el.getBoundingClientRect(), tr = t.getBoundingClientRect();
  return JSON.stringify({
    present: true,
    title: t.textContent,
    text: el.textContent,
    visible: el.offsetHeight > 0 && r.width > 0,
    belowTitle: r.top >= tr.bottom - 1,
    stamp: /^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}( · .+)?$/.test(el.textContent),
  });
})()`;

const clickCommit = (i) => `(() => {
  const el = document.querySelector('#commits-window .crow[data-i="${i}"]');
  if (!el) return "NO ROW ${i}";
  el.click();
  return el.textContent.slice(0, 40);
})()`;

const out = {};

out.booted = await evaluate(`JSON.stringify({
  branches: document.querySelectorAll("#branches-list li").length,
  commits: document.querySelectorAll("#commits-window .crow").length,
})`);

// 1. Opening a commit shows the date, under the title, in the TUI's format.
out.step1_openCommit = await evaluate(clickCommit(1));
await sleep(1200);
out.step1_meta = await evaluate(metaState);

// 2. A DIFFERENT commit repaints it — a stale date is worse than none.
out.step2_openOther = await evaluate(clickCommit(3));
await sleep(1200);
out.step2_meta = await evaluate(metaState);

// 3. The working tree is not a commit: no date line, and it must be gone from
//    the layout, not merely blanked.
out.step3_openWorkingTree = await evaluate(`(() => {
  const el = document.querySelector('#commits-window .crow[data-i="0"]');
  if (!el) return "NO WT ROW";
  el.click();
  return el.textContent.slice(0, 40);
})()`);
// openWorkingTree awaits a fresh status read before it swaps the stage — a
// 1.2s wait passed here for the wrong reason (the stage had not changed yet,
// so the PREVIOUS commit's date was still on screen and looked stale).
await sleep(3000);
out.step3_meta = await evaluate(metaState);

// 4. Back to a commit: the line comes back (step 3 must not have killed it).
out.step4_reopenCommit = await evaluate(clickCommit(2));
await sleep(1200);
out.step4_meta = await evaluate(metaState);

// 5. The sidebar-tag open (openCommitByHash) has NO feed row behind it, so the
//    date can only come from the server. This is the path the whole
//    /api/commit/{sha} change exists for.
out.step5_openTag = await evaluate(`(() => {
  const li = document.querySelector('#tags-list li[data-h]');
  if (!li) return "NO TAG ROW";
  li.click();
  return li.textContent.slice(0, 40);
})()`);
await sleep(2000);
out.step5_meta = await evaluate(metaState);

out.errors = errors; // must be []
console.log(JSON.stringify(out, null, 2));
ws.close();
