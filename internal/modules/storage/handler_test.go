package storage

import "testing"

func TestUploadRules(t *testing.T) {
	cases := []struct {
		bucket, contentType, ext string
		ok                       bool
	}{
		// Photos are never presigned: POST /storage/images checks them.
		{"market-ads", "image/jpeg", "", false},
		{"nursery-logos", "IMAGE/PNG; charset=binary", "", false},
		{"delivery-photos", "image/webp", "", false},
		{"attachments", "image/jpeg", "", false},
		{"attachments", "application/pdf", ".pdf", true},
		{"attachments", "APPLICATION/PDF; charset=binary", ".pdf", true},
		{"attachments", "application/octet-stream", ".bin", true},
		{"market-ads", "text/html", "", false},     // a page served from a public bucket
		{"market-ads", "image/svg+xml", "", false}, // SVG can carry script
		{"market-ads", "image/jpg", "", false},     // alias of a photo
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

func TestImageCategoriesHaveBuckets(t *testing.T) {
	for _, c := range imageCategories() {
		if imageBuckets[c] == "" {
			t.Errorf("%s has no bucket", c)
		}
	}
	if _, ok := imageBuckets["SELFIE"]; ok {
		t.Error("selfies go through POST /users/me/avatar")
	}
	for c, b := range imageBuckets {
		if c == "DRIVER_LICENCE" || c == "DELIVERY_PROOF" || c == "LOADING_PROOF" || c == "ATTACHMENT" {
			if publicBuckets[b] {
				t.Errorf("%s photos must stay private, got public bucket %s", c, b)
			}
		}
	}
}
