package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/ui"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/spf13/cobra"
)

func newMissionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "mission",
		Aliases: []string{"missions"},
		Short:   "Track units of work and the capsules they own",
		Long: `A mission is a unit of work the workspace owns: a brief, the capsules
(possibly across repos) that carry it out, and a log of what happened.

Commands that take a mission use, in order: the --mission flag or slug
argument, $WS_MISSION, then the mission owning the capsule you stand in.

With WS_MISSION set, ws lift and ws dock attach new capsules to the mission.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMissionList(cmd, "", false)
		},
	}
	cmd.AddCommand(newMissionNewCmd())
	cmd.AddCommand(newMissionListCmd())
	cmd.AddCommand(newMissionShowCmd())
	cmd.AddCommand(newMissionNoteCmd())
	cmd.AddCommand(newMissionBriefCmd())
	cmd.AddCommand(newMissionTitleCmd())
	cmd.AddCommand(newMissionReadyCmd())
	cmd.AddCommand(newMissionAttachCmd())
	cmd.AddCommand(newMissionDetachCmd())
	cmd.AddCommand(newMissionStateCmd("park", mission.Parked, "Park a mission (paused work)"))
	cmd.AddCommand(newMissionStateCmd("archive", mission.Archived, "Archive a mission (keeps its files)"))
	cmd.AddCommand(newMissionStateCmd("reopen", "", "Clear a parked or archived state"))
	return cmd
}

func completeMissionSlugs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx, err := LoadContext()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	missions, _ := missionStore(ctx).List()
	var out []string
	for _, m := range missions {
		out = append(out, m.Slug+"\t"+m.Title)
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// --- new ---

func newMissionNewCmd() *cobra.Command {
	var brief, briefFile string
	var noEdit bool
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Create a mission",
		Long: `Create a mission and open its brief in $EDITOR.

Pass --brief or --brief-file (- for stdin) to write the brief without an
editor, e.g. from a script or an agent.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			if brief != "" && briefFile != "" {
				return errors.New("use either --brief or --brief-file, not both")
			}
			body := brief
			if briefFile != "" {
				var data []byte
				if briefFile == "-" {
					data, err = io.ReadAll(os.Stdin)
				} else {
					data, err = os.ReadFile(briefFile)
				}
				if err != nil {
					return fmt.Errorf("reading brief: %w", err)
				}
				body = string(data)
			}

			store := missionStore(ctx)
			m, err := store.Create(strings.Join(args, " "))
			if err != nil {
				return err
			}
			if body != "" {
				if !strings.HasSuffix(body, "\n") {
					body += "\n"
				}
				if err := os.WriteFile(store.BriefPath(m.Slug), []byte(body), 0o644); err != nil {
					return err
				}
			} else if !noEdit && ui.IsInteractive() {
				if err := openEditor(store.BriefPath(m.Slug)); err != nil {
					fmt.Fprintf(ui.Stderr, "  %s editor: %v\n", ui.Orange.Render("⚠"), err)
				}
			}

			fmt.Fprintf(ui.Stderr, "  %s Mission %s created\n", ui.Green.Render("✓"), ui.Bold.Render(m.Slug))
			fmt.Fprintf(ui.Stderr, "  %s brief: %s\n", ui.Dim.Render("·"), store.BriefPath(m.Slug))
			return nil
		},
	}
	cmd.Flags().StringVar(&brief, "brief", "", "brief markdown (skips the editor)")
	cmd.Flags().StringVar(&briefFile, "brief-file", "", "read the brief from a file, or - for stdin")
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, "don't open $EDITOR")
	return cmd
}

func openEditor(path string) error {
	c := editorCmd(path)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stderr, os.Stderr
	return c.Run()
}

// editorCmd builds a command running $EDITOR (which may carry arguments, e.g.
// "code -w") on path, defaulting to vim.
func editorCmd(path string) *exec.Cmd {
	editor := strings.Fields(os.Getenv("EDITOR"))
	if len(editor) == 0 {
		editor = []string{"vim"}
	}
	return exec.Command(editor[0], append(editor[1:], path)...)
}

// --- list ---

func newMissionListCmd() *cobra.Command {
	var format string
	var all bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List missions",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMissionList(cmd, format, all)
		},
	}
	cmd.Flags().StringVarP(&format, "format", "f", "", "output format: llm")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "include archived missions")
	return cmd
}

func runMissionList(cmd *cobra.Command, format string, all bool) error {
	ctx, err := LoadContext()
	if err != nil {
		return err
	}
	missions, err := missionStore(ctx).List()
	if err != nil {
		return err
	}
	views := buildMissionViews(ctx.WS, ctx.GitHub, missions)
	shown := views[:0:0]
	hidden := 0
	for _, v := range views {
		if v.State == mission.Archived && !all {
			hidden++
			continue
		}
		shown = append(shown, v)
	}

	if format == "llm" {
		var b strings.Builder
		if len(shown) == 0 {
			b.WriteString("No missions.\n")
		}
		for _, v := range shown {
			fmt.Fprintf(&b, "%s [%s] %s", v.M.Slug, v.State, v.M.Title)
			if names := capsuleNames(v); names != "" {
				fmt.Fprintf(&b, " — %s", names)
			}
			b.WriteByte('\n')
		}
		_, err := os.Stdout.WriteString(b.String())
		return err
	} else if format != "" {
		return fmt.Errorf("unknown format %q (want llm)", format)
	}

	if len(shown) == 0 {
		fmt.Fprintf(ui.Stderr, "  No missions. Create one with %s\n", ui.Bold.Render(`ws mission new "<title>"`))
		if hidden > 0 {
			fmt.Fprintf(ui.Stderr, "  %s %d archived (ws mission list --all)\n", ui.Dim.Render("·"), hidden)
		}
		return nil
	}

	slugW, stateW := 0, 0
	for _, v := range shown {
		slugW = max(slugW, lipgloss.Width(v.M.Slug))
		stateW = max(stateW, len(v.State))
	}
	for _, v := range shown {
		line := fmt.Sprintf("  %-*s  %s  %s", slugW, v.M.Slug,
			stateStyle(v.State).Render(fmt.Sprintf("%-*s", stateW, v.State)), v.M.Title)
		if names := capsuleNames(v); names != "" {
			line += "  " + ui.Dim.Render(names)
		}
		fmt.Fprintln(ui.Stderr, line)
	}
	if hidden > 0 {
		fmt.Fprintf(ui.Stderr, "  %s %d archived (ws mission list --all)\n", ui.Dim.Render("·"), hidden)
	}
	return nil
}

func capsuleNames(v missionView) string {
	names := make([]string, len(v.Capsules))
	for i, c := range v.Capsules {
		names[i] = c.Repo + "/" + c.Name
	}
	return strings.Join(names, ", ")
}

func stateStyle(s mission.State) lipgloss.Style {
	switch s {
	case mission.Active:
		return ui.Blue
	case mission.Review:
		return ui.Orange
	case mission.Landed:
		return ui.Green
	case mission.Briefing:
		return ui.Orange
	default:
		return ui.Dim
	}
}

// --- show ---

const missionShowLogLimit = 10

func newMissionShowCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:               "show [mission]",
		Short:             "Show a mission's brief, capsules and recent log",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeMissionSlugs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			m, err := resolveMission(ctx, firstArg(args))
			if err != nil {
				return err
			}
			store := missionStore(ctx)
			v := buildMissionViews(ctx.WS, ctx.GitHub, []*mission.Mission{m})[0]
			brief, _ := store.ReadBrief(m.Slug)
			events, _ := store.ReadLog(m.Slug, missionShowLogLimit)

			switch format {
			case "llm":
				_, err := os.Stdout.WriteString(formatMissionLLM(ctx, v, store.BriefPath(m.Slug), brief, events))
				return err
			case "":
				printMissionText(ctx, v, store.BriefPath(m.Slug), brief, events)
				return nil
			default:
				return fmt.Errorf("unknown format %q (want llm)", format)
			}
		},
	}
	cmd.Flags().StringVarP(&format, "format", "f", "", "output format: llm")
	return cmd
}

func firstArg(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

func capsuleFacts(c missionCapsuleView) string {
	var parts []string
	if !c.Exists {
		parts = append(parts, "burned")
	} else if c.Branch != "" && c.Branch != c.Name {
		parts = append(parts, "branch "+c.Branch)
	}
	if c.PR != nil {
		state := "open"
		if c.PRS == mission.PRMerged {
			state = "merged"
		}
		parts = append(parts, fmt.Sprintf("PR #%d %s", c.PR.Number, state))
	}
	return strings.Join(parts, ", ")
}

func formatMissionLLM(ctx *Context, v missionView, briefPath, brief string, events []mission.Event) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Mission: %s\n", v.M.Slug)
	fmt.Fprintf(&b, "Title: %s\n", v.M.Title)
	fmt.Fprintf(&b, "State: %s\n", v.State)
	if v.M.Request != "" {
		fmt.Fprintf(&b, "Requested as: %s\n", strings.ReplaceAll(v.M.Request, "\n", " "))
	}
	fmt.Fprintf(&b, "Brief file: %s\n", briefPath)
	if len(v.Capsules) == 0 {
		b.WriteString("Capsules: none yet. Create one with `ws lift <repo> <name>`; with WS_MISSION set it joins this mission.\n")
	} else {
		b.WriteString("Capsules:\n")
		for _, c := range v.Capsules {
			fmt.Fprintf(&b, "  %s/%s", c.Repo, c.Name)
			if c.Exists {
				fmt.Fprintf(&b, " — %s", c.Path)
			}
			if f := capsuleFacts(c); f != "" {
				fmt.Fprintf(&b, " [%s]", f)
			}
			b.WriteByte('\n')
		}
	}
	b.WriteString("\n## Brief\n\n")
	if strings.TrimSpace(brief) == "" {
		b.WriteString("(empty)\n")
	} else {
		b.WriteString(strings.TrimRight(brief, "\n") + "\n")
	}
	if len(events) > 0 {
		b.WriteString("\n## Recent log\n\n")
		for _, e := range events {
			b.WriteString(formatEventLine(e, true) + "\n")
		}
	}
	return b.String()
}

func printMissionText(ctx *Context, v missionView, briefPath, brief string, events []mission.Event) {
	w := ui.Stderr
	fmt.Fprintf(w, "\n  %s  %s  %s\n", ui.Bold.Render(v.M.Title), ui.TagDim.Render(v.M.Slug), stateStyle(v.State).Render(string(v.State)))

	fmt.Fprintf(w, "\n  %s\n", ui.Bold.Render("Capsules"))
	if len(v.Capsules) == 0 {
		fmt.Fprintf(w, "    %s\n", ui.Dim.Render("none yet"))
	}
	nameW := 0
	for _, c := range v.Capsules {
		nameW = max(nameW, lipgloss.Width(ctx.WS.FormatRepoName(c.Repo)))
	}
	for _, c := range v.Capsules {
		repo := ctx.WS.FormatRepoName(c.Repo)
		pad := strings.Repeat(" ", nameW-lipgloss.Width(repo))
		line := fmt.Sprintf("    %s%s  %s", repo, pad, ui.TagDim.Render(c.Name))
		if f := capsuleFacts(c); f != "" {
			line += "  " + ui.Dim.Render(f)
		}
		fmt.Fprintln(w, line)
	}

	fmt.Fprintf(w, "\n  %s  %s\n", ui.Bold.Render("Brief"), ui.Dim.Render(briefPath))
	if strings.TrimSpace(brief) == "" {
		fmt.Fprintf(w, "    %s\n", ui.Dim.Render("(empty)"))
	}
	for line := range strings.SplitSeq(strings.TrimRight(brief, "\n"), "\n") {
		if strings.TrimSpace(brief) != "" {
			fmt.Fprintf(w, "    %s\n", line)
		}
	}

	if len(events) > 0 {
		fmt.Fprintf(w, "\n  %s\n", ui.Bold.Render("Log"))
		for _, e := range events {
			fmt.Fprintf(w, "    %s\n", formatEventLine(e, false))
		}
	}
	fmt.Fprintln(w)
}

func formatEventLine(e mission.Event, plain bool) string {
	ts := e.Time.Local().Format("2006-01-02 15:04")
	if plain {
		ts = e.Time.UTC().Format("2006-01-02T15:04Z")
	}
	var detail []string
	if e.Repo != "" || e.Capsule != "" {
		detail = append(detail, e.Repo+"/"+e.Capsule)
	}
	if e.Text != "" {
		detail = append(detail, e.Text)
	}
	who := ""
	if e.Crew != "" {
		who = " (" + e.Crew + ")"
	}
	if plain {
		return fmt.Sprintf("%s %s%s %s", ts, e.Kind, who, strings.Join(detail, " — "))
	}
	return fmt.Sprintf("%s  %s%s  %s", ui.Dim.Render(ts), e.Kind, ui.Dim.Render(who), strings.Join(detail, " — "))
}

// --- note ---

func newMissionNoteCmd() *cobra.Command {
	var slug string
	cmd := &cobra.Command{
		Use:   "note <text>",
		Short: "Append a note to a mission's log",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			m, err := resolveMission(ctx, slug)
			if err != nil {
				return err
			}
			text := strings.TrimSpace(strings.Join(args, " "))
			if text == "" {
				return errors.New("note text is empty")
			}
			if err := missionStore(ctx).AppendLog(m.Slug, mission.Event{Kind: mission.EventNote, Text: text}); err != nil {
				return err
			}
			fmt.Fprintf(ui.Stderr, "  %s Noted on %s\n", ui.Green.Render("✓"), m.Slug)
			return nil
		},
	}
	addMissionFlag(cmd, &slug)
	return cmd
}

func addMissionFlag(cmd *cobra.Command, slug *string) {
	cmd.Flags().StringVarP(slug, "mission", "m", "", "mission slug (default: $WS_MISSION, then the current capsule's mission)")
	cmd.RegisterFlagCompletionFunc("mission", completeMissionSlugs)
}

// --- attach / detach ---

func newMissionAttachCmd() *cobra.Command {
	var slug string
	var move bool
	cmd := &cobra.Command{
		Use:   "attach <repo> <capsule>",
		Short: "Add an existing capsule to a mission",
		Args:  cobra.ExactArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeRepoNames(cmd, args, toComplete)
			}
			if len(args) == 1 {
				return completeWorktreeNames(0)(cmd, args, toComplete)
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			m, err := resolveMission(ctx, slug)
			if err != nil {
				return err
			}
			repo, err := ctx.ResolveRepoStrict(args[0])
			if err != nil {
				return err
			}
			capsule, err := ctx.ResolveWorktreeStrict(repo, args[1])
			if err != nil {
				return err
			}
			if capsule == ".ground" {
				return errors.New(".ground can't belong to a mission")
			}

			store := missionStore(ctx)
			if owner, ok := store.FindByCapsule(repo, capsule); ok && owner.Slug != m.Slug {
				if !move {
					return fmt.Errorf("%s/%s already belongs to mission %s (use --move)", repo, capsule, owner.Slug)
				}
				if _, err := store.Update(owner.Slug, func(o *mission.Mission) error {
					o.RemoveCapsule(repo, capsule)
					return nil
				}); err != nil {
					return err
				}
				store.AppendLog(owner.Slug, mission.Event{Kind: mission.EventCapsuleRemove, Repo: repo, Capsule: capsule, Text: "moved to " + m.Slug})
			}

			if err := attachCapsule(ctx.WS, m.Slug, repo, capsule); err != nil {
				return err
			}
			fmt.Fprintf(ui.Stderr, "  %s %s %s → mission %s\n", ui.Green.Render("✓"), ctx.WS.FormatRepoName(repo), ui.TagDim.Render(capsule), m.Slug)
			return nil
		},
	}
	addMissionFlag(cmd, &slug)
	cmd.Flags().BoolVar(&move, "move", false, "take the capsule from the mission that owns it")
	return cmd
}

// attachCapsule adds a capsule (with its current branch) to a mission and logs it.
func attachCapsule(ws *workspace.Workspace, slug, repo, capsule string) error {
	store := mission.Store{Root: ws.Root}
	branch := workspace.GitCurrentBranch(filepath.Join(ws.RepoDir(repo), capsule))
	added := false
	if _, err := store.Update(slug, func(m *mission.Mission) error {
		added = m.AddCapsuleBranch(repo, capsule, branch)
		return nil
	}); err != nil {
		return err
	}
	if added {
		return store.AppendLog(slug, mission.Event{Kind: mission.EventCapsuleAdded, Repo: repo, Capsule: capsule})
	}
	return nil
}

// detachCapsule removes a capsule from a mission and logs it. It reports
// whether the mission owned the capsule.
func detachCapsule(ws *workspace.Workspace, slug, repo, capsule string) (bool, error) {
	store := mission.Store{Root: ws.Root}
	removed := false
	if _, err := store.Update(slug, func(m *mission.Mission) error {
		removed = m.RemoveCapsule(repo, capsule)
		return nil
	}); err != nil || !removed {
		return removed, err
	}
	return true, store.AppendLog(slug, mission.Event{Kind: mission.EventCapsuleRemove, Repo: repo, Capsule: capsule})
}

// setMissionState stores a manual state ("" reopens) and logs it.
func setMissionState(ws *workspace.Workspace, slug string, state mission.State) (string, error) {
	store := mission.Store{Root: ws.Root}
	if _, err := store.Update(slug, func(m *mission.Mission) error {
		m.State = state
		return nil
	}); err != nil {
		return "", err
	}
	label := string(state)
	if state == "" {
		label = "reopened"
	}
	return label, store.AppendLog(slug, mission.Event{Kind: mission.EventState, Text: label})
}

func newMissionDetachCmd() *cobra.Command {
	var slug string
	cmd := &cobra.Command{
		Use:   "detach <repo> <capsule>",
		Short: "Remove a capsule from a mission (the capsule itself is kept)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			m, err := resolveMission(ctx, slug)
			if err != nil {
				return err
			}
			// Fall back to the literal name so a mission can drop capsules of a
			// repo that has since left ws.toml (ws doctor suggests this).
			repo, err := ctx.ResolveRepoStrict(args[0])
			if err != nil {
				if !m.HasCapsule(args[0], args[1]) {
					return err
				}
				repo = args[0]
			}
			capsule := args[1]
			removed, err := detachCapsule(ctx.WS, m.Slug, repo, capsule)
			if err != nil {
				return err
			}
			if !removed {
				return fmt.Errorf("%s/%s is not part of mission %s", repo, capsule, m.Slug)
			}
			fmt.Fprintf(ui.Stderr, "  %s %s %s detached from %s\n", ui.Green.Render("✓"), ctx.WS.FormatRepoName(repo), ui.TagDim.Render(capsule), m.Slug)
			return nil
		},
	}
	addMissionFlag(cmd, &slug)
	return cmd
}

// --- park / archive / reopen ---

func newMissionStateCmd(name string, state mission.State, short string) *cobra.Command {
	return &cobra.Command{
		Use:               name + " [mission]",
		Short:             short,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeMissionSlugs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			m, err := resolveMission(ctx, firstArg(args))
			if err != nil {
				return err
			}
			label, err := setMissionState(ctx.WS, m.Slug, state)
			if err != nil {
				return err
			}
			fmt.Fprintf(ui.Stderr, "  %s %s %s\n", ui.Green.Render("✓"), m.Slug, label)
			return nil
		},
	}
}
