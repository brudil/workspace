package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"text/template"

	"github.com/brudil/workspace/internal/config"
	"github.com/brudil/workspace/internal/workspace"
	"github.com/spf13/cobra"
)

// PromptData holds the context available to prompt templates and JSON output.
type PromptData struct {
	WorkspaceDisplayName string `json:"workspace_display_name"`
	RepoName             string `json:"repo_name"`
	RepoDisplayName      string `json:"repo_display_name"`
	RepoColor            string `json:"repo_color"`
	CapsuleName          string `json:"capsule_name"`
	IsCapsuleBoarded     bool   `json:"is_capsule_boarded"`
}

func newPromptCmd() *cobra.Command {
	var format string
	var tmpl string

	cmd := &cobra.Command{
		Use:           "prompt",
		Short:         "Output workspace context for shell prompts",
		Long:          "Outputs current repo and worktree info for use in shell prompts (starship, p10k, PS1).\nSilently exits with no output when not inside a workspace repo.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrompt(format, tmpl)
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "short", "Output format: short, json")
	cmd.Flags().StringVarP(&tmpl, "template", "t", "", "Go template for custom output (e.g. '{{.RepoName}}:{{.CapsuleName}}')")
	cmd.RegisterFlagCompletionFunc("format", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"short", "json"}, cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}

func runPrompt(format, tmpl string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return nil // silent
	}

	data, ok := resolvePromptData(cwd)
	if !ok {
		return nil
	}

	return formatPrompt(os.Stdout, data, format, tmpl)
}

// resolvePromptData detects workspace context from the given directory.
// Returns false if not inside a workspace repo.
func resolvePromptData(cwd string) (PromptData, bool) {
	root, err := config.Discover(cwd)
	if err != nil {
		return PromptData{}, false
	}

	repo, wt, ok := workspace.DetectRepo(root, cwd)
	if !ok {
		return PromptData{}, false
	}

	cfg, _, err := config.Load(root)
	if err != nil {
		return PromptData{}, false
	}

	wsName := cfg.Workspace.DisplayName
	if wsName == "" {
		wsName = cfg.Workspace.Org
	}

	repoDisplayName := repo
	var color string
	if rc, ok := cfg.Repos[repo]; ok {
		if rc.DisplayName != "" {
			repoDisplayName = rc.DisplayName
		}
		color = rc.Color
	}

	var isBoarded bool
	if slices.Contains(cfg.Boarded[repo], wt) {
		isBoarded = true
	}

	return PromptData{
		WorkspaceDisplayName: wsName,
		RepoName:             repo,
		RepoDisplayName:      repoDisplayName,
		RepoColor:            color,
		CapsuleName:          wt,
		IsCapsuleBoarded:     isBoarded,
	}, true
}

// formatPrompt writes prompt data to w in the requested format.
func formatPrompt(w *os.File, data PromptData, format, tmpl string) error {
	switch {
	case tmpl != "":
		t, err := template.New("prompt").Parse(tmpl)
		if err != nil {
			return nil // bad template — silent
		}
		t.Execute(w, data)
		fmt.Fprintln(w)

	case format == "json":
		json.NewEncoder(w).Encode(data)

	default:
		fmt.Fprintf(w, "%s / %s\n", data.WorkspaceDisplayName, data.RepoDisplayName)
	}

	return nil
}
