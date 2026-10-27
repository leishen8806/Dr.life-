// Package storage creates direct-to-object-store signed URLs. It never proxies file bytes.
package storage

import (
	"context"
	"errors"
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
)

type Policy struct {
	AllowedContentTypes []string
	MaxContentLength    int64
}

type PutRequest struct {
	ContentType   string
	ContentLength int64
}

type SignedURL struct {
	Key       string
	URL       string
	ExpiresAt time.Time
}

type Storage interface {
	PresignPut(context.Context, PutRequest) (SignedURL, error)
	PresignGet(context.Context, string) (SignedURL, error)
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
			return nil
		}
	}
	return ErrContentTypeNotAllowed
}
