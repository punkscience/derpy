package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// copyFile is a tiny helper for assembling temp-dir fixtures.
func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open src %s: %v", src, err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create dst %s: %v", dst, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy: %v", err)
	}
}

func TestIndexSourceHashesUncachedFiles(t *testing.T) {
	fixture := "dummy_music/1.flac"
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("fixture %s not available: %v", fixture, err)
	}

	dir := t.TempDir()
	a := filepath.Join(dir, "a.flac")
	b := filepath.Join(dir, "b.flac")
	copyFile(t, fixture, a)
	copyFile(t, fixture, b)
	// Non-audio file: indexer must skip.
	if err := os.WriteFile(filepath.Join(dir, "README.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatal(err)
	}

	sc := NewSumCache()

	var mu sync.Mutex
	var lastDone, lastTotal int
	IndexSource(context.Background(), dir, sc, func(done, total int) {
		mu.Lock()
		lastDone, lastTotal = done, total
		mu.Unlock()
	})

	// Both audio files should be in cache, README ignored.
	all := sc.All()
	if len(all) != 2 {
		t.Errorf("SumCache has %d entries, want 2 (got %v)", len(all), all)
	}
	if _, ok := sc.Lookup(a); !ok {
		t.Errorf("a.flac missing from cache")
	}
	if _, ok := sc.Lookup(b); !ok {
		t.Errorf("b.flac missing from cache")
	}

	mu.Lock()
	defer mu.Unlock()
	if lastTotal != 2 || lastDone != 2 {
		t.Errorf("final progress = %d/%d, want 2/2", lastDone, lastTotal)
	}
}

func TestIndexSourceSkipsCachedFiles(t *testing.T) {
	fixture := "dummy_music/1.flac"
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("fixture %s not available: %v", fixture, err)
	}

	dir := t.TempDir()
	a := filepath.Join(dir, "a.flac")
	copyFile(t, fixture, a)

	sc := NewSumCache()

	// Pre-populate cache so the indexer should skip recomputation.
	info, _ := os.Stat(a)
	sc.Put(a, SumCacheEntry{
		Mtime: info.ModTime().UnixNano(),
		Size:  info.Size(),
		Sum:   "pretend-prehashed",
	})

	IndexSource(context.Background(), dir, sc, nil)

	// Cached entry must be preserved verbatim — no recompute, no overwrite.
	got, ok := sc.Lookup(a)
	if !ok {
		t.Fatalf("a.flac dropped from cache during indexing")
	}
	if got != "pretend-prehashed" {
		t.Errorf("cached entry overwritten: got %q, want pretend-prehashed", got)
	}
}

func TestIndexSourceHandlesEmptyDir(t *testing.T) {
	dir := t.TempDir()
	sc := NewSumCache()

	called := false
	IndexSource(context.Background(), dir, sc, func(done, total int) {
		called = true
	})

	if called {
		t.Errorf("progress callback fired on empty dir; want zero invocations")
	}
	if len(sc.All()) != 0 {
		t.Errorf("cache populated on empty dir: %v", sc.All())
	}
}

func TestIndexSourceCancellable(t *testing.T) {
	fixture := "dummy_music/1.flac"
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("fixture %s not available: %v", fixture, err)
	}

	dir := t.TempDir()
	// Drop several files so cancellation has a chance to bite mid-loop.
	for i := 0; i < 10; i++ {
		copyFile(t, fixture, filepath.Join(dir, "t"+string(rune('0'+i))+".flac"))
	}

	sc := NewSumCache()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled — indexer should exit on first iteration

	IndexSource(ctx, dir, sc, nil)

	if got := len(sc.All()); got > 0 {
		t.Errorf("pre-cancelled indexer processed %d files; want 0", got)
	}
}

func TestViewShowsIndexingStatusLine(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})
	m.Update(trackLoadedMsg{sum: "sumA", tags: nil})
	m.width = 100
	m.indexingDone = 7
	m.indexingTotal = 42

	out := m.View()
	if !contains(out, "indexing 7/42 tracks") {
		t.Errorf("View should show indexing status: %q", out)
	}
}

func TestViewHidesIndexingStatusWhenDone(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})
	m.Update(trackLoadedMsg{sum: "sumA", tags: nil})
	m.width = 100
	m.indexingDone = 42
	m.indexingTotal = 42

	out := m.View()
	if contains(out, "indexing") {
		t.Errorf("View should hide indexing status when done == total: %q", out)
	}
}

// contains is a tiny strings.Contains-ish helper kept local so the test
// file's imports stay minimal.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
