// Package agentstate reads what an agent is doing — working, waiting at its
// prompt, or asking a question — from one Observation: the plain text of the
// bottom of its screen, its window title and its OSC 9;4 progress report.
// Each kind of evidence is a Detector (screen.go, signals.go); agents.go
// composes them per agent into a Profile, whose Read also yields the
// dialog's choices, the spinner's timer and the stall key. The screen rules
// began as a port of erbrus's screen classifier. Pure, stdlib only.
package agentstate
