package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

// cue holds one song's boundaries in the timeline of the set video that shows it.
type cue struct {
	Song  int
	Set   int
	Title string
	Video string  // file name in the show's video folder
	Start float64 // seconds in the video; negative if the song began before the camera rolled
	End   float64
}

var cueHeader = []string{"song", "set", "title", "video", "start", "end"}

const cueComment = `# Song boundaries. Times are H:MM:SS.mmm in the set video named in the "video" column,
# or on the board recording when that column is empty.
# To fix a split, edit start/end (check them in any video player), then run
# "splitter show prepare -recut -songs N <show folder>" for those songs.
# Lines starting with # are ignored.
`

// writeCues saves cues as an editable CSV file, with notes added as comment lines.
func writeCues(path string, cues []cue, notes ...string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	w.WriteString(cueComment)
	for _, note := range notes {
		w.WriteString("# " + note + "\n")
	}
	c := csv.NewWriter(w)
	c.Write(cueHeader)
	for _, q := range cues {
		c.Write([]string{
			fmt.Sprintf("%02d", q.Song),
			strconv.Itoa(q.Set),
			q.Title,
			q.Video,
			formatTimestamp(q.Start),
			formatTimestamp(q.End),
		})
	}
	c.Flush()
	if err := c.Error(); err != nil {
		f.Close()
		return err
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// readCues loads a cue file written by writeCues, possibly edited by hand.
func readCues(path string) ([]cue, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.Comment = '#'
	r.FieldsPerRecord = len(cueHeader)
	r.TrimLeadingSpace = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	var cues []cue
	seen := map[int]bool{}
	for i, row := range rows {
		if i == 0 && strings.EqualFold(row[0], cueHeader[0]) {
			continue
		}
		line := fmt.Sprintf("%s: row %d", path, i+1)
		song, err := strconv.Atoi(strings.TrimSpace(row[0]))
		if err != nil || song <= 0 {
			return nil, fmt.Errorf("%s: invalid song number %q", line, row[0])
		}
		if seen[song] {
			return nil, fmt.Errorf("%s: song %d is listed twice", line, song)
		}
		seen[song] = true
		set, err := strconv.Atoi(strings.TrimSpace(row[1]))
		if err != nil {
			return nil, fmt.Errorf("%s: invalid set number %q", line, row[1])
		}
		start, err := parseTimestamp(row[4])
		if err != nil {
			return nil, fmt.Errorf("%s: start: %w", line, err)
		}
		end, err := parseTimestamp(row[5])
		if err != nil {
			return nil, fmt.Errorf("%s: end: %w", line, err)
		}
		if end <= start {
			return nil, fmt.Errorf("%s: end %s is not after start %s", line, row[5], row[4])
		}
		cues = append(cues, cue{
			Song:  song,
			Set:   set,
			Title: strings.TrimSpace(row[2]),
			Video: strings.TrimSpace(row[3]),
			Start: start,
			End:   end,
		})
	}
	sort.Slice(cues, func(a, b int) bool { return cues[a].Song < cues[b].Song })
	return cues, nil
}

// formatTimestamp renders seconds as H:MM:SS.mmm, with a leading minus for negative times.
func formatTimestamp(sec float64) string {
	sign := ""
	if sec < 0 {
		sign = "-"
		sec = -sec
	}
	ms := int64(math.Round(sec * 1000))
	return fmt.Sprintf("%s%d:%02d:%02d.%03d", sign, ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

// parseTimestamp accepts H:MM:SS.mmm, M:SS.mmm, or plain seconds.
func parseTimestamp(s string) (float64, error) {
	s = strings.TrimSpace(s)
	sign := 1.0
	if strings.HasPrefix(s, "-") {
		sign = -1
		s = s[1:]
	}
	parts := strings.Split(s, ":")
	if s == "" || len(parts) > 3 {
		return 0, fmt.Errorf("invalid time %q", s)
	}
	total := 0.0
	for i, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v < 0 || (i < len(parts)-1 && v != math.Trunc(v)) {
			return 0, fmt.Errorf("invalid time %q", s)
		}
		total = total*60 + v
	}
	return sign * total, nil
}
