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
// "backends", one entry per configured backend, plus a top-level array of
// tables named "quick" holding internal/quick's saved shortcuts. See the
// package comment for why secrets live in this same file rather than split
// out, and QuickRecord's comment for why the quick array is a config-owned
// type rather than internal/quick's own QuickItem.
type fileFormat struct {
	Backends []tomlBackend `toml:"backends"`
	Quick    []tomlQuick   `toml:"quick"`
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

// tomlQuick is the on-disk shape of one entry in the "quick" array,
// snake_case TOML tags in place of QuickRecord's Go field names -- the same
// split tomlBackend keeps from backend.Backend, for the same reason.
type tomlQuick struct {
	Label       string `toml:"label"`
	BackendName string `toml:"backend_name"`
	BaseURL     string `toml:"base_url"`
	Kind        string `toml:"kind"`
	Holder      string `toml:"holder"`
	Name        string `toml:"name"`
	Href        string `toml:"href"`
}

// QuickRecord is the on-disk shape of one saved shortcut, returned by
// LoadQuick and accepted by SaveQuick. It exists as a config-owned type,
// rather than LoadQuick/SaveQuick trading in internal/quick's own QuickItem
// directly, because internal/quick already depends on this package (it
// stores through LoadQuick/SaveQuick) -- QuickItem living here too would
// make the dependency circular. internal/quick's own store.go converts
// between QuickRecord and QuickItem, the mirror image of how toBackend/
// fromBackend convert between tomlBackend and backend.Backend on this
// side. Kind is carried as the raw "action"/"document" string found on
// disk; internal/quick is the one that knows what to do with anything
// else (see its Normalise).
type QuickRecord struct {
	Label       string
	BackendName string
	BaseURL     string
	Kind        string
	Holder      string
	Name        string
	Href        string
}

func toQuickRecord(t tomlQuick) QuickRecord {
	return QuickRecord{
		Label:       t.Label,
		BackendName: t.BackendName,
		BaseURL:     t.BaseURL,
		Kind:        t.Kind,
		Holder:      t.Holder,
		Name:        t.Name,
		Href:        t.Href,
	}
}

func fromQuickRecord(r QuickRecord) tomlQuick {
	return tomlQuick{
		Label:       r.Label,
		BackendName: r.BackendName,
		BaseURL:     r.BaseURL,
		Kind:        r.Kind,
		Holder:      r.Holder,
		Name:        r.Name,
		Href:        r.Href,
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
	doc, ok := decodeFile(path)
	if !ok {
		return nil, nil
	}
	raw := make([]backend.Backend, len(doc.Backends))
	for i, t := range doc.Backends {
		raw[i] = toBackend(t)
	}
	return filterBackends(raw), nil
}

// LoadQuick returns the shortcuts stored in the "quick" array of the same
// config file LoadBackends reads. Unlike LoadBackends, it does not
// normalise, filter to usable or cap its result -- that domain logic
// (Normalise, "usable", MaxQuick) lives in internal/quick, one level above,
// mirroring how backend.Normalise/Validate stay in internal/backend rather
// than here; this package only calls those for backends because it already
// depends on internal/backend, and it cannot symmetrically depend on
// internal/quick (see QuickRecord's comment). LoadQuick shares
// LoadBackends' "never throws" contract: a missing file, unparseable TOML
// or a loosely-permissioned file all degrade to zero records rather than
// an error, reported through Warnf. The one error is Path's own.
func LoadQuick() ([]QuickRecord, error) {
	path, err := Path()
	if err != nil {
		return nil, fmt.Errorf("config: could not resolve config path: %w", err)
	}
	doc, ok := decodeFile(path)
	if !ok {
		return nil, nil
	}
	out := make([]QuickRecord, len(doc.Quick))
	for i, t := range doc.Quick {
		out[i] = toQuickRecord(t)
	}
	return out, nil
}

// decodeFile does the actual read shared by LoadBackends and LoadQuick,
// once the path is known. Split out so each Load*'s only fallible step
// (path resolution) stays visibly separate from the steps that degrade
// instead of failing, and so both top-level arrays are read from one
// decode of the file rather than two.
func decodeFile(path string) (fileFormat, bool) {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fileFormat{}, false // no config yet: nothing stored, not an error
	case err != nil:
		Warnf("could not stat %s, treating it as empty: %v", path, err)
		return fileFormat{}, false
	}
	warnIfInsecure(path, info)

	var doc fileFormat
	if _, err := toml.DecodeFile(path, &doc); err != nil {
		Warnf("%s is not valid TOML, ignoring it: %v", path, err)
		return fileFormat{}, false
	}
	return doc, true
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
// LoadBackends reads them, then writes the result as the config file's
// "backends" array, replacing whatever was there before -- but preserving
// the "quick" array untouched, so saving backends never drops a shortcut
// saved through SaveQuick. Unlike LoadBackends, a write failure (a full
// disk, a missing parent that could not be created) is returned rather
// than swallowed: there is no "degrade gracefully" option for a save the
// caller asked for and believes succeeded.
func SaveBackends(backends []backend.Backend) error {
	path, err := Path()
	if err != nil {
		return fmt.Errorf("config: could not resolve config path: %w", err)
	}

	clean := filterBackends(backends)
	doc := existingDoc(path) // preserve the "quick" array a SaveQuick call may have written
	doc.Backends = make([]tomlBackend, len(clean))
	for i, b := range clean {
		doc.Backends[i] = fromBackend(b)
	}
	if err := writeDoc(path, doc); err != nil {
		return fmt.Errorf("config: could not save %s: %w", path, err)
	}
	return nil
}

// SaveQuick writes records as the config file's "quick" array, preserving
// whatever backends are already stored there -- the mirror image of
// SaveBackends preserving "quick". Unlike SaveBackends, it does not
// normalise, filter or cap records: internal/quick's own Save does that
// before calling here, for the same reason LoadQuick does not (see its
// comment). A write failure (a full disk, a missing parent that could not
// be created) is returned rather than swallowed, matching SaveBackends'
// contract.
func SaveQuick(records []QuickRecord) error {
	path, err := Path()
	if err != nil {
		return fmt.Errorf("config: could not resolve config path: %w", err)
	}

	doc := existingDoc(path) // preserve the "backends" array a SaveBackends call may have written
	doc.Quick = make([]tomlQuick, len(records))
	for i, r := range records {
		doc.Quick[i] = fromQuickRecord(r)
	}
	if err := writeDoc(path, doc); err != nil {
		return fmt.Errorf("config: could not save %s: %w", path, err)
	}
	return nil
}

// existingDoc reads path's current on-disk contents for merging into a
// Save call, so saving one array (backends or quick) does not clobber the
// other. Best-effort only, deliberately ignoring the error: a missing or
// unparseable file yields a zero-value fileFormat -- "nothing to
// preserve" -- rather than failing the save. Save must still succeed
// (and, unavoidably, drop the corrupt half) even when the existing file
// cannot be parsed; the alternative, refusing to save a valid backend
// list because the quick array next to it is hand-edited garbage, would
// be worse.
func existingDoc(path string) fileFormat {
	var doc fileFormat
	_, _ = toml.DecodeFile(path, &doc)
	return doc
}

// writeDoc encodes doc as TOML and writes it to path, creating the parent
// directory if needed. Shared by SaveBackends and SaveQuick so both arrays
// are written by one atomic rename, never two separate writes that could
// interleave with a concurrent reader.
func writeDoc(path string, doc fileFormat) error {
	// 0700: the directory holds a file full of secrets, so it gets the same
	// owner-only treatment as the file itself.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
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
