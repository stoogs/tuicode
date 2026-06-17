package hw

import (
	"strconv"
	"strings"
)

// BytesPerParamGB returns the rough (generous) GB-per-billion-params footprint
// for a quantization level. Q4≈0.55, Q5≈0.65, Q6≈0.8, Q8≈1.0.
func BytesPerParamGB(quant string) float64 {
	q := strings.ToUpper(quant)
	switch {
	case strings.Contains(q, "Q2"):
		return 0.40
	case strings.Contains(q, "Q3"):
		return 0.48
	case strings.Contains(q, "Q4"):
		return 0.55
	case strings.Contains(q, "Q5"):
		return 0.65
	case strings.Contains(q, "Q6"):
		return 0.80
	case strings.Contains(q, "Q8"):
		return 1.00
	case strings.Contains(q, "F16"), strings.Contains(q, "FP16"), strings.Contains(q, "BF16"):
		return 2.00
	default:
		return 0.65 // unknown → assume Q5-ish
	}
}

// ParseParamsBillions extracts a parameter count in billions from a string like
// "30B", "270M", "7b", or "8x7B". Returns 0 if unparseable.
func ParseParamsBillions(s string) float64 {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0
	}
	// Handle MoE notation "8X7B" → treat as 8*7 = 56B total params (generous).
	if x := strings.IndexByte(s, 'X'); x > 0 {
		a := parseNumSuffix(s[:x] + "B")
		b := parseNumSuffix(s[x+1:])
		if a > 0 && b > 0 {
			return a * b
		}
	}
	return parseNumSuffix(s)
}

// parseNumSuffix parses a number with a B/M suffix into billions.
func parseNumSuffix(s string) float64 {
	s = strings.TrimSpace(strings.ToUpper(s))
	var mult float64 = 1
	switch {
	case strings.HasSuffix(s, "B"):
		s = strings.TrimSuffix(s, "B")
		mult = 1
	case strings.HasSuffix(s, "M"):
		s = strings.TrimSuffix(s, "M")
		mult = 0.001
	}
	s = strings.TrimSpace(s)
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v * mult
}

// Standard context rungs we round down to.
var contextRungs = []int{4096, 8192, 16384, 32768, 65536, 131072, 262144}

// Estimate is the result of a context-size estimation.
type Estimate struct {
	ParamsBillions float64
	Quant          string
	FootprintGB    float64
	BudgetGB       float64
	MaxContext     int    // recommended, rounded to a rung
	RawTokens      int    // pre-rounding token budget
	Source         string // "gpu" or "ram"
	Warning        string // set when below the comfortable threshold or model can't fit
	Fits           bool   // whether the model footprint fits at all
}

// EstimateContext estimates a safe context length for a model given detected
// memory, parameter count, quantization, and an optional model max (0 = unknown).
func EstimateContext(d Detection, paramsB float64, quant string, modelMax int) Estimate {
	mem := d.Authoritative()
	reserve := d.Reserve()
	footprintGB := paramsB * BytesPerParamGB(quant)

	e := Estimate{
		ParamsBillions: paramsB,
		Quant:          quant,
		FootprintGB:    footprintGB,
		Source:         mem.Source,
	}

	totalGB := float64(mem.Total) / gib
	reserveGB := float64(reserve) / gib

	budget := (totalGB - footprintGB - reserveGB) * 0.9
	e.BudgetGB = budget
	if budget <= 0 || paramsB <= 0 {
		e.Fits = footprintGB+reserveGB <= totalGB
		e.MaxContext = 0
		if !e.Fits {
			e.Warning = "model may not fit in " + mem.Source + " — expect CPU spill or OOM"
		} else {
			e.Warning = "not enough headroom for a useful context"
		}
		return e
	}
	e.Fits = true

	// gb_per_1k ≈ params_billions * 0.0021
	gbPer1k := paramsB * 0.0021
	if gbPer1k <= 0 {
		gbPer1k = 0.0021
	}
	rawTokens := int(budget / gbPer1k * 1000)
	e.RawTokens = rawTokens

	ctx := roundDownRung(rawTokens)
	if modelMax > 0 && ctx > modelMax {
		ctx = modelMax
	}
	e.MaxContext = ctx

	switch {
	case ctx < 32768:
		e.Warning = "context below ~32k — local tool-calling may be flaky"
	case ctx < 65536:
		e.Warning = "below the 64k target — tool-calling reliability may suffer"
	}
	return e
}

// Usage is a projected memory footprint for a model at a chosen context, in GB.
type Usage struct {
	WeightsGB float64 // model weights footprint
	KVGB      float64 // KV cache for the chosen context
	TotalGB   float64 // weights + KV
	Known     bool    // false when params/quant unknown (estimate not meaningful)
}

// EstimateUsage projects VRAM/RAM usage for a model loaded at the given context.
// context <= 0 means the model's default; we assume a nominal 4k for the KV term
// so "default" still shows a sensible baseline. Uses the same gb_per_1k heuristic
// as EstimateContext, so changing the context changes the KV term linearly.
func EstimateUsage(paramsB float64, quant string, context int) Usage {
	if paramsB <= 0 {
		return Usage{Known: false}
	}
	weights := paramsB * BytesPerParamGB(quant)
	ctx := context
	if ctx <= 0 {
		ctx = 4096 // nominal default for the baseline KV estimate
	}
	gbPer1k := paramsB * 0.0021
	kv := float64(ctx) / 1000.0 * gbPer1k
	return Usage{
		WeightsGB: weights,
		KVGB:      kv,
		TotalGB:   weights + kv,
		Known:     true,
	}
}

// roundDownRung rounds a token count down to the nearest standard rung.
func roundDownRung(tokens int) int {
	rung := contextRungs[0]
	for _, r := range contextRungs {
		if tokens >= r {
			rung = r
		}
	}
	if tokens < contextRungs[0] {
		// Below the smallest rung; still offer the smallest as a floor.
		return contextRungs[0]
	}
	return rung
}
