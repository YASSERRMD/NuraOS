package cap_test

import (
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/cap"
)

func TestValidateKnownCaps(t *testing.T) {
	known := []string{
		"cap_chown", "cap_dac_override", "cap_setuid", "cap_setgid",
		"cap_setpcap", "cap_net_admin", "cap_sys_admin", "cap_bpf",
		"cap_checkpoint_restore",
	}
	if err := cap.Validate(known); err != nil {
		t.Fatalf("Validate known caps: %v", err)
	}
}

func TestValidateUnknownCap(t *testing.T) {
	if err := cap.Validate([]string{"cap_fly"}); err == nil {
		t.Fatal("expected error for unknown capability, got nil")
	}
}

func TestValidateAll(t *testing.T) {
	// "all" should expand to many caps (minus setuid/setgid) and validate cleanly.
	if err := cap.Validate([]string{"all"}); err != nil {
		t.Fatalf("Validate all: %v", err)
	}
}

func TestDropUnknownReturnsError(t *testing.T) {
	if err := cap.Drop([]string{"cap_unicorn"}); err == nil {
		t.Fatal("expected error for unknown capability name, got nil")
	}
}
