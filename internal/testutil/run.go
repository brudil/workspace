package testutil

import (
	"bytes"
	"os"
	"sync"
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

	// Capture os.Stdout and os.Stderr (commands write to them directly).
	// Read while the command runs: output bigger than a pipe's buffer would
	// otherwise block the command forever.
	oldStdout, oldStderr := os.Stdout, os.Stderr
	r, w, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = w, wErr
	var stdout, stderr bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); stdout.ReadFrom(r) }()
	go func() { defer wg.Done(); stderr.ReadFrom(rErr) }()

	cmd := cli.NewRootCmd("test")
	cmd.SetArgs(args)

	runErr := cmd.Execute()

	w.Close()
	wErr.Close()
	wg.Wait()
	os.Stdout, os.Stderr = oldStdout, oldStderr

	return Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Err:    runErr,
	}
}
