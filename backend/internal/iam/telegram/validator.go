package telegram

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const MaxInitDataBytes = 4 << 10

var ErrInvalidInitData = errors.New("telegram: invalid initData")

type User struct {
	ID              int64  `json:"id"`
	FirstName       string `json:"first_name"`
	Username        string `json:"username"`
	LanguageCode    string `json:"language_code"`
	AllowsWriteToPM bool   `json:"allows_write_to_pm"`
}

type ValidatedData struct {
	User       User
	AuthDate   time.Time
	QueryID    string
	StartParam string
}

type Failure string

const (
	FailureInvalidSignature Failure = "invalid_signature"
	FailureExpired          Failure = "expired"
	FailureMalformed        Failure = "malformed"
)

type ValidationError struct{ Failure Failure }

func (e *ValidationError) Error() string { return ErrInvalidInitData.Error() }
func (e *ValidationError) Unwrap() error { return ErrInvalidInitData }

type Validator struct {
	MaxAge     time.Duration
	FutureSkew time.Duration
	Now        func() time.Time
}

// NewValidator defaults to 24 hours because initData is not refreshed while a
// Mini App remains open. Refresh tokens carry later silent renewal. The wider
// window than the PoC's one hour increases replay exposure and is configurable.
func NewValidator(maxAge time.Duration) Validator {
	return Validator{MaxAge: maxAge, FutureSkew: time.Minute, Now: time.Now}
}

func (v Validator) Validate(raw string, token []byte) (ValidatedData, error) {
	malformed := func() (ValidatedData, error) {
		return ValidatedData{}, &ValidationError{Failure: FailureMalformed}
	}
	if len(raw) == 0 || len(raw) > MaxInitDataBytes || len(token) == 0 || v.MaxAge <= 0 || v.FutureSkew < 0 {
		return malformed()
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return malformed()
	}
	for _, value := range values {
		if len(value) != 1 {
			return malformed()
		}
	}
	want, err := hex.DecodeString(values.Get("hash"))
	if err != nil || len(want) != sha256.Size {
		return malformed()
	}
	keys := make([]string, 0, len(values)-1)
	for key := range values {
		if key != "hash" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"="+values.Get(key))
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secret.Write(token)
	digest := hmac.New(sha256.New, secret.Sum(nil))
	_, _ = digest.Write([]byte(strings.Join(lines, "\n")))
	if !hmac.Equal(digest.Sum(nil), want) {
		return ValidatedData{}, &ValidationError{Failure: FailureInvalidSignature}
	}
	authUnix, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil {
		return malformed()
	}
	var user User
	if err := json.Unmarshal([]byte(values.Get("user")), &user); err != nil || user.ID <= 0 {
		return malformed()
	}
	authDate := time.Unix(authUnix, 0)
	now := time.Now()
	if v.Now != nil {
		now = v.Now()
	}
	if now.Sub(authDate) > v.MaxAge || authDate.Sub(now) > v.FutureSkew {
		return ValidatedData{}, &ValidationError{Failure: FailureExpired}
	}
	return ValidatedData{User: user, AuthDate: authDate, QueryID: values.Get("query_id"), StartParam: values.Get("start_param")}, nil
}

func FailureOf(err error) Failure {
	var validation *ValidationError
	if errors.As(err, &validation) {
		return validation.Failure
	}
	return FailureMalformed
}
