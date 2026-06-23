package auth

import "time"

const (
	mockOTP              = "123456"
	defaultUserFirstName = "GreenRoot"
	defaultUserRole      = "BUYER"
	activityLogin        = "LOGIN"
	sessionStatusActive  = "ACTIVE"
	sessionStatusLogout  = "LOGGED_OUT"
)

type User struct {
	ID              int64      `json:"id"`
	UserCode        string     `json:"user_code"`
	FirstName       string     `json:"first_name"`
	LastName        *string    `json:"last_name,omitempty"`
	Mobile          string     `json:"mobile"`
	Email           *string    `json:"email,omitempty"`
	ProfileImageURL *string    `json:"profile_image_url,omitempty"`
	MobileVerified  bool       `json:"mobile_verified"`
	EmailVerified   bool       `json:"email_verified"`
	Status          string     `json:"status"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	Roles           []string   `json:"roles"`
}

type Session struct {
	ID             int64     `json:"id"`
	UserID         int64     `json:"user_id"`
	Status         string    `json:"status"`
	LoginTime      time.Time `json:"login_time"`
	LastActivityAt time.Time `json:"last_activity_at"`
}
