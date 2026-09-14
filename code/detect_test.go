package main

import (
	"math"
	"testing"
)

const (
	loud  = -20.0
	quiet = -70.0
)

// span is a stretch of constant level in a synthetic level curve.
type span struct {
	sec   float64
	level float64
}

func buildLevels(spans ...span) []float64 {
	var levels []float64
	for _, s := range spans {
		for i := 0; i < int(s.sec/defaultDetectConfig.FrameSec); i++ {
			levels = append(levels, s.level)
		}
	}
	return levels
}

// stopsFor derives the stop score of a single-channel level curve.
func stopsFor(levels []float64) []float64 {
	typical := percentile(levels, 90)
	stops := make([]float64, len(levels))
	for i, v := range levels {
		stops[i] = math.Min(math.Max(typical-v, 0), defaultDetectConfig.MaxStopDB)
	}
	return stops
}

func expectSegments(t *testing.T, got, expected []segment, tolerance float64) {
	t.Helper()
	if len(got) != len(expected) {
		t.Fatalf("Expected %d segments, got %d: %+v", len(expected), len(got), got)
	}
	for i := range expected {
		if math.Abs(got[i].start-expected[i].start) > tolerance || math.Abs(got[i].end-expected[i].end) > tolerance {
			t.Errorf("Segment %d: expected %+v, got %+v", i, expected[i], got[i])
		}
	}
}

func TestDetectShowSongs(t *testing.T) {
	testCases := []struct {
		name     string
		levels   []float64
		setSizes []int
		expected [][]segment
		splits   []float64
	}{
		{
			name: "GapsBetweenSongsAndShortNoodleIgnored",
			levels: buildLevels(span{30, quiet}, span{20, loud}, span{20, quiet},
				span{200, loud}, span{10, quiet}, span{180, loud}, span{10, quiet}, span{240, loud}, span{30, quiet}),
			setSizes: []int{3},
			expected: [][]segment{{{70, 270}, {280, 460}, {470, 710}}},
		},
		{
			name: "BackToBackSongsSplitWhereInstrumentsStop",
			levels: buildLevels(span{10, quiet}, span{200, loud}, span{1, quiet}, span{200, loud},
				span{10, quiet}, span{150, loud}, span{10, quiet}),
			setSizes: []int{3},
			expected: [][]segment{{{10, 210}, {210, 411}, {421, 571}}},
			splits:   []float64{210},
		},
		{
			name: "SplitLeavesRoomForBothSongs",
			levels: buildLevels(span{10, quiet}, span{190, loud}, span{1, -45}, span{179, loud},
				span{1, quiet}, span{29, loud}, span{10, quiet}),
			setSizes: []int{2},
			expected: [][]segment{{{10, 200}, {200, 410}}},
			splits:   []float64{200},
		},
		{
			name: "StopStartEndingStaysWithSong",
			levels: buildLevels(span{10, quiet}, span{200, loud}, span{4, quiet}, span{4, loud}, span{8, quiet},
				span{1, loud}, span{8, quiet}, span{19, loud}, span{30, quiet}, span{150, loud}, span{10, quiet}),
			setSizes: []int{2},
			expected: [][]segment{{{10, 254}, {284, 434}}},
		},
		{
			name:     "CountInStaysWithSong",
			levels:   buildLevels(span{30, quiet}, span{2, loud}, span{5, quiet}, span{200, loud}, span{30, quiet}),
			setSizes: []int{1},
			expected: [][]segment{{{30, 237}}},
		},
		{
			name: "BreakdownInsideSongMerged",
			levels: buildLevels(span{10, quiet}, span{100, loud}, span{5, quiet}, span{100, loud},
				span{20, quiet}, span{150, loud}, span{10, quiet}),
			setSizes: []int{2},
			expected: [][]segment{{{10, 215}, {235, 385}}},
		},
		{
			name: "LongestGapIsTheSetBreak",
			levels: buildLevels(span{10, quiet}, span{100, loud}, span{10, quiet}, span{100, loud},
				span{300, quiet}, span{100, loud}, span{10, quiet}),
			setSizes: []int{2, 1},
			expected: [][]segment{{{10, 110}, {120, 220}}, {{520, 620}}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, splits := detectShowSongs(tc.levels, stopsFor(tc.levels), tc.setSizes, defaultDetectConfig)
			if len(got) != len(tc.expected) {
				t.Fatalf("Expected %d sets, got %d", len(tc.expected), len(got))
			}
			for i := range tc.expected {
				expectSegments(t, got[i], tc.expected[i], 1.0)
			}
			if len(splits) != len(tc.splits) {
				t.Fatalf("Expected splits %v, got %v", tc.splits, splits)
			}
			for i := range splits {
				if math.Abs(splits[i]-tc.splits[i]) > 1.0 {
					t.Errorf("Expected split %d at %v, got %v", i, tc.splits[i], splits[i])
				}
			}
		})
	}
}

func TestStopScores(t *testing.T) {
	// Two channels, two samples per frame: frame 1 has one channel stopped, frame 2 both.
	frames := [][2]float32{{1, 1}, {0.001, 1}, {0.001, 0.001}}
	for i := 0; i < 7; i++ {
		frames = append(frames, [2]float32{1, 1})
	}
	var samples []float32
	for _, fr := range frames {
		samples = append(samples, fr[0], fr[1], fr[0], fr[1])
	}
	got := stopScores(samples, 2, []int{0, 1}, 2, 40)
	for i, expected := range []float64{0, 20, 40} {
		if math.Abs(got[i]-expected) > 1e-6 {
			t.Errorf("Frame %d: expected stop score %v, got %v", i, expected, got[i])
		}
	}
}

func TestPadSegments(t *testing.T) {
	segs := []segment{{10, 100}, {104, 200}}
	got := padSegments(segs, 2, 5, 203)
	expectSegments(t, got, []segment{{8, 102}, {102, 203}}, 1e-9)
}

func TestFrameLevelsDB(t *testing.T) {
	// Two channels, hop of two samples: channel 0 is loud then quiet, channel 1 is silent.
	samples := []float32{1, 0, 1, 0, 0.1, 0, 0.1, 0}
	got := frameLevelsDB(samples, 2, []int{0}, 2)
	if len(got) != 2 || math.Abs(got[0]) > 1e-6 || math.Abs(got[1]+20) > 1e-3 {
		t.Errorf("Expected levels [0 -20] dB, got %v", got)
	}
	if silent := frameLevelsDB(samples, 2, []int{1}, 2); silent[0] > -100 {
		t.Errorf("Expected a silent channel far below -100 dB, got %v", silent)
	}
}

func TestPercentile(t *testing.T) {
	values := []float64{5, 1, 4, 2, 3}
	if got := percentile(values, 50); got != 3 {
		t.Errorf("Expected median 3, got %f", got)
	}
	if got := percentile(values, 90); math.Abs(got-4.6) > 1e-9 {
		t.Errorf("Expected 90th percentile 4.6, got %f", got)
	}
}
