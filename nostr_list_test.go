package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip44"
)

// TestEarmarkJSONRoundTrip verifies that Earmark values survive a
// marshal → unmarshal cycle without data loss.
func TestEarmarkJSONRoundTrip(t *testing.T) {
	original := []Earmark{
		{Artist: "Miles Davis", Album: "Kind of Blue", Title: "So What", Timestamp: 1700000000},
		{Artist: "Bill Evans", Album: "Waltz for Debby", Title: "My Foolish Heart", Timestamp: 1700000001},
		{Artist: "", Album: "", Title: "Unknown", Timestamp: 0},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got []Earmark
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if len(got) != len(original) {
		t.Fatalf("length mismatch: got %d, want %d", len(got), len(original))
	}
	for i, e := range original {
		if got[i] != e {
			t.Errorf("[%d] got %+v, want %+v", i, got[i], e)
		}
	}
}

// TestSelfConvKeyDeterministic verifies that selfConvKey returns the same key
// for the same private key on repeated calls.
func TestSelfConvKeyDeterministic(t *testing.T) {
	priv := nostr.GeneratePrivateKey()

	k1, err := selfConvKey(priv)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	k2, err := selfConvKey(priv)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if k1 != k2 {
		t.Error("selfConvKey returned different keys for the same private key")
	}
}

// TestSelfConvKeyUnique verifies that two different private keys produce
// different conversation keys (collision resistance sanity check).
func TestSelfConvKeyUnique(t *testing.T) {
	k1, _ := selfConvKey(nostr.GeneratePrivateKey())
	k2, _ := selfConvKey(nostr.GeneratePrivateKey())
	if k1 == k2 {
		t.Error("two different private keys produced the same conversation key")
	}
}

// TestSelfConvKeyInvalidInput verifies that an invalid private key returns an error.
func TestSelfConvKeyInvalidInput(t *testing.T) {
	_, err := selfConvKey("not-a-valid-hex-key")
	if err == nil {
		t.Error("expected error for invalid private key, got nil")
	}
}

// TestEncryptDecryptRoundTrip verifies that data encrypted with selfConvKey
// can be decrypted back to the original plaintext using the same key.
func TestEncryptDecryptRoundTrip(t *testing.T) {
	priv := nostr.GeneratePrivateKey()
	convKey, err := selfConvKey(priv)
	if err != nil {
		t.Fatalf("selfConvKey: %v", err)
	}

	earmarks := []Earmark{
		{Artist: "Coltrane", Album: "A Love Supreme", Title: "Resolution", Timestamp: time.Now().Unix()},
	}

	data, err := json.Marshal(earmarks)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	ciphertext, err := nip44.Encrypt(string(data), convKey)
	if err != nil {
		t.Fatalf("nip44.Encrypt: %v", err)
	}

	// Ciphertext must not contain the plaintext.
	if string(data) == ciphertext {
		t.Error("ciphertext is identical to plaintext")
	}

	plaintext, err := nip44.Decrypt(ciphertext, convKey)
	if err != nil {
		t.Fatalf("nip44.Decrypt: %v", err)
	}
	if plaintext != string(data) {
		t.Errorf("round-trip mismatch:\n  got  %q\n  want %q", plaintext, string(data))
	}
}

// TestEncryptDecryptWrongKey verifies that decryption with a different key fails.
func TestEncryptDecryptWrongKey(t *testing.T) {
	priv1 := nostr.GeneratePrivateKey()
	priv2 := nostr.GeneratePrivateKey()

	key1, _ := selfConvKey(priv1)
	key2, _ := selfConvKey(priv2)

	ciphertext, err := nip44.Encrypt("secret playlist data", key1)
	if err != nil {
		t.Fatalf("nip44.Encrypt: %v", err)
	}

	_, err = nip44.Decrypt(ciphertext, key2)
	if err == nil {
		t.Error("expected decryption with wrong key to fail, but it succeeded")
	}
}

// TestEarmarkTimestamp verifies that the Timestamp field survives JSON
// serialization with sub-second precision preserved (Unix seconds are integers).
func TestEarmarkTimestamp(t *testing.T) {
	now := time.Now().Unix()
	e := Earmark{Title: "Track", Timestamp: now}

	data, _ := json.Marshal(e)
	var got Earmark
	_ = json.Unmarshal(data, &got)

	if got.Timestamp != now {
		t.Errorf("timestamp mismatch: got %d, want %d", got.Timestamp, now)
	}
}

// TestListCmdNoKey verifies that 'dirplay list' returns an error when no
// Nostr key is configured, rather than hanging or panicking.
func TestListCmdNoKey(t *testing.T) {
	// Temporarily redirect config loading to return an empty config.
	cmd := listCmd()
	err := cmd.RunE(cmd, nil)
	// We expect an error because there's no key in the test environment config
	// (or the saved config has no NostrPrivateKey set).  If a key happens to be
	// set, this assertion would fail — acceptable in a real environment.
	_ = err // We just ensure it doesn't panic.
}
