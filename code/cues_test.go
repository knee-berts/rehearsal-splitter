package main

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCuesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cues.csv")
	cues := []cue{
		{Song: 1, Set: 1, Title: "Mary Jane's Last Dance", Video: "Set 1.MP4", Start: -12.25, End: 185.5},
		{Song: 2, Set: 2, Title: `Song, "Live"`, Video: "Set 2.MP4", Start: 3600.125, End: 3900},
	}
	if err := writeCues(path, cues); err != nil {
		t.Fatalf("writeCues failed: %v", err)
	}
	got, err := readCues(path)
	if err != nil {
		t.Fatalf("readCues failed: %v", err)
	}
	if !reflect.DeepEqual(got, cues) {
		t.Errorf("Round trip mismatch:\nExpected: %+v\nGot:      %+v", cues, got)
	}
}

func TestReadCuesHandEdited(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cues.csv")
	content := "# edited by hand\nsong,set,title,video,start,end\n" +
		"02, 1, Creep, Set 1.MP4, 5:10, 9:00.5\n" +
		"01, 1, Jet Airliner, Set 1.MP4, 95.5, 0:04:40\n"
	os.WriteFile(path, []byte(content), 0o644)

	got, err := readCues(path)
	if err != nil {
		t.Fatalf("readCues failed: %v", err)
	}
	expected := []cue{
		{Song: 1, Set: 1, Title: "Jet Airliner", Video: "Set 1.MP4", Start: 95.5, End: 280},
		{Song: 2, Set: 1, Title: "Creep", Video: "Set 1.MP4", Start: 310, End: 540.5},
	}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("Expected: %+v\nGot:      %+v", expected, got)
	}
}

func TestReadCuesRejectsBadRows(t *testing.T) {
	testCases := map[string]string{
		"EndBeforeStart": "01,1,A,Set 1.MP4,0:02:00,0:01:00\n",
		"DuplicateSong":  "01,1,A,Set 1.MP4,0:01:00,0:02:00\n01,1,B,Set 1.MP4,0:03:00,0:04:00\n",
		"BadTime":        "01,1,A,Set 1.MP4,abc,0:02:00\n",
		"MissingColumn":  "01,1,A,0:01:00,0:02:00\n",
	}
	for name, content := range testCases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cues.csv")
			os.WriteFile(path, []byte(content), 0o644)
			if _, err := readCues(path); err == nil {
				t.Error("Expected an error")
			}
		})
	}
}

func TestTimestamps(t *testing.T) {
	formatCases := map[float64]string{
		0:         "0:00:00.000",
		95.5:      "0:01:35.500",
		4478.5188: "1:14:38.519",
		-12.25:    "-0:00:12.250",
	}
	for sec, expected := range formatCases {
		if got := formatTimestamp(sec); got != expected {
			t.Errorf("formatTimestamp(%v) = %q, expected %q", sec, got, expected)
		}
	}

	parseCases := map[string]float64{
		"1:14:38.519": 4478.519,
		"5:10":        310,
		"95.5":        95.5,
		"-0:00:12.25": -12.25,
	}
	for s, expected := range parseCases {
		got, err := parseTimestamp(s)
		if err != nil || math.Abs(got-expected) > 1e-9 {
			t.Errorf("parseTimestamp(%q) = %v, %v; expected %v", s, got, err, expected)
		}
	}
	for _, bad := range []string{"", "1:2:3:4", "1.5:00", "x", strings.Repeat(":", 2)} {
		if _, err := parseTimestamp(bad); err == nil {
			t.Errorf("parseTimestamp(%q) should fail", bad)
		}
	}
}
