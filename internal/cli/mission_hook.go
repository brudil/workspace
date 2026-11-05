package cli

import (
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/brudil/workspace/internal/execlog"
	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
	"github.com/spf13/cobra"
)

// hookPayloadMax bounds what a hook may hand over on stdin.
const hookPayloadMax = 1 << 20

func newMissionHookCmd() *cobra.Command {
	var root string
	cmd := &cobra.Command{
		Use:   "hook <event>",
		Short: "For agents' hooks: record what crew is doing (ws installs these when it launches crew)",
		Long: `Run by an agent's own hooks, which ws installs in each crew session it
launches (Claude Code through --settings, Cursor through a plugin): it records
what the crew reported, from WS_MISSION and WS_CREW and the event's JSON on
stdin. Events: start, prompt (it was sent a message: working), reply (its
turn's final message), stop (it finished its turn: waiting for you), notify.
It never fails, so a hook can't get in the agent's way.`,
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, _ := io.ReadAll(io.LimitReader(cmd.InOrStdin(), hookPayloadMax))
			slug, crewID := os.Getenv(mission.EnvMission), os.Getenv(mission.EnvCrew)
			if root == "" || slug == "" || crewID == "" {
				return nil // not crew ws launched
			}
			var payload map[string]any
			json.Unmarshal(data, &payload)
			h, ok := hookFromEvent(args[0], payload)
			if !ok {
				return nil
			}
			if err := (mission.Store{Root: root}).CrewHook(slug, crewID, h); err != nil {
				execlog.Note(root, "crew "+slug+"/"+crewID, "hook %s: %v", args[0], err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&root, "root", "", "the workspace root")
	return cmd
}

// hookFromEvent reads one of ws's hook events, with its agent's payload:
// Claude Code's and Cursor's use different field names for the same things.
func hookFromEvent(event string, p map[string]any) (mission.Hook, bool) {
	str := func(keys ...string) string {
		for _, k := range keys {
			if s, ok := p[k].(string); ok && s != "" {
				return s
			}
		}
		return ""
	}
	switch event {
	case harness.HookStart:
		return mission.Hook{Conversation: str("conversation_id", "session_id")}, true
	case harness.HookPrompt:
		return mission.Hook{Prompted: true}, true
	case harness.HookReply:
		return mission.Hook{Reply: str("text")}, true
	case harness.HookStop:
		return mission.Hook{Activity: mission.ActivityWaiting, Reply: str("last_assistant_message")}, true
	case harness.HookNotify:
		h := mission.Hook{Notice: str("message")}
		if t := str("notification_type"); strings.Contains(t, "permission") || strings.Contains(t, "elicitation") {
			h.Activity = mission.ActivityPermission
		}
		return h, true
	}
	return mission.Hook{}, false
}
