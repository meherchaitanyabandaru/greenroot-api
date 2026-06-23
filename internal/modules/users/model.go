package users

import "time"

const (
	activityUpdateProfile = "UPDATE_PROFILE"
	activityCreateAddress = "CREATE_ADDRESS"
	activityUpdateAddress = "UPDATE_ADDRESS"
	activityDeleteAddress = "DELETE_ADDRESS"
)

type User struct {
	ID              int64      `json:"id"`
	UserCode        string     `json:"user_code"`
	FirstName       string     `json:"first_name"`
	LastName        *string    `json:"last_name,omitempty"`
	Gender          *string    `json:"gender,omitempty"`
	Mobile          string     `json:"mobile"`
	Email           *string    `json:"email,omitempty"`
	ProfileImageURL *string    `json:"profile_image_url,omitempty"`
	MobileVerified  bool       `json:"mobile_verified"`
	EmailVerified   bool       `json:"email_verified"`
	Status          string     `json:"status"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	Roles           []Role     `json:"roles,omitempty"`
}

type Address struct {
	ID            int64      `json:"id"`
	UserID        int64      `json:"user_id"`
	AddressType   *string    `json:"address_type,omitempty"`
	ContactName   *string    `json:"contact_name,omitempty"`
	ContactMobile *string    `json:"contact_mobile,omitempty"`
	AddressLine1  string     `json:"address_line1"`
	AddressLine2  *string    `json:"address_line2,omitempty"`
	City          *string    `json:"city,omitempty"`
	State         *string    `json:"state,omitempty"`
	Country       *string    `json:"country,omitempty"`
	PostalCode    *string    `json:"postal_code,omitempty"`
	Latitude      *float64   `json:"latitude,omitempty"`
	Longitude     *float64   `json:"longitude,omitempty"`
	IsDefault     bool       `json:"is_default"`
	CreatedAt     *time.Time `json:"created_at,omitempty"`
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
}

type Role struct {
	ID   int16  `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

type Session struct {
	ID             int64      `json:"id"`
	UserID         int64      `json:"user_id"`
	LoginTime      time.Time  `json:"login_time"`
	LastActivityAt time.Time  `json:"last_activity_at"`
	Status         string     `json:"status"`
	DeviceType     *string    `json:"device_type,omitempty"`
	OSName         *string    `json:"os_name,omitempty"`
	AppVersion     *string    `json:"app_version,omitempty"`
	IPAddress      *string    `json:"ip_address,omitempty"`
	UserAgent      *string    `json:"user_agent,omitempty"`
	CreatedAt      *time.Time `json:"created_at,omitempty"`
}
