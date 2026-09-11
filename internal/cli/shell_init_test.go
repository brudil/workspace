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

	// Must contain case for eval-able commands. The label ends at mc: burn is
	// deliberately not in it.
	if !strings.Contains(output, "jump|j|lift|dock|init|mc)") {
		t.Error("output missing expected eval case pattern")
	}
	for _, cmd := range []string{"jump", "lift", "dock", "init", "mc"} {
		if !strings.Contains(output, cmd) {
			t.Errorf("output missing %s in eval case", cmd)
		}
	}

	// burn gets its own branch: it must not be run inside a command
	// substitution, because the shell has to step out of the capsule before
	// the worktree is removed rather than be repositioned afterwards.
	if !strings.Contains(output, "burn|rm)") || !strings.Contains(output, "__ws_burn") {
		t.Error("output missing burn branch calling __ws_burn")
	}
	if !strings.Contains(output, `builtin cd -q -- "$root"`) {
		t.Error("burn helper does not step the shell out to the workspace root")
	}
	if !strings.Contains(output, `WS_ORIGIN_PWD=$origin command workspace "$@"`) {
		t.Error("burn helper does not report the shell's directory to the binary")
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
