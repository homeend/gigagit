// export.js — the exported overview page: gg web's split view, offline.
// The data is the JSON blob #gg-data (overviewhtml.Export); MD is gg web's
// markdown.js and SD its splitdiff.js, both inlined just above by
// overviewhtml.Render. No storage, no network: the page is a snapshot.
(() => {
  "use strict";
  const D = JSON.parse(document.getElementById("gg-data").textContent);
  const $ = (id) => document.getElementById(id);
  const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
  const files = new Map((D.files || []).map((f) => [f.path, f]));
  const anchors = D.anchors || [];
  const live = (a) => !!a && !a.missing && files.has(a.path);
  const st = { file: "", diff: false, anchor: -1 };

  // cell paints one line: intraline marks, then syntax runs (gg web's
  // files.js renderCell without its search hits).
  function cell(text, toks, spans, side) {
    if ((!spans || !spans.length) && (!toks || !toks.length)) return esc(text);
    const rs = Array.from(text);
    const emph = new Array(rs.length).fill(false), cls = new Array(rs.length).fill("");
    for (const [a, b] of spans || []) for (let i = a; i < b && i < rs.length; i++) emph[i] = true;
    for (const [a, b, c] of toks || []) {
      if (!/^[a-z]{2,3}$/.test(c)) continue;
      for (let i = a; i < b && i < rs.length; i++) cls[i] = c;
    }
    let out = "";
    for (let i = 0; i < rs.length; ) {
      let j = i + 1;
      while (j < rs.length && emph[j] === emph[i] && cls[j] === cls[i]) j++;
      const seg = esc(rs.slice(i, j).join(""));
      out += emph[i] ? `<mark class="${side}">${seg}</mark>` : cls[i] ? `<span class="tk-${cls[i]}">${seg}</span>` : seg;
      i = j;
    }
    return out;
  }

  // band is the selected anchor's lines in the file on screen, or null.
  function band() {
    const a = anchors[st.anchor];
    if (!a || a.path !== st.file || !a.start) return null;
    return { start: a.start, end: a.end || a.start };
  }
  const inBand = (b, n) => !!b && n >= b.start && n <= b.end;

  function header() {
    $("x-title").textContent = D.title || "Overview";
    document.title = D.title || "Overview";
    const range = D.left && D.right ? D.left + " → " + D.right : "";
    // D.version is buildinfo.String(), which already starts with "gg".
    $("x-meta").textContent = [D.repo, range, "exported " + D.exported_at + " by " + D.version].filter(Boolean).join(" · ");
    $("x-files").innerHTML = [...files.keys()].map((p) => `<option value="${esc(p)}">${esc(p)}</option>`).join("");
  }

  function paintOverview() {
    const ov = $("x-ov");
    ov.innerHTML = MD.mdHTML({ blocks: D.blocks }, esc, { anchors: true }) || `<p class="x-dim">(empty overview)</p>`;
    for (const el of Array.from(ov.querySelectorAll("a.md-anchor"))) {
      if (!live(anchors[Number(el.dataset.a)])) el.replaceWith(...Array.from(el.childNodes)); // plain, as gg web draws it
    }
  }

  function paintAnchorMarks() {
    for (const el of $("x-ov").querySelectorAll("a.md-anchor")) el.classList.toggle("asel", Number(el.dataset.a) === st.anchor);
  }

  function paintCode() {
    const body = $("viewer-body"), f = files.get(st.file);
    $("x-files").value = st.file;
    $("x-flip").disabled = !f || !!f.placeholder;
    if (!f) {
      $("x-status").textContent = "";
      body.innerHTML = `<div class="x-notice">${files.size ? "pick a file" : "this overview links to no code"}</div>`;
      return;
    }
    $("x-status").textContent = (f.status ? f.status + " " : "") + (f.old_path ? f.old_path + " → " : "") + (st.diff ? "diff" : "file");
    if (f.placeholder) {
      body.innerHTML = `<div class="x-notice">${esc(f.placeholder)}</div>`;
      return;
    }
    const b = band();
    let html = "", top = 0; // top: the widest line number, for the gutter (gg web's --no-digits)
    if (st.diff) {
      if (!f.changed || !f.diff) {
        body.innerHTML = `<div class="x-notice">${f.changed ? "no diff to show for this file" : "unchanged in this range"}</div>`;
        return;
      }
      for (const r of SD.unifiedRows({ rows: f.diff })) {
        top = Math.max(top, r.old || 0, r.nu || 0);
        const sign = r.kind === "add" ? "+" : r.kind === "del" ? "−" : " ";
        const ranged = r.nu && inBand(b, r.nu) ? " vrange" : "";
        html += `<div class="vline vd-${r.kind}${ranged}" data-n="${r.nu || ""}"><span class="vno">${r.kind === "del" ? r.old : r.nu}</span>` +
          `<span class="vsign">${sign}</span><span class="vtext">${cell(r.text, r.tok, r.spans, r.kind === "del" ? "l" : "r") || " "}</span></div>`;
      }
    } else {
      top = (f.lines || []).length;
      (f.lines || []).forEach((l, i) => {
        const n = i + 1;
        html += `<div class="vline${inBand(b, n) ? " vrange" : ""}" data-n="${n}"><span class="vno">${n}</span>` +
          `<span class="vtext">${cell(l.text, l.tok, null, "r") || " "}</span></div>`;
      });
    }
    body.style.setProperty("--no-digits", String(String(top).length));
    body.innerHTML = html;
  }

  function scrollToBand() {
    const b = band(), body = $("viewer-body");
    if (!b) return void (body.scrollTop = 0);
    const el = body.querySelector(`.vline[data-n="${b.start}"]`);
    if (el) el.scrollIntoView({ block: "center" });
  }

  function openAnchor(i) {
    const a = anchors[i];
    if (!live(a)) return;
    Object.assign(st, { anchor: i, file: a.path });
    paintAnchorMarks();
    paintCode();
    scrollToBand();
    const link = $("x-ov").querySelector(`a.md-anchor[data-a="${i}"]`);
    if (link) link.scrollIntoView({ block: "nearest" });
  }

  // step opens the next (dir 1) or previous (-1) anchor that opens a file.
  function step(dir) {
    const n = anchors.length;
    const from = st.anchor < 0 ? (dir > 0 ? -1 : n) : st.anchor;
    for (let k = 1; k <= n; k++) {
      const i = (((from + dir * k) % n) + n) % n;
      if (live(anchors[i])) return openAnchor(i);
    }
  }

  function flip() {
    if ($("x-flip").disabled) return;
    st.diff = !st.diff;
    paintCode();
    scrollToBand();
  }

  function wire() {
    $("x-ov").addEventListener("click", (e) => {
      const a = e.target.closest("a.md-anchor");
      if (!a) return;
      e.preventDefault();
      openAnchor(Number(a.dataset.a));
    });
    $("x-files").addEventListener("change", (e) => {
      Object.assign(st, { file: e.target.value, anchor: -1 });
      paintAnchorMarks();
      paintCode();
      $("viewer-body").scrollTop = 0;
    });
    $("x-flip").addEventListener("click", flip);
    // n / p / D work on the file list too (focused after a pick): handled
    // and stopped here, so its type-to-find never sees them; its arrows stay.
    document.addEventListener("keydown", (e) => {
      if (e.ctrlKey || e.metaKey || e.altKey) return;
      if (e.key === "n") step(1);
      else if (e.key === "p") step(-1);
      else if (e.key === "D") flip();
      else return;
      e.preventDefault();
    });
    const drag = $("x-drag"), split = $("x-split");
    drag.addEventListener("pointerdown", (e) => {
      drag.setPointerCapture(e.pointerId);
      const move = (ev) => {
        const r = split.getBoundingClientRect();
        const pct = Math.min(80, Math.max(15, ((ev.clientX - r.left) / r.width) * 100));
        split.style.setProperty("--ov-w", pct + "%");
      };
      const up = () => {
        drag.removeEventListener("pointermove", move);
        drag.removeEventListener("pointerup", up);
      };
      drag.addEventListener("pointermove", move);
      drag.addEventListener("pointerup", up);
    });
  }

  header();
  paintOverview();
  wire();
  const first = anchors.findIndex(live);
  if (first >= 0) openAnchor(first);
  else {
    st.file = files.keys().next().value || "";
    paintCode();
  }
})();
