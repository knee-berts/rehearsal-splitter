package main

import (
	"math"
	"math/cmplx"
)

// nextPow2 returns the smallest power of two that is >= n.
func nextPow2(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// fft computes an in-place radix-2 fast Fourier transform; len(a) must be a power of two.
// With invert set it computes the inverse transform, including the 1/n scaling.
func fft(a []complex128, invert bool) {
	n := len(a)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}

	roots := make([]complex128, n/2)
	for k := range roots {
		ang := 2 * math.Pi * float64(k) / float64(n)
		if invert {
			ang = -ang
		}
		roots[k] = complex(math.Cos(ang), math.Sin(ang))
	}
	for length := 2; length <= n; length <<= 1 {
		half, step := length/2, n/length
		for i := 0; i < n; i += length {
			for k := 0; k < half; k++ {
				u := a[i+k]
				v := a[i+k+half] * roots[k*step]
				a[i+k] = u + v
				a[i+k+half] = u - v
			}
		}
	}

	if invert {
		scale := complex(1/float64(n), 0)
		for i := range a {
			a[i] *= scale
		}
	}
}

// crossCorrelate returns r[k] = sum over t of a[t]*b[t+k] for lags k from -(len(a)-1) to
// len(b)-1, with lag k stored at index k+len(a)-1. A peak at lag k means b lags a by k
// samples. With phat set the cross spectrum is whitened (GCC-PHAT), which gives a sharp
// peak for broadband signals recorded through different microphones.
func crossCorrelate(a, b []float64, phat bool) []float64 {
	n := nextPow2(len(a) + len(b))
	fa := make([]complex128, n)
	fb := make([]complex128, n)
	for i, v := range a {
		fa[i] = complex(v, 0)
	}
	for i, v := range b {
		fb[i] = complex(v, 0)
	}
	fft(fa, false)
	fft(fb, false)
	for i := range fa {
		x := cmplx.Conj(fa[i]) * fb[i]
		if phat {
			if m := cmplx.Abs(x); m > 1e-12 {
				x /= complex(m, 0)
			} else {
				x = 0
			}
		}
		fa[i] = x
	}
	fft(fa, true)

	r := make([]float64, len(a)+len(b)-1)
	for k := -(len(a) - 1); k < len(b); k++ {
		idx := k
		if idx < 0 {
			idx += n
		}
		r[k+len(a)-1] = real(fa[idx])
	}
	return r
}

// peakLag searches lags [minLag, maxLag] of a crossCorrelate result (where lenA is len(a))
// for the highest value. It returns the lag refined to a fraction of a sample by parabolic
// interpolation, and the ratio of the peak to the highest value more than exclude lags
// away, which measures how unambiguous the match is.
func peakLag(r []float64, lenA, minLag, maxLag, exclude int) (lag, ratio float64) {
	lo := max(minLag+lenA-1, 0)
	hi := min(maxLag+lenA-1, len(r)-1)
	if lo > hi {
		return 0, 0
	}
	best := lo
	for i := lo; i <= hi; i++ {
		if r[i] > r[best] {
			best = i
		}
	}
	second := 0.0
	for i := lo; i <= hi; i++ {
		if (i < best-exclude || i > best+exclude) && r[i] > second {
			second = r[i]
		}
	}

	frac := 0.0
	if best > 0 && best < len(r)-1 {
		y0, y1, y2 := r[best-1], r[best], r[best+1]
		if den := y0 - 2*y1 + y2; den != 0 {
			frac = 0.5 * (y0 - y2) / den
		}
	}
	lag = float64(best-(lenA-1)) + frac
	if second <= 0 {
		return lag, math.Inf(1)
	}
	return lag, r[best] / second
}

// onsetEnvelope reduces audio to one onset-strength value per hop samples: the rises in log
// RMS level, normalized to zero mean and unit variance. Onsets line up across microphones
// and rooms far better than raw waveforms, which makes them good for a coarse search.
func onsetEnvelope(x []float32, hop int) []float64 {
	frames := len(x) / hop
	env := make([]float64, frames)
	prev := 0.0
	for f := 0; f < frames; f++ {
		sum := 0.0
		for _, v := range x[f*hop : (f+1)*hop] {
			sum += float64(v) * float64(v)
		}
		level := math.Log(math.Sqrt(sum/float64(hop)) + 1e-6)
		if f > 0 && level > prev {
			env[f] = level - prev
		}
		prev = level
	}
	normalize(env)
	return env
}

// normalize scales x in place to zero mean and unit variance.
func normalize(x []float64) {
	if len(x) == 0 {
		return
	}
	mean := 0.0
	for _, v := range x {
		mean += v
	}
	mean /= float64(len(x))
	variance := 0.0
	for _, v := range x {
		variance += (v - mean) * (v - mean)
	}
	std := math.Sqrt(variance/float64(len(x))) + 1e-12
	for i := range x {
		x[i] = (x[i] - mean) / std
	}
}

// fitLine returns the least-squares intercept and slope of y as a function of x.
func fitLine(x, y []float64) (intercept, slope float64) {
	n := float64(len(x))
	var sx, sy, sxx, sxy float64
	for i := range x {
		sx += x[i]
		sy += y[i]
		sxx += x[i] * x[i]
		sxy += x[i] * y[i]
	}
	den := n*sxx - sx*sx
	if n == 0 || den == 0 {
		if n == 0 {
			return 0, 0
		}
		return sy / n, 0
	}
	slope = (n*sxy - sx*sy) / den
	return (sy - slope*sx) / n, slope
}
