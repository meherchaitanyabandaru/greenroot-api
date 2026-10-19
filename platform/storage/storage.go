package storage

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
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
	BucketMarketAds      = "market-ads"
	BucketQuotationPDFs  = "quotation-pdfs"
	BucketOrderPDFs      = "order-pdfs"
	BucketNurseryLogos   = "nursery-logos"
)

var validBuckets = map[string]bool{
	BucketProfileImages:  true,
	BucketPlantImages:    true,
	BucketLoadingPhotos:  true,
	BucketDeliveryPhotos: true,
	BucketAttachments:    true,
	BucketMarketAds:      true,
	BucketQuotationPDFs:  true,
	BucketOrderPDFs:      true,
	BucketNurseryLogos:   true,
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

// RemoveByURL deletes an object this client stored, given the public URL
// PutObject returned. URLs from elsewhere are ignored (nil error).
func (c *Client) RemoveByURL(ctx context.Context, fileURL string) error {
	rest, ok := strings.CutPrefix(fileURL, c.publicURL+"/")
	if !ok {
		return nil
	}
	bucket, key, ok := strings.Cut(rest, "/")
	if !ok || !validBuckets[bucket] || key == "" {
		return nil
	}
	if err := c.minio.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("storage: remove %s/%s: %w", bucket, key, err)
	}
	return nil
}

// publicBuckets hold images every app shows by plain URL (profile selfies,
// plant and ad photos, logos); the rest stay private behind presigned URLs.
var publicBuckets = []string{BucketProfileImages, BucketPlantImages, BucketMarketAds, BucketNurseryLogos}

// EnsureBuckets creates any missing platform bucket and makes the image
// buckets publicly readable. For local MinIO and CI; production buckets are
// provisioned by infra.
func (c *Client) EnsureBuckets(ctx context.Context) error {
	for bucket := range validBuckets {
		exists, err := c.minio.BucketExists(ctx, bucket)
		if err != nil {
			return fmt.Errorf("storage: check bucket %s: %w", bucket, err)
		}
		if !exists {
			if err := c.minio.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
				return fmt.Errorf("storage: create bucket %s: %w", bucket, err)
			}
		}
	}
	for _, bucket := range publicBuckets {
		policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}`, bucket)
		if err := c.minio.SetBucketPolicy(ctx, bucket, policy); err != nil {
			return fmt.Errorf("storage: make %s public: %w", bucket, err)
		}
	}
	return nil
}

// IsValidBucket returns true if the bucket name is one of the known platform buckets.
func IsValidBucket(bucket string) bool { return validBuckets[bucket] }
