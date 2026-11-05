package workspace

import (
	"os"
	"testing"

	"github.com/brudil/workspace/internal/testgit"
)

// Tests make commits: never ask the machine's signing agent for them.
func TestMain(m *testing.M) {
	cleanup := testgit.NoSigning()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
