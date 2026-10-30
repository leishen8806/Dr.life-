package logistics

import (
	"errors"
	"io"
	"net/http"
)

// RouteFitReceiver is the unauthenticated, signature-verified entry point for
// RouteFit events. Persistence is intentionally injected so the HTTP boundary
// cannot acknowledge an event before its transaction commits.
type RouteFitReceiver struct {
	Verifier RouteFitWebhookVerifier
	Record   func(r *http.Request, event RouteFitEvent, rawBody []byte) error
}

func (h RouteFitReceiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if err := h.Verifier.Verify(r, body); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	event, err := ParseRouteFitEvent(body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if h.Record == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if err := h.Record(r, event, body); err != nil {
		if errors.Is(err, ErrInvalidRouteFitEvent) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
