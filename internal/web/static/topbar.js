// topbar.js — the custom buttons in the top bar: up to ten palette commands from
// [web] toolbar (global config), after a separator line that follows refresh.
//
// The worktree path matters more than the buttons. When the row does not fit,
// buttons leave from the end into the ⋯ button, whose vertical menu opens below
// it; only with every button inside ⋯ is the path cut in the middle. With no
// custom button to show, the row is left exactly as it would be without this
// file. The arithmetic lives in toolbarfit.js.
import { $, elidePath, runes, state } from "./core.js";
import { hideCtxMenu, showCtxMenu, topLayer } from "./layers.js";
import { commandCatalog, runCommand } from "./palette.js";
import { fitToolbar, resolveToolbar } from "./toolbarfit.js";

const PATH_HINT = "double-click to copy the path · right-click for the menu";

let rows = []; // the commands on show: {label, row}
let hiddenFrom = 0; // rows[hiddenFrom…] are inside ⋯

function shownPath(text) {
  const el = $("repo-worktree");
  if (el.textContent !== text) el.textContent = text;
}

// widthOf is an element's share of the flex row: itself plus the row's gap.
function widthOf(el, gap) {
  return el.getBoundingClientRect().width + gap;
}

function fitTopBar() {
  const top = $("top");
  const path = $("repo-worktree");
  const full = state.worktree || path.textContent;
  if (!rows.length) {
    if (top.classList.contains("has-custom")) {
      top.classList.remove("has-custom");
      shownPath(full);
      path.title = PATH_HINT;
    }
    return discardOwnChanges();
  }
  // Measure the whole row at full width: every button out, the path whole.
  const sep = $("top-sep");
  const more = $("top-more");
  const buttons = [...$("top-custom").querySelectorAll("button.top-cmd")];
  top.classList.add("top-measuring"); // nothing shrinks while it is measured
  buttons.forEach((b) => (b.hidden = false));
  more.hidden = false;
  shownPath(full);
  const style = getComputedStyle(top);
  const gap = parseFloat(style.columnGap) || 0;
  const room = top.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight);
  const custom = $("top-custom");
  const fixed = [...top.children]
    .filter((el) => el !== custom && el !== path && getComputedStyle(el).display !== "none")
    .reduce((sum, el) => sum + widthOf(el, gap), 0);
  const pathW = path.getBoundingClientRect().width;
  const fit = fitToolbar({
    room,
    fixed,
    sep: widthOf(sep, gap),
    more: widthOf(more, gap),
    path: pathW,
    buttons: buttons.map((b) => widthOf(b, gap)),
  });
  top.classList.remove("top-measuring");
  hiddenFrom = fit.shown;
  buttons.forEach((b, i) => (b.hidden = i >= fit.shown));
  more.hidden = fit.shown === buttons.length;
  if (fit.cutPath) {
    // Pixels to columns by this path's own average, whatever its font is.
    const left = room - fixed - widthOf(sep, gap) - widthOf(more, gap);
    const perChar = pathW / Math.max(1, runes(full).length);
    shownPath(elidePath(full, Math.max(8, Math.floor(left / perChar))));
  }
  path.title = full + " — " + PATH_HINT;
  discardOwnChanges();
}

// renderTopBar rebuilds the buttons from state.toolbar and what applies now.
function renderTopBar() {
  const box = $("top-custom");
  const next = resolveToolbar(state.toolbar, commandCatalog()).filter((r) => r.status === "ok");
  // Asked again on every workspace read: the same buttons are left alone.
  const same = next.length === rows.length && next.every((r, i) => r.label === rows[i].label);
  rows = next;
  if (same) return;
  if (rows.length) {
    $("top").classList.add("has-custom");
    box.innerHTML = `<span id="top-sep"></span>`;
    rows.forEach(({ label, row }) => {
      const b = document.createElement("button");
      b.className = "top-cmd";
      b.textContent = label;
      b.title = row.detail ? `${label} (${row.detail})` : label;
      b.addEventListener("click", () => {
        b.blur(); // a focused button would take the next enter
        runCommand(row);
      });
      box.append(b);
    });
    const more = document.createElement("button");
    more.id = "top-more";
    more.textContent = "⋯";
    more.title = "the rest of the top bar buttons";
    more.addEventListener("click", openMore);
    box.append(more);
  } else {
    box.textContent = "";
  }
  fitTopBar();
}

function openMore(e) {
  // stopPropagation: the document-level outside-click closer would otherwise
  // see this same click and close the menu the moment it opens.
  e.stopPropagation();
  const t = topLayer();
  if (t && t.id === "ctx") {
    hideCtxMenu(); // second click toggles closed
    return;
  }
  const r = $("top-more").getBoundingClientRect();
  showCtxMenu(rows.slice(hiddenFrom).map(({ label, row }) => ({ label, act: () => runCommand(row) })), r.left, r.bottom + 4);
}

// The row re-fits whenever anything in it changes width: a window resize, a
// branch or worktree switch, a chip coming or going. Both observers run before
// the next paint, so a re-fit never shows. Its own writes are not news.
const changes = new MutationObserver(fitTopBar);
function discardOwnChanges() {
  changes.takeRecords();
}
changes.observe($("top"), { subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ["class"] });
new ResizeObserver(() => fitTopBar()).observe($("top"));
document.fonts?.ready.then(fitTopBar);
// A workspace opening or closing changes which commands apply.
document.addEventListener("gg:workspace-question", renderTopBar);

export { fitTopBar, renderTopBar };
