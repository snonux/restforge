// Package urlresolve implements RFC 3986 reference resolution.
//
// This is the Go side of flutter/lib/services/url_resolver.dart, which is
// itself the Dart side of pebble/src/pkjs/url.js. That file exists only
// because PebbleKit JS has no URL constructor, so RFC 3986 section 5.2 had
// to be written out by hand; the Dart port delegates to Uri.resolve instead
// of reimplementing removeDotSegments/merge, and every RFC 3986 section 5.4
// test vector passes against it unchanged. Go's net/url.URL.ResolveReference
// already implements the same sections (5.2 and 5.4), so this package
// delegates to it for the same reason -- see the table-driven tests in
// urlresolve_test.go, ported from pebble/tools/test-url.js and
// flutter/test/services/url_resolver_test.dart, which confirm no correction
// is needed here either.
//
// Note what this is *not*: it is not path construction. The path always
// comes from the server, in an href it put in a document; only the scheme
// and authority are ours, because a server behind a reverse proxy cannot
// know the public name the client reached it by. That distinction is the
// whole of the hypermedia rule -- follow what you were given, never
// assemble a path from knowledge you think you have about the server. See
// pebble/docs/DESIGN.md, "The rule everything else follows from".
//
// Every function here is total: given a string, it returns a string, never
// panics. url.js's hand-written parser is regex-based and accepts any
// input, so it never fails either; net/url.Parse is stricter about what an
// RFC 3986 URI reference looks like (an unparsable port, for example) and
// returns an error on the inputs it rejects. Rather than pushing that error
// onto every caller -- which would need a (string, error) return for what
// is otherwise a pure function with no caller-visible failure mode -- each
// function here falls back to something safe to keep going with. A request
// built from a fallback still goes out and fails naturally at the HTTP
// layer, the same place a request to a merely-wrong-but-parsable URL would
// fail anyway.
package urlresolve

import "net/url"

// Resolve resolves href against base and returns the resulting absolute URL
// as a string, per RFC 3986 section 5.2. Falls back to href unchanged if
// either string is not a URI reference net/url.Parse can make sense of.
func Resolve(href, base string) string {
	baseURL, err := url.Parse(base)
	if err != nil {
		return href
	}
	hrefURL, err := url.Parse(href)
	if err != nil {
		return href
	}
	return baseURL.ResolveReference(hrefURL).String()
}

// Origin is the scheme and authority of rawURL, e.g.
// "https://host.example.org" -- for logging a request without repeating the
// whole thing on every line. Empty if rawURL has no scheme, no authority (a
// path-only, relative reference), or is not parsable at all.
func Origin(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	// Clear everything but scheme and authority (host, plus userinfo if
	// present) and let url.URL.String do the RFC 3986 serialisation, rather
	// than hand-concatenating "scheme://host" and risking a mismatch with
	// net/url's own formatting (e.g. dropped userinfo).
	origin := *parsed
	origin.Opaque = ""
	origin.Path = ""
	origin.RawPath = ""
	origin.RawQuery = ""
	origin.ForceQuery = false
	origin.Fragment = ""
	origin.RawFragment = ""
	return origin.String()
}

// Path is everything in rawURL after the origin: the path plus, if present,
// the query. This is what goes in a log line, and it is safe to log because
// it never carries a secret -- a secret belongs in a request header, never
// a query string (see internal/backend's package comment). Falls back to
// rawURL unchanged if it is not parsable.
func Path(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	p := parsed.EscapedPath()
	// ForceQuery covers a "?" with nothing after it (an empty query, which
	// is not the same as no query at all -- RFC 3986 section 3.4).
	if parsed.ForceQuery || parsed.RawQuery != "" {
		return p + "?" + parsed.RawQuery
	}
	return p
}

// EncodeForm builds an application/x-www-form-urlencoded body from fields.
//
// Field order: url.Values.Encode sorts by key, unlike Go's randomised map
// iteration order. That determinism is not semantically required by
// application/x-www-form-urlencoded (field order rarely matters to a
// server), but it makes the output reproducible across calls, which is
// worth having for free and is what TestEncodeFormOrdering pins down.
//
// Percent encoding is url.QueryEscape's, by way of url.Values.Encode, which
// already applies the space-as-plus convention form encoding requires (and
// plain URI encoding does not) -- matching encodeForm in url.js without
// that file's hand-rolled ".replace(/%20/g, '+')", which Go's stdlib does
// not need either.
func EncodeForm(fields map[string]string) string {
	values := make(url.Values, len(fields))
	for name, value := range fields {
		values.Set(name, value)
	}
	return values.Encode()
}
