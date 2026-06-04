package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PlayerModel represents the state of the music player TUI
type PlayerModel struct {
	playlist     []string
	currentIndex int
	player       *AudioPlayer
	playing      bool
	paused       bool
	position     time.Duration
	duration     time.Duration
	width        int
	height       int
	err          error
	artist       string
	title        string
	album        string
	tickInterval time.Duration
	// mpris is the optional MPRIS2 D-Bus service.  It is nil when the session
	// D-Bus is unavailable or when running in --no-tui mode.
	mpris *MPRISService

	// Nostr key-entry state: when the user presses [E] or [P] and no key is
	// configured, the TUI switches into a one-time key-entry mode.
	nostrKeyEntry    bool   // true while waiting for the user to type their key
	nostrKeyBuffer   string // accumulates characters typed by the user
	nostrKeyForPost  bool   // true when key entry was triggered by [P] (public post) vs [E] (earmark)
	nostrStatus      string // last Nostr result message shown to the user

	// Tag and Sum state. The TagIndex and SumCache are loaded once at startup
	// and shared across all track-loads. currentSum/currentTags refresh on
	// each track change via the eager-compute step in loadCurrentTrack.
	tagIndex    *TagIndex
	sumCache    *SumCache
	currentSum  string   // tag.Sum of the currently-playing Track, "" if unknown
	currentTags []string // Tags applied to the currently-playing Track, nil if none

	// Tag-entry state: when the user presses [T] on a playing Track whose
	// Sum is known, the TUI enters an inline editor pre-filled with the
	// current Tag list. Submitting saves the new Tag set to the on-disk
	// TagIndex; cancelling discards the buffer.
	tagEntry  bool
	tagBuffer string

	// Background-indexer progress. While indexingDone < indexingTotal the
	// View shows an animated three-dot spinner above the controls; the line
	// vanishes when done catches total or when the indexer is not running
	// (both fields zero).
	indexingDone  int
	indexingTotal int
	// spinnerFrame advances modulo 3 on each indexProgressMsg, driving the
	// PS three-dot colour rotation (lime -> olive -> dark-olive).
	spinnerFrame int
}

// indexProgressMsg is sent by IndexSource (running in a background
// goroutine) each time it advances. done==total signals the sweep is
// complete and the status line should disappear.
type indexProgressMsg struct {
	done  int
	total int
}

// tagsSavedMsg signals that a write to ~/.config/derpy/tags.json completed.
// Carries the saved sum so the View can ignore stale saves when the user
// has already skipped to a different Track.
type tagsSavedMsg struct {
	sum string
	err error
}

// Messages for the TUI
type tickMsg time.Time
type positionMsg time.Duration
type trackEndedMsg struct{}
type playErrorMsg error
type trackLoadedMsg struct {
	duration time.Duration
	artist   string
	title    string
	album    string
	sum      string   // tag.Sum of the loaded Track, "" if hashing failed
	tags     []string // Tags applied to the loaded Track (defensive copy from TagIndex)
}
// nostrPublishedMsg is sent after a Nostr publish attempt completes.
type nostrPublishedMsg struct {
	err       error  // nil on success
	action    string // "earmark" or "post" — drives the status display
	queued    bool   // true when the earmark was saved locally but not yet published (offline)
	duplicate bool   // true when the track was already earmarked
}

// queueFlushedMsg is sent after a background queue-flush attempt completes.
type queueFlushedMsg struct {
	count int // number of earmarks successfully published from the queue
}

// cleanupMsg is sent after the startup old-earmark cleanup completes.
type cleanupMsg struct {
	removed int // number of earmarks older than EarmarkMaxAge that were purged
}


type trackDeletedMsg struct {
	err error
}

// NewPlayerModel creates a new player model.
//
// The Tag index and Sum cache are loaded from disk here so the player has
// them ready by the time the first trackLoadedMsg arrives. Load failures
// degrade to empty in-memory copies — tag features go silent for the
// session rather than blocking playback.
func NewPlayerModel(playlist []string) *PlayerModel {
	ti, _ := LoadTagIndex()
	sc, _ := LoadSumCache()
	return &PlayerModel{
		playlist:     playlist,
		currentIndex: 0,
		player:       NewAudioPlayer(),
		tickInterval: 100 * time.Millisecond, // Make tick interval configurable
		tagIndex:     ti,
		sumCache:     sc,
	}
}

// Init initializes the model
func (m *PlayerModel) Init() tea.Cmd {
	return tea.Batch(
		m.loadCurrentTrack(),
		m.tickCmd(),
		m.flushQueueCmd(),   // retry any earmarks that failed to publish last session
		m.cleanupCmd(),      // purge earmarks older than 30 days + their Blossom chunks
	)
}

// Update handles messages and updates the model
func (m *PlayerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		// While in Tag-entry mode, consume all keystrokes for input.
		// Mirrors the nostrKeyEntry handler below — the two modes are
		// mutually exclusive in practice because their trigger keys
		// ([T] vs [E]/[P]) are themselves captured as input characters
		// while the other mode is active.
		if m.tagEntry {
			switch msg.String() {
			case "esc":
				// Discard the buffer; on-disk Tags are untouched.
				m.tagEntry = false
				m.tagBuffer = ""
			case "enter":
				// Normalize and commit the new Tag set. The in-memory
				// update happens before the disk write returns so the
				// right column reflects the change immediately.
				tags := NormalizeTags(m.tagBuffer)
				sum := m.currentSum
				if sum != "" {
					m.tagIndex.Set(sum, tags)
					m.currentTags = tags
				}
				m.tagEntry = false
				m.tagBuffer = ""
				if sum != "" {
					return m, m.persistTagIndexCmd(sum)
				}
				return m, nil
			case "backspace", "ctrl+h":
				if len(m.tagBuffer) > 0 {
					runes := []rune(m.tagBuffer)
					m.tagBuffer = string(runes[:len(runes)-1])
				}
			default:
				if len(msg.Runes) == 1 && unicode.IsPrint(msg.Runes[0]) {
					m.tagBuffer += string(msg.Runes)
				}
			}
			return m, nil
		}

		// While in Nostr key-entry mode, consume all keystrokes for input.
		if m.nostrKeyEntry {
			switch msg.String() {
			case "esc":
				// Cancel key entry without saving.
				m.nostrKeyEntry = false
				m.nostrKeyBuffer = ""
				m.nostrStatus = "Nostr key entry cancelled."
			case "enter":
				// User submitted their key — validate, save, then perform the
				// action that triggered key entry ([E] earmark or [P] post).
				key := m.nostrKeyBuffer
				forPost := m.nostrKeyForPost
				m.nostrKeyEntry = false
				m.nostrKeyBuffer = ""
				m.nostrKeyForPost = false
				if forPost {
					m.nostrStatus = "Nostr: searching for links and posting..."
					return m, m.saveKeyAndPublish(key)
				}
				m.nostrStatus = "Nostr: saving earmark..."
				return m, m.saveKeyAndAddEarmark(key)
			case "backspace", "ctrl+h":
				if len(m.nostrKeyBuffer) > 0 {
					m.nostrKeyBuffer = m.nostrKeyBuffer[:len(m.nostrKeyBuffer)-1]
				}
			default:
				// Append printable characters only (ignore control sequences and null
				// bytes, which Windows terminals can inject during clipboard paste).
				if len(msg.Runes) == 1 && unicode.IsPrint(msg.Runes[0]) {
					m.nostrKeyBuffer += string(msg.Runes)
				}
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.player.Close()
			return m, tea.Quit

		case " ":
			// Toggle pause/play
			if m.playing {
				if m.paused {
					m.player.Resume()
					m.paused = false
					m.mpris.NotifyStateChanged(m)
					// Restart ticking when resuming
					return m, m.tickCmd()
				} else {
					m.player.Pause()
					m.paused = true
					m.mpris.NotifyStateChanged(m)
					// Stop ticking when paused (handled by tickMsg case)
				}
			}

		case "left":
			// Previous track
			m.player.Stop()
			m.currentIndex--
			if m.currentIndex < 0 {
				m.currentIndex = len(m.playlist) - 1 // Loop to last track
			}
			return m, m.loadCurrentTrack()

		case "right":
			// Next track
			m.player.Stop()
			m.currentIndex++
			if m.currentIndex >= len(m.playlist) {
				m.currentIndex = 0 // Loop back to first track
			}
			return m, m.loadCurrentTrack()

		case "e":
			// Add the current track to the private Nostr earmark list.
			// The earmark is written to the local queue first (guarantees no
			// data loss), then a Nostr publish is attempted in the background.
			if m.playing {
				cfg, err := LoadConfig()
				if err != nil || cfg.NostrPrivateKey == "" {
					// No key configured — enter inline key-entry mode.
					m.nostrKeyEntry = true
					m.nostrKeyBuffer = ""
					m.nostrStatus = ""
					return m, nil
				}
				// Key is available: earmark to private Nostr list.
				m.nostrStatus = "Nostr: saving earmark..."
				return m, m.saveEarmarkCmd(cfg.NostrPrivateKey)
			}

		case "p":
			// Publish the current track as a public Nostr post with listen links.
			if m.playing {
				cfg, err := LoadConfig()
				if err != nil || cfg.NostrPrivateKey == "" {
					m.nostrKeyEntry = true
					m.nostrKeyBuffer = ""
					m.nostrKeyForPost = true
					m.nostrStatus = ""
					return m, nil
				}
				m.nostrStatus = "Nostr: searching for links and posting..."
				return m, m.publishToNostr(cfg.NostrPrivateKey)
			}

		case "d":
			// Delete current track from disk and advance to next
			if m.playing && m.currentIndex < len(m.playlist) {
				return m, m.deleteCurrentTrack()
			}

		case "t":
			// Enter Tag-entry mode for the current Track. Requires a known
			// Sum (eager hash on track-load — see slice 2). If Sum is
			// missing (rare: tag.Sum failed for this file), [T] is a
			// no-op so we don't accept Tag input we can't persist.
			if m.playing && m.currentSum != "" {
				m.tagEntry = true
				m.tagBuffer = JoinTags(m.currentTags)
			}
		}

	// ---- MPRIS2 command messages injected by MPRISService -------------------

	case mprisPlayMsg:
		if m.playing && m.paused {
			// Resume from pause.
			m.player.Resume()
			m.paused = false
			m.mpris.NotifyStateChanged(m)
			return m, m.tickCmd()
		} else if !m.playing {
			// Nothing loaded yet — load and play.
			return m, m.loadCurrentTrack()
		}

	case mprisPauseMsg:
		if m.playing && !m.paused {
			m.player.Pause()
			m.paused = true
			m.mpris.NotifyStateChanged(m)
		}

	case mprisPlayPauseMsg:
		if m.playing {
			if m.paused {
				m.player.Resume()
				m.paused = false
				m.mpris.NotifyStateChanged(m)
				return m, m.tickCmd()
			} else {
				m.player.Pause()
				m.paused = true
				m.mpris.NotifyStateChanged(m)
			}
		}

	case mprisStopMsg:
		if m.playing {
			m.player.Stop()
			m.playing = false
			m.paused = false
			m.mpris.NotifyStateChanged(m)
		}

	case mprisNextMsg:
		m.player.Stop()
		m.currentIndex++
		if m.currentIndex >= len(m.playlist) {
			m.currentIndex = 0
		}
		return m, m.loadCurrentTrack()

	case mprisPreviousMsg:
		m.player.Stop()
		m.currentIndex--
		if m.currentIndex < 0 {
			m.currentIndex = len(m.playlist) - 1
		}
		return m, m.loadCurrentTrack()

	case mprisSeekMsg:
		if m.playing {
			newPos := m.position + msg.offset
			if newPos < 0 {
				newPos = 0
			}
			if newPos > m.duration {
				newPos = m.duration
			}
			if err := m.player.Seek(newPos); err == nil {
				m.position = newPos
				m.mpris.EmitSeeked(m)
			}
		}

	case mprisSetPositionMsg:
		if m.playing {
			pos := msg.pos
			if pos < 0 {
				pos = 0
			}
			if pos > m.duration {
				pos = m.duration
			}
			if err := m.player.Seek(pos); err == nil {
				m.position = pos
				m.mpris.EmitSeeked(m)
			}
		}

	case tickMsg:
		// Get current position from player directly
		m.position = m.player.GetPosition()

		// Check if track ended using the new HasEnded method
		if m.playing && m.player.HasEnded() {
			return m, func() tea.Msg {
				return trackEndedMsg{}
			}
		}

		// Only continue ticking if we're actually playing
		if m.playing && !m.paused {
			return m, m.tickCmd()
		}

		return m, nil

	case trackEndedMsg:
		m.player.Stop()
		m.currentIndex++
		if m.currentIndex >= len(m.playlist) {
			m.currentIndex = 0 // Loop back to the first track
		}
		return m, m.loadCurrentTrack()

	case trackLoadedMsg:
		m.playing = true
		m.paused = false
		m.position = 0
		m.duration = msg.duration
		m.artist = msg.artist
		m.title = msg.title
		m.album = msg.album
		m.currentSum = msg.sum
		m.currentTags = msg.tags
		// Notify MPRIS clients that metadata and status have changed.
		m.mpris.NotifyStateChanged(m)
		m.mpris.EmitSeeked(m) // Position jumped to 0 on track change.
		// Restart the tick cycle for position updates
		return m, m.tickCmd()

	case trackDeletedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		// Playlist was already updated in deleteCurrentTrack; load the next track.
		// currentIndex was adjusted to stay in bounds.
		if len(m.playlist) == 0 {
			return m, tea.Quit
		}
		return m, m.loadCurrentTrack()

	case nostrPublishedMsg:
		if msg.err != nil {
			m.nostrStatus = fmt.Sprintf("Nostr: %s failed — %v", msg.action, msg.err)
		} else if msg.action == "post" {
			m.nostrStatus = "Nostr: posted!"
		} else if msg.duplicate {
			m.nostrStatus = "Nostr: already earmarked"
		} else if msg.queued {
			m.nostrStatus = "Nostr: earmark queued (offline — will sync later)"
		} else {
			m.nostrStatus = "Nostr: earmark saved!"
		}
		return m, nil

	case queueFlushedMsg:
		if msg.count > 0 {
			m.nostrStatus = fmt.Sprintf("Nostr: synced %d queued earmark(s)", msg.count)
		}
		return m, nil

	case indexProgressMsg:
		m.indexingDone = msg.done
		m.indexingTotal = msg.total
		m.spinnerFrame = (m.spinnerFrame + 1) % 3
		return m, nil

	case tagsSavedMsg:
		// Fire-and-observe: the in-memory Tag index was updated before this
		// message was dispatched, so the UI is already current. We surface
		// disk-write failures via the nostrStatus line for now (no dedicated
		// tag status line yet — kept lightweight for slice 4).
		if msg.err != nil {
			m.nostrStatus = fmt.Sprintf("Tags: save failed — %v", msg.err)
		}
		return m, nil

	case cleanupMsg:
		if msg.removed > 0 {
			m.nostrStatus = fmt.Sprintf("Nostr: removed %d expired earmark(s)", msg.removed)
		}
		return m, nil

	case playErrorMsg:
		m.err = error(msg)
		return m, nil

	case positionMsg:
		m.position = time.Duration(msg)
	}

	return m, nil
}

// View renders the TUI
func (m *PlayerModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("error: %v\nq or esc to quit", m.err)
	}

	if len(m.playlist) == 0 {
		return "silence.\nq or esc to quit"
	}

	// Determine track display
	var trackDisplay string
	if m.title != "" && m.artist != "" {
		trackDisplay = fmt.Sprintf("%s - %s", m.artist, m.title)
	} else if m.title != "" {
		trackDisplay = m.title
	} else {
		// Fallback to filename
		trackDisplay = filepath.Base(m.playlist[m.currentIndex])
		if ext := filepath.Ext(trackDisplay); ext != "" {
			trackDisplay = strings.TrimSuffix(trackDisplay, ext)
		}
	}

	// Build the UI
	var content strings.Builder

	// Title
	content.WriteString(psTitle.Render("punk.science · derpy"))
	content.WriteString("\n\n")

	// Current track
	content.WriteString(psTrack.Render(trackDisplay))
	content.WriteString("\n")

	// Track info
	trackInfo := fmt.Sprintf("Track %d of %d", m.currentIndex+1, len(m.playlist))
	content.WriteString(psStatus.Render(trackInfo))
	content.WriteString("\n")

	// Status
	status := "■ stopped"
	if m.playing {
		if m.paused {
			status = "⏸ paused"
		} else {
			status = "▶ playing"
		}
	}
	content.WriteString(psStatus.Render(status))
	content.WriteString("\n\n")

	// Progress bar
	progressBar := m.renderProgressBar(40)
	content.WriteString(psProgress.Render(progressBar))
	content.WriteString("\n")

	// Time display
	posStr := formatDuration(m.position)
	durStr := formatDuration(m.duration)
	timeDisplay := fmt.Sprintf("%s / %s", posStr, durStr)
	content.WriteString(psStatus.Render(timeDisplay))
	content.WriteString("\n")

	// Nostr key-entry overlay: shown instead of controls while waiting for input.
	if m.nostrKeyEntry {
		// Mask the typed key with asterisks for security.
		masked := strings.Repeat("*", len(m.nostrKeyBuffer))
		content.WriteString("\n")
		content.WriteString(psPrompt.Render("nostr key required."))
		content.WriteString("\n")
		content.WriteString(psPrompt.Render("paste nsec1... or hex key — enter to save, esc to cancel"))
		content.WriteString("\n")
		content.WriteString(psPrompt.Render(fmt.Sprintf("> %s", masked)))
		return content.String()
	}

	// Nostr status (last publish result).
	if m.nostrStatus != "" {
		content.WriteString(psNostr.Render(m.nostrStatus))
		content.WriteString("\n")
	}

	// Tag-entry overlay: shown in place of the controls hint while the
	// user is editing Tags for the current Track. The right column stays
	// visible (see grilling decision: "you're editing what's there —
	// better to see it").
	if m.tagEntry {
		content.WriteString("\n")
		content.WriteString(psPrompt.Render("tags for this track."))
		content.WriteString("\n")
		content.WriteString(psPrompt.Render("comma-separated · stripped to [a-z0-9 ]"))
		content.WriteString("\n")
		content.WriteString(psPrompt.Render(fmt.Sprintf("> %s", m.tagBuffer)))
		content.WriteString("\n")
		content.WriteString(psPrompt.Render("enter save  esc cancel"))
		return m.composeWithTagColumn(content.String())
	}

	// Background-indexer status. Visible only while the sweep is in
	// progress (0 < done < total). Disappears the moment it catches up,
	// without leaving a blank line behind on the next render.
	if m.indexingTotal > 0 && m.indexingDone < m.indexingTotal {
		content.WriteString(psIndexer.Render(fmt.Sprintf("indexing %d/%d tracks…", m.indexingDone, m.indexingTotal)))
		content.WriteString("\n")
	}

	// Controls
	controls := "← prev  → next  space pause  e earmark  p post  t tag  d delete  esc quit"
	content.WriteString(psControls.Render(controls))

	return m.composeWithTagColumn(content.String())
}

// composeWithTagColumn joins the left-column content with the right-edge
// Tag column when applicable, falling back to single-column otherwise.
// Centralised so both the normal playback view and the tag-entry overlay
// share the same composition logic.
func (m *PlayerModel) composeWithTagColumn(left string) string {
	if right := renderTagsColumn(m.currentTags); right != "" && m.width >= twoColumnMinWidth {
		return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}
	return left
}

// twoColumnMinWidth is the terminal width below which the right-edge Tag
// column is dropped and the TUI falls back to a single-column layout.
const twoColumnMinWidth = 60

// tagColumnWidth is the fixed character width of the right-edge Tag column.
// Tags longer than this are truncated with an ellipsis so the column stays
// rectangular regardless of Tag length.
const tagColumnWidth = 20

// renderTagsColumn returns the vertical Tag stack rendered as a lipgloss
// block, suitable for JoinHorizontal with the main content. Returns "" when
// there are no Tags to display, signalling the caller to skip the column.
func renderTagsColumn(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(psTagHeader.Copy().MarginLeft(2).Render("tags"))
	sb.WriteString("\n")
	for _, t := range tags {
		sb.WriteString(psTag.Copy().MarginLeft(2).Render(truncateTag(t, tagColumnWidth)))
		sb.WriteString("\n")
	}
	return sb.String()
}

// truncateTag clips s to at most width runes, appending an ellipsis when
// truncation actually happens. Operates on runes, not bytes, so it handles
// Tags with multi-byte characters cleanly (even though NormalizeTags
// currently only emits [a-z0-9 ]).
func truncateTag(s string, width int) string {
	if width <= 1 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	return string(runes[:width-1]) + "…"
}

// renderProgressBar renders a progress bar
func (m *PlayerModel) renderProgressBar(width int) string {
	if m.duration == 0 {
		return strings.Repeat("─", width)
	}

	progress := float64(m.position) / float64(m.duration)
	filled := int(progress * float64(width))

	if filled > width {
		filled = width
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("─", width-filled)
	return fmt.Sprintf("[%s]", bar)
}

// tickCmd returns a command to send tick messages
func (m *PlayerModel) tickCmd() tea.Cmd {
	return tea.Tick(m.tickInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// loadCurrentTrack loads and plays the current track.
//
// As part of the same goroutine we also compute the Track's Sum (using the
// SumCache when valid, otherwise tag.Sum on the file) and look up its Tags.
// Sum computation runs before LoadTrack so we don't contend with beep on
// persistTagIndexCmd writes the in-memory TagIndex to disk in a goroutine.
// The TagIndex itself was already mutated synchronously in the [T] enter
// handler — this command only persists, so callers can return immediately
// without blocking the UI on disk I/O.
func (m *PlayerModel) persistTagIndexCmd(sum string) tea.Cmd {
	ti := m.tagIndex
	return func() tea.Msg {
		err := SaveTagIndex(ti)
		return tagsSavedMsg{sum: sum, err: err}
	}
}

// the same file handle. Sum/tag failures are non-fatal — playback proceeds
// with sum="" and tags=nil, and the right-edge column simply stays empty.
func (m *PlayerModel) loadCurrentTrack() tea.Cmd {
	return func() tea.Msg {
		if m.currentIndex >= len(m.playlist) {
			return nil
		}

		track := m.playlist[m.currentIndex]

		// Compute Sum + look up Tags before opening the file for playback.
		// Failures here are non-fatal; we just play the track without tag
		// awareness this session.
		sum, _ := m.sumCache.LookupOrCompute(track)
		var tags []string
		if sum != "" {
			tags = m.tagIndex.Get(sum)
		}

		// Load the track
		if err := m.player.LoadTrack(track); err != nil {
			return playErrorMsg(fmt.Errorf("failed to load track: %w", err))
		}

		// Start playing
		if err := m.player.Play(); err != nil {
			return playErrorMsg(fmt.Errorf("failed to play track: %w", err))
		}

		return trackLoadedMsg{
			duration: m.player.GetDuration(),
			artist:   m.player.GetArtist(),
			title:    m.player.GetTitle(),
			album:    m.player.GetAlbum(),
			sum:      sum,
			tags:     tags,
		}
	}
}

// publishToNostr returns a Bubble Tea command that searches for listen links
// and publishes a public kind-1 Nostr post about the current track.
func (m *PlayerModel) publishToNostr(hexKey string) tea.Cmd {
	artist, title, album := m.artist, m.title, m.album
	return func() tea.Msg {
		err := PublishNostrTrackNote(hexKey, artist, title, album)
		return nostrPublishedMsg{action: "post", err: err}
	}
}

// saveEarmarkCmd returns a Bubble Tea command that earmarks the current track.
//
// Correct sequencing — chunk identities (SHA-256s) must be known before the
// earmark is published so the manifest is complete from the start:
//
//  1. Duplicate check (local queue)
//  2. Encrypt file into chunks — all SHA-256s now known, no network yet
//  3. Write earmark WITH manifest to local queue (data safety)
//  4. Upload encrypted chunks to Blossom servers — fills in server lists
//  5. Publish earmark WITH complete manifest to Nostr in one shot
//  6. Remove from local queue on success
//
// If offline at step 4/5, the earmark (with manifest) stays in the queue and
// will be synced the next time the app starts with connectivity.
func (m *PlayerModel) saveEarmarkCmd(hexKey string) tea.Cmd {
	artist, title, album := m.artist, m.title, m.album
	path := m.playlist[m.currentIndex]

	return func() tea.Msg {
		// Step 1: duplicate check before any work.
		existing, _ := LoadQueue()
		stub := Earmark{Path: path, Artist: artist, Title: title, Album: album}
		if isDuplicateEarmark(existing, stub) {
			return nostrPublishedMsg{action: "earmark", duplicate: true}
		}

		// Step 2: encrypt locally — gives us all chunk SHA-256s immediately.
		prepared, manifest, err := PrepareUpload(path)
		if err != nil {
			return nostrPublishedMsg{action: "earmark", err: fmt.Errorf("could not encrypt file: %w", err)}
		}

		// Step 3: persist earmark with manifest to local queue.
		e := Earmark{
			Artist:    artist,
			Album:     album,
			Title:     title,
			Path:      path,
			Timestamp: time.Now().Unix(),
			Blossom:   manifest,
		}
		if err := AppendToQueue(e); err != nil {
			return nostrPublishedMsg{action: "earmark", err: fmt.Errorf("could not save to local queue: %w", err)}
		}

		// Step 4: upload chunks — fills in confirmed server lists.
		servers, err := ResolveBlossomServers(hexKey)
		if err != nil || len(servers) == 0 {
			servers = LoadBlossomServers()
		}
		uploadCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := UploadPrepared(uploadCtx, hexKey, prepared, manifest, servers, nil); err != nil {
			// Upload failed — earmark is in queue without server lists.
			// It will be retried on next startup via FlushQueue.
			return nostrPublishedMsg{action: "earmark", queued: true}
		}

		// Step 5: publish earmark with complete manifest to Nostr.
		if err := AddEarmark(hexKey, e); err != nil {
			return nostrPublishedMsg{action: "earmark", queued: true}
		}

		// Step 6: all done — remove from outbox.
		_ = RemoveFromQueue(e)
		return nostrPublishedMsg{action: "earmark"}
	}
}

// saveKeyAndPublish validates the key the user typed inline, persists it, and
// then publishes a public Nostr post about the current track.
func (m *PlayerModel) saveKeyAndPublish(rawKey string) tea.Cmd {
	artist, title, album := m.artist, m.title, m.album
	return func() tea.Msg {
		rawKey = strings.TrimSpace(rawKey)
		if rawKey == "" {
			return nostrPublishedMsg{action: "post", err: fmt.Errorf("no key entered")}
		}

		hexKey, err := resolvePrivateKey(rawKey)
		if err != nil {
			return nostrPublishedMsg{action: "post", err: fmt.Errorf("invalid key: %w", err)}
		}

		cfg, loadErr := LoadConfig()
		if loadErr != nil {
			cfg = &Config{}
		}
		cfg.NostrPrivateKey = hexKey
		_ = SaveConfig(cfg)

		err = PublishNostrTrackNote(hexKey, artist, title, album)
		return nostrPublishedMsg{action: "post", err: err}
	}
}

// saveKeyAndAddEarmark validates the key the user typed inline, persists it,
// and then runs the same encrypt → upload → earmark sequence as saveEarmarkCmd.
func (m *PlayerModel) saveKeyAndAddEarmark(rawKey string) tea.Cmd {
	artist, title, album := m.artist, m.title, m.album
	path := m.playlist[m.currentIndex]
	return func() tea.Msg {
		rawKey = strings.TrimSpace(rawKey)
		if rawKey == "" {
			return nostrPublishedMsg{action: "earmark", err: fmt.Errorf("no key entered")}
		}

		hexKey, err := resolvePrivateKey(rawKey)
		if err != nil {
			return nostrPublishedMsg{action: "earmark", err: fmt.Errorf("invalid key: %w", err)}
		}

		cfg, loadErr := LoadConfig()
		if loadErr != nil {
			cfg = &Config{}
		}
		cfg.NostrPrivateKey = hexKey
		_ = SaveConfig(cfg)

		// Duplicate check.
		existingQ, _ := LoadQueue()
		stub := Earmark{Path: path, Artist: artist, Title: title, Album: album}
		if isDuplicateEarmark(existingQ, stub) {
			return nostrPublishedMsg{action: "earmark", duplicate: true}
		}

		// Encrypt → upload → earmark (same sequence as saveEarmarkCmd).
		prepared, manifest, err := PrepareUpload(path)
		if err != nil {
			return nostrPublishedMsg{action: "earmark", err: fmt.Errorf("could not encrypt file: %w", err)}
		}

		e := Earmark{
			Artist:    artist,
			Album:     album,
			Title:     title,
			Path:      path,
			Timestamp: time.Now().Unix(),
			Blossom:   manifest,
		}
		if err := AppendToQueue(e); err != nil {
			return nostrPublishedMsg{action: "earmark", err: fmt.Errorf("could not save to local queue: %w", err)}
		}

		servers, sErr := ResolveBlossomServers(hexKey)
		if sErr != nil || len(servers) == 0 {
			servers = LoadBlossomServers()
		}
		uploadCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := UploadPrepared(uploadCtx, hexKey, prepared, manifest, servers, nil); err != nil {
			return nostrPublishedMsg{action: "earmark", queued: true}
		}

		if err = AddEarmark(hexKey, e); err != nil {
			return nostrPublishedMsg{action: "earmark", queued: true}
		}

		_ = RemoveFromQueue(e)
		return nostrPublishedMsg{action: "earmark"}
	}
}

// flushQueueCmd returns a Bubble Tea command that attempts to publish any
// earmarks left in the local offline queue from previous sessions.
// It is run once at startup so earmarks queued while offline are synced as
// soon as the app starts with connectivity.
func (m *PlayerModel) flushQueueCmd() tea.Cmd {
	return func() tea.Msg {
		cfg, err := LoadConfig()
		if err != nil || cfg.NostrPrivateKey == "" {
			return queueFlushedMsg{}
		}
		count, _ := FlushQueue(cfg.NostrPrivateKey)
		return queueFlushedMsg{count: count}
	}
}

// cleanupCmd runs at startup to purge earmarks older than EarmarkMaxAge (30
// days) and delete their Blossom chunks from all associated servers.
func (m *PlayerModel) cleanupCmd() tea.Cmd {
	return func() tea.Msg {
		cfg, err := LoadConfig()
		if err != nil || cfg.NostrPrivateKey == "" {
			return cleanupMsg{}
		}
		removed, _ := CleanupOldEarmarks(cfg.NostrPrivateKey)
		return cleanupMsg{removed: removed}
	}
}

// deleteCurrentTrack stops playback, removes the file from disk, removes it
// from the playlist, and signals the TUI to load the next track.
func (m *PlayerModel) deleteCurrentTrack() tea.Cmd {
	// Capture path and index before the goroutine runs
	filePath := m.playlist[m.currentIndex]
	idx := m.currentIndex

	// Stop playback immediately (synchronous)
	m.player.Stop()
	m.playing = false

	// Remove the entry from the playlist
	m.playlist = append(m.playlist[:idx], m.playlist[idx+1:]...)

	// Clamp index so it points to the next track (or wraps around)
	if len(m.playlist) > 0 && m.currentIndex >= len(m.playlist) {
		m.currentIndex = 0
	}

	return func() tea.Msg {
		if err := os.Remove(filePath); err != nil {
			return trackDeletedMsg{err: fmt.Errorf("delete failed: %w", err)}
		}
		return trackDeletedMsg{}
	}
}

// formatDuration formats a time.Duration as mm:ss
func formatDuration(d time.Duration) string {
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}
