package domain

// LiveWork is what quitting gg would end: live agent sessions plus live AI
// tasks without a running session of their own (queued ones, headless ones —
// an interactive task's session is already counted as a session). The TUI's
// quit guard and a standalone gg web's both ask before ending it.
func LiveWork() int {
	n := Sessions().LiveCount()
	for _, info := range Tasks().List() {
		if !info.State.Live() {
			continue
		}
		if info.Session != "" {
			if s, ok := Sessions().Get(info.Session); ok && s.Info().State == SessionRunning {
				continue
			}
		}
		n++
	}
	return n
}
