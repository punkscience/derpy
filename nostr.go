package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

// defaultNostrRelays is the fallback relay list used when the user has not
// configured any relays via 'derpy relay add'.
// relay.damus.io and nos.lol are generally open for writes.
var defaultNostrRelays = []string{
	"wss://relay.damus.io",
	"wss://nos.lol",
	"wss://relay.primal.net",
	"wss://nostr.wine",
}

// nip65IndexRelays are read-only indexer relays that aggregate kind-10002
// relay lists from across the network. They are queried (in addition to the
// configured relays) when looking up a user's NIP-65 relay list, because the
// user may have published their relay list from another client to relays
// derpy is not configured with. purplepag.es is the canonical NIP-65 index.
var nip65IndexRelays = []string{
	"wss://purplepag.es",
	"wss://relay.nostr.band",
}

// publishToRelays connects to all relays concurrently and publishes ev to each.
// Returns nil if at least one relay accepted the event.
func publishToRelays(ctx context.Context, relays []string, ev nostr.Event) error {
	type result struct{ err error }
	ch := make(chan result, len(relays))

	for _, u := range relays {
		u := u
		go func() {
			relay, err := nostr.RelayConnect(ctx, u)
			if err != nil {
				ch <- result{err}
				return
			}
			defer relay.Close()
			ch <- result{relay.Publish(ctx, ev)}
		}()
	}

	published := 0
	var lastErr error
	for range relays {
		r := <-ch
		if r.err != nil {
			lastErr = r.err
		} else {
			published++
		}
	}

	if published == 0 {
		if lastErr != nil {
			return fmt.Errorf("failed to publish to any relay: %w", lastErr)
		}
		return fmt.Errorf("failed to publish to any relay")
	}
	return nil
}

// queryRelays queries all relays concurrently and returns the most recently
// created event matching the filter, or nil if none is found.
func queryRelays(ctx context.Context, relays []string, filter nostr.Filter) *nostr.Event {
	ch := make(chan *nostr.Event, len(relays))

	for _, u := range relays {
		u := u
		go func() {
			relay, err := nostr.RelayConnect(ctx, u)
			if err != nil {
				ch <- nil
				return
			}
			evs, err := relay.QuerySync(ctx, filter)
			relay.Close()
			if err != nil || len(evs) == 0 {
				ch <- nil
				return
			}
			ch <- evs[0]
		}()
	}

	var latest *nostr.Event
	for range relays {
		ev := <-ch
		if ev != nil && (latest == nil || ev.CreatedAt > latest.CreatedAt) {
			latest = ev
		}
	}
	return latest
}

// resolveNostrKey returns the user's Nostr private key (raw hex) following the
// resolution order: DERPY_NOSTR_KEY env var → config file → empty string.
// Used by the TUI to determine whether [E] and [P] controls should be shown
// and by the key-entry handlers to decide when to prompt inline.
func resolveNostrKey() string {
	if env := os.Getenv("DERPY_NOSTR_KEY"); env != "" {
		if hex, err := resolvePrivateKey(env); err == nil {
			return hex
		}
	}
	cfg, err := LoadConfig()
	if err == nil && cfg.NostrPrivateKey != "" {
		return cfg.NostrPrivateKey
	}
	return ""
}

// resolvePrivateKey accepts either a bech32 nsec1... string or a raw 64-char
// hex string and always returns the raw hex private key.
func resolvePrivateKey(key string) (string, error) {
	// Strip whitespace and any non-printable characters (e.g. null bytes that
	// Windows terminal emulators can inject during clipboard paste).
	key = strings.TrimSpace(key)
	key = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, key)
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

// fetchUserWriteRelays fetches the user's NIP-65 relay list (kind 10002) and
// returns the URLs they have marked as write (or read+write) relays.
// The query goes to the configured relays plus the NIP-65 indexer relays so
// a list published from another client is still found.
// Returns nil when no relay list event is found so callers can fall back.
func fetchUserWriteRelays(ctx context.Context, pubHex string) []string {
	filter := nostr.Filter{
		Kinds:   []int{10002},
		Authors: []string{pubHex},
		Limit:   1,
	}
	ev := queryRelays(ctx, unionRelays(LoadNostrRelays(), nip65IndexRelays), filter)
	if ev == nil {
		return nil
	}
	return parseWriteRelays(ev)
}

// parseWriteRelays extracts the write (or read+write) relay URLs from a
// kind-10002 NIP-65 relay list event.
func parseWriteRelays(ev *nostr.Event) []string {
	var relays []string
	for _, tag := range ev.Tags {
		// NIP-65 relay tag: ["r", "wss://...", ("read"|"write")?]
		// No third element means both read and write.
		if len(tag) < 2 || tag[0] != "r" {
			continue
		}
		if len(tag) == 2 || tag[2] == "write" {
			relays = append(relays, tag[1])
		}
	}
	return relays
}

// nip65Cache memoises the user's NIP-65 write relays for the lifetime of the
// process (with a TTL) so repeated publishes in one TUI session don't re-query
// the network every time. Empty results are cached too, so an offline session
// doesn't pay the lookup timeout on every earmark or post.
var nip65Cache = struct {
	sync.Mutex
	pubHex    string
	relays    []string
	fetchedAt time.Time
}{}

// nip65CacheTTL is how long a cached NIP-65 lookup stays fresh.
const nip65CacheTTL = 15 * time.Minute

// userPublishRelays returns the full outbox relay set for the user's own
// events: their NIP-65 write relays unioned with the configured relay list.
// The NIP-65 lookup runs with its own short timeout and is TTL-cached; when
// it fails or the user has no relay list, the configured relays are used alone.
func userPublishRelays(pubHex string) []string {
	nip65Cache.Lock()
	if nip65Cache.pubHex == pubHex && time.Since(nip65Cache.fetchedAt) < nip65CacheTTL {
		cached := nip65Cache.relays
		nip65Cache.Unlock()
		return unionRelays(cached, LoadNostrRelays())
	}
	nip65Cache.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	writeRelays := fetchUserWriteRelays(ctx, pubHex)
	cancel()

	nip65Cache.Lock()
	nip65Cache.pubHex = pubHex
	nip65Cache.relays = writeRelays
	nip65Cache.fetchedAt = time.Now()
	nip65Cache.Unlock()

	return unionRelays(writeRelays, LoadNostrRelays())
}

// unionRelays returns a deduplicated slice containing all URLs from a and b,
// preserving order (a's entries appear first).
func unionRelays(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, r := range append(a, b...) {
		if _, ok := seen[r]; !ok {
			seen[r] = struct{}{}
			out = append(out, r)
		}
	}
	return out
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

	// Search for a single listen link: Bandcamp preferred, YouTube as fallback.
	// This is best-effort — a missing link does not block publishing.
	link := FindBestLink(artist, title, album)

	ev := buildTrackNote(npub, pubHex, artist, title, link)
	if err := ev.Sign(hexKey); err != nil {
		return fmt.Errorf("could not sign Nostr event: %w", err)
	}

	// Outbox model: publish to the user's NIP-65 write relays (so the event
	// appears wherever their profile is followed from) unioned with the
	// configured relay list.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return publishToRelays(ctx, userPublishRelays(pubHex), ev)
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
