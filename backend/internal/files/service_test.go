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

func TestCompleteMovesOnlyOwnedPendingFileToVerifying(t *testing.T) {
	store := &memoryStore{file: File{PublicID: "file-1", OwnerAccount: 7, Status: StatusPending}}
	service := New(store, &fakeObjects{}, Options{})

	file, err := service.Complete(context.Background(), 7, "file-1")
	if err != nil {
		t.Fatal(err)
	}
	if file.Status != StatusVerifying || store.file.Status != StatusVerifying {
		t.Fatalf("file status = %q, stored = %q", file.Status, store.file.Status)
	}
	if _, err := service.Complete(context.Background(), 8, "file-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Complete cross-owner error = %v, want %v", err, ErrNotFound)
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

type memoryStore struct{ file File }

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
	return Usage{}, nil
}

type fakeObjects struct {
	in       storage.PutRequest
	download storage.SignedURL
	getKey   string
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
