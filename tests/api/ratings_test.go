package apitest

import (
	"net/http"
	"testing"
)

type ratingEnvelope struct {
	Rating struct {
		ID            int64  `json:"id"`
		RatingType    string `json:"rating_type"`
		OverallRating *int   `json:"overall_rating"`
	} `json:"rating"`
}

// TestAppRating_UpsertsOnePerUser covers the ratings table end to end (it was
// missing from fresh installs until migration 20261017_07).
func TestAppRating_UpsertsOnePerUser(t *testing.T) {
	token := login(t, buyerPhone)

	first := post(t, "/api/v1/ratings/app", map[string]any{"overall_rating": 5, "would_recommend": true, "comment": "great"}, token)
	assertStatus(t, first, http.StatusOK)
	var a ratingEnvelope
	decode(t, first, &a)
	if a.Rating.ID == 0 || a.Rating.RatingType != "APP" || a.Rating.OverallRating == nil || *a.Rating.OverallRating != 5 {
		t.Fatalf("first app rating: %+v", a.Rating)
	}

	second := post(t, "/api/v1/ratings/app", map[string]any{"overall_rating": 3, "would_recommend": false}, token)
	assertStatus(t, second, http.StatusOK)
	var b ratingEnvelope
	decode(t, second, &b)
	if b.Rating.ID != a.Rating.ID || b.Rating.OverallRating == nil || *b.Rating.OverallRating != 3 {
		t.Fatalf("second app rating must update the same row: first=%+v second=%+v", a.Rating, b.Rating)
	}

	bad := post(t, "/api/v1/ratings/app", map[string]any{"overall_rating": 6}, token)
	if bad.StatusCode < 400 || bad.StatusCode >= 500 {
		t.Fatalf("out-of-range rating: got %d, want 4xx", bad.StatusCode)
	}
	_ = bad.Body.Close()
	assertStatus(t, post(t, "/api/v1/ratings/app", map[string]any{"overall_rating": 4}, ""), http.StatusUnauthorized)
}
