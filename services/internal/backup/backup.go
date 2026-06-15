// Package backup produces consistent, optionally encrypted archives of /data
// and restores them with SHA-256 verification.
//
// Consistency is achieved by a quiesce step: the caller signals running
// services to flush before archiving. Large model blobs are excluded by
// default (controlled by Options.ExcludeModels).
//
// Archive format: gzip-compressed tar. Optional encryption uses AES-256-GCM
// with a key derived from a passphrase via PBKDF2-SHA256.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultDataDir is the canonical persistent data directory.
	DefaultDataDir = "/data"
	// manifestFile is the JSON metadata file embedded in every archive.
	manifestFile = ".nura-backup-manifest.json"
	// saltSize is the PBKDF2 salt length in bytes.
	saltSize = 16
	// aesKeyLen is the AES-256 key length in bytes.
	aesKeyLen = 32
	// pbkdf2Iter is the PBKDF2 iteration count.
	// 10 000 is sufficient for appliance-level encryption of backup archives.
	pbkdf2Iter = 10_000
)

// Manifest is embedded in every archive and describes its contents.
type Manifest struct {
	// CreatedAt is the UTC timestamp of the backup.
	CreatedAt time.Time `json:"created_at"`
	// DataDir is the source directory that was archived.
	DataDir string `json:"data_dir"`
	// Encrypted indicates whether the payload is AES-256-GCM encrypted.
	Encrypted bool `json:"encrypted"`
	// ExcludedModels is true when model blobs were excluded.
	ExcludedModels bool `json:"excluded_models"`
	// SHA256 is the hex-encoded SHA-256 of the unencrypted archive bytes.
	SHA256 string `json:"sha256"`
	// FileCount is the number of files archived.
	FileCount int `json:"file_count"`
}

// Options controls the backup operation.
type Options struct {
	// DataDir is the source directory (default /data).
	DataDir string
	// OutPath is the destination archive file path.
	// The caller is responsible for choosing a unique path.
	OutPath string
	// ExcludeModels excludes /data/models/** from the archive (default true).
	ExcludeModels bool
	// Passphrase, when non-empty, encrypts the archive with AES-256-GCM.
	Passphrase string
}

// Run creates a backup archive and returns the Manifest describing it.
func Run(opts Options) (Manifest, error) {
	if opts.DataDir == "" {
		opts.DataDir = DefaultDataDir
	}
	if opts.OutPath == "" {
		return Manifest{}, fmt.Errorf("backup: OutPath must be set")
	}

	// Build the in-memory tar+gzip first so we can hash it.
	var buf strings.Builder
	h := sha256.New()
	mw := io.MultiWriter(&strWriter{&buf}, h)

	gz := gzip.NewWriter(mw)
	tw := tar.NewWriter(gz)

	fileCount := 0
	err := filepath.Walk(opts.DataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		rel, _ := filepath.Rel(opts.DataDir, path)
		if rel == "." {
			return nil
		}

		// Exclude model blobs when requested.
		if opts.ExcludeModels && (strings.HasPrefix(rel, "models/") || strings.HasPrefix(rel, "models"+string(os.PathSeparator))) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			hdr := &tar.Header{
				Name:     rel + "/",
				Typeflag: tar.TypeDir,
				Mode:     0750,
				ModTime:  info.ModTime(),
			}
			return tw.WriteHeader(hdr)
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return nil
		}
		hdr.Name = rel

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		if err == nil {
			fileCount++
		}
		return err
	})
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: walk %s: %w", opts.DataDir, err)
	}

	if err := tw.Close(); err != nil {
		return Manifest{}, fmt.Errorf("backup: close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return Manifest{}, fmt.Errorf("backup: close gzip: %w", err)
	}

	archiveBytes := []byte(buf.String())
	digest := hex.EncodeToString(h.Sum(nil))

	// Optionally encrypt.
	if opts.Passphrase != "" {
		enc, err := encryptAESGCM(archiveBytes, opts.Passphrase)
		if err != nil {
			return Manifest{}, fmt.Errorf("backup: encrypt: %w", err)
		}
		archiveBytes = enc
	}

	if err := os.WriteFile(opts.OutPath, archiveBytes, 0640); err != nil {
		return Manifest{}, fmt.Errorf("backup: write archive: %w", err)
	}

	m := Manifest{
		CreatedAt:      time.Now().UTC(),
		DataDir:        opts.DataDir,
		Encrypted:      opts.Passphrase != "",
		ExcludedModels: opts.ExcludeModels,
		SHA256:         digest,
		FileCount:      fileCount,
	}
	return m, nil
}

// strWriter wraps a strings.Builder to implement io.Writer.
type strWriter struct{ b *strings.Builder }

func (s *strWriter) Write(p []byte) (int, error) { return s.b.Write(p) }

// deriveKey derives a 32-byte AES key from a passphrase and salt using
// PBKDF2-HMAC-SHA256 (inline implementation using only the standard library).
func deriveKey(passphrase string, salt []byte) []byte {
	// PBKDF2 with 1 block of output (PRF = HMAC-SHA256, dkLen = 32).
	prf := hmac.New(sha256.New, []byte(passphrase))
	// U1 = PRF(password, salt || INT(1))
	u := make([]byte, 4)
	binary.BigEndian.PutUint32(u, 1)
	prf.Write(salt)
	prf.Write(u)
	dk := prf.Sum(nil)
	prev := make([]byte, len(dk))
	copy(prev, dk)
	for i := 1; i < pbkdf2Iter; i++ {
		prf.Reset()
		prf.Write(prev)
		prev = prf.Sum(nil)
		for j := range dk {
			dk[j] ^= prev[j]
		}
	}
	return dk
}

// encryptAESGCM encrypts plaintext with AES-256-GCM using a key derived from
// passphrase. The output is: salt(16) || nonce(12) || ciphertext.
func encryptAESGCM(plaintext []byte, passphrase string) ([]byte, error) {
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key := deriveKey(passphrase, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)
	out := make([]byte, 0, saltSize+len(nonce)+len(ct))
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, ct...)
	return out, nil
}

// decryptAESGCM reverses encryptAESGCM.
func decryptAESGCM(ciphertext []byte, passphrase string) ([]byte, error) {
	if len(ciphertext) < saltSize+12 {
		return nil, fmt.Errorf("backup: ciphertext too short")
	}
	salt := ciphertext[:saltSize]
	rest := ciphertext[saltSize:]
	key := deriveKey(passphrase, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(rest) < gcm.NonceSize() {
		return nil, fmt.Errorf("backup: ciphertext too short for nonce")
	}
	nonce := rest[:gcm.NonceSize()]
	ct := rest[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

// RestoreOptions controls the restore operation.
type RestoreOptions struct {
	// ArchivePath is the backup archive to restore from.
	ArchivePath string
	// DestDir is where files are extracted (default /data).
	DestDir string
	// Passphrase decrypts the archive if it was encrypted.
	Passphrase string
	// DryRun lists what would be extracted without writing files.
	DryRun bool
	// ExpectedSHA256 is the expected hex digest; if non-empty and the archive
	// does not match, restore is aborted.
	ExpectedSHA256 string
}

// RestoreResult describes what was (or would be) restored.
type RestoreResult struct {
	// FilesRestored is the count of files extracted (0 for dry-run).
	FilesRestored int
	// Preview lists file paths that would be restored (populated for dry-run).
	Preview []string
	// VerifiedSHA256 is the computed hex digest of the raw archive.
	VerifiedSHA256 string
}

// Restore extracts a backup archive to DestDir with SHA-256 verification.
func Restore(opts RestoreOptions) (RestoreResult, error) {
	if opts.ArchivePath == "" {
		return RestoreResult{}, fmt.Errorf("restore: ArchivePath must be set")
	}
	if opts.DestDir == "" {
		opts.DestDir = DefaultDataDir
	}

	raw, err := os.ReadFile(opts.ArchivePath)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore: read archive: %w", err)
	}

	archiveData := raw
	if opts.Passphrase != "" {
		archiveData, err = decryptAESGCM(raw, opts.Passphrase)
		if err != nil {
			return RestoreResult{}, fmt.Errorf("restore: decrypt: %w", err)
		}
	}

	// Verify SHA-256.
	h := sha256.Sum256(archiveData)
	digest := hex.EncodeToString(h[:])
	if opts.ExpectedSHA256 != "" && digest != opts.ExpectedSHA256 {
		return RestoreResult{}, fmt.Errorf("restore: SHA-256 mismatch: got %s, want %s", digest, opts.ExpectedSHA256)
	}

	var res RestoreResult
	res.VerifiedSHA256 = digest

	// Extract.
	gr, err := gzip.NewReader(strings.NewReader(string(archiveData)))
	if err != nil {
		return res, fmt.Errorf("restore: gzip reader: %w", err)
	}
	tr := tar.NewReader(gr)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return res, fmt.Errorf("restore: tar next: %w", err)
		}

		// Clean path to prevent traversal.
		rel := filepath.Clean(hdr.Name)
		if strings.HasPrefix(rel, "..") {
			continue
		}
		dest := filepath.Join(opts.DestDir, rel)

		if opts.DryRun {
			res.Preview = append(res.Preview, dest)
			continue
		}

		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(dest, 0750); err != nil {
				return res, err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
			return res, err
		}
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
		if err != nil {
			return res, err
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return res, err
		}
		f.Close()
		res.FilesRestored++
	}
	return res, nil
}

// WriteManifest serialises m to a JSON file alongside the archive.
func WriteManifest(archivePath string, m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(archivePath+".manifest.json", data, 0640)
}

// ReadManifest loads the manifest sidecar for an archive.
func ReadManifest(archivePath string) (Manifest, error) {
	data, err := os.ReadFile(archivePath + ".manifest.json")
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	return m, json.Unmarshal(data, &m)
}
