// markdown.js — paints a parsed markdown tree as HTML.
//
// This is a PAINTER, not a parser: the server parses forge text once
// (internal/markdown) and sends the tree; the page never looks at markdown
// source. The file has no dependencies at all, so the node test can load it
// (internal/web/markdownjs_test.go feeds it the Go parser's own goldens).
//
// Safety is structural, not a sanitizer:
//   - every string taken from the tree goes through the esc() it is handed;
//   - the tags and attributes below are the only ones ever written, and no
//     attribute value but an href, a title and data-lang comes from the tree;
//   - an href is written only for an http(s) URL — checked HERE as well as in
//     the parser, so a tree that did not come from the parser is inert too;
//   - an image is a link to its URL, never an <img>: the page fetches nothing;
//   - a node kind this file does not know paints as its escaped text.
// Every value is type-checked before use: the tree is JSON off the wire.

const HTTP = /^https?:\/\/[^\s\u0000-\u001f]+$/i;
const LANG = /^[A-Za-z0-9_+#.-]{1,32}$/;
const TOK = /^[a-z]{1,4}$/;
const ALIGN = { left: 1, center: 1, right: 1 };
const MAX_DEPTH = 24; // far past the parser's own cap; a guard, not a feature

const str = (v) => (typeof v === "string" ? v : "");
const arr = (v) => (Array.isArray(v) ? v : []);

// mdInlineHTML paints one inline run (a summary line, a paragraph's content).
export function mdInlineHTML(inl, esc, depth = 0) {
  if (depth > MAX_DEPTH) return "";
  let out = "";
  for (const n of arr(inl)) {
    if (!n || typeof n !== "object") continue;
    const kids = () => mdInlineHTML(n.in, esc, depth + 1);
    switch (n.k) {
      case "text": out += esc(str(n.t)); break;
      case "br": out += "<br>"; break;
      case "code": out += `<code class="md-ic">${esc(str(n.t))}</code>`; break;
      case "ref": out += `<span class="md-ref">${esc(str(n.t))}</span>`; break;
      case "strong": out += `<strong>${kids()}</strong>`; break;
      case "em": out += `<em>${kids()}</em>`; break;
      case "del": out += `<del>${kids()}</del>`; break;
      case "link": out += anchor(n.url, kids(), esc); break;
      case "image": out += anchor(n.url, esc(str(n.t) ? `[image: ${str(n.t)}]` : "[image]"), esc); break;
      default: out += esc(str(n.t)) + kids();
    }
  }
  return out;
}

// anchor wraps already-painted HTML in a link — only for an http(s) URL.
function anchor(url, inner, esc) {
  const u = str(url);
  if (!HTTP.test(u)) return inner;
  return `<a href="${esc(u)}" target="_blank" rel="noopener noreferrer" title="${esc(u)}">${inner}</a>`;
}

// mdHTML paints a whole parsed text ({blocks: [...]}); "" for anything else.
export function mdHTML(doc, esc) {
  if (!doc || typeof doc !== "object") return "";
  return blocksHTML(doc.blocks, esc, 0);
}

function blocksHTML(blocks, esc, depth, lead = "") {
  if (depth > MAX_DEPTH) return "";
  let out = "";
  let first = true;
  for (const b of arr(blocks)) {
    if (!b || typeof b !== "object") continue;
    // lead is a task item's box: it sits inside the item's first paragraph.
    const head = first ? lead : "";
    switch (b.k) {
      case "p": out += `<p>${head}${mdInlineHTML(b.in, esc)}</p>`; break;
      case "h": out += head + headingHTML(b, esc); break;
      case "list": out += head + listHTML(b, esc, depth); break;
      case "quote": out += `${head}<blockquote class="md-q">${blocksHTML(b.blocks, esc, depth + 1)}</blockquote>`; break;
      case "code": out += head + codeHTML(b, esc); break;
      case "table": out += head + tableHTML(b, esc); break;
      case "hr": out += `${head}<hr class="md-hr">`; break;
      default: out += `<p>${head}${mdInlineHTML(b.in, esc)}</p>`;
    }
    first = false;
  }
  return out;
}

// A heading keeps its LEVEL as a class but never paints an h1/h2: those belong
// to the overlay that hosts the text.
function headingHTML(b, esc) {
  const level = Math.min(6, Math.max(1, Number.isInteger(b.level) ? b.level : 1));
  const tag = "h" + Math.min(6, level + 2);
  return `<${tag} class="md-h md-h${level}">${mdInlineHTML(b.in, esc)}</${tag}>`;
}

function listHTML(b, esc, depth) {
  const ordered = b.ordered === true;
  const start = ordered && Number.isInteger(b.start) && b.start >= 0 && b.start !== 1 ? ` start="${b.start}"` : "";
  let items = "";
  for (const it of arr(b.items)) {
    if (!it || typeof it !== "object") continue;
    const task = it.task === "done" ? "☑" : it.task === "open" ? "☐" : "";
    const lead = task ? `<span class="md-box">${task}</span> ` : "";
    items += `<li${task ? ' class="md-task"' : ""}>${blocksHTML(it.blocks, esc, depth + 1, lead)}</li>`;
  }
  const tag = ordered ? "ol" : "ul";
  return `<${tag} class="md-list"${start}>${items}</${tag}>`;
}

function codeHTML(b, esc) {
  const lang = LANG.test(str(b.lang)) ? str(b.lang) : "";
  const cap = lang.toLowerCase() === "suggestion" ? `<div class="md-cap">suggestion</div>` : "";
  const lines = arr(b.lines).map((l) => codeLineHTML(l, esc)).join("\n");
  return `${cap}<pre class="md-code"${lang ? ` data-lang="${esc(lang)}"` : ""}><code>${lines}</code></pre>`;
}

// One code line: its token runs (rune offsets) become .tk-* spans. A run that
// is malformed, out of range, overlapping or of an unknown class shape is
// skipped — the text is painted either way.
function codeLineHTML(l, esc) {
  if (!l || typeof l !== "object") return "";
  const runes = Array.from(str(l.t));
  let out = "";
  let pos = 0;
  for (const t of arr(l.toks)) {
    if (!t || !Number.isInteger(t.s) || !Number.isInteger(t.e) || !TOK.test(str(t.c))) continue;
    if (t.s < pos || t.e <= t.s || t.e > runes.length) continue;
    out += esc(runes.slice(pos, t.s).join(""));
    out += `<span class="tk-${t.c}">${esc(runes.slice(t.s, t.e).join(""))}</span>`;
    pos = t.e;
  }
  return out + esc(runes.slice(pos).join(""));
}

function tableHTML(b, esc) {
  const align = arr(b.align);
  const cls = (i) => (ALIGN[align[i]] === 1 ? ` class="md-al-${align[i]}"` : "");
  const row = (cells, tag) =>
    "<tr>" + arr(cells).map((c, i) => `<${tag}${cls(i)}>${mdInlineHTML(c, esc)}</${tag}>`).join("") + "</tr>";
  const body = arr(b.rows).map((r) => row(r, "td")).join("");
  return `<div class="md-tablewrap"><table class="md-table"><thead>${row(b.head, "th")}</thead><tbody>${body}</tbody></table></div>`;
}
