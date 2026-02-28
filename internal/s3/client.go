package s3

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Client wraps aws-sdk-go-v2 to provide S3 operations for a single bucket.
type Client struct {
	s3           *awss3.Client
	up           *manager.Uploader //nolint:staticcheck
	bucket       string
	storageClass string
}

// New creates a Client targeting bucket. Credentials are loaded via the default
// AWS credential chain (env vars, ~/.aws, IMDS, etc.).
func New(ctx context.Context, bucket string, opts ...Option) (*Client, error) {
	cfg := &s3Config{region: "us-east-1", storageClass: "STANDARD_IA"}
	for _, o := range opts {
		o(cfg)
	}

	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.region),
	)
	if err != nil {
		return nil, fmt.Errorf("s3: load config: %w", err)
	}

	var clientOpts []func(*awss3.Options)
	if cfg.endpoint != "" {
		ep := cfg.endpoint
		clientOpts = append(clientOpts, func(o *awss3.Options) {
			o.BaseEndpoint = aws.String(ep)
		})
	}
	if cfg.pathStyle {
		clientOpts = append(clientOpts, func(o *awss3.Options) {
			o.UsePathStyle = true
		})
	}

	s3c := awss3.NewFromConfig(awsCfg, clientOpts...)
	up := manager.NewUploader(s3c, func(u *manager.Uploader) { //nolint:staticcheck
		u.PartSize = 16 * 1024 * 1024
		u.Concurrency = 4
	})

	return &Client{
		s3:           s3c,
		up:           up,
		bucket:       bucket,
		storageClass: cfg.storageClass,
	}, nil
}

// Upload uploads body to key with an immutable cache header and configured storage class.
// The size parameter is unused but kept for interface clarity; the uploader streams directly.
func (c *Client) Upload(ctx context.Context, key string, body io.Reader, _ int64) error {
	_, err := c.up.Upload(ctx, &awss3.PutObjectInput{ //nolint:staticcheck
		Bucket:       aws.String(c.bucket),
		Key:          aws.String(key),
		Body:         body,
		CacheControl: aws.String("public, max-age=31536000, immutable"),
		ContentType:  aws.String("application/octet-stream"),
		StorageClass: types.StorageClass(c.storageClass),
	})
	if err != nil {
		return fmt.Errorf("s3: upload %q: %w", key, err)
	}
	return nil
}

// UploadManifest uploads body to key with a short-lived cache header (max-age=300)
// and STANDARD storage class, suitable for the manifest.json index file.
func (c *Client) UploadManifest(ctx context.Context, key string, body io.Reader, _ int64) error {
	_, err := c.up.Upload(ctx, &awss3.PutObjectInput{ //nolint:staticcheck
		Bucket:       aws.String(c.bucket),
		Key:          aws.String(key),
		Body:         body,
		CacheControl: aws.String("public, max-age=300"),
		ContentType:  aws.String("application/json"),
		StorageClass: types.StorageClassStandard,
	})
	if err != nil {
		return fmt.Errorf("s3: upload manifest %q: %w", key, err)
	}
	return nil
}

// Head returns the byte size and existence of key. Returns (0, false, nil) when the key
// does not exist so callers can distinguish missing from error.
func (c *Client) Head(ctx context.Context, key string) (size int64, exists bool, err error) {
	out, err := c.s3.HeadObject(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var notFound *types.NotFound
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &notFound) || errors.As(err, &noSuchKey) {
			return 0, false, nil
		}
		// HEAD responses carry no body; SDK may surface a plain HTTP 404.
		var httpErr interface{ HTTPStatusCode() int }
		if errors.As(err, &httpErr) && httpErr.HTTPStatusCode() == 404 {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("s3: head %q: %w", key, err)
	}
	if out.ContentLength == nil {
		return 0, true, nil
	}
	return *out.ContentLength, true, nil
}

// List returns all keys sharing prefix, following pagination continuation tokens.
func (c *Client) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	var token *string
	for {
		out, err := c.s3.ListObjectsV2(ctx, &awss3.ListObjectsV2Input{
			Bucket:            aws.String(c.bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("s3: list %q: %w", prefix, err)
		}
		for _, obj := range out.Contents {
			if obj.Key != nil {
				keys = append(keys, *obj.Key)
			}
		}
		if out.NextContinuationToken == nil {
			break
		}
		token = out.NextContinuationToken
	}
	return keys, nil
}

// Delete removes key from the bucket.
func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("s3: delete %q: %w", key, err)
	}
	return nil
}
