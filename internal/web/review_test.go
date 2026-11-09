package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brudil/workspace/internal/mission"
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

// write sends a JSON write as the web UI's own page does.
func write(t *testing.T, s *Server, method, path string, v any) (*http.Response, ReviewState) {
	t.Helper()
	data, _ := json.Marshal(v)
	res := do(t, s, method, path, true, func(r *http.Request) {
		r.Body = io.NopCloser(bytes.NewReader(data))
		r.ContentLength = int64(len(data))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", s.Info().Origin())
	})
	var st ReviewState
	json.NewDecoder(res.Body).Decode(&st)
	return res, st
}

func TestReviewWrites(t *testing.T) {
	s, _ := reviewServer(t)
	if _, err := (mission.Store{Root: s.Root}).Create("M"); err != nil {
		t.Fatal(err)
	}
	var submitted []string
	s.OnReview = func(slug string, r *review.Review) { submitted = append(submitted, slug+"/"+r.ID+"/"+r.Verdict) }
	s.clock = func() time.Time { return time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC) }
	api := "/api/missions/m/capsules/api/fix"

	var d review.Diff
	json.NewDecoder(do(t, s, "GET", api+"/diff", true, nil).Body).Decode(&d)
	res, st := write(t, s, "GET", api+"/review", nil)
	if res.StatusCode != 200 || st.Draft != nil || len(st.Reviews) != 0 || st.Viewed == nil {
		t.Fatalf("empty state: %d %+v", res.StatusCode, st)
	}

	note := map[string]any{"base": d.Base, "head": d.Head, "path": "a.go", "side": "new", "line": 3, "kind": "blocking", "body": "Two? Why two."}
	res, st = write(t, s, "POST", api+"/review/comments", note)
	if res.StatusCode != 200 || st.Draft == nil || len(st.Draft.Comments) != 1 || st.Draft.Comments[0].Excerpt[0] != "var x = 2" {
		t.Fatalf("add: %d %+v", res.StatusCode, st.Draft)
	}
	res, st = write(t, s, "PATCH", api+"/review/comments/r1.c1", map[string]any{"kind": "question", "body": "Why two?"})
	if res.StatusCode != 200 || st.Draft.Comments[0].Kind != "question" {
		t.Errorf("edit: %d %+v", res.StatusCode, st.Draft)
	}
	if res, _ := write(t, s, "PATCH", api+"/review/comments/r1.c9", map[string]any{"kind": "nit", "body": "x"}); res.StatusCode != http.StatusNotFound {
		t.Errorf("edit a missing comment: %d", res.StatusCode)
	}
	if res, _ := write(t, s, "POST", api+"/review/comments", map[string]any{"base": d.Base, "head": d.Head, "path": "a.go", "side": "new", "line": 99, "kind": "nit", "body": "x"}); res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("a line past the end: %d", res.StatusCode)
	}
	if res, _ := write(t, s, "POST", api+"/review/comments", map[string]any{"head": "HEAD", "path": "a.go"}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("no commit ids: %d", res.StatusCode)
	}
	if res, _ := write(t, s, "POST", api+"/review/comments", map[string]any{"base": d.Base, "head": d.Head, "path": "a.go", "surprise": 1}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown field: %d", res.StatusCode)
	}

	res, st = write(t, s, "PUT", api+"/review/viewed", map[string]any{"path": "a.go", "blob": d.Files[0].NewBlob, "viewed": true})
	if res.StatusCode != 200 || st.Viewed["a.go"] != d.Files[0].NewBlob {
		t.Errorf("viewed: %d %+v", res.StatusCode, st.Viewed)
	}

	res, st = write(t, s, "POST", api+"/review/submit", map[string]any{"base": d.Base, "head": d.Head, "verdict": "changes", "summary": "One question."})
	if res.StatusCode != 200 || st.Draft != nil || len(st.Reviews) != 1 || st.Reviews[0].Verdict != "changes" {
		t.Fatalf("submit: %d %+v", res.StatusCode, st)
	}
	if strings.Join(submitted, ",") != "m/r1/changes" {
		t.Errorf("OnReview = %q", submitted)
	}
	if res, _ := write(t, s, "POST", api+"/review/submit", map[string]any{"base": d.Base, "head": d.Head, "verdict": "changes"}); res.StatusCode != http.StatusUnprocessableEntity || len(submitted) != 1 {
		t.Errorf("changes requested with nothing said: %d", res.StatusCode)
	}

	// Discarding a draft.
	write(t, s, "POST", api+"/review/comments", note)
	res, st = write(t, s, "DELETE", api+"/review", nil)
	if res.StatusCode != 200 || st.Draft != nil {
		t.Errorf("discard: %d %+v", res.StatusCode, st.Draft)
	}
}
