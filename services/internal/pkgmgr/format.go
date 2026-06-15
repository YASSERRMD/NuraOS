// Package pkgmgr implements the NuraOS package format (.nupkg), signature
// verification, package database, and install/remove/list operations.
//
// Package archive layout (gzip-compressed tar):
//
//	manifest.json  -- JSON manifest (name, version, files, checksums, hooks)
//	manifest.sig   -- raw 64-byte Ed25519 signature of manifest.json content
//	<file paths>   -- payload files listed in manifest, relative to overlay root
//
// Packages install into /data/overlay/ preserving relative paths from the archive.
// The package database lives at /data/packages/db.json.
package pkgmgr

const (
	SchemaVersion = 1

	// DefaultDBPath is the package database location on the persistent partition.
	DefaultDBPath = "/data/packages/db.json"

	// DefaultOverlayDir is where package files are extracted.
	DefaultOverlayDir = "/data/overlay"

	// DefaultPubKeyPath is the Ed25519 public key used to verify package signatures.
	DefaultPubKeyPath = "/etc/nura/pkg.pub"
)

// FileEntry describes one file in a package.
type FileEntry struct {
	// Path is the destination path relative to the overlay root (e.g. "sbin/my-tool").
	Path string `json:"path"`
	// SHA256 is the lowercase hex SHA-256 digest of the file content.
	SHA256 string `json:"sha256"`
	// Mode is the octal file permission string (e.g. "0755").
	Mode string `json:"mode"`
}

// Manifest is the parsed content of manifest.json inside a .nupkg archive.
type Manifest struct {
	Schema      int         `json:"schema"`
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Description string      `json:"description"`
	Arch        string      `json:"arch"`
	Depends     []string    `json:"depends,omitempty"`
	Files       []FileEntry `json:"files"`
	// InstallHook is an optional command run after installation (relative to overlay).
	InstallHook string `json:"install_hook,omitempty"`
	// RemoveHook is an optional command run before removal (relative to overlay).
	RemoveHook string `json:"remove_hook,omitempty"`
}
