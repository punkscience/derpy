package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

// rootCmd builds the top-level Cobra command for dirplay.
func rootCmd() *cobra.Command {
	var noTUI bool

	cmd := &cobra.Command{
		Use:   "dirplay [--no-tui] <music_directory> [keywords...]",
		Short: "Terminal music player — shuffles and plays a directory of audio files",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			musicDir := args[0]
			keywords := args[1:]
			return runPlayer(musicDir, keywords, noTUI)
		},
	}

	cmd.Flags().BoolVar(&noTUI, "no-tui", false, "Play without the terminal UI")

	// Subcommands
	cmd.AddCommand(tokenCmd())

	return cmd
}

// tokenCmd returns the 'token' subcommand that stores a ListenBrainz token.
func tokenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "token <listenbrainz_token>",
		Short: "Save a ListenBrainz user token to the config file",
		Long: `Save your ListenBrainz user token to ~/.config/dirplay/config.json.

The token is read from the config file on startup (before checking the
LISTENBRAINZ_TOKEN environment variable). Pass an empty string to clear it:

  dirplay token ""`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token := args[0]

			cfg, err := LoadConfig()
			if err != nil {
				// Non-fatal: start with a blank config
				cfg = &Config{}
			}

			cfg.ListenBrainzToken = token
			if err := SaveConfig(cfg); err != nil {
				return fmt.Errorf("could not save config: %w", err)
			}

			if token == "" {
				fmt.Println("ListenBrainz token cleared.")
			} else {
				path, _ := configFilePath()
				fmt.Printf("ListenBrainz token saved to %s\n", path)
			}
			return nil
		},
	}
}

// runPlayer scans the directory, filters by keywords, shuffles (if no keywords), and starts playback.
func runPlayer(musicDir string, keywords []string, noTUI bool) error {
	if _, err := os.Stat(musicDir); os.IsNotExist(err) {
		return fmt.Errorf("directory does not exist: %s", musicDir)
	}

	playlist, err := scanMusicDirectory(musicDir, keywords)
	if err != nil {
		return fmt.Errorf("error scanning directory: %w", err)
	}
	if len(playlist) == 0 {
		if len(keywords) > 0 {
			return fmt.Errorf("no audio files matching %v found in directory: %s", keywords, musicDir)
		}
		return fmt.Errorf("no audio files found in directory: %s", musicDir)
	}

	// Always shuffle by default
	shufflePlaylist(playlist)

	if noTUI {
		return runNoTUI(playlist)
	}
	return runTUI(playlist)
}

// runNoTUI plays the playlist sequentially without a terminal UI.
func runNoTUI(playlist []string) error {
	player := NewAudioPlayer()
	defer player.Shutdown()

	for _, track := range playlist {
		fmt.Printf("Playing: %s\n", track)

		if err := player.LoadTrack(track); err != nil {
			fmt.Fprintf(os.Stderr, "Error loading track %s: %v\n", track, err)
			continue
		}
		if err := player.Play(); err != nil {
			fmt.Fprintf(os.Stderr, "Error playing track %s: %v\n", track, err)
			continue
		}

		for !player.HasEnded() {
			// Poll position so that ListenBrainz scrobbling fires at 25%.
			_ = player.GetPosition()
			time.Sleep(100 * time.Millisecond)
		}
		player.Stop()
	}

	fmt.Println("Playlist finished.")
	return nil
}

// runTUI starts the Bubble Tea TUI player and, if the session D-Bus is
// available, registers an MPRIS2 service so external clients (playerctl,
// Waybar, etc.) can query and control playback.
func runTUI(playlist []string) error {
	model := NewPlayerModel(playlist)
	program := tea.NewProgram(model, tea.WithAltScreen())

	// Create the MPRIS service before starting the program so the D-Bus name
	// is registered before the first PropertiesChanged signal fires.
	mpris, err := NewMPRISService(model, program)
	if err != nil {
		// Non-fatal: log and continue without MPRIS.
		fmt.Fprintf(os.Stderr, "MPRIS: failed to start D-Bus service: %v\n", err)
	}
	// Give the model a reference so it can emit state-change signals.
	model.mpris = mpris
	defer mpris.Close()

	if _, err := program.Run(); err != nil {
		return fmt.Errorf("error running TUI: %w", err)
	}
	return nil
}

// scanMusicDirectory recursively scans a directory for audio files, optionally filtering by keywords.
func scanMusicDirectory(root string, keywords []string) ([]string, error) {
	var playlist []string

	audioExts := map[string]bool{
		".mp3":  true,
		".wav":  true,
		".flac": true,
		".ogg":  true,
		".m4a":  true,
		".aac":  true,
	}

	// Prepare keywords for case-insensitive matching
	var lowerKeywords []string
	for _, k := range keywords {
		lowerKeywords = append(lowerKeywords, strings.ToLower(k))
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if audioExts[strings.ToLower(filepath.Ext(path))] {
			// If keywords are provided, check if any match the relative path or filename
			if len(lowerKeywords) > 0 {
				rel, err := filepath.Rel(root, path)
				if err != nil {
					rel = path
				}
				relLower := strings.ToLower(rel)

				matched := false
				for _, kw := range lowerKeywords {
					if strings.Contains(relLower, kw) {
						matched = true
						break
					}
				}
				if !matched {
					return nil
				}
			}
			playlist = append(playlist, path)
		}
		return nil
	})

	return playlist, err
}
