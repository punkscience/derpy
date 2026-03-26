package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PlayerModel represents the state of the music player TUI
type PlayerModel struct {
	playlist     []string
	currentIndex int
	player       *AudioPlayer
	playing      bool
	paused       bool
	position     time.Duration
	duration     time.Duration
	width        int
	height       int
	err          error
	artist       string
	title        string
	album        string
	tickInterval time.Duration
	// mpris is the optional MPRIS2 D-Bus service.  It is nil when the session
	// D-Bus is unavailable or when running in --no-tui mode.
	mpris *MPRISService
}

// Messages for the TUI
type tickMsg time.Time
type positionMsg time.Duration
type trackEndedMsg struct{}
type playErrorMsg error
type trackLoadedMsg struct {
	duration time.Duration
	artist   string
	title    string
	album    string
}
type noteSavedMsg struct {
	success bool
	error   string
}
type trackDeletedMsg struct {
	err error
}

// NewPlayerModel creates a new player model
func NewPlayerModel(playlist []string) *PlayerModel {
	return &PlayerModel{
		playlist:     playlist,
		currentIndex: 0,
		player:       NewAudioPlayer(),
		tickInterval: 100 * time.Millisecond, // Make tick interval configurable
	}
}

// Init initializes the model
func (m *PlayerModel) Init() tea.Cmd {
	// Start the first track
	return tea.Batch(
		m.loadCurrentTrack(),
		m.tickCmd(),
	)
}

// Update handles messages and updates the model
func (m *PlayerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.player.Close()
			return m, tea.Quit

		case " ":
			// Toggle pause/play
			if m.playing {
				if m.paused {
					m.player.Resume()
					m.paused = false
					m.mpris.NotifyStateChanged(m)
					// Restart ticking when resuming
					return m, m.tickCmd()
				} else {
					m.player.Pause()
					m.paused = true
					m.mpris.NotifyStateChanged(m)
					// Stop ticking when paused (handled by tickMsg case)
				}
			}

		case "left":
			// Previous track
			m.player.Stop()
			m.currentIndex--
			if m.currentIndex < 0 {
				m.currentIndex = len(m.playlist) - 1 // Loop to last track
			}
			return m, m.loadCurrentTrack()

		case "right":
			// Next track
			m.player.Stop()
			m.currentIndex++
			if m.currentIndex >= len(m.playlist) {
				m.currentIndex = 0 // Loop back to first track
			}
			return m, m.loadCurrentTrack()

		case "n":
			// Save current track to notes
			if m.playing {
				return m, m.saveTrackNote()
			}

		case "d":
			// Delete current track from disk and advance to next
			if m.playing && m.currentIndex < len(m.playlist) {
				return m, m.deleteCurrentTrack()
			}
		}

	// ---- MPRIS2 command messages injected by MPRISService -------------------

	case mprisPlayMsg:
		if m.playing && m.paused {
			// Resume from pause.
			m.player.Resume()
			m.paused = false
			m.mpris.NotifyStateChanged(m)
			return m, m.tickCmd()
		} else if !m.playing {
			// Nothing loaded yet — load and play.
			return m, m.loadCurrentTrack()
		}

	case mprisPauseMsg:
		if m.playing && !m.paused {
			m.player.Pause()
			m.paused = true
			m.mpris.NotifyStateChanged(m)
		}

	case mprisPlayPauseMsg:
		if m.playing {
			if m.paused {
				m.player.Resume()
				m.paused = false
				m.mpris.NotifyStateChanged(m)
				return m, m.tickCmd()
			} else {
				m.player.Pause()
				m.paused = true
				m.mpris.NotifyStateChanged(m)
			}
		}

	case mprisStopMsg:
		if m.playing {
			m.player.Stop()
			m.playing = false
			m.paused = false
			m.mpris.NotifyStateChanged(m)
		}

	case mprisNextMsg:
		m.player.Stop()
		m.currentIndex++
		if m.currentIndex >= len(m.playlist) {
			m.currentIndex = 0
		}
		return m, m.loadCurrentTrack()

	case mprisPreviousMsg:
		m.player.Stop()
		m.currentIndex--
		if m.currentIndex < 0 {
			m.currentIndex = len(m.playlist) - 1
		}
		return m, m.loadCurrentTrack()

	case mprisSeekMsg:
		if m.playing {
			newPos := m.position + msg.offset
			if newPos < 0 {
				newPos = 0
			}
			if newPos > m.duration {
				newPos = m.duration
			}
			if err := m.player.Seek(newPos); err == nil {
				m.position = newPos
				m.mpris.EmitSeeked(m)
			}
		}

	case mprisSetPositionMsg:
		if m.playing {
			pos := msg.pos
			if pos < 0 {
				pos = 0
			}
			if pos > m.duration {
				pos = m.duration
			}
			if err := m.player.Seek(pos); err == nil {
				m.position = pos
				m.mpris.EmitSeeked(m)
			}
		}

	case tickMsg:
		// Get current position from player directly
		m.position = m.player.GetPosition()

		// Check if track ended using the new HasEnded method
		if m.playing && m.player.HasEnded() {
			return m, func() tea.Msg {
				return trackEndedMsg{}
			}
		}

		// Only continue ticking if we're actually playing
		if m.playing && !m.paused {
			return m, m.tickCmd()
		}

		return m, nil

	case trackEndedMsg:
		m.player.Stop()
		m.currentIndex++
		if m.currentIndex >= len(m.playlist) {
			m.currentIndex = 0 // Loop back to the first track
		}
		return m, m.loadCurrentTrack()

	case trackLoadedMsg:
		m.playing = true
		m.paused = false
		m.position = 0
		m.duration = msg.duration
		m.artist = msg.artist
		m.title = msg.title
		m.album = msg.album
		// Notify MPRIS clients that metadata and status have changed.
		m.mpris.NotifyStateChanged(m)
		m.mpris.EmitSeeked(m) // Position jumped to 0 on track change.
		// Restart the tick cycle for position updates
		return m, m.tickCmd()

	case trackDeletedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		// Playlist was already updated in deleteCurrentTrack; load the next track.
		// currentIndex was adjusted to stay in bounds.
		if len(m.playlist) == 0 {
			return m, tea.Quit
		}
		return m, m.loadCurrentTrack()

	case noteSavedMsg:
		// Handle note saving feedback (could show a brief message)
		// For now, we'll just ignore it as the save happens silently
		return m, nil

	case playErrorMsg:
		m.err = error(msg)
		return m, nil

	case positionMsg:
		m.position = time.Duration(msg)
	}

	return m, nil
}

// View renders the TUI
func (m *PlayerModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\nPress 'q' or 'esc' to quit", m.err)
	}

	if len(m.playlist) == 0 {
		return "No tracks in playlist\nPress 'q' or 'esc' to quit"
	}

	// Determine track display
	var trackDisplay string
	if m.title != "" && m.artist != "" {
		trackDisplay = fmt.Sprintf("%s - %s", m.artist, m.title)
	} else if m.title != "" {
		trackDisplay = m.title
	} else {
		// Fallback to filename
		trackDisplay = filepath.Base(m.playlist[m.currentIndex])
		if ext := filepath.Ext(trackDisplay); ext != "" {
			trackDisplay = strings.TrimSuffix(trackDisplay, ext)
		}
	}

	// Create styles
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#04B575")).
		MarginBottom(1)

	trackStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FAFAFA")).
		MarginBottom(1)

	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#626262")).
		MarginBottom(1)

	progressStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#04B575"))

	controlsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#626262")).
		MarginTop(2)

	// Build the UI
	var content strings.Builder

	// Title
	content.WriteString(titleStyle.Render("♪ dirplay"))
	content.WriteString("\n\n")

	// Current track
	content.WriteString(trackStyle.Render(fmt.Sprintf("Playing: %s", trackDisplay)))
	content.WriteString("\n")

	// Track info
	trackInfo := fmt.Sprintf("Track %d of %d", m.currentIndex+1, len(m.playlist))
	content.WriteString(statusStyle.Render(trackInfo))
	content.WriteString("\n")

	// Status
	status := "■ Stopped"
	if m.playing {
		if m.paused {
			status = "⏸ Paused"
		} else {
			status = "▶ Playing"
		}
	}
	content.WriteString(statusStyle.Render(status))
	content.WriteString("\n\n")

	// Progress bar
	progressBar := m.renderProgressBar(40)
	content.WriteString(progressStyle.Render(progressBar))
	content.WriteString("\n")

	// Time display
	posStr := formatDuration(m.position)
	durStr := formatDuration(m.duration)
	timeDisplay := fmt.Sprintf("%s / %s", posStr, durStr)
	content.WriteString(statusStyle.Render(timeDisplay))
	content.WriteString("\n")

	// Controls
	controls := "Controls: [←] Previous  [→] Next  [SPACE] Pause/Play  [N] Note  [D] Delete  [ESC] Quit"
	content.WriteString(controlsStyle.Render(controls))

	return content.String()
}

// renderProgressBar renders a progress bar
func (m *PlayerModel) renderProgressBar(width int) string {
	if m.duration == 0 {
		return strings.Repeat("─", width)
	}

	progress := float64(m.position) / float64(m.duration)
	filled := int(progress * float64(width))

	if filled > width {
		filled = width
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("─", width-filled)
	return fmt.Sprintf("[%s]", bar)
}

// tickCmd returns a command to send tick messages
func (m *PlayerModel) tickCmd() tea.Cmd {
	return tea.Tick(m.tickInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// loadCurrentTrack loads and plays the current track
func (m *PlayerModel) loadCurrentTrack() tea.Cmd {
	return func() tea.Msg {
		if m.currentIndex >= len(m.playlist) {
			return nil
		}

		track := m.playlist[m.currentIndex]

		// Load the track
		if err := m.player.LoadTrack(track); err != nil {
			return playErrorMsg(fmt.Errorf("failed to load track: %w", err))
		}

		// Start playing
		if err := m.player.Play(); err != nil {
			return playErrorMsg(fmt.Errorf("failed to play track: %w", err))
		}

		return trackLoadedMsg{
			duration: m.player.GetDuration(),
			artist:   m.player.GetArtist(),
			title:    m.player.GetTitle(),
			album:    m.player.GetAlbum(),
		}
	}
}

// saveTrackNote saves the current track information to track-notes.md
func (m *PlayerModel) saveTrackNote() tea.Cmd {
	return func() tea.Msg {
		// Get user's home directory
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return noteSavedMsg{success: false, error: "Could not find home directory"}
		}

		notesFile := filepath.Join(homeDir, "track-notes.md")

		// Create the note entry in the format: [ ] Artist - Album - Track
		noteEntry := fmt.Sprintf("[ ] %s - %s - %s\n", m.artist, m.album, m.title)

		// Check if file exists
		_, err = os.Stat(notesFile)
		fileExists := !os.IsNotExist(err)

		var file *os.File
		if fileExists {
			// Open file for appending
			file, err = os.OpenFile(notesFile, os.O_APPEND|os.O_WRONLY, 0644)
		} else {
			// Create new file with header
			file, err = os.Create(notesFile)
			if err != nil {
				return noteSavedMsg{success: false, error: "Could not create notes file"}
			}
			// Write header first
			if _, err := file.WriteString("# DirPlay Track Notes\n"); err != nil {
				file.Close()
				return noteSavedMsg{success: false, error: "Could not write header to notes file"}
			}
		}

		if err != nil {
			return noteSavedMsg{success: false, error: "Could not open notes file"}
		}
		defer file.Close()

		// Write the note entry
		if _, err := file.WriteString(noteEntry); err != nil {
			return noteSavedMsg{success: false, error: "Could not write to notes file"}
		}

		return noteSavedMsg{success: true}
	}
}

// deleteCurrentTrack stops playback, removes the file from disk, removes it
// from the playlist, and signals the TUI to load the next track.
func (m *PlayerModel) deleteCurrentTrack() tea.Cmd {
	// Capture path and index before the goroutine runs
	filePath := m.playlist[m.currentIndex]
	idx := m.currentIndex

	// Stop playback immediately (synchronous)
	m.player.Stop()
	m.playing = false

	// Remove the entry from the playlist
	m.playlist = append(m.playlist[:idx], m.playlist[idx+1:]...)

	// Clamp index so it points to the next track (or wraps around)
	if len(m.playlist) > 0 && m.currentIndex >= len(m.playlist) {
		m.currentIndex = 0
	}

	return func() tea.Msg {
		if err := os.Remove(filePath); err != nil {
			return trackDeletedMsg{err: fmt.Errorf("delete failed: %w", err)}
		}
		return trackDeletedMsg{}
	}
}

// formatDuration formats a time.Duration as mm:ss
func formatDuration(d time.Duration) string {
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}
