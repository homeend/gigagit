# Friendly ssh host-key failure messages — design

**Date:** 2026-07-22 · **Status:** approved

## Problem

gg's TUI runs git non-interactively by design: `GIT_TERMINAL_PROMPT=0`
always, plus ssh `BatchMode` (`gitexec.WithSSHBatchMode`) so a prompt can
never freeze the UI. First ssh contact with a host whose key is not in
`~/.ssh/known_hosts` therefore fails hard with `Host key verification
failed.` plus git's misleading "check your access rights" tail — it reads
like a permissions bug (seen three times in `errors.log`, 2026-07-21). The
sibling credentials case (`could not read Username … terminal prompts
disabled`) is already rewritten by `friendlyOpError`; the host-key case is
the gap.

## Decision (user-approved)

Friendly message only — no interactive accept-key flow. gg's BatchMode is
intentional; the fix is the message, not the behavior. An interactive
ssh-keyscan/accept lane was considered and rejected for now
(security-sensitive TOFU, much bigger surface).

## Design

Two new cases in `friendlyOpError` (`internal/tui/view.go`), beside the
credentials rewrite. TUI-only, like that precedent — the CLI stays raw
English (agent surface), and `internal/pusherr` is not the home (host-key
failures are transport-level, hit fetch/pull too, no engine consumer).

1. **Changed key — classified first.** Signature: stderr contains
   `host identification has changed` (ssh's MITM warning block; in
   BatchMode it also ends with the generic line, so order matters).
   Message: `error: the remote host's ssh key CHANGED — verify with the
   host before trusting it, then update ~/.ssh/known_hosts`.
   Must NOT advise acceptance.
2. **Unknown host.** Signature: stderr contains
   `host key verification failed`. Message: `error: ssh does not trust
   this host yet — press ctrl+o and run the push/pull once to accept its
   key (gg cannot prompt)`. Points at the existing `ctrl+o` shell escape,
   where ssh can prompt interactively.

Both route through the existing `i18n.T("error: %s", …)` frame →
`statusIsError` red styling; the two inner keys are added to all four
bundles (ja/ko/zh/ru), inserted beside the credentials key (lane A of the
adding-translations skill; keys are verbless so CheckVerbs is trivial).

## Tests

Table-style cases in `internal/tui/status_error_test.go` following
`TestFriendlyOpErrorExplainsMissingCredentials`: unknown-host input
(remedy mentions ctrl+o, raw "Host key verification failed" does not
leak, line is an error), changed-key input (mentions CHANGED, does not
advise ctrl+o acceptance, wins over the generic signature when both are
present). Bundle coverage is enforced by the existing i18n scan gates.

## Out of scope

Interactive accept-key recovery (possible future stage); CLI message
rewriting; any change to BatchMode behavior.
