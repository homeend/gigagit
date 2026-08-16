// probe-search.mjs — the browser check for task 4 (deep commit search).
//
//   node probe-search.mjs <cdp-port> <app-url>
//
// Run it against a build WITHOUT the change first (every check must fail),
// then against the change. Derived from probe-template.mjs.
//
// The fixture it expects (see the report): 120 commits on main, one of them
// carrying the token ZEBRAFISH at depth ~78 — OUTSIDE the 50-commit first
// page, which is the point of the whole task — and two commits touching
// rare/needle.txt.
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

// --- assertions -------------------------------------------------------------
const results = [];
function check(name, ok, detail) {
  results.push({ name, ok: !!ok, detail });
}

// --- page helpers -----------------------------------------------------------

// Geometry, not class names: an element can carry `hidden` and still be on
// screen when nothing styles that class for its id.
const isVisible = (sel) => `(() => {
  const el = document.querySelector(${JSON.stringify(sel)});
  if (!el) return "MISSING";
  const r = el.getBoundingClientRect();
  return JSON.stringify({ h: el.offsetHeight, w: el.offsetWidth, onScreen: r.top >= 0 && r.left >= 0 });
})()`;

const key = (k, opts = {}) => `(() => {
  document.dispatchEvent(new KeyboardEvent("keydown", Object.assign(
    { key: ${JSON.stringify(k)}, bubbles: true, cancelable: true }, ${JSON.stringify(opts)})));
  return 1;
})()`;

const keyOn = (sel, k, opts = {}) => `(() => {
  const el = document.querySelector(${JSON.stringify(sel)});
  if (!el) return "MISSING";
  el.dispatchEvent(new KeyboardEvent("keydown", Object.assign(
    { key: ${JSON.stringify(k)}, bubbles: true, cancelable: true }, ${JSON.stringify(opts)})));
  return 1;
})()`;

const type = (sel, value) => `(() => {
  const el = document.querySelector(${JSON.stringify(sel)});
  if (!el) return "MISSING";
  el.value = ${JSON.stringify(value)};
  el.dispatchEvent(new Event("input", { bubbles: true }));
  return el.value;
})()`;

// Subjects WITHOUT the ref chips and the ◉ mark: a decorated tip reads
// "mainc700", and comparing that against an undecorated row is a false
// negative waiting to happen after a rewrite moves the branch.
const rowSubjects = (n) => `JSON.stringify([...document.querySelectorAll("#commits-window .crow")]
  .slice(0, ${n}).map((r) => {
    const s = r.querySelector(".subj");
    if (!s) return "";
    const c = s.cloneNode(true);
    c.querySelectorAll(".ref").forEach((e) => e.remove());
    return c.textContent.replace("◉ ", "").trim();
  }))`;

const markedCount = `[...document.querySelectorAll("#commits-window .crow .subj")]
  .filter((s) => s.textContent.startsWith("◉")).length`;

const rowCount = `document.querySelectorAll("#commits-window .crow").length`;

// A lane graph draws several stroked paths; a flat row draws one dot in a
// one-cell svg carrying the flatdot class. That is the visible difference
// between "graph" and "no graph", measured rather than assumed.
const graphShape = `(() => {
  const g = document.querySelector("#commits-window .crow .graph svg");
  if (!g) return "NO SVG";
  return g.classList.contains("flatdot") ? "flat" : "lanes";
})()`;

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

const answerModal = (re) => `(() => {
  const b = [...document.querySelectorAll("#modal-options button")].find((x) => ${re}.test(x.textContent));
  if (!b) return "MODAL OPTION MISSING";
  b.click();
  return b.textContent;
})()`;

const clickAway = `(() => { document.body.click(); return 1; })()`;

const ctrlClickRow = (i) => `(() => {
  const el = document.querySelector('#commits-window .crow[data-i="${i}"]');
  if (!el) return "NO ROW";
  const r = el.getBoundingClientRect();
  el.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, ctrlKey: true, clientX: r.x + 40, clientY: r.y + 5 }));
  return (el.querySelector(".subj") || {}).textContent || "";
})()`;

const opText = `(document.getElementById("op-text") || {}).textContent || ""`;

const selectedSubject = `(() => {
  const el = document.querySelector("#commits-window .crow.sel .subj");
  return el ? el.textContent : "NO SELECTED ROW";
})()`;

await send("Runtime.enable");
await send("Page.enable");
await send("Emulation.setDeviceMetricsOverride", { width: 1400, height: 900, deviceScaleFactor: 1, mobile: false });
await send("Page.navigate", { url: APP });
await sleep(5000);

const out = {};

// --- 0. the surfaces exist and start hidden ---------------------------------
out.booted = await evaluate(`JSON.stringify({ commits: ${rowCount} })`);
check("boot loaded the feed", /"commits":[1-9]/.test(out.booted || ""), out.booted);

out.barAtBoot = await evaluate(isVisible("#ffilter"));
check("the filter bar exists and starts hidden", out.barAtBoot === `{"h":0,"w":0,"onScreen":true}` || /"h":0/.test(out.barAtBoot || ""), out.barAtBoot);

// --- 1. \ opens the filter bar ----------------------------------------------
await evaluate(key("\\"));
await sleep(300);
out.barOpen = await evaluate(isVisible("#ffilter"));
check("\\ shows the filter bar", /"h":[1-9]/.test(out.barOpen || ""), out.barOpen);

// --- 2. a path filter narrows the feed and drops the graph ------------------
out.graphBefore = await evaluate(graphShape);
check("the unfiltered feed draws lanes", out.graphBefore === "lanes", out.graphBefore);

out.typedPath = await evaluate(type("#ff-path", "rare/needle.txt"));
await sleep(1200); // debounce + the server walk
out.filteredCount = await evaluate(rowCount);
out.filteredSubjects = await evaluate(rowSubjects(5));
check("the path filter narrowed the list", out.filteredCount === 2, `${out.filteredCount} rows: ${out.filteredSubjects}`);

out.graphFiltered = await evaluate(graphShape);
check("a filtered list draws no lanes", out.graphFiltered === "flat", out.graphFiltered);

out.note = await evaluate(`(document.getElementById("ff-note") || {}).textContent || "MISSING"`);
check("the bar reports the match count", /match/.test(out.note || ""), out.note);

// --- 3. one control clears it ------------------------------------------------
out.cleared = await evaluate(`(() => { const b = document.getElementById("ff-clear"); if (!b) return "MISSING"; b.click(); return 1; })()`);
await sleep(1200);
out.restoredCount = await evaluate(rowCount);
out.graphRestored = await evaluate(graphShape);
check("clearing restores the full list", out.restoredCount > 40, String(out.restoredCount));
check("clearing brings the lanes back", out.graphRestored === "lanes", out.graphRestored);

// --- 4. eager search finds a commit OUTSIDE the first page ------------------
await evaluate(key("Escape")); // close the filter bar
await sleep(200);
await evaluate(key("/"));
await sleep(200);
out.quickFilterOpen = await evaluate(isVisible("#cfilter"));
check("/ opens the quick filter", /"h":[1-9]/.test(out.quickFilterOpen || ""), out.quickFilterOpen);

await evaluate(type("#cfilter-input", "ZEBRAFISH"));
await sleep(400);
out.loadedMatchCount = await evaluate(`(document.getElementById("cfilter-count") || {}).textContent || "MISSING"`);
check(
  "the target is NOT in the loaded pages",
  /^0 \/ \d+$/.test((out.loadedMatchCount || "").trim()),
  out.loadedMatchCount
);

await evaluate(keyOn("#cfilter-input", "f", { ctrlKey: true }));
await sleep(6000);
out.afterDeep = await evaluate(`(document.getElementById("cfilter-count") || {}).textContent || "MISSING"`);
out.deepSelected = await evaluate(selectedSubject);
out.deepOp = await evaluate(opText);
check("the deep search found the match", /ZEBRAFISH/.test(out.deepSelected || ""), out.deepSelected);
const loadedBefore = Number(((out.loadedMatchCount || "").split("/")[1] || "0").trim());
const loadedAfter = Number(((out.afterDeep || "").split("/")[1] || "0").trim());
check(
  "it had to page unloaded history to find it",
  /^1 \//.test((out.afterDeep || "").trim()) && loadedAfter > loadedBefore,
  `${out.loadedMatchCount} → ${out.afterDeep}`
);

// A second press must dig PAST the hit rather than land on it again.
await evaluate(keyOn("#cfilter-input", "f", { ctrlKey: true }));
await sleep(6000);
out.secondPress = await evaluate(opText);
check("a repeat press keeps digging past the hit", /no further match/.test(out.secondPress || ""), out.secondPress);

await evaluate(keyOn("#cfilter-input", "Escape"));
await sleep(300);
// The deep search left the cursor ~500 rows down, and the list is
// virtualized: row 0 is not in the DOM until the pane is scrolled back.
await evaluate(`(() => { document.getElementById("commits-scroll").scrollTop = 0; return 1; })()`);
await sleep(400);

// --- 5. the fuzzy file finder ------------------------------------------------
await evaluate(key("F"));
await sleep(600);
out.finderOpen = await evaluate(isVisible("#finder"));
check("F opens the file finder", /"h":[1-9]/.test(out.finderOpen || ""), out.finderOpen);

await evaluate(type("#finder-input", "needle"));
await sleep(800);
out.finderRows = await evaluate(`JSON.stringify([...document.querySelectorAll("#finder-list li")].map((l) => l.textContent))`);
check("the finder ranks the tracked path", /rare\/needle\.txt/.test(out.finderRows || ""), out.finderRows);

await evaluate(key("Enter"));
await sleep(1500);
out.historyTitle = await evaluate(`(document.getElementById("history-title") || {}).textContent || "MISSING"`);
check("enter opens that file's history", /rare\/needle\.txt/.test(out.historyTitle || ""), out.historyTitle);
await evaluate(key("Escape"));
await sleep(400);

// --- 6. solo from a commit ---------------------------------------------------
out.commitMenu = await evaluate(openMenu('#commits-window .crow[data-i="1"]'));
check("the commit menu offers solo-from-here", /solo from this commit/.test(out.commitMenu || ""), out.commitMenu);
// Anchored: "bookmark this commit…" contains "mark this commit" and would
// make this pass on a build with no marking at all.
check("the commit menu offers marking", /mark this commit \(ctrl\+click\)/.test(out.commitMenu || ""), out.commitMenu);

out.tipBefore = await evaluate(`(document.querySelector('#commits-window .crow[data-i="0"] .subj') || {}).textContent || ""`);
out.soloClicked = await evaluate(clickMenuRow("/^solo from this commit$/"));
await sleep(1500);
out.soloChip = await evaluate(isVisible("#solo-chip"));
out.soloChipText = await evaluate(`(document.getElementById("solo-chip") || {}).textContent || "MISSING"`);
out.tipAfter = await evaluate(`(document.querySelector('#commits-window .crow[data-i="0"] .subj') || {}).textContent || ""`);
check("the solo chip appears", /"h":[1-9]/.test(out.soloChip || ""), out.soloChip);
check("the chip names a short sha", /^solo: [0-9a-f]{8} /.test(out.soloChipText || ""), out.soloChipText);
check("the list is scoped to that commit's ancestry", out.tipAfter && out.tipAfter !== out.tipBefore, `${out.tipBefore} → ${out.tipAfter}`);

out.chipCleared = await evaluate(`(() => { document.getElementById("solo-chip").click(); return 1; })()`);
await sleep(1500);
out.tipRestored = await evaluate(`(document.querySelector('#commits-window .crow[data-i="0"] .subj') || {}).textContent || ""`);
check("the chip clears the scope", out.tipRestored === out.tipBefore, `${out.tipAfter} → ${out.tipRestored}`);

// --- 7. marked commits: compare the pair -------------------------------------
await evaluate(clickAway);
await sleep(200);
out.mark0 = await evaluate(ctrlClickRow(0));
out.mark1 = await evaluate(ctrlClickRow(1));
await sleep(300);
out.markedRows = await evaluate(markedCount);
check("ctrl+click marks rows", out.markedRows === 2, `${out.markedRows} rows carry ◉ (${out.mark0} / ${out.mark1})`);

out.markMenu = await evaluate(openMenu('#commits-window .crow[data-i="0"]'));
check("two marks offer a compare", /compare the 2 marked commits/.test(out.markMenu || ""), out.markMenu);
check("two marks offer a squash", /squash 2 marked commits/.test(out.markMenu || ""), out.markMenu);

out.compareClicked = await evaluate(clickMenuRow("/compare the 2 marked commits/"));
await sleep(1500);
out.compareHeader = await evaluate(`(document.getElementById("files-header") || {}).textContent || "MISSING"`);
out.compareFiles = await evaluate(`document.querySelectorAll("#files-list li").length`);
check("compare opens on the two hashes", /^[0-9a-f]{8} ↔ [0-9a-f]{8}$/.test((out.compareHeader || "").trim()), out.compareHeader);
check("the compare lists changed files", out.compareFiles > 0, String(out.compareFiles));
await evaluate(key("Escape"));
await sleep(500);
await evaluate(key("Escape"));
await sleep(500);

// --- 8. squash the marked pair (LAST: it rewrites history) -------------------
out.beforeSquash = await evaluate(rowSubjects(3));
out.squashMenu = await evaluate(openMenu('#commits-window .crow[data-i="0"]'));
out.squashClicked = await evaluate(clickMenuRow("/squash 2 marked commits/"));
await sleep(600);
out.squashConfirm = await evaluate(answerModal("/^squash$/"));
check("the squash asks first", out.squashConfirm === "squash", out.squashConfirm);
await sleep(9000);
out.squashOp = await evaluate(opText);
out.afterSquash = await evaluate(rowSubjects(3));
check(
  "the squash folded the two newest commits",
  (() => {
    try {
      const before = JSON.parse(out.beforeSquash || "[]");
      const after = JSON.parse(out.afterSquash || "[]");
      // The two newest fold into one, so the row that was second is now first.
      return before.length === 3 && after.length === 3 && after[0] === before[1] && after[1] === before[2];
    } catch {
      return false;
    }
  })(),
  `${out.beforeSquash} → ${out.afterSquash} (${out.squashOp})`
);

// --- verdict -----------------------------------------------------------------
out.errors = errors; // must be [] — a non-empty list is a failure, not noise
check("no console errors or exceptions", errors.length === 0, JSON.stringify(errors));

console.log(JSON.stringify(out, null, 2));
console.log("\n--- checks ---");
for (const r of results) console.log(`${r.ok ? "PASS" : "FAIL"}  ${r.name}${r.ok ? "" : "  <- " + r.detail}`);
const failed = results.filter((r) => !r.ok).length;
console.log(`\n${results.length - failed}/${results.length} passed — ${failed ? "FAIL" : "PASS"}`);
ws.close();
process.exit(failed ? 1 : 0);
