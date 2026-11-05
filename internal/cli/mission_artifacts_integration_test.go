package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brudil/workspace/internal/cli"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/testutil"
)

// artifactsFixture is a mission "cache" with a backend capsule holding a
// proposal draft, a private note, and a capsule outside the mission.
func artifactsFixture(t *testing.T) (w *testutil.Workspace, capsule, proposal, note string) {
	t.Helper()
	w = missionWorkspace(t)
	run(t, w, nil, "mission", "new", "Cache", "--brief", "## Goal\nCache it.")
	t.Setenv(mission.EnvMission, "cache")
	run(t, w, nil, "lift", "backend", "cache-lookup")
	t.Setenv(mission.EnvMission, "")
	run(t, w, nil, "lift", "frontend", "elsewhere") // not the mission's
	capsule = filepath.Join(w.Root, "repos", "backend", "cache-lookup")
	proposal = filepath.Join(capsule, "docs", "proposal.md")
	os.MkdirAll(filepath.Dir(proposal), 0o755)
	os.WriteFile(proposal, []byte("# Proposal\n"), 0o644)
	note = filepath.Join(w.Root, ".ws", "missions", "cache", "files", "spike.md")
	os.MkdirAll(filepath.Dir(note), 0o755)
	os.WriteFile(note, []byte("spike\n"), 0o644)
	return w, capsule, proposal, note
}

func TestArtifacts_AddAnythingAndShowWhereItIs(t *testing.T) {
	w, capsule, proposal, note := artifactsFixture(t)

	// A path in a mission capsule, absolute or relative to where you are.
	r := run(t, w, nil, "mission", "artifact", "add", "-m", "cache", proposal, "--kind", "proposal", "Cache proposal")
	if !strings.Contains(r.Stderr, "[proposal, draft] backend/cache-lookup:docs/proposal.md — Cache proposal — read: "+proposal) {
		t.Errorf("add said: %s", r.Stderr)
	}
	t.Setenv(cli.ShellPWD, capsule)
	run(t, w, nil, "mission", "artifact", "add", "-m", "cache", "docs/proposal.md", "--kind", "adr")
	t.Setenv(cli.ShellPWD, "")
	// A file on the default branch, a private note, a PR URL, a link.
	run(t, w, nil, "mission", "artifact", "add", "-m", "cache", "backend:README.md")
	run(t, w, nil, "mission", "artifact", "add", "-m", "cache", note, "--kind", "note")
	run(t, w, nil, "mission", "artifact", "add", "-m", "cache", "https://github.com/test-org/backend/pull/88")
	run(t, w, nil, "mission", "artifact", "add", "-m", "cache", "https://example.com/dash", "Dashboard")

	m := loadMission(t, w, "cache")
	var refs []string
	for _, a := range m.Artifacts {
		refs = append(refs, a.Kind+" "+a.Ref)
	}
	want := "adr backend/cache-lookup:docs/proposal.md|doc backend:README.md|note mission:spike.md|pr backend#88|link https://example.com/dash"
	if got := strings.Join(refs, "|"); got != want {
		t.Errorf("recorded:\n %s\nwant\n %s", got, want)
	}
	if m.Artifacts[0].Label != "Cache proposal" {
		t.Errorf("adding again should keep the label: %+v", m.Artifacts[0])
	}

	show := run(t, w, nil, "mission", "show", "cache", "-f", "llm").Stdout
	for _, line := range []string{
		"[adr, draft] backend/cache-lookup:docs/proposal.md — Cache proposal — read: " + proposal,
		"[doc, landed] backend:README.md — read: " + filepath.Join(w.Root, "repos", "backend", ".ground", "README.md") + " — https://github.com/test-org/backend/blob/main/README.md",
		"[note, private] mission:spike.md — read: " + note,
		"[pr, pr] backend#88 — https://github.com/test-org/backend/pull/88",
		"[link, link] https://example.com/dash — Dashboard",
		"Private files: " + filepath.Join(w.Root, ".ws", "missions", "cache", "files"),
	} {
		if !strings.Contains(show, line) {
			t.Errorf("show -f llm missing %q:\n%s", line, show)
		}
	}

	run(t, w, nil, "mission", "artifact", "rm", "-m", "cache", "backend#88")
	if m := loadMission(t, w, "cache"); len(m.Artifacts) != 4 {
		t.Errorf("after rm: %d artifacts", len(m.Artifacts))
	}
}

func TestArtifacts_RefusalsSayWhatToDo(t *testing.T) {
	w, _, _, _ := artifactsFixture(t)
	outside := filepath.Join(t.TempDir(), "elsewhere.md")
	os.WriteFile(outside, []byte("x"), 0o644)
	elsewhere := filepath.Join(w.Root, "repos", "frontend", "elsewhere", "README.md")
	for _, tc := range []struct{ arg, want string }{
		{outside, "copy it into the mission's files"},
		{elsewhere, "frontend/elsewhere isn't one of mission cache's capsules: attach it with ws mission attach frontend elsewhere"},
		{"frontend/elsewhere:README.md", "isn't one of mission cache's capsules"},
		{"backend:docs/nope.md", "no file"},
		{"mission:../brief.md", "isn't a path inside"},
	} {
		r := testutil.RunCommand(t, w.Root, nil, "mission", "artifact", "add", "-m", "cache", tc.arg)
		if r.Err == nil || !strings.Contains(r.Err.Error(), tc.want) {
			t.Errorf("add %s: err %v, want %q", tc.arg, r.Err, tc.want)
		}
	}
	r := testutil.RunCommand(t, w.Root, nil, "mission", "artifact", "add", "-m", "cache", "https://example.com", "--kind", "Big Doc")
	if r.Err == nil || !strings.Contains(r.Err.Error(), "one lowercase word") {
		t.Errorf("bad kind: %v", r.Err)
	}
	if m := loadMission(t, w, "cache"); len(m.Artifacts) != 0 {
		t.Errorf("refused adds recorded %+v", m.Artifacts)
	}
}

// Artifacts recorded before references (url only) still read, as links.
func TestArtifacts_OldURLEntriesReadAsLinks(t *testing.T) {
	w := missionWorkspace(t)
	run(t, w, nil, "mission", "new", "Old", "--brief", "x")
	path := filepath.Join(w.Root, ".ws", "missions", "old", "mission.toml")
	data, _ := os.ReadFile(path)
	os.WriteFile(path, append(data, []byte("\n[[artifacts]]\nurl = \"https://example.com/design\"\nlabel = \"Design\"\nadded = 2026-11-01T10:00:00Z\n")...), 0o644)
	if show := run(t, w, nil, "mission", "show", "old", "-f", "llm").Stdout; !strings.Contains(show, "[link, link] https://example.com/design — Design") {
		t.Errorf("show:\n%s", show)
	}
}

// The orchestrator sees the artifacts, and a dispatch can name what the next
// crew should read first.
func TestArtifacts_OrchestratorAndDispatchRead(t *testing.T) {
	w, store, _ := dispatchFixture(t)
	capsule := filepath.Join(w.Root, "repos", "backend", "cache-lookup")
	os.MkdirAll(filepath.Join(capsule, "docs"), 0o755)
	os.WriteFile(filepath.Join(capsule, "docs", "proposal.md"), []byte("# P\n"), 0o644)
	t.Setenv(cli.ShellPWD, capsule)
	run(t, w, nil, "mission", "artifact", "add", "docs/proposal.md", "--kind", "proposal", "Cache proposal")
	t.Setenv(cli.ShellPWD, "")

	prompt := run(t, w, nil, "mission", "orchestrate", "--crew", "c1", "--trigger", "done", "--print-prompt").Stdout
	if !strings.Contains(prompt, "## Artifacts") || !strings.Contains(prompt, "[proposal, draft] backend/cache-lookup:docs/proposal.md — Cache proposal") || !strings.Contains(prompt, "--read <artifact>") {
		t.Errorf("prompt:\n%s", prompt)
	}

	run(t, w, nil, "mission", "dispatch", "--read", "backend/cache-lookup:docs/proposal.md", "Implement the proposal")
	m, _ := store.Load("cache")
	h := m.Crew[len(m.Crew)-1].Handoff
	if !strings.HasPrefix(h, "Implement the proposal\n\nRead these first:\n- [proposal, draft] backend/cache-lookup:docs/proposal.md — Cache proposal — read: "+filepath.Join(capsule, "docs", "proposal.md")) {
		t.Errorf("handoff:\n%s", h)
	}
	r := testutil.RunCommand(t, w.Root, nil, "mission", "dispatch", "--read", "backend:nope.md", "x")
	if r.Err == nil || !strings.Contains(r.Err.Error(), "--read backend:nope.md: no file") {
		t.Errorf("bad --read: %v", r.Err)
	}
}
