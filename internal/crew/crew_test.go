package crew

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
)

func TestPickPlace(t *testing.T) {
	caps := []Capsule{{Name: "fe/a", Path: "/a"}, {Name: "fe/b", Path: "/b"}}
	cases := []struct {
		busy      map[string]string
		want      string
		capsule   string
		busyCrew  string
		errIs     error
		errString string
	}{
		{busy: nil, capsule: "fe/a"},
		{busy: map[string]string{"fe/a": "c1"}, capsule: "fe/b"},
		{busy: map[string]string{"fe/a": "c1", "fe/b": "c2"}, capsule: "fe/a", busyCrew: "c1"},
		{busy: map[string]string{"fe/b": "c2"}, want: "fe/b", capsule: "fe/b", busyCrew: "c2"},
		{want: "be/x", errString: "no capsule be/x (it has: fe/a, fe/b)"},
	}
	for i, c := range cases {
		p, err := PickPlace(caps, c.busy, c.want)
		switch {
		case c.errString != "":
			var u *UnknownCapsuleError
			if !errors.As(err, &u) || err.Error() != c.errString {
				t.Errorf("%d: err = %v", i, err)
			}
		case err != nil:
			t.Errorf("%d: err = %v", i, err)
		case p.Capsule != c.capsule || p.Busy != c.busyCrew:
			t.Errorf("%d: place = %+v", i, p)
		}
	}
	if _, err := PickPlace(nil, nil, ""); !errors.Is(err, ErrNoCapsule) {
		t.Errorf("no capsules: %v", err)
	}
}

func TestBusyIn_IgnoresDeadProcesses(t *testing.T) {
	now := time.Now()
	m := &mission.Mission{Crew: []mission.Crew{
		{ID: "c1", Capsule: "fe/a", PID: os.Getpid()}, // running
		{ID: "c2", Capsule: "fe/b", PID: 999999},      // its process is gone
		{ID: "c3", Capsule: "fe/c", Ended: &now},      // ended
		{ID: "c4", Capsule: "fe/d"},                   // starting
	}}
	busy := BusyIn(m)
	if busy["fe/a"] != "c1" || busy["fe/b"] != "" || busy["fe/c"] != "" || busy["fe/d"] != "c4" {
		t.Errorf("busy = %v", busy)
	}
}

// Crew that handed back (reported done, or finished briefing) are idle:
// they don't make their capsule busy, but they're listed so whoever starts
// new crew there can end them first.
func TestBusyIn_HandedBackCrewAreIdle(t *testing.T) {
	m := &mission.Mission{State: mission.Active, Crew: []mission.Crew{
		{ID: "c1", Capsule: "fe/a", PID: os.Getpid(), Status: mission.CrewDone},   // reported done
		{ID: "c2", Capsule: "fe/a", PID: os.Getpid(), Status: mission.CrewAsking}, // asking: still on it
		{ID: "c3", Capsule: "fe/b", PID: os.Getpid(), Role: mission.RoleBriefing}, // briefed, mission ready
		{ID: "c4", Capsule: "fe/c", PID: 999999, Status: mission.CrewDone},        // done, process gone
		{ID: "c5", Capsule: "fe/d", PID: os.Getpid()},                             // working
	}}
	busy := BusyIn(m)
	if busy["fe/a"] != "c2" || busy["fe/b"] != "" || busy["fe/c"] != "" || busy["fe/d"] != "c5" {
		t.Errorf("busy = %v", busy)
	}
	idle := HandedBackIn(m)
	if strings.Join(idle["fe/a"], ",") != "c1" || strings.Join(idle["fe/b"], ",") != "c3" || len(idle["fe/c"]) != 0 || len(idle["fe/d"]) != 0 {
		t.Errorf("handed back = %v", idle)
	}
	// While the mission is still being briefed, the briefing crew is busy.
	m.State = mission.Briefing
	if BusyIn(m)["fe/b"] != "c3" {
		t.Errorf("briefing crew should be busy while briefing: %v", BusyIn(m))
	}
}

type ended struct {
	exit    string
	stopped bool
}

func localFixture(t *testing.T) (*LocalHost, mission.Store, string, chan ended) {
	t.Helper()
	store := mission.Store{Root: t.TempDir()}
	m, _ := store.Create("Local")
	ends := make(chan ended, 4)
	h := &LocalHost{Store: store, OnEnded: func(s Spec, exit string, stopped bool) { ends <- ended{exit, stopped} }}
	t.Cleanup(h.Leave)
	return h, store, m.Slug, ends
}

func shellLaunch(script string) func() (harness.Launch, error) {
	return func() (harness.Launch, error) {
		return harness.Launch{Args: []string{"/bin/sh", "-c", script}, Prompt: "hello"}, nil
	}
}

func TestStart_RecordsAndTheHostRecordsTheEnd(t *testing.T) {
	h, store, slug, ends := localFixture(t)
	st, err := Start(Request{Store: store, Host: h, Root: store.Root, Slug: slug, Harness: "shell",
		Place: Place{Dir: store.Root, Capsule: "fe/a"}, Dispatch: "do stage two", By: "orchestrator",
		Width: 40, Height: 5, Prepare: shellLaunch(`printf 'out-%s\n' "$WS_CREW"; exit 3`)})
	if err != nil {
		t.Fatal(err)
	}
	if st.Crew.ID != "c1" || st.Prompt != "hello" {
		t.Errorf("started = %+v", st)
	}
	<-st.Term.Done()
	// Done means recorded: the end, the output and the hook are all there.
	m, _ := store.Load(slug)
	c := m.Crew[0]
	if c.Ended == nil || c.Exit != "exit 3" || c.Capsule != "fe/a" || c.Dispatch != "do stage two" {
		t.Errorf("crew = %+v", c)
	}
	if data, _ := os.ReadFile(store.TranscriptPath(slug, "c1")); !strings.Contains(string(data), "out-c1") {
		t.Errorf("transcript = %q", data)
	}
	select {
	case e := <-ends:
		if e.exit != "exit 3" || e.stopped {
			t.Errorf("hook got %+v", e)
		}
	default:
		t.Error("hook didn't run before Done")
	}
}

func TestLocalHost_StoppedAndLeaving(t *testing.T) {
	h, store, slug, ends := localFixture(t)
	req := Request{Store: store, Host: h, Root: store.Root, Slug: slug, Harness: "shell", Width: 40, Height: 5, Prepare: shellLaunch(`sleep 30`)}
	a, _ := Start(req)
	b, _ := Start(req)

	a.Term.Close(time.Second)
	<-a.Term.Done()
	if e := <-ends; !e.stopped || e.exit != "hangup" {
		t.Errorf("stopped crew: %+v", e)
	}
	h.Leave() // stops b and waits for its end to be recorded
	if e := <-ends; !e.stopped || e.exit != "stopped: hangup" {
		t.Errorf("left crew: %+v", e)
	}
	if m, _ := store.Load(slug); m.Crew[1].Exit != "stopped: hangup" || !b.Term.Exited() {
		t.Errorf("b = %+v", m.Crew[1])
	}
}

type failingHost struct{ LocalHost }

func (*failingHost) Launch(Spec) (Term, error) { return nil, errors.New("no server") }

func TestStart_FailedLaunchIsRecorded(t *testing.T) {
	store := mission.Store{Root: t.TempDir()}
	m, _ := store.Create("Fails")
	_, err := Start(Request{Store: store, Host: &failingHost{}, Slug: m.Slug, Harness: "shell", Prepare: shellLaunch("true")})
	if err == nil {
		t.Fatal("no error")
	}
	got, _ := store.Load(m.Slug)
	if len(got.Crew) != 1 || got.Crew[0].Exit != "failed to start: no server" {
		t.Errorf("crew = %+v", got.Crew)
	}
}

func TestStart_LogsLaunchPromptAndEnd(t *testing.T) {
	h, store, slug, _ := localFixture(t)
	os.MkdirAll(store.Root+"/.ws", 0o755)
	st, err := Start(Request{Store: store, Host: h, Root: store.Root, Slug: slug, Harness: "shell",
		Width: 40, Height: 5, Prepare: func() (harness.Launch, error) {
			return harness.Launch{Args: []string{"/bin/sh", "-c", `printf 'ready> '; read x; echo "got $x"; exit 4`}, Dir: store.Root, Notes: []string{"ran agent create-chat: chat abc"}}, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	if !TypePrompt(store.Root, slug+"/c1", st.Term, "hello there") {
		t.Fatal("prompt not typed")
	}
	<-st.Term.Done()
	data, _ := os.ReadFile(store.Root + "/.ws/exec.log")
	log := string(data)
	for _, want := range []string{
		"[crew " + slug + "/c1] ran agent create-chat: chat abc",
		"[crew " + slug + "/c1] start pid ",
		"$ /bin/sh -c 'printf '\\''ready> '\\''; read x; echo \"got $x\"; exit 4'",
		"WS_MISSION=" + slug + " WS_CREW=c1",
		"[crew " + slug + "/c1] typed in its prompt (11 chars):\n    hello there",
		"[crew " + slug + "/c1] end: exit 4",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("exec log missing %q:\n%s", want, log)
		}
	}
}

// Crew of a harness with hooks get them installed: the agent reports its
// activity to this ws, told the workspace root.
func TestHookArgs_InstalledForHarnessesWithHooks(t *testing.T) {
	root := t.TempDir()
	args, note := hookArgs(root, "claude")
	if len(args) != 2 || args[0] != "--settings" || !strings.Contains(args[1], filepath.Join(root, ".ws", "bin", "ws")+" mission hook --root "+root+" stop") {
		t.Errorf("claude: %q", args)
	}
	if !strings.Contains(note, "installed hooks") {
		t.Errorf("note = %q", note)
	}
	if args, _ := hookArgs(root, "cursor"); len(args) != 2 || args[1] != filepath.Join(root, ".ws", "hooks", "cursor") {
		t.Errorf("cursor: %q", args)
	}
	if args, note := hookArgs(root, "shell"); args != nil || note != "" {
		t.Errorf("shell has no hooks: %q %q", args, note)
	}
}

// A new crew takes a capsule nobody is in before one whose crew handed back
// but is still open; it shares only when there's no choice.
func TestPreferFree(t *testing.T) {
	caps := []Capsule{{Name: "fe/a"}, {Name: "fe/b"}}
	handed := map[string][]string{"fe/a": {"c1"}}
	if p, _ := PreferFree(caps, map[string]string{}, handed, ""); p.Capsule != "fe/b" {
		t.Errorf("picked %s, want the free fe/b", p.Capsule)
	}
	if p, _ := PreferFree(caps, map[string]string{"fe/b": "c2"}, handed, ""); p.Capsule != "fe/a" || p.Busy != "" {
		t.Errorf("only fe/a is left (its crew handed back): got %+v", p)
	}
	if p, _ := PreferFree(caps, map[string]string{}, handed, "fe/a"); p.Capsule != "fe/a" {
		t.Errorf("asked for fe/a: got %+v", p)
	}
}
