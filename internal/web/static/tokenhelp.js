// tokenhelp.js — the TUI's ctrl+d cheat sheets (popup_help.go): the token
// grammar and date formats of a branch prefix or a text template, with live
// examples. Opened over the add-prefix form and the text-template form; esc
// returns to the form.
import { $, esc } from "./core.js";
import { closeLayer, mountOverlay, pushLayer } from "./layers.js";

// tokensHelp is the TUI's tokensHelp(now, text): {text, heading} lines.
// Token syntax is protocol and the examples are live dates, never prose.
function tokensHelp(now, text) {
  const tok = (k, desc) => ({ text: k + " ".repeat(Math.max(0, 22 - [...k].length)) + desc });
  const p2 = (n) => String(n).padStart(2, "0");
  const ymd = now.getFullYear() + "-" + p2(now.getMonth() + 1) + "-" + p2(now.getDate());
  const compact = now.getFullYear() + p2(now.getMonth() + 1) + p2(now.getDate()) + "-" + p2(now.getHours()) + p2(now.getMinutes());
  const lines = [
    { text: "Tokens", heading: true },
    tok("<user:LABEL>", text ? "asks you for LABEL whenever the text is used" : "asks you for LABEL whenever the prefix is used"),
    tok("<seq:NAME>", "per-repo counter NAME (1, 2, …)"),
    tok("<seq:NAME:N>", "the same, zero-padded to N digits"),
    tok("<date>", "today as yyyy-MM-dd"),
    tok("<date:FMT>", "now, formatted by FMT (see below)"),
  ];
  if (text) lines.push(tok("<branch>", "the branch you are on"));
  lines.push(
    tok("<parent-branch>", text ? "the same as <branch> here" : "the branch the new branch forks from"),
    tok("<repo>", "the repository directory name"),
    tok("<random-alpha:N>", "N random lowercase letters"),
    tok("<random-num:N>", "N random digits")
  );
  if (text) lines.push({ text: "Any other <…> stays in the text as written." });
  lines.push(
    { text: "" },
    { text: "Date format (FMT)", heading: true },
    tok("yyyy", "year, 4 digits"),
    tok("MM", "month 01–12"),
    tok("dd", "day 01–31"),
    tok("HH", "hour 00–23"),
    tok("mm", "minute 00–59"),
    tok("ss", "second 00–59"),
    { text: "Other characters pass through as separators (avoid yyyy/MM/dd/HH/mm/ss" },
    { text: "and digits inside literal text — they are format verbs, not literals)." },
    { text: "" },
    { text: "Examples", heading: true },
    tok("<date>", ymd),
    tok("<date:yyyy-MM-dd>", ymd),
    tok("<date:yyyyMMdd-HHmm>", compact)
  );
  return lines;
}

const box = mountOverlay("tokenhelp");
box.innerHTML = `<div id="tokenhelp-box"></div>`;

// openTokensHelp shows the sheet over the form that asked (text = the
// text-template sheet); esc or a click outside returns to it.
function openTokensHelp(text) {
  const title = text ? "Text template tokens & date formats" : "Prefix tokens & date formats";
  const body = tokensHelp(new Date(), text)
    .map((l) => (l.heading ? `<b>${esc(l.text)}</b>` : esc(l.text)))
    .join("\n");
  $("tokenhelp-box").innerHTML = `<h3>${esc(title)}</h3><pre>${body}</pre><div class="sfoot">esc returns to the form</div>`;
  pushLayer("tokenhelp", box, {
    onKey: (e) => {
      if (e.key === "Escape") {
        closeLayer("tokenhelp");
        e.preventDefault();
      }
      return true;
    },
  });
}

box.addEventListener("click", (e) => {
  if (e.target === box) closeLayer("tokenhelp");
});

export { openTokensHelp };
