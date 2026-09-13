package main

import (
	"math"
	"math/cmplx"
	"math/rand"
	"testing"
)

func noise(rng *rand.Rand, n int) []float64 {
	x := make([]float64, n)
	for i := range x {
		x[i] = rng.NormFloat64()
	}
	return x
}

func TestFFTRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	original := make([]complex128, 64)
	for i := range original {
		original[i] = complex(rng.NormFloat64(), rng.NormFloat64())
	}
	a := append([]complex128(nil), original...)
	fft(a, false)
	fft(a, true)
	for i := range a {
		if cmplx.Abs(a[i]-original[i]) > 1e-9 {
			t.Fatalf("Round trip differs at %d: %v vs %v", i, a[i], original[i])
		}
	}
}

func TestCrossCorrelateFindsDelay(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	signal := noise(rng, 6000)

	testCases := []struct {
		name  string
		delay int // b lags a by this many samples
		phat  bool
	}{
		{"PositiveDelay", 700, false},
		{"PositiveDelayPHAT", 700, true},
		{"NegativeDelay", -300, false},
		{"NegativeDelayPHAT", -300, true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			a := signal[1000:4000]
			// b[t] = a[t-delay], so a[t] lines up with b[t+delay].
			b := append([]float64(nil), signal[1000-tc.delay:4000-tc.delay+500]...)
			for i, v := range noise(rng, len(b)) {
				b[i] += 0.3 * v
			}
			r := crossCorrelate(a, b, tc.phat)
			lag, ratio := peakLag(r, len(a), -len(a)+1, len(b)-1, 5)
			if math.Abs(lag-float64(tc.delay)) > 0.5 {
				t.Errorf("Expected lag %d, got %.2f", tc.delay, lag)
			}
			if ratio < 2 {
				t.Errorf("Expected a clear peak, got ratio %.2f", ratio)
			}
		})
	}
}

func TestPeakLagInterpolates(t *testing.T) {
	const lenA = 5
	r := make([]float64, 40)
	for i := range r {
		k := float64(i - (lenA - 1))
		r[i] = 100 - (k-10.25)*(k-10.25)
	}
	lag, _ := peakLag(r, lenA, -4, 35, 3)
	if math.Abs(lag-10.25) > 1e-6 {
		t.Errorf("Expected interpolated lag 10.25, got %f", lag)
	}
}

func TestPeakLagRespectsRange(t *testing.T) {
	r := []float64{0, 9, 0, 0, 5, 0, 0}
	lag, _ := peakLag(r, 1, 3, 6, 1)
	if math.Round(lag) != 4 {
		t.Errorf("Expected the peak inside the lag range at 4, got %f", lag)
	}
}

func TestOnsetEnvelope(t *testing.T) {
	x := make([]float32, 1000)
	for i := 500; i < 1000; i++ {
		x[i] = 0.5
	}
	env := onsetEnvelope(x, 100)
	if len(env) != 10 {
		t.Fatalf("Expected 10 frames, got %d", len(env))
	}
	best := 0
	for i := range env {
		if env[i] > env[best] {
			best = i
		}
	}
	if best != 5 {
		t.Errorf("Expected the onset at frame 5, got %d", best)
	}
}

func TestFitLine(t *testing.T) {
	x := []float64{0, 1, 2, 3, 4}
	y := []float64{3, 3.5, 4, 4.5, 5}
	intercept, slope := fitLine(x, y)
	if math.Abs(intercept-3) > 1e-9 || math.Abs(slope-0.5) > 1e-9 {
		t.Errorf("Expected intercept 3 and slope 0.5, got %f and %f", intercept, slope)
	}
}
