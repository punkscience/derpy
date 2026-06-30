package main

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

const bskyPDS = "https://bsky.social"

// createBskySession authenticates with the PDS and returns a client bearing the
// session JWT and DID for subsequent calls. The Identifier field accepts a
// handle directly — ServerCreateSession resolves it internally.
func createBskySession(ctx context.Context, handle, appPassword string) (*xrpc.Client, error) {
	client := &xrpc.Client{Host: bskyPDS}
	auth, err := atproto.ServerCreateSession(ctx, client, &atproto.ServerCreateSession_Input{
		Identifier: handle,
		Password:   appPassword,
	})
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	return &xrpc.Client{
		Host: bskyPDS,
		Auth: &xrpc.AuthInfo{
			AccessJwt: auth.AccessJwt,
			Did:       auth.Did,
			Handle:    auth.Handle,
		},
	}, nil
}

// buildBskyPostText builds the post body and its richtext facets.
//
// The text has the shape:
//
//	Really digging {title} by {artist} right now 🎵
//	listen on {platform}
//	#music #derpy
//
// When link is empty the "listen on …" line is omitted.
// Facets are built for the link text and both hashtag spans; byte offsets
// are computed against the UTF-8 encoding of the full text.
func buildBskyPostText(artist, title, link string) (text string, facets []*bsky.RichtextFacet) {
	var digging string
	switch {
	case title != "" && artist != "":
		digging = fmt.Sprintf("%s by %s", title, artist)
	case title != "":
		digging = title
	case artist != "":
		digging = fmt.Sprintf("a track by %s", artist)
	default:
		digging = "this track"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Really digging %s right now 🎵", digging))
	if link != "" {
		var platform string
		switch {
		case strings.Contains(link, "bandcamp.com"):
			platform = "Bandcamp"
		case strings.Contains(link, "youtube.com"):
			platform = "YouTube"
		default:
			platform = "listen"
		}
		sb.WriteString(fmt.Sprintf("\nlisten on %s", platform))
	}
	sb.WriteString("\n#music #derpy")

	text = sb.String()
	textBytes := []byte(text)

	// Build facets by locating substrings in the UTF-8 byte slice.

	// Link facet — locate the "listen on {platform}" span.
	if link != "" {
		// The listen line appears after the first newline.
		nlIdx := strings.Index(text, "\n")
		if nlIdx >= 0 {
			listenLine := text[nlIdx+1 : strings.Index(text[nlIdx+1:], "\n")+nlIdx+1]
			start := strings.Index(text, listenLine)
			end := start + len(listenLine)
			facets = append(facets, &bsky.RichtextFacet{
				Features: []*bsky.RichtextFacet_Features_Elem{
					{RichtextFacet_Link: &bsky.RichtextFacet_Link{Uri: link}},
				},
				Index: &bsky.RichtextFacet_ByteSlice{
					ByteStart: int64(start),
					ByteEnd:   int64(end),
				},
			})
		}
	}

	// Tag facets — locate #music and #derpy in the byte slice.
	for _, tag := range []string{"#music", "#derpy"} {
		start := bytesIndex(textBytes, []byte(tag))
		if start < 0 {
			continue
		}
		end := start + len(tag)
		facets = append(facets, &bsky.RichtextFacet{
			Features: []*bsky.RichtextFacet_Features_Elem{
				{RichtextFacet_Tag: &bsky.RichtextFacet_Tag{Tag: strings.TrimPrefix(tag, "#")}},
			},
			Index: &bsky.RichtextFacet_ByteSlice{
				ByteStart: int64(start),
				ByteEnd:   int64(end),
			},
		})
	}

	return text, facets
}

// bytesIndex returns the index of the first occurrence of needle in haystack,
// or -1 if not found. Mirrors bytes.Index but avoids an extra import.
func bytesIndex(haystack, needle []byte) int {
	if len(needle) == 0 {
		return 0
	}
	if len(needle) > len(haystack) {
		return -1
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// PublishBskyPost authenticates to Bluesky and publishes a text post about the
// given track. handle is the user's Bluesky handle (e.g. "user.bsky.social");
// appPassword is an app-specific password. link is a pre-resolved listen link
// (may be empty).
func PublishBskyPost(handle, appPassword, artist, title, link string) error {
	// Normalize whitespace from copy-paste.
	handle = strings.TrimSpace(handle)
	handle = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, handle)
	appPassword = strings.TrimSpace(appPassword)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	authClient, err := createBskySession(ctx, handle, appPassword)
	if err != nil {
		return err
	}

	text, facets := buildBskyPostText(artist, title, link)

	post := &bsky.FeedPost{
		Text:      text,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Facets:    facets,
		Langs:     []string{"en"},
	}

	_, err = atproto.RepoCreateRecord(ctx, authClient, &atproto.RepoCreateRecord_Input{
		Repo:       authClient.Auth.Did,
		Collection: "app.bsky.feed.post",
		Record:     &lexutil.LexiconTypeDecoder{Val: post},
	})
	if err != nil {
		return fmt.Errorf("could not create post: %w", err)
	}
	return nil
}