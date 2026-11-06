package kess

import (
	"net/url"
	"strings"
)

const sandboxHost = "devwebpayment.kesspay.io"

func validPaymentHost(host string) bool {
	u, err := url.Parse("https://" + host)
	return err == nil && u.Hostname() == host && u.Host == host && u.Path == "" && u.User == nil
}

func validPaymentURL(raw, host string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() == host && u.User == nil && strings.HasPrefix(u.EscapedPath(), "/pay/")
}
