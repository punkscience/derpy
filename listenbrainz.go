package main

import (
	"log"
	"time"

	"github.com/hirigaray/go-listenbrainz"
)

// ListenBrainzClient wraps the ListenBrainz client functionality
type ListenBrainzClient struct {
	token string
}

// NewListenBrainzClient creates a new ListenBrainz client if a token is available.
// It checks ~/.config/dirplay/config.json first, then falls back to LISTENBRAINZ_TOKEN env var.
func NewListenBrainzClient() *ListenBrainzClient {
	token := LoadListenBrainzToken()
	if token == "" {
		return nil // No token available, disable ListenBrainz functionality
	}

	return &ListenBrainzClient{
		token: token,
	}
}

// IsEnabled returns true if ListenBrainz client is configured and ready
func (lbc *ListenBrainzClient) IsEnabled() bool {
	return lbc != nil && lbc.token != ""
}

// SubmitListenNow submits a "playing now" listen to ListenBrainz
func (lbc *ListenBrainzClient) SubmitListenNow(artist, title, album string) error {
	if !lbc.IsEnabled() {
		return nil
	}

	track := listenbrainz.Track{
		Artist: artist,
		Title:  title,
		Album:  album,
	}

	_, err := listenbrainz.SubmitPlayingNow(track, lbc.token)
	if err != nil {
		return err
	}

	return nil
}

// SubmitScrobble submits a scrobble (completed listen) to ListenBrainz
func (lbc *ListenBrainzClient) SubmitScrobble(artist, title, album string, listenedAt time.Time) error {
	if !lbc.IsEnabled() {
		return nil
	}

	track := listenbrainz.Track{
		Artist: artist,
		Title:  title,
		Album:  album,
	}

	_, err := listenbrainz.SubmitSingle(track, lbc.token, listenedAt.Unix())
	if err != nil {
		return err
	}

	return nil
}

// ScrobbleTracker tracks when to scrobble a track (at 25% progress)
type ScrobbleTracker struct {
	lbClient  *ListenBrainzClient
	artist    string
	title     string
	album     string
	duration  time.Duration
	startTime time.Time
	scrobbled bool
	submitted bool // Track if we've submitted "playing now"
}

// NewScrobbleTracker creates a new scrobble tracker
func NewScrobbleTracker(lbClient *ListenBrainzClient, artist, title, album string, duration time.Duration) *ScrobbleTracker {
	return &ScrobbleTracker{
		lbClient:  lbClient,
		artist:    artist,
		title:     title,
		album:     album,
		duration:  duration,
		startTime: time.Now(),
		scrobbled: false,
		submitted: false,
	}
}

// Update checks current position and submits scrobble at 25% if not already done
func (st *ScrobbleTracker) Update(currentPosition time.Duration) {
	if !st.lbClient.IsEnabled() {
		return
	}

	// Submit "playing now" once at the beginning
	if !st.submitted {
		err := st.lbClient.SubmitListenNow(st.artist, st.title, st.album)
		if err != nil {
			log.Printf("Warning: Failed to submit playing now to ListenBrainz: %v", err)
		} else {
			log.Printf("Submitted playing now to ListenBrainz: %s - %s", st.artist, st.title)
		}
		st.submitted = true
	}

	// Check if we should scrobble (25% of track duration)
	scrobbleThreshold := st.duration / 4 // 25%
	if !st.scrobbled && currentPosition >= scrobbleThreshold && st.duration > 30*time.Second {
		err := st.lbClient.SubmitScrobble(st.artist, st.title, st.album, st.startTime)
		if err != nil {
			log.Printf("Warning: Failed to scrobble to ListenBrainz: %v", err)
		} else {
			log.Printf("Scrobbled to ListenBrainz: %s - %s", st.artist, st.title)
		}
		st.scrobbled = true
	}
}

// Reset resets the scrobble tracker for a new track
func (st *ScrobbleTracker) Reset(artist, title, album string, duration time.Duration) {
	st.artist = artist
	st.title = title
	st.album = album
	st.duration = duration
	st.startTime = time.Now()
	st.scrobbled = false
	st.submitted = false
}
