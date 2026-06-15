package pkgmgr

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Options controls installer behaviour.
type Options struct {
	// DBPath is the path to db.json (default: DefaultDBPath).
	DBPath string
	// OverlayDir is where package files are extracted (default: DefaultOverlayDir).
	OverlayDir string
	// PubKey is the Ed25519 public key used to verify package signatures.
	PubKey ed25519.PublicKey
}

func (o *Options) dbPath() string {
	if o.DBPath != "" {
		return o.DBPath
	}
	return DefaultDBPath
}

func (o *Options) overlayDir() string {
	if o.OverlayDir != "" {
		return o.OverlayDir
	}
	return DefaultOverlayDir
}

// Install reads a .nupkg file, verifies its signature and checksums, extracts
// the payload into the overlay directory, and records the install in the DB.
// If the package is already installed its files are overwritten (upgrade).
func Install(pkgPath string, opts Options) (*Manifest, error) {
	f, err := os.Open(pkgPath)
	if err != nil {
		return nil, fmt.Errorf("open package: %w", err)
	}
	defer f.Close()

	m, payload, err := OpenPackage(f, opts.PubKey)
	if err != nil {
		return nil, err
	}

	db, err := LoadDB(opts.dbPath())
	if err != nil {
		return nil, err
	}

	// Verify declared dependencies are installed.
	for _, dep := range m.Depends {
		if _, ok := db.Get(dep); !ok {
			return nil, fmt.Errorf("unsatisfied dependency: %s requires %s (not installed)", m.Name, dep)
		}
	}

	overlay := opts.overlayDir()
	if err := os.MkdirAll(overlay, 0o755); err != nil {
		return nil, fmt.Errorf("create overlay dir: %w", err)
	}

	var installed []string
	for _, fe := range m.Files {
		dst := filepath.Join(overlay, fe.Path)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, fmt.Errorf("create dir for %s: %w", fe.Path, err)
		}
		mode := os.FileMode(0o644)
		if fe.Mode != "" {
			if parsed, err := strconv.ParseUint(fe.Mode, 8, 32); err == nil {
				mode = os.FileMode(parsed)
			}
		}
		if err := os.WriteFile(dst, payload[fe.Path], mode); err != nil {
			return nil, fmt.Errorf("write %s: %w", fe.Path, err)
		}
		installed = append(installed, fe.Path)
	}

	db.Add(InstallRecord{
		Name:        m.Name,
		Version:     m.Version,
		Description: m.Description,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
		Files:       installed,
		Depends:     m.Depends,
		RemoveHook:  m.RemoveHook,
	})
	if err := db.Save(opts.dbPath()); err != nil {
		return nil, fmt.Errorf("save package db: %w", err)
	}

	// Run install hook if specified (relative to overlay).
	if m.InstallHook != "" {
		runHook(overlay, m.InstallHook)
	}

	return m, nil
}

// Remove uninstalls a named package, running its remove hook first if present.
// It refuses to remove a package that other installed packages depend on.
func Remove(name string, opts Options) error {
	db, err := LoadDB(opts.dbPath())
	if err != nil {
		return err
	}

	rec, ok := db.Get(name)
	if !ok {
		return fmt.Errorf("package %q is not installed", name)
	}

	// Refuse if dependents exist.
	if deps := db.Dependents(name); len(deps) != 0 {
		return fmt.Errorf("cannot remove %s: required by %s", name, strings.Join(deps, ", "))
	}

	overlay := opts.overlayDir()

	// Run remove hook before deleting files.
	if rec.RemoveHook != "" {
		runHook(overlay, rec.RemoveHook)
	}

	for _, path := range rec.Files {
		dst := filepath.Join(overlay, path)
		_ = os.Remove(dst)
	}

	db.Remove(name)
	return db.Save(opts.dbPath())
}

// List returns all installed packages sorted by name.
func List(opts Options) ([]InstallRecord, error) {
	db, err := LoadDB(opts.dbPath())
	if err != nil {
		return nil, err
	}
	recs := make([]InstallRecord, 0, len(db.Packages))
	for _, r := range db.Packages {
		recs = append(recs, r)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	return recs, nil
}

// LoadPubKey reads a hex-encoded Ed25519 public key from path.
// The file must contain exactly 64 hex characters (32 bytes).
func LoadPubKey(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read public key: %w", err)
	}
	keyHex := strings.TrimSpace(string(data))
	raw, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key must be %d bytes, got %d", ed25519.PublicKeySize, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

// runHook executes hook relative to overlayDir, logging but not propagating errors.
func runHook(overlayDir, hook string) {
	parts := strings.Fields(hook)
	if len(parts) == 0 {
		return
	}
	bin := filepath.Join(overlayDir, parts[0])
	cmd := exec.Command(bin, parts[1:]...)
	cmd.Dir = overlayDir
	_ = cmd.Run()
}
