package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/logx"
)

func TestRequestIDIsGeneratedOrEchoed(t *testing.T) {
	for _, supplied := range []string{"", "a", "caller-request-123", strings.Repeat("x", 64)} {
		t.Run("supplied="+supplied, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if supplied != "" {
				r.Header.Set("X-Request-ID", supplied)
			}
			w := httptest.NewRecorder()
			handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				WriteOK(w, r, map[string]bool{"ready": true})
			}))
			handler.ServeHTTP(w, r)

			requestID := w.Header().Get("X-Request-ID")
			if requestID == "" || (supplied != "" && requestID != supplied) {
				t.Fatalf("unexpected response request id %q", requestID)
			}
			var response map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response["request_id"] != requestID {
				t.Fatalf("header and body request IDs differ: %#v", response)
			}
		})
	}
}

func TestRequestIDRejectsInvalidClientValues(t *testing.T) {
	for _, supplied := range []string{strings.Repeat("a", 65), "has space", "has\nnewline", "special!"} {
		t.Run(supplied, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("X-Request-ID", supplied)
			w := httptest.NewRecorder()
			RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				WriteOK(w, r, nil)
			})).ServeHTTP(w, r)
			generated := w.Header().Get("X-Request-ID")
			if generated == "" || generated == supplied {
				t.Fatalf("invalid request id was not replaced: %q", generated)
			}
			var response map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response["request_id"] != generated {
				t.Fatalf("header and body request IDs differ: header=%q body=%#v", generated, response["request_id"])
			}
		})
	}
}

func TestResponsesHaveUniformShapeAndTranslations(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(contextWithRequestID(r, "req-test"))
	w := httptest.NewRecorder()
	WriteOK(w, r, map[string]string{"result": "ok"})

	var success map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &success); err != nil {
		t.Fatal(err)
	}
	if success["ok"] != true || success["request_id"] != "req-test" || success["data"] == nil {
		t.Fatalf("unexpected success shape: %#v", success)
	}

	for code, appErr := range appErrors {
		if appErr.Message.ZH == "" || appErr.Message.EN == "" || appErr.Message.KM == "" {
			t.Errorf("error %d is missing a translation: %+v", code, appErr.Message)
		}
	}
	w = httptest.NewRecorder()
	WriteError(w, r, NewAppError(CodeInvalidArgument))
	var failure map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if failure["ok"] != false || failure["request_id"] != "req-test" {
		t.Fatalf("unexpected error shape: %#v", failure)
	}
	errorObject := failure["error"].(map[string]any)
	if errorObject["code"] != float64(CodeInvalidArgument) {
		t.Fatalf("unexpected error code: %#v", errorObject)
	}
	message := errorObject["message"].(map[string]any)
	for _, language := range []string{"zh", "en", "km"} {
		if message[language] == "" {
			t.Errorf("missing %s message: %#v", language, message)
		}
	}
}

func TestRecoverReturnsGenericErrorAndDoesNotLogPanicValue(t *testing.T) {
	var output bytes.Buffer
	logger := logx.NewLogger(0, &output)
	r := httptest.NewRequest(http.MethodGet, "/panic", nil)
	w := httptest.NewRecorder()
	handler := RequestID(Recover(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("sensitive panic details")
	})))
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if !strings.Contains(output.String(), `"stack":"goroutine `) {
		t.Fatalf("panic stack missing from log: %s", output.String())
	}
	if strings.Contains(w.Body.String(), "sensitive panic details") || strings.Contains(output.String(), "sensitive panic details") || strings.Contains(w.Body.String(), "goroutine") || strings.Contains(w.Body.String(), "httpx_test.go") {
		t.Fatalf("panic details leaked: response=%s log=%s", w.Body.String(), output.String())
	}
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["error"].(map[string]any)["code"] != float64(CodeInternalError) {
		t.Fatalf("unexpected panic response: %#v", response)
	}
}

func TestRequestLogUsesRoutePatternAndDoesNotLogQueryOrBody(t *testing.T) {
	var output bytes.Buffer
	logger := logx.NewLogger(0, &output)
	router := chi.NewRouter()
	router.Use(RequestID)
	router.Use(RequestLogger(logger))
	router.Use(Recover(logger))
	router.Post("/users/{userID}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r := httptest.NewRequest(http.MethodPost, "/users/0123456789?token=query-secret", strings.NewReader("request-body-secret"))
	router.ServeHTTP(httptest.NewRecorder(), r)

	line := output.String()
	for _, forbidden := range []string{"0123456789", "query-secret", "request-body-secret"} {
		if strings.Contains(line, forbidden) {
			t.Errorf("request log contains %q: %s", forbidden, line)
		}
	}
	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["path"] != "/users/{userID}" || entry["method"] != "POST" || entry["status"] != float64(http.StatusNoContent) || entry["request_id"] == "" || entry["role"] != "" {
		t.Fatalf("unexpected access log: %#v", entry)
	}
}

func contextWithRequestID(r *http.Request, requestID string) context.Context {
	return context.WithValue(r.Context(), requestIDKey{}, requestID)
}
