package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// syncRate and syncFilter shape the audio used to line recordings up: a band that
	// microphones and board channels share, at a rate that keeps correlation cheap.
	syncRate   = 8000
	syncFilter = "highpass=f=150,lowpass=f=3800"
)

// runTool runs an external command and returns its stdout; errors include its stderr.
func runTool(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 2000 {
			msg = "..." + msg[len(msg)-2000:]
		}
		return nil, fmt.Errorf("%s failed: %w\n%s", name, err, msg)
	}
	return stdout.Bytes(), nil
}

// probeDuration returns the duration of a media file in seconds.
func probeDuration(path string) (float64, error) {
	out, err := runTool("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
}

// probeVideoCodec returns the codec name of the first video stream, such as "hevc".
func probeVideoCodec(path string) (string, error) {
	out, err := runTool("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=codec_name", "-of", "csv=p=0", path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// keyframeAtOrBefore returns the time of the last video keyframe at or before sec.
func keyframeAtOrBefore(path string, sec float64) (float64, error) {
	if sec <= 0 {
		return 0, nil
	}
	for _, lookback := range []float64{10, 60} {
		from := math.Max(sec-lookback, 0)
		out, err := runTool("ffprobe", "-v", "error", "-select_streams", "v:0",
			"-read_intervals", fmt.Sprintf("%.3f%%%.3f", from, sec+0.5),
			"-show_entries", "packet=pts_time,flags", "-of", "csv=p=0", path)
		if err != nil {
			return 0, err
		}
		best, found := 0.0, false
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Split(strings.TrimSpace(line), ",")
			if len(fields) < 2 || !strings.Contains(fields[1], "K") {
				continue
			}
			t, err := strconv.ParseFloat(fields[0], 64)
			if err == nil && t <= sec+1e-6 && (!found || t > best) {
				best, found = t, true
			}
		}
		if found {
			return best, nil
		}
		if from == 0 {
			return 0, nil
		}
	}
	return 0, fmt.Errorf("%s: no keyframe found before %s", path, formatTimestamp(sec))
}

// firstVideoTime returns the timestamp of a file's first video packet. A stream-copied cut
// starts its audio slightly before the first keyframe, so this is a few milliseconds past zero.
func firstVideoTime(path string) (float64, error) {
	out, err := runTool("ffprobe", "-v", "error", "-select_streams", "v:0", "-read_intervals", "%+#1",
		"-show_entries", "packet=pts_time", "-of", "csv=p=0", path)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(strings.ReplaceAll(string(out), ",", " "))
	if len(fields) == 0 {
		return 0, fmt.Errorf("%s: no video packets", path)
	}
	return strconv.ParseFloat(fields[0], 64)
}

// decodeAudio decodes the first audio stream of path to mono float32 samples at rate, after
// an optional ffmpeg audio filter chain. A positive duration limits how much is read.
func decodeAudio(path string, rate int, filter string, start, duration float64) ([]float32, error) {
	args := []string{"-v", "error", "-nostdin"}
	if start > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", start))
	}
	args = append(args, "-i", path)
	if duration > 0 {
		args = append(args, "-t", fmt.Sprintf("%.3f", duration))
	}
	chain := fmt.Sprintf("aresample=%d", rate)
	if filter != "" {
		chain = filter + "," + chain
	}
	args = append(args, "-vn", "-map", "0:a:0", "-af", chain, "-ac", "1",
		"-c:a", "pcm_f32le", "-f", "f32le", "pipe:1")
	out, err := runTool("ffmpeg", args...)
	if err != nil {
		return nil, err
	}
	return float32sFromBytes(out), nil
}

// decodeBoard reads all board channel files in one pass. It returns a band-limited mono
// sum at syncRate for syncing, and every channel interleaved at levelRate for detection.
func decodeBoard(files []wavInfo, levelRate int) (sum, levels []float32, err error) {
	tmp, err := os.MkdirTemp("", "splitter-board-")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(tmp)

	args := []string{"-v", "error", "-nostdin", "-y"}
	var graph strings.Builder
	total := 0
	for i, f := range files {
		args = append(args, "-i", f.Path)
		fmt.Fprintf(&graph, "[%d:a]", i)
		total += f.Channels
	}
	if len(files) > 1 {
		fmt.Fprintf(&graph, "amerge=inputs=%d,", len(files))
	}
	terms := make([]string, total)
	for c := range terms {
		terms[c] = fmt.Sprintf("c%d", c)
	}
	fmt.Fprintf(&graph, "asplit=2[m][s];[m]aresample=%d[levels];[s]pan=mono|c0=%s,%s,aresample=%d[sum]",
		levelRate, strings.Join(terms, "+"), syncFilter, syncRate)

	sumPath := filepath.Join(tmp, "sum.f32")
	levelsPath := filepath.Join(tmp, "levels.f32")
	args = append(args, "-filter_complex", graph.String(),
		"-map", "[levels]", "-c:a", "pcm_f32le", "-f", "f32le", levelsPath,
		"-map", "[sum]", "-c:a", "pcm_f32le", "-f", "f32le", sumPath)
	if _, err := runTool("ffmpeg", args...); err != nil {
		return nil, nil, err
	}

	raw, err := os.ReadFile(sumPath)
	if err != nil {
		return nil, nil, err
	}
	sum = float32sFromBytes(raw)
	raw, err = os.ReadFile(levelsPath)
	if err != nil {
		return nil, nil, err
	}
	return sum, float32sFromBytes(raw), nil
}

// float32sFromBytes converts little-endian float32 samples.
func float32sFromBytes(b []byte) []float32 {
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return out
}

// cutVideo copies dur seconds of the first video and audio streams of src into an MP4 at
// dst without re-encoding. start should be a keyframe time so the cut begins exactly there.
func cutVideo(src, dst string, start, dur float64) error {
	codec, err := probeVideoCodec(src)
	if err != nil {
		return err
	}
	tmp := dst + ".partial"
	defer os.Remove(tmp)
	args := []string{"-v", "error", "-nostdin", "-y",
		// Seek just past the keyframe so rounding cannot land on the one before it.
		"-ss", fmt.Sprintf("%.3f", start+0.01), "-i", src,
		"-t", fmt.Sprintf("%.3f", dur),
		"-map", "0:v:0", "-map", "0:a:0?", "-c", "copy", "-avoid_negative_ts", "make_zero"}
	args = append(args, videoTagArgs(codec)...)
	args = append(args, "-movflags", "+faststart", "-f", "mp4", tmp)
	if _, err := runTool("ffmpeg", args...); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// muxWithMix writes dst with the video stream of video and the audio of mix encoded as AAC.
// mixStart is the time in the mix that lines up with the first frame of the video.
func muxWithMix(video, mix, dst string, mixStart, videoDur float64) error {
	codec, err := probeVideoCodec(video)
	if err != nil {
		return err
	}
	shift := "anull"
	if mixStart > 0 {
		shift = fmt.Sprintf("atrim=start=%.6f,asetpts=PTS-STARTPTS", mixStart)
	} else if mixStart < 0 {
		shift = fmt.Sprintf("adelay=delays=%.3f:all=1", -mixStart*1000)
	}
	tmp := dst + ".partial"
	defer os.Remove(tmp)
	args := []string{"-v", "error", "-nostdin", "-y", "-i", video, "-i", mix,
		"-filter_complex", fmt.Sprintf("[1:a]%s,aresample=48000,apad[mix]", shift),
		"-map", "0:v:0", "-map", "[mix]", "-c:v", "copy"}
	args = append(args, videoTagArgs(codec)...)
	args = append(args, "-c:a", "aac", "-b:a", "320k", "-t", fmt.Sprintf("%.3f", videoDur),
		"-movflags", "+faststart", "-f", "mp4", tmp)
	if _, err := runTool("ffmpeg", args...); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// videoTagArgs tags HEVC video as hvc1, which QuickTime and Apple devices require.
func videoTagArgs(codec string) []string {
	if codec == "hevc" {
		return []string{"-tag:v", "hvc1"}
	}
	return nil
}
