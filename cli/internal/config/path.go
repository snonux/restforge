package config

import (
	"os"
	"path/filepath"
)

// ConfigEnvVar overrides the config file location outright: set it to the
// full path of a TOML file and Path returns it verbatim (relative or not --
// the caller chose it explicitly, so it is not this package's place to
// second-guess it). Tests use it to point at a t.TempDir() file instead of
// touching a real home directory.
const ConfigEnvVar = "RESTFORGE_CONFIG"

// configFileName is the file within the restforge config directory that
// holds the backend list, used only when ConfigEnvVar is unset.
const configFileName = "config.toml"

// Path resolves the config file location: ConfigEnvVar's value if set,
// otherwise <os.UserConfigDir()>/restforge/config.toml. os.UserConfigDir
// already honours $XDG_CONFIG_HOME on Linux, so no further platform-specific
// logic belongs here. The only error this can return is UserConfigDir's own
// -- the location genuinely cannot be determined, e.g. neither
// $XDG_CONFIG_HOME nor $HOME is set.
func Path() (string, error) {
	if p := os.Getenv(ConfigEnvVar); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "restforge", configFileName), nil
}
