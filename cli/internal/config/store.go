package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/snonux/restforge/cli/internal/backend"
)

// fileFormat is the on-disk TOML shape: a top-level array of tables named
// "backends", one entry per configured backend. See the package comment for
// why secrets live in this same file rather than split out.
type fileFormat struct {
	Backends []tomlBackend `toml:"backends"`
}

// tomlBackend mirrors backend.Backend field-for-field, but with snake_case
// TOML tags (TOML convention) in place of Backend's Go field names. Kept
// separate from Backend itself so internal/backend stays free of storage
// concerns -- see that package's comment.
type tomlBackend struct {
	Name       string `toml:"name"`
	BaseURL    string `toml:"base_url"`
	AuthHeader string `toml:"auth_header"`
	Secret     string `toml:"secret"`
	StartRel   string `toml:"start_rel"`
}

func toBackend(t tomlBackend) backend.Backend {
	return backend.Backend{
		Name:       t.Name,
		BaseURL:    t.BaseURL,
		AuthHeader: t.AuthHeader,
		Secret:     t.Secret,
		StartRel:   t.StartRel,
	}
}

func fromBackend(b backend.Backend) tomlBackend {
	return tomlBackend{
		Name:       b.Name,
		BaseURL:    b.BaseURL,
		AuthHeader: b.AuthHeader,
		Secret:     b.Secret,
		StartRel:   b.StartRel,
	}
}

// LoadBackends returns the configured backends, normalised and validated,
// capped at backend.MaxBackends. See the package comment for the "never
// throws" contract this keeps: a missing file, unparseable TOML, an
// insecure file mode, or an entry that fails backend.Validate all degrade
// to fewer (or zero) backends, reported through Warnf, rather than an
// error. The returned error is non-nil only when Path itself fails.
func LoadBackends() ([]backend.Backend, error) {
	path, err := Path()
	if err != nil {
		return nil, fmt.Errorf("config: could not resolve config path: %w", err)
	}
	return loadFrom(path), nil
}

// loadFrom does the actual read for LoadBackends, once the path is known.
// Split out so LoadBackends' only fallible step (path resolution) is
// visibly separate from the steps that degrade instead of failing.
func loadFrom(path string) []backend.Backend {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil // no config yet: zero backends, not an error
	case err != nil:
		Warnf("could not stat %s, treating it as no backends: %v", path, err)
		return nil
	}
	warnIfInsecure(path, info)

	var doc fileFormat
	if _, err := toml.DecodeFile(path, &doc); err != nil {
		Warnf("%s is not valid TOML, ignoring it: %v", path, err)
		return nil
	}

	raw := make([]backend.Backend, len(doc.Backends))
	for i, t := range doc.Backends {
		raw[i] = toBackend(t)
	}
	return filterBackends(raw)
}

// filterBackends normalises every entry and keeps only the ones that pass
// backend.Validate, capped at backend.MaxBackends -- the shared filtering
// step LoadBackends and SaveBackends both apply, mirroring
// settings_service.dart's loadBackends/saveBackends (and, before that,
// pebble/src/pkjs/settings.js's load/save), which likewise never let an
// unusable or excess entry reach the caller.
func filterBackends(in []backend.Backend) []backend.Backend {
	out := make([]backend.Backend, 0, len(in))
	for _, b := range in {
		if len(out) >= backend.MaxBackends {
			break
		}
		n := backend.Normalise(b)
		if backend.Validate(n) != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

// SaveBackends normalises, validates and caps backends exactly like
// LoadBackends reads them, then writes the result as the config file's sole
// content, replacing whatever was there before. Unlike LoadBackends, a
// write failure (a full disk, a missing parent that could not be created)
// is returned rather than swallowed: there is no "degrade gracefully"
// option for a save the caller asked for and believes succeeded.
func SaveBackends(backends []backend.Backend) error {
	path, err := Path()
	if err != nil {
		return fmt.Errorf("config: could not resolve config path: %w", err)
	}

	clean := filterBackends(backends)
	if err := writeFile(path, clean); err != nil {
		return fmt.Errorf("config: could not save %s: %w", path, err)
	}
	return nil
}

// writeFile encodes backends as TOML and writes them to path, creating the
// parent directory if needed.
func writeFile(path string, backends []backend.Backend) error {
	// 0700: the directory holds a file full of secrets, so it gets the same
	// owner-only treatment as the file itself.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	doc := fileFormat{Backends: make([]tomlBackend, len(backends))}
	for i, b := range backends {
		doc.Backends[i] = fromBackend(b)
	}
	data, err := toml.Marshal(doc)
	if err != nil {
		return err
	}
	return writeAtomic(path, data)
}

// writeAtomic writes data to a temp file next to path (same directory, so
// the rename below is on one filesystem and therefore atomic) at mode 0600,
// then renames it over path. A crash or power loss mid-write leaves either
// the old file or the new one, never a half-written mix of both, and the
// temp file is never briefly world-readable: os.CreateTemp already creates
// it at 0600, and the explicit Chmod below makes that guarantee visible
// here rather than left implicit in a stdlib default -- see the package
// comment's "enforce the mode, don't just rely on it" invariant.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
