//go:build darwin

package hw

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// detectRAM reads total physical memory via `sysctl hw.memsize` and an
// available-memory figure derived from `vm_stat`. Best-effort: a zero Total on
// failure is handled gracefully downstream.
func detectRAM() Memory {
	m := Memory{Source: "ram"}
	m.Total = sysctlInt64("hw.memsize")
	m.Free = vmStatAvailable()
	return m
}

// detectGPU reports the GPU memory pool. On Apple Silicon the GPU uses unified
// memory — the same physical RAM as the CPU — so we surface the whole pool with
// the chip name and unified=true. On Intel Macs there is no CUDA-class GPU for
// Ollama (it runs on CPU), so we report no GPU and let RAM be authoritative.
func detectGPU(ctx context.Context) (mem Memory, name string, unified, ok bool) {
	if runtime.GOARCH != "arm64" {
		return Memory{}, "", false, false
	}
	ram := detectRAM()
	if ram.Total <= 0 {
		return Memory{}, "", false, false
	}
	name = sysctlString("machdep.cpu.brand_string") // e.g. "Apple M3 Pro"
	if name == "" {
		name = "Apple Silicon"
	}
	return Memory{Source: "unified", Total: ram.Total, Free: ram.Free}, name, true, true
}

// sysctlInt64 returns an integer sysctl value (0 on any failure).
func sysctlInt64(name string) int64 {
	v, err := strconv.ParseInt(sysctlString(name), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// sysctlString returns a string sysctl value, trimmed ("" on failure).
func sysctlString(name string) string {
	out, err := exec.Command("sysctl", "-n", name).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// vmStatAvailable parses `vm_stat` for an "available memory" estimate: the
// free, inactive, and speculative page counts times the page size. This mirrors
// what Activity Monitor treats as reclaimable; it errs slightly generous, which
// the estimator's reserve + 0.9 budget factor already accounts for.
func vmStatAvailable() int64 {
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0
	}
	return parseVMStat(string(out))
}

// parseVMStat extracts available bytes from vm_stat output. Exported-style logic
// kept testable as a pure function.
func parseVMStat(s string) int64 {
	var pageSize int64 = 4096 // default; overridden by the header when present
	var free, inactive, speculative int64

	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Header carries the page size: "...(page size of 16384 bytes)".
		if strings.Contains(line, "page size of") {
			if ps := extractInt(line); ps > 0 {
				pageSize = ps
			}
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		val := extractInt(line[colon+1:]) // page counts, e.g. "  123456."
		switch key {
		case "Pages free":
			free = val
		case "Pages inactive":
			inactive = val
		case "Pages speculative":
			speculative = val
		}
	}
	return (free + inactive + speculative) * pageSize
}

// extractInt pulls the first run of digits out of s (ignoring trailing '.' etc).
func extractInt(s string) int64 {
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			if start < 0 {
				start = i
			}
		} else if start >= 0 {
			v, _ := strconv.ParseInt(s[start:i], 10, 64)
			return v
		}
	}
	if start >= 0 {
		v, _ := strconv.ParseInt(s[start:], 10, 64)
		return v
	}
	return 0
}
