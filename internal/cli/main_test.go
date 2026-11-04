package cli

import (
	"os"
	"testing"

	"github.com/brudil/workspace/internal/ui"
	"github.com/charmbracelet/colorprofile"
)

func TestMain(m *testing.M) {
	ui.Profile = colorprofile.NoTTY
	// Never spawn real orchestrators from tests: os.Executable is the test binary.
	orchestrateSpawner = func(root, slug, crewID, trigger string) {}
	os.Exit(m.Run())
}

// plainView renders a model's view content without styles; golden files are
// recorded unstyled.
func plainView(v interface{ render() string }) string {
	return ui.StripStyles(v.render())
}
