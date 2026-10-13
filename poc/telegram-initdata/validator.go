package telegraminitdata

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

var ErrInvalidInitData = errors.New("invalid Telegram initData")

type User struct {
	ID int64 `json:"id"`
}

type ValidatedData struct {
	User     User
	AuthDate time.Time
}

type Validator struct {
	MaxAge     time.Duration
	FutureSkew time.Duration
	Now        func() time.Time
}

func NewValidator() Validator {
	return Validator{MaxAge: time.Hour, FutureSkew: time.Minute, Now: time.Now}
}

func (v Validator) Validate(raw, token string) (ValidatedData, error) {
	if len(raw) == 0 || len(raw) > MaxInitDataBytes || token == "" {
		return ValidatedData{}, ErrInvalidInitData
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return ValidatedData{}, ErrInvalidInitData
	}
	for _, value := range values {
		if len(value) != 1 {
			return ValidatedData{}, ErrInvalidInitData
		}
	}

	want, err := hex.DecodeString(values.Get("hash"))
	if err != nil || len(want) != sha256.Size {
		return ValidatedData{}, ErrInvalidInitData
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

	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secretMAC.Write([]byte(token))
	dataMAC := hmac.New(sha256.New, secretMAC.Sum(nil))
	_, _ = dataMAC.Write([]byte(strings.Join(lines, "\n")))
	if !hmac.Equal(dataMAC.Sum(nil), want) {
		return ValidatedData{}, ErrInvalidInitData
	}

	authUnix, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil {
		return ValidatedData{}, ErrInvalidInitData
	}
	var user User
	if err := json.Unmarshal([]byte(values.Get("user")), &user); err != nil || user.ID <= 0 {
		return ValidatedData{}, ErrInvalidInitData
	}
	authDate := time.Unix(authUnix, 0)
	now := time.Now()
	if v.Now != nil {
		now = v.Now()
	}
	if v.MaxAge <= 0 || v.FutureSkew < 0 || now.Sub(authDate) > v.MaxAge || authDate.Sub(now) > v.FutureSkew {
		return ValidatedData{}, ErrInvalidInitData
	}
	return ValidatedData{User: user, AuthDate: authDate}, nil
}
