package storage

type PresignRequest struct {
	Bucket      string `json:"bucket"`       // one of: profile-images, plant-images, loading-photos, delivery-photos, attachments
	FileName    string `json:"file_name"`    // original file name, e.g. "photo.jpg"
	ContentType string `json:"content_type"` // MIME type, e.g. "image/jpeg"
	Size        int64  `json:"size"`         // bytes; the upload must be exactly this size
}

type PresignResponse struct {
	UploadURL    string `json:"upload_url"` // PUT to this URL to upload the file (expires in 15 min)
	FileURL      string `json:"file_url"`   // permanent URL — store this in attachments table
	Key          string `json:"key"`        // object key inside the bucket
	Bucket       string `json:"bucket"`
	ExpiresInSec int    `json:"expires_in_sec"`
}
