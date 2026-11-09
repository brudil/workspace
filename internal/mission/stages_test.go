package mission

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const stagedBrief = `## Goal
Cache token lookups.

## Stages
1. Write a proposal in ` + "`records/cache-adr`" + `. Check in with me.
2. Implement it in ` + "`api/cache-lookup`" + `,
   following the proposal.
3. Update the docs.

## Acceptance
- Tests pass.
`

func TestParseStages(t *testing.T) {
	st := ParseStages(stagedBrief)
	if len(st) != 3 {
		t.Fatalf("stages = %+v", st)
	}
	if !st[0].CheckIn || st[1].CheckIn || st[2].CheckIn {
		t.Errorf("check-ins: %v %v %v", st[0].CheckIn, st[1].CheckIn, st[2].CheckIn)
	}
	if st[1].Text != "Implement it in `api/cache-lookup`,\nfollowing the proposal." || st[1].Title() != "Implement it in `api/cache-lookup`," {
		t.Errorf("stage 2 = %q", st[1].Text)
	}
	if st[2].Text != "Update the docs." {
		t.Errorf("stage 3 ran into the next section: %q", st[2].Text)
	}
	if got := st[0].NamedCapsules(); len(got) != 1 || got[0] != "records/cache-adr" {
		t.Errorf("named = %v", got)
	}
	caps := []Capsule{{Repo: "api", Name: "cache-lookup"}, {Repo: "records", Name: "cache-adr"}}
	if st[1].CapsuleIn(caps) != "api/cache-lookup" || st[2].CapsuleIn(caps) != "" {
		t.Errorf("capsules: %q %q", st[1].CapsuleIn(caps), st[2].CapsuleIn(caps))
	}

	// Bullets work too; no list is one implicit stage.
	if st := ParseStages("### Stages\n- one\n- two\n"); len(st) != 2 || st[1].Text != "two" {
		t.Errorf("bulleted = %+v", st)
	}
	if st := ParseStages("## Goal\nJust do it."); len(st) != 1 || !st[0].Implicit || st[0].Title() != "the whole brief" {
		t.Errorf("no stages = %+v", st)
	}
}

func TestStageProgress_MatchesByTextThenPosition(t *testing.T) {
	at := time.Now()
	m := &Mission{Stages: []StageRecord{
		// Reformatted since: still stage 1.
		{Text: "Write a **proposal** in records/cache-adr. check in   with me", N: 1, Of: 3, Accepted: &at},
		// Reworded, same position and count: stage 2.
		{Text: "Build it in api", N: 2, Of: 3, Crew: []string{"c2"}},
		// Matches nothing.
		{Text: "Something removed", N: 4, Of: 4},
	}}
	states, orphans := m.StageProgress(stagedBrief)
	if !states[0].Accepted() || states[1].Record == nil || states[1].Record.Crew[0] != "c2" || states[2].Record != nil {
		t.Errorf("matching: %+v", states)
	}
	if !states[1].Current || states[0].Current || states[2].Current {
		t.Errorf("current isn't stage 2: %+v", states)
	}
	if len(orphans) != 1 || orphans[0].Text != "Something removed" {
		t.Errorf("orphans = %+v", orphans)
	}
	if st, ok := StageFor(states, "c2"); !ok || st.N != 2 {
		t.Errorf("c2's stage = %+v", st)
	}

	// With a stage added, position no longer counts.
	more, _ := m.StageProgress(strings.Replace(stagedBrief, "3. Update the docs.", "3. Update the docs.\n4. Announce it.", 1))
	if more[1].Record != nil {
		t.Errorf("matched by position with a stage added: %+v", more[1])
	}
}

func TestJoinAndAcceptStages(t *testing.T) {
	s := Store{Root: t.TempDir()}
	m, _ := s.Create("Cache")
	s.WriteBrief(m.Slug, stagedBrief)
	c1, _ := s.StartCrew(m.Slug, "claude", "")

	st, err := s.JoinStage(m.Slug, c1.ID, 0)
	if err != nil || st.N != 1 {
		t.Fatalf("joined %+v, %v", st, err)
	}
	if _, _, err := s.AcceptStage(m.Slug, "orchestrator", "looks good"); !errors.Is(err, ErrStageBusy) {
		t.Errorf("accepted while c1 works: %v", err)
	}
	s.SetCrewStatus(m.Slug, c1.ID, CrewAsking, "which cache?")
	if _, _, err := s.AcceptStage(m.Slug, "orchestrator", ""); err == nil || !strings.Contains(err.Error(), "asking") {
		t.Errorf("accepted while c1 asks: %v", err)
	}
	s.SetCrewStatus(m.Slug, c1.ID, CrewDone, "proposal written")
	acc, states, err := s.AcceptStage(m.Slug, "orchestrator", "the proposal covers it")
	if err != nil || acc.N != 1 || acc.Record.AcceptedBy != "orchestrator" {
		t.Fatalf("accept: %+v, %v", acc, err)
	}
	if cur, _ := CurrentStage(states); cur.N != 2 {
		t.Errorf("current after accepting 1 = %d", cur.N)
	}

	// New crew join the current stage; one dispatched for a stage joins that.
	c2, _ := s.StartCrew(m.Slug, "claude", "")
	if st, _ := s.JoinStage(m.Slug, c2.ID, 0); st.N != 2 {
		t.Errorf("c2 joined stage %d", st.N)
	}
	c3, _ := s.StartCrew(m.Slug, "claude", "")
	if _, err := s.JoinStage(m.Slug, c3.ID, 4); err == nil {
		t.Error("joined a stage that isn't there")
	}
	s.JoinStage(m.Slug, c3.ID, 3)
	got, _ := s.Load(m.Slug)
	states, _ = got.StageProgress(stagedBrief)
	if st, _ := StageFor(states, "c3"); st.N != 3 || states[2].Record.Started == nil {
		t.Errorf("c3: %+v", states[2])
	}
	events, _ := s.ReadLog(m.Slug, 0)
	var stageEvents []string
	for _, e := range events {
		if e.Kind == EventStage {
			stageEvents = append(stageEvents, e.Text)
		}
	}
	if len(stageEvents) != 4 || stageEvents[1] != "stage 1 accepted: Write a proposal in `records/cache-adr`. Check in with me. · the proposal covers it" {
		t.Errorf("log: %q", stageEvents)
	}
}

func TestAcceptStage_WholeBriefAndAllDone(t *testing.T) {
	s := Store{Root: t.TempDir()}
	m, _ := s.Create("Small")
	s.WriteBrief(m.Slug, "## Goal\nFix the typo.")
	if acc, _, err := s.AcceptStage(m.Slug, "you", "fixed"); err != nil || !acc.Implicit {
		t.Fatalf("accept the whole brief: %+v, %v", acc, err)
	}
	if _, _, err := s.AcceptStage(m.Slug, "you", ""); !errors.Is(err, ErrAllAccepted) {
		t.Errorf("accepted twice: %v", err)
	}
}

// A stage's short form is for lists: briefs' stages are often a paragraph
// of markdown with a bold lead.
func TestStageShort(t *testing.T) {
	for text, want := range map[string]string{
		"**Proposal (records capsule `hello-world-proposal`).** Write `proposals/hello-world-python.md` with: a title, the problem.": "Proposal",
		"**Implementation (demo capsule `hello-world-python`).** Add `hello.py` at the repo root.":                                   "Implementation",
		"Propose the greeting table in `demo/hello`. Check in with me.":                                                              "Propose the greeting table in demo/hello",
		"Implement it.": "Implement it",
		"__Spike__: see [the notes](https://example.com/n) first":                                "Spike",
		"Write up v1.2 of the [ADR](x) in `records/adr`":                                         "Write up v1.2 of the ADR in records/adr",
		"Port every handler in the api to the new router, then delete the old one and its tests": "Port every handler in the api to the new router, then…",
		"Ship it (with flags)": "Ship it",
	} {
		if got := (Stage{N: 1, Text: text}).Short(); got != want {
			t.Errorf("Short(%q) = %q, want %q", text, got, want)
		}
	}
	if got := (Stage{N: 1, Text: "**Proposal.** Write `x.md`:\nthe [plan](u)."}).Plain(); got != "Proposal. Write x.md: the plan." {
		t.Errorf("Plain = %q", got)
	}
	if got := (Stage{N: 1, Implicit: true}).Short(); got != "the whole brief" {
		t.Errorf("implicit = %q", got)
	}
}
