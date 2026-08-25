package main

import "github.com/homeend/gigagit/internal/gittest"

// Pin the ambient git environment (identity, autocrlf off, no signing) for
// every test in this package — see internal/gittest.
func init() { gittest.Isolate() }
