package urlresolve_test

// Ported from pebble/tools/test-url.js, which checks pebble/src/pkjs/url.js
// against the RFC 3986 section 5.4 reference test vectors -- the
// specification ships its own test suite, so this file uses it rather than
// inventing cases. See also flutter/test/services/url_resolver_test.dart,
// the Dart port of the same vectors. Every vector is kept even though
// net/url.URL.ResolveReference gets each one right on the first try: they
// are the specification, not scaffolding that became unnecessary once
// net/url existed.

import (
	"testing"

	"github.com/snonux/restforge/cli/internal/urlresolve"
)

const rfcBase = "http://a/b/c/d;p?q"

// TestResolveNormal is RFC 3986 section 5.4.1.
func TestResolveNormal(t *testing.T) {
	vectors := []struct{ href, want string }{
		{"g:h", "g:h"},
		{"g", "http://a/b/c/g"},
		{"./g", "http://a/b/c/g"},
		{"g/", "http://a/b/c/g/"},
		{"/g", "http://a/g"},
		{"//g", "http://g"},
		{"?y", "http://a/b/c/d;p?y"},
		{"g?y", "http://a/b/c/g?y"},
		{"#s", "http://a/b/c/d;p?q#s"},
		{"g#s", "http://a/b/c/g#s"},
		{"g?y#s", "http://a/b/c/g?y#s"},
		{";x", "http://a/b/c/;x"},
		{"g;x", "http://a/b/c/g;x"},
		{"g;x?y#s", "http://a/b/c/g;x?y#s"},
		{"", "http://a/b/c/d;p?q"},
		{".", "http://a/b/c/"},
		{"./", "http://a/b/c/"},
		{"..", "http://a/b/"},
		{"../", "http://a/b/"},
		{"../g", "http://a/b/g"},
		{"../..", "http://a/"},
		{"../../", "http://a/"},
		{"../../g", "http://a/g"},
	}
	for _, v := range vectors {
		t.Run(v.href, func(t *testing.T) {
			if got := urlresolve.Resolve(v.href, rfcBase); got != v.want {
				t.Errorf("Resolve(%q, %q) = %q, want %q", v.href, rfcBase, got, v.want)
			}
		})
	}
}

// TestResolveAbnormal is RFC 3986 section 5.4.2 -- the vectors a naive
// split/join implementation gets wrong.
func TestResolveAbnormal(t *testing.T) {
	vectors := []struct{ href, want string }{
		{"../../../g", "http://a/g"},
		{"../../../../g", "http://a/g"},
		{"/./g", "http://a/g"},
		{"/../g", "http://a/g"},
		{"g.", "http://a/b/c/g."},
		{".g", "http://a/b/c/.g"},
		{"g..", "http://a/b/c/g.."},
		{"..g", "http://a/b/c/..g"},
		{"./../g", "http://a/b/g"},
		{"./g/.", "http://a/b/c/g/"},
		{"g/./h", "http://a/b/c/g/h"},
		{"g/../h", "http://a/b/c/h"},
		{"g;x=1/./y", "http://a/b/c/g;x=1/y"},
		{"g;x=1/../y", "http://a/b/c/y"},
		// Dot segments inside a query or fragment are data, not path syntax.
		{"g?y/./x", "http://a/b/c/g?y/./x"},
		{"g?y/../x", "http://a/b/c/g?y/../x"},
		{"g#s/./x", "http://a/b/c/g#s/./x"},
		{"g#s/../x", "http://a/b/c/g#s/../x"},
	}
	for _, v := range vectors {
		t.Run(v.href, func(t *testing.T) {
			if got := urlresolve.Resolve(v.href, rfcBase); got != v.want {
				t.Errorf("Resolve(%q, %q) = %q, want %q", v.href, rfcBase, got, v.want)
			}
		})
	}
}

// TestResolveRealShapes covers the shapes this app actually meets: a
// root-relative href from a server behind a reverse proxy, resolved against
// a configured base.
func TestResolveRealShapes(t *testing.T) {
	const base = "https://host.example.org/cgi-bin/app/"

	t.Run("root-relative href keeps the configured origin", func(t *testing.T) {
		got := urlresolve.Resolve("/cgi-bin/app/status", base)
		want := "https://host.example.org/cgi-bin/app/status"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("a relative href resolves under the base", func(t *testing.T) {
		got := urlresolve.Resolve("status", base)
		want := "https://host.example.org/cgi-bin/app/status"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("a base without a trailing slash loses its last segment", func(t *testing.T) {
		// The reason backend.Normalise appends a trailing slash: without one
		// the base's last segment is discarded and every href lands one
		// level too high.
		got := urlresolve.Resolve("status", "https://host.example.org/cgi-bin/app")
		want := "https://host.example.org/cgi-bin/status"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("an absolute href overrides the base entirely", func(t *testing.T) {
		got := urlresolve.Resolve("https://other.example.org/x", base)
		want := "https://other.example.org/x"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestOrigin(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"strips the path", "https://host.example.org/a/b?c=d", "https://host.example.org"},
		{"a non-absolute URL has no origin", "/a/b", ""},
		{"an unparsable url has no origin", "://bad", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := urlresolve.Origin(tt.in); got != tt.want {
				t.Errorf("Origin(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"keeps the query", "https://host.example.org/a/b?c=d", "/a/b?c=d"},
		{"an unparsable url falls back to itself", "://bad", "://bad"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := urlresolve.Path(tt.in); got != tt.want {
				t.Errorf("Path(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestResolveUnparsableBaseFallsBack pins the fallback behaviour that keeps
// Resolve total. url.js's regex-based parser accepts anything, so it never
// fails; net/url.Parse is stricter about what an RFC 3986 URI reference
// looks like and does return an error on some inputs (an unparsable port,
// here) -- see the package doc comment. A malformed base (a typo'd port in
// a hand-typed settings field) or a malformed href (a hostile or buggy
// server) degrades instead of panicking the caller.
func TestResolveUnparsableBaseFallsBack(t *testing.T) {
	got := urlresolve.Resolve("status", "http://host:abc/path")
	if got != "status" {
		t.Errorf("Resolve with an unparsable base = %q, want %q (the href unchanged)", got, "status")
	}
}

func TestEncodeForm(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]string
		want string
	}{
		{"uses + for spaces", map[string]string{"note": "a b"}, "note=a+b"},
		{"escapes separators", map[string]string{"a&b": "c=d"}, "a%26b=c%3Dd"},
		{"an empty field set is an empty body", map[string]string{}, ""},
		{"a nil field set is an empty body", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := urlresolve.EncodeForm(tt.in); got != tt.want {
				t.Errorf("EncodeForm(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestEncodeFormOrdering pins the deterministic field ordering documented on
// EncodeForm: url.Values.Encode sorts by key, so multi-field output does not
// depend on Go's randomised map iteration order.
func TestEncodeFormOrdering(t *testing.T) {
	fields := map[string]string{"z": "1", "a": "2", "m": "3"}
	want := "a=2&m=3&z=1"
	for i := 0; i < 10; i++ {
		if got := urlresolve.EncodeForm(fields); got != want {
			t.Fatalf("EncodeForm(%v) = %q, want %q (iteration %d)", fields, got, want, i)
		}
	}
}
