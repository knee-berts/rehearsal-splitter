package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestParseSongList(t *testing.T) {
	got, err := parseSongList("3, 7-9,12")
	if err != nil {
		t.Fatalf("parseSongList failed: %v", err)
	}
	expected := map[int]bool{3: true, 7: true, 8: true, 9: true, 12: true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("Expected %v, got %v", expected, got)
	}
	if all, err := parseSongList(""); err != nil || len(all) != 0 {
		t.Errorf("Expected an empty list to select every song, got %v, %v", all, err)
	}
	for _, bad := range []string{"x", "0", "9-7", "3-"} {
		if _, err := parseSongList(bad); err == nil {
			t.Errorf("parseSongList(%q) should fail", bad)
		}
	}
}

func TestNaturalLess(t *testing.T) {
	names := []string{"Set 10.MP4", "Set 2.MP4", "set 1.mp4"}
	sort.Slice(names, func(a, b int) bool { return naturalLess(names[a], names[b]) })
	expected := []string{"set 1.mp4", "Set 2.MP4", "Set 10.MP4"}
	if !reflect.DeepEqual(names, expected) {
		t.Errorf("Expected %v, got %v", expected, names)
	}
}

func TestInstrumentChannels(t *testing.T) {
	boards := []wavInfo{
		{Path: "audio/01JVOX.WAV", Channels: 1},
		{Path: "audio/05GUITAR.WAV", Channels: 1},
		{Path: "audio/keys-stereo.wav", Channels: 2},
		{Path: "audio/04RVOX.WAV", Channels: 1},
	}
	if got := instrumentChannels(boards); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Errorf("Expected instrument channels [1 2 3], got %v", got)
	}
	vocalsOnly := []wavInfo{{Path: "Vocal.wav", Channels: 1}, {Path: "VOX2.wav", Channels: 1}}
	if got := instrumentChannels(vocalsOnly); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Errorf("Expected every channel when all are vocals, got %v", got)
	}
}

func TestVideoCovering(t *testing.T) {
	videos := []videoSync{
		{File: "Set 1.MP4", Duration: 3996.8, BoardOffset: 301},
		{File: "Set 2.MP4", Duration: 3239.8, BoardOffset: 4478.5},
	}
	testCases := []struct {
		seg      segment
		expected string
	}{
		{segment{400, 600}, "Set 1.MP4"},
		{segment{4250, 4500}, "Set 1.MP4"},
		{segment{4400, 4700}, "Set 2.MP4"},
		{segment{0, 200}, ""},
	}
	for _, tc := range testCases {
		v, ok := videoCovering(videos, tc.seg)
		if (tc.expected == "" && ok) || (tc.expected != "" && v.File != tc.expected) {
			t.Errorf("Segment %+v: expected %q, got %q (found %v)", tc.seg, tc.expected, v.File, ok)
		}
	}
}

func TestReconcileCues(t *testing.T) {
	previous := []cue{
		{Song: 1, Set: 1, Title: "Mixed", Video: "Set 1.MP4", Start: 10, End: 200},
		{Song: 2, Set: 1, Title: "Unchanged", Video: "Set 1.MP4", Start: 210, End: 400.001},
		{Song: 3, Set: 1, Title: "Longer ending", Video: "Set 1.MP4", Start: 410, End: 600},
		{Song: 4, Set: 1, Title: "Mixed but not found", Video: "Set 1.MP4", Start: 610, End: 800},
	}
	detected := []cue{
		{Song: 1, Set: 1, Title: "Mixed", Video: "Set 1.MP4", Start: 9, End: 205},
		{Song: 2, Set: 1, Title: "Unchanged", Video: "Set 1.MP4", Start: 210, End: 400.0004},
		{Song: 3, Set: 1, Title: "Longer ending", Video: "Set 1.MP4", Start: 410, End: 640},
		{Song: 5, Set: 1, Title: "New", Video: "Set 1.MP4", Start: 810, End: 900},
	}
	mixed := map[int]bool{1: true, 4: true}
	cues, recut, notes := reconcileCues(previous, detected, func(old cue) string {
		if mixed[old.Song] {
			return "it already has a mixdown"
		}
		return ""
	})

	expected := []cue{previous[0], detected[1], detected[2], previous[3], detected[3]}
	if !reflect.DeepEqual(cues, expected) {
		t.Errorf("Expected cues %+v\ngot %+v", expected, cues)
	}
	if !reflect.DeepEqual(recut, map[int]bool{3: true}) {
		t.Errorf("Expected only song 3 to be cut again, got %v", recut)
	}
	if len(notes) != 2 {
		t.Errorf("Expected notes for the kept song and the changed song, got %q", notes)
	}
}

func TestFindMixdown(t *testing.T) {
	dir := t.TempDir()
	if got, err := findMixdown(filepath.Join(dir, "missing")); got != "" || err != nil {
		t.Errorf("Expected no mixdown for a missing folder, got %q, %v", got, err)
	}

	old := filepath.Join(dir, "Creep v1.wav")
	newer := filepath.Join(dir, "Creep v2.aif")
	os.WriteFile(old, []byte("x"), 0o644)
	os.WriteFile(newer, []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "._Creep v3.wav"), []byte("x"), 0o644)
	past := time.Now().Add(-time.Hour)
	os.Chtimes(old, past, past)

	got, err := findMixdown(dir)
	if err != nil || got != newer {
		t.Errorf("Expected the newest audio file %q, got %q, %v", newer, got, err)
	}
}
