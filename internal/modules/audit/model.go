package audit

import "time"

type AuditLog struct {
	ID          int64     `json:"id"`
	Module      string    `json:"module,omitempty"`
	EntityType  string    `json:"entity_type,omitempty"`
	RecordID    int64     `json:"record_id"`
	Action      string    `json:"action_type"`
	Description *string   `json:"description,omitempty"`
	OldData     *string   `json:"old_data,omitempty"`
	NewData     *string   `json:"new_data,omitempty"`
	UserID      *int64    `json:"user_id,omitempty"`
	RequestID   *string   `json:"request_id,omitempty"`
	NurseryID   *int64    `json:"nursery_id,omitempty"`
	SourceIP    *string   `json:"source_ip,omitempty"`
	UserAgent   *string   `json:"user_agent,omitempty"`
	ChangedAt   time.Time `json:"changed_at"`
}

type ActorContext struct {
	UserID    int64
	Roles     []string
	IPAddress string
	UserAgent string
}
