package subcmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/yasserrmd/nuraos/services/internal/resolver"
	"github.com/yasserrmd/nuraos/services/internal/unit"
)

// DryRun loads units from dir, resolves the start order, and prints the plan
// to w. It does not start any processes.
func DryRun(dir string, w io.Writer) error {
	units, err := unit.LoadDir(dir)
	if err != nil {
		return fmt.Errorf("load units: %w", err)
	}

	plan, err := resolver.Resolve(units)
	if err != nil {
		return fmt.Errorf("dependency resolution: %w", err)
	}

	fmt.Fprintf(w, "Start plan (%d unit(s)):\n", len(plan.Order))
	fmt.Fprintf(w, "%-4s  %-20s  %-10s  %-10s  %s\n", "Seq", "Name", "Type", "Restart", "Requires")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 72))
	for i, u := range plan.Order {
		requires := strings.Join(u.Requires, ",")
		if requires == "" {
			requires = "-"
		}
		fmt.Fprintf(w, "%-4d  %-20s  %-10s  %-10s  %s\n",
			i+1, u.Name, string(u.Type), string(u.Restart.Policy), requires)
	}
	return nil
}
