package subcmd

import (
	"fmt"
	"io"

	"github.com/yasserrmd/nuraos/services/internal/resolver"
	"github.com/yasserrmd/nuraos/services/internal/unit"
)

// Check loads all units from dir, validates them, and resolves their order.
// It reports any errors to w and returns a non-nil error if validation fails.
func Check(dir string, w io.Writer) error {
	units, err := unit.LoadDir(dir)
	if err != nil {
		return fmt.Errorf("load units: %w", err)
	}
	fmt.Fprintf(w, "loaded %d enabled unit(s) from %s\n", len(units), dir)

	for _, u := range units {
		fmt.Fprintf(w, "  ok  %s (%s)\n", u.Name, u.Type)
	}

	if _, err := resolver.Resolve(units); err != nil {
		return fmt.Errorf("dependency resolution: %w", err)
	}
	fmt.Fprintf(w, "dependency graph: acyclic, all references satisfied\n")
	return nil
}
