package files

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/storage"
)

func TestPresignRejectsUnsupportedPurpose(t *testing.T) {
	service := New(nil, nil, Options{})
	if _, err := service.Presign(t.Context(), 1, PresignInput{Purpose: "unknown"}); err == nil {
		t.Fatal("Presign accepted an unsupported purpose")
	}
}

func TestPresignCreatesPendingFileAndSignsExactMetadata(t *testing.T) {
	store := &memoryStore{}
	objects := &fakeObjects{}
	service := New(store, objects, Options{NewPublicID: func() string { return "01hxfile" }})
	checksum := base64.StdEncoding.EncodeToString(make([]byte, 32))

	got, err := service.Presign(context.Background(), 7, PresignInput{Purpose: "product_image", ContentType: "image/jpeg", Size: 12, SHA256: checksum})
	if err != nil {
		t.Fatal(err)
	}
	if got.File.OwnerAccount != 7 || got.File.Status != StatusPending {
		t.Fatalf("file = %#v", got.File)
	}
	if got.Upload.Key != "objects/test" || objects.in.ContentLength != 12 || objects.in.ChecksumSHA256 != checksum {
		t.Fatalf("upload = %#v input = %#v", got.Upload, objects.in)
	}
}

func TestPresignRejectsDailyUploadCountAndByteQuota(t *testing.T) {
	checksum := base64.StdEncoding.EncodeToString(make([]byte, 32))
	for _, test := range []struct {
		name    string
		usage   Usage
		options Options
		input   PresignInput
	}{
		{name: "count", usage: Usage{Count: 2}, options: Options{MaxDailyUploads: 2}, input: PresignInput{Purpose: "product_image", ContentType: "image/jpeg", Size: 1, SHA256: checksum}},
		{name: "bytes", usage: Usage{Bytes: 9}, options: Options{MaxDailyBytes: 10}, input: PresignInput{Purpose: "product_image", ContentType: "image/jpeg", Size: 2, SHA256: checksum}},
	} {
		t.Run(test.name, func(t *testing.T) {
			objects := &fakeObjects{}
			service := New(&memoryStore{usage: test.usage}, objects, test.options)
			if _, err := service.Presign(context.Background(), 7, test.input); !errors.Is(err, ErrQuotaExceeded) {
				t.Fatalf("Presign() error=%v, want %v", err, ErrQuotaExceeded)
			}
			if objects.in.ContentType != "" {
				t.Fatal("quota rejection must not issue an upload URL")
			}
		})
	}
}

func TestCompleteMovesOnlyOwnedPendingFileToVerifying(t *testing.T) {
	store := &memoryStore{file: File{PublicID: "file-1", OwnerAccount: 7, Status: StatusPending}}
	verify := &fakeVerifier{}
	service := New(store, &fakeObjects{}, Options{Verifier: verify})

	file, err := service.Complete(context.Background(), 7, "file-1")
	if err != nil {
		t.Fatal(err)
	}
	if file.Status != StatusVerifying || store.file.Status != StatusVerifying {
		t.Fatalf("file status = %q, stored = %q", file.Status, store.file.Status)
	}
	if verify.file.PublicID != "file-1" || verify.file.ObjectKey != "" {
		t.Fatalf("verification task file = %#v", verify.file)
	}
	if _, err := service.Complete(context.Background(), 8, "file-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Complete cross-owner error = %v, want %v", err, ErrNotFound)
	}
}

func TestCompleteFailsClosedWithoutVerificationQueue(t *testing.T) {
	store := &memoryStore{file: File{PublicID: "file-1", OwnerAccount: 7, Status: StatusPending}}
	service := New(store, &fakeObjects{}, Options{})
	if _, err := service.Complete(context.Background(), 7, "file-1"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Complete without verifier error = %v, want %v", err, ErrNotConfigured)
	}
}

func TestDownloadURLRequiresOwnedReadyFile(t *testing.T) {
	store := &memoryStore{file: File{PublicID: "file-1", OwnerAccount: 7, ObjectKey: "objects/test", Status: StatusReady}}
	objects := &fakeObjects{download: storage.SignedURL{URL: "https://example.test/get", ExpiresAt: time.Now().Add(time.Minute)}}
	service := New(store, objects, Options{})

	got, err := service.DownloadURL(context.Background(), 7, "file-1")
	if err != nil || got.URL == "" || objects.getKey != "objects/test" {
		t.Fatalf("DownloadURL() = %#v, %v; key=%q", got, err, objects.getKey)
	}
	store.file.Status = StatusPending
	if _, err := service.DownloadURL(context.Background(), 7, "file-1"); !errors.Is(err, ErrNotReady) {
		t.Fatalf("DownloadURL pending error = %v, want %v", err, ErrNotReady)
	}
}

type memoryStore struct {
	file  File
	usage Usage
}

func (s *memoryStore) Create(_ context.Context, file File) error { s.file = file; return nil }
func (s *memoryStore) GetOwned(_ context.Context, owner int64, publicID string) (File, error) {
	if s.file.OwnerAccount != owner || s.file.PublicID != publicID {
		return File{}, ErrNotFound
	}
	return s.file, nil
}
func (s *memoryStore) MarkVerifying(_ context.Context, owner int64, publicID string) (File, error) {
	if s.file.OwnerAccount != owner || s.file.PublicID != publicID || s.file.Status != StatusPending {
		return File{}, ErrNotFound
	}
	s.file.Status = StatusVerifying
	return s.file, nil
}
func (s *memoryStore) DailyUsage(context.Context, int64, time.Time) (Usage, error) {
	return s.usage, nil
}

type fakeObjects struct {
	in       storage.PutRequest
	download storage.SignedURL
	getKey   string
}

type fakeVerifier struct{ file File }

func (v *fakeVerifier) EnqueueVerification(_ context.Context, file File) error {
	v.file = file
	return nil
}

func (s *fakeObjects) PresignPut(_ context.Context, in storage.PutRequest) (storage.SignedURL, error) {
	s.in = in
	return storage.SignedURL{Key: "objects/test", URL: "https://example.test/put"}, nil
}
func (s *fakeObjects) PresignGet(_ context.Context, key string) (storage.SignedURL, error) {
	s.getKey = key
	return s.download, nil
}
func (*fakeObjects) Stat(context.Context, string) (storage.ObjectStat, error) {
	return storage.ObjectStat{}, nil
}
func (*fakeObjects) Open(context.Context, string) (io.ReadCloser, error) { return nil, nil }
func (*fakeObjects) Delete(context.Context, string) error                { return nil }
