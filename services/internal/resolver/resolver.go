// Package resolver computes a linearised start order from a set of units.
//
// It implements Kahn's algorithm for topological sorting and detects
// dependency cycles, reporting them with the full cycle path.
package resolver

import (
	"fmt"
	"strings"

	"github.com/yasserrmd/nuraos/services/internal/unit"
)

// Plan is the ordered sequence of units to start.
type Plan struct {
	// Order is the start sequence from first to last.
	Order []*unit.Unit
}

// Resolve computes the start order for the given units.
// It returns an error if:
//   - a unit depends on a name that is not in the set
//   - there is a dependency cycle
func Resolve(units []*unit.Unit) (*Plan, error) {
	// Build index by name.
	byName := make(map[string]*unit.Unit, len(units))
	for _, u := range units {
		if _, exists := byName[u.Name]; exists {
			return nil, fmt.Errorf("duplicate unit name: %s", u.Name)
		}
		byName[u.Name] = u
	}

	// Validate all referenced names exist.
	for _, u := range units {
		for _, dep := range allDeps(u) {
			if _, ok := byName[dep]; !ok {
				return nil, fmt.Errorf("unit %q depends on unknown unit %q", u.Name, dep)
			}
		}
	}

	// Kahn's algorithm: compute in-degree per node.
	inDegree := make(map[string]int, len(units))
	// adjacency: node -> list of nodes that depend on node
	dependants := make(map[string][]string, len(units))
	for _, u := range units {
		if _, ok := inDegree[u.Name]; !ok {
			inDegree[u.Name] = 0
		}
		for _, dep := range allDeps(u) {
			inDegree[u.Name]++
			dependants[dep] = append(dependants[dep], u.Name)
		}
	}

	// Seed the queue with all nodes that have no dependencies.
	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}
	// Sort for deterministic output.
	sortStrings(queue)

	var order []*unit.Unit
	for len(queue) > 0 {
		// Pop front.
		name := queue[0]
		queue = queue[1:]
		order = append(order, byName[name])

		// Reduce in-degree for all units that depend on name.
		deps := dependants[name]
		sortStrings(deps)
		for _, d := range deps {
			inDegree[d]--
			if inDegree[d] == 0 {
				queue = append(queue, d)
				sortStrings(queue)
			}
		}
	}

	// If order length < units length, a cycle exists.
	if len(order) != len(units) {
		cycle := detectCycle(units, byName)
		return nil, fmt.Errorf("dependency cycle detected: %s", cycle)
	}

	return &Plan{Order: order}, nil
}

// allDeps merges After and Requires, deduplicating.
func allDeps(u *unit.Unit) []string {
	seen := make(map[string]bool)
	var result []string
	for _, d := range append(u.After, u.Requires...) {
		if !seen[d] {
			seen[d] = true
			result = append(result, d)
		}
	}
	return result
}

// detectCycle uses DFS to find and report one cycle.
func detectCycle(units []*unit.Unit, byName map[string]*unit.Unit) string {
	visited := make(map[string]int) // 0=unvisited, 1=in-stack, 2=done
	var path []string

	var dfs func(name string) bool
	dfs = func(name string) bool {
		visited[name] = 1
		path = append(path, name)
		for _, dep := range allDeps(byName[name]) {
			if visited[dep] == 1 {
				// found cycle; path already contains the cycle nodes
				return true
			}
			if visited[dep] == 0 {
				if dfs(dep) {
					return true
				}
			}
		}
		path = path[:len(path)-1]
		visited[name] = 2
		return false
	}

	for _, u := range units {
		if visited[u.Name] == 0 {
			if dfs(u.Name) {
				return strings.Join(path, " -> ")
			}
		}
	}
	return "(unknown cycle)"
}

// sortStrings sorts a string slice in place (insertion sort for small slices).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}
