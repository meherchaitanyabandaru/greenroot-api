package imagepolicy

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func jpg(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)), &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func pngImg(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.NRGBA{A: 0}) // transparent corner
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// bomb is a valid tiny PNG whose header claims w×h pixels (CRC fixed up).
func bomb(t *testing.T, w, h uint32) []byte {
	data := pngImg(t, 1, 1)
	ihdr := 8 // after the PNG signature: length(4) type(4) data(13) crc(4)
	binary.BigEndian.PutUint32(data[ihdr+8:], w)
	binary.BigEndian.PutUint32(data[ihdr+12:], h)
	binary.BigEndian.PutUint32(data[ihdr+21:], crc32.ChecksumIEEE(data[ihdr+4:ihdr+21]))
	return data
}

func TestCheck(t *testing.T) {
	ad := Defaults[MarketAd] // JPEG only, 400–1600
	logo := Defaults[NurseryBranding]
	var g bytes.Buffer
	_ = gif.Encode(&g, image.NewPaletted(image.Rect(0, 0, 500, 500), []color.Color{color.Black}), nil)
	whole := jpg(t, 1200, 900)
	cases := []struct {
		name string
		p    Policy
		data []byte
		want error
	}{
		{"a photo within the limits", ad, whole, nil},
		{"portrait is judged by its shortest side", ad, jpg(t, 450, 1600), nil},
		{"empty", ad, nil, ErrEmpty},
		{"too small to read", ad, jpg(t, 640, 300), ErrTooSmall},
		{"longer than the app resizes to", ad, jpg(t, 2000, 1000), ErrTooBig},
		{"PNG where only JPEG is allowed", ad, pngImg(t, 800, 600), ErrFormat},
		{"PNG logo with transparency", logo, pngImg(t, 600, 600), nil},
		{"GIF", logo, g.Bytes(), ErrFormat},
		{"text pretending to be a photo", ad, []byte("hello, I am a jpeg"), ErrFormat},
		{"truncated JPEG", ad, whole[:len(whole)/2], ErrMalformed},
		{"over the byte limit", Policy{Category: MarketAd, MaxSide: 1600, MinSide: 400, MaxBytes: 1000, AllowedFormats: []string{JPEG}}, whole, ErrTooLarge},
		{"decompression bomb header", logo, bomb(t, 50000, 50000), ErrTooManyPixels},
		{"oversize header under the pixel cap", logo, bomb(t, 4000, 4000), ErrTooBig},
	}
	for _, c := range cases {
		info, err := Check(c.p, c.data)
		if !errors.Is(err, c.want) || (c.want == nil && err != nil) {
			t.Errorf("%s: want %v, got %v", c.name, c.want, err)
		}
		if c.want == nil && (info.Width == 0 || info.Ext == "" || Message(c.p, err) == "") {
			t.Errorf("%s: incomplete info %+v", c.name, info)
		}
	}
}

func TestValidate(t *testing.T) {
	for _, d := range Defaults {
		if err := Validate(d); err != nil {
			t.Errorf("default %s is unsafe: %v", d.Category, err)
		}
	}
	base := Defaults[MarketAd]
	lic := Defaults[DriverLicence]
	edit := func(p Policy, f func(*Policy)) Policy { f(&p); return p }
	bad := map[string]Policy{
		"unknown category":        edit(base, func(p *Policy) { p.Category = "MEME" }),
		"max side too big":        edit(base, func(p *Policy) { p.MaxSide = 10000 }),
		"min side above max":      edit(base, func(p *Policy) { p.MinSide = p.MaxSide + 1 }),
		"quality 100":             edit(base, func(p *Policy) { p.Quality = 100 }),
		"quality 10":              edit(base, func(p *Policy) { p.Quality = 10 }),
		"50 MB":                   edit(base, func(p *Policy) { p.MaxBytes = 50 << 20 }),
		"no formats":              edit(base, func(p *Policy) { p.AllowedFormats = nil }),
		"GIF":                     edit(base, func(p *Policy) { p.AllowedFormats = []string{"GIF"} }),
		"duplicate format":        edit(base, func(p *Policy) { p.AllowedFormats = []string{JPEG, JPEG} }),
		"unreadable licence":      edit(lic, func(p *Policy) { p.MaxSide = 800; p.MinSide = 400 }),
		"over-compressed licence": edit(lic, func(p *Policy) { p.Quality = 60 }),
	}
	for name, p := range bad {
		if err := Validate(p); !errors.Is(err, ErrUnsafePolicy) {
			t.Errorf("%s: want ErrUnsafePolicy, got %v", name, err)
		}
	}
	if err := Validate(edit(base, func(p *Policy) { p.Quality = 60; p.IsCompressionEnabled = false })); err != nil {
		t.Errorf("a market ad at quality 60 is allowed: %v", err)
	}
}

func TestStoreFallsBackToDefaults(t *testing.T) {
	s := NewStore(nil)
	if got := s.Get(t.Context(), DriverLicence); got.MinSide != Defaults[DriverLicence].MinSide {
		t.Errorf("no database: want the default licence policy, got %+v", got)
	}
	if len(s.All(t.Context())) != len(Categories) {
		t.Error("every category has a policy")
	}
}
