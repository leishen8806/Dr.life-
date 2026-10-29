package storage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestNewFromConfigBuildsS3CompatibleStorage(t *testing.T) {
	objects, err := NewFromConfig(RuntimeConfig{
		Driver: "minio", Bucket: "files", Endpoint: "http://storage.test", AccessKey: "access", SecretKey: "secret",
		AllowedContentTypes: []string{"image/jpeg"}, MaxContentLength: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("payload"))
	if _, err := objects.PresignPut(context.Background(), PutRequest{ContentType: "image/jpeg", ContentLength: 7, ChecksumSHA256: base64.StdEncoding.EncodeToString(sum[:])}); err != nil {
		t.Fatal(err)
	}
}

func TestNewFromConfigRejectsIncompleteOrUnknownDriver(t *testing.T) {
	for _, cfg := range []RuntimeConfig{{}, {Driver: "unknown", Bucket: "files", AllowedContentTypes: []string{"image/jpeg"}, MaxContentLength: 1}} {
		if _, err := NewFromConfig(cfg); err == nil {
			t.Fatalf("NewFromConfig(%+v) succeeded", cfg)
		}
	}
}
