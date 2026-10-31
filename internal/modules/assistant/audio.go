package assistant

import (
	"bytes"
	"encoding/binary"
	"errors"
)

var (
	errUnsupportedAudio = errors.New("unsupported audio format")
	errCorruptAudio     = errors.New("unreadable audio")
)

// audioInfo checks the recording by its bytes (not the declared type) and
// reads its length from the container header: M4A/MP4 (what the app
// records) or WAV. Returns the type to send on.
func audioInfo(data []byte) (contentType string, seconds float64, err error) {
	switch {
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE":
		s, err := wavSeconds(data)
		return "audio/wav", s, err
	case len(data) >= 8 && string(data[4:8]) == "ftyp":
		s, err := mp4Seconds(data)
		return "audio/mp4", s, err
	}
	return "", 0, errUnsupportedAudio
}

func wavSeconds(data []byte) (float64, error) {
	var byteRate uint32
	for pos := 12; pos+8 <= len(data); {
		id, size := string(data[pos:pos+4]), int(binary.LittleEndian.Uint32(data[pos+4:pos+8]))
		body := pos + 8
		switch id {
		case "fmt ":
			if size < 16 || body+16 > len(data) {
				return 0, errCorruptAudio
			}
			byteRate = binary.LittleEndian.Uint32(data[body+8 : body+12])
		case "data":
			if byteRate == 0 {
				return 0, errCorruptAudio
			}
			// A recorder still writing may leave size 0 or too large: use what is there.
			if size == 0 || body+size > len(data) {
				size = len(data) - body
			}
			return float64(size) / float64(byteRate), nil
		}
		pos = body + size + size%2
	}
	return 0, errCorruptAudio
}

// mp4Seconds reads the movie header (moov/mvhd): duration / timescale.
func mp4Seconds(data []byte) (float64, error) {
	moov, ok := findBox(data, "moov")
	if !ok {
		return 0, errCorruptAudio
	}
	mvhd, ok := findBox(moov, "mvhd")
	if !ok || len(mvhd) < 20 {
		return 0, errCorruptAudio
	}
	var timescale uint32
	var duration uint64
	if mvhd[0] == 1 { // version 1: 64-bit times
		if len(mvhd) < 32 {
			return 0, errCorruptAudio
		}
		timescale = binary.BigEndian.Uint32(mvhd[20:24])
		duration = binary.BigEndian.Uint64(mvhd[24:32])
	} else {
		timescale = binary.BigEndian.Uint32(mvhd[12:16])
		duration = uint64(binary.BigEndian.Uint32(mvhd[16:20]))
	}
	if timescale == 0 {
		return 0, errCorruptAudio
	}
	return float64(duration) / float64(timescale), nil
}

// findBox returns the body of the first box of this type at this level.
func findBox(data []byte, kind string) ([]byte, bool) {
	want := []byte(kind)
	for pos := 0; pos+8 <= len(data); {
		size := uint64(binary.BigEndian.Uint32(data[pos : pos+4]))
		header := uint64(8)
		switch size {
		case 0:
			size = uint64(len(data) - pos)
		case 1:
			if pos+16 > len(data) {
				return nil, false
			}
			size, header = binary.BigEndian.Uint64(data[pos+8:pos+16]), 16
		}
		if size < header || uint64(pos)+size > uint64(len(data)) {
			return nil, false
		}
		if bytes.Equal(data[pos+4:pos+8], want) {
			return data[uint64(pos)+header : uint64(pos)+size], true
		}
		pos += int(size)
	}
	return nil, false
}
