// Sum cache: the persistent path → Sum mapping that bridges the file-system
// view (paths users see) and the Tag index (keyed by metadata-invariant
// Sum). See CONTEXT.md and docs/adr/0001-tags-external-index.md.
//
// Lookups are validated against the file's current mtime+size; a stale
// entry (file rewritten or resized) is treated as a miss so the caller
// recomputes the Sum.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/dhowden/tag"
)

const (
	sumCacheFileName = "sum-cache.json"
	sumCacheVersion  = 1
)

// SumCacheEntry is the per-path record in the Sum cache.
type SumCacheEntry struct {
	Mtime int64  `json:"mtime"` // file mtime in Unix nanoseconds
	Size  int64  `json:"size"`  // file size in bytes
	Sum   string `json:"sum"`   // tag.Sum output (SHA-1 hex)
}

// SumCache is the persistent path → Sum mapping.
type SumCache struct {
	Version int                      `json:"version"`
	Entries map[string]SumCacheEntry `json:"entries"`

	mu sync.RWMutex `json:"-"`
}

// NewSumCache returns an empty cache at the current schema version.
func NewSumCache() *SumCache {
	return &SumCache{Version: sumCacheVersion, Entries: map[string]SumCacheEntry{}}
}

// sumCacheFilePath returns the absolute path to ~/.config/derpy/sum-cache.json.
func sumCacheFilePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sumCacheFileName), nil
}

// LoadSumCache reads the cache from disk. A missing file is treated as an
// empty cache — first-run is not an error.
func LoadSumCache() (*SumCache, error) {
	path, err := sumCacheFilePath()
	if err != nil {
		return NewSumCache(), err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return NewSumCache(), nil
	}
	if err != nil {
		return NewSumCache(), err
	}

	sc := NewSumCache()
	if err := json.Unmarshal(data, sc); err != nil {
		return NewSumCache(), fmt.Errorf("sum cache: parse %s: %w", path, err)
	}
	if sc.Entries == nil {
		sc.Entries = map[string]SumCacheEntry{}
	}
	return sc, nil
}

// SaveSumCache writes sc atomically to disk.
func SaveSumCache(sc *SumCache) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	sc.mu.RLock()
	data, marshalErr := json.MarshalIndent(sc, "", "  ")
	sc.mu.RUnlock()
	if marshalErr != nil {
		return marshalErr
	}

	return atomicWrite(filepath.Join(dir, sumCacheFileName), data, 0o600)
}

// Lookup returns the cached Sum for path iff the file's current mtime+size
// match the cached entry. A missing file, missing entry, or mismatch all
// produce ok=false.
func (sc *SumCache) Lookup(path string) (sum string, ok bool) {
	sc.mu.RLock()
	entry, found := sc.Entries[path]
	sc.mu.RUnlock()
	if !found {
		return "", false
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	if info.ModTime().UnixNano() != entry.Mtime || info.Size() != entry.Size {
		return "", false
	}
	return entry.Sum, true
}

// Put stores or updates the entry for path.
func (sc *SumCache) Put(path string, entry SumCacheEntry) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.Entries[path] = entry
}

// All returns a snapshot map of all entries (defensive copy).
func (sc *SumCache) All() map[string]SumCacheEntry {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	out := make(map[string]SumCacheEntry, len(sc.Entries))
	for k, v := range sc.Entries {
		out[k] = v
	}
	return out
}

// PathsForSums returns the set of paths in the cache whose Sum is in want.
// Used by the --tags search path to map tagged Sums back to file locations.
// Callers should verify each returned path still exists; this function does
// not stat the file system.
func (sc *SumCache) PathsForSums(want map[string]bool) []string {
	if len(want) == 0 {
		return nil
	}
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	var paths []string
	for path, entry := range sc.Entries {
		if want[entry.Sum] {
			paths = append(paths, path)
		}
	}
	return paths
}

// ComputeSum opens path and returns its tag.Sum along with the mtime+size
// that the caller should record alongside. tag.Sum reads the audio data
// only (skipping ID3/MP4/FLAC metadata blocks per-format) so the result
// survives embedded-metadata edits and renames.
func ComputeSum(path string) (sum string, mtime int64, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, 0, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", 0, 0, err
	}

	s, err := tag.Sum(f)
	if err != nil {
		return "", 0, 0, fmt.Errorf("sum %s: %w", path, err)
	}
	return s, info.ModTime().UnixNano(), info.Size(), nil
}

// LookupOrCompute returns the Sum for path, using the cache when valid and
// otherwise computing it via tag.Sum and storing the result in the cache.
// The cache is mutated but not persisted to disk; callers are responsible
// for SaveSumCache when they want to flush.
func (sc *SumCache) LookupOrCompute(path string) (string, error) {
	if sum, ok := sc.Lookup(path); ok {
		return sum, nil
	}
	sum, mtime, size, err := ComputeSum(path)
	if err != nil {
		return "", err
	}
	sc.Put(path, SumCacheEntry{Mtime: mtime, Size: size, Sum: sum})
	return sum, nil
}
