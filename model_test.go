package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TestNewPlayerModelLoadsTagState verifies that NewPlayerModel wires up a
// non-nil TagIndex and SumCache so loadCurrentTrack can dereference them
// without panicking, even on a brand-new install with no on-disk state.
func TestNewPlayerModelLoadsTagState(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac", "b.flac"})
	if m.tagIndex == nil {
		t.Errorf("tagIndex is nil; NewPlayerModel should default to an empty index")
	}
	if m.sumCache == nil {
		t.Errorf("sumCache is nil; NewPlayerModel should default to an empty cache")
	}
}

// TestTrackLoadedMsgStashesSumAndTags verifies the Update handler for
// trackLoadedMsg copies the Sum and Tags onto the model so the Zen whisper
// row can render them on subsequent ticks.
func TestTrackLoadedMsgStashesSumAndTags(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})

	msg := trackLoadedMsg{
		duration: 3 * time.Minute,
		artist:   "Aphex Twin",
		title:    "Xtal",
		album:    "Selected Ambient Works 85-92",
		sum:      "deadbeefcafef00d",
		tags:     []string{"ambient", "idm"},
	}
	m.Update(msg)

	if m.currentSum != "deadbeefcafef00d" {
		t.Errorf("currentSum = %q, want deadbeefcafef00d", m.currentSum)
	}
	if !reflect.DeepEqual(m.currentTags, []string{"ambient", "idm"}) {
		t.Errorf("currentTags = %v, want [ambient idm]", m.currentTags)
	}
	if m.artist != "Aphex Twin" || m.title != "Xtal" {
		t.Errorf("metadata not applied: artist=%q title=%q", m.artist, m.title)
	}
}

// TestPlayErrorSkipsToNextTrack verifies a single load/play failure is
// non-fatal: the model logs it, advances to the next track, and returns a
// reload command instead of setting m.err (which would freeze the UI).
func TestPlayErrorSkipsToNextTrack(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir()) // keep the log out of the real home
	t.Setenv("HOME", t.TempDir())

	m := NewPlayerModel([]string{"a.mp3", "b.mp3", "c.mp3"})
	m.currentIndex = 0

	_, cmd := m.Update(playErrorMsg(fmt.Errorf("failed to load track %q: boom", "a.mp3")))

	if m.err != nil {
		t.Fatalf("m.err set on a single failure; skip should be non-fatal: %v", m.err)
	}
	if m.currentIndex != 1 {
		t.Errorf("currentIndex = %d, want 1 (advanced to next track)", m.currentIndex)
	}
	if m.loadFailures != 1 {
		t.Errorf("loadFailures = %d, want 1", m.loadFailures)
	}
	if cmd == nil {
		t.Errorf("expected a reload command to load the next track, got nil")
	}
}

// TestPlayErrorFatalAfterWholePlaylistFails verifies the runaway guard: once
// consecutive failures reach the playlist length (nothing plays at all), the
// model surfaces a fatal error rather than looping forever.
func TestPlayErrorFatalAfterWholePlaylistFails(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	m := NewPlayerModel([]string{"a.mp3", "b.mp3"})

	m.Update(playErrorMsg(fmt.Errorf("fail 1")))
	if m.err != nil {
		t.Fatalf("first failure should not be fatal: %v", m.err)
	}
	_, cmd := m.Update(playErrorMsg(fmt.Errorf("fail 2")))
	if m.err == nil {
		t.Fatalf("second failure (== playlist length) should be fatal")
	}
	if cmd != nil {
		t.Errorf("no reload command expected once fatal, got one")
	}
}

// TestTrackLoadedResetsFailureGuard verifies a successful load clears the
// consecutive-failure counter so a bad track earlier in the run doesn't push
// a later, unrelated failure over the fatal threshold.
func TestTrackLoadedResetsFailureGuard(t *testing.T) {
	m := NewPlayerModel([]string{"a.mp3", "b.mp3"})
	m.loadFailures = 1
	m.Update(trackLoadedMsg{title: "X", artist: "Y"})
	if m.loadFailures != 0 {
		t.Errorf("loadFailures = %d after a successful load, want 0", m.loadFailures)
	}
}

func TestRenderTagsColumnEmpty(t *testing.T) {
	if got := renderTagsColumn(nil); got != "" {
		t.Errorf("renderTagsColumn(nil) = %q, want \"\"", got)
	}
	if got := renderTagsColumn([]string{}); got != "" {
		t.Errorf("renderTagsColumn([]) = %q, want \"\"", got)
	}
}

func TestRenderTagsColumnContainsHeaderAndTags(t *testing.T) {
	got := renderTagsColumn([]string{"jazz", "piano"})
	if !strings.Contains(got, "tags") {
		t.Errorf("renderTagsColumn missing header: %q", got)
	}
	if !strings.Contains(got, "jazz") {
		t.Errorf("renderTagsColumn missing 'jazz': %q", got)
	}
	if !strings.Contains(got, "piano") {
		t.Errorf("renderTagsColumn missing 'piano': %q", got)
	}
}

func TestTruncateTag(t *testing.T) {
	tests := []struct {
		in    string
		width int
		want  string
	}{
		{"jazz", 20, "jazz"},
		{"this is a very long tag", 10, "this is a…"},
		{"exactly10c", 10, "exactly10c"},
		{"exactly11ch", 10, "exactly11…"},
		// Multi-byte safety even though NormalizeTags strips these — guards
		// against future relaxation of the rule.
		{"caféterrace", 5, "café…"},
	}
	for _, tt := range tests {
		got := truncateTag(tt.in, tt.width)
		if got != tt.want {
			t.Errorf("truncateTag(%q, %d) = %q, want %q", tt.in, tt.width, got, tt.want)
		}
	}
}

// TestViewShowsTagsInlineInWhisper verifies the Zen contract: the current
// Track's Tags fold into the centered position whisper ("1 / 1 · jazz ·
// piano") instead of a right-edge column, so no side 'tags' header exists.
func TestViewShowsTagsInlineInWhisper(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})
	m.Update(trackLoadedMsg{sum: "s1", tags: []string{"jazz", "piano"}, title: "X", artist: "Y"})
	m.width = 100

	out := m.View()
	if !strings.Contains(out, "1 / 1 · jazz · piano") {
		t.Errorf("tagged View should fold tags into the whisper row: %q", out)
	}
}

func TestViewHidesTagsWhenUntagged(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})
	m.Update(trackLoadedMsg{sum: "s1", tags: nil, title: "X", artist: "Y"})
	m.width = 100

	out := m.View()
	// The literal word "tags" should not appear because there's no header,
	// no tag content, and the existing single-column layout never mentions
	// the word.
	if strings.Contains(out, "tags") {
		t.Errorf("untagged View should not contain 'tags' header: %q", out)
	}
}

// TestViewKeepsTagsInlineWhenNarrow verifies Zen dropped the old two-column
// breakpoint: Tags stay in the whisper row at any terminal width.
func TestViewKeepsTagsInlineWhenNarrow(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})
	m.Update(trackLoadedMsg{sum: "s1", tags: []string{"jazz"}, title: "X", artist: "Y"})
	m.width = twoColumnMinWidth - 1 // just below the old threshold

	out := m.View()
	if !strings.Contains(out, "jazz") {
		t.Errorf("narrow View should keep tags inline in the whisper row: %q", out)
	}
}

// --- Slice 4: [T] keybinding + tag-entry overlay ---

// keyMsg builds a tea.KeyMsg for a single character or named key. Mirrors
// Bubble Tea's internal construction so msg.String() and msg.Runes both
// look right to the handler.
func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		runes := []rune(s)
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: runes}
	}
}

func TestPressTEntersTagEntryAndPrefillsBuffer(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})
	m.Update(trackLoadedMsg{sum: "sumA", tags: []string{"jazz", "piano"}})
	m.playing = true // trackLoadedMsg handler sets this, but be explicit

	if m.tagEntry {
		t.Fatalf("tagEntry should start false")
	}
	m.Update(keyMsg("t"))
	if !m.tagEntry {
		t.Errorf("expected tagEntry=true after [T]")
	}
	if m.tagBuffer != "jazz, piano" {
		t.Errorf("tagBuffer = %q, want \"jazz, piano\" (pre-filled with current Tags)", m.tagBuffer)
	}
}

func TestPressTIgnoredWhenNotPlaying(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})
	// playing stays false
	m.Update(keyMsg("t"))
	if m.tagEntry {
		t.Errorf("tagEntry entered while not playing")
	}
}

func TestPressTIgnoredWhenSumUnknown(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})
	// Simulate a load with sum="" (tag.Sum failed).
	m.Update(trackLoadedMsg{sum: "", tags: nil})
	m.playing = true
	m.Update(keyMsg("t"))
	if m.tagEntry {
		t.Errorf("tagEntry entered without a known Sum; cannot persist Tags")
	}
}

func TestTagEntryEscDiscards(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})
	m.Update(trackLoadedMsg{sum: "sumA", tags: []string{"jazz"}})
	m.playing = true
	m.Update(keyMsg("t"))
	if !m.tagEntry {
		t.Fatalf("setup: failed to enter tagEntry")
	}

	// Edit the buffer, then cancel.
	m.tagBuffer = "completely different"
	m.Update(keyMsg("esc"))

	if m.tagEntry {
		t.Errorf("tagEntry still active after esc")
	}
	if m.tagBuffer != "" {
		t.Errorf("tagBuffer not cleared after esc: %q", m.tagBuffer)
	}
	// Tags on disk should be untouched — currentTags still the originals.
	if !reflect.DeepEqual(m.currentTags, []string{"jazz"}) {
		t.Errorf("currentTags changed by esc: %v, want [jazz]", m.currentTags)
	}
}

func TestTagEntryEnterNormalizesAndUpdates(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})
	m.Update(trackLoadedMsg{sum: "sumA", tags: []string{"jazz"}})
	m.playing = true
	m.Update(keyMsg("t"))

	// Replace the pre-filled buffer with a messy input that exercises the
	// normalization rule.
	m.tagBuffer = "JAZZ, drum 'n' bass, 808 BASS, jazz"
	m.Update(keyMsg("enter"))

	if m.tagEntry {
		t.Errorf("tagEntry still active after enter")
	}
	want := []string{"jazz", "drum n bass", "808 bass"}
	if !reflect.DeepEqual(m.currentTags, want) {
		t.Errorf("currentTags after enter = %v, want %v", m.currentTags, want)
	}
	if !reflect.DeepEqual(m.tagIndex.Get("sumA"), want) {
		t.Errorf("tagIndex.Get(sumA) after enter = %v, want %v", m.tagIndex.Get("sumA"), want)
	}
}

func TestTagEntryBackspaceRemovesRune(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})
	m.Update(trackLoadedMsg{sum: "sumA", tags: nil})
	m.playing = true
	m.Update(keyMsg("t"))

	m.tagBuffer = "jazz"
	m.Update(keyMsg("backspace"))
	if m.tagBuffer != "jaz" {
		t.Errorf("after backspace tagBuffer = %q, want \"jaz\"", m.tagBuffer)
	}
}

func TestViewShowsTagEntryOverlay(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})
	m.Update(trackLoadedMsg{sum: "sumA", tags: []string{"jazz"}})
	m.playing = true
	m.width = 100
	m.Update(keyMsg("t"))
	m.tagBuffer = "dnb"

	out := m.View()
	if !strings.Contains(out, "tags for this track.") {
		t.Errorf("tag-entry overlay header missing: %q", out)
	}
	if !strings.Contains(out, "dnb") {
		t.Errorf("tag-entry overlay should echo buffer: %q", out)
	}
	// The current set stays visible ("now: jazz") while the buffer holds the
	// draft — you are editing what is there.
	if !strings.Contains(out, "jazz") {
		t.Errorf("right column should remain visible during tag entry: %q", out)
	}
}

// TestViewSpinnerRendersDotsWhenIndexing verifies that while an indexer sweep
// is in progress the View renders the PS three-dot motif and the N/M fraction
// instead of the old static "indexing N/M tracks…" label.
func TestViewSpinnerRendersDotsWhenIndexing(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})
	m.indexingTotal = 10
	m.indexingDone = 3
	m.spinnerFrame = 0
	m.width = 80

	out := m.View()

	if !strings.Contains(out, "●") {
		t.Errorf("spinner dot missing from View output: %q", out)
	}
	if !strings.Contains(out, "3/10") {
		t.Errorf("indexer fraction '3/10' missing from View output: %q", out)
	}
	if strings.Contains(out, "indexing") {
		t.Errorf("old static 'indexing' label should be gone; found it in: %q", out)
	}
}

// TestSpinnerFrameModuloWraps verifies that spinnerFrame wraps correctly
// after 3 increments so the animation loops cleanly.
func TestSpinnerFrameModuloWraps(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})
	m.indexingTotal = 10
	m.spinnerFrame = 2

	// Simulate one more indexProgressMsg arriving.
	m.Update(indexProgressMsg{done: 3, total: 10})

	if m.spinnerFrame != 0 {
		t.Errorf("spinnerFrame after wrapping from 2 = %d, want 0", m.spinnerFrame)
	}
}

// TestRenderControls verifies that [E] and [P] appear only when a key is available.
func TestRenderControls(t *testing.T) {
	tests := []struct {
		name   string
		hasKey bool
		want   string
		not    string
	}{
		{
			name:   "with key",
			hasKey: true,
			want:   "e earmark",
			not:    "",
		},
		{
			name:   "without key",
			hasKey: false,
			want:   "",
			not:    "e earmark  p post",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderControls(tt.hasKey, false)
			if tt.want != "" && !strings.Contains(got, tt.want) {
				t.Errorf("renderControls(%v) missing %q: %q", tt.hasKey, tt.want, got)
			}
			if tt.not != "" && strings.Contains(got, tt.not) {
				t.Errorf("renderControls(%v) should not contain %q: %q", tt.hasKey, tt.not, got)
			}
		})
	}
}

// TestViewControlsHideWhenNoKey verifies that renderControls(false, false)
// omits [E] and [P] while preserving all other controls.
func TestViewControlsHideWhenNoKey(t *testing.T) {
	got := renderControls(false, false)

	if strings.Contains(got, "e earmark") || strings.Contains(got, "p post") {
		t.Errorf("renderControls(false) should not contain [E]/[P]: %q", got)
	}
	if !strings.Contains(got, "← prev") || !strings.Contains(got, "esc quit") {
		t.Errorf("renderControls(false) missing basic controls: %q", got)
	}
}

// TestTrackLoadedMsgEmptyTagsOK verifies that an untagged track produces
// nil currentTags rather than carrying stale tags from a previous track.
func TestTrackLoadedMsgEmptyTagsOK(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac", "b.flac"})

	// Simulate first track being tagged.
	m.Update(trackLoadedMsg{sum: "sumA", tags: []string{"jazz"}})
	if m.currentSum != "sumA" || !reflect.DeepEqual(m.currentTags, []string{"jazz"}) {
		t.Fatalf("setup mismatch: sum=%q tags=%v", m.currentSum, m.currentTags)
	}

	// Now an untagged second track.
	m.Update(trackLoadedMsg{sum: "sumB", tags: nil})
	if m.currentSum != "sumB" {
		t.Errorf("currentSum = %q, want sumB", m.currentSum)
	}
	if m.currentTags != nil {
		t.Errorf("currentTags = %v, want nil (untagged tracks should not carry stale Tags)", m.currentTags)
	}
}

// TestRenderSocialStatus verifies the combined post-status messages.
func TestRenderSocialStatus(t *testing.T) {
	tests := []struct {
		name string
		msg  socialPublishMsg
		want string
	}{
		{
			name: "both succeed",
			msg:  socialPublishMsg{nostrOK: true, bskyOK: true},
			want: "Posted to Bluesky + Nostr!",
		},
		{
			name: "nostr only succeeds",
			msg:  socialPublishMsg{nostrOK: true},
			want: "Nostr: posted!",
		},
		{
			name: "bsky only succeeds",
			msg:  socialPublishMsg{bskyOK: true},
			want: "Bluesky: posted!",
		},
		{
			name: "nostr fails bsky succeeds",
			msg: socialPublishMsg{
				nostrErr: fmt.Errorf("timeout"),
				bskyOK:   true,
			},
			want: "Nostr: failed — timeout  Bluesky: posted!",
		},
		{
			name: "neither attempted",
			msg:  socialPublishMsg{},
			want: "Post: nothing to do",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderSocialStatus(tt.msg)
			if got != tt.want {
				t.Errorf("renderSocialStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRenderControlsDualPlatform verifies controls display with Bluesky-only.
func TestRenderControlsDualPlatform(t *testing.T) {
	// Bluesky-only: shows [P] but not [E].
	got := renderControls(false, true)
	if !strings.Contains(got, "p post") {
		t.Errorf("renderControls(false, true) should contain [P]: %q", got)
	}
	if strings.Contains(got, "e earmark") {
		t.Errorf("renderControls(false, true) should not contain [E]: %q", got)
	}

	// Both: shows both [E] and [P].
	got = renderControls(true, true)
	if !strings.Contains(got, "e earmark") || !strings.Contains(got, "p post") {
		t.Errorf("renderControls(true, true) should contain [E] and [P]: %q", got)
	}

	// Neither: hides both.
	got = renderControls(false, false)
	if strings.Contains(got, "e earmark") || strings.Contains(got, "p post") {
		t.Errorf("renderControls(false, false) should hide [E]/[P]: %q", got)
	}
}
