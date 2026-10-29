// Package files owns private upload metadata. Object bytes remain in storage.
package files

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/storage"
)

var (
	ErrUnsupportedPurpose = errors.New("unsupported file purpose")
	ErrNotConfigured      = errors.New("files service is not configured")
	ErrNotFound           = errors.New("file not found")
	ErrNotReady           = errors.New("file is not ready for download")
	ErrQuotaExceeded      = errors.New("file upload quota exceeded")
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusVerifying Status = "verifying"
	StatusReady     Status = "ready"
)

type File struct {
	PublicID, Purpose, ObjectKey, ContentType string
	OwnerAccount, Size                        int64
	SHA256                                    []byte
	Status                                    Status
	CreatedAt                                 time.Time
}
type Usage struct {
	Count int
	Bytes int64
}
type Store interface {
	Create(context.Context, File) error
	GetOwned(context.Context, int64, string) (File, error)
	MarkVerifying(context.Context, int64, string) (File, error)
	DailyUsage(context.Context, int64, time.Time) (Usage, error)
}
type Options struct {
	NewPublicID     func() string
	Now             func() time.Time
	MaxDailyUploads int
	MaxDailyBytes   int64
}
type PresignInput struct {
	Purpose, ContentType, SHA256 string
	Size                         int64
}
type PresignResult struct {
	File   File
	Upload storage.SignedURL
}
type Service struct {
	store   Store
	objects storage.Storage
	options Options
}

func New(store Store, objects storage.Storage, options Options) *Service {
	if options.NewPublicID == nil {
		options.NewPublicID = randomPublicID
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Service{store: store, objects: objects, options: options}
}

func (s *Service) Presign(ctx context.Context, ownerAccount int64, in PresignInput) (PresignResult, error) {
	if s == nil || s.store == nil || s.objects == nil || ownerAccount <= 0 {
		return PresignResult{}, ErrNotConfigured
	}
	if !purposeAllows(in.Purpose, in.ContentType) {
		return PresignResult{}, ErrUnsupportedPurpose
	}
	usage, err := s.store.DailyUsage(ctx, ownerAccount, s.options.Now().UTC())
	if err != nil {
		return PresignResult{}, err
	}
	if (s.options.MaxDailyUploads > 0 && usage.Count >= s.options.MaxDailyUploads) || (s.options.MaxDailyBytes > 0 && usage.Bytes+in.Size > s.options.MaxDailyBytes) {
		return PresignResult{}, ErrQuotaExceeded
	}
	upload, err := s.objects.PresignPut(ctx, storage.PutRequest{ContentType: in.ContentType, ContentLength: in.Size, ChecksumSHA256: in.SHA256})
	if err != nil {
		return PresignResult{}, err
	}
	checksum, err := decodeChecksum(in.SHA256)
	if err != nil {
		return PresignResult{}, err
	}
	file := File{PublicID: s.options.NewPublicID(), OwnerAccount: ownerAccount, Purpose: in.Purpose, ObjectKey: upload.Key, ContentType: in.ContentType, Size: in.Size, SHA256: checksum, Status: StatusPending, CreatedAt: s.options.Now().UTC()}
	if file.PublicID == "" {
		return PresignResult{}, ErrNotConfigured
	}
	if err := s.store.Create(ctx, file); err != nil {
		return PresignResult{}, err
	}
	return PresignResult{File: file, Upload: upload}, nil
}

// Complete makes a pending upload visible to the verification worker. It does
// not inspect object bytes in the API process.
func (s *Service) Complete(ctx context.Context, ownerAccount int64, publicID string) (File, error) {
	if s == nil || s.store == nil || ownerAccount <= 0 || publicID == "" {
		return File{}, ErrNotConfigured
	}
	return s.store.MarkVerifying(ctx, ownerAccount, publicID)
}

func (s *Service) DownloadURL(ctx context.Context, ownerAccount int64, publicID string) (storage.SignedURL, error) {
	if s == nil || s.store == nil || s.objects == nil || ownerAccount <= 0 || publicID == "" {
		return storage.SignedURL{}, ErrNotConfigured
	}
	file, err := s.store.GetOwned(ctx, ownerAccount, publicID)
	if err != nil {
		return storage.SignedURL{}, err
	}
	if file.Status != StatusReady {
		return storage.SignedURL{}, ErrNotReady
	}
	return s.objects.PresignGet(ctx, file.ObjectKey)
}

func purposeAllows(purpose, contentType string) bool {
	switch purpose {
	case "merchant_kyc":
		return contentType == "image/jpeg" || contentType == "image/png" || contentType == "application/pdf"
	case "product_image", "inbound_photo", "delivery_proof":
		return contentType == "image/jpeg" || contentType == "image/png" || contentType == "image/webp"
	default:
		return false
	}
}
func decodeChecksum(value string) ([]byte, error) {
	// storage validates the wire format before a URL is signed; keeping a byte form avoids re-parsing in stores.
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return nil, storage.ErrChecksumInvalid
	}
	return decoded, nil
}
func randomPublicID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}
