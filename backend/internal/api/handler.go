package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/leishen8806/Dr.life-/backend/internal/api/gen"
	"github.com/leishen8806/Dr.life-/backend/internal/iam"
	"github.com/leishen8806/Dr.life-/backend/internal/iam/telegram"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
)

type TelegramLogin interface {
	Login(context.Context, iam.LoginInput) (iam.LoginResult, error)
}

type Handler struct {
	Login    TelegramLogin
	ClientIP gateway.ClientIPResolver
}

func (Handler) GetPublicPing(w http.ResponseWriter, r *http.Request) {
	httpx.WriteOK(w, r, map[string]bool{"pong": true})
}

func (h Handler) PostTelegramAuth(w http.ResponseWriter, r *http.Request) {
	if h.Login == nil {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
		return
	}
	var request gen.TelegramAuthRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, telegram.MaxInitDataBytes+1024))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&request)
	var trailing any
	trailingErr := decoder.Decode(&trailing)
	if err != nil || trailingErr != io.EOF || request.InitData == "" || len(request.InitData) > telegram.MaxInitDataBytes || !telegram.End(request.End).Valid() {
		httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInvalidArgument))
		return
	}
	device := ""
	if request.DeviceId != nil {
		device = *request.DeviceId
	}
	result, err := h.Login.Login(r.Context(), iam.LoginInput{End: telegram.End(request.End), InitData: request.InitData, DeviceID: device, IP: h.ClientIP.Resolve(r)})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteOK(w, r, result)
}
