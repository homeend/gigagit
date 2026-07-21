package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

// staleCwdText explains a getcwd failure: the directory the shell launched gg
// from was deleted (or deleted and recreated — the shell still holds the old
// inode), so every child process fails with "fatal: Unable to read current
// working directory". Shared by the startup pre-flight and friendlyGitError.
const staleCwdText = "gg: the current directory no longer exists — it was deleted or replaced while this shell was in it.\n" +
	"    Run `cd \"$PWD\"` to re-enter it (or cd to an existing directory), then try again."

// staleCwdMessage reports the friendly stale-cwd explanation when the
// process's working directory is gone, and "" when it is fine. Checked up
// front in main so every repo-touching path fails with advice instead of a
// raw git passthrough.
func staleCwdMessage() string {
	if _, err := os.Getwd(); err != nil {
		return staleCwdText
	}
	return ""
}

// friendlyGitError maps a raw git failure — as produced by the gitexec Runner,
// e.g. "status failed (exit 128): fatal: not a git repository (or any of the
// parent directories): .git" — into a short, human-readable explanation for the
// common startup problems, falling back to the cleaned-up git message for
// anything unrecognized. Returned messages are prefixed "gg: " and may span
// multiple lines.
func friendlyGitError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "not a git repository"):
		return "gg: this folder is not a git repository.\n" +
			"    Run gg from inside a git repository, or create one here with `git init`."
	case errors.As(err, new(*exec.Error)) ||
		strings.Contains(low, "executable file not found") ||
		strings.Contains(low, "command not found"):
		return "gg: git was not found on your PATH.\n" +
			"    Install git and make sure the `git` command works in your shell."
	case strings.Contains(low, "dubious ownership"):
		return "gg: git refused this repository for safety (it is owned by another user).\n" +
			"    Trust it with: git config --global --add safe.directory <path>"
	case strings.Contains(low, "unable to read current working directory"):
		return staleCwdText
	}
	// Unrecognized: drop the "<verb> failed (exit N): " runner noise if present,
	// keeping git's own message, which is usually the useful part.
	if i := strings.Index(s, "): "); i >= 0 && strings.Contains(s[:i], "failed (exit") {
		s = strings.TrimSpace(s[i+len("): "):])
	}
	return "gg: " + s
}
