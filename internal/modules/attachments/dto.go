package attachments

type ListRequest struct {
	Page       int
	PerPage    int
	EntityType string
	EntityID   int64
	Search     string
}

type AttachmentRequest struct {
	EntityType string  `json:"entity_type"`
	EntityID   int64   `json:"entity_id"`
	FileName   string  `json:"file_name"`
	FileURL    string  `json:"file_url"`
	FileType   *string `json:"file_type"`
	FileSize   *int64  `json:"file_size"`
}

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type ListResponse struct {
	Attachments []Attachment `json:"attachments"`
	Pagination  Pagination   `json:"pagination"`
}

type AttachmentResponse struct {
	Attachment Attachment `json:"attachment"`
}

type MessageResponse struct {
	Message string `json:"message"`
}
