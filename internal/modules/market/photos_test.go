package market

import (
	"errors"
	"strings"
	"testing"
)

type fakeFiles struct{}

func (fakeFiles) Locate(u string) (string, string, bool) {
	if rest, ok := strings.CutPrefix(u, "https://cdn.greenroot/"); ok {
		bucket, key, _ := strings.Cut(rest, "/")
		return bucket, key, true
	}
	return "", "", false
}

func TestCheckPhotos(t *testing.T) {
	s := &Service{}
	if err := s.checkPhotos([]string{"https://example.com/a.jpg"}); err != nil {
		t.Errorf("without storage the links are not checked: %v", err)
	}
	s.UseStorage(fakeFiles{})
	cases := map[string]struct {
		photos []string
		ok     bool
	}{
		"our ad photos":          {[]string{"https://cdn.greenroot/market-ads/a.jpg", "https://cdn.greenroot/market-ads/b.jpg"}, true},
		"none":                   {nil, true},
		"a link from elsewhere":  {[]string{"https://example.com/a.jpg"}, false},
		"a private file of ours": {[]string{"https://cdn.greenroot/attachments/u1/licence.jpg"}, false},
		"too many":               {[]string{"https://cdn.greenroot/market-ads/1.jpg", "https://cdn.greenroot/market-ads/2.jpg", "https://cdn.greenroot/market-ads/3.jpg", "https://cdn.greenroot/market-ads/4.jpg"}, false},
	}
	for name, c := range cases {
		err := s.checkPhotos(c.photos)
		if c.ok != (err == nil) || (err != nil && !errors.Is(err, ErrInvalidInput)) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}
