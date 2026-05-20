// Package-level Tag index storage and the normalization rule that turns
// raw user input into canonical Tag strings.
//
// The Tag index is the source of truth for which Tags are attached to which
// Tracks. It lives at ~/.config/derpy/tags.json and is keyed by the Sum
// produced by tag.Sum() — see CONTEXT.md and
// docs/adr/0001-tags-external-index.md for the rationale.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	tagsFileName    = "tags.json"
	tagIndexVersion = 1
)

// TagIndex maps a Track's Sum to its applied Tag strings.
type TagIndex struct {
	Version int                 `json:"version"`
	Tags    map[string][]string `json:"tags"`

	mu sync.RWMutex `json:"-"`
}

// NewTagIndex returns an empty TagIndex at the current schema version.
func NewTagIndex() *TagIndex {
	return &TagIndex{Version: tagIndexVersion, Tags: map[string][]string{}}
}

// tagsFilePath returns the absolute path to ~/.config/derpy/tags.json.
func tagsFilePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, tagsFileName), nil
}

// LoadTagIndex reads the Tag index from disk. A missing file is treated as
// an empty index — first-run is not an error.
func LoadTagIndex() (*TagIndex, error) {
	path, err := tagsFilePath()
	if err != nil {
		return NewTagIndex(), err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return NewTagIndex(), nil
	}
	if err != nil {
		return NewTagIndex(), err
	}

	ti := NewTagIndex()
	if err := json.Unmarshal(data, ti); err != nil {
		return NewTagIndex(), fmt.Errorf("tag index: parse %s: %w", path, err)
	}
	if ti.Tags == nil {
		ti.Tags = map[string][]string{}
	}
	return ti, nil
}

// SaveTagIndex writes ti atomically to ~/.config/derpy/tags.json.
func SaveTagIndex(ti *TagIndex) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	ti.mu.RLock()
	data, marshalErr := json.MarshalIndent(ti, "", "  ")
	ti.mu.RUnlock()
	if marshalErr != nil {
		return marshalErr
	}

	return atomicWrite(filepath.Join(dir, tagsFileName), data, 0o600)
}

// Get returns a defensive copy of the Tag list for sum, or nil if none.
func (ti *TagIndex) Get(sum string) []string {
	ti.mu.RLock()
	defer ti.mu.RUnlock()
	tags, ok := ti.Tags[sum]
	if !ok {
		return nil
	}
	out := make([]string, len(tags))
	copy(out, tags)
	return out
}

// Set replaces the Tag list for sum. Passing an empty/nil slice removes the
// entry entirely so we don't accumulate empty entries on disk.
func (ti *TagIndex) Set(sum string, tags []string) {
	ti.mu.Lock()
	defer ti.mu.Unlock()
	if len(tags) == 0 {
		delete(ti.Tags, sum)
		return
	}
	stored := make([]string, len(tags))
	copy(stored, tags)
	ti.Tags[sum] = stored
}

// SumsWithAnyTag returns the set of Sums tagged with at least one of the
// given Tags. Used by the --tags pre-filter. Empty input → empty set.
//
// Tag matching is exact; callers must pass already-normalized tags.
func (ti *TagIndex) SumsWithAnyTag(wanted []string) map[string]bool {
	if len(wanted) == 0 {
		return map[string]bool{}
	}
	want := make(map[string]bool, len(wanted))
	for _, t := range wanted {
		want[t] = true
	}

	ti.mu.RLock()
	defer ti.mu.RUnlock()

	matches := map[string]bool{}
	for sum, tags := range ti.Tags {
		for _, t := range tags {
			if want[t] {
				matches[sum] = true
				break
			}
		}
	}
	return matches
}

// NormalizeTags parses a raw comma-separated input string into canonical Tag
// strings. The rule (see CONTEXT.md and the grilling session that produced
// ADR-0001):
//
//   - split on commas
//   - lowercase
//   - retain only [a-z0-9 ]; strip everything else
//   - collapse runs of whitespace to a single space
//   - trim leading/trailing whitespace
//   - drop empty entries
//   - dedupe, preserving first-occurrence order
//
// Examples:
//
//	"Drum n Bass"      → ["drum n bass"]
//	"Drum 'n' Bass"    → ["drum n bass"]   (apostrophes stripped)
//	"808 bass, 2step"  → ["808 bass", "2step"]
//	"jazz, JAZZ, Jazz" → ["jazz"]
//	"  ,, jazz ,, "    → ["jazz"]
func NormalizeTags(input string) []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range strings.Split(input, ",") {
		t := normalizeOneTag(raw)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// normalizeOneTag applies the per-tag transform: lowercase, [a-z0-9 ] only,
// collapse spaces, trim. Returns "" if nothing survives.
func normalizeOneTag(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := true // suppresses leading spaces and runs
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevSpace = false
		case r == ' ' || r == '\t':
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		default:
			// silently strip
		}
	}
	// Trim any trailing space we may have emitted.
	return strings.TrimRight(b.String(), " ")
}

// JoinTags renders a Tag slice for human display in the TUI and CLI.
func JoinTags(tags []string) string {
	return strings.Join(tags, ", ")
}

// filterPlaylistByTags returns the subset of paths whose Track has at
// least one of the Tags in rawTags. The raw tag list is normalized so the
// caller can pass whatever the user typed at the CLI.
//
// Lookup walks the SumCache for entries whose Sum is in the matched set
// (built once via TagIndex.SumsWithAnyTag), then intersects with paths.
// Files that no longer exist on disk are dropped silently — stale cache
// entries shouldn't surface as ghost matches.
//
// Empty rawTags returns paths unchanged. ti and sc may be the empty index
// / cache; an empty index just produces an empty result.
//
// Tracks whose Sum has not yet been computed (not in the cache) are
// invisible to this filter on cold startups — slice 6's background
// indexer fills the cache in over time.
func filterPlaylistByTags(paths []string, rawTags string, ti *TagIndex, sc *SumCache) []string {
	wanted := NormalizeTags(rawTags)
	if len(wanted) == 0 {
		return paths
	}

	matchedSums := ti.SumsWithAnyTag(wanted)
	if len(matchedSums) == 0 {
		return nil
	}

	allowed := make(map[string]bool, len(matchedSums))
	for path, entry := range sc.All() {
		if matchedSums[entry.Sum] {
			if _, err := os.Stat(path); err == nil {
				allowed[path] = true
			}
		}
	}
	if len(allowed) == 0 {
		return nil
	}

	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if allowed[p] {
			out = append(out, p)
		}
	}
	return out
}

// FilterPlaylistByTagsFromDisk is the production wrapper that loads the
// TagIndex and SumCache from disk and applies the filter. Use this from
// CLI command paths; tests should call filterPlaylistByTags directly with
// injected fixtures.
func FilterPlaylistByTagsFromDisk(paths []string, rawTags string) []string {
	if rawTags == "" {
		return paths
	}
	ti, _ := LoadTagIndex()
	sc, _ := LoadSumCache()
	return filterPlaylistByTags(paths, rawTags, ti, sc)
}

// atomicWrite writes data to path atomically by writing a sibling tmp file
// and renaming it over the target. Avoids leaving a half-written file if
// the process dies mid-write.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
