package ledger

import (
	"math"
	"reflect"
	"testing"
	"testing/quick"
)

func TestAllocateBoundaries(t *testing.T) {
	for _, total := range []int64{1, 2, 3, 7, 97, 999, 1001, 9999, 10001, math.MaxInt64} {
		got, err := Allocate(total, []Share{{BasisPoints: 3333}, {BasisPoints: 3333}, {BasisPoints: 3334, Remainder: true}})
		if err != nil {
			t.Fatal(err)
		}
		if got[0]+got[1]+got[2] != total || got[0] < 0 || got[1] < 0 || got[2] < 0 {
			t.Fatalf("total=%d allocation=%v", total, got)
		}
		again, err := Allocate(total, []Share{{BasisPoints: 3333}, {BasisPoints: 3333}, {BasisPoints: 3334, Remainder: true}})
		if err != nil || !reflect.DeepEqual(got, again) {
			t.Fatalf("allocation is not deterministic: %v / %v (%v)", got, again, err)
		}
	}
}

func TestAllocateProperty(t *testing.T) {
	property := func(rawTotal uint64, rawA, rawB uint16, rawRemainder uint8) bool {
		total := int64(rawTotal%uint64(math.MaxInt64)) + 1
		a := int64(rawA % 10001)
		remaining := int64(10000) - a
		b := int64(rawB) % (remaining + 1)
		shares := []Share{{BasisPoints: a}, {BasisPoints: b}, {BasisPoints: 10000 - a - b}}
		shares[int(rawRemainder)%len(shares)].Remainder = true
		first, err := Allocate(total, shares)
		if err != nil {
			return false
		}
		second, err := Allocate(total, shares)
		if err != nil || !reflect.DeepEqual(first, second) {
			return false
		}
		var sum int64
		for _, amount := range first {
			if amount < 0 || sum > math.MaxInt64-amount {
				return false
			}
			sum += amount
		}
		return sum == total
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 10000}); err != nil {
		t.Fatal(err)
	}
}

func TestAllocateRejectsInvalidShares(t *testing.T) {
	for _, shares := range [][]Share{
		nil,
		{{BasisPoints: 10000}},
		{{BasisPoints: 5000, Remainder: true}, {BasisPoints: 4999}},
		{{BasisPoints: 5000, Remainder: true}, {BasisPoints: 5000, Remainder: true}},
		{{BasisPoints: -1, Remainder: true}, {BasisPoints: 10001}},
	} {
		if _, err := Allocate(1, shares); err == nil {
			t.Fatalf("shares=%v accepted", shares)
		}
	}
}
