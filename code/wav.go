package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

// wavInfo describes the sample layout of a WAV file.
type wavInfo struct {
	Path          string
	Channels      int
	SampleRate    int
	BitsPerSample int
	BlockAlign    int
	DataOffset    int64 // byte offset of the first sample frame
	Frames        int64 // complete sample frames in the data chunk
	fmtChunk      []byte
}

// Duration returns the length of the audio in seconds.
func (w wavInfo) Duration() float64 {
	return float64(w.Frames) / float64(w.SampleRate)
}

// readWAVInfo parses the RIFF chunks of a WAV file up to its data chunk.
func readWAVInfo(path string) (wavInfo, error) {
	info := wavInfo{Path: path}
	f, err := os.Open(path)
	if err != nil {
		return info, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return info, err
	}

	var header [12]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return info, fmt.Errorf("%s: not a WAV file", path)
	}
	if string(header[0:4]) == "RF64" {
		return info, fmt.Errorf("%s: RF64 WAV files are not supported", path)
	}
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return info, fmt.Errorf("%s: not a RIFF/WAVE file", path)
	}

	pos := int64(12)
	for {
		var chunk [8]byte
		if _, err := io.ReadFull(f, chunk[:]); err != nil {
			return info, fmt.Errorf("%s: no data chunk found", path)
		}
		id := string(chunk[0:4])
		size := int64(binary.LittleEndian.Uint32(chunk[4:8]))
		pos += 8

		switch id {
		case "fmt ":
			if size < 16 {
				return info, fmt.Errorf("%s: fmt chunk too small", path)
			}
			info.fmtChunk = make([]byte, size)
			if _, err := io.ReadFull(f, info.fmtChunk); err != nil {
				return info, fmt.Errorf("%s: reading fmt chunk: %w", path, err)
			}
			info.Channels = int(binary.LittleEndian.Uint16(info.fmtChunk[2:4]))
			info.SampleRate = int(binary.LittleEndian.Uint32(info.fmtChunk[4:8]))
			info.BlockAlign = int(binary.LittleEndian.Uint16(info.fmtChunk[12:14]))
			info.BitsPerSample = int(binary.LittleEndian.Uint16(info.fmtChunk[14:16]))
			if _, err := f.Seek(size%2, io.SeekCurrent); err != nil {
				return info, err
			}
		case "data":
			if info.fmtChunk == nil {
				return info, fmt.Errorf("%s: data chunk comes before the fmt chunk", path)
			}
			if info.BlockAlign <= 0 || info.SampleRate <= 0 || info.Channels <= 0 {
				return info, fmt.Errorf("%s: invalid fmt chunk", path)
			}
			// Recorders can leave a bogus size or a partial last frame, so trust the file length.
			if available := st.Size() - pos; size > available {
				size = available
			}
			info.DataOffset = pos
			info.Frames = size / int64(info.BlockAlign)
			return info, nil
		default:
			if _, err := f.Seek(size+size%2, io.SeekCurrent); err != nil {
				return info, err
			}
		}
		pos += size + size%2
	}
}

// writeWAVSlice writes frames [start, start+count) of src to a new WAV file at dst. Frames
// outside the source are filled with silence, so every slice has exactly count frames and
// slices of different channels stay aligned.
func writeWAVSlice(src wavInfo, dst string, start, count int64) error {
	if count <= 0 {
		return fmt.Errorf("slice of %s has no frames", src.Path)
	}
	align := int64(src.BlockAlign)
	dataBytes := count * align
	fmtPad := int64(len(src.fmtChunk) % 2)
	riffSize := 4 + 8 + int64(len(src.fmtChunk)) + fmtPad + 8 + dataBytes + dataBytes%2
	if riffSize > math.MaxUint32 {
		return fmt.Errorf("slice of %s is too long for a WAV file", src.Path)
	}

	in, err := os.Open(src.Path)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".partial"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)

	w := bufio.NewWriterSize(out, 1<<20)
	w.WriteString("RIFF")
	binary.Write(w, binary.LittleEndian, uint32(riffSize))
	w.WriteString("WAVEfmt ")
	binary.Write(w, binary.LittleEndian, uint32(len(src.fmtChunk)))
	w.Write(src.fmtChunk)
	if fmtPad == 1 {
		w.WriteByte(0)
	}
	w.WriteString("data")
	binary.Write(w, binary.LittleEndian, uint32(dataBytes))

	silence := byte(0)
	if src.BitsPerSample == 8 {
		silence = 0x80 // 8-bit PCM is unsigned
	}
	lead := min(max(-start, 0), count)
	readFrom := max(start, 0)
	readCount := max(min(start+count, src.Frames)-readFrom, 0)
	tail := count - lead - readCount

	err = writeFill(w, silence, lead*align)
	if err == nil && readCount > 0 {
		if _, err = in.Seek(src.DataOffset+readFrom*align, io.SeekStart); err == nil {
			_, err = io.CopyN(w, in, readCount*align)
		}
	}
	if err == nil {
		err = writeFill(w, silence, tail*align)
	}
	if err == nil && dataBytes%2 == 1 {
		err = w.WriteByte(0)
	}
	if err == nil {
		err = w.Flush()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("writing %s: %w", dst, err)
	}
	return os.Rename(tmp, dst)
}

// writeFill writes n copies of b.
func writeFill(w io.Writer, b byte, n int64) error {
	buf := make([]byte, min(n, 1<<16))
	for i := range buf {
		buf[i] = b
	}
	for n > 0 {
		k := min(n, int64(len(buf)))
		if _, err := w.Write(buf[:k]); err != nil {
			return err
		}
		n -= k
	}
	return nil
}
