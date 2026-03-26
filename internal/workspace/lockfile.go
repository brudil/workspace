package workspace

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// AcquireLockFile creates a lock file with the current PID.
// Returns an error if another live process holds the lock.
func AcquireLockFile(path string) error {
	if data, err := os.ReadFile(path); err == nil {
		pidStr := strings.TrimSpace(string(data))
		if pid, err := strconv.Atoi(pidStr); err == nil {
			proc, err := os.FindProcess(pid)
			if err == nil {
				if err := proc.Signal(syscall.Signal(0)); err == nil {
					return fmt.Errorf("silo watch already running (PID %d)", pid)
				}
			}
		}
		os.Remove(path)
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644)
}

// ReleaseLockFile removes the lock file.
func ReleaseLockFile(path string) {
	os.Remove(path)
}
