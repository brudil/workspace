package cli_test

import (
	"os"
	"strings"
	"testing"

	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/testutil"
)

// hook runs ws mission hook <event> as an agent's hook would: the crew's
// environment, the event's JSON on stdin.
func hook(t *testing.T, w *testutil.Workspace, event, payload string) {
	t.Helper()
	r, wr, _ := os.Pipe()
	wr.WriteString(payload)
	wr.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old; r.Close() }()
	if res := testutil.RunCommand(t, w.Root, nil, "mission", "hook", "--root", w.Root, event); res.Err != nil {
		t.Fatalf("hook %s: %v", event, res.Err)
	}
}

// What the agents report through their hooks lands on the crew's record:
// working once it's sent a message (which also answers what it said before),
// waiting with its last reply once its turn ends, permission when it asks.
func TestMissionHook_RecordsWhatCrewReport(t *testing.T) {
	w, store := orchestrateFixture(t)
	store.StartCrewAs("cache", "cursor", "", "") // c2, no chat id yet
	store.SetCrewStatus("cache", "c2", mission.CrewDone, "stage one done")
	t.Setenv(mission.EnvMission, "cache")
	t.Setenv(mission.EnvCrew, "c2")
	crew := func() mission.Crew {
		m, _ := store.Load("cache")
		return m.Crew[1]
	}

	// Cursor's: its session id is the chat to resume.
	hook(t, w, "start", `{"conversation_id":"conv-77","session_id":"conv-77","hook_event_name":"sessionStart"}`)
	if c := crew(); c.Conversation != "conv-77" {
		t.Errorf("conversation = %q", c.Conversation)
	}
	hook(t, w, "prompt", `{"prompt":"now stage two","hook_event_name":"beforeSubmitPrompt"}`)
	if c := crew(); c.Activity != mission.ActivityWorking || c.Status != "" || c.ActivityAt == nil {
		t.Errorf("after a message: activity %q, status %q", c.Activity, c.Status)
	}
	hook(t, w, "reply", `{"text":"Stage two is in: hello.py prints hello.\nDetails…","hook_event_name":"afterAgentResponse"}`)
	hook(t, w, "stop", `{"status":"completed","hook_event_name":"stop"}`)
	if c := crew(); c.Activity != mission.ActivityWaiting || !strings.HasPrefix(c.LastReply, "Stage two is in") {
		t.Errorf("after its turn: activity %q, reply %q", c.Activity, c.LastReply)
	}

	// Claude Code's: the reply comes with Stop; a permission notification.
	hook(t, w, "stop", `{"last_assistant_message":"READY","hook_event_name":"Stop"}`)
	hook(t, w, "notify", `{"message":"Claude needs your permission to use Bash","notification_type":"permission_prompt"}`)
	if c := crew(); c.LastReply != "READY" || c.Activity != mission.ActivityPermission || c.Notice != "Claude needs your permission to use Bash" {
		t.Errorf("claude: %+v", c)
	}
	hook(t, w, "notify", `{"message":"Claude is waiting for your input","notification_type":"idle_prompt"}`)
	if c := crew(); c.Activity != mission.ActivityPermission || c.Notice != "Claude is waiting for your input" {
		t.Errorf("an idle notice changes only the notice: %+v", c)
	}

	// Not crew ws launched (no WS_CREW): nothing, and no error.
	t.Setenv(mission.EnvCrew, "")
	before := crew()
	hook(t, w, "prompt", `{}`)
	if c := crew(); c.Activity != before.Activity {
		t.Error("a hook outside crew changed the record")
	}
}
