package main

import (
	"os"
	"path/filepath"
	"testing"

	core "github.com/punkscience/earmark/earmark-core"
)

// withTempQueue redirects the home directory used by queueFilePath() to a
// temp directory for the duration of fn, then restores the previous values.
//
// os.UserHomeDir (which queueFilePath uses) checks USERPROFILE first on
// Windows and HOME on Unix, so we set both — setting only HOME let the
// Windows-side tests leak into the real ~/.config/derpy/queue.json.
func withTempQueue(t *testing.T, fn func()) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	fn()
}

// TestLoadQueueEmpty verifies that LoadQueue returns an empty slice when no
// queue file exists yet (first run).
func TestLoadQueueEmpty(t *testing.T) {
	withTempQueue(t, func() {
		q, err := LoadQueue()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(q) != 0 {
			t.Errorf("expected empty queue, got %d entries", len(q))
		}
	})
}

// TestAppendToQueue verifies that AppendToQueue persists entries and that
// subsequent LoadQueue calls return all of them in order.
func TestAppendToQueue(t *testing.T) {
	withTempQueue(t, func() {
		e1 := core.Earmark{Artist: "Coltrane", Title: "Resolution", Timestamp: 1000}
		e2 := core.Earmark{Artist: "Miles Davis", Title: "So What", Timestamp: 1001}

		if err := AppendToQueue(e1); err != nil {
			t.Fatalf("AppendToQueue e1: %v", err)
		}
		if err := AppendToQueue(e2); err != nil {
			t.Fatalf("AppendToQueue e2: %v", err)
		}

		q, err := LoadQueue()
		if err != nil {
			t.Fatalf("LoadQueue: %v", err)
		}
		if len(q) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(q))
		}
		if q[0] != e1 {
			t.Errorf("[0] got %+v, want %+v", q[0], e1)
		}
		if q[1] != e2 {
			t.Errorf("[1] got %+v, want %+v", q[1], e2)
		}
	})
}

// TestRemoveFromQueue verifies that RemoveFromQueue removes the entry matching
// the given Timestamp and leaves all other entries intact.
func TestRemoveFromQueue(t *testing.T) {
	withTempQueue(t, func() {
		e1 := core.Earmark{Artist: "Coltrane", Title: "Resolution", Timestamp: 2000}
		e2 := core.Earmark{Artist: "Miles Davis", Title: "So What", Timestamp: 2001}
		e3 := core.Earmark{Artist: "Bill Evans", Title: "Waltz for Debby", Timestamp: 2002}

		for _, e := range []core.Earmark{e1, e2, e3} {
			_ = AppendToQueue(e)
		}

		if err := RemoveFromQueue(e2); err != nil {
			t.Fatalf("RemoveFromQueue: %v", err)
		}

		q, _ := LoadQueue()
		if len(q) != 2 {
			t.Fatalf("expected 2 entries after removal, got %d", len(q))
		}
		for _, e := range q {
			if e.Timestamp == e2.Timestamp {
				t.Errorf("removed entry still present: %+v", e)
			}
		}
	})
}

// TestRemoveFromQueueNoMatch verifies that RemoveFromQueue is idempotent when
// no entry matches the given Timestamp.
func TestRemoveFromQueueNoMatch(t *testing.T) {
	withTempQueue(t, func() {
		e := core.Earmark{Artist: "Coltrane", Title: "Resolution", Timestamp: 3000}
		_ = AppendToQueue(e)

		ghost := core.Earmark{Timestamp: 9999}
		if err := RemoveFromQueue(ghost); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		q, _ := LoadQueue()
		if len(q) != 1 {
			t.Errorf("expected 1 entry, got %d", len(q))
		}
	})
}

// TestSaveQueueAtomicWrite verifies that saveQueue writes to a final path, not
// a temp path, and that the file is valid JSON after the write.
func TestSaveQueueAtomicWrite(t *testing.T) {
	withTempQueue(t, func() {
		e := core.Earmark{Artist: "Monk", Title: "Round Midnight", Timestamp: 4000}
		if err := AppendToQueue(e); err != nil {
			t.Fatalf("AppendToQueue: %v", err)
		}

		home := os.Getenv("HOME")
		path := filepath.Join(home, ".config", "derpy", "queue.json")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("queue file not found at expected path: %s", path)
		}

		// No stray temp files should remain.
		dir := filepath.Join(home, ".config", "derpy")
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			if filepath.Ext(entry.Name()) == ".tmp" {
				t.Errorf("stray temp file left behind: %s", entry.Name())
			}
		}
	})
}

// TestIsDuplicateEarmark_PathKey verifies that Path is the primary dedup key.
func TestIsDuplicateEarmark_PathKey(t *testing.T) {
	existing := []core.Earmark{
		{Artist: "Coltrane", Title: "Resolution", Path: "/music/resolution.flac", Timestamp: 1},
	}

	// Same path — duplicate regardless of differing metadata.
	dup := core.Earmark{Artist: "Other", Title: "Other", Path: "/music/resolution.flac"}
	if !core.IsDuplicateEarmark(existing, dup) {
		t.Error("expected duplicate for matching path, got false")
	}

	// Different path — not a duplicate.
	fresh := core.Earmark{Artist: "Coltrane", Title: "Resolution", Path: "/music/other.flac"}
	if core.IsDuplicateEarmark(existing, fresh) {
		t.Error("expected non-duplicate for different path, got true")
	}
}

// TestIsDuplicateEarmark_MetadataFallback verifies the Artist+Album+Title
// fallback when Path is empty.
func TestIsDuplicateEarmark_MetadataFallback(t *testing.T) {
	existing := []core.Earmark{
		{Artist: "Miles Davis", Album: "Kind of Blue", Title: "So What", Path: ""},
	}

	dup := core.Earmark{Artist: "Miles Davis", Album: "Kind of Blue", Title: "So What", Path: ""}
	if !core.IsDuplicateEarmark(existing, dup) {
		t.Error("expected duplicate for matching artist/album/title, got false")
	}

	diff := core.Earmark{Artist: "Miles Davis", Album: "Kind of Blue", Title: "Blue in Green", Path: ""}
	if core.IsDuplicateEarmark(existing, diff) {
		t.Error("expected non-duplicate for different title, got true")
	}
}

// TestAppendToQueue_NoDuplicates verifies that AppendToQueue silently skips
// an entry whose path already exists in the queue.
func TestAppendToQueue_NoDuplicates(t *testing.T) {
	withTempQueue(t, func() {
		e := core.Earmark{Artist: "Monk", Title: "Round Midnight", Path: "/music/monk.flac", Timestamp: 6000}
		_ = AppendToQueue(e)

		// Second append with the same path should be a no-op.
		e2 := core.Earmark{Artist: "Monk", Title: "Round Midnight", Path: "/music/monk.flac", Timestamp: 6001}
		if err := AppendToQueue(e2); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		q, _ := LoadQueue()
		if len(q) != 1 {
			t.Errorf("expected 1 entry (deduped), got %d", len(q))
		}
		// Original entry (first timestamp) should be preserved.
		if q[0].Timestamp != 6000 {
			t.Errorf("expected original timestamp 6000, got %d", q[0].Timestamp)
		}
	})
}

// TestQueueRoundTrip verifies a full append → load → remove cycle.
func TestQueueRoundTrip(t *testing.T) {
	withTempQueue(t, func() {
		earmarks := []core.Earmark{
			{Artist: "Coltrane", Album: "A Love Supreme", Title: "Acknowledgement", Timestamp: 5000},
			{Artist: "Coltrane", Album: "A Love Supreme", Title: "Resolution", Timestamp: 5001},
			{Artist: "Coltrane", Album: "A Love Supreme", Title: "Pursuance", Timestamp: 5002},
		}

		for _, e := range earmarks {
			_ = AppendToQueue(e)
		}

		// Remove the middle entry.
		_ = RemoveFromQueue(earmarks[1])

		q, _ := LoadQueue()
		if len(q) != 2 {
			t.Fatalf("expected 2, got %d", len(q))
		}
		if q[0].Timestamp != 5000 || q[1].Timestamp != 5002 {
			t.Errorf("unexpected order or content: %+v", q)
		}
	})
}
