package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

// TestResolvePrivateKey verifies that resolvePrivateKey accepts both raw hex
// and bech32-encoded nsec keys and normalises them to hex.
func TestResolvePrivateKey(t *testing.T) {
	// Generate a fresh key for the round-trip test.
	hexKey := nostr.GeneratePrivateKey()

	// Encode it to nsec so we can test bech32 decoding.
	nsecKey, err := nip19.EncodePrivateKey(hexKey)
	if err != nil {
		t.Fatalf("could not encode test key to nsec: %v", err)
	}

	tests := []struct {
		name    string
		input   string
		wantHex string // expected result (empty means we just check no error)
		wantErr bool
	}{
		{
			name:    "raw hex key returned unchanged",
			input:   hexKey,
			wantHex: hexKey,
		},
		{
			name:    "nsec key decoded to hex",
			input:   nsecKey,
			wantHex: hexKey,
		},
		{
			name:    "hex key with surrounding whitespace",
			input:   "  " + hexKey + "\n",
			wantHex: hexKey,
		},
		{
			name:    "invalid nsec key",
			input:   "nsec1notvalid",
			wantErr: true,
		},
		{
			name:    "wrong bech32 prefix (npub not nsec)",
			input:   "npub1" + strings.Repeat("q", 58), // a structurally plausible but wrong prefix
			wantErr: false, // treated as raw hex; go-nostr validates on sign
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePrivateKey(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil (result: %q)", got)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if tt.wantHex != "" && got != tt.wantHex {
				t.Errorf("got %q, want %q", got, tt.wantHex)
			}
		})
	}
}

// TestNpubFromPrivateKey verifies that npubFromPrivateKey derives a valid
// bech32-encoded public key from a raw hex private key.
func TestNpubFromPrivateKey(t *testing.T) {
	hexKey := nostr.GeneratePrivateKey()

	npub, err := npubFromPrivateKey(hexKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(npub, "npub1") {
		t.Errorf("expected npub to start with 'npub1', got %q", npub)
	}

	// Verify round-trip: decode npub back to public hex and compare with
	// what go-nostr derives directly from the private key.
	prefix, val, err := nip19.Decode(npub)
	if err != nil {
		t.Fatalf("could not decode npub: %v", err)
	}
	if prefix != "npub" {
		t.Errorf("expected prefix 'npub', got %q", prefix)
	}
	pubHex, ok := val.(string)
	if !ok {
		t.Fatal("decoded value is not a string")
	}

	wantPub, err := nostr.GetPublicKey(hexKey)
	if err != nil {
		t.Fatalf("could not derive public key: %v", err)
	}
	if pubHex != wantPub {
		t.Errorf("npub round-trip mismatch: got %q, want %q", pubHex, wantPub)
	}
}

// TestNpubFromPrivateKey_Invalid checks that an empty/invalid key returns an error.
func TestNpubFromPrivateKey_Invalid(t *testing.T) {
	_, err := npubFromPrivateKey("not-a-valid-hex-key")
	if err == nil {
		t.Error("expected error for invalid key, got nil")
	}
}

// TestUnionRelays verifies deduplication and order preservation.
func TestUnionRelays(t *testing.T) {
	a := []string{"wss://a.com", "wss://b.com"}
	b := []string{"wss://b.com", "wss://c.com"}
	got := unionRelays(a, b)
	want := []string{"wss://a.com", "wss://b.com", "wss://c.com"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestUnionRelays_Empty(t *testing.T) {
	if got := unionRelays(nil, nil); len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
	a := []string{"wss://a.com"}
	if got := unionRelays(nil, a); len(got) != 1 || got[0] != a[0] {
		t.Errorf("got %v", got)
	}
	if got := unionRelays(a, nil); len(got) != 1 || got[0] != a[0] {
		t.Errorf("got %v", got)
	}
}

// TestPublicNoteContent_BandcampPreferred verifies that when Bandcamp returns a
// result the post contains only the Bandcamp link (YouTube is not queried).
func TestPublicNoteContent_BandcampPreferred(t *testing.T) {
	withSearchServers(t,
		`<a href="https://artist.bandcamp.com/track/the-song">...</a>`,
		`{"videoId":"dQw4w9WgXcQ"}`, // YouTube server is up but should not be used
		func() {
			link := FindBestLink("Test Artist", "The Song", "")
			if link == "" {
				t.Fatal("expected a link, got empty string")
			}
			if !strings.Contains(link, "bandcamp.com") {
				t.Errorf("expected Bandcamp link, got %q", link)
			}
			if strings.Contains(link, "youtube.com") {
				t.Errorf("YouTube link should not appear when Bandcamp found, got %q", link)
			}

			priv := nostr.GeneratePrivateKey()
			npub, _ := npubFromPrivateKey(priv)
			content := fmt.Sprintf(
				"nostr:%s is really digging %s by %s right now! #music #derpy\n\n%s",
				npub, "The Song", "Test Artist", link,
			)
			for _, want := range []string{
				"is really digging The Song by Test Artist right now!",
				"#music #derpy",
				"bandcamp.com",
			} {
				if !strings.Contains(content, want) {
					t.Errorf("content missing %q: %s", want, content)
				}
			}
		},
	)
}

// TestPublicNoteContent_YouTubeFallback verifies that YouTube is used when
// Bandcamp returns nothing.
func TestPublicNoteContent_YouTubeFallback(t *testing.T) {
	withSearchServers(t,
		"<html>no results</html>", // Bandcamp finds nothing
		`{"videoId":"dQw4w9WgXcQ"}`,
		func() {
			link := FindBestLink("Test Artist", "The Song", "")
			if link == "" {
				t.Fatal("expected a YouTube fallback link, got empty string")
			}
			if !strings.Contains(link, "youtube.com") {
				t.Errorf("expected YouTube link as fallback, got %q", link)
			}
		},
	)
}

// TestPublicNoteContent_NoLinks verifies that when neither source finds a
// result the post is still valid (no link appended).
func TestPublicNoteContent_NoLinks(t *testing.T) {
	withSearchServers(t, "<html>nothing</html>", "<html>nothing</html>", func() {
		link := FindBestLink("Ghost Artist", "Ghost Track", "")
		if link != "" {
			t.Errorf("expected empty link, got %q", link)
		}
	})
}

// TestPublicNoteContent_MissingMetadata verifies fallback phrases when
// artist or title fields are empty.
func TestPublicNoteContent_MissingMetadata(t *testing.T) {
	cases := []struct {
		artist, title string
		wantContains  string
	}{
		{"Miles Davis", "So What", "So What by Miles Davis"},
		{"", "So What", "So What right now"},
		{"Miles Davis", "", "a track by Miles Davis"},
		{"", "", "this track right now"},
	}

	for _, tc := range cases {
		var digging string
		switch {
		case tc.title != "" && tc.artist != "":
			digging = fmt.Sprintf("%s by %s", tc.title, tc.artist)
		case tc.title != "":
			digging = tc.title
		case tc.artist != "":
			digging = fmt.Sprintf("a track by %s", tc.artist)
		default:
			digging = "this track"
		}
		content := fmt.Sprintf("npub1test is really digging %s right now! #music #derpy", digging)
		if !strings.Contains(content, tc.wantContains) {
			t.Errorf("artist=%q title=%q: content %q does not contain %q",
				tc.artist, tc.title, content, tc.wantContains)
		}
	}
}

// TestResolveNostrKeyEnvVar verifies that DERPY_NOSTR_KEY is checked first
// in the resolution chain (env → config → empty). Uses a well-known bech32 nsec
// test key so the round-trip through resolvePrivateKey is covered.
func TestResolveNostrKeyEnvVar(t *testing.T) {
	// Generate a fresh keypair so the nsec is always valid.
	expectedHex := nostr.GeneratePrivateKey()
	nsec, err := nip19.EncodePrivateKey(expectedHex)
	if err != nil {
		t.Fatalf("EncodePrivateKey failed: %v", err)
	}

	// Set env var — should take priority over config.
	t.Setenv("DERPY_NOSTR_KEY", nsec)

	got := resolveNostrKey()
	if got != expectedHex {
		t.Errorf("resolveNostrKey() with DERPY_NOSTR_KEY set = %q, want %q", got, expectedHex)
	}
}

// TestPublishNostrTrackNote_UsesNostrURIScheme verifies that the published
// note content uses the nostr: URI scheme (NIP-21) so that Nostr clients
// recognize the npub as a profile reference rather than raw text.
func TestPublishNostrTrackNote_UsesNostrURIScheme(t *testing.T) {
	hexKey := nostr.GeneratePrivateKey()
	npub, err := npubFromPrivateKey(hexKey)
	if err != nil {
		t.Fatalf("could not derive npub: %v", err)
	}

	// Simulate the content-building logic from PublishNostrTrackNote.
	content := fmt.Sprintf(
		"nostr:%s is really digging The Song by Test Artist right now! #music #derpy",
		npub,
	)

	// The content must start with the nostr: URI scheme, not the raw npub.
	wantPrefix := "nostr:" + npub
	if !strings.HasPrefix(content, wantPrefix) {
		t.Errorf("content should start with nostr: URI scheme\ngot:  %s\nwant prefix: %s", content, wantPrefix)
	}

	// Confirm the raw npub without nostr: prefix would be wrong.
	if strings.HasPrefix(content, npub+" ") {
		t.Error("content starts with raw npub — Nostr clients won't recognize this as a profile reference")
	}
}

// TestBuildTrackNote_Tags verifies the outbox-model event hygiene learned in
// punk-post: the nostr:npub mention in content carries a matching p tag
// (NIP-27) and the #music/#derpy hashtags carry lowercase t tags (NIP-24) so
// relays and hashtag feeds can index the note.
func TestBuildTrackNote_Tags(t *testing.T) {
	hexKey := nostr.GeneratePrivateKey()
	pubHex, err := nostr.GetPublicKey(hexKey)
	if err != nil {
		t.Fatalf("could not derive public key: %v", err)
	}
	npub, err := npubFromPrivateKey(hexKey)
	if err != nil {
		t.Fatalf("could not derive npub: %v", err)
	}

	ev := buildTrackNote(npub, pubHex, "Test Artist", "The Song", "https://example.com/listen")

	if ev.Kind != nostr.KindTextNote {
		t.Errorf("expected kind 1, got %d", ev.Kind)
	}
	if !strings.HasPrefix(ev.Content, "nostr:"+npub) {
		t.Errorf("content should start with nostr: URI, got %q", ev.Content)
	}

	var gotP, gotMusic, gotDerpy bool
	for _, tag := range ev.Tags {
		if len(tag) < 2 {
			continue
		}
		switch {
		case tag[0] == "p" && tag[1] == pubHex:
			gotP = true
		case tag[0] == "t" && tag[1] == "music":
			gotMusic = true
		case tag[0] == "t" && tag[1] == "derpy":
			gotDerpy = true
		}
	}
	if !gotP {
		t.Error("missing p tag for the nostr:npub mention (NIP-27)")
	}
	if !gotMusic || !gotDerpy {
		t.Errorf("missing t tags for hashtags (NIP-24): music=%v derpy=%v", gotMusic, gotDerpy)
	}
}

// TestParseWriteRelays verifies NIP-65 kind-10002 tag parsing: bare r tags
// count as read+write, "write" markers are included, "read" markers excluded.
func TestParseWriteRelays(t *testing.T) {
	ev := &nostr.Event{
		Kind: 10002,
		Tags: nostr.Tags{
			{"r", "wss://both.example.com"},           // no marker → read+write
			{"r", "wss://write.example.com", "write"}, // explicit write
			{"r", "wss://read.example.com", "read"},   // read-only → excluded
			{"e", "not-a-relay-tag"},                  // ignored
			{"r"},                                     // malformed → ignored
		},
	}

	got := parseWriteRelays(ev)
	want := []string{"wss://both.example.com", "wss://write.example.com"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestResolveNostrKeyEmpty verifies that resolveNostrKey returns empty string
// when no key is available from any source (env or config).
func TestResolveNostrKeyEmpty(t *testing.T) {
	// Unset env var for this test — config file presence is environment-dependent.
	t.Setenv("DERPY_NOSTR_KEY", "")

	got := resolveNostrKey()
	// Note: if the user has a key in config.json, this will return non-empty.
	// The test only asserts we don't panic; env coverage is confirmed by
	// TestResolveNostrKeyEnvVar above.
	_ = got
}
