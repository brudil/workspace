package cli

import (
	"strings"
	"testing"

	"github.com/brudil/workspace/internal/mission"
	"github.com/charmbracelet/x/ansi"
)

// What crew report through their hooks beats guessing from the screen:
// waiting is waiting even while the screen moves, permission needs you,
// and the deck shows what the crew last said.
func TestDeckHooks_ReportedActivityWins(t *testing.T) {
	m, slug := shellCrewDeck(t)
	m = launch(t, m)
	r, _ := liveRun(m, slug)
	m = typeText(m, "echo moving") // the screen just changed: a guess says working

	m.store.CrewHook(slug, "c1", mission.Hook{Prompted: true})
	m.store.CrewHook(slug, "c1", mission.Hook{Activity: mission.ActivityWaiting, Reply: "## Done\nStage two is in: hello.py prints hello."})
	m, _ = reloadRecords(m)
	if working, _ := r.activity(); working {
		t.Error("reported waiting, but the deck says working")
	}
	if b := r.activityBadge(); !strings.Contains(b, "waiting") {
		t.Errorf("badge = %q", b)
	}
	if line := ansi.Strip(m.crewSummary(m.data.Missions[m.rows[m.cursor].mission])); !strings.Contains(line, "said: Done") {
		t.Errorf("crew line doesn't show what it said: %q", line)
	}

	m.store.CrewHook(slug, "c1", mission.Hook{Activity: mission.ActivityPermission, Notice: "Claude needs your permission to use Bash"})
	m, _ = reloadRecords(m)
	m = selectMission(t, m, slug)
	st := m.missionStatus(&m.data.Missions[m.rows[m.cursor].mission])
	if !strings.Contains(st.label, "? permission") || len(st.why) == 0 || !strings.Contains(st.why[0], "c1 needs your permission: Claude needs your permission to use Bash") {
		t.Errorf("status = %+v", st)
	}

	m.store.CrewHook(slug, "c1", mission.Hook{Prompted: true}) // you answered it
	m, _ = reloadRecords(m)
	m = typeText(m, "echo again")
	if working, _ := r.activity(); !working {
		t.Error("reported working with a moving screen, but the deck says waiting")
	}
}
