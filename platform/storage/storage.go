package storage

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
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
	// BucketPrefix makes bucket names unique where they are global (AWS S3):
	// "greenroot-prod-" stores "attachments" as "greenroot-prod-attachments".
	// Code keeps using the plain names. Empty for local MinIO.
	BucketPrefix string
	// UseIAMRole takes credentials from the EC2 instance role (IMDSv2)
	// instead of AccessKeyID/SecretAccessKey: no long-lived keys to leak.
	UseIAMRole bool
}

type Client struct {
	minio     *minio.Client
	publicURL string
	prefix    string
}

// physical is the real bucket name for a platform bucket.
func (c *Client) physical(bucket string) string { return c.prefix + bucket }

func New(cfg Config) (*Client, error) {
	creds := credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, "")
	if cfg.UseIAMRole {
		creds = credentials.NewIAM("") // instance metadata; refreshed before expiry
	}
	mc, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  creds,
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: init minio client: %w", err)
	}
	return &Client{minio: mc, publicURL: cfg.PublicURL, prefix: cfg.BucketPrefix}, nil
}

// PresignPut generates a pre-signed PUT URL. The caller uploads the file directly.
// Returns (uploadURL, fileURL, error).
// uploadURL — the client uses this to PUT the file (expires in ttl).
// fileURL   — the permanent URL to store in the database and return to end users.
// contentType and size are signed into the URL: the upload must send exactly
// that Content-Type and Content-Length, so a ".jpg" cannot be stored (and
// served) as a web page, and no upload is larger than the API allowed.
func (c *Client) PresignPut(ctx context.Context, bucket, key, contentType string, size int64, ttl time.Duration) (uploadURL string, fileURL string, err error) {
	if !validBuckets[bucket] {
		return "", "", fmt.Errorf("storage: unknown bucket %q", bucket)
	}
	u, err := c.minio.PresignHeader(ctx, http.MethodPut, c.physical(bucket), key, ttl, nil,
		http.Header{"Content-Type": []string{contentType}, "Content-Length": []string{strconv.FormatInt(size, 10)}})
	if err != nil {
		return "", "", fmt.Errorf("storage: presign put %s/%s: %w", bucket, key, err)
	}
	return u.String(), c.fileURL(bucket, key), nil
}

func (c *Client) fileURL(bucket, key string) string {
	return fmt.Sprintf("%s/%s/%s", c.publicURL, c.physical(bucket), key)
}

// PresignGet generates a pre-signed GET URL for private buckets (loading-photos, delivery-photos, attachments).
func (c *Client) PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (string, error) {
	u, err := c.minio.PresignedGetObject(ctx, c.physical(bucket), key, ttl, nil)
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
	_, err := c.minio.PutObject(ctx, c.physical(bucket), key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType},
	)
	if err != nil {
		return "", fmt.Errorf("storage: put %s/%s: %w", bucket, key, err)
	}
	return c.fileURL(bucket, key), nil
}

// Locate splits a file URL this client issued into bucket and key; ok is
// false for URLs from anywhere else.
func (c *Client) Locate(fileURL string) (bucket, key string, ok bool) {
	rest, ok := strings.CutPrefix(fileURL, c.publicURL+"/")
	if !ok {
		return "", "", false
	}
	physical, key, ok := strings.Cut(rest, "/")
	if !ok {
		return "", "", false
	}
	bucket, ok = strings.CutPrefix(physical, c.prefix)
	if !ok || !validBuckets[bucket] || key == "" || strings.Contains(key, "..") {
		return "", "", false
	}
	return bucket, key, true
}

// PresignGetURL is PresignGet for a file URL this client issued.
func (c *Client) PresignGetURL(ctx context.Context, fileURL string, ttl time.Duration) (string, error) {
	bucket, key, ok := c.Locate(fileURL)
	if !ok {
		return "", fmt.Errorf("storage: not a platform file URL")
	}
	return c.PresignGet(ctx, bucket, key, ttl)
}

// RemoveByURL deletes an object this client stored, given the public URL
// PutObject returned. URLs from elsewhere are ignored (nil error).
func (c *Client) RemoveByURL(ctx context.Context, fileURL string) error {
	bucket, key, ok := c.Locate(fileURL)
	if !ok {
		return nil
	}
	if err := c.minio.RemoveObject(ctx, c.physical(bucket), key, minio.RemoveObjectOptions{}); err != nil {
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
		exists, err := c.minio.BucketExists(ctx, c.physical(bucket))
		if err != nil {
			return fmt.Errorf("storage: check bucket %s: %w", bucket, err)
		}
		if !exists {
			if err := c.minio.MakeBucket(ctx, c.physical(bucket), minio.MakeBucketOptions{}); err != nil {
				return fmt.Errorf("storage: create bucket %s: %w", bucket, err)
			}
		}
	}
	for _, bucket := range publicBuckets {
		policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}`, c.physical(bucket))
		if err := c.minio.SetBucketPolicy(ctx, c.physical(bucket), policy); err != nil {
			return fmt.Errorf("storage: make %s public: %w", bucket, err)
		}
	}
	return nil
}

// UploaderPrefix is the key prefix of private files a user uploaded.
func UploaderPrefix(userID int64) string { return "u" + strconv.FormatInt(userID, 10) + "/" }

// IsValidBucket returns true if the bucket name is one of the known platform buckets.
func IsValidBucket(bucket string) bool { return validBuckets[bucket] }
