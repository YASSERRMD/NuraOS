package diagbundle_test

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/diagbundle"
)

// populateCrashDir creates synthetic crash files in dir.
func populateCrashDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		name := filepath.Join(dir, "gateway-20240101T00000"+string(rune('0'+i))+"Z.json")
		content := `{"service":{"name":"gateway"},"log_tail":["password=s3cr3t","exiting"]}`
		if err := os.WriteFile(name, []byte(content), 0644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	return dir
}

// TestBuildCreatesArchive verifies Build produces a .tar.gz file.
func TestBuildCreatesArchive(t *testing.T) {
	crashDir := populateCrashDir(t)
	outDir := t.TempDir()

	path, err := diagbundle.Build(diagbundle.Options{
		CrashDir: crashDir,
		OutDir:   outDir,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("archive not found at %s: %v", path, err)
	}
	if !strings.HasSuffix(path, ".tar.gz") {
		t.Errorf("archive path %q does not end with .tar.gz", path)
	}
}

// TestBuildArchiveContainsFiles verifies crash files are in the archive.
func TestBuildArchiveContainsFiles(t *testing.T) {
	crashDir := populateCrashDir(t)
	outDir := t.TempDir()

	archivePath, err := diagbundle.Build(diagbundle.Options{
		CrashDir: crashDir,
		OutDir:   outDir,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	tr := tar.NewReader(gr)

	count := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		if strings.HasSuffix(hdr.Name, ".json") {
			count++
		}
	}
	if count < 3 {
		t.Errorf("archive contains %d JSON files; want >= 3", count)
	}
}

// TestBuildRedactsSecrets verifies secrets in crash files are scrubbed in the archive.
func TestBuildRedactsSecrets(t *testing.T) {
	crashDir := populateCrashDir(t)
	outDir := t.TempDir()

	archivePath, err := diagbundle.Build(diagbundle.Options{
		CrashDir: crashDir,
		OutDir:   outDir,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	f, _ := os.Open(archivePath)
	defer f.Close()
	gr, _ := gzip.NewReader(f)
	tr := tar.NewReader(gr)

	for {
		_, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		data, _ := io.ReadAll(tr)
		if strings.Contains(string(data), "s3cr3t") {
			t.Errorf("archive entry contains unredacted secret 's3cr3t'")
		}
	}
}

// TestBuildEmptyCrashDirErrors verifies Build fails on missing crash dir.
func TestBuildEmptyCrashDirErrors(t *testing.T) {
	_, err := diagbundle.Build(diagbundle.Options{
		CrashDir: "/nonexistent-crash-dir-xyz",
		OutDir:   t.TempDir(),
	})
	if err == nil {
		t.Error("Build with missing crash dir: expected error, got nil")
	}
}
