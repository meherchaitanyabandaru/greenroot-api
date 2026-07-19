package attachments

import (
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"time"
)

type ActorContext = authctx.ActorContext

type Attachment struct {
	ID             int64     `json:"id"`
	AttachmentCode string    `json:"attachment_code"`
	EntityType     string    `json:"entity_type"`
	EntityID       int64     `json:"entity_id"`
	FileName       string    `json:"file_name"`
	FileURL        string    `json:"file_url"`
	FileType       *string   `json:"file_type,omitempty"`
	FileSize       *int64    `json:"file_size,omitempty"`
	UploadedBy     *int64    `json:"uploaded_by,omitempty"`
	UploadedAt     time.Time `json:"uploaded_at"`
}
