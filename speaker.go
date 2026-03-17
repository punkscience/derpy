package main

// speaker.go provides a PulseAudio-based audio output backend.
// This replaces gopxl/beep/speaker (which requires libasound2-dev ALSA headers)
// with a pure-Go PulseAudio client that works on any standard Debian desktop
// running PulseAudio or PipeWire (with PulseAudio compatibility layer).

import (
	"fmt"
	"sync"

	"github.com/gopxl/beep"
	"github.com/jfreymuth/pulse"
)

var (
	spkMu     sync.Mutex
	spkBuf    [][2]float64
	spkPlayer beep.Streamer
	spkClient *pulse.Client
	spkStream *pulse.PlaybackStream
)

// speakerInit connects to PulseAudio and starts a continuous audio output stream.
// It is safe to call only once per application lifecycle; subsequent calls are
// guarded by the speakerInitialized flag in AudioPlayer.
func speakerInit(sampleRate beep.SampleRate, bufferSize int) error {
	// Allocate the conversion buffer before starting the stream so the
	// callback goroutine sees a valid slice immediately after Start().
	spkBuf = make([][2]float64, bufferSize)

	var err error
	spkClient, err = pulse.NewClient(
		pulse.ClientApplicationName("dirplay"),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to PulseAudio: %w", err)
	}

	spkStream, err = spkClient.NewPlayback(
		pulse.Float32Reader(func(out []float32) (int, error) {
			// This callback runs on a goroutine owned by the pulse library.
			// spkMu serialises access to spkBuf and spkPlayer with the main thread.
			spkMu.Lock()
			defer spkMu.Unlock()

			numFrames := len(out) / 2
			if numFrames > len(spkBuf) {
				spkBuf = make([][2]float64, numFrames)
			}

			if spkPlayer == nil {
				// No active player — output silence so the stream stays open.
				for i := range out {
					out[i] = 0
				}
				return len(out), nil
			}

			n, ok := spkPlayer.Stream(spkBuf[:numFrames])
			if !ok {
				spkPlayer = nil
			}

			// Convert float64 stereo to interleaved float32 (L, R, L, R, …).
			for i := 0; i < n; i++ {
				out[i*2] = float32(spkBuf[i][0])
				out[i*2+1] = float32(spkBuf[i][1])
			}
			// Fill any remaining frames with silence.
			for i := n * 2; i < len(out); i++ {
				out[i] = 0
			}
			return len(out), nil
		}),
		pulse.PlaybackStereo,
		pulse.PlaybackSampleRate(int(sampleRate)),
		pulse.PlaybackLatency(0.1),
	)
	if err != nil {
		spkClient.Close()
		spkClient = nil
		return fmt.Errorf("failed to create PulseAudio playback stream: %w", err)
	}

	spkStream.Start()
	return nil
}

// speakerPlay sets the active streamer. The pulse callback will begin pulling
// samples from s on the next audio buffer fill.
func speakerPlay(s beep.Streamer) {
	spkMu.Lock()
	defer spkMu.Unlock()
	spkPlayer = s
}

// speakerClear stops audio output by removing the active streamer.
// The stream itself stays open and outputs silence until the next speakerPlay call.
func speakerClear() {
	spkMu.Lock()
	defer spkMu.Unlock()
	spkPlayer = nil
}

// speakerLock acquires the speaker mutex, blocking the audio callback from
// running. Use this to make atomic state changes (e.g. toggling pause) that
// must not race with sample delivery.
func speakerLock() {
	spkMu.Lock()
}

// speakerUnlock releases the speaker mutex.
func speakerUnlock() {
	spkMu.Unlock()
}
