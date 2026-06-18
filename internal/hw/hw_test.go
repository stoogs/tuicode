package hw

import (
	"testing"
)

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
	// Q8 should cost more per param than Q4, which costs more than Q3.
	if !(BytesPerParamGB("Q3_K_M") < BytesPerParamGB("Q4_K_M") &&
		BytesPerParamGB("Q4_K_M") < BytesPerParamGB("Q8_0")) {
		t.Errorf("expected Q3 < Q4 < Q8, got %v %v %v",
			BytesPerParamGB("Q3_K_M"), BytesPerParamGB("Q4_K_M"), BytesPerParamGB("Q8_0"))
	}
}

func TestEstimateUsageUsesFileSize(t *testing.T) {
	// When the on-disk size is known it drives the weights figure (more accurate
	// than params×quant), and a bigger context grows the KV term.
	const gib14 = 14 * gib
	small := EstimateUsage(14, "Q4_K_M", 8192, gib14)
	big := EstimateUsage(14, "Q4_K_M", 65536, gib14)
	if small.WeightsGB < 13.9 || small.WeightsGB > 14.1 {
		t.Errorf("weights should track file size (~14GB), got %.2f", small.WeightsGB)
	}
	if big.KVGB <= small.KVGB {
		t.Errorf("larger context should grow KV: %.2f vs %.2f", big.KVGB, small.KVGB)
	}
	if big.TotalGB <= big.WeightsGB {
		t.Errorf("total should exceed weights (KV + overhead)")
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

func unifiedDetection(totalGB float64) Detection {
	b := int64(totalGB * gib)
	return Detection{
		Mode:    Auto,
		GPU:     &Memory{Source: "unified", Total: b, Free: b},
		HasGPU:  true,
		Unified: true,
		RAM:     Memory{Source: "ram", Total: b, Free: b},
	}
}

func TestUnifiedReserveIsProportional(t *testing.T) {
	// ~30% of the pool is kept free, scaling with the machine.
	for _, totalGB := range []float64{8, 16, 64} {
		got := float64(unifiedDetection(totalGB).Reserve()) / gib
		want := totalGB * unifiedReserveFraction
		if got < want-0.1 || got > want+0.1 {
			t.Errorf("%gGB unified reserve = %.1fGB, want ~%.1fGB", totalGB, got, want)
		}
	}
}

func TestUnifiedCapsHeadroomVsDiscreteGPU(t *testing.T) {
	// An ~11.7GB model (18B Q4) fits a discrete 16GB GPU (2GB reserve) but must
	// be flagged on 16GB unified memory, where ~4.8GB stays free for the OS/apps.
	if e := EstimateContext(gpuDetection(16), 18, "Q4_K_M", 0); !e.Fits {
		t.Errorf("18B Q4 should fit a discrete 16GB GPU")
	}
	if e := EstimateContext(unifiedDetection(16), 18, "Q4_K_M", 0); e.Fits {
		t.Errorf("18B Q4 should not fit comfortably in 16GB unified memory")
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

func TestEstimateUsageSlidingWindow(t *testing.T) {
	gib := 1024.0 * 1024 * 1024 // float var so the non-integral product converts
	// Gemma-style: 11.9B Q4 (~6.9GB), 48 layers, 1024 window, 128k context.
	weights := int64(6.9 * gib)
	naive := EstimateUsage(11.9, "Q4_K_M", 131072, weights)
	sw := EstimateUsageArch(11.9, "Q4_K_M", 131072, weights, 48, 1024)

	if !sw.Known || !naive.Known {
		t.Fatal("expected known usage")
	}
	// Sliding window must dramatically cut the KV term vs full attention.
	if sw.KVGB >= naive.KVGB*0.5 {
		t.Errorf("sliding-window KV %.2f not much less than naive %.2f", sw.KVGB, naive.KVGB)
	}
	// Naive badly overshoots (~17.5GB); sliding-window lands far closer to the
	// real ~11.6GB and never below the weights.
	if naive.TotalGB < 15 {
		t.Errorf("expected naive to overshoot (>15GB), got %.1f", naive.TotalGB)
	}
	if sw.TotalGB > 13 || sw.TotalGB < sw.WeightsGB {
		t.Errorf("sliding-window total %.1f out of sane range", sw.TotalGB)
	}
	// Below the window, sliding window changes nothing.
	if EstimateUsageArch(11.9, "Q4_K_M", 1024, weights, 48, 1024).KVGB !=
		EstimateUsage(11.9, "Q4_K_M", 1024, weights).KVGB {
		t.Error("sliding window should not affect ctx <= window")
	}
}
