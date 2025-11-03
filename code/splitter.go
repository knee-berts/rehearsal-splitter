package main

import (
	"bufio"
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
	"strings"
)

// Config holds all our settings.
type Config struct {
	InputFile        string  `json:"input_file"`
	MinSilenceDur    float64 `json:"min_silence_duration"`
	SilenceThreshold string  `json:"silence_threshold"`
	MinSongLength    float64 `json:"min_song_length"`
	OutputPrefix     string  `json:"output_prefix"`
	OutputDir        string  `json:"output_dir"`
	UploadToDrive    bool    `json:"upload_to_drive"`
	RcloneRemote     string  `json:"rclone_remote"`
	DriveSubfolder   string  `json:"drive_subfolder"`
	SetlistFile      string  `json:"setlist_file"`
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
	UploadToDrive:    false,
	RcloneRemote:     "gdrive:",
	DriveSubfolder:   "SplitSongs",
	SetlistFile:      "",
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
	cliUpload        bool
	cliRemote        string
	cliSubfolder     string
	cliSetlistFile   string
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
	flag.BoolVar(&cliUpload, "upload", defaultConfig.UploadToDrive, "Upload output folder to Google Drive")
	flag.StringVar(&cliRemote, "remote", defaultConfig.RcloneRemote, "rclone remote name (e.g., 'gdrive:')")
	flag.StringVar(&cliSubfolder, "subfolder", defaultConfig.DriveSubfolder, "Google Drive subfolder to upload to")
	flag.StringVar(&cliSetlistFile, "setlist", defaultConfig.SetlistFile, "Path to a .txt setlist file for renaming")
}

// loadConfig manages loading settings from defaults, file, and (parsed) cli flags.
func loadConfig() (Config, error) {
	// 1. Start with the defaults
	cfg := defaultConfig

	// 2. Load Config File
	fileConfig, err := loadConfigFromFile(configFilePath)
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
		if fileConfig.UploadToDrive {
			cfg.UploadToDrive = fileConfig.UploadToDrive
		}
		if fileConfig.RcloneRemote != "" {
			cfg.RcloneRemote = fileConfig.RcloneRemote
		}
		if fileConfig.DriveSubfolder != "" {
			cfg.DriveSubfolder = fileConfig.DriveSubfolder
		}
		if fileConfig.SetlistFile != "" {
			cfg.SetlistFile = fileConfig.SetlistFile
		}
	} else if !os.IsNotExist(err) {
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
	if userSetFlags["upload"] {
		cfg.UploadToDrive = cliUpload
	}
	if userSetFlags["remote"] {
		cfg.RcloneRemote = cliRemote
	}
	if userSetFlags["subfolder"] {
		cfg.DriveSubfolder = cliSubfolder
	}
	if userSetFlags["setlist"] {
		cfg.SetlistFile = cliSetlistFile
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

// main is the entry point of our script (MODIFIED)
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

	log.Printf("Using config: Input='%s', Duration=%.1fs, Threshold=%s, MinSong=%.1fs, Output='%s'",
		cfg.InputFile, cfg.MinSilenceDur, cfg.SilenceThreshold, cfg.MinSongLength, cfg.OutputDir)

	// 3. --- rclone Pre-Check (NEW) ---
	if cfg.UploadToDrive {
		log.Println("Upload enabled, running rclone pre-check...")
		if !isRcloneInstalled() {
			log.Fatal("Error: 'upload_to_drive' is true but 'rclone' was not found in your PATH.")
		}

		if err := testRcloneConnection(cfg); err != nil {
			log.Fatalf("rclone pre-check failed: %v\nPlease check 'rclone config' and your remote permissions.", err)
		}
		log.Println("rclone connection successful.")
	}

	// 4. Check for ffmpeg
	if !isFFmpegInstalled() {
		log.Fatal("Error: 'ffmpeg' command not found. Please install FFmpeg and ensure it's in your system's PATH.")
	}

	// 5. Check if input file exists
	if _, err := os.Stat(cfg.InputFile); os.IsNotExist(err) {
		log.Fatalf("Error: Input file '%s' not found.", cfg.InputFile)
	}

	// 6. Get video duration
	totalDuration := getVideoDuration(cfg)
	log.Printf("Total video duration: %.2f seconds", totalDuration)

	// 7. Detect silence
	silences := detectSilentSegments(cfg)

	// 8. Calculate valid song segments
	songSegments := calculateNonSilentSegments(silences, totalDuration, cfg)

	// 9. Handle "no silence" case
	if len(silences) == 0 {
		log.Println("No silence detected.")
		if totalDuration >= cfg.MinSongLength {
			log.Println("Treating the entire video as one song.")
			songSegments = []segment{{start: 0, end: totalDuration}}
		}
	}

	// 10. Export valid songs
	var exportedFiles []string // <-- DECLARED HERE
	if len(songSegments) == 0 {
		log.Println("No song segments found that meet the minimum length criteria.")
	} else {
		log.Printf("Found %d non-silent (song) segment(s) that meet criteria.", len(songSegments))
		exportedFiles = splitVideoIntoSegments(cfg, songSegments) // <-- ASSIGNED HERE
	}

	// 11. --- Rename from Setlist (Optional) ---
	if cfg.SetlistFile != "" {
		if len(exportedFiles) > 0 {
			renameFilesFromSetlist(cfg, exportedFiles)
		} else {
			log.Println("Skipping setlist rename, no files were exported.")
		}
	}

	// 12. Upload to Drive (Optional)
	if cfg.UploadToDrive {
		if _, err := os.Stat(cfg.OutputDir); os.IsNotExist(err) {
			log.Printf("Skipping upload, output directory '%s' does not exist.", cfg.OutputDir)
		} else {
			uploadToDrive(cfg)
		}
	}

	log.Println("\nAll done!")
}

// --- Helper Functions ---

// isFFmpegInstalled (unchanged)
func isFFmpegInstalled() bool {
	cmd := exec.Command("ffmpeg", "-version")
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

// runFFmpeg (unchanged)
func runFFmpeg(args ...string) (string, error) {
	cmd := exec.Command("ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

// isRcloneInstalled (unchanged)
func isRcloneInstalled() bool {
	cmd := exec.Command("rclone", "version")
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

// testRcloneConnection (unchanged)
func testRcloneConnection(cfg Config) error {
	log.Println("Verifying rclone remote and permissions...")
	destination := cfg.RcloneRemote + cfg.DriveSubfolder
	cmd := exec.Command("rclone", "mkdir", destination)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("could not access rclone remote '%s'.\nError: %s", destination, stderr.String())
	}
	return nil
}

// getVideoDuration (unchanged)
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

// detectSilentSegments (unchanged)
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

// calculateNonSilentSegments (unchanged)
func calculateNonSilentSegments(silences []segment, totalDuration float64, cfg Config) []segment {
	songSegments := make([]segment, 0)
	lastEndTime := 0.0

	if len(silences) == 0 {
		return songSegments
	}
	start := lastEndTime
	end := silences[0].start
	if (end - start) >= cfg.MinSongLength {
		songSegments = append(songSegments, segment{start: start, end: end})
	}
	for i := 0; i < len(silences)-1; i++ {
		start = silences[i].end
		end = silences[i+1].start
		if (end - start) >= cfg.MinSongLength {
			songSegments = append(songSegments, segment{start: start, end: end})
		}
	}
	lastSilenceEnd := silences[len(silences)-1].end
	start = lastSilenceEnd
	end = totalDuration
	if (end-start) > 0.1 && (end-start) >= cfg.MinSongLength {
		songSegments = append(songSegments, segment{start: start, end: end})
	}
	return songSegments
}

// splitVideoIntoSegments (MODIFIED)
// Now returns a list of the files it created
func splitVideoIntoSegments(cfg Config, segments []segment) []string {
	if _, err := os.Stat(cfg.OutputDir); os.IsNotExist(err) {
		os.Mkdir(cfg.OutputDir, 0755)
		log.Printf("Created output directory: %s", cfg.OutputDir)
	}
	fileExt := filepath.Ext(cfg.InputFile)
	exportedFiles := make([]string, 0)

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
		} else {
			exportedFiles = append(exportedFiles, outputFilename)
		}
	}
	return exportedFiles
}

// uploadToDrive (unchanged)
func uploadToDrive(cfg Config) {
	log.Println("--- Starting Google Drive Upload ---")
	destination := cfg.RcloneRemote + cfg.DriveSubfolder + "/" + cfg.OutputDir
	log.Printf("Uploading local folder '%s' to '%s'", cfg.OutputDir, destination)
	cmd := exec.Command("rclone", "copy", cfg.OutputDir, destination, "-P")
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	if err := cmd.Run(); err != nil {
		log.Printf("Error: rclone upload failed: %v", err)
		log.Println("Please ensure rclone is installed and configured ('rclone config').")
	} else {
		log.Println("--- Google Drive Upload Complete ---")
	}
}

// --- ADD THIS NEW FUNCTION ---
// sanitizeFilename cleans a song title to be a valid file name
func sanitizeFilename(name string) string {
	// 1. Trim whitespace
	name = strings.TrimSpace(name)
	// 2. Define invalid characters (anything not a letter, number, space, hyphen, underscore)
	invalidChars := regexp.MustCompile(`[^\w\s\-]`)
	name = invalidChars.ReplaceAllString(name, "")
	// 3. Replace spaces with underscores
	name = strings.ReplaceAll(name, " ", "_")
	// 4. Handle potential empty names
	if name == "" {
		name = "Untitled_Song"
	}
	return name
}

// --- ADD THIS NEW FUNCTION ---
// renameFilesFromSetlist reads a setlist file and renames exported files
func renameFilesFromSetlist(cfg Config, exportedFiles []string) {
	log.Println("--- Renaming files from setlist ---")

	// 1. Open the setlist file
	file, err := os.Open(cfg.SetlistFile)
	if err != nil {
		log.Printf("Error: Could not open setlist file '%s': %v", cfg.SetlistFile, err)
		log.Println("Skipping rename.")
		return
	}
	defer file.Close()

	// 2. Read song titles into a slice
	var songTitles []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		title := scanner.Text()
		if title != "" { // Skip empty lines
			songTitles = append(songTitles, title)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Error reading setlist file: %v", err)
		log.Println("Skipping rename.")
		return
	}

	// 3. Compare file counts
	if len(songTitles) < len(exportedFiles) {
		log.Printf("Warning: Setlist has %d songs, but %d files were exported.", len(songTitles), len(exportedFiles))
		log.Println("Only the first %d files will be renamed.", len(songTitles))
	} else if len(songTitles) > len(exportedFiles) {
		log.Printf("Warning: Setlist has %d songs, but only %d files were exported.", len(songTitles), len(exportedFiles))
	}

	// 4. Rename files
	for i, oldFilePath := range exportedFiles {
		if i >= len(songTitles) {
			break // Stop if we run out of song titles
		}

		// Get components of the old path
		dir := filepath.Dir(oldFilePath)
		ext := filepath.Ext(oldFilePath)

		// Create new name
		newSongName := sanitizeFilename(songTitles[i])
		// Format: 01 - Song_Name.mp4
		newFileName := fmt.Sprintf("%02d - %s%s", i+1, newSongName, ext)
		newFilePath := filepath.Join(dir, newFileName)

		// Rename
		err := os.Rename(oldFilePath, newFilePath)
		if err != nil {
			log.Printf("Error renaming '%s' to '%s': %v", oldFilePath, newFilePath, err)
		} else {
			log.Printf("Renamed '%s' -> '%s'", filepath.Base(oldFilePath), newFileName)
		}
	}
	log.Println("--- Setlist renaming complete ---")
}
