package notifications

import "time"

const (
	actionInsert = "INSERT"
	actionUpdate = "UPDATE"
	actionDelete = "DELETE"
)

type Notification struct {
	ID               int64      `json:"id"`
	NotificationCode string     `json:"notification_code"`
	UserID           *int64     `json:"user_id,omitempty"`
	Type             string     `json:"notification_type"`
	TemplateID       *int64     `json:"template_id,omitempty"`
	Title            *string    `json:"title,omitempty"`
	Message          *string    `json:"message,omitempty"`
	Channel          string     `json:"channel"`
	Status           string     `json:"notification_status"`
	Data             *string    `json:"data,omitempty"`
	SentAt           *time.Time `json:"sent_at,omitempty"`
	ReadAt           *time.Time `json:"read_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        *time.Time `json:"updated_at,omitempty"`
}

type Device struct {
	ID               int64      `json:"id"`
	UserID           int64      `json:"user_id"`
	FCMToken         string     `json:"fcm_token"`
	DeviceType       *string    `json:"device_type,omitempty"`
	ExternalDeviceID *string    `json:"device_id_external,omitempty"`
	Platform         *string    `json:"platform,omitempty"`
	AppVersion       *string    `json:"app_version,omitempty"`
	IsActive         bool       `json:"is_active"`
	LastSeenAt       *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        *time.Time `json:"updated_at,omitempty"`
}

type Template struct {
	ID              int64      `json:"id"`
	Code            string     `json:"template_code"`
	Name            *string    `json:"template_name,omitempty"`
	Channel         string     `json:"channel"`
	Subject         *string    `json:"subject,omitempty"`
	MessageTemplate *string    `json:"message_template,omitempty"`
	IsActive        bool       `json:"is_active"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

type ActorContext struct {
	UserID    int64
	Roles     []string
	IPAddress string
	UserAgent string
}
