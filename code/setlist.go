package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// Set is one set of a show with its songs in play order.
type Set struct {
	Name  string
	Songs []string
}

// SetlistSong is a song with its position in the whole show.
type SetlistSong struct {
	Number int // 1-based across all sets
	Set    int // 0-based index into the sets
	Title  string
}

var (
	setHeaderRe   = regexp.MustCompile(`(?i)^(set\s*\d+|encore(\s*\d+)?)\s*:?$`)
	listPrefixRe  = regexp.MustCompile(`^([-*+]\s+|\d+[.)]\s+)`)
	unsafeNameRe  = regexp.MustCompile(`[/\\:*?"<>|\x00-\x1f]`)
	spaceRunRe    = regexp.MustCompile(`\s+`)
	quoteReplacer = strings.NewReplacer("‘", "'", "’", "'", "“", `"`, "”", `"`)
)

// readSetlist loads a setlist file; see parseSetlist for the format.
func readSetlist(path string) ([]Set, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseSetlist(f)
}

// parseSetlist reads one song title per line. Lines like "SET 1:" or "Encore" start a new
// set, and songs before any header belong to "Set 1". Other markdown headings are ignored,
// bullets and numbering are stripped, and curly quotes are straightened.
func parseSetlist(r io.Reader) ([]Set, error) {
	var sets []Set
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(quoteReplacer.Replace(scanner.Text()))
		if strings.HasPrefix(line, "#") {
			line = strings.TrimSpace(strings.TrimLeft(line, "#"))
			if setHeaderRe.MatchString(line) {
				sets = append(sets, Set{Name: strings.TrimSpace(strings.TrimSuffix(line, ":"))})
			}
			continue
		}
		if setHeaderRe.MatchString(line) {
			sets = append(sets, Set{Name: strings.TrimSpace(strings.TrimSuffix(line, ":"))})
			continue
		}
		line = strings.TrimSpace(listPrefixRe.ReplaceAllString(line, ""))
		if line == "" {
			continue
		}
		if len(sets) == 0 {
			sets = append(sets, Set{Name: "Set 1"})
		}
		last := &sets[len(sets)-1]
		last.Songs = append(last.Songs, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	nonEmpty := sets[:0]
	for _, s := range sets {
		if len(s.Songs) > 0 {
			nonEmpty = append(nonEmpty, s)
		}
	}
	if len(nonEmpty) == 0 {
		return nil, fmt.Errorf("setlist has no songs")
	}
	return nonEmpty, nil
}

// flattenSetlist numbers the songs of all sets in play order.
func flattenSetlist(sets []Set) []SetlistSong {
	var songs []SetlistSong
	for si, s := range sets {
		for _, title := range s.Songs {
			songs = append(songs, SetlistSong{Number: len(songs) + 1, Set: si, Title: title})
		}
	}
	return songs
}

// songFolderName builds a readable folder name like "07 - Creep" that is safe on macOS,
// Windows, and cloud drives.
func songFolderName(number int, title string) string {
	name := unsafeNameRe.ReplaceAllString(title, "")
	name = strings.Trim(spaceRunRe.ReplaceAllString(name, " "), " .")
	if name == "" {
		name = "Untitled"
	}
	return fmt.Sprintf("%02d - %s", number, name)
}
