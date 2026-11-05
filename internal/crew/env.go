package crew

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/brudil/workspace/internal/mission"
)

// envOriginPWD is ws's shell wrapper's record of where a command was run;
// it would mislead crew started somewhere else.
const envOriginPWD = "WS_ORIGIN_PWD"

// agentSessionEnv is the identity of an agent session ws itself may be
// running in (a deck started from Claude Code's terminal, or ws mission
// orchestrate run by an agent). Crew and the orchestrator are sessions of
// their own: inheriting these would make Claude Code treat them as nested
// in, or talking to, that session. Settings such as CLAUDE_CONFIG_DIR or
// CLAUDE_CODE_USE_BEDROCK aren't identity and pass through.
var agentSessionEnv = []string{
	"CLAUDECODE", "CLAUDE_PID", "CLAUDE_EFFORT",
	"CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_EXECPATH",
	"CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_SESSION_ATTENDED",
	"CLAUDE_CODE_MESSAGING_SOCKET", "CLAUDE_CODE_MESSAGING_TOKEN",
}

// Env is the environment for crew: ours, minus anything that would mislead
// it (another mission's identity, a role, another agent session's identity),
// plus the mission and crew ids, a capable TERM, and a PATH on which ws and
// workspace are this binary.
func Env(root, slug, crewID string) []string {
	drop := map[string]bool{mission.EnvMission: true, mission.EnvCrew: true, mission.EnvRole: true, envOriginPWD: true, "TERM": true, "COLORTERM": true}
	for _, k := range agentSessionEnv {
		drop[k] = true
	}
	var env []string
	path := os.Getenv("PATH")
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[k] && k != "PATH" {
			env = append(env, kv)
		}
	}
	if dir, err := ShimDir(root); err == nil {
		path = prependPath(dir, path)
	}
	return append(env,
		"PATH="+path,
		mission.EnvMission+"="+slug,
		mission.EnvCrew+"="+crewID,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)
}

// shimNames are the commands crew may use to reach ws: the binary's own
// name, and the shell wrapper's (which agents' non-interactive shells often
// don't load).
var shimNames = []string{"ws", "workspace"}

// ShimDir keeps <root>/.ws/bin/{ws,workspace} pointing at the running
// binary. Crew get it first on PATH, so the mission commands they're told to
// run come from the same ws build as the deck, even when another (older)
// ws is installed, as during a trial of a branch build.
func ShimDir(root string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Join(root, ".ws", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	for _, name := range shimNames {
		link := filepath.Join(dir, name)
		if cur, err := os.Readlink(link); err == nil && cur == exe {
			continue
		}
		// Replace atomically so concurrent launches never see a gap.
		tmp := link + ".tmp"
		os.Remove(tmp)
		if err := os.Symlink(exe, tmp); err != nil {
			return "", err
		}
		if err := os.Rename(tmp, link); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// prependPath puts dir first on a PATH value, dropping any later copy.
func prependPath(dir, path string) string {
	parts := []string{dir}
	for _, p := range filepath.SplitList(path) {
		if p != dir && p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, string(os.PathListSeparator))
}
