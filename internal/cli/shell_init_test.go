package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestShellInitZsh(t *testing.T) {
	cmd := NewRootCmd("test")
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"shell-init", "zsh"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("shell-init zsh failed: %v", err)
	}

	output := buf.String()

	// Must contain ws() function definition
	if !strings.Contains(output, "ws()") {
		t.Error("output missing ws() function definition")
	}

	// Must contain eval for jump/use commands
	if !strings.Contains(output, "eval") {
		t.Error("output missing eval in ws() function")
	}

	// Must contain workspace invocation
	if !strings.Contains(output, "command workspace") {
		t.Error("output missing 'command workspace' invocation")
	}

	// Must contain case for eval-able commands
	for _, cmd := range []string{"jump", "lift", "dock", "init", "mc"} {
		if !strings.Contains(output, cmd) {
			t.Errorf("output missing %s in eval case", cmd)
		}
	}

	// Must contain compdef linking the _workspace completer to ws
	if !strings.Contains(output, "compdef _workspace ws") {
		t.Error("output missing compdef _workspace ws")
	}
}

func TestShellInitUnsupportedShell(t *testing.T) {
	cmd := NewRootCmd("test")
	cmd.SetArgs([]string{"shell-init", "fish"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unsupported shell, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported shell") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestShellInitNoArgs(t *testing.T) {
	cmd := NewRootCmd("test")
	cmd.SetArgs([]string{"shell-init"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no shell specified, got nil")
	}
}
