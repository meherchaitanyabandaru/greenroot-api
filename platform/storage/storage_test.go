package storage

import "testing"

func TestLocateWithPrefix(t *testing.T) {
	c, err := New(Config{Endpoint: "localhost:9000", PublicURL: "https://files.example.in", BucketPrefix: "gr-prod-"})
	if err != nil {
		t.Fatal(err)
	}
	u := c.fileURL(BucketAttachments, "abc.png")
	if u != "https://files.example.in/gr-prod-attachments/abc.png" {
		t.Fatalf("file URL %s", u)
	}
	if b, k, ok := c.Locate(u); !ok || b != BucketAttachments || k != "abc.png" {
		t.Fatalf("Locate(%s) = %q %q %v", u, b, k, ok)
	}
	for _, bad := range []string{
		"https://files.example.in/attachments/abc.png",          // missing prefix
		"https://evil.example.com/gr-prod-attachments/abc.png",  // other host
		"https://files.example.in/gr-prod-unknown/abc.png",      // unknown bucket
		"https://files.example.in/gr-prod-attachments/../x.png", // traversal
		"https://files.example.in/gr-prod-attachments/",         // no key
	} {
		if _, _, ok := c.Locate(bad); ok {
			t.Errorf("Locate(%s) accepted", bad)
		}
	}
}

func TestLocateWithoutPrefix(t *testing.T) {
	c, _ := New(Config{Endpoint: "localhost:9000", PublicURL: "http://localhost:9000"})
	if b, k, ok := c.Locate("http://localhost:9000/market-ads/a.jpg"); !ok || b != BucketMarketAds || k != "a.jpg" {
		t.Fatalf("got %q %q %v", b, k, ok)
	}
}
