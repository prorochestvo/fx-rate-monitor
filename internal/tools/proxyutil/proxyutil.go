// Package proxyutil resolves and redacts the operator-configured outbound proxy
// URL. It is shared by the cmd/collector and cmd/doctor binaries, which both
// read PROXY_URL at startup.
package proxyutil

import (
	"log"
	"net/url"
	"os"

	"github.com/prorochestvo/dsninjector"
)

// ResolveURL reads envName from the process environment, parses it via
// dsninjector.Unmarshal, and returns the URL string. Returns "" when the
// variable is unset or empty (no proxy). Calls log.Fatalf when the value is
// present but cannot be parsed by dsninjector, because a malformed proxy URL
// is an operator configuration error that must be fixed before the service
// starts.
//
// On every call a single startup line is emitted via log.Printf — the same
// sink every other startup line uses, so the message lands in both stdout
// and the file logger and is not silently filtered by the global verbosity
// level:
//   - "proxy: not configured" when the variable is absent.
//   - "proxy: PROXY_URL=<redacted>" when a valid URL is found; credentials in
//     the userinfo component are stripped from the logged value.
func ResolveURL(envName string) string {
	_, ok := os.LookupEnv(envName)
	if !ok {
		log.Printf("proxy: not configured")
		return ""
	}
	dsn, err := dsninjector.Unmarshal(envName)
	if err != nil {
		log.Fatalf("settings: %s: %s", envName, err.Error())
	}
	raw := dsn.Driver() + "://" + dsn.Addr()
	log.Printf("proxy: PROXY_URL=%s", RedactURL(raw))
	return raw
}

// RedactURL strips the password from a proxy URL before it is written to any
// log sink. If the URL contains no userinfo or cannot be parsed it is returned
// unchanged.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = url.User(u.User.Username())
	return u.String()
}
