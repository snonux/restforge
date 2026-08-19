package backend_test

import (
	"strings"
	"testing"

	"github.com/snonux/restforge/cli/internal/backend"
)

// TestNormalise mirrors the "normalisation" group in
// flutter/test/services/settings_service_test.dart's settings_service_test.dart.
func TestNormalise(t *testing.T) {
	tests := []struct {
		name string
		in   backend.Backend
		want backend.Backend
	}{
		{
			// Without the trailing slash, RFC 3986 resolution drops the
			// last path segment and every href in the document resolves
			// one level too high.
			name: "a trailing slash is added",
			in: backend.Backend{
				Name:    "homelab",
				BaseURL: "https://host.example.org/cgi-bin/app",
				Secret:  "k",
			},
			want: backend.Backend{
				Name:       "homelab",
				BaseURL:    "https://host.example.org/cgi-bin/app/",
				AuthHeader: backend.DefaultAuthHeader,
				Secret:     "k",
			},
		},
		{
			name: "an already-slash-terminated base URL is unchanged",
			in: backend.Backend{
				Name:    "homelab",
				BaseURL: "https://host.example.org/app/",
				Secret:  "k",
			},
			want: backend.Backend{
				Name:       "homelab",
				BaseURL:    "https://host.example.org/app/",
				AuthHeader: backend.DefaultAuthHeader,
				Secret:     "k",
			},
		},
		{
			name: "an empty base URL stays empty rather than becoming a bare slash",
			in: backend.Backend{
				Name:   "homelab",
				Secret: "k",
			},
			want: backend.Backend{
				Name:       "homelab",
				AuthHeader: backend.DefaultAuthHeader,
				Secret:     "k",
			},
		},
		{
			name: "the auth header defaults",
			in: backend.Backend{
				Name:    "homelab",
				BaseURL: "https://host.example.org/app",
				Secret:  "k",
			},
			want: backend.Backend{
				Name:       "homelab",
				BaseURL:    "https://host.example.org/app/",
				AuthHeader: backend.DefaultAuthHeader,
				Secret:     "k",
			},
		},
		{
			name: "an explicit auth header is preserved",
			in: backend.Backend{
				Name:       "homelab",
				BaseURL:    "https://h/",
				AuthHeader: "Authorization",
				Secret:     "k",
			},
			want: backend.Backend{
				Name:       "homelab",
				BaseURL:    "https://h/",
				AuthHeader: "Authorization",
				Secret:     "k",
			},
		},
		{
			name: "surrounding whitespace is trimmed from every field",
			in: backend.Backend{
				Name:     "  homelab  ",
				BaseURL:  "  https://h/  ",
				Secret:   "  k  ",
				StartRel: "  next  ",
			},
			want: backend.Backend{
				Name:       "homelab",
				BaseURL:    "https://h/", // already trimmed to end in '/', so no fix-up needed
				AuthHeader: backend.DefaultAuthHeader,
				Secret:     "k",
				StartRel:   "next",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := backend.Normalise(tt.in); got != tt.want {
				t.Errorf("Normalise(%+v) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

// TestNormaliseTruncatesLongFields checks the MaxFieldLength cap ported
// from SettingsService._trim -- a pathological input must not make the
// settings screen or the backend picker unusable, so it is silently
// truncated rather than rejected here (Validate is where rejection
// happens, and only for the checks it explicitly names).
func TestNormaliseTruncatesLongFields(t *testing.T) {
	long := strings.Repeat("a", backend.MaxFieldLength+50)

	got := backend.Normalise(backend.Backend{Name: long, Secret: "k"})

	if len(got.Name) != backend.MaxFieldLength {
		t.Fatalf("Name length = %d, want %d", len(got.Name), backend.MaxFieldLength)
	}
	if got.Name != strings.Repeat("a", backend.MaxFieldLength) {
		t.Errorf("Name = %q, want %d copies of %q", got.Name, backend.MaxFieldLength, "a")
	}
}

// TestNormaliseTruncatesMultiByteRunes checks that truncation counts runes,
// not bytes, so it can never split a multi-byte UTF-8 character and leave
// invalid UTF-8 behind.
func TestNormaliseTruncatesMultiByteRunes(t *testing.T) {
	long := strings.Repeat("é", backend.MaxFieldLength+10) // 2 bytes per rune

	got := backend.Normalise(backend.Backend{Name: long, Secret: "k"})

	if got.Name != strings.Repeat("é", backend.MaxFieldLength) {
		t.Errorf("Name = %q, want %d copies of %q", got.Name, backend.MaxFieldLength, "é")
	}
}

// TestValidate mirrors the "validation" group in
// flutter/test/services/settings_service_test.dart, running each input
// through Normalise first exactly as the settings screen does.
func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		in      backend.Backend
		wantErr string // substring expected in Validate's error, or "" for nil
	}{
		{
			name:    "a missing secret is reported",
			in:      backend.Backend{Name: "a", BaseURL: "https://h/"},
			wantErr: "secret is required",
		},
		{
			name:    "a missing name is reported",
			in:      backend.Backend{BaseURL: "https://h/", Secret: "s"},
			wantErr: "name is required",
		},
		{
			name:    "a missing base URL is reported",
			in:      backend.Backend{Name: "a", Secret: "s"},
			wantErr: "base URL is required",
		},
		{
			name:    "a relative base URL is reported",
			in:      backend.Backend{Name: "a", BaseURL: "/x/", Secret: "s"},
			wantErr: "absolute",
		},
		{
			name:    "a complete backend validates",
			in:      backend.Backend{Name: "a", BaseURL: "https://h/x", Secret: "s"},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := backend.Normalise(tt.in)
			err := backend.Validate(b)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate(%+v) = %v, want nil", b, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate(%+v) = nil, want error containing %q", b, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate(%+v) = %q, want it to contain %q", b, err.Error(), tt.wantErr)
			}
		})
	}
}

// TestConstants pins the values ported from SettingsService so a future
// accidental change is caught here rather than silently changing behaviour
// shared with the Flutter app.
func TestConstants(t *testing.T) {
	if backend.DefaultAuthHeader != "X-API-Key" {
		t.Errorf("DefaultAuthHeader = %q, want %q", backend.DefaultAuthHeader, "X-API-Key")
	}
	if backend.MaxFieldLength != 256 {
		t.Errorf("MaxFieldLength = %d, want %d", backend.MaxFieldLength, 256)
	}
	if backend.MaxBackends != 12 {
		t.Errorf("MaxBackends = %d, want %d", backend.MaxBackends, 12)
	}
}
