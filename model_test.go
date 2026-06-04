package main

import (
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
// trackLoadedMsg copies the Sum and Tags onto the model so the right-edge
// column (slice 3) can render them on subsequent ticks.
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

// TestViewIncludesTagsWhenWideAndTagged verifies the right-edge column
// shows up when (a) the current Track has Tags and (b) the terminal is at
// or above twoColumnMinWidth.
func TestViewIncludesTagsWhenWideAndTagged(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})
	m.Update(trackLoadedMsg{sum: "s1", tags: []string{"jazz", "piano"}, title: "X", artist: "Y"})
	m.width = 100

	out := m.View()
	if !strings.Contains(out, "jazz") {
		t.Errorf("wide+tagged View missing 'jazz': %q", out)
	}
	if !strings.Contains(out, "tags") {
		t.Errorf("wide+tagged View missing 'tags' header: %q", out)
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

func TestViewHidesTagsWhenTerminalNarrow(t *testing.T) {
	m := NewPlayerModel([]string{"a.flac"})
	m.Update(trackLoadedMsg{sum: "s1", tags: []string{"jazz"}, title: "X", artist: "Y"})
	m.width = twoColumnMinWidth - 1 // just below threshold

	out := m.View()
	if strings.Contains(out, "jazz") {
		t.Errorf("narrow View should drop right column; saw 'jazz' in %q", out)
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
	// Right column stays visible during tag entry — verify the existing
	// "jazz" tag is still rendered alongside the overlay.
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
