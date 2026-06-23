package audit

type ListRequest struct {
	Page      int
	PerPage   int
	TableName string
	Action    string
	ChangedBy int64
	RecordID  int64
}
type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}
type ListResponse struct {
	AuditLogs  []AuditLog `json:"audit_logs"`
	Pagination Pagination `json:"pagination"`
}
