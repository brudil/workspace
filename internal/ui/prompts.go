package ui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

func IsInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func PickRepo(repos []string, displayNames map[string]string) (string, error) {
	if !IsInteractive() {
		return "", fmt.Errorf("no repo specified and not in an interactive terminal\n\nUsage: pass repo as argument or run from inside a repo directory")
	}

	var selected string
	opts := make([]huh.Option[string], len(repos))
	for i, r := range repos {
		label := r
		if dn, ok := displayNames[r]; ok {
			label = fmt.Sprintf("%s (%s)", dn, r)
		}
		opts[i] = huh.NewOption(label, r)
	}

	err := huh.NewSelect[string]().
		Title("Select repo").
		Options(opts...).
		Value(&selected).
		Run()

	return selected, err
}

func PickWorktree(worktrees []string) (string, error) {
	if !IsInteractive() {
		return "", fmt.Errorf("no branch specified and not in an interactive terminal\n\nUsage: pass branch as argument or run from inside a worktree")
	}

	var selected string
	opts := make([]huh.Option[string], len(worktrees))
	for i, w := range worktrees {
		opts[i] = huh.NewOption(w, w)
	}

	err := huh.NewSelect[string]().
		Title("Select worktree").
		Options(opts...).
		Value(&selected).
		Run()

	return selected, err
}

// PickMultiple shows a multi-select picker and returns indices of selected items.
func PickMultiple(title string, options []string, descriptions []string) ([]int, error) {
	if !IsInteractive() {
		return nil, fmt.Errorf("interactive terminal required for multi-select")
	}

	type indexedOption struct {
		Index int
	}

	var selected []int
	opts := make([]huh.Option[int], len(options))
	for i, o := range options {
		label := o
		if i < len(descriptions) && descriptions[i] != "" {
			label = fmt.Sprintf("%s  %s", o, descriptions[i])
		}
		opts[i] = huh.NewOption(label, i)
	}

	err := huh.NewMultiSelect[int]().
		Title(title).
		Options(opts...).
		Value(&selected).
		Run()

	return selected, err
}

func Confirm(message string) (bool, error) {
	if !IsInteractive() {
		return false, fmt.Errorf("confirmation required but not in an interactive terminal")
	}

	var confirmed bool
	err := huh.NewConfirm().
		Title(message).
		Value(&confirmed).
		Run()

	return confirmed, err
}
