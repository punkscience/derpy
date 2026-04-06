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

[x] Pressing [E] while a track is playing earmarks the track to a private NIP-51 list on Nostr (kind 30001, NIP-44 encrypted — only the key holder can read it).
[x] The earmark list syncs across any device sharing the same Nostr key; `dirplay list` fetches and prints it anywhere.
[x] Earmarks include artist, album, title, path, and timestamp; stored as NIP-44 ciphertext so relays see only encrypted blobs.
[x] The user's Nostr private key (nsec1... or raw hex) can be stored via `dirplay nostr-key <key>` and is persisted in ~/.config/dirplay/config.json (0600 permissions).
[x] If no key is configured when [E] is pressed, the TUI prompts for inline key entry (masked input); the key is saved for future use after the first earmark.
[x] Earmarks are published to: relay.damus.io, nos.lol, relay.primal.net, nostr.wine — at least one must succeed.
[x] Offline earmark queue: earmarks are written to ~/.config/dirplay/queue.json immediately on [E] press and synced to Nostr on next startup when connectivity is available.
[x] Duplicate detection: earmarks are keyed by file path; re-earmarking the same file is a no-op with an "already earmarked" status message.
[x] `dirplay earmarks` fetches the private list and plays all earmarked files as a playlist (files missing from disk are skipped with a warning).

### Phase 2 — Blossom Audio Upload (planned)

Upload local audio files to Blossom servers in encrypted chunks so earmarked
tracks can be played on any device — even one that doesn't have the original
file on disk.

**Why chunking is required:** public Blossom servers enforce a ~20 MB upload
limit. A typical FLAC is 40–100 MB. Files must be split before upload.

#### Upload flow (`dirplay upload` or triggered from [E] earmark)

[ ] Split the file into 16 MB chunks (safe under the 20 MB server limit after
    encryption overhead).
[ ] Generate a random 256-bit per-file AES-256-GCM encryption key.
[ ] Encrypt each chunk independently with the same key (unique 12-byte nonce
    per chunk). Independent encryption gives per-chunk integrity verification
    without downloading everything first.
[ ] Compute the SHA-256 of each *encrypted* chunk (this is the Blossom blob ID).
[ ] Upload each chunk to at least 2 Blossom servers concurrently for redundancy.
    A single server going offline must not make a track unplayable.
[ ] Store the upload manifest inside the existing NIP-51 earmark payload
    (already NIP-44 encrypted — only the key holder can read it):

```json
{
  "artist": "...", "album": "...", "title": "...", "path": "...", "ts": 0,
  "blossom": {
    "key": "<base64 AES-256-GCM key>",
    "chunks": [
      {"index": 0, "sha256": "a3f9...", "size": 16777216,
       "servers": ["https://blossom.band", "https://cdn.satellite.earth"]},
      {"index": 1, "sha256": "b812...", "size": 16777216,
       "servers": ["https://blossom.band", "https://nostr.build"]},
      {"index": 2, "sha256": "cc04...", "size": 4194304,
       "servers": ["https://cdn.satellite.earth", "https://nostr.build"]}
    ]
  }
}
```

#### Download + playback flow (`dirplay earmarks` on a remote device)

[ ] On a device where the local file is missing, detect the `blossom` field in
    the earmark.
[ ] Download all chunks in parallel (one goroutine per chunk, try servers in
    order until one succeeds).
[ ] Show a download progress bar in the TUI before playback begins.
[ ] Verify each chunk's SHA-256 after download; retry from alternate server on
    mismatch.
[ ] Decrypt and reassemble chunks to a temp file under the OS temp directory.
[ ] Begin playback from the temp file; delete on track change or app exit.

#### Blossom server management

[ ] kind-10063 discovery: on first upload, fetch the user's kind-10063 Nostr
    event (their published Blossom server list) and use those servers
    automatically — no manual configuration needed if the user already has one.
[ ] Built-in public defaults (same pattern as relay defaults):
    - `https://blossom.band`
    - `https://cdn.satellite.earth`
    - `https://nostr.build`
[ ] `dirplay blossom list` — show active server list.
[ ] `dirplay blossom add <url>` — add a server.
[ ] `dirplay blossom remove <url>` — remove a server.
[ ] `dirplay blossom reset` — reset to built-in defaults.

#### Authentication

[ ] Each upload request requires a BUD-11 kind-24242 Nostr event signed with
    the user's private key and passed as a Bearer token in the Authorization
    header. The event must include the blob SHA-256 and an expiry timestamp.

#### Files to create / modify

| File | Role |
|------|------|
| `blossom.go` | Chunk, encrypt, upload, download, reassemble |
| `blossom_test.go` | Unit tests for chunk/encrypt/decrypt round-trips |
| `main.go` | `blossom` subcommand group; wire upload into earmark flow |
| `model.go` | Download progress display; detect missing-file earmarks |
| `nostr_list.go` | Extend `Earmark` struct with optional `Blossom` field |

## Public Nostr Post ([P] key)

[x] Pressing [P] while a track is playing searches Bandcamp and YouTube for listen links, then publishes a public kind-1 Nostr note.
[x] Note format: "[npub] is really digging [title] by [artist] right now! #music #dirplay" with Bandcamp/YouTube links appended if found.
[x] Gracefully handles missing metadata (no artist, no title, etc.).
[x] Reuses the same inline key-entry flow as [N]; key entry triggered by [P] routes back to a public post rather than an earmark.

## ListenBrainz Integration

[x] Optional scrobbling via LISTENBRAINZ_TOKEN env var or `dirplay token <token>` command.
[x] "Playing now" is submitted immediately; scrobble fires at 25% completion (minimum 30s tracks).

## Other Features

[x] Keyword filtering with AND/OR/parentheses expression syntax (`dirplay jazz AND piano`).
[x] Default source directory saved via `dirplay --set-default-source <dir>`.
[x] MPRIS2 D-Bus service on Linux for playerctl/Waybar integration.
[x] [D] key deletes the current track from disk and advances to the next.

