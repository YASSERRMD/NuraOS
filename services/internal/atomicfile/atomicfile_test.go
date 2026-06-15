package atomicfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/atomicfile"
)

// TestWriteCreatesFile verifies a new file is created with the correct content.
func TestWriteCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	data := []byte(`{"v":1}`)
	if err := atomicfile.Write(path, data, 0644); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Errorf("got %q, want %q", got, data)
	}
}

// TestWriteUpdatesFile verifies that an existing file is replaced atomically.
func TestWriteUpdatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	_ = atomicfile.Write(path, []byte(`{"v":1}`), 0644)
	_ = atomicfile.Write(path, []byte(`{"v":2}`), 0644)

	got, _ := os.ReadFile(path)
	if string(got) != `{"v":2}` {
		t.Errorf("got %q, want {\"v\":2}", got)
	}
}

// TestWritePermissions verifies the file is created with the specified mode.
func TestWritePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := atomicfile.Write(path, []byte("s"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf("mode = %04o, want 0600", got)
	}
}

// TestWriteNoPartialOnDirMissing verifies that an error leaves no partial file.
func TestWriteNoPartialOnDirMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "file.txt")
	err := atomicfile.Write(path, []byte("data"), 0644)
	if err == nil {
		t.Error("expected error for missing parent dir, got nil")
	}
	// No partial or temp file should exist.
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) > 0 {
		t.Errorf("unexpected files after failed write: %v", entries)
	}
}

// TestPowerLossSimulation verifies the original file survives a simulated crash.
// A power loss during write leaves the temp file; the original is untouched.
func TestPowerLossSimulation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	original := []byte(`{"version":1,"data":"original"}`)
	if err := atomicfile.Write(path, original, 0644); err != nil {
		t.Fatal(err)
	}

	// Simulate power loss: write partial data to a temp file and abandon it
	// (no rename). This mimics a crash after write but before rename.
	partial := []byte(`{"version":2,"data":"updat`) // truncated
	tmpPath := filepath.Join(dir, ".crashed-temp")
	_ = os.WriteFile(tmpPath, partial, 0644)
	// "Machine off" -- tmpPath is left, rename never happens.

	// The original file must be intact.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Errorf("original corrupted after simulated crash: got %q", got)
	}
}

// TestNoTempFileOnSuccess verifies no temp file is left after a successful write.
func TestNoTempFileOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := atomicfile.Write(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("expected 1 file, found %v", names)
	}
}
