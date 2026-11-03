package gateway

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type consentCheckerStub struct {
	needs bool
	err   error
}

type noPublishedDocumentStub struct{}

func (noPublishedDocumentStub) Error() string                    { return "no published legal document" }
func (noPublishedDocumentStub) NoPublishedLegalDocument() string { return "privacy" }

func (s consentCheckerStub) NeedsConsent(context.Context, string) (bool, error) {
	return s.needs, s.err
}

func TestRequireCurrentConsentBlocksProtectedRoutesButAllowsConsentFlow(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := RequireCurrentConsent(consentCheckerStub{needs: true}, nil)(next)
	for _, test := range []struct {
		path string
		want int
	}{
		{path: "/v1/buyer/products", want: http.StatusForbidden},
		{path: "/v1/buyer/me", want: http.StatusNoContent},
		{path: "/v1/buyer/me/consents", want: http.StatusNoContent},
		{path: "/v1/public/legal/terms", want: http.StatusNoContent},
		{path: "/v1/public/ping", want: http.StatusForbidden},
		{path: "/v1/buyer/me/consents-extra", want: http.StatusForbidden},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		withIdentity(request, Identity{AccountID: "account-1", Role: RoleBuyer})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Fatalf("path=%s status=%d want=%d body=%s", test.path, response.Code, test.want, response.Body.String())
		}
	}
}

func TestRequireCurrentConsentTreatsUnexpectedNoPublishedErrorAsUnavailable(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := RequireCurrentConsent(consentCheckerStub{err: noPublishedDocumentStub{}}, logger)(next)
	request := httptest.NewRequest(http.MethodGet, "/v1/buyer/products", nil)
	withIdentity(request, Identity{AccountID: "account-1", Role: RoleBuyer})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want=%d", response.Code, http.StatusServiceUnavailable)
	}
	if got := logs.String(); !strings.Contains(got, "level=ERROR") || strings.Contains(got, "consent_no_published_document") {
		t.Fatalf("logs=%q, want error rather than fail-open warning", got)
	}
}

func TestRequireCurrentConsentLogsStoreFailureAtError(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := RequireCurrentConsent(consentCheckerStub{err: errors.New("database unavailable")}, logger)(next)
	request := httptest.NewRequest(http.MethodGet, "/v1/buyer/products", nil)
	withIdentity(request, Identity{AccountID: "account-1", Role: RoleBuyer})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want=%d", response.Code, http.StatusServiceUnavailable)
	}
	if got := logs.String(); !strings.Contains(got, "level=ERROR") || !strings.Contains(got, "consent check failed") || strings.Contains(got, "consent document is not published") {
		t.Fatalf("logs=%q, want error store-failure entry", got)
	}
}
