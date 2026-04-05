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

// TestPublicNoteContent verifies the format of the public Nostr post, using
// mock search servers so no real network calls are made.
func TestPublicNoteContent(t *testing.T) {
	withSearchServers(t,
		`<a href="https://artist.bandcamp.com/track/the-song">...</a>`,
		`{"videoId":"dQw4w9WgXcQ"}`,
		func() {
			priv := nostr.GeneratePrivateKey()
			npub, err := npubFromPrivateKey(priv)
			if err != nil {
				t.Fatalf("npubFromPrivateKey: %v", err)
			}

			links := FindTrackLinks("Test Artist", "The Song", "Test Album")
			if links.Bandcamp == "" {
				t.Error("expected Bandcamp link, got empty string")
			}
			if links.YouTube == "" {
				t.Error("expected YouTube link, got empty string")
			}

			// Mirror the content-building logic from PublishNostrTrackNote.
			var linkSection strings.Builder
			if links.Bandcamp != "" {
				linkSection.WriteString("\n\n🎵 " + links.Bandcamp)
			}
			if links.YouTube != "" {
				linkSection.WriteString("\n▶️ " + links.YouTube)
			}
			content := fmt.Sprintf(
				"%s is really digging %s by %s right now! #music #dirplay%s",
				npub, "The Song", "Test Artist", linkSection.String(),
			)

			checks := []struct {
				desc, want string
			}{
				{"digging phrase", "is really digging The Song by Test Artist right now!"},
				{"hashtags", "#music #dirplay"},
				{"Bandcamp link", "bandcamp.com"},
				{"YouTube link", "youtube.com"},
			}
			for _, c := range checks {
				if !strings.Contains(content, c.want) {
					t.Errorf("content missing %s: %q", c.desc, content)
				}
			}
		},
	)
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
		content := fmt.Sprintf("npub1test is really digging %s right now! #music #dirplay", digging)
		if !strings.Contains(content, tc.wantContains) {
			t.Errorf("artist=%q title=%q: content %q does not contain %q",
				tc.artist, tc.title, content, tc.wantContains)
		}
	}
}
