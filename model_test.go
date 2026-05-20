package main

import (
	"reflect"
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
