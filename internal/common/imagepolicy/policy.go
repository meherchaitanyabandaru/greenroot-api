package imagepolicy

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers the JPEG decoder for image.Decode
	_ "image/png"  // registers the PNG decoder for image.Decode
	"net/http"
	"slices"
	"time"
)

// Categories of image upload. Each has its own policy row.
const (
	Selfie          = "SELFIE"
	MarketAd        = "MARKET_AD"
	NurseryBranding = "NURSERY_BRANDING"
	DeliveryProof   = "DELIVERY_PROOF"
	LoadingProof    = "LOADING_PROOF"
	DriverLicence   = "DRIVER_LICENCE"
	Attachment      = "ATTACHMENT" // an image attached as a document (admin attachments)
)

// Categories in display order.
var Categories = []string{Selfie, MarketAd, NurseryBranding, DeliveryProof, LoadingProof, DriverLicence, Attachment}

// Formats the API can decode and check. The apps convert HEIC/WebP photos to
// JPEG before upload; PNG stays PNG only when it has transparency.
const (
	JPEG = "JPEG"
	PNG  = "PNG"
)

// HardMaxPixels caps width × height before any image is decoded
// (decompression-bomb guard). Not configurable.
const HardMaxPixels = 40_000_000

const maxDecodes = 4

var decodeSlots = make(chan struct{}, maxDecodes)

// Policy is one category's rules.
type Policy struct {
	Category string `json:"category"`
	// MaxSide: the longest side the app resizes to; uploads longer are refused.
	MaxSide int `json:"max_side"`
	// MinSide: the shortest side an upload must have (blurry tiny photos are refused).
	MinSide int `json:"min_side"`
	// Quality: JPEG quality the app encodes with (1–100).
	Quality  int   `json:"quality"`
	MaxBytes int64 `json:"max_bytes"`
	// AllowedFormats: JPEG and/or PNG.
	AllowedFormats []string `json:"allowed_formats"`
	// IsCompressionEnabled: false lets the app send a photo already within
	// the limits as it is (no re-encoding). Resizing an over-size photo and
	// every server check still apply.
	IsCompressionEnabled bool       `json:"is_compression_enabled"`
	Version              int        `json:"version"`
	UpdatedByUserID      *int64     `json:"updated_by_user_id,omitempty"`
	UpdatedAt            *time.Time `json:"updated_at,omitempty"`
}

// Bounds are the limits an admin cannot go past for a category.
type Bounds struct {
	MaxSideMin, MaxSideMax int
	MinSideMin             int
	QualityMin, QualityMax int
	MaxBytesMin            int64
	MaxBytesMax            int64
}

var commonBounds = Bounds{MaxSideMin: 320, MaxSideMax: 4096, MinSideMin: 64, QualityMin: 50, QualityMax: 95, MaxBytesMin: 50 << 10, MaxBytesMax: 10 << 20}

// BoundsFor returns the safe bounds for category. Licence photos must stay
// readable, so their floors are higher.
func BoundsFor(category string) Bounds {
	b := commonBounds
	if category == DriverLicence {
		b.MaxSideMin, b.MinSideMin, b.QualityMin, b.MaxBytesMin = 1280, 480, 80, 500<<10
	}
	return b
}

// Defaults are the policies used when the database has none (or cannot be read).
var Defaults = map[string]Policy{
	Selfie:          {Category: Selfie, MaxSide: 800, MinSide: 200, Quality: 85, MaxBytes: 1 << 20, AllowedFormats: []string{JPEG, PNG}, IsCompressionEnabled: true},
	MarketAd:        {Category: MarketAd, MaxSide: 1600, MinSide: 400, Quality: 82, MaxBytes: 2 << 20, AllowedFormats: []string{JPEG}, IsCompressionEnabled: true},
	NurseryBranding: {Category: NurseryBranding, MaxSide: 800, MinSide: 200, Quality: 90, MaxBytes: 1 << 20, AllowedFormats: []string{JPEG, PNG}, IsCompressionEnabled: true},
	DeliveryProof:   {Category: DeliveryProof, MaxSide: 2048, MinSide: 480, Quality: 85, MaxBytes: 3 << 20, AllowedFormats: []string{JPEG}, IsCompressionEnabled: true},
	LoadingProof:    {Category: LoadingProof, MaxSide: 2048, MinSide: 480, Quality: 85, MaxBytes: 3 << 20, AllowedFormats: []string{JPEG}, IsCompressionEnabled: true},
	DriverLicence:   {Category: DriverLicence, MaxSide: 2048, MinSide: 600, Quality: 90, MaxBytes: 4 << 20, AllowedFormats: []string{JPEG, PNG}, IsCompressionEnabled: true},
	Attachment:      {Category: Attachment, MaxSide: 4096, MinSide: 200, Quality: 90, MaxBytes: 8 << 20, AllowedFormats: []string{JPEG, PNG}, IsCompressionEnabled: true},
}

func IsCategory(c string) bool { return slices.Contains(Categories, c) }

// Errors of Validate (an unsafe policy) and Check (an image that breaks it).
var (
	ErrUnsafePolicy  = errors.New("unsafe image policy")
	ErrEmpty         = errors.New("empty file")
	ErrTooLarge      = errors.New("file too large")
	ErrFormat        = errors.New("format not allowed")
	ErrMalformed     = errors.New("not a readable image")
	ErrTooManyPixels = errors.New("image has too many pixels")
	ErrTooBig        = errors.New("image larger than allowed")
	ErrTooSmall      = errors.New("image smaller than allowed")
)

// Validate refuses a policy outside its category's bounds.
func Validate(p Policy) error {
	if !IsCategory(p.Category) {
		return fmt.Errorf("%w: unknown category", ErrUnsafePolicy)
	}
	b := BoundsFor(p.Category)
	switch {
	case p.MaxSide < b.MaxSideMin || p.MaxSide > b.MaxSideMax:
		return fmt.Errorf("%w: max_side must be %d–%d", ErrUnsafePolicy, b.MaxSideMin, b.MaxSideMax)
	case p.MinSide < b.MinSideMin || p.MinSide > p.MaxSide:
		return fmt.Errorf("%w: min_side must be %d–max_side", ErrUnsafePolicy, b.MinSideMin)
	case p.Quality < b.QualityMin || p.Quality > b.QualityMax:
		return fmt.Errorf("%w: quality must be %d–%d", ErrUnsafePolicy, b.QualityMin, b.QualityMax)
	case p.MaxBytes < b.MaxBytesMin || p.MaxBytes > b.MaxBytesMax:
		return fmt.Errorf("%w: max_bytes must be %d–%d", ErrUnsafePolicy, b.MaxBytesMin, b.MaxBytesMax)
	case len(p.AllowedFormats) == 0:
		return fmt.Errorf("%w: at least one format", ErrUnsafePolicy)
	}
	for i, f := range p.AllowedFormats {
		if f != JPEG && f != PNG || slices.Contains(p.AllowedFormats[:i], f) {
			return fmt.Errorf("%w: formats are JPEG and PNG", ErrUnsafePolicy)
		}
	}
	return nil
}

// Info is what Check learned about an accepted image.
type Info struct {
	Format      string // JPEG or PNG
	ContentType string
	Ext         string
	Width       int
	Height      int
}

var formats = map[string]struct{ name, ext string }{
	"image/jpeg": {JPEG, ".jpg"},
	"image/png":  {PNG, ".png"},
}

// Check accepts data only if it is a whole, readable image meeting p. The
// type comes from the bytes, never the file name or the client's header.
// Sides are compared as longest/shortest, so a photo whose EXIF says
// "rotate 90°" is judged the same either way up.
func Check(p Policy, data []byte) (Info, error) {
	if len(data) == 0 {
		return Info{}, ErrEmpty
	}
	if int64(len(data)) > p.MaxBytes {
		return Info{}, ErrTooLarge
	}
	ct := http.DetectContentType(data)
	f, ok := formats[ct]
	if !ok {
		return Info{}, ErrFormat
	}
	if !slices.Contains(p.AllowedFormats, f.name) {
		return Info{}, ErrFormat
	}
	// Header only first: the pixel count is known before any memory is spent.
	cfg, decoded, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || formats["image/"+decoded].name != f.name {
		return Info{}, ErrMalformed // e.g. a PNG renamed .jpg, or a broken header
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > HardMaxPixels {
		return Info{}, ErrTooManyPixels
	}
	longest, shortest := max(cfg.Width, cfg.Height), min(cfg.Width, cfg.Height)
	if longest > p.MaxSide {
		return Info{}, ErrTooBig
	}
	if shortest < p.MinSide {
		return Info{}, ErrTooSmall
	}
	// A full decode catches truncated or corrupt pixel data. At most
	// maxDecodes at once: each can take tens of MB (MaxSide² pixels).
	decodeSlots <- struct{}{}
	_, _, err = image.Decode(bytes.NewReader(data))
	<-decodeSlots
	if err != nil {
		return Info{}, ErrMalformed
	}
	return Info{Format: f.name, ContentType: ct, Ext: f.ext, Width: cfg.Width, Height: cfg.Height}, nil
}

// Message is the user-facing reason an image was refused.
func Message(p Policy, err error) string {
	switch {
	case errors.Is(err, ErrEmpty):
		return "The photo is empty. Take it again."
	case errors.Is(err, ErrTooLarge):
		return fmt.Sprintf("The photo is too large (max %d KB). Take it again or choose a smaller one.", p.MaxBytes>>10)
	case errors.Is(err, ErrFormat):
		return "Only photos can be uploaded here (JPG" + map[bool]string{true: " or PNG", false: ""}[slices.Contains(p.AllowedFormats, PNG)] + ")."
	case errors.Is(err, ErrTooManyPixels), errors.Is(err, ErrTooBig):
		return fmt.Sprintf("The photo is too big (max %d pixels on the longest side). Update the app and try again.", p.MaxSide)
	case errors.Is(err, ErrTooSmall):
		return fmt.Sprintf("The photo is too small to read (at least %d pixels on the shortest side). Take it again closer and in good light.", p.MinSide)
	default:
		return "This file is not a readable photo. Take it again."
	}
}
