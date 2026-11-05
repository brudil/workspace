package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/testutil"
)

// writeHandoff writes a handoff file whose Read first section is readFirst.
func writeHandoff(t *testing.T, readFirst string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "handoff.md")
	text := "## Done\n- Cache in api/cache.go.\n## Learned\n- Lookups happen twice per request.\n## Left\n- Eviction on revocation.\n## Read first\n" + readFirst + "\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDone_KeepsAHandoffForWhoeverComesNext(t *testing.T) {
	w, store, out := dispatchFixture(t)
	t.Setenv(mission.EnvCrew, "c1")
	os.MkdirAll(store.FilesDir("cache"), 0o755)
	spike := filepath.Join(store.FilesDir("cache"), "spike.md")
	os.WriteFile(spike, []byte("notes"), 0o644)

	refused := func(args []string, want string) {
		t.Helper()
		r := testutil.RunCommand(t, w.Root, nil, append([]string{"mission", "done", "Added the cache"}, args...)...)
		if r.Err == nil || !strings.Contains(r.Err.Error(), want) {
			t.Errorf("done %v: err = %v, want %q", args, r.Err, want)
		}
	}
	refused(nil, "give a handoff")
	bad := filepath.Join(t.TempDir(), "bad.md")
	os.WriteFile(bad, []byte("## Done\nx\n"), 0o644)
	refused([]string{"--handoff", bad}, `no "Learned", "Left", "Read first" section`)
	refused([]string{"--handoff", writeHandoff(t, "- docs/nowhere.md — the plan")}, `"Read first" names what ws can't find`)
	if got, _ := store.Load("cache"); got.Crew[0].Status != "" {
		t.Fatalf("a refused done still marked the crew: %+v", got.Crew[0])
	}

	run(t, w, nil, "mission", "done", "Added the cache", "--handoff", writeHandoff(t, "- "+spike+" — the spike\n- https://example.com/design"))
	data, err := os.ReadFile(store.HandoffPath("cache", "c1"))
	if err != nil {
		t.Fatal(err)
	}
	// A file is kept as its reference, so it reads the same from anywhere.
	if !strings.Contains(string(data), "- mission:spike.md — the spike") {
		t.Errorf("handoff file:\n%s", data)
	}
	show := run(t, w, nil, "mission", "show", "-f", "llm").Stdout
	for _, want := range []string{
		"mission:handoffs/c1.md — Added the cache",
		"Latest handoffs", "crew c1: Added the cache — read: " + store.HandoffPath("cache", "c1"),
		"Added the cache · left: Eviction on revocation.",
	} {
		if !strings.Contains(show, want) {
			t.Errorf("show missing %q:\n%s", want, show)
		}
	}

	// The next crew starts where c1 left off, with where to read what it
	// named.
	t.Setenv(mission.EnvCrew, "")
	run(t, w, nil, "mission", "dispatch", "Add eviction")
	got := waitFile(t, out)
	for _, want := range []string{"dispatch: Add eviction", "## Where earlier crew left off", "# Added the cache", "Lookups happen twice", "Where to read what it names:", "read: " + spike} {
		if !strings.Contains(got, want) {
			t.Errorf("new crew's prompt missing %q:\n%s", want, got)
		}
	}
}
