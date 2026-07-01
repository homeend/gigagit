package tui

import (
	"fmt"
	"testing"
)

// BenchmarkFullViewScale measures the ENTIRE per-frame View() across feed
// sizes. This is the per-keystroke cost during held-key paging: if it grows
// with n, key processing falls behind the terminal auto-repeat rate, a tty
// backlog builds, and paging continues long after the key is released.
func BenchmarkFullViewScale(b *testing.B) {
	for _, n := range []int{1000, 10000, 50000, 100000} {
		m := benchModel(n, 20, 8)
		m.ready = true
		m.sel[panelCommits] = n - 1 // deep at the end, as during held-End paging
		// Populate the per-frame caches the way runtime does (rebuildCommitGraph
		// maintains them on every commits change); benchModel assigns m.commits
		// directly, which leaves them cold and would measure the fallback paths.
		m = m.syncCommitsIdx()
		m.identWCache = m.scanCommitIdentWidth(m.commits)
		m.identWValid = true
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = m.View()
			}
		})
	}
}
