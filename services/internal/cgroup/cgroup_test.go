package cgroup

import (
	"testing"
)

func TestParseMemory(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  bool
	}{
		{"", "max", false},
		{"0", "max", false},
		{"max", "max", false},
		{"MAX", "max", false},
		{"128M", "134217728", false},
		{"256m", "268435456", false},
		{"1G", "1073741824", false},
		{"512K", "524288", false},
		{"1024", "1024", false},
		{"bad", "", true},
	}

	for _, c := range cases {
		got, err := parseMemory(c.in)
		if c.err {
			if err == nil {
				t.Errorf("parseMemory(%q): expected error, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseMemory(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseMemory(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestClampCPUWeight(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{0, 100},
		{-1, 100},
		{100, 100},
		{200, 200},
		{10000, 10000},
		{99999, 10000},
	}
	for _, c := range cases {
		got := clampCPUWeight(c.in)
		if got != c.want {
			t.Errorf("clampCPUWeight(%d) = %d; want %d", c.in, got, c.want)
		}
	}
}
