package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// searchHTTPClient is used for all outbound search requests.
// Tests may replace this with a client backed by an httptest.Server.
var searchHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
}

// bandcampSearchBase and youtubeSearchBase are the root URLs for searches.
// Tests override these to point at local httptest servers.
var (
	bandcampSearchBase = "https://bandcamp.com/search"
	youtubeSearchBase  = "https://www.youtube.com/results"
)

// bandcampTrackRe matches Bandcamp track page URLs embedded in search-result HTML.
// Track slugs use lowercase letters, digits, and hyphens/underscores.
var bandcampTrackRe = regexp.MustCompile(`https://[a-z0-9-]+\.bandcamp\.com/track/[a-z0-9_-]+`)

// youtubeVideoIDRe matches the videoId field in the ytInitialData JSON blob
// that YouTube embeds in every search-results page.
var youtubeVideoIDRe = regexp.MustCompile(`"videoId":"([a-zA-Z0-9_-]{11})"`)

// SearchLinks holds optional streaming/purchase links for a track.
type SearchLinks struct {
	Bandcamp string // empty if not found
	YouTube  string // empty if not found
}

// FindBestLink searches for a single listen link to embed in a public post.
// Bandcamp is tried first; YouTube is only queried if Bandcamp returns nothing.
// Returns an empty string if neither source finds a match.
func FindBestLink(artist, title, _ string) string {
	if bc := searchBandcamp(artist, title); bc != "" {
		return bc
	}
	return searchYouTube(artist, title)
}

// FindTrackLinks searches Bandcamp and YouTube for the track in parallel
// and returns whatever URLs were found within the shared HTTP timeout.
// Errors are silently swallowed — callers treat an empty string as "not found".
func FindTrackLinks(artist, title, _ string) SearchLinks {
	var links SearchLinks
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		links.Bandcamp = searchBandcamp(artist, title)
	}()
	go func() {
		defer wg.Done()
		links.YouTube = searchYouTube(artist, title)
	}()

	wg.Wait()
	return links
}

// searchBandcamp returns the URL of the first Bandcamp track result for the
// given artist and title, or an empty string on any error or no match.
func searchBandcamp(artist, title string) string {
	q := buildSearchQuery(artist, title)
	if q == "" {
		return ""
	}
	// item_type=t restricts results to tracks (vs. albums or artists).
	target := fmt.Sprintf("%s?q=%s&item_type=t", bandcampSearchBase, url.QueryEscape(q))
	body, err := fetchSearchPage(target)
	if err != nil {
		return ""
	}
	return bandcampTrackRe.FindString(body)
}

// searchYouTube returns the URL of the first YouTube video result for the
// given artist and title, or an empty string on any error or no match.
func searchYouTube(artist, title string) string {
	q := buildSearchQuery(artist, title)
	if q == "" {
		return ""
	}
	target := fmt.Sprintf("%s?search_query=%s", youtubeSearchBase, url.QueryEscape(q))
	body, err := fetchSearchPage(target)
	if err != nil {
		return ""
	}
	// YouTube embeds search results as ytInitialData JSON; extract the first videoId.
	if m := youtubeVideoIDRe.FindStringSubmatch(body); len(m) > 1 {
		return "https://www.youtube.com/watch?v=" + m[1]
	}
	return ""
}

// buildSearchQuery joins artist and title into a plain-text query string.
func buildSearchQuery(artist, title string) string {
	parts := make([]string, 0, 2)
	if artist != "" {
		parts = append(parts, artist)
	}
	if title != "" {
		parts = append(parts, title)
	}
	return strings.Join(parts, " ")
}

// fetchSearchPage performs a GET request with a browser-like User-Agent and
// returns the full response body as a string.
func fetchSearchPage(target string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	// A realistic User-Agent is needed for YouTube; Bandcamp is lenient.
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	resp, err := searchHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
