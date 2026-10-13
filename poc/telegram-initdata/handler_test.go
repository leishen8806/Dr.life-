package telegraminitdata

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestVerifyHandler(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	validator := NewValidator()
	validator.Now = func() time.Time { return now }
	handler := NewHandler(PoolVerifier{
		Validator: validator,
		Pools:     map[End][]Bot{Buyer: {{Label: "primary", Token: testToken}}},
	})

	body, _ := json.Marshal(map[string]string{"end": "buyer", "init_data": signedAt(now, testToken)})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/poc/auth/verify", bytes.NewReader(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var result map[string]any
	_ = json.Unmarshal(response.Body.Bytes(), &result)
	if result["hit_bot"] != "buyer/primary" || result["tried_count"] != float64(1) {
		t.Fatalf("body = %v", result)
	}

	badBody, _ := json.Marshal(map[string]string{"end": "buyer", "init_data": "invalid"})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/poc/auth/verify", bytes.NewReader(badBody)))
	if response.Code != http.StatusUnauthorized || response.Body.String() == "" {
		t.Fatalf("invalid response = %d %s", response.Code, response.Body.String())
	}
	if bytes.Contains(response.Body.Bytes(), []byte("hash")) || bytes.Contains(response.Body.Bytes(), []byte("token")) {
		t.Fatalf("failure leaked internal detail: %s", response.Body.String())
	}
}

func TestVerifyHandlerRejectsMalformedRequest(t *testing.T) {
	handler := NewHandler(PoolVerifier{Validator: NewValidator()})
	for _, body := range []string{
		`{"end":"unknown","init_data":"x"}`,
		`{"end":"buyer","init_data":"x","unexpected":true}`,
		`{"end":"buyer","init_data":"x"}{}`,
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/poc/auth/verify", bytes.NewBufferString(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %q: status = %d", body, response.Code)
		}
	}
}
