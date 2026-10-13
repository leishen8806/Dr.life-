package telegraminitdata

import (
	"testing"
	"time"
)

func TestPoolFallbackAndEndIsolation(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	validator := NewValidator()
	validator.Now = func() time.Time { return now }
	standbyData := signedAt(now, "buyer-standby")
	pools := map[End][]Bot{
		Buyer:    {{Label: "primary", Token: "buyer-primary"}, {Label: "standby", Token: "buyer-standby"}},
		Merchant: {{Label: "primary", Token: "merchant-primary"}},
	}
	verifier := PoolVerifier{Validator: validator, Pools: pools}

	result, err := verifier.Verify(Buyer, standbyData)
	if err != nil || result.HitBot != "buyer/standby" || result.TriedCount != 2 {
		t.Fatalf("Verify() = %+v, %v", result, err)
	}
	if _, err := verifier.Verify(Merchant, standbyData); err == nil {
		t.Fatal("buyer initData passed merchant pool")
	}

	// Removing the primary models a revoked bot token: standby stays valid, old primary data fails.
	verifier.Pools[Buyer] = verifier.Pools[Buyer][1:]
	result, err = verifier.Verify(Buyer, standbyData)
	if err != nil || result.HitBot != "buyer/standby" || result.TriedCount != 1 {
		t.Fatalf("standby after removal = %+v, %v", result, err)
	}
	if _, err := verifier.Verify(Buyer, signedAt(now, "buyer-primary")); err == nil {
		t.Fatal("removed primary token still validated")
	}
}

func TestLoadPoolsFromEnv(t *testing.T) {
	t.Setenv("POC_TG_BUYER_PRIMARY_TOKEN", "primary-value")
	t.Setenv("POC_TG_BUYER_STANDBY_TOKEN", "standby-value")
	t.Setenv("POC_TG_MERCHANT_PRIMARY_TOKEN", "merchant-value")
	pools := LoadPoolsFromEnv()
	if len(pools[Buyer]) != 2 || pools[Buyer][0].Label != "primary" || pools[Buyer][1].Label != "standby" {
		t.Fatalf("buyer pool = %#v", pools[Buyer])
	}
	if len(pools[Merchant]) != 1 || len(pools[Driver]) != 0 {
		t.Fatalf("unexpected pools = %#v", pools)
	}
}
