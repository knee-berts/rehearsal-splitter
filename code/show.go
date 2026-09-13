package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const showUsage = `Usage:
  splitter show prepare [flags] <show folder>
  splitter show finish [flags] <show folder>

A show folder holds the board recorder's channel WAVs (audio/), the set videos (video/),
and a setlist (setlist.md or setlist.txt, with "SET 1:" style headers).

prepare  lines the set videos up with the board recording, finds every song, and writes
         songs/NN - Title/ with the song's channel WAVs (channels/), its camera video,
         and an empty mix/ folder. Song boundaries are saved to songs/cues.csv.
finish   lines up the mixdown saved in each song's mix/ folder with the song's video and
         writes songs/NN - Title/NN - Title.mp4.

Flags:
`

const (
	levelRate      = 1000 // sample rate of the per-channel level analysis
	cuesFileName   = "cues.csv"
	syncFileName   = "sync.json"
	channelsFolder = "channels"
	mixFolder      = "mix"
)

var (
	vocalChannelRe = regexp.MustCompile(`(?i)(vox|voc|vocal|talk)`)
	numberChunkRe  = regexp.MustCompile(`\d+|\D+`)
	videoExts      = map[string]bool{".mp4": true, ".mov": true, ".m4v": true, ".mkv": true}
)

// showOptions holds the flags of the show subcommands.
type showOptions struct {
	Dir      string
	AudioDir string
	VideoDir string
	OutDir   string
	Setlist  string
	Songs    map[int]bool // empty means every song
	PreRoll  float64
	PostRoll float64
	Redetect bool
	Recut    bool
	Force    bool
	MaxShift float64
}

// runShow runs a show subcommand and returns the process exit code.
func runShow(args []string) int {
	log.SetFlags(0)
	if len(args) == 0 || (args[0] != "prepare" && args[0] != "finish") {
		fmt.Fprint(os.Stderr, showUsage)
		return 2
	}
	cmd := args[0]
	opts := showOptions{}
	fs := flag.NewFlagSet("show "+cmd, flag.ContinueOnError)
	fs.StringVar(&opts.AudioDir, "audio", "audio", "folder with the board channel WAVs, inside the show folder")
	fs.StringVar(&opts.VideoDir, "video", "video", "folder with the set videos, inside the show folder")
	fs.StringVar(&opts.OutDir, "out", "songs", "folder for the song folders, inside the show folder")
	fs.StringVar(&opts.Setlist, "setlist", "", "setlist file (default: setlist.md or setlist.txt in the show folder)")
	songList := fs.String("songs", "", "only process these song numbers, like 3,7-9")
	if cmd == "prepare" {
		fs.Float64Var(&opts.PreRoll, "pre", 2, "seconds to keep before each song")
		fs.Float64Var(&opts.PostRoll, "post", 5, "seconds to keep after each song")
		fs.BoolVar(&opts.Redetect, "redetect", false, "analyze the recordings again and overwrite cues.csv")
		fs.BoolVar(&opts.Recut, "recut", false, "cut channel WAVs and videos again even if they exist")
	} else {
		fs.BoolVar(&opts.Force, "force", false, "rebuild song videos that are already up to date")
		fs.Float64Var(&opts.MaxShift, "maxshift", 30, "largest offset in seconds to search between a mixdown and its video")
	}
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, showUsage)
		fs.PrintDefaults()
	}

	// Accept flags both before and after the show folder.
	var positional []string
	rest := args[1:]
	for {
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(positional) != 1 {
		fs.Usage()
		return 2
	}
	opts.Dir = positional[0]

	var err error
	if opts.Songs, err = parseSongList(*songList); err != nil {
		log.Printf("Error: %v", err)
		return 2
	}
	if err = requireTools("ffmpeg", "ffprobe"); err == nil {
		if cmd == "prepare" {
			err = prepareShow(opts)
		} else {
			err = finishShow(opts)
		}
	}
	if err != nil {
		log.Printf("Error: %v", err)
		return 1
	}
	return 0
}

// prepareShow finds the songs (or reuses the saved cue sheet) and cuts each song's folder.
func prepareShow(opts showOptions) error {
	sets, err := loadShowSetlist(opts)
	if err != nil {
		return err
	}
	boards, err := boardChannels(filepath.Join(opts.Dir, opts.AudioDir))
	if err != nil {
		return err
	}
	outDir := filepath.Join(opts.Dir, opts.OutDir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	cuesPath := filepath.Join(outDir, cuesFileName)
	syncPath := filepath.Join(outDir, syncFileName)

	var sync showSync
	var cues []cue
	if !opts.Redetect && fileExists(cuesPath) && fileExists(syncPath) {
		log.Printf("Using the song boundaries in %s (-redetect analyzes the recordings again).", cuesPath)
		if sync, err = readSync(syncPath); err != nil {
			return err
		}
		if cues, err = readCues(cuesPath); err != nil {
			return err
		}
	} else {
		var videos []string
		if videos, err = setVideos(filepath.Join(opts.Dir, opts.VideoDir)); err != nil {
			return err
		}
		if sync, cues, err = analyzeShow(boards, videos, sets, opts); err != nil {
			return err
		}
		if err = writeSync(syncPath, sync); err != nil {
			return err
		}
		if err = writeCues(cuesPath, cues, sync.Notes...); err != nil {
			return err
		}
		log.Printf("Saved song boundaries to %s", cuesPath)
	}
	printCues(cues)
	return cutSongs(opts, boards, sync, cues)
}

// analyzeShow lines every set video up with the board recording and detects the songs.
func analyzeShow(boards []wavInfo, videos []string, sets []Set, opts showOptions) (showSync, []cue, error) {
	began := time.Now()
	sync := showSync{BoardDuration: boardDuration(boards)}
	log.Printf("Reading %d board channels (%s long)...", len(boards), formatTimestamp(sync.BoardDuration))
	sum, levels, err := decodeBoard(boards, levelRate)
	if err != nil {
		return sync, nil, err
	}

	for _, path := range videos {
		name := filepath.Base(path)
		log.Printf("Lining up %s with the board recording...", name)
		camera, err := decodeAudio(path, syncRate, syncFilter, 0, 0)
		if err != nil {
			return sync, nil, err
		}
		v, err := syncVideo(sum, camera, syncRate, defaultSyncConfig)
		if err != nil {
			return sync, nil, fmt.Errorf("%s: %w", name, err)
		}
		v.File = name
		log.Printf("  starts %s into the board recording, clock drift %+.1f ppm, %d audio windows agree within %.1f ms",
			formatTimestamp(v.BoardOffset), v.DriftPPM, v.Windows, v.MaxResidualMS)
		if v.Windows < 3 {
			log.Printf("  Warning: only %d audio windows matched; check the sync of songs from %s.", v.Windows, name)
		}
		sync.Videos = append(sync.Videos, v)
	}

	log.Println("Finding songs...")
	cfg := defaultDetectConfig
	hop := int(levelRate * cfg.FrameSec)
	instruments := instrumentChannels(boards)
	frameLevels := frameLevelsDB(levels, totalChannels(boards), instruments, hop)
	stops := stopScores(levels, totalChannels(boards), instruments, hop, cfg.MaxStopDB)
	sizes := make([]int, len(sets))
	for i, s := range sets {
		sizes[i] = len(s.Songs)
	}
	perSet, splits := detectShowSongs(frameLevels, stops, sizes, cfg)

	songs := flattenSetlist(sets)
	var found []SetlistSong
	var segs []segment
	first := 0
	for si, setSegs := range perSet {
		if len(setSegs) != sizes[si] {
			log.Printf("Warning: found %d of the %d songs in %s; add the rest to %s by hand.",
				len(setSegs), sizes[si], sets[si].Name, cuesFileName)
		}
		for j, seg := range setSegs {
			found = append(found, songs[first+j])
			segs = append(segs, seg)
		}
		first += sizes[si]
	}
	for i := 1; i < len(segs); i++ {
		for _, at := range splits {
			if segs[i].start == at {
				sync.Notes = append(sync.Notes, fmt.Sprintf(
					"Check songs %02d and %02d: they ran together without a gap, so the split between them is a best guess.",
					found[i-1].Number, found[i].Number))
			}
		}
	}
	for _, note := range sync.Notes {
		log.Printf("Note: %s", note)
	}
	segs = padSegments(segs, opts.PreRoll, opts.PostRoll, sync.BoardDuration)

	cues := make([]cue, len(found))
	for i, s := range found {
		cues[i] = cue{Song: s.Number, Set: s.Set + 1, Title: s.Title, Start: segs[i].start, End: segs[i].end}
		if v, ok := videoCovering(sync.Videos, segs[i]); ok {
			cues[i].Video = v.File
			cues[i].Start = v.videoTime(segs[i].start)
			cues[i].End = v.videoTime(segs[i].end)
		}
	}
	log.Printf("Analysis took %s.", time.Since(began).Round(time.Second))
	return sync, cues, nil
}

// cutSongs writes each song's folder: channel WAVs, the camera video, and an empty mix folder.
func cutSongs(opts showOptions, boards []wavInfo, sync showSync, cues []cue) error {
	outDir := filepath.Join(opts.Dir, opts.OutDir)
	for _, c := range cues {
		if len(opts.Songs) > 0 && !opts.Songs[c.Song] {
			continue
		}
		name := songFolderName(c.Song, c.Title)
		folder := filepath.Join(outDir, name)
		for _, dir := range []string{filepath.Join(folder, channelsFolder), filepath.Join(folder, mixFolder)} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}

		boardStart, boardEnd := c.Start, c.End
		if c.Video != "" {
			v, ok := findVideo(sync, c.Video)
			if !ok {
				return fmt.Errorf("song %d: %s is not listed in %s", c.Song, c.Video, syncFileName)
			}
			src := filepath.Join(opts.Dir, opts.VideoDir, c.Video)
			key, err := keyframeAtOrBefore(src, math.Max(c.Start, 0))
			if err != nil {
				return err
			}
			clip := filepath.Join(folder, name+" (camera).mp4")
			if opts.Recut || !fileExists(clip) {
				if err := cutVideo(src, clip, key, math.Min(c.End, v.Duration)-key); err != nil {
					return fmt.Errorf("song %d video: %w", c.Song, err)
				}
			}
			// The clip's audio starts a few milliseconds before its first keyframe, so start the
			// channels at the clip's 0:00 to line them up with it exactly. If the song began
			// before the camera rolled, keep that extra audio instead.
			lead, err := firstVideoTime(clip)
			if err != nil {
				return err
			}
			channelStart := key - lead
			if c.Start < 0 {
				channelStart = c.Start
			}
			boardStart, boardEnd = v.boardTime(channelStart), v.boardTime(c.End)
		}

		for _, b := range boards {
			base := strings.TrimSuffix(filepath.Base(b.Path), filepath.Ext(b.Path))
			dst := filepath.Join(folder, channelsFolder, base+".wav")
			if !opts.Recut && fileExists(dst) {
				continue
			}
			rate := float64(b.SampleRate)
			start := int64(math.Round(boardStart * rate))
			if err := writeWAVSlice(b, dst, start, int64(math.Round(boardEnd*rate))-start); err != nil {
				return err
			}
		}
		log.Printf("Wrote %s", folder)
	}
	return nil
}

// printCues shows the song boundaries as a table.
func printCues(cues []cue) {
	log.Printf("\n  #  %-30s %-14s %-12s %-12s %s", "Song", "Video", "Start", "End", "Length")
	for _, c := range cues {
		video := c.Video
		if video == "" {
			video = "(board only)"
		}
		title := c.Title
		if len(title) > 30 {
			title = title[:29] + "…"
		}
		length := int(math.Round(c.End - c.Start))
		log.Printf("  %02d %-30s %-14s %-12s %-12s %d:%02d", c.Song, title, video,
			formatTimestamp(c.Start), formatTimestamp(c.End), length/60, length%60)
	}
	log.Println()
}

// loadShowSetlist reads the -setlist file, or setlist.md / setlist.txt in the show folder.
func loadShowSetlist(opts showOptions) ([]Set, error) {
	path := opts.Setlist
	if path == "" {
		for _, name := range []string{"setlist.md", "setlist.txt"} {
			if p := filepath.Join(opts.Dir, name); fileExists(p) {
				path = p
				break
			}
		}
		if path == "" {
			return nil, fmt.Errorf("no setlist.md or setlist.txt in %s (use -setlist)", opts.Dir)
		}
	}
	sets, err := readSetlist(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return sets, nil
}

// boardChannels reads the headers of the board's channel WAVs, sorted by file name.
func boardChannels(dir string) ([]wavInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var boards []wavInfo
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.EqualFold(filepath.Ext(e.Name()), ".wav") {
			continue
		}
		info, err := readWAVInfo(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		boards = append(boards, info)
	}
	if len(boards) == 0 {
		return nil, fmt.Errorf("no WAV files in %s", dir)
	}
	sort.Slice(boards, func(a, b int) bool {
		return naturalLess(filepath.Base(boards[a].Path), filepath.Base(boards[b].Path))
	})
	for _, b := range boards[1:] {
		if b.SampleRate != boards[0].SampleRate {
			return nil, fmt.Errorf("%s is %d Hz but %s is %d Hz", filepath.Base(b.Path), b.SampleRate,
				filepath.Base(boards[0].Path), boards[0].SampleRate)
		}
		if math.Abs(b.Duration()-boards[0].Duration()) > 1 {
			log.Printf("Warning: %s and %s differ in length by more than a second.",
				filepath.Base(b.Path), filepath.Base(boards[0].Path))
		}
	}
	return boards, nil
}

// setVideos lists the video files of a folder in natural order ("Set 2" before "Set 10").
func setVideos(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var videos []string
	for _, e := range entries {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") && videoExts[strings.ToLower(filepath.Ext(e.Name()))] {
			videos = append(videos, filepath.Join(dir, e.Name()))
		}
	}
	if len(videos) == 0 {
		return nil, fmt.Errorf("no video files in %s", dir)
	}
	sort.Slice(videos, func(a, b int) bool { return naturalLess(filepath.Base(videos[a]), filepath.Base(videos[b])) })
	return videos, nil
}

// boardDuration returns the length of the shortest board channel.
func boardDuration(boards []wavInfo) float64 {
	d := math.Inf(1)
	for _, b := range boards {
		d = math.Min(d, b.Duration())
	}
	return d
}

func totalChannels(boards []wavInfo) int {
	n := 0
	for _, b := range boards {
		n += b.Channels
	}
	return n
}

// instrumentChannels returns the indexes of the channels whose file names do not look like
// vocal mics. Vocal mics pick up talking and crowd noise between songs, so song detection
// ignores them unless nothing else is left.
func instrumentChannels(boards []wavInfo) []int {
	var use, all []int
	for _, b := range boards {
		vocal := vocalChannelRe.MatchString(filepath.Base(b.Path))
		for c := 0; c < b.Channels; c++ {
			if !vocal {
				use = append(use, len(all))
			}
			all = append(all, len(all))
		}
	}
	if len(use) == 0 {
		return all
	}
	return use
}

// videoCovering returns the video that shows the largest part of a board-time segment.
func videoCovering(videos []videoSync, seg segment) (videoSync, bool) {
	best, bestOverlap := videoSync{}, 0.0
	for _, v := range videos {
		overlap := math.Min(seg.end, v.boardTime(v.Duration)) - math.Max(seg.start, v.BoardOffset)
		if overlap > bestOverlap {
			best, bestOverlap = v, overlap
		}
	}
	return best, bestOverlap > 0
}

func findVideo(sync showSync, file string) (videoSync, bool) {
	for _, v := range sync.Videos {
		if v.File == file {
			return v, true
		}
	}
	return videoSync{}, false
}

func readSync(path string) (showSync, error) {
	var sync showSync
	data, err := os.ReadFile(path)
	if err != nil {
		return sync, err
	}
	if err := json.Unmarshal(data, &sync); err != nil {
		return sync, fmt.Errorf("%s: %w", path, err)
	}
	return sync, nil
}

func writeSync(path string, sync showSync) error {
	data, err := json.MarshalIndent(sync, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// requireTools checks that external commands are installed.
func requireTools(names ...string) error {
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("'%s' was not found in your PATH; install FFmpeg (for example: brew install ffmpeg)", name)
		}
	}
	return nil
}

// parseSongList parses song numbers like "3,7-9" into a set.
func parseSongList(s string) (map[int]bool, error) {
	songs := map[int]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(part, "-")
		a, errA := strconv.Atoi(strings.TrimSpace(lo))
		b, errB := a, error(nil)
		if isRange {
			b, errB = strconv.Atoi(strings.TrimSpace(hi))
		}
		if errA != nil || errB != nil || a <= 0 || b < a {
			return nil, fmt.Errorf("invalid song list %q", s)
		}
		for n := a; n <= b; n++ {
			songs[n] = true
		}
	}
	return songs, nil
}

// naturalLess orders names so that embedded numbers compare by value.
func naturalLess(a, b string) bool {
	pa := numberChunkRe.FindAllString(strings.ToLower(a), -1)
	pb := numberChunkRe.FindAllString(strings.ToLower(b), -1)
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] == pb[i] {
			continue
		}
		na, errA := strconv.Atoi(pa[i])
		nb, errB := strconv.Atoi(pb[i])
		if errA == nil && errB == nil && na != nb {
			return na < nb
		}
		return pa[i] < pb[i]
	}
	return len(pa) < len(pb)
}
