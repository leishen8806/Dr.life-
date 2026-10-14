package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/leishen8806/Dr.life-/backend/internal/iam"
	"github.com/leishen8806/Dr.life-/backend/internal/iam/telegram"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

type fakeLogin struct {
	input  iam.LoginInput
	result iam.LoginResult
	err    error
}

func (f *fakeLogin) Login(_ context.Context, input iam.LoginInput) (iam.LoginResult, error) {
	f.input = input
	return f.result, f.err
}

func TestTelegramAuthHandlerContract(t *testing.T) {
	fake := &fakeLogin{result: iam.LoginResult{AccessToken: "access", AccessExpiresIn: 900, RefreshToken: "refresh", RefreshExpiresIn: 2592000, Account: iam.LoginAccount{PublicID: "account", Role: "buyer", Status: "active"}, NeedsLanguage: true, IsNewAccount: true}}
	h := Handler{Login: fake}
	req := httptest.NewRequest(http.MethodPost, "/v1/public/auth/telegram", strings.NewReader(`{"end":"buyer","init_data":"signed","device_id":"device:1"}`))
	req.RemoteAddr = "198.51.100.2:1234"
	w := httptest.NewRecorder()
	httpx.RequestID(http.HandlerFunc(h.PostTelegramAuth)).ServeHTTP(w, req)
	if w.Code != http.StatusOK || fake.input.End != telegram.Buyer || fake.input.IP != "198.51.100.2" || fake.input.DeviceID != "device:1" {
		t.Fatalf("status/input=%d %+v body=%s", w.Code, fake.input, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["ok"] != true || body["request_id"] == "" {
		t.Fatalf("body=%v", body)
	}
}

func TestTelegramAuthHandlerRejectsInvalidAndUnifiesErrors(t *testing.T) {
	for _, body := range []string{`{}`, `{"end":"admin","init_data":"x"}`, `{"end":"buyer","init_data":"x","extra":true}`, `{"end":"buyer","init_data":"` + strings.Repeat("x", telegram.MaxInitDataBytes+1) + `"}`} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/public/auth/telegram", strings.NewReader(body))
		httpx.RequestID(http.HandlerFunc((Handler{Login: &fakeLogin{}}).PostTelegramAuth)).ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"code":1002`) {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}
	fake := &fakeLogin{err: httpx.NewAppError(httpx.CodeTelegramLoginFailed)}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/public/auth/telegram", strings.NewReader(`{"end":"buyer","init_data":"contains-private-detail"}`))
	httpx.RequestID(http.HandlerFunc((Handler{Login: fake}).PostTelegramAuth)).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "private-detail") || !strings.Contains(w.Body.String(), `"code":2003`) {
		t.Fatalf("body=%s", w.Body.String())
	}
}
