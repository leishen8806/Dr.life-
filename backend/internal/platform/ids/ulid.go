package ids

import (
	"crypto/rand"
	"errors"
	"math/big"
	"time"
)

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var ErrRandomSource = errors.New("ids: random source failed")

// NewULID returns a 26-character Crockford Base32 identifier.
func NewULID() (string, error) {
	var raw [16]byte
	millis := uint64(time.Now().UnixMilli())
	for i := 5; i >= 0; i-- {
		raw[i] = byte(millis)
		millis >>= 8
	}
	if _, err := rand.Read(raw[6:]); err != nil {
		return "", ErrRandomSource
	}
	n := new(big.Int).SetBytes(raw[:])
	base := big.NewInt(32)
	rem := new(big.Int)
	out := make([]byte, 26)
	for i := len(out) - 1; i >= 0; i-- {
		n.QuoRem(n, base, rem)
		out[i] = crockford[rem.Int64()]
	}
	return string(out), nil
}
