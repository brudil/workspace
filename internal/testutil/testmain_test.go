package testutil

import (
	"os"
	"testing"

	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/testgit"
)

// Tests make commits: never ask the machine's signing agent for them.
func TestMain(m *testing.M) {
	cleanup := testgit.NoSigning()
	// Hermetic: whether the Cursor CLI is installed here mustn't change
	// what doctor reports.
	os.Setenv(harness.EnvCursorBin, "/nonexistent/cursor-agent-for-tests")
	code := m.Run()
	cleanup()
	os.Exit(code)
}
