package mission

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Stages. A mission's plan is its brief's "## Stages": a numbered list,
// written by the user or a briefing agent, each item one stage for
// separate crew to do in turn. The brief stays the user's; ws keeps the
// progress (StageRecord) in mission.toml: which crew worked on a stage,
// when it started and when it was accepted. A brief without stages is one
// stage, the whole brief.

// Stage is one stage of the plan, as the brief lists it.
type Stage struct {
	N    int    // its position, from 1
	Text string // the item as written, without its number
	// CheckIn: the item says to check in with the user before the next
	// stage starts.
	CheckIn bool
	// Implicit: the brief lists no stages, so this is the whole brief.
	Implicit bool
}

// StageRecord is ws's record of a stage's progress. It's matched to its
// stage by text, or by position while the number of stages is unchanged
// (see stageRecordsFor), so rewording a stage keeps its progress.
type StageRecord struct {
	Text string `toml:"text"`
	// N and Of are the stage's position and the number of stages when it
	// was last matched.
	N          int        `toml:"n"`
	Of         int        `toml:"of"`
	Crew       []string   `toml:"crew,omitempty"`
	Started    *time.Time `toml:"started,omitempty"`
	Accepted   *time.Time `toml:"accepted,omitempty"`
	AcceptedBy string     `toml:"accepted_by,omitempty"`
	Why        string     `toml:"why,omitempty"` // why it was accepted
}

// StageState is a stage with its progress.
type StageState struct {
	Stage
	Record  *StageRecord // nil until crew start on it or it's accepted
	Current bool         // the first stage not yet accepted
}

// Accepted reports whether the stage has been accepted.
func (s StageState) Accepted() bool { return s.Record != nil && s.Record.Accepted != nil }

var (
	stagesHeading = regexp.MustCompile(`(?i)^(#{1,6})\s+stages\s*:?\s*#*\s*$`)
	anyHeading    = regexp.MustCompile(`^(#{1,6})\s`)
	stageItem     = regexp.MustCompile(`^(?:\d+[.)]|[-*+])\s+(.*)$`)
	checkIn       = regexp.MustCompile(`(?i)\bcheck[\s-]?in\b`)
)

// ParseStages reads the brief's stages: the list under a "Stages"
// heading, numbered or bulleted, with each item's indented lines joined
// to it. It always returns at least one: without a list, the whole brief
// is one implicit stage.
func ParseStages(brief string) []Stage {
	var stages []Stage
	in, level := false, 0
	for _, line := range strings.Split(strings.ReplaceAll(brief, "\r\n", "\n"), "\n") {
		if g := stagesHeading.FindStringSubmatch(line); g != nil {
			in, level = true, len(g[1])
			continue
		}
		if !in {
			continue
		}
		if g := anyHeading.FindStringSubmatch(line); g != nil && len(g[1]) <= level {
			break
		}
		if g := stageItem.FindStringSubmatch(line); g != nil && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			stages = append(stages, Stage{N: len(stages) + 1, Text: strings.TrimSpace(g[1])})
			continue
		}
		if t := strings.TrimSpace(line); t != "" && len(stages) > 0 {
			st := &stages[len(stages)-1]
			st.Text += "\n" + t
		}
	}
	for i := range stages {
		stages[i].CheckIn = checkIn.MatchString(stages[i].Text)
	}
	if len(stages) == 0 {
		return []Stage{{N: 1, Implicit: true}}
	}
	return stages
}

// Title is the stage's first line, for lists.
func (s Stage) Title() string {
	if s.Implicit {
		return "the whole brief"
	}
	first, _, _ := strings.Cut(s.Text, "\n")
	return first
}

var capsuleToken = regexp.MustCompile("`([A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9._-]+)`")

// NamedCapsules is every repo/name the stage names in backticks, the way
// briefs name capsules.
func (s Stage) NamedCapsules() []string {
	var out []string
	for _, g := range capsuleToken.FindAllStringSubmatch(s.Text, -1) {
		out = append(out, g[1])
	}
	return out
}

// CapsuleIn is the first of caps the stage names, as repo/name, or "".
func (s Stage) CapsuleIn(caps []Capsule) string {
	for _, c := range caps {
		name := c.Repo + "/" + c.Name
		for _, ok := range []string{"`" + name + "`", " " + name + " ", " " + name + ".", " " + name + ","} {
			if strings.Contains(" "+s.Text+" ", ok) {
				return name
			}
		}
	}
	return ""
}

// normStage is a stage's text as matching compares it: case, spacing,
// markdown emphasis and a closing full stop don't count.
func normStage(s string) string {
	s = strings.NewReplacer("*", "", "_", "", "`", "").Replace(strings.ToLower(s))
	return strings.TrimRight(strings.Join(strings.Fields(s), " "), ".")
}

// stageRecordsFor matches records to stages: by text first, then any
// record still unmatched to the stage at its position, if the number of
// stages is what it was. It returns each stage's record index (-1 for
// none) and the records that match no stage.
func stageRecordsFor(stages []Stage, recs []StageRecord) ([]int, []int) {
	byStage := make([]int, len(stages))
	used := make([]bool, len(recs))
	for i := range byStage {
		byStage[i] = -1
	}
	for i, st := range stages {
		for j, r := range recs {
			if !used[j] && normStage(r.Text) == normStage(st.Text) {
				byStage[i], used[j] = j, true
				break
			}
		}
	}
	for j, r := range recs {
		if i := r.N - 1; !used[j] && r.Of == len(stages) && i >= 0 && i < len(stages) && byStage[i] < 0 {
			byStage[i], used[j] = j, true
		}
	}
	var orphans []int
	for j := range recs {
		if !used[j] {
			orphans = append(orphans, j)
		}
	}
	return byStage, orphans
}

// StageProgress is the brief's stages with their progress, and the
// records that no longer match a stage (it was removed or rewritten).
func (m *Mission) StageProgress(brief string) ([]StageState, []StageRecord) {
	stages := ParseStages(brief)
	byStage, orphanIdx := stageRecordsFor(stages, m.Stages)
	out := make([]StageState, len(stages))
	current := false
	for i, st := range stages {
		out[i].Stage = st
		if j := byStage[i]; j >= 0 {
			r := m.Stages[j]
			out[i].Record = &r
		}
		if !current && !out[i].Accepted() {
			out[i].Current, current = true, true
		}
	}
	var orphans []StageRecord
	for _, j := range orphanIdx {
		orphans = append(orphans, m.Stages[j])
	}
	return out, orphans
}

// CurrentStage is the first stage not yet accepted, if any.
func CurrentStage(states []StageState) (StageState, bool) {
	for _, s := range states {
		if s.Current {
			return s, true
		}
	}
	return StageState{}, false
}

// StageFor is the stage crew is for, from its record, if it joined one.
func StageFor(states []StageState, crewID string) (StageState, bool) {
	for _, s := range states {
		if s.Record != nil && contains(s.Record.Crew, crewID) {
			return s, true
		}
	}
	return StageState{}, false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// recordFor finds or adds the record for stage st in m (matched as
// StageProgress does), keeping its text and position current.
func recordFor(m *Mission, stages []Stage, st Stage) *StageRecord {
	byStage, _ := stageRecordsFor(stages, m.Stages)
	j := byStage[st.N-1]
	if j < 0 {
		m.Stages = append(m.Stages, StageRecord{})
		j = len(m.Stages) - 1
	}
	r := &m.Stages[j]
	r.Text, r.N, r.Of = st.Text, st.N, len(stages)
	return r
}

// JoinStage records crew as working on stage n of the brief's stages (0
// for the current one), and returns it.
func (s Store) JoinStage(slug, crewID string, n int) (Stage, error) {
	brief, _ := s.ReadBrief(slug)
	var joined Stage
	if _, err := s.Update(slug, func(m *Mission) error {
		states, _ := m.StageProgress(brief)
		st, err := PickStage(states, n)
		if err != nil {
			return err
		}
		joined = st.Stage
		r := recordFor(m, ParseStages(brief), st.Stage)
		if !contains(r.Crew, crewID) {
			r.Crew = append(r.Crew, crewID)
		}
		if r.Started == nil {
			t := now()
			r.Started = &t
		}
		return nil
	}); err != nil {
		return Stage{}, err
	}
	return joined, s.AppendLog(slug, Event{Kind: EventStage, Crew: crewID, Text: fmt.Sprintf("on stage %d: %s", joined.N, joined.Title())})
}

// PickStage is stage n (from 1), or for 0 the current stage, or the last
// when every stage is accepted.
func PickStage(states []StageState, n int) (StageState, error) {
	if n < 0 || n > len(states) {
		return StageState{}, fmt.Errorf("there's no stage %d: the brief lists %d", n, len(states))
	}
	if n > 0 {
		return states[n-1], nil
	}
	if st, ok := CurrentStage(states); ok {
		return st, nil
	}
	return states[len(states)-1], nil
}

// ErrStageBusy is refusing to accept a stage its crew are still on.
var ErrStageBusy = errors.New("the stage's crew are still on it")

// ErrAllAccepted is accept with no stage left to accept.
var ErrAllAccepted = errors.New("every stage is accepted")

// AcceptStage accepts the current stage: its work is complete and good
// enough. It's refused while any of the stage's crew are still working
// (running, not reported done) or asking the user something. It returns
// the stage accepted and the stages as they are now.
func (s Store) AcceptStage(slug, by, why string) (StageState, []StageState, error) {
	brief, _ := s.ReadBrief(slug)
	var accepted StageState
	m, err := s.Update(slug, func(m *Mission) error {
		states, _ := m.StageProgress(brief)
		cur, ok := CurrentStage(states)
		if !ok {
			return ErrAllAccepted
		}
		if cur.Record != nil {
			for _, id := range cur.Record.Crew {
				for _, c := range m.Crew {
					switch {
					case c.ID != id || c.Role != "":
					case c.Status == CrewAsking:
						return fmt.Errorf("%w: %s is asking the user something", ErrStageBusy, id)
					case c.Ended == nil && c.Status != CrewDone:
						return fmt.Errorf("%w: %s is still working on it (it reports done when it's finished)", ErrStageBusy, id)
					}
				}
			}
		}
		r := recordFor(m, ParseStages(brief), cur.Stage)
		t := now()
		r.Accepted, r.AcceptedBy, r.Why = &t, by, strings.TrimSpace(why)
		accepted = cur
		accepted.Record = r
		return nil
	})
	if err != nil {
		return StageState{}, nil, err
	}
	text := fmt.Sprintf("stage %d accepted: %s", accepted.N, accepted.Title())
	if why != "" {
		text += " · " + strings.TrimSpace(why)
	}
	if err := s.AppendLog(slug, Event{Kind: EventStage, Crew: by, Text: text}); err != nil {
		return StageState{}, nil, err
	}
	states, _ := m.StageProgress(brief)
	return accepted, states, nil
}
