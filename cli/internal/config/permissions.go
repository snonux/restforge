package config

import "io/fs"

// insecurePermBits are the mode bits that must be clear for the config file
// to be considered private: any bit granting the owning group or "other"
// read, write or execute. The file holds backend secrets in the clear, so
// any of these bits being set means something other than the file's owner
// can read them.
const insecurePermBits = fs.FileMode(0o077)

// warnIfInsecure reports, via Warnf, when info's mode grants group or other
// any access. It never blocks the load -- the caller still reads the file
// afterwards -- because refusing to load an insecure file would trade a
// recoverable problem (loose permissions, fixed by a chmod) for an
// unrecoverable one from the user's point of view (every configured backend
// disappearing). Warning and continuing lets the user fix the mode without
// losing their configuration in the meantime.
func warnIfInsecure(path string, info fs.FileInfo) {
	if perm := info.Mode().Perm(); perm&insecurePermBits != 0 {
		Warnf("%s has mode %04o, which grants group or other access to backend secrets; run \"chmod 0600 %s\" to fix it", path, perm, path)
	}
}
