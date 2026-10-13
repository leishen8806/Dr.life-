package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
)

type ErrorCode int

const (
	ErrorCodeCommonBase    ErrorCode = 1000
	ErrorCodeIdentityBase  ErrorCode = 2000
	ErrorCodeCommerceBase  ErrorCode = 3000
	ErrorCodeLogisticsBase ErrorCode = 4000
	ErrorCodeFinanceBase   ErrorCode = 5000
	ErrorCodeAIBase        ErrorCode = 6000
	ErrorCodeExternalBase  ErrorCode = 7000

	CodeInternalError      ErrorCode = 1001
	CodeInvalidArgument    ErrorCode = 1002
	CodeResourceNotFound   ErrorCode = 1003
	CodeTooManyRequests    ErrorCode = 1004
	CodeServiceUnavailable ErrorCode = 1005
	CodeMethodNotAllowed   ErrorCode = 1006
	CodeUnauthenticated    ErrorCode = 2001
	CodePermissionDenied   ErrorCode = 2002
)

type Messages struct {
	ZH string `json:"zh"`
	EN string `json:"en"`
	KM string `json:"km"`
}

type AppError struct {
	Code       ErrorCode
	StatusCode int
	Message    Messages
}

func (e *AppError) Error() string {
	return "application error"
}

var appErrors = map[ErrorCode]AppError{
	CodeInternalError:      {Code: CodeInternalError, StatusCode: http.StatusInternalServerError, Message: Messages{ZH: "服务器内部错误", EN: "Internal server error", KM: "Internal server error"}},
	CodeInvalidArgument:    {Code: CodeInvalidArgument, StatusCode: http.StatusBadRequest, Message: Messages{ZH: "请求参数无效", EN: "Invalid request parameters", KM: "Invalid request parameters"}},
	CodeResourceNotFound:   {Code: CodeResourceNotFound, StatusCode: http.StatusNotFound, Message: Messages{ZH: "请求的资源不存在", EN: "The requested resource was not found", KM: "The requested resource was not found"}},
	CodeTooManyRequests:    {Code: CodeTooManyRequests, StatusCode: http.StatusTooManyRequests, Message: Messages{ZH: "请求过于频繁", EN: "Too many requests", KM: "Too many requests"}},
	CodeServiceUnavailable: {Code: CodeServiceUnavailable, StatusCode: http.StatusServiceUnavailable, Message: Messages{ZH: "服务暂时不可用", EN: "Service temporarily unavailable", KM: "Service temporarily unavailable"}},
	CodeMethodNotAllowed:   {Code: CodeMethodNotAllowed, StatusCode: http.StatusMethodNotAllowed, Message: Messages{ZH: "请求方法不允许", EN: "Method not allowed", KM: "Method not allowed"}},
	CodeUnauthenticated:    {Code: CodeUnauthenticated, StatusCode: http.StatusUnauthorized, Message: Messages{ZH: "请先登录", EN: "Authentication required", KM: "Authentication required"}},
	CodePermissionDenied:   {Code: CodePermissionDenied, StatusCode: http.StatusForbidden, Message: Messages{ZH: "无权执行此操作", EN: "Permission denied", KM: "Permission denied"}},
}

// TODO-KM-REVIEW: English placeholders in KM fields require review by a Khmer native speaker.
func NewAppError(code ErrorCode) *AppError {
	appErr, ok := appErrors[code]
	if !ok {
		appErr = appErrors[CodeInternalError]
	}
	return &appErr
}

type successResponse struct {
	OK        bool   `json:"ok"`
	Data      any    `json:"data"`
	RequestID string `json:"request_id"`
}

type errorBody struct {
	Code    ErrorCode `json:"code"`
	Message Messages  `json:"message"`
}

type errorResponse struct {
	OK        bool      `json:"ok"`
	Error     errorBody `json:"error"`
	RequestID string    `json:"request_id"`
}

func WriteOK(w http.ResponseWriter, r *http.Request, data any) {
	writeJSON(w, http.StatusOK, successResponse{OK: true, Data: data, RequestID: RequestIDFromContext(r.Context())})
}

func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	appErr := NewAppError(CodeInternalError)
	var provided *AppError
	if errors.As(err, &provided) && provided != nil {
		appErr = provided
	}
	writeJSON(w, appErr.StatusCode, errorResponse{
		OK: false,
		Error: errorBody{
			Code:    appErr.Code,
			Message: appErr.Message,
		},
		RequestID: RequestIDFromContext(r.Context()),
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
