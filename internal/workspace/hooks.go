package workspace

import (
	"io"
	"os/exec"
)

// RunHook executes a shell command in the given directory, streaming output.
func RunHook(dir, command string, stdout, stderr io.Writer) error {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
