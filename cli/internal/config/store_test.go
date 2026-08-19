// Ported from flutter/test/services/settings_service_test.dart's storage
// groups ("no storage", "normalisation", "the secret/metadata split",
// "incomplete entries", "corrupt storage degrades to no backends", "the
// backend cap"), adapted from shared_preferences+secure-storage/JSON to one
// TOML file. This package has no split secret store, so there is no
// equivalent to the Dart file's ReadFailingSecretStore/FailingSecretStore
// tests -- those exercised a platform secure-storage failure this package
// does not have. In its place are the new tests this task adds: 0600
// enforcement on save, and the warning (not failure) when an existing file
// is loosely permissioned.
//
// Every test isolates itself with t.TempDir() and t.Setenv(ConfigEnvVar,
// ...) so none of it depends on real $HOME or $XDG_CONFIG_HOME state.
package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snonux/restforge/cli/internal/backend"
	"github.com/snonux/restforge/cli/internal/config"
)

// withConfig points ConfigEnvVar at a fresh path inside t.TempDir() and
// returns it, so each test starts from "no config file exists yet" without
// touching a real home directory.
func withConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv(config.ConfigEnvVar, path)
	return path
}

// captureWarnings replaces config.Warnf for the duration of the test and
// returns a func reporting every message logged so far. See warn.go's
// doc comment for why this is the package's chosen test hook.
func captureWarnings(t *testing.T) func() []string {
	t.Helper()
	var got []string
	prev := config.Warnf
	config.Warnf = func(format string, args ...any) {
		got = append(got, fmt.Sprintf(format, args...))
	}
	t.Cleanup(func() { config.Warnf = prev })
	return func() []string { return got }
}

func TestNoStorageMeansNoBackends(t *testing.T) {
	withConfig(t)

	got, err := config.LoadBackends()

	if err != nil {
		t.Fatalf("LoadBackends() error = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("LoadBackends() = %v, want empty", got)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	withConfig(t)

	saved := []backend.Backend{
		{Name: "homelab", BaseURL: "https://h/", Secret: "SECRET-VALUE"},
	}
	if err := config.SaveBackends(saved); err != nil {
		t.Fatalf("SaveBackends() error = %v", err)
	}

	got, err := config.LoadBackends()
	if err != nil {
		t.Fatalf("LoadBackends() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("LoadBackends() = %v, want 1 backend", got)
	}
	if got[0].Secret != "SECRET-VALUE" {
		t.Errorf("Secret = %q, want %q", got[0].Secret, "SECRET-VALUE")
	}
	if got[0].Name != "homelab" {
		t.Errorf("Name = %q, want %q", got[0].Name, "homelab")
	}
}

func TestNormalisationOnSave(t *testing.T) {
	withConfig(t)

	// Without the trailing slash, RFC 3986 resolution drops the last path
	// segment and every href in the document resolves one level too high.
	err := config.SaveBackends([]backend.Backend{
		{Name: "homelab", BaseURL: "https://host.example.org/cgi-bin/app", Secret: "k"},
	})
	if err != nil {
		t.Fatalf("SaveBackends() error = %v", err)
	}

	got, err := config.LoadBackends()
	if err != nil {
		t.Fatalf("LoadBackends() error = %v", err)
	}
	if got[0].BaseURL != "https://host.example.org/cgi-bin/app/" {
		t.Errorf("BaseURL = %q, want trailing slash added", got[0].BaseURL)
	}
	if got[0].AuthHeader != backend.DefaultAuthHeader {
		t.Errorf("AuthHeader = %q, want default %q", got[0].AuthHeader, backend.DefaultAuthHeader)
	}
}

func TestUnnamedBackendIsDropped(t *testing.T) {
	withConfig(t)

	err := config.SaveBackends([]backend.Backend{
		{Name: "", BaseURL: "https://a/", Secret: "x"},
		{Name: "good", BaseURL: "https://b/", Secret: "x"},
	})
	if err != nil {
		t.Fatalf("SaveBackends() error = %v", err)
	}

	got, err := config.LoadBackends()
	if err != nil {
		t.Fatalf("LoadBackends() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "good" {
		t.Fatalf("LoadBackends() = %v, want only %q", got, "good")
	}
}

func TestBackendCap(t *testing.T) {
	withConfig(t)

	many := make([]backend.Backend, 0, backend.MaxBackends+8)
	for i := 0; i < backend.MaxBackends+8; i++ {
		many = append(many, backend.Backend{
			Name:    fmt.Sprintf("b%d", i),
			BaseURL: fmt.Sprintf("https://h/%d/", i),
			Secret:  "s",
		})
	}

	if err := config.SaveBackends(many); err != nil {
		t.Fatalf("SaveBackends() error = %v", err)
	}

	got, err := config.LoadBackends()
	if err != nil {
		t.Fatalf("LoadBackends() error = %v", err)
	}
	if len(got) != backend.MaxBackends {
		t.Fatalf("len(LoadBackends()) = %d, want %d", len(got), backend.MaxBackends)
	}
}

func TestCorruptStorageDegradesToNoBackends(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "not TOML at all", content: "not toml at all {{{"},
		{name: "backends is not an array of tables", content: `backends = "nope"` + "\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := withConfig(t)
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			got, err := config.LoadBackends()
			if err != nil {
				t.Fatalf("LoadBackends() error = %v, want nil (degrade, don't fail)", err)
			}
			if len(got) != 0 {
				t.Fatalf("LoadBackends() = %v, want empty", got)
			}
		})
	}
}

func TestJunkArrayMembersAreDropped(t *testing.T) {
	path := withConfig(t)
	// A backends table missing every field (no name, no base_url, no
	// secret) is the TOML analogue of the Dart test's junk JSON array
	// members ([null,3,"x"]): it decodes fine but Validate rejects it.
	content := "[[backends]]\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := config.LoadBackends()
	if err != nil {
		t.Fatalf("LoadBackends() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("LoadBackends() = %v, want empty", got)
	}
}

func TestNewFileIsCreatedAt0600(t *testing.T) {
	path := withConfig(t)

	if err := config.SaveBackends([]backend.Backend{
		{Name: "homelab", BaseURL: "https://h/", Secret: "k"},
	}); err != nil {
		t.Fatalf("SaveBackends() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %04o, want %04o", perm, 0o600)
	}
}

func TestLoadWarnsOnInsecurePermissions(t *testing.T) {
	path := withConfig(t)
	content := "[[backends]]\nname = \"homelab\"\nbase_url = \"https://h/\"\nsecret = \"k\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	warnings := captureWarnings(t)

	got, err := config.LoadBackends()

	if err != nil {
		t.Fatalf("LoadBackends() error = %v, want nil (warn, don't fail)", err)
	}
	if len(got) != 1 {
		t.Fatalf("LoadBackends() = %v, want the backend to still load", got)
	}
	msgs := warnings()
	if len(msgs) == 0 {
		t.Fatal("Warnf was not called for a 0644 config file")
	}
	if !strings.Contains(msgs[0], "0600") {
		t.Errorf("warning = %q, want it to mention the fix (chmod 0600)", msgs[0])
	}
}

func TestLoadDoesNotWarnOnSecurePermissions(t *testing.T) {
	path := withConfig(t)
	content := "[[backends]]\nname = \"homelab\"\nbase_url = \"https://h/\"\nsecret = \"k\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	warnings := captureWarnings(t)

	if _, err := config.LoadBackends(); err != nil {
		t.Fatalf("LoadBackends() error = %v", err)
	}

	if msgs := warnings(); len(msgs) != 0 {
		t.Errorf("Warnf called with a 0600 file: %v", msgs)
	}
}

func TestLoadBackendsFailsOnlyWhenPathCannotBeResolved(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	if _, err := config.LoadBackends(); err == nil {
		t.Fatal("LoadBackends() error = nil, want an error when the config path cannot be resolved")
	}
}

func TestPathHonoursEnvVar(t *testing.T) {
	want := filepath.Join(t.TempDir(), "somewhere", "backends.toml")
	t.Setenv(config.ConfigEnvVar, want)

	got, err := config.Path()

	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestPathFallsBackToUserConfigDir(t *testing.T) {
	t.Setenv(config.ConfigEnvVar, "")
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	got, err := config.Path()

	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	want := filepath.Join(xdg, "restforge", "config.toml")
	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}
