package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Check command line arguments
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s [--no-tui] <music_directory>\n", os.Args[0])
		os.Exit(1)
	}

	var musicDir string
	noTui := false
	if len(os.Args) > 2 && os.Args[1] == "--no-tui" {
		noTui = true
		musicDir = os.Args[2]
	} else {
		musicDir = os.Args[1]
	}

	// Verify the directory exists
	if _, err := os.Stat(musicDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Directory does not exist: %s\n", musicDir)
		os.Exit(1)
	}

	// Scan directory for audio files
	playlist, err := scanMusicDirectory(musicDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error scanning directory: %v\n", err)
		os.Exit(1)
	}

	if len(playlist) == 0 {
		fmt.Fprintf(os.Stderr, "No audio files found in directory: %s\n", musicDir)
		os.Exit(1)
	}

	// Shuffle the playlist
	shufflePlaylist(playlist)

	if noTui {
		// Non-TUI mode
		player := NewAudioPlayer()
		defer player.Shutdown()

		for _, track := range playlist {
			fmt.Printf("Playing: %s\n", track)
			err := player.LoadTrack(track)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error loading track %s: %v\n", track, err)
				continue
			}

			err = player.Play()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error playing track %s: %v\n", track, err)
				continue
			}

			// Wait for the track to finish
			for !player.HasEnded() {
				time.Sleep(100 * time.Millisecond)
			}
			player.Stop()
		}
		fmt.Println("Playlist finished.")
	} else {
		// Create and run the TUI application
		model := NewPlayerModel(playlist)
		program := tea.NewProgram(model, tea.WithAltScreen())

		if _, err := program.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
			os.Exit(1)
		}
	}
}

// scanMusicDirectory recursively scans a directory for audio files
func scanMusicDirectory(root string) ([]string, error) {
	var playlist []string

	// Supported audio file extensions
	audioExts := map[string]bool{
		".mp3":  true,
		".wav":  true,
		".flac": true,
		".ogg":  true,
		".m4a":  true,
		".aac":  true,
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check if file has supported audio extension
		ext := strings.ToLower(filepath.Ext(path))
		if audioExts[ext] {
			playlist = append(playlist, path)
		}

		return nil
	})

	return playlist, err
}
