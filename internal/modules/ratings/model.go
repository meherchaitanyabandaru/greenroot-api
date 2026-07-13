package ratings

import "time"

type Rating struct {
	ID               int64     `json:"id"`
	RatingType       string    `json:"rating_type"`
	RatedByUserID    int64     `json:"rated_by_user_id"`
	OrderID          *int64    `json:"order_id,omitempty"`
	DispatchID       *int64    `json:"dispatch_id,omitempty"`
	OverallRating    *int      `json:"overall_rating,omitempty"`
	WouldRecommend   *bool     `json:"would_recommend,omitempty"`
	DriverBehaviour  *int      `json:"driver_behaviour_rating,omitempty"`
	OnTimeDelivery   *int      `json:"on_time_delivery_rating,omitempty"`
	PlantCondition   *int      `json:"plant_condition_rating,omitempty"`
	PlantQuality     *int      `json:"plant_quality_rating,omitempty"`
	Communication    *int      `json:"communication_rating,omitempty"`
	OverallExperience *int     `json:"overall_experience_rating,omitempty"`
	WouldBuyAgain    *bool     `json:"would_buy_again,omitempty"`
	Comment          *string   `json:"comment,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// SubmitAppRatingRequest is the payload for POST /ratings/app.
type SubmitAppRatingRequest struct {
	OverallRating  int    `json:"overall_rating"`
	WouldRecommend *bool  `json:"would_recommend"`
	Comment        string `json:"comment"`
}

// SubmitTripRatingRequest is the payload for POST /ratings/trip/:dispatch_id.
type SubmitTripRatingRequest struct {
	DriverBehaviour *int   `json:"driver_behaviour_rating"`
	OnTimeDelivery  *int   `json:"on_time_delivery_rating"`
	PlantCondition  *int   `json:"plant_condition_rating"`
	Comment         string `json:"comment"`
}

// SubmitOrderRatingRequest is the payload for POST /ratings/order/:order_id.
type SubmitOrderRatingRequest struct {
	PlantQuality      *int   `json:"plant_quality_rating"`
	Communication     *int   `json:"communication_rating"`
	OverallExperience *int   `json:"overall_experience_rating"`
	WouldBuyAgain     *bool  `json:"would_buy_again"`
	Comment           string `json:"comment"`
}

type RatingResponse struct {
	Rating Rating `json:"rating"`
}

type RatingsResponse struct {
	Ratings []Rating `json:"ratings"`
}

type ListRatingsRequest struct {
	RatingType string
	Page       int
	PerPage    int
}

type ActorContext struct {
	UserID int64
	Roles  []string
}
