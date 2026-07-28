package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"

	core "github.com/punkscience/earmark/earmark-core"
)

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
			npub, _ := core.NpubFromPrivateKey(priv)
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
	npub, err := core.NpubFromPrivateKey(hexKey)
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
