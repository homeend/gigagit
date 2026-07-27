---
name: debugging-clipboard-copy
description: Use when a gg copy action doesn't work — "copy does nothing", copied text never reaches the system clipboard, paste yields old/garbage/mojibake text, or copy works in one terminal but not under tmux/SSH/WSL. Covers the environmental causes that look like gg bugs.
---

# Debugging clipboard copy

## The core trap: copy reports success when it did nothing

`clipboard.Copy` (`internal/clipboard/native.go:304`) tries the platform's native
command, then falls back to the **OSC 52 terminal escape**
(`copy()`, `native.go:258-297`). Writing that escape to the tty almost always
*succeeds as a write* — whether or not the terminal honours it. So `Copy` returns
`("osc52", nil)`, and `internal/tui/clipboard_cmd.go:48` discards the method
label and paints a green "Copied …".

**A green "Copied" is not evidence the clipboard changed.** Never conclude "the
feature works" from the status line, and never conclude "the feature is broken"
from a failed paste. Both are compatible with every cause below.

Corollary: the copy row you're looking at (file path, absolute path, SHA, branch
name — Commits, Files, `g`/`G` switchers) is almost never the bug. All copy
actions share one `Copy`. **If one is broken, they all are** — check that before
reading any menu-row code.

## Triage order

Run these first. They take seconds and settle it.

```bash
ls /proc/sys/fs/binfmt_misc/     # WSL: is WSLInterop registered?
printf x | clip.exe; echo $?     # WSL: does a Windows exe run at all?
which xclip xsel wl-copy         # Linux: is any native tool installed?
echo "$DISPLAY $WAYLAND_DISPLAY $TMUX $SSH_CONNECTION"
```

| Symptom | Cause | Fix |
|---|---|---|
| `binfmt_misc/` has no `WSLInterop*`; `clip.exe` exits 126 `exec format error` | **WSL interop unregistered.** `systemd=true` in `/etc/wsl.conf` makes `systemd-binfmt.service` flush the registration at boot. `exec.LookPath` still finds `clip.exe`, so gg picks it (`native.go:61`) and only fails at exec time. | `sudo sh -c 'mkdir -p /etc/binfmt.d && echo ":WSLInterop:M::MZ::/init:PF" > /etc/binfmt.d/WSLInterop.conf' && sudo systemctl restart systemd-binfmt`. The `/etc/binfmt.d` file is what makes it survive reboot — a bare `echo > /proc/.../register` comes back broken. |
| Linux desktop, no `xclip`/`xsel`/`wl-copy` on PATH | **No native tool.** A CLI process cannot set the X11/Wayland selection from raw bytes; it needs a helper or a terminal that honours OSC 52. | `apt install xclip` (X11) / `wl-clipboard` (Wayland). gg picks it up with no restart. The `!` notification center already warns for this case (`clipboardNotice`, `internal/tui/notify.go:236`). |
| Wayland session, `wl-copy` installed, still nothing — **inside tmux** | tmux does not propagate `WAYLAND_DISPLAY` (not in its default `update-environment`). | Already handled: `resolveWaylandDisplay` (`native.go:88`) scans `$XDG_RUNTIME_DIR` for a live `wayland-N` socket and injects it into the child. If this recurs, verify the socket exists before suspecting gg. |
| Short strings (tag/branch names) paste as CJK mojibake; a 40-char SHA is fine | `clip.exe`'s stdin-encoding heuristic misreads short pure-ASCII payloads as UTF-16. | Already handled: `clipboardStdin` (`native.go:220`) UTF-16LE-encodes stdin for `clip`/`clip.exe` only. |
| Remote/SSH session | OSC 52 is the *expected* path; it works only if the terminal honours it. | Not a gg bug. `preferOSC52` (`native.go:206`) deliberately tries OSC 52 first here. |

## Selection order (why gg chose what it chose)

`nativeCopyCmd` (`native.go:48`) — read it before theorising:

- darwin → `pbcopy` · windows → `clip`
- linux, **WSL → `clip.exe` wins over WSLg's `wl-copy`** (the Windows clipboard is the one the user sees)
- linux → `wl-copy` (only if a Wayland display *resolves*), then `xclip`, then `xsel`
- nothing → OSC 52, else `ErrUnavailable`

Selection is by `exec.LookPath` presence only. **Presence ≠ runnable** — that gap is the WSL-interop bug above.

## Do not reintroduce

`tmux load-buffer -w -` was tried and **reverted**. `set-clipboard on` is
necessary but not sufficient: tmux only emits OSC 52 outward if it believes the
outer terminal supports it. Symptom is `tmux show-buffer` holding the text while
the system clipboard doesn't. Worse, its exit-0 masks the OSC 52 fallback with a
false "copied".

## When you can't reproduce

Detection is environment-dependent and often unreproducible from the dev box.
Ship a throwaway probe rather than guessing: a `cmd/ggclipprobe` that calls the
real `clipboard.Copy`/`Probe` and prints the chosen method, the env it read, and
the runtime-dir listing. That is what pinned the X11-without-xclip case. Delete
it before merging.

## Tests

`internal/clipboard/native_test.go` drives the pure core — `nativeCopyCmd`,
`probe`, `resolveWaylandDisplay`, `clipboardStdin` — through injected
GOOS/WSL/env/`lookPath`/Wayland seams. Add the matrix row there; no real
clipboard needed.
