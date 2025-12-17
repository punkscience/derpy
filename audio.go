package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dhowden/tag"
	"github.com/gopxl/beep"
	"github.com/gopxl/beep/flac"
	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/speaker"
	"github.com/gopxl/beep/vorbis"
	"github.com/gopxl/beep/wav"
)

// CompletionStreamer wraps a streamer to detect when it completes
type CompletionStreamer struct {
	beep.Streamer
	completed bool
}

func (cs *CompletionStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	n, ok = cs.Streamer.Stream(samples)
	if !ok {
		cs.completed = true
	}
	return n, ok
}

func (cs *CompletionStreamer) Err() error {
	if s, ok := cs.Streamer.(interface{ Err() error }); ok {
		return s.Err()
	}
	return nil
}

func (cs *CompletionStreamer) IsCompleted() bool {
	return cs.completed
}

// AudioPlayer manages audio playback
type AudioPlayer struct {
	streamer           beep.StreamSeekCloser
	ctrl               *beep.Ctrl
	format             beep.Format
	playing            bool
	file               *os.File
	duration           time.Duration
	currentPos         time.Duration
	artist             string
	title              string
	album              string
	startTime          time.Time
	hasEnded           bool
	completionStream   *CompletionStreamer
	speakerInitialized bool
	scrobbleTracker    *ScrobbleTracker
	lbClient           *ListenBrainzClient
}

// NewAudioPlayer creates a new audio player instance
func NewAudioPlayer() *AudioPlayer {
	return &AudioPlayer{
		lbClient: NewListenBrainzClient(),
	}
}

// LoadTrack loads an audio file for playback
func (ap *AudioPlayer) LoadTrack(filePath string) error {
	// Stop any current playback and reset state
	ap.Stop()

	// Wait a moment for resources to be fully released
	time.Sleep(50 * time.Millisecond)

	// Open the audio file
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	ap.file = file

	// Read metadata tags
	tags, err := tag.ReadFrom(file)
	if err == nil {
		ap.artist = tags.Artist()
		ap.title = tags.Title()
		ap.album = tags.Album()
	} else {
		// Fallback to filename if no tags
		ap.title = filepath.Base(filePath)
		ap.artist = "Unknown Artist"
		ap.album = "Unknown Album"
	}

	// Reset file pointer for audio decoding
	if _, err := file.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek file: %w", err)
	}

	// Decode based on file extension
	ext := strings.ToLower(filepath.Ext(filePath))
	var streamer beep.StreamSeekCloser
	var format beep.Format

	switch ext {
	case ".mp3":
		streamer, format, err = mp3.Decode(file)
	case ".wav":
		streamer, format, err = wav.Decode(file)
	case ".flac":
		streamer, format, err = flac.Decode(file)
	case ".ogg":
		streamer, format, err = vorbis.Decode(file)
	default:
		return fmt.Errorf("unsupported audio format: %s", ext)
	}

	if err != nil {
		file.Close()
		return fmt.Errorf("failed to decode audio: %w", err)
	}

	ap.streamer = streamer
	ap.format = format

	// Calculate duration
	streamLen := streamer.Len()
	ap.duration = format.SampleRate.D(streamLen)
	ap.currentPos = 0

	// Initialize ListenBrainz scrobble tracker
	if ap.lbClient.IsEnabled() {
		ap.scrobbleTracker = NewScrobbleTracker(ap.lbClient, ap.artist, ap.title, ap.album, ap.duration)
	}

	// Initialize speaker only once per application lifecycle
	if !ap.speakerInitialized {
		if err := speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10)); err != nil {
			return fmt.Errorf("failed to initialize speaker: %w", err)
		}
		ap.speakerInitialized = true
	}

	return nil
}

// Play starts or resumes playback
func (ap *AudioPlayer) Play() error {
	if ap.streamer == nil {
		return fmt.Errorf("no track loaded")
	}

	if ap.playing {
		return nil // Already playing
	}

	// Clear any existing audio from speaker
	speaker.Clear()

	// Give speaker time to fully clear
	time.Sleep(10 * time.Millisecond)

	// Create completion detector wrapper
	ap.completionStream = &CompletionStreamer{
		Streamer: ap.streamer,
	}

	// Create control wrapper for pause/resume functionality
	ap.ctrl = &beep.Ctrl{
		Streamer: ap.completionStream,
		Paused:   false,
	}

	// Record start time for position tracking and reset position
	ap.startTime = time.Now()
	ap.currentPos = 0

	// Start playback
	speaker.Play(ap.ctrl)
	ap.playing = true

	return nil
}

// Pause pauses playback
func (ap *AudioPlayer) Pause() {
	if ap.ctrl != nil && ap.playing {
		speaker.Lock()
		ap.ctrl.Paused = true
		ap.currentPos += time.Since(ap.startTime) // Capture position at pause
		speaker.Unlock()
	}
}

// Resume resumes playback
func (ap *AudioPlayer) Resume() {
	if ap.ctrl != nil && ap.playing {
		speaker.Lock()
		ap.ctrl.Paused = false
		ap.startTime = time.Now() // Reset start time on resume
		speaker.Unlock()
	}
}

// IsPaused returns true if playback is paused
func (ap *AudioPlayer) IsPaused() bool {
	if ap.ctrl == nil {
		return false
	}
	speaker.Lock()
	paused := ap.ctrl.Paused
	speaker.Unlock()
	return paused
}

// Stop stops playback
func (ap *AudioPlayer) Stop() {
	if ap.playing {
		// Clear the speaker to stop any audio
		speaker.Clear()

		// Wait for speaker to fully stop
		time.Sleep(20 * time.Millisecond)

		ap.playing = false
		ap.hasEnded = false

		// Update currentPos to where we stopped
		if ap.ctrl != nil && !ap.ctrl.Paused {
			ap.currentPos = ap.GetPosition()
		}
	}

	// Clean up resources
	if ap.streamer != nil {
		ap.streamer.Close()
		ap.streamer = nil
	}

	if ap.file != nil {
		ap.file.Close()
		ap.file = nil
	}

	// Clear references to prevent accumulation
	ap.ctrl = nil
	ap.completionStream = nil
	ap.scrobbleTracker = nil
}

// Close closes the audio player and releases resources
func (ap *AudioPlayer) Close() {
	ap.Stop()

	// Final cleanup - clear speaker one last time
	speaker.Clear()

	// Reset speaker initialization flag if needed for restart
	ap.speakerInitialized = false
}

// Shutdown completely shuts down the audio player
func (ap *AudioPlayer) Shutdown() {
	ap.Close()
}

// GetDuration returns the total duration of the current track
func (ap *AudioPlayer) GetDuration() time.Duration {
	return ap.duration
}

// GetPosition returns the current playback position
func (ap *AudioPlayer) GetPosition() time.Duration {
	speaker.Lock()
	defer speaker.Unlock()

	if ap.ctrl != nil && ap.ctrl.Paused {
		return ap.currentPos
	}

	if !ap.playing {
		return ap.currentPos
	}

	// Calculate position based on elapsed time since start
	elapsed := time.Since(ap.startTime)
	pos := ap.currentPos + elapsed

	// Don't exceed duration
	if pos > ap.duration {
		pos = ap.duration
	}

	// Update ListenBrainz scrobble tracker
	if ap.scrobbleTracker != nil {
		ap.scrobbleTracker.Update(pos)
	}

	return pos
}

// Seek seeks to a specific position in the track
func (ap *AudioPlayer) Seek(pos time.Duration) error {
	if ap.streamer == nil {
		return fmt.Errorf("no track loaded")
	}

	// Convert time to sample position
	samples := ap.format.SampleRate.N(pos)

	// Seek to position
	if err := ap.streamer.Seek(samples); err != nil {
		return fmt.Errorf("failed to seek: %w", err)
	}

	ap.currentPos = pos
	return nil
}

// IsPlaying returns true if audio is currently playing
func (ap *AudioPlayer) IsPlaying() bool {
	return ap.playing && !ap.IsPaused()
}

// GetArtist returns the artist of the current track
func (ap *AudioPlayer) GetArtist() string {
	return ap.artist
}

// GetTitle returns the title of the current track
func (ap *AudioPlayer) GetTitle() string {
	return ap.title
}

// GetAlbum returns the album of the current track
func (ap *AudioPlayer) GetAlbum() string {
	return ap.album
}

// HasEnded returns true if the current track has finished playing
func (ap *AudioPlayer) HasEnded() bool {
	if ap.duration == 0 || !ap.playing {
		return false
	}

	// First check if our completion streamer detected the end
	if ap.completionStream != nil && ap.completionStream.IsCompleted() {
		ap.hasEnded = true
		return true
	}

	// Also check if position has reached the end (fallback)
	currentPos := ap.GetPosition()

	if currentPos >= ap.duration {
		ap.hasEnded = true
		return true
	}

	return ap.hasEnded
}
