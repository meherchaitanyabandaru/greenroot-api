package pdfdoc

import _ "embed"

//go:embed logo.jpg
var logoJPEG []byte

const (
	logoWidth  = 480
	logoHeight = 124
	// LogoAspect is width / height (~3.871).
	LogoAspect = float64(logoWidth) / float64(logoHeight)
)

