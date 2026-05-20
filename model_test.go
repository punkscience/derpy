package main

import (
	"reflect"
	"strings"
	"testing"
	"time"
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
