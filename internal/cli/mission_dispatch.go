package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/brudil/workspace/internal/crew"
	"github.com/brudil/workspace/internal/deckserver"
	"github.com/brudil/workspace/internal/execlog"
	"github.com/brudil/workspace/internal/harness"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/ui"
	"github.com/spf13/cobra"
)

// A dispatched crew's screen size until a deck attaches and resizes it.
const dispatchWidth, dispatchHeight = 120, 40

// dispatchHost is where dispatched crew run; tests replace it.
var dispatchHost = func(root string) crew.Host { return deckserver.NewHost(root) }

func newMissionDispatchCmd() *cobra.Command {
	var slug, capsule, harnessName string
	var queue bool
	var reads []string
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
			if by != mission.RoleOrchestrator && by != "you" {
				return errors.New("crew don't dispatch other crew: run ws mission done when your part is finished, and the orchestrator decides what follows")
			}
			if m.State == mission.Briefing {
				return fmt.Errorf("mission %s is still being briefed", m.Slug)
			}
			handoff := strings.TrimSpace(strings.Join(args, " "))
			if len(reads) > 0 {
				list, err := readFirstList(ctx, m, reads)
				if err != nil {
					return err
				}
				handoff += "\n\nRead these first:\n" + list
			}
			crewPick := agentRoles(ctx.Config).ForCrew()
			if harnessName == "" {
				harnessName = crewPick.Harness
			}
			queued := queue || !ctx.Config.OrchestratorAutoDispatch() || !deckServerEnabled()
			// Out of dispatches is the answer that matters most: say it
			// before anything else (CountDispatch enforces it again below).
			if by == mission.RoleOrchestrator && !queued && m.Dispatches >= ctx.Config.OrchestratorMaxDispatches() {
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
				if err := store.QueueDispatch(m.Slug, &mission.Dispatch{Text: handoff, Capsule: place.Capsule, Harness: harnessName, By: by}); err != nil {
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
			if by == mission.RoleOrchestrator {
				if err := store.CountDispatch(m.Slug, ctx.Config.OrchestratorMaxDispatches()); err != nil {
					return dispatchLimitErr(err)
				}
				counted = true
			} else {
				store.UserActed(m.Slug)
			}
			brief, _ := store.ReadBrief(m.Slug)
			b := harness.Brief{Slug: m.Slug, Title: m.Title, Brief: brief, Dir: place.Dir, Handoff: handoff, Model: crewPick.ModelFor(h.Name())}
			host := dispatchHost(ctx.WS.Root)
			defer host.Leave()
			endHandedBack(ctx.WS.Root, store, host, m, place.Capsule)
			started, err := crew.Start(crew.Request{
				Store: store, Host: host, Root: ctx.WS.Root,
				Slug: m.Slug, Harness: h.Name(), Place: place, Handoff: handoff, By: by,
				Width: dispatchWidth, Height: dispatchHeight,
				Prepare: func() (harness.Launch, error) { return h.Start(b) },
			})
			if err != nil {
				if counted {
					store.ReleaseDispatch(m.Slug) // nothing launched: don't use up the allowance
				}
				return err
			}
			id := started.Crew.ID
			if started.Prompt != "" && !crew.TypePrompt(ctx.WS.Root, m.Slug+"/"+id, started.Term, started.Prompt) {
				return fmt.Errorf("%s exited before its prompt could be sent: %s", id, started.Term.ExitSummary())
			}
			// Make sure the paste reached the server before letting go of it.
			if f, ok := host.(interface{ Flush() }); ok {
				f.Flush()
			}
			fmt.Fprintf(ui.Stderr, "  %s dispatched %s on %s in %s; it's running in the deck server\n", ui.Green.Render("✓"), id, m.Slug, place.Capsule)
			return nil
		},
	}
	addMissionFlag(cmd, &slug)
	cmd.Flags().StringVar(&capsule, "capsule", "", "capsule to work in, as repo/name (default: one no crew is working in)")
	cmd.Flags().StringVar(&harnessName, "harness", "", "harness to launch (default: the crew's: $WS_DECK_HARNESS, [crew] harness, or cursor)")
	cmd.Flags().StringArrayVar(&reads, "read", nil, "an artifact the crew should read first (repeatable): its reference, a file, or a URL; the handoff says where to read it")
	cmd.Flags().BoolVar(&queue, "queue", false, "queue it for the user to launch from the deck instead of launching now")
	return cmd
}

// readFirstList resolves what a dispatch names with --read into lines for
// the handoff, with where to read each. A recorded artifact keeps its kind
// and label.
func readFirstList(ctx *Context, m *mission.Mission, reads []string) (string, error) {
	v := buildMissionViewsFrom(ctx.WS, nil, []*mission.Mission{m})[0]
	store := missionStore(ctx)
	var b strings.Builder
	for _, in := range reads {
		r, err := normalizeArtifact(ctx, m, in)
		if err != nil {
			return "", fmt.Errorf("--read %s: %w", in, err)
		}
		a := mission.Artifact{Ref: r.String(), Kind: defaultKind(r)}
		for _, rec := range m.Artifacts {
			if rec.Ref == a.Ref {
				a = rec
			}
		}
		b.WriteString("- " + artifactLine(resolveArtifact(ctx.WS, store, v, a)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// endHandedBack ends the sessions of crew that reported done in capsule but
// are still open, before new crew start there: they're idle, and two agents
// in one worktree would edit the same files. Their ends are recorded as
// stopped, so they can be resumed, and nothing runs the orchestrator for
// them again (their done already did).
func endHandedBack(root string, store mission.Store, host crew.Host, m *mission.Mission, capsule string) {
	ids := crew.HandedBackIn(m)[capsule]
	if len(ids) == 0 {
		return
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = m.Slug + "/" + id
	}
	reached := map[string]bool{}
	for _, k := range crew.StopHandedBack(host, keys) {
		reached[k] = true
	}
	var ended, left []string
	for i, id := range ids {
		if reached[keys[i]] {
			ended = append(ended, id)
		} else {
			left = append(left, id)
		}
	}
	if len(ended) > 0 {
		text := fmt.Sprintf("ended %s's session in %s: it had handed back its work, and new crew start there (R resumes it)", strings.Join(ended, ", "), capsule)
		store.AppendLog(m.Slug, mission.Event{Kind: mission.EventNote, Text: text})
		execlog.Note(root, "crew "+m.Slug, "%s", text)
		fmt.Fprintf(ui.Stderr, "  %s %s\n", ui.Dim.Render("·"), text)
	}
	if len(left) > 0 {
		fmt.Fprintf(ui.Stderr, "  %s %s handed back but couldn't be reached to end its session; it stays open in %s\n", ui.Orange.Render("⚠"), strings.Join(left, ", "), capsule)
	}
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
// edit the same files. Its errors tell the orchestrator how to recover.
func dispatchPlace(ctx *Context, m *mission.Mission, want string, shared bool) (crew.Place, error) {
	var caps []crew.Capsule
	for _, c := range buildMissionViewsFrom(ctx.WS, nil, []*mission.Mission{m})[0].Capsules {
		if c.Exists {
			caps = append(caps, crew.Capsule{Name: c.Repo + "/" + c.Name, Path: c.Path})
		}
	}
	p, err := crew.PreferFree(caps, crew.BusyIn(m), crew.HandedBackIn(m), want)
	var unknown *crew.UnknownCapsuleError
	switch {
	case errors.As(err, &unknown):
		return p, fmt.Errorf("mission %s has %s; create it with ws lift <repo> <branch>, which joins it to the mission", m.Slug, unknown)
	case errors.Is(err, crew.ErrNoCapsule):
		return p, fmt.Errorf("mission %s has no capsule to work in: create one with ws lift <repo> <branch> (it joins the mission), then dispatch with --capsule <repo>/<branch>", m.Slug)
	case err != nil:
		return p, err
	case p.Busy != "" && !shared && want != "":
		return p, fmt.Errorf("%s is still working in %s; wait for it to finish, or create another capsule with ws lift <repo> <branch> (it joins the mission) and dispatch there", p.Busy, p.Capsule)
	case p.Busy != "" && !shared:
		var names []string
		for _, c := range caps {
			names = append(names, c.Name)
		}
		return p, fmt.Errorf("crew are working in every capsule of %s (%s); wait for them, or create another capsule with ws lift <repo> <branch> and dispatch with --capsule", m.Slug, strings.Join(names, ", "))
	}
	return p, nil
}
