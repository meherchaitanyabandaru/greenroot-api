package storage

import "testing"

func TestUploadRules(t *testing.T) {
	cases := []struct {
		bucket, contentType, ext string
		ok                       bool
	}{
		{"market-ads", "image/jpeg", ".jpg", true},
		{"market-ads", "image/jpg", ".jpg", true}, // alias
		{"nursery-logos", "IMAGE/PNG; charset=binary", ".png", true},
		{"delivery-photos", "image/webp", ".webp", true},
		{"attachments", "application/pdf", ".pdf", true},
		{"attachments", "application/octet-stream", ".bin", true},
		{"market-ads", "text/html", "", false},     // a page served from a public bucket
		{"market-ads", "image/svg+xml", "", false}, // SVG can carry script
		{"market-ads", "application/pdf", "", false},
		{"delivery-photos", "application/pdf", "", false},
		{"attachments", "text/html", "", false},
		{"plant-images", "image/jpeg", "", false}, // server-only bucket
		{"quotation-pdfs", "application/pdf", "", false},
		{"profile-images", "image/jpeg", "", false}, // selfies go through /users/me/avatar
	}
	for _, c := range cases {
		ext, ok := uploadExtension(c.bucket, normalizeContentType(c.contentType))
		if ok != c.ok || ext != c.ext {
			t.Errorf("%s %q: got (%q, %v), want (%q, %v)", c.bucket, c.contentType, ext, ok, c.ext, c.ok)
		}
	}
}
