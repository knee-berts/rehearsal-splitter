package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
)

// Config holds all our settings.
type Config struct {
	InputFile        string  `json:"input_file"`
	MinSilenceDur    float64 `json:"min_silence_duration"`
	SilenceThreshold string  `json:"silence_threshold"`
	MinSongLength    float64 `json:"min_song_length"`
	OutputPrefix     string  `json:"output_prefix"`
	OutputDir        string  `json:"output_dir"`
}

// segment holds the start and end time of a clip
type segment struct {
	start float64
	end   float64
}

// --- 1. SCRIPT DEFAULTS ---
var defaultConfig = Config{
	InputFile:        "practice_session.mp4",
	MinSilenceDur:    2.0,
	SilenceThreshold: "-12dB",
	MinSongLength:    200.0,
	OutputPrefix:     "Song",
	OutputDir:        "output",
}

// --- 2. Flag variables (global) ---
var (
	configFilePath   string
	cliInput         string
	cliDuration      float64
	cliThreshold     string
	cliMinSongLength float64
	cliPrefix        string
	cliOutput        string
)

// defineFlags registers all CLI flags
func defineFlags() {
	flag.StringVar(&configFilePath, "config", "config.json", "Path to config JSON file")
	flag.StringVar(&cliInput, "input", defaultConfig.InputFile, "Input video file")
	flag.Float64Var(&cliDuration, "duration", defaultConfig.MinSilenceDur, "Minimum silence duration (seconds)")
	flag.StringVar(&cliThreshold, "threshold", defaultConfig.SilenceThreshold, "Silence threshold (e.g., -30dB)")
	flag.Float64Var(&cliMinSongLength, "minsonglength", defaultConfig.MinSongLength, "Minimum song length (seconds)")
	flag.StringVar(&cliPrefix, "prefix", defaultConfig.OutputPrefix, "Output file prefix")
	flag.StringVar(&cliOutput, "output", defaultConfig.OutputDir, "Output directory")
}

// loadConfig manages loading settings from defaults, file, and (parsed) cli flags.
func loadConfig() (Config, error) {
	// 1. Start with the defaults
	cfg := defaultConfig

	// 2. Load Config File
	fileConfig, err := loadConfigFromFile(configFilePath) // Uses global configFilePath
	if err == nil {
		// Merge fileConfig onto defaultConfig
		if fileConfig.InputFile != "" {
			cfg.InputFile = fileConfig.InputFile
		}
		if fileConfig.MinSilenceDur != 0.0 {
			cfg.MinSilenceDur = fileConfig.MinSilenceDur
		}
		if fileConfig.SilenceThreshold != "" {
			cfg.SilenceThreshold = fileConfig.SilenceThreshold
		}
		if fileConfig.MinSongLength != 0.0 {
			cfg.MinSongLength = fileConfig.MinSongLength
		}
		if fileConfig.OutputPrefix != "" {
			cfg.OutputPrefix = fileConfig.OutputPrefix
		}
		if fileConfig.OutputDir != "" {
			cfg.OutputDir = fileConfig.OutputDir
		}
	} else if !os.IsNotExist(err) {
		// File exists but is bad
		log.Printf("Warning: Could not parse config file '%s': %v. Using defaults.", configFilePath, err)
	}

	// 3. Override with CLI Flags
	userSetFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		userSetFlags[f.Name] = true
	})

	if userSetFlags["input"] {
		cfg.InputFile = cliInput
	}
	if userSetFlags["duration"] {
		cfg.MinSilenceDur = cliDuration
	}
	if userSetFlags["threshold"] {
		cfg.SilenceThreshold = cliThreshold
	}
	if userSetFlags["minsonglength"] {
		cfg.MinSongLength = cliMinSongLength
	}
	if userSetFlags["prefix"] {
		cfg.OutputPrefix = cliPrefix
	}
	if userSetFlags["output"] {
		cfg.OutputDir = cliOutput
	}

	return cfg, nil
}

// loadConfigFromFile helper (unchanged)
func loadConfigFromFile(path string) (Config, error) {
	var fileConfig Config
	data, err := os.ReadFile(path)
	if err != nil {
		return fileConfig, err
	}
	err = json.Unmarshal(data, &fileConfig)
	if err != nil {
		return fileConfig, err
	}
	return fileConfig, nil
}

// main is the entry point of our script
func main() {
	// 1. Define & Parse flags
	defineFlags()
	flag.Parse()

	// 2. Load configuration
	log.Println("Starting practice splitter...")
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	// MODIFIED Log message
	log.Printf("Using config: Input='%s', Duration=%.1fs, Threshold=%s, MinSong=%.1fs, Output='%s'",
		cfg.InputFile, cfg.MinSilenceDur, cfg.SilenceThreshold, cfg.MinSongLength, cfg.OutputDir)

	// 3. Check for ffmpeg
	if !isFFmpegInstalled() {
		log.Fatal("Error: 'ffmpeg' command not found. Please install FFmpeg and ensure it's in your system's PATH.")
	}

	// 4. Check if input file exists
	if _, err := os.Stat(cfg.InputFile); os.IsNotExist(err) {
		log.Fatalf("Error: Input file '%s' not found.", cfg.InputFile)
	}

	// 5. Get video duration
	totalDuration := getVideoDuration(cfg)
	log.Printf("Total video duration: %.2f seconds", totalDuration)

	// 6. Detect silence
	silences := detectSilentSegments(cfg)

	// 7. Calculate valid song segments
	songSegments := calculateNonSilentSegments(silences, totalDuration, cfg)

	// 8. Handle "no silence" case
	if len(silences) == 0 {
		log.Println("No silence detected.")
		if totalDuration >= cfg.MinSongLength {
			log.Println("Treating the entire video as one song.")
			songSegments = []segment{{start: 0, end: totalDuration}}
		}
	}

	// 9. Export valid songs
	if len(songSegments) == 0 {
		log.Println("No song segments found that meet the minimum length criteria.")
	} else {
		log.Printf("Found %d non-silent (song) segment(s) that meet criteria.", len(songSegments))
		splitVideoIntoSegments(cfg, songSegments)
	}

	log.Println("\nAll done!")
}

// --- Helper Functions ---

// isFFmpegInstalled
func isFFmpegInstalled() bool {
	cmd := exec.Command("ffmpeg", "-version")
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

// runFFmpeg
func runFFmpeg(args ...string) (string, error) {
	cmd := exec.Command("ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

// getVideoDuration
func getVideoDuration(cfg Config) float64 {
	log.Println("Getting video duration...")
	output, _ := runFFmpeg("-i", cfg.InputFile)
	re := regexp.MustCompile(`Duration: (\d{2}):(\d{2}):(\d{2})\.(\d{2})`)
	matches := re.FindStringSubmatch(output)
	if len(matches) < 5 {
		log.Fatalf("Could not parse video duration from ffmpeg output. Output was: %s", output)
	}
	hours, _ := strconv.ParseFloat(matches[1], 64)
	minutes, _ := strconv.ParseFloat(matches[2], 64)
	seconds, _ := strconv.ParseFloat(matches[3], 64)
	hundredths, _ := strconv.ParseFloat(matches[4], 64)
	return (hours * 3600) + (minutes * 60) + seconds + (hundredths / 100.0)
}

// detectSilentSegments
func detectSilentSegments(cfg Config) []segment {
	log.Println("Detecting silence... This may take a few minutes.")
	silenceFilter := fmt.Sprintf("silencedetect=noise=%s:d=%.1f", cfg.SilenceThreshold, cfg.MinSilenceDur)
	output, _ := runFFmpeg("-i", cfg.InputFile, "-af", silenceFilter, "-f", "null", "-")
	startRe := regexp.MustCompile(`silence_start: (\d+\.?\d*)`)
	endRe := regexp.MustCompile(`silence_end: (\d+\.?\d*)`)
	startMatches := startRe.FindAllStringSubmatch(output, -1)
	endMatches := endRe.FindAllStringSubmatch(output, -1)
	var silences []segment
	for i := 0; i < len(startMatches) && i < len(endMatches); i++ {
		start, _ := strconv.ParseFloat(startMatches[i][1], 64)
		end, _ := strconv.ParseFloat(endMatches[i][1], 64)
		silences = append(silences, segment{start, end})
	}
	return silences
}

// calculateNonSilentSegments
func calculateNonSilentSegments(silences []segment, totalDuration float64, cfg Config) []segment {
	songSegments := make([]segment, 0) // Initialize as empty, non-nil slice
	lastEndTime := 0.0

	if len(silences) == 0 {
		return songSegments // Return empty; main() handles this case
	}

	// 1. Handle first song
	start := lastEndTime
	end := silences[0].start
	if (end - start) >= cfg.MinSongLength {
		songSegments = append(songSegments, segment{start: start, end: end})
	}

	// 2. Handle middle songs
	for i := 0; i < len(silences)-1; i++ {
		start = silences[i].end
		end = silences[i+1].start
		if (end - start) >= cfg.MinSongLength {
			songSegments = append(songSegments, segment{start: start, end: end})
		}
	}

	// 3. Handle last song
	lastSilenceEnd := silences[len(silences)-1].end
	start = lastSilenceEnd
	end = totalDuration
	// The 0.1 check is to avoid tiny slivers at the end.
	if (end-start) > 0.1 && (end-start) >= cfg.MinSongLength {
		songSegments = append(songSegments, segment{start: start, end: end})
	}

	return songSegments
}

// splitVideoIntoSegments
func splitVideoIntoSegments(cfg Config, segments []segment) {
	if _, err := os.Stat(cfg.OutputDir); os.IsNotExist(err) {
		os.Mkdir(cfg.OutputDir, 0755)
		log.Printf("Created output directory: %s", cfg.OutputDir)
	}
	fileExt := filepath.Ext(cfg.InputFile)
	for i, seg := range segments {
		outputFilename := fmt.Sprintf("%s/%s_%02d%s", cfg.OutputDir, cfg.OutputPrefix, i+1, fileExt)
		duration := seg.end - seg.start
		log.Printf("Exporting segment %d: %s (from %.2fs, duration %.2fs)", i+1, outputFilename, seg.start, duration)
		args := []string{
			"-i", cfg.InputFile,
			"-ss", fmt.Sprintf("%.3f", seg.start),
			"-t", fmt.Sprintf("%.3f", duration),
			"-c:v", "copy",
			"-c:a", "copy",
			outputFilename,
		}
		cmd := exec.Command("ffmpeg", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("Error splitting segment %d: %s\nOutput: %s\n", i+1, err, string(output))
		}
	}
}
