package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// configDir returns the path to derpy's config directory (~/.config/derpy).
func configDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "derpy"), nil
}

// configFilePath returns the path to the config file.
func configFilePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Config holds all persisted derpy configuration.
type Config struct {
	ListenBrainzToken string `json:"listenbrainz_token,omitempty"`
	DefaultSource     string `json:"default_source,omitempty"`
	// NostrPrivateKey stores the user's Nostr private key (raw hex).
	// Stored as hex so we are not confused about bech32 vs hex across reads.
	// The file is written with 0o600 permissions, but users should still treat
	// this value as a secret and never share the config file.
	NostrPrivateKey string `json:"nostr_private_key,omitempty"`
	// BlueskyHandle is the user's Bluesky handle (e.g. "user.bsky.social").
	BlueskyHandle string `json:"bluesky_handle,omitempty"`
	// BlueskyAppPassword is an app-specific password created in Bluesky Settings.
	BlueskyAppPassword string `json:"bluesky_app_password,omitempty"`
	// NostrRelays is the list of relay WebSocket URLs derpy publishes to and
	// fetches from. When empty, defaultNostrRelays in nostr.go is used.
	NostrRelays []string `json:"nostr_relays,omitempty"`
	// BlossomServers is the list of Blossom server base URLs used for audio
	// file uploads and downloads. When empty, defaultBlossomServers in
	// blossom.go is used.
	BlossomServers []string `json:"blossom_servers,omitempty"`
}

// LoadNostrRelays returns the user-configured relay list, falling back to
// defaultNostrRelays when none have been configured.
func LoadNostrRelays() []string {
	cfg, err := LoadConfig()
	if err == nil && len(cfg.NostrRelays) > 0 {
		return cfg.NostrRelays
	}
	return defaultNostrRelays
}

// LoadConfig reads the config file; returns an empty Config if the file does not exist.
func LoadConfig() (*Config, error) {
	path, err := configFilePath()
	if err != nil {
		return &Config{}, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return &Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return &Config{}, err
	}
	return &cfg, nil
}

// SaveConfig writes the config to disk, creating directories as needed.
func SaveConfig(cfg *Config) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	path := filepath.Join(dir, "config.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// LoadListenBrainzToken returns the token from the config file if set,
// otherwise falls back to the LISTENBRAINZ_TOKEN environment variable.
func LoadListenBrainzToken() string {
	cfg, err := LoadConfig()
	if err == nil && cfg.ListenBrainzToken != "" {
		return cfg.ListenBrainzToken
	}
	return os.Getenv("LISTENBRAINZ_TOKEN")
}

// resolveBskyConfig returns the user's Bluesky handle and app password
// following the resolution order: DERPY_BSKY_HANDLE / DERPY_BSKY_APP_PASSWORD
// env vars → config file → empty strings.
func resolveBskyConfig() (handle, appPassword string) {
	handle = os.Getenv("DERPY_BSKY_HANDLE")
	appPassword = os.Getenv("DERPY_BSKY_APP_PASSWORD")
	if handle != "" && appPassword != "" {
		return
	}
	cfg, err := LoadConfig()
	if err == nil {
		if handle == "" {
			handle = cfg.BlueskyHandle
		}
		if appPassword == "" {
			appPassword = cfg.BlueskyAppPassword
		}
	}
	return
}
