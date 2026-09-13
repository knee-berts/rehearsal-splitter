package main

import (
	"math"
	"math/rand"
	"testing"
)

const testRate = 8000

// makeMusic returns noise with sharp attacks at random intervals, which gives both the
// onset envelope and the waveform something to lock onto.
func makeMusic(rng *rand.Rand, seconds float64) []float32 {
	out := make([]float32, int(seconds*testRate))
	next, amp, since := 0, 0.0, 0
	for i := range out {
		if i == next {
			amp = 0.2 + 0.8*rng.Float64()
			since = 0
			next = i + int((0.1+0.4*rng.Float64())*testRate)
		}
		out[i] = float32(amp * math.Exp(-float64(since)/testRate*12) * rng.NormFloat64())
		since++
	}
	return out
}

// recordFrom simulates a second recorder: n samples of src starting at startSec, with its
// clock running driftPPM slower than the source's, plus its own noise.
func recordFrom(rng *rand.Rand, src []float32, startSec, driftPPM float64, n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		x := (startSec + float64(i)/testRate*(1+driftPPM*1e-6)) * testRate
		j := int(math.Floor(x))
		f := float32(x - float64(j))
		if j >= 0 && j+1 < len(src) {
			out[i] = src[j]*(1-f) + src[j+1]*f
		}
		out[i] += float32(0.05 * rng.NormFloat64())
	}
	return out
}

var fastSyncConfig = syncConfig{
	EnvRate:    100,
	WindowSec:  10,
	MarginSec:  0.5,
	Windows:    8,
	MinRatio:   2,
	MinQuality: 2.5,
	MaxResidMS: 3,
}

func TestSyncVideo(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	board := makeMusic(rng, 400)
	camera := recordFrom(rng, board, 123.456, -40, 200*testRate)

	got, err := syncVideo(board, camera, testRate, fastSyncConfig)
	if err != nil {
		t.Fatalf("syncVideo failed: %v", err)
	}
	if math.Abs(got.BoardOffset-123.456) > 0.001 {
		t.Errorf("Expected board offset 123.456s, got %.4fs", got.BoardOffset)
	}
	if math.Abs(got.DriftPPM+40) > 5 {
		t.Errorf("Expected drift -40 ppm, got %.1f ppm", got.DriftPPM)
	}
	if got.Windows < 6 {
		t.Errorf("Expected most windows to match, got %d", got.Windows)
	}
}

func TestSyncVideoRejectsUnrelatedAudio(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	board := makeMusic(rng, 300)
	camera := makeMusic(rng, 100)
	if _, err := syncVideo(board, camera, testRate, fastSyncConfig); err == nil {
		t.Error("Expected an error for recordings that do not match")
	}
}

func TestAlignMix(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	performance := makeMusic(rng, 200)
	camera := recordFrom(rng, performance, 20, 0, 120*testRate)

	for _, mixStart := range []float64{2.5, 0, -1.25} {
		// A positive mixStart is a bounce with extra audio before the video's first frame.
		mix := recordFrom(rng, performance, 20-mixStart, 0, 125*testRate)
		got, windows, err := alignMix(camera, mix, testRate, 30, fastSyncConfig)
		if err != nil {
			t.Fatalf("alignMix(%v) failed: %v", mixStart, err)
		}
		if math.Abs(got-mixStart) > 0.001 || windows == 0 {
			t.Errorf("Expected mix start %.3fs, got %.4fs from %d windows", mixStart, got, windows)
		}
	}
}

func TestFitOffsetsIgnoresOutliers(t *testing.T) {
	var times, offsets []float64
	for i := 0; i < 10; i++ {
		times = append(times, float64(i*300))
		offsets = append(offsets, 301+float64(i*300)*-9e-6)
	}
	offsets[3] += 0.03 // a window that locked onto the wrong beat
	intercept, drift, used, resid := fitOffsets(times, offsets, 3)
	if math.Abs(intercept-301) > 1e-6 || math.Abs(drift+9) > 0.01 || used != 9 || resid > 0.01 {
		t.Errorf("Got intercept %.6f, drift %.3f ppm, %d points, residual %.3f ms", intercept, drift, used, resid)
	}
}

func TestVideoSyncTimeConversion(t *testing.T) {
	v := videoSync{BoardOffset: 4478.5188, DriftPPM: -8.7}
	for _, sec := range []float64{0, 95.5, 3239.8} {
		if back := v.videoTime(v.boardTime(sec)); math.Abs(back-sec) > 1e-9 {
			t.Errorf("videoTime(boardTime(%v)) = %v", sec, back)
		}
	}
}
