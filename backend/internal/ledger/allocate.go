package ledger

import (
	"math/big"
)

type Share struct {
	BasisPoints int64
	Remainder   bool
}

func Allocate(total int64, shares []Share) ([]int64, error) {
	if total <= 0 || len(shares) == 0 {
		return nil, ErrInvalidAmount
	}
	var basisPoints int64
	remainder := -1
	for i, share := range shares {
		if share.BasisPoints < 0 || share.BasisPoints > 10000 || share.Remainder && remainder >= 0 {
			return nil, ErrInvalidRequest
		}
		basisPoints += share.BasisPoints
		if share.Remainder {
			remainder = i
		}
	}
	if basisPoints != 10000 || remainder < 0 {
		return nil, ErrInvalidRequest
	}

	result := make([]int64, len(shares))
	var allocated int64
	for i, share := range shares {
		if i == remainder {
			continue
		}
		amount := new(big.Int).Mul(big.NewInt(total), big.NewInt(share.BasisPoints))
		amount.Quo(amount, big.NewInt(10000))
		result[i] = amount.Int64()
		allocated += result[i]
	}
	result[remainder] = total - allocated
	if result[remainder] < 0 {
		return nil, ErrInvalidAmount
	}
	return result, nil
}
