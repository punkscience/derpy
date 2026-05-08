package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// reconcileTimeout is the total budget for the launch-time pass. It covers
// the BUD-02 listing fan-out and the orphan-chunk DELETE storm. The Nostr
// republish uses its own independent timeout so a slow Blossom server never
// starves the write that actually converges the earmark list.
const reconcileTimeout = 30 * time.Second

// ReconcileReport summarises the work a single Reconcile call performed,
// so the caller can render a concise one-line status to the user.
type ReconcileReport struct {
	// AgedOut is the number of earmarks dropped because their Timestamp was
	// older than EarmarkMaxAge. Age prune is time-driven and runs even when
	// no Blossom server produced an authoritative listing.
	AgedOut int

	// OrphanEarmarks is the number of earmarks dropped because every
	// authoritative server reported none of the manifest's chunks.
	// Always zero when Inconclusive is true.
	OrphanEarmarks int

	// OrphanChunksDeleted is the number of DELETE requests issued for
	// blobs a server listed but no surviving earmark references.
	// Always zero when Inconclusive is true.
	OrphanChunksDeleted int

	// Republished is true when a new Nostr event was written. False means
	// the list was already consistent and no relay traffic was generated.
	Republished bool

	// Inconclusive is true when no Blossom server produced an authoritative
	// listing. Orphan detection (both directions) is suppressed in that
	// case; only age-prune can still fire.
	Inconclusive bool
}

// Reconcile converges the user's Nostr earmark list and Blossom-stored
// chunks in a single launch-time pass. It is the canonical entry point for
// orphan prevention and replaces the earlier HEAD-based reconcile plus the
// standalone CleanupOldEarmarks pass.
//
// Algorithm:
//  1. Fetch and decrypt the current earmark list.
//  2. Split by age: entries older than EarmarkMaxAge are age-pruned.
//  3. Query BUD-02 GET /list/<pubkey> on every configured Blossom server
//     in parallel (unioned with any server ever recorded in a manifest,
//     so blobs on abandoned servers get cleaned up too).
//  4. Using the authoritative listings:
//     a. drop earmarks whose manifest chunks appear on no server
//     (orphan earmarks);
//     b. the final keep-list's referenced SHA set is the authoritative
//     set of blobs that should remain on Blossom. Everything else
//     a server lists is an orphan chunk and gets deleted.
//  5. Publish the new list once, then issue DELETEs.
//
// Safety invariants the body upholds:
//   - Never drop on silence. If every server listing came back inconclusive
//     (network error, 4xx, garbled JSON), orphan detection is skipped
//     entirely. Only age-prune still runs.
//   - Never drop an earmark with Blossom == nil. Nil manifest means the
//     entry is outbox-queued or pre-upload; there is nothing to reconcile.
//   - Publish before DELETE. A failed publish must never leave chunks gone
//     while the Nostr list still points at them — it would create exactly
//     the orphan-earmark state reconcile is supposed to prevent.
//   - Empty earmark list → no-op. An empty fetch is ambiguous (fresh user
//     vs. silent relay) and touching Blossom based on ambiguous Nostr
//     state is a one-way trip.
func Reconcile(hexPrivKey string) (ReconcileReport, error) {
	var report ReconcileReport

	ctx, cancel := context.WithTimeout(context.Background(), reconcileTimeout)
	defer cancel()

	earmarks, err := fetchEarmarksCtx(ctx, hexPrivKey, earmarkListTag)
	if err != nil {
		return report, fmt.Errorf("could not fetch earmarks: %w", err)
	}
	if len(earmarks) == 0 {
		// Ambiguous — could be a fresh account or a silent relay. We never
		// touch Blossom on an empty fetch; the user can run `derpy earmarks
		// --nuke` to force a clean wipe.
		return report, nil
	}

	// Step 2: age-split.
	cutoff := time.Now().Add(-EarmarkMaxAge).Unix()
	var ageKeep []Earmark
	for _, e := range earmarks {
		if e.Timestamp < cutoff {
			report.AgedOut++
		} else {
			ageKeep = append(ageKeep, e)
		}
	}

	// Step 3: list every user blob on every relevant server. We union the
	// currently configured server list with every server ever recorded in
	// an earmark manifest so blobs on servers the user has since dropped
	// from their config still get cleaned up.
	configured, err := ResolveBlossomServers(hexPrivKey)
	if err != nil {
		return report, fmt.Errorf("could not resolve Blossom servers: %w", err)
	}
	servers := unionRelays(configured, historicalServers(earmarks))
	serverBlobs := listAllServers(ctx, servers, hexPrivKey)

	authoritative := 0
	for _, m := range serverBlobs {
		if m != nil {
			authoritative++
		}
	}
	report.Inconclusive = authoritative == 0

	// Step 4a: orphan-earmark pruning. Only runs when at least one server
	// produced an authoritative listing; otherwise we pass ageKeep through
	// unchanged.
	finalKeep := ageKeep
	if !report.Inconclusive {
		finalKeep = make([]Earmark, 0, len(ageKeep))
		for _, e := range ageKeep {
			if e.Blossom == nil || manifestHasAnyChunk(e.Blossom, serverBlobs) {
				finalKeep = append(finalKeep, e)
			} else {
				report.OrphanEarmarks++
			}
		}
	}

	// Step 5: publish FIRST. A publish failure must never leave chunks
	// gone with Nostr still pointing at them. We also publish when only
	// AgedOut > 0 so age-prune can converge even when orphan detection is
	// suppressed by inconclusive listings.
	if report.AgedOut > 0 || report.OrphanEarmarks > 0 {
		publishCtx, publishCancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := publishEarmarks(publishCtx, hexPrivKey, finalKeep)
		publishCancel()
		if err != nil {
			return report, fmt.Errorf("could not republish reconciled list: %w", err)
		}
		report.Republished = true
	}

	// Step 4b: orphan-chunk deletion, derived from the FINAL keep list so
	// age-pruned and orphan-earmark chunks both fall through to be deleted.
	// Only runs with authoritative listings.
	if !report.Inconclusive {
		referenced := referencedChunks(finalKeep)
		report.OrphanChunksDeleted = deleteOrphanBlobs(ctx, hexPrivKey, serverBlobs, referenced)
	}
	return report, nil
}

// listAllServers calls BUD-02 GET /list/<pubkey> on every server in
// parallel. The returned map keys every server URL; the value is the
// authoritative set of SHAs that server reports for the user, or nil when
// the server was inconclusive (network error, 4xx, malformed response).
// A nil entry is the explicit signal "we do not know what this server
// holds" and suppresses orphan detection for that server.
func listAllServers(ctx context.Context, servers []string, hexPrivKey string) map[string]map[string]struct{} {
	result := make(map[string]map[string]struct{}, len(servers))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, s := range servers {
		s := s
		wg.Add(1)
		go func() {
			defer wg.Done()
			blobs, err := ListUserBlobs(ctx, s, hexPrivKey)
			mu.Lock()
			if err == nil {
				result[s] = blobs
			} else {
				result[s] = nil
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return result
}

// manifestHasAnyChunk is the orphan-earmark predicate: true iff at least
// one chunk in manifest is reported by at least one authoritative server.
// "Any chunk" (not "every chunk") is the correct test here because a
// partial survivor is still potentially recoverable; completeness is
// re-verified at playback time. Nil entries in serverBlobs are skipped.
func manifestHasAnyChunk(manifest *BlossomManifest, serverBlobs map[string]map[string]struct{}) bool {
	for _, c := range manifest.Chunks {
		for _, blobs := range serverBlobs {
			if blobs == nil {
				continue
			}
			if _, ok := blobs[c.SHA256]; ok {
				return true
			}
		}
	}
	return false
}

// referencedChunks returns the set of chunk SHAs referenced by the given
// earmarks. Used to classify each (server, sha) pair in the listings as
// either "still referenced" or "orphan blob to delete".
func referencedChunks(earmarks []Earmark) map[string]struct{} {
	set := make(map[string]struct{})
	for _, e := range earmarks {
		if e.Blossom == nil {
			continue
		}
		for _, c := range e.Blossom.Chunks {
			set[c.SHA256] = struct{}{}
		}
	}
	return set
}

// historicalServers returns every server URL that appears in any earmark
// manifest. Used to probe servers the user may have uploaded to in the past
// but has since removed from their active config — the blobs still live
// there and still count as orphans we should clean up.
func historicalServers(earmarks []Earmark) []string {
	seen := make(map[string]struct{})
	for _, e := range earmarks {
		if e.Blossom == nil {
			continue
		}
		for _, c := range e.Blossom.Chunks {
			for _, s := range c.Servers {
				seen[s] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	return out
}

// deleteOrphanBlobs fires BUD-01 DELETE requests in parallel for every
// (server, sha) pair where the server listed the blob but `referenced`
// does not contain the SHA. Best-effort: errors are swallowed because the
// next reconcile pass will catch any blob we missed. Returns the number
// of DELETEs issued (attempt count, not success count).
func deleteOrphanBlobs(ctx context.Context, hexPrivKey string, serverBlobs map[string]map[string]struct{}, referenced map[string]struct{}) int {
	count := 0
	var wg sync.WaitGroup
	for server, blobs := range serverBlobs {
		if blobs == nil {
			continue
		}
		for sha := range blobs {
			if _, keep := referenced[sha]; keep {
				continue
			}
			count++
			server, sha := server, sha
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = deleteChunk(ctx, server, sha, hexPrivKey)
			}()
		}
	}
	wg.Wait()
	return count
}
