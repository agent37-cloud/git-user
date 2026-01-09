package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// createTempDB creates a temporary database for testing
func createTempDB(t *testing.T) (*Store, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := createStore(dbPath)
	require.NoError(t, err, "failed to create test database")

	cleanup := func() {
		if store != nil && store.db != nil {
			store.db.Close()
		}
	}

	return store, cleanup
}

// createTempGitRepo creates a temporary git repository for testing
func createTempGitRepo(t *testing.T) (string, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	
	// Initialize git repo
	cmd := exec.Command("git", "init", tmpDir)
	err := cmd.Run()
	require.NoError(t, err, "failed to initialize git repo")

	// Configure git to avoid warnings
	cmd = exec.Command("git", "-C", tmpDir, "config", "user.name", "Test User")
	_ = cmd.Run()
	cmd = exec.Command("git", "-C", tmpDir, "config", "user.email", "test@example.com")
	_ = cmd.Run()

	cleanup := func() {
		// tmpDir is automatically cleaned by t.TempDir()
	}

	return tmpDir, cleanup
}

// withTimeout creates a context with timeout for testing
func withTimeout(t *testing.T, duration time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	t.Cleanup(cancel)
	return ctx
}

// withCancelledContext creates an already-cancelled context for testing
func withCancelledContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// setEnv sets an environment variable for the duration of the test
func setEnv(t *testing.T, key, value string) {
	t.Helper()
	oldValue, existed := os.LookupEnv(key)
	err := os.Setenv(key, value)
	require.NoError(t, err, "failed to set environment variable")

	t.Cleanup(func() {
		if existed {
			os.Setenv(key, oldValue)
		} else {
			os.Unsetenv(key)
		}
	})
}

// makeReadOnly makes a file or directory read-only
func makeReadOnly(t *testing.T, path string) {
	t.Helper()
	err := os.Chmod(path, 0444)
	require.NoError(t, err, "failed to make path read-only")
}

// writeCorruptDB creates a corrupted database file
func writeCorruptDB(t *testing.T, path string) {
	t.Helper()
	err := os.MkdirAll(filepath.Dir(path), 0755)
	require.NoError(t, err, "failed to create directory")
	
	err = os.WriteFile(path, []byte("not a valid sqlite database"), 0644)
	require.NoError(t, err, "failed to write corrupt database")
}

// chdir changes directory for the test and restores it on cleanup
func chdir(t *testing.T, dir string) {
	t.Helper()
	origDir, err := os.Getwd()
	require.NoError(t, err, "failed to get current directory")

	err = os.Chdir(dir)
	require.NoError(t, err, "failed to change directory")

	t.Cleanup(func() {
		os.Chdir(origDir)
	})
}
