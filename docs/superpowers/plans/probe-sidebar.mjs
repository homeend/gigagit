// probe-sidebar.mjs — the browser check for the sidebar toggle fix.
//
//   node probe-sidebar.mjs <cdp-port> <app-url>
//
// Run it against the build WITHOUT the fix first (the toggle checks must
// fail), then against the fix. Derived from probe-template.mjs.
//
// What it pins: the sidebar toggle actually toggles — from the key (t), from
// the ☰ menu row, and from the footer chip — measured by WIDTH, not by a
// class, and persisted server-side. Plus: the old `b` key no longer does it.
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

const results = [];
function check(name, ok, detail) {
  results.push({ name, ok: !!ok, detail });
}

// The sidebar's WIDTH is the question — a `nosb` class on #panes can be set
// while the column is plainly still there (that is exactly the shape of the
// bug this probe exists for: the state flipped, the layout did not).
const sidebarWidth = `(() => {
  const el = document.getElementById("branches-pane");
  return el ? Math.round(el.getBoundingClientRect().width) : -1;
})()`;

const key = (k) => `(() => {
  document.dispatchEvent(new KeyboardEvent("keydown", { key: ${JSON.stringify(k)}, bubbles: true, cancelable: true }));
  return 1;
})()`;

const clickMenuRow = (re) => `(() => {
  const b = [...document.querySelectorAll("#ctx-menu button")].find((x) => ${re}.test(x.textContent));
  if (!b) return "ROW MISSING";
  b.click();
  return b.textContent;
})()`;

const openMainMenu = `(() => { document.getElementById("menu-btn").click(); return document.querySelectorAll("#ctx-menu button").length; })()`;

const clickAway = `(() => { document.body.click(); return 1; })()`;

await send("Runtime.enable");
await send("Page.enable");
await send("Emulation.setDeviceMetricsOverride", { width: 1400, height: 900, deviceScaleFactor: 1, mobile: false });
await send("Page.navigate", { url: APP });
await sleep(5000);

const out = {};

out.atBoot = await evaluate(sidebarWidth);
check("the sidebar starts visible", out.atBoot > 0, String(out.atBoot));

// --- 1. the key ------------------------------------------------------------
await evaluate(key("t"));
await sleep(400);
out.afterT = await evaluate(sidebarWidth);
check("t hides the sidebar", out.afterT === 0, `${out.atBoot} → ${out.afterT}`);

await evaluate(key("t"));
await sleep(400);
out.afterT2 = await evaluate(sidebarWidth);
check("t brings it back", out.afterT2 === out.atBoot, `${out.afterT} → ${out.afterT2}`);

// --- 2. the ☰ menu row -----------------------------------------------------
out.menuRows = await evaluate(openMainMenu);
await sleep(200);
out.menuClicked = await evaluate(clickMenuRow("/^toggle sidebar$/"));
await sleep(400);
out.afterMenu = await evaluate(sidebarWidth);
check("the ☰ row is there", out.menuClicked === "toggle sidebar", out.menuClicked);
check("the ☰ row hides the sidebar", out.afterMenu === 0, `${out.afterT2} → ${out.afterMenu}`);

await evaluate(clickAway);
await sleep(200);
await evaluate(openMainMenu);
await sleep(200);
await evaluate(clickMenuRow("/^toggle sidebar$/"));
await sleep(400);
out.afterMenu2 = await evaluate(sidebarWidth);
check("the ☰ row brings it back", out.afterMenu2 === out.atBoot, `${out.afterMenu} → ${out.afterMenu2}`);

// --- 3. the footer chip ----------------------------------------------------
await evaluate(clickAway);
await sleep(200);
out.footClicked = await evaluate(`(() => {
  const b = document.querySelector('#foot button[data-act="sidebar"]');
  if (!b) return "MISSING";
  b.click();
  return b.textContent;
})()`);
await sleep(400);
out.afterFoot = await evaluate(sidebarWidth);
check("the footer chip hides the sidebar", out.afterFoot === 0, `${out.afterMenu2} → ${out.afterFoot}`);
check("the footer chip names the new key", /^t /.test(out.footClicked || ""), out.footClicked);

// --- 4. the state is persisted SERVER-side ---------------------------------
out.uistate = await evaluate(`fetch("/api/uistate").then((r) => r.json()).then((j) => JSON.stringify(j))`);
check("hidden is persisted server-side", /"sidebar_hidden":true/.test(out.uistate || ""), out.uistate);

// --- 5. the old key is gone -------------------------------------------------
await evaluate(key("b"));
await sleep(400);
out.afterB = await evaluate(sidebarWidth);
check("b no longer toggles it", out.afterB === 0, `still hidden? ${out.afterB}`);

// leave it as we found it
await evaluate(key("t"));
await sleep(400);
out.restored = await evaluate(sidebarWidth);
check("t restores it at the end", out.restored === out.atBoot, `${out.afterB} → ${out.restored}`);

out.errors = errors;
check("no console errors or exceptions", errors.length === 0, JSON.stringify(errors));

console.log(JSON.stringify(out, null, 2));
console.log("\n--- checks ---");
for (const r of results) console.log(`${r.ok ? "PASS" : "FAIL"}  ${r.name}${r.ok ? "" : "  <- " + r.detail}`);
const failed = results.filter((r) => !r.ok).length;
console.log(`\n${results.length - failed}/${results.length} passed — ${failed ? "FAIL" : "PASS"}`);
ws.close();
process.exit(failed ? 1 : 0);
