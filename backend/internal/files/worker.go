package files

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"

	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/storage"
)

// VerificationStore is the worker-only state transition port.
type VerificationStore interface {
	GetVerifying(context.Context, string) (File, error)
	MarkReady(context.Context, string, string) error
	MarkRejected(context.Context, string) error
}

type VerificationWorker struct {
	store   VerificationStore
	objects storage.ObjectVerifier
}

type workerRegistrar interface {
	Register(string, queue.Handler) error
}

func NewVerificationWorker(store VerificationStore, objects storage.ObjectVerifier) *VerificationWorker {
	return &VerificationWorker{store: store, objects: objects}
}

func RegisterVerificationHandler(worker workerRegistrar, store VerificationStore, objects storage.ObjectVerifier) error {
	if worker == nil {
		return ErrNotConfigured
	}
	return worker.Register(TaskVerify, NewVerificationWorker(store, objects).Handle)
}

func (w *VerificationWorker) Handle(ctx context.Context, task *asynq.Task) error {
	if w == nil || w.store == nil || w.objects == nil || task == nil {
		return ErrNotConfigured
	}
	var payload struct {
		PublicID string `json:"public_id"`
	}
	if err := json.Unmarshal(task.Payload(), &payload); err != nil || payload.PublicID == "" {
		return errors.New("invalid file verification task")
	}
	file, err := w.store.GetVerifying(ctx, payload.PublicID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	stat, err := w.objects.Stat(ctx, file.ObjectKey)
	if err != nil {
		return err
	}
	if !matchesMetadata(file, stat) {
		return w.reject(ctx, file)
	}
	body, err := w.objects.Open(ctx, file.ObjectKey)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, body)
	closeErr := body.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if subtle.ConstantTimeCompare(hash.Sum(nil), file.SHA256) != 1 {
		return w.reject(ctx, file)
	}
	readyKey, err := w.objects.Promote(ctx, file.ObjectKey, stat.ETag)
	if err != nil {
		return w.reject(ctx, file)
	}
	readyStat, err := w.objects.Stat(ctx, readyKey)
	if err != nil || !matchesMetadata(file, readyStat) {
		_ = w.objects.Delete(ctx, readyKey)
		return w.reject(ctx, file)
	}
	readyBody, err := w.objects.Open(ctx, readyKey)
	if err != nil {
		_ = w.objects.Delete(ctx, readyKey)
		return w.reject(ctx, file)
	}
	readyHash := sha256.New()
	_, readyCopyErr := io.Copy(readyHash, readyBody)
	readyCloseErr := readyBody.Close()
	if readyCopyErr != nil || readyCloseErr != nil || subtle.ConstantTimeCompare(readyHash.Sum(nil), file.SHA256) != 1 {
		_ = w.objects.Delete(ctx, readyKey)
		return w.reject(ctx, file)
	}
	if err := w.store.MarkReady(ctx, file.PublicID, readyKey); err != nil {
		_ = w.objects.Delete(ctx, readyKey)
		return err
	}
	return w.objects.Delete(ctx, file.ObjectKey)
}

func matchesMetadata(file File, stat storage.ObjectStat) bool {
	checksum, err := base64.StdEncoding.DecodeString(stat.ChecksumSHA256)
	return err == nil && stat.Size == file.Size && stat.ContentType == file.ContentType && subtle.ConstantTimeCompare(checksum, file.SHA256) == 1
}

func (w *VerificationWorker) reject(ctx context.Context, file File) error {
	if err := w.store.MarkRejected(ctx, file.PublicID); err != nil {
		return err
	}
	return w.objects.Delete(ctx, file.ObjectKey)
}
