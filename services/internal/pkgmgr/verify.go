package pkgmgr

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrBadSignature is returned when the manifest signature is invalid.
var ErrBadSignature = errors.New("package signature verification failed")

// ErrBadChecksum is returned when a payload file's SHA-256 does not match the manifest.
var ErrBadChecksum = errors.New("package checksum mismatch")

// ErrMissingEntry is returned when an expected entry is absent from the archive.
var ErrMissingEntry = errors.New("missing archive entry")

// OpenPackage reads a .nupkg archive from r, verifies the Ed25519 signature of
// manifest.json and the SHA-256 checksum of every payload file listed in the
// manifest, then returns the parsed manifest and a map of path -> file content.
//
// The function rejects the package on any verification failure; it never
// partially installs a tampered archive.
func OpenPackage(r io.Reader, pubKey ed25519.PublicKey) (*Manifest, map[string][]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, nil, fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	// First pass: read every entry into memory.
	raw := make(map[string][]byte)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read archive: %w", err)
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, nil, fmt.Errorf("read entry %s: %w", name, err)
		}
		raw[name] = data
	}

	manifestBytes, ok := raw["manifest.json"]
	if !ok {
		return nil, nil, fmt.Errorf("%w: manifest.json", ErrMissingEntry)
	}
	sigBytes, ok := raw["manifest.sig"]
	if !ok {
		return nil, nil, fmt.Errorf("%w: manifest.sig", ErrMissingEntry)
	}

	if len(sigBytes) != ed25519.SignatureSize {
		return nil, nil, fmt.Errorf("%w: signature must be %d bytes, got %d",
			ErrBadSignature, ed25519.SignatureSize, len(sigBytes))
	}
	if !ed25519.Verify(pubKey, manifestBytes, sigBytes) {
		return nil, nil, ErrBadSignature
	}

	var m Manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return nil, nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Schema != SchemaVersion {
		return nil, nil, fmt.Errorf("unsupported package schema %d (expected %d)",
			m.Schema, SchemaVersion)
	}
	if m.Name == "" || m.Version == "" {
		return nil, nil, fmt.Errorf("manifest missing name or version")
	}

	// Verify each file listed in the manifest.
	payload := make(map[string][]byte, len(m.Files))
	for _, fe := range m.Files {
		data, exists := raw[fe.Path]
		if !exists {
			return nil, nil, fmt.Errorf("%w: %s", ErrMissingEntry, fe.Path)
		}
		sum := sha256.Sum256(data)
		got := hex.EncodeToString(sum[:])
		if !strings.EqualFold(got, fe.SHA256) {
			return nil, nil, fmt.Errorf("%w: %s (got %s want %s)",
				ErrBadChecksum, fe.Path, got, strings.ToLower(fe.SHA256))
		}
		payload[fe.Path] = data
	}

	return &m, payload, nil
}

// BuildPackage writes a signed .nupkg archive to w.
// files maps relative paths to their content.
// The manifest is signed with privKey; pubKey is not embedded in the archive.
func BuildPackage(w io.Writer, m *Manifest, files map[string][]byte, privKey ed25519.PrivateKey) error {
	// Compute SHA-256 for each file; skip if the caller already set it (allows
	// tests to supply an intentionally wrong checksum to verify rejection).
	for i, fe := range m.Files {
		data, ok := files[fe.Path]
		if !ok {
			return fmt.Errorf("file %s listed in manifest but not provided", fe.Path)
		}
		if fe.SHA256 == "" {
			sum := sha256.Sum256(data)
			m.Files[i].SHA256 = hex.EncodeToString(sum[:])
		}
	}

	manifestBytes, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	sig := ed25519.Sign(privKey, manifestBytes)

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	writeEntry := func(name string, data []byte, mode int64) error {
		hdr := &tar.Header{
			Name: name,
			Mode: mode,
			Size: int64(len(data)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err := tw.Write(data)
		return err
	}

	if err := writeEntry("manifest.json", manifestBytes, 0o644); err != nil {
		return fmt.Errorf("write manifest.json: %w", err)
	}
	if err := writeEntry("manifest.sig", sig, 0o644); err != nil {
		return fmt.Errorf("write manifest.sig: %w", err)
	}
	for _, fe := range m.Files {
		data := files[fe.Path]
		mode := int64(0o644)
		if fe.Mode != "" {
			var parsed uint64
			if _, err := fmt.Sscanf(fe.Mode, "%o", &parsed); err == nil {
				mode = int64(parsed)
			}
		}
		if err := writeEntry(fe.Path, data, mode); err != nil {
			return fmt.Errorf("write %s: %w", fe.Path, err)
		}
	}

	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}
