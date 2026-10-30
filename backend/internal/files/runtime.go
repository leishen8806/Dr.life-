package files

import (
	"github.com/leishen8806/Dr.life-/backend/internal/platform/config"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/storage"
)

// NewObjectStore builds the configured private object-store adapter. Callers
// must treat configuration failures as startup failures rather than serving
// upload endpoints without their verification storage.
func NewObjectStore(cfg config.Config) (storage.Storage, error) {
	return storage.NewFromConfig(storage.RuntimeConfig{
		Driver:              cfg.StorageDriver,
		Bucket:              cfg.StorageBucket,
		Region:              cfg.StorageRegion,
		Endpoint:            cfg.StorageEndpoint,
		AccessKey:           cfg.StorageAccessKey,
		SecretKey:           cfg.StorageSecretKey,
		AllowedContentTypes: cfg.StorageAllowedContentTypes,
		MaxContentLength:    cfg.StorageMaxContentLength,
	})
}
