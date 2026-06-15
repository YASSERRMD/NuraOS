package pkgmgr_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/pkgmgr"
)

func genKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pub, priv
}

func buildTestPackage(t *testing.T, priv ed25519.PrivateKey, m *pkgmgr.Manifest, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := pkgmgr.BuildPackage(&buf, m, files, priv); err != nil {
		t.Fatalf("BuildPackage: %v", err)
	}
	return buf.Bytes()
}

func TestOpenPackageValid(t *testing.T) {
	pub, priv := genKey(t)
	m := &pkgmgr.Manifest{
		Schema:  1,
		Name:    "test-pkg",
		Version: "1.0.0",
		Files:   []pkgmgr.FileEntry{{Path: "sbin/hello", Mode: "0755"}},
	}
	files := map[string][]byte{"sbin/hello": []byte("#!/bin/sh\necho hello\n")}
	data := buildTestPackage(t, priv, m, files)

	got, payload, err := pkgmgr.OpenPackage(bytes.NewReader(data), pub)
	if err != nil {
		t.Fatalf("OpenPackage: %v", err)
	}
	if got.Name != "test-pkg" {
		t.Errorf("Name = %q; want test-pkg", got.Name)
	}
	if string(payload["sbin/hello"]) != string(files["sbin/hello"]) {
		t.Error("payload content mismatch")
	}
}

func TestOpenPackageWrongKey(t *testing.T) {
	_, priv := genKey(t)
	otherPub, _ := genKey(t)

	m := &pkgmgr.Manifest{Schema: 1, Name: "pkg", Version: "1.0.0",
		Files: []pkgmgr.FileEntry{{Path: "sbin/x", Mode: "0755"}}}
	data := buildTestPackage(t, priv, m, map[string][]byte{"sbin/x": []byte("x")})

	_, _, err := pkgmgr.OpenPackage(bytes.NewReader(data), otherPub)
	if err == nil {
		t.Fatal("expected error with wrong public key, got nil")
	}
}

func TestOpenPackageTamperedPayload(t *testing.T) {
	pub, priv := genKey(t)
	// Pre-set a wrong SHA256 so BuildPackage does not recompute it.
	// This simulates a package whose manifest checksum was signed correctly
	// but whose payload file has been replaced with different content.
	wrongHash := "aabbccdd11223344aabbccdd11223344aabbccdd11223344aabbccdd11223344"
	m := &pkgmgr.Manifest{Schema: 1, Name: "pkg", Version: "1.0.0",
		Files: []pkgmgr.FileEntry{{Path: "sbin/x", Mode: "0755", SHA256: wrongHash}}}
	data := buildTestPackage(t, priv, m, map[string][]byte{"sbin/x": []byte("original content")})

	_, _, err := pkgmgr.OpenPackage(bytes.NewReader(data), pub)
	if err == nil {
		t.Fatal("expected error with wrong checksum, got nil")
	}
}

func TestInstallAndList(t *testing.T) {
	pub, priv := genKey(t)
	dir := t.TempDir()

	m := &pkgmgr.Manifest{
		Schema:  1,
		Name:    "my-addon",
		Version: "2.0.0",
		Files:   []pkgmgr.FileEntry{{Path: "sbin/addon", Mode: "0755"}},
	}
	data := buildTestPackage(t, priv, m, map[string][]byte{"sbin/addon": []byte("#!/bin/sh\n")})

	pkgFile := filepath.Join(dir, "my-addon-2.0.0.nupkg")
	if err := os.WriteFile(pkgFile, data, 0o644); err != nil {
		t.Fatal(err)
	}

	opts := pkgmgr.Options{
		DBPath:     filepath.Join(dir, "db.json"),
		OverlayDir: filepath.Join(dir, "overlay"),
		PubKey:     pub,
	}

	installed, err := pkgmgr.Install(pkgFile, opts)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if installed.Name != "my-addon" {
		t.Errorf("Name = %q; want my-addon", installed.Name)
	}

	// Verify file was extracted.
	got, err := os.ReadFile(filepath.Join(dir, "overlay", "sbin", "addon"))
	if err != nil {
		t.Fatalf("read installed file: %v", err)
	}
	if string(got) != "#!/bin/sh\n" {
		t.Errorf("file content = %q; want #!/bin/sh\\n", got)
	}

	recs, err := pkgmgr.List(opts)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(recs) != 1 || recs[0].Name != "my-addon" {
		t.Errorf("List = %v; want [{my-addon ...}]", recs)
	}
}

func TestRemove(t *testing.T) {
	pub, priv := genKey(t)
	dir := t.TempDir()

	m := &pkgmgr.Manifest{Schema: 1, Name: "rm-test", Version: "1.0.0",
		Files: []pkgmgr.FileEntry{{Path: "sbin/tool", Mode: "0755"}}}
	data := buildTestPackage(t, priv, m, map[string][]byte{"sbin/tool": []byte("x")})

	pkgFile := filepath.Join(dir, "rm-test.nupkg")
	if err := os.WriteFile(pkgFile, data, 0o644); err != nil {
		t.Fatal(err)
	}
	opts := pkgmgr.Options{
		DBPath:     filepath.Join(dir, "db.json"),
		OverlayDir: filepath.Join(dir, "overlay"),
		PubKey:     pub,
	}
	if _, err := pkgmgr.Install(pkgFile, opts); err != nil {
		t.Fatal(err)
	}

	if err := pkgmgr.Remove("rm-test", opts); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// File should be gone.
	if _, err := os.Stat(filepath.Join(dir, "overlay", "sbin", "tool")); !os.IsNotExist(err) {
		t.Error("file still exists after remove")
	}

	recs, _ := pkgmgr.List(opts)
	if len(recs) != 0 {
		t.Errorf("List after remove = %v; want empty", recs)
	}
}

func TestDependencyCheck(t *testing.T) {
	pub, priv := genKey(t)
	dir := t.TempDir()

	// Install base.
	base := &pkgmgr.Manifest{Schema: 1, Name: "base", Version: "1.0.0",
		Files: []pkgmgr.FileEntry{{Path: "sbin/base", Mode: "0755"}}}
	baseData := buildTestPackage(t, priv, base, map[string][]byte{"sbin/base": []byte("x")})
	baseFile := filepath.Join(dir, "base.nupkg")
	os.WriteFile(baseFile, baseData, 0o644)

	// Try to install addon that depends on base.
	addon := &pkgmgr.Manifest{Schema: 1, Name: "addon", Version: "1.0.0",
		Depends: []string{"base"},
		Files:   []pkgmgr.FileEntry{{Path: "sbin/addon", Mode: "0755"}}}
	addonData := buildTestPackage(t, priv, addon, map[string][]byte{"sbin/addon": []byte("y")})
	addonFile := filepath.Join(dir, "addon.nupkg")
	os.WriteFile(addonFile, addonData, 0o644)

	opts := pkgmgr.Options{
		DBPath:     filepath.Join(dir, "db.json"),
		OverlayDir: filepath.Join(dir, "overlay"),
		PubKey:     pub,
	}

	// Installing addon without base should fail.
	if _, err := pkgmgr.Install(addonFile, opts); err == nil {
		t.Fatal("expected error for unsatisfied dependency, got nil")
	}

	// Install base first, then addon.
	if _, err := pkgmgr.Install(baseFile, opts); err != nil {
		t.Fatalf("Install base: %v", err)
	}
	if _, err := pkgmgr.Install(addonFile, opts); err != nil {
		t.Fatalf("Install addon: %v", err)
	}

	// Removing base while addon depends on it should fail.
	if err := pkgmgr.Remove("base", opts); err == nil {
		t.Fatal("expected error when removing package with dependents, got nil")
	}

	// Remove addon first, then base succeeds.
	if err := pkgmgr.Remove("addon", opts); err != nil {
		t.Fatalf("Remove addon: %v", err)
	}
	if err := pkgmgr.Remove("base", opts); err != nil {
		t.Fatalf("Remove base: %v", err)
	}
}
