package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// capturedRequest records what the fake ListenBrainz server received.
type capturedRequest struct {
	method    string
	userAgent string
	auth      string
	sub       lbSubmission
	rawBody   string
}

// newFakeListenBrainz starts an httptest server standing in for
// api.listenbrainz.org and redirects the package's API base and HTTP client at
// it for the duration of the test. It returns a pointer to the most recent
// captured request.
func newFakeListenBrainz(t *testing.T, status int, respBody string) *capturedRequest {
	t.Helper()

	got := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.method = r.Method
		got.userAgent = r.Header.Get("User-Agent")
		got.auth = r.Header.Get("Authorization")
		got.rawBody = string(body)
		_ = json.Unmarshal(body, &got.sub)

		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)

	oldBase, oldClient := listenBrainzAPIBase, listenBrainzHTTPClient
	listenBrainzAPIBase = srv.URL
	listenBrainzHTTPClient = srv.Client()
	t.Cleanup(func() {
		listenBrainzAPIBase = oldBase
		listenBrainzHTTPClient = oldClient
	})
	noRetryDelay(t)

	return got
}

// noRetryDelay strips the retry backoff so tests that exercise a retryable
// failure finish instantly instead of sleeping the production delays.
func noRetryDelay(t *testing.T) {
	t.Helper()
	old := listenBrainzRetryBackoff
	listenBrainzRetryBackoff = make([]time.Duration, len(old))
	t.Cleanup(func() { listenBrainzRetryBackoff = old })
}

// TestSubmitSendsIdentifyingUserAgent is the regression test for the scrobbling
// outage: ListenBrainz resets the HTTP/2 stream (surfacing as PROTOCOL_ERROR)
// for requests carrying Go's default agent, so derpy must always identify itself.
func TestSubmitSendsIdentifyingUserAgent(t *testing.T) {
	got := newFakeListenBrainz(t, http.StatusOK, `{"status":"ok"}`)

	lbc := &ListenBrainzClient{token: "test-token"}
	if err := lbc.SubmitListenNow("Artist", "Title", "Album"); err != nil {
		t.Fatalf("SubmitListenNow returned error: %v", err)
	}

	if got.userAgent == "" {
		t.Fatal("no User-Agent sent; ListenBrainz will reset the stream")
	}
	if strings.HasPrefix(got.userAgent, "Go-http-client") {
		t.Fatalf("Go default User-Agent %q is rejected by ListenBrainz", got.userAgent)
	}
	if !strings.HasPrefix(got.userAgent, "derpy/") {
		t.Errorf("User-Agent = %q, want it to start with %q", got.userAgent, "derpy/")
	}
	// MetaBrainz require contact information in the agent string.
	if !strings.Contains(got.userAgent, "github.com/Punk-Science-Studios-Inc/derpy") {
		t.Errorf("User-Agent = %q, want it to carry contact information", got.userAgent)
	}
}

func TestSubmitListenNowPayload(t *testing.T) {
	got := newFakeListenBrainz(t, http.StatusOK, `{"status":"ok"}`)

	lbc := &ListenBrainzClient{token: "test-token"}
	if err := lbc.SubmitListenNow("Artist", "Title", "Album"); err != nil {
		t.Fatalf("SubmitListenNow returned error: %v", err)
	}

	if got.method != http.MethodPost {
		t.Errorf("method = %q, want POST", got.method)
	}
	if want := "Token test-token"; got.auth != want {
		t.Errorf("Authorization = %q, want %q", got.auth, want)
	}
	if got.sub.ListenType != "playing_now" {
		t.Errorf("listen_type = %q, want %q", got.sub.ListenType, "playing_now")
	}
	if len(got.sub.Payload) != 1 {
		t.Fatalf("payload has %d entries, want 1", len(got.sub.Payload))
	}
	p := got.sub.Payload[0]
	if p.Track.Artist != "Artist" || p.Track.Title != "Title" || p.Track.Album != "Album" {
		t.Errorf("track_metadata = %+v, want Artist/Title/Album", p.Track)
	}
	// The API rejects a "playing_now" submission that carries a timestamp.
	if strings.Contains(got.rawBody, "listened_at") {
		t.Errorf("playing_now body must omit listened_at, got %s", got.rawBody)
	}
}

func TestSubmitScrobblePayload(t *testing.T) {
	got := newFakeListenBrainz(t, http.StatusOK, `{"status":"ok"}`)

	at := time.Unix(1700000000, 0)
	lbc := &ListenBrainzClient{token: "test-token"}
	if err := lbc.SubmitScrobble("Artist", "Title", "Album", at); err != nil {
		t.Fatalf("SubmitScrobble returned error: %v", err)
	}

	if got.sub.ListenType != "single" {
		t.Errorf("listen_type = %q, want %q", got.sub.ListenType, "single")
	}
	if len(got.sub.Payload) != 1 {
		t.Fatalf("payload has %d entries, want 1", len(got.sub.Payload))
	}
	if got.sub.Payload[0].ListenedAt != at.Unix() {
		t.Errorf("listened_at = %d, want %d", got.sub.Payload[0].ListenedAt, at.Unix())
	}
}

// TestSubmitReportsRejection covers the previously swallowed failure: the old
// code ignored the status code, so a bad token or rate limit looked like success.
func TestSubmitReportsRejection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"bad token", http.StatusUnauthorized, `{"code":401,"error":"Invalid authorization token."}`},
		{"rate limited", http.StatusTooManyRequests, `{"code":429,"error":"Rate limit exceeded"}`},
		{"bad payload", http.StatusBadRequest, `{"code":400,"error":"Invalid JSON document"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newFakeListenBrainz(t, tc.status, tc.body)

			lbc := &ListenBrainzClient{token: "test-token"}
			err := lbc.SubmitScrobble("Artist", "Title", "Album", time.Unix(1700000000, 0))
			if err == nil {
				t.Fatal("expected an error for a non-2xx response, got nil")
			}
			// The server's own explanation should reach the log.
			if !strings.Contains(err.Error(), tc.body) {
				t.Errorf("error %q does not carry the server response %q", err, tc.body)
			}
		})
	}
}

func TestDisabledClientDoesNotSubmit(t *testing.T) {
	got := newFakeListenBrainz(t, http.StatusOK, `{"status":"ok"}`)

	var lbc *ListenBrainzClient // nil client: no token configured
	if err := lbc.SubmitListenNow("Artist", "Title", "Album"); err != nil {
		t.Errorf("SubmitListenNow on disabled client returned error: %v", err)
	}
	if err := lbc.SubmitScrobble("Artist", "Title", "Album", time.Now()); err != nil {
		t.Errorf("SubmitScrobble on disabled client returned error: %v", err)
	}
	if got.method != "" {
		t.Error("disabled client made a request")
	}
}

// TestUpdateSubmitsOncePerTrack guards the tick loop: Update runs every 100ms,
// but each submission must fire exactly once.
func TestUpdateSubmitsOncePerTrack(t *testing.T) {
	// Submissions are dispatched on their own goroutines, so the counter must
	// be atomic and the handler must not race with the assertions below.
	var calls atomic.Int32
	done := make(chan struct{}, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"status":"ok"}`)
		done <- struct{}{}
	}))
	defer srv.Close()

	oldBase, oldClient := listenBrainzAPIBase, listenBrainzHTTPClient
	listenBrainzAPIBase, listenBrainzHTTPClient = srv.URL, srv.Client()
	defer func() { listenBrainzAPIBase, listenBrainzHTTPClient = oldBase, oldClient }()

	st := NewScrobbleTracker(&ListenBrainzClient{token: "test-token"},
		"Artist", "Title", "Album", 4*time.Minute)

	// Tick repeatedly past the 25% threshold.
	for i := 0; i < 20; i++ {
		st.Update(90 * time.Second)
	}

	// Exactly two submissions are expected: "playing now" and the scrobble.
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for submission %d of 2", i+1)
		}
	}
	select {
	case <-done:
		t.Fatalf("more than 2 submissions were sent (%d requests)", calls.Load())
	case <-time.After(300 * time.Millisecond):
	}
}

// newFlakyListenBrainz starts a server that fails the first failures requests
// with the given status, then succeeds. It returns the request count.
func newFlakyListenBrainz(t *testing.T, failures int, status int) *int32 {
	t.Helper()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if int(atomic.AddInt32(&calls, 1)) <= failures {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"error":"transient"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	t.Cleanup(srv.Close)

	oldBase, oldClient := listenBrainzAPIBase, listenBrainzHTTPClient
	listenBrainzAPIBase, listenBrainzHTTPClient = srv.URL, srv.Client()
	t.Cleanup(func() { listenBrainzAPIBase, listenBrainzHTTPClient = oldBase, oldClient })
	noRetryDelay(t)

	return &calls
}

// TestSubmitRetriesTransientFailure covers the dropped-listen bug: the tracker
// marks a submission done before dispatching it, so a submission that gives up
// on the first transient error loses that listen for good.
func TestSubmitRetriesTransientFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
	}{
		{"server error", http.StatusInternalServerError},
		{"bad gateway", http.StatusBadGateway},
		{"rate limited", http.StatusTooManyRequests},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := newFlakyListenBrainz(t, 1, tc.status)

			lbc := &ListenBrainzClient{token: "test-token"}
			if err := lbc.SubmitScrobble("Artist", "Title", "Album", time.Unix(1700000000, 0)); err != nil {
				t.Fatalf("expected the retry to succeed, got %v", err)
			}
			if got := atomic.LoadInt32(calls); got != 2 {
				t.Errorf("expected 2 attempts (one failure, one success), got %d", got)
			}
		})
	}
}

// TestSubmitDoesNotRetryPermanentFailure guards the other side: resending a
// rejected payload unchanged only repeats the rejection, so a bad token or a
// malformed body must fail on the first attempt.
func TestSubmitDoesNotRetryPermanentFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
	}{
		{"bad token", http.StatusUnauthorized},
		{"bad payload", http.StatusBadRequest},
		{"forbidden", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Fail every request; a non-retryable status must stop at one.
			calls := newFlakyListenBrainz(t, 99, tc.status)

			lbc := &ListenBrainzClient{token: "test-token"}
			if err := lbc.SubmitScrobble("Artist", "Title", "Album", time.Unix(1700000000, 0)); err == nil {
				t.Fatal("expected an error for a rejected submission, got nil")
			}
			if got := atomic.LoadInt32(calls); got != 1 {
				t.Errorf("expected exactly 1 attempt for a permanent failure, got %d", got)
			}
		})
	}
}

// TestSubmitGivesUpAfterMaxAttempts keeps the retry bounded, so a sustained
// upstream outage cannot spin indefinitely.
func TestSubmitGivesUpAfterMaxAttempts(t *testing.T) {
	calls := newFlakyListenBrainz(t, 99, http.StatusInternalServerError)

	lbc := &ListenBrainzClient{token: "test-token"}
	err := lbc.SubmitScrobble("Artist", "Title", "Album", time.Unix(1700000000, 0))
	if err == nil {
		t.Fatal("expected an error once the attempts are exhausted, got nil")
	}
	if got := atomic.LoadInt32(calls); int(got) != listenBrainzMaxAttempts {
		t.Errorf("expected %d attempts, got %d", listenBrainzMaxAttempts, got)
	}
	if !strings.Contains(err.Error(), "after 3 attempts") {
		t.Errorf("error %q should say how many attempts were made", err)
	}
}

// TestRetryBackoffMatchesMaxAttempts pins the invariant that submit indexes
// into: one delay per retry, which is one fewer than the attempt count.
func TestRetryBackoffMatchesMaxAttempts(t *testing.T) {
	if len(listenBrainzRetryBackoff) != listenBrainzMaxAttempts-1 {
		t.Fatalf("listenBrainzRetryBackoff has %d entries, need %d for %d attempts",
			len(listenBrainzRetryBackoff), listenBrainzMaxAttempts-1, listenBrainzMaxAttempts)
	}
}
