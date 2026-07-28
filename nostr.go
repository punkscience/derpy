package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/nbd-wtf/go-nostr"
	core "github.com/punkscience/earmark/earmark-core"
)

// The earmark protocol itself — relays, Blossom, the encrypted list, channels,
// and the NIP-65 outbox relay set — lives in
// github.com/punkscience/earmark/earmark-core, shared with the earmark CLI.
// What stays here is derpy's own key resolution and its "digging this track"
// note, neither of which is protocol.

// resolveNostrKey returns derpy's configured Nostr private key (raw hex),
// preferring the DERPY_NOSTR_KEY environment variable over the config file.
func resolveNostrKey() string {
	if env := os.Getenv("DERPY_NOSTR_KEY"); env != "" {
		if hex, err := core.ResolvePrivateKey(env); err == nil {
			return hex
		}
	}
	cfg, err := LoadConfig()
	if err == nil && cfg.NostrPrivateKey != "" {
		return cfg.NostrPrivateKey
	}
	return ""
}

// PublishNostrTrackNote publishes a public kind-1 note about the track the user
// is currently playing. This is derpy's own social flourish and is unrelated to
// earmarks, which are private by construction.
func PublishNostrTrackNote(privateKey, artist, title, album string) error {
	hexKey, err := core.ResolvePrivateKey(privateKey)
	if err != nil {
		return err
	}

	npub, err := core.NpubFromPrivateKey(hexKey)
	if err != nil {
		return err
	}

	pubHex, err := nostr.GetPublicKey(hexKey)
	if err != nil {
		return fmt.Errorf("could not derive public key: %w", err)
	}

	// Search for a single listen link: Bandcamp preferred, YouTube as fallback.
	// This is best-effort — a missing link does not block publishing.
	link := FindBestLink(artist, title, album)

	ev := buildTrackNote(npub, pubHex, artist, title, link)
	if err := ev.Sign(hexKey); err != nil {
		return fmt.Errorf("could not sign Nostr event: %w", err)
	}

	// Outbox model: publish to the user's NIP-65 write relays (so the event
	// appears wherever their profile is followed from) unioned with the
	// configured relay list. The lookup is TTL-cached inside the core.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return core.PublishToRelays(ctx, core.UserPublishRelays(pubHex), ev)
}

// buildTrackNote assembles the unsigned kind-1 event for a track post.
// Split out from PublishNostrTrackNote so content and tag construction are
// unit-testable without touching the network.
func buildTrackNote(npub, pubHex, artist, title, link string) nostr.Event {
	var linkSection string
	if link != "" {
		linkSection = "\n\n" + link
	}

	// Build the "digging X by Y" phrase, gracefully handling missing metadata.
	var digging string
	switch {
	case title != "" && artist != "":
		digging = fmt.Sprintf("%s by %s", title, artist)
	case title != "":
		digging = title
	case artist != "":
		digging = fmt.Sprintf("a track by %s", artist)
	default:
		digging = "this track"
	}

	// NIP-21 nostr: URI scheme so Nostr clients render the npub as a
	// clickable profile reference instead of raw text.
	content := fmt.Sprintf(
		"nostr:%s is really digging %s right now! #music #derpy%s",
		npub,
		digging,
		linkSection,
	)

	return nostr.Event{
		PubKey:    pubHex,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      nostr.KindTextNote, // kind 1 — plain text note
		Content:   content,
		Tags: nostr.Tags{
			// NIP-27: a nostr:npub mention in content gets a matching p tag.
			{"p", pubHex},
			// NIP-24: hashtags in content get lowercase t tags so relay
			// search and hashtag feeds can index the note.
			{"t", "music"},
			{"t", "derpy"},
		},
	}
}
