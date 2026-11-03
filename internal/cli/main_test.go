package cli

import (
	"os"
	"testing"

	"github.com/brudil/workspace/internal/ui"
	"github.com/charmbracelet/colorprofile"
)

func TestMain(m *testing.M) {
	ui.Profile = colorprofile.NoTTY
	os.Exit(m.Run())
}

// plainView renders a model's view content without styles; golden files are
// recorded unstyled.
func plainView(v interface{ render() string }) string {
	return ui.StripStyles(v.render())
}
