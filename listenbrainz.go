package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// listenBrainzAPIBase is the root URL for ListenBrainz submissions.
// Tests override this to point at a local httptest server.
var listenBrainzAPIBase = "https://api.listenbrainz.org"

// listenBrainzHTTPClient is used for all outbound ListenBrainz requests.
// The timeout is not optional: it bounds how long a submission goroutine can
// live. It is generous because ScrobbleTracker dispatches submissions off the
// playback path, so a slow request costs nothing but a goroutine — whereas a
// tight timeout turns ordinary upstream slowness into a dropped listen.
var listenBrainzHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
}

// listenBrainzMaxAttempts bounds how many times a transient submission failure
// is retried. api.listenbrainz.org intermittently fails to answer within the
// client timeout; without a retry the listen is dropped, because the tracker
// marks a submission done before dispatching it and never revisits the flag.
const listenBrainzMaxAttempts = 3

// listenBrainzRetryBackoff is the delay before each retry, so it holds one
// fewer entry than listenBrainzMaxAttempts. Tests set it to zero delays.
var listenBrainzRetryBackoff = []time.Duration{1 * time.Second, 3 * time.Second}

// listenBrainzUserAgent identifies derpy to the MetaBrainz servers.
//
// This is load-bearing, not cosmetic. api.listenbrainz.org sits behind an
// openresty front end that refuses requests carrying Go's default
// "Go-http-client/2.0" agent by resetting the HTTP/2 stream, which surfaces
// in Go as the opaque:
//
//	stream error: stream ID 1; PROTOCOL_ERROR; received from peer
//
// MetaBrainz require applications to identify themselves and provide contact
// information, so every request sends a real agent string.
func listenBrainzUserAgent() string {
	return fmt.Sprintf("derpy/%s ( https://github.com/Punk-Science-Studios-Inc/derpy )", Version)
}

// ListenBrainzClient wraps the ListenBrainz client functionality
type ListenBrainzClient struct {
	token string
}

// lbTrackMetadata is the track_metadata object of a submission payload.
type lbTrackMetadata struct {
	Title  string `json:"track_name"`
	Artist string `json:"artist_name"`
	Album  string `json:"release_name,omitempty"`
}

// lbPayload is a single listen within a submission. ListenedAt is omitted for
// "playing_now" submissions, which the API requires to carry no timestamp.
type lbPayload struct {
	ListenedAt int64           `json:"listened_at,omitempty"`
	Track      lbTrackMetadata `json:"track_metadata"`
}

// lbSubmission is the JSON body posted to /1/submit-listens.
type lbSubmission struct {
	ListenType string      `json:"listen_type"`
	Payload    []lbPayload `json:"payload"`
}

// NewListenBrainzClient creates a new ListenBrainz client if a token is available.
// It checks ~/.config/derpy/config.json first, then falls back to LISTENBRAINZ_TOKEN env var.
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

// submit posts a submission to /1/submit-listens and reports any non-2xx
// response as an error, including the server's own explanation.
//
// Checking the status code matters: a rejected listen (expired token, rate
// limit, malformed payload) returns a perfectly successful HTTP exchange, so
// ignoring the code makes every failure look like a success.
func (lbc *ListenBrainzClient) submit(sub lbSubmission) error {
	body, err := json.Marshal(sub)
	if err != nil {
		return fmt.Errorf("encoding %s submission: %w", sub.ListenType, err)
	}

	var lastErr error
	for attempt := 0; attempt < listenBrainzMaxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(listenBrainzRetryBackoff[attempt-1])
		}

		retryable, err := lbc.submitOnce(sub.ListenType, body)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable {
			return err
		}
	}

	return fmt.Errorf("after %d attempts: %w", listenBrainzMaxAttempts, lastErr)
}

// submitOnce performs a single submission attempt. It reports whether the
// failure is worth retrying: transport errors (timeouts, resets), 429 and 5xx
// are transient, while any other 4xx means the request itself is wrong and
// resending it unchanged would only repeat the rejection.
//
// The request is rebuilt on every attempt because the body reader is consumed
// by the first one.
func (lbc *ListenBrainzClient) submitOnce(listenType string, body []byte) (retryable bool, err error) {
	req, err := http.NewRequest(http.MethodPost, listenBrainzAPIBase+"/1/submit-listens", bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("building %s request: %w", listenType, err)
	}
	req.Header.Set("Authorization", "Token "+lbc.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", listenBrainzUserAgent())

	resp, err := listenBrainzHTTPClient.Do(req)
	if err != nil {
		// The server was never reached, or never answered. Always transient.
		return true, fmt.Errorf("submitting %s: %w", listenType, err)
	}
	defer resp.Body.Close()

	// Read a bounded amount so the connection can be reused and so the
	// server's error message reaches the log.
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return retryable, fmt.Errorf("listenbrainz rejected %s: %s: %s",
			listenType, resp.Status, strings.TrimSpace(string(snippet)))
	}
	return false, nil
}

// SubmitListenNow submits a "playing now" listen to ListenBrainz
func (lbc *ListenBrainzClient) SubmitListenNow(artist, title, album string) error {
	if !lbc.IsEnabled() {
		return nil
	}

	return lbc.submit(lbSubmission{
		ListenType: "playing_now",
		Payload: []lbPayload{{
			Track: lbTrackMetadata{Title: title, Artist: artist, Album: album},
		}},
	})
}

// SubmitScrobble submits a scrobble (completed listen) to ListenBrainz
func (lbc *ListenBrainzClient) SubmitScrobble(artist, title, album string, listenedAt time.Time) error {
	if !lbc.IsEnabled() {
		return nil
	}

	return lbc.submit(lbSubmission{
		ListenType: "single",
		Payload: []lbPayload{{
			ListenedAt: listenedAt.Unix(),
			Track:      lbTrackMetadata{Title: title, Artist: artist, Album: album},
		}},
	})
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

// Update checks current position and submits scrobble at 25% if not already done.
//
// Each submission is dispatched on its own goroutine. Update is reached via
// AudioPlayer.GetPosition, which holds the speaker mutex that the PulseAudio
// fill callback also needs, so performing network I/O inline starves the audio
// callback and freezes the TUI for the length of the request.
//
// The "already done" flags are set before dispatch so a submission fires
// exactly once, even though Update is called on every 100ms tick.
func (st *ScrobbleTracker) Update(currentPosition time.Duration) {
	if !st.lbClient.IsEnabled() {
		return
	}

	// Submit "playing now" once at the beginning
	if !st.submitted {
		st.submitted = true
		lbClient, artist, title, album := st.lbClient, st.artist, st.title, st.album
		go func() {
			if err := lbClient.SubmitListenNow(artist, title, album); err != nil {
				log.Printf("Warning: Failed to submit playing now to ListenBrainz: %v", err)
			}
		}()
	}

	// Check if we should scrobble (25% of track duration)
	scrobbleThreshold := st.duration / 4 // 25%
	if !st.scrobbled && currentPosition >= scrobbleThreshold && st.duration > 30*time.Second {
		st.scrobbled = true
		lbClient, artist, title, album := st.lbClient, st.artist, st.title, st.album
		startTime := st.startTime
		go func() {
			if err := lbClient.SubmitScrobble(artist, title, album, startTime); err != nil {
				log.Printf("Warning: Failed to scrobble to ListenBrainz: %v", err)
			}
		}()
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
