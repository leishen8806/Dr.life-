// Package storage creates direct-to-object-store signed URLs. It never proxies file bytes.
package storage

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"time"
)

const (
	putTTL = 10 * time.Minute
	getTTL = 5 * time.Minute
)

var (
	ErrContentTypeNotAllowed = errors.New("storage content type is not allowed")
	ErrContentLengthTooLarge = errors.New("storage content length exceeds limit")
	ErrContentLengthInvalid  = errors.New("storage content length must be positive")
	ErrURLExpired            = errors.New("storage signed URL has expired")
	ErrInvalidSignature      = errors.New("storage signed URL signature is invalid")
	ErrPolicyNotConfigured   = errors.New("storage upload policy must be explicitly configured")
	ErrSecretRequired        = errors.New("storage local signing secret is required")
	ErrInvalidObjectKey      = errors.New("storage object key is invalid")
	ErrChecksumInvalid       = errors.New("storage SHA-256 checksum is invalid")
)

type Policy struct {
	AllowedContentTypes []string
	MaxContentLength    int64
}

type PutRequest struct {
	ContentType    string
	ContentLength  int64
	ChecksumSHA256 string
}

type SignedURL struct {
	Key       string
	URL       string
	ExpiresAt time.Time
	Headers   map[string]string
}

// ObjectStat is immutable object metadata returned by the storage provider.
// ChecksumSHA256 uses standard base64 as required by S3's checksum header.
type ObjectStat struct {
	Size           int64
	ContentType    string
	ChecksumSHA256 string
	ETag           string
}

type Storage interface {
	PresignPut(context.Context, PutRequest) (SignedURL, error)
	PresignGet(context.Context, string) (SignedURL, error)
	Stat(context.Context, string) (ObjectStat, error)
}

// ObjectVerifier is available only to the verification worker that streams
// object contents and deletes rejected uploads.
type ObjectVerifier interface {
	Stat(context.Context, string) (ObjectStat, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Promote(context.Context, string, string) (string, error)
	Delete(context.Context, string) error
}

func (p Policy) validate(in PutRequest) error {
	if len(p.AllowedContentTypes) == 0 || p.MaxContentLength <= 0 {
		return ErrPolicyNotConfigured
	}
	if in.ContentLength <= 0 {
		return ErrContentLengthInvalid
	}
	if p.MaxContentLength > 0 && in.ContentLength > p.MaxContentLength {
		return ErrContentLengthTooLarge
	}
	for _, contentType := range p.AllowedContentTypes {
		if in.ContentType == contentType {
			checksum, err := base64.StdEncoding.DecodeString(in.ChecksumSHA256)
			if err != nil || len(checksum) != 32 {
				return ErrChecksumInvalid
			}
			return nil
		}
	}
	return ErrContentTypeNotAllowed
}
