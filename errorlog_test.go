package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLogTrackErrorAppendsToHomeFile verifies logTrackError creates the log
// in the user's home folder and appends one line per call. USERPROFILE is
// redirected to a temp dir (os.UserHomeDir honours it first on Windows) so the
// test never touches the real home folder.
func TestLogTrackErrorAppendsToHomeFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home) // for os.UserHomeDir on non-Windows

	if err := logTrackError(fmt.Errorf("failed to load track %q: boom", "a.mp3")); err != nil {
		t.Fatalf("first logTrackError: %v", err)
	}
	if err := logTrackError(fmt.Errorf("failed to load track %q: bang", "b.mp3")); err != nil {
		t.Fatalf("second logTrackError: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, errorLogName))
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines, got %d: %q", len(lines), string(data))
	}
	if !strings.Contains(lines[0], "a.mp3") || !strings.Contains(lines[1], "b.mp3") {
		t.Errorf("log lines missing track paths: %q", string(data))
	}
}
