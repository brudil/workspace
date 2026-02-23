package testutil

import (
	"testing"
)

func TestRunCommand_Doctor(t *testing.T) {
	w := SetupWorkspace(t, WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []RepoOpts{{Name: "repo-a"}},
	})

	result := RunCommand(t, w.Root, nil, "doctor")

	if result.Err != nil {
		t.Fatalf("doctor command failed: %v\nstderr: %s", result.Err, result.Stderr)
	}
}
