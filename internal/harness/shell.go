package harness

import (
	"os"
	"os/exec"
)

// Shell runs your $SHELL as crew. It's for working on a mission by hand and
// for checking the deck end to end without an agent installed. It has no
// conversation to resume, and ignores the prompt (the brief is one
// `ws mission show` away).
type Shell struct{}

func init() { register(Shell{}) }

func (Shell) Name() string { return "shell" }

func shellPath() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

func (Shell) Available() error {
	_, err := exec.LookPath(shellPath())
	return err
}

func (Shell) Start(b Brief) (Launch, error) {
	return Launch{Args: []string{shellPath(), "-i"}, Dir: b.Dir}, nil
}

func (Shell) Resume(string, string) (Launch, error) { return Launch{}, ErrNoResume }
