package gateway

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

// ConsentChecker is a narrow IAM port. The gateway never reads ops tables.
type ConsentChecker interface {
	NeedsConsent(context.Context, string) (bool, error)
}

type unpublishedConsentKindsChecker interface {
	UnpublishedKinds(context.Context, string) ([]string, error)
}

func RequireCurrentConsent(checker ConsentChecker, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if checker == nil || consentExemptPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			identity, ok := IdentityFromContext(r.Context())
			if !ok || (identity.Role != RoleBuyer && identity.Role != RoleMerchant && identity.Role != RoleDriver) {
				next.ServeHTTP(w, r)
				return
			}
			needs, err := checker.NeedsConsent(r.Context(), identity.AccountID)
			if err != nil {
				if logger != nil {
					logger.ErrorContext(r.Context(), "consent check failed; returning service unavailable", "error", err)
				}
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
				return
			}
			if kindsChecker, ok := checker.(unpublishedConsentKindsChecker); ok {
				missing, missingErr := kindsChecker.UnpublishedKinds(r.Context(), identity.AccountID)
				if missingErr != nil {
					if logger != nil {
						logger.ErrorContext(r.Context(), "consent check failed; returning service unavailable", "error", missingErr)
					}
					httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
					return
				}
				if logger != nil {
					for _, kind := range missing {
						logger.WarnContext(r.Context(), "consent document is not published; allowing that kind", "event", "consent_no_published_document", "kind", kind)
					}
				}
			}
			if needs {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeConsentRequired))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func consentExemptPath(path string) bool {
	switch path {
	case "/v1/public/auth/telegram", "/v1/public/auth/refresh", "/v1/public/legal/privacy", "/v1/public/legal/terms":
		return true
	}
	for _, end := range []string{"buyer", "merchant", "driver"} {
		base := "/v1/" + end + "/me"
		if path == base || path == base+"/consents" || path == base+"/consents/revoke" {
			return true
		}
	}
	return false
}
