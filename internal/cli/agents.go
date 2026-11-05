package cli

import (
	"github.com/brudil/workspace/internal/config"
	"github.com/brudil/workspace/internal/harness"
)

// agentRoles is which harness and model each role uses, as configured in
// [crew], [briefing] and [orchestrator]. A nil config gives the defaults.
func agentRoles(cfg *config.Config) harness.Roles {
	if cfg == nil {
		return harness.Roles{}
	}
	return harness.Roles{
		Crew:         harness.Pick{Harness: cfg.Crew.Harness, Model: cfg.Crew.Model},
		Briefing:     harness.Pick{Harness: cfg.Briefing.Harness, Model: cfg.Briefing.Model},
		Orchestrator: harness.Pick{Harness: cfg.Orchestrator.Harness, Model: cfg.Orchestrator.Model},
	}
}
