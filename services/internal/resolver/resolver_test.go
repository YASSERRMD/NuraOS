package resolver_test

import (
	"testing"

	"github.com/yasserrmd/nuraos/services/internal/resolver"
	"github.com/yasserrmd/nuraos/services/internal/unit"
)

func makeUnit(name string, requires, after []string) *unit.Unit {
	return &unit.Unit{
		Name:     name,
		Exec:     "/bin/" + name,
		Type:     unit.TypeLongrun,
		Requires: requires,
		After:    after,
		Enabled:  true,
	}
}

func names(plan *resolver.Plan) []string {
	result := make([]string, len(plan.Order))
	for i, u := range plan.Order {
		result[i] = u.Name
	}
	return result
}

func TestResolveLinear(t *testing.T) {
	// a -> b -> c (c starts first)
	units := []*unit.Unit{
		makeUnit("a", []string{"b"}, nil),
		makeUnit("b", []string{"c"}, nil),
		makeUnit("c", nil, nil),
	}
	plan, err := resolver.Resolve(units)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := names(plan)
	// c must come before b, b before a
	pos := func(name string) int {
		for i, n := range got {
			if n == name {
				return i
			}
		}
		return -1
	}
	if pos("c") >= pos("b") || pos("b") >= pos("a") {
		t.Errorf("wrong order: %v (want c < b < a)", got)
	}
}

func TestResolveDiamond(t *testing.T) {
	// a requires b and c; b requires d; c requires d
	units := []*unit.Unit{
		makeUnit("a", []string{"b", "c"}, nil),
		makeUnit("b", []string{"d"}, nil),
		makeUnit("c", []string{"d"}, nil),
		makeUnit("d", nil, nil),
	}
	plan, err := resolver.Resolve(units)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := names(plan)
	pos := func(name string) int {
		for i, n := range got {
			if n == name {
				return i
			}
		}
		return -1
	}
	if pos("d") >= pos("b") || pos("d") >= pos("c") {
		t.Errorf("d must start before b and c, got: %v", got)
	}
	if pos("b") >= pos("a") || pos("c") >= pos("a") {
		t.Errorf("a must start after b and c, got: %v", got)
	}
}

func TestResolveCycleDetected(t *testing.T) {
	units := []*unit.Unit{
		makeUnit("a", []string{"b"}, nil),
		makeUnit("b", []string{"a"}, nil),
	}
	_, err := resolver.Resolve(units)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
}

func TestResolveUnknownDep(t *testing.T) {
	units := []*unit.Unit{
		makeUnit("a", []string{"nonexistent"}, nil),
	}
	_, err := resolver.Resolve(units)
	if err == nil {
		t.Fatal("expected unknown dep error, got nil")
	}
}

func TestResolveDuplicateName(t *testing.T) {
	units := []*unit.Unit{
		makeUnit("a", nil, nil),
		makeUnit("a", nil, nil),
	}
	_, err := resolver.Resolve(units)
	if err == nil {
		t.Fatal("expected duplicate name error, got nil")
	}
}

func TestResolveNoUnits(t *testing.T) {
	plan, err := resolver.Resolve(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Order) != 0 {
		t.Errorf("expected empty order, got %v", names(plan))
	}
}

func TestResolveAfterOrdering(t *testing.T) {
	// After is ordering-only (no readiness gate); still must be respected.
	units := []*unit.Unit{
		makeUnit("a", nil, []string{"b"}),
		makeUnit("b", nil, nil),
	}
	plan, err := resolver.Resolve(units)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := names(plan)
	pos := func(name string) int {
		for i, n := range got {
			if n == name {
				return i
			}
		}
		return -1
	}
	if pos("b") >= pos("a") {
		t.Errorf("b must start before a, got %v", got)
	}
}
