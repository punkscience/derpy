package main

import (
	"strings"
	"testing"
	"time"
)

// TestRenderInlineBar verifies the compact progress bar renders the expected
// number of filled and unfilled cells for a range of done/total inputs.
func TestRenderInlineBar(t *testing.T) {
	cases := []struct {
		name           string
		done, total    int
		width          int
		wantFilled     int
		wantUnfilled   int
	}{
		{"empty", 0, 10, 10, 0, 10},
		{"half", 5, 10, 10, 5, 5},
		{"full", 10, 10, 10, 10, 0},
		{"over-cap", 12, 10, 10, 10, 0},
		{"negative-done", -3, 10, 10, 0, 10},
		{"uneven-ratio", 1, 3, 12, 4, 8}, // 1/3 * 12 = 4
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := renderInlineBar(c.done, c.total, c.width)
			if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
				t.Fatalf("bar missing brackets: %q", got)
			}
			filled := strings.Count(got, "█")
			unfilled := strings.Count(got, "─")
			if filled != c.wantFilled || unfilled != c.wantUnfilled {
				t.Errorf("bar = %q; filled=%d unfilled=%d; want filled=%d unfilled=%d",
					got, filled, unfilled, c.wantFilled, c.wantUnfilled)
			}
		})
	}
}

// TestRenderInlineBar_ZeroTotal checks that a zero-total input produces an
// empty bar without dividing by zero.
func TestRenderInlineBar_ZeroTotal(t *testing.T) {
	got := renderInlineBar(0, 0, 8)
	want := "[" + strings.Repeat("─", 8) + "]"
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// TestRenderEarmarkProgress verifies each phase produces the expected status
// string. The "uploading" phase must include the done/total counter.
func TestRenderEarmarkProgress(t *testing.T) {
	if got := renderEarmarkProgress(earmarkProgressMsg{phase: "encrypting"}); got != "Nostr: encrypting..." {
		t.Errorf("encrypting phase: got %q", got)
	}
	if got := renderEarmarkProgress(earmarkProgressMsg{phase: "publishing"}); got != "Nostr: publishing..." {
		t.Errorf("publishing phase: got %q", got)
	}

	got := renderEarmarkProgress(earmarkProgressMsg{phase: "uploading", chunksDone: 3, chunksTotal: 12})
	if !strings.HasPrefix(got, "Nostr: uploading ") {
		t.Errorf("uploading phase prefix wrong: %q", got)
	}
	if !strings.HasSuffix(got, " 3/12") {
		t.Errorf("uploading phase counter wrong: %q", got)
	}

	if got := renderEarmarkProgress(earmarkProgressMsg{phase: "unknown"}); got != "Nostr: working..." {
		t.Errorf("fallback phase: got %q", got)
	}
}

// TestUpdate_EarmarkProgressMsg verifies that feeding an earmarkProgressMsg
// into the model's Update method populates nostrStatus with the rendered
// progress string — the same line the user sees next to "Nostr:".
func TestUpdate_EarmarkProgressMsg(t *testing.T) {
	m := &PlayerModel{}
	updated, _ := m.Update(earmarkProgressMsg{phase: "uploading", chunksDone: 2, chunksTotal: 5})
	pm, ok := updated.(*PlayerModel)
	if !ok {
		t.Fatalf("Update did not return *PlayerModel, got %T", updated)
	}
	if !strings.Contains(pm.nostrStatus, "uploading") || !strings.Contains(pm.nostrStatus, "2/5") {
		t.Errorf("nostrStatus after uploading msg: %q", pm.nostrStatus)
	}

	updated, _ = pm.Update(earmarkProgressMsg{phase: "publishing"})
	pm = updated.(*PlayerModel)
	if pm.nostrStatus != "Nostr: publishing..." {
		t.Errorf("nostrStatus after publishing msg: %q", pm.nostrStatus)
	}
}

// TestSendProgress_NilProgramSafe ensures sendProgress is a no-op when the
// model has no program attached (the default in unit tests).
func TestSendProgress_NilProgramSafe(t *testing.T) {
	// Must not panic.
	sendProgress(nil, earmarkProgressMsg{phase: "encrypting"})
}

// TestNostrPublishedMsg_ClearsInFlight verifies that receiving an earmark
// completion (success, duplicate, queued, or error) clears the in-flight flag
// so the next [E] press is accepted.
func TestNostrPublishedMsg_ClearsInFlight(t *testing.T) {
	for _, msg := range []nostrPublishedMsg{
		{action: "earmark"},
		{action: "earmark", duplicate: true},
		{action: "earmark", queued: true},
		{action: "earmark", err: errStub("boom")},
	} {
		m := &PlayerModel{earmarkInFlight: true}
		updated, _ := m.Update(msg)
		if updated.(*PlayerModel).earmarkInFlight {
			t.Errorf("earmarkInFlight not cleared for %+v", msg)
		}
	}
}

// TestNostrPublishedMsg_PostDoesNotClearInFlight verifies that a public-post
// completion does not touch the earmark in-flight flag: the two flows are
// independent and [E] progress must remain protected.
func TestNostrPublishedMsg_PostDoesNotClearInFlight(t *testing.T) {
	m := &PlayerModel{earmarkInFlight: true}
	updated, _ := m.Update(nostrPublishedMsg{action: "post"})
	if !updated.(*PlayerModel).earmarkInFlight {
		t.Error("post completion should not clear earmark in-flight flag")
	}
}

// TestTrackLoadedMsg_ClearsStatusWhenIdle verifies that advancing to a new
// track wipes any stale Nostr status line — unless an earmark is still in
// flight, in which case the live progress line must survive.
func TestTrackLoadedMsg_ClearsStatusWhenIdle(t *testing.T) {
	m := &PlayerModel{
		playlist:     []string{"a.mp3"},
		player:       NewAudioPlayer(),
		tickInterval: 100 * time.Millisecond,
		nostrStatus:  "Nostr: earmark saved!",
	}
	updated, _ := m.Update(trackLoadedMsg{duration: time.Second, title: "x"})
	if updated.(*PlayerModel).nostrStatus != "" {
		t.Errorf("idle track change should clear status; got %q",
			updated.(*PlayerModel).nostrStatus)
	}
}

// TestTrackLoadedMsg_PreservesStatusWhileInFlight verifies that the live
// earmark progress line is preserved across track rollover so the user does
// not lose visibility during a long upload.
func TestTrackLoadedMsg_PreservesStatusWhileInFlight(t *testing.T) {
	m := &PlayerModel{
		playlist:        []string{"a.mp3"},
		player:          NewAudioPlayer(),
		tickInterval:    100 * time.Millisecond,
		nostrStatus:     "Nostr: uploading [██─────] 2/8",
		earmarkInFlight: true,
	}
	updated, _ := m.Update(trackLoadedMsg{duration: time.Second, title: "x"})
	if got := updated.(*PlayerModel).nostrStatus; got != "Nostr: uploading [██─────] 2/8" {
		t.Errorf("in-flight progress should survive track change; got %q", got)
	}
}

// errStub implements error for the in-flight-clear test cases.
type errStub string

func (e errStub) Error() string { return string(e) }
