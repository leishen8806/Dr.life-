package main

import "testing"

func TestParseAttributeRejectsNonEnumSKUVariant(t *testing.T) {
	row := []string{"CAT-TILE", "color", "颜色", "Color", "ពណ៌（待审）", "text", "", "false", "true", "", "", "", "test"}
	_, err := parseAttribute(row, 2)
	if err == nil {
		t.Fatal("parseAttribute accepted a non-enum SKU dimension")
	}
}

func TestParseAttributeTrimsKhmerReviewMarkersAndBuildsEnumValues(t *testing.T) {
	row := []string{"CAT-TILE", "size", "尺寸", "Size", "ទំហំ（待审）", "enum", "mm", "true", "true", "300x300|600x600", "300x300|600x600", "300x300（待审）|600x600", "test"}
	attribute, err := parseAttribute(row, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !attribute.KMPendingReview || attribute.Name.KM != "ទំហំ" {
		t.Fatalf("Khmer review marker was not normalized: %#v", attribute)
	}
	if len(attribute.Values) != 2 || attribute.Values[0].KM != "300x300" || attribute.Values[0].Code != "value_1" {
		t.Fatalf("enum values = %#v", attribute.Values)
	}
}
