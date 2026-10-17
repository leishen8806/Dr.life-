package catalog_test

import (
	"errors"
	"testing"

	"github.com/leishen8806/Dr.life-/backend/internal/catalog"
)

func TestValidateLocalizedRequiresAllThreeLanguages(t *testing.T) {
	cases := []struct {
		name      string
		text      catalog.LocalizedText
		wantField string
	}{
		{name: "complete", text: catalog.LocalizedText{"zh": "瓷砖", "en": "Tiles", "km": "ក្បឿង"}},
		{name: "missing km", text: catalog.LocalizedText{"zh": "瓷砖", "en": "Tiles"}, wantField: "name.km"},
		{name: "blank en", text: catalog.LocalizedText{"zh": "瓷砖", "en": "   ", "km": "ក្បឿង"}, wantField: "name.en"},
		{name: "empty", text: catalog.LocalizedText{}, wantField: "name.zh"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := catalog.ValidateLocalized("name", tc.text)
			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("ValidateLocalized() = %v, want nil", err)
				}
				return
			}
			var invalid *catalog.ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("ValidateLocalized() = %v, want *catalog.ValidationError", err)
			}
			if invalid.Field != tc.wantField {
				t.Fatalf("field = %q, want %q", invalid.Field, tc.wantField)
			}
		})
	}
}

func TestValidateTiersEnforcesMinimumTwoAndUniqueMOQ(t *testing.T) {
	valid := []catalog.Tier{
		{MinQty: "10", UnitPriceE4: 140000},
		{MinQty: "100", UnitPriceE4: 120000},
	}
	if err := catalog.ValidateTiers(valid); err != nil {
		t.Fatalf("ValidateTiers(valid) = %v, want nil", err)
	}

	cases := []struct {
		name      string
		tiers     []catalog.Tier
		wantField string
	}{
		{name: "empty", tiers: nil, wantField: "tiers"},
		{name: "single tier", tiers: valid[:1], wantField: "tiers"},
		{name: "duplicate moq spelling", tiers: []catalog.Tier{{MinQty: "10", UnitPriceE4: 1}, {MinQty: "10.0", UnitPriceE4: 2}}, wantField: "tiers[1].min_qty"},
		{name: "zero moq", tiers: []catalog.Tier{{MinQty: "0", UnitPriceE4: 1}, {MinQty: "5", UnitPriceE4: 2}}, wantField: "tiers[0].min_qty"},
		{name: "negative price", tiers: []catalog.Tier{{MinQty: "1", UnitPriceE4: -1}, {MinQty: "5", UnitPriceE4: 2}}, wantField: "tiers[0].unit_price_e4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var invalid *catalog.ValidationError
			if err := catalog.ValidateTiers(tc.tiers); !errors.As(err, &invalid) {
				t.Fatalf("ValidateTiers() = %v, want *catalog.ValidationError", err)
			} else if invalid.Field != tc.wantField {
				t.Fatalf("field = %q, want %q", invalid.Field, tc.wantField)
			}
		})
	}
}

func TestValidateProductForPublishGatesTrilingualAndTiers(t *testing.T) {
	tiers := []catalog.Tier{{MinQty: "10", UnitPriceE4: 140000}, {MinQty: "100", UnitPriceE4: 120000}}
	complete := catalog.LocalizedText{"zh": "瓷砖", "en": "Tiles", "km": "ក្បឿង"}

	product := catalog.Product{Name: complete, Status: catalog.StatusDraft}
	if err := catalog.ValidateProductForPublish(product, tiers); err != nil {
		t.Fatalf("complete draft = %v, want nil", err)
	}

	product.Name = catalog.LocalizedText{"zh": "瓷砖", "en": "Tiles"}
	var invalid *catalog.ValidationError
	if err := catalog.ValidateProductForPublish(product, tiers); !errors.As(err, &invalid) || invalid.Field != "name.km" {
		t.Fatalf("incomplete trilingual = %v, want name.km", err)
	}

	product.Name = complete
	if err := catalog.ValidateProductForPublish(product, tiers[:1]); !errors.As(err, &invalid) || invalid.Field != "tiers" {
		t.Fatalf("single tier = %v, want tiers", err)
	}

	product.Status = catalog.StatusOn
	if err := catalog.ValidateProductForPublish(product, tiers); !errors.As(err, &invalid) || invalid.Field != "status" {
		t.Fatalf("already published = %v, want status", err)
	}
}

func TestParseDecimalCanonicalisesAndRejectsFloatInput(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		scale   int
		want    string
		wantErr bool
	}{
		{name: "integer", in: "12", scale: 3, want: "12"},
		{name: "trailing zeros trimmed", in: "12.500", scale: 3, want: "12.5"},
		{name: "full scale", in: "0.0001", scale: 4, want: "0.0001"},
		{name: "too many decimals", in: "0.0001", scale: 3, wantErr: true},
		{name: "negative", in: "-1", scale: 3, wantErr: true},
		{name: "two separators", in: "1.2.3", scale: 3, wantErr: true},
		{name: "empty", in: "", scale: 3, wantErr: true},
		{name: "padded", in: " 8 ", scale: 3, want: "8"},
		{name: "zero", in: "0", scale: 3, want: "0"},
		{name: "leading zeros", in: "00012.0", scale: 3, want: "12"},
		{name: "exponent notation", in: "1e3", scale: 3, wantErr: true},
		{name: "dangling separator", in: "3.", scale: 3, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := catalog.ParseDecimal(tc.in, tc.scale)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseDecimal(%q) = %q, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDecimal(%q) error = %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ParseDecimal(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestProductStatusIsBoundedAndMerchantSettable(t *testing.T) {
	for _, status := range []catalog.ProductStatus{catalog.StatusDraft, catalog.StatusReview, catalog.StatusOn, catalog.StatusOff, catalog.StatusRejected} {
		if !status.Valid() {
			t.Fatalf("%q.Valid() = false", status)
		}
	}
	if catalog.ProductStatus("bogus").Valid() {
		t.Fatal("bogus status reported valid")
	}
	for _, status := range []catalog.ProductStatus{catalog.StatusDraft, catalog.StatusOff} {
		if !status.MerchantSettable() {
			t.Fatalf("%q.MerchantSettable() = false", status)
		}
	}
	for _, status := range []catalog.ProductStatus{catalog.StatusOn, catalog.StatusReview, catalog.StatusRejected} {
		if status.MerchantSettable() {
			t.Fatalf("%q.MerchantSettable() = true, want false", status)
		}
	}
}
