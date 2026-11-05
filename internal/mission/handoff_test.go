package mission

import (
	"os"
	"strings"
	"testing"
	"time"
)

const goodHandoff = `Some preamble the agent wrote.

## Done
- Cache in api/internal/tokens/cache.go.

## Learned
- Tokens are looked up twice per request.

### Left
- Eviction on revocation.
- Docs.

## Read first
- ` + "`records/cache-adr:adr/0042.md`" + ` — the ADR
- https://example.com/x
* api#88 - the PR
`

func TestParseHandoff(t *testing.T) {
	h, err := ParseHandoff(goodHandoff)
	if err != nil {
		t.Fatal(err)
	}
	if h.Done != "- Cache in api/internal/tokens/cache.go." || !strings.Contains(h.Left, "Docs.") {
		t.Errorf("sections: %+v", h)
	}
	want := []ReadItem{{"records/cache-adr:adr/0042.md", "the ADR"}, {"https://example.com/x", ""}, {"api#88", "the PR"}}
	if len(h.ReadFirst) != len(want) {
		t.Fatalf("read first = %+v", h.ReadFirst)
	}
	for i := range want {
		if h.ReadFirst[i] != want[i] {
			t.Errorf("read first %d = %+v, want %+v", i, h.ReadFirst[i], want[i])
		}
	}
	if h.LeftLine() != "Eviction on revocation." {
		t.Errorf("left line = %q", h.LeftLine())
	}
}

func TestParseHandoff_NothingAndRefusals(t *testing.T) {
	h, err := ParseHandoff("# Done\nx\n# Learned\ny\n# Left\nNothing.\n# Read first\nNone\n## Notes\nextra")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.ReadFirst) != 0 || h.LeftLine() != "" || !strings.Contains(h.Other, "extra") {
		t.Errorf("handoff = %+v", h)
	}
	for text, want := range map[string]string{
		"## Done\nx\n## Learned\ny":                                                `no "Left", "Read first" section`,
		"## Done\nx\n## Learned\n\n## Left\nz\n## Read first\nNone":                `"Learned" says nothing`,
		"## Done\nx\n## Learned\ny\n## Left\nz\n## Read first\nthe ADR in records": `"Read first" lists what to read`,
		"just a summary": `no "Done", "Learned", "Left", "Read first" section`,
	} {
		if _, err := ParseHandoff(text); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", text, err, want)
		}
	}
}

func TestReportDone_KeepsTheHandoff(t *testing.T) {
	s := Store{Root: t.TempDir()}
	m, _ := s.Create("Cache")
	c, _ := s.StartCrew(m.Slug, "claude", "conv")
	s.SetCrewProcess(m.Slug, c.ID, 1, "api/cache")
	h, _ := ParseHandoff(goodHandoff)

	if err := s.ReportDone(m.Slug, c.ID, "Added the cache", h); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.HandoffPath(m.Slug, c.ID))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Added the cache", "crew c1, in api/cache", "## Learned\n\n- Tokens", "- records/cache-adr:adr/0042.md — the ADR"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("file missing %q:\n%s", want, data)
		}
	}
	got, _ := s.Load(m.Slug)
	if cr := got.Crew[0]; cr.Status != CrewDone || cr.Note != "Added the cache" || cr.HandedOff == nil {
		t.Errorf("crew = %+v", cr)
	}
	if len(got.Artifacts) != 1 || got.Artifacts[0] != (Artifact{Ref: "mission:handoffs/c1.md", Kind: "handoff", Label: "Added the cache", Crew: "c1", Added: got.Artifacts[0].Added}) {
		t.Errorf("artifacts = %+v", got.Artifacts)
	}
	events, _ := s.ReadLog(m.Slug, 0)
	if last := events[len(events)-1]; last.Kind != EventCrewDone || last.Text != "Added the cache · left: Eviction on revocation." {
		t.Errorf("log = %+v", last)
	}

	// Done again: the file and artifact are replaced, not added to.
	if err := s.ReportDone(m.Slug, c.ID, "Added eviction too", h); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Load(m.Slug)
	if len(got.Artifacts) != 1 || got.Artifacts[0].Label != "Added eviction too" {
		t.Errorf("artifacts after a second done = %+v", got.Artifacts)
	}
}

func TestLatestHandoffs_OnePerCapsuleNewestFirst(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	at := func(min int) *time.Time { t := t0.Add(time.Duration(min) * time.Minute); return &t }
	m := &Mission{Crew: []Crew{
		{ID: "c1", Capsule: "records/adr", HandedOff: at(1)},
		{ID: "c2", Capsule: "api/cache", HandedOff: at(5)},
		{ID: "c3", Capsule: "records/adr", HandedOff: at(9)},
		{ID: "c4", Capsule: "api/cache"}, // none left
	}}
	var ids []string
	for _, c := range m.LatestHandoffs() {
		ids = append(ids, c.ID)
	}
	if strings.Join(ids, ",") != "c3,c2" {
		t.Errorf("latest = %v, want c3,c2", ids)
	}
}

func TestLoad_ReadsTheOldDispatchKey(t *testing.T) {
	s := Store{Root: t.TempDir()}
	m, _ := s.Create("Old")
	path := s.missionDir(m.Slug) + "/" + missionFile
	data, _ := os.ReadFile(path)
	data = append(data, []byte("\n[[crew]]\nid = \"c1\"\nhandoff = \"write the ADR\"\n")...)
	os.WriteFile(path, data, 0o644)
	got, err := s.Load(m.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if c := got.Crew[0]; c.Dispatch != "write the ADR" || c.OldDispatch != "" {
		t.Errorf("crew = %+v", c)
	}
}
