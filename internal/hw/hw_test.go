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
	g, name, ok := detectGPU(context.Background())
	if !ok {
		t.Fatal("should detect GPU")
	}
	if g.Total != 16384*1024*1024 {
		t.Errorf("total = %d", g.Total)
	}
	if name != "NVIDIA GeForce RTX 4080" {
		t.Errorf("name = %q", name)
	}
}

func TestDetectGPUAbsent(t *testing.T) {
	defer func() { runNvidiaSMI = realSMI }()
	runNvidiaSMI = func(ctx context.Context, args ...string) (string, error) {
		return "", errors.New("nvidia-smi: not found")
	}
	_, _, ok := detectGPU(context.Background())
	if ok {
		t.Fatal("should not detect GPU")
	}
}

var realSMI = runNvidiaSMI

func TestParseParamsBillions(t *testing.T) {
	cases := map[string]float64{
		"30B":  30,
		"270M": 0.27,
		"7b":   7,
		"8x7B": 56,
		"":     0,
	}
	for in, want := range cases {
		if got := ParseParamsBillions(in); got != want {
			t.Errorf("ParseParamsBillions(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestBytesPerParamGB(t *testing.T) {
	if BytesPerParamGB("Q4_K_M") != 0.55 {
		t.Errorf("Q4 = %v", BytesPerParamGB("Q4_K_M"))
	}
	if BytesPerParamGB("Q8_0") != 1.0 {
		t.Errorf("Q8 = %v", BytesPerParamGB("Q8_0"))
	}
}

func gpuDetection(totalGB float64) Detection {
	b := int64(totalGB * gib)
	return Detection{
		Mode:   Auto,
		GPU:    &Memory{Source: "gpu", Total: b, Free: b},
		HasGPU: true,
		RAM:    Memory{Source: "ram", Total: 64 * gib, Free: 60 * gib},
	}
}

func TestEstimateContextMonotonic(t *testing.T) {
	// More memory → larger-or-equal context.
	prev := 0
	for _, totalGB := range []float64{8, 12, 16, 24, 48} {
		e := EstimateContext(gpuDetection(totalGB), 30, "Q4_K_M", 0)
		if e.MaxContext < prev {
			t.Errorf("non-monotonic: %dGB → ctx %d < prev %d", int(totalGB), e.MaxContext, prev)
		}
		prev = e.MaxContext
	}
}

func TestEstimateRespectsModelMax(t *testing.T) {
	e := EstimateContext(gpuDetection(80), 7, "Q4_K_M", 32768)
	if e.MaxContext > 32768 {
		t.Errorf("ctx %d exceeds model max 32768", e.MaxContext)
	}
}

func TestEstimateRoundsToRung(t *testing.T) {
	e := EstimateContext(gpuDetection(24), 30, "Q4_K_M", 0)
	valid := false
	for _, r := range contextRungs {
		if e.MaxContext == r {
			valid = true
		}
	}
	if e.MaxContext != 0 && !valid {
		t.Errorf("ctx %d not a standard rung", e.MaxContext)
	}
}

func TestEstimateCPUMode(t *testing.T) {
	d := Detection{
		Mode: CPUOnly,
		GPU:  &Memory{Source: "gpu", Total: 8 * gib, Free: 8 * gib},
		RAM:  Memory{Source: "ram", Total: 64 * gib, Free: 60 * gib},
	}
	e := EstimateContext(d, 7, "Q4_K_M", 0)
	if e.Source != "ram" {
		t.Errorf("cpu-only should use RAM, got %q", e.Source)
	}
}

func TestEstimateWontFit(t *testing.T) {
	// 70B Q8 ≈ 70GB footprint, won't fit in 16GB GPU.
	e := EstimateContext(gpuDetection(16), 70, "Q8_0", 0)
	if e.Fits {
		t.Errorf("70B Q8 should not fit in 16GB")
	}
	if e.Warning == "" {
		t.Errorf("expected a warning")
	}
}
