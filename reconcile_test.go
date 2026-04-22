package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestReconcileFilter_AllPresent verifies that when every chunk HEADs to 200,
// no earmarks are removed.
func TestReconcileFilter_AllPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	earmarks := []Earmark{
		{Title: "a", Blossom: &BlossomManifest{Chunks: []BlossomChunk{
			{SHA256: strings.Repeat("a", 64), Servers: []string{srv.URL}},
		}}},
		{Title: "b", Blossom: &BlossomManifest{Chunks: []BlossomChunk{
			{SHA256: strings.Repeat("b", 64), Servers: []string{srv.URL}},
		}}},
	}

	keep, removed := reconcileFilter(context.Background(), earmarks)
	if removed != 0 {
		t.Errorf("removed = %d; want 0", removed)
	}
	if len(keep) != 2 {
		t.Errorf("keep length = %d; want 2", len(keep))
	}
}

// TestReconcileFilter_OrphanedEarmark verifies that an earmark whose only
// server authoritatively returns 404 for every chunk is removed, while a
// sibling earmark on a live server survives.
func TestReconcileFilter_OrphanedEarmark(t *testing.T) {
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer live.Close()
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer dead.Close()

	earmarks := []Earmark{
		{Title: "live", Blossom: &BlossomManifest{Chunks: []BlossomChunk{
			{SHA256: strings.Repeat("1", 64), Servers: []string{live.URL}},
		}}},
		{Title: "orphan", Blossom: &BlossomManifest{Chunks: []BlossomChunk{
			{SHA256: strings.Repeat("2", 64), Servers: []string{dead.URL}},
		}}},
	}

	keep, removed := reconcileFilter(context.Background(), earmarks)
	if removed != 1 {
		t.Fatalf("removed = %d; want 1", removed)
	}
	if len(keep) != 1 || keep[0].Title != "live" {
		t.Errorf("keep = %+v; want single 'live' entry", keep)
	}
}

// TestReconcileFilter_NilManifestPreserved verifies that earmarks without a
// manifest (queued while offline, never uploaded) are always kept even though
// we cannot verify them.
func TestReconcileFilter_NilManifestPreserved(t *testing.T) {
	earmarks := []Earmark{
		{Title: "queued", Blossom: nil},
	}
	keep, removed := reconcileFilter(context.Background(), earmarks)
	if removed != 0 || len(keep) != 1 {
		t.Errorf("queued earmark should survive reconcile; keep=%v removed=%d", keep, removed)
	}
}

// TestReconcileFilter_PartialMissingChunk verifies that an earmark is dropped
// when any single chunk is authoritatively missing — a torn upload leaves the
// track unplayable, so the listing must be cleaned up.
func TestReconcileFilter_PartialMissingChunk(t *testing.T) {
	present := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer present.Close()
	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer missing.Close()

	earmarks := []Earmark{
		{Title: "torn", Blossom: &BlossomManifest{Chunks: []BlossomChunk{
			{SHA256: strings.Repeat("a", 64), Servers: []string{present.URL}},
			{SHA256: strings.Repeat("b", 64), Servers: []string{missing.URL}},
		}}},
	}
	keep, removed := reconcileFilter(context.Background(), earmarks)
	if removed != 1 || len(keep) != 0 {
		t.Errorf("torn earmark should be removed; keep=%v removed=%d", keep, removed)
	}
}

// TestReconcileFilter_InconclusiveErrorKept verifies the conservative
// semantics of ManifestComplete: a server that errors out (here, a closed
// listener) is "unknown" rather than "missing", so the earmark is preserved.
func TestReconcileFilter_InconclusiveErrorKept(t *testing.T) {
	// Create and immediately close a server so the URL is unreachable.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	earmarks := []Earmark{
		{Title: "unreachable", Blossom: &BlossomManifest{Chunks: []BlossomChunk{
			{SHA256: strings.Repeat("c", 64), Servers: []string{srv.URL}},
		}}},
	}
	keep, removed := reconcileFilter(context.Background(), earmarks)
	if removed != 0 || len(keep) != 1 {
		t.Errorf("unreachable server should not trigger removal; keep=%v removed=%d", keep, removed)
	}
}
