//go:build windows

package main

// mpris_windows.go implements the Windows equivalent of the Linux MPRIS2
// service: the System Media Transport Controls (SMTC). It gives derpy a
// presence in the Windows media UI (the Quick Settings media card, the lock
// screen, and the hardware media keys):
//
//   - State -> OS: current title/artist/album, playback status, and timeline
//     (position/duration) are pushed to SMTC from NotifyStateChanged/EmitSeeked.
//   - OS -> app: the media card's and media keys' Play/Pause/Next/Previous/Stop
//     buttons are forwarded into the Bubble Tea event loop as the same
//     mprisXxxMsg messages the Linux MPRIS path uses (see mpris_types.go), so
//     model.go handles them identically on every platform.
//
// The public API (NewMPRISService/Close/NotifyStateChanged/EmitSeeked) matches
// the Linux implementation in mpris.go exactly, so model.go and main.go need no
// platform-specific code.
//
// SMTC is a WinRT API. Rather than the console-window + GetForWindow interop
// dance (which also needs a Win32 message pump), we let a WinRT MediaPlayer own
// the SMTC: Windows.Media.Playback.MediaPlayer activates and owns an SMTC
// without any window, and works for unpackaged Win32/console apps. We never
// give the MediaPlayer a source or call Play on it — derpy's own beep pipeline
// still does all the audio; the MediaPlayer exists only as the SMTC host.
//
// Threading: WinRT calls are serialised onto a single OS-locked goroutine
// initialised into the multithreaded apartment (MTA). Callers (the Bubble Tea
// goroutine) hand it work over a channel. Button-pressed events are delivered
// by the WinRT runtime on its own MTA thread-pool threads (no message pump
// required) and only read the button enum before forwarding a message.

import (
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	tea "github.com/charmbracelet/bubbletea"
	winrt "github.com/dece2183/media-winrt-go"
	"github.com/dece2183/media-winrt-go/windows/foundation"
	"github.com/dece2183/media-winrt-go/windows/media"
	"github.com/dece2183/media-winrt-go/windows/media/playback"
	"github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// roInitMultiThreaded is RO_INIT_MULTITHREADED — join the process MTA so WinRT
// event callbacks are delivered on thread-pool threads without a message pump.
const roInitMultiThreaded = 1

// App identity for the media UI. Unpackaged apps resolve the media card's app
// name from an AppUserModelID registered under HKCU\...\AppUserModelId; without
// it Windows shows "Unknown app".
const (
	appUserModelID = "PunkScience.Derpy"
	appDisplayName = "Derpy"
)

// WinRT interface IIDs used for setting music metadata directly. media-winrt-go
// exposes the display updater but not its music-properties accessors, so those
// two interfaces are driven through their vtables (layout verified against the
// system Windows.Media metadata).
const (
	iidDisplayUpdater          = "8abbc53e-fa55-4ecf-ad8e-c984e5dd1550"
	iidMusicDisplayProperties  = "6bbf0c59-d0a0-4d26-92a0-f978e1d18e7b"
	iidMusicDisplayProperties2 = "00368462-97d3-44b9-b00f-008afcefaf18"
)

// buttonPressedHandlerIID is the parameterised IID for
// TypedEventHandler<SystemMediaTransportControls, ...ButtonPressedEventArgs>.
var buttonPressedHandlerIID = winrt.ParameterizedInstanceGUID(
	foundation.GUIDTypedEventHandler,
	media.SignatureSystemMediaTransportControls,
	media.SignatureSystemMediaTransportControlsButtonPressedEventArgs,
)

// displayUpdaterVtbl mirrors ISystemMediaTransportControlsDisplayUpdater's
// vtable (from the media-winrt-go binding). Only SetType and GetMusicProperties
// are called here; the rest keep the slot offsets correct.
type displayUpdaterVtbl struct {
	ole.IInspectableVtbl
	GetType            uintptr
	SetType            uintptr
	GetAppMediaId      uintptr
	SetAppMediaId      uintptr
	GetThumbnail       uintptr
	SetThumbnail       uintptr
	GetMusicProperties uintptr
	GetVideoProperties uintptr
	GetImageProperties uintptr
	CopyFromFileAsync  uintptr
	ClearAll           uintptr
	Update             uintptr
}

// musicPropsVtbl mirrors IMusicDisplayProperties.
type musicPropsVtbl struct {
	ole.IInspectableVtbl
	GetTitle       uintptr
	PutTitle       uintptr
	GetAlbumArtist uintptr
	PutAlbumArtist uintptr
	GetArtist      uintptr
	PutArtist      uintptr
}

// musicProps2Vtbl mirrors IMusicDisplayProperties2 (adds AlbumTitle).
type musicProps2Vtbl struct {
	ole.IInspectableVtbl
	GetAlbumTitle  uintptr
	PutAlbumTitle  uintptr
	GetTrackNumber uintptr
	PutTrackNumber uintptr
	GetGenres      uintptr
}

// smtcSnapshot is a point-in-time copy of the model state the WinRT worker
// needs. It is built on the Bubble Tea goroutine (single writer) and consumed
// on the worker goroutine, so the two never touch shared PlayerModel fields
// concurrently.
type smtcSnapshot struct {
	playing  bool
	paused   bool
	path     string
	title    string
	artist   string
	album    string
	position time.Duration
	duration time.Duration
}

// MPRISService drives the Windows SMTC. All WinRT objects below are owned by
// the worker goroutine and must only be touched there.
type MPRISService struct {
	program *tea.Program

	cmds   chan func()   // work handed to the worker goroutine
	closed chan struct{} // closed by Close() to stop the worker
	once   sync.Once     // guards close(closed)
	ok     bool          // true once SMTC is live; false = inert stub

	// Worker-goroutine-only fields.
	player     *playback.MediaPlayer
	smtc       *media.SystemMediaTransportControls
	updater    *media.SystemMediaTransportControlsDisplayUpdater
	btnHandler *foundation.TypedEventHandler
	lastPath   string // last path pushed to the display updater (dedup)
}

// NewMPRISService starts the SMTC worker and waits for it to acquire the
// controls. If anything fails (unsupported Windows, WinRT unavailable) it logs
// and returns an inert service so derpy keeps playing with no media overlay —
// exactly the graceful-degradation contract the Linux path provides.
func NewMPRISService(program *tea.Program) (*MPRISService, error) {
	s := &MPRISService{
		program: program,
		cmds:    make(chan func(), 16),
		closed:  make(chan struct{}),
	}

	ready := make(chan error, 1)
	go s.worker(ready)

	if err := <-ready; err != nil {
		log.Printf("SMTC: media controls unavailable, continuing without them: %v", err)
		return &MPRISService{}, nil // inert stub (ok == false)
	}
	s.ok = true
	return s, nil
}

// worker owns the WinRT apartment and every SMTC object. It runs on a single
// locked OS thread for the life of the service.
func (s *MPRISService) worker(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Set the app identity before the SMTC session is created so the media card
	// shows "Derpy" rather than "Unknown app".
	setAppIdentity()

	if err := ole.RoInitialize(roInitMultiThreaded); err != nil {
		ready <- fmt.Errorf("RoInitialize: %w", err)
		return
	}

	if err := s.initSMTC(); err != nil {
		ready <- err
		return
	}
	ready <- nil

	for {
		select {
		case fn := <-s.cmds:
			fn()
		case <-s.closed:
			s.teardown()
			return
		}
	}
}

// setAppIdentity registers a friendly display name for derpy's AppUserModelID
// and binds the current process to it. Both steps are best-effort.
func setAppIdentity() {
	if k, _, err := registry.CreateKey(
		registry.CURRENT_USER,
		`SOFTWARE\Classes\AppUserModelId\`+appUserModelID,
		registry.SET_VALUE,
	); err == nil {
		_ = k.SetStringValue("DisplayName", appDisplayName)
		_ = k.Close()
	}

	if p, err := windows.UTF16PtrFromString(appUserModelID); err == nil {
		proc := windows.NewLazySystemDLL("shell32.dll").NewProc("SetCurrentProcessExplicitAppUserModelID")
		_, _, _ = proc.Call(uintptr(unsafe.Pointer(p)))
	}
}

// initSMTC activates a MediaPlayer, borrows its SMTC, enables the transport
// buttons, and subscribes to button presses. Runs on the worker goroutine.
func (s *MPRISService) initSMTC() error {
	player, err := playback.NewMediaPlayer()
	if err != nil {
		return fmt.Errorf("NewMediaPlayer: %w", err)
	}
	s.player = player

	// Disable the MediaPlayer's command manager so SMTC button presses are
	// delivered to our ButtonPressed handler rather than auto-handled.
	if cm, err := player.GetCommandManager(); err == nil {
		_ = cm.SetIsEnabled(false)
	}

	smtc, err := player.GetSystemMediaTransportControls()
	if err != nil {
		return fmt.Errorf("GetSystemMediaTransportControls: %w", err)
	}
	s.smtc = smtc

	if err := smtc.SetIsEnabled(true); err != nil {
		return fmt.Errorf("SetIsEnabled: %w", err)
	}
	// Enable the buttons derpy can act on. (SetIsStopEnabled isn't exposed by
	// the binding; the card has no Stop button anyway, though a media key or
	// external controller may still send Stop — handled in onButtonPressed.)
	_ = smtc.SetIsPlayEnabled(true)
	_ = smtc.SetIsPauseEnabled(true)
	_ = smtc.SetIsNextEnabled(true)
	_ = smtc.SetIsPreviousEnabled(true)

	updater, err := smtc.GetDisplayUpdater()
	if err != nil {
		return fmt.Errorf("GetDisplayUpdater: %w", err)
	}
	s.updater = updater

	s.btnHandler = foundation.NewTypedEventHandler(ole.NewGUID(buttonPressedHandlerIID), s.onButtonPressed)
	if _, err := smtc.AddButtonPressed(s.btnHandler); err != nil {
		return fmt.Errorf("AddButtonPressed: %w", err)
	}
	return nil
}

// onButtonPressed runs on a WinRT thread-pool thread. It reads which button was
// pressed and forwards the matching message into the Bubble Tea loop, where the
// existing mprisXxxMsg handlers act on it.
func (s *MPRISService) onButtonPressed(_ *foundation.TypedEventHandler, _ unsafe.Pointer, argsPtr unsafe.Pointer) {
	if argsPtr == nil {
		return
	}
	args := (*media.SystemMediaTransportControlsButtonPressedEventArgs)(argsPtr)
	btn, err := args.GetButton()
	if err != nil {
		return
	}
	if msg, ok := messageForButton(btn); ok {
		s.program.Send(msg)
	}
}

// messageForButton maps an SMTC transport button to the mprisXxxMsg the model
// handles. The bool is false for buttons derpy does not act on.
func messageForButton(btn media.SystemMediaTransportControlsButton) (tea.Msg, bool) {
	switch btn {
	case media.SystemMediaTransportControlsButtonPlay:
		return mprisPlayMsg{}, true
	case media.SystemMediaTransportControlsButtonPause:
		return mprisPauseMsg{}, true
	case media.SystemMediaTransportControlsButtonNext:
		return mprisNextMsg{}, true
	case media.SystemMediaTransportControlsButtonPrevious:
		return mprisPreviousMsg{}, true
	case media.SystemMediaTransportControlsButtonStop:
		return mprisStopMsg{}, true
	default:
		return nil, false
	}
}

// playbackStatusForState maps derpy's playing/paused flags to the SMTC status.
func playbackStatusForState(playing, paused bool) media.MediaPlaybackStatus {
	if !playing {
		return media.MediaPlaybackStatusStopped
	}
	if paused {
		return media.MediaPlaybackStatusPaused
	}
	return media.MediaPlaybackStatusPlaying
}

// NotifyStateChanged pushes the current playback status, timeline, and (on
// track change) metadata to SMTC. Safe to call on the Bubble Tea goroutine.
func (s *MPRISService) NotifyStateChanged(m *PlayerModel) { s.post(m) }

// EmitSeeked refreshes SMTC after a seek. SMTC has no dedicated seek signal;
// pushing the updated timeline is the equivalent, so it shares post with
// NotifyStateChanged.
func (s *MPRISService) EmitSeeked(m *PlayerModel) { s.post(m) }

// post snapshots the model and hands an update closure to the worker. Updates
// are best-effort: if the worker is busy the snapshot is dropped, since a newer
// one will follow.
func (s *MPRISService) post(m *PlayerModel) {
	if s == nil || !s.ok {
		return
	}
	snap := smtcSnapshot{
		playing:  m.playing,
		paused:   m.paused,
		title:    m.title,
		artist:   m.artist,
		album:    m.album,
		position: m.position,
		duration: m.duration,
	}
	if m.currentIndex < len(m.playlist) {
		snap.path = m.playlist[m.currentIndex]
	}
	// Fall back to the filename when the track has no title tag, matching the
	// TUI, so the media card never shows a blank (or the app name).
	if strings.TrimSpace(snap.title) == "" {
		snap.title = fileTitle(snap.path)
	}
	// s.cmds is never closed, so this send can never panic; default drops the
	// update when the buffer is full or the worker has stopped.
	select {
	case s.cmds <- func() { s.applyState(snap) }:
	default:
	}
}

// applyState pushes a snapshot to SMTC. Runs on the worker goroutine.
func (s *MPRISService) applyState(snap smtcSnapshot) {
	if s.smtc == nil {
		return
	}

	_ = s.smtc.SetPlaybackStatus(playbackStatusForState(snap.playing, snap.paused))

	if tl, err := media.NewSystemMediaTransportControlsTimelineProperties(); err == nil {
		_ = tl.SetStartTime(foundation.TimeSpan{Duration: 0})
		_ = tl.SetEndTime(durationToTimeSpan(snap.duration))
		_ = tl.SetMinSeekTime(foundation.TimeSpan{Duration: 0})
		_ = tl.SetMaxSeekTime(durationToTimeSpan(snap.duration))
		_ = tl.SetPosition(durationToTimeSpan(snap.position))
		_ = s.smtc.UpdateTimelineProperties(tl)
		tl.Release()
	}

	// Metadata only needs refreshing when the track actually changes, not on
	// every pause/resume/seek.
	if snap.path != "" && snap.path != s.lastPath {
		s.lastPath = snap.path
		if err := s.setMusicMetadata(snap.title, snap.artist, snap.album); err != nil {
			log.Printf("SMTC: metadata update failed: %v", err)
		}
	}
}

// setMusicMetadata sets the display updater's music title/artist/album from
// derpy's own metadata and pushes the update. Runs on the worker goroutine.
// media-winrt-go doesn't expose the music-properties accessors, so they are
// driven through their vtables.
func (s *MPRISService) setMusicMetadata(title, artist, album string) error {
	du, err := s.updater.QueryInterface(ole.NewGUID(iidDisplayUpdater))
	if err != nil {
		return fmt.Errorf("QueryInterface display updater: %w", err)
	}
	defer du.Release()
	duVtbl := (*displayUpdaterVtbl)(unsafe.Pointer(du.RawVTable))

	// Music layout so the title/artist/album fields are shown.
	_, _, _ = syscall.SyscallN(duVtbl.SetType, uintptr(unsafe.Pointer(du)), uintptr(media.MediaPlaybackTypeMusic))

	var mp *ole.IInspectable
	if hr, _, _ := syscall.SyscallN(
		duVtbl.GetMusicProperties,
		uintptr(unsafe.Pointer(du)),
		uintptr(unsafe.Pointer(&mp)),
	); hr != 0 || mp == nil {
		return fmt.Errorf("GetMusicProperties hr=0x%x", hr)
	}
	defer mp.Release()

	mpVtbl := (*musicPropsVtbl)(unsafe.Pointer(mp.RawVTable))
	putHString(mpVtbl.PutTitle, unsafe.Pointer(mp), title)
	putHString(mpVtbl.PutArtist, unsafe.Pointer(mp), artist)

	if mp2, err := mp.QueryInterface(ole.NewGUID(iidMusicDisplayProperties2)); err == nil {
		mp2Vtbl := (*musicProps2Vtbl)(unsafe.Pointer(mp2.RawVTable))
		putHString(mp2Vtbl.PutAlbumTitle, unsafe.Pointer(mp2), album)
		mp2.Release()
	}

	return s.updater.Update()
}

// putHString calls a WinRT put_(HSTRING) vtable slot with s. A fresh HSTRING is
// created and released around the call (WinRT copies it internally).
func putHString(fn uintptr, this unsafe.Pointer, s string) {
	h, err := ole.NewHString(s)
	if err != nil {
		return
	}
	defer ole.DeleteHString(h)
	_, _, _ = syscall.SyscallN(fn, uintptr(this), uintptr(h))
}

// fileTitle returns the filename (without extension) for use as a title when a
// track has no title tag.
func fileTitle(path string) string {
	if path == "" {
		return ""
	}
	base := filepath.Base(path)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

// durationToTimeSpan converts a Go duration to a WinRT TimeSpan (100-ns ticks).
func durationToTimeSpan(d time.Duration) foundation.TimeSpan {
	if d < 0 {
		d = 0
	}
	return foundation.TimeSpan{Duration: int64(d / 100)}
}

// Close stops the worker, which disables SMTC and releases the MediaPlayer.
func (s *MPRISService) Close() {
	if s == nil || !s.ok {
		return
	}
	s.once.Do(func() { close(s.closed) })
}

// teardown disables SMTC and closes the MediaPlayer. Runs on the worker
// goroutine as it exits.
func (s *MPRISService) teardown() {
	if s.smtc != nil {
		_ = s.smtc.SetIsEnabled(false)
	}
	if s.player != nil {
		_ = s.player.Close()
	}
}
