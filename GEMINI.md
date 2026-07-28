# GEMINI.md - derpy Context

This file provides instructional context for Gemini when working on the `derpy` project.

## Project Overview

`derpy` is a cross-platform command-line music player written in Go. It recursively scans a directory for audio files, shuffles them into a playlist, and provides a minimal Terminal User Interface (TUI) for playback control.

### Core Technologies
- **Language:** Go (1.24.4+)
- **TUI Framework:** [Bubble Tea](https://github.com/charmbracelet/bubbletea) & [Lip Gloss](https://github.com/charmbracelet/lipgloss)
- **Audio Engine:** [Beep](https://github.com/gopxl/beep)
- **CLI Parser:** [Cobra](https://github.com/spf13/cobra)
- **Metadata Tags:** [tag](https://github.com/dhowden/tag)
- **Scrobbling:** [ListenBrainz](https://listenbrainz.org/) (via `go-listenbrainz`)

## Architecture

The project is structured into several Go source files:
- `main.go`: Entry point, CLI command definitions (using Cobra), and directory scanning logic.
- `model.go`: The Bubble Tea TUI model and update/view logic.
- `audio.go`: High-level `AudioPlayer` abstraction managing `beep` streamers and scrobble tracking.
- `speaker.go`: Low-level speaker initialization and access (wraps `beep/speaker`).
- `config.go`: Configuration persistence (stored in `~/.config/derpy/config.json`).
- `listenbrainz.go`: Integration with ListenBrainz for "Now Playing" and scrobbling (at 25% progress).
- `shuffle.go`: Playlist randomization logic.

## Building and Running

### Build Commands
```bash
# Install dependencies
go mod tidy

# Build the executable
go build -o derpy
```

### Running the Player
```bash
# Play a directory (TUI mode)
./derpy /path/to/music

# Play with keyword filtering (matches filename or relative path)
# Shuffling still applies to the filtered results.
./derpy /path/to/music jesus gospel

# Play without TUI
./derpy --no-tui /path/to/music
```

# Configure ListenBrainz token
./derpy token YOUR_LISTENBRAINZ_TOKEN
```

## Development Conventions

- **TUI Updates:** Follow the Elm architecture (Model, Update, View) as defined by Bubble Tea.
- **Audio Safety:** Use `speakerLock()` and `speakerUnlock()` when modifying audio state that the speaker might be reading.
- **Metadata:** Prefer ID3/Metadata tags for display; fallback to filenames if tags are missing.
- **External Integration:**
    - **ListenBrainz:** Tokens are loaded from `~/.config/derpy/config.json` or the `LISTENBRAINZ_TOKEN` environment variable.
    - **Track Notes:** Pressing `n` appends the current track to `~/track-notes.md`.
    - **File Deletion:** Pressing `d` deletes the current file from disk and advances the playlist.

## Keyboard Controls (TUI)

| Key | Action |
|-----|---------|
| `←` (Left Arrow) | Previous track |
| `→` (Right Arrow) | Next track |
| `SPACE` | Pause/Resume playback |
| `n` | Save track info to `~/track-notes.md` |
| `d` | **Delete** current file from disk and skip to next |
| `q` or `ESC` | Quit application |

## The earmark protocol is not in this repo

Earmark crypto, Blossom storage, the Nostr list and channels live in
`github.com/punkscience/earmark/earmark-core`, shared with the earmark CLI and
imported here as `core`. `blossom.go` and `nostr_list.go` were deleted when it
was extracted — do not reintroduce protocol code here.

`go.mod` carries a `replace` to `../earmark/earmark-core`, so derpy currently
needs the earmark repo checked out as a sibling directory.

Keyboard: `[E]` earmark (with a channel target picker when you are in any),
`[C]` browse the channel feed.
