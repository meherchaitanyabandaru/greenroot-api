package attachments

import "github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"

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

type Pagination = response.Pagination

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
