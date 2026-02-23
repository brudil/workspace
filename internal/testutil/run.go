package testutil

import (
	"bytes"
	"os"
	"testing"

	"github.com/brudil/workspace/internal/cli"
	"github.com/brudil/workspace/internal/github"
)

// Result captures the output of a command execution.
type Result struct {
	Stdout string
	Stderr string
	Err    error
}

// RunCommand executes a ws CLI command against the given workspace root.
// Pass nil for gh to use a no-op stub.
func RunCommand(t *testing.T, root string, gh github.Client, args ...string) Result {
	t.Helper()

	if gh == nil {
		gh = &StubClient{}
	}

	// Build context from the fixture workspace
	ctx, err := cli.LoadContextFromDir(root)
	if err != nil {
		t.Fatalf("LoadContextFromDir(%s) failed: %v", root, err)
	}
	ctx.GitHub = gh

	// Set override so commands use our context
	cli.SetContextOverride(ctx)
	defer cli.ClearContextOverride()

	// Capture os.Stdout (commands write to it directly)
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Capture os.Stderr
	oldStderr := os.Stderr
	rErr, wErr, _ := os.Pipe()
	os.Stderr = wErr

	cmd := cli.NewRootCmd("test")
	cmd.SetArgs(args)

	runErr := cmd.Execute()

	// Restore and read captured output
	w.Close()
	os.Stdout = oldStdout
	var stdout bytes.Buffer
	stdout.ReadFrom(r)

	wErr.Close()
	os.Stderr = oldStderr
	var stderr bytes.Buffer
	stderr.ReadFrom(rErr)

	return Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Err:    runErr,
	}
}
