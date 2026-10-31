package assistant

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

func wav(seconds float64, byteRate uint32) []byte {
	n := int(seconds * float64(byteRate))
	b := make([]byte, 44+n)
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+n))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], byteRate/2)
	binary.LittleEndian.PutUint32(b[28:], byteRate)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(n))
	return b
}

func box(kind string, body ...[]byte) []byte {
	size := 8
	for _, b := range body {
		size += len(b)
	}
	out := make([]byte, 8, size)
	binary.BigEndian.PutUint32(out, uint32(size))
	copy(out[4:], kind)
	for _, b := range body {
		out = append(out, b...)
	}
	return out
}

func mvhd(version byte, timescale uint32, duration uint64) []byte {
	if version == 1 {
		b := make([]byte, 32)
		b[0] = 1
		binary.BigEndian.PutUint32(b[20:], timescale)
		binary.BigEndian.PutUint64(b[24:], duration)
		return b
	}
	b := make([]byte, 20)
	binary.BigEndian.PutUint32(b[12:], timescale)
	binary.BigEndian.PutUint32(b[16:], uint32(duration))
	return b
}

// As phones write them: ftyp, then media data, with moov at the end.
func m4a(version byte, timescale uint32, duration uint64) []byte {
	out := box("ftyp", []byte("M4A \x00\x00\x00\x00"))
	out = append(out, box("mdat", make([]byte, 300))...)
	return append(out, box("moov", box("mvhd", mvhd(version, timescale, duration)), box("trak"))...)
}

func TestAudioInfo(t *testing.T) {
	cases := []struct {
		name    string
		data    []byte
		ct      string
		seconds float64
		err     error
	}{
		{"wav 2.5s", wav(2.5, 32000), "audio/wav", 2.5, nil},
		{"m4a v0", m4a(0, 44100, 44100*12), "audio/mp4", 12, nil},
		{"m4a v1", m4a(1, 1000, 31500), "audio/mp4", 31.5, nil},
		{"text", []byte("hello, this is not audio"), "", 0, errUnsupportedAudio},
		{"mp3 is not accepted", append([]byte("ID3\x04"), make([]byte, 100)...), "", 0, errUnsupportedAudio},
		{"m4a without moov", box("ftyp", []byte("M4A \x00\x00\x00\x00")), "audio/mp4", 0, errCorruptAudio},
		{"m4a truncated in moov", m4a(0, 44100, 44100)[:350], "audio/mp4", 0, errCorruptAudio},
		{"m4a zero timescale", m4a(0, 0, 100), "audio/mp4", 0, errCorruptAudio},
		{"wav without fmt", append([]byte("RIFF\x00\x00\x00\x00WAVEdata\x04\x00\x00\x00"), 1, 2, 3, 4), "audio/wav", 0, errCorruptAudio},
	}
	for _, c := range cases {
		ct, s, err := audioInfo(c.data)
		if !errors.Is(err, c.err) || (c.err == nil && (ct != c.ct || math.Abs(s-c.seconds) > 0.01)) {
			t.Errorf("%s: got (%q, %.2f, %v), want (%q, %.2f, %v)", c.name, ct, s, err, c.ct, c.seconds, c.err)
		}
	}
}

// A box claiming more bytes than the file has must not be read past the end.
func TestFindBoxRejectsOversizedBoxes(t *testing.T) {
	b := box("moov", box("mvhd", mvhd(0, 1000, 1000)))
	binary.BigEndian.PutUint32(b, 1<<30)
	if _, ok := findBox(b, "moov"); ok {
		t.Fatal("oversized box accepted")
	}
}
