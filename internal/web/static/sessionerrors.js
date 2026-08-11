// sessionerrors.js — the Session errors overlay: a read-only list of this
// server session's genuine failures (the observ failure ring the TUI's
// settings errors view reads — captured at the domain boundary, user aborts
// excluded). Newest first; the durable errors.log path is shown for history
// older than this session.
import { $, esc, getJSON } from "./core.js";
import { closeLayer, pushLayer } from "./layers.js";

let data = null;

async function openSessionErrorsView() {
  try {
    data = await getJSON("/api/session-errors");
  } catch (e) {
    return;
  }
  renderSessionErrors();
  pushLayer("sesserrors", $("sesserrors"), {
    onKey: (e) => {
      if (e.key === "Escape") {
        closeLayer("sesserrors");
        e.preventDefault();
        return true;
      }
      return true;
    },
  });
}

function fmtTime(rfc) {
  const d = new Date(rfc);
  return isNaN(d) ? rfc : d.toLocaleTimeString();
}

function renderSessionErrors() {
  if (!data) return;
  const rows = (data.errors || [])
    .map(
      (e) => `
    <div class="erow">
      <div class="srow"><span class="snote">${esc(fmtTime(e.time))}</span><span class="xbadge">${esc(e.source)}</span></div>
      <div class="edetail">${esc(e.detail)}</div>
    </div>`
    )
    .join("");
  const trunc = data.truncated ? '<div class="srow"><span class="snote">(older entries not shown)</span></div>' : "";
  $("sesserrors-box").innerHTML = `
    <h2>session errors</h2>
    ${rows || '<div class="srow"><span class="snote">(no failures this session)</span></div>'}
    ${trunc}
    <div class="sfoot">genuine failures only — user aborts are excluded · newest first · this server session's ring; the full history persists in ${esc(data.log_path || "the state dir's errors.log")} · esc closes</div>`;
}

$("sesserrors").addEventListener("click", (e) => {
  if (e.target === $("sesserrors")) closeLayer("sesserrors");
});

export { openSessionErrorsView };
