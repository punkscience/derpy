package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// reconcileTimeout bounds the total time spent probing Blossom servers during
// reconcile. Each earmark fans out HEAD requests across its chunk servers in
// parallel, so the wall-clock cost is dominated by the slowest single server.
const reconcileTimeout = 45 * time.Second

// ReconcileEarmarks removes earmarks whose Blossom chunks have been deleted
// out-of-band (e.g. by the Android app's earmark-removal flow that deletes the
// chunks but fails to update the encrypted Nostr listing). The check is
// conservative: an earmark is only pruned when every chunk server
// authoritatively answers "not found" — a transient network error leaves the
// earmark in place so we never drop a listing on the basis of a flaky relay or
// a temporarily unreachable Blossom host.
//
// Earmarks whose manifest is nil (queued while offline, never uploaded) are
// always preserved because there is nothing to verify yet.
//
// Returns the number of earmarks removed; zero means the list was already
// consistent and no relay republish happened.
func ReconcileEarmarks(hexPrivKey string) (int, error) {
	earmarks, err := FetchEarmarks(hexPrivKey)
	if err != nil {
		return 0, fmt.Errorf("could not fetch earmarks: %w", err)
	}
	if len(earmarks) == 0 {
		return 0, nil
	}

	probeCtx, probeCancel := context.WithTimeout(context.Background(), reconcileTimeout)
	defer probeCancel()
	keep, removed := reconcileFilter(probeCtx, earmarks)

	if removed == 0 {
		return 0, nil
	}

	publishCtx, publishCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer publishCancel()
	if err := publishEarmarks(publishCtx, hexPrivKey, keep); err != nil {
		return 0, fmt.Errorf("could not republish reconciled earmark list: %w", err)
	}
	return removed, nil
}

// reconcileFilter probes every earmark manifest in parallel and partitions the
// input into entries to keep (manifest nil or still retrievable) and a count
// of entries to drop (manifest present but every chunk server reports 404).
// Extracted so it can be exercised against httptest-backed Blossom servers
// without the surrounding fetch/publish relay traffic.
func reconcileFilter(ctx context.Context, earmarks []Earmark) ([]Earmark, int) {
	complete := make([]bool, len(earmarks))
	var wg sync.WaitGroup
	for i, e := range earmarks {
		if e.Blossom == nil {
			complete[i] = true
			continue
		}
		i, e := i, e
		wg.Add(1)
		go func() {
			defer wg.Done()
			complete[i] = ManifestComplete(ctx, e.Blossom)
		}()
	}
	wg.Wait()

	keep := make([]Earmark, 0, len(earmarks))
	removed := 0
	for i, e := range earmarks {
		if complete[i] {
			keep = append(keep, e)
		} else {
			removed++
		}
	}
	return keep, removed
}
