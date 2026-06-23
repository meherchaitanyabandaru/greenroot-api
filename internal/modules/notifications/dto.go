package notifications

type ListNotificationsRequest struct {
	Page    int
	PerPage int
	UserID  int64
	Type    string
	Status  string
	Channel string
	Unread  *bool
	Search  string
}

type CreateNotificationRequest struct {
	UserID     *int64         `json:"user_id"`
	Type       string         `json:"notification_type"`
	TemplateID *int64         `json:"template_id"`
	Title      *string        `json:"title"`
	Message    *string        `json:"message"`
	Channel    string         `json:"channel"`
	Data       map[string]any `json:"data"`
}

type DeviceRequest struct {
	FCMToken         string  `json:"fcm_token"`
	DeviceType       *string `json:"device_type"`
	ExternalDeviceID *string `json:"device_id_external"`
	Platform         *string `json:"platform"`
	AppVersion       *string `json:"app_version"`
}

type TemplateRequest struct {
	Code            string  `json:"template_code"`
	Name            *string `json:"template_name"`
	Channel         string  `json:"channel"`
	Subject         *string `json:"subject"`
	MessageTemplate *string `json:"message_template"`
	IsActive        bool    `json:"is_active"`
}

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type NotificationsResponse struct {
	Notifications []Notification `json:"notifications"`
	Pagination    Pagination     `json:"pagination"`
}

type NotificationResponse struct {
	Notification Notification `json:"notification"`
}

type DevicesResponse struct {
	Devices []Device `json:"devices"`
}

type DeviceResponse struct {
	Device Device `json:"device"`
}

type TemplatesResponse struct {
	Templates []Template `json:"templates"`
}

type TemplateResponse struct {
	Template Template `json:"template"`
}

type MessageResponse struct {
	Message string `json:"message"`
}
