package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/brudil/workspace/internal/mission"
)

// spawnReview starts `ws mission review --auto` for a crew event, detached,
// logging to the mission's review.log. The command decides (config, de-dup)
// whether a review actually runs.
func spawnReview(root, slug, crewID, trigger string) {
	self, err := os.Executable()
	if err != nil {
		return
	}
	store := mission.Store{Root: root}
	logPath := filepath.Join(store.Dir(), slug, "review.log")
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer logf.Close()
	c := exec.Command(self, "mission", "review", "--auto", "--mission", slug, "--crew", crewID, "--trigger", trigger)
	c.Dir = root
	c.Env = append(os.Environ(), mission.EnvMission+"=", mission.EnvCrew+"=", ShellPWD+"=")
	c.Stdout, c.Stderr = logf, logf
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if c.Start() == nil {
		c.Process.Release()
	}
}

// reviewSpawner is replaced in tests.
var reviewSpawner = spawnReview

// maybeReviewAfterEnd reviews crew that ended by themselves: not briefing
// crew, not crew someone stopped, and not crew that already reported done
// (their done triggered a review).
func maybeReviewAfterEnd(root string, store mission.Store, slug, crewID, role string, stopped bool) {
	if stopped || role == mission.RoleBriefing {
		return
	}
	if m, err := store.Load(slug); err == nil {
		if c, ok := crewRecord(m, crewID); ok && c.Status == mission.CrewDone {
			return
		}
	}
	reviewSpawner(root, slug, crewID, "ended")
}
