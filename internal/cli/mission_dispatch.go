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

// A dispatched crew's screen size until a deck attaches and resizes it.
const dispatchWidth, dispatchHeight = 120, 40

// dispatchHost is where dispatched crew run; tests replace it.
var dispatchHost = func(root string) crewHost { return &serverHost{root: root} }

func newMissionDispatchCmd() *cobra.Command {
	var slug, capsule, harnessName string
	var queue bool
	cmd := &cobra.Command{
		Use:   "dispatch <handoff>",
		Short: "Start new crew on a mission with a handoff (what to do, what earlier crew left)",
		Long: `Start fresh crew on the mission's next piece of work. The handoff is all
it knows beyond the brief: what to do, where earlier crew's output is
(files, commits, PRs) and what to watch out for.

The crew runs in the deck server, so it starts even with no deck open, and
the deck picks it up. With [orchestrator] dispatch = "ask" (or --queue, or
WS_DECK_SERVER=0), it's queued instead: the deck shows it and ⏎ launches it.

The orchestrator may dispatch [orchestrator] max_dispatches crew (default
3) in a row; after that it has to flag you, and anything you do on the
mission (launching crew, dismissing a flag) starts the count again.`,
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
			if by != roleOrchestrator && by != "you" {
				return errors.New("crew don't dispatch other crew: run ws mission done when your part is finished, and the orchestrator decides what follows")
			}
			if m.State == mission.Briefing {
				return fmt.Errorf("mission %s is still being briefed", m.Slug)
			}
			handoff := strings.TrimSpace(strings.Join(args, " "))
			if harnessName == "" {
				harnessName = harness.Default()
			}
			queued := queue || !ctx.Config.OrchestratorAutoDispatch() || !deckServerEnabled()
			// Out of dispatches is the answer that matters most: say it
			// before anything else (CountDispatch enforces it again below).
			if by == roleOrchestrator && !queued && m.Dispatches >= ctx.Config.OrchestratorMaxDispatches() {
				return dispatchLimitErr(fmt.Errorf("%w: %d crew dispatched without the user", mission.ErrDispatchLimit, m.Dispatches))
			}
			// Launching now, nobody is there to confirm sharing a capsule
			// with working crew; a queued dispatch is confirmed in the deck.
			place, err := dispatchPlace(ctx, m, capsule, queued)
			if err != nil {
				return err
			}
			store := missionStore(ctx)

			if queued {
				if err := store.QueueDispatch(m.Slug, &mission.Dispatch{Text: handoff, Capsule: place.capsule, Harness: harnessName, By: by}); err != nil {
					if errors.Is(err, mission.ErrDispatchQueued) {
						return fmt.Errorf("%w; only one waits at a time, so don't queue another: say what you'd dispatch next in ws mission note, or ws mission flag the user", err)
					}
					return err
				}
				fmt.Fprintf(ui.Stderr, "  %s queued: the deck shows it on %s, and ⏎ launches it\n", ui.Green.Render("✓"), m.Slug)
				return nil
			}

			h, err := harness.Get(harnessName)
			if err == nil {
				err = h.Available()
			}
			if err != nil {
				return err
			}
			counted := false
			if by == roleOrchestrator {
				if err := store.CountDispatch(m.Slug, ctx.Config.OrchestratorMaxDispatches()); err != nil {
					return dispatchLimitErr(err)
				}
				counted = true
			} else {
				store.UserActed(m.Slug)
			}
			brief, _ := store.ReadBrief(m.Slug)
			c, err := launchDispatched(ctx.WS.Root, store, dispatchHost(ctx.WS.Root), h, harness.Brief{
				Slug: m.Slug, Title: m.Title, Brief: brief, Dir: place.dir, Handoff: handoff,
			}, place.capsule, by)
			if err != nil {
				if counted && c == "" {
					store.ReleaseDispatch(m.Slug) // nothing launched: don't use up the allowance
				}
				return err
			}
			fmt.Fprintf(ui.Stderr, "  %s dispatched %s on %s in %s; it's running in the deck server\n", ui.Green.Render("✓"), c, m.Slug, place.capsule)
			return nil
		},
	}
	addMissionFlag(cmd, &slug)
	cmd.Flags().StringVar(&capsule, "capsule", "", "capsule to work in, as repo/name (default: one no crew is working in)")
	cmd.Flags().StringVar(&harnessName, "harness", "", "harness to launch (default: $WS_DECK_HARNESS, or cursor)")
	cmd.Flags().BoolVar(&queue, "queue", false, "queue it for the user to launch from the deck instead of launching now")
	return cmd
}

// dispatchLimitErr tells the orchestrator what to do instead.
func dispatchLimitErr(err error) error {
	if errors.Is(err, mission.ErrDispatchLimit) {
		return fmt.Errorf("%w; the user hasn't stepped in since, so don't dispatch more: run ws mission flag \"<what you'd dispatch next, and why>\" instead", err)
	}
	return err
}

// dispatchPlace picks the capsule dispatched crew work in: the one asked
// for, else the first one no running crew is in. Unless shared is allowed,
// it refuses a capsule crew is still working in: two agents in one worktree
// edit the same files.
func dispatchPlace(ctx *Context, m *mission.Mission, want string, shared bool) (crewPlace, error) {
	v := buildMissionViewsFrom(ctx.WS, nil, []*mission.Mission{m})[0]
	busy := map[string]string{} // capsule → crew working there
	for _, c := range m.Crew {
		// A record whose process is gone (a crashed deck server) isn't
		// working anywhere.
		if c.Ended == nil && c.Capsule != "" && (c.PID == 0 || processAlive(c.PID)) {
			busy[c.Capsule] = c.ID
		}
	}
	var names []string
	var first, free *crewPlace
	for _, c := range v.Capsules {
		if !c.Exists {
			continue
		}
		name := c.Repo + "/" + c.Name
		names = append(names, name)
		p := crewPlace{dir: c.Path, capsule: name}
		if want == name {
			if id := busy[name]; id != "" && !shared {
				return crewPlace{}, fmt.Errorf("%s is still working in %s; wait for it to finish, or create another capsule with ws lift <repo> <branch> (it joins the mission) and dispatch there", id, name)
			}
			return p, nil
		}
		if first == nil {
			first = &p
		}
		if free == nil && busy[name] == "" {
			free = &p
		}
	}
	switch {
	case want != "" && len(names) > 0:
		return crewPlace{}, fmt.Errorf("mission %s has no capsule %s (it has: %s); create it with ws lift <repo> <branch>, which joins it to the mission", m.Slug, want, strings.Join(names, ", "))
	case want != "":
		return crewPlace{}, fmt.Errorf("mission %s has no capsule %s; create it with ws lift <repo> <branch>, which joins it to the mission", m.Slug, want)
	case free != nil:
		return *free, nil
	case first != nil && shared:
		return *first, nil
	case first != nil:
		return crewPlace{}, fmt.Errorf("crew are working in every capsule of %s (%s); wait for them, or create another capsule with ws lift <repo> <branch> and dispatch with --capsule", m.Slug, strings.Join(names, ", "))
	}
	return crewPlace{}, fmt.Errorf("mission %s has no capsule to work in: create one with ws lift <repo> <branch> (it joins the mission), then dispatch with --capsule <repo>/<branch>", m.Slug)
}

// launchDispatched starts crew in the deck server, types its opening prompt
// in, and leaves it running there. It returns the new crew's id.
func launchDispatched(root string, store mission.Store, host crewHost, h harness.Harness, b harness.Brief, capsule, by string) (string, error) {
	l, err := h.Start(b)
	if err != nil {
		return "", err
	}
	crew, err := store.StartCrewAs(b.Slug, h.Name(), l.Conversation, "")
	if err != nil {
		return "", err
	}
	store.SetHandoff(b.Slug, crew.ID, by, b.Handoff)
	term, err := host.Launch(crewSpec{
		Slug: b.Slug, CrewID: crew.ID, Harness: h.Name(), Capsule: capsule,
		Args: l.Args, Dir: l.Dir, Env: crewEnv(root, b.Slug, crew.ID),
		Width: dispatchWidth, Height: dispatchHeight,
	})
	if err != nil {
		store.EndCrew(b.Slug, crew.ID, "failed to start: "+err.Error())
		return "", err
	}
	store.SetCrewProcess(b.Slug, crew.ID, term.Pid(), capsule)
	if l.Prompt != "" && !typePrompt(term, l.Prompt) {
		host.Detach()
		return crew.ID, fmt.Errorf("%s exited before its prompt could be sent: %s", crew.ID, term.ExitSummary())
	}
	// Make sure the paste reached the server before letting go of it.
	if f, ok := host.(interface{ Flush() }); ok {
		f.Flush()
	}
	host.Detach()
	return crew.ID, nil
}
