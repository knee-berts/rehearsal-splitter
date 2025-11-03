package main

import (
	"encoding/json"
	"flag"
	"os"
	"reflect"
	"testing"
)

// resetFlags (unchanged)
func resetFlags() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
}

// createTempConfig (unchanged)
func createTempConfig(t *testing.T, cfg Config) (string, func()) {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Failed to marshal temp config: %v", err)
	}
	tmpfile, err := os.CreateTemp("", "config-*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	if _, err := tmpfile.Write(data); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	return tmpfile.Name(), func() {
		os.Remove(tmpfile.Name())
	}
}

// TestConfigLoading (MODIFIED)
func TestConfigLoading(t *testing.T) {

	// Test Case 1: All defaults
	t.Run("Defaults", func(t *testing.T) {
		resetFlags()
		defineFlags()
		args := []string{"-config=non-existent-file.json"}
		if err := flag.CommandLine.Parse(args); err != nil {
			t.Fatalf("failed to parse flags: %v", err)
		}
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig failed: %v", err)
		}

		if cfg.InputFile != defaultConfig.InputFile {
			t.Errorf("Expected InputFile %s, got %s", defaultConfig.InputFile, cfg.InputFile)
		}
		if cfg.MinSongLength != defaultConfig.MinSongLength {
			t.Errorf("Expected MinSongLength %f, got %f", defaultConfig.MinSongLength, cfg.MinSongLength)
		}
		if cfg.UploadToDrive != defaultConfig.UploadToDrive { // ADDED
			t.Errorf("Expected UploadToDrive %v, got %v", defaultConfig.UploadToDrive, cfg.UploadToDrive)
		}
	})

	// Test Case 2: Config file overrides defaults
	t.Run("FileOverridesDefaults", func(t *testing.T) {
		resetFlags()
		defineFlags()
		// Create a temp config file
		fileCfg := Config{
			InputFile:        "file_video.mp4",
			SilenceThreshold: "-20dB",
			MinSongLength:    60.0,
			UploadToDrive:    true,         // ADDED
			DriveSubfolder:   "FileFolder", // ADDED
		}
		configFile, cleanup := createTempConfig(t, fileCfg)
		defer cleanup()
		args := []string{"-config=" + configFile}
		if err := flag.CommandLine.Parse(args); err != nil {
			t.Fatalf("failed to parse flags: %v", err)
		}
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig failed: %v", err)
		}

		if cfg.InputFile != "file_video.mp4" {
			t.Errorf("Expected InputFile 'file_video.mp4', got %s", cfg.InputFile)
		}
		if cfg.UploadToDrive != true { // ADDED
			t.Errorf("Expected UploadToDrive true, got %v", cfg.UploadToDrive)
		}
		if cfg.DriveSubfolder != "FileFolder" { // ADDED
			t.Errorf("Expected DriveSubfolder 'FileFolder', got %s", cfg.DriveSubfolder)
		}
		if cfg.RcloneRemote != defaultConfig.RcloneRemote { // Check default is still there
			t.Errorf("Expected RcloneRemote %s, got %s", defaultConfig.RcloneRemote, cfg.RcloneRemote)
		}
	})

	// Test Case 3: CLI overrides config file
	t.Run("CliOverridesFile", func(t *testing.T) {
		resetFlags()
		defineFlags()
		// 1. Create a temp config file
		fileCfg := Config{
			InputFile:     "file_video.mp4",
			MinSongLength: 60.0,
			UploadToDrive: false, // File says false
		}
		configFile, cleanup := createTempConfig(t, fileCfg)
		defer cleanup()

		// 2. Define the CLI arguments
		args := []string{
			"-config=" + configFile,
			"-input=cli_video.mp4", // This should override the file
			"-upload=true",         // This should override the file
			"-subfolder=CLIFolder", // This should override the default
		}
		// 3. Parse
		if err := flag.CommandLine.Parse(args); err != nil {
			t.Fatalf("failed to parse flags: %v", err)
		}
		// 4. Load config
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig failed: %v", err)
		}

		// 5. Assert
		if cfg.InputFile != "cli_video.mp4" { // Check CLI override
			t.Errorf("Expected InputFile 'cli_video.mp4' (from CLI), got %s", cfg.InputFile)
		}
		if cfg.UploadToDrive != true { // Check CLI override
			t.Errorf("Expected UploadToDrive true (from CLI), got %v", cfg.UploadToDrive)
		}
		if cfg.DriveSubfolder != "CLIFolder" { // Check CLI override
			t.Errorf("Expected DriveSubfolder 'CLIFolder' (from CLI), got %s", cfg.DriveSubfolder)
		}
	})
}

// TestCalculateNonSilentSegments (Unchanged)
func TestCalculateNonSilentSegments(t *testing.T) {

	// Create a base config for all tests.
	// Use a short length so we don't break existing tests.
	baseTestCfg := Config{
		MinSongLength: 10.0, // All songs must be >= 10.0s
	}

	testCases := []struct {
		name          string    // Name of the test
		silences      []segment // Input: list of detected silences
		totalDuration float64   // Input: total video duration
		cfg           Config    // Input: config to use (if 0, use baseTestCfg)
		expected      []segment // Expected output: list of "song" segments
	}{
		{
			name:          "NoSilence",
			silences:      []segment{},
			totalDuration: 300.0,
			expected:      []segment{},
		},
		{
			name:          "SongAtStart",
			silences:      []segment{{start: 180.0, end: 190.0}},
			totalDuration: 300.0,
			expected: []segment{
				{start: 0.0, end: 180.0},
				{start: 190.0, end: 300.0},
			},
		},
		{
			name:          "SongInMiddle",
			silences:      []segment{{start: 0.0, end: 10.0}, {start: 200.0, end: 210.0}},
			totalDuration: 300.0,
			expected: []segment{
				{start: 10.0, end: 200.0},
				{start: 210.0, end: 300.0},
			},
		},
		{
			name:          "MultipleSongs",
			silences:      []segment{{start: 100.0, end: 110.0}, {start: 200.0, end: 210.0}},
			totalDuration: 300.0,
			expected: []segment{
				{start: 0.0, end: 100.0},   // 100s long
				{start: 110.0, end: 200.0}, // 90s long
				{start: 210.0, end: 300.0}, // 90s long
			},
		},
		{
			name:          "FilterShortSongs",
			cfg:           Config{MinSongLength: 50.0}, // Use a custom config for this test
			silences:      []segment{{start: 40.0, end: 50.0}, {start: 100.0, end: 110.0}},
			totalDuration: 120.0,
			expected: []segment{
				// Song 1: 0.0 to 40.0 (40s long). SHOULD BE FILTERED.
				// Song 2: 50.0 to 100.0 (50s long). SHOULD BE KEPT.
				{start: 50.0, end: 100.0},
				// Song 3: 110.0 to 120.0 (10s long). SHOULD BE FILTERED.
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Use the specific test case config if it exists, otherwise use the base
			cfgToUse := baseTestCfg
			if !reflect.ValueOf(tc.cfg).IsZero() {
				cfgToUse = tc.cfg
			}

			// Call the function we are testing
			result := calculateNonSilentSegments(tc.silences, tc.totalDuration, cfgToUse)

			// Compare the result to the expected output
			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("Failed on test '%s':\nExpected: %+v\nGot:      %+v",
					tc.name, tc.expected, result)
			}
		})
	}
}
