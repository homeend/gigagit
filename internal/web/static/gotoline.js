// gotoline.js — go to line (spec 2026-10-23-go-to-line): the one prompt every
// file surface opens on `:`, the number's parser, and the rule for when a
// diff's hidden line opens the file instead of unfolding in place.
import { openPrompt } from "./layers.js";

// --- gotoline model (pure; guarded against Go) ---
// parseLineNo is the typed text's line: digits only (spaces around them
// allowed), at most 7 of them; 0 = not a line.
export function parseLineNo(text) {
  const t = String(text || "").trim();
  if (!/^[0-9]{1,7}$/.test(t)) return 0;
  return Number(t);
}

// newerSrc is the version a diff's NEW side shows when the viewer can open
// it — the working tree or a commit — from the diff's context; null for
// anything else (the index, a two-sided compare), where a hidden line is
// unfolded in place instead.
export function newerSrc(ctx) {
  if (!ctx || ctx.cmpSides) return null;
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
    validate: (v) => (parseLineNo(v) ? "" : "type a line number (1 or more)"),
    onSubmit: (v) => onLine(parseLineNo(v)),
  });
}
