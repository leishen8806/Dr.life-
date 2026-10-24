package storage

type MinIOConfig struct {
	Bucket    string
	Endpoint  string
	AccessKey string
	SecretKey string
}

// NewMinIO uses the same SigV4 implementation as S3 with MinIO's path-style endpoint.
func NewMinIO(config MinIOConfig, policy Policy) Storage {
	return NewS3(S3Config{Bucket: config.Bucket, Region: "us-east-1", Endpoint: config.Endpoint, AccessKey: config.AccessKey, SecretKey: config.SecretKey, UsePathStyle: true}, policy)
}
