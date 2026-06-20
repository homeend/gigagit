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
