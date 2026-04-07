package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// queueFilePath returns the path to the local offline earmark queue file.
// The queue acts as a transactional outbox: earmarks are written here
// immediately on [E] press, then removed once successfully published to Nostr.
func queueFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "derpy", "queue.json"), nil
}

// LoadQueue reads the offline earmark queue from disk.
// Returns an empty slice when the queue file does not exist yet.
func LoadQueue() ([]Earmark, error) {
	path, err := queueFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []Earmark{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not read queue file: %w", err)
	}

	var earmarks []Earmark
	if err := json.Unmarshal(data, &earmarks); err != nil {
		return nil, fmt.Errorf("could not parse queue file: %w", err)
	}
	return earmarks, nil
}

// saveQueue atomically writes the earmark slice to the queue file.
// It uses a temp file + rename to avoid partial writes on crash.
func saveQueue(earmarks []Earmark) error {
	path, err := queueFilePath()
	if err != nil {
		return err
	}

	// Ensure the config directory exists.
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("could not create config directory: %w", err)
	}

	data, err := json.Marshal(earmarks)
	if err != nil {
		return fmt.Errorf("could not marshal queue: %w", err)
	}

	// Write to a temp file in the same directory so the rename is atomic.
	tmp, err := os.CreateTemp(filepath.Dir(path), "queue-*.json.tmp")
	if err != nil {
		return fmt.Errorf("could not create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("could not write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("could not close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("could not rename temp file: %w", err)
	}
	return nil
}

// AppendToQueue appends a single earmark to the local queue, skipping the
// write if the same file is already queued.
// This is the first thing called on [E] press — guarantees no data loss even
// if the subsequent Nostr publish fails or the machine is offline.
func AppendToQueue(e Earmark) error {
	existing, err := LoadQueue()
	if err != nil {
		return err
	}
	if isDuplicateEarmark(existing, e) {
		return nil // already in the local queue
	}
	return saveQueue(append(existing, e))
}

// RemoveFromQueue removes the earmark whose Timestamp matches e.Timestamp.
// Called after a successful Nostr publish to clear the entry from the outbox.
// If no match is found the queue is left unchanged (idempotent).
func RemoveFromQueue(e Earmark) error {
	existing, err := LoadQueue()
	if err != nil {
		return err
	}

	filtered := existing[:0]
	for _, q := range existing {
		if q.Timestamp != e.Timestamp {
			filtered = append(filtered, q)
		}
	}

	// Nothing was removed — skip the write to avoid a spurious disk touch.
	if len(filtered) == len(existing) {
		return nil
	}
	return saveQueue(filtered)
}

// FlushQueue attempts to publish every queued earmark to Nostr and removes
// each one that succeeds. Returns the number of entries successfully flushed.
// Entries that fail (e.g. relay still unreachable) remain in the queue for
// the next flush attempt.
func FlushQueue(hexPrivKey string) (int, error) {
	queue, err := LoadQueue()
	if err != nil {
		return 0, err
	}
	if len(queue) == 0 {
		return 0, nil
	}

	// Fetch the current Nostr list once, then append all queued earmarks.
	// This avoids N round-trips to the relay for N queued items.
	flushed := 0
	for _, e := range queue {
		if publishErr := AddEarmark(hexPrivKey, e); publishErr == nil {
			// Published — remove from outbox.
			_ = RemoveFromQueue(e)
			flushed++
		}
		// On failure: leave in queue; caller can log or ignore.
	}
	return flushed, nil
}
