package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/brudil/workspace/internal/mission"
)

// spawnOrchestrator starts `ws mission orchestrate --auto` for a crew event,
// detached, logging to the mission's orchestrator.log. The command decides
// (config, de-dup) whether it actually runs.
func spawnOrchestrator(root, slug, crewID, trigger string) {
	self, err := os.Executable()
	if err != nil {
		return
	}
	store := mission.Store{Root: root}
	logPath := filepath.Join(store.Dir(), slug, "orchestrator.log")
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer logf.Close()
	c := exec.Command(self, "mission", "orchestrate", "--auto", "--mission", slug, "--crew", crewID, "--trigger", trigger)
	c.Dir = root
	c.Env = append(os.Environ(), mission.EnvMission+"=", mission.EnvCrew+"=", ShellPWD+"=", EnvRole+"=")
	c.Stdout, c.Stderr = logf, logf
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if c.Start() == nil {
		c.Process.Release()
	}
}

// orchestrateSpawner is replaced in tests.
var orchestrateSpawner = spawnOrchestrator

// maybeOrchestrateAfterEnd runs the orchestrator for crew that ended by themselves: not briefing
// crew, not crew someone stopped, and not crew that already reported done
// (their done already triggered it).
func maybeOrchestrateAfterEnd(root string, store mission.Store, slug, crewID, role string, stopped bool) {
	if stopped || role == mission.RoleBriefing {
		return
	}
	if m, err := store.Load(slug); err == nil {
		if c, ok := crewRecord(m, crewID); ok && c.Status == mission.CrewDone {
			return
		}
	}
	orchestrateSpawner(root, slug, crewID, "ended")
}
