package main

import (
	"fmt"
	"math"
	"sort"
)

// videoSync places a set video on the board recording's timeline.
type videoSync struct {
	File          string  `json:"file"`
	Duration      float64 `json:"duration"`
	BoardOffset   float64 `json:"board_offset"`    // board time at the video's first frame
	DriftPPM      float64 `json:"drift_ppm"`       // how much faster the board clock runs
	Windows       int     `json:"matched_windows"` // audio windows that agreed on the fit
	MaxResidualMS float64 `json:"max_residual_ms"`
}

// showSync is saved next to the cue sheet so later runs can skip the analysis.
type showSync struct {
	BoardDuration float64     `json:"board_duration"`
	Videos        []videoSync `json:"videos"`
	Notes         []string    `json:"notes,omitempty"` // song boundaries worth checking by hand
}

// boardTime converts a time in the video to the board recording's timeline.
func (v videoSync) boardTime(videoSec float64) float64 {
	return v.BoardOffset + videoSec*(1+v.DriftPPM*1e-6)
}

// videoTime converts a board time to the video's timeline.
func (v videoSync) videoTime(boardSec float64) float64 {
	return (boardSec - v.BoardOffset) / (1 + v.DriftPPM*1e-6)
}

// syncConfig tunes how two recordings are lined up.
type syncConfig struct {
	EnvRate    int     // onset envelope frames per second for the coarse search
	WindowSec  float64 // length of each fine alignment window
	MarginSec  float64 // how far a fine window may move away from the coarse offset
	Windows    int     // fine windows spread across the recording
	MinRatio   float64 // coarse peak ratio needed to accept a match at all
	MinQuality float64 // fine peak ratio a window needs to be trusted
	MaxResidMS float64 // windows further than this from the fitted line are ignored
}

var defaultSyncConfig = syncConfig{
	EnvRate:    100,
	WindowSec:  30,
	MarginSec:  1,
	Windows:    24,
	MinRatio:   2,
	MinQuality: 2.5,
	MaxResidMS: 3,
}

// syncVideo finds where a camera recording sits inside the board recording. Both are mono
// samples at rate, filtered the same way. A coarse search correlates onset envelopes over
// the whole recording; GCC-PHAT windows then measure the offset to a fraction of a
// millisecond at many points, and a line through them gives the offset and clock drift.
func syncVideo(board, camera []float32, rate int, cfg syncConfig) (videoSync, error) {
	dur := float64(len(camera)) / float64(rate)
	coarse, err := coarseOffset(board, camera, rate, cfg, math.Inf(1))
	if err != nil {
		return videoSync{}, err
	}

	var times, offsets []float64
	for _, p := range windowStarts(dur, cfg) {
		if offset, quality, ok := alignWindow(board, camera, rate, p, coarse, cfg); ok && quality >= cfg.MinQuality {
			times = append(times, p)
			offsets = append(offsets, offset)
		}
	}
	result := videoSync{Duration: dur, BoardOffset: coarse}
	if len(times) == 0 {
		return result, nil
	}
	result.BoardOffset, result.DriftPPM, result.Windows, result.MaxResidualMS = fitOffsets(times, offsets, cfg.MaxResidMS)
	return result, nil
}

// alignMix finds the time in a mixdown that lines up with the start of a song's video,
// searching up to maxShift seconds either way. It returns that time and how many of the
// fine windows agreed with it.
func alignMix(camera, mix []float32, rate int, maxShift float64, cfg syncConfig) (mixStart float64, windows int, err error) {
	coarse, err := coarseOffset(mix, camera, rate, cfg, maxShift)
	if err != nil {
		return 0, 0, err
	}
	dur := float64(len(camera)) / float64(rate)
	cfg.WindowSec = math.Min(cfg.WindowSec, dur/3)
	cfg.Windows = 5
	var offsets []float64
	for _, p := range windowStarts(dur, cfg) {
		if offset, quality, ok := alignWindow(mix, camera, rate, p, coarse, cfg); ok && quality >= cfg.MinQuality {
			offsets = append(offsets, offset)
		}
	}
	if len(offsets) == 0 {
		return coarse, 0, nil
	}
	return median(offsets), len(offsets), nil
}

// coarseOffset correlates onset envelopes to find the offset, in seconds, at which
// long[t+offset] matches short[t], limited to offsets within maxShift seconds.
func coarseOffset(long, short []float32, rate int, cfg syncConfig, maxShift float64) (float64, error) {
	hop := rate / cfg.EnvRate
	longEnv := onsetEnvelope(long, hop)
	shortEnv := onsetEnvelope(short, hop)
	if len(longEnv) == 0 || len(shortEnv) == 0 {
		return 0, fmt.Errorf("recording is too short to line up")
	}
	minLag, maxLag := -len(shortEnv)+1, len(longEnv)-1
	if !math.IsInf(maxShift, 1) {
		limit := int(maxShift * float64(cfg.EnvRate))
		minLag, maxLag = max(minLag, -limit), min(maxLag, limit)
	}
	r := crossCorrelate(shortEnv, longEnv, false)
	lag, ratio := peakLag(r, len(shortEnv), minLag, maxLag, cfg.EnvRate)
	if ratio < cfg.MinRatio {
		return 0, fmt.Errorf("no clear match between the recordings (peak ratio %.2f)", ratio)
	}
	return lag / float64(cfg.EnvRate), nil
}

// windowStarts spreads cfg.Windows window start times across a recording of dur seconds,
// keeping away from the very start and end where one recording often runs alone.
func windowStarts(dur float64, cfg syncConfig) []float64 {
	first, last := cfg.WindowSec, dur-2*cfg.WindowSec
	if last < first {
		first, last = 0, math.Max(dur-cfg.WindowSec, 0)
	}
	n := max(cfg.Windows, 1)
	starts := make([]float64, n)
	for i := range starts {
		starts[i] = first
		if n > 1 {
			starts[i] += (last - first) * float64(i) / float64(n-1)
		}
	}
	return starts
}

// alignWindow measures the offset of the short-recording window starting at sec, searching
// cfg.MarginSec around the coarse offset. It returns the offset (long time minus short
// time) and the peak ratio.
func alignWindow(long, short []float32, rate int, sec, coarse float64, cfg syncConfig) (offset, quality float64, ok bool) {
	w := int(cfg.WindowSec * float64(rate))
	m := int(cfg.MarginSec * float64(rate))
	ss := int(sec * float64(rate))
	ls := int(math.Round((sec+coarse)*float64(rate))) - m
	if w <= 0 || ss < 0 || ss+w > len(short) || ls < 0 || ls+w+2*m > len(long) {
		return 0, 0, false
	}
	a := toFloat64(short[ss : ss+w])
	b := toFloat64(long[ls : ls+w+2*m])
	lag, quality := peakLag(crossCorrelate(a, b, true), len(a), 0, 2*m, rate/100)
	return (float64(ls-ss) + lag) / float64(rate), quality, true
}

// fitOffsets fits offset = intercept + slope*time robustly: a Theil-Sen estimate first, then
// least squares over the points within maxResidMS of it. It returns the intercept, the
// slope in parts per million, the points used, and their largest residual in milliseconds.
func fitOffsets(times, offsets []float64, maxResidMS float64) (intercept, driftPPM float64, used int, maxResid float64) {
	if len(times) < 3 {
		return median(offsets), 0, len(offsets), spreadMS(offsets)
	}
	var slopes []float64
	for i := range times {
		for j := i + 1; j < len(times); j++ {
			if dt := times[j] - times[i]; dt != 0 {
				slopes = append(slopes, (offsets[j]-offsets[i])/dt)
			}
		}
	}
	slope := median(slopes)
	rest := make([]float64, len(times))
	for i := range times {
		rest[i] = offsets[i] - slope*times[i]
	}
	intercept = median(rest)

	var keptT, keptO []float64
	for i := range times {
		if math.Abs(offsets[i]-(intercept+slope*times[i]))*1000 <= maxResidMS {
			keptT = append(keptT, times[i])
			keptO = append(keptO, offsets[i])
		}
	}
	if len(keptT) >= 2 {
		intercept, slope = fitLine(keptT, keptO)
	}
	for i := range keptT {
		maxResid = math.Max(maxResid, math.Abs(keptO[i]-(intercept+slope*keptT[i]))*1000)
	}
	return intercept, slope * 1e6, len(keptT), maxResid
}

func median(x []float64) float64 {
	if len(x) == 0 {
		return 0
	}
	s := append([]float64(nil), x...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// spreadMS returns the largest distance of any value from the median, in milliseconds.
func spreadMS(x []float64) float64 {
	m, spread := median(x), 0.0
	for _, v := range x {
		spread = math.Max(spread, math.Abs(v-m)*1000)
	}
	return spread
}

func toFloat64(x []float32) []float64 {
	out := make([]float64, len(x))
	for i, v := range x {
		out[i] = float64(v)
	}
	return out
}
