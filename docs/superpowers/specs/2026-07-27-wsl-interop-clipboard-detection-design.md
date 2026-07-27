# WSL-interop clipboard detection

Date: 2026-07-27 · Branch: `feat/wsl-clipboard-detection`

## Problem

On WSL, `nativeCopyCmd` picks `clip.exe` on `exec.LookPath` success alone
(`internal/clipboard/native.go:61`). When WSL interop is unregistered — the
kernel has no `binfmt_misc` entry for Windows executables — the file is still on
PATH and still executable, so detection reports success and the failure only
appears at exec time as `exec format error`. `copy()` then falls back to the
OSC 52 escape, whose write succeeds whether or not the terminal honours it, so
`Copy` returns `("osc52", nil)` and the TUI paints a green "Copied …" while the
clipboard never changed.

This is not hypothetical: it has bitten this project three times, most recently
because `systemd=true` in `/etc/wsl.conf` lets `systemd-binfmt.service` flush the
`WSLInterop` registration at boot. Each time it was reported as a gg bug in
whichever copy row the user happened to press.

`clipboard.Probe()` cannot see it either — it runs the same
presence-only detection — so the existing `clipboard_tool_missing` notice stays
silent in exactly the case where the user most needs telling.

## Goals

1. Detect the broken-interop state and tell the user, through the existing
   notification center.
2. When interop is dead, stop choosing a command that cannot run, and fall
   through to the Linux clipboard tools — under WSLg there is a live Wayland
   socket, and WSLg syncs its clipboard to Windows, so `wl-copy` is a real
   second route out.

Non-goals: repairing binfmt (needs root), changing what a copy action reports at
copy time, or touching any copy call site.

## Design

### 1. Detection — `internal/clipboard`

```go
func wslInteropOK(readFile func(string) ([]byte, error)) bool
```

Pure, seam-injected like the rest of the package. Resolution order:

| Condition | Result | Why |
|---|---|---|
| `/proc/sys/fs/binfmt_misc/status` unreadable | **true** | binfmt_misc not visible = no evidence. Never regress a machine we cannot read (e.g. WSL1, an unmounted binfmt_misc). |
| status is `disabled` | **false** | Master switch off; nothing can exec. |
| `WSLInterop` or `WSLInterop-late` readable and its first line is `enabled` | **true** | The registration is live. Both names exist across WSL versions. |
| otherwise | **false** | Mounted and enabled, but the registration is gone — the observed failure. |

Unknown resolves to **true** deliberately: a false positive would strip
`clip.exe` from a machine where it works, which is worse than staying silent.

**Not cached.** Interop can be repaired mid-session, and the existing clipboard
notice already promises "no restart". Three small `/proc` reads per copy is free.

### 2. Selection — `nativeCopyCmd`

Gains an `interopOK func() bool` seam. The WSL branch becomes

```go
if isWSL && interopOK() && has("clip.exe") { ... }
```

so a broken-interop machine falls into the existing `wl-copy` / `xclip` / `xsel`
branch below. `Copy` and `probe` both pass the real reader, so the notice and the
actual behavior cannot drift.

### 3. Notice — `internal/tui/notify.go`

`clipboard.Availability` gains `WSLInteropBroken bool`, set by `probe` when
`isWSL && !interopOK()`.

New notice id `wsl_interop_broken`:

- **Fires only when `!av.Available`.** If a fallback tool covers it, copy works
  and gg stays quiet — broken Windows-exe interop in general is not gg's
  business.
- **Supersedes** `clipboard_tool_missing`: `clipboardNotice` returns nil when
  `WSLInteropBroken` is set, so one problem yields one notice.
- Dismiss-only (Not now / Never for this repo) — gg cannot sudo. Self-clears on
  the next load once either fix lands, matching the existing notice.
- Body: why a green "Copied" lied, then both remedies — the persistent
  `/etc/binfmt.d/WSLInterop.conf` + `systemctl restart systemd-binfmt`
  one-liner, or installing `wl-clipboard`.

All strings through `i18n.T`, keys added to ja/ko/zh/ru (the AST gates in
`internal/tui` fail the build otherwise).

## Testing

`internal/clipboard`: `wslInteropOK` across all four table rows plus an entry
whose first line is `disabled`; `nativeCopyCmd` under WSL-with-broken-interop
falling to `wl-copy` / `xclip` / none; `probe` setting the flag. Existing
`native_test.go` call sites take the new seam.

`internal/tui`: the notice fires when no fallback exists, stays quiet when one
does, and suppresses `clipboardNotice`.

No test needs a real clipboard or a real WSL kernel — every input is injected.

## Docs

`CHANGELOG.md`; the `debugging-clipboard-copy` skill's WSL row (it currently
documents the pre-fix behavior); `CLAUDE.md`'s `clipboard` package entry.
