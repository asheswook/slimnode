package s3

// Option configures a Client.
type Option func(*s3Config)

type s3Config struct {
	endpoint     string
	region       string
	pathStyle    bool
	storageClass string
}

// WithEndpoint sets a custom S3-compatible endpoint URL (e.g. for R2, DO Spaces).
func WithEndpoint(url string) Option {
	return func(c *s3Config) {
		c.endpoint = url
	}
}

// WithRegion sets the AWS region.
func WithRegion(region string) Option {
	return func(c *s3Config) {
		c.region = region
	}
}

// WithPathStyle enables path-style S3 URLs (required for some S3-compatible providers).
func WithPathStyle(b bool) Option {
	return func(c *s3Config) {
		c.pathStyle = b
	}
}

// WithStorageClass sets the S3 storage class for block file uploads (e.g. STANDARD, STANDARD_IA).
func WithStorageClass(class string) Option {
	return func(c *s3Config) {
		c.storageClass = class
	}
}
