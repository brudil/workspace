package ui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Styles are declared at package level but initialised inside init() so they
// bind to the stderr renderer (lipgloss v1 captures the renderer at creation).
var (
	Bold   lipgloss.Style
	Dim    lipgloss.Style
	Blue   lipgloss.Style
	Orange lipgloss.Style
	Green  lipgloss.Style
	Red    lipgloss.Style

	TagDim    lipgloss.Style
	TagBlue   lipgloss.Style
	TagOrange lipgloss.Style
	TagGreen  lipgloss.Style
)

func init() {
	// Use stderr for TTY detection so colors work inside eval "$(...)"
	// where stdout is captured by the shell but stderr still goes to the terminal.
	lipgloss.SetDefaultRenderer(lipgloss.NewRenderer(os.Stderr))

	Bold = lipgloss.NewStyle().Bold(true)
	Dim = lipgloss.NewStyle().Faint(true)
	Blue = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	Orange = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	Green = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	Red = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))

	TagDim = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Background(lipgloss.Color("236")).Padding(0, 1)
	TagBlue = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Background(lipgloss.Color("17")).Padding(0, 1)
	TagOrange = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Background(lipgloss.Color("52")).Padding(0, 1)
	TagGreen = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Background(lipgloss.Color("22")).Padding(0, 1)
}

const BoardedMarker = "›"

// Hyperlink wraps text in an OSC 8 hyperlink escape sequence.
// Terminals that don't support it will just show the text.
func Hyperlink(url, text string) string {
	return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, text)
}
