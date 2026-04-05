package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

// defaultNostrRelays is the list of well-known public relays dirplay publishes to.
// Notes are sent to all of them; success requires at least one to accept.
var defaultNostrRelays = []string{
	"wss://relay.damus.io",
	"wss://nos.lol",
	"wss://relay.nostr.band",
	"wss://nostr.wine",
}

// resolvePrivateKey accepts either a bech32 nsec1... string or a raw 64-char
// hex string and always returns the raw hex private key.
func resolvePrivateKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if strings.HasPrefix(key, "nsec1") {
		prefix, val, err := nip19.Decode(key)
		if err != nil {
			return "", fmt.Errorf("invalid nsec key: %w", err)
		}
		if prefix != "nsec" {
			return "", fmt.Errorf("expected an nsec key, got %q prefix", prefix)
		}
		hex, ok := val.(string)
		if !ok {
			return "", fmt.Errorf("unexpected nsec decode type")
		}
		return hex, nil
	}
	// Treat as raw hex; go-nostr will validate it when signing.
	return key, nil
}

// npubFromPrivateKey derives the bech32-encoded public key (npub) from a raw
// hex private key. Used to identify the user in the published note text.
func npubFromPrivateKey(hexPrivKey string) (string, error) {
	pubHex, err := nostr.GetPublicKey(hexPrivKey)
	if err != nil {
		return "", fmt.Errorf("could not derive public key: %w", err)
	}
	npub, err := nip19.EncodePublicKey(pubHex)
	if err != nil {
		// Fall back to raw hex if encoding fails.
		return pubHex, nil
	}
	return npub, nil
}

// PublishNostrTrackNote signs and publishes a kind-1 Nostr text note
// earmarking the given track. privateKey may be an nsec1... bech32 string or
// a raw hex string.  The function returns an error only if no relay accepted
// the event; partial failures are silently swallowed.
func PublishNostrTrackNote(privateKey, artist, title, album string) error {
	hexKey, err := resolvePrivateKey(privateKey)
	if err != nil {
		return err
	}

	npub, err := npubFromPrivateKey(hexKey)
	if err != nil {
		return err
	}

	pubHex, err := nostr.GetPublicKey(hexKey)
	if err != nil {
		return fmt.Errorf("could not derive public key: %w", err)
	}

	// Search Bandcamp and YouTube in parallel for listen links.
	// This is best-effort: missing links do not block publishing.
	links := FindTrackLinks(artist, title, album)

	var linkSection strings.Builder
	if links.Bandcamp != "" {
		linkSection.WriteString("\n\n🎵 " + links.Bandcamp)
	}
	if links.YouTube != "" {
		linkSection.WriteString("\n▶️ " + links.YouTube)
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

	content := fmt.Sprintf(
		"%s is really digging %s right now! #music #dirplay%s",
		npub,
		digging,
		linkSection.String(),
	)

	ev := nostr.Event{
		PubKey:    pubHex,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      nostr.KindTextNote, // kind 1 — plain text note
		Content:   content,
		Tags:      nostr.Tags{},
	}
	if err := ev.Sign(hexKey); err != nil {
		return fmt.Errorf("could not sign Nostr event: %w", err)
	}

	// Publish to all relays; require at least one success.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	published := 0
	var lastErr error
	for _, url := range defaultNostrRelays {
		relay, err := nostr.RelayConnect(ctx, url)
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
		return fmt.Errorf("failed to publish to any Nostr relay: %w", lastErr)
	}
	return nil
}
