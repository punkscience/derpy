package main

import (
	"testing"

	"github.com/gopxl/beep"
)

// fixedStreamer emits a set number of stereo frames, then reports completion.
// It stands in for a decoded track of known length at a known sample rate.
type fixedStreamer struct {
	remaining int
}

func (s *fixedStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	if s.remaining <= 0 {
		return 0, false
	}
	n = len(samples)
	if n > s.remaining {
		n = s.remaining
	}
	for i := 0; i < n; i++ {
		samples[i][0] = 0.25
		samples[i][1] = 0.25
	}
	s.remaining -= n
	return n, true
}

func (s *fixedStreamer) Err() error { return nil }

// drain pulls every frame from s in fixed-size chunks and returns the total.
func drain(s beep.Streamer) int {
	buf := make([][2]float64, 512)
	total := 0
	for {
		n, ok := s.Stream(buf)
		total += n
		if !ok {
			return total
		}
	}
}

// A track already at the device rate must pass through untouched — no
// resampler inserted, frame count unchanged.
func TestResampleForDevicePassthrough(t *testing.T) {
	src := &fixedStreamer{remaining: 1000}

	got := resampleForDevice(src, deviceSampleRate)

	if beep.Streamer(src) != got {
		t.Fatalf("matching-rate track should not be wrapped in a resampler")
	}
	if frames := drain(got); frames != 1000 {
		t.Fatalf("passthrough changed frame count: got %d, want 1000", frames)
	}
}

// A track whose native rate is lower than the device rate is the chipmunk
// case: without resampling its samples play too fast. After resampleForDevice
// the output frame count must scale up to the device rate.
func TestResampleForDeviceUpsamples(t *testing.T) {
	const srcRate beep.SampleRate = 22050
	const inFrames = int(srcRate) // one second of audio

	out := drain(resampleForDevice(&fixedStreamer{remaining: inFrames}, srcRate))

	want := float64(deviceSampleRate) / float64(srcRate) // 2.0
	got := float64(out) / float64(inFrames)
	if diff := got - want; diff < -0.01 || diff > 0.01 {
		t.Fatalf("22050Hz track not resampled to %dHz: %d in -> %d out (ratio %.4f, want %.4f)",
			deviceSampleRate, inFrames, out, got, want)
	}
}

// A track whose native rate is higher than the device rate scales down to the
// device rate (otherwise it would play slightly slow/low).
func TestResampleForDeviceDownsamples(t *testing.T) {
	const srcRate beep.SampleRate = 48000
	const inFrames = int(srcRate) // one second of audio

	out := drain(resampleForDevice(&fixedStreamer{remaining: inFrames}, srcRate))

	want := float64(deviceSampleRate) / float64(srcRate)
	got := float64(out) / float64(inFrames)
	if diff := got - want; diff < -0.01 || diff > 0.01 {
		t.Fatalf("48000Hz track not resampled to %dHz: %d in -> %d out (ratio %.4f, want %.4f)",
			deviceSampleRate, inFrames, out, got, want)
	}
}
