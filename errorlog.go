package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// errorLogName is the file, kept directly in the user's home folder, where
// derpy records tracks it could not load or play. Living at the top of the
// home folder (rather than under ~/.config) makes failures easy to find.
const errorLogName = "derpy-errors.log"

// errorLogPath returns the absolute path to derpy's playback error log. It
// falls back to a relative path in the working directory if the home folder
// cannot be resolved, so logging degrades gracefully rather than panicking.
func errorLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return errorLogName
	}
	return filepath.Join(home, errorLogName)
}

// logTrackError appends a timestamped, tab-separated entry describing a track
// that failed to load or play. It is best-effort: any error opening or writing
// the log is returned so the caller can surface it, but playback should
// continue regardless of the outcome.
func logTrackError(cause error) error {
	f, err := os.OpenFile(errorLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	line := fmt.Sprintf("%s\t%v\n", time.Now().Format(time.RFC3339), cause)
	if _, err := f.WriteString(line); err != nil {
		return err
	}
	return nil
}
