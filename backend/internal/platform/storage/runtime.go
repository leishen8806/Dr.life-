package storage

import (
	"errors"
	"strings"
)

var ErrRuntimeConfigInvalid = errors.New("storage runtime configuration is invalid")

// RuntimeConfig contains only the non-secret shape required to construct the
// configured object-store adapter. Callers must not log its credential fields.
type RuntimeConfig struct {
	Driver, Bucket, Region, Endpoint, AccessKey, SecretKey string
	LocalBaseURL, LocalRootDir                            string
	AllowedContentTypes                                   []string
	MaxContentLength                                      int64
}

func NewFromConfig(config RuntimeConfig) (Storage, error) {
	policy := Policy{AllowedContentTypes: config.AllowedContentTypes, MaxContentLength: config.MaxContentLength}
	if len(policy.AllowedContentTypes) == 0 || policy.MaxContentLength <= 0 {
		return nil, ErrRuntimeConfigInvalid
	}
	switch strings.ToLower(strings.TrimSpace(config.Driver)) {
	case "s3":
		if config.Bucket == "" || config.AccessKey == "" || config.SecretKey == "" {
			return nil, ErrRuntimeConfigInvalid
		}
		return NewS3(S3Config{Bucket: config.Bucket, Region: config.Region, Endpoint: config.Endpoint, AccessKey: config.AccessKey, SecretKey: config.SecretKey}, policy), nil
	case "minio":
		if config.Bucket == "" || config.Endpoint == "" || config.AccessKey == "" || config.SecretKey == "" {
			return nil, ErrRuntimeConfigInvalid
		}
		return NewMinIO(MinIOConfig{Bucket: config.Bucket, Endpoint: config.Endpoint, AccessKey: config.AccessKey, SecretKey: config.SecretKey}, policy), nil
	case "local":
		if config.LocalBaseURL == "" || config.LocalRootDir == "" || config.SecretKey == "" {
			return nil, ErrRuntimeConfigInvalid
		}
		return NewLocal(LocalConfig{BaseURL: config.LocalBaseURL, RootDir: config.LocalRootDir, Secret: []byte(config.SecretKey)}, policy)
	default:
		return nil, ErrRuntimeConfigInvalid
	}
}
