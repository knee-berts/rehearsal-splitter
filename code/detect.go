package main

import (
	"math"
	"sort"
)

// detectConfig tunes song boundary detection.
type detectConfig struct {
	FrameSec       float64 // seconds per level frame
	QuietDropDB    float64 // frames this far below the loud level (90th percentile) are quiet
	MinGapSec      float64 // shortest quiet run that can separate two songs
	MinSongSec     float64 // shorter loud stretches (tuning, noodling) are not songs on their own
	AttachGapSec   float64 // a shorter loud stretch this close to a song belongs to it
	SplitWinSec    float64 // window used to score split points
	SplitMarginSec float64 // a split must leave at least this much song on both sides
	MaxStopDB      float64 // cap on how much one channel's drop adds to the stop score
}

var defaultDetectConfig = detectConfig{
	FrameSec:       0.5,
	QuietDropDB:    20,
	MinGapSec:      3,
	MinSongSec:     60,
	AttachGapSec:   10,
	SplitWinSec:    2,
	SplitMarginSec: 120,
	MaxStopDB:      40,
}

// frameLevelsDB returns the combined RMS level in dBFS of the chosen channels of interleaved
// samples, one value per frame of hop samples.
func frameLevelsDB(samples []float32, channels int, use []int, hop int) []float64 {
	frames := len(samples) / channels / hop
	levels := make([]float64, frames)
	for f := 0; f < frames; f++ {
		sum := 0.0
		base := f * hop * channels
		for s := 0; s < hop; s++ {
			row := samples[base+s*channels : base+(s+1)*channels]
			for _, c := range use {
				v := float64(row[c])
				sum += v * v
			}
		}
		levels[f] = 10 * math.Log10(sum/float64(hop)+1e-12)
	}
	return levels
}

// stopScores returns, for each frame of hop samples, how far the chosen channels have dropped
// below their own typical playing level (90th percentile), capped at maxDrop dB per channel
// and averaged over the channels. The score is high only when most instruments stop at
// once, as they do between songs; quiet passages inside a song usually keep someone playing.
func stopScores(samples []float32, channels int, use []int, hop int, maxDrop float64) []float64 {
	scores := make([]float64, len(samples)/channels/hop)
	if len(use) == 0 {
		return scores
	}
	for _, c := range use {
		levels := frameLevelsDB(samples, channels, []int{c}, hop)
		typical := percentile(levels, 90)
		for f, v := range levels {
			scores[f] += math.Min(math.Max(typical-v, 0), maxDrop)
		}
	}
	for f := range scores {
		scores[f] /= float64(len(use))
	}
	return scores
}

// detectShowSongs finds the songs of every set. levels holds the combined instrument level
// and stops the stop score (see stopScores), one value per frame. setSizes holds the number
// of songs in each set, and the longest gaps between songs are taken as set breaks. It
// returns each set's song segments in seconds, plus the times where songs ran together
// without a gap and had to be split.
func detectShowSongs(levels, stops []float64, setSizes []int, cfg detectConfig) ([][]segment, []float64) {
	groups := splitIntoSets(songStretches(movingAverage(levels, 3), cfg), len(setSizes))
	songs := make([][]segment, len(setSizes))
	var splits []float64
	for i, g := range groups {
		var setSplits []float64
		songs[i], setSplits = fitSongCount(g, setSizes[i], stops, cfg)
		splits = append(splits, setSplits...)
	}
	return songs, splits
}

// loudStretches returns the loud parts of the curve, separated by quiet runs of at least
// MinGapSec.
func loudStretches(levels []float64, cfg detectConfig) []segment {
	threshold := percentile(levels, 90) - cfg.QuietDropDB
	minGap := int(math.Ceil(cfg.MinGapSec / cfg.FrameSec))

	var segs []segment
	add := func(from, to int) {
		if to > from {
			segs = append(segs, segment{start: float64(from) * cfg.FrameSec, end: float64(to) * cfg.FrameSec})
		}
	}
	loudStart := 0
	for i := 0; i < len(levels); {
		if levels[i] >= threshold {
			i++
			continue
		}
		j := i
		for j < len(levels) && levels[j] < threshold {
			j++
		}
		if j-i >= minGap || i == 0 || j == len(levels) {
			add(loudStart, i)
			loudStart = j
		}
		i = j
	}
	add(loudStart, len(levels))
	return segs
}

// songStretches returns the loud stretches that hold songs: those lasting at least
// MinSongSec. A shorter stretch within AttachGapSec of a song joins that song, so an ending
// or intro where the band stops and starts (the vocal carrying the gaps, or drum hits
// answering it) is not cut off. Other short stretches, like tuning and noodling between
// songs, are dropped.
func songStretches(levels []float64, cfg detectConfig) []segment {
	stretches := loudStretches(levels, cfg)
	isSong := func(s segment) bool { return s.end-s.start >= cfg.MinSongSec }
	for joined := true; joined; {
		joined = false
		for i, s := range stretches {
			if isSong(s) {
				continue
			}
			gapBefore, gapAfter := math.Inf(1), math.Inf(1)
			if i > 0 && isSong(stretches[i-1]) {
				gapBefore = s.start - stretches[i-1].end
			}
			if i+1 < len(stretches) && isSong(stretches[i+1]) {
				gapAfter = stretches[i+1].start - s.end
			}
			if math.Min(gapBefore, gapAfter) > cfg.AttachGapSec {
				continue
			}
			if gapBefore <= gapAfter {
				stretches[i-1].end = s.end
			} else {
				stretches[i+1].start = s.start
			}
			stretches = append(stretches[:i], stretches[i+1:]...)
			joined = true
			break
		}
	}

	var songs []segment
	for _, s := range stretches {
		if isSong(s) {
			songs = append(songs, s)
		}
	}
	return songs
}

// splitIntoSets divides songs into n groups at the n-1 longest gaps between them.
func splitIntoSets(segs []segment, n int) [][]segment {
	groups := make([][]segment, n)
	if n <= 1 || len(segs) < n {
		groups[0] = segs
		return groups
	}
	order := make([]int, len(segs)-1) // order[i] = song index after which the gap comes
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return gapAfter(segs, order[a]) > gapAfter(segs, order[b])
	})
	cuts := append([]int(nil), order[:n-1]...)
	sort.Ints(cuts)
	prev := 0
	for i, c := range cuts {
		groups[i] = segs[prev : c+1]
		prev = c + 1
	}
	groups[n-1] = segs[prev:]
	return groups
}

func gapAfter(segs []segment, i int) float64 {
	return segs[i+1].start - segs[i].end
}

// fitSongCount merges or splits stretches until there are want songs. Stretches with the
// shortest gap between them are merged first (a quiet breakdown inside one song). The
// longest stretch is split where the most instruments stop (two songs played back to back);
// those split times are returned too.
func fitSongCount(segs []segment, want int, stops []float64, cfg detectConfig) ([]segment, []float64) {
	segs = append([]segment(nil), segs...)
	for len(segs) > want && len(segs) > 1 {
		shortest := 0
		for i := 1; i < len(segs)-1; i++ {
			if gapAfter(segs, i) < gapAfter(segs, shortest) {
				shortest = i
			}
		}
		segs[shortest].end = segs[shortest+1].end
		segs = append(segs[:shortest+1], segs[shortest+2:]...)
	}

	var splits []float64
	for len(segs) < want && len(segs) > 0 {
		longest := 0
		for i := range segs {
			if segs[i].end-segs[i].start > segs[longest].end-segs[longest].start {
				longest = i
			}
		}
		at, ok := bestSplit(stops, segs[longest], cfg)
		if !ok {
			break
		}
		parts := []segment{{start: segs[longest].start, end: at}, {start: at, end: segs[longest].end}}
		segs = append(segs[:longest], append(parts, segs[longest+1:]...)...)
		splits = append(splits, at)
	}
	sort.Float64s(splits)
	return segs, splits
}

// bestSplit returns the center of the SplitWinSec window with the highest stop score inside
// seg that leaves at least SplitMarginSec on both sides.
func bestSplit(stops []float64, seg segment, cfg detectConfig) (float64, bool) {
	win := max(int(math.Round(cfg.SplitWinSec/cfg.FrameSec)), 1)
	margin := int(math.Ceil(cfg.SplitMarginSec / cfg.FrameSec))
	first := int(math.Round(seg.start/cfg.FrameSec)) + margin
	last := min(int(math.Round(seg.end/cfg.FrameSec)), len(stops)) - margin - win
	if first > last {
		return 0, false
	}
	best, bestSum := first, math.Inf(-1)
	for i := first; i <= last; i++ {
		sum := 0.0
		for _, v := range stops[i : i+win] {
			sum += v
		}
		if sum > bestSum {
			best, bestSum = i, sum
		}
	}
	return (float64(best) + float64(win)/2) * cfg.FrameSec, true
}

// padSegments extends each song by pre seconds before and post seconds after, without
// crossing the midpoint of the gap to a neighbouring song or the ends of the recording.
func padSegments(segs []segment, pre, post, total float64) []segment {
	padded := make([]segment, len(segs))
	for i, s := range segs {
		lo, hi := 0.0, total
		if i > 0 {
			lo = (segs[i-1].end + s.start) / 2
		}
		if i < len(segs)-1 {
			hi = (s.end + segs[i+1].start) / 2
		}
		padded[i] = segment{start: math.Max(s.start-pre, lo), end: math.Min(s.end+post, hi)}
	}
	return padded
}

// movingAverage smooths x with a centered window of k samples.
func movingAverage(x []float64, k int) []float64 {
	out := make([]float64, len(x))
	half := k / 2
	for i := range x {
		lo, hi := max(i-half, 0), min(i+half+1, len(x))
		sum := 0.0
		for _, v := range x[lo:hi] {
			sum += v
		}
		out[i] = sum / float64(hi-lo)
	}
	return out
}

// percentile returns the p-th percentile of values using linear interpolation.
func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	pos := p / 100 * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := min(lo+1, len(sorted)-1)
	return sorted[lo] + (sorted[hi]-sorted[lo])*(pos-float64(lo))
}
