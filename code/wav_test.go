package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// frameBytes encodes frame f of a test signal: channel c holds the value f*channels+c.
func frameBytes(f int64, channels, bytesPerSample int) []byte {
	out := make([]byte, 0, channels*bytesPerSample)
	for c := 0; c < channels; c++ {
		var v [4]byte
		binary.LittleEndian.PutUint32(v[:], uint32(f*int64(channels)+int64(c)))
		out = append(out, v[:bytesPerSample]...)
	}
	return out
}

// writeTestWAV writes a PCM WAV with a LIST chunk before the data and stray bytes after the
// last whole frame, like the board recorder's files.
func writeTestWAV(t *testing.T, path string, channels, rate, bits int, frames int64, stray int) {
	t.Helper()
	bytesPer := bits / 8
	align := channels * bytesPer
	var data bytes.Buffer
	for f := int64(0); f < frames; f++ {
		data.Write(frameBytes(f, channels, bytesPer))
	}
	data.Write(make([]byte, stray))

	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(4+8+16+8+6+8+data.Len()))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, uint16(channels))
	binary.Write(&b, binary.LittleEndian, uint32(rate))
	binary.Write(&b, binary.LittleEndian, uint32(rate*align))
	binary.Write(&b, binary.LittleEndian, uint16(align))
	binary.Write(&b, binary.LittleEndian, uint16(bits))
	b.WriteString("LIST")
	binary.Write(&b, binary.LittleEndian, uint32(5))
	b.Write([]byte{'I', 'N', 'F', 'O', 0, 0}) // odd size plus pad byte
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(data.Len()))
	b.Write(data.Bytes())
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatalf("writing test WAV: %v", err)
	}
}

func TestReadWAVInfo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.wav")
	writeTestWAV(t, path, 2, 48000, 24, 1000, 2)

	info, err := readWAVInfo(path)
	if err != nil {
		t.Fatalf("readWAVInfo failed: %v", err)
	}
	if info.Channels != 2 || info.SampleRate != 48000 || info.BitsPerSample != 24 || info.BlockAlign != 6 {
		t.Errorf("Unexpected format: %+v", info)
	}
	if info.Frames != 1000 {
		t.Errorf("Expected 1000 whole frames, got %d", info.Frames)
	}
	if info.DataOffset != 12+8+16+8+6+8 {
		t.Errorf("Expected data offset 58, got %d", info.DataOffset)
	}
}

func TestReadWAVInfoRejectsOtherFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "video.mp4")
	os.WriteFile(path, []byte("\x00\x00\x00\x18ftypisom not a wav"), 0o644)
	if _, err := readWAVInfo(path); err == nil {
		t.Error("Expected an error for a non-WAV file")
	}
}

func TestWriteWAVSlice(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "board.wav")
	const frames = 1000
	writeTestWAV(t, srcPath, 2, 48000, 24, frames, 2)
	src, err := readWAVInfo(srcPath)
	if err != nil {
		t.Fatalf("readWAVInfo failed: %v", err)
	}

	testCases := []struct {
		name         string
		start, count int64
	}{
		{"Middle", 100, 200},
		{"BeforeStart", -50, 100},
		{"PastEnd", 950, 101},
		{"EntirelyAfter", 2000, 10},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dst := filepath.Join(dir, tc.name+".wav")
			if err := writeWAVSlice(src, dst, tc.start, tc.count); err != nil {
				t.Fatalf("writeWAVSlice failed: %v", err)
			}
			got, err := readWAVInfo(dst)
			if err != nil {
				t.Fatalf("reading slice failed: %v", err)
			}
			if got.Frames != tc.count || got.Channels != 2 || got.BitsPerSample != 24 {
				t.Fatalf("Unexpected slice layout: %+v", got)
			}

			raw, _ := os.ReadFile(dst)
			data := raw[got.DataOffset : got.DataOffset+tc.count*6]
			var expected bytes.Buffer
			for i := int64(0); i < tc.count; i++ {
				if f := tc.start + i; f >= 0 && f < frames {
					expected.Write(frameBytes(f, 2, 3))
				} else {
					expected.Write(make([]byte, 6))
				}
			}
			if !bytes.Equal(data, expected.Bytes()) {
				t.Error("Slice samples do not match the source frames")
			}
			if _, err := os.Stat(dst + ".partial"); !os.IsNotExist(err) {
				t.Error("Temporary .partial file was left behind")
			}
		})
	}
}
