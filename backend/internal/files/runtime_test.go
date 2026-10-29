package files

import (
	"testing"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/config"
)

func TestNewObjectStoreMapsConfiguredStorage(t *testing.T) {
	objects, err := NewObjectStore(config.Config{
		StorageDriver:              "minio",
		StorageBucket:              "uploads",
		StorageEndpoint:            "http://127.0.0.1:9000",
		StorageAccessKey:           "access",
		StorageSecretKey:           "secret",
		StorageAllowedContentTypes: []string{"image/jpeg"},
		StorageMaxContentLength:    1024,
	})
	if err != nil {
		t.Fatalf("NewObjectStore() error = %v", err)
	}
	if objects == nil {
		t.Fatal("NewObjectStore() returned nil storage")
	}
}
