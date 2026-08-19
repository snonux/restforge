// Package backend defines the pure Backend value type: the Go port of the
// value half of flutter/lib/services/settings_service.dart's Backend class,
// Normalise and Validate. It carries over that file's reasoning (itself
// carried over from pebble/src/pkjs/settings.js): a secret goes into an
// Authorization-style request header and nowhere else. In particular it must
// never be sent in a query string, because a key in a URL is written to the
// server's request log and, behind a reverse proxy, the proxy's log too.
//
// This package is deliberately storage-free: no file I/O, no TOML, no JSON
// decoding. The Dart file mixes the value type with shared_preferences and
// secure-storage I/O in one file; here that split is explicit so
// internal/http, internal/nav, internal/action and internal/live can depend
// on Backend without pulling in filesystem or config concerns. A later task
// (internal/config) does the I/O -- reading and writing the backend list and
// its secrets -- on top of this package's pure type.
package backend

import (
	"errors"
	"net/url"
	"strings"
)

// DefaultAuthHeader is the header Secret is sent in when nothing overrides
// it. Matches flutter's SettingsService.defaultAuthHeader and, before that,
// pebble/src/pkjs/settings.js.
const DefaultAuthHeader = "X-API-Key"

// MaxFieldLength caps every free-text field so a pathological config cannot
// make the settings screen or the backend picker unusable. Matches
// SettingsService.maxFieldLength -- generous, because the limit that
// actually matters is screen space.
const MaxFieldLength = 256

// MaxBackends caps how many backends may be configured, for the same
// reason as MaxFieldLength. Enforcing the cap is internal/config's job (it
// owns the stored list); this package only carries the number so both
// agree on it. Matches SettingsService.maxBackends.
const MaxBackends = 12

// Backend is one configured server: a plain value with no behaviour beyond
// reading itself. See the package comment for why Secret must never be sent
// anywhere but AuthHeader.
type Backend struct {
	// Name is the display name shown on the backend picker.
	Name string
	// BaseURL is the Siren API root, absolute, ending in '/'.
	BaseURL string
	// AuthHeader is the header Secret is sent in.
	AuthHeader string
	// Secret is the API key. Never logged or otherwise sent anywhere but
	// AuthHeader -- see the package comment.
	Secret string
	// StartRel is an optional link rel to follow immediately after the
	// root document.
	StartRel string
}

// Normalise coerces user- or storage-supplied fields into the shape
// Validate and every later package expect: every field trimmed of
// surrounding whitespace and capped at MaxFieldLength, AuthHeader defaulted
// to DefaultAuthHeader when empty, and BaseURL fixed up to end in '/'.
//
// It does not reject anything -- Validate does that, and only for input a
// user just typed. Input that has drifted (an older layout, a hand-edited
// value) degrades to something usable instead of taking the caller down.
func Normalise(b Backend) Backend {
	baseURL := clean(b.BaseURL)
	// Every href in a Siren document is resolved against this, and RFC 3986
	// resolution drops the last path segment of the base unless it ends in
	// '/'. Fixing it here rather than rejecting it saves the user from a
	// class of error whose only symptom is a 404 two screens later.
	if baseURL != "" && !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	authHeader := clean(b.AuthHeader)
	if authHeader == "" {
		authHeader = DefaultAuthHeader
	}

	return Backend{
		Name:       clean(b.Name),
		BaseURL:    baseURL,
		AuthHeader: authHeader,
		Secret:     clean(b.Secret),
		StartRel:   clean(b.StartRel),
	}
}

// Validate reports the first problem with b, or nil when it is usable.
// Meant to be called by the settings UI before it lets a user leave, so the
// message is written to be shown as-is. Mirrors
// SettingsService.validate's checks and order, translated from a nullable
// String to Go's (error | nil) idiom.
func Validate(b Backend) error {
	switch {
	case b.Name == "":
		return errors.New("name is required")
	case b.BaseURL == "":
		return errors.New("base URL is required")
	case !isAbsoluteHTTPURL(b.BaseURL):
		return errors.New("base URL must be absolute, e.g. https://host/path/")
	case b.Secret == "":
		return errors.New("secret is required")
	}
	return nil
}

// isAbsoluteHTTPURL reports whether raw is an absolute http(s) URL whose
// path ends in a slash, the same shape SettingsService.validate checks with
// the regexp ^https?://[^\s/]+/. Trailing-slash fix-up is Normalise's job
// (see its comment for why); this only checks the shape, it does not fix
// it up.
func isAbsoluteHTTPURL(raw string) bool {
	if !strings.HasSuffix(raw, "/") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Host != ""
}

// clean trims surrounding whitespace and caps the result at MaxFieldLength
// runes, mirroring SettingsService._trim. Truncation is rune-based, not
// byte-based, so it can never split a multi-byte UTF-8 character and leave
// invalid UTF-8 behind.
func clean(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > MaxFieldLength {
		r = r[:MaxFieldLength]
	}
	return string(r)
}
