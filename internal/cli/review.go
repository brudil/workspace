package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/brudil/workspace/internal/review"
	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/spf13/cobra"
)

func newReviewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Reviews of a mission's capsules: read what the user asked for",
		Long: `The user reviews a mission capsule's commits in the web UI (v in ws deck):
comments on lines, then a verdict. Once submitted, a review is the
mission's: crew read it with ws review show and act on it, and the
orchestrator decides what follows.

A comment's kind says what to do with it: blocking (must change),
suggestion (a judgement call: take it unless you have a reason not to),
question (answer it), nit (do it if it's cheap).`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) { cmd.SilenceUsage = true },
	}
	cmd.AddCommand(newReviewShowCmd(), newReviewListCmd())
	return cmd
}

func newReviewShowCmd() *cobra.Command {
	var slug, format string
	cmd := &cobra.Command{
		Use:   "show [review]",
		Short: "Print a review: its comments with the lines they're on, and what to do",
		Long: `Print a submitted review, by its id (r3), as markdown: the verdict and
summary, then each comment with the lines it's on as they were, and what
to do. Without an id, the latest review of the capsule you're in, or else
of the mission.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			m, err := resolveMission(ctx, slug)
			if err != nil {
				return err
			}
			store := review.For(ctx.WS.Root, m.Slug)
			var r *review.Review
			if len(args) == 1 {
				if r, err = store.Load(args[0]); err != nil {
					return err
				}
				if r.State == review.Draft {
					return fmt.Errorf("%s is a draft: it's the user's until they submit it", r.ID)
				}
			} else {
				capsule := ""
				cwd, _ := userDir()
				if repo, name, ok := workspace.DetectRepo(ctx.WS.Root, cwd); ok && m.HasCapsule(repo, name) {
					capsule = repo + "/" + name
				}
				if r, err = store.Latest(capsule); err == nil && r == nil && capsule != "" {
					r, err = store.Latest("")
				}
				if err != nil {
					return err
				}
				if r == nil {
					return errors.New("no reviews of mission " + m.Slug + " yet")
				}
			}
			_ = format // markdown reads well to people and agents alike
			fmt.Print(review.Render(r))
			return nil
		},
	}
	addMissionFlag(cmd, &slug)
	cmd.Flags().StringVarP(&format, "format", "f", "text", "output format: text or llm (both markdown)")
	return cmd
}

func newReviewListCmd() *cobra.Command {
	var slug string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List a mission's reviews",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			m, err := resolveMission(ctx, slug)
			if err != nil {
				return err
			}
			all, err := review.For(ctx.WS.Root, m.Slug).List()
			if err != nil {
				return err
			}
			if len(all) == 0 {
				fmt.Fprintf(ui.Stderr, "  %s no reviews of %s yet: v in ws deck reviews a capsule\n", ui.Dim.Render("·"), m.Slug)
				return nil
			}
			for _, r := range all {
				what := strings.TrimPrefix(review.Title(r), "Review "+r.ID+" of "+r.Capsule+": ")
				when := ""
				if r.State == review.Draft {
					what = fmt.Sprintf("draft, %d comments", len(r.Comments))
				} else {
					when = r.Submitted.Local().Format("2006-01-02 15:04")
				}
				fmt.Printf("%-4s %-28s %-24s %s\n", r.ID, r.Capsule, what, when)
			}
			return nil
		},
	}
	addMissionFlag(cmd, &slug)
	return cmd
}
