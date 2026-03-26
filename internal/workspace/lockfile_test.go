package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLockFileAcquireRelease(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "silo.lock")

	// First acquire should succeed
	if err := AcquireLockFile(lockPath); err != nil {
		t.Fatalf("first AcquireLockFile() error: %v", err)
	}

	// Verify lock file exists with our PID
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("reading lock file: %v", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		t.Error("lock file should contain a PID")
	}

	// Second acquire should fail (our process is still alive)
	if err := AcquireLockFile(lockPath); err == nil {
		t.Error("second AcquireLockFile() should fail while lock is held")
	} else if !strings.Contains(err.Error(), "already running") {
		t.Errorf("error = %q, want message about already running", err.Error())
	}

	// Release the lock
	ReleaseLockFile(lockPath)

	// Verify lock file is gone
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("lock file should be removed after release")
	}

	// Acquire again should succeed
	if err := AcquireLockFile(lockPath); err != nil {
		t.Fatalf("AcquireLockFile() after release error: %v", err)
	}
}

func TestLockFileStalePID(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "silo.lock")

	// Write a lock file with a PID that almost certainly doesn't exist
	os.WriteFile(lockPath, []byte("999999999"), 0644)

	// Acquire should succeed because the PID is dead
	if err := AcquireLockFile(lockPath); err != nil {
		t.Fatalf("AcquireLockFile() with stale PID error: %v", err)
	}

	// Verify the lock now contains our PID
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("reading lock file: %v", err)
	}
	if strings.TrimSpace(string(data)) == "999999999" {
		t.Error("lock file should have been updated with current PID")
	}
}

func TestLockFile_NoExistingFile(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "nonexistent", "silo.lock")

	// Should fail because parent directory doesn't exist
	// (this tests the edge case — lock file creation requires the parent to exist)
	err := AcquireLockFile(lockPath)
	if err == nil {
		t.Error("expected error when parent directory doesn't exist")
	}
}

func TestReleaseLockFile_NoFile(t *testing.T) {
	// Releasing a nonexistent lock should not panic
	ReleaseLockFile(filepath.Join(t.TempDir(), "nonexistent.lock"))
}
