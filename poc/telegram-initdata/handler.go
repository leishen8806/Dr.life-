package telegraminitdata

import (
	"encoding/json"
	"io"
	"net/http"
)

func NewHandler(verifier PoolVerifier) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /poc/auth/verify", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			End      End    `json:"end"`
			InitData string `json:"init_data"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxInitDataBytes+1024)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF || (request.End != Buyer && request.End != Merchant && request.End != Driver) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error_code": "INVALID_REQUEST"})
			return
		}
		result, err := verifier.Verify(request.End, request.InitData)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error_code": "INVALID_INIT_DATA"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":          true,
			"hit_bot":     result.HitBot,
			"tried_count": result.TriedCount,
			"tg_user_id":  result.Data.User.ID,
			"auth_date":   result.Data.AuthDate.Unix(),
		})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
