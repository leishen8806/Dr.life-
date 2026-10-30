package kess

import (
	"net/url"
	"strings"
)

const sandboxHost = "devwebpayment.kesspay.io"

func validSandboxPayURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() == sandboxHost && u.User == nil && strings.HasPrefix(u.EscapedPath(), "/pay/")
}
