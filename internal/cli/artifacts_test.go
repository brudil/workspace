package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/brudil/workspace/internal/github"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/workspace"
)

func TestParseArtifactRef_RoundTrips(t *testing.T) {
	for _, ref := range []string{
		"records:adr/0042-cache.md",
		"records/cache-adr:adr/0042-cache.md",
		"mission:notes/spike.md",
		"api#88",
		"https://example.com/design?x=1",
	} {
		if got := parseArtifactRef(ref).String(); got != ref {
			t.Errorf("%s round-trips as %s", ref, got)
		}
	}
}

// Where an artifact is now is worked out from its capsule, the capsule's PR
// and the files on disk; a capsule file follows its PR onto the default
// branch.
func TestResolveArtifact_States(t *testing.T) {
	root := t.TempDir()
	ws := &workspace.Workspace{Root: root, Org: "acme", DefaultBranch: "main"}
	store := mission.Store{Root: root}
	write := func(parts ...string) string {
		p := filepath.Join(append([]string{root}, parts...)...)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
		return p
	}
	draft := write("repos", "records", "draft-cap", "adr", "1.md")
	review := write("repos", "records", "review-cap", "adr", "2.md")
	behind := write("repos", "records", "behind-cap", "adr", "3.md") // merged, .ground not aligned yet
	landedGround := write("repos", "records", ".ground", "adr", "4.md")
	write("repos", "records", "merged-cap", "adr", "4.md")
	burnedGround := write("repos", "records", ".ground", "adr", "5.md")
	private := write(".ws", "missions", "m", "files", "notes.md")

	capsule := func(name string, pr *github.PR, prs mission.PRState) missionCapsuleView {
		return missionCapsuleView{Capsule: mission.Capsule{Repo: "records", Name: name}, Path: filepath.Join(root, "repos", "records", name), Exists: true, PR: pr, PRS: prs}
	}
	openPR := &github.PR{Number: 7, URL: "https://github.com/acme/records/pull/7"}
	v := missionView{M: &mission.Mission{Slug: "m"}, Capsules: []missionCapsuleView{
		capsule("draft-cap", nil, mission.PRNone),
		capsule("review-cap", openPR, mission.PROpen),
		capsule("behind-cap", &github.PR{Number: 8}, mission.PRMerged),
		capsule("merged-cap", &github.PR{Number: 9}, mission.PRMerged),
		{Capsule: mission.Capsule{Repo: "records", Name: "gone-cap"}},
		{Capsule: mission.Capsule{Repo: "api", Name: "fix"}, Exists: true, Path: filepath.Join(root, "repos", "api", "fix"), PR: &github.PR{Number: 88}, PRS: mission.PROpen},
	}}
	for _, tc := range []struct {
		ref, kind, state, path, url string
	}{
		{"records/draft-cap:adr/1.md", "adr", "draft", draft, ""},
		{"records/review-cap:adr/2.md", "adr", "in review", review, "https://github.com/acme/records/pull/7"},
		{"records/behind-cap:adr/3.md", "adr", "landed", behind, "https://github.com/acme/records/blob/main/adr/3.md"},
		{"records/merged-cap:adr/4.md", "adr", "landed", landedGround, "https://github.com/acme/records/blob/main/adr/4.md"},
		{"records/gone-cap:adr/5.md", "adr", "landed", burnedGround, "https://github.com/acme/records/blob/main/adr/5.md"},
		{"records/gone-cap:adr/6.md", "adr", "missing", "", ""},
		{"records/draft-cap:adr/nope.md", "adr", "missing", "", ""},
		{"records:adr/4.md", "", "landed", landedGround, "https://github.com/acme/records/blob/main/adr/4.md"},
		{"mission:notes.md", "note", "private", private, ""},
		{"mission:gone.md", "note", "missing", filepath.Join(root, ".ws", "missions", "m", "files", "gone.md"), ""},
		{"api#88", "", "open", "", "https://github.com/acme/api/pull/88"},
		{"api#12", "issue", "pr", "", "https://github.com/acme/api/issues/12"},
		{"https://example.com/d", "", "link", "", "https://example.com/d"},
	} {
		av := resolveArtifact(ws, store, v, mission.Artifact{Ref: tc.ref, Kind: tc.kind})
		if av.State != tc.state || av.Path != tc.path || av.URL != tc.url {
			t.Errorf("%s: state %q path %q url %q, want %q %q %q", tc.ref, av.State, av.Path, av.URL, tc.state, tc.path, tc.url)
		}
		if tc.kind == "" && av.Kind != defaultKind(parseArtifactRef(tc.ref)) {
			t.Errorf("%s: default kind %q", tc.ref, av.Kind)
		}
	}
}
