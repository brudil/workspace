package cli

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestMain(m *testing.M) {
	lipgloss.SetDefaultRenderer(lipgloss.NewRenderer(os.Stderr, termenv.WithProfile(termenv.Ascii)))
	os.Exit(m.Run())
}
