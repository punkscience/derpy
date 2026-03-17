package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// configDir returns the path to dirplay's config directory (~/.config/dirplay).
func configDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "dirplay"), nil
}

// configFilePath returns the path to the config file.
func configFilePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Config holds all persisted dirplay configuration.
type Config struct {
	ListenBrainzToken string `json:"listenbrainz_token,omitempty"`
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
