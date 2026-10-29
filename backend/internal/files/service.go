package files

import (
	"context"
	"errors"
)

type Options struct{}
type PresignInput struct{ Purpose string }
type PresignResult struct{}
type Service struct{}

func New(_ any, _ any, _ Options) *Service { return &Service{} }

func (*Service) Presign(_ context.Context, _ int64, in PresignInput) (PresignResult, error) {
	switch in.Purpose {
	case "merchant_kyc", "product_image", "inbound_photo", "delivery_proof":
		return PresignResult{}, errors.New("files service is not configured")
	default:
		return PresignResult{}, errors.New("unsupported file purpose")
	}
}
