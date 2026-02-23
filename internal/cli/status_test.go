package cli

import (
	"encoding/json"
	"testing"
)

func TestStatusJSON_FullRoundTrip(t *testing.T) {
	data := statusJSON{
		Workspace: "My WS",
		Repos: []repoJSON{
			{
				Name:    "api",
				Boarded: []string{"main"},
				Worktrees: []worktreeJSON{
					{
						Name:   "main",
						Branch: "main",
						Dirty:  false,
						Ahead:  0,
						Behind: 2,
					},
					{
						Name:   "feature-x",
						Branch: "feature-x",
						Dirty:  true,
						Ahead:  3,
						Behind: 0,
						PR: &prJSON{
							Number:         42,
							Title:          "Add feature X",
							State:          "OPEN",
							URL:            "https://github.com/org/api/pull/42",
							ReviewDecision: "APPROVED",
							CheckStatus:    "success",
						},
					},
				},
			},
		},
	}

	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got statusJSON
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Workspace != "My WS" {
		t.Errorf("Workspace = %q, want %q", got.Workspace, "My WS")
	}
	if len(got.Repos) != 1 {
		t.Fatalf("len(Repos) = %d, want 1", len(got.Repos))
	}

	repo := got.Repos[0]
	if repo.Name != "api" {
		t.Errorf("Repo.Name = %q, want %q", repo.Name, "api")
	}
	if len(repo.Boarded) != 1 || repo.Boarded[0] != "main" {
		t.Errorf("Repo.Boarded = %v, want [main]", repo.Boarded)
	}
	if repo.Error != "" {
		t.Errorf("Repo.Error = %q, want empty", repo.Error)
	}
	if len(repo.Worktrees) != 2 {
		t.Fatalf("len(Worktrees) = %d, want 2", len(repo.Worktrees))
	}

	wt0 := repo.Worktrees[0]
	if wt0.Behind != 2 {
		t.Errorf("wt0.Behind = %d, want 2", wt0.Behind)
	}
	if wt0.PR != nil {
		t.Error("wt0.PR should be nil")
	}

	wt1 := repo.Worktrees[1]
	if !wt1.Dirty {
		t.Error("wt1.Dirty should be true")
	}
	if wt1.Ahead != 3 {
		t.Errorf("wt1.Ahead = %d, want 3", wt1.Ahead)
	}
	if wt1.PR == nil {
		t.Fatal("wt1.PR should not be nil")
	}
	if wt1.PR.Number != 42 {
		t.Errorf("PR.Number = %d, want 42", wt1.PR.Number)
	}
	if wt1.PR.ReviewDecision != "APPROVED" {
		t.Errorf("PR.ReviewDecision = %q, want %q", wt1.PR.ReviewDecision, "APPROVED")
	}
	if wt1.PR.CheckStatus != "success" {
		t.Errorf("PR.CheckStatus = %q, want %q", wt1.PR.CheckStatus, "success")
	}
}

func TestStatusJSON_OmitsEmptyError(t *testing.T) {
	data := repoJSON{
		Name:      "api",
		Boarded:   []string{"main"},
		Worktrees: []worktreeJSON{},
	}

	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	json.Unmarshal(b, &raw)

	if _, ok := raw["error"]; ok {
		t.Error("expected 'error' key to be omitted when empty")
	}
}

func TestStatusJSON_IncludesError(t *testing.T) {
	data := repoJSON{
		Name:      "api",
		Error:     "permission denied",
		Worktrees: nil,
	}

	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	json.Unmarshal(b, &raw)

	if _, ok := raw["error"]; !ok {
		t.Error("expected 'error' key to be present")
	}
}

func TestStatusJSON_OmitsNilPR(t *testing.T) {
	data := worktreeJSON{
		Name:   "main",
		Branch: "main",
	}

	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	json.Unmarshal(b, &raw)

	if _, ok := raw["pr"]; ok {
		t.Error("expected 'pr' key to be omitted when nil")
	}
}

func TestStatusJSON_IncludesPR(t *testing.T) {
	data := worktreeJSON{
		Name:   "feature",
		Branch: "feature",
		PR: &prJSON{
			Number: 1,
			Title:  "test",
			State:  "OPEN",
		},
	}

	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	json.Unmarshal(b, &raw)

	if _, ok := raw["pr"]; !ok {
		t.Error("expected 'pr' key to be present")
	}
}
