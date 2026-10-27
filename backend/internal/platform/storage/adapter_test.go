package storage

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestAdaptersSharePresignContract(t *testing.T) {
	local, err := NewLocal(LocalConfig{BaseURL: "https://files.example.test", Secret: []byte("test-secret")}, Policy{AllowedContentTypes: []string{"image/jpeg"}, MaxContentLength: 100})
	if err != nil {
		t.Fatal(err)
	}
	adapters := map[string]Storage{
		"local": local,
		"s3":    NewS3(S3Config{Bucket: "bucket", Region: "us-east-1", Endpoint: "https://s3.example.test", AccessKey: "test", SecretKey: "test"}, Policy{AllowedContentTypes: []string{"image/jpeg"}, MaxContentLength: 100}),
		"minio": NewMinIO(MinIOConfig{Bucket: "bucket", Endpoint: "https://minio.example.test", AccessKey: "test", SecretKey: "test"}, Policy{AllowedContentTypes: []string{"image/jpeg"}, MaxContentLength: 100}),
	}
	for name, adapter := range adapters {
		t.Run(name, func(t *testing.T) {
			signed, err := adapter.PresignPut(context.Background(), PutRequest{ContentType: "image/jpeg", ContentLength: 100})
			if err != nil {
				t.Fatal(err)
			}
			if signed.Key == "" || signed.URL == "" {
				t.Fatalf("incomplete signed request: %#v", signed)
			}
			if name != "local" {
				u, err := url.Parse(signed.URL)
				if err != nil {
					t.Fatal(err)
				}
				headers := u.Query().Get("X-Amz-SignedHeaders")
				if !strings.Contains(headers, "content-type") || !strings.Contains(headers, "content-length") {
					t.Fatalf("signed headers = %q, want content type and length", headers)
				}
			}
			if _, err := adapter.PresignGet(context.Background(), signed.Key); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAdaptersRejectInvalidObjectKeys(t *testing.T) {
	local, err := NewLocal(LocalConfig{BaseURL: "https://files.example.test", Secret: []byte("test-secret")}, Policy{AllowedContentTypes: []string{"image/jpeg"}, MaxContentLength: 100})
	if err != nil {
		t.Fatal(err)
	}
	for name, adapter := range map[string]Storage{
		"local": local,
		"s3":    NewS3(S3Config{Bucket: "bucket", Region: "us-east-1", Endpoint: "https://s3.example.test", AccessKey: "test", SecretKey: "test"}, Policy{AllowedContentTypes: []string{"image/jpeg"}, MaxContentLength: 100}),
		"minio": NewMinIO(MinIOConfig{Bucket: "bucket", Endpoint: "https://minio.example.test", AccessKey: "test", SecretKey: "test"}, Policy{AllowedContentTypes: []string{"image/jpeg"}, MaxContentLength: 100}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := adapter.PresignGet(context.Background(), "objects/.."); !errors.Is(err, ErrInvalidObjectKey) {
				t.Fatalf("PresignGet() error = %v, want %v", err, ErrInvalidObjectKey)
			}
		})
	}
}
