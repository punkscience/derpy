package main

// mpris.go implements the MPRIS2 D-Bus interface for dirplay.
//
// MPRIS2 spec: https://specifications.freedesktop.org/mpris-spec/latest/
//
// Two D-Bus interfaces are exposed on the object path /org/mpris/MediaPlayer2:
//   - org.mpris.MediaPlayer2          (identity / capabilities)
//   - org.mpris.MediaPlayer2.Player   (playback control and state)
//
// Commands from external clients (playerctl, Waybar, etc.) are forwarded into
// the Bubble Tea event loop via tea.Program.Send(), so they are handled safely
// on the same goroutine as all other model updates.

import (
	"fmt"
	"log"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

// ---- MPRIS message types injected into the Bubble Tea event loop ------------

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

// ---- D-Bus introspection XML ------------------------------------------------

const mprisIntrospectXML = `
<node>
  <interface name="org.mpris.MediaPlayer2">
    <property name="CanQuit"             type="b"  access="read"/>
    <property name="CanRaise"            type="b"  access="read"/>
    <property name="HasTrackList"        type="b"  access="read"/>
    <property name="Identity"            type="s"  access="read"/>
    <property name="SupportedMimeTypes"  type="as" access="read"/>
    <property name="SupportedUriSchemes" type="as" access="read"/>
  </interface>
  <interface name="org.mpris.MediaPlayer2.Player">
    <property name="PlaybackStatus" type="s"     access="read"/>
    <property name="LoopStatus"     type="s"     access="readwrite"/>
    <property name="Rate"           type="d"     access="readwrite"/>
    <property name="Shuffle"        type="b"     access="readwrite"/>
    <property name="Metadata"       type="a{sv}" access="read"/>
    <property name="Volume"         type="d"     access="readwrite"/>
    <property name="Position"       type="x"     access="read"/>
    <property name="MinimumRate"    type="d"     access="read"/>
    <property name="MaximumRate"    type="d"     access="read"/>
    <property name="CanGoNext"      type="b"     access="read"/>
    <property name="CanGoPrevious"  type="b"     access="read"/>
    <property name="CanPlay"        type="b"     access="read"/>
    <property name="CanPause"       type="b"     access="read"/>
    <property name="CanSeek"        type="b"     access="read"/>
    <property name="CanControl"     type="b"     access="read"/>
    <method name="Next"/>
    <method name="Previous"/>
    <method name="Pause"/>
    <method name="PlayPause"/>
    <method name="Stop"/>
    <method name="Play"/>
    <method name="Seek">
      <arg direction="in"  type="x" name="Offset"/>
    </method>
    <method name="SetPosition">
      <arg direction="in"  type="o" name="TrackId"/>
      <arg direction="in"  type="x" name="Position"/>
    </method>
    <method name="OpenUri">
      <arg direction="in"  type="s" name="Uri"/>
    </method>
    <signal name="Seeked">
      <arg type="x" name="Position"/>
    </signal>
  </interface>
  ` + introspect.IntrospectDataString + `
</node>`

// ---- MPRISService -----------------------------------------------------------

// MPRISService registers dirplay on the session D-Bus and handles MPRIS2
// method calls by forwarding them as messages into the Bubble Tea program.
type MPRISService struct {
	conn    *dbus.Conn
	model   *PlayerModel
	program *tea.Program
}

// NewMPRISService creates and registers the MPRIS2 service.  If the session
// D-Bus is unavailable it returns (nil, nil) so the caller can continue
// without MPRIS support.
func NewMPRISService(model *PlayerModel, program *tea.Program) (*MPRISService, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		log.Printf("MPRIS: could not connect to session D-Bus, continuing without MPRIS support: %v", err)
		return nil, nil //nolint:nilerr // graceful degradation
	}

	svc := &MPRISService{
		conn:    conn,
		model:   model,
		program: program,
	}

	// Request the well-known service name.
	reply, err := conn.RequestName(
		"org.mpris.MediaPlayer2.dirplay",
		dbus.NameFlagDoNotQueue,
	)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("MPRIS: RequestName failed: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		return nil, fmt.Errorf("MPRIS: name already taken (reply=%d)", reply)
	}

	// Export this object for method calls.  godbus dispatches incoming D-Bus
	// method calls to matching exported interfaces; we implement both the
	// org.freedesktop.DBus.Properties interface (for property reads) and the
	// actual MPRIS2 player interface (for playback methods) on the same type.
	objectPath := dbus.ObjectPath("/org/mpris/MediaPlayer2")

	// org.mpris.MediaPlayer2 — identity/capability properties (no methods need renaming)
	conn.Export(svc, objectPath, "org.mpris.MediaPlayer2")

	// org.mpris.MediaPlayer2.Player — playback control methods.
	// We map the D-Bus "Seek" method to the Go method "SeekByOffset" to avoid
	// a false-positive from go vet's stdmethods check, which expects any method
	// named "Seek" to match the io.Seeker signature.
	playerMethodMap := map[string]string{
		"Seek": "SeekByOffset",
	}
	conn.ExportWithMap(svc, playerMethodMap, objectPath, "org.mpris.MediaPlayer2.Player")

	conn.Export(svc, objectPath, "org.freedesktop.DBus.Properties")

	// Export introspection so that tools like d-feet and playerctl can
	// discover the interfaces without trial-and-error.
	conn.Export(introspect.Introspectable(mprisIntrospectXML), objectPath,
		"org.freedesktop.DBus.Introspectable")

	return svc, nil
}

// Close releases the D-Bus connection.
func (s *MPRISService) Close() {
	if s != nil && s.conn != nil {
		s.conn.Close()
	}
}

// ---- org.freedesktop.DBus.Properties ----------------------------------------
//
// playerctl and most MPRIS clients use the standard Properties interface rather
// than per-interface property accessors, so we implement Get/GetAll/Set here.

// Get returns a single property value.  iface is one of the two MPRIS2
// interface names; prop is the property name.
func (s *MPRISService) Get(iface, prop string) (dbus.Variant, *dbus.Error) {
	props := s.allProperties(iface)
	if props == nil {
		return dbus.Variant{}, dbus.NewError(
			"org.freedesktop.DBus.Error.UnknownInterface",
			[]interface{}{fmt.Sprintf("unknown interface %q", iface)},
		)
	}
	v, ok := props[prop]
	if !ok {
		return dbus.Variant{}, dbus.NewError(
			"org.freedesktop.DBus.Error.InvalidArgs",
			[]interface{}{fmt.Sprintf("unknown property %q on %q", prop, iface)},
		)
	}
	return v, nil
}

// GetAll returns all properties for the given interface as a map.
func (s *MPRISService) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	props := s.allProperties(iface)
	if props == nil {
		return nil, dbus.NewError(
			"org.freedesktop.DBus.Error.UnknownInterface",
			[]interface{}{fmt.Sprintf("unknown interface %q", iface)},
		)
	}
	return props, nil
}

// Set is a no-op for read-only properties; writable properties (Rate, Volume,
// LoopStatus, Shuffle) are accepted silently so clients do not error.
func (s *MPRISService) Set(iface, prop string, value dbus.Variant) *dbus.Error {
	// All writable properties are acknowledged but not applied — dirplay does
	// not yet support runtime rate/volume/loop changes via MPRIS.
	return nil
}

// allProperties builds the property map for the given interface by reading
// current model state.  Returns nil for unknown interfaces.
func (s *MPRISService) allProperties(iface string) map[string]dbus.Variant {
	switch iface {
	case "org.mpris.MediaPlayer2":
		return map[string]dbus.Variant{
			"CanQuit":             dbus.MakeVariant(false),
			"CanRaise":            dbus.MakeVariant(false),
			"HasTrackList":        dbus.MakeVariant(false),
			"Identity":            dbus.MakeVariant("dirplay"),
			"SupportedMimeTypes":  dbus.MakeVariant([]string{"audio/mpeg", "audio/flac", "audio/ogg", "audio/wav"}),
			"SupportedUriSchemes": dbus.MakeVariant([]string{"file"}),
		}
	case "org.mpris.MediaPlayer2.Player":
		return map[string]dbus.Variant{
			"PlaybackStatus": dbus.MakeVariant(s.playbackStatus()),
			"LoopStatus":     dbus.MakeVariant("None"),
			"Rate":           dbus.MakeVariant(float64(1.0)),
			"Shuffle":        dbus.MakeVariant(false),
			"Metadata":       dbus.MakeVariant(s.metadata()),
			"Volume":         dbus.MakeVariant(float64(1.0)),
			"Position":       dbus.MakeVariant(s.positionMicros()),
			"MinimumRate":    dbus.MakeVariant(float64(1.0)),
			"MaximumRate":    dbus.MakeVariant(float64(1.0)),
			"CanGoNext":      dbus.MakeVariant(true),
			"CanGoPrevious":  dbus.MakeVariant(true),
			"CanPlay":        dbus.MakeVariant(true),
			"CanPause":       dbus.MakeVariant(true),
			"CanSeek":        dbus.MakeVariant(true),
			"CanControl":     dbus.MakeVariant(true),
		}
	}
	return nil
}

// ---- org.mpris.MediaPlayer2 -------------------------------------------------

// Raise is a no-op (terminal UI has no window to raise).
func (s *MPRISService) Raise() *dbus.Error { return nil }

// Quit is a no-op; dirplay is controlled via the TUI keyboard.
func (s *MPRISService) Quit() *dbus.Error { return nil }

// ---- org.mpris.MediaPlayer2.Player methods ----------------------------------

// Play starts playback.
func (s *MPRISService) Play() *dbus.Error {
	s.program.Send(mprisPlayMsg{})
	return nil
}

// Pause pauses playback.
func (s *MPRISService) Pause() *dbus.Error {
	s.program.Send(mprisPauseMsg{})
	return nil
}

// PlayPause toggles play/pause.
func (s *MPRISService) PlayPause() *dbus.Error {
	s.program.Send(mprisPlayPauseMsg{})
	return nil
}

// Stop stops playback.
func (s *MPRISService) Stop() *dbus.Error {
	s.program.Send(mprisStopMsg{})
	return nil
}

// Next advances to the next track.
func (s *MPRISService) Next() *dbus.Error {
	s.program.Send(mprisNextMsg{})
	return nil
}

// Previous goes to the previous track.
func (s *MPRISService) Previous() *dbus.Error {
	s.program.Send(mprisPreviousMsg{})
	return nil
}

// SeekByOffset moves the playback position by offsetMicros microseconds relative
// to the current position.  A negative offset seeks backward.
//
// This method is exported to D-Bus under the name "Seek" via ExportWithMap to
// avoid a go vet stdmethods false positive (go vet expects any Go method named
// "Seek" to match the io.Seeker signature).
func (s *MPRISService) SeekByOffset(offsetMicros int64) *dbus.Error {
	offset := time.Duration(offsetMicros) * time.Microsecond
	s.program.Send(mprisSeekMsg{offset: offset})
	return nil
}

// SetPosition seeks to an absolute position (in microseconds).
// trackId is the D-Bus object path of the track; if it does not match the
// currently playing track the seek is ignored per the MPRIS2 spec.
func (s *MPRISService) SetPosition(trackId dbus.ObjectPath, positionMicros int64) *dbus.Error {
	// Validate that the trackId matches the currently playing track.
	expected := dbus.ObjectPath(fmt.Sprintf("/org/dirplay/track/%d", s.model.currentIndex))
	if trackId != expected {
		// Per spec: "If the TrackId argument is not the same as the current
		// TrackId, this request should be ignored."
		return nil
	}
	pos := time.Duration(positionMicros) * time.Microsecond
	s.program.Send(mprisSetPositionMsg{pos: pos})
	return nil
}

// OpenUri is a no-op; dirplay plays from a directory, not individual URIs.
func (s *MPRISService) OpenUri(uri string) *dbus.Error { return nil }

// ---- State change notifications ---------------------------------------------

// NotifyStateChanged emits a PropertiesChanged signal on the D-Bus so that
// clients (Waybar, playerctl, etc.) pick up the new status and metadata
// immediately without polling.
//
// Call this from the Bubble Tea Update() method after any state transition
// (track load, pause, resume, stop, next, previous).
func (s *MPRISService) NotifyStateChanged() {
	if s == nil || s.conn == nil {
		return
	}

	changedProps := map[string]dbus.Variant{
		"PlaybackStatus": dbus.MakeVariant(s.playbackStatus()),
		"Metadata":       dbus.MakeVariant(s.metadata()),
	}

	err := s.conn.Emit(
		"/org/mpris/MediaPlayer2",
		"org.freedesktop.DBus.Properties.PropertiesChanged",
		"org.mpris.MediaPlayer2.Player",
		changedProps,
		[]string{}, // invalidated properties — none
	)
	if err != nil {
		log.Printf("MPRIS: failed to emit PropertiesChanged: %v", err)
	}
}

// EmitSeeked emits the Seeked signal with the current position.  Call this
// after a seek operation completes.
func (s *MPRISService) EmitSeeked() {
	if s == nil || s.conn == nil {
		return
	}
	err := s.conn.Emit(
		"/org/mpris/MediaPlayer2",
		"org.mpris.MediaPlayer2.Player.Seeked",
		s.positionMicros(),
	)
	if err != nil {
		log.Printf("MPRIS: failed to emit Seeked: %v", err)
	}
}

// ---- Helpers ----------------------------------------------------------------

// playbackStatus returns the MPRIS2 PlaybackStatus string for the current
// model state.
func (s *MPRISService) playbackStatus() string {
	if !s.model.playing {
		return "Stopped"
	}
	if s.model.paused {
		return "Paused"
	}
	return "Playing"
}

// positionMicros returns the current playback position in microseconds, as
// required by the MPRIS2 spec.
func (s *MPRISService) positionMicros() int64 {
	return s.model.position.Microseconds()
}

// metadata builds the MPRIS2 Metadata map from the current model state.
// The xesam:artist field must be a []string per the MPRIS2 spec.
func (s *MPRISService) metadata() map[string]dbus.Variant {
	trackId := dbus.ObjectPath(fmt.Sprintf("/org/dirplay/track/%d", s.model.currentIndex))

	artist := s.model.artist
	if artist == "" {
		artist = "Unknown Artist"
	}
	title := s.model.title
	if title == "" {
		// Fall back to the bare filename without extension.
		if s.model.currentIndex < len(s.model.playlist) {
			title = basenameWithoutExt(s.model.playlist[s.model.currentIndex])
		}
	}
	album := s.model.album

	return map[string]dbus.Variant{
		"mpris:trackid": dbus.MakeVariant(trackId),
		"mpris:length":  dbus.MakeVariant(s.model.duration.Microseconds()),
		"xesam:title":   dbus.MakeVariant(title),
		"xesam:artist":  dbus.MakeVariant([]string{artist}),
		"xesam:album":   dbus.MakeVariant(album),
	}
}

// basenameWithoutExt returns the filename component of a path, with its
// extension stripped.
func basenameWithoutExt(path string) string {
	// Walk backward to the last slash.
	idx := strings.LastIndexByte(path, '/')
	base := path[idx+1:]
	// Strip the extension.
	if dot := strings.LastIndexByte(base, '.'); dot > 0 {
		base = base[:dot]
	}
	return base
}
