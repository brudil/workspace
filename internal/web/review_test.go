package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brudil/workspace/internal/review"
	"github.com/brudil/workspace/internal/testgit"
)

func TestMain(m *testing.M) {
	cleanup := testgit.NoSigning()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// reviewServer serves a mission "m" with one capsule, api/fix, that changes
// a file since origin/main.
func reviewServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	gitT(t, dir, "init", "-q", "--initial-branch=main")
	gitT(t, dir, "config", "user.name", "Test")
	gitT(t, dir, "config", "user.email", "test@example.com")
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nvar x = 1\n"), 0o644)
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-q", "-m", "base")
	gitT(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nvar x = 2\n"), 0o644)
	gitT(t, dir, "commit", "-q", "-am", "two")
	s := startTest(t, t.TempDir(), &fakeWorkspace{}, nil)
	s.Capsule = func(slug, repo, name string) (string, string, error) {
		if slug == "m" && repo == "api" && name == "fix" {
			return dir, "origin/main", nil
		}
		return "", "", ErrNotFound
	}
	return s, dir
}

func TestReviewAPI(t *testing.T) {
	s, dir := reviewServer(t)
	res := do(t, s, "GET", "/api/missions/m/capsules/api/fix/diff", true, nil)
	var d review.Diff
	if err := json.NewDecoder(res.Body).Decode(&d); err != nil || res.StatusCode != 200 {
		t.Fatalf("diff: %d %v", res.StatusCode, err)
	}
	if len(d.Files) != 1 || d.Files[0].Path != "a.go" || d.Files[0].Additions != 1 || len(d.Commits) != 1 {
		t.Errorf("diff = %+v", d)
	}

	// Only the mission's own capsules.
	if res := do(t, s, "GET", "/api/missions/m/capsules/api/other/diff", true, nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("another capsule: %d", res.StatusCode)
	}
	if res := do(t, s, "GET", "/api/missions/other/capsules/api/fix/diff", true, nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("another mission: %d", res.StatusCode)
	}

	q := url.Values{"rev": {d.Head}, "path": {"a.go"}}
	res = do(t, s, "GET", "/api/missions/m/capsules/api/fix/blob?"+q.Encode(), true, nil)
	var blob struct{ Lines []string }
	json.NewDecoder(res.Body).Decode(&blob)
	if res.StatusCode != 200 || strings.Join(blob.Lines, "|") != "package a||var x = 2" {
		t.Errorf("blob: %d %q", res.StatusCode, blob.Lines)
	}
	q = url.Values{"base": {d.Base}, "head": {d.Head}, "path": {"a.go"}}
	res = do(t, s, "GET", "/api/missions/m/capsules/api/fix/file-diff?"+q.Encode(), true, nil)
	var f review.File
	if json.NewDecoder(res.Body).Decode(&f); res.StatusCode != 200 || len(f.Hunks) != 1 {
		t.Errorf("file diff: %d %+v", res.StatusCode, f)
	}

	// Revisions are full commit ids the API gave out, and paths are
	// relative and not options.
	for _, bad := range []url.Values{
		{"rev": {"HEAD"}, "path": {"a.go"}},
		{"rev": {"--output=/tmp/x"}, "path": {"a.go"}},
		{"rev": {d.Head}, "path": {"/etc/passwd"}},
		{"rev": {d.Head}, "path": {"--help"}},
		{"rev": {d.Head}},
	} {
		if res := do(t, s, "GET", "/api/missions/m/capsules/api/fix/blob?"+bad.Encode(), true, nil); res.StatusCode != http.StatusBadRequest {
			t.Errorf("blob %v: %d", bad, res.StatusCode)
		}
	}
	gitT(t, dir, "update-ref", "-d", "refs/remotes/origin/main")
	if res := do(t, s, "GET", "/api/missions/m/capsules/api/fix/diff", true, nil); res.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body(res), "origin/main") {
		t.Errorf("no base: %d", res.StatusCode)
	}
}
