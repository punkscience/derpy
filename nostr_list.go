package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip44"
)

const (
	// earmarkListTag is the "d" tag that identifies the earmark list event.
	// Using a namespaced tag avoids clashing with other apps' kind-30001 lists.
	earmarkListTag = "dirplay-earmarks"

	// earmarkKind is the NIP-51 "categorized bookmarks" kind.
	// It is an addressable event: relays keep only the latest version per (pubkey, kind, d).
	earmarkKind = 30001

	// EarmarkMaxAge is the retention window for earmarks. Entries older than
	// this are deleted from Nostr and their Blossom chunks are purged on startup.
	EarmarkMaxAge = 30 * 24 * time.Hour
)

// Earmark represents a single earmarked track stored in the private list.
type Earmark struct {
	Artist    string           `json:"artist"`
	Album     string           `json:"album"`
	Title     string           `json:"title"`
	Path      string           `json:"path,omitempty"`    // absolute path on the machine that earmarked it
	Timestamp int64            `json:"ts"`                // Unix seconds
	Blossom   *BlossomManifest `json:"blossom,omitempty"` // set once the file has been uploaded
}

// selfConvKey derives the NIP-44 conversation key for self-encryption by using
// the user's own public key as the "recipient". Only the holder of the private
// key can derive the same conversation key and decrypt the content.
//
// NIP-44 API note: GenerateConversationKey(pub, sk) — public key comes first.
func selfConvKey(hexPrivKey string) ([32]byte, error) {
	pubHex, err := nostr.GetPublicKey(hexPrivKey)
	if err != nil {
		return [32]byte{}, fmt.Errorf("could not derive public key: %w", err)
	}
	key, err := nip44.GenerateConversationKey(pubHex, hexPrivKey)
	if err != nil {
		return [32]byte{}, fmt.Errorf("could not generate conversation key: %w", err)
	}
	return key, nil
}

// FetchEarmarks fetches and decrypts the private earmark list from Nostr relays.
// Returns an empty slice when no list has been published yet.
func FetchEarmarks(hexPrivKey string) ([]Earmark, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return fetchEarmarksCtx(ctx, hexPrivKey)
}

// fetchEarmarksCtx is the context-aware inner implementation shared by
// FetchEarmarks and AddEarmark.
func fetchEarmarksCtx(ctx context.Context, hexPrivKey string) ([]Earmark, error) {
	pubHex, err := nostr.GetPublicKey(hexPrivKey)
	if err != nil {
		return nil, fmt.Errorf("could not derive public key: %w", err)
	}

	convKey, err := selfConvKey(hexPrivKey)
	if err != nil {
		return nil, err
	}

	filter := nostr.Filter{
		Kinds:   []int{earmarkKind},
		Authors: []string{pubHex},
		Tags:    nostr.TagMap{"d": []string{earmarkListTag}},
		Limit:   1,
	}

	// Query all relays concurrently; keep the most recently created event.
	latest := queryRelays(ctx, LoadNostrRelays(), filter)
	if latest == nil {
		return []Earmark{}, nil
	}

	plaintext, err := nip44.Decrypt(latest.Content, convKey)
	if err != nil {
		return nil, fmt.Errorf("could not decrypt earmark list: %w", err)
	}

	var earmarks []Earmark
	if err := json.Unmarshal([]byte(plaintext), &earmarks); err != nil {
		return nil, fmt.Errorf("could not parse earmark list: %w", err)
	}
	return earmarks, nil
}

// isDuplicateEarmark reports whether e is already represented in existing.
// Path is the primary key. When Path is empty (e.g. older earmarks that
// predate path storage) the Artist+Album+Title triple is used as a fallback.
func isDuplicateEarmark(existing []Earmark, e Earmark) bool {
	for _, x := range existing {
		if e.Path != "" && x.Path == e.Path {
			return true
		}
		if e.Path == "" && x.Artist == e.Artist && x.Album == e.Album && x.Title == e.Title {
			return true
		}
	}
	return false
}

// AddEarmark fetches the current list, appends the new entry if it is not
// already present, and re-publishes the encrypted event. Because kind-30001
// is addressable, relays automatically replace the previous version (same
// pubkey + kind + "d" tag).
// Fetch and publish use independent timeouts so a slow fetch does not consume
// the publish budget.
func AddEarmark(hexPrivKey string, e Earmark) error {
	fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 15*time.Second)
	existing, _ := fetchEarmarksCtx(fetchCtx, hexPrivKey) // start fresh on error
	fetchCancel()

	if isDuplicateEarmark(existing, e) {
		return nil // already earmarked — nothing to do
	}

	publishCtx, publishCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer publishCancel()
	return publishEarmarks(publishCtx, hexPrivKey, append(existing, e))
}

// UpdateEarmark fetches the current list, replaces the entry whose Timestamp
// matches updated.Timestamp, and re-publishes. Used by the upload command to
// attach a BlossomManifest to an existing earmark without changing its identity.
func UpdateEarmark(hexPrivKey string, updated Earmark) error {
	fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 15*time.Second)
	existing, err := fetchEarmarksCtx(fetchCtx, hexPrivKey)
	fetchCancel()
	if err != nil {
		return fmt.Errorf("could not fetch earmarks: %w", err)
	}

	found := false
	for i, e := range existing {
		if e.Timestamp == updated.Timestamp {
			existing[i] = updated
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("no earmark with timestamp %d found", updated.Timestamp)
	}

	publishCtx, publishCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer publishCancel()
	return publishEarmarks(publishCtx, hexPrivKey, existing)
}

// CleanupOldEarmarks removes earmarks older than EarmarkMaxAge from the Nostr
// list and deletes their Blossom chunks from all associated servers.
//
// The operation is best-effort: Blossom deletions that fail (server down,
// already purged) are silently ignored. If the Nostr republish fails the
// function returns an error but chunk deletions that already completed are
// not rolled back.
//
// Returns the number of earmarks removed.
func CleanupOldEarmarks(hexPrivKey string) (int, error) {
	earmarks, err := FetchEarmarks(hexPrivKey)
	if err != nil {
		return 0, fmt.Errorf("could not fetch earmarks for cleanup: %w", err)
	}

	cutoff := time.Now().Add(-EarmarkMaxAge).Unix()

	var keep, remove []Earmark
	for _, e := range earmarks {
		if e.Timestamp < cutoff {
			remove = append(remove, e)
		} else {
			keep = append(keep, e)
		}
	}

	if len(remove) == 0 {
		return 0, nil
	}

	// Delete Blossom chunks for all old earmarks in parallel.
	// Use a generous timeout — there may be many chunks across multiple servers.
	blossomCtx, blossomCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	var wg sync.WaitGroup
	for _, e := range remove {
		if e.Blossom != nil {
			e := e
			wg.Add(1)
			go func() {
				defer wg.Done()
				DeleteManifestChunks(blossomCtx, hexPrivKey, e.Blossom)
			}()
		}
	}
	wg.Wait()
	blossomCancel()

	// Republish the earmark list without the old entries.
	publishCtx, publishCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer publishCancel()
	if err := publishEarmarks(publishCtx, hexPrivKey, keep); err != nil {
		return 0, fmt.Errorf("could not republish earmark list after cleanup: %w", err)
	}

	return len(remove), nil
}

// publishEarmarks encrypts and publishes the complete earmark slice as a
// NIP-51 kind-30001 addressable event to all configured relays.
func publishEarmarks(ctx context.Context, hexPrivKey string, earmarks []Earmark) error {
	pubHex, err := nostr.GetPublicKey(hexPrivKey)
	if err != nil {
		return fmt.Errorf("could not derive public key: %w", err)
	}

	convKey, err := selfConvKey(hexPrivKey)
	if err != nil {
		return err
	}

	data, err := json.Marshal(earmarks)
	if err != nil {
		return fmt.Errorf("could not marshal earmarks: %w", err)
	}

	ciphertext, err := nip44.Encrypt(string(data), convKey)
	if err != nil {
		return fmt.Errorf("could not encrypt earmarks: %w", err)
	}

	ev := nostr.Event{
		PubKey:    pubHex,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      earmarkKind,
		Content:   ciphertext,
		// The "d" tag is the addressable identifier — relays use (pubkey, kind, d)
		// as the composite key and discard older events with the same triple.
		Tags: nostr.Tags{{"d", earmarkListTag}},
	}
	if err := ev.Sign(hexPrivKey); err != nil {
		return fmt.Errorf("could not sign earmark event: %w", err)
	}

	return publishToRelays(ctx, LoadNostrRelays(), ev)
}
