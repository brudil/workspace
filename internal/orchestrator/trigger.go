package orchestrator

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/brudil/workspace/internal/crew"
	"github.com/brudil/workspace/internal/mission"
)

// Spawner starts the orchestrator for a crew event: "done" or "ended".
type Spawner func(root, slug, crewID, trigger string)

// Spawn starts `ws mission orchestrate --auto` for a crew event, detached,
// logging to the mission's orchestrator.log. That command decides (config,
// once per event) whether it actually runs.
func Spawn(root, slug, crewID, trigger string) {
	self, err := os.Executable()
	if err != nil {
		return
	}
	store := mission.Store{Root: root}
	logf, err := os.OpenFile(filepath.Join(store.Dir(), slug, "orchestrator.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer logf.Close()
	c := exec.Command(self, "mission", "orchestrate", "--auto", "--mission", slug, "--crew", crewID, "--trigger", trigger)
	c.Dir = root
	// It's nobody's crew: none of the spawner's mission identity applies.
	c.Env = append(os.Environ(), mission.EnvMission+"=", mission.EnvCrew+"=", mission.EnvRole+"=", "WS_ORIGIN_PWD=")
	c.Stdout, c.Stderr = logf, logf
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if c.Start() == nil {
		c.Process.Release()
	}
}

// AfterEnd is the hook hosts run when crew end: it starts the orchestrator
// for crew that ended by themselves. Not briefing crew, not crew someone
// stopped, and not crew that already reported done (their done triggered
// it already).
func AfterEnd(root string, store mission.Store, spawn Spawner) crew.EndHook {
	return func(spec crew.Spec, exit string, stopped bool) {
		if stopped || spec.Role == mission.RoleBriefing {
			return
		}
		if m, err := store.Load(spec.Slug); err == nil {
			for _, c := range m.Crew {
				if c.ID == spec.CrewID && c.Status == mission.CrewDone {
					return
				}
			}
		}
		spawn(root, spec.Slug, spec.CrewID, "ended")
	}
}
