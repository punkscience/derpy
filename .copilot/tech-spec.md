# App: dirplay

## Description

A command-line application that will play music from a specified folder. 

Example: dirplay c:\Users\me\Music

## Features

[x] By default, the app will read all directory contents recursively into a list and then shuffle the list.
[x] The first song in the shuffled list will be played. 
[x] The user can press the left or right cursor buttons to interrupt playback and go back to the previous track or skip to the next track. 
[x] The user can pause playback by pressing space
[x] The user can exit the app by pressing escape
[x] The app should support audio playback on Windows, Linux and Mac
[x] The TUI interface should be minimal, simply showing the playing track and a progress bar.

## Nostr Integration

[x] Pressing [N] while a track is playing earmarks the track to a private NIP-51 list on Nostr (kind 30001, NIP-44 encrypted — only the key holder can read it).
[x] The earmark list syncs across any device sharing the same Nostr key; `dirplay list` fetches and prints it anywhere.
[x] Earmarks include artist, album, title, and timestamp; stored as NIP-44 ciphertext so relays see only encrypted blobs.
[x] The user's Nostr private key (nsec1... or raw hex) can be stored via `dirplay nostr-key <key>` and is persisted in ~/.config/dirplay/config.json (0600 permissions).
[x] If no key is configured when [N] is pressed, the TUI prompts for inline key entry (masked input); the key is saved for future use after the first earmark.
[x] Earmarks are published to: relay.damus.io, nos.lol, relay.nostr.band, nostr.wine — at least one must succeed.
[x] nostr.go + search.go retained for future public broadcast / Bandcamp+YouTube link feature (phase 2).

### Phase 2 (planned)
[ ] Blossom server upload of local audio files for cross-device streaming.
[ ] Public kind-1 broadcast option when earmarking, with Bandcamp/YouTube listen links.

## ListenBrainz Integration

[x] Optional scrobbling via LISTENBRAINZ_TOKEN env var or `dirplay token <token>` command.
[x] "Playing now" is submitted immediately; scrobble fires at 25% completion (minimum 30s tracks).

## Other Features

[x] Keyword filtering with AND/OR/parentheses expression syntax (`dirplay jazz AND piano`).
[x] Default source directory saved via `dirplay --set-default-source <dir>`.
[x] MPRIS2 D-Bus service on Linux for playerctl/Waybar integration.
[x] [D] key deletes the current track from disk and advances to the next.

