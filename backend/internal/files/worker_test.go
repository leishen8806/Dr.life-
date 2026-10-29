package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/storage"
)

func TestVerificationWorkerMarksMatchingObjectReady(t *testing.T) {
	body := []byte("verified file")
	sum := sha256.Sum256(body)
	store := &verificationMemoryStore{file: File{PublicID: "file-1", ObjectKey: "objects/file-1", ContentType: "image/jpeg", Size: int64(len(body)), SHA256: sum[:], Status: StatusVerifying}}
	objects := &verificationObjects{body: body, stat: storage.ObjectStat{Size: int64(len(body)), ContentType: "image/jpeg", ChecksumSHA256: base64.StdEncoding.EncodeToString(sum[:])}}

	if err := NewVerificationWorker(store, objects).Handle(context.Background(), asynq.NewTask(TaskVerify, []byte(`{"public_id":"file-1"}`))); err != nil {
		t.Fatal(err)
	}
	if store.file.Status != StatusReady || objects.deleted {
		t.Fatalf("status=%q deleted=%t", store.file.Status, objects.deleted)
	}
}

func TestVerificationWorkerRejectsAndDeletesChecksumMismatch(t *testing.T) {
	expected := sha256.Sum256([]byte("expected"))
	body := []byte("tampered")
	store := &verificationMemoryStore{file: File{PublicID: "file-1", ObjectKey: "objects/file-1", ContentType: "image/jpeg", Size: int64(len(body)), SHA256: expected[:], Status: StatusVerifying}}
	objects := &verificationObjects{body: body, stat: storage.ObjectStat{Size: int64(len(body)), ContentType: "image/jpeg"}}

	if err := NewVerificationWorker(store, objects).Handle(context.Background(), asynq.NewTask(TaskVerify, []byte(`{"public_id":"file-1"}`))); err != nil {
		t.Fatal(err)
	}
	if store.file.Status != StatusRejected || !objects.deleted {
		t.Fatalf("status=%q deleted=%t", store.file.Status, objects.deleted)
	}
}

type verificationMemoryStore struct{ file File }

func (s *verificationMemoryStore) GetVerifying(_ context.Context, publicID string) (File, error) {
	if s.file.PublicID != publicID || s.file.Status != StatusVerifying {
		return File{}, ErrNotFound
	}
	return s.file, nil
}
func (s *verificationMemoryStore) MarkReady(_ context.Context, publicID string) error {
	if s.file.PublicID != publicID || s.file.Status != StatusVerifying {
		return ErrNotFound
	}
	s.file.Status = StatusReady
	return nil
}
func (s *verificationMemoryStore) MarkRejected(_ context.Context, publicID string) error {
	if s.file.PublicID != publicID || s.file.Status != StatusVerifying {
		return ErrNotFound
	}
	s.file.Status = StatusRejected
	return nil
}

type verificationObjects struct {
	body    []byte
	stat    storage.ObjectStat
	deleted bool
}

func (s *verificationObjects) PresignPut(context.Context, storage.PutRequest) (storage.SignedURL, error) { return storage.SignedURL{}, nil }
func (s *verificationObjects) PresignGet(context.Context, string) (storage.SignedURL, error)               { return storage.SignedURL{}, nil }
func (s *verificationObjects) Stat(context.Context, string) (storage.ObjectStat, error)                    { return s.stat, nil }
func (s *verificationObjects) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.body)), nil
}
func (s *verificationObjects) Delete(context.Context, string) error { s.deleted = true; return nil }
