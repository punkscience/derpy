package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

const (
	// blossomChunkSize is the maximum size of each plaintext chunk before
	// encryption. 16 MiB sits safely under public servers' 20 MiB upload limits
	// after AES-256-GCM overhead (12-byte nonce + 16-byte auth tag = 28 bytes).
	blossomChunkSize = 16 * 1024 * 1024

	// blossomAuthKind is the Nostr event kind for BUD-11 upload/delete auth.
	blossomAuthKind = 24242
)

// defaultBlossomServers is the fallback list used when the user has not
// configured any servers. All three accept uploads from any authenticated pubkey.
var defaultBlossomServers = []string{
	"https://blossom.band",
	"https://cdn.satellite.earth",
	"https://nostr.build",
}

// LoadBlossomServers returns the user-configured Blossom server list, falling
// back to defaultBlossomServers when none have been configured.
func LoadBlossomServers() []string {
	cfg, err := LoadConfig()
	if err == nil && len(cfg.BlossomServers) > 0 {
		return cfg.BlossomServers
	}
	return defaultBlossomServers
}

// BlossomChunk describes one encrypted chunk of an uploaded audio file.
type BlossomChunk struct {
	Index   int      `json:"index"`
	SHA256  string   `json:"sha256"`           // hex SHA-256 of the encrypted chunk
	Size    int      `json:"size"`             // byte length of the encrypted chunk
	Servers []string `json:"servers"`          // servers that hold this chunk
}

// BlossomManifest is stored inside the NIP-51 earmark when a file has been
// uploaded to Blossom servers. The key field is the base64-encoded AES-256-GCM
// encryption key shared by all chunks.
type BlossomManifest struct {
	Key    string         `json:"key"`    // base64 AES-256-GCM key (32 bytes)
	Chunks []BlossomChunk `json:"chunks"`
}

// blossomBlobDescriptor is the JSON body returned by a Blossom server after a
// successful upload (BUD-01).
type blossomBlobDescriptor struct {
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int    `json:"size"`
	Type     string `json:"type"`
	Uploaded int64  `json:"uploaded"`
}

// --- Encryption helpers --------------------------------------------------

// generateEncryptionKey returns 32 cryptographically random bytes for use as
// an AES-256-GCM key.
func generateEncryptionKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("could not generate encryption key: %w", err)
	}
	return key, nil
}

// encryptChunk encrypts data with AES-256-GCM using a random nonce.
// Output format: [12-byte nonce][ciphertext][16-byte GCM tag]
// The SHA-256 of the output is used as the Blossom blob identifier.
func encryptChunk(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("could not create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("could not create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("could not generate nonce: %w", err)
	}

	// Seal appends ciphertext+tag to nonce so the output is self-contained.
	return gcm.Seal(nonce, nonce, data, nil), nil
}

// decryptChunk reverses encryptChunk. It expects [nonce][ciphertext+tag].
func decryptChunk(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("could not create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("could not create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("encrypted chunk too short (%d bytes)", len(data))
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed (wrong key or corrupt data): %w", err)
	}
	return plain, nil
}

// sha256Hex returns the lower-case hex SHA-256 digest of data.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// --- BUD-11 authentication -----------------------------------------------

// blossomAuthToken creates a signed kind-24242 Nostr event and returns it
// base64-encoded for use as a Bearer token in Blossom HTTP requests.
//
// action must be "upload", "get", or "delete".
// sha256hex is the hex SHA-256 of the blob being acted on.
func blossomAuthToken(hexPrivKey, sha256hex, action string) (string, error) {
	pubHex, err := nostr.GetPublicKey(hexPrivKey)
	if err != nil {
		return "", fmt.Errorf("could not derive public key: %w", err)
	}

	ev := nostr.Event{
		PubKey:    pubHex,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      blossomAuthKind,
		Content:   fmt.Sprintf("%s %s", action, sha256hex),
		Tags: nostr.Tags{
			{"t", action},
			{"x", sha256hex},
			// Token valid for 5 minutes — enough for a single request.
			{"expiration", fmt.Sprintf("%d", time.Now().Add(5*time.Minute).Unix())},
		},
	}
	if err := ev.Sign(hexPrivKey); err != nil {
		return "", fmt.Errorf("could not sign auth event: %w", err)
	}

	data, err := json.Marshal(ev)
	if err != nil {
		return "", fmt.Errorf("could not marshal auth event: %w", err)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// --- Network: upload / download ------------------------------------------

// uploadChunk uploads a single encrypted chunk to one Blossom server.
// It creates a new BUD-11 auth token per call (tokens are single-use by convention).
func uploadChunk(ctx context.Context, serverURL string, data []byte, sha256hex, hexPrivKey string) error {
	token, err := blossomAuthToken(hexPrivKey, sha256hex, "upload")
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		serverURL+"/upload", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("could not build upload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Nostr "+token)
	req.ContentLength = int64(len(data))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, body)
	}
	return nil
}

// uploadChunkToServers uploads data to all servers concurrently and requires
// at least one to succeed. Returns the list of servers that accepted the chunk.
func uploadChunkToServers(ctx context.Context, servers []string, data []byte, sha256hex, hexPrivKey string) ([]string, error) {
	type result struct {
		server string
		err    error
	}
	ch := make(chan result, len(servers))

	for _, s := range servers {
		s := s
		go func() {
			ch <- result{s, uploadChunk(ctx, s, data, sha256hex, hexPrivKey)}
		}()
	}

	var succeeded []string
	var lastErr error
	for range servers {
		r := <-ch
		if r.err != nil {
			lastErr = r.err
		} else {
			succeeded = append(succeeded, r.server)
		}
	}

	if len(succeeded) == 0 {
		return nil, fmt.Errorf("chunk %s: upload failed on all servers: %w", sha256hex[:8], lastErr)
	}
	return succeeded, nil
}

// downloadChunk fetches a single encrypted chunk from a Blossom server and
// verifies its SHA-256 matches the expected value.
func downloadChunk(ctx context.Context, serverURL, sha256hex string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		serverURL+"/"+sha256hex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not build download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	// Verify integrity before returning.
	if got := sha256Hex(data); got != sha256hex {
		return nil, fmt.Errorf("SHA-256 mismatch: got %s, want %s", got[:8], sha256hex[:8])
	}
	return data, nil
}

// downloadChunkWithFallback tries each server in the chunk's server list until
// one succeeds, verifying SHA-256 on each attempt.
func downloadChunkWithFallback(ctx context.Context, chunk BlossomChunk) ([]byte, error) {
	var lastErr error
	for _, server := range chunk.Servers {
		data, err := downloadChunk(ctx, server, chunk.SHA256)
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("chunk %d: all servers failed: %w", chunk.Index, lastErr)
}

// --- High-level operations -----------------------------------------------

// UploadProgress is called by UploadFile after each chunk is successfully
// uploaded so callers can display progress.
type UploadProgress func(chunksUploaded, chunksTotal int)

// UploadFile splits filePath into 16 MiB chunks, encrypts each with a fresh
// random AES-256-GCM key, uploads each chunk to all provided servers
// concurrently, and returns the BlossomManifest needed to reassemble the file.
//
// progress may be nil.
func UploadFile(ctx context.Context, hexPrivKey, filePath string, servers []string, progress UploadProgress) (*BlossomManifest, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("could not open file: %w", err)
	}
	defer f.Close()

	key, err := generateEncryptionKey()
	if err != nil {
		return nil, err
	}

	var chunks []BlossomChunk
	buf := make([]byte, blossomChunkSize)
	index := 0

	for {
		n, readErr := io.ReadFull(f, buf)
		if n == 0 {
			break
		}

		encrypted, err := encryptChunk(buf[:n], key)
		if err != nil {
			return nil, fmt.Errorf("chunk %d: encryption failed: %w", index, err)
		}

		sum := sha256Hex(encrypted)
		accepted, err := uploadChunkToServers(ctx, servers, encrypted, sum, hexPrivKey)
		if err != nil {
			return nil, err
		}

		chunks = append(chunks, BlossomChunk{
			Index:   index,
			SHA256:  sum,
			Size:    len(encrypted),
			Servers: accepted,
		})
		index++

		if progress != nil {
			progress(index, 0) // total unknown until EOF; caller can use -1 to indicate streaming
		}

		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("could not read file: %w", readErr)
		}
	}

	return &BlossomManifest{
		Key:    base64.StdEncoding.EncodeToString(key),
		Chunks: chunks,
	}, nil
}

// DownloadProgress is called by DownloadAndReassemble after each chunk is
// fetched and decrypted so callers can display progress.
type DownloadProgress func(chunksDownloaded, chunksTotal int)

// DownloadAndReassemble fetches all chunks in the manifest in parallel,
// decrypts and reassembles them, and writes the result to a temp file.
// Returns the temp file path; the caller is responsible for deleting it.
//
// progress may be nil.
func DownloadAndReassemble(ctx context.Context, manifest *BlossomManifest, progress DownloadProgress) (string, error) {
	key, err := base64.StdEncoding.DecodeString(manifest.Key)
	if err != nil {
		return "", fmt.Errorf("could not decode encryption key: %w", err)
	}

	total := len(manifest.Chunks)
	plainChunks := make([][]byte, total)

	// Download all chunks concurrently.
	var mu sync.Mutex
	var wg sync.WaitGroup
	errs := make([]error, total)
	downloaded := 0

	for _, chunk := range manifest.Chunks {
		chunk := chunk
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := downloadChunkWithFallback(ctx, chunk)
			if err != nil {
				errs[chunk.Index] = err
				return
			}
			plain, err := decryptChunk(data, key)
			if err != nil {
				errs[chunk.Index] = fmt.Errorf("chunk %d: %w", chunk.Index, err)
				return
			}
			plainChunks[chunk.Index] = plain

			mu.Lock()
			downloaded++
			if progress != nil {
				progress(downloaded, total)
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	// Check for any download/decrypt failures.
	for i, err := range errs {
		if err != nil {
			return "", fmt.Errorf("could not retrieve chunk %d: %w", i, err)
		}
	}

	// Write reassembled plaintext to a temp file.
	tmp, err := os.CreateTemp("", "dirplay-*.audio")
	if err != nil {
		return "", fmt.Errorf("could not create temp file: %w", err)
	}
	defer tmp.Close()

	for _, chunk := range plainChunks {
		if _, err := tmp.Write(chunk); err != nil {
			os.Remove(tmp.Name())
			return "", fmt.Errorf("could not write temp file: %w", err)
		}
	}

	return tmp.Name(), nil
}

// --- Server discovery (NIP-65 analogue for Blossom: kind 10063) ----------

// fetchBlossomServers fetches the user's kind-10063 Nostr event and returns
// their preferred Blossom server URLs. Returns nil if no event is found.
func fetchBlossomServers(ctx context.Context, pubHex string) []string {
	filter := nostr.Filter{
		Kinds:   []int{10063},
		Authors: []string{pubHex},
		Limit:   1,
	}
	ev := queryRelays(ctx, LoadNostrRelays(), filter)
	if ev == nil {
		return nil
	}

	var servers []string
	for _, tag := range ev.Tags {
		// kind-10063 uses ["server", "https://..."] tags.
		if len(tag) >= 2 && tag[0] == "server" {
			servers = append(servers, tag[1])
		}
	}
	return servers
}

// ResolveBlossomServers returns the servers to use for an upload or download:
// the user's kind-10063 servers (if any) unioned with the configured list.
// A short timeout is used for the Nostr lookup so a slow relay doesn't block.
func ResolveBlossomServers(hexPrivKey string) ([]string, error) {
	pubHex, err := nostr.GetPublicKey(hexPrivKey)
	if err != nil {
		return nil, fmt.Errorf("could not derive public key: %w", err)
	}

	discoverCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	nip10063 := fetchBlossomServers(discoverCtx, pubHex)
	cancel()

	return unionRelays(nip10063, LoadBlossomServers()), nil
}
