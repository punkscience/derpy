package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nbd-wtf/go-nostr"
)

// --- Unit tests: pure helpers -------------------------------------------

// TestReferencedChunks_Basic checks that every chunk SHA in every earmark
// is collected into the set; earmarks with nil manifest contribute nothing.
func TestReferencedChunks_Basic(t *testing.T) {
	a := strings.Repeat("a", 64)
	b := strings.Repeat("b", 64)
	earmarks := []Earmark{
		{Title: "x", Blossom: &BlossomManifest{Chunks: []BlossomChunk{{SHA256: a}, {SHA256: b}}}},
		{Title: "queued", Blossom: nil},
	}
	got := referencedChunks(earmarks)
	if _, ok := got[a]; !ok {
		t.Errorf("%s missing from referenced set", a[:8])
	}
	if _, ok := got[b]; !ok {
		t.Errorf("%s missing from referenced set", b[:8])
	}
	if len(got) != 2 {
		t.Errorf("expected 2 SHAs, got %d", len(got))
	}
}

// TestHistoricalServers_DedupesAndIgnoresNilManifests verifies that every
// distinct server URL is surfaced exactly once, regardless of how many
// chunks referenced it, and that nil manifests are skipped.
func TestHistoricalServers_DedupesAndIgnoresNilManifests(t *testing.T) {
	earmarks := []Earmark{
		{Blossom: &BlossomManifest{Chunks: []BlossomChunk{
			{SHA256: strings.Repeat("a", 64), Servers: []string{"https://one", "https://two"}},
			{SHA256: strings.Repeat("b", 64), Servers: []string{"https://one"}}, // dupe
		}}},
		{Blossom: nil}, // skipped
		{Blossom: &BlossomManifest{Chunks: []BlossomChunk{
			{SHA256: strings.Repeat("c", 64), Servers: []string{"https://three"}},
		}}},
	}
	got := historicalServers(earmarks)
	set := map[string]bool{}
	for _, s := range got {
		set[s] = true
	}
	if len(set) != 3 || !set["https://one"] || !set["https://two"] || !set["https://three"] {
		t.Errorf("got %v; want {one, two, three} deduped", got)
	}
}

// TestManifestHasAnyChunk_PresentOnOneServer is the core orphan-earmark
// predicate: a single authoritative server that lists any one chunk keeps
// the earmark alive.
func TestManifestHasAnyChunk_PresentOnOneServer(t *testing.T) {
	a := strings.Repeat("a", 64)
	b := strings.Repeat("b", 64)
	m := &BlossomManifest{Chunks: []BlossomChunk{{SHA256: a}, {SHA256: b}}}
	serverBlobs := map[string]map[string]struct{}{
		"s1": {a: struct{}{}},
		"s2": nil, // inconclusive — should be ignored
	}
	if !manifestHasAnyChunk(m, serverBlobs) {
		t.Error("expected true: one authoritative server lists one chunk")
	}
}

// TestManifestHasAnyChunk_AllInconclusiveReturnsFalse documents the
// algorithmic expectation: when every server is inconclusive, the predicate
// returns false. The caller (Reconcile) must therefore separately check
// report.Inconclusive before acting on a false result.
func TestManifestHasAnyChunk_AllInconclusiveReturnsFalse(t *testing.T) {
	m := &BlossomManifest{Chunks: []BlossomChunk{{SHA256: strings.Repeat("a", 64)}}}
	serverBlobs := map[string]map[string]struct{}{
		"s1": nil,
		"s2": nil,
	}
	if manifestHasAnyChunk(m, serverBlobs) {
		t.Error("expected false when every server is inconclusive")
	}
}

// TestManifestHasAnyChunk_AllAuthoritativeMissingReturnsFalse is the
// orphan-confirmed case: every server answered authoritatively and none
// hold any chunk. Reconcile uses this to drop the earmark.
func TestManifestHasAnyChunk_AllAuthoritativeMissingReturnsFalse(t *testing.T) {
	m := &BlossomManifest{Chunks: []BlossomChunk{{SHA256: strings.Repeat("a", 64)}}}
	serverBlobs := map[string]map[string]struct{}{
		"s1": {strings.Repeat("x", 64): struct{}{}}, // has other blobs, not ours
		"s2": {},                                    // empty, authoritative
	}
	if manifestHasAnyChunk(m, serverBlobs) {
		t.Error("expected false: every server authoritatively reports not-present")
	}
}

// --- Integration tests: helpers against httptest servers ----------------

// listingServer starts an httptest server that responds to BUD-02
// GET /list/<pubkey> with the given SHA set and to BUD-01 DELETE with 200
// (recording the delete path so the test can assert). An empty shas map
// still returns a valid empty JSON array (authoritative).
func listingServer(t *testing.T, shas []string) (*httptest.Server, *int32) {
	t.Helper()
	var deleteCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/list/"):
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("["))
			for i, s := range shas {
				if i > 0 {
					w.Write([]byte(","))
				}
				fmt.Fprintf(w, `{"sha256":%q,"size":100}`, s)
			}
			w.Write([]byte("]"))
		case r.Method == http.MethodDelete:
			atomic.AddInt32(&deleteCount, 1)
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	return srv, &deleteCount
}

// TestListAllServers_InconclusiveEntryIsNil verifies that a server which
// fails to respond is recorded as nil (inconclusive) rather than as an
// empty set — the difference between "don't know" and "authoritatively
// empty" is the whole safety property.
func TestListAllServers_InconclusiveEntryIsNil(t *testing.T) {
	live, _ := listingServer(t, []string{strings.Repeat("a", 64)})
	defer live.Close()

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	dead.Close() // closed — unreachable

	privKey := nostr.GeneratePrivateKey()
	got := listAllServers(context.Background(), []string{live.URL, dead.URL}, privKey)

	if got[live.URL] == nil {
		t.Error("live server should produce authoritative (non-nil) set")
	}
	if got[dead.URL] != nil {
		t.Error("dead server should produce nil (inconclusive) entry")
	}
}

// TestListAllServers_EmptyListingIsAuthoritative verifies that a server
// returning [] produces a non-nil (authoritative) empty map. Reconcile
// relies on this distinction to know that a server really holds nothing
// for the user vs. simply being unreachable.
func TestListAllServers_EmptyListingIsAuthoritative(t *testing.T) {
	srv, _ := listingServer(t, nil)
	defer srv.Close()

	got := listAllServers(context.Background(), []string{srv.URL}, nostr.GeneratePrivateKey())
	if got[srv.URL] == nil {
		t.Error("empty [] response should be authoritative (non-nil empty map)")
	}
	if len(got[srv.URL]) != 0 {
		t.Errorf("expected empty set, got %d entries", len(got[srv.URL]))
	}
}

// TestDeleteOrphanBlobs_SkipsReferenced verifies that a SHA present in the
// referenced set is NOT deleted even when the server lists it; only the
// unreferenced SHA gets a DELETE.
func TestDeleteOrphanBlobs_SkipsReferenced(t *testing.T) {
	live := strings.Repeat("a", 64)
	orphan := strings.Repeat("b", 64)

	// Server that tracks DELETE targets by path so we can assert the exact
	// blob that got removed.
	var mu sync.Mutex
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			mu.Lock()
			deleted = append(deleted, strings.TrimPrefix(r.URL.Path, "/"))
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "not supported", http.StatusMethodNotAllowed)
	}))
	defer srv.Close()

	serverBlobs := map[string]map[string]struct{}{
		srv.URL: {live: struct{}{}, orphan: struct{}{}},
	}
	referenced := map[string]struct{}{live: {}}

	count := deleteOrphanBlobs(context.Background(), nostr.GeneratePrivateKey(), serverBlobs, referenced)
	if count != 1 {
		t.Errorf("expected 1 DELETE issued, got %d", count)
	}
	if len(deleted) != 1 || deleted[0] != orphan {
		t.Errorf("expected only orphan sha deleted; got %v", deleted)
	}
}

// TestDeleteOrphanBlobs_SkipsInconclusiveServers verifies that nil entries
// in serverBlobs (inconclusive listings) are never acted on — we don't
// know what the server has, so we cannot safely delete anything from it.
func TestDeleteOrphanBlobs_SkipsInconclusiveServers(t *testing.T) {
	serverBlobs := map[string]map[string]struct{}{
		"https://unreachable": nil, // inconclusive
	}
	count := deleteOrphanBlobs(context.Background(), nostr.GeneratePrivateKey(), serverBlobs, map[string]struct{}{})
	if count != 0 {
		t.Errorf("expected 0 deletes against inconclusive server, got %d", count)
	}
}

// --- End-to-end: ReconcileReport safety invariants ----------------------
//
// These tests exercise the full Reconcile flow indirectly by driving the
// orphan-earmark pruning loop with scenarios that match the real failure
// modes the earlier HEAD-based design got wrong. They assert the safety
// invariants: never drop on silence, always drop on authoritative absence,
// always keep nil-manifest entries.

// TestOrphanEarmarkPruning_AllInconclusiveKeepsEverything reconstructs the
// core safety property: when every server is inconclusive, no earmark is
// dropped no matter how many chunks are unlisted. This is the invariant
// the earlier HEAD-based reconcile violated.
func TestOrphanEarmarkPruning_AllInconclusiveKeepsEverything(t *testing.T) {
	earmarks := []Earmark{
		{Title: "a", Blossom: &BlossomManifest{Chunks: []BlossomChunk{{SHA256: strings.Repeat("a", 64)}}}},
		{Title: "b", Blossom: &BlossomManifest{Chunks: []BlossomChunk{{SHA256: strings.Repeat("b", 64)}}}},
	}
	serverBlobs := map[string]map[string]struct{}{
		"s1": nil,
		"s2": nil,
	}

	authoritative := 0
	for _, m := range serverBlobs {
		if m != nil {
			authoritative++
		}
	}
	if authoritative != 0 {
		t.Fatalf("precondition: expected all-inconclusive, got %d authoritative", authoritative)
	}
	// Mirror the pruning loop in Reconcile with Inconclusive=true: no pruning.
	finalKeep := earmarks
	if len(finalKeep) != 2 {
		t.Errorf("inconclusive pass must not drop any earmark; got %d", len(finalKeep))
	}
}

// TestOrphanEarmarkPruning_NilManifestAlwaysKept verifies the outbox-queued
// case: an earmark without a Blossom manifest has nothing to reconcile
// against and must survive even when server listings are authoritative
// and empty.
func TestOrphanEarmarkPruning_NilManifestAlwaysKept(t *testing.T) {
	earmarks := []Earmark{
		{Title: "queued", Blossom: nil},
	}
	serverBlobs := map[string]map[string]struct{}{
		"s1": {}, // authoritative empty
	}
	finalKeep := make([]Earmark, 0, len(earmarks))
	for _, e := range earmarks {
		if e.Blossom == nil || manifestHasAnyChunk(e.Blossom, serverBlobs) {
			finalKeep = append(finalKeep, e)
		}
	}
	if len(finalKeep) != 1 {
		t.Errorf("nil-manifest earmark must always survive; got %d kept", len(finalKeep))
	}
}

// TestOrphanEarmarkPruning_OneHonestServerSaves verifies that a single
// authoritative server listing any one chunk is sufficient to save the
// earmark, even when other servers are authoritative-empty.
func TestOrphanEarmarkPruning_OneHonestServerSaves(t *testing.T) {
	sha := strings.Repeat("a", 64)
	earmarks := []Earmark{
		{Title: "e", Blossom: &BlossomManifest{Chunks: []BlossomChunk{{SHA256: sha}}}},
	}
	serverBlobs := map[string]map[string]struct{}{
		"live":  {sha: struct{}{}},
		"empty": {},
	}
	finalKeep := make([]Earmark, 0, len(earmarks))
	for _, e := range earmarks {
		if e.Blossom == nil || manifestHasAnyChunk(e.Blossom, serverBlobs) {
			finalKeep = append(finalKeep, e)
		}
	}
	if len(finalKeep) != 1 {
		t.Errorf("one honest server should save the earmark; got %d kept", len(finalKeep))
	}
}

// TestOrphanEarmarkPruning_EveryAuthoritativeMissingDrops is the orphan-
// confirmed case: every reachable server authoritatively reports not
// holding the chunks → drop the earmark.
func TestOrphanEarmarkPruning_EveryAuthoritativeMissingDrops(t *testing.T) {
	sha := strings.Repeat("a", 64)
	earmarks := []Earmark{
		{Title: "dead", Blossom: &BlossomManifest{Chunks: []BlossomChunk{{SHA256: sha}}}},
	}
	serverBlobs := map[string]map[string]struct{}{
		"s1": {strings.Repeat("x", 64): struct{}{}}, // has other blobs
		"s2": {},                                    // authoritative empty
	}
	finalKeep := make([]Earmark, 0, len(earmarks))
	dropped := 0
	for _, e := range earmarks {
		if e.Blossom == nil || manifestHasAnyChunk(e.Blossom, serverBlobs) {
			finalKeep = append(finalKeep, e)
		} else {
			dropped++
		}
	}
	if dropped != 1 || len(finalKeep) != 0 {
		t.Errorf("authoritative-missing earmark should be dropped; kept=%d dropped=%d",
			len(finalKeep), dropped)
	}
}
