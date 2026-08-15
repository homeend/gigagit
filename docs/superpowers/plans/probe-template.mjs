// probe-template.mjs — the browser check every web task owes.
//
//   node probe-template.mjs <cdp-port> <app-url> [step]
//
// Copy it next to your work, fill in the steps, and run it TWICE: first
// against a build WITHOUT your change (it must fail), then against yours.
// Paste both outputs into your report.
//
// Why raw CDP: the playwright npm package does not resolve on this machine,
// but its Chromium is on disk and node 22 has a built-in WebSocket, so this
// file needs no dependencies at all.
const PORT = Number(process.argv[2] || 9222);
const APP = process.argv[3];
const STEP = process.argv[4] || "all";

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
// Console errors and thrown exceptions are how a broken ES module shows up.
// `node --check` cannot see them; this can. Assert the list stays empty.
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
// A fixed viewport keeps geometry assertions meaningful run to run.
await send("Emulation.setDeviceMetricsOverride", { width: 1400, height: 900, deviceScaleFactor: 1, mobile: false });
await send("Page.navigate", { url: APP });
await sleep(5000); // the SPA boots, loads the repo, renders

const out = {};

// --- helpers you will want -------------------------------------------------

// Open a right-click menu on a row and return its labels.
const openMenu = (selector) => `(() => {
  const el = document.querySelector(${JSON.stringify(selector)});
  if (!el) return "NO ROW: ${selector}";
  const r = el.getBoundingClientRect();
  el.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: r.x + 30, clientY: r.y + 5 }));
  return [...document.querySelectorAll("#ctx-menu > *")]
    .map((n) => n.tagName === "BUTTON" ? (n.classList.contains("danger") ? "!" : "") + n.textContent : "--")
    .join(" | ");
})()`;

// Click a menu row by regex.
const clickMenuRow = (re) => `(() => {
  const b = [...document.querySelectorAll("#ctx-menu button")].find((x) => ${re}.test(x.textContent));
  if (!b) return "ROW MISSING";
  b.click();
  return b.textContent;
})()`;

// Fill the shared prompt and confirm.
const submitPrompt = (value) => `(() => {
  const i = document.getElementById("prompt-input");
  if (${JSON.stringify(value)} !== null) i.value = ${JSON.stringify(value)};
  document.getElementById("prompt-ok").click();
  return 1;
})()`;

// Answer a parked decision modal by option label.
const answerModal = (re) => `(() => {
  const b = [...document.querySelectorAll("#modal-options button")].find((x) => ${re}.test(x.textContent));
  if (!b) return "MODAL OPTION MISSING";
  b.click();
  return b.textContent;
})()`;

// What the status line last said — most ops report their result here.
const opText = `(document.getElementById("op-text") || {}).textContent || ""`;

// Geometry, not class names: a "collapsed" class can be set on a visible
// element. Measure instead.
const isVisible = (sel) => `(() => {
  const el = document.querySelector(${JSON.stringify(sel)});
  if (!el) return "MISSING";
  const r = el.getBoundingClientRect();
  return JSON.stringify({ h: el.offsetHeight, onScreen: r.bottom <= window.innerHeight + 0.5 && r.top >= 0 });
})()`;

// --- your steps ------------------------------------------------------------

out.booted = await evaluate(`JSON.stringify({
  branches: document.querySelectorAll("#branches-list li").length,
  commits: document.querySelectorAll("#commits-window .crow").length,
})`);

if (STEP === "all" || STEP === "example") {
  out.menu = await evaluate(openMenu('#commits-window .crow[data-i="1"]'));
  out.clicked = await evaluate(clickMenuRow("/^copy commit id$/"));
  await sleep(500);
  out.op = await evaluate(opText);
}

out.errors = errors; // must be [] — a non-empty list is a failure, not noise
console.log(JSON.stringify(out, null, 2));
ws.close();
