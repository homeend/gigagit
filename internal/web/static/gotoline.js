// gotoline.js — go to line (spec 2026-10-23-go-to-line): the one prompt every
// file surface opens on `:`, the number's parser, and the rule for when a
// diff's hidden line opens the file instead of unfolding in place.
import { openPrompt } from "./layers.js";

// --- gotoline model (pure; guarded against Go) ---
// parseLineNo is the typed text's line: digits only (spaces around them
// allowed), the first 7 of them — the TUI's prompt takes no more; 0 = not a
// line.
export function parseLineNo(text) {
  const t = String(text || "").trim();
  if (!/^[0-9]+$/.test(t)) return 0;
  return Number(t.slice(0, 7));
}

// gotoAnswer is what enter does with the typed text: {n} lands line n; n 0
// with no why is a quiet cancel (empty, or 0 — the TUI's rule); a why keeps
// the prompt open saying so.
export function gotoAnswer(text) {
  const n = parseLineNo(text);
  if (n || /^[0-9]*$/.test(String(text || "").trim())) return { n, why: "" };
  return { n: 0, why: "type a line number (1 or more)" };
}

// newerSrc is the version a diff's NEW side shows when the viewer can open
// it — the working tree or a commit — from the diff's context; null for
// anything else (the index, a stored copy), where a hidden line is unfolded
// in place instead. The TUI's newerSide in the same order: a compare's right
// side, then a working review's marker, then the note address.
export function newerSrc(ctx) {
  if (!ctx) return null;
  if (ctx.cmpSides) {
    const right = ctx.cmpSides.right || "";
    if (right === "worktree") return { src: "worktree" };
    return /^commit:[0-9a-f]+$/.test(right) ? { src: "commit", rev: right.slice(7) } : null;
  }
  if (ctx.newWorktree) return { src: "worktree" };
  if (ctx.state === "commit") return ctx.rev ? { src: "commit", rev: ctx.rev } : null;
  if (ctx.state === "unstaged" || ctx.state === "untracked") return { src: "worktree" };
  return null;
}
// --- end gotoline model ---

// openGotoLine asks for a line number and hands it to onLine.
export function openGotoLine(onLine) {
  openPrompt({
    title: "Go to line",
    placeholder: "line number",
    allowEmpty: true, // enter on an empty field cancels, as on 0
    validate: (v) => gotoAnswer(v).why,
    onSubmit: (v) => {
      const n = gotoAnswer(v).n;
      if (n) onLine(n);
    },
  });
}
