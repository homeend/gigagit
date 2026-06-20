package tui

import "github.com/charmbracelet/lipgloss"

// lanePalette is the recycled set of lane colors (256-color codes), drawn the
// way every git client colors graph lanes. Index by lane % len.
var lanePalette = []lipgloss.Color{"33", "208", "40", "201", "51", "220", "129"}

func laneColor(lane int) lipgloss.Color {
	if lane < 0 {
		lane = 0
	}
	return lanePalette[lane%len(lanePalette)]
}

// commitDotDecorator returns a rowDecorator that recolors the single '●' node
// glyph at text column nodeCol with color. It acts only on a row's first visual
// line (visualLine 0); it is a no-op on wrap continuations, when the node has
// scrolled off the left (nodeCol < hscroll), or when the rune at the target
// column is not '●'. Visible width is preserved (it only restyles one rune).
func commitDotDecorator(nodeCol int, color lipgloss.Color) rowDecorator {
	style := lipgloss.NewStyle().Foreground(color)
	return func(visible string, hscroll, visualLine int) string {
		if visualLine != 0 {
			return visible
		}
		col := nodeCol - hscroll
		if col < 0 {
			return visible
		}
		r := []rune(visible)
		if col >= len(r) || r[col] != '●' {
			return visible
		}
		return string(r[:col]) + style.Render(string(r[col])) + string(r[col+1:])
	}
}
