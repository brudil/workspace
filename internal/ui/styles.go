package ui

import (
	"fmt"

	"charm.land/lipgloss/v2"
)

// Lip Gloss v2 styles are plain values: Render always emits full-fidelity
// ANSI, and colours are downsampled when written. Write styled output through
// ui.Stderr / ui.Stdout (see output.go) so it matches the terminal.
var (
	Bold   = lipgloss.NewStyle().Bold(true)
	Dim    = lipgloss.NewStyle().Faint(true)
	Blue   = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	Orange = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	Green  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	Red    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))

	TagDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Background(lipgloss.Color("236")).Padding(0, 1)
	TagBlue   = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Background(lipgloss.Color("17")).Padding(0, 1)
	TagOrange = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Background(lipgloss.Color("52")).Padding(0, 1)
	TagGreen  = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Background(lipgloss.Color("22")).Padding(0, 1)
)

const BoardedMarker = "›"

// Hyperlink wraps text in an OSC 8 hyperlink escape sequence.
// Terminals that don't support it will just show the text.
func Hyperlink(url, text string) string {
	return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, text)
}
