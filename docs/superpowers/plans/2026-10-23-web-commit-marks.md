# Web commit marks — plan

Spec: `docs/superpowers/specs/2026-10-23-web-commit-marks-design.md`. Executed inline, TDD.

1. Server: `POST /api/commit-drop` (`buildDrop`, BuildDrop) — tests: drops two
   non-adjacent commits; refusals (one sha, non-hex, off-branch, root); write guard.
2. Server: squash `reorder` + `not_adjacent` — tests: 422 body flag; reorder:true
   squashes c4+c2 with c3 kept.
3. Server: `POST /api/commit-cherry-pick` — tests: two picks land oldest first;
   merge refused; non-hex 400; write guard.
4. Server: review target `range` (`shas`) — tests: 2 and 3+ ranges + label; root
   refused; non-hex refused.
5. Client: commits.js pure helpers (`markedCompareEnds`, `runBetween`) under node;
   menu rows 1–4, 6–8; shift+click / shift+↑↓; review.js `shas`; help rows;
   source-pin tests.
6. Browser probe (Chromium + Firefox) on a fixture repo; race gate; CHANGELOG.
