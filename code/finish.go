package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var mixExts = map[string]bool{".wav": true, ".aif": true, ".aiff": true, ".flac": true, ".m4a": true, ".mp3": true, ".caf": true}

// finishShow builds the final video of every song whose mix folder holds a mixdown.
func finishShow(opts showOptions) error {
	outDir := filepath.Join(opts.Dir, opts.OutDir)
	cues, err := readCues(filepath.Join(outDir, cuesFileName))
	if err != nil {
		return fmt.Errorf("%w (run prepare first)", err)
	}

	var built, current, failed int
	var waiting, unprepared []int
	for _, c := range cues {
		if len(opts.Songs) > 0 && !opts.Songs[c.Song] {
			continue
		}
		name := songFolderName(c.Song, c.Title)
		folder := filepath.Join(outDir, name)
		clip := filepath.Join(folder, name+" (camera).mp4")
		if !fileExists(clip) {
			unprepared = append(unprepared, c.Song)
			continue
		}
		mix, err := findMixdown(filepath.Join(folder, mixFolder))
		if err != nil {
			return err
		}
		if mix == "" {
			waiting = append(waiting, c.Song)
			continue
		}
		dst := filepath.Join(folder, name+".mp4")
		if !opts.Force && newerThan(dst, mix, clip) {
			current++
			continue
		}

		align, reference, err := lineUpMix(folder, clip, mix, c.Start, opts.MaxShift)
		if err != nil {
			log.Printf("%s: could not line up %s with the video: %v", name, filepath.Base(mix), err)
			failed++
			continue
		}
		dur, err := probeDuration(clip)
		if err != nil {
			return err
		}
		if err := muxWithMix(clip, mix, dst, align.Start, dur); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		note := ""
		if align.Windows < 3 {
			note = "; the match was weak, so check the sync"
		}
		log.Printf("%s: wrote %s, mixdown offset %+.3fs (matched to the %s)%s",
			name, filepath.Base(dst), align.Start, reference, note)
		built++
	}

	if len(waiting) > 0 {
		log.Printf("Waiting for a mixdown in %s/: songs %s", mixFolder, joinSongs(waiting))
	}
	if len(unprepared) > 0 {
		log.Printf("Not prepared yet (run prepare): songs %s", joinSongs(unprepared))
	}
	log.Printf("Built %d, already up to date %d, failed %d.", built, current, failed)
	if failed > 0 {
		return fmt.Errorf("%d song(s) could not be finished", failed)
	}
	return nil
}

// lineUpMix finds the time in the mixdown that matches the first frame of the song video,
// and names what the mixdown was matched against. It prefers the song's channel WAVs, which
// the mixdown was made from and which prepare cut to start with the video, and falls back
// to the camera audio if they are gone.
func lineUpMix(folder, clip, mix string, cueStart, maxShift float64) (mixAlignment, string, error) {
	mixAudio, err := decodeAudio(mix, syncRate, syncFilter, 0, 0)
	if err != nil {
		return mixAlignment{}, "", err
	}
	ref, refAtVideo, reference, err := songReference(folder, clip, cueStart)
	if err != nil {
		return mixAlignment{}, "", err
	}
	align, err := alignMix(ref, mixAudio, syncRate, maxShift, defaultSyncConfig)
	if err != nil {
		return mixAlignment{}, reference, err
	}
	align.Start += refAtVideo
	return align, reference, nil
}

// songReference decodes the audio a mixdown is matched against. It also returns the time in
// that audio at the video's first frame and a description for the log.
func songReference(folder, clip string, cueStart float64) ([]float32, float64, string, error) {
	channels, err := boardChannels(filepath.Join(folder, channelsFolder))
	if err != nil {
		log.Printf("  No channel WAVs to match against (%v); using the camera audio.", err)
		camera, err := decodeAudio(clip, syncRate, syncFilter, 0, 0)
		return camera, 0, "camera audio", err
	}
	sum, _, err := decodeBoard(channels, levelRate)
	if err != nil {
		return nil, 0, "", err
	}
	atVideo := 0.0
	if cueStart < 0 {
		// The song began before the camera rolled and prepare kept that audio, so the
		// channels start before the video.
		lead, err := firstVideoTime(clip)
		if err != nil {
			return nil, 0, "", err
		}
		atVideo = -cueStart - lead
	}
	return sum, atVideo, "channel WAVs", nil
}

// findMixdown returns the newest audio file in dir, or "" if there is none.
func findMixdown(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	type candidate struct {
		path    string
		modTime time.Time
	}
	var files []candidate
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !mixExts[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return "", err
		}
		files = append(files, candidate{filepath.Join(dir, e.Name()), info.ModTime()})
	}
	if len(files) == 0 {
		return "", nil
	}
	sort.Slice(files, func(a, b int) bool { return files[a].modTime.After(files[b].modTime) })
	if len(files) > 1 {
		log.Printf("  %d audio files in %s; using the newest, %s", len(files), dir, filepath.Base(files[0].path))
	}
	return files[0].path, nil
}

// newerThan reports whether path exists and was modified after every source.
func newerThan(path string, sources ...string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	for _, src := range sources {
		s, err := os.Stat(src)
		if err != nil || !info.ModTime().After(s.ModTime()) {
			return false
		}
	}
	return true
}

// joinSongs formats song numbers like "01, 05, 12".
func joinSongs(songs []int) string {
	parts := make([]string, len(songs))
	for i, n := range songs {
		parts[i] = fmt.Sprintf("%02d", n)
	}
	return strings.Join(parts, ", ")
}
