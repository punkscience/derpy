package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"dirplay/internal/filter"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

// rootCmd builds the top-level Cobra command for dirplay.
func rootCmd() *cobra.Command {
	var noTUI bool
	var source string
	var setDefaultSource string

	cmd := &cobra.Command{
		Use:   "dirplay [--source <dir>] [--set-default-source <dir>] [expression...]",
		Short: "Terminal music player — shuffles and plays a directory of audio files",
		Long: `dirplay scans a directory recursively for audio files and plays them shuffled.

Keyword expression syntax (AND binds tighter than OR):
  jazz AND piano          path must contain both "jazz" and "piano"
  jazz OR blues           path must contain "jazz" or "blues"
  (jazz OR blues) AND piano
  "jazz piano"            exact phrase match

Multiple expression arguments are joined with OR:
  dirplay jazz blues  →  jazz OR blues

Matching is case-insensitive against the full file path.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Handle --set-default-source before anything else.
			if setDefaultSource != "" {
				cfg, err := LoadConfig()
				if err != nil {
					cfg = &Config{}
				}
				cfg.DefaultSource = setDefaultSource
				if err := SaveConfig(cfg); err != nil {
					return fmt.Errorf("could not save config: %w", err)
				}
				path, _ := configFilePath()
				fmt.Printf("Default source set to %q in %s\n", setDefaultSource, path)
				return nil
			}

			// Resolve the music directory: flag > config default > error.
			musicDir := source
			if musicDir == "" {
				cfg, err := LoadConfig()
				if err == nil {
					musicDir = cfg.DefaultSource
				}
			}
			if musicDir == "" {
				return fmt.Errorf("no source directory specified — use --source <dir> or set a default with --set-default-source <dir>")
			}

			// Join multiple args with OR so "jazz blues" means "jazz OR blues".
			exprStr := strings.Join(args, " OR ")
			return runPlayer(musicDir, exprStr, noTUI)
		},
	}

	cmd.Flags().BoolVar(&noTUI, "no-tui", false, "Play without the terminal UI")
	cmd.Flags().StringVar(&source, "source", "", "Directory to scan for audio files")
	cmd.Flags().StringVar(&setDefaultSource, "set-default-source", "", "Save a default source directory to the config and exit")

	// Subcommands
	cmd.AddCommand(tokenCmd())
	cmd.AddCommand(nostrKeyCmd())

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

// nostrKeyCmd returns the 'nostr-key' subcommand that stores a Nostr private key.
func nostrKeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "nostr-key <nsec_or_hex_key>",
		Short: "Save a Nostr private key to the config file",
		Long: `Save your Nostr private key to ~/.config/dirplay/config.json.

The key is used to sign and publish track-earmark notes to public Nostr relays
when you press [N] while a track is playing.

You can pass either a bech32-encoded nsec key (nsec1...) or a raw 64-character
hex string.  The key is stored as hex internally.

IMPORTANT: Your private key is a secret — anyone who has it can post as you.
The config file is stored with 0600 permissions, but treat it like a password.

Pass an empty string to clear the saved key:

  dirplay nostr-key ""`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rawInput := args[0]

			cfg, err := LoadConfig()
			if err != nil {
				cfg = &Config{}
			}

			if rawInput == "" {
				cfg.NostrPrivateKey = ""
				if err := SaveConfig(cfg); err != nil {
					return fmt.Errorf("could not save config: %w", err)
				}
				fmt.Println("Nostr private key cleared.")
				return nil
			}

			// Validate and normalise to hex before storing.
			hexKey, err := resolvePrivateKey(rawInput)
			if err != nil {
				return fmt.Errorf("invalid key: %w", err)
			}

			cfg.NostrPrivateKey = hexKey
			if err := SaveConfig(cfg); err != nil {
				return fmt.Errorf("could not save config: %w", err)
			}
			path, _ := configFilePath()
			fmt.Printf("Nostr private key saved to %s\n", path)
			fmt.Println("Press [N] in dirplay to earmark the current track on Nostr.")
			return nil
		},
	}
}

// runPlayer scans the directory, optionally filters by a keyword expression,
// shuffles, and starts playback. exprStr may be empty to play everything.
func runPlayer(musicDir string, exprStr string, noTUI bool) error {
	if _, err := os.Stat(musicDir); os.IsNotExist(err) {
		return fmt.Errorf("directory does not exist: %s", musicDir)
	}

	playlist, err := scanMusicDirectory(musicDir, exprStr)
	if err != nil {
		return fmt.Errorf("error scanning directory: %w", err)
	}
	if len(playlist) == 0 {
		if exprStr != "" {
			return fmt.Errorf("no audio files matching %q found in directory: %s", exprStr, musicDir)
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
	mpris, err := NewMPRISService(program)
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

// scanMusicDirectory recursively scans root for audio files. If exprStr is
// non-empty it is parsed as a boolean keyword expression (AND/OR/parentheses)
// and only matching paths are returned. Matching is case-insensitive against
// the full absolute path.
func scanMusicDirectory(root string, exprStr string) ([]string, error) {
	audioExts := map[string]bool{
		".mp3":  true,
		".wav":  true,
		".flac": true,
		".ogg":  true,
		".m4a":  true,
		".aac":  true,
	}

	// Parse the expression once before the walk; fail fast on syntax errors.
	var expr filter.Expr
	if exprStr != "" {
		var err error
		expr, err = filter.ParseExpr(exprStr)
		if err != nil {
			return nil, fmt.Errorf("invalid keyword expression: %w", err)
		}
	}

	var all []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if audioExts[strings.ToLower(filepath.Ext(path))] {
			all = append(all, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if expr == nil {
		return all, nil
	}
	return filter.FilterExpr(all, expr), nil
}
