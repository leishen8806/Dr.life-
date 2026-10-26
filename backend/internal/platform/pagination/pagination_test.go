package pagination

import (
	"net/url"
	"slices"
	"testing"
)

func mustValues(t *testing.T, raw string) url.Values {
	t.Helper()
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	return values
}

func TestParsePage(t *testing.T) {
	page, err := ParsePage(mustValues(t, ""))
	if err != nil || page != (Page{Number: 1, Size: 20}) {
		t.Fatalf("default page = %+v err = %v", page, err)
	}
	page, err = ParsePage(mustValues(t, "page=3&page_size=100"))
	if err != nil || page != (Page{Number: 3, Size: 100}) {
		t.Fatalf("parsed page = %+v err = %v", page, err)
	}
	for _, raw := range []string{"page=0", "page=-1", "page=abc", "page_size=0", "page_size=101", "page_size=abc"} {
		if _, err := ParsePage(mustValues(t, raw)); err == nil {
			t.Fatalf("expected error for %q", raw)
		}
	}
}

func TestParseSortRejectsUnknownFields(t *testing.T) {
	sorts, err := ParseSort("name,-created_at", "name", "created_at")
	if err != nil {
		t.Fatal(err)
	}
	if len(sorts) != 2 || sorts[0] != (Sort{Field: "name"}) || sorts[1] != (Sort{Field: "created_at", Desc: true}) {
		t.Fatalf("sorts = %+v", sorts)
	}
	for _, raw := range []string{"unknown", "name,-", "name,unknown", ","} {
		if _, err := ParseSort(raw, "name", "created_at"); err == nil {
			t.Fatalf("expected error for %q", raw)
		}
	}
	if sorts, err := ParseSort("", "name"); err != nil || len(sorts) != 0 {
		t.Fatalf("empty sorts = %+v err = %v", sorts, err)
	}
}

func TestParseFilterRejectsUnknownFields(t *testing.T) {
	filters, err := ParseFilter(mustValues(t, "filter[status]=paid&filter[merchant_id]=m_1"), "status", "merchant_id")
	if err != nil {
		t.Fatal(err)
	}
	if filters["status"] != "paid" || filters["merchant_id"] != "m_1" {
		t.Fatalf("filters = %+v", filters)
	}
	for _, raw := range []string{"filter[owner]=x", "filter[]=x"} {
		if _, err := ParseFilter(mustValues(t, raw), "status"); err == nil {
			t.Fatalf("expected error for %q", raw)
		}
	}
}

func TestCursorRoundTrip(t *testing.T) {
	values := []string{"2026-10-17T00:00:00Z", "order_01"}
	decoded, err := DecodeCursor(EncodeCursor(values...))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(decoded, values) {
		t.Fatalf("cursor round trip = %v, want %v", decoded, values)
	}
	for _, raw := range []string{"", "not-base64!", "W10"} {
		if _, err := DecodeCursor(raw); err == nil {
			t.Fatalf("expected error for %q", raw)
		}
	}
}
