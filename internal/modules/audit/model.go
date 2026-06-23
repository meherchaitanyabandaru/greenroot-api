package audit

import "time"

type AuditLog struct {
	ID        int64     `json:"id"`
	TableName string    `json:"table_name"`
	RecordID  int64     `json:"record_id"`
	Action    string    `json:"action_type"`
	OldData   *string   `json:"old_data,omitempty"`
	NewData   *string   `json:"new_data,omitempty"`
	ChangedBy *int64    `json:"changed_by,omitempty"`
	SourceIP  *string   `json:"source_ip,omitempty"`
	UserAgent *string   `json:"user_agent,omitempty"`
	ChangedAt time.Time `json:"changed_at"`
}

type ActorContext struct {
	UserID    int64
	Roles     []string
	IPAddress string
	UserAgent string
}
