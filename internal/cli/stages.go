package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/ui"
	"github.com/spf13/cobra"
)

// briefStage tells new work crew which stage of the brief's plan they're
// for: stage n, or for 0 the current one (the first not accepted). A brief
// without stages leaves it out.
func briefStage(b *harness.Brief, m *mission.Mission, brief string, n int) error {
	states, _ := m.StageProgress(brief)
	st, err := mission.PickStage(states, n)
	if err != nil {
		return err
	}
	if !st.Implicit {
		b.Stage, b.StageN, b.StageOf = st.Text, st.N, len(states)
	}
	return nil
}

func newMissionAcceptCmd() *cobra.Command {
	var slug string
	cmd := &cobra.Command{
		Use:   "accept <why>",
		Short: "Accept the mission's current stage: its work is complete and good enough",
		Long: `Accept the current stage of the brief's plan (the first not yet accepted):
its crew's work is complete and good enough to build on. The next stage
becomes the current one, and new crew work on it.

For the orchestrator, and you. It's refused while the stage's crew are
still working or asking you something. A brief without stages is one
stage, the whole brief.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			m, err := resolveMission(ctx, slug)
			if err != nil {
				return err
			}
			by := raisedBy()
			if by != mission.RoleOrchestrator && by != "you" {
				return errors.New("crew report done (ws mission done); the orchestrator or the user accepts the stage")
			}
			acc, states, err := missionStore(ctx).AcceptStage(m.Slug, by, strings.Join(args, " "))
			if err != nil {
				return err
			}
			if acc.Implicit {
				fmt.Fprintf(ui.Stderr, "  %s accepted the work on %s\n", ui.Green.Render("✓"), m.Slug)
				return nil
			}
			fmt.Fprintf(ui.Stderr, "  %s accepted stage %d of %d: %s\n", ui.Green.Render("✓"), acc.N, len(states), acc.Title())
			if next, ok := mission.CurrentStage(states); ok {
				fmt.Fprintf(ui.Stderr, "  next: stage %d: %s\n", next.N, next.Title())
			} else {
				fmt.Fprintf(ui.Stderr, "  that was the last stage\n")
			}
			return nil
		},
	}
	addMissionFlag(cmd, &slug)
	return cmd
}

// stageLines describes the brief's stages and their progress in words, one
// line each, for agents and plain output. A brief without stages has none,
// unless its work has been accepted.
func stageLines(m *mission.Mission, brief string) []string {
	states, orphans := m.StageProgress(brief)
	var out []string
	for _, st := range states {
		if st.Implicit && !st.Accepted() {
			return nil
		}
		mark := "to do"
		switch {
		case st.Accepted():
			mark = "accepted"
		case st.Current:
			mark = "current"
		}
		line := fmt.Sprintf("%d. [%s] %s", st.N, mark, oneLine(st.Text))
		if st.Implicit {
			line = "[accepted] the whole brief"
		}
		var facts []string
		if st.CheckIn {
			facts = append(facts, "check in with the user after it")
		}
		if st.Record != nil && len(st.Record.Crew) > 0 {
			facts = append(facts, "crew "+strings.Join(st.Record.Crew, ", "))
		}
		if st.Accepted() {
			a := "accepted by " + st.Record.AcceptedBy
			if st.Record.Why != "" {
				a += ": " + st.Record.Why
			}
			facts = append(facts, a)
		}
		if len(facts) > 0 {
			line += " (" + strings.Join(facts, "; ") + ")"
		}
		out = append(out, line)
	}
	for _, o := range orphans {
		line := "no longer in the brief: " + oneLine(o.Text)
		if o.Accepted != nil {
			line += " (accepted)"
		}
		out = append(out, line)
	}
	return out
}

// oneLine joins a stage's lines.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// stageCapsuleWarnings names what the brief's stages point at, as a
// capsule written `repo/name` with a repo of the workspace, that isn't one
// of the mission's capsules. ws only warns: the brief is the user's.
func stageCapsuleWarnings(ctx *Context, m *mission.Mission, brief string) []string {
	have := map[string]bool{}
	for _, c := range m.Capsules {
		have[c.Repo+"/"+c.Name] = true
	}
	var out []string
	for _, st := range mission.ParseStages(brief) {
		for _, ref := range st.NamedCapsules() {
			repo, name, _ := strings.Cut(ref, "/")
			r, ok := ctx.WS.ResolveAlias(repo)
			if !ok || have[r+"/"+name] {
				continue
			}
			out = append(out, fmt.Sprintf("stage %d names `%s`, which isn't one of the mission's capsules (yet): ws lift %s %s, or change the brief", st.N, ref, r, name))
		}
	}
	return out
}

func printWarnings(warnings []string) {
	for _, w := range warnings {
		fmt.Fprintf(ui.Stderr, "  %s %s\n", ui.Orange.Render("!"), w)
	}
}
