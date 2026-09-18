// searchbar.js — the search BAR of the in-view search (inviewsearch.js is
// the engine): one `<input>` per host, wired to that host's Search. Typing
// re-finds live and scrolls to the nearest hit from where the reader was,
// enter keeps the query and hands the keyboard back to the view, esc while
// typing puts the view back where it started, esc with a kept query clears
// it (and only the NEXT esc leaves the view). Nothing here touches a host's
// DOM beyond its own bar; the `?` help row registers here once.
import { $ } from "./core.js";
import { registerHelp } from "./menus.js";
import { stepHit } from "./inviewsearch.js";

// bindSearchBar wires the bar `#<id>` (lead span, input, count span) to a
// host — see files.js / filehist.js:
//   search     its Search
//   render()   re-render the view; the render RE-FINDS the query over the
//              lines it paints (the visible ones), so hits and paint agree
//   goTo(i)    make hit i current and scroll it into view
//   here()     where the reader is with no hit current (a {row, side, col})
//   origin()   the view state esc restores; restore(o) puts it back
//   focus()    optional: put DOM focus on the content once enter blurs the bar
// It returns the verbs the host's key hook calls: open(backward),
// step(delta), clear(), reset(), active() — and paint(), which a host calls
// after a re-render of its own so the count follows the re-find.
function bindSearchBar(id, host) {
  const bar = $(id);
  const input = $(id + "-input");
  const lead = $(id + "-lead");
  const count = $(id + "-count");
  const s = host.search;
  let orig = null; // the host's view state when typing began (esc restores it)

  const paint = () => {
    lead.textContent = s.backward ? "@" : "/";
    count.textContent = s.count();
  };
  const show = () => {
    bar.classList.remove("hidden");
    paint();
  };
  const hide = () => {
    bar.classList.add("hidden");
    input.value = "";
    count.textContent = "";
  };
  // land re-renders the host with the current hits and scrolls to the
  // current hit, or puts the view back where typing began when nothing
  // matches (so a mistyped query never strands the reader mid-file).
  const land = () => {
    host.render();
    if (s.cur >= 0) host.goTo(s.cur);
    else if (orig) host.restore(orig);
    paint();
  };
  const commit = () => {
    s.typing = false;
    if (s.query === "") {
      clear();
      return;
    }
    paint();
    if (host.focus) host.focus(); // the keys go back to the content, not the body
  };
  const cancel = () => {
    s.typing = false;
    s.clear();
    host.render();
    if (orig) host.restore(orig);
    hide();
  };
  // clear drops a query (kept or being typed) and its paint. It re-renders
  // only when there were hits to unpaint: a host calls it on every new file
  // and on leaving the view, when there is nothing to draw.
  const clear = () => {
    if (!s.active()) return;
    const painted = s.hits.length > 0;
    s.clear();
    if (painted) host.render();
    hide();
  };
  // reset forgets the query without a re-render: for a host that is about
  // to draw something else anyway (a new file, leaving the view).
  const reset = () => {
    s.clear();
    hide();
  };
  const open = (backward) => {
    orig = host.origin();
    const painted = s.hits.length > 0;
    s.open(backward, host.here());
    if (painted) host.render(); // a kept query's tint must not outlive it
    input.value = "";
    show();
    input.focus();
  };
  const step = (delta) => {
    if (s.query === "") return;
    const i = stepHit(s.hits, s.pos(host.here()), delta);
    if (i < 0) return;
    host.goTo(i);
    paint();
  };

  input.addEventListener("input", () => {
    s.typing = true;
    s.query = input.value;
    land();
  });
  input.addEventListener("focus", () => {
    // Clicking back into a kept query resumes typing from where the reader
    // is now, not from where the first search began.
    if (!s.typing) {
      orig = host.origin();
      s.origin = s.pos(host.here());
      s.typing = true;
    }
  });
  input.addEventListener("blur", () => {
    // A click elsewhere keeps the query, the way enter does; esc and enter
    // clear `typing` before they blur, so they are not double-handled here.
    if (s.typing) commit();
  });
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      e.preventDefault();
      e.stopPropagation();
      s.typing = false;
      input.blur();
      commit();
    } else if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      s.typing = false;
      input.blur();
      cancel();
    }
    // Every other key is the query's: ] and [ are characters while typing.
  });

  // paint is for the host: a re-render it started itself (the f toggle, a
  // resize, a notes refresh, an unfold) re-finds and changes the count.
  return { open, step, clear, reset, paint, active: () => s.active(), input };
}

registerHelp({
  key: "/ · @ · ] · [ · in-view search",
  html:
    "in an open diff, or in the blame overlay: <b>/</b> searches the visible text forward, <b>@</b> backward " +
    "(case-insensitive; in <b>changes only</b> mode the folded lines are not searched — unfold or press <b>f</b>). " +
    "Typing scrolls to the nearest hit as you go and the bar counts <b>hit/total</b>; <b>enter</b> keeps the query and " +
    "hands the keys back to the view, <b>]</b> / <b>[</b> step to the next / previous hit (wrapping), " +
    "<b>esc</b> while typing puts the view back where it was, <b>esc</b> on a kept query clears it — only the next esc " +
    "leaves the diff or closes blame. Every hit is tinted, the current one brighter — the TUI's / @ ] [ in its diff, blame and preview views",
});

export { bindSearchBar };
