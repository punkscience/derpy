package main

// mpris_types.go defines the Bubble Tea message types used to forward MPRIS2
// commands into the player model.  These types are shared across all platforms
// so that model.go can compile on Windows even though the D-Bus service itself
// is Linux-only.

import "time"

// mprisPlayMsg tells the model to start/resume playback.
type mprisPlayMsg struct{}

// mprisPauseMsg tells the model to pause playback.
type mprisPauseMsg struct{}

// mprisPlayPauseMsg tells the model to toggle play/pause.
type mprisPlayPauseMsg struct{}

// mprisStopMsg tells the model to stop playback.
type mprisStopMsg struct{}

// mprisNextMsg tells the model to advance to the next track.
type mprisNextMsg struct{}

// mprisPreviousMsg tells the model to go to the previous track.
type mprisPreviousMsg struct{}

// mprisSeekMsg tells the model to seek by a relative offset.
type mprisSeekMsg struct{ offset time.Duration }

// mprisSetPositionMsg tells the model to seek to an absolute position.
type mprisSetPositionMsg struct{ pos time.Duration }
