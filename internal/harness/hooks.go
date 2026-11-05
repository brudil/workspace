package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Hooks is a harness whose agent can report its activity to ws through its
// own hooks, installed for one session when ws launches it. ws learns that
// crew is working (you sent it a message), waiting (it finished its turn),
// or stopped for permission, rather than guessing from its screen.
type Hooks interface {
	// HookArgs are arguments, placed right after the program, that make the
	// session run cmd followed by one of the Hook* events below. dir is a
	// folder the harness may write its hook setup into.
	HookArgs(cmd []string, dir string) ([]string, error)
}

// The events ws's hook command understands, whichever agent sends them.
const (
	HookStart  = "start"  // a session started (its id in the payload)
	HookPrompt = "prompt" // it was sent a message: it's working
	HookReply  = "reply"  // its turn's final message
	HookStop   = "stop"   // it finished its turn: it's waiting for you
	HookNotify = "notify" // it raised a notification (waiting, permission)
)

// hookCommand is cmd and the event, quoted for the shell that runs hooks.
func hookCommand(cmd []string, event string) string {
	parts := make([]string, 0, len(cmd)+1)
	for _, c := range append(append([]string{}, cmd...), event) {
		parts = append(parts, shellQuote(c))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '-' || r == '_' || r == ':' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// HookArgs installs ws's hooks in this Claude Code session alone, through
// --settings (an inline JSON string): they add to your own hooks.
//
// VERIFIED (claude 2.1.318): SessionStart (session_id), UserPromptSubmit
// (also for the opening prompt given as the argument), Stop
// (last_assistant_message is the turn's final message), and Notification
// (notification_type idle_prompt after about a minute's wait, or a
// permission request) all fire, with the session's environment (WS_MISSION,
// WS_CREW) and the event as JSON on stdin.
func (c *Claude) HookArgs(cmd []string, dir string) ([]string, error) {
	entry := func(event string) []map[string]any {
		return []map[string]any{{"hooks": []map[string]string{{"type": "command", "command": hookCommand(cmd, event)}}}}
	}
	settings, err := json.Marshal(map[string]any{"hooks": map[string]any{
		"SessionStart":     entry(HookStart),
		"UserPromptSubmit": entry(HookPrompt),
		"Stop":             entry(HookStop),
		"Notification":     entry(HookNotify),
	}})
	if err != nil {
		return nil, err
	}
	return []string{"--settings", string(settings)}, nil
}

// HookArgs installs ws's hooks in this Cursor session alone, as a plugin
// written into dir and loaded with --plugin-dir: they add to your own hooks.
//
// VERIFIED (agent 2026.11.05): a plugin is a folder with
// .cursor-plugin/plugin.json and hooks/hooks.json; sessionStart
// (conversation_id), beforeSubmitPrompt, afterAgentResponse (text, the
// turn's final message) and stop (status) fire from it with the session's
// environment and the event as JSON on stdin. Cursor has no notification
// for a permission prompt.
//
// VERIFIED: Cursor runs a plugin's hooks for an event only when one of your
// own hooks files has a hook for that event too, and one whose command is a
// path (a bare "true" doesn't count); sessionStart is the exception. So
// without entries for beforeSubmitPrompt, afterAgentResponse and stop in
// ~/.cursor/hooks.json, only sessionStart reaches ws. EnableCursorHooks adds
// entries that do nothing (ws doctor --fix).
func (c *Cursor) HookArgs(cmd []string, dir string) ([]string, error) {
	hooks := map[string][]map[string]string{}
	for event, ours := range map[string]string{
		"sessionStart":       HookStart,
		"beforeSubmitPrompt": HookPrompt,
		"afterAgentResponse": HookReply,
		"stop":               HookStop,
	} {
		hooks[event] = []map[string]string{{"command": hookCommand(cmd, ours)}}
	}
	files := map[string]any{
		filepath.Join(".cursor-plugin", "plugin.json"): map[string]string{
			"name": "ws-crew-hooks", "version": "1.0.0",
			"description": "Tells ws (the workspace tool) what this crew is doing.",
		},
		filepath.Join("hooks", "hooks.json"): map[string]any{"version": 1, "hooks": hooks},
	}
	for name, v := range files {
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, err
		}
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("writing cursor hooks: %w", err)
		}
		// Atomically: crew launching at once write the same files, and a
		// session starting must never read half of one.
		if err := writeAtomically(path, append(data, '\n')); err != nil {
			return nil, fmt.Errorf("writing cursor hooks: %w", err)
		}
	}
	return []string{"--plugin-dir", dir}, nil
}

// writeAtomically writes path through a temporary file of its own, renamed
// into place.
func writeAtomically(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(f.Name(), 0o644)
	}
	if werr == nil {
		werr = os.Rename(f.Name(), path)
	}
	if werr != nil {
		os.Remove(f.Name())
	}
	return werr
}

// cursorTurnEvents are the events Cursor runs ws's plugin hooks for only if
// ~/.cursor/hooks.json has a hook for them too.
var cursorTurnEvents = []string{"beforeSubmitPrompt", "afterAgentResponse", "stop"}

// cursorNoopCommand is a hook that does nothing, by path.
const cursorNoopCommand = "/usr/bin/true"

// CursorUserHooksPath is where Cursor reads your own hooks.
func CursorUserHooksPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cursor", "hooks.json"), nil
}

// CursorHooksMissing lists the turn events ~/.cursor/hooks.json has no
// hook for (by path), so Cursor won't run ws's plugin hooks for them.
func CursorHooksMissing() ([]string, error) {
	path, err := CursorUserHooksPath()
	if err != nil {
		return nil, err
	}
	doc, err := readCursorHooks(path)
	if err != nil {
		return nil, err
	}
	hooks, _ := doc["hooks"].(map[string]any)
	var missing []string
	for _, event := range cursorTurnEvents {
		if !hasPathHook(hooks[event]) {
			missing = append(missing, event)
		}
	}
	return missing, nil
}

func hasPathHook(entries any) bool {
	list, _ := entries.([]any)
	for _, e := range list {
		m, _ := e.(map[string]any)
		cmd, _ := m["command"].(string)
		if c := strings.TrimSpace(cmd); strings.HasPrefix(c, "/") || strings.HasPrefix(c, "~/") {
			return true
		}
	}
	return false
}

// readCursorHooks reads a hooks file as generic JSON (keeping whatever it
// holds), or an empty one if there's none.
func readCursorHooks(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{"version": 1, "hooks": map[string]any{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s isn't valid JSON: %w", path, err)
	}
	if _, ok := doc["hooks"].(map[string]any); !ok {
		doc["hooks"] = map[string]any{}
	}
	return doc, nil
}

// EnableCursorHooks adds a do-nothing hook to ~/.cursor/hooks.json for each
// turn event it has none for, so Cursor runs ws's per-crew plugin hooks.
// Everything already in the file stays.
func EnableCursorHooks() error {
	path, err := CursorUserHooksPath()
	if err != nil {
		return err
	}
	doc, err := readCursorHooks(path)
	if err != nil {
		return err
	}
	hooks := doc["hooks"].(map[string]any)
	for _, event := range cursorTurnEvents {
		if !hasPathHook(hooks[event]) {
			list, _ := hooks[event].([]any)
			hooks[event] = append(list, map[string]any{"command": cursorNoopCommand})
		}
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeAtomically(path, append(data, '\n'))
}
