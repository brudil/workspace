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
launches (Claude Code through --settings, Cursor through a plugin, pi through
an extension): it records
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
// Claude Code's, Cursor's and pi's use different field names for the same
// things. (pi's are what ws's own extension sends: see harness.Pi.)
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
		reply := str("last_assistant_message") // Claude Code says it
		if reply == "" {
			reply = lastAssistantText(str("transcript_path")) // Cursor's is in its transcript
		}
		return mission.Hook{Activity: mission.ActivityWaiting, Reply: reply}, true
	case harness.HookNotify:
		h := mission.Hook{Notice: str("message")}
		// pi's ui_prompt is an extension's dialog waiting for an answer.
		if t := str("notification_type"); strings.Contains(t, "permission") || strings.Contains(t, "elicitation") || t == "ui_prompt" {
			h.Activity = mission.ActivityPermission
		}
		return h, true
	}
	return mission.Hook{}, false
}

// transcriptTail is how much of a transcript's end is read for its last
// message.
const transcriptTail = 512 << 10

// lastAssistantText is the last message the agent wrote in a transcript of
// JSON lines (Cursor's: {"role":"assistant","message":{"content":[{"type":
// "text","text":…}]}}), or "" if there's none to read.
func lastAssistantText(path string) string {
	if !strings.HasSuffix(path, ".jsonl") {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() > transcriptTail {
		f.Seek(info.Size()-transcriptTail, io.SeekStart)
	}
	data, _ := io.ReadAll(f)
	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		var entry struct {
			Role    string `json:"role"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(lines[i]), &entry) != nil || entry.Role != "assistant" {
			continue
		}
		var parts []string
		for _, c := range entry.Message.Content {
			if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
				parts = append(parts, c.Text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return ""
}
