package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/testutil"
	"github.com/brudil/workspace/internal/workspace"
)

func TestLift_CreatesWorktreeAndBoards(t *testing.T) {
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a"}},
	})

	result := testutil.RunCommand(t, w.Root, nil, "lift", "repo-a", "my-feature")

	if result.Err != nil {
		t.Fatalf("lift failed: %v\nstderr: %s", result.Err, result.Stderr)
	}

	// Worktree directory created
	wtDir := filepath.Join(w.Root, "repos", "repo-a", "my-feature")
	if _, err := os.Stat(wtDir); err != nil {
		t.Errorf("worktree dir not created: %v", err)
	}

	// Branch is correct
	branch := workspace.GitCurrentBranch(wtDir)
	if branch != "my-feature" {
		t.Errorf("branch = %q, want %q", branch, "my-feature")
	}
}

func TestLift_CustomBase(t *testing.T) {
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a"}},
	})

	result := testutil.RunCommand(t, w.Root, nil, "lift", "repo-a", "my-feature", "origin/main")

	if result.Err != nil {
		t.Fatalf("lift with base failed: %v\nstderr: %s", result.Err, result.Stderr)
	}

	wtDir := filepath.Join(w.Root, "repos", "repo-a", "my-feature")
	if _, err := os.Stat(wtDir); err != nil {
		t.Errorf("worktree dir not created: %v", err)
	}
}

func TestDock_ExistingBranch(t *testing.T) {
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a", Branches: []string{"feature-x"}}},
	})

	result := testutil.RunCommand(t, w.Root, nil, "dock", "repo-a", "feature-x")

	if result.Err != nil {
		t.Fatalf("dock failed: %v\nstderr: %s", result.Err, result.Stderr)
	}

	wtDir := filepath.Join(w.Root, "repos", "repo-a", "feature-x")
	if _, err := os.Stat(wtDir); err != nil {
		t.Errorf("worktree dir not created: %v", err)
	}

	branch := workspace.GitCurrentBranch(wtDir)
	if branch != "feature-x" {
		t.Errorf("branch = %q, want %q", branch, "feature-x")
	}
}

func TestDock_PRNumber(t *testing.T) {
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a", Branches: []string{"pr-branch"}}},
	})

	stub := &testutil.StubClient{
		PRFromNumberFn: func(org, repo string, number int) (*github.PR, error) {
			return &github.PR{Number: 42, Title: "My PR", HeadRefName: "pr-branch"}, nil
		},
	}

	result := testutil.RunCommand(t, w.Root, stub, "dock", "repo-a", "42")

	if result.Err != nil {
		t.Fatalf("dock by PR number failed: %v\nstderr: %s", result.Err, result.Stderr)
	}

	wtDir := filepath.Join(w.Root, "repos", "repo-a", "pr-branch")
	branch := workspace.GitCurrentBranch(wtDir)
	if branch != "pr-branch" {
		t.Errorf("branch = %q, want %q", branch, "pr-branch")
	}
}

func TestDock_PRURL(t *testing.T) {
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a", Branches: []string{"url-branch"}}},
	})

	stub := &testutil.StubClient{
		PRFromNumberFn: func(org, repo string, number int) (*github.PR, error) {
			return &github.PR{Number: 99, Title: "URL PR", HeadRefName: "url-branch"}, nil
		},
	}

	result := testutil.RunCommand(t, w.Root, stub, "dock", "https://github.com/test-org/repo-a/pull/99")

	if result.Err != nil {
		t.Fatalf("dock by PR URL failed: %v\nstderr: %s", result.Err, result.Stderr)
	}

	wtDir := filepath.Join(w.Root, "repos", "repo-a", "url-branch")
	branch := workspace.GitCurrentBranch(wtDir)
	if branch != "url-branch" {
		t.Errorf("branch = %q, want %q", branch, "url-branch")
	}
}

func TestDebrief_RemovesMergedCapsule(t *testing.T) {
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a"}},
	})

	// Lift a capsule — branch is created at the same commit as main,
	// so git considers it already merged into main.
	liftResult := testutil.RunCommand(t, w.Root, nil, "lift", "repo-a", "merged-feature")
	if liftResult.Err != nil {
		t.Fatalf("lift failed: %v\nstderr: %s", liftResult.Err, liftResult.Stderr)
	}

	wtDir := filepath.Join(w.Root, "repos", "repo-a", "merged-feature")
	if _, err := os.Stat(wtDir); err != nil {
		t.Fatalf("worktree dir not created after lift: %v", err)
	}

	// Debrief should detect the branch as merged and remove the worktree.
	debriefResult := testutil.RunCommand(t, w.Root, nil, "debrief")
	if debriefResult.Err != nil {
		t.Fatalf("debrief failed: %v\nstderr: %s", debriefResult.Err, debriefResult.Stderr)
	}

	// Worktree directory should be gone.
	if _, err := os.Stat(wtDir); err == nil {
		t.Errorf("worktree dir still exists after debrief; expected removal")
	}
}

func TestDebrief_SkipsDirtyCapsule(t *testing.T) {
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a"}},
	})

	// Lift a capsule (branch at same commit as main, so merged).
	liftResult := testutil.RunCommand(t, w.Root, nil, "lift", "repo-a", "dirty-feature")
	if liftResult.Err != nil {
		t.Fatalf("lift failed: %v\nstderr: %s", liftResult.Err, liftResult.Stderr)
	}

	wtDir := filepath.Join(w.Root, "repos", "repo-a", "dirty-feature")

	// Create an uncommitted file to make the worktree dirty.
	if err := os.WriteFile(filepath.Join(wtDir, "uncommitted.txt"), []byte("dirty"), 0644); err != nil {
		t.Fatalf("failed to create dirty file: %v", err)
	}

	// Debrief should skip the dirty capsule even though its branch is merged.
	debriefResult := testutil.RunCommand(t, w.Root, nil, "debrief")
	if debriefResult.Err != nil {
		t.Fatalf("debrief failed: %v\nstderr: %s", debriefResult.Err, debriefResult.Stderr)
	}

	// Worktree directory should still exist because it's dirty.
	if _, err := os.Stat(wtDir); err != nil {
		t.Errorf("worktree dir removed despite being dirty: %v", err)
	}
}

func TestDebrief_ReportsInOrbit(t *testing.T) {
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a"}},
	})

	// Lift a capsule.
	liftResult := testutil.RunCommand(t, w.Root, nil, "lift", "repo-a", "orbit-feature")
	if liftResult.Err != nil {
		t.Fatalf("lift failed: %v\nstderr: %s", liftResult.Err, liftResult.Stderr)
	}

	wtDir := filepath.Join(w.Root, "repos", "repo-a", "orbit-feature")

	// Make a commit so the branch diverges from main and is no longer merged.
	os.WriteFile(filepath.Join(wtDir, "work.txt"), []byte("new work"), 0644)
	testutil.GitCmd(t, wtDir, "add", ".")
	testutil.GitCmd(t, wtDir, "commit", "-m", "diverge from main")

	// Debrief should report this capsule as "still in orbit".
	debriefResult := testutil.RunCommand(t, w.Root, nil, "debrief")
	if debriefResult.Err != nil {
		t.Fatalf("debrief failed: %v\nstderr: %s", debriefResult.Err, debriefResult.Stderr)
	}

	// Worktree directory should still exist.
	if _, err := os.Stat(wtDir); err != nil {
		t.Errorf("worktree dir removed for in-orbit capsule: %v", err)
	}
}

func TestBoard_AddsToWorkspace(t *testing.T) {
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a"}},
	})

	result := testutil.RunCommand(t, w.Root, nil, "board", "repo-a", ".ground")
	if result.Err != nil {
		t.Fatalf("board failed: %v\nstderr: %s", result.Err, result.Stderr)
	}

	// Verify ws.local.toml has boarded state
	localToml := filepath.Join(w.Root, "ws.local.toml")
	data, err := os.ReadFile(localToml)
	if err != nil {
		t.Fatalf("ws.local.toml not found: %v", err)
	}
	if !strings.Contains(string(data), ".ground") {
		t.Errorf("expected .ground in ws.local.toml, got:\n%s", string(data))
	}
}

func TestUnboard_RemovesFromWorkspace(t *testing.T) {
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a"}},
	})

	// Board first
	result := testutil.RunCommand(t, w.Root, nil, "board", "repo-a", ".ground")
	if result.Err != nil {
		t.Fatalf("board failed: %v\nstderr: %s", result.Err, result.Stderr)
	}

	// Unboard
	result = testutil.RunCommand(t, w.Root, nil, "unboard", "repo-a", ".ground")
	if result.Err != nil {
		t.Fatalf("unboard failed: %v\nstderr: %s", result.Err, result.Stderr)
	}
}

func TestJump_OutputsCd(t *testing.T) {
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a"}},
	})

	result := testutil.RunCommand(t, w.Root, nil, "jump", "repo-a", ".ground")
	if result.Err != nil {
		t.Fatalf("jump failed: %v\nstderr: %s", result.Err, result.Stderr)
	}

	expected := filepath.Join(w.Root, "repos", "repo-a", ".ground")
	if !strings.Contains(result.Stdout, expected) {
		t.Errorf("expected cd to %s in stdout, got: %q", expected, result.Stdout)
	}
}

func TestDoctor_HealthyWorkspace(t *testing.T) {
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a"}},
	})

	result := testutil.RunCommand(t, w.Root, nil, "doctor")
	if result.Err != nil {
		t.Fatalf("doctor failed on healthy workspace: %v\nstderr: %s", result.Err, result.Stderr)
	}
}

func TestDoctor_MissingRepo(t *testing.T) {
	w := testutil.SetupWorkspace(t, testutil.WorkspaceOpts{
		Org:           "test-org",
		DefaultBranch: "main",
		Repos:         []testutil.RepoOpts{{Name: "repo-a"}, {Name: "repo-b"}},
	})

	// Remove repo-b's bare dir to simulate a missing repo
	os.RemoveAll(filepath.Join(w.Root, "repos", "repo-b", ".bare"))

	result := testutil.RunCommand(t, w.Root, nil, "doctor")
	if result.Err == nil {
		t.Fatal("expected doctor to return an error for missing repo")
	}
}

