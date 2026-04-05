package main

import (
	"context"
	"encoding/json"
	"fmt"
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
)

// Earmark represents a single earmarked track stored in the private list.
type Earmark struct {
	Artist    string `json:"artist"`
	Album     string `json:"album"`
	Title     string `json:"title"`
	Timestamp int64  `json:"ts"` // Unix seconds
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

// fetchEarmarksCtx is the context-aware inner implementation used by both
// FetchEarmarks and AddEarmark (which needs to pass its own timeout context).
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

	// Query all relays and keep the most recently created event, since different
	// relays may have different versions if a sync was interrupted.
	var latest *nostr.Event
	for _, relayURL := range defaultNostrRelays {
		relay, err := nostr.RelayConnect(ctx, relayURL)
		if err != nil {
			continue
		}
		events, err := relay.QuerySync(ctx, filter)
		relay.Close()
		if err != nil || len(events) == 0 {
			continue
		}
		if latest == nil || events[0].CreatedAt > latest.CreatedAt {
			latest = events[0]
		}
	}

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

// AddEarmark fetches the current list, appends the new entry, and re-publishes
// the encrypted event. Because kind-30001 is addressable, relays automatically
// replace the previous version (same pubkey + kind + "d" tag).
func AddEarmark(hexPrivKey string, e Earmark) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	existing, err := fetchEarmarksCtx(ctx, hexPrivKey)
	if err != nil {
		// Start fresh rather than block the user if the read fails.
		existing = []Earmark{}
	}

	return publishEarmarks(ctx, hexPrivKey, append(existing, e))
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

	published := 0
	var lastErr error
	for _, relayURL := range defaultNostrRelays {
		relay, err := nostr.RelayConnect(ctx, relayURL)
		if err != nil {
			lastErr = err
			continue
		}
		if err := relay.Publish(ctx, ev); err != nil {
			lastErr = err
		} else {
			published++
		}
		relay.Close()
	}

	if published == 0 {
		return fmt.Errorf("failed to publish earmark list to any relay: %w", lastErr)
	}
	return nil
}
