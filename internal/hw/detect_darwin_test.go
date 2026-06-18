//go:build darwin

package hw

import "testing"

func TestParseVMStat(t *testing.T) {
	// Real-ish vm_stat output with a 16K page size (Apple Silicon).
	out := `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                               10000.
Pages active:                            500000.
Pages inactive:                           20000.
Pages speculative:                         5000.
Pages wired down:                        300000.`
	got := parseVMStat(out)
	// (10000 + 20000 + 5000) * 16384 bytes
	want := int64((10000 + 20000 + 5000) * 16384)
	if got != want {
		t.Errorf("parseVMStat = %d, want %d", got, want)
	}
}

func TestParseVMStatDefaultPageSize(t *testing.T) {
	// No header → fall back to a 4K page size.
	out := "Pages free:                               1000.\nPages inactive:                            500."
	got := parseVMStat(out)
	want := int64((1000 + 500) * 4096)
	if got != want {
		t.Errorf("parseVMStat = %d, want %d", got, want)
	}
}

func TestExtractInt(t *testing.T) {
	cases := map[string]int64{
		"  123456.":            123456,
		"page size of 16384 b": 16384,
		"":                     0,
		"none":                 0,
	}
	for in, want := range cases {
		if got := extractInt(in); got != want {
			t.Errorf("extractInt(%q) = %d, want %d", in, got, want)
		}
	}
}
