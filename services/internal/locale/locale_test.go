package locale_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/locale"
)

// TestLoadDefaultWhenFileMissing verifies C.UTF-8 is used when no config exists.
func TestLoadDefaultWhenFileMissing(t *testing.T) {
	cfg := locale.Load("/nonexistent-locale-path-xyz")
	if cfg.Locale != locale.DefaultLocale {
		t.Errorf("Load missing = %q; want %q", cfg.Locale, locale.DefaultLocale)
	}
	if cfg.Encoding != "UTF-8" {
		t.Errorf("Encoding = %q; want UTF-8", cfg.Encoding)
	}
}

// TestLoadValidLocale verifies a valid locale file is read correctly.
func TestLoadValidLocale(t *testing.T) {
	f := filepath.Join(t.TempDir(), "locale")
	os.WriteFile(f, []byte("en_US.UTF-8\n"), 0644)
	cfg := locale.Load(f)
	if cfg.Locale != "en_US.UTF-8" {
		t.Errorf("Load = %q; want en_US.UTF-8", cfg.Locale)
	}
}

// TestLoadRejectsNonUTF8Locale verifies locales without .UTF-8 suffix are
// rejected and default is used.
func TestLoadRejectsNonUTF8Locale(t *testing.T) {
	f := filepath.Join(t.TempDir(), "locale")
	os.WriteFile(f, []byte("en_US.ISO-8859-1\n"), 0644)
	cfg := locale.Load(f)
	if cfg.Locale != locale.DefaultLocale {
		t.Errorf("Load non-UTF-8 locale = %q; want default %q", cfg.Locale, locale.DefaultLocale)
	}
}

// TestLoadSkipsComments verifies # comment lines in the locale file are skipped.
func TestLoadSkipsComments(t *testing.T) {
	f := filepath.Join(t.TempDir(), "locale")
	os.WriteFile(f, []byte("# comment\nar_AE.UTF-8\n"), 0644)
	cfg := locale.Load(f)
	if cfg.Locale != "ar_AE.UTF-8" {
		t.Errorf("Load after comment = %q; want ar_AE.UTF-8", cfg.Locale)
	}
}

// TestValidateUTF8AcceptsValidInput verifies valid UTF-8 passes.
func TestValidateUTF8AcceptsValidInput(t *testing.T) {
	cases := []string{
		"hello world",
		"مرحبا",            // Arabic
		"مرحبا", // Arabic (explicit)
		"שלום",             // Hebrew
		"日本語",
		"",
	}
	for _, c := range cases {
		if !locale.ValidateUTF8([]byte(c)) {
			t.Errorf("ValidateUTF8(%q) = false; want true", c)
		}
	}
}

// TestValidateUTF8RejectsInvalidInput verifies invalid byte sequences are caught.
func TestValidateUTF8RejectsInvalidInput(t *testing.T) {
	invalid := []byte{0xff, 0xfe, 0x00}
	if locale.ValidateUTF8(invalid) {
		t.Error("ValidateUTF8(invalid bytes) = true; want false")
	}
}

// TestSanitiseUTF8PreservesValidText verifies valid text is not modified.
func TestSanitiseUTF8PreservesValidText(t *testing.T) {
	s := "Hello مرحبا 日本語 שלום"
	if got := locale.SanitiseUTF8(s); got != s {
		t.Errorf("SanitiseUTF8 modified valid text: got %q; want %q", got, s)
	}
}

// TestRoundTripArabicText verifies Arabic RTL text survives encode/decode losslessly.
func TestRoundTripArabicText(t *testing.T) {
	texts := []string{
		"مرحبا بالعالم",   // Arabic: "Hello World"
		"سلام",             // Arabic: "Hello"
		"شلوم",             // Hebrew
		"Hello World",
		"UTF-8 test: éàü", // Latin extended
	}
	for _, s := range texts {
		out, ok := locale.RoundTrip(s)
		if !ok {
			t.Errorf("RoundTrip(%q) was lossy: got %q", s, out)
		}
	}
}

// TestIsRTLDetectsArabicAndHebrew verifies RTL detection.
func TestIsRTLDetectsArabicAndHebrew(t *testing.T) {
	if !locale.IsRTL("مرحبا") {
		t.Error("IsRTL(Arabic) = false; want true")
	}
	if !locale.IsRTL("שלום") {
		t.Error("IsRTL(Hebrew) = false; want true")
	}
	if locale.IsRTL("Hello World") {
		t.Error("IsRTL(Latin) = true; want false")
	}
	if locale.IsRTL("日本語") {
		t.Error("IsRTL(Japanese) = true; want false")
	}
}

// TestValidateLocaleAcceptsUTF8Suffix verifies valid locales pass.
func TestValidateLocaleAcceptsUTF8Suffix(t *testing.T) {
	valid := []string{"C.UTF-8", "en_US.UTF-8", "ar_AE.UTF-8", "ja_JP.UTF-8"}
	for _, l := range valid {
		if err := locale.ValidateLocale(l); err != nil {
			t.Errorf("ValidateLocale(%q) unexpected error: %v", l, err)
		}
	}
}

// TestValidateLocaleRejectsNonUTF8 verifies non-UTF-8 locales are rejected.
func TestValidateLocaleRejectsNonUTF8(t *testing.T) {
	invalid := []string{"en_US.ISO-8859-1", "C", "POSIX", "en_US"}
	for _, l := range invalid {
		if err := locale.ValidateLocale(l); err == nil {
			t.Errorf("ValidateLocale(%q) expected error; got nil", l)
		}
	}
}

// TestEnvVarsContainsLANG verifies EnvVars includes LANG and LC_ALL.
func TestEnvVarsContainsLANG(t *testing.T) {
	cfg := locale.Load("/nonexistent")
	vars := locale.EnvVars(cfg)
	hasLANG := false
	hasLCALL := false
	for _, v := range vars {
		if v == "LANG=C.UTF-8" {
			hasLANG = true
		}
		if v == "LC_ALL=C.UTF-8" {
			hasLCALL = true
		}
	}
	if !hasLANG {
		t.Error("EnvVars missing LANG=C.UTF-8")
	}
	if !hasLCALL {
		t.Error("EnvVars missing LC_ALL=C.UTF-8")
	}
}
