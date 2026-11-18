package notifications

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var telegramUsernamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{4,31}$`)

// StartAppLink builds the Telegram Mini App deep link without ever handling a
// Bot token. The username is public configuration; the payload is an opaque,
// short-lived application value validated by the receiving endpoint.
func StartAppLink(username, payload string) (string, error) {
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	if !telegramUsernamePattern.MatchString(username) {
		return "", errors.New("notifications: invalid Telegram bot username")
	}
	payload = strings.TrimSpace(payload)
	if payload == "" || len(payload) > 64 {
		return "", errors.New("notifications: invalid Telegram startapp payload")
	}
	return fmt.Sprintf("https://t.me/%s?startapp=%s", username, url.QueryEscape(payload)), nil
}
