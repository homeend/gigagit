// Package shellinit emits shell wrapper functions that let `gg` change the
// calling shell's directory on exit (via a temp --cwd-file), so create-and-
// switch and worktree switching drop the user into the chosen worktree.
package shellinit

import "fmt"

const posixWrapper = `gg() {
  local _gg_cwd
  _gg_cwd="$(mktemp)"
  command gg --cwd-file "$_gg_cwd" "$@"
  local _gg_code=$?
  if [ -s "$_gg_cwd" ]; then
    cd "$(cat "$_gg_cwd")" || true
  fi
  rm -f "$_gg_cwd"
  return $_gg_code
}
`

const fishWrapper = `function gg
    set -l _gg_cwd (mktemp)
    command gg --cwd-file "$_gg_cwd" $argv
    set -l _gg_code $status
    if test -s "$_gg_cwd"
        cd (cat "$_gg_cwd")
    end
    rm -f "$_gg_cwd"
    return $_gg_code
end
`

// Script returns the wrapper function for the given shell ("bash", "zsh", or
// "fish"). bash and zsh share a POSIX wrapper.
func Script(shell string) (string, error) {
	switch shell {
	case "bash", "zsh":
		return posixWrapper, nil
	case "fish":
		return fishWrapper, nil
	default:
		return "", fmt.Errorf("shell-init: unsupported shell %q (use bash, zsh, or fish)", shell)
	}
}
