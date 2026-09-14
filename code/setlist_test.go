package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseSetlist(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected []Set
	}{
		{
			name:  "SetHeaders",
			input: "SET 1:\nJet Airliner\nMary Jane’s Last Dance\n\nSET 2:\n1979\nMr. Brightside\n",
			expected: []Set{
				{Name: "SET 1", Songs: []string{"Jet Airliner", "Mary Jane's Last Dance"}},
				{Name: "SET 2", Songs: []string{"1979", "Mr. Brightside"}},
			},
		},
		{
			name:  "PlainListWithoutHeaders",
			input: "Reba\nKid Charlemagne\n",
			expected: []Set{
				{Name: "Set 1", Songs: []string{"Reba", "Kid Charlemagne"}},
			},
		},
		{
			name:  "MarkdownDecoration",
			input: "# Rehearsal\n- Reba\n* Sabotage\n2. Creep\n\n## Encore\nThe Middle\n",
			expected: []Set{
				{Name: "Set 1", Songs: []string{"Reba", "Sabotage", "Creep"}},
				{Name: "Encore", Songs: []string{"The Middle"}},
			},
		},
		{
			name:  "EmptySetsDropped",
			input: "Set 1\nSet 2:\nRemedy\n",
			expected: []Set{
				{Name: "Set 2", Songs: []string{"Remedy"}},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sets, err := parseSetlist(strings.NewReader(tc.input))
			if err != nil {
				t.Fatalf("parseSetlist failed: %v", err)
			}
			if !reflect.DeepEqual(sets, tc.expected) {
				t.Errorf("Expected: %+v\nGot:      %+v", tc.expected, sets)
			}
		})
	}
}

func TestParseSetlistEmpty(t *testing.T) {
	if _, err := parseSetlist(strings.NewReader("SET 1:\n\n")); err == nil {
		t.Error("Expected an error for a setlist without songs")
	}
}

func TestFlattenSetlist(t *testing.T) {
	sets := []Set{
		{Name: "SET 1", Songs: []string{"A", "B"}},
		{Name: "SET 2", Songs: []string{"C"}},
	}
	expected := []SetlistSong{
		{Number: 1, Set: 0, Title: "A"},
		{Number: 2, Set: 0, Title: "B"},
		{Number: 3, Set: 1, Title: "C"},
	}
	if got := flattenSetlist(sets); !reflect.DeepEqual(got, expected) {
		t.Errorf("Expected: %+v\nGot:      %+v", expected, got)
	}
}

func TestSongFolderName(t *testing.T) {
	testCases := []struct {
		number   int
		title    string
		expected string
	}{
		{1, "Mary Jane's Last Dance", "01 - Mary Jane's Last Dance"},
		{24, "Mr. Brightside", "24 - Mr. Brightside"},
		{3, "AC/DC: Live?", "03 - ACDC Live"},
		{12, "Song...", "12 - Song"},
		{5, "   ", "05 - Untitled"},
	}
	for _, tc := range testCases {
		if got := songFolderName(tc.number, tc.title); got != tc.expected {
			t.Errorf("songFolderName(%d, %q) = %q, expected %q", tc.number, tc.title, got, tc.expected)
		}
	}
}
