package api

import (
	"net/http"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

type Handler struct{}

func (Handler) GetPublicPing(w http.ResponseWriter, r *http.Request) {
	httpx.WriteOK(w, r, map[string]bool{"pong": true})
}
