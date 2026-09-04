//go:build windows

package main

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dece2183/media-winrt-go/windows/media"
)

func TestPlaybackStatusForState(t *testing.T) {
	cases := []struct {
		name    string
		playing bool
		paused  bool
		want    media.MediaPlaybackStatus
	}{
		{"stopped when not playing", false, false, media.MediaPlaybackStatusStopped},
		{"stopped when not playing even if paused flag set", false, true, media.MediaPlaybackStatusStopped},
		{"playing", true, false, media.MediaPlaybackStatusPlaying},
		{"paused", true, true, media.MediaPlaybackStatusPaused},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := playbackStatusForState(c.playing, c.paused); got != c.want {
				t.Errorf("playbackStatusForState(%v, %v) = %d, want %d", c.playing, c.paused, got, c.want)
			}
		})
	}
}

func TestMessageForButton(t *testing.T) {
	cases := []struct {
		btn    media.SystemMediaTransportControlsButton
		want   tea.Msg
		wantOK bool
	}{
		{media.SystemMediaTransportControlsButtonPlay, mprisPlayMsg{}, true},
		{media.SystemMediaTransportControlsButtonPause, mprisPauseMsg{}, true},
		{media.SystemMediaTransportControlsButtonNext, mprisNextMsg{}, true},
		{media.SystemMediaTransportControlsButtonPrevious, mprisPreviousMsg{}, true},
		{media.SystemMediaTransportControlsButtonStop, mprisStopMsg{}, true},
		{media.SystemMediaTransportControlsButtonRecord, nil, false},
		{media.SystemMediaTransportControlsButtonFastForward, nil, false},
		{media.SystemMediaTransportControlsButtonChannelUp, nil, false},
	}
	for _, c := range cases {
		msg, ok := messageForButton(c.btn)
		if ok != c.wantOK {
			t.Errorf("messageForButton(%d) ok = %v, want %v", c.btn, ok, c.wantOK)
			continue
		}
		if ok && msg != c.want {
			t.Errorf("messageForButton(%d) = %#v, want %#v", c.btn, msg, c.want)
		}
	}
}

func TestFileTitle(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`C:\music\Artist\Song Name.flac`, "Song Name"},
		{`C:\music\track.mp3`, "track"},
		{"noext", "noext"},
		{"", ""},
	}
	for _, c := range cases {
		if got := fileTitle(c.in); got != c.want {
			t.Errorf("fileTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDurationToTimeSpan(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want int64 // 100-ns ticks
	}{
		{0, 0},
		{-5 * time.Second, 0},      // clamps negatives to zero
		{time.Second, 10_000_000},  // 1s = 10,000,000 ticks
		{100 * time.Nanosecond, 1}, // one tick
		{3 * time.Minute, 1_800_000_000},
	}
	for _, c := range cases {
		if got := durationToTimeSpan(c.in); got.Duration != c.want {
			t.Errorf("durationToTimeSpan(%v).Duration = %d, want %d", c.in, got.Duration, c.want)
		}
	}
}
