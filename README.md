# dirplay - A Cross-Platform Music Player

A command-line music player that plays audio files from a specified directory with a minimal Terminal User Interface (TUI).

## Features

- ✅ Recursively scans directories for audio files
- ✅ Shuffles playlist automatically
- ✅ Cross-platform audio playback (Windows, Linux, macOS)
- ✅ Minimal TUI with current track display and progress bar
- ✅ Keyboard controls for navigation and playback control
- ✅ Supports multiple audio formats: MP3, WAV, FLAC, OGG, M4A, AAC

## Installation

### Prerequisites
- Go 1.19 or higher

#### Linux
- For audio playback, you will need the ALSA development library.
- On Debian/Ubuntu, you can install it with:
  ```bash
  sudo apt install libasound2-dev
  ```
- On Fedora/Red Hat, you can install it with:
  ```bash
  sudo dnf install alsa-lib-devel
  ```

### Build from source
```bash
git clone <repository-url>
cd dirplay
go mod tidy
go build -o dirplay.exe  # On Windows
# or
go build -o dirplay      # On Linux/macOS
```

## Usage

```bash
dirplay <music_directory>
```

### Examples
```bash
# Windows
dirplay.exe "C:\Users\me\Music"

# Linux/macOS  
./dirplay "/home/user/Music"
./dirplay "~/Music"
```

## Controls

| Key | Action |
|-----|---------|
| `←` (Left Arrow) | Previous track |
| `→` (Right Arrow) | Next track |
| `SPACE` | Pause/Resume playback |
| `ESC` or `q` | Quit application |

## Supported Audio Formats

- **MP3** (.mp3)
- **WAV** (.wav)  
- **FLAC** (.flac)
- **OGG Vorbis** (.ogg)
- **M4A** (.m4a)
- **AAC** (.aac)

## How it works

1. **Directory Scan**: The application recursively scans the specified directory for supported audio files
2. **Playlist Shuffle**: All found audio files are added to a playlist and automatically shuffled
3. **Playback**: The first track in the shuffled playlist starts playing automatically
4. **Navigation**: Use arrow keys to skip between tracks or space to pause/resume
5. **Loop**: When the playlist ends, it automatically loops back to the first track

## Technical Details

- Built with Go using the [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI framework
- Audio playback powered by [Beep](https://github.com/gopxl/beep) audio library
- Cross-platform compatibility through Go's standard library and audio abstractions
- Real-time progress tracking and display

## Troubleshooting

### No audio output
- Ensure your system has audio drivers installed
- Check that the audio files are in a supported format
- Verify the directory path is correct and accessible

### Build errors
- Make sure you have Go 1.19+ installed
- Run `go mod tidy` to ensure all dependencies are downloaded
- Check that CGO is enabled if required by audio libraries

### Performance issues
- Large music collections may take a moment to scan initially
- Consider organizing music in smaller subdirectories if scanning is slow

## License

[Add your license information here]