package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withSearchServers spins up fake Bandcamp and YouTube HTTP servers, sets the
// package-level URL variables and HTTP client to point at them, runs f, then
// restores the originals.
func withSearchServers(t *testing.T, bandcampHTML, youtubeHTML string, f func()) {
	t.Helper()

	bcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(bandcampHTML))
	}))
	defer bcSrv.Close()

	ytSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(youtubeHTML))
	}))
	defer ytSrv.Close()

	// Save and replace globals.
	origBC := bandcampSearchBase
	origYT := youtubeSearchBase
	origClient := searchHTTPClient

	bandcampSearchBase = bcSrv.URL
	youtubeSearchBase = ytSrv.URL
	// Use a client without redirects so test servers are hit directly.
	searchHTTPClient = bcSrv.Client()

	defer func() {
		bandcampSearchBase = origBC
		youtubeSearchBase = origYT
		searchHTTPClient = origClient
	}()

	f()
}

func TestSearchBandcamp_Found(t *testing.T) {
	fakeHTML := `<html><body>
<a href="https://someartist.bandcamp.com/track/some-song">Some Song</a>
</body></html>`

	withSearchServers(t, fakeHTML, "", func() {
		got := searchBandcamp("Some Artist", "Some Song")
		want := "https://someartist.bandcamp.com/track/some-song"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestSearchBandcamp_NotFound(t *testing.T) {
	withSearchServers(t, "<html><body>No results</body></html>", "", func() {
		got := searchBandcamp("Unknown Artist", "Unknown Track")
		if got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})
}

func TestSearchBandcamp_EmptyQuery(t *testing.T) {
	// No HTTP call should be made — just returns empty.
	got := searchBandcamp("", "")
	if got != "" {
		t.Errorf("expected empty string for empty query, got %q", got)
	}
}

func TestSearchYouTube_Found(t *testing.T) {
	// YouTube embeds video data as JSON in ytInitialData; mimic that structure.
	fakeHTML := `<html><body>
var ytInitialData = {"videoId":"dQw4w9WgXcQ","title":"Never Gonna Give You Up"};
</body></html>`

	withSearchServers(t, "", fakeHTML, func() {
		got := searchYouTube("Rick Astley", "Never Gonna Give You Up")
		want := "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestSearchYouTube_NotFound(t *testing.T) {
	withSearchServers(t, "", "<html><body>no video here</body></html>", func() {
		got := searchYouTube("Ghost Artist", "Ghost Track")
		if got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})
}

func TestSearchYouTube_EmptyQuery(t *testing.T) {
	got := searchYouTube("", "")
	if got != "" {
		t.Errorf("expected empty string for empty query, got %q", got)
	}
}

func TestFindTrackLinks_BothFound(t *testing.T) {
	bcHTML := `<a href="https://artist.bandcamp.com/track/the-track">...</a>`
	ytHTML := `{"videoId":"abcdefghijk"}`

	withSearchServers(t, bcHTML, ytHTML, func() {
		links := FindTrackLinks("Artist", "The Track", "Album")
		if links.Bandcamp != "https://artist.bandcamp.com/track/the-track" {
			t.Errorf("Bandcamp: got %q", links.Bandcamp)
		}
		if links.YouTube != "https://www.youtube.com/watch?v=abcdefghijk" {
			t.Errorf("YouTube: got %q", links.YouTube)
		}
	})
}

func TestFindTrackLinks_NoneFound(t *testing.T) {
	withSearchServers(t, "<html>nothing</html>", "<html>nothing</html>", func() {
		links := FindTrackLinks("Artist", "Track", "Album")
		if links.Bandcamp != "" || links.YouTube != "" {
			t.Errorf("expected empty links, got Bandcamp=%q YouTube=%q", links.Bandcamp, links.YouTube)
		}
	})
}

func TestBuildSearchQuery(t *testing.T) {
	tests := []struct {
		artist, title string
		want          string
	}{
		{"Miles Davis", "So What", "Miles Davis So What"},
		{"", "So What", "So What"},
		{"Miles Davis", "", "Miles Davis"},
		{"", "", ""},
	}
	for _, tt := range tests {
		got := buildSearchQuery(tt.artist, tt.title)
		if got != tt.want {
			t.Errorf("buildSearchQuery(%q, %q) = %q, want %q", tt.artist, tt.title, got, tt.want)
		}
	}
}

func TestBandcampTrackRe(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{
			input: `<a href="https://miles-davis.bandcamp.com/track/so-what">So What</a>`,
			want:  "https://miles-davis.bandcamp.com/track/so-what",
		},
		{
			input: `href="https://artist-name.bandcamp.com/track/track_with_underscores"`,
			want:  "https://artist-name.bandcamp.com/track/track_with_underscores",
		},
		{
			input: "no bandcamp links here",
			want:  "",
		},
	}
	for _, c := range cases {
		got := bandcampTrackRe.FindString(c.input)
		if got != c.want {
			t.Errorf("bandcampTrackRe.FindString(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestYoutubeVideoIDRe(t *testing.T) {
	cases := []struct {
		input   string
		wantID  string
		wantNil bool
	}{
		{`{"videoId":"dQw4w9WgXcQ"}`, "dQw4w9WgXcQ", false},
		{`"videoId":"abc-DEF_123"`, "abc-DEF_123", false},
		{`no video here`, "", true},
		{`"videoId":"tooshort"`, "", true}, // 8 chars — not 11
	}
	for _, c := range cases {
		m := youtubeVideoIDRe.FindStringSubmatch(c.input)
		if c.wantNil {
			if len(m) > 0 {
				t.Errorf("expected no match for %q, got %v", c.input, m)
			}
			continue
		}
		if len(m) < 2 || m[1] != c.wantID {
			t.Errorf("for %q: got match %v, want ID %q", c.input, m, c.wantID)
		}
	}
}

// TestSearchQueryEncoding checks that special characters are URL-encoded in
// the query.  We verify by inspecting the request URL the test server receives.
func TestSearchQueryEncoding(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	origBC := bandcampSearchBase
	origClient := searchHTTPClient
	bandcampSearchBase = srv.URL
	searchHTTPClient = srv.Client()
	defer func() {
		bandcampSearchBase = origBC
		searchHTTPClient = origClient
	}()

	searchBandcamp("AC/DC", "Highway to Hell")

	// The query should contain the encoded form of "AC/DC Highway to Hell".
	if !strings.Contains(gotURL, "AC%2FDC") && !strings.Contains(gotURL, "AC%2fDC") {
		t.Errorf("URL %q does not contain encoded slash in artist name", gotURL)
	}
}
