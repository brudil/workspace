package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brudil/workspace/internal/review"
	"github.com/brudil/workspace/internal/testutil"
)

// A review submitted in the web UI is crew's to read with ws review show
// (from their capsule, the latest of it), listed with ws review list, and
// the orchestrator gets it whole when run for it. Drafts stay the user's.
func TestReview_ShowListAndOrchestrate(t *testing.T) {
	w, _ := orchestrateFixture(t)
	capsule := filepath.Join(w.Root, "repos", "backend", "cache-lookup")
	os.WriteFile(filepath.Join(capsule, "cache.go"), []byte("package cache\n\nvar ttl = 0\n"), 0o644)
	testutil.GitCmd(t, capsule, "add", "-A")
	testutil.GitCmd(t, capsule, "commit", "-q", "-m", "cache")
	d, err := review.Compare(capsule, "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	store := review.For(w.Root, "cache")
	diffed := review.Diffed{Dir: capsule, BaseRef: "origin/main", Base: d.Base, Head: d.Head}
	at := time.Now()
	store.AddNote("backend/cache-lookup", diffed, review.NewNote{Path: "cache.go", Side: "new", Line: 3, Kind: "blocking", Body: "A zero TTL caches nothing."}, at)
	if r := testutil.RunCommand(t, w.Root, nil, "review", "show", "-m", "cache"); r.Err == nil || !strings.Contains(r.Err.Error(), "no reviews") {
		t.Errorf("a draft showed: %v %s", r.Err, r.Stdout)
	}
	if r := testutil.RunCommand(t, w.Root, nil, "review", "show", "-m", "cache", "r1"); r.Err == nil || !strings.Contains(r.Err.Error(), "draft") {
		t.Errorf("draft by id: %v", r.Err)
	}
	if _, err := store.Submit("backend/cache-lookup", diffed, review.Changes, "One thing.", at); err != nil {
		t.Fatal(err)
	}

	r := run(t, w, nil, "review", "show", "-m", "cache", "-f", "llm")
	for _, want := range []string{"# Review r1 of backend/cache-lookup: changes requested", "> One thing.", "### r1.c1 · blocking · cache.go:3 at", "3  var ttl = 0", "A zero TTL caches nothing.", "## What to do"} {
		if !strings.Contains(r.Stdout, want) {
			t.Errorf("show lacks %q:\n%s", want, r.Stdout)
		}
	}
	// From inside the capsule, no mission or id needed.
	t.Chdir(capsule)
	if r := testutil.RunCommand(t, capsule, nil, "review", "show"); r.Err != nil || !strings.Contains(r.Stdout, "Review r1 of backend/cache-lookup") {
		t.Errorf("from the capsule: %v %s", r.Err, r.Stdout)
	}
	if r := run(t, w, nil, "review", "list", "-m", "cache"); !strings.Contains(r.Stdout, "r1   backend/cache-lookup") || !strings.Contains(r.Stdout, "changes requested") {
		t.Errorf("list:\n%s", r.Stdout)
	}

	p := run(t, w, nil, "mission", "orchestrate", "-m", "cache", "--trigger", "review", "--print-prompt")
	for _, want := range []string{"submitted the review below (mission:reviews/r1.md)", "latest last): c1.", "A zero TTL caches nothing."} {
		if !strings.Contains(p.Stdout, want) {
			t.Errorf("orchestrator prompt lacks %q", want)
		}
	}
	if show := run(t, w, nil, "mission", "show", "cache", "-f", "llm"); !strings.Contains(show.Stdout, "mission:reviews/r1.md") {
		t.Errorf("mission show doesn't list the review:\n%s", show.Stdout)
	}
}
