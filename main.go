package main

import (
	"context"
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
	cmd.AddCommand(listCmd())
	cmd.AddCommand(relayCmd())
	cmd.AddCommand(blossomCmd())
	cmd.AddCommand(uploadCmd())
	cmd.AddCommand(earmarksCmd())

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
when you press [E] while a track is playing.

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
			fmt.Println("Press [E] in dirplay to earmark the current track on Nostr.")
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

// listCmd returns the 'list' subcommand that prints the private earmark list.
func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Print your private Nostr earmark list",
		Long: `Fetch and decrypt your private earmark list from Nostr relays.

Requires a Nostr private key saved via 'dirplay nostr-key <key>'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := LoadConfig()
			if err != nil || cfg.NostrPrivateKey == "" {
				return fmt.Errorf("no Nostr private key configured — run: dirplay nostr-key <nsec_or_hex_key>")
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Fetching earmarks from Nostr...")

			earmarks, err := FetchEarmarks(cfg.NostrPrivateKey)
			if err != nil {
				return fmt.Errorf("could not fetch earmarks: %w", err)
			}

			if len(earmarks) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No earmarks found. Press [E] while a track is playing to add one.")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "\n%d earmark(s):\n\n", len(earmarks))
			for i, e := range earmarks {
				ts := time.Unix(e.Timestamp, 0).Format("2006-01-02 15:04")
				var desc string
				switch {
				case e.Artist != "" && e.Album != "" && e.Title != "":
					desc = fmt.Sprintf("%s — %s — %s", e.Artist, e.Album, e.Title)
				case e.Artist != "" && e.Title != "":
					desc = fmt.Sprintf("%s — %s", e.Artist, e.Title)
				case e.Title != "":
					desc = e.Title
				default:
					desc = "(unknown track)"
				}
				pathInfo := ""
			if e.Path != "" {
				pathInfo = "\n        " + e.Path
			}
			fmt.Fprintf(cmd.OutOrStdout(), " %3d.  %-60s  (%s)%s\n", i+1, desc, ts, pathInfo)
			}
			return nil
		},
	}
}

// relayCmd returns the 'relay' subcommand group for managing Nostr relays.
func relayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "relay",
		Short: "Manage the Nostr relay list",
		Long: `Manage the list of Nostr relay WebSocket URLs that dirplay publishes to
and fetches from.

When no relays are configured the built-in defaults are used. Adding even one
relay that you know accepts your pubkey is usually enough.`,
	}
	cmd.AddCommand(relayListCmd(), relayAddCmd(), relayRemoveCmd(), relayResetCmd())
	return cmd
}

func relayListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show the active relay list",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _ := LoadConfig()
			relays := LoadNostrRelays()
			if len(cfg.NostrRelays) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(using built-in defaults)")
			}
			for _, r := range relays {
				fmt.Fprintln(cmd.OutOrStdout(), r)
			}
			return nil
		},
	}
}

func relayAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <wss://relay.example.com>",
		Short: "Add a relay to the list",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := args[0]
			if !strings.HasPrefix(url, "wss://") && !strings.HasPrefix(url, "ws://") {
				return fmt.Errorf("relay URL must start with wss:// or ws://")
			}
			cfg, err := LoadConfig()
			if err != nil {
				cfg = &Config{}
			}
			// Start from defaults if this is the first custom relay.
			if len(cfg.NostrRelays) == 0 {
				cfg.NostrRelays = append([]string{}, defaultNostrRelays...)
			}
			for _, r := range cfg.NostrRelays {
				if r == url {
					fmt.Fprintf(cmd.OutOrStdout(), "%s is already in the relay list\n", url)
					return nil
				}
			}
			cfg.NostrRelays = append(cfg.NostrRelays, url)
			if err := SaveConfig(cfg); err != nil {
				return fmt.Errorf("could not save config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added %s\n", url)
			return nil
		},
	}
}

func relayRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <wss://relay.example.com>",
		Short: "Remove a relay from the list",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := args[0]
			cfg, err := LoadConfig()
			if err != nil {
				cfg = &Config{}
			}
			// Operate on defaults if nothing custom is saved yet.
			if len(cfg.NostrRelays) == 0 {
				cfg.NostrRelays = append([]string{}, defaultNostrRelays...)
			}
			filtered := cfg.NostrRelays[:0]
			found := false
			for _, r := range cfg.NostrRelays {
				if r == url {
					found = true
				} else {
					filtered = append(filtered, r)
				}
			}
			if !found {
				return fmt.Errorf("%s is not in the relay list", url)
			}
			cfg.NostrRelays = filtered
			if err := SaveConfig(cfg); err != nil {
				return fmt.Errorf("could not save config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s\n", url)
			return nil
		},
	}
}

func relayResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Reset the relay list to built-in defaults",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				cfg = &Config{}
			}
			cfg.NostrRelays = nil // nil → LoadNostrRelays() returns defaults
			if err := SaveConfig(cfg); err != nil {
				return fmt.Errorf("could not save config: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Relay list reset to defaults:")
			for _, r := range defaultNostrRelays {
				fmt.Fprintln(cmd.OutOrStdout(), " ", r)
			}
			return nil
		},
	}
}

// blossomCmd returns the 'blossom' subcommand group for managing Blossom servers.
func blossomCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blossom",
		Short: "Manage the Blossom audio server list",
		Long: `Manage the list of Blossom server URLs used for uploading and downloading
encrypted audio chunks.

When no servers are configured the built-in defaults are used. At least two
servers are recommended so each chunk has redundancy.`,
	}
	cmd.AddCommand(blossomListCmd(), blossomAddCmd(), blossomRemoveCmd(), blossomResetCmd())
	return cmd
}

func blossomListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show the active Blossom server list",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _ := LoadConfig()
			servers := LoadBlossomServers()
			if len(cfg.BlossomServers) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(using built-in defaults)")
			}
			for _, s := range servers {
				fmt.Fprintln(cmd.OutOrStdout(), s)
			}
			return nil
		},
	}
}

func blossomAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <https://server.example.com>",
		Short: "Add a Blossom server to the list",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := args[0]
			if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
				return fmt.Errorf("server URL must start with https:// or http://")
			}
			cfg, err := LoadConfig()
			if err != nil {
				cfg = &Config{}
			}
			if len(cfg.BlossomServers) == 0 {
				cfg.BlossomServers = append([]string{}, defaultBlossomServers...)
			}
			for _, s := range cfg.BlossomServers {
				if s == url {
					fmt.Fprintf(cmd.OutOrStdout(), "%s is already in the server list\n", url)
					return nil
				}
			}
			cfg.BlossomServers = append(cfg.BlossomServers, url)
			if err := SaveConfig(cfg); err != nil {
				return fmt.Errorf("could not save config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added %s\n", url)
			return nil
		},
	}
}

func blossomRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <https://server.example.com>",
		Short: "Remove a Blossom server from the list",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := args[0]
			cfg, err := LoadConfig()
			if err != nil {
				cfg = &Config{}
			}
			if len(cfg.BlossomServers) == 0 {
				cfg.BlossomServers = append([]string{}, defaultBlossomServers...)
			}
			filtered := cfg.BlossomServers[:0]
			found := false
			for _, s := range cfg.BlossomServers {
				if s == url {
					found = true
				} else {
					filtered = append(filtered, s)
				}
			}
			if !found {
				return fmt.Errorf("%s is not in the server list", url)
			}
			cfg.BlossomServers = filtered
			if err := SaveConfig(cfg); err != nil {
				return fmt.Errorf("could not save config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s\n", url)
			return nil
		},
	}
}

func blossomResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Reset the Blossom server list to built-in defaults",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				cfg = &Config{}
			}
			cfg.BlossomServers = nil
			if err := SaveConfig(cfg); err != nil {
				return fmt.Errorf("could not save config: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Blossom server list reset to defaults:")
			for _, s := range defaultBlossomServers {
				fmt.Fprintln(cmd.OutOrStdout(), " ", s)
			}
			return nil
		},
	}
}

// uploadCmd returns the 'upload' subcommand that encrypts and uploads an audio
// file to Blossom servers, then attaches the manifest to the matching earmark.
func uploadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upload <file>",
		Short: "Encrypt and upload an audio file to Blossom servers",
		Long: `Split, encrypt (AES-256-GCM), and upload an audio file to your configured
Blossom servers. Each 16 MiB chunk is uploaded to at least two servers for
redundancy.

After a successful upload the manifest (chunk SHA-256s, server URLs, and
encryption key) is attached to the matching earmark so the file can be
downloaded and played on any device running dirplay with the same Nostr key.

Requires a Nostr private key saved via 'dirplay nostr-key <key>'.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := args[0]

			cfg, err := LoadConfig()
			if err != nil || cfg.NostrPrivateKey == "" {
				return fmt.Errorf("no Nostr private key configured — run: dirplay nostr-key <nsec_or_hex_key>")
			}

			// Resolve servers: kind-10063 discovery + configured list.
			fmt.Fprintln(cmd.OutOrStdout(), "Resolving Blossom servers...")
			servers, err := ResolveBlossomServers(cfg.NostrPrivateKey)
			if err != nil || len(servers) == 0 {
				servers = LoadBlossomServers()
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Uploading to: %s\n", strings.Join(servers, ", "))

			// Fetch earmarks so we can attach the manifest afterwards.
			fmt.Fprintln(cmd.OutOrStdout(), "Fetching earmarks...")
			earmarks, _ := FetchEarmarks(cfg.NostrPrivateKey)

			// Find a matching earmark by path (exact) or artist+title.
			absPath, _ := filepath.Abs(filePath)
			var matchIdx int = -1
			for i, e := range earmarks {
				if e.Path == absPath || (e.Path == "" && e.Title != "" &&
					strings.EqualFold(filepath.Base(filePath), e.Title)) {
					matchIdx = i
					break
				}
			}

			// Upload with progress.
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			total := 0 // will be set after upload (we don't know chunk count upfront)
			manifest, err := UploadFile(ctx, cfg.NostrPrivateKey, filePath, servers,
				func(done, _ int) {
					total = done
					fmt.Fprintf(cmd.OutOrStdout(), "\r  chunk %d uploaded...", done)
				})
			if err != nil {
				return fmt.Errorf("upload failed: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\r  %d chunk(s) uploaded.           \n", total)

			// Attach manifest to the earmark if we found one.
			if matchIdx >= 0 {
				earmarks[matchIdx].Blossom = manifest
				fmt.Fprintln(cmd.OutOrStdout(), "Attaching manifest to earmark...")
				if err := UpdateEarmark(cfg.NostrPrivateKey, earmarks[matchIdx]); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not update earmark: %v\n", err)
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "Earmark updated.")
				}
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "(no matching earmark found — manifest not attached)")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Upload complete: %d chunk(s) across %d server(s).\n",
				len(manifest.Chunks), len(servers))
			return nil
		},
	}
}

// earmarksCmd returns the 'earmarks' subcommand that plays earmarked files
// as a playlist.
func earmarksCmd() *cobra.Command {
	var noTUI bool

	cmd := &cobra.Command{
		Use:   "earmarks",
		Short: "Play your earmarked tracks as a playlist",
		Long: `Fetch your private Nostr earmark list and play the files as a playlist.

Tracks are played in the order they were earmarked. Files that no longer exist
on disk are skipped with a warning.

Requires a Nostr private key saved via 'dirplay nostr-key <key>'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := LoadConfig()
			if err != nil || cfg.NostrPrivateKey == "" {
				return fmt.Errorf("no Nostr private key configured — run: dirplay nostr-key <nsec_or_hex_key>")
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Fetching earmarks from Nostr...")

			earmarks, err := FetchEarmarks(cfg.NostrPrivateKey)
			if err != nil {
				return fmt.Errorf("could not fetch earmarks: %w", err)
			}
			if len(earmarks) == 0 {
				return fmt.Errorf("no earmarks found — press [E] while a track is playing to add one")
			}

			// Build playlist from stored paths. When a file is missing but the
			// earmark has a Blossom manifest, download and decrypt to a temp file.
			var playlist []string
			var tempFiles []string // cleaned up after playback
			var skipped int
			for _, e := range earmarks {
				if e.Path == "" && e.Blossom == nil {
					skipped++
					continue
				}

				// File exists on disk — use it directly.
				if e.Path != "" {
					if _, err := os.Stat(e.Path); err == nil {
						playlist = append(playlist, e.Path)
						continue
					}
				}

				// File missing — try Blossom download.
				if e.Blossom != nil {
					desc := e.Title
					if desc == "" {
						desc = filepath.Base(e.Path)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "Downloading: %s\n", desc)
					dlCtx, dlCancel := context.WithTimeout(context.Background(), 5*time.Minute)
					tmpPath, err := DownloadAndReassemble(dlCtx, e.Blossom,
						func(done, total int) {
							fmt.Fprintf(cmd.OutOrStdout(), "\r  chunk %d/%d...", done, total)
						})
					dlCancel()
					if err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "\nskipping (download failed): %s — %v\n", desc, err)
						skipped++
						continue
					}
					fmt.Fprintln(cmd.OutOrStdout(), "\r  download complete.           ")
					playlist = append(playlist, tmpPath)
					tempFiles = append(tempFiles, tmpPath)
					continue
				}

				fmt.Fprintf(cmd.ErrOrStderr(), "skipping (not found on disk): %s\n", e.Path)
				skipped++
			}
			// Ensure temp files are removed when playback ends.
			defer func() {
				for _, f := range tempFiles {
					os.Remove(f)
				}
			}()

			if len(playlist) == 0 {
				msg := "no earmarked files found on disk"
				if skipped > 0 {
					msg += fmt.Sprintf(" (%d earmark(s) have no recorded path or the file has moved)", skipped)
				}
				return fmt.Errorf("%s", msg)
			}

			if skipped > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Playing %d track(s), %d skipped.\n\n", len(playlist), skipped)
			}

			if noTUI {
				return runNoTUI(playlist)
			}
			return runTUI(playlist)
		},
	}

	cmd.Flags().BoolVar(&noTUI, "no-tui", false, "Play without the terminal UI")
	return cmd
}
