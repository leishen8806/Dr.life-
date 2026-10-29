package files

import (
	"context"
	"encoding/base64"
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

type memoryStore struct{ file File }

func (s *memoryStore) Create(_ context.Context, file File) error { s.file = file; return nil }
func (s *memoryStore) GetOwned(_ context.Context, owner int64, publicID string) (File, error) {
	if s.file.OwnerAccount != owner || s.file.PublicID != publicID {
		return File{}, ErrNotFound
	}
	return s.file, nil
}
func (s *memoryStore) MarkVerifying(context.Context, int64, string) (File, error) { return File{}, nil }
func (s *memoryStore) DailyUsage(context.Context, int64, time.Time) (Usage, error) {
	return Usage{}, nil
}

type fakeObjects struct{ in storage.PutRequest }

func (s *fakeObjects) PresignPut(_ context.Context, in storage.PutRequest) (storage.SignedURL, error) {
	s.in = in
	return storage.SignedURL{Key: "objects/test", URL: "https://example.test/put"}, nil
}
func (*fakeObjects) PresignGet(context.Context, string) (storage.SignedURL, error) {
	return storage.SignedURL{}, nil
}
func (*fakeObjects) Stat(context.Context, string) (storage.ObjectStat, error) {
	return storage.ObjectStat{}, nil
}
func (*fakeObjects) Open(context.Context, string) (io.ReadCloser, error) { return nil, nil }
func (*fakeObjects) Delete(context.Context, string) error                { return nil }
