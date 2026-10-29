# gg web `#`: a link into another checkout asks to switch — design

Parity with the TUI's `#` (internal/tui/goto_link.go). Today the web's `#`
answers a link into another checkout with an op line, "that link is in X —
open gg web there". The TUI turns the prompt into a confirm — "that link
names another checkout: X", `[enter] switch there  [esc] stay` — and enter
switches the session there and lands the link once the new checkout has
loaded (a repository link only switches). Every link kind, every caller of
the page's `gotoLink` (`#`, a note's link).

## Behaviour

- `GET /api/link-command` answering `{checkout}` (unchanged) → the page shows
  the modal confirm (`showLocalConfirm`): prompt
  `that link names another checkout: <checkout>`, options
  `switch there` (first: enter) and `stay` (esc — `stay` joins the modal's
  escape words). Not destructive, so the confirming option leads, as the
  TUI's enter does.
- `switch there` → `POST /api/link-switch {link}`. The server resolves the
  link again (never a command from the wire), and:
  - a place in this very checkout (the user's checkout moved meanwhile) →
    409 "that link is in this checkout now — paste it again";
  - a repository link → arms nothing;
  - otherwise builds the navigate against the link's checkout (`s.open`,
    as `/api/reroot` opens it; `linknav.Command` lowers a hunk there) and
    arms it as the replay a steer-ask Switch arms (`steerAsk.replay`,
    target = the checkout, `steerReplayTTL`), so `adoptService`'s
    `steerAskRerooted` hands it to the page as its start-at — landed by the
    same path (`applyStartAt`, a session overview by
    `sessionOverviewFocus`).
  - Answers `{checkout}`; the page calls `doReroot(checkout)` — the web's
    own switch lane (soft for a listed worktree, reload for another repo;
    unreachable / repairable / busy refusals are its own, as for every other
    web switch). A parked agent ask is superseded (one replay slot).
- Resolve errors keep today's op line `cannot open link: …`.

## Rulings

- A modal, not the prompt itself turning into a confirm: the web prompt
  closes on submit and resolves afterwards (its validate is synchronous);
  the modal is the page's confirm surface. Words are the TUI's.
- The TUI refuses a repairable target from `#`; the web's switch lane offers
  its repair confirm, as it does for the steer-ask Switch — one web switch
  path.
- A refused switch leaves the replay armed for at most `steerReplayTTL`,
  as the steer-ask Switch does; a re-root anywhere else drops it.

## Tests

- Server: link-switch arms a replay that lands after `adoptService` to the
  other worktree (file navigate; session overview → file_focus); a repo
  link arms nothing; a link into this checkout → 409; a bad link → 422.
- Page: source pins (gotoLink shows the confirm and posts link-switch then
  doReroot; `stay` is an escape word). Browser probe: `#` with a link into
  a second worktree → confirm → switch → landed.
