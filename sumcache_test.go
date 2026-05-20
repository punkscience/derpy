package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// isWindows is a small helper so tests can skip the XDG_CONFIG_HOME-based
// fixtures on Windows, where os.UserConfigDir uses %AppData% instead.
func isWindows() bool { return runtime.GOOS == "windows" }

func TestSumCacheLookupHitWhenUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "track.bin")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)

	sc := NewSumCache()
	sc.Put(path, SumCacheEntry{
		Mtime: info.ModTime().UnixNano(),
		Size:  info.Size(),
		Sum:   "deadbeef",
	})

	sum, ok := sc.Lookup(path)
	if !ok {
		t.Fatalf("Lookup on unchanged file = miss, want hit")
	}
	if sum != "deadbeef" {
		t.Errorf("Lookup sum = %q, want deadbeef", sum)
	}
}

func TestSumCacheLookupMissWhenMtimeChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "track.bin")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)

	sc := NewSumCache()
	sc.Put(path, SumCacheEntry{
		Mtime: info.ModTime().UnixNano(),
		Size:  info.Size(),
		Sum:   "deadbeef",
	})

	// Bump the file's mtime forward by a second.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	if _, ok := sc.Lookup(path); ok {
		t.Errorf("Lookup after mtime change = hit, want miss")
	}
}

func TestSumCacheLookupMissWhenSizeChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "track.bin")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)

	sc := NewSumCache()
	sc.Put(path, SumCacheEntry{
		Mtime: info.ModTime().UnixNano(),
		Size:  info.Size(),
		Sum:   "deadbeef",
	})

	// Rewrite with longer content, but force the mtime to the original
	// so we can isolate the size-change branch from the mtime-change branch.
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}

	if _, ok := sc.Lookup(path); ok {
		t.Errorf("Lookup after size change = hit, want miss")
	}
}

func TestSumCacheLookupMissWhenFileAbsent(t *testing.T) {
	sc := NewSumCache()
	sc.Put("/no/such/file", SumCacheEntry{Mtime: 1, Size: 1, Sum: "x"})
	if _, ok := sc.Lookup("/no/such/file"); ok {
		t.Errorf("Lookup on missing file = hit, want miss")
	}
}

func TestSumCacheLookupMissWhenEntryAbsent(t *testing.T) {
	sc := NewSumCache()
	if _, ok := sc.Lookup("/anything"); ok {
		t.Errorf("Lookup on empty cache = hit, want miss")
	}
}

func TestSumCachePathsForSums(t *testing.T) {
	sc := NewSumCache()
	sc.Put("/a.flac", SumCacheEntry{Sum: "sumA"})
	sc.Put("/b.flac", SumCacheEntry{Sum: "sumB"})
	sc.Put("/c.flac", SumCacheEntry{Sum: "sumA"}) // same Sum as a.flac
	sc.Put("/d.flac", SumCacheEntry{Sum: "sumD"})

	paths := sc.PathsForSums(map[string]bool{"sumA": true, "sumD": true})

	got := map[string]bool{}
	for _, p := range paths {
		got[p] = true
	}
	want := map[string]bool{"/a.flac": true, "/c.flac": true, "/d.flac": true}
	if len(got) != len(want) {
		t.Errorf("PathsForSums returned %d paths, want %d (%v)", len(got), len(want), paths)
	}
	for p := range want {
		if !got[p] {
			t.Errorf("PathsForSums missing %q (got %v)", p, paths)
		}
	}
}

func TestComputeSumOnFixture(t *testing.T) {
	// derpy ships dummy_music/*.flac as a fixture. tag.Sum should succeed
	// on these and produce stable, distinct hashes.
	flac := "dummy_music/1.flac"
	if _, err := os.Stat(flac); err != nil {
		t.Skipf("fixture %s not available: %v", flac, err)
	}

	sum, mtime, size, err := ComputeSum(flac)
	if err != nil {
		t.Fatalf("ComputeSum: %v", err)
	}
	if sum == "" {
		t.Errorf("ComputeSum returned empty sum")
	}
	if mtime == 0 {
		t.Errorf("ComputeSum returned zero mtime")
	}
	if size == 0 {
		t.Errorf("ComputeSum returned zero size")
	}

	// Re-running on the same untouched file must produce the same sum.
	sum2, _, _, err := ComputeSum(flac)
	if err != nil {
		t.Fatalf("ComputeSum (second call): %v", err)
	}
	if sum != sum2 {
		t.Errorf("ComputeSum is not deterministic: %q vs %q", sum, sum2)
	}
}

func TestSumCacheLookupOrCompute(t *testing.T) {
	flac := "dummy_music/1.flac"
	if _, err := os.Stat(flac); err != nil {
		t.Skipf("fixture %s not available: %v", flac, err)
	}

	sc := NewSumCache()

	// First call: cache miss, computes via tag.Sum.
	sum1, err := sc.LookupOrCompute(flac)
	if err != nil {
		t.Fatalf("LookupOrCompute (miss): %v", err)
	}
	if sum1 == "" {
		t.Errorf("LookupOrCompute returned empty sum")
	}

	// Second call: cache hit, must return the same sum without recomputing.
	sum2, err := sc.LookupOrCompute(flac)
	if err != nil {
		t.Fatalf("LookupOrCompute (hit): %v", err)
	}
	if sum1 != sum2 {
		t.Errorf("LookupOrCompute hit returned different sum: %q vs %q", sum1, sum2)
	}
}

func TestSumCacheRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if isWindows() {
		t.Skip("XDG_CONFIG_HOME not honoured by os.UserConfigDir on Windows")
	}

	orig := NewSumCache()
	orig.Put("/x.flac", SumCacheEntry{Mtime: 100, Size: 200, Sum: "abc"})
	orig.Put("/y.flac", SumCacheEntry{Mtime: 300, Size: 400, Sum: "def"})

	if err := SaveSumCache(orig); err != nil {
		t.Fatalf("SaveSumCache: %v", err)
	}

	loaded, err := LoadSumCache()
	if err != nil {
		t.Fatalf("LoadSumCache: %v", err)
	}
	if loaded.Version != sumCacheVersion {
		t.Errorf("loaded Version = %d, want %d", loaded.Version, sumCacheVersion)
	}
	if e := loaded.Entries["/x.flac"]; e.Sum != "abc" || e.Mtime != 100 || e.Size != 200 {
		t.Errorf("x.flac round-trip mismatch: %+v", e)
	}
	if e := loaded.Entries["/y.flac"]; e.Sum != "def" || e.Mtime != 300 || e.Size != 400 {
		t.Errorf("y.flac round-trip mismatch: %+v", e)
	}
}
