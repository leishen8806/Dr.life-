package files

import "testing"

func TestPresignRejectsUnsupportedPurpose(t *testing.T) {
	service := New(nil, nil, Options{})
	if _, err := service.Presign(t.Context(), 1, PresignInput{Purpose: "unknown"}); err == nil {
		t.Fatal("Presign accepted an unsupported purpose")
	}
}
