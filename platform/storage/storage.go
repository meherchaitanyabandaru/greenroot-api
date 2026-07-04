package storage

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Bucket names used across the platform.
const (
	BucketProfileImages  = "profile-images"
	BucketPlantImages    = "plant-images"
	BucketLoadingPhotos  = "loading-photos"
	BucketDeliveryPhotos = "delivery-photos"
	BucketAttachments    = "attachments"
)

var validBuckets = map[string]bool{
	BucketProfileImages:  true,
	BucketPlantImages:    true,
	BucketLoadingPhotos:  true,
	BucketDeliveryPhotos: true,
	BucketAttachments:    true,
}

// Config holds connection settings. Same fields work for MinIO (local) and AWS S3 (prod).
type Config struct {
	Endpoint        string // MinIO: "localhost:9000"  |  AWS: "s3.amazonaws.com"
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
	Region          string
	PublicURL       string // base URL used to build the final file URL shown to clients
}

type Client struct {
	minio     *minio.Client
	publicURL string
}

func New(cfg Config) (*Client, error) {
	mc, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: init minio client: %w", err)
	}
	return &Client{minio: mc, publicURL: cfg.PublicURL}, nil
}

// PresignPut generates a pre-signed PUT URL. The caller uploads the file directly.
// Returns (uploadURL, fileURL, error).
// uploadURL — the client uses this to PUT the file (expires in ttl).
// fileURL   — the permanent URL to store in the database and return to end users.
func (c *Client) PresignPut(ctx context.Context, bucket, key string, ttl time.Duration) (uploadURL string, fileURL string, err error) {
	if !validBuckets[bucket] {
		return "", "", fmt.Errorf("storage: unknown bucket %q", bucket)
	}

	params := url.Values{}
	u, err := c.minio.PresignedPutObject(ctx, bucket, key, ttl)
	if err != nil {
		return "", "", fmt.Errorf("storage: presign put %s/%s: %w", bucket, key, err)
	}
	_ = params

	fileURL = fmt.Sprintf("%s/%s/%s", c.publicURL, bucket, key)
	return u.String(), fileURL, nil
}

// PresignGet generates a pre-signed GET URL for private buckets (loading-photos, delivery-photos, attachments).
func (c *Client) PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (string, error) {
	u, err := c.minio.PresignedGetObject(ctx, bucket, key, ttl, nil)
	if err != nil {
		return "", fmt.Errorf("storage: presign get %s/%s: %w", bucket, key, err)
	}
	return u.String(), nil
}

// PutObject uploads bytes directly from the server to MinIO.
// Returns the permanent public file URL.
func (c *Client) PutObject(ctx context.Context, bucket, key, contentType string, data []byte) (string, error) {
	if !validBuckets[bucket] {
		return "", fmt.Errorf("storage: unknown bucket %q", bucket)
	}
	_, err := c.minio.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType},
	)
	if err != nil {
		return "", fmt.Errorf("storage: put %s/%s: %w", bucket, key, err)
	}
	return fmt.Sprintf("%s/%s/%s", c.publicURL, bucket, key), nil
}

// IsValidBucket returns true if the bucket name is one of the known platform buckets.
func IsValidBucket(bucket string) bool { return validBuckets[bucket] }
