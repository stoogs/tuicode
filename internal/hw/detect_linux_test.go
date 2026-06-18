//go:build linux

package hw

import (
	"context"
	"errors"
	"testing"
)

func TestParseMeminfo(t *testing.T) {
	content := `MemTotal:       32000000 kB
MemFree:         1000000 kB
MemAvailable:   16000000 kB
Buffers:          500000 kB`
	total, avail := ParseMeminfo(content)
	if total != 32000000*1024 {
		t.Errorf("total = %d", total)
	}
	if avail != 16000000*1024 {
		t.Errorf("avail = %d", avail)
	}
}

func TestDetectGPU(t *testing.T) {
	defer func() { runNvidiaSMI = realSMI }()
	runNvidiaSMI = func(ctx context.Context, args ...string) (string, error) {
		return "16384, 15000, NVIDIA GeForce RTX 4080\n", nil
	}
	g, name, unified, ok := detectGPU(context.Background())
	if !ok {
		t.Fatal("should detect GPU")
	}
	if g.Total != 16384*1024*1024 {
		t.Errorf("total = %d", g.Total)
	}
	if name != "NVIDIA GeForce RTX 4080" {
		t.Errorf("name = %q", name)
	}
	if unified {
		t.Errorf("a discrete NVIDIA GPU is not unified memory")
	}
}

func TestDetectGPUAbsent(t *testing.T) {
	defer func() { runNvidiaSMI = realSMI }()
	runNvidiaSMI = func(ctx context.Context, args ...string) (string, error) {
		return "", errors.New("nvidia-smi: not found")
	}
	_, _, _, ok := detectGPU(context.Background())
	if ok {
		t.Fatal("should not detect GPU")
	}
}

var realSMI = runNvidiaSMI
