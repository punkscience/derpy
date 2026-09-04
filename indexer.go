// Background indexer for the Sum cache.
//
// On a fresh install or after the user adds music, the SumCache is empty
// or stale for a chunk of the library. This sweep walks the source
// directory, computes tag.Sum for any audio file not already in the
// cache, and persists periodically. Playback is never blocked — the
// sweep runs in its own goroutine and only its progress goes through the
// Bubble Tea program.
//
// See CONTEXT.md and docs/adr/0001-tags-external-index.md for the wider
// architecture this slots into.
package main

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
)

// indexerAudioExts mirrors scanMusicDirectory's extension set. Kept as a
// package-level var (not const) so the indexer and the playlist scanner
// stay in lockstep when new formats are added.
var indexerAudioExts = map[string]bool{
	".mp3":  true,
	".wav":  true,
	".flac": true,
	".ogg":  true,
	".m4a":  true,
	".aac":  true,
}

// indexerSaveEvery is the number of newly-hashed files between SumCache
// flushes to disk. Striking a balance between losing progress on crash
// (smaller is better) and disk-write churn (larger is better).
const indexerSaveEvery = 50

// IndexSource walks sourceDir and ensures every audio file is represented
// in sc. Files already in cache with matching (mtime, size) are skipped;
// stale or absent entries are recomputed via tag.Sum and inserted.
//
// Progress is reported through send (typically tea.Program.Send) so the
// caller can update the TUI. send may be nil — the sweep then runs
// silently.
//
// Errors hashing individual files (corrupt audio, permission denied) are
// swallowed so a single bad file doesn't halt the sweep. Cache writes
// happen every indexerSaveEvery files and a final time on return, so
// progress survives crashes and graceful cancellation.
//
// Honours ctx.Done(): cancellation stops the sweep at the next file
// boundary and the deferred save still flushes whatever was done.
func IndexSource(ctx context.Context, sourceDir string, sc *SumCache, send func(progressDone, progressTotal int)) {
	defer func() { _ = SaveSumCache(sc) }()

	var files []string
	_ = filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries silently
		}
		if d.IsDir() {
			return nil
		}
		if indexerAudioExts[strings.ToLower(filepath.Ext(path))] {
			files = append(files, path)
		}
		return nil
	})

	total := len(files)
	if send != nil && total > 0 {
		send(0, total)
	}

	done := 0
	newlyHashed := 0
	for _, path := range files {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if _, ok := sc.Lookup(path); ok {
			done++
			if send != nil {
				send(done, total)
			}
			continue
		}

		sum, mtime, size, err := ComputeSum(path)
		done++
		if err == nil {
			sc.Put(path, SumCacheEntry{Mtime: mtime, Size: size, Sum: sum})
			newlyHashed++
			if newlyHashed%indexerSaveEvery == 0 {
				_ = SaveSumCache(sc)
			}
		}
		if send != nil {
			send(done, total)
		}
	}
}
