package testutil

import (
	"testing"

	"github.com/brudil/workspace/internal/ui"
)

// StubConfirm replaces ui.ConfirmFunc for the duration of the test so that
// calls to ui.Confirm return the given result without prompting.
func StubConfirm(t *testing.T, result bool) {
	t.Helper()
	orig := ui.ConfirmFunc
	ui.ConfirmFunc = func(string) (bool, error) { return result, nil }
	t.Cleanup(func() { ui.ConfirmFunc = orig })
}
