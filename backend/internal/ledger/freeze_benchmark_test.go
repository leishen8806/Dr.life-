package ledger

import (
	"math"
	"testing"
)

func BenchmarkSpotCheckDispatch(b *testing.B) {
	postings := []Posting{{AccountID: 1}, {AccountID: 2}}
	b.Run("disabled", func(b *testing.B) {
		service := &Service{}
		for b.Loop() {
			service.maybeSpotCheck(postings)
		}
	})
	b.Run("enabled_not_due", func(b *testing.B) {
		service := &Service{spotCheckEvery: math.MaxUint64}
		for b.Loop() {
			service.maybeSpotCheck(postings)
		}
	})
}
