package github

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// --- deriveStatus tests ---

func TestDeriveStatus_Success(t *testing.T) {
	raw := json.RawMessage(`{
		"statusCheckRollup": [
			{"conclusion": "SUCCESS", "status": "COMPLETED"},
			{"conclusion": "SUCCESS", "status": "COMPLETED"}
		]
	}`)
	if got := deriveStatus(raw); got != "success" {
		t.Errorf("deriveStatus() = %q, want %q", got, "success")
	}
}

func TestDeriveStatus_Failure(t *testing.T) {
	raw := json.RawMessage(`{
		"statusCheckRollup": [
			{"conclusion": "SUCCESS", "status": "COMPLETED"},
			{"conclusion": "FAILURE", "status": "COMPLETED"}
		]
	}`)
	if got := deriveStatus(raw); got != "failure" {
		t.Errorf("deriveStatus() = %q, want %q", got, "failure")
	}
}

func TestDeriveStatus_Pending(t *testing.T) {
	raw := json.RawMessage(`{
		"statusCheckRollup": [
			{"conclusion": "SUCCESS", "status": "COMPLETED"},
			{"conclusion": "", "status": "IN_PROGRESS"}
		]
	}`)
	if got := deriveStatus(raw); got != "pending" {
		t.Errorf("deriveStatus() = %q, want %q", got, "pending")
	}
}

func TestDeriveStatus_FailureBeatsPending(t *testing.T) {
	raw := json.RawMessage(`{
		"statusCheckRollup": [
			{"conclusion": "FAILURE", "status": "COMPLETED"},
			{"conclusion": "", "status": "IN_PROGRESS"}
		]
	}`)
	if got := deriveStatus(raw); got != "failure" {
		t.Errorf("deriveStatus() = %q, want %q (failure should take priority over pending)", got, "failure")
	}
}

func TestDeriveStatus_Empty(t *testing.T) {
	raw := json.RawMessage(`{"statusCheckRollup": []}`)
	if got := deriveStatus(raw); got != "" {
		t.Errorf("deriveStatus() = %q, want empty", got)
	}
}

func TestDeriveStatus_InvalidJSON(t *testing.T) {
	raw := json.RawMessage(`not json`)
	if got := deriveStatus(raw); got != "" {
		t.Errorf("deriveStatus() = %q, want empty", got)
	}
}

func TestDeriveStatus_ErrorConclusion(t *testing.T) {
	raw := json.RawMessage(`{
		"statusCheckRollup": [
			{"conclusion": "ERROR", "status": "COMPLETED"}
		]
	}`)
	if got := deriveStatus(raw); got != "failure" {
		t.Errorf("deriveStatus() = %q, want %q", got, "failure")
	}
}

func TestDeriveStatus_CancelledConclusion(t *testing.T) {
	raw := json.RawMessage(`{
		"statusCheckRollup": [
			{"conclusion": "CANCELLED", "status": "COMPLETED"}
		]
	}`)
	if got := deriveStatus(raw); got != "failure" {
		t.Errorf("deriveStatus() = %q, want %q", got, "failure")
	}
}

func TestDeriveStatus_TimedOutConclusion(t *testing.T) {
	raw := json.RawMessage(`{
		"statusCheckRollup": [
			{"conclusion": "TIMED_OUT", "status": "COMPLETED"}
		]
	}`)
	if got := deriveStatus(raw); got != "failure" {
		t.Errorf("deriveStatus() = %q, want %q", got, "failure")
	}
}

func TestDeriveStatus_ActionRequiredConclusion(t *testing.T) {
	raw := json.RawMessage(`{
		"statusCheckRollup": [
			{"conclusion": "ACTION_REQUIRED", "status": "COMPLETED"}
		]
	}`)
	if got := deriveStatus(raw); got != "failure" {
		t.Errorf("deriveStatus() = %q, want %q", got, "failure")
	}
}

func TestDeriveStatus_QueuedPending(t *testing.T) {
	raw := json.RawMessage(`{
		"statusCheckRollup": [
			{"conclusion": "", "status": "QUEUED"}
		]
	}`)
	if got := deriveStatus(raw); got != "pending" {
		t.Errorf("deriveStatus() = %q, want %q", got, "pending")
	}
}

func TestDeriveStatus_PendingStatus(t *testing.T) {
	raw := json.RawMessage(`{
		"statusCheckRollup": [
			{"conclusion": "", "status": "PENDING"}
		]
	}`)
	if got := deriveStatus(raw); got != "pending" {
		t.Errorf("deriveStatus() = %q, want %q", got, "pending")
	}
}

// --- extractAuthor tests ---

func TestExtractAuthor_ValidLogin(t *testing.T) {
	raw := json.RawMessage(`{"author":{"login":"octocat"}}`)
	if got := extractAuthor(raw); got != "octocat" {
		t.Errorf("extractAuthor() = %q, want %q", got, "octocat")
	}
}

func TestExtractAuthor_MissingAuthor(t *testing.T) {
	raw := json.RawMessage(`{"title":"some PR"}`)
	if got := extractAuthor(raw); got != "" {
		t.Errorf("extractAuthor() = %q, want empty", got)
	}
}

func TestExtractAuthor_InvalidJSON(t *testing.T) {
	raw := json.RawMessage(`not json`)
	if got := extractAuthor(raw); got != "" {
		t.Errorf("extractAuthor() = %q, want empty", got)
	}
}

// --- PR cache function tests ---

func TestWritePRCache_And_ReadPRCache(t *testing.T) {
	cacheDir := t.TempDir()
	prs := []PR{
		{Number: 1, Title: "First PR", HeadRefName: "feature-a"},
		{Number: 2, Title: "Second PR", HeadRefName: "feature-b"},
	}

	WritePRCache(cacheDir, "my-org", "my-repo", prs)

	got, err := ReadPRCache(cacheDir, "my-org", "my-repo")
	if err != nil {
		t.Fatalf("ReadPRCache() error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d PRs, want 2", len(got))
	}
	if got[0].Title != "First PR" {
		t.Errorf("PR[0].Title = %q, want %q", got[0].Title, "First PR")
	}
	if got[1].HeadRefName != "feature-b" {
		t.Errorf("PR[1].HeadRefName = %q, want %q", got[1].HeadRefName, "feature-b")
	}
}

func TestWritePRCache_PreservesAllFields(t *testing.T) {
	cacheDir := t.TempDir()
	prs := []PR{
		{
			Number:         42,
			Title:          "My PR",
			HeadRefName:    "my-branch",
			State:          "OPEN",
			ReviewDecision: "APPROVED",
			StatusRollup:   "success",
			URL:            "https://github.com/org/repo/pull/42",
			Author:         "octocat",
		},
	}

	WritePRCache(cacheDir, "org", "repo", prs)

	got, err := ReadPRCache(cacheDir, "org", "repo")
	if err != nil {
		t.Fatalf("ReadPRCache() error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d PRs, want 1", len(got))
	}
	pr := got[0]
	if pr.Number != 42 {
		t.Errorf("Number = %d, want 42", pr.Number)
	}
	if pr.State != "OPEN" {
		t.Errorf("State = %q, want %q", pr.State, "OPEN")
	}
	if pr.ReviewDecision != "APPROVED" {
		t.Errorf("ReviewDecision = %q, want %q", pr.ReviewDecision, "APPROVED")
	}
	if pr.StatusRollup != "success" {
		t.Errorf("StatusRollup = %q, want %q", pr.StatusRollup, "success")
	}
	if pr.URL != "https://github.com/org/repo/pull/42" {
		t.Errorf("URL = %q, want %q", pr.URL, "https://github.com/org/repo/pull/42")
	}
	if pr.Author != "octocat" {
		t.Errorf("Author = %q, want %q", pr.Author, "octocat")
	}
}

func TestReadPRCache_Miss(t *testing.T) {
	cacheDir := t.TempDir()
	got, err := ReadPRCache(cacheDir, "org", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil on cache miss, got %v", got)
	}
}

func TestReadPRCache_CorruptedFile(t *testing.T) {
	cacheDir := t.TempDir()
	path := filepath.Join(cacheDir, "org-repo-prs.json")
	os.WriteFile(path, []byte("not json"), 0644)

	got, err := ReadPRCache(cacheDir, "org", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil on corrupted cache, got %v", got)
	}
}

func TestWritePRCache_CreatesDirectory(t *testing.T) {
	baseDir := t.TempDir()
	cacheDir := filepath.Join(baseDir, "nested", "cache")

	prs := []PR{{Number: 1, Title: "Test"}}
	WritePRCache(cacheDir, "org", "repo", prs)

	got, err := ReadPRCache(cacheDir, "org", "repo")
	if err != nil {
		t.Fatalf("ReadPRCache() error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d PRs, want 1", len(got))
	}
}

func TestWritePRCache_EmptySlice(t *testing.T) {
	cacheDir := t.TempDir()
	WritePRCache(cacheDir, "org", "repo", []PR{})

	got, err := ReadPRCache(cacheDir, "org", "repo")
	if err != nil {
		t.Fatalf("ReadPRCache() error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d PRs, want 0", len(got))
	}
}

// --- User cache function tests ---

func TestWriteUserCache_And_ReadUserCache(t *testing.T) {
	cacheDir := t.TempDir()
	WriteUserCache(cacheDir, "octocat")

	got := ReadUserCache(cacheDir)
	if got != "octocat" {
		t.Errorf("ReadUserCache() = %q, want %q", got, "octocat")
	}
}

func TestReadUserCache_Miss(t *testing.T) {
	cacheDir := t.TempDir()
	got := ReadUserCache(cacheDir)
	if got != "" {
		t.Errorf("ReadUserCache() = %q, want empty on miss", got)
	}
}

func TestCacheDir(t *testing.T) {
	dir := CacheDir()
	if dir == "" {
		t.Error("CacheDir() should not be empty")
	}
	expected := filepath.Join(os.TempDir(), "ws-pr-cache")
	if dir != expected {
		t.Errorf("CacheDir() = %q, want %q", dir, expected)
	}
}
